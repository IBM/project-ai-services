package application

import (
	"fmt"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	appFlags "github.com/project-ai-services/ai-services/internal/pkg/cli/constants/application"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

var (
	output         string
	experimentalPs bool
)

func isOutputWide() bool {
	return strings.ToLower(output) == "wide"
}

var psCmd = &cobra.Command{
	Use:   "ps [name]",
	Short: "Lists all or specified running application(s)",
	Long: `Retrieves information about all the running applications if no name is provided
Lists information about a specific application if the name is provided
Arguments
  [name]: Application name (optional)
`,
	Args: cobra.MaximumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Build and run flag validator
		flagValidator := buildPsFlagValidator()

		return flagValidator.Validate(cmd)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		var applicationName string
		if len(args) > 0 {
			applicationName = args[0]
		}

		rt := vars.RuntimeFactory.GetRuntimeType()

		// When experimentalTemplates is true and runtime is podman, use experimental catalog ps api
		// For openshift runtime, always use the older/stable code path regardless of experimental flag
		if experimentalPs && rt == types.RuntimeTypePodman {
			return processApplication(applicationName)
		}

		// Create application instance using factory
		factory := application.NewFactory(rt)
		app, err := factory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		opts := appTypes.ListOptions{
			ApplicationName: applicationName,
			OutputWide:      isOutputWide(),
		}

		_, err = app.List(opts)
		if err != nil {
			return fmt.Errorf("failed to fetch application: %w", err)
		}

		return nil
	},
}

func init() {
	initPsCommonFlags()
}

func initPsCommonFlags() {
	psCmd.Flags().BoolVar(
		&experimentalPs,
		"experimental",
		false,
		"Include experimental application templates",
	)

	psCmd.Flags().StringVarP(
		&output,
		appFlags.Ps.Output,
		"o",
		"",
		"Output format (e.g., wide)",
	)
}

// buildPsFlagValidator creates and configures the flag validator for the ps command.
func buildPsFlagValidator() *flagvalidator.FlagValidator {
	runtimeType := vars.RuntimeFactory.GetRuntimeType()

	builder := flagvalidator.NewFlagValidatorBuilder(runtimeType)

	// Register common flags
	builder.
		AddCommonFlag(appFlags.Ps.Output, nil)

	return builder.Build()
}

func processApplication(appName string) error {
	// Create application client with server URL and token
	appClient, err := catalogClient.NewApplicationClient()

	appIDList, err := getAppIDList(appClient, appName)
	if err != nil {
		return err
	}
	if len(appIDList) == 0 {
		return fmt.Errorf("no application found with name %s", appName)
	}

	return render(appClient, appIDList)
}

// getAppIDList retrieves application ID(s) from the catalog API.
// If appName is empty, returns all application IDs.
// If appName is provided, returns the ID of the matching application.
func getAppIDList(appClient *catalogClient.ApplicationClient, appName string) ([]string, error) {
	// List all applications
	resp, err := appClient.ListApplications(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch applications: %w", err)
	}

	// If appName is empty, return all IDs
	if appName == "" {
		ids := make([]string, 0, len(resp.Data))
		for _, app := range resp.Data {
			ids = append(ids, app.ID)
		}

		return ids, nil
	}

	// If appName is provided, find matching application and return its ID
	for _, app := range resp.Data {
		if app.Name == appName {
			return []string{app.ID}, nil
		}
	}

	// No matching application found
	return nil, fmt.Errorf("application with name '%s' not found", appName)
}

// render retrieves and processes the PS information for multiple application IDs.
// It fetches the process status for each application using the catalog API and prints the results in tabular format.
func render(appClient *catalogClient.ApplicationClient, appIDList []string) error {
	// Create table writer
	printer := utils.NewTableWriter()
	defer printer.CloseTableWriter()

	// Set table headers based on output format
	outputWide := isOutputWide()
	setTableHeaders(printer, outputWide)

	// Process each application ID
	for _, appID := range appIDList {
		// Get PS information for the application
		psResp, err := appClient.GetApplicationPS(appID)
		if err != nil {
			logger.Errorf("Error fetching PS for application %s: %v\n", appID, err)

			continue
		}

		// Process services pods
		for _, pod := range psResp.Services {
			rows := buildPodRowFromAPI(psResp.Name, pod, outputWide)
			printer.AppendRow(rows...)
		}

		// Process components pods
		for _, pod := range psResp.Components {
			rows := buildPodRowFromAPI(psResp.Name, pod, outputWide)
			printer.AppendRow(rows...)
		}
	}

	return nil
}

// setTableHeaders sets the table headers based on output format.
func setTableHeaders(printer *utils.Printer, outputWide bool) {
	if outputWide {
		printer.SetHeaders("APPLICATION NAME", "POD ID", "POD NAME", "STATUS", "CREATED", "CONTAINERS")
	} else {
		printer.SetHeaders("APPLICATION NAME", "POD NAME", "STATUS")
	}
}

// buildPodRowFromAPI builds a table row from API response data.
func buildPodRowFromAPI(appName string, pod catalogTypes.Pod, wideOutput bool) []string {
	status := getPodStatusFromAPI(pod)

	// If wide option flag is not set, return appName, podName and status only
	if !wideOutput {
		return []string{appName, pod.PodName, status}
	}

	containerNames := getContainerNamesFromAPI(pod)

	// Parse the Created string and convert to TimeAgo format
	created := "N/A"
	if pod.Created != "" {
		// Try to parse the Created timestamp
		parsedTime, err := time.Parse(catalogConstants.RFC3339WithTimezone, pod.Created)
		if err == nil {
			created = utils.TimeAgo(parsedTime)
		} else {
			// If parsing fails, use the original string
			created = pod.Created
		}
	}

	return []string{
		appName,
		pod.PodID[:12],
		pod.PodName,
		status,
		created,
		strings.Join(containerNames, ", "),
	}
}

// getPodStatusFromAPI determines the pod status from API response.
func getPodStatusFromAPI(pod catalogTypes.Pod) string {
	status := string(pod.Status)

	// If the pod is running, check if it's healthy
	if strings.ToLower(status) == "running" {
		if pod.Healthy {
			status += fmt.Sprintf(" (%s)", constants.Ready)
		} else {
			status += fmt.Sprintf(" (%s)", constants.NotReady)
		}
	}

	return status
}

// getContainerNamesFromAPI extracts container names with their status from API response.
func getContainerNamesFromAPI(pod catalogTypes.Pod) []string {
	containerNames := make([]string, 0, len(pod.Containers))
	for _, container := range pod.Containers {
		health := constants.NotReady
		if container.Healthy {
			health = constants.Ready
		}
		containerNames = append(containerNames, fmt.Sprintf("%s (%s)", container.Name, health))
	}

	return containerNames
}
