package deletion

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// DeletionService handles application deletion operations.
type DeletionService struct {
	appRepo               dbrepo.ApplicationRepository
	serviceRepo           dbrepo.ServiceRepository
	componentRepo         dbrepo.ComponentRepository
	serviceDependencyRepo dbrepo.ServiceDependencyRepository
}

// NewDeletionService creates a new deletion service instance.
func NewDeletionService(
	appRepo dbrepo.ApplicationRepository,
	serviceRepo dbrepo.ServiceRepository,
	componentRepo dbrepo.ComponentRepository,
	serviceDependencyRepo dbrepo.ServiceDependencyRepository,
) *DeletionService {
	return &DeletionService{
		appRepo:               appRepo,
		serviceRepo:           serviceRepo,
		componentRepo:         componentRepo,
		serviceDependencyRepo: serviceDependencyRepo,
	}
}

// PerformDeletion carries out the async cascade deletion for an application.
// When keepData is true, preserves underlying data (pods, volumes, orphaned components).
// When keepData is false, deletes all data including application data directory.
func (s *DeletionService) PerformDeletion(ctx context.Context, appID uuid.UUID, services []models.Service, keepData bool) {
	// Identify orphaned components before deletion
	orphanedComponents, err := s.identifyOrphanedComponents(ctx, appID, services)
	if err != nil {
		return // Error already logged and status updated
	}

	// Initialize runtime client
	rt, err := vars.RuntimeFactory.Create("")
	if err != nil {
		logger.ErrorfCtx(ctx, "failed to init runtime client for app %s: %s", appID, err)
		_ = catalogutils.UpdateApplicationStatus(ctx, s.appRepo, appID, models.ApplicationStatusError, "failed to init runtime client")

		return
	}

	// Get Caddy proxy manager - fail if CADDY_ADMIN_URL not set
	proxyManager, err := proxy.GetCaddyProxyManager()
	if err != nil {
		logger.ErrorfCtx(ctx, "failed to get Caddy proxy manager for app %s: %s", appID, err)
		_ = catalogutils.UpdateApplicationStatus(ctx, s.appRepo, appID, models.ApplicationStatusError, fmt.Sprintf("failed to get Caddy proxy manager: %s", err))

		return
	}

	// Delete services and track errors
	errorMessages := s.deleteServices(ctx, rt, services, keepData, proxyManager)

	// Delete orphaned components and track errors
	componentErrors := s.deleteOrphanedComponents(ctx, rt, orphanedComponents, keepData)
	errorMessages = append(errorMessages, componentErrors...)

	// Check if any errors occurred during deletion
	if len(errorMessages) > 0 {
		s.handleDeletionFailure(ctx, appID, errorMessages)

		return
	}

	// Delete application from DB only if no errors occurred
	if err := s.appRepo.Delete(ctx, appID); err != nil {
		errMsg := fmt.Sprintf("failed to delete application: %s", err)
		logger.ErrorfCtx(ctx, "application %s: %s", appID, errMsg)
		_ = catalogutils.UpdateApplicationStatus(ctx, s.appRepo, appID, models.ApplicationStatusError, errMsg)

		return
	}

	logger.InfofCtx(ctx, "Application %s deleted successfully", appID)
}

// identifyOrphanedComponents identifies components that will become orphaned after service deletion.
func (s *DeletionService) identifyOrphanedComponents(ctx context.Context, appID uuid.UUID, services []models.Service) ([]uuid.UUID, error) {
	serviceIDs := s.buildServiceIDMap(services)

	componentCandidates, err := s.collectComponentCandidates(ctx, appID, services)
	if err != nil {
		return nil, err
	}

	return s.filterOrphanedComponents(ctx, componentCandidates, serviceIDs), nil
}

// buildServiceIDMap creates a map of service IDs for quick lookup.
func (s *DeletionService) buildServiceIDMap(services []models.Service) map[uuid.UUID]bool {
	serviceIDs := make(map[uuid.UUID]bool, len(services))
	for _, svc := range services {
		serviceIDs[svc.ID] = true
	}

	return serviceIDs
}

// collectComponentCandidates gathers all components used by services being deleted.
func (s *DeletionService) collectComponentCandidates(ctx context.Context, appID uuid.UUID, services []models.Service) (map[uuid.UUID]bool, error) {
	componentCandidates := make(map[uuid.UUID]bool)

	for _, svc := range services {
		deps, err := s.serviceDependencyRepo.GetDependenciesByServiceID(ctx, svc.ID)
		if err != nil {
			logger.ErrorfCtx(ctx, "failed to get dependencies for service %s: %s", svc.ID, err)
			_ = catalogutils.UpdateApplicationStatus(ctx, s.appRepo, appID, models.ApplicationStatusError, "failed to get service dependencies")

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
func (s *DeletionService) filterOrphanedComponents(ctx context.Context, componentCandidates map[uuid.UUID]bool, serviceIDs map[uuid.UUID]bool) []uuid.UUID {
	var orphanedComponents []uuid.UUID

	for componentID := range componentCandidates {
		if s.isComponentOrphaned(ctx, componentID, serviceIDs) {
			orphanedComponents = append(orphanedComponents, componentID)
		}
	}

	return orphanedComponents
}

// isComponentOrphaned checks if a component has no remaining dependent services.
func (s *DeletionService) isComponentOrphaned(ctx context.Context, componentID uuid.UUID, serviceIDs map[uuid.UUID]bool) bool {
	dependentServices, err := s.serviceDependencyRepo.GetServicesByDependency(ctx, componentID, models.DependencyTypeComponent)
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

// unregisterServiceRoutes performs best-effort route cleanup for a service.
// Updates DB status if route unregistration fails, but does not block deletion.
func (s *DeletionService) unregisterServiceRoutes(ctx context.Context, proxyManager proxy.ProxyManager, svc models.Service) error {
	if len(svc.Endpoints) == 0 || proxyManager == nil {
		return nil
	}

	if err := proxy.UnregisterRoutesFromEndpoints(
		ctx,
		proxyManager,
		svc.Endpoints,
		"service",
		svc.ID.String(),
	); err != nil {
		// Update DB status with route cleanup failure
		_ = catalogutils.UpdateServiceStatus(ctx, s.serviceRepo, svc.ID, models.ServiceStatusError,
			fmt.Sprintf("route unregistration failed: %v", err))

		return err
	}

	return nil
}

// deleteServices deletes all services (pods + DB records) and returns any error messages.
//
//nolint:cyclop // Function complexity is acceptable for deletion orchestration
func (s *DeletionService) deleteServices(ctx context.Context, rt runtime.Runtime, services []models.Service, keepData bool, proxyManager proxy.ProxyManager) []string {
	var errorMessages []string
	forceDelete := true

	for _, svc := range services {
		// List service pods
		pods, err := rt.ListPods(map[string][]string{
			"label": {fmt.Sprintf("ai-services.io/template=%s", svc.ID)},
		})
		if err != nil {
			errMsg := fmt.Sprintf("service %s: failed to list pods: %s", svc.ID, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateServiceStatus(ctx, s.serviceRepo, svc.ID, models.ServiceStatusError, fmt.Sprintf("failed to list pods: %s", err))

			continue
		}

		// Cleanup Caddy routes before deleting pods (blocks deletion on failure)
		if err := s.unregisterServiceRoutes(ctx, proxyManager, svc); err != nil {
			errorMessages = append(errorMessages, fmt.Sprintf("service %s: %s", svc.ID, err))

			continue
		}

		// Delete service secrets
		secretErrors := s.deleteSecretsFromPods(ctx, rt, pods, keepData, "service", svc.ID)
		if len(secretErrors) > 0 {
			errorMessages = append(errorMessages, secretErrors...)
		}

		// Delete service pods first so runtime releases attached volumes.
		podErrors := s.deletePods(ctx, rt, pods, forceDelete)
		hasDeletionErrors := false
		if len(podErrors) > 0 {
			hasDeletionErrors = true
			errMsg := fmt.Sprintf("service %s: failed to delete %d pod(s)", svc.ID, len(podErrors))
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateServiceStatus(ctx, s.serviceRepo, svc.ID, models.ServiceStatusError, fmt.Sprintf("failed to delete %d pod(s)", len(podErrors)))
		}

		// Delete volumes only after pods are deleted and only when keepData is false.
		if !keepData {
			volumeErrors := s.deleteVolumesFromPods(ctx, rt, pods, "service", svc.ID)
			if len(volumeErrors) > 0 {
				hasDeletionErrors = true
				errorMessages = append(errorMessages, volumeErrors...)
			}
		}

		if hasDeletionErrors {
			continue
		}

		// Delete service from DB
		if err := s.serviceRepo.Delete(ctx, svc.ID); err != nil {
			errMsg := fmt.Sprintf("service %s: failed to delete from DB: %s", svc.ID, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateServiceStatus(ctx, s.serviceRepo, svc.ID, models.ServiceStatusError, fmt.Sprintf("failed to delete from DB: %s", err))
		}
	}

	return errorMessages
}

// deletePods deletes all pods and returns any error messages.
func (s *DeletionService) deletePods(ctx context.Context, rt runtime.Runtime, pods []runtimeTypes.Pod, forceDelete bool) []string {
	var podErrors []string
	for _, pod := range pods {
		if err := rt.DeletePod(pod.ID, &forceDelete); err != nil {
			// Ignore "not found" errors - pod already deleted or never existed
			if catalogutils.IsNotFoundError(err) {
				logger.InfofCtx(ctx, "Pod %s already deleted or does not exist", pod.ID)

				continue
			}
			errMsg := fmt.Sprintf("failed to delete pod %s: %s", pod.ID, err)
			podErrors = append(podErrors, errMsg)
		}
	}

	return podErrors
}

// deleteOrphanedComponents deletes orphaned components (pods + DB records) and returns any error messages.
//
//nolint:cyclop // Function complexity is acceptable for deletion orchestration
func (s *DeletionService) deleteOrphanedComponents(ctx context.Context, rt runtime.Runtime, componentIDs []uuid.UUID, keepData bool) []string {
	var errorMessages []string
	forceDelete := true

	for _, componentID := range componentIDs {
		// List component pods
		pods, err := rt.ListPods(map[string][]string{
			"label": {fmt.Sprintf("ai-services.io/template=%s", componentID)},
		})
		if err != nil {
			errMsg := fmt.Sprintf("component %s: failed to list pods: %s", componentID, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, s.componentRepo, componentID, models.ComponentStatusError, fmt.Sprintf("failed to list pods: %s", err))

			continue
		}

		// Delete component secrets
		secretErrors := s.deleteSecretsFromPods(ctx, rt, pods, keepData, "component", componentID)
		if len(secretErrors) > 0 {
			errorMessages = append(errorMessages, secretErrors...)
		}

		// Delete component pods first so runtime releases attached volumes.
		podErrors := s.deletePods(ctx, rt, pods, forceDelete)
		hasDeletionErrors := false
		if len(podErrors) > 0 {
			hasDeletionErrors = true
			errMsg := fmt.Sprintf("component %s: failed to delete %d pod(s)", componentID, len(podErrors))
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, s.componentRepo, componentID, models.ComponentStatusError, fmt.Sprintf("failed to delete %d pod(s)", len(podErrors)))
		}

		// Delete component volumes only after pods are deleted and only when keepData is false.
		if !keepData {
			volumeErrors := s.deleteVolumesFromPods(ctx, rt, pods, "component", componentID)
			if len(volumeErrors) > 0 {
				hasDeletionErrors = true
				errorMessages = append(errorMessages, volumeErrors...)
			}
		}

		if hasDeletionErrors {
			continue
		}

		// Delete component from DB
		if err := s.componentRepo.Delete(ctx, componentID); err != nil {
			errMsg := fmt.Sprintf("component %s: failed to delete from DB: %s", componentID, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, s.componentRepo, componentID, models.ComponentStatusError, fmt.Sprintf("failed to delete from DB: %s", err))
		}
	}

	return errorMessages
}

// handleDeletionFailure updates application status when deletion fails.
func (s *DeletionService) handleDeletionFailure(ctx context.Context, appID uuid.UUID, errorMessages []string) {
	errMsg := fmt.Sprintf("Application deletion failed with %d error(s), application not deleted", len(errorMessages))
	logger.ErrorfCtx(ctx, "application %s: %s", appID, errMsg)
	_ = catalogutils.UpdateApplicationStatus(ctx, s.appRepo, appID, models.ApplicationStatusError, errMsg)
}

// deleteVolumesFromPods extracts volume names from pod labels and deletes them using the runtime client.
// Volumes are always deleted when keepData=false. This method is only called when keepData=false.
//
// Returns a list of error messages for any volumes that failed to delete.
func (s *DeletionService) deleteVolumesFromPods(ctx context.Context, rt runtime.Runtime, pods []runtimeTypes.Pod, instanceType string, instanceID uuid.UUID) []string {
	var errorMessages []string
	volumesToDelete := make(map[string]bool) // Use map to avoid duplicates

	// Extract volume names from pod labels
	for _, pod := range pods {
		if volumeNames, ok := pod.Labels[catalogconstants.CatalogVolumeLabel]; ok && volumeNames != "" {
			// Split comma-separated volume names (in case a pod has multiple volumes)
			volumes := strings.Split(volumeNames, ",")
			for _, volumeName := range volumes {
				volumeName = strings.TrimSpace(volumeName)
				if volumeName != "" {
					volumesToDelete[volumeName] = true
				}
			}
		}
	}

	if len(volumesToDelete) == 0 {
		// Just return if there are no volumes to delete
		return nil
	}

	logger.InfofCtx(ctx, "Deleting %d volume(s) for %s %s", len(volumesToDelete), instanceType, instanceID)

	// Delete each unique volume using the runtime client
	for volumeName := range volumesToDelete {
		if err := rt.DeleteVolume(volumeName); err != nil {
			// Ignore "not found" errors - volume already deleted or never existed
			if catalogutils.IsNotFoundError(err) {
				logger.InfofCtx(ctx, "Volume %s already deleted or does not exist", volumeName)

				continue
			}
			errMsg := fmt.Sprintf("%s %s: failed to delete volume %s: %s", instanceType, instanceID, volumeName, err)
			errorMessages = append(errorMessages, errMsg)
			logger.ErrorfCtx(ctx, "%s %s: failed to delete volume %s: %s", instanceType, instanceID, volumeName, err)
		} else {
			logger.InfofCtx(ctx, "Successfully deleted volume: %s", volumeName)
		}
	}

	return errorMessages
}

// deleteSecretsFromPods extracts secret names from pod labels and deletes them.
//
// Deletion logic:
//   - If keepData=false: Delete ALL secrets (default behavior - complete cleanup)
//   - If keepData=true: Only delete secrets WITHOUT skip-cleanup label OR with skip-cleanup="false" (e.g., API keys)
//     Preserve secrets WITH skip-cleanup="true" (e.g., DB credentials)
//
// Returns a list of error messages for any secrets that failed to delete.
func (s *DeletionService) deleteSecretsFromPods(ctx context.Context, rt runtime.Runtime, pods []runtimeTypes.Pod, keepData bool, instanceType string, instanceID uuid.UUID) []string {
	var errorMessages []string
	secretsToDelete := make(map[string]bool) // Use map to avoid duplicates

	// Extract secret names from pod labels
	for _, pod := range pods {
		if secretName, ok := pod.Labels[catalogconstants.CatalogSecretLabel]; ok {
			// If keepData=false, delete ALL secrets
			if !keepData {
				secretsToDelete[secretName] = true
			} else {
				// If keepData=true, only delete secrets without skip-cleanup or with skip-cleanup="false"
				if skipValue, hasSkipLabel := pod.Labels[catalogconstants.CatalogSecretSkipLabel]; !hasSkipLabel || skipValue == "false" {
					secretsToDelete[secretName] = true
				}
			}
		}
	}

	if len(secretsToDelete) == 0 {
		// no secrets found to delete, just return
		return errorMessages
	}

	logger.InfofCtx(ctx, "Deleting %d secret(s) for %s %s (keepData=%v)", len(secretsToDelete), instanceType, instanceID, keepData)

	// Delete each unique secret
	for secretName := range secretsToDelete {
		if err := rt.DeleteSecret(secretName); err != nil {
			// Ignore "not found" errors - secret already deleted or never existed
			if catalogutils.IsNotFoundError(err) {
				logger.InfofCtx(ctx, "Secret %s already deleted or does not exist", secretName)

				continue
			}
			errMsg := fmt.Sprintf("%s %s: failed to delete secret %s: %s", instanceType, instanceID, secretName, err)
			errorMessages = append(errorMessages, errMsg)
			logger.ErrorfCtx(ctx, "%s %s: failed to delete secret %s: %s", instanceType, instanceID, secretName, err)
		} else {
			logger.InfofCtx(ctx, "Successfully deleted secret: %s", secretName)
		}
	}

	return errorMessages
}

// Made with Bob
