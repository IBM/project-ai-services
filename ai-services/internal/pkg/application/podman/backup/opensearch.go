package backup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/containers/podman/v5/pkg/bindings/containers"

	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/common"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// BackupOpenSearch performs OpenSearch backup using a sidecar container.
func BackupOpenSearch(ctx context.Context, podID, backupFile string) error {
	sidecarName := fmt.Sprintf("opensearch-backup-sidecar-%d", time.Now().Unix())

	// Create and start sidecar container
	containerID, err := common.CreateAndStartSidecar(ctx, sidecarName, podID)
	if err != nil {
		return fmt.Errorf("failed to create and start sidecar: %w", err)
	}

	// Ensure cleanup happens in all scenarios
	cleanupSidecar := func() {
		const stopTimeout = 10
		logger.Infof("Cleaning up sidecar container...\n", 0)
		// Force stop with timeout
		timeout := uint(stopTimeout)
		stopErr := containers.Stop(ctx, containerID, &containers.StopOptions{Timeout: &timeout})
		if stopErr != nil {
			logger.Warningf("Failed to stop sidecar container %s: %v\n", containerID, stopErr)
			// Try to kill if stop fails
			logger.Infof("Attempting to kill sidecar container...\n", 0)
			killErr := containers.Kill(ctx, containerID, nil)
			if killErr != nil {
				logger.Warningf("Failed to kill sidecar container %s: %v\n", containerID, killErr)
			}
		}
		logger.Infof("Sidecar container cleanup completed\n", 0)
	}
	defer cleanupSidecar()

	// Create a channel to handle backup completion or timeout
	done := make(chan error, 1)
	go func() {
		done <- prepareSidecarAndBackup(ctx, containerID, backupFile)
	}()

	// Wait for backup to complete or context to be cancelled
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		logger.Warningf("Backup operation cancelled or timed out\n")

		return fmt.Errorf("backup operation cancelled: %w", ctx.Err())
	}
}

// prepareSidecarAndBackup prepares the sidecar container and performs the backup.
func prepareSidecarAndBackup(ctx context.Context, containerID, backupFile string) error {
	// Get OpenSearch password from secret
	osPassword, err := common.GetOpenSearchPasswordFromSecret(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to get OpenSearch password: %w", err)
	}

	// Create backup directory in container
	containerBackupPath := "/tmp/opensearch_backup"
	if err := common.ExecInContainer(ctx, containerID, []string{"mkdir", "-p", containerBackupPath}); err != nil {
		return fmt.Errorf("failed to create backup directory in container: %w", err)
	}

	// Perform backup using curl
	if err := performBackupWithCurl(ctx, containerID, "localhost:9200", osPassword, containerBackupPath); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Copy backup files from container to host, then create tar archive on host
	if err := CopyAndTarBackup(ctx, containerID, containerBackupPath, backupFile); err != nil {
		return fmt.Errorf("failed to copy and archive backup: %w", err)
	}

	logger.Infof("OpenSearch backup completed!\n", 0)

	return nil
}

// performBackupWithCurl performs the OpenSearch backup using curl commands in container.
func performBackupWithCurl(ctx context.Context, containerID, osHost, osPassword, backupDir string) error {
	logger.Infof("Exporting OpenSearch indices...\n", 0)

	// Escape single quotes in password for shell
	escapedPassword := strings.ReplaceAll(osPassword, "'", "'\\''")
	curlBase := fmt.Sprintf("curl -s -k -u 'admin:%s' https://%s", escapedPassword, osHost)

	// List all indices that start with "rag"
	listScript := fmt.Sprintf(`%s/_cat/indices?format=json | jq -r '.[] | select(.index | startswith("rag")) | .index'`, curlBase)
	listCmd := exec.CommandContext(ctx, "podman", "exec", containerID, "sh", "-c", listScript)
	output, err := listCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list indices: %w, output: %s", err, string(output))
	}

	// Parse indices from output
	indicesStr := strings.TrimSpace(string(output))
	if indicesStr == "" {
		logger.Warningf("No indices found starting with 'rag'\n")

		return nil
	}

	indices := strings.Split(indicesStr, "\n")
	logger.Infof("Found %d indices to backup\n", len(indices), 0)

	// Backup each index
	backedUpCount := 0
	var lastErr error

	for _, indexName := range indices {
		indexName = strings.TrimSpace(indexName)
		if indexName == "" {
			continue
		}

		if err := backupIndexWithCurl(ctx, containerID, osHost, osPassword, backupDir, indexName); err != nil {
			logger.Errorf("Failed to backup index %s: %v\n", indexName, err)
			lastErr = err

			continue
		}

		backedUpCount++
	}

	if backedUpCount == 0 && lastErr != nil {
		return fmt.Errorf("failed to backup any indices, last error: %w", lastErr)
	}

	if lastErr != nil {
		logger.Warningf("Backup completed with errors. Successfully backed up %d/%d indices\n", backedUpCount, len(indices))
	} else {
		logger.Infof("✓ Backup completed successfully. Backed up %d indices\n", backedUpCount, 0)
	}

	// Create backup_info.json
	if err := createBackupInfo(ctx, containerID, backupDir); err != nil {
		logger.Warningf("Failed to create backup_info.json: %v\n", err)
	}

	return nil
}

// backupIndexWithCurl backs up a single index using curl in container.
func backupIndexWithCurl(ctx context.Context, containerID, osHost, osPassword, backupDir, indexName string) error {
	logger.Infof("  Exporting index: %s\n", indexName, 0)

	// Escape single quotes in password for shell
	escapedPassword := strings.ReplaceAll(osPassword, "'", "'\\''")
	curlBase := fmt.Sprintf("curl -s -k -u 'admin:%s' https://%s", escapedPassword, osHost)

	if err := exportIndexMetadata(ctx, containerID, curlBase, backupDir, indexName); err != nil {
		return err
	}

	if err := exportIndexData(ctx, containerID, curlBase, backupDir, indexName); err != nil {
		return err
	}

	countDocuments(ctx, containerID, backupDir, indexName)

	return nil
}

// exportIndexMetadata exports mapping and settings for an index.
func exportIndexMetadata(ctx context.Context, containerID, curlBase, backupDir, indexName string) error {
	// Export mapping
	mappingScript := fmt.Sprintf(`%s/%s/_mapping | jq '.' > %s/%s_mapping.json`, curlBase, indexName, backupDir, indexName)
	if err := common.ExecInContainer(ctx, containerID, []string{"sh", "-c", mappingScript}); err != nil {
		return fmt.Errorf("failed to export mapping: %w", err)
	}

	// Export settings
	settingsScript := fmt.Sprintf(`%s/%s/_settings | jq '.' > %s/%s_settings.json`, curlBase, indexName, backupDir, indexName)
	if err := common.ExecInContainer(ctx, containerID, []string{"sh", "-c", settingsScript}); err != nil {
		return fmt.Errorf("failed to export settings: %w", err)
	}

	return nil
}

// exportIndexData exports all documents from an index using scroll API.
func exportIndexData(ctx context.Context, containerID, curlBase, backupDir, indexName string) error {
	// First, initiate scroll
	scrollInitScript := fmt.Sprintf(`%s/%s/_search?scroll=5m -H 'Content-Type: application/json' -d '{"query":{"match_all":{}},"size":1000}' | jq '.' > /tmp/scroll_init.json`, curlBase, indexName)
	if err := common.ExecInContainer(ctx, containerID, []string{"sh", "-c", scrollInitScript}); err != nil {
		return fmt.Errorf("failed to initiate scroll: %w", err)
	}

	// Extract scroll_id and hits with improved error handling and loop protection
	extractScript := buildScrollExportScript(curlBase, backupDir, indexName)
	if err := common.ExecInContainer(ctx, containerID, []string{"sh", "-c", extractScript}); err != nil {
		return fmt.Errorf("failed to export data: %w", err)
	}

	return nil
}

// buildScrollExportScript builds the shell script for exporting data using scroll API.
func buildScrollExportScript(curlBase, backupDir, indexName string) string {
	return fmt.Sprintf(`
		set -e
		set -o pipefail
		
		SCROLL_ID=$(jq -r '._scroll_id' /tmp/scroll_init.json)
		if [ -z "$SCROLL_ID" ] || [ "$SCROLL_ID" = "null" ]; then
			echo "Failed to get scroll_id from initial response" >&2
			exit 1
		fi
		
		jq '.hits.hits' /tmp/scroll_init.json > %s/%s_data.json
		
		# Continue scrolling until no more hits (with max iterations protection)
		MAX_ITERATIONS=1000
		ITERATION=0
		
		while [ $ITERATION -lt $MAX_ITERATIONS ]; do
			ITERATION=$((ITERATION + 1))
			
			# Execute scroll request with error handling
			RESPONSE=$(%s/_search/scroll -H 'Content-Type: application/json' -d "{\"scroll\":\"5m\",\"scroll_id\":\"$SCROLL_ID\"}" 2>&1)
			CURL_EXIT=$?
			
			if [ $CURL_EXIT -ne 0 ]; then
				echo "Error in scroll request (exit code: $CURL_EXIT): $RESPONSE" >&2
				break
			fi
			
			# Check if response is valid JSON
			HITS=$(echo "$RESPONSE" | jq '.hits.hits | length' 2>/dev/null)
			JQ_EXIT=$?
			
			if [ $JQ_EXIT -ne 0 ]; then
				echo "Invalid JSON response from scroll API" >&2
				break
			fi
			
			if [ -z "$HITS" ] || [ "$HITS" = "null" ] || [ "$HITS" -eq 0 ]; then
				break
			fi
			
			# Append hits to data file (merge arrays)
			echo "$RESPONSE" | jq '.hits.hits' > /tmp/new_hits.json
			jq -s '.[0] + .[1]' %s/%s_data.json /tmp/new_hits.json > /tmp/merged.json
			mv /tmp/merged.json %s/%s_data.json
			
			# Get new scroll_id
			SCROLL_ID=$(echo "$RESPONSE" | jq -r '._scroll_id' 2>/dev/null)
			if [ -z "$SCROLL_ID" ] || [ "$SCROLL_ID" = "null" ]; then
				break
			fi
		done
		
		# Clear scroll (ignore errors)
		if [ -n "$SCROLL_ID" ] && [ "$SCROLL_ID" != "null" ]; then
			%s/_search/scroll -X DELETE -H 'Content-Type: application/json' -d "{\"scroll_id\":\"$SCROLL_ID\"}" > /dev/null 2>&1 || true
		fi
		
		exit 0
	`, backupDir, indexName, curlBase, backupDir, indexName, backupDir, indexName, curlBase)
}

// countDocuments counts and logs the number of documents in the backup.
func countDocuments(ctx context.Context, containerID, backupDir, indexName string) {
	countScript := fmt.Sprintf(`jq 'length' %s/%s_data.json`, backupDir, indexName)
	countCmd := exec.CommandContext(ctx, "podman", "exec", containerID, "sh", "-c", countScript)
	countOutput, err := countCmd.CombinedOutput()
	if err == nil {
		docCount := strings.TrimSpace(string(countOutput))
		logger.Infof("    ✓ %s documents\n", docCount, 0)
	}
}

// createBackupInfo creates a backup_info.json file with metadata.
func createBackupInfo(ctx context.Context, containerID, backupDir string) error {
	timestamp := time.Now().Format(time.RFC3339)
	infoScript := fmt.Sprintf(`cat > %s/../backup_info.json << 'EOF'
{
  "backup_date": "%s",
  "type": "opensearch"
}
EOF`, backupDir, timestamp)

	return common.ExecInContainer(ctx, containerID, []string{"sh", "-c", infoScript})
}

// Made with Bob
