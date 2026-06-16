package validators

import (
	"context"
	"fmt"
	"net/http"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
)

// ValidationError represents a validation error with HTTP status code.
type ValidationError struct {
	Code    int
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// ApplicationValidator handles validation of application deployment requests.
type ApplicationValidator struct {
	provider *catalog.CatalogProvider
}

// NewApplicationValidator creates a new application validator.
func NewApplicationValidator(provider *catalog.CatalogProvider) *ApplicationValidator {
	return &ApplicationValidator{
		provider: provider,
	}
}

// ValidateDeploymentRequest validates the entire deployment request.
func (v *ApplicationValidator) ValidateDeploymentRequest(ctx context.Context, req apimodels.CreateApplicationRequest) error {
	// Validate based on deployment type
	if v.provider.ArchitectureExists(req.CatalogID) {
		return v.ValidateArchitectureDeployment(ctx, req)
	} else if v.provider.ServiceExists(req.CatalogID) {
		return v.ValidateServiceDeployment(ctx, req)
	} else {
		return &ValidationError{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("Catalog ID '%s' not found as architecture or service", req.CatalogID),
		}
	}
}

// ValidateArchitectureDeployment validates an architecture deployment request.
func (v *ApplicationValidator) ValidateArchitectureDeployment(ctx context.Context, req apimodels.CreateApplicationRequest) error {
	// Load architecture
	architecture, err := v.provider.LoadArchitecture(req.CatalogID)
	if err != nil {
		return &ValidationError{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("Architecture '%s' not found in catalog", req.CatalogID),
		}
	}

	// Validate architecture version
	if architecture.Version != req.Version {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Architecture '%s' version mismatch: requested '%s', available '%s'", req.CatalogID, req.Version, architecture.Version),
		}
	}

	// Validate that at least one service is provided
	if len(req.Services) == 0 {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: "At least one service must be specified for architecture deployment",
		}
	}

	// Validate services
	return v.ValidateServices(ctx, req.Services, architecture)
}

// validateVersion is a validator that accepts a version string and metadata loader.
func (v *ApplicationValidator) validateVersion(
	itemType, itemID, requestedVersion string,
	getVersion func() (string, error),
) error {
	availableVersion, err := getVersion()
	if err != nil {
		return &ValidationError{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("%s '%s' runtime metadata not found", itemType, itemID),
		}
	}
	if availableVersion != requestedVersion {
		return &ValidationError{
			Code: http.StatusBadRequest,
			Message: fmt.Sprintf("%s '%s' version mismatch: requested '%s', available '%s'",
				itemType, itemID, requestedVersion, availableVersion),
		}
	}

	return nil
}

// ValidateServiceVersion validates that the service version matches the runtime metadata.
func (v *ApplicationValidator) ValidateServiceVersion(serviceID, requestedVersion string) error {
	return v.validateVersion("Service", serviceID, requestedVersion, func() (string, error) {
		metadata, err := v.provider.LoadServiceRuntimeMetadata(serviceID)
		if err != nil {
			return "", err
		}

		return metadata.Version, nil
	})
}

// ValidateComponentVersion validates that the component version matches the runtime metadata.
func (v *ApplicationValidator) ValidateComponentVersion(componentType, providerID, requestedVersion string) error {
	itemID := fmt.Sprintf("%s/%s", componentType, providerID)

	return v.validateVersion("Component", itemID, requestedVersion, func() (string, error) {
		metadata, err := v.provider.LoadComponentRuntimeMetadata(componentType, providerID)
		if err != nil {
			return "", err
		}

		return metadata.Version, nil
	})
}

// validateParamsWithSchema is a parameter validator that loads schema and validates params.
func (v *ApplicationValidator) validateParamsWithSchema(
	params map[string]any,
	loadSchema func() (map[string]any, error),
	contextName string,
) error {
	if len(params) == 0 {
		return nil
	}
	schema, err := loadSchema()
	if err == nil && len(schema) > 0 {
		return ValidateParams(params, schema, contextName)
	}

	return nil
}

// ValidateServiceParams validates service-level parameters against schema.
func (v *ApplicationValidator) ValidateServiceParams(ctx context.Context, serviceID string, params map[string]any) error {
	return v.validateParamsWithSchema(params, func() (map[string]any, error) {
		return v.provider.GetServiceParams(ctx, serviceID)
	}, fmt.Sprintf("service '%s'", serviceID))
}

// ValidateComponentParams validates component parameters against schema.
func (v *ApplicationValidator) ValidateComponentParams(ctx context.Context, componentType, providerID string, params map[string]any) error {
	return v.validateParamsWithSchema(params, func() (map[string]any, error) {
		return v.provider.GetComponentProviderParams(ctx, componentType, providerID)
	}, fmt.Sprintf("component '%s/%s'", componentType, providerID))
}

// validateServiceComponents validates all components in a service.
func (v *ApplicationValidator) validateServiceComponents(ctx context.Context, components []apimodels.Component) error {
	// Check for duplicate components (same component_type + provider_id combination)
	if err := v.validateNoDuplicateComponents(components); err != nil {
		return err
	}

	for _, component := range components {
		if err := v.ValidateSingleComponent(ctx, component); err != nil {
			return err
		}
	}

	return nil
}

// validateNoDuplicateComponents ensures no duplicate component type exists in the array.
func (v *ApplicationValidator) validateNoDuplicateComponents(components []apimodels.Component) error {
	seen := make(map[string]bool)

	for _, component := range components {
		// Create unique key based on component type only
		componentKey := component.ComponentType

		if seen[componentKey] {
			return &ValidationError{
				Code: http.StatusBadRequest,
				Message: fmt.Sprintf(
					"Duplicate component found: component type '%s' appears multiple times. "+
						"Each component type must be unique within a service",
					component.ComponentType,
				),
			}
		}

		seen[componentKey] = true
	}

	return nil
}

// validateComponentsMatchDependencies validates that all components in the request
// are supported by the service (i.e., match the service's dependencies).
func (v *ApplicationValidator) validateComponentsMatchDependencies(
	components []apimodels.Component,
	catalogService *types.Service,
) error {
	// Build a map of supported component types from service dependencies
	supportedComponents := make(map[string]bool)
	for _, dep := range catalogService.Dependencies {
		supportedComponents[dep.ID] = true
	}

	// Check each component in the request
	for _, component := range components {
		if !supportedComponents[component.ComponentType] {
			return &ValidationError{
				Code: http.StatusBadRequest,
				Message: fmt.Sprintf(
					"Component type '%s' is not supported by service '%s'",
					component.ComponentType,
					catalogService.ID,
				),
			}
		}
	}

	return nil
}

// validateServiceCore performs core validation for a service (existence, version, params, components).
func (v *ApplicationValidator) validateServiceCore(ctx context.Context, service apimodels.Service) error {
	// Verify service exists in catalog
	catalogService, err := v.provider.LoadService(service.CatalogID)
	if err != nil {
		return &ValidationError{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("Service '%s' not found in catalog", service.CatalogID),
		}
	}

	// Validate service version
	if err := v.ValidateServiceVersion(service.CatalogID, service.Version); err != nil {
		return err
	}

	// Validate service-level parameters
	if err := v.ValidateServiceParams(ctx, service.CatalogID, service.Params); err != nil {
		return err
	}

	// Validate that components match service dependencies
	if err := v.validateComponentsMatchDependencies(service.Components, catalogService); err != nil {
		return err
	}

	// Validate all components
	return v.validateServiceComponents(ctx, service.Components)
}

// ValidateSingleComponent validates a single component (existence, version, and parameters).
func (v *ApplicationValidator) ValidateSingleComponent(ctx context.Context, component apimodels.Component) error {
	// Verify component provider exists
	_, err := v.provider.LoadComponent(component.ComponentType, component.ProviderID)
	if err != nil {
		return &ValidationError{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("Provider '%s' not found for component type '%s'", component.ProviderID, component.ComponentType),
		}
	}

	// Validate component version
	if err := v.ValidateComponentVersion(component.ComponentType, component.ProviderID, component.Version); err != nil {
		return err
	}

	// Validate component parameters
	return v.ValidateComponentParams(ctx, component.ComponentType, component.ProviderID, component.Params)
}

// ValidateServiceDeployment validates a single service deployment request.
func (v *ApplicationValidator) ValidateServiceDeployment(ctx context.Context, req apimodels.CreateApplicationRequest) error {
	// Load service metadata from catalog
	catalogService, err := v.provider.LoadService(req.CatalogID)
	if err != nil {
		return &ValidationError{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("Service '%s' not found in catalog", req.CatalogID),
		}
	}

	// Validate service version
	if err := v.ValidateServiceVersion(req.CatalogID, req.Version); err != nil {
		return err
	}

	// Validate that service can be deployed standalone
	if !catalogService.Standalone {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Service '%s' cannot be deployed standalone", req.CatalogID),
		}
	}

	// For service deployment, there should be exactly one service in the array
	if len(req.Services) != 1 {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: "When deploying a service, services array must contain exactly one service",
		}
	}

	service := req.Services[0]
	if service.CatalogID != req.CatalogID {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Service '%s' not found in catalog", req.CatalogID),
		}
	}

	// Perform core service validation
	if err := v.validateServiceCore(ctx, service); err != nil {
		return err
	}

	// Validate component consistency
	return ValidateComponentConsistency(service.Components)
}

// ValidateSingleServiceInArchitecture validates a single service within an architecture deployment.
func (v *ApplicationValidator) ValidateSingleServiceInArchitecture(ctx context.Context, service apimodels.Service, validServiceIDs map[string]bool, architectureID string) error {
	// Verify service is compatible with architecture
	if !validServiceIDs[service.CatalogID] {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Service '%s' is not compatible with architecture '%s'", service.CatalogID, architectureID),
		}
	}

	// Perform core service validation (existence, version, params, components)
	return v.validateServiceCore(ctx, service)
}

// ValidateServices validates all services in the request.
func (v *ApplicationValidator) ValidateServices(ctx context.Context, services []apimodels.Service, architecture *types.Architecture) error {
	// Validate that services array is not empty (defensive check)
	if len(services) == 0 {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: "Services array cannot be empty",
		}
	}

	// Build a map of valid service IDs from architecture
	validServiceIDs := make(map[string]bool)
	for _, svcRef := range architecture.Services {
		validServiceIDs[svcRef.ID] = true
	}

	// Collect all components from all services
	allComponents := []apimodels.Component{}

	for _, service := range services {
		// Validate single service
		if err := v.ValidateSingleServiceInArchitecture(ctx, service, validServiceIDs, architecture.ID); err != nil {
			return err
		}

		// Collect components for consistency validation
		allComponents = append(allComponents, service.Components...)
	}

	// Validate component consistency across all services
	return ValidateComponentConsistency(allComponents)
}

// ValidateComponentConsistency validates that the same component (type + provider)
// has identical parameters across all occurrences.
func ValidateComponentConsistency(components []apimodels.Component) error {
	componentKeyToHash := make(map[string]string)
	componentKeyToFirst := make(map[string]apimodels.Component)

	for _, component := range components {
		// Create unique key for this component type + provider combination
		componentKey := fmt.Sprintf("%s:%s", component.ComponentType, component.ProviderID)

		// Calculate hash
		componentHash := utils.CalculateComponentHash(
			component.ComponentType,
			component.ProviderID,
			component.Params,
		)

		// Check if this component type+provider was seen before
		if existingHash, exists := componentKeyToHash[componentKey]; exists {
			if existingHash != componentHash {
				// Same component type+provider, but different parameters
				firstComp := componentKeyToFirst[componentKey]

				return &ValidationError{
					Code: http.StatusBadRequest,
					Message: fmt.Sprintf(
						"Component parameter mismatch: component '%s' with provider '%s' has inconsistent parameters. "+
							"All instances of the same component must have identical parameters. "+
							"First instance: %v, Conflicting instance: %v",
						component.ComponentType,
						component.ProviderID,
						firstComp.Params,
						component.Params,
					),
				}
			}
		} else {
			// First occurrence of this component type+provider combination
			componentKeyToHash[componentKey] = componentHash
			componentKeyToFirst[componentKey] = component
		}
	}

	return nil
}

// Made with Bob
