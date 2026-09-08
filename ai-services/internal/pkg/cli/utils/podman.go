package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// PodmanRun executes `podman <args>` via the CLI and returns combined stdout+stderr.
func PodmanRun(args ...string) ([]byte, error) {
	out, err := exec.Command("podman", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("podman %s: %w (output: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return out, nil
}

// PodmanContainerName extracts the container name from a `podman ps --format json` entry.
// The "Names" field is a JSON array of strings.
func PodmanContainerName(c map[string]any) string {
	switch v := c["Names"].(type) {
	case []any:
		if len(v) > 0 {
			return fmt.Sprintf("%v", v[0])
		}
	case string:
		return v
	}

	return ""
}

// DeleteVolumes removes the specified volumes.
func DeleteVolumes(ctx context.Context, rt runtime.Runtime, volumeNames []string) error {
	if len(volumeNames) == 0 {
		// Just return if there are no volumes to delete.
		return nil
	}

	logger.Infof("Deleting %d volume(s)\n", len(volumeNames))

	var errors []string
	for _, volumeName := range volumeNames {
		logger.Infof("Deleting volume: %s\n", volumeName)

		if err := rt.DeleteVolume(ctx, volumeName); err != nil {
			// Ignore "not found" errors - volume already deleted or never existed
			if utils.IsNotFoundError(err) {
				logger.Infof("Volume %s already deleted or does not exist\n", volumeName)

				continue
			}

			errors = append(errors, fmt.Sprintf("volume %s: %v", volumeName, err))

			continue
		}

		logger.Infof("Successfully deleted volume: %s\n", volumeName)
	}

	// Aggregate errors at the end
	if len(errors) > 0 {
		return fmt.Errorf("failed to remove volumes: \n%s", strings.Join(errors, "\n"))
	}

	return nil
}

// RemoveDataDir deletes the directory at dataPath if it exists, logging a note when absent.
func RemoveDataDir(ctx context.Context, dataPath string) error {
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

// DeletePods force-deletes every pod in the list and aggregates any errors.
func DeletePods(ctx context.Context, rt runtime.Runtime, pods []types.Pod) error {
	var errs []string

	for _, p := range pods {
		logger.InfofCtx(ctx, "Deleting pod: %s\n", p.Name)

		if err := rt.DeletePod(ctx, p.ID, utils.BoolPtr(true)); err != nil {
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

// DeleteSecrets iterates over the provided secret names and removes each one via the
// runtime. "Not found" errors are treated as no-ops (the secret was already deleted
// or never existed) and are logged without aborting the loop. Any other deletion
// error is returned immediately, halting further processing.
func DeleteSecrets(ctx context.Context, rt runtime.Runtime, secrets []string) error {
	for _, secret := range secrets {
		if err := rt.DeleteSecret(ctx, secret); err != nil {
			if utils.IsNotFoundError(err) {
				logger.InfofCtx(ctx, "Secret %s already deleted or does not exist\n", secret)

				continue
			}

			return err
		}
		logger.Infof("Successfully deleted secret: %s\n", secret)
	}

	return nil
}

// FetchVolumesToDelete extracts volume names from pod labels and separates them based on skip-cleanup label.
// Returns two lists: volumes to delete immediately, and volumes to skip (only deleted when --skip-cleanup is not set).
func FetchVolumesToDelete(pods []types.Pod) ([]string, []string) {
	volumeMapToDelete := make(map[string]bool) // Use map to avoid duplicates
	volumeMapToSkip := make(map[string]bool)

	for _, pod := range pods {
		// fetch volume names from pod labels
		if volumeNames, ok := pod.Labels[constants.VolumeLabel]; ok && volumeNames != "" {
			// Check if this pod has skip-cleanup label for volumes
			_, hasSkipLabel := pod.Labels[constants.VolumeSkipLabel]

			// Split comma-separated volume names (in case a pod has multiple volumes)
			volumes := strings.Split(volumeNames, ",")
			for _, volumeName := range volumes {
				volumeName = strings.TrimSpace(volumeName)
				if volumeName != "" {
					if hasSkipLabel {
						volumeMapToSkip[volumeName] = true
					} else {
						volumeMapToDelete[volumeName] = true
					}
				}
			}
		}
	}

	// Convert maps to slices
	volumesToDelete := make([]string, 0, len(volumeMapToDelete))
	for volumeName := range volumeMapToDelete {
		volumesToDelete = append(volumesToDelete, volumeName)
	}

	volumesToSkip := make([]string, 0, len(volumeMapToSkip))
	for volumeName := range volumeMapToSkip {
		volumesToSkip = append(volumesToSkip, volumeName)
	}

	return volumesToDelete, volumesToSkip
}

// CleanupSkippedResources deletes secrets and volumes that are preserved when --skip-cleanup is set.
func CleanupSkippedResources(ctx context.Context, rt runtime.Runtime, secretsToSkip []string, volumesToSkip []string, skipCleanup bool) error {
	if skipCleanup {
		logger.Infoln("Skipping cleanup of preserved resources (--skip-cleanup flag set)")

		return nil
	}

	// Delete catalog secrets
	if err := DeleteSecrets(ctx, rt, secretsToSkip); err != nil {
		return err
	}

	// Delete volumes with skip-cleanup label (only when --skip-cleanup is not set)
	return DeleteVolumes(ctx, rt, volumesToSkip)
}
