package catalog

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// GetArchitectureDeployOptions returns deploy options for all services in an architecture.
// Global components are read from architecture metadata, service components from service metadata.
func (p *CatalogProvider) GetArchitectureDeployOptions(architectureID string) (*types.DeployOptionsArchitecture, error) {
	// Load architecture metadata
	arch, err := p.LoadArchitecture(architectureID)
	if err != nil {
		return nil, fmt.Errorf("architecture not found: %w", err)
	}

	// Build global components from architecture metadata
	globalComponents := make([]types.DeployOptionsComponent, 0, len(arch.GlobalComponents))
	for _, compRef := range arch.GlobalComponents {
		component, err := p.buildDeployOptionsComponent(compRef.Type)
		if err != nil {
			return nil, fmt.Errorf("failed to build global component '%s': %w", compRef.Type, err)
		}
		globalComponents = append(globalComponents, *component)
	}

	// Build services with their components from service metadata
	services := make([]types.DeployOptionsService, 0, len(arch.Services))
	for _, svcRef := range arch.Services {
		service, err := p.LoadService(svcRef.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load service '%s': %w", svcRef.ID, err)
		}

		// Build all components for this service from its dependencies
		components := make([]types.DeployOptionsComponent, 0, len(service.Dependencies))
		for _, dep := range service.Dependencies {
			component, err := p.buildDeployOptionsComponent(dep.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to build component '%s' for service '%s': %w", dep.ID, service.ID, err)
			}
			components = append(components, *component)
		}

		services = append(services, types.DeployOptionsService{
			Type:       service.Type,
			ID:         service.ID,
			Name:       service.Name,
			Components: components,
		})
	}

	return &types.DeployOptionsArchitecture{
		ID:               arch.ID,
		Name:             arch.Name,
		GlobalComponents: globalComponents,
		Services:         services,
	}, nil
}

// GetServiceDeployOptions returns deploy options for a specific service.
func (p *CatalogProvider) GetServiceDeployOptions(serviceID string) (*types.DeployOptionsService, error) {
	// Load service metadata
	service, err := p.LoadService(serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	// Build components list
	components := make([]types.DeployOptionsComponent, 0, len(service.Dependencies))
	for _, dep := range service.Dependencies {
		component, err := p.buildDeployOptionsComponent(dep.ID)
		if err != nil {
			logger.Errorf(fmt.Sprintf("failed to build component '%s': %v", dep.ID, err))

			continue
		}
		components = append(components, *component)
	}

	return &types.DeployOptionsService{
		ID:         service.ID,
		Name:       service.Name,
		Components: components,
	}, nil
}

// buildDeployOptionsComponent builds a DeployOptionsComponent for a given component type.
func (p *CatalogProvider) buildDeployOptionsComponent(componentType string) (*types.DeployOptionsComponent, error) {
	// List all components of this type
	allComponents, err := p.ListComponents()
	if err != nil {
		return nil, fmt.Errorf("failed to list components: %w", err)
	}

	// Filter components by type and build providers
	providers := make([]types.DeployOptionsProvider, 0, len(allComponents))
	var componentName string

	for _, comp := range allComponents {
		if comp.ComponentType != componentType {
			continue
		}

		// Get component name from first matching component
		if componentName == "" && comp.ComponentName != "" {
			componentName = comp.ComponentName
		}

		// Build provider
		provider := types.DeployOptionsProvider{
			ID:          comp.ID,
			Name:        comp.Name,
			Description: comp.Description,
		}

		// Only add schema if the schema file has non-empty properties
		if p.hasNonEmptySchemaProperties(componentType, comp.ID) {
			provider.Schema = fmt.Sprintf("/api/v1/components/%s/providers/%s/params", componentType, comp.ID)
		}

		providers = append(providers, provider)
	}

	// Return error if no providers found for this component type
	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers found for component type '%s'", componentType)
	}

	return &types.DeployOptionsComponent{
		Type:      componentType,
		Name:      componentName,
		Providers: providers,
	}, nil
}

// hasNonEmptySchemaProperties checks if a component provider has a schema file with non-empty properties.
func (p *CatalogProvider) hasNonEmptySchemaProperties(componentType, providerID string) bool {
	// Get the component's catalog path
	componentKey := fmt.Sprintf("%s/%s", componentType, providerID)
	componentPath, err := p.GetCatalogItemPath(componentKey)
	if err != nil {
		return false
	}

	// Get runtime from global factory
	runtime := vars.RuntimeFactory.GetRuntimeType()
	runtimeStr := string(runtime)

	// Try to load values.schema.json for the current runtime
	schemaPath := filepath.Join(componentPath, runtimeStr, "values.schema.json")
	schemaData, err := assets.CatalogFS.ReadFile(schemaPath)
	if err != nil {
		return false
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		return false
	}

	// Check if properties field exists and is not empty
	if properties, ok := schema["properties"].(map[string]any); ok {
		return len(properties) > 0
	}

	return false
}

// GetComponentProviderParams returns the JSON schema for a specific provider's configuration.
// If the schema file is not present, returns an empty schema instead of failing.
func (p *CatalogProvider) GetComponentProviderParams(componentType, providerID string) (map[string]any, error) {
	// Verify component exists and get its path
	_, err := p.LoadComponent(componentType, providerID)
	if err != nil {
		return nil, fmt.Errorf("component provider not found: %w", err)
	}

	// Get the component's catalog path
	componentKey := fmt.Sprintf("%s/%s", componentType, providerID)
	componentPath, err := p.GetCatalogItemPath(componentKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get component path: %w", err)
	}

	// Get runtime from global factory
	runtime := vars.RuntimeFactory.GetRuntimeType()
	runtimeStr := string(runtime)
	schemaPath := filepath.Join(componentPath, runtimeStr, "values.schema.json")
	schemaData, err := assets.CatalogFS.ReadFile(schemaPath)
	if err != nil {
		// If schema file doesn't exist, return empty schema instead of failing
		logger.Warningf(fmt.Sprintf("schema file not found at '%s': %v", schemaPath, err))

		return map[string]any{}, nil
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	return schema, nil
}

// Made with Bob
