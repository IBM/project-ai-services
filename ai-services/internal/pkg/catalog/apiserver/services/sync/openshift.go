package sync

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
	runtimetypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// openShiftSync implements RuntimeSync for the OpenShift runtime.
type openShiftSync struct{}

func newOpenShiftSync() *openShiftSync {
	return &openShiftSync{}
}

// FetchPodStatuses fetches all pods labelled with the given templateID using the OpenShift runtime.
func (s *openShiftSync) FetchPodStatuses(rt runtime.Runtime, templateID string) ([]*PodStatus, error) {
	filteredPods, err := common.FetchFilteredPods(rt, templateID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pods: %w", err)
	}

	if len(filteredPods) == 0 {
		return nil, fmt.Errorf("no pod found with template ID: %s", templateID)
	}

	podStatuses := make([]*PodStatus, 0, len(filteredPods))
	for _, pod := range filteredPods {
		health := derivePodHealth(pod.Containers)
		podStatuses = append(podStatuses, &PodStatus{
			State:   pod.Status,
			Health:  health,
			PodName: pod.Name,
		})
	}

	return podStatuses, nil
}

// derivePodHealth determines pod-level health from its container statuses.
// A pod is healthy only when all containers report "healthy".
func derivePodHealth(containers []runtimetypes.Container) string {
	if len(containers) == 0 {
		return string(constants.NotReady)
	}

	for _, c := range containers {
		if c.Health != string(constants.Ready) {
			return string(constants.NotReady)
		}
	}

	return string(constants.Ready)
}

// ShouldValidateResources returns false — OpenShift uses Helm charts; all secrets, volumes,
// and pods are managed by Helm so there are no .tmpl pod specs to count.
func (s *openShiftSync) ShouldValidateResources() bool { return false }

// Ensure openShiftSync implements RuntimeSync at compile time.
var _ RuntimeSync = (*openShiftSync)(nil)
