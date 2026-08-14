package podman

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/application/common"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// Info displays detailed information about an application.
func (p *PodmanApplication) Info(opts types.InfoOptions) error {
	if opts.Legacy {
		return common.LegacyInfo(opts, p.runtime)
	}

	return common.RenderApplicationInfo(opts.Name, getContainerStatus)
}

// getContainerStatus derives UI/API status from container names within the pod.
func getContainerStatus(pods []catalogTypes.Pod, catalogID string) (string, string) {
	uiStatus, apiStatus := "", ""

	for _, pod := range pods {
		if !strings.HasPrefix(pod.PodName, catalogID) {
			continue
		}

		for _, container := range pod.Containers {
			uiContainerName := fmt.Sprintf("%s-ui", pod.PodName)
			apiContainerName := ""
			if strings.Contains(container.Name, "backend-server") {
				apiContainerName = container.Name
			} else {
				apiContainerName = fmt.Sprintf("%s-%s-api", pod.PodName, catalogID)
			}

			if container.Name == uiContainerName && container.Healthy {
				uiStatus = "running"
			}

			if container.Name == apiContainerName && container.Healthy {
				apiStatus = "running"
			}
		}
	}

	return uiStatus, apiStatus
}
