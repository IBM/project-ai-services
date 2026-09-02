package catalog

import (
	"context"
	"fmt"
	"strings"

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
		if comp.ComponentType != componentType {
			continue
		}

		// Skip excluded components (e.g., watsonx)
		excluded := false
		for _, excludedID := range excludeComponentProviders {
			if strings.EqualFold(comp.ID, excludedID) {
				logger.DebugfCtx(ctx, "Skipping model extraction for excluded component: %s/%s\n", comp.ComponentType, comp.ID)
				excluded = true

				break
			}
		}
		if excluded {
			continue
		}

		if err := p.addComponentModels(ctx, comp.ComponentType, comp.ID, allModels); err != nil {
			return fmt.Errorf("failed to collect models for component %s/%s: %w", comp.ComponentType, comp.ID, err)
		}
	}

	return nil
}

// addComponentModels adds component models to the provided map.
// For components, models are read from values.schema.json file using GetComponentProviderParams.
func (p *CatalogProvider) addComponentModels(ctx context.Context, componentType, componentID string, allModels map[string]bool) error {
	// Use existing GetComponentProviderParams to load the schema
	schema, err := p.GetComponentProviderParams(ctx, componentType, componentID, string(vars.RuntimeFactory.GetRuntimeType()))
	if err != nil {
		return fmt.Errorf("failed to get component schema for %s/%s: %w", componentType, componentID, err)
	}

	// If schema is empty, skip this component
	if len(schema) == 0 {
		logger.DebugfCtx(ctx, "No schema found for component %s/%s, skipping model extraction\n", componentType, componentID)

		return nil
	}

	// Extract models from schema
	extractModelsFromSchema(schema, allModels)

	return nil
}

// extractModelsFromSchema extracts model names from a JSON schema.
// It looks for properties with "model" in the key and extracts values from oneOf/const fields.
func extractModelsFromSchema(schema map[string]any, modelSet map[string]bool) {
	// Get properties object
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}

	// Look for model-related properties
	for key, value := range properties {
		if !strings.Contains(strings.ToLower(key), "model") {
			continue
		}

		propMap, ok := value.(map[string]any)
		if !ok {
			continue
		}

		// Extract models from oneOf field
		extractModelsFromProperty(propMap, modelSet)
	}
}

// extractModelsFromProperty extracts model values from a property's oneOf array.
func extractModelsFromProperty(propMap map[string]any, modelSet map[string]bool) {
	// Check for oneOf array with const values
	if oneOf, ok := propMap["oneOf"].([]any); ok {
		for _, option := range oneOf {
			if optMap, ok := option.(map[string]any); ok {
				if constVal, ok := optMap["const"].(string); ok && constVal != "" {
					modelSet[constVal] = true
				}
			}
		}
	}
}
