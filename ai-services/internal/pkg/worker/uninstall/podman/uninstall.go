package podman

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
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

	config, err := getWorkerCaddyPodConfig(ctx, rt, pods)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to retrieve BaseDir from worker pod: %v. Using default BaseDir.\n", err)
		baseDir = utils.GetBaseDir()
	} else {
		baseDir = config.BaseDir
	}

	volumesToDelete := fetchVolumesToDelete(pods)

	logger.InfofCtx(ctx, "Using base directory for cleanup: %s\n", baseDir)

	if err := podmanutils.DeletePods(ctx, rt, pods); err != nil {
		return err
	}

	secretsToDelete := []string{constants.PodmanAuthSecret}
	if err := podmanutils.DeleteSecrets(ctx, rt, secretsToDelete); err != nil {
		return err
	}

	if err := podmanutils.DeleteVolumes(ctx, rt, volumesToDelete); err != nil {
		return err
	}

	workerDataPath := filepath.Join(baseDir, workerconstants.WorkerDataSubDir)

	return podmanutils.RemoveDataDir(ctx, workerDataPath)
}
func fetchVolumesToDelete(pods []types.Pod) []string {
	volumesToDelete := []string{}
	for _, pod := range pods {
		if volumeNames, ok := pod.Labels[constants.VolumeLabel]; ok && volumeNames != "" {
			volumes := strings.Split(volumeNames, ",")
			volumesToDelete = append(volumesToDelete, volumes...)
		}
	}

	return volumesToDelete
}

// getWorkerCaddyPodConfig retrieves worker Caddy pod configuration by inspecting
// the running pod and its containers. It extracts the AI_SERVICES_BASE_DIR
// environment variable injected by caddy.yaml.tmpl at deploy time.
func getWorkerCaddyPodConfig(ctx context.Context, rt runtime.Runtime, pods []types.Pod) (*WorkerCaddyConfig, error) {
	podID := ""
	for _, pod := range pods {
		if pod.Name == workerconstants.WorkerCaddyPodName {
			podID = pod.ID
		}
	}
	if podID == "" {
		return nil, fmt.Errorf("no pod found with name '%s'", workerconstants.WorkerCaddyPodName)
	}
	pInfo, err := rt.InspectPod(ctx, podID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect pod %s: %w", podID, err)
	}

	config := &WorkerCaddyConfig{}

	for _, container := range pInfo.Containers {
		cInfo, err := rt.InspectContainer(ctx, container.ID)
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
