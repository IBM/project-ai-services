package openshift

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// Create deploys a new application based on a template.
func (o *OpenshiftApplication) Create(_ context.Context, opts types.CreateOptions) error {
	// fetch app, namespace and timeout from opts
	app := opts.Name
	namespace := app
	timeout := opts.Timeout

	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{Runtime: vars.RuntimeFactory.GetRuntimeType()})

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

	// Load the Chart from assets
	chart, err := tp.LoadChart(opts.TemplateName)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}

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
		err = helmClient.Install(app, chart, &helm.InstallOpts{Timeout: timeout})
	} else {
		// if App exists, perform upgrade
		err = helmClient.Upgrade(app, chart, &helm.UpgradeOpts{Timeout: timeout})
	}

	if err != nil {
		return fmt.Errorf("failed to perform app installation: %w", err)
	}

	logger.Infof("Successfully deployed the App: %s", app)

	return nil
}
