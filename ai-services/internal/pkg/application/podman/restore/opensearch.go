package restore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	appcommon "github.com/project-ai-services/ai-services/internal/pkg/application/common"
	"github.com/project-ai-services/ai-services/internal/pkg/application/common/opensearch"
	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/common"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	defaultOpenSearchHost = "localhost:9200"
	containerBackupPath   = "/tmp/opensearch_backup"
)

// RestoreOpenSearch restores OpenSearch data using podman sidecar approach.
func RestoreOpenSearch(ctx context.Context, templateID, backupFile string) error {
	logger.Infof("Restoring OpenSearch data for template: %s\n", templateID)
	logger.Infoln("OpenSearch Import (Sidecar Container Approach)")

	// Find OpenSearch container and get pod ID using common function
	containerName, podID, err := common.FindContainerAndPod(ctx, templateID)
	if err != nil {
		return err
	}

	logger.Infof("Container: %s\n", containerName)
	logger.Infof("Pod ID: %s\n", podID)

	// Extract and locate backup directory
	backupDir, cleanup, err := ExtractAndLocateBackup(backupFile)
	if err != nil {
		return err
	}
	defer cleanup()

	// Manage sidecar lifecycle and perform restore
	return manageSidecarWithGo(ctx, podID, backupDir)
}

// manageSidecarWithGo manages the lifecycle of a podman sidecar container using runtime package.
func manageSidecarWithGo(ctx context.Context, podID, backupDir string) error {
	sidecarName := fmt.Sprintf("opensearch-restore-sidecar-%d-%d", time.Now().Unix(), os.Getpid())

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
			// Prepare sidecar and perform restore
			return prepareSidecarAndRestore(ctx, containerID, backupDir)
		},
	)
}

// prepareSidecarAndRestore prepares the sidecar container and performs the restore.
func prepareSidecarAndRestore(ctx context.Context, containerID, backupDir string) error {
	// Create podman client once for all operations
	pc, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to create podman client: %w", err)
	}

	osPassword, err := common.GetOpenSearchPasswordFromSecret(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to get OpenSearch password: %w", err)
	}

	backupOSDir, containerBackupPath, err := determineBackupPaths(backupDir)
	if err != nil {
		return err
	}

	if err := copyBackupToSidecar(pc, containerID, backupOSDir, containerBackupPath); err != nil {
		return err
	}

	if err := performRestoreWithCurl(ctx, pc, containerID, defaultOpenSearchHost, osPassword, containerBackupPath); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	logger.Infoln("OpenSearch import completed!")

	return nil
}

// determineBackupPaths determines the backup directory paths based on format.
func determineBackupPaths(backupDir string) (string, string, error) {
	var backupOSDir string

	if filepath.Base(backupDir) == "opensearch_backup" {
		backupOSDir = backupDir
	} else {
		backupOSDir = filepath.Join(backupDir, "opensearch")
	}

	if _, err := os.Stat(backupOSDir); os.IsNotExist(err) {
		return "", "", fmt.Errorf("OpenSearch backup directory not found: %s", backupOSDir)
	}

	return backupOSDir, containerBackupPath, nil
}

// copyBackupToSidecar copies backup files to the sidecar container.
func copyBackupToSidecar(pc *podman.PodmanClient, containerID, backupOSDir, containerBackupPath string) error {
	logger.Infoln("Copying backup files to sidecar...")

	if err := pc.CopyDirToContainer(containerID, backupOSDir, containerBackupPath); err != nil {
		return fmt.Errorf("failed to copy backup files: %w", err)
	}

	return nil
}

// execInContainer executes a command in a container using the runtime package.
func execInContainer(pc *podman.PodmanClient, containerID string, cmd []string) error {
	return pc.ExecInContainer(containerID, cmd)
}

// performRestoreWithCurl performs the OpenSearch restore using curl commands in container.
func performRestoreWithCurl(ctx context.Context, pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir string) error {
	// Verify backup directory exists in container
	if err := verifyBackupDirectory(pc, containerID, backupDir); err != nil {
		return err
	}

	// List and validate indices
	indices, err := listBackupIndices(pc, containerID, backupDir)
	if err != nil {
		return err
	}

	logger.Infof("Found %d indices to restore\n", len(indices))

	// Restore each index with error tracking
	return restoreAllIndices(ctx, pc, containerID, osHost, osPassword, backupDir, indices)
}

// verifyBackupDirectory checks if the backup directory exists in the container.
func verifyBackupDirectory(pc *podman.PodmanClient, containerID, backupDir string) error {
	verifyScript := fmt.Sprintf("test -d %s && echo 'exists' || echo 'not found'", backupDir)
	if err := execInContainer(pc, containerID, []string{"sh", "-c", verifyScript}); err != nil {
		return fmt.Errorf("backup directory not found in container: %w", err)
	}

	return nil
}

// listBackupIndices lists and validates index names from backup files.
func listBackupIndices(pc *podman.PodmanClient, containerID, backupDir string) ([]string, error) {
	listScript := fmt.Sprintf("cd %s && ls *_data.json 2>/dev/null | sed 's/_data.json//' || true", backupDir)

	output, err := pc.ExecInContainerWithOutput(containerID, []string{"sh", "-c", listScript})
	if err != nil {
		return nil, fmt.Errorf("failed to list indices: %w, output: %s", err, output)
	}

	indicesStr := strings.TrimSpace(output)
	if indicesStr == "" {
		return nil, fmt.Errorf("no indices found in backup directory")
	}

	indices := strings.Split(indicesStr, "\n")

	// Validate index names to prevent command injection
	validIndices := make([]string, 0, len(indices))
	for _, indexName := range indices {
		indexName = strings.TrimSpace(indexName)
		if indexName == "" {
			continue
		}
		if err := appcommon.ValidateIndexName(indexName); err != nil {
			logger.Warningf("Skipping invalid index name %s: %v\n", indexName, err)

			continue
		}
		validIndices = append(validIndices, indexName)
	}

	if len(validIndices) == 0 {
		return nil, fmt.Errorf("no valid indices found in backup directory")
	}

	return validIndices, nil
}

// restoreAllIndices restores all indices and tracks errors.
func restoreAllIndices(ctx context.Context, pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir string, indices []string) error {
	restoredCount := 0
	var errors []error

	for _, indexName := range indices {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("restore cancelled: %w", ctx.Err())
		default:
		}

		if err := restoreIndexWithCurl(ctx, pc, containerID, osHost, osPassword, backupDir, indexName); err != nil {
			logger.Errorf("Failed to restore index %s: %v\n", indexName, err)
			errors = append(errors, fmt.Errorf("index %s: %w", indexName, err))

			continue
		}
		restoredCount++
	}

	if restoredCount == 0 && len(errors) > 0 {
		return fmt.Errorf("failed to restore any indices: %d errors occurred", len(errors))
	}

	if len(errors) > 0 {
		logger.Warningf("Restore completed with %d errors. Successfully restored %d/%d indices\n", len(errors), restoredCount, len(indices))
	} else {
		logger.Infof("✓ Restore completed successfully. Restored %d indices\n", restoredCount)
	}

	return nil
}

// restoreIndexWithCurl restores a single index using curl in container.
// Password is passed via environment variable to avoid exposure in process lists.
// The restore process follows: cleanup (delete existing) -> create -> insert data.
func restoreIndexWithCurl(ctx context.Context, pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	logger.Infof("  Restoring index: %s\n", indexName)

	// Verify required backup files exist
	if err := verifyBackupFiles(pc, containerID, backupDir, indexName); err != nil {
		return err
	}

	// Step 1: Cleanup - Delete existing index if it exists
	logger.Infoln("    Cleaning up existing index...")
	if err := deleteExistingIndex(pc, containerID, osHost, osPassword, indexName); err != nil {
		logger.Warningf("    Failed to delete existing index (may not exist): %v\n", err)
	} else {
		logger.Infof("    ✓ Existing index cleaned up\n")
	}

	// Step 2: Create index with settings and mappings
	logger.Infof("    Creating index with mappings...\n")
	if err := createIndexWithMappings(pc, containerID, osHost, osPassword, backupDir, indexName); err != nil {
		return err
	}
	logger.Infof("    ✓ Index created\n")

	// Step 3: Insert data - Bulk index documents
	logger.Infof("    Inserting documents...\n")
	if err := bulkIndexDocuments(pc, containerID, osHost, osPassword, backupDir, indexName); err != nil {
		return err
	}
	logger.Infof("    ✓ Documents inserted\n")

	// Step 4: Refresh index to make documents searchable
	if err := refreshIndex(pc, containerID, osHost, osPassword, indexName); err != nil {
		return err
	}

	logger.Infof("    ✓ Index restored successfully\n")

	return nil
}

// verifyBackupFiles checks if all required backup files exist.
func verifyBackupFiles(pc *podman.PodmanClient, containerID, backupDir, indexName string) error {
	requiredFiles := []string{
		fmt.Sprintf("%s/%s_mapping.json", backupDir, indexName),
		fmt.Sprintf("%s/%s_settings.json", backupDir, indexName),
		fmt.Sprintf("%s/%s_data.json", backupDir, indexName),
	}

	for _, file := range requiredFiles {
		verifyScript := fmt.Sprintf("test -f %s && echo 'exists' || echo 'not found'", file)
		if err := execInContainer(pc, containerID, []string{"sh", "-c", verifyScript}); err != nil {
			return fmt.Errorf("required backup file not found: %s", file)
		}
	}

	return nil
}

// deleteExistingIndex deletes an existing index if it exists.
func deleteExistingIndex(pc *podman.PodmanClient, containerID, osHost, osPassword, indexName string) error {
	// Generate delete script using common function
	deleteScript := opensearch.GenerateDeleteIndexScript(osHost, indexName)

	// Wrap with password environment variable
	wrappedScript := opensearch.WrapScriptWithPassword(osPassword, deleteScript)

	return pc.ExecInContainerWithEnv(containerID, map[string]string{"OS_PASSWORD": osPassword}, wrappedScript)
}

// createIndexWithMappings creates an index with settings and mappings.
func createIndexWithMappings(pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	// Generate create index script using common function
	createScript := opensearch.GenerateCreateIndexScript(osHost, backupDir, indexName)

	// Wrap with password environment variable
	wrappedScript := opensearch.WrapScriptWithPassword(osPassword, createScript)

	if err := pc.ExecInContainerWithEnv(containerID, map[string]string{"OS_PASSWORD": osPassword}, wrappedScript); err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	return nil
}

// bulkIndexDocuments performs bulk indexing of documents.
func bulkIndexDocuments(pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	// Generate bulk index script using common function
	bulkScript := opensearch.GenerateBulkIndexScript(osHost, backupDir, indexName)

	// Wrap with password environment variable
	wrappedScript := opensearch.WrapScriptWithPassword(osPassword, bulkScript)

	if err := pc.ExecInContainerWithEnv(containerID, map[string]string{"OS_PASSWORD": osPassword}, wrappedScript); err != nil {
		return fmt.Errorf("failed to bulk index documents: %w", err)
	}

	return nil
}

// refreshIndex refreshes an index to make documents searchable.
func refreshIndex(pc *podman.PodmanClient, containerID, osHost, osPassword, indexName string) error {
	// Generate refresh script using common function
	refreshScript := opensearch.GenerateRefreshIndexScript(osHost, indexName)

	// Wrap with password environment variable
	wrappedScript := opensearch.WrapScriptWithPassword(osPassword, refreshScript)

	if err := pc.ExecInContainerWithEnv(containerID, map[string]string{"OS_PASSWORD": osPassword}, wrappedScript); err != nil {
		return fmt.Errorf("failed to refresh index: %w", err)
	}

	return nil
}

// Made with Bob
