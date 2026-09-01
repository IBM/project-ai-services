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

// NewApplicationService creates the appropriate ApplicationServiceInterface implementation
// based on the runtime type. It is the single construction point for the apiserver.
//
// reg wires the worker registry into the deployment executor so that requests
// carrying a non-empty WorkerName are routed to the named remote worker over the
// gRPC CommandStream. This applies to both Podman and OpenShift local runtimes.
//
// connectorRepo and datasourceSvc enable the two datasource-in-deploy-flow features:
//   - connectorRepo is wired into ApplicationValidator so that connector refs supplied
//     in the create request are validated against the DB before deployment begins.
//   - datasourceSvc is wired into ApplicationServiceBase so that after a successful
//     deployment the connectors are automatically attached to eligible services.
//
// Both parameters are optional (nil is accepted); when nil the respective feature is
// skipped silently, preserving backward compatibility for test callers.
func NewApplicationService(
	appRepo dbrepo.ApplicationRepository,
	serviceRepo dbrepo.ServiceRepository,
	componentRepo dbrepo.ComponentRepository,
	serviceDependencyRepo dbrepo.ServiceDependencyRepository,
	provider *catalog.CatalogProvider,
	runtimeType runtimeTypes.RuntimeType,
	reg stream.WorkerRegistry,
	connectorRepo dbrepo.ConnectorRepository,
	datasourceSvc appservice.DatasourceConnector,
) ApplicationServiceInterface {
	validator := validators.NewApplicationValidator(provider)
	if connectorRepo != nil {
		validator = validator.WithConnectorRepo(connectorRepo)
	}

	base := appservice.ApplicationServiceBase{
		AppRepo:               appRepo,
		ServiceRepo:           serviceRepo,
		ComponentRepo:         componentRepo,
		ServiceDependencyRepo: serviceDependencyRepo,
		Provider:              provider,
		DeploymentPlanner:     deployment.NewDeploymentPlanner(provider, componentRepo).WithWorkerRegistry(reg),
		DeploymentExecutor:    deployment.NewDeploymentExecutor(provider, appRepo, serviceRepo, componentRepo).WithWorkerRegistry(reg),
		DeletionExecutor:      deletion.NewDeletionExecutor(appRepo, serviceRepo, componentRepo, serviceDependencyRepo),
		Validator:             validator,
		DatasourceService:     datasourceSvc,
	}

	switch runtimeType {
	case runtimeTypes.RuntimeTypePodman:
		return appservice.NewPodmanApplicationService(base)
	case runtimeTypes.RuntimeTypeOpenShift:
		return appservice.NewOpenShiftApplicationService(base)
	default:
		panic(fmt.Sprintf("unsupported runtime type %q", runtimeType))
	}
}
