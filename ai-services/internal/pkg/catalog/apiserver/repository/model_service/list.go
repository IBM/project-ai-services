package modelservice

import (
	"context"
	"fmt"
	"maps"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// ListModels returns one page of the caller's models, local and remote, as a single list.
//
// The list is scoped to req.CreatedBy, as for applications.
func (s *ModelService) ListModels(ctx context.Context, req apimodels.ListModelsRequest) (*apimodels.ModelListResponse, error) {
	if err := s.validateListRequest(req); err != nil {
		return nil, err
	}

	filters := &dbrepo.ModelFilters{
		CreatedBy:      req.CreatedBy,
		DeploymentType: req.DeploymentType,
		ComponentType:  req.ComponentType,
		Providers:      req.ProviderIDs,
		Models:         req.Models,
		Status:         req.Status,
		Limit:          req.PageSize,
		Offset:         (req.Page - 1) * req.PageSize,
	}

	componentTypes := s.modelComponentTypes()

	total, err := s.modelRepo.GetCount(ctx, filters, componentTypes)
	if err != nil {
		return nil, fmt.Errorf("failed to count models: %w", err)
	}

	rows, err := s.modelRepo.List(ctx, filters, componentTypes)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}

	counts, err := s.connectedServices(ctx, rows)
	if err != nil {
		return nil, err
	}

	providers := s.providerNames()
	items := make([]apimodels.ModelListItem, 0, len(rows))

	for _, row := range rows {
		items = append(items, toModelListItem(row, providers, counts[row.ID]))
	}

	return &apimodels.ModelListResponse{
		Data:       items,
		Pagination: types.NewPaginationMetadata(req.Page, req.PageSize, total),
	}, nil
}

// connectedServices counts the services using each model of the page, in one query per
// dependency type present on the page: local models are linked as components, remote ones as
// connectors. A type with no model on the page is not queried.
func (s *ModelService) connectedServices(ctx context.Context, rows []dbrepo.ModelRow) (map[uuid.UUID]int, error) {
	groups := []struct {
		depType dbmodels.DependencyType
		ids     []uuid.UUID
	}{
		{depType: dbmodels.DependencyTypeComponent},
		{depType: dbmodels.DependencyTypeConnector},
	}

	for _, row := range rows {
		depType := modelDependencyType(row)

		for i := range groups {
			if groups[i].depType == depType {
				groups[i].ids = append(groups[i].ids, row.ID)
			}
		}
	}

	counts := make(map[uuid.UUID]int, len(rows))

	for _, g := range groups {
		if len(g.ids) == 0 {
			continue
		}

		got, err := s.svcDepRepo.GetServiceCountByDependency(ctx, g.ids, g.depType)
		if err != nil {
			return nil, fmt.Errorf("failed to count services using models: %w", err)
		}

		maps.Copy(counts, got)
	}

	return counts, nil
}

// toModelListItem maps a database row to its API shape.
func toModelListItem(row dbrepo.ModelRow, providers map[string]string, services int) apimodels.ModelListItem {
	return apimodels.ModelListItem{ModelBase: toModelBase(row, providers, services)}
}
