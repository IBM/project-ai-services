package catalog

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// GetArchitectureModels collects all unique models from component schemas across
// every service in the given architecture.
// Note: Only components have models in their schemas; services do not define models.
func (p *CatalogProvider) GetArchitectureModels(ctx context.Context, architectureID string, excludeComponentProviders ...string) ([]string, error) {
	arch, err := p.LoadArchitecture(architectureID)
	if err != nil {
		return nil, fmt.Errorf("architecture '%s' not found: %w", architectureID, ErrCatalogItemNotFound)
	}

	allModels := make(map[string]bool)
	if err := p.collectArchitectureModels(ctx, arch.Services, allModels, excludeComponentProviders); err != nil {
		return nil, err
	}

	return utils.ExtractMapKeys(allModels), nil
}

// GetServiceModels collects all unique models from the component schemas of the
// given service's dependencies.
// Note: Only components have models in their schemas; services do not define models.
func (p *CatalogProvider) GetServiceModels(ctx context.Context, serviceID string, excludeComponentProviders ...string) ([]string, error) {
	service, err := p.LoadService(serviceID)
	if err != nil {
		return nil, fmt.Errorf("service '%s' not found: %w", serviceID, ErrCatalogItemNotFound)
	}

	allModels := make(map[string]bool)
	if err := p.collectComponentsModels(ctx, service.Dependencies, allModels, excludeComponentProviders); err != nil {
		return nil, err
	}

	return utils.ExtractMapKeys(allModels), nil
}

// GetCatalogModels collects all unique models for a template ID that may be
// either a service or an architecture. It tries architecture first, then service.
// Kept for CLI paths that do not know the type ahead of time.
func (p *CatalogProvider) GetCatalogModels(ctx context.Context, templateID string, excludeComponentProviders ...string) ([]string, error) {
	if models, err := p.GetArchitectureModels(ctx, templateID, excludeComponentProviders...); err == nil {
		return models, nil
	}

	if models, err := p.GetServiceModels(ctx, templateID, excludeComponentProviders...); err == nil {
		return models, nil
	}

	return nil, fmt.Errorf("template '%s' not found as service or architecture: %w", templateID, ErrCatalogItemNotFound)
}

// collectArchitectureModels collects models from all component dependencies across all services in an architecture.
func (p *CatalogProvider) collectArchitectureModels(ctx context.Context, services []types.ServiceReference, allModels map[string]bool, excludeComponentProviders []string) error {
	for _, svcRef := range services {
		service, err := p.LoadService(svcRef.ID)
		if err != nil {
			return fmt.Errorf("failed to load service %s: %w", svcRef.ID, err)
		}

		// Collect component models from service dependencies
		if err := p.collectComponentsModels(ctx, service.Dependencies, allModels, excludeComponentProviders); err != nil {
			return fmt.Errorf("failed to collect models for service %s: %w", svcRef.ID, err)
		}
	}

	return nil
}

// collectComponentsModels collects models for components based on dependencies.
func (p *CatalogProvider) collectComponentsModels(ctx context.Context, dependencies []types.DependencyReference, allModels map[string]bool, excludeComponentProviders []string) error {
	if len(dependencies) == 0 {
		return nil
	}

	components, err := p.ListComponents()
	if err != nil {
		return fmt.Errorf("failed to list components: %w", err)
	}

	for _, dep := range dependencies {
		if err := p.collectComponentsByTypeModels(ctx, dep.ID, components, allModels, excludeComponentProviders); err != nil {
			return err
		}
	}

	return nil
}

// collectComponentsByTypeModels collects models for all components of a specific type.
func (p *CatalogProvider) collectComponentsByTypeModels(ctx context.Context, componentType string, components []types.Component, allModels map[string]bool, excludeComponentProviders []string) error {
	for _, comp := range components {
		if comp.ComponentType.ID != componentType {
			continue
		}

		// Skip excluded components (e.g., watsonx)
		excluded := false
		for _, excludedID := range excludeComponentProviders {
			if strings.EqualFold(comp.ID, excludedID) {
				logger.DebugfCtx(ctx, "Skipping model extraction for excluded component: %s/%s\n", comp.ComponentType.ID, comp.ID)
				excluded = true

				break
			}
		}
		if excluded {
			continue
		}

		if err := p.addComponentModels(ctx, comp.ComponentType.ID, comp.ID, allModels); err != nil {
			return fmt.Errorf("failed to collect models for component %s/%s: %w", comp.ComponentType.ID, comp.ID, err)
		}
	}

	return nil
}

// addComponentModels adds component models to the provided map.
// For components, models are read from values.schema.json file using GetComponentProviderParams.
func (p *CatalogProvider) addComponentModels(ctx context.Context, componentType, componentID string, allModels map[string]bool) error {
	// Use existing GetComponentProviderParams to load the schema.
	provider, err := p.WithRuntime(string(vars.RuntimeFactory.GetRuntimeType()))
	if err != nil {
		return fmt.Errorf("failed to resolve runtime for component schema %s/%s: %w", componentType, componentID, err)
	}

	schema, err := provider.GetComponentProviderParams(ctx, componentType, componentID)
	if err != nil {
		return fmt.Errorf("failed to get component schema for %s/%s: %w", componentType, componentID, err)
	}

	// If schema is empty, skip this component
	if len(schema) == 0 {
		logger.DebugfCtx(ctx, "No schema found for component %s/%s, skipping model extraction\n", componentType, componentID)

		return nil
	}

	// Extract models from schema
	models.ExtractFromSchema(schema, allModels)

	return nil
}

// GetModelProviders lists the providers a model can be deployed with, as the provider id with its
// display name, in no particular order. deploymentType selects the kind: "local" for the model components,
// "remote" for the "models" connectors, and "" for both.
func (p *CatalogProvider) GetModelProviders(deploymentType constants.DeploymentType) []types.ProviderInfo {
	result := make([]types.ProviderInfo, 0)

	add := func(id, name string) {
		if slices.ContainsFunc(result, func(pr types.ProviderInfo) bool { return pr.ID == id }) {
			return
		}

		result = append(result, types.ProviderInfo{ID: id, Name: name})
	}

	if deploymentType == "" || deploymentType == constants.DeploymentTypeLocal {
		for _, c := range p.ListComponentsByCategory(constants.ComponentCategoryModels) {
			add(c.ID, c.Name)
		}
	}

	if deploymentType == "" || deploymentType == constants.DeploymentTypeRemote {
		// ListConnectors errors when no connector of the type is registered: there are no providers.
		connectors, _ := p.ListConnectors(constants.ConnectorTypeModels)
		for _, c := range connectors {
			add(c.ID, c.Name)
		}
	}

	return result
}

// GetModels lists every model the catalog knows, once per model id, sorted by id. The
// models come from the models.yaml of the model components and the "models" connectors.
//
// A model served by several providers is returned as listed by the first one in item-key order
// ("type/id"), so the result is stable. Only its provider-independent fields (ID, Name, Title,
// Description) are meaningful.
func (p *CatalogProvider) GetModels() ([]types.Model, error) {
	result := make([]types.Model, 0)

	for _, item := range p.sortedItems() {
		if !isModelItem(item) {
			continue
		}

		models, err := utils.LoadYAMLList[types.Model](item.itemFS, item.Path, constants.ModelsFileName)
		if err != nil {
			return nil, err
		}

		for _, m := range models {
			if slices.ContainsFunc(result, func(r types.Model) bool { return r.ID == m.ID }) {
				continue
			}

			result = append(result, m)
		}
	}

	slices.SortFunc(result, func(a, b types.Model) int { return strings.Compare(a.ID, b.ID) })

	return result, nil
}

// GetModel returns modelID as GetModels lists it, or ErrModelNotFound if the catalog doesn't list it.
func (p *CatalogProvider) GetModel(modelID string) (*types.Model, error) {
	for _, item := range p.sortedItems() {
		model, err := findModel(item, modelID)
		if err != nil {
			return nil, err
		}
		if model != nil {
			return model, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrModelNotFound, modelID)
}

// isModelItem reports whether the item serves models: a model component or a "models" connector.
func isModelItem(item *catalogItem) bool {
	isModelComponent := item.Component != nil && item.Component.Category == constants.ComponentCategoryModels
	isModelConnector := item.Connector != nil && item.Connector.ConnectorType == constants.ConnectorTypeModels

	return isModelComponent || isModelConnector
}
