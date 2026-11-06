package modelservice

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// GetModelByID returns one model, local or remote.
//
// As for applications, a missing model is a 404 and a model created by someone else a 403; an empty
// createdBy skips the ownership check.
func (s *ModelService) GetModelByID(ctx context.Context, id uuid.UUID, createdBy string) (*apimodels.ModelDetail, error) {
	row, err := s.modelRepo.GetByID(ctx, id, s.modelComponentTypes())
	if err != nil {
		return nil, fmt.Errorf("failed to get model: %w", err)
	}

	if row == nil {
		return nil, validators.NotFound("model %q not found", id)
	}

	if createdBy != "" && row.CreatedBy != createdBy {
		return nil, validators.Forbidden("user does not own this model")
	}

	depType := modelDependencyType(*row)

	applications, err := s.modelApplications(ctx, row.ID, depType)
	if err != nil {
		return nil, err
	}

	counts, err := s.svcDepRepo.GetServiceCountByDependency(ctx, []uuid.UUID{row.ID}, depType)
	if err != nil {
		return nil, fmt.Errorf("failed to count services using model: %w", err)
	}

	detail := &apimodels.ModelDetail{
		ID:                row.ID.String(),
		DeploymentType:    apimodels.DeploymentType(row.DeploymentType),
		Name:              row.Name,
		Provider:          providerInfo(*row, s.providerNames()),
		Worker:            workerInfo(*row),
		Model:             row.Model,
		Params:            row.Params,
		Status:            row.Status,
		Message:           row.Message,
		Applications:      applications,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		Endpoints:         modelEndpoints(),
		ConnectedServices: counts[row.ID],
	}

	return detail, nil
}

// modelEndpoints returns the endpoint through which a model, local or remote, is called: the public
// LiteLLM gateway.
// The url is the value of LITELLM_PUBLIC_ENDPOINT exactly as set, empty when it is not.
func modelEndpoints() []map[string]any {
	return []map[string]any{{
		"type": constants.ModelEndpointTypeExternal,
		"url":  os.Getenv(constants.LiteLLMPublicEndpointEnv),
	}}
}

// modelDependencyType is how services link to a model: local models as components, remote ones
// as connectors.
func modelDependencyType(row dbrepo.ModelRow) dbmodels.DependencyType {
	if row.DeploymentType == constants.DeploymentTypeLocal {
		return dbmodels.DependencyTypeComponent
	}

	return dbmodels.DependencyTypeConnector
}

// modelApplications returns the applications that use a model, one entry per application, sorted
// by name.
func (s *ModelService) modelApplications(ctx context.Context, id uuid.UUID, depType dbmodels.DependencyType) ([]apimodels.ModelApplication, error) {
	linked, err := s.svcDepRepo.GetLinkedServiceEndpoints(ctx, id, depType)
	if err != nil {
		return nil, fmt.Errorf("failed to get applications using model: %w", err)
	}

	// Several services of one application can use the same model: list the application once.
	seen := make(map[uuid.UUID]bool, len(linked))
	applications := make([]apimodels.ModelApplication, 0, len(linked))

	for _, l := range linked {
		if seen[l.ApplicationID] {
			continue
		}

		seen[l.ApplicationID] = true
		applications = append(applications, apimodels.ModelApplication{ID: l.ApplicationID.String(), Name: l.ApplicationName})
	}

	slices.SortFunc(applications, func(a, b apimodels.ModelApplication) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})

	return applications, nil
}
