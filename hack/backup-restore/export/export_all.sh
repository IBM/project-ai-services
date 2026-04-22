#!/bin/bash
# Master Export Script - Combines OpenSearch and Digitize backups
# Usage: ./export_all.sh [app-name] [output-file]

set -e

APP_NAME="${1:-rag-dev}"
OUTPUT_FILE="${2:-backup_$(date +%Y%m%d_%H%M%S).tar.gz}"

echo "============================================================"
echo "AI Services Complete Export Tool"
echo "============================================================"
echo "App name: $APP_NAME"
echo "Output: $OUTPUT_FILE"
echo ""

# Create temporary directory for combining backups
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

# Step 1: Export OpenSearch
echo "Step 1/2: Exporting OpenSearch vector database..."
echo "-----------------------------------------------------------"
bash "$OLDPWD/export_opensearch.sh" "$APP_NAME" "opensearch_backup.tar.gz"
if [ ! -f "opensearch_backup.tar.gz" ]; then
    echo "Error: OpenSearch backup failed"
    cd "$OLDPWD"
    rm -rf "$TEMP_DIR"
    exit 1
fi
echo ""

# Step 2: Export Digitize data
echo " Step 2/2: Exporting Digitize application data..."
echo "-----------------------------------------------------------"
bash "$OLDPWD/export_digitize.sh" "digitize_backup.tar.gz"
if [ ! -f "digitize_backup.tar.gz" ]; then
    echo "Error: Digitize backup failed"
    cd "$OLDPWD"
    rm -rf "$TEMP_DIR"
    exit 1
fi
echo ""

# Combine both backups
echo "Combining backups..."
mkdir -p combined_backup

# Extract OpenSearch backup
echo "Extracting OpenSearch backup..."
tar -xzf opensearch_backup.tar.gz
mv backup combined_backup/opensearch

# Extract Digitize backup
echo "Extracting Digitize backup..."
tar -xzf digitize_backup.tar.gz
mv backup combined_backup/digitize

# Create final combined backup
echo "Creating final backup archive..."
tar -czf "$OLDPWD/$OUTPUT_FILE" combined_backup/

# Cleanup
cd "$OLDPWD"
rm -rf "$TEMP_DIR"

echo ""
echo "============================================================"
echo "✅ Complete export finished successfully!"
echo "============================================================"
echo "Backup file: $OUTPUT_FILE"
ls -lh "$OUTPUT_FILE"
echo ""
echo "This backup contains:"
echo "  - OpenSearch vector database (embeddings)"
echo "  - Digitize application data (/var/cache)"
echo ""
echo "Use ./import_all.sh to restore this backup"

# Made with Bob
