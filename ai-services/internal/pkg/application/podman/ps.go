package podman

import (
	"fmt"
	"strings"

	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// List returns information about running applications.
func (p *PodmanApplication) List(opts appTypes.ListOptions) ([]appTypes.ApplicationInfo, error) {
	// filter and fetch pods based on appName
	pods, err := p.fetchFilteredPods(opts.ApplicationName)
	if err != nil {
		return nil, err
	}

	// if no pods are present and also if appName is provided then simply log and return
	if len(pods) == 0 && opts.ApplicationName != "" {
		logger.Infof("No Pods found for the given application name: %s", opts.ApplicationName)

		return nil, nil
	}

	// fetch the table writer object
	printer := utils.NewTableWriter()
	defer printer.CloseTableWriter()

	// set table headers
	p.setTableHeaders(printer, opts.OutputWide)

	// render each pod info as rows in the table
	p.renderPodRows(printer, pods, opts.OutputWide)

	return nil, nil
}

func (p *PodmanApplication) fetchFilteredPods(appName string) ([]types.Pod, error) {
	listFilters := map[string][]string{}
	if appName != "" {
		listFilters["label"] = []string{fmt.Sprintf("ai-services.io/application=%s", appName)}
	}

	pods, err := p.runtime.ListPods(listFilters)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return pods, nil
}

func (p *PodmanApplication) setTableHeaders(printer *utils.Printer, outputWide bool) {
	if outputWide {
		printer.SetHeaders("APPLICATION NAME", "POD ID", "POD NAME", "STATUS", "CREATED", "EXPOSED", "CONTAINERS")
	} else {
		printer.SetHeaders("APPLICATION NAME", "POD NAME", "STATUS")
	}
}

func (p *PodmanApplication) renderPodRows(printer *utils.Printer, pods []types.Pod, wideOutput bool) {
	for _, pod := range pods {
		p.processAndAppendPodRow(printer, pod, wideOutput)
	}
}

func (p *PodmanApplication) processAndAppendPodRow(printer *utils.Printer, pod types.Pod, wideOutput bool) {
	appName := p.fetchPodNameFromLabels(pod.Labels)
	if appName == "" {
		// skip pods which are not linked to ai-services
		return
	}

	// do pod inspect
	pInfo, err := p.runtime.InspectPod(pod.ID)
	if err != nil {
		// log and skip pod if inspect failed
		logger.Errorf("Failed to do pod inspect: '%s' with error: %v", pod.ID, err)

		return
	}

	// fetch pod row
	rows := p.buildPodRow(appName, pInfo, wideOutput)
	// append pod row to the table
	printer.AppendRow(rows...)
}

func (p *PodmanApplication) fetchPodNameFromLabels(labels map[string]string) string {
	return labels[constants.ApplicationAnnotationKey]
}

func (p *PodmanApplication) buildPodRow(appName string, pod *types.Pod, wideOutput bool) []string {
	status := p.getPodStatus(pod)

	// if wide option flag is not set, then return appName, podName and status only
	if !wideOutput {
		return []string{appName, pod.Name, status}
	}

	containerNames := p.getContainerNames(pod)

	podPorts, err := p.getPodPorts(pod)
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

func (p *PodmanApplication) getPodPorts(pInfo *types.Pod) ([]string, error) {
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

func (p *PodmanApplication) getContainerNames(pod *types.Pod) []string {
	containerNames := []string{}

	for _, container := range pod.Containers {
		cInfo, err := p.runtime.InspectContainer(container.ID)
		if err != nil {
			// skip container if inspect failed
			logger.Infof("failed to do container inspect for pod: '%s', containerID: '%s' with error: %v", pod.Name, container.ID, err, logger.VerbosityLevelDebug)

			continue
		}

		// Along with container name append the container status too
		status := p.fetchContainerStatus(cInfo)
		cInfo.Name += fmt.Sprintf(" (%s)", status)

		containerNames = append(containerNames, cInfo.Name)
	}

	if len(containerNames) == 0 {
		containerNames = []string{"none"}
	}

	return containerNames
}

func (p *PodmanApplication) getPodStatus(pInfo *types.Pod) string {
	// if the pod Status is running, make sure to check if its healthy or not, otherwise fallback to default pod state
	if pInfo.State == "Running" {
		healthyContainers := 0
		for _, container := range pInfo.Containers {
			cInfo, err := p.runtime.InspectContainer(container.ID)
			if err != nil {
				// skip container if inspect failed
				logger.Infof("failed to do container inspect for pod: '%s', containerID: '%s' with error: %v", pInfo.Name, container.ID, err, logger.VerbosityLevelDebug)

				continue
			}

			status := p.fetchContainerStatus(cInfo)
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

func (p *PodmanApplication) fetchContainerStatus(cInfo *types.Container) string {
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
