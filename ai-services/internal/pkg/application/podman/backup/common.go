package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// CopyBackup copies the OpenSearch backup files from the container into stageDir
// (backup_info.json and opensearch_backup/).
func CopyBackup(ctx context.Context, containerID, containerBackupPath, stageDir string) error {
	logger.Infof("Copying backup files from container to host...\n")

	// Copy backup_info.json from container
	backupInfoSrc := fmt.Sprintf("%s:/tmp/backup_info.json", containerID)
	backupInfoDest := filepath.Join(stageDir, "backup_info.json")
	cpInfoCmd := exec.CommandContext(ctx, "podman", "cp", backupInfoSrc, backupInfoDest)
	if output, err := cpInfoCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy backup_info.json: %w, output: %s", err, string(output))
	}

	// Copy opensearch_backup directory from container
	// Using "/." to copy contents of directory
	backupDirSrc := fmt.Sprintf("%s:%s/.", containerID, containerBackupPath)
	backupDirDest := filepath.Join(stageDir, "opensearch_backup")

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

	return nil
}

// Made with Bob
