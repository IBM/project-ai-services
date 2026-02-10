package podman

import (
	"strings"

	"github.com/containers/podman/v5/libpod/define"
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
func toPodContainerList(input any) []types.Container {
	switch val := input.(type) {
	case []*podmanTypes.ListPodContainer:
		out := make([]types.Container, 0, len(val))
		for _, r := range val {
			out = append(out, types.Container{
				ID:     r.Id,
				Name:   r.Names,
				Status: r.Status,
			})
		}

		return out

	case []define.InspectPodContainerInfo:
		out := make([]types.Container, 0, len(val))
		for _, r := range val {
			out = append(out, types.Container{
				ID:     r.ID,
				Name:   r.Name,
				Status: r.State,
			})
		}

		return out

	default:
		panic("unsupported type to do mapper to pod containers list")
	}
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

func toPodInspectReport(input *podmanTypes.PodInspectReport) *types.Pod {
	return &types.Pod{
		ID:               input.ID,
		Name:             input.Name,
		Labels:           input.Labels,
		Containers:       toPodContainerList(input.Containers),
		Ports:            toPortBindings(input.InfraConfig),
		InfraContainerID: input.InfraContainerID,
		State:            input.State,
		Created:          input.Created,
	}
}

func toPortBindings(infraConfig *define.InspectPodInfraConfig) map[string][]string {
	podPorts := make(map[string][]string)

	if infraConfig != nil && infraConfig.PortBindings != nil {
		for containerPort, hostPorts := range infraConfig.PortBindings {
			for _, hostPort := range hostPorts {
				podPorts[containerPort] = append(podPorts[containerPort], hostPort.HostPort)
			}
		}
	}

	return podPorts
}

func toInspectContainer(input *define.InspectContainerData) *types.Container {
	container := &types.Container{
		ID:     input.ID,
		Name:   input.Name,
		Status: input.State.Status,
	}

	// Set health status if available
	if input.State.Health != nil {
		container.Health = input.State.Health.Status
	}

	// Set annotations if available
	if input.Config != nil && input.Config.Annotations != nil {
		container.Annotations = input.Config.Annotations
	}

	// Set healthcheck start period if available
	if input.Config != nil && input.Config.Healthcheck != nil {
		container.HealthcheckStartPeriod = input.Config.Healthcheck.StartPeriod
	}

	return container
}
