package podman

import (
	"strings"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// toPodsList - convert podman pods to desired type.
func toPodsList(input any) []runtime.Pod {
	switch val := input.(type) {
	case []*types.ListPodsReport:
		out := make([]runtime.Pod, 0, len(val))
		for _, r := range val {
			out = append(out, runtime.Pod{
				ID:         r.Id,
				Name:       r.Name,
				Status:     r.Status,
				Labels:     r.Labels,
				Containers: toPodContainerList(r.Containers),
			})
		}

		return out

	case *types.KubePlayReport:
		out := make([]runtime.Pod, 0, len(val.Pods))
		for _, r := range val.Pods {
			out = append(out, runtime.Pod{
				ID: r.ID,
			})
		}

		return out

	default:
		panic("unsupported type to do mapper to podList")
	}
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

// toContainerList - convert podman containers to desired type.
func toContainerList(input []types.ListContainer) []runtime.Container {
	out := make([]runtime.Container, 0, len(input))
	for _, r := range input {
		out = append(out, runtime.Container{
			ID:     r.ID,
			Name:   strings.Join(r.Names, ","),
			Status: r.Status,
		})
	}

	return out
}

// toImageList - convert podman image type to desired type.
func toImageList(input []*types.ImageSummary) []runtime.Image {
	out := make([]runtime.Image, 0, len(input))
	for _, r := range input {
		out = append(out, runtime.Image{
			RepoTags:    r.RepoTags,
			RepoDigests: r.RepoDigests,
		})
	}

	return out
}
