package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// ApplicationService provides business logic for application operations.
type ApplicationService struct {
	appRepo       dbrepo.ApplicationRepository
	serviceRepo   dbrepo.ServiceRepository
	depRepo       dbrepo.ServiceDependencyRepository
	componentRepo dbrepo.ComponentRepository
	provider      *catalog.CatalogProvider
}

// response type for response
type DeleteApplicationResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// NewApplicationService creates a new application service.
func NewApplicationService(
	appRepo dbrepo.ApplicationRepository,
	serviceRepo dbrepo.ServiceRepository,
	depRepo dbrepo.ServiceDependencyRepository,
	componentRepo dbrepo.ComponentRepository,
	provider *catalog.CatalogProvider,
) *ApplicationService {
	return &ApplicationService{
		appRepo:       appRepo,
		serviceRepo:   serviceRepo,
		depRepo:       depRepo,
		componentRepo: componentRepo,
		provider:      provider,
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

// CreateApplication creates a new application with the given configuration.
func (s *ApplicationService) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	// to be implemented
	return nil, nil
}

// DeleteApplication initiates async deletion of an application.
func (s *ApplicationService) DeleteApplication(ctx context.Context, id uuid.UUID, user string, skipCleanup bool) (*DeleteApplicationResponse, error) {
	app, err := s.appRepo.GetByID(ctx, id)

	if err != nil {
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

	for _, svc := range app.Services {
		if err := s.serviceRepo.UpdateStatus(ctx, svc.ID, models.ApplicationStatusDeleting); err != nil {
			return nil, fmt.Errorf("failed to set service %s to deleting: %w", svc.ID, err)
		}
	}

	go s.performDeletion(context.Background(), id, app.Services, skipCleanup)

	return &DeleteApplicationResponse{
		ID:      id.String(),
		Status:  string(models.ApplicationStatusDeleting),
		Message: "Deletion initiated successfully",
	}, nil
}

// performDeletion carries out the async cascade deletion for an application
// collect component IDs referenced by this app's services
// identify which components become orphaned
// delete the application CASCADE removes services + service_dependencies
// delete orphaned components
// TODO: teardown pods/containers once Create flow is ready skipCleanup flag
func (s *ApplicationService) performDeletion(ctx context.Context, appID uuid.UUID, services []models.Service, skipCleanup bool) {
	// Step 1: collect component IDs and service IDs being deleted
	serviceIDs := make(map[uuid.UUID]bool, len(services))
	componentCandidates := make(map[uuid.UUID]bool)

	for _, svc := range services {
		serviceIDs[svc.ID] = true

		deps, err := s.depRepo.GetDependenciesByServiceID(ctx, svc.ID)
		if err != nil {
			s.appRepo.UpdateStatus(ctx, appID, models.ApplicationStatusError,
				fmt.Sprintf("failed to get dependencies for service %s: %s", svc.ID, err))
			return
		}

		for _, dep := range deps {
			if dep.DependencyType == models.DependencyTypeComponent {
				componentCandidates[dep.DependencyID] = true
			}
		}
	}

	var orphanedComponents []uuid.UUID

	for componentID := range componentCandidates {
		consumers, err := s.depRepo.GetServicesByDependency(ctx, componentID, models.DependencyTypeComponent)
		if err != nil {
			s.appRepo.UpdateStatus(ctx, appID, models.ApplicationStatusError,
				fmt.Sprintf("failed to check consumers of component %s: %s", componentID, err))
			return
		}

		onlyUsedByThisApp := true
		for _, consumerID := range consumers {
			if !serviceIDs[consumerID] {
				onlyUsedByThisApp = false
				break
			}
		}

		if onlyUsedByThisApp {
			orphanedComponents = append(orphanedComponents, componentID)
		}
	}

	if err := s.appRepo.Delete(ctx, appID); err != nil {
		s.appRepo.UpdateStatus(ctx, appID, models.ApplicationStatusError,
			fmt.Sprintf("failed to delete application: %s", err))
		return
	}

	for _, componentID := range orphanedComponents {
		if err := s.componentRepo.Delete(ctx, componentID); err != nil {
			fmt.Printf("warning: failed to delete orphaned component %s: %s\n", componentID, err)
		}
	}

	// TODO: teardown pods/containers, skipCleanup flag
	// Deferred until Create Application flow is ready
	// if !skipCleanup {
	// 	 s.teardownPods(ctx, appID, services)
	// }
}
