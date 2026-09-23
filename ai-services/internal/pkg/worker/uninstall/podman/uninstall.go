package podman

import (
	"context"
	"fmt"
	"path/filepath"

	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
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

	return PerformCleanup(ctx, rt, pods, opts.SkipCleanup)
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

// WorkerPodConfig holds configuration recovered from the running worker pod.
type WorkerPodConfig struct {
	// BaseDir is the base directory recovered from the AI_SERVICES_BASE_DIR
	// env var injected by worker.yaml.tmpl at deploy time.
	BaseDir string
}

// PerformCleanup removes a Podman-based worker deployment by deleting its pods, secrets,
// volumes, and on-disk data directory. Resources that were preserved by a previous
// --skip-cleanup run are also reconciled according to the current skipCleanup flag.
//
// Parameters:
//   - ctx:         context used for logging and cancellation propagation.
//   - rt:          runtime client used to delete Podman resources (pods, secrets, volumes).
//   - pods:        list of running pods belonging to the worker deployment.
//   - skipCleanup: when true, secrets and volumes tagged for deferred deletion are left intact;
//     when false, those previously skipped resources are also removed.
//
// Returns an error if any deletion step (pods, secrets, volumes, or data directory) fails.
func PerformCleanup(ctx context.Context, rt runtime.Runtime, pods []types.Pod, skipCleanup bool) error {
	logger.InfolnCtx(ctx, "Proceeding with deletion...")

	var baseDir string

	config, _, err := podmanutils.GetPodConfig(ctx, rt, workerconstants.WorkerPodLabel)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to retrieve BaseDir from worker pod: %v. Using default BaseDir.\n", err)
		baseDir = utils.GetBaseDir()
	} else if config.BaseDir == "" {
		logger.WarningfCtx(ctx, "Failed to retrieve BaseDir from worker pod: env var not set. Using default BaseDir.\n")
		baseDir = utils.GetBaseDir()
	} else {
		baseDir = config.BaseDir
	}

	secretsToDelete, secretsToSkip := podmanutils.FetchSecretsToDelete(pods)
	volumesToDelete, volumesToSkip := podmanutils.FetchVolumesToDelete(pods)

	logger.InfofCtx(ctx, "Using base directory for cleanup: %s\n", baseDir)

	if err := podmanutils.DeletePods(ctx, rt, pods); err != nil {
		return err
	}

	if err := podmanutils.DeleteSecrets(ctx, rt, secretsToDelete); err != nil {
		return err
	}

	if err := podmanutils.DeleteVolumes(ctx, rt, volumesToDelete); err != nil {
		return err
	}

	workerDataPath := filepath.Join(baseDir, workerconstants.WorkerDataSubDir)
	if err := podmanutils.RemoveDataDir(ctx, workerDataPath); err != nil {
		return err
	}

	// Delete skip-cleanup resources (secrets and volumes preserved when --skip-cleanup is set)
	if err := podmanutils.CleanupSkippedResources(ctx, rt, secretsToSkip, volumesToSkip, skipCleanup); err != nil {
		return err
	}

	logger.InfolnCtx(ctx, "Worker service removed successfully")

	return nil
}
