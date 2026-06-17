package catalog

import (
	"context"
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
func (p *CatalogProvider) GetArchitectureDeployOptions(ctx context.Context, architectureID string) (*types.DeployOptionsArchitecture, error) {
	// Load architecture metadata
	arch, err := p.LoadArchitecture(architectureID)
	if err != nil {
		return nil, fmt.Errorf("architecture not found: %w", err)
	}

	// Build global components from architecture metadata
	globalComponents, err := p.buildGlobalComponents(ctx, arch.GlobalComponents)
	if err != nil {
		return nil, err
	}

	// Build services with their components from service metadata
	services, err := p.buildArchitectureServices(ctx, arch.Services)
	if err != nil {
		return nil, err
	}

	return &types.DeployOptionsArchitecture{
		ID:               arch.ID,
		Name:             arch.Name,
		Version:          arch.Version,
		GlobalComponents: globalComponents,
		Services:         services,
	}, nil
}

// buildGlobalComponents builds deploy options for global components.
func (p *CatalogProvider) buildGlobalComponents(ctx context.Context, compRefs []types.ComponentReference) ([]types.DeployOptionsComponent, error) {
	globalComponents := make([]types.DeployOptionsComponent, 0, len(compRefs))
	for _, compRef := range compRefs {
		component, err := p.buildDeployOptionsComponent(ctx, compRef.Type, false)
		if err != nil {
			return nil, fmt.Errorf("failed to build global component '%s': %w", compRef.Type, err)
		}
		globalComponents = append(globalComponents, *component)
	}

	return globalComponents, nil
}

// buildArchitectureServices builds deploy options for all services in an architecture.
func (p *CatalogProvider) buildArchitectureServices(ctx context.Context, svcRefs []types.ServiceReference) ([]types.DeployOptionsService, error) {
	services := make([]types.DeployOptionsService, 0, len(svcRefs))
	for _, svcRef := range svcRefs {
		deployOptionsService, err := p.buildSingleService(ctx, svcRef.ID)
		if err != nil {
			return nil, err
		}
		services = append(services, *deployOptionsService)
	}

	return services, nil
}

// buildSingleService builds deploy options for a single service.
func (p *CatalogProvider) buildSingleService(ctx context.Context, serviceID string) (*types.DeployOptionsService, error) {
	service, err := p.LoadService(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load service '%s': %w", serviceID, err)
	}

	// Load service runtime metadata to get version
	serviceVersion := p.getServiceVersion(service.ID)

	// Build all components for this service from its dependencies
	components, err := p.buildServiceComponents(ctx, service.ID, service.Dependencies)
	if err != nil {
		return nil, err
	}

	deployOptionsService := &types.DeployOptionsService{
		ID:         service.ID,
		Name:       service.Name,
		Version:    serviceVersion,
		Components: components,
	}

	// Only add schema if the service has non-empty schema properties
	p.addServiceSchemaIfPresent(ctx, deployOptionsService, service.ID)

	return deployOptionsService, nil
}

// buildServiceComponents builds deploy options components for a service's dependencies.
func (p *CatalogProvider) buildServiceComponents(ctx context.Context, serviceID string, dependencies []types.DependencyReference) ([]types.DeployOptionsComponent, error) {
	components := make([]types.DeployOptionsComponent, 0, len(dependencies))
	for _, dep := range dependencies {
		component, err := p.buildDeployOptionsComponent(ctx, dep.ID, true)
		if err != nil {
			return nil, fmt.Errorf("failed to build component '%s' for service '%s': %w", dep.ID, serviceID, err)
		}
		components = append(components, *component)
	}

	return components, nil
}

// getServiceVersion retrieves the version for a service, returning empty string if not found.
func (p *CatalogProvider) getServiceVersion(serviceID string) string {
	if runtimeMetadata, err := p.LoadServiceRuntimeMetadata(serviceID); err == nil {
		return runtimeMetadata.Version
	}

	return ""
}

// addServiceSchemaIfPresent adds schema URL to service if it has non-empty properties.
func (p *CatalogProvider) addServiceSchemaIfPresent(ctx context.Context, deployOptionsService *types.DeployOptionsService, serviceID string) {
	if schema, err := p.GetServiceParams(ctx, serviceID); err == nil && hasNonEmptyProperties(schema) {
		deployOptionsService.Schema = fmt.Sprintf("/api/v1/services/%s/params", serviceID)
	}
}

// GetServiceDeployOptions returns deploy options for a specific service.
func (p *CatalogProvider) GetServiceDeployOptions(ctx context.Context, serviceID string) (*types.DeployOptionsService, error) {
	// Load service metadata
	service, err := p.LoadService(serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	// Load service runtime metadata to get version
	serviceVersion := ""
	if runtimeMetadata, err := p.LoadServiceRuntimeMetadata(service.ID); err == nil {
		serviceVersion = runtimeMetadata.Version
	}

	// Build components list
	components := make([]types.DeployOptionsComponent, 0, len(service.Dependencies))
	for _, dep := range service.Dependencies {
		component, err := p.buildDeployOptionsComponent(ctx, dep.ID, true)
		if err != nil {
			logger.ErrorfCtx(ctx, "failed to build component '%s': %v", dep.ID, err)

			continue
		}
		components = append(components, *component)
	}

	// Load resources from runtime-specific metadata
	var resources *types.Resources
	runtimeMetadata, err := p.LoadServiceRuntimeMetadata(service.ID)
	if err == nil && runtimeMetadata.Resources != nil {
		// Convert RuntimeResources to types.Resources
		resources = &types.Resources{
			CPU:          runtimeMetadata.Resources.CPU,
			Memory:       runtimeMetadata.Resources.Memory,
			Storage:      runtimeMetadata.Resources.Storage,
			Accelerators: runtimeMetadata.Resources.Accelerators,
		}
	}

	deployOptions := &types.DeployOptionsService{
		ID:         service.ID,
		Name:       service.Name,
		Version:    serviceVersion,
		Components: components,
		Resources:  resources,
	}

	// Only add schema if the service has non-empty schema properties
	if schema, err := p.GetServiceParams(ctx, serviceID); err == nil && hasNonEmptyProperties(schema) {
		deployOptions.Schema = fmt.Sprintf("/api/v1/services/%s/params", serviceID)
	}

	return deployOptions, nil
}

// buildDeployOptionsComponent builds a DeployOptionsComponent for a given component type.
// includeResources controls whether to include resource information in providers.
func (p *CatalogProvider) buildDeployOptionsComponent(ctx context.Context, componentType string, includeResources bool) (*types.DeployOptionsComponent, error) {
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

		// Build provider with version, resources and schema
		provider := p.buildProvider(ctx, comp, componentType, includeResources)
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

// buildProvider builds a DeployOptionsProvider from a component, including version, resources and schema if applicable.
func (p *CatalogProvider) buildProvider(ctx context.Context, comp types.Component, componentType string, includeResources bool) types.DeployOptionsProvider {
	// Load component runtime metadata
	providerVersion := ""
	var resources *types.Resources

	if runtimeMetadata, err := p.LoadComponentRuntimeMetadata(componentType, comp.ID); err == nil {
		providerVersion = runtimeMetadata.Version

		// Only include resources if requested and available
		if includeResources && runtimeMetadata.Resources != nil {
			resources = &types.Resources{
				CPU:          runtimeMetadata.Resources.CPU,
				Memory:       runtimeMetadata.Resources.Memory,
				Storage:      runtimeMetadata.Resources.Storage,
				Accelerators: runtimeMetadata.Resources.Accelerators,
			}
		}
	}

	provider := types.DeployOptionsProvider{
		ID:          comp.ID,
		Name:        comp.Name,
		Description: comp.Description,
		Version:     providerVersion,
		Default:     comp.Default,
		Resources:   resources,
	}

	// Only add schema if the schema file has non-empty properties
	if schema, err := p.GetComponentProviderParams(ctx, componentType, comp.ID); err == nil && hasNonEmptyProperties(schema) {
		provider.Schema = fmt.Sprintf("/api/v1/components/%s/providers/%s/params", componentType, comp.ID)
	}

	return provider
}

// hasNonEmptyProperties checks if a schema has non-empty properties.
func hasNonEmptyProperties(schema map[string]any) bool {
	if properties, ok := schema["properties"].(map[string]any); ok {
		return len(properties) > 0
	}

	return false
}

// GetComponentProviderParams returns the JSON schema for a specific provider's configuration.
// If the schema file is not present, returns an empty schema instead of failing.
func (p *CatalogProvider) GetComponentProviderParams(ctx context.Context, componentType, providerID string) (map[string]any, error) {
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
		logger.WarningfCtx(ctx, "schema file not found at '%s': %v", schemaPath, err)

		return map[string]any{}, nil
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	return schema, nil
}

// GetServiceParams returns the JSON schema for a specific service's configuration.
// If the schema file is not present, returns an empty schema instead of failing.
func (p *CatalogProvider) GetServiceParams(ctx context.Context, serviceID string) (map[string]any, error) {
	// Verify service exists and get its path
	_, err := p.LoadService(serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	// Get the service's catalog path
	servicePath, err := p.GetCatalogItemPath(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service path: %w", err)
	}

	// Get runtime from global factory
	runtime := vars.RuntimeFactory.GetRuntimeType()
	runtimeStr := string(runtime)
	schemaPath := filepath.Join(servicePath, runtimeStr, "values.schema.json")
	schemaData, err := assets.CatalogFS.ReadFile(schemaPath)
	if err != nil {
		// If schema file doesn't exist, return empty schema instead of failing
		logger.WarningfCtx(ctx, "schema file not found at '%s': %v", schemaPath, err)

		return map[string]any{}, nil
	}

	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	return schema, nil
}

// Made with Bob
