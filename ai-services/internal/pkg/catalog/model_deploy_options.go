package catalog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/profiles"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	clitemplates "github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
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

	// TODO: cache a model-id -> providers index instead of scanning every item's models.yaml per request.
	// Warm latency is ~2ms currently.

	for _, item := range p.sortedItems() {
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

	components, err := p.ListComponents()
	if err != nil {
		return nil, err
	}
	models.SetComponentTypeNames(result, components)

	result.Providers = models.FilterAndSortProviders(result.Providers, providerID)

	return result, nil
}

// addModelProvider appends the provider behind item to result, unless it cannot deploy the model here.
func (p *CatalogProvider) addModelProvider(ctx context.Context, result *types.ModelDeployOptions, item *catalogItem, model *types.Model, runtimeType string) error {
	if item.Connector != nil {
		models.SetInfo(result, model.Name, model.ComponentType)
		result.Providers = append(result.Providers, types.ModelDeployProvider{
			ID:             item.Connector.ID,
			Name:           item.Connector.Name,
			DeploymentType: constants.DeploymentTypeRemote,
			Schema:         fmt.Sprintf(constants.ConnectorParamsURLFormat, item.Connector.ConnectorType, item.Connector.ID),
		})

		return nil
	}

	models.SetInfo(result, model.Name, item.Component.ComponentType.ID)

	// No profiles in models.yaml: a legacy per-app component (e.g. llm/watsonx), not deployable on its own.
	if len(model.Profiles) == 0 {
		return nil
	}
	// No metadata file for this runtime: provider is unsupported here.
	// Any other failure (e.g. corrupt YAML) is a real error and must surface.
	md, err := p.LoadComponentRuntimeMetadata(item.Component.ComponentType.ID, item.Component.ID)
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
	isModelConnector := item.Connector != nil && item.Connector.ConnectorType == constants.ConnectorTypeModels
	if item.Component == nil && !isModelConnector {
		return nil, nil
	}

	return models.Find(item.itemFS, item.Path, modelID)
}

// buildLocalModelProvider builds a local provider entry. models.yaml lists which profile
// ids the model supports; profiles.yaml defines what each id means.
func (p *CatalogProvider) buildLocalModelProvider(ctx context.Context, item *catalogItem, model *types.Model, md *clitemplates.AppMetadata, runtimeType string) (*types.ModelDeployProvider, error) {
	comp := item.Component

	allProfiles, err := profiles.List(item.itemFS, item.Path)
	if err != nil {
		return nil, err
	}
	profilesByID := profiles.ByID(allProfiles)

	var base types.Resources
	if md.Resources != nil {
		base.CPU, base.Storage = md.Resources.CPU, md.Resources.Storage
	}

	deployProfiles, err := models.BuildDeployProfiles(model, profilesByID, comp.ID, base)
	if err != nil {
		return nil, err
	}

	provider := &types.ModelDeployProvider{
		ID:             comp.ID,
		Name:           comp.Name,
		DeploymentType: constants.DeploymentTypeLocal,
		Default:        comp.Default,
		Profiles:       deployProfiles,
	}

	// Omit the schema link when the provider has no settings to fill in.
	if schema, err := p.GetComponentProviderParams(ctx, comp.ComponentType.ID, comp.ID); err == nil && hasNonEmptyProperties(schema) {
		provider.Schema = fmt.Sprintf(constants.ComponentParamsURLFormat, comp.ComponentType.ID, comp.ID, runtimeType)
	}

	return provider, nil
}
