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
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
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
	if err := s.insertDeploymentRecords(ctx, plan); err != nil {
		return nil, fmt.Errorf("failed to insert deployment records: %w", err)
	}

	// Phase 4: Spawn goroutine for async deployment execution with panic recovery
	go s.executeDeploymentAsync(plan, req)

	// Phase 5: Return 202 Accepted immediately with application ID
	response := &apimodels.CreateApplicationResponse{
		ID:      plan.ApplicationID.String(),
		Status:  string(models.ApplicationStatusDeploying),
		Message: "Application deployment initiated",
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
			if updateErr := s.updateApplicationStatus(ctx, plan.ApplicationID.String(), models.ApplicationStatusError, errMsg); updateErr != nil {
				log.Printf("Failed to update application status after panic: %v", updateErr)
			}
		}
	}()

	// Create a new context for the async operation (not tied to the HTTP request context)
	ctx := context.Background()

	// Determine runtime type (currently only Podman is supported)
	runtimeType := runtimeTypes.RuntimeTypePodman

	// Execute deployment using the existing plan (don't recreate it)
	err := s.deploymentExecutor.ExecuteWithPlan(ctx, plan, req, runtimeType)
	if err != nil {
		log.Printf("Deployment failed for application %s: %v", plan.ApplicationName, err)

		// Update application status to Error
		if updateErr := s.updateApplicationStatus(ctx, plan.ApplicationID.String(), models.ApplicationStatusError, err.Error()); updateErr != nil {
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
) error {
	// 1. Insert application record
	app := &models.Application{
		ID:             plan.ApplicationID,
		Name:           plan.ApplicationName,
		CatalogID:      plan.CatalogID,
		DeploymentType: s.getDeploymentType(plan.IsArchitecture),
		Status:         models.ApplicationStatusDownloading,
		Message:        "Deployment in progress",
		CreatedBy:      "admin", // TODO: Get from auth context
	}

	if err := s.appRepo.Insert(ctx, app); err != nil {
		return fmt.Errorf("failed to insert application: %w", err)
	}

	// 2. Insert component records (all components are new)
	componentIDMap := make(map[string]uuid.UUID) // hash -> UUID
	for hash, comp := range plan.Components {
		// Generate new UUID for component
		instanceUUID := uuid.New()

		// Insert component into database
		component := &models.Component{
			ID:       instanceUUID,
			Type:     comp.ComponentType,
			Provider: comp.ProviderID,
			Metadata: comp.Params,
			// Endpoints will be populated during deployment
		}

		if err := s.componentRepo.Insert(ctx, component); err != nil {
			return fmt.Errorf("failed to insert component %s: %w", hash, err)
		}
		componentIDMap[hash] = instanceUUID

		// Store the generated component ID back in the plan for endpoint updates
		comp.DatabaseID = instanceUUID
	}

	// 3. Insert service records and their dependencies
	for serviceID, svc := range plan.Services {
		// Insert service record
		service := &models.Service{
			ID:        uuid.Nil, // Will be generated by Insert
			AppID:     plan.ApplicationID,
			CatalogID: svc.CatalogID,
			Status:    models.ApplicationStatusDownloading,
			Version:   svc.Version,
			// Endpoints will be populated during deployment
		}

		if err := s.serviceRepo.Insert(ctx, service); err != nil {
			return fmt.Errorf("failed to insert service %s: %w", serviceID, err)
		}

		// Store the generated service ID back in the plan for status updates
		svc.DatabaseID = service.ID

		// 4. Insert service dependencies (links to components)
		for _, compHash := range svc.ComponentRefs {
			componentID, exists := componentIDMap[compHash]
			if !exists {
				return fmt.Errorf("component hash %s not found in component map", compHash)
			}

			dependency := &models.ServiceDependency{
				ServiceID:      service.ID,
				DependencyID:   componentID,
				DependencyType: models.DependencyTypeComponent,
			}

			if err := s.serviceDependencyRepo.AddDependency(ctx, dependency); err != nil {
				return fmt.Errorf("failed to add service dependency: %w", err)
			}
		}
	}

	return nil
}

// getDeploymentType determines the deployment type based on whether it's an architecture.
func (s *ApplicationService) getDeploymentType(isArchitecture bool) models.DeploymentType {
	if isArchitecture {
		return models.DeploymentTypeArchitectures
	}

	return models.DeploymentTypeServices
}

// updateApplicationStatus updates the status and message of an application.
func (s *ApplicationService) updateApplicationStatus(ctx context.Context, appID string, status models.ApplicationStatus, message string) error {
	// Parse the application ID
	appUUID, err := uuid.Parse(appID)
	if err != nil {
		log.Printf("Failed to parse application ID %s: %v", appID, err)
		return fmt.Errorf("invalid application ID: %w", err)
	}

	// Update the application status in the database
	if err := s.appRepo.UpdateStatus(ctx, appUUID, status, message); err != nil {
		log.Printf("Failed to update application %s status in database: %v", appID, err)
		return fmt.Errorf("failed to update application status: %w", err)
	}

	log.Printf("Application %s status updated: %s - %s", appID, status, message)
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

// Made with Bob
