package podman

import (
	"strings"

	podmanTypes "github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// toPodsList - convert podman pods to desired type.
func toPodsList(input any) []types.Pod {
	switch val := input.(type) {
	case []*podmanTypes.ListPodsReport:
		out := make([]types.Pod, 0, len(val))
		for _, r := range val {
			out = append(out, types.Pod{
				ID:         r.Id,
				Name:       r.Name,
				Status:     r.Status,
				Labels:     r.Labels,
				Containers: toPodContainerList(r.Containers),
			})
		}

		return out

	case *podmanTypes.KubePlayReport:
		out := make([]types.Pod, 0, len(val.Pods))
		for _, r := range val.Pods {
			out = append(out, types.Pod{
				ID: r.ID,
			})
		}

		return out

	default:
		panic("unsupported type to do mapper to podList")
	}
}

// toPodContainerList - convert podman pod containers to desired type.
func toPodContainerList(reports []*podmanTypes.ListPodContainer) []types.Container {
	out := make([]types.Container, 0, len(reports))
	for _, r := range reports {
		out = append(out, types.Container{
			ID:     r.Id,
			Name:   r.Names,
			Status: r.Status,
		})
	}

	return out
}

// toContainerList - convert podman containers to desired type.
func toContainerList(input []podmanTypes.ListContainer) []types.Container {
	out := make([]types.Container, 0, len(input))
	for _, r := range input {
		out = append(out, types.Container{
			ID:     r.ID,
			Name:   strings.Join(r.Names, ","),
			Status: r.Status,
		})
	}

	return out
}

// toImageList - convert podman image type to desired type.
func toImageList(input []*podmanTypes.ImageSummary) []types.Image {
	out := make([]types.Image, 0, len(input))
	for _, r := range input {
		out = append(out, types.Image{
			RepoTags:    r.RepoTags,
			RepoDigests: r.RepoDigests,
		})
	}

	return out
}
