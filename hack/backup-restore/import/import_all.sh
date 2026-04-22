#!/bin/bash
# Master Import Script - Restores OpenSearch and Digitize data
# Usage: ./import_all.sh <backup-file>

set -e

BACKUP_FILE="$1"

if [ -z "$BACKUP_FILE" ]; then
    echo "Error: Backup file not specified"
    echo "Usage: ./import_all.sh <backup-file>"
    exit 1
fi

if [ ! -f "$BACKUP_FILE" ]; then
    echo "Error: Backup file not found: $BACKUP_FILE"
    exit 1
fi

echo "============================================================"
echo "AI Services Complete Import Tool"
echo "============================================================"
echo "Backup file: $BACKUP_FILE"
echo ""

# Create temporary directory for extracting combined backup
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

# Extract the combined backup
echo "Extracting combined backup..."
tar -xzf "$OLDPWD/$BACKUP_FILE"

# Check if this is a combined backup or single component
if [ -d "combined_backup/opensearch" ] && [ -d "combined_backup/digitize" ]; then
    echo "  ✓ Detected combined backup (OpenSearch + Digitize)"
    
    # Create individual backup files
    echo "Preparing OpenSearch backup..."
    tar -czf opensearch_backup.tar.gz -C combined_backup opensearch
    mv opensearch_backup.tar.gz "$OLDPWD/"
    
    echo "Preparing Digitize backup..."
    tar -czf digitize_backup.tar.gz -C combined_backup digitize
    mv digitize_backup.tar.gz "$OLDPWD/"
    
    cd "$OLDPWD"
    rm -rf "$TEMP_DIR"
    
    # Step 1: Restore OpenSearch
    echo ""
    echo "Step 1/2: Restoring OpenSearch vector database..."
    echo "-----------------------------------------------------------"
    bash "./import_opensearch.sh" "opensearch_backup.tar.gz"
    if [ $? -ne 0 ]; then
        echo " Error: OpenSearch restore failed"
        rm -f opensearch_backup.tar.gz digitize_backup.tar.gz
        exit 1
    fi
    echo ""
    
    # Step 2: Restore Digitize data
    echo "Step 2/2: Restoring Digitize application data..."
    echo "-----------------------------------------------------------"
    bash "./import_digitize.sh" "digitize_backup.tar.gz"
    if [ $? -ne 0 ]; then
        echo "Error: Digitize restore failed"
        rm -f opensearch_backup.tar.gz digitize_backup.tar.gz
        exit 1
    fi
    
    # Cleanup temporary backup files
    rm -f opensearch_backup.tar.gz digitize_backup.tar.gz
    
else
    # This is a legacy backup format (from old export.sh)
    echo "✓ Detected legacy backup format"
    cd "$OLDPWD"
    rm -rf "$TEMP_DIR"
    
    echo ""
    echo "This backup uses the legacy format."
    echo "Using the original import.sh script..."
    echo ""
    
    bash "./import.sh" "$BACKUP_FILE"
    exit $?
fi

echo ""
echo "============================================================"
echo "✅ Complete import finished successfully!"
echo "============================================================"
echo "Restored components:"
echo "  - OpenSearch vector database (embeddings)"
echo "  - Digitize application data (/var/cache)"
echo ""
echo " Refresh your browser to see the restored documents in the UI"

# Made with Bob
