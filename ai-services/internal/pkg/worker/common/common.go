package common

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
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

// WorkerPodConfig holds configuration recovered from the running worker pod.
type WorkerPodConfig struct {
	// BaseDir is the base directory recovered from the AI_SERVICES_BASE_DIR
	// env var injected by worker.yaml.tmpl at deploy time.
	BaseDir string
}

// PerformPodmanCleanup removes a Podman-based worker deployment by deleting its pods, secrets,
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
func PerformPodmanCleanup(ctx context.Context, rt runtime.Runtime, pods []types.Pod, skipCleanup bool) error {
	logger.InfolnCtx(ctx, "Proceeding with deletion...")

	var baseDir string

	config, err := getWorkerPodConfig(ctx, rt, pods)
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

// getWorkerPodConfig retrieves worker pod configuration by inspecting
// the running pod and its containers. It extracts the AI_SERVICES_BASE_DIR
// environment variable injected by worker.yaml.tmpl at deploy time.
func getWorkerPodConfig(ctx context.Context, rt runtime.Runtime, pods []types.Pod) (*WorkerPodConfig, error) {
	podID := ""
	for _, pod := range pods {
		if pod.Name == workerconstants.WorkerPodName {
			podID = pod.ID

			break
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

// PerformOpenshiftCleanup uninstalls the worker Helm release from the given OpenShift namespace
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
func PerformOpenshiftCleanup(ctx context.Context, rt runtime.Runtime, namespace string, skipCleanup bool) error {
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
