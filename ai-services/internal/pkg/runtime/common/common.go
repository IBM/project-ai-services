package common

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// FetchFilteredPods Fetch all pods for a given app based on label.
func FetchFilteredPods(r runtime.Runtime, appID string) ([]types.Pod, error) {
	listFilters := map[string][]string{}
	if appID != "" {
		listFilters["label"] = []string{fmt.Sprintf("%s=%s", constants.ApplicationTemplateKey, appID)}
	}

	pods, err := r.ListPods(listFilters)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	return pods, nil
}

func ProcessPod(r runtime.Runtime, pod types.Pod) (*types.Pod, error) {
	// do pod inspect
	pInfo, err := r.InspectPod(pod.ID)
	if err != nil {
		// log and skip pod if inspect failed
		logger.Errorf("Failed to do pod inspect: '%s' with error: %v", pod.ID, err)

		return nil, nil
	}
	// load pod status
	pInfo.Status, pInfo.Health = getPodStatus(r, pInfo)

	// truncate podID to 13 characters
	const podIDShortLength = 13
	truncatedPodID := pod.ID
	if len(pod.ID) > podIDShortLength {
		truncatedPodID = pod.ID[:podIDShortLength]
	}

	pInfo.ID = truncatedPodID

	return pInfo, nil
}

func getPodStatus(r runtime.Runtime, pInfo *types.Pod) (string, string) {
	// if the pod Status is running, make sure to check if its healthy or not, otherwise fallback to default pod state
	if pInfo.State == "Running" {
		healthyContainers := 0
		for i, container := range pInfo.Containers {
			cInfo, err := r.InspectContainer(container.ID)
			if err != nil {
				// skip container if inspect failed
				logger.Debugf("failed to do container inspect for pod: '%s', containerID: '%s' with error: %v", pInfo.Name, container.ID, err)

				continue
			}

			status := fetchContainerStatus(cInfo)
			if status == string(constants.Ready) {
				healthyContainers++
			}
			pInfo.Containers[i].Health = status
		}

		// if all the containers are healthy, then append 'healthy' to pod state or else mark it as unhealthy
		if healthyContainers == len(pInfo.Containers) {
			return pInfo.State, string(constants.Ready)
		} else {
			return pInfo.State, string(constants.NotReady)
		}
	}

	return pInfo.State, string(constants.NotReady)
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
