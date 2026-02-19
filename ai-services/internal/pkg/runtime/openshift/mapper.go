package openshift

import (
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
			Containers: toOpenshiftContainerList(pod.Spec.Containers),
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
		Labels:     pod.Labels,
		Containers: toOpenshiftContainerList(pod.Spec.Containers),
		Created:    pod.CreationTimestamp.Time,
		Ports:      extractPodPorts(pod.Spec.Containers),
	}
}

func extractPodPorts(containers []corev1.Container) map[string][]string {
	ports := make(map[string][]string)
	for _, container := range containers {
		for _, port := range container.Ports {
			ports[container.Name] = append(ports[container.Name], string(port.ContainerPort))
		}
	}

	return ports
}

func toOpenshiftContainerList(containers []corev1.Container) []types.Container {
	containerList := make([]types.Container, 0, len(containers))
	for _, container := range containers {
		containerList = append(containerList, types.Container{
			Name: container.Name,
		})
	}

	return containerList
}

func toOpenShiftContainer(cs *corev1.ContainerStatus, pod *corev1.Pod) *types.Container {
	container := &types.Container{
		ID:          cs.ContainerID,
		Name:        cs.Name,
		Annotations: pod.Annotations,
	}
	switch {
	case cs.State.Running != nil:
		container.Status = "Running"
		startedAt := cs.State.Running.StartedAt.Time
		container.HealthcheckStartPeriod = time.Since(startedAt)
		if cs.Ready {
			container.Health = "Healthy"
		} else {
			container.Health = "Unhealthy"
		}
	case cs.State.Waiting != nil:
		container.Status = "Waiting"
		container.Health = cs.State.Waiting.Reason

	case cs.State.Terminated != nil:
		container.Status = "Terminated"
		container.Health = cs.State.Terminated.Reason
	default:
		container.Status = "Unknown"
	}

	return container
}

func toOpenShiftRoute(r *routev1.Route) *types.Route {
	return &types.Route{
		Name:       r.Name,
		HostPort:   r.Spec.Host,
		TargetPort: r.Spec.Port.TargetPort.String(),
	}
}
