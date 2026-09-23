package podman

import (
	"context"
	"fmt"

	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workercommon "github.com/project-ai-services/ai-services/internal/pkg/worker/common"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerutils "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall/utils"
)

// Uninstall removes all worker components deployed by `worker join`.
func Uninstall(ctx context.Context, opts workerutils.UninstallOptions) error {
	rt, err := runtime.CreateRuntime(opts.RuntimeType, "")
	if err != nil {
		return fmt.Errorf("worker uninstall: init runtime: %w", err)
	}

	pods, err := getWorkerPodList(ctx, rt)
	if err != nil {
		return fmt.Errorf("worker uninstall: list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.InfolnCtx(ctx, "No worker pods found — nothing to uninstall.")

		return nil
	}

	logger.Warningln("Ensure no application pods are running on this worker before uninstalling, as they will become unreachable and will need to be deleted manually.")

	if ok, err := podmanutils.ConfirmUninstall(ctx, pods, opts.AutoYes); err != nil || !ok {
		return err
	}

	return workercommon.PerformPodmanCleanup(ctx, rt, pods, opts.SkipCleanup)
}

// ─── internal ─────────────────────────────────────────────────────────────────

func getWorkerPodList(ctx context.Context, rt runtime.Runtime) ([]types.Pod, error) {
	labels := []string{workerconstants.WorkerProxyLabel, workerconstants.WorkerPodLabel}

	var podList []types.Pod
	for _, label := range labels {
		pods, err := rt.ListPods(ctx, map[string][]string{"label": {label}})
		if err != nil {
			return nil, err
		}

		podList = append(podList, pods...)
	}

	return podList, nil
}
