package openshift

import (
	"context"
	"fmt"
	"path"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	openshiftutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/utils/openshift"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
)

// OpenShiftDeployer deploys a local model on an OpenShift runtime, local or remote over the worker
// stream, as a KServe InferenceService installed from the provider's Helm chart. It reuses the
// helpers of the application deployment (Helm manager, chart install, prerequisites, readiness
// wait), with one difference: every model gets a namespace of its own, so that it can be removed
// without touching anything else.
type OpenShiftDeployer struct{}

// NewOpenShiftDeployer creates a new OpenShiftDeployer.
func NewOpenShiftDeployer() *OpenShiftDeployer {
	return &OpenShiftDeployer{}
}

// Check accepts every plan: the cluster scheduler, not the control plane, places the predictor on
// a node with the accelerators the chart requests.
func (d *OpenShiftDeployer) Check(context.Context, *types.LocalPlan) error {
	return nil
}

// Prepare installs the prerequisite charts (the KServe ServingRuntimes) into the model's
// namespace. There is nothing to pull or download here: KServe pulls the image and fetches the
// weights from the chart's storageUri when the predictor starts.
func (d *OpenShiftDeployer) Prepare(ctx context.Context, plan *types.LocalPlan) error {
	helm := openshiftutils.NewHelmManager(plan.Runtime, catalogutils.ModelNamespace(plan.ComponentID))

	return openshiftutils.DeployPrerequisites(ctx, helm)
}

// Start installs the provider's chart, waits for the InferenceService to be Ready and returns the
// address of its predictor Service.
//
// TODO(mTLS): Egress is set to the predictor's in-cluster address, which the control plane can only
// reach when it runs in the same cluster. For a remote worker, create the worker mTLS
// ingress/egress route here and return the control-plane private egress as Egress (see
// types.Deployer).
func (d *OpenShiftDeployer) Start(ctx context.Context, plan *types.LocalPlan) (types.ModelEndpoints, error) {
	componentType := plan.Req.ComponentType
	namespace := catalogutils.ModelNamespace(plan.ComponentID)
	componentKey := fmt.Sprintf("%s/%s", componentType, plan.Req.ProviderID)

	fsys, err := plan.Catalog.GetItemFS(componentKey)
	if err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to get filesystem for component %s: %w", componentKey, err)
	}

	componentPath, err := plan.Catalog.GetCatalogItemPath(componentKey)
	if err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to get component catalog path: %w", err)
	}

	chartPath := path.Join(componentPath, plan.Runtime.Type().String())
	helm := openshiftutils.NewHelmManager(plan.Runtime, namespace)
	release := catalogutils.HelmReleaseName(plan.ComponentID, componentType)

	if err := openshiftutils.InstallChart(ctx, helm, release, chartPath, fsys, plan.Values, plan.ComponentID.String()); err != nil {
		return types.ModelEndpoints{}, err
	}

	// The chart names its InferenceService after the component type.
	if err := openshiftutils.WaitForInferenceServiceReady(ctx, plan.Runtime, fsys, chartPath, componentType); err != nil {
		return types.ModelEndpoints{}, err
	}

	address := openshiftutils.PredictorAddress(componentType, namespace)

	return types.ModelEndpoints{Internal: address, Egress: address}, nil
}
