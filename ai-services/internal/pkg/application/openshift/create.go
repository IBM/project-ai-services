package openshift

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

func (o *OpenshiftApplication) Create(ctx context.Context, opts types.CreateOptions) error {
	logger.Infof("Creating application '%s' using template '%s'\n", opts.Name, opts.TemplateName)

	// fetch app, namespace and timeout from opts
	app := opts.Name
	namespace := app
	timeout := opts.Timeout

	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{Runtime: vars.RuntimeFactory.GetRuntimeType()})

	s := spinner.New("Setting the operation timeout...")
	s.Start(ctx)
	// populate the operation timeout
	if timeout == 0 {
		// load metadata.yml to read the app metadata
		appMetadata, err := tp.LoadMetadata(opts.TemplateName, false)
		if err != nil {
			return fmt.Errorf("failed to read the app metadata: %w", err)
		}

		// means timeout is not set, then read from the app metadata
		for _, runtime := range appMetadata.Runtimes {
			if runtime.Name == string(runtimeTypes.RuntimeTypeOpenShift) {
				timeout = runtime.Timeout
			}
		}
	}
	s.Stop("Successfully set the operation timeout: " + timeout.String())

	// Load the Chart from assets
	s = spinner.New("Loading the Chart '" + opts.TemplateName + "'...")
	s.Start(ctx)
	chart, err := tp.LoadChart(opts.TemplateName)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}
	s.Stop("Loaded the Chart '" + opts.TemplateName + "' successfully")

	s = spinner.New("Deploying application '" + app + "'...")
	s.Start(ctx)
	// create a new Helm client
	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		return err
	}

	// Check if the app exists
	isAppExist, err := helmClient.IsReleaseExist(app)
	if err != nil {
		return err
	}

	if !isAppExist {
		// if App does not exist then perform install
		logger.Infof("App: %s does not exist, proceeding with install...", app)
		err = helmClient.Install(app, chart, &helm.InstallOpts{Timeout: timeout})
	} else {
		// if App exists, perform upgrade
		logger.Infof("App: %s already exist, proceeding with reconciling...", app)
		err = helmClient.Upgrade(app, chart, &helm.UpgradeOpts{Timeout: timeout})
	}

	if err != nil {
		return fmt.Errorf("failed to perform app installation: %w", err)
	}

	s.Stop("Application '" + opts.Name + "' deployed successfully")

	return nil
}
