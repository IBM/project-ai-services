package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
