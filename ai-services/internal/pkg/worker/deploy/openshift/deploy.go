package openshift

import (
	"context"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	helmutils "github.com/project-ai-services/ai-services/internal/pkg/utils/helm"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
	"helm.sh/helm/v4/pkg/chart"
)

var (
	defaultHelmTimeout = 10 * time.Minute
)

// DeployWorker orchestrates the full deployment of the worker gRPC stream pod on OpenShift.
// It loads the Helm chart from the embedded assets, prepares the chart values using the
// provided gateway address and authentication token, and installs or upgrades the Helm
// release in the worker namespace.
func DeployWorker(ctx context.Context, opts workertypes.OpenshiftWorkerOptions) error {
	namespace := workerconstants.WorkerAppName
	logger.Infof("Deploying worker grpc stream to OpenShift in namespace '%s'\n", namespace)

	tp := templates.NewEmbedTemplateProvider(&assets.WorkerFS, "")

	// Step 1: Load the Chart from assets/worker/openshift
	chartData, err := helmutils.LoadChart(ctx, tp, workerconstants.WorkerAppTemplate)
	if err != nil {
		return err
	}

	// Step 2: Prepare values with argument parameters
	values, err := prepareValues(tp, opts)
	if err != nil {
		return err
	}

	// Step 3: Deploy the worker using Helm
	if err := deployWorkerHelm(ctx, chartData, values, namespace); err != nil {
		return err
	}

	return nil
}

// prepareValues builds the Helm values map for the worker chart.
// It generates the argument parameters from the gateway address and token,
// then merges them with the chart's default values via the template provider.
func prepareValues(tp templates.Template, opts workertypes.OpenshiftWorkerOptions) (map[string]any, error) {
	// Generate argument parameters
	argParams := map[string]string{
		workerconstants.ArgParamWorkerToken:       opts.Token,
		workerconstants.ArgParamWorkerGatewayAddr: opts.GatewayAddr,
	}

	// Load values from chart with overrides
	values, err := tp.LoadValues(workerconstants.WorkerAppTemplate, nil, argParams)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare values: %w", err)
	}

	return values, nil
}

// deployWorkerHelm installs or upgrades the worker Helm release in the given namespace.
// It creates a namespaced Helm client, performs an install-or-upgrade operation with the
// provided chart and values, and enforces a timeout of helmTimeout.
func deployWorkerHelm(ctx context.Context, chartData chart.Charter, values map[string]any, namespace string) error {
	s := spinner.New("Deploying worker to OpenShift...")

	s.Start(ctx)

	// Create Helm client for the worker namespace
	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		s.Fail("failed to create Helm client")

		return fmt.Errorf("failed to create Helm client: %w", err)
	}

	if err := helmClient.InstallOrUpgrade(ctx, workerconstants.WorkerHelmReleaseName, chartData, values, defaultHelmTimeout); err != nil {
		s.Fail("failed to deploy worker")

		return fmt.Errorf("failed to deploy worker: %w", err)
	}

	s.Stop("Worker deployed successfully")

	return nil
}
