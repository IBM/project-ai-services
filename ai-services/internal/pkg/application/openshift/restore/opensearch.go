package restore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	appcommon "github.com/project-ai-services/ai-services/internal/pkg/application/common"
	"github.com/project-ai-services/ai-services/internal/pkg/application/common/opensearch"
	"github.com/project-ai-services/ai-services/internal/pkg/application/openshift/common"
	podmanRestore "github.com/project-ai-services/ai-services/internal/pkg/application/podman/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	containerBackupPath = "/tmp/opensearch_backup"
)

// RestoreOpenSearch restores OpenSearch data for OpenShift runtime.
func RestoreOpenSearch(ctx context.Context, applicationID, backupFile string) error {
	logger.Infof("Restoring OpenSearch data for OpenShift application: %s\n", applicationID)
	logger.Infof("Backup file: %s\n", backupFile)
	logger.Infoln("OpenSearch Import (Sidecar Pod Approach)")

	// Get absolute path to backup file
	absFilename, err := filepath.Abs(backupFile)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for backup file: %w", err)
	}

	// Extract and locate backup directory
	backupDir, cleanup, err := podmanRestore.ExtractAndLocateBackup(absFilename)
	if err != nil {
		return err
	}
	defer cleanup()

	// Find OpenSearch pod
	namespace, podName, err := common.FindOpenSearchPod(applicationID)
	if err != nil {
		return err
	}

	logger.Infof("Namespace: %s\n", namespace)
	logger.Infof("OpenSearch Pod: %s\n", podName)

	// Get OpenSearch service name
	serviceName, err := common.GetOpenSearchService(applicationID, namespace)
	if err != nil {
		return fmt.Errorf("failed to get OpenSearch service: %w", err)
	}

	// Create sidecar pod and perform restore
	return manageSidecarPod(ctx, namespace, serviceName, backupDir)
}

// manageSidecarPod manages the lifecycle of a sidecar pod for restore operations.
func manageSidecarPod(ctx context.Context, namespace, serviceName, backupDir string) error {
	sidecarName := common.GenerateSidecarName("opensearch-restore-sidecar")

	// Create sidecar pod
	podCreated, err := common.CreateSidecarPod(sidecarName, namespace)
	if err != nil {
		return fmt.Errorf("failed to create sidecar pod: %w", err)
	}

	// Ensure cleanup happens
	if podCreated {
		defer common.CleanupPod(sidecarName, namespace)
	}

	// Perform restore operations
	return performRestore(ctx, sidecarName, namespace, serviceName, backupDir)
}

// performRestore performs the actual restore operations in the sidecar pod.
func performRestore(ctx context.Context, podName, namespace, serviceName, backupDir string) error {
	// Get OpenSearch password
	osPassword, err := common.GetOpenSearchPasswordFromSecret(namespace)
	if err != nil {
		return fmt.Errorf("failed to get OpenSearch password: %w", err)
	}

	// Determine backup paths
	backupOSDir, containerBackupPath, err := determineBackupPaths(backupDir)
	if err != nil {
		return err
	}

	// Copy backup to sidecar pod
	if err := copyBackupToSidecar(podName, namespace, backupOSDir, containerBackupPath); err != nil {
		return err
	}

	// Construct OpenSearch host using service name and port
	osHost := fmt.Sprintf("%s:9200", serviceName)
	logger.Infof("OpenSearch host: %s\n", osHost)

	// Perform restore with curl
	if err := performRestoreWithCurl(ctx, podName, namespace, osHost, osPassword, containerBackupPath); err != nil {
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

// copyBackupToSidecar copies backup files to the sidecar pod using oc cp.
func copyBackupToSidecar(podName, namespace, backupOSDir, containerBackupPath string) error {
	logger.Infoln("Copying backup files to sidecar pod...")

	// Create backup directory in pod
	script := fmt.Sprintf("mkdir -p %s", containerBackupPath)
	cmd := exec.Command("oc", "exec", podName, "-n", namespace, "--", "sh", "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create backup directory in pod: %w, output: %s", err, string(output))
	}

	// Copy files using oc cp command (executed on host, not in pod)
	// Use "/." suffix to copy directory contents, not the directory itself
	// Format: oc cp <local-path>/. <namespace>/<pod-name>:<remote-path>
	backupOSDirWithContents := backupOSDir + "/."
	cmd = exec.Command("oc", "cp", backupOSDirWithContents, fmt.Sprintf("%s/%s:%s", namespace, podName, containerBackupPath), "-n", namespace)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to copy backup files: %w, output: %s", err, string(output))
	}

	return nil
}

// performRestoreWithCurl performs the OpenSearch restore using curl commands in the pod.
func performRestoreWithCurl(ctx context.Context, podName, namespace, osHost, osPassword, backupDir string) error {
	// List and validate indices
	indices, err := listBackupIndices(podName, namespace, backupDir)
	if err != nil {
		return err
	}

	logger.Infof("Found %d indices to restore\n", len(indices))

	// Restore each index with error tracking
	return restoreAllIndices(ctx, podName, namespace, osHost, osPassword, backupDir, indices)
}

// listBackupIndices lists and validates index names from backup files.
func listBackupIndices(podName, namespace, backupDir string) ([]string, error) {
	listScript := fmt.Sprintf("cd %s && ls *_data.json 2>/dev/null | sed 's/_data.json//' || true", backupDir)

	output, err := common.ExecInPodWithOutput(podName, namespace, listScript)
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
func restoreAllIndices(ctx context.Context, podName, namespace, osHost, osPassword, backupDir string, indices []string) error {
	restoredCount := 0
	var errors []error

	for _, indexName := range indices {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("restore cancelled: %w", ctx.Err())
		default:
		}

		if err := restoreIndex(podName, namespace, osHost, osPassword, backupDir, indexName); err != nil {
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

// restoreIndex restores a single index using curl in the pod.
func restoreIndex(podName, namespace, osHost, osPassword, backupDir, indexName string) error {
	logger.Infof("  Restoring index: %s\n", indexName)

	// Step 1: Delete existing index if it exists
	logger.Infoln("    Cleaning up existing index...")
	deleteScript := opensearch.GenerateDeleteIndexScript(osHost, indexName)
	wrappedDelete := opensearch.WrapScriptWithPassword(osPassword, deleteScript)
	if err := common.ExecInPod(podName, namespace, wrappedDelete); err != nil {
		logger.Warningf("    Failed to delete existing index (may not exist): %v\n", err)
	} else {
		logger.Infoln("    ✓ Existing index cleaned up")
	}

	// Step 2: Create index with settings and mappings
	logger.Infoln("    Creating index with mappings...")
	createScript := opensearch.GenerateCreateIndexScript(osHost, backupDir, indexName)
	wrappedCreate := opensearch.WrapScriptWithPassword(osPassword, createScript)
	if err := common.ExecInPod(podName, namespace, wrappedCreate); err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}
	logger.Infoln("    ✓ Index created")

	// Step 3: Insert data - Bulk index documents
	logger.Infoln("    Inserting documents...")
	bulkScript := opensearch.GenerateBulkIndexScript(osHost, backupDir, indexName)
	wrappedBulk := opensearch.WrapScriptWithPassword(osPassword, bulkScript)
	if err := common.ExecInPod(podName, namespace, wrappedBulk); err != nil {
		return fmt.Errorf("failed to bulk index documents: %w", err)
	}
	logger.Infoln("    ✓ Documents inserted")

	// Step 4: Refresh index to make documents searchable
	refreshScript := opensearch.GenerateRefreshIndexScript(osHost, indexName)
	wrappedRefresh := opensearch.WrapScriptWithPassword(osPassword, refreshScript)
	if err := common.ExecInPod(podName, namespace, wrappedRefresh); err != nil {
		return fmt.Errorf("failed to refresh index: %w", err)
	}

	logger.Infoln("    ✓ Index restored successfully")

	return nil
}

// Made with Bob
