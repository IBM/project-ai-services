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

	return PerformCleanup(ctx, rt, namespace, opts.SkipCleanup)
}

// PerformCleanup uninstalls the worker Helm release from the given OpenShift namespace
// and, unless skipCleanup is true, removes the associated PersistentVolumeClaims and the mTLS
// secret that were created during deployment.
//
// Parameters:
//   - ctx:         context used for logging and cancellation propagation.
//   - rt:          runtime client used to delete Kubernetes resources (PVCs and secrets).
//   - namespace:   the OpenShift namespace from which the release is uninstalled.
//   - skipCleanup: when true, only the Helm release is removed; PVCs and secrets are left intact.
//
// Returns an error if the Helm uninstall or any resource deletion fails.
func PerformCleanup(ctx context.Context, rt runtime.Runtime, namespace string, skipCleanup bool) error {
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

		logger.DebugfCtx(ctx, "Deleting worker secrets...")

		if err := rt.DeleteSecret(ctx, workerconstants.WorkerMTLSSecretName); err != nil {
			s.Fail("failed to delete worker secret")

			return fmt.Errorf("failed to delete secret %s: %w", workerconstants.WorkerMTLSSecretName, err)
		}
	}

	s.Stop("Worker service uninstalled successfully")

	return nil
}
