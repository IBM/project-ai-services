package repository

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deletion"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	clitemplates "github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	consts "github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// ApplicationService provides business logic for application operations.
type ApplicationService struct {
	appRepo               dbrepo.ApplicationRepository
	serviceRepo           dbrepo.ServiceRepository
	componentRepo         dbrepo.ComponentRepository
	serviceDependencyRepo dbrepo.ServiceDependencyRepository
	provider              *catalog.CatalogProvider
	deploymentPlanner     *deployment.DeploymentPlanner
	deploymentExecutor    *deployment.DeploymentExecutor
	deletionService       *deletion.DeletionService
	validator             *validators.ApplicationValidator
}

// ValidationError represents a validation error with HTTP status code.
type ValidationError = validators.ValidationError

// NewApplicationService creates a new application service.
func NewApplicationService(
	appRepo dbrepo.ApplicationRepository,
	serviceRepo dbrepo.ServiceRepository,
	componentRepo dbrepo.ComponentRepository,
	serviceDependencyRepo dbrepo.ServiceDependencyRepository,
	provider *catalog.CatalogProvider,
) *ApplicationService {
	return &ApplicationService{
		appRepo:               appRepo,
		serviceRepo:           serviceRepo,
		componentRepo:         componentRepo,
		serviceDependencyRepo: serviceDependencyRepo,
		provider:              provider,
		deploymentPlanner:     deployment.NewDeploymentPlanner(provider, componentRepo),
		deploymentExecutor:    deployment.NewDeploymentExecutor(provider, appRepo, serviceRepo, componentRepo),
		deletionService:       deletion.NewDeletionService(appRepo, serviceRepo, componentRepo, serviceDependencyRepo),
		validator:             validators.NewApplicationValidator(provider),
	}
}

// ListApplicationsRequest contains parameters for listing applications.
type ListApplicationsRequest struct {
	Page           int
	PageSize       int
	DeploymentType string
	CatalogID      string
}

// ListApplications retrieves a paginated list of applications with filters.
func (s *ApplicationService) ListApplications(ctx context.Context, req ListApplicationsRequest) (*types.ApplicationListResponse, error) {
	// Build filters for repository query (all filters are at DB level now)
	filters := &dbrepo.ApplicationFilters{
		DeploymentType: req.DeploymentType,
		CatalogID:      req.CatalogID,
		Limit:          req.PageSize,
		Offset:         (req.Page - 1) * req.PageSize,
	}

	// Get total count for pagination metadata
	totalCount, err := s.appRepo.GetCount(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get application count: %w", err)
	}

	// Get applications from database with filters
	applications, err := s.appRepo.GetAll(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve applications: %w", err)
	}

	// Build application list with type information
	apps := make([]types.Application, 0, len(applications))
	for _, app := range applications {
		appData, err := s.buildApplication(app)
		if err != nil {
			return nil, err
		}

		apps = append(apps, appData)
	}

	// All pagination is done at DB level, so summaries are already paginated
	totalPages := (totalCount + req.PageSize - 1) / req.PageSize
	if totalPages == 0 {
		totalPages = 1
	}

	response := &types.ApplicationListResponse{
		Data: apps,
		Pagination: types.PaginationMetadata{
			Page:       req.Page,
			PageSize:   req.PageSize,
			TotalItems: totalCount,
			TotalPages: totalPages,
			HasNext:    req.Page < totalPages,
			HasPrev:    req.Page > 1,
		},
	}

	return response, nil
}

// buildApplication creates an Application from a models.Application.
func (s *ApplicationService) buildApplication(app models.Application) (types.Application, error) {
	// Get type (display name) from catalog metadata
	typeName, err := s.getApplicationType(app.CatalogID, app.DeploymentType)
	if err != nil {
		return types.Application{}, fmt.Errorf("failed to get application type for catalog_id '%s': %w", app.CatalogID, err)
	}

	appData := types.Application{
		ID:             app.ID.String(),
		Name:           app.Name,
		DeploymentType: string(app.DeploymentType),
		Type:           typeName,
		Status:         string(app.Status),
		Message:        app.Message,
		CreatedAt:      app.CreatedAt.Format(constants.RFC3339WithTimezone),
		UpdatedAt:      app.UpdatedAt.Format(constants.RFC3339WithTimezone),
	}

	// Add services array only for architectures (not for individual services)
	if app.DeploymentType == models.DeploymentTypeArchitectures && len(app.Services) > 0 {
		appData.Services = s.buildServiceStatuses(app.Services)
	}

	return appData, nil
}

// buildServiceStatuses creates ApplicationService array from models.Service slice.
func (s *ApplicationService) buildServiceStatuses(services []models.Service) []types.ApplicationService {
	statuses := make([]types.ApplicationService, 0, len(services))

	for _, svc := range services {
		// Get service display name from catalog metadata
		serviceDisplayName := svc.CatalogID // Default to catalog_id
		if service, err := s.provider.LoadService(svc.CatalogID); err == nil && service.Name != "" {
			serviceDisplayName = service.Name
		}

		statuses = append(statuses, types.ApplicationService{
			ID:      svc.ID.String(),
			Type:    serviceDisplayName,
			Status:  string(svc.Status),
			Message: svc.Message,
		})
	}

	return statuses
}

// getApplicationType retrieves the application type from catalog metadata.
func (s *ApplicationService) getApplicationType(catalogID string, deploymentType models.DeploymentType) (string, error) {
	if deploymentType == models.DeploymentTypeArchitectures {
		arch, err := s.provider.LoadArchitecture(catalogID)
		if err != nil {
			return "", fmt.Errorf("failed to load architecture metadata: %w", err)
		}

		return arch.Name, nil
	}

	// For services
	service, err := s.provider.LoadService(catalogID)
	if err != nil {
		return "", fmt.Errorf("failed to load service metadata: %w", err)
	}

	return service.Name, nil
}

// ValidatePaginationParams validates and returns pagination parameters with defaults.
func ValidatePaginationParams(page, pageSize int) (int, int, error) {
	// Apply defaults
	if page == 0 {
		page = constants.MinPage
	}
	if pageSize == 0 {
		pageSize = constants.DefaultPageSize
	}

	// Validate page
	if page < constants.MinPage {
		return 0, 0, fmt.Errorf("invalid page parameter: must be a positive integer")
	}

	// Validate page_size
	if pageSize < constants.MinPage || pageSize > constants.MaxPageSize {
		return 0, 0, fmt.Errorf("invalid page_size parameter: must be between 1 and %d", constants.MaxPageSize)
	}

	return page, pageSize, nil
}

func (s *ApplicationService) UpdateApplication(ctx context.Context, id uuid.UUID, userID, newName string) (*types.Application, error) {
	app, err := s.appRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: ErrMsgApplicationNotFound,
		}
	}
	if app.CreatedBy != userID {
		return nil, &ValidationError{
			Code:    http.StatusForbidden,
			Message: ErrMsgUserNotOwner,
		}
	}
	err = s.appRepo.UpdateDeploymentName(ctx, id, newName)
	if err != nil {
		return nil, fmt.Errorf("failed to update application name: %w", err)
	}
	updatedApp, err := s.appRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated application %w", err)
	}
	if updatedApp == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: ErrMsgApplicationNotFound,
		}
	}

	appData, err := s.buildApplication(*updatedApp)
	if err != nil {
		return nil, err
	}

	return &appData, nil
}

// CreateApplication creates a new application with the given configuration.
// It performs synchronous validation and planning, then spawns an async goroutine
// for deployment execution, returning 202 Accepted immediately.
func (s *ApplicationService) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	// Phase 1: Validate request and check for duplicate application name
	existingApp, err := s.appRepo.GetByName(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing application: %w", err)
	}
	if existingApp != nil {
		// Application with this name already exists - return conflict error
		return nil, &ValidationError{
			Code:    http.StatusConflict,
			Message: fmt.Sprintf(ErrMsgApplicationNameExists, req.Name),
		}
	}

	// Phase 2: Validate request payload
	if err := s.validator.ValidateDeploymentRequest(ctx, req); err != nil {
		return nil, err
	}

	// Phase 3: Create deployment plan (synchronous - fail fast if invalid)
	// Use podman as default runtime type for planning
	plan, err := s.deploymentPlanner.PlanDeployment(ctx, req, runtimeTypes.RuntimeTypePodman.String())
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment plan: %w", err)
	}

	// Phase 4: Insert database records for application, services, components, and dependencies
	if err := s.insertDeploymentRecords(ctx, plan, req.CreatedBy); err != nil {
		return nil, fmt.Errorf("failed to insert deployment records: %w", err)
	}

	// Phase 5: Spawn goroutine for async deployment execution with panic recovery
	// Preserve requestID from the original context for tracing async operations
	go s.executeDeploymentAsync(ctx, plan, req)

	// Phase 6: Return 202 Accepted immediately with application ID
	response := &apimodels.CreateApplicationResponse{
		ID: plan.ApplicationID.String(),
	}

	return response, nil
}

// executeDeploymentAsync executes the deployment in a background goroutine.
// It updates the application status in the database based on deployment outcome.
// Includes panic recovery to prevent crashes.
// The context parameter should contain the requestID from the original HTTP request for tracing.
func (s *ApplicationService) executeDeploymentAsync(parentCtx context.Context, plan *deployment.DeploymentPlan, req apimodels.CreateApplicationRequest) {
	// Extract requestID from parent context to preserve it across the async boundary
	var requestID string
	if id, ok := parentCtx.Value(logger.RequestIDKey).(string); ok {
		requestID = id
	}

	// Create a new background context for the async operation (not tied to the HTTP request lifecycle)
	// but preserve the requestID for tracing
	ctx := context.Background()
	if requestID != "" {
		ctx = context.WithValue(ctx, logger.RequestIDKey, requestID)
	}

	// Defer panic recovery
	defer func() {
		if r := recover(); r != nil {
			logger.ErrorfCtx(ctx, "Panic recovered in deployment goroutine for application %s: %v", plan.ApplicationName, r)

			// Attempt to update application status to Error
			errMsg := fmt.Sprintf("Deployment panic: %v", r)
			if updateErr := utils.UpdateApplicationStatus(ctx, s.appRepo, plan.ApplicationID.String(), models.ApplicationStatusError, errMsg); updateErr != nil {
				logger.ErrorfCtx(ctx, "Failed to update application status after panic: %v", updateErr)
			}
		}
	}()

	// Determine runtime type (currently only Podman is supported)
	runtimeType := runtimeTypes.RuntimeTypePodman

	// Execute deployment using the existing plan
	err := s.deploymentExecutor.ExecuteWithPlan(ctx, plan, req, runtimeType)
	if err != nil {
		logger.ErrorfCtx(ctx, "Deployment failed for application %s: %v", plan.ApplicationName, err)

		// Update application status to Error
		if updateErr := utils.UpdateApplicationStatus(ctx, s.appRepo, plan.ApplicationID.String(), models.ApplicationStatusError, err.Error()); updateErr != nil {
			logger.ErrorfCtx(ctx, "Failed to update application status to Error: %v", updateErr)
		}

		return
	}

	logger.InfolnCtx(ctx, fmt.Sprintf("Deployment completed successfully for application %s", plan.ApplicationName))
}

// insertDeploymentRecords inserts all database records for the deployment plan.
// This includes: application, services, components (new ones), and service dependencies.
func (s *ApplicationService) insertDeploymentRecords(
	ctx context.Context,
	plan *deployment.DeploymentPlan,
	createdBy string,
) error {
	// 1. Insert application record
	if err := s.insertApplicationRecord(ctx, plan, createdBy); err != nil {
		return err
	}

	// 2. Insert component records
	componentIDMap, err := s.insertComponentRecords(ctx, plan)
	if err != nil {
		return err
	}

	// 3. Insert service records and their dependencies
	if err := s.insertServiceRecords(ctx, plan, componentIDMap); err != nil {
		return err
	}

	return nil
}

// insertApplicationRecord inserts the application record into the database.
func (s *ApplicationService) insertApplicationRecord(
	ctx context.Context,
	plan *deployment.DeploymentPlan,
	createdBy string,
) error {
	app := &models.Application{
		ID:             plan.ApplicationID,
		Name:           plan.ApplicationName,
		CatalogID:      plan.CatalogID,
		DeploymentType: utils.GetDeploymentType(plan.IsArchitecture),
		Status:         models.ApplicationStatusDownloading,
		Message:        "Initializing deployment",
		Version:        plan.Version,
		CreatedBy:      createdBy,
	}

	if err := s.appRepo.Insert(ctx, app); err != nil {
		return fmt.Errorf("failed to insert application: %w", err)
	}

	return nil
}

// insertComponentRecords inserts component records and returns a map of component hashes to UUIDs.
func (s *ApplicationService) insertComponentRecords(
	ctx context.Context,
	plan *deployment.DeploymentPlan,
) (map[string]uuid.UUID, error) {
	componentIDMap := make(map[string]uuid.UUID)

	for hash, comp := range plan.Components {
		instanceUUID := uuid.New()

		// Filter metadata to exclude sensitive data based on schema
		metadata, err := s.filterComponentMetadata(ctx, comp.ComponentType, comp.ProviderID, comp.Params)
		if err != nil {
			return nil, fmt.Errorf("failed to filter component metadata for %s: %w", hash, err)
		}

		component := &models.Component{
			ID:       instanceUUID,
			Type:     comp.ComponentType,
			Provider: comp.ProviderID,
			Status:   models.ComponentStatusInitializing,
			Version:  comp.Version,
			Metadata: metadata,
		}

		if err := s.componentRepo.Insert(ctx, component); err != nil {
			return nil, fmt.Errorf("failed to insert component %s: %w", hash, err)
		}

		componentIDMap[hash] = instanceUUID
		comp.DatabaseID = instanceUUID
	}

	return componentIDMap, nil
}

// insertServiceRecords inserts service records and their dependencies.
func (s *ApplicationService) insertServiceRecords(
	ctx context.Context,
	plan *deployment.DeploymentPlan,
	componentIDMap map[string]uuid.UUID,
) error {
	for serviceID, svc := range plan.Services {
		service := &models.Service{
			ID:        uuid.Nil,
			AppID:     plan.ApplicationID,
			CatalogID: svc.CatalogID,
			Status:    models.ServiceStatusInitializing,
			Version:   svc.Version,
		}

		if err := s.serviceRepo.Insert(ctx, service); err != nil {
			return fmt.Errorf("failed to insert service %s: %w", serviceID, err)
		}

		svc.DatabaseID = service.ID

		if err := s.insertServiceDependencies(ctx, service.ID, svc.ComponentRefs, componentIDMap); err != nil {
			return err
		}
	}

	return nil
}

// insertServiceDependencies inserts dependencies between services and components.
func (s *ApplicationService) insertServiceDependencies(
	ctx context.Context,
	serviceID uuid.UUID,
	componentRefs []string,
	componentIDMap map[string]uuid.UUID,
) error {
	for _, compHash := range componentRefs {
		componentID, exists := componentIDMap[compHash]
		if !exists {
			return fmt.Errorf("component hash %s not found in component map", compHash)
		}

		dependency := &models.ServiceDependency{
			ServiceID:      serviceID,
			DependencyID:   componentID,
			DependencyType: models.DependencyTypeComponent,
		}

		if err := s.serviceDependencyRepo.AddDependency(ctx, dependency); err != nil {
			return fmt.Errorf("failed to add service dependency: %w", err)
		}
	}

	return nil
}

// GetApplicationByID retrieves application details by ID including all services and components.
func (s *ApplicationService) GetApplicationByID(ctx context.Context, id uuid.UUID) (*types.Application, error) {
	// Fetch application from database
	app, err := s.appRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: ErrMsgApplicationNotFound,
		}
	}
	// Build complete response with services and components
	return s.buildGetApplicationResponse(ctx, app)
}

// buildGetApplicationResponse constructs the application response with type info and nested services.
func (s *ApplicationService) buildGetApplicationResponse(ctx context.Context, app *models.Application) (*types.Application, error) {
	// Get application type display name from catalog metadata
	typeName, err := s.getApplicationType(app.CatalogID, app.DeploymentType)
	if err != nil {
		return nil, fmt.Errorf("failed to get application type for catalog_id '%s': %w", app.CatalogID, err)
	}
	// Build base application response
	appresponse := &types.Application{
		ID:             app.ID.String(),
		Name:           app.Name,
		CatalogID:      app.CatalogID,
		DeploymentType: string(app.DeploymentType),
		Type:           typeName,
		Status:         string(app.Status),
		Message:        app.Message,
		Version:        app.Version,
		CreatedAt:      app.CreatedAt.Format(constants.RFC3339WithTimezone),
		UpdatedAt:      app.UpdatedAt.Format(constants.RFC3339WithTimezone),
	}

	// Load services with their components if present
	if len(app.Services) > 0 {
		appresponse.Services, err = s.loadApplicationServices(ctx, app.Services)
		if err != nil {
			return nil, fmt.Errorf("failed to get application services: %w", err)
		}
	}

	return appresponse, nil
}

// loadApplicationServices transforms service models to API response objects with components.
func (s *ApplicationService) loadApplicationServices(ctx context.Context, services []models.Service) ([]types.ApplicationService, error) {
	appServices := []types.ApplicationService{}
	for _, service := range services {
		// Build application service response
		serviceDisplayName := service.CatalogID
		if service, err := s.provider.LoadService(service.CatalogID); err == nil && service.Name != "" {
			serviceDisplayName = service.Name
		}

		appService := types.ApplicationService{
			ID:        service.ID.String(),
			Type:      serviceDisplayName,
			CatalogID: service.CatalogID,
			Endpoints: service.Endpoints,
			Version:   service.Version,
			Status:    string(service.Status),
			CreatedAt: service.CreatedAt.Format(constants.RFC3339WithTimezone),
			UpdatedAt: service.UpdatedAt.Format(constants.RFC3339WithTimezone),
		}

		// Get all dependencies for this service
		serviceDependencies, err := s.serviceDependencyRepo.GetDependenciesByServiceID(ctx, service.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get application dependencies: %w", err)
		}

		// Load component details from dependencies
		appService.Component, err = s.loadServiceComponents(ctx, serviceDependencies)
		if err != nil {
			return nil, err
		}
		appServices = append(appServices, appService)
	}

	return appServices, nil
}

// loadServiceComponents extracts component details from service dependencies.
func (s *ApplicationService) loadServiceComponents(ctx context.Context, sd []models.ServiceDependency) ([]types.ServiceComponentResp, error) {
	components := []types.ServiceComponentResp{}
	for _, dependency := range sd {
		// Only process component-type dependencies
		if dependency.DependencyType == models.DependencyTypeComponent {
			// Fetch component details from database
			component, err := s.componentRepo.GetByID(ctx, dependency.DependencyID)
			if err != nil {
				return nil, fmt.Errorf("failed to get component: %w", err)
			}
			if component == nil {
				continue
			}

			// Get provider name from catalog metadata using existing LoadComponent helper
			componentMetadata, err := s.provider.LoadComponent(component.Type, component.Provider)
			if err != nil {
				return nil, fmt.Errorf("failed to load component metadata for %s/%s: %w", component.Type, component.Provider, err)
			}

			providerName := component.Provider // Default to provider ID
			if componentMetadata != nil && componentMetadata.Name != "" {
				providerName = componentMetadata.Name
			}

			// Transform to response object
			temp := types.ServiceComponentResp{
				ID:   component.ID.String(),
				Type: component.Type,
				Provider: types.ProviderInfo{
					ID:   component.Provider,
					Name: providerName,
				},
				Status:   string(component.Status),
				Message:  component.Message,
				Metadata: component.Metadata,
			}
			components = append(components, temp)
		}
	}

	return components, nil
}

// DeleteApplicationResponse is the response body for a delete application request.
type DeleteApplicationResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// DeleteApplication initiates async deletion of an application and returns immediately.
func (s *ApplicationService) DeleteApplication(ctx context.Context, id uuid.UUID, user string, keepData bool) (*DeleteApplicationResponse, error) {
	app, err := s.appRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: ErrMsgApplicationNotFound,
		}
	}

	if app.CreatedBy != user {
		return nil, &ValidationError{
			Code:    http.StatusForbidden,
			Message: ErrMsgUserNotOwner,
		}
	}

	if app.Status == models.ApplicationStatusDeleting {
		return nil, &ValidationError{
			Code:    http.StatusConflict,
			Message: ErrMsgApplicationAlreadyDeleting,
		}
	}

	if err := utils.UpdateApplicationStatus(ctx, s.appRepo, id, models.ApplicationStatusDeleting, "Deletion initiated"); err != nil {
		return nil, err
	}

	// Extract requestID from context to preserve it across the async boundary
	var requestID string
	if reqID, ok := ctx.Value(logger.RequestIDKey).(string); ok {
		requestID = reqID
	}

	// Create a new background context for the async operation but preserve the requestID
	deletionCtx := context.Background()
	if requestID != "" {
		deletionCtx = context.WithValue(deletionCtx, logger.RequestIDKey, requestID)
	}

	go s.deletionService.PerformDeletion(deletionCtx, id, app.Services, keepData)

	return &DeleteApplicationResponse{
		ID:      id.String(),
		Status:  string(models.ApplicationStatusDeleting),
		Message: "Deletion initiated successfully",
	}, nil
}

// filterComponentMetadata filters component parameters to exclude sensitive data.
// It reads the component's schema and excludes fields marked as sensitive (e.g., format: "password").
// Returns an error if the schema cannot be loaded or parsed.
func (s *ApplicationService) filterComponentMetadata(ctx context.Context, componentType, providerID string, params map[string]any) (map[string]any, error) {
	if params == nil {
		return nil, nil
	}

	// Load component schema to determine which fields are sensitive
	schema, err := s.provider.GetComponentProviderParams(ctx, componentType, providerID)
	if err != nil {
		return nil, fmt.Errorf("failed to load schema for component %s/%s: %w", componentType, providerID, err)
	}

	// Extract properties from schema
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema for component %s/%s has no properties", componentType, providerID)
	}

	// Filter out sensitive fields recursively
	metadata, err := s.filterSensitiveFields(ctx, params, properties)
	if err != nil {
		return nil, fmt.Errorf("failed to filter sensitive fields: %w", err)
	}

	return metadata, nil
}

// filterSensitiveFields recursively filters out sensitive fields from params based on schema properties.
// Returns an error if there are issues processing nested structures.
func (s *ApplicationService) filterSensitiveFields(ctx context.Context, params map[string]any, properties map[string]any) (map[string]any, error) {
	metadata := make(map[string]any)

	for key, value := range params {
		// Check if this field exists in the schema
		fieldSchema, exists := properties[key].(map[string]any)
		if !exists {
			// If field not in schema, skip it (don't include in metadata)
			continue
		}

		// Check if field is marked as sensitive (format: "password")
		if format, hasFormat := fieldSchema["format"].(string); hasFormat && format == "password" {
			logger.DebugfCtx(ctx, "Excluding sensitive field '%s' from component metadata", key)

			continue
		}

		// Handle nested objects recursively
		if valueMap, isMap := value.(map[string]any); isMap {
			// Check if the field schema has nested properties
			if nestedProps, hasNestedProps := fieldSchema["properties"].(map[string]any); hasNestedProps {
				// Recursively filter nested object
				filteredNested, err := s.filterSensitiveFields(ctx, valueMap, nestedProps)
				if err != nil {
					return nil, fmt.Errorf("failed to filter nested field '%s': %w", key, err)
				}
				metadata[key] = filteredNested

				continue
			}
		}

		// Include non-sensitive fields
		metadata[key] = value
	}

	return metadata, nil
}

// GetApplicationResources retrieves resource usage (CPU, memory) for an application using runtime-specific stats.
func (s *ApplicationService) GetApplicationResources(ctx context.Context, id uuid.UUID) (*types.ApplicationResourcesResponse, error) {
	// Fetch application from database
	app, err := s.appRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: ErrMsgApplicationNotFound,
		}
	}

	// Create runtime client
	runtimeClient, err := vars.RuntimeFactory.Create("")
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime client: %w", err)
	}

	// Create catalog provider to load service metadata
	catalogProvider, err := catalog.NewCatalogProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog provider: %w", err)
	}

	// Collect resources from all services
	resourceTotals, err := s.collectResources(ctx, app, runtimeClient, catalogProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to collect application resources: %w", err)
	}

	// Build and return response
	return buildResourcesResponse(resourceTotals), nil
}

// resourceTotals holds aggregated resource information.
type resourceTotals struct {
	allocatedCPU    int
	allocatedMemory int
	usedCPU         float64
	usedMemory      uint64
	spyreCards      map[string]bool
}

// collectResources aggregates resources from all services in an application.
func (s *ApplicationService) collectResources(
	ctx context.Context,
	app *models.Application,
	runtimeClient runtime.Runtime,
	catalogProvider *catalog.CatalogProvider,
) (*resourceTotals, error) {
	totals := &resourceTotals{
		spyreCards: make(map[string]bool),
	}

	// Map to track components to avoid double-counting shared components among services
	countedComponents := make(map[uuid.UUID]bool)

	for _, service := range app.Services {
		if err := s.processServiceResources(ctx, app.Name, service, runtimeClient, catalogProvider, totals, countedComponents); err != nil {
			return nil, fmt.Errorf("failed to process service %s resources: %w", service.ID, err)
		}
	}

	return totals, nil
}

// processServiceResources processes a single service and updates resource totals.
func (s *ApplicationService) processServiceResources(
	ctx context.Context,
	appName string,
	service models.Service,
	runtimeClient runtime.Runtime,
	catalogProvider *catalog.CatalogProvider,
	totals *resourceTotals,
	countedComponents map[uuid.UUID]bool,
) error {
	// Get the resources (allocated + used) for deployed service
	if err := s.addServiceResources(service, catalogProvider, runtimeClient, totals); err != nil {
		return fmt.Errorf("failed to get service allocated resources: %w", err)
	}

	// Get the resources (allocated + used) for deployed components for this service
	// Pass countedComponents to avoid double-counting shared components
	if err := s.addComponentResources(ctx, service.ID, catalogProvider, runtimeClient, totals, countedComponents); err != nil {
		return fmt.Errorf("failed to get component allocated resources: %w", err)
	}

	return nil
}

// addAllocatedResources is a helper function that adds allocated CPU and memory from runtime metadata to totals.
func addAllocatedResources(runtimeMetadata *clitemplates.AppMetadata, totals *resourceTotals) {
	if runtimeMetadata.Resources != nil {
		totals.allocatedCPU += runtimeMetadata.Resources.CPU
		totals.allocatedMemory += runtimeMetadata.Resources.Memory
	}
}

// addServiceResources adds allocated and used resources from service metadata.
func (s *ApplicationService) addServiceResources(
	service models.Service,
	catalogProvider *catalog.CatalogProvider,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
) error {
	runtimeMetadata, err := catalogProvider.LoadServiceRuntimeMetadata(service.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to load service runtime metadata for catalog ID %s: %w", service.CatalogID, err)
	}

	addAllocatedResources(runtimeMetadata, totals)

	// Get the used resources for the service by fetching pods with service id
	// Each pod deployed has label: ai-services.io/template: "<service-database-id>"
	if err := addUsedResourcesByTemplateID(service.ID.String(), runtimeClient, totals); err != nil {
		return fmt.Errorf("failed to get service used resources: %w", err)
	}

	return nil
}

// addComponentResources adds allocated and used resources from the actual deployed component providers.
// This ensures we only count resources for the specific component providers deployed for this service,
// not all possible provider options. Components are tracked to avoid double-counting when shared across services.
func (s *ApplicationService) addComponentResources(
	ctx context.Context,
	serviceID uuid.UUID,
	catalogProvider *catalog.CatalogProvider,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
	countedComponents map[uuid.UUID]bool,
) error {
	// Get service dependencies (components) from database
	dependencies, err := s.serviceDependencyRepo.GetDependenciesByServiceID(ctx, serviceID)
	if err != nil {
		return fmt.Errorf("failed to get dependencies for service %s: %w", serviceID, err)
	}

	// Process each component dependency
	for _, dep := range dependencies {
		// skip if this is not a component dependency or if it is already counted
		if (dep.DependencyType != models.DependencyTypeComponent) || countedComponents[dep.DependencyID] {
			continue
		}

		// Process each component to count the allocated and used resources
		if err := s.processComponentResources(ctx, dep.DependencyID, catalogProvider, runtimeClient, totals); err != nil {
			return err
		}

		// Mark this component as counted
		countedComponents[dep.DependencyID] = true
	}

	return nil
}

// processComponentResources processes a single component and updates resource totals.
func (s *ApplicationService) processComponentResources(
	ctx context.Context,
	componentID uuid.UUID,
	catalogProvider *catalog.CatalogProvider,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
) error {
	component, err := s.componentRepo.GetByID(ctx, componentID)
	if err != nil {
		return fmt.Errorf("failed to get component %s: %w", componentID, err)
	}

	// Load component runtime metadata for the specific provider
	runtimeMetadata, err := catalogProvider.LoadComponentRuntimeMetadata(component.Type, component.Provider)
	if err != nil {
		return fmt.Errorf("failed to load runtime metadata for component %s/%s: %w", component.Type, component.Provider, err)
	}

	// Add allocated resources from this specific component provider
	addAllocatedResources(runtimeMetadata, totals)

	// Get all used resources for the pods of this component
	// Each pod deployed has label: ai-services.io/template: "<component-database-id>"
	if err := addUsedResourcesByTemplateID(component.ID.String(), runtimeClient, totals); err != nil {
		return fmt.Errorf("failed to get component used resources for %s: %w", component.ID, err)
	}

	return nil
}

// addUsedResourcesByTemplateID fetches and adds used resources from all pods with a given template ID label.
// This handles cases where a service or component has multiple pods (e.g., digitize has digitize-{slug} and digitize-db-{slug}).
func addUsedResourcesByTemplateID(
	templateID string,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
) error {
	// List all pods with the template ID label
	filters := map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/template=%s", templateID)},
	}

	pods, err := runtimeClient.ListPods(filters)
	if err != nil {
		return fmt.Errorf("failed to list pods for template %s: %w", templateID, err)
	}

	// Aggregate resources from all pods
	for _, pod := range pods {
		if err := collectPodResources(pod.Name, runtimeClient, totals); err != nil {
			return fmt.Errorf("failed to get used resources for pod %s: %w", pod.Name, err)
		}
	}

	return nil
}

// collectPodResources fetches and accumulates used resources from a single pod.
func collectPodResources(
	podName string,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
) error {
	resources, err := runtimeClient.GetPodResources(podName)
	if err != nil {
		return fmt.Errorf("failed to get resources for pod %s: %w", podName, err)
	}

	// Track all unique Spyre cards
	for _, card := range resources.SpyreCards {
		totals.spyreCards[card] = true
	}

	// Accumulate used resources
	totals.usedCPU += resources.CPUCores
	totals.usedMemory += resources.MemUsage

	return nil
}

// buildResourcesResponse constructs the final response from resource totals.
func buildResourcesResponse(totals *resourceTotals) *types.ApplicationResourcesResponse {
	// Convert map to slice for Spyre cards
	totalSpyreCards := make([]string, 0, len(totals.spyreCards))
	for card := range totals.spyreCards {
		totalSpyreCards = append(totalSpyreCards, card)
	}

	// Build accelerators map
	accelerators := make(map[string][]string)
	if len(totalSpyreCards) > 0 {
		accelerators["ibm.com/spyre_pf"] = totalSpyreCards
	}

	// Build response with total and used resources
	return &types.ApplicationResourcesResponse{
		CPU: types.ApplicationCPUInfo{
			TotalCores: float64(totals.allocatedCPU),
			UsedCores:  math.Round(totals.usedCPU*consts.PercentageDivisor) / consts.PercentageDivisor,
		},
		Memory: types.ApplicationMemInfo{
			TotalBytes: int64(totals.allocatedMemory),
			UsedBytes:  int64(totals.usedMemory),
		},
		Accelerators: accelerators,
	}
}

// ApplicationsPs retrieves runtime pod status for an application and its related resources.
// It loads the application from the database, inspects service pods directly, and then
// resolves component pods through service dependencies so shared components are returned once.
func (s *ApplicationService) ApplicationsPs(ctx context.Context, appID uuid.UUID) (*types.ApplicationPSResponse, error) {
	app, err := s.appRepo.GetByID(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: ErrMsgApplicationNotFound,
		}
	}

	// Initialize runtime client for pod operations
	rt, err := vars.RuntimeFactory.Create("")
	if err != nil {
		return nil, fmt.Errorf("failed to init %s client: %w", rt.Type(), err)
	}

	// Collect service pod details (one pod per service)
	servicePods, err := s.collectServicePods(ctx, rt, app.Services)
	if err != nil {
		return nil, fmt.Errorf("failed to collect service pods: %w", err)
	}

	// Collect component pod details
	componentPods, err := s.collectComponentPods(ctx, rt, app.Services)
	if err != nil {
		return nil, fmt.Errorf("failed to collect component pods: %w", err)
	}

	// Build response with application metadata and all pods
	return &types.ApplicationPSResponse{
		ID:         app.ID.String(),
		Name:       app.Name,
		Services:   servicePods,
		Components: componentPods,
	}, nil
}

// collectServicePods resolves one pod view per service by querying Podman with the
// service template label. Missing or failed lookups are logged and skipped so one
// unhealthy service does not prevent returning status for the rest of the application.
func (s *ApplicationService) collectServicePods(
	ctx context.Context,
	rt runtime.Runtime,
	services []models.Service,
) ([]types.Pod, error) {
	// Pre-allocate slice for efficiency (one pod per service expected)
	servicePods := make([]types.Pod, 0, len(services))

	// Load pod details for each service
	for _, service := range services {
		// Query runtime using service ID as template label
		pod, err := loadApplicationPods(rt, service.ID.String())
		if err != nil {
			// Log error but continue with other services (fault-tolerant)
			logger.ErrorfCtx(ctx, "Failed to load service pod: %v", err)

			continue
		}
		servicePods = append(servicePods, pod...)
	}

	logger.InfofCtx(ctx, "Successfully collected %d service pods", len(servicePods))

	return servicePods, nil
}

// collectComponentPods resolves pod details for component dependencies referenced by
// application services. Components are deduplicated by dependency ID because the same
// backing component can be shared by multiple services within one application.
func (s *ApplicationService) collectComponentPods(
	ctx context.Context,
	rt runtime.Runtime,
	services []models.Service,
) ([]types.Pod, error) {
	// Use map to deduplicate shared components (key: component ID)
	componentMap := make(map[string][]types.Pod)

	// Iterate through services to find component dependencies
	for _, service := range services {
		// Fetch service dependencies from database
		serviceDependencies, err := s.serviceDependencyRepo.GetDependenciesByServiceID(ctx, service.ID)
		if err != nil {
			logger.ErrorfCtx(ctx, "Failed to get dependencies for service %s: %v", service.ID, err)

			continue
		}

		// Extract component pods from dependencies
		for _, dependency := range serviceDependencies {
			if dependency.DependencyType != models.DependencyTypeComponent {
				continue
			}

			componentID := dependency.DependencyID.String()

			// Skip if component already processed (deduplication)
			if _, exists := componentMap[componentID]; exists {
				continue
			}

			// Load component pod from runtime
			componentPod, err := loadApplicationPods(rt, componentID)
			if err != nil {
				logger.ErrorfCtx(ctx, "Failed to load component pod: %v", err)

				continue
			}

			// Store in map to prevent duplicate processing
			componentMap[componentID] = componentPod
		}
	}

	// Convert map to slice for response
	componentPods := make([]types.Pod, 0, len(componentMap))
	for _, podDetails := range componentMap {
		componentPods = append(componentPods, podDetails...)
	}

	logger.InfofCtx(ctx, "Successfully collected %d unique component pods", len(componentPods))

	return componentPods, nil
}

// loadApplicationPods fetches the application pods from the runtime.
func loadApplicationPods(rt runtime.Runtime, appID string) ([]types.Pod, error) {
	filteredPod, err := common.FetchFilteredPods(rt, appID)
	if err != nil {
		return nil, err
	}
	// Validate exactly one pod exists
	if len(filteredPod) == 0 {
		return nil, fmt.Errorf("no pod found with given id")
	}

	appPodList := make([]types.Pod, 0, len(filteredPod))

	for _, pod := range filteredPod {
		processedPod, err := common.ProcessPod(rt, pod)
		if err != nil {
			return nil, fmt.Errorf("failed to process pod: %w", err)
		}

		// Transform containers to API response format with health indicators
		containers := make([]types.PodContainer, 0, len(pod.Containers))
		for _, container := range processedPod.Containers {
			containers = append(containers, types.PodContainer{
				Name:    container.Name,
				Status:  types.Status(strings.ToLower(processedPod.Status)),
				Healthy: strings.ToLower(container.Health) == string(consts.Ready),
			})
		}

		appPod := types.Pod{
			PodID:      processedPod.ID,
			PodName:    processedPod.Name,
			Status:     types.Status(strings.ToLower(processedPod.Status)),
			Healthy:    processedPod.Health == string(consts.Ready),
			Created:    pod.Created.Format(constants.RFC3339WithTimezone),
			Containers: containers,
		}

		appPodList = append(appPodList, appPod)
	}

	// Build pod response with metadata and container details
	return appPodList, nil
}

// Made with Bob
