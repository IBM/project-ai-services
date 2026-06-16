package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// CopyAndTarBackup copies backup files from container to host and creates tar archive on host.
func CopyAndTarBackup(ctx context.Context, containerID, containerBackupPath, backupFile string) error {
	logger.Infof("Copying backup files from container to host...\n")

	// Create temporary directory on host for backup files
	tempDir, err := os.MkdirTemp("", "opensearch-backup-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			logger.Warningf("Failed to remove temp directory: %v\n", err)
		}
	}()

	// Copy backup_info.json from container
	backupInfoSrc := fmt.Sprintf("%s:/tmp/backup_info.json", containerID)
	backupInfoDest := filepath.Join(tempDir, "backup_info.json")
	cpInfoCmd := exec.CommandContext(ctx, "podman", "cp", backupInfoSrc, backupInfoDest)
	if output, err := cpInfoCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy backup_info.json: %w, output: %s", err, string(output))
	}

	// Copy opensearch_backup directory from container
	// Using "/." to copy contents of directory
	backupDirSrc := fmt.Sprintf("%s:%s/.", containerID, containerBackupPath)
	backupDirDest := filepath.Join(tempDir, "opensearch_backup")

	const dirPerm = 0o755
	// Create destination directory
	if err := os.MkdirAll(backupDirDest, dirPerm); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	cpDirCmd := exec.CommandContext(ctx, "podman", "cp", backupDirSrc, backupDirDest)
	if output, err := cpDirCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy backup directory: %w, output: %s", err, string(output))
	}

	logger.Infof("✓ Backup files copied to host\n")

	// Create tar.gz archive on host
	logger.Infof("Creating tar.gz archive on host...\n")

	if err := CreateTarGzArchive(tempDir, backupFile, []string{"backup_info.json", "opensearch_backup"}); err != nil {
		return err
	}

	LogArchiveSize(backupFile)

	return nil
}

// Made with Bob
