package sync

import (
	"context"
	"fmt"
	"strings"
	"sync"
	texttemplate "text/template"
	"time"

	"github.com/google/uuid"
	catalogpkg "github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	modelpkg "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
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
	syncMutex       sync.Mutex                 // Prevents overlapping sync cycles
	isSyncing       bool                       // Tracks if a sync is currently running
	resourceCache   map[string]*ResourceCounts // Tracks expected resource counts per catalogID
	cacheMutex      sync.RWMutex               // Protects resourceCache
	catalogProvider *catalogpkg.CatalogProvider
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

	return &SyncService{
		appRepo:         appRepo,
		serviceRepo:     serviceRepo,
		componentRepo:   componentRepo,
		serviceDepsRepo: serviceDepsRepo,
		syncInterval:    syncInterval,
		stopChan:        make(chan struct{}),
		resourceCache:   make(map[string]*ResourceCounts),
		catalogProvider: catalogProvider,
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

	logger.InfofCtx(ctx, "Completed sync for application: %s", app.Name)

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

			// Sync component pod status
			status, componentMsg, err := s.syncComponentPod(ctx, rt, dep.DependencyID)
			if err != nil {
				logger.ErrorfCtx(ctx, "Failed to sync component %s: %v", dep.DependencyID, err)
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
			logger.ErrorfCtx(ctx, "Failed to sync service %s: %v", service.ID, err)
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
	// Fetch all pods using service ID as template label
	pods, err := s.fetchPodsByTemplateID(rt, service.ID.String())
	if err != nil {
		return s.handleServicePodFetchError(ctx, service, err)
	}

	// Determine service status based on pods and resources
	// For services, use AppID to generate instance slug
	newStatus, message := s.determineServiceStatusFromPods(ctx, service.CatalogID, service.AppID.String(), pods, rt)

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
func (s *SyncService) determineServiceStatusFromPods(ctx context.Context, catalogID, instanceID string, pods []*podStatus, rt runtime.Runtime) (models.ServiceStatus, string) {
	// Validate resource counts against templates
	resourceValidationMsg := s.validateResourceCounts(ctx, catalogID, instanceID, resourceItemTypeService, len(pods), rt)

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

	// Fetch all pods using component ID as template label
	pods, err := s.fetchPodsByTemplateID(rt, componentID.String())
	if err != nil {
		return s.handleComponentPodFetchError(ctx, component, componentID, err)
	}

	// Build component catalogID in format "type/provider"
	componentCatalogID := fmt.Sprintf("%s/%s", component.Type, component.Provider)

	// Validate resource counts against templates
	resourceValidationMsg := s.validateResourceCounts(ctx, componentCatalogID, componentID.String(), resourceItemTypeComponent, len(pods), rt)

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
func (s *SyncService) checkPodsHealth(pods []*podStatus) (models.ComponentStatus, string) {
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

// fetchPodsByTemplateID fetches all pods using the template ID label.
// Returns a slice of pod statuses.
func (s *SyncService) fetchPodsByTemplateID(rt runtime.Runtime, templateID string) ([]*podStatus, error) {
	// Use the same logic as in ApplicationsPs - fetch pods with template label
	filteredPods, err := common.FetchFilteredPods(rt, templateID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pods: %w", err)
	}

	if len(filteredPods) == 0 {
		return nil, fmt.Errorf("no pod found with template ID: %s", templateID)
	}

	// Process all pods
	var podStatuses []*podStatus
	for _, pod := range filteredPods {
		processedPod, err := common.ProcessPod(rt, pod)
		if err != nil {
			return nil, fmt.Errorf("failed to process pod: %w", err)
		}

		if processedPod == nil {
			return nil, fmt.Errorf("pod processing returned nil")
		}

		podStatuses = append(podStatuses, &podStatus{
			state:   processedPod.State,
			health:  processedPod.Health,
			podName: processedPod.Name,
		})
	}

	return podStatuses, nil
}

// podStatus holds the relevant pod status information.
type podStatus struct {
	state   string
	health  string
	podName string
}

// determinePodStatus determines status based on pod state and health
// Returns: isHealthy (bool), errorMessage (string).
func (s *SyncService) determinePodStatus(pod *podStatus) (bool, string) {
	if pod.state == "Running" && pod.health == string(constants.Ready) {
		return true, ""
	}

	if pod.state == "Running" && pod.health == string(constants.NotReady) {
		return false, fmt.Sprintf("Pod %s is running but unhealthy", pod.podName)
	}

	return false, fmt.Sprintf("Pod %s is in state: %s", pod.podName, pod.state)
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

// getExpectedResourceCounts retrieves expected resource counts from cache.
func (s *SyncService) getExpectedResourceCounts(catalogID string) *ResourceCounts {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()

	return s.resourceCache[catalogID]
}

// setExpectedResourceCounts stores expected resource counts in cache.
func (s *SyncService) setExpectedResourceCounts(catalogID string, counts *ResourceCounts) {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()
	s.resourceCache[catalogID] = counts
}

// countResourcesFromTemplates counts expected Pods and Secrets from service/component templates.
func (s *SyncService) countResourcesFromTemplates(ctx context.Context, catalogID, instanceID, itemType string) (*ResourceCounts, error) {
	if s.catalogProvider == nil {
		return nil, fmt.Errorf("catalog provider not initialized")
	}

	templates, values, err := s.loadTemplatesAndValues(catalogID, instanceID, itemType)
	if err != nil {
		return nil, err
	}

	// Generate instance slug for ProcessTemplates
	instanceSlug := catalogutils.GenerateInstanceSlug(instanceID)
	counts := s.processTemplatesForResourceCounts(ctx, templates, values, instanceSlug)
	if counts == nil {
		return nil, fmt.Errorf("failed to process templates")
	}

	logger.InfofCtx(ctx, "Counted resources for %s %s: %d pods, %d secret refs, %d volume refs",
		itemType, catalogID, counts.Pods, len(counts.SecretNames), len(counts.VolumeNames))

	return counts, nil
}

// loadTemplatesAndValues loads templates and values based on item type.
func (s *SyncService) loadTemplatesAndValues(catalogID, instanceID, itemType string) (map[string]*texttemplate.Template, map[string]any, error) {
	switch itemType {
	case resourceItemTypeService:
		return s.loadServiceTemplatesAndValues(catalogID, instanceID)
	case resourceItemTypeComponent:
		return s.loadComponentTemplatesAndValues(catalogID, instanceID)
	default:
		return nil, nil, fmt.Errorf("unknown item type: %s", itemType)
	}
}

// loadServiceTemplatesAndValues loads service templates and values.
// For services, instanceID should be the Application ID.
func (s *SyncService) loadServiceTemplatesAndValues(catalogID, instanceID string) (map[string]*texttemplate.Template, map[string]any, error) {
	templates, err := s.catalogProvider.LoadServiceTemplates(catalogID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load service templates: %w", err)
	}

	// Generate instance slug from Application ID
	instanceSlug := catalogutils.GenerateInstanceSlug(instanceID)
	overrides := map[string]string{
		"InstanceSlug": instanceSlug,
		"TemplateID":   instanceID,
	}

	values, err := s.catalogProvider.LoadServiceValues(catalogID, overrides)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load service values: %w", err)
	}

	return templates, values, nil
}

// loadComponentTemplatesAndValues loads component templates and values.
// For components, instanceID should be the Component ID.
func (s *SyncService) loadComponentTemplatesAndValues(catalogID, instanceID string) (map[string]*texttemplate.Template, map[string]any, error) {
	parts := strings.Split(catalogID, "/")
	if len(parts) != componentCatalogIDParts {
		return nil, nil, fmt.Errorf("invalid component catalogID format: %s (expected format: type/provider)", catalogID)
	}
	componentType, providerID := parts[0], parts[1]

	templates, err := s.catalogProvider.LoadComponentTemplates(componentType, providerID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load component templates: %w", err)
	}

	// Generate instance slug from Component ID
	instanceSlug := catalogutils.GenerateInstanceSlug(instanceID)
	overrides := map[string]string{
		"InstanceSlug": instanceSlug,
		"TemplateID":   instanceID,
	}

	values, err := s.catalogProvider.LoadComponentValues(componentType, providerID, overrides)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load component values: %w", err)
	}

	return templates, values, nil
}

// processTemplatesForResourceCounts processes templates and counts resources.
func (s *SyncService) processTemplatesForResourceCounts(ctx context.Context, templates map[string]*texttemplate.Template, values map[string]any, instanceSlug string) *ResourceCounts {
	counts := &ResourceCounts{}

	processor := func(templateName string, podSpec *modelpkg.PodSpec) error {
		if podSpec.Kind != "Pod" {
			return nil
		}

		counts.Pods++
		s.extractResourceLabelsFromPodSpec(podSpec, counts)

		return nil
	}

	if err := s.catalogProvider.ProcessTemplates(ctx, templates, values, instanceSlug, processor); err != nil {
		logger.ErrorfCtx(ctx, "Failed to process templates: %v", err)

		return nil
	}

	return counts
}

// extractResourceLabelsFromPodSpec extracts secret and volume labels from pod spec.
func (s *SyncService) extractResourceLabelsFromPodSpec(podSpec *modelpkg.PodSpec, counts *ResourceCounts) {
	if podSpec.Labels == nil {
		return
	}

	if secretLabel, ok := podSpec.Labels["ai-services.io/secret"]; ok && secretLabel != "" {
		counts.SecretNames = append(counts.SecretNames, secretLabel)
	}

	if volumeLabel, ok := podSpec.Labels["ai-services.io/volume"]; ok && volumeLabel != "" {
		counts.VolumeNames = append(counts.VolumeNames, volumeLabel)
	}
}

// validateResourceCounts validates that actual resources match expected counts from templates.
// Returns error message if validation fails, empty string if all resources are present.
func (s *SyncService) validateResourceCounts(ctx context.Context, catalogID, instanceID, itemType string, actualPodCount int, rt runtime.Runtime) string {
	// Get expected counts from cache
	expectedCounts := s.getExpectedResourceCounts(catalogID)

	// If not in cache, count from templates and cache it
	if expectedCounts == nil {
		counts, err := s.countResourcesFromTemplates(ctx, catalogID, instanceID, itemType)
		if err != nil {
			logger.ErrorfCtx(ctx, "Failed to count resources from templates for %s %s: %v", itemType, catalogID, err)
			// Don't fail sync if we can't count templates - just skip validation
			return ""
		}
		expectedCounts = counts
		s.setExpectedResourceCounts(catalogID, counts)
	}

	var errorMessages []string

	// Validate pod count
	if actualPodCount < expectedCounts.Pods {
		errorMessages = append(errorMessages,
			fmt.Sprintf("Pod count mismatch: expected %d, found %d", expectedCounts.Pods, actualPodCount))
	}

	// Validate secrets and volumes exist by checking pod labels
	// If pods are running with these labels, the secrets/volumes must exist
	if len(expectedCounts.SecretNames) > 0 || len(expectedCounts.VolumeNames) > 0 {
		resourceValidationMsg := s.validateResourcesFromPodLabels(ctx, expectedCounts.SecretNames, expectedCounts.VolumeNames, rt)
		if resourceValidationMsg != "" {
			errorMessages = append(errorMessages, resourceValidationMsg)
		}
	}

	if len(errorMessages) > 0 {
		return strings.Join(errorMessages, "; ")
	}

	return ""
}

// validateResourcesFromPodLabels validates that secrets and volumes referenced in templates exist
// by checking the runtime for their existence.
func (s *SyncService) validateResourcesFromPodLabels(ctx context.Context, expectedSecretNames, expectedVolumeNames []string, rt runtime.Runtime) string {
	if len(expectedSecretNames) == 0 && len(expectedVolumeNames) == 0 {
		return ""
	}

	var errorMessages []string

	// Validate secrets
	if secretMsg := s.validateSecrets(ctx, expectedSecretNames, rt); secretMsg != "" {
		errorMessages = append(errorMessages, secretMsg)
	}

	// Validate volumes
	if volumeMsg := s.validateVolumes(ctx, expectedVolumeNames, rt); volumeMsg != "" {
		errorMessages = append(errorMessages, volumeMsg)
	}

	if len(errorMessages) > 0 {
		return strings.Join(errorMessages, "; ")
	}

	return ""
}

// validateSecrets validates that expected secrets exist in runtime.
func (s *SyncService) validateSecrets(ctx context.Context, expectedSecretNames []string, rt runtime.Runtime) string {
	return s.validateResourceExistence(ctx, expectedSecretNames, "secret", rt.SecretExists)
}

// validateVolumes validates that expected volumes exist in runtime.
func (s *SyncService) validateVolumes(ctx context.Context, expectedVolumeNames []string, rt runtime.Runtime) string {
	return s.validateResourceExistence(ctx, expectedVolumeNames, "volume", rt.VolumeExists)
}

// validateResourceExistence is a generic helper to validate resource existence.
func (s *SyncService) validateResourceExistence(ctx context.Context, resourceNames []string, resourceType string, existsFunc func(string) (bool, error)) string {
	var missingResources []string
	for _, resourceName := range resourceNames {
		exists, err := existsFunc(resourceName)
		if err != nil {
			logger.ErrorfCtx(ctx, "Failed to check %s existence for %s: %v", resourceType, resourceName, err)

			continue
		}
		if !exists {
			missingResources = append(missingResources, resourceName)
		}
	}
	if len(missingResources) > 0 {
		return fmt.Sprintf("Missing %ss: %s", resourceType, strings.Join(missingResources, ", "))
	}

	return ""
}

// Made with Bob
