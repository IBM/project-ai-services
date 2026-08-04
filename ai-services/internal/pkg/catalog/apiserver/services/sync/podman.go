package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
)

// podmanSync implements RuntimeSync for the Podman runtime.
type podmanSync struct{}

func newPodmanSync() *podmanSync {
	return &podmanSync{}
}

// FetchPodStatuses fetches all pods labelled with the given templateID and returns their statuses.
// It uses InspectPod and InspectContainer (via common.ProcessPod) to derive state and health.
func (s *podmanSync) FetchPodStatuses(rt runtime.Runtime, templateID string) ([]*PodStatus, error) {
	filteredPods, err := common.FetchFilteredPods(rt, templateID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pods: %w", err)
	}

	if len(filteredPods) == 0 {
		return nil, fmt.Errorf("no pod found with template ID: %s", templateID)
	}

	var podStatuses []*PodStatus
	for _, pod := range filteredPods {
		processedPod, err := common.ProcessPod(rt, pod)
		if err != nil {
			return nil, fmt.Errorf("failed to process pod: %w", err)
		}

		if processedPod == nil {
			return nil, fmt.Errorf("pod processing returned nil")
		}

		podStatuses = append(podStatuses, &PodStatus{
			State:   processedPod.State,
			Health:  processedPod.Health,
			PodName: processedPod.Name,
		})
	}

	return podStatuses, nil
}

// ShouldValidateResources returns true — Podman uses .tmpl pod specs and external secrets/volumes.
func (s *podmanSync) ShouldValidateResources() bool { return true }

// ValidateResources checks that the expected Podman secrets and volumes exist.
func (s *podmanSync) ValidateResources(ctx context.Context, expectedSecretNames, expectedVolumeNames []string, rt runtime.Runtime) string {
	return validateResources(ctx, expectedSecretNames, expectedVolumeNames, rt)
}

// validateResources is shared by both syncer implementations.
func validateResources(ctx context.Context, expectedSecretNames, expectedVolumeNames []string, rt runtime.Runtime) string {
	if len(expectedSecretNames) == 0 && len(expectedVolumeNames) == 0 {
		return ""
	}

	var errorMessages []string

	if msg := validateResourceExistence(ctx, expectedSecretNames, "secret", rt.SecretExists); msg != "" {
		errorMessages = append(errorMessages, msg)
	}

	if msg := validateResourceExistence(ctx, expectedVolumeNames, "volume", rt.VolumeExists); msg != "" {
		errorMessages = append(errorMessages, msg)
	}

	return strings.Join(errorMessages, "; ")
}

// validateResourceExistence is a generic helper to validate resource existence via the provided lookup func.
func validateResourceExistence(ctx context.Context, resourceNames []string, resourceType string, existsFunc func(string) (bool, error)) string {
	var missing []string

	for _, name := range resourceNames {
		exists, err := existsFunc(name)
		if err != nil {
			logger.ErrorfCtx(ctx, "Failed to check %s existence for %s: %v", resourceType, name, err)

			continue
		}

		if !exists {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return ""
	}

	return fmt.Sprintf("missing %s(s): %s", resourceType, strings.Join(missing, ", "))
}

// Ensure podmanSync implements RuntimeSync at compile time.
var _ RuntimeSync = (*podmanSync)(nil)
