#!/bin/bash
# Unified Backup/Restore Tool for AI Services
#
# SIDECAR CONTAINER APPROACH for OpenSearch:
# This script uses a sidecar container pattern for OpenSearch backup/restore:
# 1. Detects the pod that contains the OpenSearch container
# 2. Launches a temporary Python container in the SAME POD
# 3. The sidecar shares the network namespace with OpenSearch (localhost access)
# 4. Installs opensearch-py and runs backup/restore operations
# 5. Cleans up the sidecar container after completion

set -e

VERSION="1.0.0"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print colored output
print_error() { echo -e "${RED}❌ $1${NC}"; }
print_success() { echo -e "${GREEN}✅ $1${NC}"; }
print_warning() { echo -e "${YELLOW}⚠️  $1${NC}"; }
print_info() { echo -e "${BLUE}ℹ️  $1${NC}"; }

# Auto-source .env file if it exists in script directory
if [ -f "$SCRIPT_DIR/.env" ]; then
    print_info "Loading environment variables from $SCRIPT_DIR/.env"
    set -a  # automatically export all variables
    source "$SCRIPT_DIR/.env"
    set +a
fi

# Default configuration (can be overridden by environment variables from .env)
CACHE_DIR="${CACHE_DIR:-/var/cache}"
OPENSEARCH_PASSWORD="${OPENSEARCH_PASSWORD:-}"

# Validate and set OpenSearch password
validate_opensearch_password() {
    if [ -z "$OPENSEARCH_PASSWORD" ]; then
        # No password set - use default with warning
        OPENSEARCH_PASSWORD="AiServices@12345"
        print_warning "Using default OpenSearch password (not recommended for production)"
        print_info "Set OPENSEARCH_PASSWORD in .env file or as environment variable for custom password"
    fi
}

# Show usage
show_usage() {
    cat << EOF
Unified Backup/Restore Tool for AI Services v${VERSION}

USAGE:
    ./backup-restore.sh <command> [options]

COMMANDS:
    export opensearch <app-name> <output-file>
        Export OpenSearch vector database
        Example: ./backup-restore.sh export opensearch rag-dev opensearch.tar.gz
        Note: app-name is required

    export digitize <app-name> <output-file>
        Export digitize application data (default: /var/cache)
        Example: ./backup-restore.sh export digitize rag-dev digitize.tar.gz
        Note: app-name is required

    import opensearch <app-name> <backup-file>
        Import OpenSearch vector database to specific app
        Example: ./backup-restore.sh import opensearch rag-dev opensearch.tar.gz
        Note: app-name is required

    import digitize <app-name> <backup-file>
        Import digitize application data to specific app
        Example: ./backup-restore.sh import digitize rag-dev digitize.tar.gz
        Note: app-name is required

    help
        Show this help message

    version
        Show version information

ENVIRONMENT CONFIGURATION:
    The script automatically loads environment variables from .env file in the script directory.
    
    Available variables:
        CACHE_DIR              Cache directory path (default: /var/cache)
        OPENSEARCH_PASSWORD    OpenSearch admin password (required for production)

EXAMPLES:
    # Setup: Create .env file first
    cp .env.example .env
    # Edit .env and set OPENSEARCH_PASSWORD=YourPassword

    # Full backup (run both commands)
    ./backup-restore.sh export opensearch rag-dev opensearch.tar.gz
    ./backup-restore.sh export digitize rag-dev digitize.tar.gz

    # Full restore (run both commands)
    ./backup-restore.sh import opensearch rag-dev opensearch.tar.gz
    ./backup-restore.sh import digitize rag-dev digitize.tar.gz

    # Partial backup (OpenSearch only)
    ./backup-restore.sh export opensearch rag-prod opensearch_prod.tar.gz

    # Partial restore (digitize only)
    ./backup-restore.sh import digitize rag-dev digitize_backup.tar.gz

    # Override environment variables for single command
    OPENSEARCH_PASSWORD="TempPassword" ./backup-restore.sh export opensearch rag-dev backup.tar.gz

SECURITY NOTES:
    - Create .env file from .env.example and set your passwords
    - Never commit .env files with real passwords to version control
    - The .env file is automatically loaded from the script directory
    - You can override .env variables by setting them before the command

EOF
}

# Export OpenSearch using sidecar container approach
export_opensearch() {
    local APP_NAME="$1"
    local OUTPUT_FILE="$2"
    
    # Validate required parameters
    if [ -z "$APP_NAME" ]; then
        print_error "App name is required"
        echo "Usage: ./backup-restore.sh export opensearch <app-name> <output-file>"
        exit 1
    fi
    
    if [ -z "$OUTPUT_FILE" ]; then
        OUTPUT_FILE="opensearch_backup_$(date +%Y%m%d_%H%M%S).tar.gz"
    fi
    
    local CONTAINER_NAME=$(podman ps --filter "label=ai-services.io/application=${APP_NAME}" --filter "name=opensearch" --format "{{.Names}}" | head -n 1)

    if [ -z "$CONTAINER_NAME" ]; then
        print_error "OpenSearch container not found for app: $APP_NAME"
        print_error "Make sure the container has label 'ai-services.io/application=${APP_NAME}' and name contains 'opensearch'"
        exit 1
    fi

    echo "============================================================"
    echo "OpenSearch Export (Sidecar Container Approach)"
    echo "============================================================"
    echo "Container: $CONTAINER_NAME"
    echo "App name: $APP_NAME"
    echo "Output: $OUTPUT_FILE"
    echo ""

    # Get the pod ID for the OpenSearch container
    local POD_ID=$(podman inspect $CONTAINER_NAME --format '{{.Pod}}')
    
    if [ -z "$POD_ID" ] || [ "$POD_ID" = "<no value>" ]; then
        print_error "Container is not part of a pod. Sidecar approach requires pod deployment."
        print_error "Please ensure OpenSearch is deployed as part of a pod."
        exit 1
    fi
    
    print_info "Pod ID: $POD_ID"

    # Create Python backup script
    print_info "Creating backup script..."
    cat > /tmp/backup.py << 'EOFPYTHON'
#!/usr/bin/env python3
import json, os, sys, tarfile, tempfile
from datetime import datetime
from pathlib import Path
from opensearchpy import OpenSearch

class BackupExporter:
    def __init__(self, app_name, output_file):
        self.app_name = app_name
        self.output_file = output_file
        password = os.getenv("OPENSEARCH_PASSWORD")
        if not password:
            print("ERROR: OPENSEARCH_PASSWORD environment variable not set")
            sys.exit(1)
        self.client = OpenSearch(
            hosts=[{"host": "localhost", "port": 9200}],
            http_compress=True, use_ssl=True,
            http_auth=("admin", password),
            verify_certs=False, ssl_show_warn=False, timeout=30
        )
    
    def export_index(self, index_name, temp_dir):
        print(f"  Exporting index: {index_name}")
        mapping = self.client.indices.get_mapping(index=index_name)
        settings = self.client.indices.get_settings(index=index_name)
        with open(temp_dir / f"{index_name}_mapping.json", "w") as f:
            json.dump(mapping, f)
        with open(temp_dir / f"{index_name}_settings.json", "w") as f:
            json.dump(settings, f)
        documents = []
        response = self.client.search(index=index_name, body={"query": {"match_all": {}},"size": 1000}, params={"scroll": "5m"})
        scroll_id = response["_scroll_id"]
        hits = response["hits"]["hits"]
        documents.extend(hits)
        while len(hits) > 0:
            response = self.client.scroll(scroll_id=scroll_id, params={"scroll": "5m"})
            scroll_id = response["_scroll_id"]
            hits = response["hits"]["hits"]
            documents.extend(hits)
        self.client.clear_scroll(scroll_id=scroll_id)
        with open(temp_dir / f"{index_name}_data.json", "w") as f:
            json.dump(documents, f)
        print(f"    ✓ {len(documents)} documents")
    
    def run(self):
        print("Connecting to OpenSearch...")
        info = self.client.info()
        print(f"✓ Connected to OpenSearch {info['version']['number']}")
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            os_dir = temp_path / "opensearch"
            os_dir.mkdir(exist_ok=True)
            indices = [idx for idx in self.client.indices.get_alias(index="*").keys() if idx.startswith("rag")]
            print(f"Found {len(indices)} indices")
            for idx in indices:
                self.export_index(idx, os_dir)
            with open(temp_path / "backup_info.json", "w") as f:
                json.dump({"app_name": self.app_name, "backup_date": datetime.now().isoformat(), "type": "opensearch"}, f)
            with tarfile.open(self.output_file, "w:gz") as tar:
                tar.add(temp_path, arcname="backup")
            size_mb = os.path.getsize(self.output_file) / (1024 * 1024)
            print(f"✓ Backup created: {self.output_file} ({size_mb:.2f} MB)")

if __name__ == "__main__":
    exporter = BackupExporter(sys.argv[1], sys.argv[2])
    exporter.run()
EOFPYTHON

    # Run sidecar container in the same pod
    print_info "Starting sidecar container with Python and opensearch-py..."
    
    local SIDECAR_NAME="opensearch-backup-sidecar-$$"
    
    # Start sidecar container in the same pod (shares network namespace with OpenSearch)
    # Using UBI (Universal Base Image)
    podman run -d \
        --name "$SIDECAR_NAME" \
        --pod "$POD_ID" \
        --rm \
        -e OPENSEARCH_PASSWORD="${OPENSEARCH_PASSWORD}" \
        registry.access.redhat.com/ubi9/python-312-minimal:9.7 \
        sleep 3600
    
    if [ $? -ne 0 ]; then
        print_error "Failed to start sidecar container"
        rm -f /tmp/backup.py
        exit 1
    fi
    
    print_info "Installing dependencies in sidecar..."
    podman exec "$SIDECAR_NAME" pip install --no-cache-dir opensearch-py==2.3.1
    
    if [ $? -ne 0 ]; then
        print_error "Failed to install dependencies in sidecar"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f /tmp/backup.py
        exit 1
    fi
    
    # Copy backup script to sidecar
    print_info "Copying backup script to sidecar..."
    podman cp /tmp/backup.py "$SIDECAR_NAME:/backup.py"
    
    if [ $? -ne 0 ]; then
        print_error "Failed to copy backup script to sidecar"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f /tmp/backup.py
        exit 1
    fi
    
    print_info "Running backup from sidecar container..."
    podman exec "$SIDECAR_NAME" \
        python3 /backup.py "$APP_NAME" "/tmp/$OUTPUT_FILE"
    
    if [ $? -ne 0 ]; then
        print_error "Backup failed"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f /tmp/backup.py
        exit 1
    fi
    
    # Copy backup file from sidecar to host
    print_info "Copying backup to host..."
    podman cp "$SIDECAR_NAME:/tmp/$OUTPUT_FILE" "./$OUTPUT_FILE"
    
    if [ $? -ne 0 ]; then
        print_error "Failed to copy backup from sidecar"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f /tmp/backup.py
        exit 1
    fi
    
    # Cleanup sidecar container
    print_info "Cleaning up sidecar container..."
    podman stop "$SIDECAR_NAME" 2>/dev/null
    rm -f /tmp/backup.py
            
    echo ""
    print_success "OpenSearch export completed!"
    echo "Backup file: $OUTPUT_FILE"
    ls -lh "$OUTPUT_FILE"
}

# Export Digitize
export_digitize() {
    local APP_NAME="$1"
    local OUTPUT_FILE="$2"
    
    # Validate required parameters
    if [ -z "$APP_NAME" ]; then
        print_error "App name is required"
        echo "Usage: ./backup-restore.sh export digitize <app-name> <output-file>"
        exit 1
    fi
    
    if [ -z "$OUTPUT_FILE" ]; then
        OUTPUT_FILE="digitize_backup_$(date +%Y%m%d_%H%M%S).tar.gz"
    fi

    echo "============================================================"
    echo "Digitize Data Export"
    echo "============================================================"
    echo "App name: $APP_NAME"
    echo "Container cache path: /var/cache"
    echo "Output: $OUTPUT_FILE"
    echo ""

    local DIGITIZE_CONTAINER=$(podman ps --filter "label=ai-services.io/application=${APP_NAME}" --format "{{.Names}}" | grep -E "digitize.*(backend|server)" | head -n 1)

    if [ -z "$DIGITIZE_CONTAINER" ]; then
        print_error "Digitize backend container not found for app: $APP_NAME"
        print_error "Make sure the container has label 'ai-services.io/application=${APP_NAME}' and name contains 'digitize' with 'backend' or 'server'"
        exit 1
    fi

    print_info "Creating backup from container ($DIGITIZE_CONTAINER)..."
    TEMP_DIR=$(mktemp -d)
    cd "$TEMP_DIR"

    mkdir -p backup

    # Backup entire /var/cache from CONTAINER
    print_info "Backing up /var/cache from container..."
    
    # Use podman cp to copy directory directly (no tar needed in container)
    # Copy /var/cache to backup/cache (podman cp creates the target directory)
    podman cp $DIGITIZE_CONTAINER:/var/cache ./backup/cache
    
    if [ $? -ne 0 ]; then
        print_error "Failed to copy files from container"
        cd "$OLDPWD"
        rm -rf "$TEMP_DIR"
        exit 1
    fi
    
    # Verify backup has files
    TOTAL_FILES=$(find backup/cache -type f 2>/dev/null | wc -l)
    TOTAL_SIZE=$(du -sh backup/cache 2>/dev/null | awk '{print $1}')
    
    if [ "$TOTAL_FILES" -eq "0" ]; then
        print_warning "No files found in container /var/cache"
    fi

    echo "  ✓ Backed up $TOTAL_FILES files ($TOTAL_SIZE) from container"

    tar -czf "$OLDPWD/$OUTPUT_FILE" backup/
    cd "$OLDPWD"
    rm -rf "$TEMP_DIR"

    echo ""
    print_success "Digitize data export completed!"
    echo "Backup file: $OUTPUT_FILE"
}


# Import OpenSearch using sidecar container approach
import_opensearch() {
    local APP_NAME="$1"
    local BACKUP_FILE="$2"
    
    # Validate required parameters
    if [ -z "$APP_NAME" ]; then
        print_error "App name is required"
        echo "Usage: ./backup-restore.sh import opensearch <app-name> <backup-file>"
        exit 1
    fi

    if [ -z "$BACKUP_FILE" ] || [ ! -f "$BACKUP_FILE" ]; then
        print_error "Backup file not found: $BACKUP_FILE"
        exit 1
    fi

    local CONTAINER_NAME=$(podman ps --filter "label=ai-services.io/application=${APP_NAME}" --filter "name=opensearch" --format "{{.Names}}" | head -n 1)

    if [ -z "$CONTAINER_NAME" ]; then
        print_error "OpenSearch container not found for app: $APP_NAME"
        print_error "Make sure the container has label 'ai-services.io/application=${APP_NAME}' and name contains 'opensearch'"
        exit 1
    fi

    echo "============================================================"
    echo "OpenSearch Import (Sidecar Container Approach)"
    echo "============================================================"
    echo "Container: $CONTAINER_NAME"
    echo "Backup file: $BACKUP_FILE"
    echo ""

    # Get the pod ID for the OpenSearch container
    local POD_ID=$(podman inspect $CONTAINER_NAME --format '{{.Pod}}')
    
    if [ -z "$POD_ID" ] || [ "$POD_ID" = "<no value>" ]; then
        print_error "Container is not part of a pod. Sidecar approach requires pod deployment."
        print_error "Please ensure OpenSearch is deployed as part of a pod."
        exit 1
    fi
    
    print_info "Pod ID: $POD_ID"

    # Create restore script
    print_info "Creating restore script..."
    cat > /tmp/restore.py << 'EOFPYTHON'
#!/usr/bin/env python3
import json, os, sys, tarfile, tempfile
from pathlib import Path
from opensearchpy import OpenSearch, helpers

class BackupRestorer:
    def __init__(self, backup_file):
        self.backup_file = backup_file
        password = os.getenv("OPENSEARCH_PASSWORD")
        if not password:
            print("ERROR: OPENSEARCH_PASSWORD environment variable not set")
            sys.exit(1)
        self.client = OpenSearch(
            hosts=[{"host": "localhost", "port": 9200}],
            http_compress=True, use_ssl=True,
            http_auth=("admin", password),
            verify_certs=False, ssl_show_warn=False, timeout=30
        )
    
    def restore_index(self, index_name, temp_dir):
        print(f"  Restoring index: {index_name}")
        os_dir = temp_dir / "backup" / "opensearch"
        with open(os_dir / f"{index_name}_mapping.json") as f:
            mapping = json.load(f)
        with open(os_dir / f"{index_name}_settings.json") as f:
            settings = json.load(f)
        if self.client.indices.exists(index=index_name):
            print(f"    Deleting existing index...")
            self.client.indices.delete(index=index_name)
        idx_settings = settings[index_name]["settings"]["index"]
        for key in ["creation_date", "uuid", "version", "provided_name"]:
            idx_settings.pop(key, None)
        self.client.indices.create(
            index=index_name,
            body={"settings": {"index": idx_settings}, "mappings": mapping[index_name]["mappings"]}
        )
        with open(os_dir / f"{index_name}_data.json") as f:
            documents = json.load(f)
        if documents:
            actions = [{"_index": index_name, "_id": doc["_id"], "_source": doc["_source"]} for doc in documents]
            success, errors = helpers.bulk(self.client, actions, stats_only=False, raise_on_error=False, refresh=True)
            print(f"    ✓ {success} documents restored")
    
    def run(self):
        print("Connecting to OpenSearch...")
        info = self.client.info()
        print(f"✓ Connected to OpenSearch {info['version']['number']}")
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            print("Extracting backup...")
            with tarfile.open(self.backup_file, "r:gz") as tar:
                tar.extractall(temp_path)
            info_file = temp_path / "backup" / "backup_info.json"
            if info_file.exists():
                with open(info_file) as f:
                    info = json.load(f)
                    print(f"  Backup date: {info.get('backup_date')}")
                    print(f"  App name: {info.get('app_name')}")
            os_dir = temp_path / "backup" / "opensearch"
            if os_dir.exists():
                indices = [f.stem.replace("_data", "") for f in os_dir.glob("*_data.json")]
                print(f"Found {len(indices)} indices to restore")
                for idx in indices:
                    self.restore_index(idx, temp_path)
            print("✓ Restore completed successfully")

if __name__ == "__main__":
    restorer = BackupRestorer(sys.argv[1])
    restorer.run()
EOFPYTHON

    # Run sidecar container in the same pod
    print_info "Starting sidecar container with Python and opensearch-py..."
    
    local SIDECAR_NAME="opensearch-restore-sidecar-$$"
    
    # Start sidecar container in the same pod (shares network namespace with OpenSearch)
    podman run -d \
        --name "$SIDECAR_NAME" \
        --pod "$POD_ID" \
        --rm \
        -e OPENSEARCH_PASSWORD="${OPENSEARCH_PASSWORD}" \
        registry.access.redhat.com/ubi9/python-312-minimal:9.7 \
        sleep 3600
    
    if [ $? -ne 0 ]; then
        print_error "Failed to start sidecar container"
        rm -f /tmp/restore.py
        exit 1
    fi
    
    print_info "Installing dependencies in sidecar..."
    podman exec "$SIDECAR_NAME" pip install --no-cache-dir opensearch-py==2.3.1
    
    if [ $? -ne 0 ]; then
        print_error "Failed to install dependencies in sidecar"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f /tmp/restore.py
        exit 1
    fi
    
    # Copy restore script to sidecar
    print_info "Copying restore script to sidecar..."
    podman cp /tmp/restore.py "$SIDECAR_NAME:/restore.py"
    
    if [ $? -ne 0 ]; then
        print_error "Failed to copy restore script to sidecar"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f /tmp/restore.py
        exit 1
    fi
    
    # Copy backup file to sidecar
    print_info "Copying backup to sidecar container..."
    podman cp "$BACKUP_FILE" "$SIDECAR_NAME:/tmp/backup.tar.gz"
    
    if [ $? -ne 0 ]; then
        print_error "Failed to copy backup to sidecar"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f /tmp/restore.py
        exit 1
    fi
    
    # Run restore from sidecar container
    print_info "Running restore from sidecar container..."
    podman exec "$SIDECAR_NAME" \
        python3 /restore.py /tmp/backup.tar.gz
    
    if [ $? -ne 0 ]; then
        print_error "Restore failed"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f /tmp/restore.py
        exit 1
    fi
    
    # Cleanup sidecar container
    print_info "Cleaning up sidecar container..."
    podman stop "$SIDECAR_NAME" 2>/dev/null
    rm -f /tmp/restore.py

    echo ""
    print_success "OpenSearch import completed!"
}

# Import Digitize
import_digitize() {
    local APP_NAME="$1"
    local BACKUP_FILE="$2"
    
    # Validate required parameters
    if [ -z "$APP_NAME" ]; then
        print_error "App name is required"
        echo "Usage: ./backup-restore.sh import digitize <app-name> <backup-file>"
        exit 1
    fi

    if [ -z "$BACKUP_FILE" ] || [ ! -f "$BACKUP_FILE" ]; then
        print_error "Backup file not found: $BACKUP_FILE"
        exit 1
    fi

    echo "============================================================"
    echo "Digitize Data Import"
    echo "============================================================"
    echo "App name: $APP_NAME"
    echo "Backup file: $BACKUP_FILE"
    echo ""

    local TEMP_DIR=$(mktemp -d)

    # Extract backup
    print_info "Extracting backup..."
    tar -xzf "$BACKUP_FILE" -C "$TEMP_DIR"

    if [ ! -d "$TEMP_DIR/backup/cache" ]; then
        print_error "No cache directory found in backup"
        rm -rf "$TEMP_DIR"
        exit 1
    fi

    # Restore to container - MIRROR the export strategy
    print_info "Restoring to digitize container..."
    
    local DIGITIZE_CONTAINER=$(podman ps --filter "label=ai-services.io/application=${APP_NAME}" --format "{{.Names}}" | grep -E "digitize.*(backend|server)" | head -n 1)

    if [ -z "$DIGITIZE_CONTAINER" ]; then
        print_error "Digitize backend container not found for app: $APP_NAME"
        print_error "Make sure the container has label 'ai-services.io/application=${APP_NAME}' and name contains 'digitize' with 'backend' or 'server'"
        rm -rf "$TEMP_DIR"
        exit 1
    fi

    echo "  ✓ Found container: $DIGITIZE_CONTAINER"
    
    # Show what we're restoring
    print_info "Backup contains:"
    TOTAL_FILES=$(find "$TEMP_DIR/backup/cache" -type f 2>/dev/null | wc -l)
    TOTAL_SIZE=$(du -sh "$TEMP_DIR/backup/cache" 2>/dev/null | awk '{print $1}')
    echo "  Total files in backup: $TOTAL_FILES ($TOTAL_SIZE)"
    
    if [ "$TOTAL_FILES" -eq "0" ]; then
        print_error "No files found in backup!"
        rm -rf "$TEMP_DIR"
        exit 1
    fi
    
    # RESTORE STRATEGY (mirrors export):
    # Use podman cp to copy directory directly (no tar needed in container)
    
    print_info "Restoring files to container..."
    cd "$TEMP_DIR"
    
    # Copy the cache directory directly to container's /var/cache
    # podman cp will overwrite existing files
    podman cp backup/cache/. $DIGITIZE_CONTAINER:/var/cache/
    
    if [ $? -ne 0 ]; then
        print_error "Failed to copy files to container"
        cd "$OLDPWD"
        rm -rf "$TEMP_DIR"
        exit 1
    fi
    cd "$OLDPWD"
    
    # Verify restoration on host side
    print_info "Verifying restoration..."
    RESTORED_FILES=$(find "$TEMP_DIR/backup/cache" -type f 2>/dev/null | wc -l)
    RESTORED_SIZE=$(du -sh "$TEMP_DIR/backup/cache" 2>/dev/null | awk '{print $1}')
    
    rm -rf "$TEMP_DIR"
    
    echo "  ✓ Restored to /var/cache: $RESTORED_FILES files ($RESTORED_SIZE)"
    
    # Simple check: verify container can access the directory
    if podman exec $DIGITIZE_CONTAINER test -d /var/cache 2>/dev/null; then
        echo "  ✓ Container /var/cache is accessible"
    else
        print_warning "Cannot verify container /var/cache access"
    fi
    
    if [ "$RESTORED_FILES" -eq "0" ]; then
        print_warning "No files found in backup!"
    fi

    echo ""
    print_success "Digitize data import completed!"
    echo "📁 Restored $RESTORED_FILES files to container /var/cache"
    echo "🔄 Refresh your browser to see restored documents"
    echo ""
    print_info "Note: Documents require BOTH digitize files AND OpenSearch metadata"
    print_info "If documents don't appear, also restore OpenSearch data:"
    echo "  ./backup-restore.sh import opensearch $APP_NAME opensearch_backup.tar.gz"
}


# Main command dispatcher
main() {
    if [ $# -eq 0 ]; then
        show_usage
        exit 1
    fi
    
    # Validate OpenSearch password
    validate_opensearch_password

    case "$1" in
        export)
            case "$2" in
                opensearch)
                    export_opensearch "${3:-rag-dev}" "${4:-opensearch_backup_$(date +%Y%m%d_%H%M%S).tar.gz}"
                    ;;
                digitize)
                    export_digitize "${3:-rag-dev}" "${4:-digitize_backup_$(date +%Y%m%d_%H%M%S).tar.gz}"
                    ;;
                *)
                    print_error "Unknown export target: $2"
                    echo "Valid targets: opensearch, digitize"
                    exit 1
                    ;;
            esac
            ;;
        import)
            case "$2" in
                opensearch)
                    if [ -z "$3" ] || [ -z "$4" ]; then
                        print_error "App name and backup file required"
                        echo "Usage: ./backup-restore.sh import opensearch <app-name> <backup-file>"
                        exit 1
                    fi
                    import_opensearch "$3" "$4"
                    ;;
                digitize)
                    if [ -z "$3" ] || [ -z "$4" ]; then
                        print_error "App name and backup file required"
                        echo "Usage: ./backup-restore.sh import digitize <app-name> <backup-file>"
                        exit 1
                    fi
                    import_digitize "$3" "$4"
                    ;;
                *)
                    print_error "Unknown import target: $2"
                    echo "Valid targets: opensearch, digitize"
                    exit 1
                    ;;
            esac
            ;;
        help|--help|-h)
            show_usage
            ;;
        version|--version|-v)
            echo "Backup/Restore Tool v${VERSION}"
            ;;
        *)
            print_error "Unknown command: $1"
            echo ""
            show_usage
            exit 1
            ;;
    esac
}

# Run main function
main "$@"

# Made with Bob
