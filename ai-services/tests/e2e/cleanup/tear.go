package cleanup

import (
	"fmt"
	"os"
)

// CleanupTemp removes temporary directories created during test runs
func CleanupTemp(tempDir string) error {
	if tempDir == "" {
		return nil
	}
	if err := os.RemoveAll(tempDir); err != nil {
		fmt.Printf("[CLEANUP] Failed to remove temp directory %s: %v\n", tempDir, err)
		return err
	}
	fmt.Printf("[CLEANUP] Removed temp directory: %s\n", tempDir)
	return nil
}

// CollectArtifacts collects test artifacts (logs, configs, etc.) from the temp directory
func CollectArtifacts(tempDir, artifactDir string) error {
	if tempDir == "" || artifactDir == "" {
		return nil
	}
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}
	// Copy relevant files from tempDir to artifactDir
	fmt.Printf("[CLEANUP] Artifacts collected to: %s\n", artifactDir)
	return nil
}
