package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

var (
	ErrApplicationNotFound = errors.New("application not found")
	ErrUnauthorized        = errors.New("user not authorized")
)

// ApplicationService provides business logic for application operations.
type ApplicationService struct {
	appRepo               dbrepo.ApplicationRepository
	serviceDependencyRepo dbrepo.ServiceDependencyRepository
	componentRepo         dbrepo.ComponentRepository
	provider              *catalog.CatalogProvider
}

// NewApplicationService creates a new application service.
func NewApplicationService(appRepo dbrepo.ApplicationRepository, serviceDependencyRepo dbrepo.ServiceDependencyRepository, componentRepo dbrepo.ComponentRepository, provider *catalog.CatalogProvider) *ApplicationService {
	return &ApplicationService{
		appRepo:               appRepo,
		serviceDependencyRepo: serviceDependencyRepo,
		componentRepo:         componentRepo,
		provider:              provider,
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
func (s *ApplicationService) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	// to be implemented
	return nil, nil
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
