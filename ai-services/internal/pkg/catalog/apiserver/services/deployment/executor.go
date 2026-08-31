package deployment

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/repository/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/repository/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	openshiftRuntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	podmanRuntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// DeploymentExecutor orchestrates the execution of an already-planned
// deployment, routing to the correct runtime-specific deployer.
type DeploymentExecutor struct {
	catalogProvider *catalog.CatalogProvider
	appRepo         repository.ApplicationRepository
	serviceRepo     repository.ServiceRepository
	componentRepo   repository.ComponentRepository
	// workerRegistry is used to resolve a RemoteRuntime for worker deployments.
	workerRegistry stream.WorkerRegistry
}

// NewDeploymentExecutor creates a new DeploymentExecutor instance.
func NewDeploymentExecutor(
	catalogProvider *catalog.CatalogProvider,
	appRepo repository.ApplicationRepository,
	serviceRepo repository.ServiceRepository,
	componentRepo repository.ComponentRepository,
) *DeploymentExecutor {
	return &DeploymentExecutor{
		catalogProvider: catalogProvider,
		appRepo:         appRepo,
		serviceRepo:     serviceRepo,
		componentRepo:   componentRepo,
	}
}

// WithWorkerRegistry wires the worker registry into the executor so it can
// resolve a RemoteRuntime for worker deployments. Must be called before any
// deployment that targets a non-local worker.
func (e *DeploymentExecutor) WithWorkerRegistry(reg stream.WorkerRegistry) *DeploymentExecutor {
	e.workerRegistry = reg

	return e
}

// ExecuteWithPlan runs the deployment described by plan. DB records have
// already been written by the caller before this is invoked.
func (e *DeploymentExecutor) ExecuteWithPlan(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
	runtimeType types.RuntimeType,
) error {
	if err := e.executeDeployment(ctx, plan, req, runtimeType); err != nil {
		return fmt.Errorf("failed to execute deployment: %w", err)
	}

	return nil
}

// executeDeployment routes to the correct deployer based on plan.WorkerName.
// WorkerName is always set by PlanDeployment.
func (e *DeploymentExecutor) executeDeployment(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
	runtimeType types.RuntimeType,
) error {
	// ── Remote worker deployment ──────────────────────────────────────────────
	// TODO Remove the check when remote deployment is by default
	// and the remaining code will be dead
	if plan.WorkerName != "" {
		return e.executeWorkerDeployment(ctx, plan, req)
	}

	// ── Local deployment ──────────────────────────────────────────────────────
	switch runtimeType {
	case types.RuntimeTypePodman:
		return e.executePodmanDeployment(ctx, plan, req)
	case types.RuntimeTypeOpenShift:
		return e.executeOpenShiftDeployment(ctx, plan, req)
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// executeWorkerDeployment dispatches a deployment to a named remote worker.
// Resolves the worker's runtime type from the registry, builds a RemoteRuntime
// that forwards all calls over the gRPC CommandStream, then delegates to the
// appropriate deployer (Podman or OpenShift).
func (e *DeploymentExecutor) executeWorkerDeployment(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
) error {
	// Connectivity was already confirmed by ValidateWorker; just read the type.
	rtStr, _ := e.workerRegistry.WorkerRuntimeType(plan.WorkerName)
	workerType := types.RuntimeType(rtStr)

	// RemoteRuntime forwards every call over the gRPC CommandStream — the
	// deployer does not need to know it is talking to a remote machine.
	rt, err := runtime.NewRuntimeFactory(workerType).CreateRemote(plan.WorkerName, e.workerRegistry)
	if err != nil {
		return fmt.Errorf("create remote runtime for worker %q: %w", plan.WorkerName, err)
	}

	switch workerType {
	case types.RuntimeTypePodman:
		return e.runPodmanDeployer(ctx, plan, req, rt)
	case types.RuntimeTypeOpenShift:
		return e.runOpenShiftDeployer(ctx, plan, req, rt)
	default:
		return fmt.Errorf("worker %q has unsupported runtime type %q", plan.WorkerName, workerType)
	}
}

// runPodmanDeployer creates a PodmanDeployer backed by the RemoteRuntime and
// injects the worker's registration metadata (domain suffix, HTTPS port, base
// dir) via SetPodmanWorkerConfig so the deployer uses the correct values
// instead of local environment variables.
func (e *DeploymentExecutor) runPodmanDeployer(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
	rt runtime.Runtime,
) error {
	deployer := podman.NewPodmanDeployer(rt, e.catalogProvider, e.appRepo, e.serviceRepo, e.componentRepo)

	meta, _ := e.workerRegistry.WorkerMetadata(plan.WorkerName)
	deployer.SetPodmanWorkerConfig(podman.PodmanWorkerConfig{
		DomainSuffix: meta[workerconstants.MetaKeyDomainSuffix],
		HTTPSPort:    meta[workerconstants.MetaKeyHTTPSPort],
		BaseDir:      meta[workerconstants.MetaKeyBaseDir],
	})

	return deployer.ExecuteDeployment(ctx, plan, req)
}

// runOpenShiftDeployer creates an OpenShiftDeployer backed by the RemoteRuntime
// and runs the deployment. OpenShift workers manage routes natively via the
// Kubernetes API so no config is needed.
func (e *DeploymentExecutor) runOpenShiftDeployer(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
	rt runtime.Runtime,
) error {
	deployer := openshift.NewOpenShiftDeployer(rt, e.catalogProvider, e.appRepo, e.serviceRepo, e.componentRepo)

	return deployer.ExecuteDeployment(ctx, plan, req)
}

// executePodmanDeployment executes deployment for local Podman runtime.
func (e *DeploymentExecutor) executePodmanDeployment(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
) error {
	// Initialize Podman runtime client
	rt, err := podmanRuntime.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to initialize Podman runtime: %w", err)
	}

	// Create podman deployer
	deployer := podman.NewPodmanDeployer(
		rt,
		e.catalogProvider,
		e.appRepo,
		e.serviceRepo,
		e.componentRepo,
	)

	// Execute deployment - handles both architectures and standalone services
	return deployer.ExecuteDeployment(ctx, plan, req)
}

// executeOpenShiftDeployment executes deployment for the OpenShift runtime via Helm.
func (e *DeploymentExecutor) executeOpenShiftDeployment(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
) error {
	// Initialize OpenShift runtime client scoped to the application's namespace
	// so that ListRoutes, ListPods etc. query the correct namespace.
	ns := catalogutils.AppNamespace(plan.ApplicationID)
	rt, err := openshiftRuntime.NewOpenshiftClientWithNamespace(ns)
	if err != nil {
		return fmt.Errorf("failed to initialize OpenShift runtime: %w", err)
	}

	// Create openshift deployer
	deployer := openshift.NewOpenShiftDeployer(
		rt,
		e.catalogProvider,
		e.appRepo,
		e.serviceRepo,
		e.componentRepo,
	)

	return deployer.ExecuteDeployment(ctx, plan, req)
}

// Made with Bob
