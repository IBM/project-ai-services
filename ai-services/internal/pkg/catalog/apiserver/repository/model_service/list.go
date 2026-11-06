package modelservice

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
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
		DeploymentType: string(req.DeploymentType),
		ComponentType:  req.ComponentType,
		Providers:      req.ProviderIDs,
		Models:         req.Models,
		Status:         req.Status,
		Limit:          req.PageSize,
		Offset:         (req.Page - 1) * req.PageSize,
	}

	total, err := s.modelRepo.GetCount(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to count models: %w", err)
	}

	rows, err := s.modelRepo.List(ctx, filters)
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

	totalPages := 0
	if total > 0 {
		totalPages = (total + req.PageSize - 1) / req.PageSize
	}

	return &apimodels.ModelListResponse{
		Data: items,
		Pagination: types.PaginationMetadata{
			Page:       req.Page,
			PageSize:   req.PageSize,
			TotalItems: total,
			TotalPages: totalPages,
			HasNext:    req.Page < totalPages,
			HasPrev:    req.Page > 1,
		},
	}, nil
}

// connectedServices counts the services using each model of the page, in one query per
// dependency type: local models are linked as components, remote ones as connectors.
func (s *ModelService) connectedServices(ctx context.Context, rows []dbrepo.ModelRow) (map[uuid.UUID]int, error) {
	var localIDs, remoteIDs []uuid.UUID

	for _, row := range rows {
		if row.DeploymentType == constants.DeploymentTypeLocal {
			localIDs = append(localIDs, row.ID)
		} else {
			remoteIDs = append(remoteIDs, row.ID)
		}
	}

	counts := make(map[uuid.UUID]int, len(rows))

	for depType, ids := range map[dbmodels.DependencyType][]uuid.UUID{
		dbmodels.DependencyTypeComponent: localIDs,
		dbmodels.DependencyTypeConnector: remoteIDs,
	} {
		got, err := s.svcDepRepo.GetServiceCountByDependency(ctx, ids, depType)
		if err != nil {
			return nil, fmt.Errorf("failed to count services using models: %w", err)
		}

		for id, n := range got {
			counts[id] = n
		}
	}

	return counts, nil
}

// providerNames maps "<deployment type>/<provider id>" to the provider's display name, taken from
// the catalog once per request instead of once per row.
func (s *ModelService) providerNames() map[string]string {
	names := make(map[string]string)

	for _, deploymentType := range []string{constants.DeploymentTypeLocal, constants.DeploymentTypeRemote} {
		for _, p := range s.catalogProvider.GetModelProviders(deploymentType) {
			names[providerKey(deploymentType, p.ID)] = p.Name
		}
	}

	return names
}

func providerKey(deploymentType, providerID string) string {
	return deploymentType + "/" + providerID
}

// providerInfo is the provider of a model with its display name, which falls back to the id when
// the catalog no longer lists the provider (e.g. a removed bundle).
func providerInfo(row dbrepo.ModelRow, providers map[string]string) types.ProviderInfo {
	name, ok := providers[providerKey(row.DeploymentType, row.Provider)]
	if !ok {
		name = row.Provider
	}

	return types.ProviderInfo{ID: row.Provider, Name: name}
}

// workerInfo is the worker a local model runs on, or nil when the model has none.
func workerInfo(row dbrepo.ModelRow) *types.ApplicationWorker {
	if row.WorkerName == "" {
		return nil
	}

	return &types.ApplicationWorker{ID: row.WorkerID, Name: row.WorkerName, RuntimeType: row.WorkerRuntimeType}
}

// toModelListItem maps a database row to its API shape.
func toModelListItem(row dbrepo.ModelRow, providers map[string]string, services int) apimodels.ModelListItem {
	item := apimodels.ModelListItem{
		ID:                row.ID.String(),
		DeploymentType:    apimodels.DeploymentType(row.DeploymentType),
		Name:              row.Name,
		Provider:          providerInfo(row, providers),
		Model:             row.Model,
		Params:            row.Params,
		Status:            row.Status,
		Message:           row.Message,
		ConnectedServices: services,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}

	item.Worker = workerInfo(row)

	return item
}
