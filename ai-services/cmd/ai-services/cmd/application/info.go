package application

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
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

	return printServicesInfo(ctx, appClient, application.Services, appPS, app.ID, rt)
}

func printServicesInfo(ctx context.Context, appClient *catalogClient.ApplicationClient, services []catalogTypes.ApplicationService, appPS *catalogTypes.ApplicationPSResponse, appID string, rt types.RuntimeType) error {
	logger.Infoln("Info:")
	logger.Infoln("-------")
	logger.Infoln("Day N: ")

	// InstanceSlug is derived from the application UUID — same as at deploy time
	instanceSlug := catalogUtils.GenerateInstanceSlug(appID)

	for _, service := range services {
		params := map[string]string{}
		params["SERVICE_NAME"] = service.Type

		// Populate endpoint URLs from the service endpoints stored in the DB
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

		// Populate status params generically from vars_file.yaml
		if err := populateStatusFromVarsFile(rawFiles, params, appPS.Services, instanceSlug, rt); err != nil {
			logger.WarningfCtx(ctx, "failed to populate status for '%s': %v\n", service.CatalogID, err)
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

// varsFileContainers is the minimal shape of vars_file.yaml needed for status resolution.
type varsFileContainers struct {
	Containers []struct {
		Name     string `yaml:"name"`
		Workload string `yaml:"workload"`
		Format   string `yaml:"format"`
		Alias    string `yaml:"alias"`
	} `yaml:"containers"`
}

// populateStatusFromVarsFile reads the containers section of vars_file.yaml, renders each
// name template with InstanceSlug, then resolves container health from appPods.
//
// Podman: name is the full rendered container name (e.g. "chat-bot-{slug}-ui"), matched
// exactly against PodContainer.Name. The workload field is ignored.
//
// OpenShift: workload is the deployment name (e.g. "chat-bot-ui"); pod.PodName is
// prefix-matched against workload and container name is exact-matched against c.Name.
//
// For each entry with Format ".Status", alias → "running" (healthy) or "" is set in params.
func populateStatusFromVarsFile(rawFiles map[string]string, params map[string]string, appPods []catalogTypes.Pod, instanceSlug string, rt types.RuntimeType) error {
	raw, ok := rawFiles["vars_file.yaml"]
	if !ok {
		return nil
	}

	// Render the vars_file template — {{ .InstanceSlug }} expands to the computed slug
	var rendered bytes.Buffer
	tmpl, err := template.New("vars").Parse(raw)
	if err != nil {
		return fmt.Errorf("parse vars_file.yaml: %w", err)
	}
	if err := tmpl.Execute(&rendered, map[string]string{"InstanceSlug": instanceSlug}); err != nil {
		return fmt.Errorf("execute vars_file.yaml: %w", err)
	}

	var vf varsFileContainers
	if err := yaml.Unmarshal(rendered.Bytes(), &vf); err != nil {
		return fmt.Errorf("unmarshal vars_file.yaml: %w", err)
	}

	for _, c := range vf.Containers {
		// Only handle ".Status" format entries — those drive a status alias
		if strings.TrimSpace(c.Format) != ".Status" {
			continue
		}
		alias := strings.ReplaceAll(c.Alias, "-", "_")
		params[alias] = resolveContainerStatus(c.Name, c.Workload, appPods, rt)
	}

	return nil
}

// resolveContainerStatus returns "running" when the named container is healthy.
//
// Podman: name is the full container name matched exactly against PodContainer.Name.
//
// OpenShift: workload is the deployment name; pod.PodName must have the workload as a
// prefix (OCP pod names are "{workload}-{replicaSetHash}-{podHash}"), and container
// name is exact-matched against PodContainer.Name (ContainerStatus.Name from pod spec).
func resolveContainerStatus(name, workload string, appPods []catalogTypes.Pod, rt types.RuntimeType) string {
	switch rt {
	case types.RuntimeTypePodman:
		for _, pod := range appPods {
			for _, c := range pod.Containers {
				if c.Name == name && c.Healthy {
					return "running"
				}
			}
		}
	case types.RuntimeTypeOpenShift:
		for _, pod := range appPods {
			if strings.HasPrefix(pod.PodName, workload+"-") {
				for _, c := range pod.Containers {
					if c.Name == name && c.Healthy {
						return "running"
					}
				}
			}
		}
	}

	return ""
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
