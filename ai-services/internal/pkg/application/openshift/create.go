package openshift

import (
	"context"
	"fmt"
	"time"

	"helm.sh/helm/v4/pkg/chart"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

func (o *OpenshiftApplication) Create(ctx context.Context, opts types.CreateOptions) error {
	logger.Infof("Creating application '%s' using template '%s'\n", opts.Name, opts.TemplateName)

	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{Runtime: vars.RuntimeFactory.GetRuntimeType()})

	// Step1: Fetch the operation timeout
	timeout, err := getOperationTimeout(ctx, tp, opts)
	if err != nil {
		return err
	}

	// Step2: Load the Chart from assets for given app template
	chart, err := loadCharts(ctx, tp, opts)
	if err != nil {
		return err
	}

	// Step3: Deploy Application
	if err := deployApp(ctx, chart, timeout, opts); err != nil {
		return err
	}

	//nolint:godox
	// TODO: Step4: print Next steps

	return nil
}

func getOperationTimeout(ctx context.Context, tp templates.Template, opts types.CreateOptions) (time.Duration, error) {
	s := spinner.New("Setting the operation timeout...")

	s.Start(ctx)
	timeout := opts.Timeout
	// populate the operation timeout if its either not set or set negatively
	if timeout <= 0 {
		// load metadata.yml to read the app metadata
		appMetadata, err := tp.LoadMetadata(opts.TemplateName, false)
		if err != nil {
			s.Fail("failed to read the app metadata")

			return 0, fmt.Errorf("failed to read the app metadata: %w", err)
		}

		timeout = appMetadata.Openshift.Timeout
	}
	s.Stop("Successfully set the operation timeout: " + timeout.String())

	return timeout, nil
}

func loadCharts(ctx context.Context, tp templates.Template, opts types.CreateOptions) (chart.Charter, error) {
	s := spinner.New("Loading the Chart '" + opts.TemplateName + "'...")

	s.Start(ctx)
	chart, err := tp.LoadChart(opts.TemplateName)
	if err != nil {
		s.Fail("failed to load the Chart")

		return nil, fmt.Errorf("failed to load the chart: %w", err)
	}
	s.Stop("Loaded the Chart '" + opts.TemplateName + "' successfully")

	return chart, nil
}

func deployApp(ctx context.Context, chart chart.Charter, timeout time.Duration, opts types.CreateOptions) error {
	// Fetch app name and derive namespace
	app := opts.Name
	namespace := app

	s := spinner.New("Deploying application '" + app + "'...")

	s.Start(ctx)
	// Create a new Helm client
	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		s.Fail("failed to create application")

		return err
	}

	// Check if the app exists
	isAppExist, err := helmClient.IsReleaseExist(app)
	if err != nil {
		s.Fail("failed to create application")

		return err
	}

	if !isAppExist {
		// if App does not exist then perform install
		logger.Infof("App: %s does not exist, proceeding with install...", app)
		err = helmClient.Install(app, chart, &helm.InstallOpts{Timeout: timeout})
	} else {
		// if App exists, perform upgrade so that the actual state of the app meets the desired state
		logger.Infof("App: %s already exist, proceeding with reconciling...", app)
		err = helmClient.Upgrade(app, chart, &helm.UpgradeOpts{Timeout: timeout})
	}
	if err != nil {
		s.Fail("failed to create application")

		return fmt.Errorf("failed to perform app installation: %w", err)
	}

	s.Stop("Application '" + app + "' deployed successfully")

	return nil
}
