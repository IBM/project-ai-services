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
	logger.Infof("Copying backup files from container to host...\n", 0)

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

	logger.Infof("✓ Backup files copied to host\n", 0)

	// Create tar.gz archive on host
	logger.Infof("Creating tar.gz archive on host...\n", 0)

	// Change to temp directory and create tar with relative paths
	tarCmd := exec.CommandContext(ctx, "tar", "-czf", backupFile, "-C", tempDir, "backup_info.json", "opensearch_backup")
	output, err := tarCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create tar archive: %w, output: %s", err, string(output))
	}

	const (
		bytesPerKB = 1024
		bytesPerMB = bytesPerKB * 1024
	)
	// Get file size
	fileInfo, err := os.Stat(backupFile)
	if err == nil {
		sizeMB := float64(fileInfo.Size()) / bytesPerMB
		logger.Infof("✓ Tar archive created: %s (%.2f MB)\n", backupFile, sizeMB, 0)
	} else {
		logger.Infof("✓ Tar archive created: %s\n", backupFile, 0)
	}

	return nil
}

// Made with Bob
