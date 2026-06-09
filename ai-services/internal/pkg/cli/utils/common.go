package utils

import (
	"fmt"
	"strings"
	"time"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// FetchApplications retrieves either all applications or a specific application by name.
// If appName is empty, it fetches all applications. Otherwise, it fetches the specified application.
func FetchApplications(appClient *catalogClient.ApplicationClient, appName string) ([]catalogTypes.Application, error) {
	if appName == "" {
		// Fetch all applications when no specific name is provided
		applicationList, err := GetAllApps(appClient)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch all applications: %w", err)
		}

		return applicationList, nil
	}

	// Fetch specific application by name
	application, err := GetAppByName(appClient, appName)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch application '%s': %w", appName, err)
	}

	return []catalogTypes.Application{*application}, nil
}

// BuildPodRowFromAPI builds a table row from API response data.
func BuildPodRowFromAPI(appName string, pod catalogTypes.Pod, wideOutput bool) []string {
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
