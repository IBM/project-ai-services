package openshift

import (
	"context"
	"fmt"

	clituils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	workercommon "github.com/project-ai-services/ai-services/internal/pkg/worker/common"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerutils "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall/utils"
)

// Uninstall removes all worker components deployed by `worker join`.
func Uninstall(ctx context.Context, opts workerutils.UninstallOptions) error {
	namespace := workerconstants.WorkerAppName

	rt, err := runtime.CreateRuntime(opts.RuntimeType, namespace)
	if err != nil {
		return fmt.Errorf("worker uninstall: init runtime: %w", err)
	}

	localWorker, err := workercommon.IsOpenShiftLocalWorker(ctx, rt)
	if err != nil {
		return fmt.Errorf("worker uninstall failed: %w", err)
	}
	if localWorker {
		return fmt.Errorf("the worker is co-located with the control plane and cannot be uninstalled independently")
	}

	pods, err := rt.ListPods(ctx, map[string][]string{
		"label": {workerconstants.WorkerPodLabel},
	})
	if err != nil {
		return fmt.Errorf("worker uninstall: list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.InfolnCtx(ctx, "No worker pods found — nothing to uninstall.")

		return nil
	}

	// Confirm Uninstall unless auto-yes is set
	if confirmed, err := clituils.ConfirmUninstall(ctx, pods, opts.AutoYes); err != nil || !confirmed {
		return err
	}

	return performCleanup(ctx, rt, namespace, opts.SkipCleanup)
}

func performCleanup(ctx context.Context, rt runtime.Runtime, namespace string, skipCleanup bool) error {
	release := workerconstants.WorkerHelmReleaseName

	logger.InfolnCtx(ctx, "Proceeding with uninstall...")

	s := spinner.New("Uninstalling worker service...")
	s.Start(ctx)

	if err := helm.UninstallRelease(ctx, release, namespace); err != nil {
		return err
	}

	if !skipCleanup {
		logger.DebuglnCtx(ctx, "Delete worker PVCs...")

		if err := rt.DeletePVCs(ctx, fmt.Sprintf("%s=%s", constants.ApplicationAnnotationKey, workerconstants.WorkerHelmReleaseName)); err != nil {
			s.Fail("failed to delete worker pvc")

			return fmt.Errorf("failed to delete PVCs: %w", err)
		}
	}

	s.Stop("Worker service uninstalled successfully")

	return nil
}
