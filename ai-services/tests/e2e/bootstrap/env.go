package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
)

// dirPerm defines the default permission for created directories.
const dirPerm = 0o755 // standard read/write/execute for owner, read/execute for group and others

// PrepareRuntime creates isolated temp directories for tests.
func PrepareRuntime(runID string) string {
	tempDir := filepath.Join("/tmp/ais-e2e", runID)
	if err := os.MkdirAll(tempDir, dirPerm); err != nil {
		fmt.Printf("[BOOTSTRAP] Failed to create temp directory: %v\n", err)

		return ""
	}

	if err := os.Setenv("AI_SERVICES_HOME", tempDir); err != nil {
		fmt.Printf("[BOOTSTRAP] Failed to set AI_SERVICES_HOME: %v\n", err)
	}

	fmt.Printf("[BOOTSTRAP] Temp runtime environment created at: %s\n", tempDir)

	return tempDir
}

// GetRuntimeDir returns the AI_SERVICES_HOME directory.
func GetRuntimeDir() string {
	return os.Getenv("AI_SERVICES_HOME")
}
