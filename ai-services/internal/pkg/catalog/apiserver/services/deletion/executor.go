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
	openshiftRuntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	podmanRuntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
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

func (e *DeletionExecutor) Execute(
	ctx context.Context,
	appID uuid.UUID,
	services []models.Service,
	orphanedComponentIDs []uuid.UUID,
	keepData bool,
	runtimeType types.RuntimeType,
) error {
	// Execute deployment based on runtime type using the provided plan
	switch runtimeType {
	case types.RuntimeTypePodman:
		return e.executePodmanDeletion(ctx, appID, services, orphanedComponentIDs, keepData)
	case types.RuntimeTypeOpenShift:
		return e.executeOpenShiftDeletion(ctx, appID, services, orphanedComponentIDs, keepData)
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// executePodmanDeletion executes application deletion for Podman runtime.
func (e *DeletionExecutor) executePodmanDeletion(
	ctx context.Context,
	appID uuid.UUID,
	services []models.Service,
	orphanedComponentIDs []uuid.UUID,
	keepData bool,
) error {
	// Initialize Podman runtime client
	rt, err := podmanRuntime.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to initialize Podman runtime: %w", err)
	}

	// Create podman deployer
	deleteService := podman.NewPodmanDeletion(
		rt,
		e.appRepo,
		e.serviceRepo,
		e.componentRepo,
		e.serviceDependencyRepo,
	)

	deleteService.PerformDeletion(ctx, appID, services, orphanedComponentIDs, keepData)

	return nil
}

// executeOpenShiftDeletion executes application deletion for the OpenShift runtime via Helm.
func (e *DeletionExecutor) executeOpenShiftDeletion(
	ctx context.Context,
	appID uuid.UUID,
	services []models.Service,
	orphanedComponentIDs []uuid.UUID,
	keepData bool,
) error {
	ns := catalogutils.AppNamespace(appID)
	rt, err := openshiftRuntime.NewOpenshiftClientWithNamespace(ns)
	if err != nil {
		return fmt.Errorf("failed to initialize openshift runtime: %w", err)
	}

	deletionService := openshift.NewOpenshiftDeletion(
		rt,
		ns,
		e.appRepo,
		e.serviceRepo,
		e.componentRepo,
		e.serviceDependencyRepo,
	)

	deletionService.PerformDeletion(ctx, appID, services, orphanedComponentIDs, keepData)

	return nil
}
