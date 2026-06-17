package common

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ListRagIndicesScript generates the script to list all indices starting with "rag".
func ListRagIndicesScript(osHost string) string {
	return fmt.Sprintf(`curl -s -k -u "admin:${OS_PASSWORD}" "https://%s/_cat/indices?format=json" | jq -r '.[] | select(.index | startswith("rag")) | .index'`, osHost)
}

// ParseIndicesList parses the output of the list indices command.
func ParseIndicesList(output string) []string {
	indicesStr := strings.TrimSpace(output)
	if indicesStr == "" {
		return []string{}
	}

	return strings.Split(indicesStr, "\n")
}

// HandleBackupResults checks backup results and logs appropriate messages.
func HandleBackupResults(backedUpCount, totalCount int, lastErr error) error {
	if backedUpCount == 0 && lastErr != nil {
		return fmt.Errorf("failed to backup any indices, last error: %w", lastErr)
	}

	if lastErr != nil {
		logger.Warningf("Backup completed with errors. Successfully backed up %d/%d indices\n", backedUpCount, totalCount)
	} else {
		logger.Infof("✓ Backup completed successfully. Backed up %d indices\n", backedUpCount)
	}

	return nil
}

// CheckContextCancellation checks if the context has been cancelled.
func CheckContextCancellation(ctx context.Context, backedUpCount int) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("backup cancelled: %w", ctx.Err())
	default:
		return nil
	}
}

// GenerateExportMappingScript generates the script to export index mapping.
func GenerateExportMappingScript(osHost, indexName, backupDir string) string {
	return fmt.Sprintf(`curl -s -k -u "admin:${OS_PASSWORD}" "https://%s/%s/_mapping" | jq '.' > %s/%s_mapping.json`, osHost, indexName, backupDir, indexName)
}

// GenerateExportSettingsScript generates the script to export index settings.
func GenerateExportSettingsScript(osHost, indexName, backupDir string) string {
	return fmt.Sprintf(`curl -s -k -u "admin:${OS_PASSWORD}" "https://%s/%s/_settings" | jq '.' > %s/%s_settings.json`, osHost, indexName, backupDir, indexName)
}

// GenerateScrollInitScript generates the script to initiate scroll for data export.
func GenerateScrollInitScript(osHost, indexName string) string {
	return fmt.Sprintf(`curl -s -k -u "admin:${OS_PASSWORD}" "https://%s/%s/_search?scroll=5m" -H 'Content-Type: application/json' -d '{"query":{"match_all":{}},"size":1000}' | jq '.' > /tmp/scroll_init.json`, osHost, indexName)
}

// GenerateScrollExportScript generates the complete script for exporting data using scroll API.
func GenerateScrollExportScript(osHost, backupDir, indexName string) string {
	scrollLoop := GenerateScrollLoopSection(osHost, backupDir, indexName)

	return fmt.Sprintf(`
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
	`, backupDir, indexName, scrollLoop, osHost)
}

// GenerateScrollLoopSection generates the scroll loop section of the export script.
func GenerateScrollLoopSection(osHost, backupDir, indexName string) string {
	return fmt.Sprintf(`# Continue scrolling until no more hits (with max iterations protection)
		MAX_ITERATIONS=1000
		ITERATION=0
		
		while [ $ITERATION -lt $MAX_ITERATIONS ]; do
			ITERATION=$((ITERATION + 1))
			
			# Execute scroll request with error handling
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

// GenerateCountDocumentsScript generates the script to count documents in a backup file.
func GenerateCountDocumentsScript(backupDir, indexName string) string {
	return fmt.Sprintf(`jq 'length' %s/%s_data.json`, backupDir, indexName)
}

// LogDocumentCount logs the document count from the output.
func LogDocumentCount(output string, err error) {
	if err == nil {
		docCount := strings.TrimSpace(output)
		logger.Infof("    ✓ %s documents\n", docCount)
	}
}

// GenerateBackupInfoScript generates the script to create backup_info.json.
func GenerateBackupInfoScript(backupDir string) string {
	timestamp := time.Now().Format(time.RFC3339)

	return fmt.Sprintf(`cat > %s/../backup_info.json << 'EOF'
{
  "backup_date": "%s",
  "type": "opensearch"
}
EOF`, backupDir, timestamp)
}

// GenerateBackupInfoScriptForRoot generates the script to create backup_info.json in /tmp.
func GenerateBackupInfoScriptForRoot() string {
	timestamp := time.Now().Format(time.RFC3339)

	return fmt.Sprintf(`cat > /tmp/backup_info.json << 'EOF'
{
  "backup_date": "%s",
  "type": "opensearch"
}
EOF`, timestamp)
}

// Made with Bob
