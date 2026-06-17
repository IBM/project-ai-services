package backup

import (
	"context"
	"fmt"
	"strings"
	"time"

	commonBackup "github.com/project-ai-services/ai-services/internal/pkg/application/common/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/common"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// BackupOpenSearch performs OpenSearch backup using a sidecar container.
func BackupOpenSearch(ctx context.Context, podID, backupFile string) error {
	sidecarName := fmt.Sprintf("opensearch-backup-sidecar-%d", time.Now().Unix())

	// Create podman client to use runtime methods
	pc, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to create podman client: %w", err)
	}

	// Use the generic sidecar lifecycle management from runtime package
	return pc.ManageSidecarLifecycle(
		podID,
		sidecarName,
		vars.ToolImage,
		[]string{"sleep", "3600"},
		func(ctx context.Context, containerID string) error {
			// Prepare sidecar and perform backup
			return prepareSidecarAndBackup(ctx, pc, containerID, backupFile)
		},
	)
}

// prepareSidecarAndBackup prepares the sidecar container and performs the backup.
func prepareSidecarAndBackup(ctx context.Context, pc *podman.PodmanClient, containerID, backupFile string) error {
	// Get OpenSearch password from secret
	osPassword, err := common.GetOpenSearchPasswordFromSecret(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to get OpenSearch password: %w", err)
	}

	// Create backup directory in container
	containerBackupPath := "/tmp/opensearch_backup"
	if err := pc.ExecInContainer(containerID, []string{"mkdir", "-p", containerBackupPath}); err != nil {
		return fmt.Errorf("failed to create backup directory in container: %w", err)
	}

	// Perform backup using curl
	if err := performBackupWithCurl(ctx, pc, containerID, "localhost:9200", osPassword, containerBackupPath); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Copy backup files from container to host, then create tar archive on host
	if err := CopyAndTarBackup(ctx, containerID, containerBackupPath, backupFile); err != nil {
		return fmt.Errorf("failed to copy and archive backup: %w", err)
	}

	logger.Infoln("OpenSearch backup completed!")

	return nil
}

// performBackupWithCurl performs the OpenSearch backup using curl commands in container.
func performBackupWithCurl(ctx context.Context, pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir string) error {
	logger.Infoln("Exporting OpenSearch indices...")

	indices, err := listRagIndices(pc, containerID, osHost, osPassword)
	if err != nil {
		return err
	}

	if len(indices) == 0 {
		logger.Warningf("No indices found starting with 'rag'\n")

		return nil
	}

	logger.Infof("Found %d indices to backup\n", len(indices))

	backedUpCount, lastErr := backupIndices(ctx, pc, containerID, osHost, osPassword, backupDir, indices)

	if err := handleBackupResults(backedUpCount, len(indices), lastErr); err != nil {
		return err
	}

	// Create backup_info.json
	if err := createBackupInfo(pc, containerID, backupDir); err != nil {
		logger.Warningf("Failed to create backup_info.json: %v\n", err)
	}

	return nil
}

// listRagIndices lists all indices that start with "rag".
func listRagIndices(pc *podman.PodmanClient, containerID, osHost, osPassword string) ([]string, error) {
	listScript := commonBackup.ListRagIndicesScript(osHost)
	wrappedScript := wrapScriptWithPassword(osPassword, listScript)

	output, err := pc.ExecInContainerWithOutput(containerID, []string{"sh", "-c", wrappedScript})
	if err != nil {
		return nil, fmt.Errorf("failed to list indices: %w, output: %s", err, output)
	}

	return commonBackup.ParseIndicesList(output), nil
}

// backupIndices backs up all provided indices and returns the count and any error.
func backupIndices(ctx context.Context, pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir string, indices []string) (int, error) {
	backedUpCount := 0
	var lastErr error

	for _, indexName := range indices {
		if err := commonBackup.CheckContextCancellation(ctx, backedUpCount); err != nil {
			return backedUpCount, err
		}

		indexName = strings.TrimSpace(indexName)
		if indexName == "" {
			continue
		}

		if err := backupIndexWithCurl(ctx, pc, containerID, osHost, osPassword, backupDir, indexName); err != nil {
			logger.Errorf("Failed to backup index %s: %v\n", indexName, err)
			lastErr = err

			continue
		}

		backedUpCount++
	}

	return backedUpCount, lastErr
}

// handleBackupResults checks backup results and logs appropriate messages.
func handleBackupResults(backedUpCount, totalCount int, lastErr error) error {
	return commonBackup.HandleBackupResults(backedUpCount, totalCount, lastErr)
}

// backupIndexWithCurl backs up a single index using curl in container.
func backupIndexWithCurl(ctx context.Context, pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	logger.Infof("  Exporting index: %s\n", indexName)

	if err := exportIndexMetadata(pc, containerID, osHost, osPassword, backupDir, indexName); err != nil {
		return err
	}

	if err := exportIndexData(pc, containerID, osHost, osPassword, backupDir, indexName); err != nil {
		return err
	}

	countDocuments(pc, containerID, backupDir, indexName)

	return nil
}

// exportIndexMetadata exports mapping and settings for an index using environment variables for password.
func exportIndexMetadata(pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	// Export mapping
	mappingScript := commonBackup.GenerateExportMappingScript(osHost, indexName, backupDir)
	wrappedMapping := wrapScriptWithPassword(osPassword, mappingScript)
	if err := pc.ExecInContainer(containerID, []string{"sh", "-c", wrappedMapping}); err != nil {
		return fmt.Errorf("failed to export mapping: %w", err)
	}

	// Export settings
	settingsScript := commonBackup.GenerateExportSettingsScript(osHost, indexName, backupDir)
	wrappedSettings := wrapScriptWithPassword(osPassword, settingsScript)
	if err := pc.ExecInContainer(containerID, []string{"sh", "-c", wrappedSettings}); err != nil {
		return fmt.Errorf("failed to export settings: %w", err)
	}

	return nil
}

// exportIndexData exports all documents from an index using scroll API with environment variables for password.
func exportIndexData(pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	// First, initiate scroll
	scrollInitScript := commonBackup.GenerateScrollInitScript(osHost, indexName)
	wrappedInit := wrapScriptWithPassword(osPassword, scrollInitScript)
	if err := pc.ExecInContainer(containerID, []string{"sh", "-c", wrappedInit}); err != nil {
		return fmt.Errorf("failed to initiate scroll: %w", err)
	}

	// Extract scroll_id and hits with improved error handling and loop protection
	extractScript := commonBackup.GenerateScrollExportScript(osHost, backupDir, indexName)
	wrappedExtract := wrapScriptWithPassword(osPassword, extractScript)
	if err := pc.ExecInContainer(containerID, []string{"sh", "-c", wrappedExtract}); err != nil {
		return fmt.Errorf("failed to export data: %w", err)
	}

	return nil
}

// countDocuments counts and logs the number of documents in the backup.
func countDocuments(pc *podman.PodmanClient, containerID, backupDir, indexName string) {
	countScript := commonBackup.GenerateCountDocumentsScript(backupDir, indexName)
	countOutput, err := pc.ExecInContainerWithOutput(containerID, []string{"sh", "-c", countScript})
	commonBackup.LogDocumentCount(countOutput, err)
}

// createBackupInfo creates a backup_info.json file with metadata.
func createBackupInfo(pc *podman.PodmanClient, containerID, backupDir string) error {
	infoScript := commonBackup.GenerateBackupInfoScript(backupDir)

	return pc.ExecInContainer(containerID, []string{"sh", "-c", infoScript})
}

// wrapScriptWithPassword wraps a script with password environment variable setup.
func wrapScriptWithPassword(password, script string) string {
	escapedPassword := strings.ReplaceAll(password, "'", "'\\''")

	return fmt.Sprintf(`
OS_PASSWORD='%s'
export OS_PASSWORD
%s
`, escapedPassword, script)
}

// Made with Bob
