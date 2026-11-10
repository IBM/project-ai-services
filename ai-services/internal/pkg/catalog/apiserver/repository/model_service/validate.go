package modelservice

import (
	"context"
	"fmt"
	"slices"
	"strings"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// localStatuses are the statuses of local models (component statuses); remoteStatuses are the
// statuses of remote ones (connector statuses).
var (
	localStatuses = []string{
		string(dbmodels.ComponentStatusInitializing),
		string(dbmodels.ComponentStatusDownloading),
		string(dbmodels.ComponentStatusDeploying),
		string(dbmodels.ComponentStatusRunning),
		string(dbmodels.ComponentStatusDeleting),
		string(dbmodels.ComponentStatusError),
	}
	remoteStatuses = []string{
		string(dbmodels.ConnectorStatusConnected),
		string(dbmodels.ConnectorStatusOffline),
	}
)

// statusesFor returns the statuses a model of the deployment type can have; without a deployment
// type, any model can match, so it returns both sets.
func statusesFor(deploymentType constants.DeploymentType) []string {
	switch deploymentType {
	case constants.DeploymentTypeLocal:
		return localStatuses
	case constants.DeploymentTypeRemote:
		return remoteStatuses
	default:
		return slices.Concat(localStatuses, remoteStatuses)
	}
}

// validateCreateRequest performs the checks on a create request that are not expressed as
// binding tags on apimodels.CreateModelRequest (required fields, name length and deployment_type
// are enforced there). It is a pure function so it can be tested without any service dependencies.
func validateCreateRequest(req apimodels.CreateModelRequest) error {
	if err := validators.ValidateResourceName("Model name", req.Name); err != nil {
		return err
	}

	// TODO(model-validation): validate req.Model against the catalog models (the upcoming
	// GET /catalog/models listing, or the findModel helper) and reject unknown models with a 400.
	// The model is no longer part of the provider params schema, so nothing validates it today.

	return nil
}

// ensureNameAvailable rejects a name already used by a local model (component) or a remote
// model connector. Names share one namespace because both are listed together by GET /models.
func (s *ModelService) ensureNameAvailable(ctx context.Context, name string) error {
	component, err := s.componentRepo.GetByName(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to check component name uniqueness: %w", err)
	}
	if component != nil {
		return validators.Conflict("A model with name %q already exists", name)
	}

	connector, err := s.connectorRepo.GetByName(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to check connector name uniqueness: %w", err)
	}
	if connector != nil {
		return validators.Conflict("A connector with name %q already exists", name)
	}

	return nil
}

// validateListRequest rejects filters that can never match: a deployment type that is neither
// local nor remote, a provider or model the catalog does not know, or a status no model can have.
// Empty filters are not applied and so not checked; of the multi-value filters, every value must
// be valid. The providers and the status are checked against those of the requested deployment
// type, if any: status=connected&deployment_type=local can never match.
//
// component_type is not validated yet.
func (s *ModelService) validateListRequest(req apimodels.ListModelsRequest) error {
	if t := req.DeploymentType; t != "" && t != constants.DeploymentTypeLocal && t != constants.DeploymentTypeRemote {
		return validators.BadRequest("deployment_type must be '%s' or '%s'", constants.DeploymentTypeLocal, constants.DeploymentTypeRemote)
	}

	if err := s.validateProviderIDs(req.ProviderIDs, req.DeploymentType); err != nil {
		return err
	}

	if err := s.validateModels(req.Models); err != nil {
		return err
	}

	if statuses := statusesFor(req.DeploymentType); req.Status != "" && !slices.Contains(statuses, req.Status) {
		return validators.BadRequest("invalid status %q; must be one of: %s", req.Status, strings.Join(statuses, ", "))
	}

	return nil
}

// validateProviderIDs checks that every provider is one the catalog offers for the deployment type.
func (s *ModelService) validateProviderIDs(providerIDs []string, deploymentType constants.DeploymentType) error {
	if len(providerIDs) == 0 {
		return nil
	}

	providers := s.catalogProvider.GetModelProviders(deploymentType)
	known := make([]string, 0, len(providers))

	for _, p := range providers {
		known = append(known, p.ID)
	}

	return requireKnown("provider_id", providerIDs, known)
}

// validateModels checks that every model is one the catalog lists.
func (s *ModelService) validateModels(modelIDs []string) error {
	if len(modelIDs) == 0 {
		return nil
	}

	models, err := s.catalogProvider.GetModels()
	if err != nil {
		return fmt.Errorf("failed to list catalog models: %w", err)
	}

	known := make([]string, 0, len(models))

	for _, m := range models {
		known = append(known, m.ID)
	}

	return requireKnown("model", modelIDs, known)
}

// requireKnown fails with a 400 on the first value that is not one of the known ids. The ids are
// sorted in the message so it is stable: the catalog lists its items in no particular order.
func requireKnown(field string, values, known []string) error {
	for _, v := range values {
		if !slices.Contains(known, v) {
			return validators.BadRequest("invalid %s %q; must be one of: %s", field, v, strings.Join(slices.Sorted(slices.Values(known)), ", "))
		}
	}

	return nil
}
