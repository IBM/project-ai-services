package openshift

import (
	"strconv"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	corev1 "k8s.io/api/core/v1"
)

func toOpenshiftPodList(pods *corev1.PodList) []types.Pod {
	podsList := make([]types.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		podsList = append(podsList, types.Pod{
			ID:         string(pod.UID),
			Name:       pod.Name,
			Status:     string(pod.Status.Phase),
			Labels:     pod.Labels,
			Containers: toOpenshiftContainerList(pod.Status.ContainerStatuses),
			Created:    pod.CreationTimestamp.Time,
			Ports:      extractPodPorts(pod.Spec.Containers),
		})
	}

	return podsList
}

func toOpenshiftPod(pod *corev1.Pod) *types.Pod {
	return &types.Pod{
		ID:         string(pod.UID),
		Name:       pod.Name,
		Status:     string(pod.Status.Phase),
		State:      string(pod.Status.Phase),
		Labels:     pod.Labels,
		Containers: toOpenshiftContainerList(pod.Status.ContainerStatuses),
		Created:    pod.CreationTimestamp.Time,
		Ports:      extractPodPorts(pod.Spec.Containers),
	}
}

func extractPodPorts(containers []corev1.Container) map[string][]string {
	ports := make(map[string][]string)
	for _, container := range containers {
		for _, port := range container.Ports {
			ports[container.Name] = append(ports[container.Name], strconv.Itoa(int(port.ContainerPort)))
		}
	}

	return ports
}

func toOpenshiftContainerList(containers []corev1.ContainerStatus) []types.Container {
	containerList := make([]types.Container, 0, len(containers))
	for _, cs := range containers {
		container := &types.Container{
			ID:   cs.ContainerID,
			Name: cs.Name,
		}
		setContainerStatus(&cs, container)
		containerList = append(containerList, *container)
	}

	return containerList
}

func toOpenShiftContainer(cs *corev1.ContainerStatus, pod *corev1.Pod) *types.Container {
	container := &types.Container{
		ID:          cs.ContainerID,
		Name:        cs.Name,
		Annotations: pod.Annotations,
	}
	setContainerStatus(cs, container)

	return container
}

func setContainerStatus(cs *corev1.ContainerStatus, container *types.Container) {
	switch {
	case cs.State.Running != nil:
		container.Status = "running"
		startedAt := cs.State.Running.StartedAt.Time
		container.HealthcheckStartPeriod = time.Since(startedAt)
		if cs.Ready {
			container.Health = "healthy"
		} else {
			container.Health = "unhealthy"
		}
	case cs.State.Waiting != nil:
		container.Status = "waiting"
		container.Health = cs.State.Waiting.Reason

	case cs.State.Terminated != nil:
		container.Status = "terminated"
		container.Health = cs.State.Terminated.Reason
	default:
		container.Status = "unknown"
		container.Health = "unknown"
	}
}

func toOpenShiftRouteList(routes []routev1.Route) []types.Route {
	routeList := make([]types.Route, 0, len(routes))
	for _, route := range routes {
		routeList = append(routeList, types.Route{
			Name:       route.Name,
			HostPort:   route.Spec.Host,
			TargetPort: route.Spec.Port.TargetPort.String(),
		})
	}

	return routeList
}
