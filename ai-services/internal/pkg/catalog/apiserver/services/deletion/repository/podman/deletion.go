package podman

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deletion/repository/common"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// PodmanDeletion handles application deletion operations.
type PodmanDeletion struct {
	rt                    runtime.Runtime
	appRepo               dbrepo.ApplicationRepository
	serviceRepo           dbrepo.ServiceRepository
	componentRepo         dbrepo.ComponentRepository
	serviceDependencyRepo dbrepo.ServiceDependencyRepository
}

// NewPodmanDeletion creates a new deletion service instance.
func NewPodmanDeletion(
	rt runtime.Runtime,
	appRepo dbrepo.ApplicationRepository,
	serviceRepo dbrepo.ServiceRepository,
	componentRepo dbrepo.ComponentRepository,
	serviceDependencyRepo dbrepo.ServiceDependencyRepository,
) *PodmanDeletion {
	return &PodmanDeletion{
		rt:                    rt,
		appRepo:               appRepo,
		serviceRepo:           serviceRepo,
		componentRepo:         componentRepo,
		serviceDependencyRepo: serviceDependencyRepo,
	}
}

// PerformDeletion carries out the async cascade deletion for an application.
// When keepData is true, preserves underlying data (pods, volumes, orphaned components).
// When keepData is false, deletes all data including application data directory.
func (s *PodmanDeletion) PerformDeletion(ctx context.Context, appID uuid.UUID, services []models.Service, orphanedComponentIDs []uuid.UUID, keepData bool) {
	proxyManager, err := proxy.GetCaddyProxyManager()
	if err != nil {
		common.HandleStepError(ctx, s.appRepo, appID, "failed to get Caddy proxy manager for app", err)

		return
	}

	// Delete services and track errors
	errorMessages := s.deleteServices(ctx, services, keepData, proxyManager)

	// Delete orphaned components and track errors
	componentErrors := s.deleteOrphanedComponents(ctx, orphanedComponentIDs, keepData)
	errorMessages = append(errorMessages, componentErrors...)

	// Check if any errors occurred during deletion
	if len(errorMessages) > 0 {
		common.HandleDeletionFailure(ctx, s.appRepo, appID, errorMessages)

		return
	}

	// Delete application from DB only if no errors occurred
	if err := s.appRepo.Delete(ctx, appID); err != nil {
		common.HandleStepError(ctx, s.appRepo, appID, "application DB deletion failed", err)

		return
	}

	logger.InfofCtx(ctx, "Application %s deleted successfully", appID)
}

// unregisterServiceRoutes performs best-effort route cleanup for a service.
// Updates DB status if route unregistration fails, but does not block deletion.
func (s *PodmanDeletion) unregisterServiceRoutes(ctx context.Context, proxyManager proxy.ProxyManager, svc models.Service) error {
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
func (s *PodmanDeletion) deleteServices(ctx context.Context, services []models.Service, keepData bool, proxyManager proxy.ProxyManager) []string {
	var errorMessages []string
	forceDelete := true

	for _, svc := range services {
		// List service pods
		pods, err := s.rt.ListPods(ctx, map[string][]string{
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
		secretErrors := s.deleteSecretsFromPods(ctx, pods, keepData, "service", svc.ID)
		if len(secretErrors) > 0 {
			errorMessages = append(errorMessages, secretErrors...)
		}

		// Delete service pods first so runtime releases attached volumes.
		podErrors := s.deletePods(ctx, pods, forceDelete)
		hasDeletionErrors := false
		if len(podErrors) > 0 {
			hasDeletionErrors = true
			errMsg := fmt.Sprintf("service %s: failed to delete %d pod(s)", svc.ID, len(podErrors))
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateServiceStatus(ctx, s.serviceRepo, svc.ID, models.ServiceStatusError, fmt.Sprintf("failed to delete %d pod(s)", len(podErrors)))
		}

		// Delete volumes only after pods are deleted and only when keepData is false.
		if !keepData {
			volumeErrors := s.deleteVolumesFromPods(ctx, pods, "service", svc.ID)
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
func (s *PodmanDeletion) deletePods(ctx context.Context, pods []runtimeTypes.Pod, forceDelete bool) []string {
	var podErrors []string
	for _, pod := range pods {
		if err := s.rt.DeletePod(ctx, pod.ID, &forceDelete); err != nil {
			// Ignore "not found" errors - pod already deleted or never existed
			if utils.IsNotFoundError(err) {
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
func (s *PodmanDeletion) deleteOrphanedComponents(ctx context.Context, componentIDs []uuid.UUID, keepData bool) []string {
	var errorMessages []string
	forceDelete := true

	for _, componentID := range componentIDs {
		// List component pods
		pods, err := s.rt.ListPods(ctx, map[string][]string{
			"label": {fmt.Sprintf("ai-services.io/template=%s", componentID)},
		})
		if err != nil {
			errMsg := fmt.Sprintf("component %s: failed to list pods: %s", componentID, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, s.componentRepo, componentID, models.ComponentStatusError, fmt.Sprintf("failed to list pods: %s", err))

			continue
		}

		// Delete component secrets
		secretErrors := s.deleteSecretsFromPods(ctx, pods, keepData, "component", componentID)
		if len(secretErrors) > 0 {
			errorMessages = append(errorMessages, secretErrors...)
		}

		// Delete component pods first so runtime releases attached volumes.
		podErrors := s.deletePods(ctx, pods, forceDelete)
		hasDeletionErrors := false
		if len(podErrors) > 0 {
			hasDeletionErrors = true
			errMsg := fmt.Sprintf("component %s: failed to delete %d pod(s)", componentID, len(podErrors))
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, s.componentRepo, componentID, models.ComponentStatusError, fmt.Sprintf("failed to delete %d pod(s)", len(podErrors)))
		}

		// Delete component volumes only after pods are deleted and only when keepData is false.
		if !keepData {
			volumeErrors := s.deleteVolumesFromPods(ctx, pods, "component", componentID)
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

// deleteVolumesFromPods extracts volume names from pod labels and deletes them using the runtime client.
// Volumes are always deleted when keepData=false. This method is only called when keepData=false.
//
// Returns a list of error messages for any volumes that failed to delete.
func (s *PodmanDeletion) deleteVolumesFromPods(ctx context.Context, pods []runtimeTypes.Pod, instanceType string, instanceID uuid.UUID) []string {
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
		if err := s.rt.DeleteVolume(ctx, volumeName); err != nil {
			// Ignore "not found" errors - volume already deleted or never existed
			if utils.IsNotFoundError(err) {
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
func (s *PodmanDeletion) deleteSecretsFromPods(ctx context.Context, pods []runtimeTypes.Pod, keepData bool, instanceType string, instanceID uuid.UUID) []string {
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
		if err := s.rt.DeleteSecret(ctx, secretName); err != nil {
			// Ignore "not found" errors - secret already deleted or never existed
			if utils.IsNotFoundError(err) {
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
