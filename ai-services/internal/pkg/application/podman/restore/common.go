package restore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// ExtractAndLocateBackup extracts the backup archive and locates the backup directory.
// This function is shared between digitize and opensearch restore operations.
func ExtractAndLocateBackup(backupFile string) (string, func(), error) {
	logger.Infoln("Extracting backup archive...")

	tempDir, err := os.MkdirTemp("", "restore-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	cleanup := func() {
		if err := os.RemoveAll(tempDir); err != nil {
			logger.Errorf("Failed to cleanup temp directory %s: %v\n", tempDir, err)
		}
	}

	if err := utils.ExtractTarGz(backupFile, tempDir); err != nil {
		cleanup()

		return "", nil, fmt.Errorf("failed to extract backup: %w", err)
	}

	backupDir, err := locateBackupDirectory(tempDir)
	if err != nil {
		cleanup()

		return "", nil, err
	}

	return backupDir, cleanup, nil
}

// locateBackupDirectory determines the backup directory path supporting both formats.
func locateBackupDirectory(tempDir string) (string, error) {
	// Support both old format (backup/) and new format (opensearch_backup/)
	backupDirOld := filepath.Join(tempDir, "backup")
	backupDirNew := filepath.Join(tempDir, "opensearch_backup")

	if _, err := os.Stat(backupDirOld); err == nil {
		return backupDirOld, nil
	}

	if _, err := os.Stat(backupDirNew); err == nil {
		return backupDirNew, nil
	}

	return "", formatBackupNotFoundError(tempDir)
}

// formatBackupNotFoundError creates a detailed error message for missing backup directory.
func formatBackupNotFoundError(tempDir string) error {
	entries, listErr := os.ReadDir(tempDir)
	if listErr != nil {
		return fmt.Errorf("backup directory not found in archive")
	}

	var extractedDirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			extractedDirs = append(extractedDirs, entry.Name())
		}
	}

	if len(extractedDirs) > 0 {
		return fmt.Errorf("backup directory not found in archive")
	}

	return fmt.Errorf("backup directory not found in archive")
}

// Made with Bob
