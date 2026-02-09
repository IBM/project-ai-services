package openshift

import (
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	corev1 "k8s.io/api/core/v1"
)

func toOpenshiftPodsList(kubePods []corev1.Pod) []types.Pod {
	pods := make([]types.Pod, 0, len(kubePods))
	for _, kp := range kubePods {
		pods = append(pods, types.Pod{
			ID:         string(kp.UID),
			Name:       kp.Name,
			Status:     string(kp.Status.Phase),
			Labels:     kp.Labels,
			Containers: toOpenshiftContainersList(kp),
		})
	}

	return pods
}

func toOpenshiftContainersList(pod corev1.Pod) []types.Container {
	containers := make([]types.Container, 0, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		status := "unknown"
		if cs.State.Running != nil {
			status = "running"
		} else if cs.State.Waiting != nil {
			status = "waiting"
		} else if cs.State.Terminated != nil {
			status = "terminated"
		}

		containers = append(containers, types.Container{
			ID:     cs.ContainerID,
			Name:   cs.Name,
			Status: status,
		})
	}

	return containers
}

// Made with Bob
