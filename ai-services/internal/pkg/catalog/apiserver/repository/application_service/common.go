package applicationservice

import (
	"context"
	"fmt"
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
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	consts "github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// ValidationError represents a validation error with HTTP status code.
type ValidationError = validators.ValidationError

// ListApplicationsRequest contains parameters for listing applications.
type ListApplicationsRequest struct {
	Page           int
	PageSize       int
	DeploymentType string
	CatalogID      string
}

// DeleteApplicationResponse is the response body for a delete application request.
type DeleteApplicationResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
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

// ApplicationServiceBase holds the fields and methods that are identical across all
// runtime implementations. The Podman and OpenShift concrete service types embed this
// struct and inherit these methods without any changes.
type ApplicationServiceBase struct {
	AppRepo               dbrepo.ApplicationRepository
	ServiceRepo           dbrepo.ServiceRepository
	ComponentRepo         dbrepo.ComponentRepository
	ServiceDependencyRepo dbrepo.ServiceDependencyRepository
	Provider              *catalog.CatalogProvider
	DeploymentPlanner     *deployment.DeploymentPlanner
	DeploymentExecutor    *deployment.DeploymentExecutor
	DeletionService       *deletion.DeletionService
	Validator             *validators.ApplicationValidator
}

// ListApplications retrieves a paginated list of applications with filters.
// buildApplication creates an Application from a models.Application.
func (s *ApplicationServiceBase) buildApplication(app models.Application) (types.Application, error) {
	// Get type (display name) from catalog metadata
	typeName, err := s.getApplicationType(app.CatalogID, app.DeploymentType)
	if err != nil {
		return types.Application{}, fmt.Errorf("failed to get application type for catalog_id '%s': %w", app.CatalogID, err)
	}

	appData := types.Application{
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

	// Add services array only for architectures (not for individual services)
	if app.DeploymentType == models.DeploymentTypeArchitectures && len(app.Services) > 0 {
		appData.Services = s.buildServiceStatuses(app.Services)
	}

	return appData, nil
}

// buildServiceStatuses creates ApplicationService array from models.Service slice.
func (s *ApplicationServiceBase) buildServiceStatuses(services []models.Service) []types.ApplicationService {
	statuses := make([]types.ApplicationService, 0, len(services))

	for _, svc := range services {
		// Get service display name from catalog metadata
		serviceDisplayName := svc.CatalogID // Default to catalog_id
		if service, err := s.Provider.LoadService(svc.CatalogID); err == nil && service.Name != "" {
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
func (s *ApplicationServiceBase) getApplicationType(catalogID string, deploymentType models.DeploymentType) (string, error) {
	if deploymentType == models.DeploymentTypeArchitectures {
		arch, err := s.Provider.LoadArchitecture(catalogID)
		if err != nil {
			return "", fmt.Errorf("failed to load architecture metadata: %w", err)
		}

		return arch.Name, nil
	}

	// For services
	service, err := s.Provider.LoadService(catalogID)
	if err != nil {
		return "", fmt.Errorf("failed to load service metadata: %w", err)
	}

	return service.Name, nil
}

// UpdateApplication updates the display name of an existing application.
func (s *ApplicationServiceBase) UpdateApplication(ctx context.Context, id uuid.UUID, userID, newName string) (*types.Application, error) {
	existingApp, err := s.AppRepo.GetByName(ctx, newName)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing application: %w", err)
	}
	if existingApp != nil {
		// Application with this name already exists - return conflict error
		return nil, &ValidationError{
			Code:    http.StatusConflict,
			Message: fmt.Sprintf(ErrMsgApplicationNameExists, newName),
		}
	}

	app, err := s.AppRepo.GetByID(ctx, id)
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

	err = s.AppRepo.UpdateDeploymentName(ctx, id, newName)
	if err != nil {
		return nil, fmt.Errorf("failed to update name: %w", err)
	}
	updatedApp, err := s.AppRepo.GetByID(ctx, id)
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

// GetApplicationByID retrieves application details by ID including all services and components.
func (s *ApplicationServiceBase) GetApplicationByID(ctx context.Context, id uuid.UUID) (*types.Application, error) {
	// Fetch application from database
	app, err := s.AppRepo.GetByID(ctx, id)
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
func (s *ApplicationServiceBase) buildGetApplicationResponse(ctx context.Context, app *models.Application) (*types.Application, error) {
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
func (s *ApplicationServiceBase) loadApplicationServices(ctx context.Context, services []models.Service) ([]types.ApplicationService, error) {
	appServices := []types.ApplicationService{}
	for _, service := range services {
		// Build application service response
		serviceDisplayName := service.CatalogID
		if service, err := s.Provider.LoadService(service.CatalogID); err == nil && service.Name != "" {
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
		serviceDependencies, err := s.ServiceDependencyRepo.GetDependenciesByServiceID(ctx, service.ID)
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
func (s *ApplicationServiceBase) loadServiceComponents(ctx context.Context, sd []models.ServiceDependency) ([]types.ServiceComponentResp, error) {
	components := []types.ServiceComponentResp{}
	for _, dependency := range sd {
		// Only process component-type dependencies
		if dependency.DependencyType == models.DependencyTypeComponent {
			// Fetch component details from database
			component, err := s.ComponentRepo.GetByID(ctx, dependency.DependencyID)
			if err != nil {
				return nil, fmt.Errorf("failed to get component: %w", err)
			}
			if component == nil {
				continue
			}

			// Get provider name from catalog metadata using existing LoadComponent helper
			componentMetadata, err := s.Provider.LoadComponent(component.Type, component.Provider)
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

// filterComponentMetadata filters component parameters to exclude sensitive data.
func (s *ApplicationServiceBase) filterComponentMetadata(ctx context.Context, componentType, providerID string, params map[string]any) (map[string]any, error) {
	if params == nil {
		return nil, nil
	}

	// Load component schema to determine which fields are sensitive
	schema, err := s.Provider.GetComponentProviderParams(ctx, componentType, providerID)
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
func (s *ApplicationServiceBase) filterSensitiveFields(ctx context.Context, params map[string]any, properties map[string]any) (map[string]any, error) {
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

// InsertDeploymentRecords inserts all database records for the deployment plan.
// This includes: application, services, components (new ones), and service dependencies.
func (s *ApplicationServiceBase) InsertDeploymentRecords(
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
func (s *ApplicationServiceBase) insertApplicationRecord(
	ctx context.Context,
	plan *deployment.DeploymentPlan,
	createdBy string,
) error {
	app := &models.Application{
		ID:             plan.ApplicationID,
		Name:           plan.ApplicationName,
		CatalogID:      plan.CatalogID,
		DeploymentType: catalogutils.GetDeploymentType(plan.IsArchitecture),
		Status:         models.ApplicationStatusDownloading,
		Message:        "Initializing deployment",
		Version:        plan.Version,
		CreatedBy:      createdBy,
	}

	if err := s.AppRepo.Insert(ctx, app); err != nil {
		return fmt.Errorf("failed to insert application: %w", err)
	}

	return nil
}

// insertComponentRecords inserts component records and returns a map of component hashes to UUIDs.
func (s *ApplicationServiceBase) insertComponentRecords(
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

		if err := s.ComponentRepo.Insert(ctx, component); err != nil {
			return nil, fmt.Errorf("failed to insert component %s: %w", hash, err)
		}

		componentIDMap[hash] = instanceUUID
		comp.DatabaseID = instanceUUID
	}

	return componentIDMap, nil
}

// insertServiceRecords inserts service records and their dependencies.
func (s *ApplicationServiceBase) insertServiceRecords(
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

		if err := s.ServiceRepo.Insert(ctx, service); err != nil {
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
func (s *ApplicationServiceBase) insertServiceDependencies(
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

		if err := s.ServiceDependencyRepo.AddDependency(ctx, dependency); err != nil {
			return fmt.Errorf("failed to add service dependency: %w", err)
		}
	}

	return nil
}

// ListApplications retrieves a paginated list of applications with filters.
func (s *ApplicationServiceBase) ListApplications(ctx context.Context, req ListApplicationsRequest) (*types.ApplicationListResponse, error) {
	if req.Page < 1 {
		return nil, fmt.Errorf("page must be greater than 0")
	}
	if req.PageSize < 1 {
		return nil, fmt.Errorf("pageSize must be greater than 0")
	}

	filters := &dbrepo.ApplicationFilters{
		DeploymentType: req.DeploymentType,
		CatalogID:      req.CatalogID,
		Limit:          req.PageSize,
		Offset:         (req.Page - 1) * req.PageSize,
	}

	totalCount, err := s.AppRepo.GetCount(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get application count: %w", err)
	}

	applications, err := s.AppRepo.GetAll(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve applications: %w", err)
	}

	apps := make([]types.Application, 0, len(applications))
	for _, app := range applications {
		appData, err := s.buildApplication(app)
		if err != nil {
			return nil, err
		}

		apps = append(apps, appData)
	}

	totalPages := 0
	if totalCount > 0 {
		totalPages = (totalCount + req.PageSize - 1) / req.PageSize
	}

	return &types.ApplicationListResponse{
		Data: apps,
		Pagination: types.PaginationMetadata{
			Page:       req.Page,
			PageSize:   req.PageSize,
			TotalItems: totalCount,
			TotalPages: totalPages,
			HasNext:    req.Page < totalPages,
			HasPrev:    req.Page > 1,
		},
	}, nil
}

// CreateApplication validates, plans, persists, and asynchronously deploys a new application
// for the given runtime type.
func (s *ApplicationServiceBase) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest, runtimeType runtimeTypes.RuntimeType) (*apimodels.CreateApplicationResponse, error) {
	// Phase 1: check for duplicate name
	existingApp, err := s.AppRepo.GetByName(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing application: %w", err)
	}
	if existingApp != nil {
		return nil, &ValidationError{
			Code:    http.StatusConflict,
			Message: fmt.Sprintf(ErrMsgApplicationNameExists, req.Name),
		}
	}

	// Phase 2: validate payload
	if err := s.Validator.ValidateDeploymentRequest(ctx, req); err != nil {
		return nil, err
	}

	// Phase 3: create deployment plan
	plan, err := s.DeploymentPlanner.PlanDeployment(ctx, req, runtimeType.String())
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment plan: %w", err)
	}

	// Phase 4: persist DB records
	if err := s.InsertDeploymentRecords(ctx, plan, req.CreatedBy); err != nil {
		return nil, fmt.Errorf("failed to insert deployment records: %w", err)
	}

	// Phase 5: async deployment
	go s.executeDeploymentAsync(ctx, plan, req, runtimeType)

	return &apimodels.CreateApplicationResponse{ID: plan.ApplicationID.String()}, nil
}

// executeDeploymentAsync runs the deployment in a background goroutine for the given runtime type.
func (s *ApplicationServiceBase) executeDeploymentAsync(parentCtx context.Context, plan *deployment.DeploymentPlan, req apimodels.CreateApplicationRequest, runtimeType runtimeTypes.RuntimeType) {
	var requestID string
	if id, ok := parentCtx.Value(logger.RequestIDKey).(string); ok {
		requestID = id
	}

	ctx := context.Background()
	if requestID != "" {
		ctx = context.WithValue(ctx, logger.RequestIDKey, requestID)
	}

	defer func() {
		if r := recover(); r != nil {
			logger.ErrorfCtx(ctx, "Panic recovered in deployment goroutine for application %s: %v", plan.ApplicationName, r)

			errMsg := fmt.Sprintf("Deployment panic: %v", r)
			if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, plan.ApplicationID.String(), models.ApplicationStatusError, errMsg); updateErr != nil {
				logger.ErrorfCtx(ctx, "Failed to update application status after panic: %v", updateErr)
			}
		}
	}()

	err := s.DeploymentExecutor.ExecuteWithPlan(ctx, plan, req, runtimeType)
	if err != nil {
		logger.ErrorfCtx(ctx, "Deployment failed for application %s: %v", plan.ApplicationName, err)

		if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, plan.ApplicationID.String(), models.ApplicationStatusError, err.Error()); updateErr != nil {
			logger.ErrorfCtx(ctx, "Failed to update application status to Error: %v", updateErr)
		}

		return
	}

	logger.InfolnCtx(ctx, fmt.Sprintf("Deployment completed successfully for application %s", plan.ApplicationName))
}

// ApplicationsPs returns runtime pod/container status for an application by querying the configured runtime.
func (s *ApplicationServiceBase) ApplicationsPs(ctx context.Context, appID uuid.UUID, namespace string) (*types.ApplicationPSResponse, error) {
	app, err := s.AppRepo.GetByID(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: ErrMsgApplicationNotFound,
		}
	}

	rt, err := vars.RuntimeFactory.Create(namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to init runtime client: %w", err)
	}

	servicePods, err := s.collectServicePods(ctx, rt, app.Services)
	if err != nil {
		return nil, fmt.Errorf("failed to collect service pods: %w", err)
	}

	componentPods, err := s.collectComponentPods(ctx, rt, app.Services)
	if err != nil {
		return nil, fmt.Errorf("failed to collect component pods: %w", err)
	}

	return &types.ApplicationPSResponse{
		ID:         app.ID.String(),
		Name:       app.Name,
		Services:   servicePods,
		Components: componentPods,
	}, nil
}

func (s *ApplicationServiceBase) collectServicePods(
	ctx context.Context,
	rt runtime.Runtime,
	services []models.Service,
) ([]types.Pod, error) {
	servicePods := make([]types.Pod, 0, len(services))

	for _, service := range services {
		pod, err := loadApplicationPods(rt, service.ID.String())
		if err != nil {
			logger.ErrorfCtx(ctx, "Failed to load service pod: %v", err)

			continue
		}
		servicePods = append(servicePods, pod...)
	}

	logger.InfofCtx(ctx, "Successfully collected %d service pods", len(servicePods))

	return servicePods, nil
}

func (s *ApplicationServiceBase) collectComponentPods(
	ctx context.Context,
	rt runtime.Runtime,
	services []models.Service,
) ([]types.Pod, error) {
	componentMap := make(map[string][]types.Pod)

	for _, service := range services {
		serviceDependencies, err := s.ServiceDependencyRepo.GetDependenciesByServiceID(ctx, service.ID)
		if err != nil {
			logger.ErrorfCtx(ctx, "Failed to get dependencies for service %s: %v", service.ID, err)

			continue
		}

		for _, dependency := range serviceDependencies {
			if dependency.DependencyType != models.DependencyTypeComponent {
				continue
			}

			componentID := dependency.DependencyID.String()

			if _, exists := componentMap[componentID]; exists {
				continue
			}

			componentPod, err := loadApplicationPods(rt, componentID)
			if err != nil {
				logger.ErrorfCtx(ctx, "Failed to load component pod: %v", err)

				continue
			}

			componentMap[componentID] = componentPod
		}
	}

	componentPods := make([]types.Pod, 0, len(componentMap))
	for _, podDetails := range componentMap {
		componentPods = append(componentPods, podDetails...)
	}

	logger.InfofCtx(ctx, "Successfully collected %d unique component pods", len(componentPods))

	return componentPods, nil
}

func loadApplicationPods(rt runtime.Runtime, appID string) ([]types.Pod, error) {
	filteredPod, err := common.FetchFilteredPods(rt, appID)
	if err != nil {
		return nil, err
	}
	if len(filteredPod) == 0 {
		return nil, fmt.Errorf("no pod found with given id")
	}

	appPodList := make([]types.Pod, 0, len(filteredPod))

	for _, pod := range filteredPod {
		processedPod, err := common.ProcessPod(rt, pod)
		if err != nil {
			return nil, fmt.Errorf("failed to process pod: %w", err)
		}
		// ProcessPod returns (nil, nil) when InspectPod fails, so we should skip that pod
		if processedPod == nil {
			continue
		}

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

	return appPodList, nil
}

// Made with Bob
