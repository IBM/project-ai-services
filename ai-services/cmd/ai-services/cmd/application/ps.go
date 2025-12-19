package application

import (
	"fmt"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
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
	listFilters := map[string][]string{}
	if appName != "" {
		listFilters["label"] = []string{fmt.Sprintf("ai-services.io/application=%s", appName)}
	}

	resp, err := runtimeClient.ListPods(listFilters)
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// TODO: Avoid doing the type assertion and importing types package from podman

	var pods []*types.ListPodsReport
	if val, ok := resp.([]*types.ListPodsReport); ok {
		pods = val
	}

	if len(pods) == 0 && appName != "" {
		logger.Infof("No Pods found for the given application name: %s", appName)

		return nil
	}

	p := utils.NewTableWriter()
	defer p.CloseTableWriter()

	if isOutputWide() {
		p.SetHeaders("APPLICATION NAME", "POD ID", "POD NAME", "STATUS", "CREATED", "EXPOSED", "CONTAINERS")
	} else {
		p.SetHeaders("APPLICATION NAME", "POD NAME", "STATUS")
	}

	for _, pod := range pods {
		if fetchPodNameFromLabels(pod.Labels) == "" {
			// skip pods which are not linked to ai-services
			continue
		}

		// do pod inspect
		pInfo, err := runtimeClient.InspectPod(pod.Id)
		if err != nil {
			// log and skip pod if inspect failed
			logger.Errorf("Failed to do pod inspect: '%s' with error: %v", pod.Id, err)

			continue
		}

		if isOutputWide() {
			podPorts, err := getPodPorts(pInfo)
			if err != nil {
				// if failed to fetch ports for pod, then set podPorts to none
				podPorts = []string{"none"}
			}
			containerNames := getContainerNames(runtimeClient, pod)
			p.AppendRow(
				fetchPodNameFromLabels(pod.Labels),
				pod.Id[:12],
				pod.Name,
				getPodStatus(runtimeClient, pInfo),
				utils.TimeAgo(pInfo.Created),
				strings.Join(podPorts, ", "),
				strings.Join(containerNames, ", "),
			)
		} else {
			p.AppendRow(
				fetchPodNameFromLabels(pod.Labels),
				pod.Name,
				getPodStatus(runtimeClient, pInfo),
			)
		}
	}

	return nil
}

func fetchPodNameFromLabels(labels map[string]string) string {
	return labels[constants.ApplicationAnnotationKey]
}

func getPodPorts(pInfo *types.PodInspectReport) ([]string, error) {
	podPorts := []string{}

	if pInfo.InfraConfig != nil && pInfo.InfraConfig.PortBindings != nil {
		for _, ports := range pInfo.InfraConfig.PortBindings {
			for _, port := range ports {
				podPorts = append(podPorts, port.HostPort)
			}
		}
	}

	if len(podPorts) == 0 {
		podPorts = []string{"none"}
	}

	return podPorts, nil
}

func getContainerNames(runtimeClient *podman.PodmanClient, pod *types.ListPodsReport) []string {
	containerNames := []string{}

	for _, container := range pod.Containers {
		cInfo, err := runtimeClient.InspectContainer(container.Id)
		if err != nil {
			// skip container if inspect failed
			logger.Infof("failed to do container inspect for pod: '%s', containerID: '%s' with error: %v", pod.Name, container.Id, err, 2)

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

func getPodStatus(runtimeClient *podman.PodmanClient, pInfo *types.PodInspectReport) string {
	// if the pod Status is running, make sure to check if its healthy or not, otherwise fallback to default pod state
	if pInfo.State == "Running" {
		healthyContainers := 0
		for _, container := range pInfo.Containers {
			cInfo, err := runtimeClient.InspectContainer(container.ID)
			if err != nil {
				// skip container if inspect failed
				logger.Infof("failed to do container inspect for pod: '%s', containerID: '%s' with error: %v", pInfo.Name, container.ID, err, 2)

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

func fetchContainerStatus(cInfo *define.InspectContainerData) string {
	containerStatus := cInfo.State.Status

	// if container status is not running, then return the container status
	if containerStatus != "running" {
		return containerStatus
	}

	// if running, proceed with checking health status of the container
	healthStatusCheck := cInfo.State.Health

	// if health status check is set, then return the particular health status
	if healthStatusCheck != nil {
		return healthStatusCheck.Status
	}

	// if health status check is not set, consider it to be healthy by default
	return string(constants.Ready)
}
