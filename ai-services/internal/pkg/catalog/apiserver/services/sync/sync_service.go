package sync

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	catalogpkg "github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	// DefaultSyncInterval is the default interval for syncing DB with pod status.
	DefaultSyncInterval = 30 * time.Second

	// Resource item types.
	resourceItemTypeService   = "service"
	resourceItemTypeComponent = "component"

	// Component catalogID format: "type/provider" has 2 parts.
	componentCatalogIDParts = 2

	// Error message templates.
	errMsgPodNotFound = "Pod not found or error: %v"
)

// ResourceCounts tracks expected resource counts and names from templates.
type ResourceCounts struct {
	Pods        int
	SecretNames []string // Names of secrets referenced in pod labels
	VolumeNames []string // Names of volumes referenced in pod labels
}

// SyncService handles periodic synchronization of DB records with actual pod status.
type SyncService struct {
	appRepo         dbrepo.ApplicationRepository
	serviceRepo     dbrepo.ServiceRepository
	componentRepo   dbrepo.ComponentRepository
	serviceDepsRepo dbrepo.ServiceDependencyRepository
	syncInterval    time.Duration
	stopChan        chan struct{}
	syncMutex       sync.Mutex // Prevents overlapping sync cycles
	isSyncing       bool       // Tracks if a sync is currently running
	runtimeSync     RuntimeSync // Runtime-specific sync backend
}

// newRuntimeSync constructs the appropriate RuntimeSync for the configured runtime type.
func newRuntimeSync(rt runtimeTypes.RuntimeType, catalogProvider *catalogpkg.CatalogProvider) (RuntimeSync, error) {
	switch rt {
	case runtimeTypes.RuntimeTypePodman:
		return newPodmanSync(catalogProvider), nil
	case runtimeTypes.RuntimeTypeOpenShift:
		return newOpenShiftSync(), nil
	default:
		return nil, fmt.Errorf("unsupported runtime type for sync: %s", rt)
	}
}

// NewSyncService creates a new sync service instance.
func NewSyncService(
	appRepo dbrepo.ApplicationRepository,
	serviceRepo dbrepo.ServiceRepository,
	componentRepo dbrepo.ComponentRepository,
	serviceDepsRepo dbrepo.ServiceDependencyRepository,
	syncInterval time.Duration,
) (*SyncService, error) {
	if syncInterval == 0 {
		syncInterval = DefaultSyncInterval
	}

	catalogProvider, err := catalogpkg.NewCatalogProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog provider for sync service: %w", err)
	}

	runtimeSync, err := newRuntimeSync(vars.RuntimeFactory.GetRuntimeType(), catalogProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create runtime sync: %w", err)
	}

	return &SyncService{
		appRepo:         appRepo,
		serviceRepo:     serviceRepo,
		componentRepo:   componentRepo,
		serviceDepsRepo: serviceDepsRepo,
		syncInterval:    syncInterval,
		stopChan:        make(chan struct{}),
		runtimeSync:     runtimeSync,
	}, nil
}

// Start begins the sync goroutine.
func (s *SyncService) Start(ctx context.Context) {
	go s.syncLoop(ctx)
	logger.InfolnCtx(ctx, "Sync service started")
}

// Stop gracefully stops the sync goroutine.
func (s *SyncService) Stop(ctx context.Context) {
	close(s.stopChan)
	logger.InfolnCtx(ctx, "Sync service stopped")
}

// syncLoop runs the periodic sync operation.
func (s *SyncService) syncLoop(ctx context.Context) {
	// Defer panic recovery
	defer func() {
		if r := recover(); r != nil {
			logger.ErrorfCtx(ctx, "Panic recovered in sync goroutine: %v", r)
		}
	}()

	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	// Run initial sync immediately
	s.performSync(ctx)

	for {
		select {
		case <-ticker.C:
			s.performSync(ctx)
		case <-s.stopChan:
			return
		}
	}
}

// performSync executes the synchronization logic.
func (s *SyncService) performSync(ctx context.Context) {
	// Check if a sync is already running
	s.syncMutex.Lock()
	if s.isSyncing {
		logger.DebuglnCtx(ctx, "Sync already in progress, skipping this cycle")
		s.syncMutex.Unlock()

		return
	}
	s.isSyncing = true
	s.syncMutex.Unlock()

	// Ensure we mark sync as complete when done
	defer func() {
		s.syncMutex.Lock()
		s.isSyncing = false
		s.syncMutex.Unlock()
	}()

	logger.DebuglnCtx(ctx, "Starting DB-Pod sync cycle")

	// Get all applications with Running or Error status
	filters := &dbrepo.ApplicationFilters{}
	applications, err := s.appRepo.GetAll(ctx, filters)
	if err != nil {
		logger.ErrorfCtx(ctx, "Failed to fetch applications for sync: %v", err)

		return
	}

	// Filter applications that need syncing (Running or Error state)
	for _, app := range applications {
		if app.Status == models.ApplicationStatusRunning || app.Status == models.ApplicationStatusError {
			if err := s.syncApplication(ctx, &app); err != nil {
				logger.ErrorfCtx(ctx, "Failed to sync application %s: %v", app.Name, err)
			}
		}
	}

	logger.DebuglnCtx(ctx, "Completed DB-Pod sync cycle")
}

// syncApplication syncs a single application using bottom-up approach:
// 1. Sync all components
// 2. Sync services
// 3. Update application status based on collected errors.
func (s *SyncService) syncApplication(ctx context.Context, app *models.Application) error {
	// Initialize runtime client
	rt, err := vars.RuntimeFactory.Create("")
	if err != nil {
		return fmt.Errorf("failed to create runtime client: %w", err)
	}

	logger.InfofCtx(ctx, "Syncing application: %s (ID: %s)", app.Name, app.ID)

	// Track errors during sync
	errorMessages := []string{}
	allHealthy := true

	// Step 1: Sync all components first (bottom of dependency tree)
	componentErrors, componentsPending := s.syncAllComponents(ctx, rt, app)
	if len(componentErrors) > 0 {
		errorMessages = append(errorMessages, componentErrors...)
		allHealthy = false
	}

	// Step 2: Sync services (middle of dependency tree)
	serviceErrors, servicesPending := s.syncAllServices(ctx, rt, app)
	if len(serviceErrors) > 0 {
		errorMessages = append(errorMessages, serviceErrors...)
		allHealthy = false
	}

	// Step 3: If any service or component is still initialising, skip updating the
	// application status this cycle — we don't have a complete picture yet.
	if componentsPending || servicesPending {
		logger.InfofCtx(ctx, "Skipping application %s status update: some services/components are still in Initializing state", app.Name)

		return nil
	}

	// Step 4: Update application status based on collected errors
	if err := s.updateApplicationStatus(ctx, app, allHealthy, errorMessages); err != nil {
		return fmt.Errorf("failed to update application status: %w", err)
	}

	logger.InfofCtx(ctx, "Completed sync for application: %s", app.Name)

	return nil
}

// syncAllComponents syncs all components for an application.
// Returns error messages and a pending flag — pending is true if any component was
// skipped because it has not yet reached a stable (Running/Error) state.
func (s *SyncService) syncAllComponents(ctx context.Context, rt runtime.Runtime, app *models.Application) ([]string, bool) { //nolint:cyclop
	processedComponents := make(map[uuid.UUID]bool)
	errorMessages := []string{}
	pending := false

	// Collect all unique components from all services
	for _, service := range app.Services {
		dependencies, err := s.serviceDepsRepo.GetDependenciesByServiceID(ctx, service.ID)
		if err != nil {
			logger.ErrorfCtx(ctx, "Failed to get dependencies for service %s: %v", service.ID, err)

			continue
		}

		for _, dep := range dependencies {
			if dep.DependencyType != models.DependencyTypeComponent {
				continue
			}

			// Skip if already processed
			if processedComponents[dep.DependencyID] {
				continue
			}

			// Only sync components that are in a stable, observable state
			component, err := s.componentRepo.GetByID(ctx, dep.DependencyID)
			if err != nil || component == nil {
				logger.ErrorfCtx(ctx, "Failed to get component %s for sync check: %v", dep.DependencyID, err)
				processedComponents[dep.DependencyID] = true

				continue
			}

			if component.Status != models.ComponentStatusRunning && component.Status != models.ComponentStatusError {
				logger.InfofCtx(ctx, "Skipping component %s sync: status is %s", dep.DependencyID, component.Status)
				processedComponents[dep.DependencyID] = true
				pending = true

				continue
			}

			// Sync component pod status, passing the already-fetched component to avoid a second DB call
			status, componentMsg, err := s.syncComponentPod(ctx, rt, app.ID.String(), component)
			if err != nil {
				logger.ErrorfCtx(ctx, "Failed to sync component %s: %v", dep.DependencyID, err)
			} else if status == models.ComponentStatusError && componentMsg != "" {
				// Collect error messages for application status
				errorMessages = append(errorMessages, componentMsg)
			}

			processedComponents[dep.DependencyID] = true
		}
	}

	return errorMessages, pending
}

// syncAllServices syncs all services for an application.
// Service status is determined ONLY by the service pod health, not component health.
// Returns error messages and a pending flag — pending is true if any service was
// skipped because it has not yet reached a stable (Running/Error) state.
func (s *SyncService) syncAllServices(ctx context.Context, rt runtime.Runtime, app *models.Application) ([]string, bool) {
	errorMessages := []string{}
	pending := false

	for _, service := range app.Services {
		// Only sync services that are in a stable, observable state
		if service.Status != models.ServiceStatusRunning && service.Status != models.ServiceStatusError {
			logger.InfofCtx(ctx, "Skipping service %s sync: status is %s", service.ID, service.Status)
			pending = true

			continue
		}

		serviceMsg, err := s.syncServicePod(ctx, rt, service)
		if err != nil {
			logger.ErrorfCtx(ctx, "Failed to sync service %s: %v", service.ID, err)
			// Continue with other services even if one fails
		}
		// Collect error messages for application status
		if serviceMsg != "" {
			errorMessages = append(errorMessages, fmt.Sprintf("Service %s: %s", service.CatalogID, serviceMsg))
		}
	}

	return errorMessages, pending
}

// syncServicePod syncs a single service's pod status
// Returns: error message (if any) and error.
func (s *SyncService) syncServicePod(ctx context.Context, rt runtime.Runtime, service models.Service) (string, error) {
	// Fetch all pods using service ID as template label
	pods, err := s.runtimeSync.FetchPodStatuses(rt, service.ID.String())
	if err != nil {
		return s.handleServicePodFetchError(ctx, service, err)
	}

	// Determine service status based on pods and resources.
	newStatus, message := s.determineServiceStatusFromPods(ctx, service.AppID.String(), service.CatalogID, service.AppID.String(), pods, rt)

	// Update service status if changed
	if err := s.updateServiceStatusIfChanged(ctx, service, newStatus, message); err != nil {
		return "", err
	}

	// Return error message if service is in error state
	if newStatus == models.ServiceStatusError {
		return message, nil
	}

	return "", nil
}

// handleServicePodFetchError handles the case when pods cannot be fetched for a service.
func (s *SyncService) handleServicePodFetchError(ctx context.Context, service models.Service, fetchErr error) (string, error) {
	newStatus := models.ServiceStatusError
	message := fmt.Sprintf(errMsgPodNotFound, fetchErr)

	if service.Status != newStatus {
		if err := catalogutils.UpdateServiceStatus(ctx, s.serviceRepo, service.ID, newStatus, message); err != nil {
			return "", fmt.Errorf("failed to update service status: %w", err)
		}
		logger.InfofCtx(ctx, "Updated service %s status to %s", service.ID, newStatus)
	}

	return message, nil
}

// determineServiceStatusFromPods determines service status based on pods and resource validation.
func (s *SyncService) determineServiceStatusFromPods(ctx context.Context, appID, catalogID, instanceID string, pods []*PodStatus, rt runtime.Runtime) (models.ServiceStatus, string) {
	resourceValidationMsg := s.runtimeSync.ValidateResources(ctx, ResourceValidationInput{
		AppID:          appID,
		CatalogID:      catalogID,
		InstanceID:     instanceID,
		ItemType:       resourceItemTypeService,
		ActualPodCount: len(pods),
	}, rt)

	// Check all pods - if any pod is unhealthy, service is in error
	newStatus := models.ServiceStatusRunning
	var errorMessages []string

	// Add resource validation error if present
	if resourceValidationMsg != "" {
		newStatus = models.ServiceStatusError
		errorMessages = append(errorMessages, resourceValidationMsg)
	}

	for _, pod := range pods {
		isHealthy, message := s.determinePodStatus(pod)
		if !isHealthy {
			newStatus = models.ServiceStatusError
			errorMessages = append(errorMessages, message)
		}
	}

	return newStatus, strings.Join(errorMessages, "; ")
}

// updateServiceStatusIfChanged updates service status only if it has changed.
func (s *SyncService) updateServiceStatusIfChanged(ctx context.Context, service models.Service, newStatus models.ServiceStatus, message string) error {
	if service.Status != newStatus {
		if err := catalogutils.UpdateServiceStatus(ctx, s.serviceRepo, service.ID, newStatus, message); err != nil {
			return fmt.Errorf("failed to update service status: %w", err)
		}
		logger.InfofCtx(ctx, "Updated service %s status to %s", service.ID, newStatus)
	}

	return nil
}

// syncComponentPod syncs a single component's pod status.
// The component must already be fetched by the caller to avoid a redundant DB lookup.
// Returns: status, error message (if any), and error.
func (s *SyncService) syncComponentPod(ctx context.Context, rt runtime.Runtime, appID string, component *models.Component) (models.ComponentStatus, string, error) {
	componentID := component.ID

	// Fetch all pods using component ID as template label
	pods, err := s.runtimeSync.FetchPodStatuses(rt, componentID.String())
	if err != nil {
		return s.handleComponentPodFetchError(ctx, component, componentID, err)
	}

	// Build component catalogID in format "type/provider"
	componentCatalogID := fmt.Sprintf("%s/%s", component.Type, component.Provider)

	resourceValidationMsg := s.runtimeSync.ValidateResources(ctx, ResourceValidationInput{
		AppID:          appID,
		CatalogID:      componentCatalogID,
		InstanceID:     componentID.String(),
		ItemType:       resourceItemTypeComponent,
		ActualPodCount: len(pods),
	}, rt)

	// Check all pods health
	newStatus, message := s.checkPodsHealth(pods)

	// Add resource validation error if present
	if resourceValidationMsg != "" {
		newStatus = models.ComponentStatusError
		if message != "" {
			message = fmt.Sprintf("%s; %s", resourceValidationMsg, message)
		} else {
			message = resourceValidationMsg
		}
	}

	// Update component status if changed
	if err := s.updateComponentStatusIfChanged(ctx, component, componentID, newStatus, message); err != nil {
		return newStatus, "", err
	}

	// Return formatted message for application status if component is in error
	if newStatus == models.ComponentStatusError {
		return newStatus, fmt.Sprintf("Component %s/%s: %s", component.Type, component.Provider, message), nil
	}

	return newStatus, "", nil
}

// handleComponentPodFetchError handles the case when pods cannot be fetched for a component.
func (s *SyncService) handleComponentPodFetchError(ctx context.Context, component *models.Component, componentID uuid.UUID, fetchErr error) (models.ComponentStatus, string, error) {
	newStatus := models.ComponentStatusError
	message := fmt.Sprintf(errMsgPodNotFound, fetchErr)

	if component.Status != newStatus {
		if err := catalogutils.UpdateComponentStatus(ctx, s.componentRepo, componentID, newStatus, message); err != nil {
			return newStatus, "", fmt.Errorf("failed to update component status: %w", err)
		}
		logger.InfofCtx(ctx, "Updated component %s status to %s", componentID, newStatus)
	}

	return newStatus, fmt.Sprintf("Component %s/%s: %s", component.Type, component.Provider, message), nil
}

// checkPodsHealth checks the health of all pods and returns the overall status.
func (s *SyncService) checkPodsHealth(pods []*PodStatus) (models.ComponentStatus, string) {
	newStatus := models.ComponentStatusRunning
	var errorMessages []string

	for _, pod := range pods {
		isHealthy, message := s.determinePodStatus(pod)
		if !isHealthy {
			newStatus = models.ComponentStatusError
			errorMessages = append(errorMessages, message)
		}
	}

	return newStatus, strings.Join(errorMessages, "; ")
}

// updateComponentStatusIfChanged updates component status only if it has changed.
func (s *SyncService) updateComponentStatusIfChanged(ctx context.Context, component *models.Component, componentID uuid.UUID, newStatus models.ComponentStatus, message string) error {
	if component.Status != newStatus {
		if err := catalogutils.UpdateComponentStatus(ctx, s.componentRepo, componentID, newStatus, message); err != nil {
			return fmt.Errorf("failed to update component status: %w", err)
		}
		logger.InfofCtx(ctx, "Updated component %s status to %s", componentID, newStatus)
	}

	return nil
}

// determinePodStatus determines status based on pod state and health
// Returns: isHealthy (bool), errorMessage (string).
func (s *SyncService) determinePodStatus(pod *PodStatus) (bool, string) {
	if pod.State == "Running" && pod.Health == string(constants.Ready) {
		return true, ""
	}

	if pod.State == "Running" && pod.Health == string(constants.NotReady) {
		return false, fmt.Sprintf("Pod %s is running but unhealthy", pod.PodName)
	}

	return false, fmt.Sprintf("Pod %s is in state: %s", pod.PodName, pod.State)
}

// updateApplicationStatus updates application status based on collected errors during sync
// This is much simpler since we already collected all errors during component and service sync.
func (s *SyncService) updateApplicationStatus(ctx context.Context, app *models.Application, allHealthy bool, errorMessages []string) error {
	var newStatus models.ApplicationStatus
	var message string

	if !allHealthy {
		// Application has errors
		newStatus = models.ApplicationStatusError
		if len(errorMessages) > 0 {
			message = strings.Join(errorMessages, "; ")
		} else {
			message = "One or more services or components are in error state"
		}
	} else {
		// All services and components are healthy
		newStatus = models.ApplicationStatusRunning
		message = ""
	}

	// Update only if status changed
	if app.Status != newStatus {
		if err := catalogutils.UpdateApplicationStatus(ctx, s.appRepo, app.ID, newStatus, message); err != nil {
			return fmt.Errorf("failed to update application status: %w", err)
		}
		logger.InfofCtx(ctx, "Updated application %s status to %s", app.Name, newStatus)
	}

	return nil
}

