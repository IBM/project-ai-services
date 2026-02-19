package openshift

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

func (o *OpenshiftApplication) Create(_ context.Context, opts types.CreateOptions) error {
	// fetch app and namespace from opts
	app := opts.Name
	namespace := app

	// Load the Chart from assets
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{Runtime: vars.RuntimeFactory.GetRuntimeType()})
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
		err = helmClient.Install(app, chart, nil)
	} else {
		// if App exists, perform upgrade
		err = helmClient.Upgrade(app, chart, nil)
	}

	if err != nil {
		return fmt.Errorf("failed to perform app installation: %w", err)
	}

	logger.Infof("Successfully deployed the App: %s", app)

	return nil
}
