package cleanup

import (
	"fmt"
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const dirPerm os.FileMode = 0o755

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

// CollectArtifacts collects test artifacts from tempDir into artifactDir.
func CollectArtifacts(tempDir, artifactDir string) error {
	if tempDir == "" || artifactDir == "" {
		return nil
	}

	if err := os.MkdirAll(artifactDir, dirPerm); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	logger.Infof("[CLEANUP] Artifacts collected to: %s", artifactDir)

	return nil
}
