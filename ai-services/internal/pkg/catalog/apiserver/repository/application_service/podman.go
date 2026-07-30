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
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	clitemplates "github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	consts "github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// PodmanApplicationService implements ApplicationServiceInterface for the Podman runtime.
// It embeds ApplicationServiceBase for all shared DB operations and adds Podman-specific
// deployment, pod-status, and resource-inspection logic.
type PodmanApplicationService struct {
	ApplicationServiceBase
}

// ListApplications retrieves a paginated list of applications with filters.
func (s *PodmanApplicationService) ListApplications(ctx context.Context, req ListApplicationsRequest) (*types.ApplicationListResponse, error) {
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

	totalPages := (totalCount + req.PageSize - 1) / req.PageSize
	if totalPages == 0 {
		totalPages = 1
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

// DeleteApplication initiates async deletion of an application and returns immediately.
func (s *PodmanApplicationService) DeleteApplication(ctx context.Context, id uuid.UUID, user string, keepData bool) (*DeleteApplicationResponse, error) {
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

	if err := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, id, models.ApplicationStatusDeleting, "Deleting deployment..."); err != nil {
		return nil, err
	}

	var requestID string
	if reqID, ok := ctx.Value(logger.RequestIDKey).(string); ok {
		requestID = reqID
	}

	deletionCtx := context.Background()
	if requestID != "" {
		deletionCtx = context.WithValue(deletionCtx, logger.RequestIDKey, requestID)
	}

	go s.DeletionService.PerformDeletion(deletionCtx, id, app.Services, keepData)

	return &DeleteApplicationResponse{
		ID:      id.String(),
		Status:  string(models.ApplicationStatusDeleting),
		Message: "Deletion initiated successfully",
	}, nil
}

// CreateApplication validates, plans, persists, and asynchronously deploys a new application
// using the Podman runtime executor.
func (s *PodmanApplicationService) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
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
	plan, err := s.DeploymentPlanner.PlanDeployment(ctx, req, runtimeTypes.RuntimeTypePodman.String())
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment plan: %w", err)
	}

	// Phase 4: persist DB records
	if err := s.InsertDeploymentRecords(ctx, plan, req.CreatedBy); err != nil {
		return nil, fmt.Errorf("failed to insert deployment records: %w", err)
	}

	// Phase 5: async deployment
	go s.executeDeploymentAsync(ctx, plan, req)

	return &apimodels.CreateApplicationResponse{ID: plan.ApplicationID.String()}, nil
}

// executeDeploymentAsync runs the Podman deployment in a background goroutine.
func (s *PodmanApplicationService) executeDeploymentAsync(parentCtx context.Context, plan *deployment.DeploymentPlan, req apimodels.CreateApplicationRequest) {
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

	err := s.DeploymentExecutor.ExecuteWithPlan(ctx, plan, req, runtimeTypes.RuntimeTypePodman)
	if err != nil {
		logger.ErrorfCtx(ctx, "Deployment failed for application %s: %v", plan.ApplicationName, err)

		if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, plan.ApplicationID.String(), models.ApplicationStatusError, err.Error()); updateErr != nil {
			logger.ErrorfCtx(ctx, "Failed to update application status to Error: %v", updateErr)
		}

		return
	}

	logger.InfolnCtx(ctx, fmt.Sprintf("Deployment completed successfully for application %s", plan.ApplicationName))
}

// GetApplicationResources retrieves CPU, memory, and Spyre-card usage by querying Podman pods.
func (s *PodmanApplicationService) GetApplicationResources(ctx context.Context, id uuid.UUID) (*types.ApplicationResourcesResponse, error) {
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

	runtimeClient, err := vars.RuntimeFactory.Create("")
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime client: %w", err)
	}

	catalogProvider, err := catalog.NewCatalogProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog provider: %w", err)
	}

	resourceTotals, err := s.collectResources(ctx, app, runtimeClient, catalogProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to collect application resources: %w", err)
	}

	return buildResourcesResponse(resourceTotals), nil
}

// ApplicationsPs returns runtime pod/container status for an application by querying Podman.
func (s *PodmanApplicationService) ApplicationsPs(ctx context.Context, appID uuid.UUID) (*types.ApplicationPSResponse, error) {
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

	rt, err := vars.RuntimeFactory.Create("")
	if err != nil {
		return nil, fmt.Errorf("failed to init %s client: %w", rt.Type(), err)
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

// --- resource helpers (Podman-only) ---

// resourceTotals holds aggregated resource information.
type resourceTotals struct {
	allocatedCPU    int
	allocatedMemory int
	usedCPU         float64
	usedMemory      uint64
	spyreCards      map[string]bool
}

func (s *PodmanApplicationService) collectResources(
	ctx context.Context,
	app *models.Application,
	runtimeClient runtime.Runtime,
	catalogProvider *catalog.CatalogProvider,
) (*resourceTotals, error) {
	totals := &resourceTotals{spyreCards: make(map[string]bool)}
	countedComponents := make(map[uuid.UUID]bool)

	for _, service := range app.Services {
		if err := s.processServiceResources(ctx, app.Name, service, runtimeClient, catalogProvider, totals, countedComponents); err != nil {
			return nil, fmt.Errorf("failed to process service %s resources: %w", service.ID, err)
		}
	}

	return totals, nil
}

func (s *PodmanApplicationService) processServiceResources(
	ctx context.Context,
	appName string,
	service models.Service,
	runtimeClient runtime.Runtime,
	catalogProvider *catalog.CatalogProvider,
	totals *resourceTotals,
	countedComponents map[uuid.UUID]bool,
) error {
	if err := s.addServiceResources(service, catalogProvider, runtimeClient, totals); err != nil {
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

func (s *PodmanApplicationService) addServiceResources(
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

	if err := addUsedResourcesByTemplateID(service.ID.String(), runtimeClient, totals); err != nil {
		return fmt.Errorf("failed to get service used resources: %w", err)
	}

	return nil
}

func (s *PodmanApplicationService) addComponentResources(
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

func (s *PodmanApplicationService) processComponentResources(
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

	runtimeMetadata, err := catalogProvider.LoadComponentRuntimeMetadata(component.Type, component.Provider)
	if err != nil {
		return fmt.Errorf("failed to load runtime metadata for component %s/%s: %w", component.Type, component.Provider, err)
	}

	addAllocatedResources(runtimeMetadata, totals)

	if err := addUsedResourcesByTemplateID(component.ID.String(), runtimeClient, totals); err != nil {
		return fmt.Errorf("failed to get component used resources for %s: %w", component.ID, err)
	}

	return nil
}

func addUsedResourcesByTemplateID(templateID string, runtimeClient runtime.Runtime, totals *resourceTotals) error {
	filters := map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/template=%s", templateID)},
	}

	pods, err := runtimeClient.ListPods(filters)
	if err != nil {
		return fmt.Errorf("failed to list pods for template %s: %w", templateID, err)
	}

	for _, pod := range pods {
		if err := collectPodResources(pod.Name, runtimeClient, totals); err != nil {
			return fmt.Errorf("failed to get used resources for pod %s: %w", pod.Name, err)
		}
	}

	return nil
}

func collectPodResources(podName string, runtimeClient runtime.Runtime, totals *resourceTotals) error {
	resources, err := runtimeClient.GetPodResources(podName)
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
		accelerators["ibm.com/spyre_pf"] = totalSpyreCards
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

// --- pod-status helpers (Podman-only) ---

func (s *PodmanApplicationService) collectServicePods(
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

func (s *PodmanApplicationService) collectComponentPods(
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
