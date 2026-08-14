package openshift

import (
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/application/common"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// Info displays detailed information about an application.
func (o *OpenshiftApplication) Info(opts types.InfoOptions) error {
	if opts.Legacy {
		return common.LegacyInfo(opts, o.runtime)
	}

	return common.RenderApplicationInfo(opts.Name, getPodStatus)
}

// getPodStatus derives UI/API status from the pod list
func getPodStatus(pods []catalogTypes.Pod, catalogID string) (string, string) {
	uiStatus, apiStatus := "", ""

	for _, pod := range pods {
		if strings.HasPrefix(pod.PodName, catalogID) && pod.Status == catalogTypes.Running {
			if strings.Contains(pod.PodName, "ui") {
				uiStatus = "running"
			} else if strings.Contains(pod.PodName, "api") {
				apiStatus = "running"
			}
		}
	}

	return uiStatus, apiStatus
}
