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
		ct := types.ComponentType{ID: id}
		catalogmodels.SetComponentTypeNames(&ct, comps)
		infos[id] = ct
	}

	return infos
}

// ProfileInfos maps "<component type>/<provider id>/<profile id>" to the profile, taken from the
// catalog once per request instead of once per row. Providers without profiles add nothing.
func (m ModelCatalog) ProfileInfos() map[string]apimodels.ModelProfile {
	infos := make(map[string]apimodels.ModelProfile)

	for _, c := range m.Components() {
		byID, err := m.provider.ProviderProfiles(c.ComponentType.ID, c.ID)
		if err != nil {
			continue
		}

		for id, p := range byID {
			infos[profileKey(c.ComponentType.ID, c.ID, id)] = apimodels.ModelProfile{ID: id, Name: p.Name, Description: p.Description}
		}
	}

	return infos
}

func profileKey(componentType, providerID, profileID string) string {
	return componentType + "/" + providerID + "/" + profileID
}

// profileInfo is the profile a local model is deployed with, or nil when it has none. The id
// alone is returned when the catalog no longer lists the profile (e.g. a removed bundle).
func profileInfo(row dbrepo.ModelRow, profiles map[string]apimodels.ModelProfile) *apimodels.ModelProfile {
	if row.Profile == "" {
		return nil
	}

	if p, ok := profiles[profileKey(row.ComponentType, row.Provider, row.Profile)]; ok {
		return &p
	}

	return &apimodels.ModelProfile{ID: row.Profile}
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
// providers comes from ProviderNames, componentTypes from ComponentTypeInfos and profiles from
// ProfileInfos; services is the number of services using the model.
func ToModelBase(row dbrepo.ModelRow, providers map[string]string, componentTypes map[string]types.ComponentType, profiles map[string]apimodels.ModelProfile, services int) apimodels.ModelBase {
	return apimodels.ModelBase{
		ID:                row.ID.String(),
		DeploymentType:    row.DeploymentType,
		Name:              row.Name,
		ComponentType:     componentTypeInfo(row, componentTypes),
		Provider:          providerInfo(row, providers),
		Worker:            workerInfo(row),
		Model:             row.Model,
		Profile:           profileInfo(row, profiles),
		Params:            row.Params,
		Status:            row.Status,
		Message:           row.Message,
		ConnectedServices: services,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}
