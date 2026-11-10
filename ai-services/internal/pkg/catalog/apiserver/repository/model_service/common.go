package modelservice

import (
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// This file holds what the list and get operations share: the catalog lookups and the mapping of a
// database row to the API shape common to both responses.

// modelComponentTypes returns the component types that are models, from the catalog: the types whose
// providers declare `category: models`. Only components and connectors of these types are models.
// The result is never nil, so that a catalog without model types yields no models instead of
// being read as "no restriction".
func (s *ModelService) modelComponentTypes() []string {
	types := helpers.New(s.catalogProvider).ComponentTypes()
	if types == nil {
		return []string{}
	}

	return types
}

// providerNames maps "<deployment type>/<provider id>" to the provider's display name, taken from
// the catalog once per request instead of once per row.
func (s *ModelService) providerNames() map[string]string {
	names := make(map[string]string)

	for _, deploymentType := range []constants.DeploymentType{constants.DeploymentTypeLocal, constants.DeploymentTypeRemote} {
		for _, p := range s.catalogProvider.GetModelProviders(deploymentType) {
			names[providerKey(deploymentType, p.ID)] = p.Name
		}
	}

	return names
}

func providerKey(deploymentType constants.DeploymentType, providerID string) string {
	return string(deploymentType) + "/" + providerID
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

// modelDependencyType is how services link to a model: local models as components, remote ones
// as connectors.
func modelDependencyType(row dbrepo.ModelRow) dbmodels.DependencyType {
	if row.DeploymentType == constants.DeploymentTypeLocal {
		return dbmodels.DependencyTypeComponent
	}

	return dbmodels.DependencyTypeConnector
}

// toModelBase maps a database row to the fields shared by the list and detail responses.
// services is the number of services using the model.
func toModelBase(row dbrepo.ModelRow, providers map[string]string, services int) apimodels.ModelBase {
	return apimodels.ModelBase{
		ID:                row.ID.String(),
		DeploymentType:    row.DeploymentType,
		Name:              row.Name,
		Provider:          providerInfo(row, providers),
		Worker:            workerInfo(row),
		Model:             row.Model,
		Params:            row.Params,
		Status:            row.Status,
		Message:           row.Message,
		ConnectedServices: services,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}
