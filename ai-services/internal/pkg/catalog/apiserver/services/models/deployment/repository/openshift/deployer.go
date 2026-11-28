package openshift

import (
	"context"
	"fmt"
	"path"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/routes"
	openshiftutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/utils/openshift"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// RouteRegistrar creates the private mTLS routes that connect the control plane to a model on a
// remote worker. Register returns the control-plane egress address of the model.
type RouteRegistrar interface {
	Register(ctx context.Context, rt runtime.Runtime, workerName string, componentID uuid.UUID, upstream string) (string, error)
}

// OpenShiftDeployer deploys a local model on an OpenShift runtime, local or remote over the worker
// stream, as a KServe InferenceService installed from the provider's Helm chart into the shared
// models namespace. It reuses the helpers of the application deployment (Helm manager, chart install,
// prerequisites, readiness wait).
type OpenShiftDeployer struct {
	routes RouteRegistrar
}

// NewOpenShiftDeployer creates a new OpenShiftDeployer. routes registers the private routes of
// models that run on a remote worker.
func NewOpenShiftDeployer(routes RouteRegistrar) *OpenShiftDeployer {
	return &OpenShiftDeployer{routes: routes}
}

// Check accepts every plan: the cluster scheduler, not the control plane, places the predictor on
// a node with the accelerators the chart requests.
func (d *OpenShiftDeployer) Check(context.Context, *types.LocalPlan) error {
	return nil
}

// Prepare installs the prerequisite charts (the KServe ServingRuntimes) into the models
// namespace. There is nothing to pull or download here: KServe pulls the image and fetches the
// weights from the chart's storageUri when the predictor starts.
func (d *OpenShiftDeployer) Prepare(ctx context.Context, plan *types.LocalPlan) error {
	helm := openshiftutils.NewHelmManager(plan.Runtime, catalogutils.ModelsNamespace())

	return openshiftutils.DeployPrerequisites(ctx, helm)
}

// Start installs the provider's chart, waits for the InferenceService to be Ready and returns the
// endpoints of its predictor Service.
//
// A model on the local worker is reached at the predictor's in-cluster address. For a remote
// worker that address is not reachable from the control plane, so the worker mTLS ingress and
// control-plane egress routes are created and the control-plane egress is returned as Egress.
func (d *OpenShiftDeployer) Start(ctx context.Context, plan *types.LocalPlan) (types.ModelEndpoints, error) {
	componentType := plan.Req.ComponentType
	namespace := catalogutils.ModelsNamespace()
	componentKey := fmt.Sprintf("%s/%s", componentType, plan.Req.ProviderID)

	fsys, err := plan.Catalog.GetItemFS(componentKey)
	if err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to get filesystem for component %s: %w", componentKey, err)
	}

	componentPath, err := plan.Catalog.GetCatalogItemPath(componentKey)
	if err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to get component catalog path: %w", err)
	}

	isvcName := fmt.Sprintf("%s-%s", componentType, plan.Values[catalogconstants.InstanceSlugKey])

	chartPath := path.Join(componentPath, plan.Runtime.Type().String())
	helm := openshiftutils.NewHelmManager(plan.Runtime, namespace)
	release := catalogutils.ModelHelmReleaseName(plan.ComponentID, componentType)

	if err := openshiftutils.InstallChart(ctx, helm, release, chartPath, fsys, plan.Values, plan.ComponentID.String()); err != nil {
		return types.ModelEndpoints{}, err
	}

	// The chart names its InferenceService after the component type and instance slug.
	if err := openshiftutils.WaitForInferenceService(ctx, plan.Runtime, fsys, chartPath, isvcName); err != nil {
		return types.ModelEndpoints{}, err
	}

	address := openshiftutils.PredictorAddress(isvcName, namespace)

	return d.routeEndpoints(ctx, plan, address)
}

// routeEndpoints returns the endpoints of a started model. On the local worker the control plane
// reaches the predictor directly. On a remote worker it cannot, so the private routes are created
// and the control-plane egress is the address to use.
func (d *OpenShiftDeployer) routeEndpoints(ctx context.Context, plan *types.LocalPlan, internal string) (types.ModelEndpoints, error) {
	if routes.IsLocal(plan.WorkerName) {
		return types.ModelEndpoints{Internal: internal, Egress: internal}, nil
	}

	egress, err := d.routes.Register(ctx, plan.Runtime, plan.WorkerName, plan.ComponentID, internal)
	if err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to route model %s on worker %q: %w", plan.ComponentID, plan.WorkerName, err)
	}

	return types.ModelEndpoints{Internal: internal, Egress: egress}, nil
}
