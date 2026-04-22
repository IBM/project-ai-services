#!/bin/bash
# Import Digitize Application Data (/var/cache)
# Usage: ./import_digitize.sh <backup-file>

set -e

BACKUP_FILE="$1"

if [ -z "$BACKUP_FILE" ]; then
    echo " Error: Backup file not specified"
    echo "Usage: ./import_digitize.sh <backup-file>"
    exit 1
fi

if [ ! -f "$BACKUP_FILE" ]; then
    echo " Error: Backup file not found: $BACKUP_FILE"
    exit 1
fi

echo "============================================================"
echo "Digitize Data Import Tool"
echo "============================================================"
echo "Backup file: $BACKUP_FILE"
echo ""

# Restore /var/cache directory from backup
echo "Restoring /var/cache directory from backup..."
CACHE_DIR="/var/cache"
TEMP_DIR=$(mktemp -d)

# Extract backup to temp directory
tar -xzf "$BACKUP_FILE" -C "$TEMP_DIR"

# Restore entire cache directory to host
RESTORED_FROM_BACKUP=false
if [ -d "$TEMP_DIR/backup/cache" ]; then
    echo "  Restoring entire cache directory..."
    
    # Create /var/cache if it doesn't exist
    mkdir -p "$CACHE_DIR"
    
    # Copy entire cache directory structure
    cp -r "$TEMP_DIR/backup/cache"/* "$CACHE_DIR/" 2>/dev/null || true
    
    # Count what we restored
    TOTAL_FILES=$(find "$CACHE_DIR" -type f 2>/dev/null | wc -l)
    TOTAL_SIZE=$(du -sh "$CACHE_DIR" 2>/dev/null | awk '{print $1}')
    echo "  ✓ Restored $TOTAL_FILES files ($TOTAL_SIZE) to $CACHE_DIR"
    
    RESTORED_FROM_BACKUP=true
else
    echo "  Warning: No cache directory found in backup"
fi

# Cleanup temp directory
rm -rf "$TEMP_DIR"

# Final verification
FINAL_COUNT=$(ls -1 "$CACHE_DIR/docs"/*_metadata.json 2>/dev/null | wc -l)
if [ "$RESTORED_FROM_BACKUP" = false ]; then
    echo "   Warning: No cache data in backup"
elif [ "$FINAL_COUNT" -eq 0 ]; then
    echo "  Warning: No metadata files available in $CACHE_DIR/docs/"
fi

# Copy entire /var/cache directory to digitize container
echo ""
echo " Copying /var/cache to digitize container..."

# Get the digitize backend container by name pattern
DIGITIZE_CONTAINER=$(podman ps --filter "name=digitize-backend" --format "{{.Names}}" | head -n 1)

if [ -z "$DIGITIZE_CONTAINER" ]; then
    echo "   Warning: digitize-backend container not found"
    echo "  Trying alternative container name..."
    DIGITIZE_CONTAINER=$(podman ps --filter "name=digitize" --format "{{.Names}}" | head -n 1)
fi

if [ -n "$DIGITIZE_CONTAINER" ]; then
    echo "  ✓ Found digitize container: $DIGITIZE_CONTAINER"
    
    HOST_CACHE_DIR="/var/cache"
    
    if [ -d "$HOST_CACHE_DIR" ]; then
        echo "   Copying entire $HOST_CACHE_DIR directory to container..."
        
        # Create /var/cache in container if it doesn't exist
        podman exec $DIGITIZE_CONTAINER mkdir -p /var/cache 2>/dev/null || true
        
        # Copy entire cache directory structure using tar (preserves structure)
        echo "   Creating tar archive of cache directory..."
        tar -czf /tmp/cache_for_container.tar.gz -C /var cache 2>/dev/null
        
        # Copy tar to container
        echo "   Copying to container..."
        podman cp /tmp/cache_for_container.tar.gz $DIGITIZE_CONTAINER:/tmp/ 2>/dev/null
        
        # Extract in container
        echo "   Extracting in container..."
        podman exec $DIGITIZE_CONTAINER tar -xzf /tmp/cache_for_container.tar.gz -C /var 2>/dev/null
        
        # Cleanup
        podman exec $DIGITIZE_CONTAINER rm -f /tmp/cache_for_container.tar.gz 2>/dev/null || true
        rm -f /tmp/cache_for_container.tar.gz
        
        # Verify what was copied
        echo "  Verifying files in container..."
        CONTAINER_DOCS=$(podman exec $DIGITIZE_CONTAINER sh -c "ls -1 /var/cache/docs/*.json 2>/dev/null | wc -l" 2>/dev/null || echo "0")
        CONTAINER_JOBS=$(podman exec $DIGITIZE_CONTAINER sh -c "ls -1 /var/cache/jobs/*.json 2>/dev/null | wc -l" 2>/dev/null || echo "0")
        CONTAINER_DIGITIZED=$(podman exec $DIGITIZE_CONTAINER sh -c "find /var/cache/digitized -type f 2>/dev/null | wc -l" 2>/dev/null || echo "0")
        
        echo "  ✓ Container /var/cache contents:"
        echo "     Documents: $CONTAINER_DOCS files"
        echo "     Jobs: $CONTAINER_JOBS files"
        echo "     Digitized: $CONTAINER_DIGITIZED files"
        
        if [ "$CONTAINER_DOCS" -gt 0 ]; then
            echo "    Sample document file:"
            podman exec $DIGITIZE_CONTAINER sh -c "ls -1 /var/cache/docs/*.json 2>/dev/null | head -1 | xargs basename" 2>/dev/null || true
        fi
        
        if [ "$CONTAINER_DOCS" -eq 0 ]; then
            echo "    WARNING: No document metadata files found in container!"
            echo "  Documents will not appear in the UI."
        else
            echo "   Documents should now be visible in the UI after browser refresh"
        fi
    else
        echo "    Directory $HOST_CACHE_DIR not found on host"
    fi
    
else
    echo "     ERROR: Digitize container not found!"
    echo "     Metadata files are on host at /var/cache but cannot be copied to container"
    echo "     Please ensure the digitize container is running"
fi

echo ""
echo "============================================================"
echo "✅ Digitize data import completed successfully!"
echo "============================================================"
echo "Cache directory: Restored to $CACHE_DIR (host) and container"
echo ""
echo " Refresh your browser to see the restored documents in the UI"

# Made with Bob
