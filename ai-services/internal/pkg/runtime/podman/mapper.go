package podman

import (
	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// toPodsList - convert podman pods to desired type.
func toPodsList(reports []*types.ListPodsReport) []runtime.Pod {
	out := make([]runtime.Pod, 0, len(reports))
	for _, r := range reports {
		out = append(out, runtime.Pod{
			ID:         r.Id,
			Name:       r.Name,
			Status:     r.Status,
			Labels:     r.Labels,
			Containers: toPodContainerList(r.Containers),
		})
	}

	return out
}

// toPodContainerList - convert podman pod containers to desired type.
func toPodContainerList(reports []*types.ListPodContainer) []runtime.Container {
	out := make([]runtime.Container, 0, len(reports))
	for _, r := range reports {
		out = append(out, runtime.Container{
			ID:     r.Id,
			Name:   r.Names,
			Status: r.Status,
		})
	}

	return out
}
