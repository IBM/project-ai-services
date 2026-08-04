package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	commonBackup "github.com/project-ai-services/ai-services/internal/pkg/application/common/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/application/openshift/common"
	podmanBackup "github.com/project-ai-services/ai-services/internal/pkg/application/podman/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	containerBackupPath = "/tmp/opensearch_backup"
)

// BackupOpenSearch performs OpenSearch backup for OpenShift using a sidecar pod.
func BackupOpenSearch(ctx context.Context, applicationID, backupFile string) error {
	logger.Infof("Backing up OpenSearch data for OpenShift application: %s\n", applicationID)
	logger.Infoln("OpenSearch Backup (Sidecar Pod Approach)")

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

	// Create sidecar pod and perform backup
	return manageSidecarPod(ctx, namespace, serviceName, backupFile)
}

// manageSidecarPod manages the lifecycle of a sidecar pod for backup operations.
func manageSidecarPod(ctx context.Context, namespace, serviceName, backupFile string) error {
	sidecarName := common.GenerateSidecarName("opensearch-backup-sidecar")

	// Create sidecar pod
	podCreated, err := common.CreateSidecarPod(sidecarName, namespace)
	if err != nil {
		return fmt.Errorf("failed to create sidecar pod: %w", err)
	}

	// Ensure cleanup happens
	if podCreated {
		defer common.CleanupPod(sidecarName, namespace)
	}

	// Perform backup operations
	return performBackup(ctx, sidecarName, namespace, serviceName, backupFile)
}

// performBackup performs the actual backup operations in the sidecar pod.
func performBackup(ctx context.Context, podName, namespace, serviceName, backupFile string) error {
	// Get OpenSearch password
	osPassword, err := common.GetOpenSearchPasswordFromSecret(namespace)
	if err != nil {
		return fmt.Errorf("failed to get OpenSearch password: %w", err)
	}

	// Create backup directory in pod
	if err := createBackupDirectory(podName, namespace); err != nil {
		return err
	}

	// Construct OpenSearch host using service name and port
	osHost := fmt.Sprintf("%s:9200", serviceName)
	logger.Infof("OpenSearch host: %s\n", osHost)

	// Perform backup with curl
	if err := performBackupWithCurl(ctx, podName, namespace, osHost, osPassword, containerBackupPath); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Copy backup files from pod to host and create tar archive
	if err := copyAndTarBackup(ctx, podName, namespace, containerBackupPath, backupFile); err != nil {
		return fmt.Errorf("failed to copy and archive backup: %w", err)
	}

	logger.Infoln("OpenSearch backup completed!")

	return nil
}

// createBackupDirectory creates the backup directory in the sidecar pod.
func createBackupDirectory(podName, namespace string) error {
	script := fmt.Sprintf("mkdir -p %s", containerBackupPath)
	cmd := exec.Command("oc", "exec", podName, "-n", namespace, "--", "sh", "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create backup directory in pod: %w, output: %s", err, string(output))
	}

	return nil
}

// performBackupWithCurl performs the OpenSearch backup using curl commands in the pod.
func performBackupWithCurl(ctx context.Context, podName, namespace, osHost, osPassword, backupDir string) error {
	logger.Infof("Exporting OpenSearch indices...")

	indices, err := listRagIndices(podName, namespace, osHost, osPassword)
	if err != nil {
		return err
	}

	if len(indices) == 0 {
		logger.Warningf("No indices found starting with 'rag'\n")

		return nil
	}

	logger.Infof("Found %d indices to backup\n", len(indices))

	backedUpCount, lastErr := backupIndices(ctx, podName, namespace, osHost, osPassword, backupDir, indices)

	if err := handleBackupResults(backedUpCount, len(indices), lastErr); err != nil {
		return err
	}

	// Create backup_info.json
	if err := createBackupInfo(podName, namespace, backupDir); err != nil {
		logger.Warningf("Failed to create backup_info.json: %v\n", err)
	}

	return nil
}

// listRagIndices lists all indices that start with "rag".
func listRagIndices(podName, namespace, osHost, osPassword string) ([]string, error) {
	listScript := commonBackup.ListRagIndicesScript(osHost)
	wrappedScript := wrapScriptWithPassword(osPassword, listScript)

	output, err := common.ExecInPodWithOutput(podName, namespace, wrappedScript)
	if err != nil {
		return nil, fmt.Errorf("failed to list indices: %w, output: %s", err, output)
	}

	return commonBackup.ParseIndicesList(output), nil
}

// backupIndices backs up all provided indices and returns the count and any error.
func backupIndices(ctx context.Context, podName, namespace, osHost, osPassword, backupDir string, indices []string) (int, error) {
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

		if err := backupIndexWithCurl(podName, namespace, osHost, osPassword, backupDir, indexName); err != nil {
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

// backupIndexWithCurl backs up a single index using curl in the pod.
func backupIndexWithCurl(podName, namespace, osHost, osPassword, backupDir, indexName string) error {
	logger.Infof("  Exporting index: %s\n", indexName)

	if err := exportIndexMetadata(podName, namespace, osHost, osPassword, backupDir, indexName); err != nil {
		return err
	}

	if err := exportIndexData(podName, namespace, osHost, osPassword, backupDir, indexName); err != nil {
		return err
	}

	countDocuments(podName, namespace, backupDir, indexName)

	return nil
}

// exportIndexMetadata exports mapping and settings for an index.
func exportIndexMetadata(podName, namespace, osHost, osPassword, backupDir, indexName string) error {
	// Export mapping
	mappingScript := commonBackup.GenerateExportMappingScript(osHost, indexName, backupDir)
	wrappedMapping := wrapScriptWithPassword(osPassword, mappingScript)
	if err := common.ExecInPod(podName, namespace, wrappedMapping); err != nil {
		return fmt.Errorf("failed to export mapping: %w", err)
	}

	// Export settings
	settingsScript := commonBackup.GenerateExportSettingsScript(osHost, indexName, backupDir)
	wrappedSettings := wrapScriptWithPassword(osPassword, settingsScript)
	if err := common.ExecInPod(podName, namespace, wrappedSettings); err != nil {
		return fmt.Errorf("failed to export settings: %w", err)
	}

	return nil
}

// exportIndexData exports all documents from an index using scroll API.
func exportIndexData(podName, namespace, osHost, osPassword, backupDir, indexName string) error {
	// First, initiate scroll
	scrollInitScript := commonBackup.GenerateScrollInitScript(osHost, indexName)
	wrappedInit := wrapScriptWithPassword(osPassword, scrollInitScript)
	if err := common.ExecInPod(podName, namespace, wrappedInit); err != nil {
		return fmt.Errorf("failed to initiate scroll: %w", err)
	}

	// Extract scroll_id and hits with improved error handling and loop protection
	extractScript := commonBackup.GenerateScrollExportScript(osHost, backupDir, indexName)
	wrappedExtract := wrapScriptWithPassword(osPassword, extractScript)
	if err := common.ExecInPod(podName, namespace, wrappedExtract); err != nil {
		return fmt.Errorf("failed to export data: %w", err)
	}

	return nil
}

// countDocuments counts and logs the number of documents in the backup.
func countDocuments(podName, namespace, backupDir, indexName string) {
	countScript := commonBackup.GenerateCountDocumentsScript(backupDir, indexName)
	countOutput, err := common.ExecInPodWithOutput(podName, namespace, countScript)
	commonBackup.LogDocumentCount(countOutput, err)
}

// createBackupInfo creates a backup_info.json file with metadata.
func createBackupInfo(podName, namespace, backupDir string) error {
	infoScript := commonBackup.GenerateBackupInfoScriptForRoot()

	return common.ExecInPod(podName, namespace, infoScript)
}

// copyAndTarBackup copies backup files from pod to host and creates tar archive on host.
func copyAndTarBackup(ctx context.Context, podName, namespace, containerBackupPath, backupFile string) error {
	logger.Infof("Copying backup files from pod to host...")

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

	// Copy backup_info.json from pod
	backupInfoDest := filepath.Join(tempDir, "backup_info.json")
	cpInfoCmd := exec.CommandContext(ctx, "oc", "cp", fmt.Sprintf("%s/%s:/tmp/backup_info.json", namespace, podName), backupInfoDest, "-n", namespace)
	if output, err := cpInfoCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy backup_info.json: %w, output: %s", err, string(output))
	}

	// Copy opensearch_backup directory from pod
	backupDirDest := filepath.Join(tempDir, "opensearch_backup")

	const dirPerm = 0o755
	// Create destination directory
	if err := os.MkdirAll(backupDirDest, dirPerm); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Use "/." suffix to copy directory contents
	cpDirCmd := exec.CommandContext(ctx, "oc", "cp", fmt.Sprintf("%s/%s:%s/.", namespace, podName, containerBackupPath), backupDirDest, "-n", namespace)
	if output, err := cpDirCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy backup directory: %w, output: %s", err, string(output))
	}

	logger.Infoln("✓ Backup files copied to host")

	// Create tar.gz archive on host using shared function
	logger.Infoln("Creating tar.gz archive on host...")

	if err := podmanBackup.CreateTarGzArchive(tempDir, backupFile, []string{"backup_info.json", "opensearch_backup"}); err != nil {
		return err
	}

	podmanBackup.LogArchiveSize(backupFile)

	return nil
}

// wrapScriptWithPassword wraps a script with password environment variable setup.
func wrapScriptWithPassword(password, script string) string {
	// Escape password for shell - replace single quotes with '\''
	escapedPassword := strings.ReplaceAll(password, "'", "'\\''")

	return fmt.Sprintf(`
OS_PASSWORD='%s'
export OS_PASSWORD
%s
`, escapedPassword, script)
}

// Made with Bob
