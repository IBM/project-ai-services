#!/bin/bash
# Export Digitize Application Data (/var/cache)
# Usage: ./export_digitize.sh [output-file]

set -e

OUTPUT_FILE="${1:-digitize_backup_$(date +%Y%m%d_%H%M%S).tar.gz}"
CACHE_DIR="/var/cache"

echo "============================================================"
echo "Digitize Data Export Tool"
echo "============================================================"
echo "Cache directory: $CACHE_DIR"
echo "Output: $OUTPUT_FILE"
echo ""

# Check if cache directory exists on host
if [ ! -d "$CACHE_DIR" ]; then
    echo " Error: $CACHE_DIR directory not found on host"
    exit 1
fi

# Check if there's any data to backup
DOCS_DIR="$CACHE_DIR/docs"
JOBS_DIR="$CACHE_DIR/jobs"
DIGITIZED_DIR="$CACHE_DIR/digitized"

if [ ! -d "$DOCS_DIR" ] && [ ! -d "$JOBS_DIR" ] && [ ! -d "$DIGITIZED_DIR" ]; then
    echo "Warning: No application data directories found at $CACHE_DIR"
    echo "(docs, jobs, digitized directories are missing)"
fi

# Create temporary directory for backup
echo "Creating backup of /var/cache..."
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

# Create backup directory structure
mkdir -p backup/cache

# Copy entire /var/cache directory structure from host
echo "Copying /var/cache/* to backup..."
cp -r "$CACHE_DIR"/* backup/cache/ 2>/dev/null || true

# Count what we backed up
TOTAL_FILES=$(find backup/cache -type f 2>/dev/null | wc -l)
TOTAL_SIZE=$(du -sh backup/cache 2>/dev/null | awk '{print $1}')

# Show breakdown of application directories
DOCS_COUNT=$(find backup/cache/docs -type f 2>/dev/null | wc -l)
JOBS_COUNT=$(find backup/cache/jobs -type f 2>/dev/null | wc -l)
DIGITIZED_COUNT=$(find backup/cache/digitized -type f 2>/dev/null | wc -l)

echo "Backed up $TOTAL_FILES files ($TOTAL_SIZE) from host"
if [ "$DOCS_COUNT" -gt 0 ] || [ "$JOBS_COUNT" -gt 0 ] || [ "$DIGITIZED_COUNT" -gt 0 ]; then
    echo "    Application data:"
    echo "    - Documents: $DOCS_COUNT files"
    echo "    - Jobs: $JOBS_COUNT files"
    echo "    - Digitized: $DIGITIZED_COUNT files"
fi

# Create final backup
echo "Creating tar archive..."
tar -czf "$OLDPWD/$OUTPUT_FILE" backup/
cd "$OLDPWD"
rm -rf "$TEMP_DIR"

echo ""
echo "============================================================"
echo "✅ Digitize data export completed successfully!"
echo "============================================================"
echo "Backup file: $OUTPUT_FILE"
ls -lh "$OUTPUT_FILE"

# Made with Bob
