package openshift

import (
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	corev1 "k8s.io/api/core/v1"
)

func toOpenshiftPodList(pods *corev1.PodList) []types.Pod {
	podsList := make([]types.Pod, len(pods.Items))
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
	containerList := make([]types.Container, len(containers))
	for _, container := range containers {
		containerList = append(containerList, types.Container{
			Name: container.Name,
		})
	}

	return containerList
}
