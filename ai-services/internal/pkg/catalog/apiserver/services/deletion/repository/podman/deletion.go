package podman

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deletion/repository/common"
	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/utils/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	remoteruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/remote"
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
	proxyManager, err := s.getProxyManager()
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

// getProxyManager returns a proxy manager bound to the same target as the runtime.
// For remote worker deletions this routes proxy operations over the worker stream.
func (s *PodmanDeletion) getProxyManager() (proxy.ProxyManager, error) {
	if rt, ok := s.rt.(*remoteruntime.RemoteRuntime); ok {
		return proxy.NewRemoteProxyManager(rt.Sender), nil
	}

	return proxy.GetCaddyProxyManager()
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
		secretErrors := podmanutils.DeleteSecretsFromPods(ctx, s.rt, pods, keepData, "service", svc.ID)
		if len(secretErrors) > 0 {
			errorMessages = append(errorMessages, secretErrors...)
		}

		// Delete service pods first so runtime releases attached volumes.
		podErrors := podmanutils.DeletePods(ctx, s.rt, pods, forceDelete)
		hasDeletionErrors := false
		if len(podErrors) > 0 {
			hasDeletionErrors = true
			errMsg := fmt.Sprintf("service %s: failed to delete %d pod(s)", svc.ID, len(podErrors))
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateServiceStatus(ctx, s.serviceRepo, svc.ID, models.ServiceStatusError, fmt.Sprintf("failed to delete %d pod(s)", len(podErrors)))
		}

		// Delete volumes only after pods are deleted and only when keepData is false.
		if !keepData {
			volumeErrors := podmanutils.DeleteVolumesFromPods(ctx, s.rt, pods, "service", svc.ID)
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

// deleteOrphanedComponents deletes orphaned components (pods + DB records) and returns any error messages.
func (s *PodmanDeletion) deleteOrphanedComponents(ctx context.Context, componentIDs []uuid.UUID, keepData bool) []string {
	return podmanutils.DeleteComponents(ctx, s.rt, s.componentRepo, componentIDs, keepData)
}

// Made with Bob
