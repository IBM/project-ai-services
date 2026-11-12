package helpers

import (
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// This file holds what the model list and get operations share: the catalog lookups and the
// mapping of a database row to the API shape common to both responses.

// QueryComponentTypes returns the component types that are models, to scope a repository query:
// only components and connectors of these types are models. Unlike ComponentTypes, the result is
// never nil, so that a catalog without model types yields no models instead of being read as "no
// restriction".
func (m ModelCatalog) QueryComponentTypes() []string {
	names := m.ComponentTypes()
	if names == nil {
		return []string{}
	}

	return names
}

// ProviderNames maps "<deployment type>/<provider id>" to the provider's display name, taken from
// the catalog once per request instead of once per row.
func (m ModelCatalog) ProviderNames() map[string]string {
	names := make(map[string]string)

	for _, deploymentType := range []catalogconstants.DeploymentType{catalogconstants.DeploymentTypeLocal, catalogconstants.DeploymentTypeRemote} {
		for _, p := range m.provider.GetModelProviders(deploymentType) {
			names[providerKey(deploymentType, p.ID)] = p.Name
		}
	}

	return names
}

// ComponentTypeInfos maps a model component type id to its id and display names, taken from the
// catalog once per request instead of once per row.
func (m ModelCatalog) ComponentTypeInfos() map[string]types.ComponentType {
	comps := m.Components()
	infos := make(map[string]types.ComponentType)

	for _, id := range m.ComponentTypes() {
		opts := &types.ModelDeployOptions{ComponentType: types.ComponentType{ID: id}}
		catalogmodels.SetComponentTypeNames(opts, comps)
		infos[id] = opts.ComponentType
	}

	return infos
}

// componentTypeInfo is the component type of a model with its display names, which are left empty
// when the catalog no longer lists the type.
func componentTypeInfo(row dbrepo.ModelRow, componentTypes map[string]types.ComponentType) types.ComponentType {
	if ct, ok := componentTypes[row.ComponentType]; ok {
		return ct
	}

	return types.ComponentType{ID: row.ComponentType}
}

func providerKey(deploymentType catalogconstants.DeploymentType, providerID string) string {
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

// ModelDependencyType is how services link to a model: local models as components, remote ones
// as connectors.
func ModelDependencyType(row dbrepo.ModelRow) dbmodels.DependencyType {
	if row.DeploymentType == catalogconstants.DeploymentTypeLocal {
		return dbmodels.DependencyTypeComponent
	}

	return dbmodels.DependencyTypeConnector
}

// ToModelBase maps a database row to the fields shared by the list and detail responses.
// providers comes from ProviderNames and componentTypes from ComponentTypeInfos; services is the
// number of services using the model.
func ToModelBase(row dbrepo.ModelRow, providers map[string]string, componentTypes map[string]types.ComponentType, services int) apimodels.ModelBase {
	return apimodels.ModelBase{
		ID:                row.ID.String(),
		DeploymentType:    row.DeploymentType,
		Name:              row.Name,
		ComponentType:     componentTypeInfo(row, componentTypes),
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
