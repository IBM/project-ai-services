package common

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// IsPodmanLocalWorker returns true when the catalog pod on this Podman node has
// LOCAL_WORKER=true in any of its container env vars.
func IsPodmanLocalWorker(ctx context.Context, rt runtime.Runtime) (bool, error) {
	pod, err := rt.InspectPod(ctx, workerconstants.PodmanGatewayPodName)
	if err != nil {
		if utils.IsNotFoundError(err) {
			return false, nil
		}

		return false, fmt.Errorf("inspect catalog pod: %w", err)
	}

	for _, container := range pod.Containers {
		cInfo, err := rt.InspectContainer(ctx, container.ID)
		if err != nil {
			return false, fmt.Errorf("inspect container %s: %w", container.ID, err)
		}
		if cInfo.Env[workerconstants.LocalWorkerEnvVar] == "true" {
			return true, nil
		}
	}

	return false, nil
}

// IsOpenShiftLocalWorker returns true when any catalog-backend pod in the
// namespace has LOCAL_WORKER=true in its pod spec env vars.
func IsOpenShiftLocalWorker(ctx context.Context, rt runtime.Runtime) (bool, error) {
	pods, err := rt.ListPods(ctx, map[string][]string{
		"label": {workerconstants.CatalogBackendPodLabel + "=" + workerconstants.CatalogBackendPodLabelValue},
	})
	if err != nil {
		return false, fmt.Errorf("list catalog-backend pods: %w", err)
	}

	for _, pod := range pods {
		if pod.Env[workerconstants.LocalWorkerEnvVar] == "true" {
			return true, nil
		}
	}

	return false, nil
}
