package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	rtconstants "github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimecommon "github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
)

// LoadPods returns the pods labelled with the given catalog resource ID (application, service,
// component or model) as catalog pod details. A resource without pods yields an empty slice.
//
// Pods that cannot be inspected are skipped. Pod and container statuses are lower-cased, and each
// container carries its own status and health rather than the pod's.
func LoadPods(ctx context.Context, rt runtime.Runtime, resourceID string) ([]types.Pod, error) {
	filteredPods, err := runtimecommon.FetchFilteredPods(ctx, rt, resourceID)
	if err != nil {
		return nil, err
	}

	pods := make([]types.Pod, 0, len(filteredPods))

	for _, pod := range filteredPods {
		processedPod, err := runtimecommon.ProcessPod(ctx, rt, pod)
		if err != nil {
			return nil, fmt.Errorf("failed to process pod: %w", err)
		}
		// ProcessPod returns (nil, nil) when InspectPod fails, so we should skip that pod.
		if processedPod == nil {
			continue
		}

		containers := make([]types.PodContainer, 0, len(processedPod.Containers))
		for _, container := range processedPod.Containers {
			containers = append(containers, types.PodContainer{
				Name:    container.Name,
				Status:  types.Status(strings.ToLower(container.Status)),
				Healthy: strings.ToLower(container.Health) == string(rtconstants.Ready),
			})
		}

		pods = append(pods, types.Pod{
			PodID:      processedPod.ID,
			PodName:    processedPod.Name,
			Status:     types.Status(strings.ToLower(processedPod.Status)),
			Healthy:    processedPod.Health == string(rtconstants.Ready),
			Created:    pod.Created.Format(constants.RFC3339WithTimezone),
			Labels:     pod.Labels,
			Containers: containers,
		})
	}

	return pods, nil
}
