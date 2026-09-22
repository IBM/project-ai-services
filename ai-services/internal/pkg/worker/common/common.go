package common

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
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

// WorkerPodConfig holds configuration recovered from the running worker pod.
type WorkerPodConfig struct {
	// BaseDir is the base directory recovered from the AI_SERVICES_BASE_DIR
	// env var injected by worker.yaml.tmpl at deploy time.
	BaseDir string
}

// PerformCleanup executes all cleanup operations after confirmation:
// retrieves the worker pod config, deletes the pods, and removes the worker
// data directory.
func PerformCleanup(ctx context.Context, rt runtime.Runtime, pods []types.Pod, skipCleanup bool) error {
	logger.InfolnCtx(ctx, "Proceeding with deletion...")

	var baseDir string

	config, err := getWorkerPodConfig(ctx, rt, pods)
	if err != nil {
		logger.WarningfCtx(ctx, "Failed to retrieve BaseDir from worker pod: %v. Using default BaseDir.\n", err)
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

	logger.Infoln("Worker service removed successfully")

	return nil
}

// getWorkerPodConfig retrieves worker pod configuration by inspecting
// the running pod and its containers. It extracts the AI_SERVICES_BASE_DIR
// environment variable injected by worker.yaml.tmpl at deploy time.
func getWorkerPodConfig(ctx context.Context, rt runtime.Runtime, pods []types.Pod) (*WorkerPodConfig, error) {
	podID := ""
	for _, pod := range pods {
		if pod.Name == workerconstants.WorkerPodName {
			podID = pod.ID
		}
	}
	if podID == "" {
		return nil, fmt.Errorf("no pod found with name '%s'", workerconstants.WorkerPodName)
	}
	pInfo, err := rt.InspectPod(ctx, podID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect pod %s: %w", podID, err)
	}

	config := &WorkerPodConfig{}

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
func extractConfigFromEnv(env map[string]string, config *WorkerPodConfig) {
	if value, ok := env[workerconstants.BaseDirEnvVar]; ok {
		config.BaseDir = value
	}
}
