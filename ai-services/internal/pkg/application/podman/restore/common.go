package restore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// ExtractBackup extracts the backup archive into a temp directory and returns its path
// along with a cleanup func. Use LocateBackupParts to find what the archive contains.
func ExtractBackup(backupFile string) (string, func(), error) {
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

	return tempDir, cleanup, nil
}

// LocateBackupParts returns the OpenSearch and digitize backup directories found in an
// extracted archive. A part that is absent is returned as "". It supports the combined
// archive as well as the older single-part archives. An archive with neither part is an error.
func LocateBackupParts(rootDir string) (opensearchDir, digitizeDir string, err error) {
	// Combined and OpenSearch-only archives: opensearch_backup/.
	// Oldest OpenSearch format: backup/opensearch/.
	for _, dir := range []string{
		filepath.Join(rootDir, "opensearch_backup"),
		filepath.Join(rootDir, "backup", "opensearch"),
	} {
		if isDir(dir) {
			opensearchDir = dir

			break
		}
	}

	// Combined and digitize-only archives: backup/cache/.
	if dir := filepath.Join(rootDir, "backup"); isDir(filepath.Join(dir, "cache")) {
		digitizeDir = dir
	}

	if opensearchDir == "" && digitizeDir == "" {
		return "", "", fmt.Errorf("no OpenSearch or digitize backup data found in archive")
	}

	return opensearchDir, digitizeDir, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

// Made with Bob
