package application

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
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

		if experimentalInfo && rt == types.RuntimeTypePodman {
			return renderApplicationInfo(applicationName)
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

	app, err := cliUtils.GetAppByName(appClient, appName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			logger.Warningf("Application: '%s' does not exist", appName)

			return nil
		}

		return err
	}

	application, err := appClient.GetApplication(app.ID)
	if err != nil {
		return fmt.Errorf("failed to get application: %w", err)
	}

	logger.Infoln("Application Name: " + application.Name)
	logger.Infoln("Application Template: " + application.CatalogID)
	logger.Infof("Application Version: " + application.Version)

	tp := templates.NewEmbedTemplateProvider(&assets.ServicesFS)

	logger.Infoln("Info:")
	logger.Infoln("-------")
	logger.Infoln("Day N: ")

	for _, service := range application.Services {
		params := map[string]string{}
		params["STATUS"] = strings.ToLower(service.Status)

		for _, endpoint := range service.Endpoints {
			urlType, urlTypeOk := endpoint["type"].(string)
			url, urlOk := endpoint["url"].(string)
			if urlTypeOk && urlOk {
				params[strings.ToUpper(urlType)+"_URL"] = url
			}
		}

		err = printInfo(tp, params, service.Type)
		if err != nil {
			return fmt.Errorf("failed to load application info: %w", err)
		}
	}

	return nil
}

func printInfo(tp templates.Template, params map[string]string, appTemplate string) error {
	tmpls, err := tp.LoadMdFiles(appTemplate)
	if err != nil {
		return nil
	}
	tmpl, ok := tmpls["info.md"]
	if !ok {
		return nil
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("failed to execute info.md: %w", err)
	}
	value := rendered.String()
	value = strings.ReplaceAll(value, "Day N:\n", "")
	logger.Infof(value)

	return nil
}
