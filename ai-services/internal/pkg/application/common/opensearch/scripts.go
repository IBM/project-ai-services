package opensearch

import (
	"fmt"
	"strings"
)

const (
	// BatchSize is the batch size for bulk indexing documents.
	BatchSize = 1000
)

// GenerateDeleteIndexScript generates a shell script to delete an existing OpenSearch index.
func GenerateDeleteIndexScript(osHost, indexName string) string {
	return fmt.Sprintf(`
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
fi
`, osHost, indexName, osHost, indexName)
}

// GenerateCreateIndexScript generates a shell script to create an OpenSearch index with mappings and settings.
func GenerateCreateIndexScript(osHost, backupDir, indexName string) string {
	return fmt.Sprintf(`
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
}

// GenerateBulkIndexScript generates a shell script to bulk index documents in batches.
func GenerateBulkIndexScript(osHost, backupDir, indexName string) string {
	return fmt.Sprintf(`
# Batch size for bulk indexing
BATCH_SIZE=%d
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
`, BatchSize, backupDir, indexName, indexName, osHost)
}

// GenerateRefreshIndexScript generates a shell script to refresh an OpenSearch index.
func GenerateRefreshIndexScript(osHost, indexName string) string {
	return fmt.Sprintf(`curl -k -u "admin:${OS_PASSWORD}" "https://%s/%s/_refresh" -X POST -s -o /dev/null`, osHost, indexName)
}

// WrapScriptWithPassword wraps a script with password environment variable setup.
func WrapScriptWithPassword(password, script string) string {
	// Escape password for shell - replace single quotes with '\''
	escapedPassword := escapeShellString(password)

	return fmt.Sprintf(`
OS_PASSWORD='%s'
export OS_PASSWORD
%s
`, escapedPassword, script)
}

// escapeShellString escapes a string for safe use in shell scripts.
// Replaces single quotes with '\” to safely embed in single-quoted strings.
func escapeShellString(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

// Made with Bob
