package application

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
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

Arguments:
  [name] : Application name (required)
	`,
	Example: `  # Display application information from podman runtime
  ai-services application info rag --runtime podman
  
  # Display application information from openshift runtime
  ai-services application info rag --runtime openshift
  `,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// fetch application name
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		ctx := cmd.Context()
		rt := vars.RuntimeFactory.GetRuntimeType()

		// When legacyInfo is true, use the older/stable code path
		if legacyInfo {
			// Create application instance using factory
			factory := application.NewFactory(rt)
			app, err := factory.Create(applicationName)
			if err != nil {
				return fmt.Errorf("failed to create application instance: %w", err)
			}

			opts := appTypes.InfoOptions{
				Name: applicationName,
			}

			return app.Info(ctx, opts)
		}

		// Default: use new implementation using catalog
		return renderApplicationInfo(ctx, applicationName, rt)
	},
}

func init() {
	infoCmd.Flags().BoolVar(&legacyInfo, "legacy", false, "Use legacy application info implementation")
}

func renderApplicationInfo(ctx context.Context, appName string, rt types.RuntimeType) error {
	appClient, err := catalogClient.NewApplicationClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}

	app, err := cliUtils.GetAppByName(ctx, appClient, appName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			logger.Warningf("Application: '%s' does not exist", appName)

			return nil
		}

		return err
	}

	application, err := appClient.GetApplication(ctx, app.ID)
	if err != nil {
		return fmt.Errorf("failed to get application: %w", err)
	}

	appPS, err := appClient.GetApplicationPS(ctx, app.ID)
	if err != nil {
		return fmt.Errorf("failed to get application pods: %w", err)
	}

	logger.Infoln("Application Name: " + application.Name)
	logger.Infoln("Application Template: " + application.CatalogID)
	logger.Infoln("Application Version: " + application.Version)

	return printServicesInfo(ctx, appClient, application.Services, appPS, rt)
}

func printServicesInfo(ctx context.Context, appClient *catalogClient.ApplicationClient, services []catalogTypes.ApplicationService, appPS *catalogTypes.ApplicationPSResponse, rt types.RuntimeType) error {
	logger.Infoln("Info:")
	logger.Infoln("-------")
	logger.Infoln("Day N: ")

	for _, service := range services {
		params := map[string]string{}
		params["SERVICE_NAME"] = service.Type

		uiStatus, apiStatus := getContainerStatus(appPS.Services, service.CatalogID, rt)
		params["UI_STATUS"] = uiStatus
		params["API_STATUS"] = apiStatus

		for _, endpoint := range service.Endpoints {
			urlType, urlTypeOk := endpoint["type"].(string)
			url, urlOk := endpoint["url"].(string)
			if urlTypeOk && urlOk {
				params[strings.ToUpper(urlType)+"_URL"] = url
			}
		}

		rawFiles, err := appClient.GetServiceSteps(ctx, service.CatalogID, rt.String())
		if err != nil {
			return fmt.Errorf("failed to load service steps for '%s': %w", service.CatalogID, err)
		}

		tmpls, err := parseStepsTemplates(rawFiles)
		if err != nil {
			return fmt.Errorf("failed to parse steps templates for '%s': %w", service.CatalogID, err)
		}

		err = printInfo(tmpls, params)
		if err != nil {
			return fmt.Errorf("failed to load application info: %w", err)
		}
	}

	return nil
}

// parseStepsTemplates parses the raw steps file contents (as returned by GetServiceSteps)
// into text/template instances keyed by filename. Only .md files are parsed as templates;
// other files (e.g. vars_file.yaml) are available as raw content under their own key.
func parseStepsTemplates(rawFiles map[string]string) (map[string]*template.Template, error) {
	tmpls := make(map[string]*template.Template, len(rawFiles))

	for name, content := range rawFiles {
		if !strings.HasSuffix(name, ".md") {
			continue
		}

		tmpl, err := template.New(name).Parse(content)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}

		tmpls[name] = tmpl
	}

	return tmpls, nil
}

func getContainerStatus(services []catalogTypes.Pod, catalogID string, rt types.RuntimeType) (string, string) {
	switch rt {
	case types.RuntimeTypePodman:
		return printPodmanContainerStatus(services, catalogID)
	case types.RuntimeTypeOpenShift:
		return printOpenshiftPodStatus(services, catalogID)
	default:
		return "", ""
	}
}

func printPodmanContainerStatus(services []catalogTypes.Pod, catalogID string) (string, string) {
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

/*
1. For each service fetch all labels
2. See if that service has ai-services.io/component-type label
3. If yes, check if value is api or ui
4. Update status accordingly.
*/
func printOpenshiftPodStatus(services []catalogTypes.Pod, catalogID string) (string, string) {
	uiStatus, apiStatus := "", ""

	for _, service := range services {
		if !strings.HasPrefix(service.PodName, catalogID) {
			continue
		}

		component := service.Labels[constants.ComponentLabelKey]
		switch component {
		case "ui":
			if service.Healthy {
				uiStatus = "running"
			}
		case "api":
			if service.Healthy {
				apiStatus = "running"
			}
		}
	}

	return uiStatus, apiStatus
}

func printInfo(tmpls map[string]*template.Template, params map[string]string) error {
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
