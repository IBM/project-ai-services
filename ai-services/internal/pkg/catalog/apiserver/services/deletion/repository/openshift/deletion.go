package openshift

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deletion/repository/common"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// OpenshiftDeletion handles application deletion operations.
type OpenshiftDeletion struct {
	rt                    runtime.Runtime
	ns                    string
	appRepo               dbrepo.ApplicationRepository
	serviceRepo           dbrepo.ServiceRepository
	componentRepo         dbrepo.ComponentRepository
	serviceDependencyRepo dbrepo.ServiceDependencyRepository
}

// NewOpenshiftDeletion creates a new deletion service instance.
func NewOpenshiftDeletion(
	rt runtime.Runtime,
	ns string,
	appRepo dbrepo.ApplicationRepository,
	serviceRepo dbrepo.ServiceRepository,
	componentRepo dbrepo.ComponentRepository,
	serviceDependencyRepo dbrepo.ServiceDependencyRepository,
) *OpenshiftDeletion {
	return &OpenshiftDeletion{
		rt:                    rt,
		ns:                    ns,
		appRepo:               appRepo,
		serviceRepo:           serviceRepo,
		componentRepo:         componentRepo,
		serviceDependencyRepo: serviceDependencyRepo,
	}
}

// PerformDeletion executes the deletion in four ordered phases:
// 1. Services: helm-uninstall all service releases, then delete their DB records
// 2. Components: helm-uninstall all orphaned component releases, then delete their DB records
// 3. ServiceRuntime: helm-uninstall serving-runtimes for both cpu and spyre-cards.
// 4. Delete PVC: When keepData is false, PVCs in the application namespace are also deleted.
func (s *OpenshiftDeletion) PerformDeletion(ctx context.Context, appID uuid.UUID, services []models.Service, orphanedComponentIDs []uuid.UUID, keepData bool) {
	logger.InfofCtx(ctx, "Deleting OpenShift deployment with application id '%s'  in namespace '%s'\n",
		appID, s.ns)

	// Uninstall application services
	errorMessages := s.deleteServices(ctx, s.ns, appID, services, keepData)

	// Uninstall application components
	componentErr := s.deleteComponents(ctx, s.ns, appID, orphanedComponentIDs, keepData)
	errorMessages = append(errorMessages, componentErr...)

	// Uninstall Serving runtimes
	servingRuntimeErr := s.deleteServingRuntimeRelease(ctx, s.ns)
	errorMessages = append(errorMessages, servingRuntimeErr...)

	if len(errorMessages) > 0 {
		common.HandleDeletionFailure(ctx, s.appRepo, appID, errorMessages)

		return
	}

	// Delete application from DB only if no errors occurred
	if err := s.appRepo.Delete(ctx, appID); err != nil {
		common.HandleStepError(ctx, s.appRepo, appID, "application DB deletion failed", err)

		return
	}

	logger.InfofCtx(ctx, "Application '%s' deleted successfully", appID)
}

// deleteServices uninstalls all service Helm releases sequentially and removes their DB records.
// Collects and returns all errors rather than stopping on the first failure.
func (s *OpenshiftDeletion) deleteServices(ctx context.Context, ns string, appID uuid.UUID, services []models.Service, keepData bool) []string {
	var errorMessages []string

	for _, svc := range services {
		release := catalogutils.HelmReleaseName(appID, svc.CatalogID)
		logger.InfofCtx(ctx, "Uninstalling %s release.", release)

		if err := catalogutils.HelmUninstall(ctx, ns, release); err != nil {
			errMsg := fmt.Sprintf("service %s: helm uninstall failed: %v", svc.ID, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateServiceStatus(ctx, s.serviceRepo, svc.ID, models.ServiceStatusError, fmt.Sprintf("helm uninstall failed: %v", err))

			continue
		}

		if err := s.serviceRepo.Delete(ctx, svc.ID); err != nil {
			errMsg := fmt.Sprintf("service %s: DB deletion failed: %v", svc.ID, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateServiceStatus(ctx, s.serviceRepo, svc.ID, models.ServiceStatusError, fmt.Sprintf("DB deletion failed: %v", err))
		}

		if !keepData {
			if errMsg := s.deleteVolume(ctx, appID, svc.ID.String()); errMsg != "" {
				errorMessages = append(errorMessages, errMsg)
			}
		}
	}

	return errorMessages
}

// deleteComponents uninstalls all orphaned component Helm releases sequentially and removes their DB records.
// Collects and returns all errors rather than stopping on the first failure.
func (s *OpenshiftDeletion) deleteComponents(ctx context.Context, ns string, appID uuid.UUID, componentIDs []uuid.UUID, keepData bool) []string {
	var errorMessages []string

	for _, id := range componentIDs {
		componentData, err := s.componentRepo.GetByID(ctx, id)
		if err != nil {
			errMsg := fmt.Sprintf("component %s: failed to get component from DB: %v", id, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, s.componentRepo, id, models.ComponentStatusError, fmt.Sprintf("failed to get component from DB: %v", err))

			continue
		}

		release := catalogutils.HelmReleaseName(appID, componentData.Type)
		logger.InfofCtx(ctx, "Uninstalling %s release.", release)

		if err := catalogutils.HelmUninstall(ctx, ns, release); err != nil {
			errMsg := fmt.Sprintf("component %s: helm uninstall failed: %v", id, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, s.componentRepo, id, models.ComponentStatusError, fmt.Sprintf("helm uninstall failed: %v", err))

			continue
		}

		if err := s.componentRepo.Delete(ctx, id); err != nil {
			errMsg := fmt.Sprintf("component %s: DB deletion failed: %v", id, err)
			errorMessages = append(errorMessages, errMsg)
			_ = catalogutils.UpdateComponentStatus(ctx, s.componentRepo, id, models.ComponentStatusError, fmt.Sprintf("DB deletion failed: %v", err))
		}

		if !keepData {
			if errMsg := s.deleteVolume(ctx, appID, id.String()); errMsg != "" {
				errorMessages = append(errorMessages, errMsg)
			}
		}
	}

	return errorMessages
}

func (s *OpenshiftDeletion) deleteServingRuntimeRelease(ctx context.Context, ns string) []string {
	var errorMessages []string

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1alpha1",
		Kind:    "ServingRuntimeList",
	})

	response, err := s.rt.ListCRD(list, map[string][]string{
		"label": {constants.PrerequisiteLabelKey},
	})
	if err != nil {
		errMsg := fmt.Sprintf("failed to list serving runtimes: %s", err)
		logger.ErrorlnCtx(ctx, errMsg)

		return []string{errMsg}
	}

	for _, item := range response {
		release := item.Labels[constants.PrerequisiteLabelKey]
		if release == "" {
			release = item.Name
		}

		logger.InfofCtx(ctx, "Uninstalling '%s' serving runtime release.", release)

		if err := catalogutils.HelmUninstall(ctx, ns, release); err != nil {
			errMsg := fmt.Sprintf("serving runtime '%s': helm uninstall failed: %s", release, err)
			errorMessages = append(errorMessages, errMsg)

			continue
		}
	}

	return errorMessages
}

// deleteVolume deletes PVCs associated with the given resource ID when keepData is false.
func (s *OpenshiftDeletion) deleteVolume(ctx context.Context, appID uuid.UUID, resourceID string) string {
	templateLabel := fmt.Sprintf("%s=%s", constants.ApplicationTemplateKey, resourceID)
	logger.InfofCtx(ctx, "Deleting PVC with '%s' label, from %s namespace", templateLabel, s.ns)

	if err := s.rt.DeletePVCs(templateLabel); err != nil {
		errMsg := fmt.Sprintf("failed to delete PVC with label '%s' for app '%s': %s", templateLabel, appID.String(), err.Error())
		logger.ErrorlnCtx(ctx, errMsg)

		return errMsg
	}

	return ""
}

// Made with Bob
