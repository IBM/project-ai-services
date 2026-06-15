package backup

import (
	"context"
	"fmt"
	"strings"
	"time"

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

	logger.Infof("OpenSearch backup completed!\n", 0)

	return nil
}

// performBackupWithCurl performs the OpenSearch backup using curl commands in container.
func performBackupWithCurl(ctx context.Context, pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir string) error {
	logger.Infof("Exporting OpenSearch indices...\n", 0)

	indices, err := listRagIndices(pc, containerID, osHost, osPassword)
	if err != nil {
		return err
	}

	if len(indices) == 0 {
		logger.Warningf("No indices found starting with 'rag'\n")

		return nil
	}

	logger.Infof("Found %d indices to backup\n", len(indices), 0)

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
	// Use environment variable for password to avoid exposure in process list
	listScript := fmt.Sprintf(`curl -s -k -u "admin:${OS_PASSWORD}" "https://%s/_cat/indices?format=json" | jq -r '.[] | select(.index | startswith("rag")) | .index'`, osHost)

	// Create a temporary script file to capture output
	tempScript := fmt.Sprintf(`
		OS_PASSWORD='%s'
		export OS_PASSWORD
		%s
	`, strings.ReplaceAll(osPassword, "'", "'\\''"), listScript)

	output, err := pc.ExecInContainerWithOutput(containerID, []string{"sh", "-c", tempScript})
	if err != nil {
		return nil, fmt.Errorf("failed to list indices: %w, output: %s", err, output)
	}

	indicesStr := strings.TrimSpace(output)
	if indicesStr == "" {
		return []string{}, nil
	}

	return strings.Split(indicesStr, "\n"), nil
}

// backupIndices backs up all provided indices and returns the count and any error.
func backupIndices(ctx context.Context, pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir string, indices []string) (int, error) {
	backedUpCount := 0
	var lastErr error

	for _, indexName := range indices {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return backedUpCount, fmt.Errorf("backup cancelled: %w", ctx.Err())
		default:
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
	if backedUpCount == 0 && lastErr != nil {
		return fmt.Errorf("failed to backup any indices, last error: %w", lastErr)
	}

	if lastErr != nil {
		logger.Warningf("Backup completed with errors. Successfully backed up %d/%d indices\n", backedUpCount, totalCount)
	} else {
		logger.Infof("✓ Backup completed successfully. Backed up %d indices\n", backedUpCount, 0)
	}

	return nil
}

// backupIndexWithCurl backs up a single index using curl in container.
func backupIndexWithCurl(ctx context.Context, pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	logger.Infof("  Exporting index: %s\n", indexName, 0)

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
	// Export mapping using environment variable for password
	mappingScript := fmt.Sprintf(`
		OS_PASSWORD='%s'
		export OS_PASSWORD
		curl -s -k -u "admin:${OS_PASSWORD}" "https://%s/%s/_mapping" | jq '.' > %s/%s_mapping.json
	`, strings.ReplaceAll(osPassword, "'", "'\\''"), osHost, indexName, backupDir, indexName)

	if err := pc.ExecInContainer(containerID, []string{"sh", "-c", mappingScript}); err != nil {
		return fmt.Errorf("failed to export mapping: %w", err)
	}

	// Export settings using environment variable for password
	settingsScript := fmt.Sprintf(`
		OS_PASSWORD='%s'
		export OS_PASSWORD
		curl -s -k -u "admin:${OS_PASSWORD}" "https://%s/%s/_settings" | jq '.' > %s/%s_settings.json
	`, strings.ReplaceAll(osPassword, "'", "'\\''"), osHost, indexName, backupDir, indexName)

	if err := pc.ExecInContainer(containerID, []string{"sh", "-c", settingsScript}); err != nil {
		return fmt.Errorf("failed to export settings: %w", err)
	}

	return nil
}

// exportIndexData exports all documents from an index using scroll API with environment variables for password.
func exportIndexData(pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	// First, initiate scroll using environment variable for password
	scrollInitScript := fmt.Sprintf(`
		OS_PASSWORD='%s'
		export OS_PASSWORD
		curl -s -k -u "admin:${OS_PASSWORD}" "https://%s/%s/_search?scroll=5m" -H 'Content-Type: application/json' -d '{"query":{"match_all":{}},"size":1000}' | jq '.' > /tmp/scroll_init.json
	`, strings.ReplaceAll(osPassword, "'", "'\\''"), osHost, indexName)

	if err := pc.ExecInContainer(containerID, []string{"sh", "-c", scrollInitScript}); err != nil {
		return fmt.Errorf("failed to initiate scroll: %w", err)
	}

	// Extract scroll_id and hits with improved error handling and loop protection
	extractScript := buildScrollExportScript(osHost, osPassword, backupDir, indexName)
	if err := pc.ExecInContainer(containerID, []string{"sh", "-c", extractScript}); err != nil {
		return fmt.Errorf("failed to export data: %w", err)
	}

	return nil
}

// buildScrollExportScript builds the shell script for exporting data using scroll API with environment variables.
func buildScrollExportScript(osHost, osPassword, backupDir, indexName string) string {
	escapedPassword := strings.ReplaceAll(osPassword, "'", "'\\''")
	scrollLoop := buildScrollLoopSection(osHost, backupDir, indexName)

	return fmt.Sprintf(`
		OS_PASSWORD='%s'
		export OS_PASSWORD
		
		set -e
		set -o pipefail
		
		SCROLL_ID=$(jq -r '._scroll_id' /tmp/scroll_init.json)
		if [ -z "$SCROLL_ID" ] || [ "$SCROLL_ID" = "null" ]; then
			echo "Failed to get scroll_id from initial response" >&2
			exit 1
		fi
		
		jq '.hits.hits' /tmp/scroll_init.json > %s/%s_data.json
		
		%s
		
		# Clear scroll (ignore errors)
		if [ -n "$SCROLL_ID" ] && [ "$SCROLL_ID" != "null" ]; then
			curl -s -k -u "admin:${OS_PASSWORD}" "https://%s/_search/scroll" -X DELETE -H 'Content-Type: application/json' -d "{\"scroll_id\":\"$SCROLL_ID\"}" > /dev/null 2>&1 || true
		fi
		
		exit 0
	`, escapedPassword, backupDir, indexName, scrollLoop, osHost)
}

// buildScrollLoopSection builds the scroll loop section of the export script.
func buildScrollLoopSection(osHost, backupDir, indexName string) string {
	return fmt.Sprintf(`# Continue scrolling until no more hits (with max iterations protection)
		MAX_ITERATIONS=1000
		ITERATION=0
		
		while [ $ITERATION -lt $MAX_ITERATIONS ]; do
			ITERATION=$((ITERATION + 1))
			
			# Execute scroll request with error handling using environment variable
			RESPONSE=$(curl -s -k -u "admin:${OS_PASSWORD}" "https://%s/_search/scroll" -H 'Content-Type: application/json' -d "{\"scroll\":\"5m\",\"scroll_id\":\"$SCROLL_ID\"}" 2>&1)
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
		done`, osHost, backupDir, indexName, backupDir, indexName)
}

// countDocuments counts and logs the number of documents in the backup.
func countDocuments(pc *podman.PodmanClient, containerID, backupDir, indexName string) {
	countScript := fmt.Sprintf(`jq 'length' %s/%s_data.json`, backupDir, indexName)
	countOutput, err := pc.ExecInContainerWithOutput(containerID, []string{"sh", "-c", countScript})
	if err == nil {
		docCount := strings.TrimSpace(countOutput)
		logger.Infof("    ✓ %s documents\n", docCount, 0)
	}
}

// createBackupInfo creates a backup_info.json file with metadata.
func createBackupInfo(pc *podman.PodmanClient, containerID, backupDir string) error {
	timestamp := time.Now().Format(time.RFC3339)
	infoScript := fmt.Sprintf(`cat > %s/../backup_info.json << 'EOF'
{
  "backup_date": "%s",
  "type": "opensearch"
}
EOF`, backupDir, timestamp)

	return pc.ExecInContainer(containerID, []string{"sh", "-c", infoScript})
}

// Made with Bob
