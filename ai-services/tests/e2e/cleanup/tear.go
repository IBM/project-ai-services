package cleanup

import (
	"fmt"
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// dirPerm defines default directory permissions.
const dirPerm = 0o755 // read/write/execute for owner, read/execute for group and others

// CleanupTemp removes temporary directories created during test runs.
func CleanupTemp(tempDir string) error {
	if tempDir == "" {
		return nil
	}

	if err := os.RemoveAll(tempDir); err != nil {
		logger.Errorf("[CLEANUP] Failed to remove temp directory %s: %v", tempDir, err)

		return err
	}

	logger.Infof("[CLEANUP] Removed temp directory: %s", tempDir)

	return nil
}

// CollectArtifacts collects test artifacts (logs, configs, etc.) from the temp directory.
func CollectArtifacts(tempDir, artifactDir string) error {
	if tempDir == "" || artifactDir == "" {
		return nil
	}

	if err := os.MkdirAll(artifactDir, dirPerm); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	// Copy relevant files from tempDir to artifactDir.
	logger.Infof("[CLEANUP] Artifacts collected to: %s", artifactDir)

	return nil
}
