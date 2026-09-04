package repository

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	appservice "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository/application_service"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deletion"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// ValidationError represents a validation error with HTTP status code.
// Re-exported from the applicationservice subpackage so callers use repository.ValidationError.
type ValidationError = appservice.ValidationError

// ListApplicationsRequest re-exported from the applicationservice subpackage.
type ListApplicationsRequest = appservice.ListApplicationsRequest

// DeleteApplicationResponse re-exported from the applicationservice subpackage.
type DeleteApplicationResponse = appservice.DeleteApplicationResponse

// ValidatePaginationParams re-exported from the applicationservice subpackage.
func ValidatePaginationParams(page, pageSize int) (int, int, error) {
	return appservice.ValidatePaginationParams(page, pageSize)
}

// NewApplicationService creates an ApplicationServiceInterface for the given runtime type.
// reg wires the worker registry so that requests carrying a WorkerName are routed to the
// named remote worker over the gRPC CommandStream.
func NewApplicationService(
	appRepo dbrepo.ApplicationRepository,
	serviceRepo dbrepo.ServiceRepository,
	componentRepo dbrepo.ComponentRepository,
	serviceDependencyRepo dbrepo.ServiceDependencyRepository,
	provider *catalog.CatalogProvider,
	runtimeType runtimeTypes.RuntimeType,
	reg stream.WorkerRegistry,
) ApplicationServiceInterface {
	if runtimeType != runtimeTypes.RuntimeTypePodman && runtimeType != runtimeTypes.RuntimeTypeOpenShift {
		panic(fmt.Sprintf("unsupported runtime type %q", runtimeType))
	}

	return &appservice.ApplicationServiceBase{
		AppRepo:               appRepo,
		ServiceRepo:           serviceRepo,
		ComponentRepo:         componentRepo,
		ServiceDependencyRepo: serviceDependencyRepo,
		Provider:              provider,
		DeploymentPlanner:     deployment.NewDeploymentPlanner(provider, componentRepo, runtimeType.String()).WithWorkerRegistry(reg),
		DeploymentExecutor:    deployment.NewDeploymentExecutor(provider, appRepo, serviceRepo, componentRepo).WithWorkerRegistry(reg),
		DeletionExecutor:      deletion.NewDeletionExecutor(appRepo, serviceRepo, componentRepo, serviceDependencyRepo),
		Validator:             validators.NewApplicationValidator(provider),
		RuntimeType:           runtimeType,
		DeploymentRegistry:    appservice.NewDeploymentRegistry(),
		WorkerRegistry:        reg,
	}
}
