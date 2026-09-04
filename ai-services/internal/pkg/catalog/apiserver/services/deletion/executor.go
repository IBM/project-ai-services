package deletion

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deletion/repository/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deletion/repository/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// DeletionExecutor orchestrates the complete application deletion process.
type DeletionExecutor struct {
	appRepo               repository.ApplicationRepository
	serviceRepo           repository.ServiceRepository
	componentRepo         repository.ComponentRepository
	serviceDependencyRepo repository.ServiceDependencyRepository
}

// NewDeletionExecutor creates a new DeletionExecutor instance.
func NewDeletionExecutor(
	appRepo repository.ApplicationRepository,
	serviceRepo repository.ServiceRepository,
	componentRepo repository.ComponentRepository,
	serviceDependencyRepo repository.ServiceDependencyRepository,
) *DeletionExecutor {
	return &DeletionExecutor{
		appRepo:               appRepo,
		serviceRepo:           serviceRepo,
		componentRepo:         componentRepo,
		serviceDependencyRepo: serviceDependencyRepo,
	}
}

// Execute carries out the deletion using the already-resolved runtime.
// The caller (ApplicationServiceBase.executeDeletionAsync) is responsible for
// resolving the correct runtime — local or RemoteRuntime for worker apps.
func (e *DeletionExecutor) Execute(
	ctx context.Context,
	appID uuid.UUID,
	services []models.Service,
	orphanedComponentIDs []uuid.UUID,
	keepData bool,
	rt runtime.Runtime,
) error {
	switch rt.Type() {
	case types.RuntimeTypePodman:
		return e.executePodmanDeletion(ctx, appID, services, orphanedComponentIDs, keepData, rt)
	case types.RuntimeTypeOpenShift:
		return e.executeOpenShiftDeletion(ctx, appID, services, orphanedComponentIDs, keepData, rt)
	default:
		return fmt.Errorf("unsupported runtime type: %s", rt.Type())
	}
}

// executePodmanDeletion performs deletion for the Podman runtime.
func (e *DeletionExecutor) executePodmanDeletion(
	ctx context.Context,
	appID uuid.UUID,
	services []models.Service,
	orphanedComponentIDs []uuid.UUID,
	keepData bool,
	rt runtime.Runtime,
) error {
	podman.NewPodmanDeletion(
		rt,
		e.appRepo,
		e.serviceRepo,
		e.componentRepo,
		e.serviceDependencyRepo,
	).PerformDeletion(ctx, appID, services, orphanedComponentIDs, keepData)

	return nil
}

// executeOpenShiftDeletion performs deletion for the OpenShift runtime.
func (e *DeletionExecutor) executeOpenShiftDeletion(
	ctx context.Context,
	appID uuid.UUID,
	services []models.Service,
	orphanedComponentIDs []uuid.UUID,
	keepData bool,
	rt runtime.Runtime,
) error {
	openshift.NewOpenshiftDeletion(
		rt,
		catalogutils.AppNamespace(appID),
		e.appRepo,
		e.serviceRepo,
		e.componentRepo,
		e.serviceDependencyRepo,
	).PerformDeletion(ctx, appID, services, orphanedComponentIDs, keepData)

	return nil
}
