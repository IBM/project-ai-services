package catalog

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
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
	var globalComponents []types.DeployOptionsComponent
	for _, compRef := range arch.GlobalComponents {
		component, err := p.buildDeployOptionsComponent(compRef.Type)
		if err != nil {
			return nil, fmt.Errorf("failed to build global component '%s': %w", compRef.Type, err)
		}
		globalComponents = append(globalComponents, *component)
	}

	// Build services with their components from service metadata
	var services []types.DeployOptionsService
	for _, svcRef := range arch.Services {
		service, err := p.LoadService(svcRef.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load service '%s': %w", svcRef.ID, err)
		}

		// Build all components for this service from its dependencies
		var components []types.DeployOptionsComponent
		for _, dep := range service.Dependencies {
			component, err := p.buildDeployOptionsComponent(dep.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to build component '%s' for service '%s': %w", dep.ID, service.ID, err)
			}
			components = append(components, *component)
		}

		services = append(services, types.DeployOptionsService{
			Type:        "service",
			ServiceID:   service.ID,
			ServiceName: service.Name,
			Components:  components,
		})
	}

	return &types.DeployOptionsArchitecture{
		ArchitectureID:   arch.ID,
		ArchitectureName: arch.Name,
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
	var components []types.DeployOptionsComponent
	for _, dep := range service.Dependencies {
		component, err := p.buildDeployOptionsComponent(dep.ID)
		if err != nil {
			continue
		}
		components = append(components, *component)
	}

	return &types.DeployOptionsService{
		ServiceID:   service.ID,
		ServiceName: service.Name,
		Components:  components,
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
	var providers []types.DeployOptionsProvider
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
			Schema:      fmt.Sprintf("/api/v1/components/%s/providers/%s/params", componentType, comp.ID),
		}

		// Add specifications from component metadata
		if comp.Specifications != nil {
			provider.Specifications = comp.Specifications
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

// GetComponentProviderParams returns the JSON schema for a specific provider's configuration.
// If the schema file is not present, returns an empty schema instead of failing.
func (p *CatalogProvider) GetComponentProviderParams(componentType, providerID string, runtime runtimeTypes.RuntimeType) (map[string]interface{}, error) {
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

	// Load values.schema.json using the runtime type
	runtimeStr := string(runtime)
	schemaPath := filepath.Join(componentPath, runtimeStr, "values.schema.json")
	schemaData, err := assets.CatalogFS.ReadFile(schemaPath)
	if err != nil {
		// If schema file doesn't exist, return empty schema instead of failing
		return map[string]interface{}{}, nil
	}

	var schema map[string]interface{}
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	return schema, nil
}

// Made with Bob
