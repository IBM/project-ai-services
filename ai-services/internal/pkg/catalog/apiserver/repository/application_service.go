package repository

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	ErrApplicationNotFound = errors.New("application not found")
	ErrUnauthorized        = errors.New("user not authorized")
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
}

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
			ID:     svc.ID.String(),
			Type:   serviceDisplayName,
			Status: string(svc.Status),
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
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrApplicationNotFound
		}

		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app.CreatedBy != userID {
		return nil, ErrUnauthorized
	}
	err = s.appRepo.UpdateDeploymentName(ctx, id, newName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrApplicationNotFound
		}

		return nil, fmt.Errorf("failed to update application name: %w", err)
	}
	updatedApp, err := s.appRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated application %w", err)
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
	if err == nil && existingApp != nil {
		return nil, fmt.Errorf("application with name '%s' already exists", req.Name)
	}

	// Phase 2: Create deployment plan (synchronous - fail fast if invalid)
	// Use podman as default runtime type for planning
	plan, err := s.deploymentPlanner.PlanDeployment(ctx, req, runtimeTypes.RuntimeTypePodman.String())
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment plan: %w", err)
	}

	// Phase 3: Insert database records for application, services, components, and dependencies
	if err := s.insertDeploymentRecords(ctx, plan, req.CreatedBy); err != nil {
		return nil, fmt.Errorf("failed to insert deployment records: %w", err)
	}

	// Phase 4: Spawn goroutine for async deployment execution with panic recovery
	go s.executeDeploymentAsync(plan, req)

	// Phase 5: Return 202 Accepted immediately with application ID
	response := &apimodels.CreateApplicationResponse{
		ID: plan.ApplicationID.String(),
	}

	return response, nil
}

// executeDeploymentAsync executes the deployment in a background goroutine.
// It updates the application status in the database based on deployment outcome.
// Includes panic recovery to prevent crashes.
func (s *ApplicationService) executeDeploymentAsync(plan *deployment.DeploymentPlan, req apimodels.CreateApplicationRequest) {
	// Defer panic recovery
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic recovered in deployment goroutine for application %s: %v", plan.ApplicationName, r)

			// Attempt to update application status to Error
			ctx := context.Background()
			errMsg := fmt.Sprintf("Deployment panic: %v", r)
			if updateErr := utils.UpdateApplicationStatus(ctx, s.appRepo, plan.ApplicationID.String(), models.ApplicationStatusError, errMsg); updateErr != nil {
				log.Printf("Failed to update application status after panic: %v", updateErr)
			}
		}
	}()

	// Create a new context for the async operation (not tied to the HTTP request context)
	ctx := context.Background()

	// Determine runtime type (currently only Podman is supported)
	runtimeType := runtimeTypes.RuntimeTypePodman

	// Execute deployment using the existing plan
	err := s.deploymentExecutor.ExecuteWithPlan(ctx, plan, req, runtimeType)
	if err != nil {
		log.Printf("Deployment failed for application %s: %v", plan.ApplicationName, err)

		// Update application status to Error
		if updateErr := utils.UpdateApplicationStatus(ctx, s.appRepo, plan.ApplicationID.String(), models.ApplicationStatusError, err.Error()); updateErr != nil {
			log.Printf("Failed to update application status to Error: %v", updateErr)
		}

		return
	}

	log.Printf("Deployment completed successfully for application %s", plan.ApplicationName)
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

		component := &models.Component{
			ID:       instanceUUID,
			Type:     comp.ComponentType,
			Provider: comp.ProviderID,
			Status:   models.ComponentStatusInitializing,
			Version:  comp.Version,
			Metadata: comp.Params,
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
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrApplicationNotFound
		}

		return nil, fmt.Errorf("failed to get application: %w", err)
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
		DeploymentType: string(app.DeploymentType),
		Type:           typeName,
		Status:         string(app.Status),
		Message:        app.Message,
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
		appService := types.ApplicationService{
			ID:        service.ID.String(),
			Type:      service.CatalogID,
			Endpoints: service.Endpoints,
			Version:   service.Version,
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

			// Transform to response object
			temp := types.ServiceComponentResp{
				Type:     component.Type,
				Provider: component.Provider,
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
func (s *ApplicationService) DeleteApplication(ctx context.Context, id uuid.UUID, user string, force bool) (*DeleteApplicationResponse, error) {
	app, err := s.appRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("not found: application does not exist")
		}

		return nil, fmt.Errorf("not found: %w", err)
	}

	if app.CreatedBy != user {
		return nil, fmt.Errorf("forbidden: user does not own this application")
	}

	if app.Status == models.ApplicationStatusDeleting {
		return nil, fmt.Errorf("conflict: application is already being deleted")
	}

	if err := s.appRepo.UpdateStatus(ctx, id, models.ApplicationStatusDeleting, "Deletion initiated"); err != nil {
		return nil, err
	}

	// Service status update deferred until later
	go s.performDeletion(context.Background(), id, app.Services, force)

	return &DeleteApplicationResponse{
		ID:      id.String(),
		Status:  string(models.ApplicationStatusDeleting),
		Message: "Deletion initiated successfully",
	}, nil
}

// performDeletion carries out the async cascade deletion for an application.
// When force is true, orphaned component records are also deleted.
//
//nolint:cyclop,gocognit,nestif,funlen
func (s *ApplicationService) performDeletion(ctx context.Context, appID uuid.UUID, services []models.Service, force bool) {
	serviceIDs := make(map[uuid.UUID]bool, len(services))
	for _, svc := range services {
		serviceIDs[svc.ID] = true
	}

	// Identify orphaned components before deletion while service_dependencies still exist.
	var orphanedComponents []uuid.UUID

	if force {
		componentCandidates := make(map[uuid.UUID]bool)

		for _, svc := range services {
			deps, err := s.serviceDependencyRepo.GetDependenciesByServiceID(ctx, svc.ID)
			if err != nil {
				logger.Errorf("failed to get dependencies for service %s: %s", svc.ID, err)
				_ = s.appRepo.UpdateStatus(ctx, appID, models.ApplicationStatusError, "failed to get service dependencies")

				return
			}

			for _, dep := range deps {
				if dep.DependencyType == models.DependencyTypeComponent {
					componentCandidates[dep.DependencyID] = true
				}
			}
		}

		for componentID := range componentCandidates {
			dependentServices, err := s.serviceDependencyRepo.GetServicesByDependency(ctx, componentID, models.DependencyTypeComponent)
			if err != nil {
				logger.Errorf("failed to check component %s orphan status: %s", componentID, err)

				continue
			}

			isOrphan := true

			for _, svcID := range dependentServices {
				if !serviceIDs[svcID] {
					isOrphan = false

					break
				}
			}

			if isOrphan {
				orphanedComponents = append(orphanedComponents, componentID)
			}
		}
	}

	// Delete the application; CASCADE removes services and service_dependencies.
	if err := s.appRepo.Delete(ctx, appID); err != nil {
		logger.Errorf("failed to delete application %s: %s", appID, err)

		return
	}

	for _, componentID := range orphanedComponents {
		if err := s.componentRepo.Delete(ctx, componentID); err != nil {
			logger.Errorf("failed to delete orphaned component %s: %s", componentID, err)
		}
	}
}

// GetApplicationResources retrieves resource usage (CPU, memory) for an application using runtime-specific stats.
func (s *ApplicationService) GetApplicationResources(ctx context.Context, id uuid.UUID) (*types.ApplicationResourcesResponse, error) {
	// Fetch application from database
	app, err := s.appRepo.GetByID(ctx, id)
	if err != nil {
		return nil, handleGetApplicationError(err)
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
	resourceTotals := s.collectServiceResources(ctx, app, runtimeClient, catalogProvider)

	// Build and return response
	return buildResourcesResponse(resourceTotals), nil
}

// handleGetApplicationError handles errors from GetByID.
func handleGetApplicationError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrApplicationNotFound
	}

	return fmt.Errorf("failed to get application: %w", err)
}

// resourceTotals holds aggregated resource information.
type resourceTotals struct {
	allocatedCPU    int
	allocatedMemory int
	usedCPU         float64
	usedMemory      uint64
	spyreCards      map[string]bool
}

// collectServiceResources aggregates resources from all services in an application.
func (s *ApplicationService) collectServiceResources(
	ctx context.Context,
	app *models.Application,
	runtimeClient runtime.Runtime,
	catalogProvider *catalog.CatalogProvider,
) *resourceTotals {
	totals := &resourceTotals{
		spyreCards: make(map[string]bool),
	}

	// Track components we've already counted to avoid double-counting shared components
	countedComponents := make(map[uuid.UUID]bool)

	for _, service := range app.Services {
		s.processServiceResources(ctx, app.Name, service, runtimeClient, catalogProvider, totals, countedComponents)
	}

	return totals
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
) {
	// Load allocated resources from service metadata
	s.addServiceAllocatedResources(service.CatalogID, catalogProvider, totals)

	// Load allocated resources from deployed components for this service
	// Pass countedComponents to avoid double-counting shared components
	// Also collect used resources from component pods
	s.addComponentAllocatedResources(ctx, service.ID, catalogProvider, runtimeClient, totals, countedComponents)

	// Get all pods for this service using the template ID label
	// Each pod deployed has label: ai-services.io/template: "<service-database-id>"
	addUsedResourcesByTemplateID(service.ID.String(), runtimeClient, totals)
}

// addServiceAllocatedResources adds allocated resources from service metadata.
func (s *ApplicationService) addServiceAllocatedResources(
	catalogID string,
	catalogProvider *catalog.CatalogProvider,
	totals *resourceTotals,
) {
	runtimeMetadata, err := catalogProvider.LoadServiceRuntimeMetadata(catalogID)
	if err == nil && runtimeMetadata.Resources != nil {
		totals.allocatedCPU += runtimeMetadata.Resources.CPU
		totals.allocatedMemory += runtimeMetadata.Resources.Memory
	}
}

// addComponentAllocatedResources adds allocated resources from the actual deployed component providers.
// This ensures we only count resources for the specific component providers deployed for this service,
// not all possible provider options. Components are tracked to avoid double-counting when shared across services.
// Also collects used resources from component pods.
func (s *ApplicationService) addComponentAllocatedResources(
	ctx context.Context,
	serviceID uuid.UUID,
	catalogProvider *catalog.CatalogProvider,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
	countedComponents map[uuid.UUID]bool,
) {
	// Get service dependencies (components) from database
	dependencies, err := s.serviceDependencyRepo.GetDependenciesByServiceID(ctx, serviceID)
	if err != nil {
		logger.Warningf("Failed to get dependencies for service %s: %v\n", serviceID, err)

		return
	}

	// Process each component dependency
	for _, dep := range dependencies {
		if dep.DependencyType != models.DependencyTypeComponent {
			continue
		}

		// Skip if we've already counted this component (shared across services)
		if countedComponents[dep.DependencyID] {
			continue
		}

		// Get component details from database
		component, err := s.componentRepo.GetByID(ctx, dep.DependencyID)
		if err != nil {
			logger.Warningf("Failed to get component %s: %v\n", dep.DependencyID, err)

			continue
		}

		// Load component runtime metadata for the specific provider
		runtimeMetadata, err := catalogProvider.LoadComponentRuntimeMetadata(component.Type, component.Provider)
		if err != nil {
			logger.Warningf("Failed to load runtime metadata for component %s/%s: %v\n", component.Type, component.Provider, err)

			continue
		}

		// Add allocated resources from this specific component provider
		if runtimeMetadata.Resources != nil {
			totals.allocatedCPU += runtimeMetadata.Resources.CPU
			totals.allocatedMemory += runtimeMetadata.Resources.Memory
		}

		// Get all pods for this component using the template ID label
		// Each pod deployed has label: ai-services.io/template: "<component-database-id>"
		addUsedResourcesByTemplateID(component.ID.String(), runtimeClient, totals)

		// Mark this component as counted
		countedComponents[dep.DependencyID] = true
	}
}

// addUsedResources fetches and adds used resources from a single pod.
func addUsedResources(
	podName string,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
) {
	resources, err := runtimeClient.GetPodResources(podName)
	if err != nil {
		logger.Warningf("Failed to get resources for pod %s: %v\n", podName, err)

		return
	}

	// Track all unique Spyre cards
	for _, card := range resources.SpyreCards {
		totals.spyreCards[card] = true
	}

	// Accumulate used resources
	totals.usedCPU += resources.CPUCores
	totals.usedMemory += resources.MemUsage
}

// addUsedResourcesByTemplateID fetches and adds used resources from all pods with a given template ID label.
// This handles cases where a service or component has multiple pods (e.g., digitize has digitize-{slug} and digitize-db-{slug}).
func addUsedResourcesByTemplateID(
	templateID string,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
) {
	// List all pods with the template ID label
	filters := map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/template=%s", templateID)},
	}

	pods, err := runtimeClient.ListPods(filters)
	if err != nil {
		logger.Warningf("Failed to list pods for template %s: %v\n", templateID, err)

		return
	}

	// Aggregate resources from all pods
	for _, pod := range pods {
		addUsedResources(pod.Name, runtimeClient, totals)
	}
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
		accelerators["spyre"] = totalSpyreCards
	}

	// Build response with total and used resources
	return &types.ApplicationResourcesResponse{
		CPU: types.ApplicationCPUInfo{
			TotalCores: float64(totals.allocatedCPU),
			UsedCores:  totals.usedCPU,
		},
		Memory: types.ApplicationMemInfo{
			TotalBytes: int64(totals.allocatedMemory),
			UsedBytes:  int64(totals.usedMemory),
		},
		Accelerators: accelerators,
	}
}

// Made with Bob
