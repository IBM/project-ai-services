package specs

import "github.com/project-ai-services/ai-services/internal/pkg/models"

func FetchPodAnnotations(podspec models.PodSpec) map[string]string {
	return podspec.Annotations
}

func FetchContainerNames(podspec models.PodSpec) []string {
	containerNames := make([]string, 0, len(podspec.Spec.Containers))
	for _, v1Container := range podspec.Spec.Containers {
		containerNames = append(containerNames, v1Container.Name)
	}

	return containerNames
}
