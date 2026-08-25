// Package uninstall removes all worker-node components deployed by
// `worker join`: the Caddy reverse-proxy pod and the on-disk worker data
// directory (Caddyfile, caddy state, etc.).
//
// It is intentionally scoped to what `worker join` created.  Application pods
// deployed on the worker by the catalog are the operator's responsibility and
// are not touched here.
package uninstall

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// Options carries the parameters needed to uninstall a worker node.
type Options struct {
	// RuntimeType is the local runtime (podman / openshift).
	RuntimeType types.RuntimeType

	// AutoYes skips the interactive confirmation prompt.
	AutoYes bool
}

// Uninstall removes all worker components deployed by `worker join`.
func Uninstall(ctx context.Context, opts Options) error {
	rt, err := runtime.CreateRuntime(opts.RuntimeType, "")
	if err != nil {
		return fmt.Errorf("worker uninstall: init runtime: %w", err)
	}

	pods, err := rt.ListPods(map[string][]string{
		"label": {workerconstants.WorkerProxyLabel},
	})
	if err != nil {
		return fmt.Errorf("worker uninstall: list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.InfolnCtx(ctx, "No worker pods found — nothing to uninstall.")

		return nil
	}

	logger.Warningln("Ensure no application pods are running on this worker before uninstalling, as they will become unreachable and will need to be deleted manually.")

	if ok, err := confirmUninstall(ctx, pods, opts.AutoYes); err != nil || !ok {
		return err
	}

	return performCleanup(ctx, rt, pods)
}

// ─── internal ─────────────────────────────────────────────────────────────────

// WorkerCaddyConfig holds configuration recovered from the running Caddy pod.
type WorkerCaddyConfig struct {
	// BaseDir is the base directory recovered from the AI_SERVICES_BASE_DIR
	// env var injected by caddy.yaml.tmpl at deploy time.
	BaseDir string
}

// performCleanup executes all cleanup operations after confirmation:
// retrieves the Caddy pod config, deletes the pods, and removes the worker
// data directory.
func performCleanup(ctx context.Context, rt runtime.Runtime, pods []types.Pod) error {
	logger.InfolnCtx(ctx, "Proceeding with deletion...")

	var baseDir string

	config, err := getWorkerCaddyPodConfig(rt, pods[0].ID)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to retrieve BaseDir from worker pod: %v. Using default BaseDir.\n", err)
		baseDir = utils.GetBaseDir()
	} else {
		baseDir = config.BaseDir
	}

	logger.InfofCtx(ctx, "Using base directory for cleanup: %s\n", baseDir)

	if err := deletePods(ctx, rt, pods); err != nil {
		return err
	}

	workerDataPath := filepath.Join(baseDir, workerconstants.WorkerDataSubDir)

	return removeDataDir(ctx, workerDataPath)
}

// getWorkerCaddyPodConfig retrieves worker Caddy pod configuration by inspecting
// the running pod and its containers. It extracts the AI_SERVICES_BASE_DIR
// environment variable injected by caddy.yaml.tmpl at deploy time.
func getWorkerCaddyPodConfig(rt runtime.Runtime, podID string) (*WorkerCaddyConfig, error) {
	pInfo, err := rt.InspectPod(podID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect pod %s: %w", podID, err)
	}

	config := &WorkerCaddyConfig{}

	for _, container := range pInfo.Containers {
		cInfo, err := rt.InspectContainer(container.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect container %s: %w", container.Name, err)
		}

		extractConfigFromEnv(cInfo.Env, config)
	}

	return config, nil
}

// extractConfigFromEnv populates config from Caddy container environment variables.
func extractConfigFromEnv(env map[string]string, config *WorkerCaddyConfig) {
	if value, ok := env[workerconstants.BaseDirEnvVar]; ok {
		config.BaseDir = value
	}
}

// confirmUninstall prints the pod list and prompts the user when autoYes is false.
// Returns (false, nil) when the user declines, (true, nil) when confirmed.
func confirmUninstall(ctx context.Context, pods []types.Pod, autoYes bool) (bool, error) {
	if autoYes {
		return true, nil
	}

	logger.InfolnCtx(ctx, "The following worker pods will be deleted:")

	for _, p := range pods {
		logger.InfofCtx(ctx, "  -> %s\n", p.Name)
	}

	confirmed, err := utils.ConfirmAction("\nDo you want to continue?")
	if err != nil {
		return false, fmt.Errorf("worker uninstall: confirmation: %w", err)
	}

	if !confirmed {
		logger.InfolnCtx(ctx, "Uninstall cancelled.")
	}

	return confirmed, nil
}

// deletePods force-deletes every pod in the list and aggregates any errors.
func deletePods(ctx context.Context, rt runtime.Runtime, pods []types.Pod) error {
	var errs []string

	for _, p := range pods {
		logger.InfofCtx(ctx, "Deleting pod: %s\n", p.Name)

		if err := rt.DeletePod(p.ID, utils.BoolPtr(true)); err != nil {
			errs = append(errs, fmt.Sprintf("pod %s: %v", p.Name, err))

			continue
		}

		logger.InfofCtx(ctx, "Deleted pod: %s\n", p.Name)
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to delete pods:\n%s", strings.Join(errs, "\n"))
	}

	return nil
}

// removeDataDir deletes path if it exists, logging a note when absent.
func removeDataDir(ctx context.Context, dataPath string) error {
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		logger.InfofCtx(ctx, "data directory does not exist, skipping: %s\n", dataPath)

		return nil
	}

	logger.InfofCtx(ctx, "Deleting data at: %s\n", dataPath)

	if err := os.RemoveAll(dataPath); err != nil {
		return fmt.Errorf("failed to remove data directory %s: %w", dataPath, err)
	}

	logger.InfofCtx(ctx, "Successfully removed data at: %s\n", dataPath)

	return nil
}
