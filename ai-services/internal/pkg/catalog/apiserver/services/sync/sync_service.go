package sync

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	// DefaultSyncInterval is the default interval for syncing DB with pod status.
	DefaultSyncInterval = 30 * time.Second
)

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
}

// NewSyncService creates a new sync service instance.
func NewSyncService(
	appRepo dbrepo.ApplicationRepository,
	serviceRepo dbrepo.ServiceRepository,
	componentRepo dbrepo.ComponentRepository,
	serviceDepsRepo dbrepo.ServiceDependencyRepository,
	syncInterval time.Duration,
) *SyncService {
	if syncInterval == 0 {
		syncInterval = DefaultSyncInterval
	}

	return &SyncService{
		appRepo:         appRepo,
		serviceRepo:     serviceRepo,
		componentRepo:   componentRepo,
		serviceDepsRepo: serviceDepsRepo,
		syncInterval:    syncInterval,
		stopChan:        make(chan struct{}),
	}
}

// Start begins the sync goroutine.
func (s *SyncService) Start() {
	go s.syncLoop()
	logger.Infoln("Sync service started")
}

// Stop gracefully stops the sync goroutine.
func (s *SyncService) Stop() {
	close(s.stopChan)
	logger.Infoln("Sync service stopped")
}

// syncLoop runs the periodic sync operation.
func (s *SyncService) syncLoop() {
	// Defer panic recovery
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic recovered in sync goroutine: %v", r)
		}
	}()

	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	// Run initial sync immediately
	s.performSync()

	for {
		select {
		case <-ticker.C:
			s.performSync()
		case <-s.stopChan:
			return
		}
	}
}

// performSync executes the synchronization logic.
func (s *SyncService) performSync() {
	// Check if a sync is already running
	s.syncMutex.Lock()
	if s.isSyncing {
		logger.Infof("Sync already in progress, skipping this cycle", logger.VerbosityLevelDebug)
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

	ctx := context.Background()

	logger.Infof("Starting DB-Pod sync cycle", logger.VerbosityLevelDebug)

	// Get all applications with Running or Error status
	filters := &dbrepo.ApplicationFilters{}
	applications, err := s.appRepo.GetAll(ctx, filters)
	if err != nil {
		logger.Errorf("Failed to fetch applications for sync: %v", err)

		return
	}

	// Filter applications that need syncing (Running or Error state)
	for _, app := range applications {
		if app.Status == models.ApplicationStatusRunning || app.Status == models.ApplicationStatusError {
			if err := s.syncApplication(ctx, &app); err != nil {
				logger.Errorf("Failed to sync application %s: %v", app.Name, err)
			}
		}
	}

	logger.Infof("Completed DB-Pod sync cycle", logger.VerbosityLevelDebug)
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

	logger.Infof("Syncing application: %s (ID: %s)", app.Name, app.ID)

	// Track errors during sync
	errorMessages := []string{}
	allHealthy := true

	// Step 1: Sync all components first (bottom of dependency tree)
	componentErrors := s.syncAllComponents(ctx, rt, app)
	if len(componentErrors) > 0 {
		errorMessages = append(errorMessages, componentErrors...)
		allHealthy = false
	}

	// Step 2: Sync services (middle of dependency tree)
	serviceErrors := s.syncAllServices(ctx, rt, app)
	if len(serviceErrors) > 0 {
		errorMessages = append(errorMessages, serviceErrors...)
		allHealthy = false
	}

	// Step 3: Update application status based on collected errors
	if err := s.updateApplicationStatus(ctx, app, allHealthy, errorMessages); err != nil {
		return fmt.Errorf("failed to update application status: %w", err)
	}

	logger.Infof("Completed sync for application: %s", app.Name)

	return nil
}

// syncAllComponents syncs all components for an application
// Returns: error messages for application-level reporting.
func (s *SyncService) syncAllComponents(ctx context.Context, rt runtime.Runtime, app *models.Application) []string {
	processedComponents := make(map[uuid.UUID]bool)
	errorMessages := []string{}

	// Collect all unique components from all services
	for _, service := range app.Services {
		dependencies, err := s.serviceDepsRepo.GetDependenciesByServiceID(ctx, service.ID)
		if err != nil {
			logger.Errorf("Failed to get dependencies for service %s: %v", service.ID, err)

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

			// Sync component pod status
			status, componentMsg, err := s.syncComponentPod(ctx, rt, dep.DependencyID)
			if err != nil {
				logger.Errorf("Failed to sync component %s: %v", dep.DependencyID, err)
			} else if status == models.ComponentStatusError && componentMsg != "" {
				// Collect error messages for application status
				errorMessages = append(errorMessages, componentMsg)
			}

			processedComponents[dep.DependencyID] = true
		}
	}

	return errorMessages
}

// syncAllServices syncs all services for an application
// Service status is determined ONLY by the service pod health, not component health
// Returns: error messages for application-level reporting.
func (s *SyncService) syncAllServices(ctx context.Context, rt runtime.Runtime, app *models.Application) []string {
	errorMessages := []string{}

	for _, service := range app.Services {
		serviceMsg, err := s.syncServicePod(ctx, rt, service)
		if err != nil {
			logger.Errorf("Failed to sync service %s: %v", service.ID, err)
			// Continue with other services even if one fails
		}
		// Collect error messages for application status
		if serviceMsg != "" {
			errorMessages = append(errorMessages, fmt.Sprintf("Service %s: %s", service.CatalogID, serviceMsg))
		}
	}

	return errorMessages
}

// syncServicePod syncs a single service's pod status
// Returns: error message (if any) and error.
func (s *SyncService) syncServicePod(ctx context.Context, rt runtime.Runtime, service models.Service) (string, error) {
	// Fetch pod using service ID as template label
	pod, err := s.fetchPodByTemplateID(rt, service.ID.String())
	if err != nil {
		// Pod not found or error - mark service as Error
		newStatus := models.ServiceStatusError
		message := fmt.Sprintf("Pod not found or error: %v", err)

		if service.Status != newStatus {
			if err := s.serviceRepo.UpdateStatus(ctx, service.ID, newStatus, message); err != nil {
				return "", fmt.Errorf("failed to update service status: %w", err)
			}
			logger.Infof("Updated service %s status to %s", service.ID, newStatus)
		}

		return message, nil
	}

	// Determine service status based on pod health
	newStatus, message := s.determineServiceStatus(pod)

	// Update only if status changed
	if service.Status != newStatus {
		if err := s.serviceRepo.UpdateStatus(ctx, service.ID, newStatus, message); err != nil {
			return "", fmt.Errorf("failed to update service status: %w", err)
		}
		logger.Infof("Updated service %s status to %s", service.ID, newStatus)
	}

	// Return error message if service is in error state
	if newStatus == models.ServiceStatusError {
		return message, nil
	}

	return "", nil
}

// syncComponentPod syncs a single component's pod status
// Returns: status, error message (if any), and error.
func (s *SyncService) syncComponentPod(ctx context.Context, rt runtime.Runtime, componentID uuid.UUID) (models.ComponentStatus, string, error) {
	// Get component from DB
	component, err := s.componentRepo.GetByID(ctx, componentID)
	if err != nil {
		return models.ComponentStatusError, "", fmt.Errorf("failed to get component: %w", err)
	}
	if component == nil {
		return models.ComponentStatusError, "", fmt.Errorf("component not found: %s", componentID)
	}

	// Fetch pod using component ID as template label
	pod, err := s.fetchPodByTemplateID(rt, componentID.String())
	if err != nil {
		// Pod not found or error - mark component as Error
		newStatus := models.ComponentStatusError
		message := fmt.Sprintf("Pod not found or error: %v", err)

		if component.Status != newStatus {
			if err := s.componentRepo.UpdateStatus(ctx, componentID, newStatus, message); err != nil {
				return newStatus, "", fmt.Errorf("failed to update component status: %w", err)
			}
			logger.Infof("Updated component %s status to %s", componentID, newStatus)
		}
		// Return formatted message for application status
		return newStatus, fmt.Sprintf("Component %s/%s: %s", component.Type, component.Provider, message), nil
	}

	// Determine component status based on pod health
	newStatus, message := s.determineComponentStatus(pod)

	// Update only if status changed
	if component.Status != newStatus {
		if err := s.componentRepo.UpdateStatus(ctx, componentID, newStatus, message); err != nil {
			return newStatus, "", fmt.Errorf("failed to update component status: %w", err)
		}
		logger.Infof("Updated component %s status to %s", componentID, newStatus)
	}

	// Return formatted message for application status if component is in error
	if newStatus == models.ComponentStatusError {
		return newStatus, fmt.Sprintf("Component %s/%s: %s", component.Type, component.Provider, message), nil
	}

	return newStatus, "", nil
}

// fetchPodByTemplateID fetches a pod using the template ID label.
func (s *SyncService) fetchPodByTemplateID(rt runtime.Runtime, templateID string) (*podStatus, error) {
	// Use the same logic as in ApplicationsPs - fetch pods with template label
	filteredPods, err := common.FetchFilteredPods(rt, templateID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pods: %w", err)
	}

	if len(filteredPods) == 0 {
		return nil, fmt.Errorf("no pod found with template ID: %s", templateID)
	}

	// Take the first pod (should only be one per template ID)
	pod := filteredPods[0]

	// Process pod to get status and health
	processedPod, err := common.ProcessPod(rt, pod)
	if err != nil {
		return nil, fmt.Errorf("failed to process pod: %w", err)
	}

	if processedPod == nil {
		return nil, fmt.Errorf("pod processing returned nil")
	}

	return &podStatus{
		state:   processedPod.State,
		health:  processedPod.Health,
		podName: processedPod.Name,
	}, nil
}

// podStatus holds the relevant pod status information.
type podStatus struct {
	state   string
	health  string
	podName string
}

// determinePodStatus determines status based on pod state and health
// Returns: isRunning (bool), errorMessage (string).
func (s *SyncService) determinePodStatus(pod *podStatus) (bool, string) {
	if pod.state == "Running" && pod.health == string(constants.Ready) {
		return true, ""
	}

	if pod.state == "Running" && pod.health == string(constants.NotReady) {
		return false, fmt.Sprintf("Pod %s is running but unhealthy", pod.podName)
	}

	return false, fmt.Sprintf("Pod %s is in state: %s", pod.podName, pod.state)
}

// determineServiceStatus determines service status based on pod state and health.
func (s *SyncService) determineServiceStatus(pod *podStatus) (models.ServiceStatus, string) {
	isRunning, message := s.determinePodStatus(pod)
	if isRunning {
		return models.ServiceStatusRunning, message
	}

	return models.ServiceStatusError, message
}

// determineComponentStatus determines component status based on pod state and health.
func (s *SyncService) determineComponentStatus(pod *podStatus) (models.ComponentStatus, string) {
	isRunning, message := s.determinePodStatus(pod)
	if isRunning {
		return models.ComponentStatusRunning, message
	}

	return models.ComponentStatusError, message
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
		if err := s.appRepo.UpdateStatus(ctx, app.ID, newStatus, message); err != nil {
			return fmt.Errorf("failed to update application status: %w", err)
		}
		logger.Infof("Updated application %s status to %s", app.Name, newStatus)
	}

	return nil
}

// Made with Bob
