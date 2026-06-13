package restore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/application/podman/common"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	defaultOpenSearchHost = "localhost:9200"
	containerBackupPath   = "/tmp/opensearch_backup"
	maxIndexNameLength    = 255
)

// RestoreOpenSearch restores OpenSearch data using podman sidecar approach.
func RestoreOpenSearch(ctx context.Context, templateID, backupFile string) error {
	logger.Infof("Restoring OpenSearch data for template: %s\n", templateID, 0)
	logger.Infof("OpenSearch Import (Sidecar Container Approach)\n", 0)

	// Find OpenSearch container and get pod ID
	containerName, podID, err := findContainerAndPod(ctx, templateID)
	if err != nil {
		return err
	}

	logger.Infof("Container: %s\n", containerName, 0)
	logger.Infof("Pod ID: %s\n", podID, 0)

	// Extract and locate backup directory
	backupDir, cleanup, err := ExtractAndLocateBackup(backupFile)
	if err != nil {
		return err
	}
	defer cleanup()

	// Manage sidecar lifecycle and perform restore
	return manageSidecarWithGo(ctx, podID, backupDir)
}

// findContainerAndPod finds the OpenSearch container and its pod ID.
func findContainerAndPod(ctx context.Context, templateID string) (string, string, error) {
	return common.FindContainerAndPod(ctx, templateID)
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

	logger.Infof("OpenSearch import completed!\n", 0)

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
	logger.Infof("Copying backup files to sidecar...\n", 0)

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

	logger.Infof("Found %d indices to restore\n", len(indices), 0)

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
		if err := validateIndexName(indexName); err != nil {
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

// validateIndexName validates an index name to prevent command injection.
func validateIndexName(indexName string) error {
	if err := validateIndexNameLength(indexName); err != nil {
		return err
	}

	if err := validateIndexNameCharacters(indexName); err != nil {
		return err
	}

	if err := validateIndexNamePrefix(indexName); err != nil {
		return err
	}

	return nil
}

// validateIndexNameLength checks if the index name length is valid.
func validateIndexNameLength(indexName string) error {
	if len(indexName) == 0 || len(indexName) > maxIndexNameLength {
		return fmt.Errorf("invalid index name length: %d", len(indexName))
	}

	return nil
}

// validateIndexNameCharacters checks if all characters in the index name are valid.
func validateIndexNameCharacters(indexName string) error {
	// OpenSearch index names must be lowercase and can contain: letters, numbers, -, _, +, .
	// Reject any characters that could be used for command injection
	for _, char := range indexName {
		if !isValidIndexChar(char) {
			return fmt.Errorf("invalid character in index name: %c", char)
		}
	}

	return nil
}

// isValidIndexChar checks if a character is valid for an index name.
func isValidIndexChar(char rune) bool {
	return (char >= 'a' && char <= 'z') ||
		(char >= '0' && char <= '9') ||
		char == '-' || char == '_' || char == '+' || char == '.'
}

// validateIndexNamePrefix checks if the index name starts with a valid character.
func validateIndexNamePrefix(indexName string) error {
	// Reject names starting with special characters that could be problematic
	if indexName[0] == '-' || indexName[0] == '_' || indexName[0] == '+' {
		return fmt.Errorf("index name cannot start with special character")
	}

	return nil
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
		logger.Infof("✓ Restore completed successfully. Restored %d indices\n", restoredCount, 0)
	}

	return nil
}

// restoreIndexWithCurl restores a single index using curl in container.
// Password is passed via environment variable to avoid exposure in process lists.
// The restore process follows: cleanup (delete existing) -> create -> insert data.
func restoreIndexWithCurl(ctx context.Context, pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	logger.Infof("  Restoring index: %s\n", indexName, 0)

	// Verify required backup files exist
	if err := verifyBackupFiles(pc, containerID, backupDir, indexName); err != nil {
		return err
	}

	// Step 1: Cleanup - Delete existing index if it exists
	logger.Infof("    Cleaning up existing index...\n", 0)
	if err := deleteExistingIndex(pc, containerID, osHost, osPassword, indexName); err != nil {
		logger.Warningf("    Failed to delete existing index (may not exist): %v\n", err)
	} else {
		logger.Infof("    ✓ Existing index cleaned up\n", 0)
	}

	// Step 2: Create index with settings and mappings
	logger.Infof("    Creating index with mappings...\n", 0)
	if err := createIndexWithMappings(pc, containerID, osHost, osPassword, backupDir, indexName); err != nil {
		return err
	}
	logger.Infof("    ✓ Index created\n", 0)

	// Step 3: Insert data - Bulk index documents
	logger.Infof("    Inserting documents...\n", 0)
	if err := bulkIndexDocuments(pc, containerID, osHost, osPassword, backupDir, indexName); err != nil {
		return err
	}
	logger.Infof("    ✓ Documents inserted\n", 0)

	// Step 4: Refresh index to make documents searchable
	if err := refreshIndex(pc, containerID, osHost, osPassword, indexName); err != nil {
		return err
	}

	logger.Infof("    ✓ Index restored successfully\n", 0)

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
	// Use environment variable for password to avoid exposure in process list
	// Check if index exists first, then delete it
	deleteScript := fmt.Sprintf(`
# Check if index exists
RESPONSE=$(curl -k -u "admin:${OS_PASSWORD}" "https://%s/%s" -X HEAD -s -w "%%{http_code}" -o /dev/null)
if [ "$RESPONSE" = "200" ]; then
	# Index exists, delete it
	DELETE_RESPONSE=$(curl -k -u "admin:${OS_PASSWORD}" "https://%s/%s" -X DELETE -s -w "\n%%{http_code}")
	HTTP_CODE=$(echo "$DELETE_RESPONSE" | tail -n 1)
	if [ "$HTTP_CODE" != "200" ]; then
		echo "Failed to delete index. HTTP code: $HTTP_CODE" >&2
		exit 1
	fi
	echo "Index deleted successfully"
else
	echo "Index does not exist, skipping delete"
fi
`, osHost, indexName, osHost, indexName)

	return pc.ExecInContainerWithEnv(containerID, map[string]string{"OS_PASSWORD": osPassword}, deleteScript)
}

// createIndexWithMappings creates an index with settings and mappings.
func createIndexWithMappings(pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	// Use environment variable for password and validate HTTP response
	createScript := fmt.Sprintf(`
MAPPING=$(cat %s/%s_mapping.json | jq -c '."%s".mappings')
SETTINGS=$(cat %s/%s_settings.json | jq -c '."%s".settings.index | del(.creation_date, .uuid, .version, .provided_name)')
BODY=$(jq -n --argjson settings "{\"index\": $SETTINGS}" --argjson mappings "$MAPPING" '{settings: $settings, mappings: $mappings}')
RESPONSE=$(curl -k -u "admin:${OS_PASSWORD}" "https://%s/%s" -X PUT -H "Content-Type: application/json" -d "$BODY" -s -w "\n%%{http_code}")
HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
BODY=$(echo "$RESPONSE" | head -n -1)
if [ "$HTTP_CODE" != "200" ]; then
	echo "Failed to create index. HTTP code: $HTTP_CODE, Response: $BODY" >&2
	exit 1
fi
# Validate response contains acknowledged field
if ! echo "$BODY" | jq -e '.acknowledged == true' > /dev/null 2>&1; then
	echo "Index creation not acknowledged. Response: $BODY" >&2
	exit 1
fi
`, backupDir, indexName, indexName, backupDir, indexName, indexName, osHost, indexName)

	if err := pc.ExecInContainerWithEnv(containerID, map[string]string{"OS_PASSWORD": osPassword}, createScript); err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	return nil
}

// bulkIndexDocuments performs bulk indexing of documents.
func bulkIndexDocuments(pc *podman.PodmanClient, containerID, osHost, osPassword, backupDir, indexName string) error {
	// Use environment variable for password and validate HTTP response
	// Process documents in batches of 1000 to balance speed and request size limits
	bulkScript := fmt.Sprintf(`
# Batch size for bulk indexing (increased for faster processing)
BATCH_SIZE=1000
DATA_FILE="%s/%s_data.json"
INDEX_NAME="%s"
OS_HOST="%s"

# Count total documents
TOTAL_DOCS=$(jq 'length' "$DATA_FILE")
echo "Total documents to index: $TOTAL_DOCS"

# Calculate number of batches
BATCHES=$(( ($TOTAL_DOCS + $BATCH_SIZE - 1) / $BATCH_SIZE ))
echo "Processing in $BATCHES batch(es) of up to $BATCH_SIZE documents"

# Process each batch
BATCH_NUM=0
while [ $BATCH_NUM -lt $BATCHES ]; do
	START_IDX=$(( $BATCH_NUM * $BATCH_SIZE ))
	echo "Processing batch $(( $BATCH_NUM + 1 ))/$BATCHES (starting at document $START_IDX)..."
	
	# Extract batch and format for bulk API
	BATCH_DATA=$(jq -c ".[$START_IDX:$START_IDX+$BATCH_SIZE] | .[] | {\"index\": {\"_index\": \"$INDEX_NAME\", \"_id\": ._id}}, ._source" "$DATA_FILE")
	
	# Send batch to OpenSearch
	RESPONSE=$(echo "$BATCH_DATA" | curl -k -u "admin:${OS_PASSWORD}" "https://$OS_HOST/_bulk" -X POST -H "Content-Type: application/x-ndjson" --data-binary @- -s -w "\n%%{http_code}")
	HTTP_CODE=$(echo "$RESPONSE" | tail -n 1)
	BODY=$(echo "$RESPONSE" | head -n -1)
	
	if [ "$HTTP_CODE" != "200" ]; then
		echo "Failed to bulk index documents. HTTP code: $HTTP_CODE, Response: $BODY" >&2
		exit 1
	fi
	
	# Validate response for errors
	if echo "$BODY" | jq -e '.errors == true' > /dev/null 2>&1; then
		echo "Bulk indexing had errors in batch $(( $BATCH_NUM + 1 )). Response: $BODY" >&2
		exit 1
	fi
	
	BATCH_NUM=$(( $BATCH_NUM + 1 ))
done

echo "Successfully indexed all $TOTAL_DOCS documents in $BATCHES batch(es)"
`, backupDir, indexName, indexName, osHost)

	if err := pc.ExecInContainerWithEnv(containerID, map[string]string{"OS_PASSWORD": osPassword}, bulkScript); err != nil {
		return fmt.Errorf("failed to bulk index documents: %w", err)
	}

	return nil
}

// refreshIndex refreshes an index to make documents searchable.
func refreshIndex(pc *podman.PodmanClient, containerID, osHost, osPassword, indexName string) error {
	refreshScript := fmt.Sprintf(`curl -k -u "admin:${OS_PASSWORD}" "https://%s/%s/_refresh" -X POST -s -o /dev/null`, osHost, indexName)

	if err := pc.ExecInContainerWithEnv(containerID, map[string]string{"OS_PASSWORD": osPassword}, refreshScript); err != nil {
		return fmt.Errorf("failed to refresh index: %w", err)
	}

	return nil
}

// Made with Bob
