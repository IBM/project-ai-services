package application

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

var output string

func init() {
	psCmd.Flags().StringVarP(
		&output,
		"output",
		"o",
		"",
		"Output format (e.g., wide)",
	)
}

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
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		var applicationName string
		if len(args) > 0 {
			applicationName = args[0]
		}

		// podman connectivity
		runtimeClient, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}

		err = runPsCmd(runtimeClient, applicationName)
		if err != nil {
			return fmt.Errorf("failed to fetch application: %w", err)
		}

		return nil
	},
}

func runPsCmd(runtimeClient *podman.PodmanClient, appName string) error {
	// filter and fetch pods based on appName
	pods, err := fetchFilteredPods(runtimeClient, appName)
	if err != nil {
		return err
	}

	// if no pods are present and also if appName is provided then simply log and return
	if len(pods) == 0 && appName != "" {
		logger.Infof("No Pods found for the given application name: %s", appName)

		return nil
	}

	// fetch the table writter object
	p := utils.NewTableWriter()
	defer p.CloseTableWriter()

	// set table headers
	setTableHeaders(p)

	// render each pod info as rows in the table
	renderPodRows(runtimeClient, p, pods)

	return nil
}

func fetchFilteredPods(client *podman.PodmanClient, appName string) ([]types.Pod, error) {
	listFilters := map[string][]string{}
	if appName != "" {
		listFilters["label"] = []string{fmt.Sprintf("ai-services.io/application=%s", appName)}
	}

	pods, err := client.ListPods(listFilters)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return pods, nil
}

// setTableHeaders - sets and renders the table header based on the wide options flag set (-o wide).
func setTableHeaders(p *utils.Printer) {
	if isOutputWide() {
		p.SetHeaders("APPLICATION NAME", "POD ID", "POD NAME", "STATUS", "CREATED", "EXPOSED", "CONTAINERS")
	} else {
		p.SetHeaders("APPLICATION NAME", "POD NAME", "STATUS")
	}
}

// renderPodRows - renders each pod rows on the table.
func renderPodRows(runtimeClient *podman.PodmanClient, p *utils.Printer, pods []types.Pod) {
	for _, pod := range pods {
		processAndAppendPodRow(runtimeClient, p, pod)
	}
}

// processAndAppendPodRow - processes the pod to get the required info.
// Builds and appends the row containing pod info on to the table.
func processAndAppendPodRow(runtimeClient *podman.PodmanClient, p *utils.Printer, pod types.Pod) {
	appName := fetchPodNameFromLabels(pod.Labels)
	if appName == "" {
		// skip pods which are not linked to ai-services
		return
	}

	// do pod inspect
	pInfo, err := runtimeClient.InspectPod(pod.ID)
	if err != nil {
		// log and skip pod if inspect failed
		logger.Errorf("Failed to do pod inspect: '%s' with error: %v", pod.ID, err)

		return
	}

	// fetch pod row
	rows := buildPodRow(runtimeClient, appName, pInfo)
	// append pod row to the table
	p.AppendRow(rows...)
}

// buildPodRow - builds the row using the pod info based on the wide options flag set (-o wide).
func buildPodRow(runtimeClient *podman.PodmanClient, appName string, pod *types.Pod) []string {
	status := getPodStatus(runtimeClient, pod)

	// if wide option flag is not set, then return appName, podName and status only
	if !isOutputWide() {
		return []string{appName, pod.Name, status}
	}

	containerNames := getContainerNames(runtimeClient, pod)

	podPorts, err := getPodPorts(pod)
	if err != nil {
		podPorts = []string{"none"}
	}

	return []string{
		appName,
		pod.ID[:12],
		pod.Name,
		status,
		utils.TimeAgo(pod.Created),
		strings.Join(podPorts, ", "),
		strings.Join(containerNames, ", "),
	}
}

func getPodPorts(pInfo *types.Pod) ([]string, error) {
	podPorts := []string{}

	if pInfo.Ports != nil {
		for _, hostPorts := range pInfo.Ports {
			podPorts = append(podPorts, hostPorts...)
		}
	}

	if len(podPorts) == 0 {
		podPorts = []string{"none"}
	}

	return podPorts, nil
}

func fetchPodNameFromLabels(labels map[string]string) string {
	return labels[constants.ApplicationAnnotationKey]
}

func getContainerNames(runtimeClient *podman.PodmanClient, pod *types.Pod) []string {
	containerNames := []string{}

	for _, container := range pod.Containers {
		cInfo, err := runtimeClient.InspectContainer(container.ID)
		if err != nil {
			// skip container if inspect failed
			logger.Infof("failed to do container inspect for pod: '%s', containerID: '%s' with error: %v", pod.Name, container.ID, err, logger.VerbosityLevelDebug)

			continue
		}

		// Along with container name append the container status too
		status := fetchContainerStatus(cInfo)
		cInfo.Name += fmt.Sprintf(" (%s)", status)

		containerNames = append(containerNames, cInfo.Name)
	}

	if len(containerNames) == 0 {
		containerNames = []string{"none"}
	}

	return containerNames
}

func getPodStatus(runtimeClient *podman.PodmanClient, pInfo *types.Pod) string {
	// if the pod Status is running, make sure to check if its healthy or not, otherwise fallback to default pod state
	if pInfo.State == "Running" {
		healthyContainers := 0
		for _, container := range pInfo.Containers {
			cInfo, err := runtimeClient.InspectContainer(container.ID)
			if err != nil {
				// skip container if inspect failed
				logger.Infof("failed to do container inspect for pod: '%s', containerID: '%s' with error: %v", pInfo.Name, container.ID, err, logger.VerbosityLevelDebug)

				continue
			}

			status := fetchContainerStatus(cInfo)
			if status == string(constants.Ready) {
				healthyContainers++
			}
		}

		// if all the containers are healthy, then append 'healthy' to pod state or else mark it as unhealthy
		if healthyContainers == len(pInfo.Containers) {
			pInfo.State += fmt.Sprintf(" (%s)", constants.Ready)
		} else {
			pInfo.State += fmt.Sprintf(" (%s)", constants.NotReady)
		}
	}

	return pInfo.State
}

func fetchContainerStatus(cInfo *types.Container) string {
	containerStatus := cInfo.Status

	// if container status is not running, then return the container status
	if containerStatus != "running" {
		return containerStatus
	}

	// if running, proceed with checking health status of the container
	healthStatusCheck := cInfo.Health

	// if health status check is set, then return the particular health status
	if healthStatusCheck != "" {
		return healthStatusCheck
	}

	// if health status check is not set, consider it to be healthy by default
	return string(constants.Ready)
}
