package catalog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	clitemplates "github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
)

const (
	deploymentTypeLocal  = "local"  // runs as a pod on a worker
	deploymentTypeRemote = "remote" // hosted externally; only registered
	modelConnectorType   = "models" // connector_type of remote model providers
)

// ErrModelNotFound is returned when no provider lists the requested model.
var ErrModelNotFound = errors.New("model not found in catalog")

// GetModelDeployOptions lists every provider that can deploy modelID.
// Local providers (vllm-spyre, vllm-cpu, ...) come with the model's hardware profiles;
// remote providers (watsonx, hosted_vllm) come with only a schema link.
func (p *CatalogProvider) GetModelDeployOptions(ctx context.Context, modelID, providerID string) (*types.ModelDeployOptions, error) {
	runtimeType, err := p.resolveRuntimeType("")
	if err != nil {
		return nil, err
	}

	result := &types.ModelDeployOptions{ID: modelID, Providers: []types.ModelDeployProvider{}}
	found := false

	for _, item := range p.allItems() {
		model, err := findModel(item, modelID)
		if err != nil {
			return nil, err
		}
		if model == nil {
			continue
		}
		found = true

		if err := p.addModelProvider(ctx, result, item, model, runtimeType); err != nil {
			return nil, err
		}
	}

	if !found {
		return nil, fmt.Errorf("%w: %s", ErrModelNotFound, modelID)
	}

	result.Providers = filterAndSortModelProviders(result.Providers, providerID)

	return result, nil
}

// addModelProvider appends the provider behind item to result, unless it cannot deploy the model here.
func (p *CatalogProvider) addModelProvider(ctx context.Context, result *types.ModelDeployOptions, item *catalogItem, model *types.Model, runtimeType string) error {
	if item.Connector != nil {
		setModelInfo(result, model.Name, model.ComponentType)
		result.Providers = append(result.Providers, types.ModelDeployProvider{
			ID:             item.Connector.ID,
			Name:           item.Connector.Name,
			DeploymentType: deploymentTypeRemote,
			Schema:         fmt.Sprintf("/api/v1/connectors/%s/providers/%s/params", item.Connector.ConnectorType, item.Connector.ID),
		})

		return nil
	}

	setModelInfo(result, model.Name, item.Component.ComponentType)

	// No profiles in models.yaml: a legacy per-app component (e.g. llm/watsonx), not deployable on its own.
	if len(model.Profiles) == 0 {
		return nil
	}
	// No metadata file for this runtime: provider is unsupported here.
	// Any other failure (e.g. corrupt YAML) is a real error and must surface.
	md, err := p.LoadComponentRuntimeMetadata(item.Component.ComponentType, item.Component.ID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return err
	}

	provider, err := p.buildLocalModelProvider(ctx, item, model, md, runtimeType)
	if err != nil {
		return err
	}
	result.Providers = append(result.Providers, *provider)

	return nil
}

// findModel returns modelID's entry from the item's models.yaml, or nil if the item doesn't list it.
// Only components and model connectors have a models.yaml; other items return nil.
func findModel(item *catalogItem, modelID string) (*types.Model, error) {
	isModelConnector := item.Connector != nil && item.Connector.ConnectorType == modelConnectorType
	if item.Component == nil && !isModelConnector {
		return nil, nil
	}

	models, err := loadYAMLList[types.Model](item.itemFS, item.Path, modelsFileName)
	if err != nil {
		return nil, err
	}
	for i := range models {
		if models[i].ID == modelID {
			return &models[i], nil
		}
	}

	return nil, nil
}

// setModelInfo sets the response's name and type once, from the first provider seen.
func setModelInfo(result *types.ModelDeployOptions, name, componentType string) {
	if result.Name == "" {
		result.Name = name
	}
	if result.ComponentType == "" {
		result.ComponentType = componentType
	}
}

// buildLocalModelProvider builds a local provider entry. models.yaml lists which profile
// ids the model supports; profiles.yaml defines what each id means.
func (p *CatalogProvider) buildLocalModelProvider(ctx context.Context, item *catalogItem, model *types.Model, md *clitemplates.AppMetadata, runtimeType string) (*types.ModelDeployProvider, error) {
	comp := item.Component

	allProfiles, err := loadYAMLList[types.Profile](item.itemFS, item.Path, profilesFileName)
	if err != nil {
		return nil, err
	}
	profilesByID := make(map[string]types.Profile, len(allProfiles))
	for _, pr := range allProfiles {
		profilesByID[pr.ID] = pr
	}

	var cpu, storage int
	if md.Resources != nil {
		cpu, storage = md.Resources.CPU, md.Resources.Storage
	}

	profiles := make([]types.ModelDeployProfile, 0, len(model.Profiles))
	for _, id := range model.Profiles {
		pr, ok := profilesByID[id]
		if !ok {
			return nil, fmt.Errorf("model '%s' lists profile '%s' which is not defined in %s for provider '%s'", model.ID, id, profilesFileName, comp.ID)
		}
		profiles = append(profiles, types.ModelDeployProfile{
			ID:          pr.ID,
			Name:        pr.Name,
			Description: pr.Description,
			Default:     isDefaultProfile(model, pr),
			Resources: types.Resources{
				CPU:          cpu,
				Memory:       pr.Params.Resources.Memory,
				Storage:      storage,
				Accelerators: pr.Params.Resources.Accelerators,
			},
		})
	}

	provider := &types.ModelDeployProvider{
		ID:             comp.ID,
		Name:           comp.Name,
		DeploymentType: deploymentTypeLocal,
		Default:        comp.Default,
		Profiles:       profiles,
	}

	// Omit the schema link when the provider has no settings to fill in.
	if schema, err := p.GetComponentProviderParams(ctx, comp.ComponentType, comp.ID); err == nil && hasNonEmptyProperties(schema) {
		provider.Schema = fmt.Sprintf("/api/v1/components/%s/providers/%s/params?runtime=%s", comp.ComponentType, comp.ID, runtimeType)
	}

	return provider, nil
}

// isDefaultProfile reports the preselected profile: the model's default_profile if set,
// else the profile's own default flag.
func isDefaultProfile(model *types.Model, pr types.Profile) bool {
	if model.DefaultProfile != "" {
		return model.DefaultProfile == pr.ID
	}

	return pr.Default
}

// filterAndSortModelProviders applies the provider filter, then sorts:
// local first, default first, then by id.
func filterAndSortModelProviders(providers []types.ModelDeployProvider, providerID string) []types.ModelDeployProvider {
	filtered := make([]types.ModelDeployProvider, 0, len(providers))
	for _, pr := range providers {
		if providerID == "" || pr.ID == providerID {
			filtered = append(filtered, pr)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if a.DeploymentType != b.DeploymentType {
			return a.DeploymentType == deploymentTypeLocal
		}
		if a.Default != b.Default {
			return a.Default
		}

		return a.ID < b.ID
	})

	return filtered
}
