package applicationservice

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
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	clitemplates "github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	consts "github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// DatasourceConnector is the minimal interface used by ApplicationServiceBase for
// post-deploy connector attachment. It exposes only ConnectDatasourcesToApplication,
// which is the single method called after a successful deployment. This narrow interface
// breaks the import cycle between the application_service and repository packages.
type DatasourceConnector interface {
	ConnectDatasourcesToApplication(ctx context.Context, applicationID uuid.UUID, datasourceIDs []uuid.UUID) (*apimodels.ConnectDatasourcesResponse, error)
}

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

// resourceTotals holds aggregated resource information.
type resourceTotals struct {
	allocatedCPU    int
	allocatedMemory int
	usedCPU         float64
	usedMemory      uint64
	spyreCards      map[string]bool
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
	DeletionExecutor      *deletion.DeletionExecutor
	Validator             *validators.ApplicationValidator

	// RuntimeType is the runtime this service instance targets (Podman or OpenShift).
	// Set once at construction; used by CreateApplication to plan and execute deployments.
	// TODO: Remove once the existing/legacy flow is retired — in the new flow every
	// application has a WorkerID and CreateApplication will derive the runtime from the
	// worker's registered runtime type instead of this field.
	RuntimeType runtimeTypes.RuntimeType

	// DeploymentRegistry tracks in-flight deployments so they can be cancelled
	// by a concurrent delete request. Nil means no cancellation (e.g. OpenShift stub).
	DeploymentRegistry *DeploymentRegistry

	// DatasourceService is optional. When set, connector refs supplied in the create
	// request are propagated to eligible Digitize services after the application reaches
	// Running status. When nil, connector attachment is skipped (e.g. test environments
	// where no datasource service is configured).
	DatasourceService DatasourceConnector

	// WorkerRegistry is used to resolve a remote runtime for worker-hosted applications.
	WorkerRegistry stream.WorkerRegistry
}

// createRuntime returns the runtime.Runtime appropriate for app.
// When WorkerID is set (new flow) it resolves the worker name and builds a RemoteRuntime
// over the gRPC CommandStream — this covers both the "Local" worker and actual remote workers.
// When WorkerID is nil (existing/legacy flow, pre-worker feature) it falls back to
// vars.RuntimeFactory directly, deriving the namespace from app.ID when needed (OpenShift).
func (s *ApplicationServiceBase) createRuntime(ctx context.Context, app *models.Application) (runtime.Runtime, error) {
	if app.WorkerID != nil {
		if s.WorkerRegistry == nil {
			return nil, fmt.Errorf("worker deployment not configured on this server")
		}

		workerName, ok := s.WorkerRegistry.WorkerNameByID(*app.WorkerID)
		if !ok {
			return nil, fmt.Errorf("worker %s for application %s is not connected", app.WorkerID, app.ID)
		}

		rtStr, _ := s.WorkerRegistry.WorkerRuntimeType(workerName)
		rt, err := runtime.NewRuntimeFactory(runtimeTypes.RuntimeType(rtStr)).CreateRemote(workerName, s.WorkerRegistry)
		if err != nil {
			return nil, fmt.Errorf("create remote runtime for worker %q: %w", workerName, err)
		}

		return rt, nil
	}

	return vars.RuntimeFactory.Create(catalogutils.AppNamespace(app.ID))
}

// buildWorkerInfo resolves the worker name and runtime type from the registry and returns
// an ApplicationWorker for embedding in the application response. The ID is always set;
// name and runtime_type are set only when the worker is currently connected.
func (s *ApplicationServiceBase) buildWorkerInfo(workerID uuid.UUID) *types.ApplicationWorker {
	w := &types.ApplicationWorker{ID: workerID.String()}
	if s.WorkerRegistry == nil {
		return w
	}
	name, ok := s.WorkerRegistry.WorkerNameByID(workerID)
	if !ok {
		return w
	}

	w.Name = name
	if rtStr, ok := s.WorkerRegistry.WorkerRuntimeType(name); ok {
		w.RuntimeType = rtStr
	}

	return w
}

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

	if app.WorkerID != nil {
		appData.Worker = s.buildWorkerInfo(*app.WorkerID)
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

	if app.WorkerID != nil {
		appresponse.Worker = s.buildWorkerInfo(*app.WorkerID)
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
	schema, err := s.Provider.GetComponentProviderParams(ctx, componentType, providerID, string(vars.RuntimeFactory.GetRuntimeType()))
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

	// Attach the worker FK. In the new flow WorkerName is always set by PlanDeployment
	// (including "Local" for same-machine deployments). The nil check handles the
	// existing flow where WorkerName is empty and there is no worker row to look up.
	// TODO: Remove the empty-string guard once the existing flow is retired.
	if plan.WorkerName != "" {
		if dbID, ok := s.DeploymentPlanner.WorkerDBID(plan.WorkerName); ok {
			app.WorkerID = &dbID
		}
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
func (s *ApplicationServiceBase) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
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
	plan, err := s.DeploymentPlanner.PlanDeployment(ctx, req, s.RuntimeType.String())
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment plan: %w", err)
	}

	// Phase 4: persist DB records
	if err := s.InsertDeploymentRecords(ctx, plan, req.CreatedBy); err != nil {
		return nil, fmt.Errorf("failed to insert deployment records: %w", err)
	}

	// Phase 5: async deployment.
	// Build the deployment context here, before launching the goroutine, so that
	// Register is called synchronously. This closes the race where a concurrent
	// DeleteApplication could call Cancel before the goroutine has had a chance
	// to call Register, causing cancellation to be silently missed.
	deployCtx := context.Background()
	if id, ok := ctx.Value(logger.RequestIDKey).(string); ok && id != "" {
		deployCtx = context.WithValue(deployCtx, logger.RequestIDKey, id)
	}

	if s.DeploymentRegistry != nil {
		deployCtx = s.DeploymentRegistry.Register(deployCtx, plan.ApplicationID)
	}

	go s.executeDeploymentAsync(deployCtx, plan, req)

	return &apimodels.CreateApplicationResponse{ID: plan.ApplicationID.String()}, nil
}

// executeDeploymentAsync runs the deployment in a background goroutine for the given runtime type.
// deployCtx is already derived and registered with the DeploymentRegistry by the caller.
func (s *ApplicationServiceBase) executeDeploymentAsync(deployCtx context.Context, plan *deployment.DeploymentPlan, req apimodels.CreateApplicationRequest) {
	runtimeType := s.RuntimeType
	ctx := deployCtx

	// Deregister on any exit path — success, error, or panic.
	if s.DeploymentRegistry != nil {
		defer s.DeploymentRegistry.Deregister(plan.ApplicationID)
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
		// Context cancelled — deletion is in charge of status, exit silently.
		if ctx.Err() != nil {
			logger.InfofCtx(ctx, "Deployment cancelled for application %s (deletion in progress)", plan.ApplicationName)

			return
		}

		logger.ErrorfCtx(ctx, "Deployment failed for application %s: %v", plan.ApplicationName, err)

		if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, plan.ApplicationID.String(), models.ApplicationStatusError, err.Error()); updateErr != nil {
			logger.ErrorfCtx(ctx, "Failed to update application status to Error: %v", updateErr)
		}

		return
	}

	logger.InfolnCtx(ctx, fmt.Sprintf("Deployment completed successfully for application %s", plan.ApplicationName))

	// Post-deploy connector attachment: once the application is Running, connect any
	// datasource connectors that were specified in the create request.
	// Failures here do NOT revert the Running status — the deployment succeeded; only
	// the connector attachment is partial. Errors are logged for operator visibility.
	s.attachConnectorsPostDeploy(ctx, plan.ApplicationID, req.Services)
}

// attachConnectorsPostDeploy calls ConnectDatasourcesToApplication with all unique connector
// UUIDs found in the create request's service connector refs. It is a best-effort operation:
// failures are logged but do not affect the application's Running status. No-op when
// DatasourceService is nil or when no service in the request carries connector refs.
func (s *ApplicationServiceBase) attachConnectorsPostDeploy(ctx context.Context, applicationID uuid.UUID, services []apimodels.Service) {
	if s.DatasourceService == nil {
		return
	}

	datasourceIDs := collectDatasourceIDs(ctx, services)
	if len(datasourceIDs) == 0 {
		return
	}

	resp, err := s.DatasourceService.ConnectDatasourcesToApplication(ctx, applicationID, datasourceIDs)
	if err != nil {
		logger.ErrorfCtx(ctx, "post-deploy connector attachment failed for application %s: %v", applicationID, err)

		return
	}

	logConnectErrors(ctx, applicationID, resp)
}

// collectDatasourceIDs returns a deduplicated slice of connector UUIDs from all service
// connector refs in the create request. The same connector ID may appear across multiple
// services; ConnectDatasourcesToApplication resolves eligible services internally, so each
// unique ID needs to be passed only once.
func collectDatasourceIDs(ctx context.Context, services []apimodels.Service) []uuid.UUID {
	seen := make(map[uuid.UUID]bool)
	var ids []uuid.UUID

	for _, svc := range services {
		for _, ref := range svc.Connectors {
			id, err := uuid.Parse(ref.ID)
			if err != nil {
				// Should not happen — ValidateConnectorRefs already validated UUIDs.
				logger.WarningfCtx(ctx, "skipping unparseable connector ref ID %q: %v", ref.ID, err)

				continue
			}

			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}

	return ids
}

// logConnectErrors logs any per-datasource failures returned by ConnectDatasourcesToApplication.
func logConnectErrors(ctx context.Context, applicationID uuid.UUID, resp *apimodels.ConnectDatasourcesResponse) {
	if resp == nil || len(resp.Errors) == 0 {
		return
	}

	for _, connErr := range resp.Errors {
		logger.WarningfCtx(ctx, "post-deploy connector %s failed for application %s: %s",
			connErr.DatasourceID, applicationID, connErr.Error)
	}
}

// GetApplicationResources retrieves CPU, memory, and Spyre-card usage for an application.
func (s *ApplicationServiceBase) GetApplicationResources(ctx context.Context, id uuid.UUID) (*types.ApplicationResourcesResponse, error) {
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

	runtimeClient, err := s.createRuntime(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime client: %w", err)
	}

	resourceTotals, err := s.collectResources(ctx, app, runtimeClient, s.Provider)
	if err != nil {
		return nil, fmt.Errorf("failed to collect application resources: %w", err)
	}

	return buildResourcesResponse(resourceTotals), nil
}

func (s *ApplicationServiceBase) collectResources(
	ctx context.Context,
	app *models.Application,
	runtimeClient runtime.Runtime,
	catalogProvider *catalog.CatalogProvider,
) (*resourceTotals, error) {
	totals := &resourceTotals{spyreCards: make(map[string]bool)}
	countedComponents := make(map[uuid.UUID]bool)

	for _, service := range app.Services {
		if err := s.processServiceResources(ctx, service, runtimeClient, catalogProvider, totals, countedComponents); err != nil {
			return nil, fmt.Errorf("failed to process service %s resources: %w", service.ID, err)
		}
	}

	return totals, nil
}

func (s *ApplicationServiceBase) processServiceResources(
	ctx context.Context,
	service models.Service,
	runtimeClient runtime.Runtime,
	catalogProvider *catalog.CatalogProvider,
	totals *resourceTotals,
	countedComponents map[uuid.UUID]bool,
) error {
	if err := s.addServiceResources(ctx, service, catalogProvider, runtimeClient, totals); err != nil {
		return fmt.Errorf("failed to get service allocated resources: %w", err)
	}

	if err := s.addComponentResources(ctx, service.ID, catalogProvider, runtimeClient, totals, countedComponents); err != nil {
		return fmt.Errorf("failed to get component allocated resources: %w", err)
	}

	return nil
}

func addAllocatedResources(runtimeMetadata *clitemplates.AppMetadata, totals *resourceTotals) {
	if runtimeMetadata.Resources != nil {
		totals.allocatedCPU += runtimeMetadata.Resources.CPU
		totals.allocatedMemory += runtimeMetadata.Resources.Memory
	}
}

func (s *ApplicationServiceBase) addServiceResources(
	ctx context.Context,
	service models.Service,
	catalogProvider *catalog.CatalogProvider,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
) error {
	runtimeMetadata, err := catalogProvider.LoadServiceRuntimeMetadata(service.CatalogID, string(vars.RuntimeFactory.GetRuntimeType()))
	if err != nil {
		return fmt.Errorf("failed to load service runtime metadata for catalog ID %s: %w", service.CatalogID, err)
	}

	addAllocatedResources(runtimeMetadata, totals)

	if err := addUsedResourcesByTemplateID(ctx, service.ID.String(), runtimeClient, totals); err != nil {
		return fmt.Errorf("failed to get service used resources: %w", err)
	}

	return nil
}

func (s *ApplicationServiceBase) addComponentResources(
	ctx context.Context,
	serviceID uuid.UUID,
	catalogProvider *catalog.CatalogProvider,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
	countedComponents map[uuid.UUID]bool,
) error {
	dependencies, err := s.ServiceDependencyRepo.GetDependenciesByServiceID(ctx, serviceID)
	if err != nil {
		return fmt.Errorf("failed to get dependencies for service %s: %w", serviceID, err)
	}

	for _, dep := range dependencies {
		if dep.DependencyType != models.DependencyTypeComponent || countedComponents[dep.DependencyID] {
			continue
		}

		if err := s.processComponentResources(ctx, dep.DependencyID, catalogProvider, runtimeClient, totals); err != nil {
			return err
		}

		countedComponents[dep.DependencyID] = true
	}

	return nil
}

func (s *ApplicationServiceBase) processComponentResources(
	ctx context.Context,
	componentID uuid.UUID,
	catalogProvider *catalog.CatalogProvider,
	runtimeClient runtime.Runtime,
	totals *resourceTotals,
) error {
	component, err := s.ComponentRepo.GetByID(ctx, componentID)
	if err != nil {
		return fmt.Errorf("failed to get component %s: %w", componentID, err)
	}

	runtimeMetadata, err := catalogProvider.LoadComponentRuntimeMetadata(component.Type, component.Provider, string(vars.RuntimeFactory.GetRuntimeType()))
	if err != nil {
		return fmt.Errorf("failed to load runtime metadata for component %s/%s: %w", component.Type, component.Provider, err)
	}

	addAllocatedResources(runtimeMetadata, totals)

	if err := addUsedResourcesByTemplateID(ctx, component.ID.String(), runtimeClient, totals); err != nil {
		return fmt.Errorf("failed to get component used resources for %s: %w", component.ID, err)
	}

	return nil
}

func addUsedResourcesByTemplateID(ctx context.Context, templateID string, runtimeClient runtime.Runtime, totals *resourceTotals) error {
	filters := map[string][]string{
		"label": {fmt.Sprintf("%s=%s", consts.ApplicationTemplateKey, templateID)},
	}

	pods, err := runtimeClient.ListPods(ctx, filters)
	if err != nil {
		return fmt.Errorf("failed to list pods for template %s: %w", templateID, err)
	}

	for _, pod := range pods {
		if err := collectPodResources(ctx, pod.Name, runtimeClient, totals); err != nil {
			return fmt.Errorf("failed to get used resources for pod %s: %w", pod.Name, err)
		}
	}

	return nil
}

func collectPodResources(ctx context.Context, podName string, runtimeClient runtime.Runtime, totals *resourceTotals) error {
	resources, err := runtimeClient.GetPodResources(ctx, podName)
	if err != nil {
		return fmt.Errorf("failed to get resources for pod %s: %w", podName, err)
	}

	for _, card := range resources.SpyreCards {
		totals.spyreCards[card] = true
	}

	totals.usedCPU += resources.CPU
	totals.usedMemory += resources.MemUsage

	return nil
}

func buildResourcesResponse(totals *resourceTotals) *types.ApplicationResourcesResponse {
	totalSpyreCards := make([]string, 0, len(totals.spyreCards))
	for card := range totals.spyreCards {
		totalSpyreCards = append(totalSpyreCards, card)
	}

	accelerators := make(map[string][]string)
	if len(totalSpyreCards) > 0 {
		accelerators[consts.SpyreResourceName] = totalSpyreCards
	}

	return &types.ApplicationResourcesResponse{
		CPU: types.ApplicationCPUInfo{
			Total: float64(totals.allocatedCPU),
			Used:  math.Round(totals.usedCPU*consts.PercentageDivisor) / consts.PercentageDivisor,
		},
		Memory: types.ApplicationMemInfo{
			TotalBytes: int64(totals.allocatedMemory),
			UsedBytes:  int64(totals.usedMemory),
		},
		Accelerators: accelerators,
	}
}

// ApplicationsPs returns runtime pod/container status for an application by querying the configured runtime.
func (s *ApplicationServiceBase) ApplicationsPs(ctx context.Context, appID uuid.UUID) (*types.ApplicationPSResponse, error) {
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

	rt, err := s.createRuntime(ctx, app)
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
		pod, err := loadApplicationPods(ctx, rt, service.ID.String())
		if err != nil {
			return nil, fmt.Errorf("failed to load service pod for service %s: %w", service.ID, err)
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
			return nil, fmt.Errorf("failed to get dependencies for service %s: %w", service.ID, err)
		}

		for _, dependency := range serviceDependencies {
			if dependency.DependencyType != models.DependencyTypeComponent {
				continue
			}

			componentID := dependency.DependencyID.String()

			if _, exists := componentMap[componentID]; exists {
				continue
			}

			componentPod, err := loadApplicationPods(ctx, rt, componentID)
			if err != nil {
				return nil, fmt.Errorf("failed to load component pod %s: %w", componentID, err)
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

func loadApplicationPods(ctx context.Context, rt runtime.Runtime, appID string) ([]types.Pod, error) {
	filteredPod, err := common.FetchFilteredPods(ctx, rt, appID)
	if err != nil {
		return nil, err
	}
	if len(filteredPod) == 0 {
		return nil, fmt.Errorf("no pod found with given id")
	}

	appPodList := make([]types.Pod, 0, len(filteredPod))

	for _, pod := range filteredPod {
		processedPod, err := common.ProcessPod(ctx, rt, pod)
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
			Labels:     pod.Labels,
			Containers: containers,
		}

		appPodList = append(appPodList, appPod)
	}

	return appPodList, nil
}

// validateForDeletion fetches the application and validates that the requesting
// user owns it and it is not already being deleted. Returns the app model on success.
func (s *ApplicationServiceBase) validateForDeletion(ctx context.Context, id uuid.UUID, user string) (*models.Application, error) {
	app, err := s.AppRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return nil, &ValidationError{Code: http.StatusNotFound, Message: ErrMsgApplicationNotFound}
	}
	if app.CreatedBy != user {
		return nil, &ValidationError{Code: http.StatusForbidden, Message: ErrMsgUserNotOwner}
	}
	if app.Status == models.ApplicationStatusDeleting {
		return nil, &ValidationError{Code: http.StatusConflict, Message: ErrMsgApplicationAlreadyDeleting}
	}

	return app, nil
}

func (s *ApplicationServiceBase) DeleteApplication(ctx context.Context, id uuid.UUID, user string, keepData bool) (*DeleteApplicationResponse, error) {
	app, err := s.validateForDeletion(ctx, id, user)
	if err != nil {
		return nil, err
	}

	// Resolve the runtime before cancelling or launching the goroutine so that
	// worker connectivity errors surface synchronously to the HTTP caller.
	rt, err := s.createRuntime(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve runtime for deletion: %w", err)
	}

	// Cancel any in-flight deployment before transitioning to Deleting.
	// No-op when DeploymentRegistry is nil (e.g. OpenShift stub).
	if s.DeploymentRegistry != nil {
		s.DeploymentRegistry.Cancel(id)
	}

	if err := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, id, models.ApplicationStatusDeleting, "Deleting deployment..."); err != nil {
		return nil, err
	}

	orphanedComponentIDs, err := s.identifyOrphanedComponents(ctx, id, app.Services)
	if err != nil {
		return nil, fmt.Errorf("failed to get application components: %w", err)
	}

	var requestID string
	if reqID, ok := ctx.Value(logger.RequestIDKey).(string); ok {
		requestID = reqID
	}

	deletionCtx := context.Background()
	if requestID != "" {
		deletionCtx = context.WithValue(deletionCtx, logger.RequestIDKey, requestID)
	}

	go s.executeDeletionAsync(deletionCtx, id, app.Services, orphanedComponentIDs, keepData, rt)

	return &DeleteApplicationResponse{
		ID:      id.String(),
		Status:  string(models.ApplicationStatusDeleting),
		Message: "Deletion initiated successfully",
	}, nil
}

func (s *ApplicationServiceBase) executeDeletionAsync(
	parentCtx context.Context,
	appID uuid.UUID,
	services []models.Service,
	orphanedComponentIDs []uuid.UUID,
	keepData bool,
	rt runtime.Runtime,
) {
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
			logger.ErrorfCtx(ctx, "Panic recovered in deletion goroutine for application %s: %v", appID, r)

			errMsg := fmt.Sprintf("Deletion panic: %v", r)
			if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, appID.String(), models.ApplicationStatusError, errMsg); updateErr != nil {
				logger.ErrorfCtx(ctx, "Failed to update application status after panic: %v", updateErr)
			}
		}
	}()

	err := s.DeletionExecutor.Execute(ctx, appID, services, orphanedComponentIDs, keepData, rt)
	if err != nil {
		logger.ErrorfCtx(ctx, "Deletion failed for application %s: %v", appID.String(), err)

		if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, appID.String(), models.ApplicationStatusError, err.Error()); updateErr != nil {
			logger.ErrorfCtx(ctx, "Failed to update application status to Error: %v", updateErr)
		}

		return
	}

	logger.InfolnCtx(ctx, fmt.Sprintf("Deletion completed successfully for application id '%s'", appID.String()))
}

// identifyOrphanedComponents identifies components that will become orphaned after service deletion.
func (s *ApplicationServiceBase) identifyOrphanedComponents(ctx context.Context, appID uuid.UUID, services []models.Service) ([]uuid.UUID, error) {
	serviceIDs := s.buildServiceIDMap(services)

	componentCandidates, err := s.collectComponentCandidates(ctx, appID, services)
	if err != nil {
		return nil, err
	}

	return s.filterOrphanedComponents(ctx, componentCandidates, serviceIDs), nil
}

// buildServiceIDMap creates a map of service IDs for quick lookup.
func (s *ApplicationServiceBase) buildServiceIDMap(services []models.Service) map[uuid.UUID]bool {
	serviceIDs := make(map[uuid.UUID]bool, len(services))
	for _, svc := range services {
		serviceIDs[svc.ID] = true
	}

	return serviceIDs
}

func (s *ApplicationServiceBase) collectComponentCandidates(ctx context.Context, appID uuid.UUID, services []models.Service) (map[uuid.UUID]bool, error) {
	componentCandidates := make(map[uuid.UUID]bool)

	for _, svc := range services {
		deps, err := s.ServiceDependencyRepo.GetDependenciesByServiceID(ctx, svc.ID)
		if err != nil {
			logger.ErrorfCtx(ctx, "failed to get dependencies for service %s: %s", svc.ID, err)
			_ = catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, appID, models.ApplicationStatusError, "failed to get service dependencies")

			return nil, err
		}

		for _, dep := range deps {
			if dep.DependencyType == models.DependencyTypeComponent {
				componentCandidates[dep.DependencyID] = true
			}
		}
	}

	return componentCandidates, nil
}

// filterOrphanedComponents checks which components are truly orphaned.
func (s *ApplicationServiceBase) filterOrphanedComponents(ctx context.Context, componentCandidates map[uuid.UUID]bool, serviceIDs map[uuid.UUID]bool) []uuid.UUID {
	var orphanedComponents []uuid.UUID

	for componentID := range componentCandidates {
		if s.isComponentOrphaned(ctx, componentID, serviceIDs) {
			orphanedComponents = append(orphanedComponents, componentID)
		}
	}

	return orphanedComponents
}

// isComponentOrphaned checks if a component has no remaining dependent services.
func (s *ApplicationServiceBase) isComponentOrphaned(ctx context.Context, componentID uuid.UUID, serviceIDs map[uuid.UUID]bool) bool {
	dependentServices, err := s.ServiceDependencyRepo.GetServicesByDependency(ctx, componentID, models.DependencyTypeComponent)
	if err != nil {
		logger.ErrorfCtx(ctx, "failed to check component %s orphan status: %s", componentID, err)

		return false
	}

	for _, svcID := range dependentServices {
		if !serviceIDs[svcID] {
			return false
		}
	}

	return true
}

// Made with Bob
