package application

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	experimentalInfo bool
)

var infoCmd = &cobra.Command{
	Use:   "info [name]",
	Short: "Application info",
	Long: `Displays the information about the running application
		Arguments
		- [name]: Application name (Required)
	`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// fetch application name
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		rt := vars.RuntimeFactory.GetRuntimeType()

		if experimentalInfo {

			return nil
		}

		// Create application instance using factory
		factory := application.NewFactory(rt)
		app, err := factory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		opts := appTypes.InfoOptions{
			Name: applicationName,
		}

		return app.Info(opts)
	},
}

func init() {
	infoCmd.Flags().BoolVar(&experimentalInfo, "experimental", false, "Include experimental application info")
}

func renderApplicationInfo(appName string) error {
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}

	application, err := cliUtils.GetAppByName(appClient, appName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			logger.Warningf("Application: '%s' does not exist")
			return nil
		}
		return err
	}

	// applicationPS, err := appClient.GetApplicationPS(application.ID)
	// if err != nil {
	// 	return err
	// }

	logger.Infoln("Application Name: " + application.Name)

	logger.Infoln("Application Template: " + application.CatalogID)

	logger.Infof("Application Version: " + application.Version)

	tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)
	rt, err := vars.RuntimeFactory.Create("")
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	if err := helpers.PrintInfo(tp, rt, appName, application.CatalogID); err != nil {
		// not failing if overall info command, if we cannot display Info
		logger.Errorf("failed to display info: %v\n", err)

		return nil
	}

}
