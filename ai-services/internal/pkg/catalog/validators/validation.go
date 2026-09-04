package validators

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	pkgutils "github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
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
	// connectorRepo is optional; when set, ValidateConnectorRefs performs DB-level
	// existence and status checks. When nil, only catalog-level rules are enforced.
	connectorRepo dbrepo.ConnectorRepository
}

// NewApplicationValidator creates a new application validator.
func NewApplicationValidator(provider *catalog.CatalogProvider) *ApplicationValidator {
	return &ApplicationValidator{
		provider: provider,
	}
}

// WithConnectorRepo returns a copy of the validator with the connector repository set.
// Calling this enables DB-level connector ref validation during CreateApplication.
func (v *ApplicationValidator) WithConnectorRepo(repo dbrepo.ConnectorRepository) *ApplicationValidator {
	return &ApplicationValidator{
		provider:      v.provider,
		connectorRepo: repo,
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
			Message: fmt.Sprintf("Architecture '%s' version mismatch: requested '%s', available '%s'", architecture.Name, req.Version, architecture.Version),
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
		metadata, err := v.provider.LoadServiceRuntimeMetadata(serviceID, string(vars.RuntimeFactory.GetRuntimeType()))
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
		metadata, err := v.provider.LoadComponentRuntimeMetadata(componentType, providerID, string(vars.RuntimeFactory.GetRuntimeType()))
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
		return v.provider.GetServiceParams(ctx, serviceID, string(vars.RuntimeFactory.GetRuntimeType()))
	}, fmt.Sprintf("service '%s'", serviceID))
}

// ValidateComponentParams validates component parameters against schema.
func (v *ApplicationValidator) ValidateComponentParams(ctx context.Context, componentType, providerID string, params map[string]any) error {
	return v.validateParamsWithSchema(params, func() (map[string]any, error) {
		return v.provider.GetComponentProviderParams(ctx, componentType, providerID, string(vars.RuntimeFactory.GetRuntimeType()))
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
					catalogService.Name,
				),
			}
		}
	}

	return nil
}

// validateServiceCore performs core validation for a service (version, params, components,
// and optional connector refs).
func (v *ApplicationValidator) validateServiceCore(ctx context.Context, service apimodels.Service, catalogService *types.Service) error {
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
	if err := v.validateServiceComponents(ctx, service.Components); err != nil {
		return err
	}

	// Validate connector refs (catalog-level + optional DB-level when connectorRepo is set).
	return v.ValidateConnectorRefs(ctx, service, catalogService, v.connectorRepo)
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
			Message: fmt.Sprintf("Service '%s' cannot be deployed standalone", catalogService.Name),
		}
	}

	// For service deployment, there should be exactly one service in the array
	if len(req.Services) != 1 {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: "Service deployment should have exactly one service",
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
	return v.validateServiceCore(ctx, service, catalogService)
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

	// seenComponents accumulates the first occurrence of each component type/provider
	// pair as services are validated, for cross-service consistency checking.
	seenComponents := make(map[string]seenComponent)

	for _, service := range services {
		// Load service once — used for name in messages and passed into validation
		catalogService, err := v.provider.LoadService(service.CatalogID)
		if err != nil {
			return &ValidationError{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("Service '%s' not found in catalog", service.CatalogID),
			}
		}

		// Verify service is compatible with architecture
		if !validServiceIDs[service.CatalogID] {
			return &ValidationError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("Service '%s' is not compatible with architecture '%s'", catalogService.Name, architecture.Name),
			}
		}

		if err := v.validateServiceCore(ctx, service, catalogService); err != nil {
			return err
		}

		if err := checkComponentConsistency(catalogService.Name, service, seenComponents); err != nil {
			return err
		}
	}

	return nil
}

// ValidateConnectorRefs validates the connectors list on a single service against both the
// catalog YAML (accepts_datasource flag) and the DB connectors table (existence, type, status).
//
// Rules applied per service:
//   - A ConnectorRef with type "datasource" is only valid when the service's catalog YAML
//     declares accepts_datasource: true; otherwise returns 400.
//   - Each ConnectorRef.ID must parse as a valid UUID; otherwise returns 400.
//   - Each ConnectorRef.ID must reference a row in the connectors table whose type matches
//     ConnectorRef.Type and whose status is "connected"; otherwise returns 400.
//   - The same ID must not appear more than once within a single service's connectors list;
//     otherwise returns 400.
//
// Pass a nil connectorRepo to skip the DB look-up (e.g. in unit tests that only exercise
// catalog validation). In that case only the catalog-level rules are enforced.
func (v *ApplicationValidator) ValidateConnectorRefs(
	ctx context.Context,
	service apimodels.Service,
	catalogService *types.Service,
	connectorRepo dbrepo.ConnectorRepository,
) error {
	if len(service.Connectors) == 0 {
		return nil
	}

	seenIDs := make(map[string]bool, len(service.Connectors))

	for _, ref := range service.Connectors {
		if err := v.validateOneConnectorRef(ctx, ref, service.CatalogID, catalogService, seenIDs, connectorRepo); err != nil {
			return err
		}
	}

	return nil
}

// validateOneConnectorRef validates a single ConnectorRef entry: deduplication, catalog-level
// guard, UUID format, and (when connectorRepo is non-nil) DB-level existence/type/status checks.
func (v *ApplicationValidator) validateOneConnectorRef(
	ctx context.Context,
	ref apimodels.ConnectorRef,
	serviceCatalogID string,
	catalogService *types.Service,
	seenIDs map[string]bool,
	connectorRepo dbrepo.ConnectorRepository,
) error {
	if seenIDs[ref.ID] {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("duplicate connector ID %q in service %q connectors list", ref.ID, serviceCatalogID),
		}
	}
	seenIDs[ref.ID] = true

	// Catalog-level guard: the service must declare accepts_datasource: true.
	if ref.Type == catalogconstants.ConnectorTypeDatasource && !catalogService.AcceptsDatasource {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("service %q does not accept datasource connectors (accepts_datasource is not set)", serviceCatalogID),
		}
	}

	connectorID, err := uuid.Parse(ref.ID)
	if err != nil {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("connector ref ID %q is not a valid UUID: %v", ref.ID, err),
		}
	}

	if connectorRepo == nil {
		return nil
	}

	return validateConnectorInDB(ctx, connectorID, ref, serviceCatalogID, connectorRepo)
}

// validateConnectorInDB performs the DB-level existence, type, and status checks for a
// single connector ref. It is called only when connectorRepo is non-nil.
func validateConnectorInDB(
	ctx context.Context,
	connectorID uuid.UUID,
	ref apimodels.ConnectorRef,
	serviceCatalogID string,
	connectorRepo dbrepo.ConnectorRepository,
) error {
	connector, err := connectorRepo.GetByID(ctx, connectorID, false)
	if err != nil {
		if err == dbrepo.ErrConnectorNotFound {
			return &ValidationError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("connector %q referenced in service %q not found", ref.ID, serviceCatalogID),
			}
		}

		return fmt.Errorf("failed to look up connector %q: %w", ref.ID, err)
	}

	if connector.Type != ref.Type {
		return &ValidationError{
			Code: http.StatusBadRequest,
			Message: fmt.Sprintf(
				"connector %q has type %q but was referenced as type %q in service %q",
				ref.ID, connector.Type, ref.Type, serviceCatalogID,
			),
		}
	}

	if connector.Status != dbmodels.ConnectorStatusConnected {
		return &ValidationError{
			Code: http.StatusBadRequest,
			Message: fmt.Sprintf(
				"connector %q (type %q) is not connected (status: %q); only connected datasources may be attached",
				ref.ID, ref.Type, connector.Status,
			),
		}
	}

	return nil
}

// seenComponent records the first occurrence of a component type/provider pair
// across architecture services, for cross-service parameter consistency checking.
type seenComponent struct {
	hash        string
	params      map[string]any
	serviceName string
}

// checkComponentConsistency checks each component in service against seen,
// returning an error if any component type/provider pair has mismatched parameters.
// It updates seen with first-seen entries in place.
func checkComponentConsistency(serviceName string, service apimodels.Service, seen map[string]seenComponent) error {
	for _, component := range service.Components {
		key := fmt.Sprintf("%s/%s", component.ComponentType, component.ProviderID)
		hash := utils.CalculateComponentHash(component.ComponentType, component.ProviderID, component.Params)

		if first, exists := seen[key]; exists {
			if first.hash != hash {
				return &ValidationError{
					Code: http.StatusBadRequest,
					Message: fmt.Sprintf(
						"Parameter mismatch between service '%s' and service '%s': "+
							"Different values given for %s",
						first.serviceName,
						serviceName,
						formatParamKeys(diffParamKeys(first.params, component.Params)),
					),
				}
			}
		} else {
			seen[key] = seenComponent{hash: hash, params: component.Params, serviceName: serviceName}
		}
	}

	return nil
}

// diffParamKeys returns the sorted list of parameter keys whose values differ between a and b,
// including keys present in one map but not the other.
func diffParamKeys(a, b map[string]any) []string {
	var keys []string

	for k, va := range a {
		if vb, ok := b[k]; !ok || fmt.Sprintf("%v", va) != fmt.Sprintf("%v", vb) {
			keys = append(keys, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)

	return keys
}

// formatParamKeys wraps each key in single quotes and joins them with commas.
func formatParamKeys(keys []string) string {
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = fmt.Sprintf("'%s'", k)
	}

	return strings.Join(quoted, ", ")
}

// ConnectorValidator validates CreateDatasourceRequest payloads against the catalog.
type ConnectorValidator struct {
	provider *catalog.CatalogProvider
}

// datasourceNameRe restricts connector names to letters, digits, hyphens, and underscores.
// This matches the character set allowed by most cloud resource naming conventions.
var datasourceNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// NewConnectorValidator creates a new ConnectorValidator backed by the given catalog provider.
func NewConnectorValidator(provider *catalog.CatalogProvider) *ConnectorValidator {
	return &ConnectorValidator{provider: provider}
}

// ValidateCreateDatasourceRequest validates the full CreateDatasourceRequest:
//  1. Validates the name contains only allowed characters (letters, digits, hyphens, underscores).
//  2. Verifies the provider exists in the catalog under the "datasource" connector type.
//  3. Validates params against the provider's JSON Schema (if one is present).
func (v *ConnectorValidator) ValidateCreateDatasourceRequest(ctx context.Context, req apimodels.CreateDatasourceRequest) error {
	// Name character validation — case-insensitive duplicate detection is handled at
	// the DB query level via LOWER(name) = LOWER($1).
	if !datasourceNameRe.MatchString(req.Name) {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: "Datasource name may only contain letters, digits, hyphens (-), and underscores (_)",
		}
	}

	if !v.provider.ConnectorExists(catalogconstants.ConnectorTypeDatasource, req.ProviderID) {
		return &ValidationError{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("Datasource provider %q not found in catalog", req.ProviderID),
		}
	}

	rawSchema, err := v.provider.GetConnectorProviderParams(ctx, catalogconstants.ConnectorTypeDatasource, req.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to load param schema for provider %q: %w", req.ProviderID, err)
	}

	schema, err := pkgutils.ConvertRawJsontoMap(rawSchema)
	if err != nil {
		return fmt.Errorf("failed to decode param schema for provider %q: %w", req.ProviderID, err)
	}

	if len(schema) == 0 {
		// No schema defined for this provider — no parameter constraints to enforce.
		return nil
	}

	if len(req.Params) == 0 {
		return &ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Params is required for datasource provider %q", req.ProviderID),
		}
	}

	return ValidateParams(req.Params, schema, fmt.Sprintf("datasource provider %q", req.ProviderID))
}

// Made with Bob
