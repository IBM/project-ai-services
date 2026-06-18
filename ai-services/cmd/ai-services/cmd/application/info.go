package application

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	legacyInfo bool
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

		// When legacyInfo is true and runtime is podman, use the older/stable code path
		// For openshift runtime, always use the older/stable code path regardless of legacy flag
		if legacyInfo && rt == types.RuntimeTypePodman {
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
		}

		// Default: use new implementation using catalog
		// For openshift runtime, always use the older/stable code path
		if rt == types.RuntimeTypePodman {
			return renderApplicationInfo(applicationName)
		}

		// OpenShift runtime uses the older implementation
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
	infoCmd.Flags().BoolVar(&legacyInfo, "legacy", false, "Use legacy application info implementation")
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

	appPS, err := appClient.GetApplicationPS(app.ID)
	if err != nil {
		return fmt.Errorf("failed to get application pods: %w", err)
	}

	logger.Infoln("Application Name: " + application.Name)
	logger.Infoln("Application Template: " + application.CatalogID)
	logger.Infoln("Application Version: " + application.Version)

	return printServicesInfo(application.Services, appPS)
}

func printServicesInfo(services []catalogTypes.ApplicationService, appPS *catalogTypes.ApplicationPSResponse) error {
	catalogProvider, err := catalog.NewCatalogProvider()
	if err != nil {
		return fmt.Errorf("failed to create catalog provider: %w", err)
	}

	logger.Infoln("Info:")
	logger.Infoln("-------")
	logger.Infoln("Day N: ")

	for _, service := range services {
		params := map[string]string{}
		params["SERVICE_NAME"] = service.Type

		uiStatus, apiSatatus := getContainerStatus(appPS.Services, service.CatalogID)
		params["UI_STATUS"] = uiStatus
		params["API_STATUS"] = apiSatatus

		for _, endpoint := range service.Endpoints {
			urlType, urlTypeOk := endpoint["type"].(string)
			url, urlOk := endpoint["url"].(string)
			if urlTypeOk && urlOk {
				params[strings.ToUpper(urlType)+"_URL"] = url
			}
		}

		tmpls, err := catalogProvider.LoadServicesMD(service.CatalogID)
		if err != nil {
			return fmt.Errorf("failed to load service md files: %w", err)
		}

		err = printInfo(tmpls, params, service.CatalogID)
		if err != nil {
			return fmt.Errorf("failed to load application info: %w", err)
		}
	}

	return nil
}

func getContainerStatus(services []catalogTypes.Pod, catalogID string) (string, string) {
	uiStatus, apiStatus := "", ""

	for _, servicePod := range services {
		if strings.HasPrefix(servicePod.PodName, catalogID) {
			for _, podContainer := range servicePod.Containers {
				// TODO: Set the container status in info.md generically
				uiContainerName := fmt.Sprintf("%s-ui", servicePod.PodName)
				apiContainerName := ""
				if strings.Contains(podContainer.Name, "backend-server") {
					apiContainerName = podContainer.Name
				} else {
					apiContainerName = fmt.Sprintf("%s-%s-api", servicePod.PodName, catalogID)
				}

				if podContainer.Name == uiContainerName && podContainer.Healthy {
					uiStatus = "running"
				}
				if podContainer.Name == apiContainerName && podContainer.Healthy {
					apiStatus = "running"
				}
			}
		}
	}

	return uiStatus, apiStatus
}

func printInfo(tmpls map[string]*template.Template, params map[string]string, appTemplate string) error {
	tmpl, ok := tmpls["info.md"]
	if !ok {
		logger.Warningf("failed to find info.md template")

		return nil
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("failed to execute info.md: %w", err)
	}
	value := rendered.String()
	value = strings.ReplaceAll(value, "Day N:\n", "")
	logger.Infoln(value)

	return nil
}
