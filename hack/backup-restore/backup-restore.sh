#!/bin/bash
# Unified Backup/Restore Tool for AI Services
#
# USAGE:
#   ./backup-restore.sh <command> <target> <app-name> [options] --runtime <podman|openshift>
#
# EXAMPLES:
#   # Podman - Export OpenSearch
#   ./backup-restore.sh export opensearch rag-dev opensearch.tar.gz --runtime podman
#
#   # OpenShift - Import digitize
#   ./backup-restore.sh import digitize rag-dev digitize.tar.gz --runtime openshift
#
# SIDECAR CONTAINER APPROACH for OpenSearch:
# This script uses a sidecar container pattern for OpenSearch backup/restore:
# 1. Finds the pod that contains the OpenSearch container (across all namespaces)
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

# Show usage
show_usage() {
    cat << EOF
Unified Backup/Restore Tool for AI Services v${VERSION}

USAGE:
    ./backup-restore.sh <command> <target> <app-name> [options] --runtime <runtime>

REQUIRED PARAMETERS:
    --runtime <runtime>     Container runtime (podman or openshift)
                           Must be specified at the end of the command

COMMANDS:
    export <target> <app-name> [output-file]
        Export data from specified target
        Targets: opensearch, digitize
        
    import <target> <app-name> <backup-file>
        Import data to specified target
        Targets: opensearch, digitize

    help
        Show this help message

    version
        Show version information

EXAMPLES:
    # Export OpenSearch with Podman
    ./backup-restore.sh export opensearch rag-dev opensearch.tar.gz --runtime podman

    # Import digitize with OpenShift
    ./backup-restore.sh import digitize rag-dev digitize.tar.gz --runtime openshift

    # Auto-generate output filename (export only)
    ./backup-restore.sh export opensearch rag-dev --runtime podman

    # Export digitize
    ./backup-restore.sh export digitize rag-prod digitize.tar.gz --runtime openshift

ENVIRONMENT CONFIGURATION:
    The script automatically loads environment variables from .env file in the script directory.
    
    Available variables:
        CACHE_DIR              Cache directory path (default: /var/cache)
        OPENSEARCH_PASSWORD    OpenSearch admin password (required for production)

SECURITY NOTES:
    - Create .env file from .env.example and set your passwords
    - Never commit .env files with real passwords to version control
    - The .env file is automatically loaded from the script directory
    - You can override .env variables by setting them before the command

EOF
}

# Validate runtime parameter
validate_runtime() {
    local RUNTIME="$1"
    if [ "$RUNTIME" != "podman" ] && [ "$RUNTIME" != "openshift" ]; then
        print_error "Invalid runtime: $RUNTIME"
        echo "Valid runtimes: podman, openshift"
        exit 1
    fi
}

# Validate app name parameter
validate_app_name() {
    local APP_NAME="$1"
    if [ -z "$APP_NAME" ]; then
        print_error "App name is required"
        echo "Usage: ./backup-restore.sh --runtime <runtime> <command> <target> <app-name> [options]"
        exit 1
    fi
}

# Validate backup file parameter
validate_backup_file() {
    local BACKUP_FILE="$1"
    if [ -z "$BACKUP_FILE" ] || [ ! -f "$BACKUP_FILE" ]; then
        print_error "Backup file not found: $BACKUP_FILE"
        exit 1
    fi
}

# Validate and set OpenSearch password
validate_opensearch_password() {
    if [ -z "$OPENSEARCH_PASSWORD" ]; then
        # No password set - use default
        OPENSEARCH_PASSWORD="AiServices@12345"
    fi
}

# Print section header
# Usage: print_header <title>
print_header() {
    echo "============================================================"
    echo "$1"
    echo "============================================================"
}

# Find pod in OpenShift across all namespaces
# Usage: find_pod_openshift <app-name> <component>
# Returns: "namespace pod-name" or empty string if not found
find_pod_openshift() {
    local APP_NAME="$1"
    local COMPONENT="$2"
    
    # For digitize, the actual label is "digitize-api"
    local SEARCH_COMPONENT="$COMPONENT"
    if [ "$COMPONENT" = "digitize" ]; then
        SEARCH_COMPONENT="digitize-api"
    fi
    
    local POD_INFO=$(oc get pods --all-namespaces -l "ai-services.io/application=${APP_NAME},ai-services.io/component=${SEARCH_COMPONENT}" -o jsonpath='{range .items[0]}{.metadata.namespace}{" "}{.metadata.name}{end}' 2>/dev/null)
    
    if [ -z "$POD_INFO" ]; then
        return 1
    fi
    
    echo "$POD_INFO"
    return 0
}

# Parse pod info into namespace and pod name
# Usage: parse_pod_info <pod-info-string>
# Sets global variables: NAMESPACE and POD_NAME
parse_pod_info() {
    local POD_INFO="$1"
    NAMESPACE=$(echo "$POD_INFO" | awk '{print $1}')
    POD_NAME=$(echo "$POD_INFO" | awk '{print $2}')
}

# Dispatch to runtime-specific function
# Usage: dispatch_runtime <runtime> <target> <operation> <args...>
dispatch_runtime() {
    local RUNTIME="$1"
    local TARGET="$2"
    local OPERATION="$3"
    shift 3
    
    local FUNC_NAME="${OPERATION}_${TARGET}_"
    if [ "$RUNTIME" = "openshift" ]; then
        FUNC_NAME="${FUNC_NAME}openshift"
    else
        FUNC_NAME="${FUNC_NAME}podman"
    fi
    
    $FUNC_NAME "$@"
}

# Print operation details (export or import)
# Usage: print_operation_details <operation> <app-name> <file>
print_operation_details() {
    local OPERATION="$1"
    local APP_NAME="$2"
    local FILE="$3"
    
    echo "App name: $APP_NAME"
    if [ "$OPERATION" = "export" ]; then
        echo "Output file: $FILE"
    else
        echo "Backup file: $FILE"
    fi
    echo ""
}

# Find and validate pod (combines find + validate + parse + display)
# Usage: find_and_validate_pod <app-name> <component>
# Sets global variables: NAMESPACE and POD_NAME
# Returns: 0 if found, 1 if not found (for digitize, allows fallback)
find_and_validate_pod() {
    local APP_NAME="$1"
    local COMPONENT="$2"
    
    print_info "Finding ${COMPONENT} pod for app: $APP_NAME..."
    local POD_INFO=$(find_pod_openshift "$APP_NAME" "$COMPONENT")
    
    if [ $? -ne 0 ] || [ -z "$POD_INFO" ]; then
        # For digitize, don't exit - allow fallback strategies in calling function
        if [ "$COMPONENT" = "digitize" ]; then
            NAMESPACE=""
            POD_NAME=""
            return 1
        fi
        # For other components, exit with error
        print_error "${COMPONENT} pod not found for app: $APP_NAME"
        print_error "Make sure the pod has labels: ai-services.io/application=${APP_NAME} and ai-services.io/component=${COMPONENT}"
        exit 1
    fi
    
    parse_pod_info "$POD_INFO"
    echo "  ✓ Found pod: $POD_NAME"
    echo "  ✓ Namespace: $NAMESPACE"
    return 0
}

# Create and wait for OpenShift pod
# Usage: create_openshift_pod <pod-name> <namespace> <image> <security-context> <volume-mounts> <volumes>
create_openshift_pod() {
    local POD_NAME="$1"
    local NAMESPACE="$2"
    local IMAGE="$3"
    local SECURITY_CONTEXT="$4"  # YAML format securityContext
    local VOLUME_MOUNTS="$5"     # YAML format volumeMounts (container level)
    local VOLUMES="$6"            # YAML format volumes (spec level)
    
    print_info "Creating pod: $POD_NAME..."
    
    cat <<EOF | oc apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: $POD_NAME
  namespace: $NAMESPACE
spec:
$SECURITY_CONTEXT
  containers:
  - name: worker
    image: $IMAGE
    command: ["sleep", "3600"]
$VOLUME_MOUNTS
$VOLUMES
  restartPolicy: Never
EOF
    
    print_info "Waiting for pod to be ready..."
    oc wait --for=condition=Ready pod/$POD_NAME -n $NAMESPACE --timeout=60s
    echo "  ✓ Pod ready: $POD_NAME"
}

# Cleanup resources (pod or container)
# Usage: cleanup_resources <runtime> <resource-name> [namespace]
cleanup_resources() {
    local RUNTIME="$1"
    local RESOURCE_NAME="$2"
    local NAMESPACE="$3"
    
    print_info "Cleaning up resources..."
    if [ "$RUNTIME" = "openshift" ]; then
        oc delete pod $RESOURCE_NAME -n $NAMESPACE --wait=false 2>/dev/null
    else
        podman stop $RESOURCE_NAME 2>/dev/null
    fi
}

# Manage Podman sidecar container lifecycle
# Usage: manage_podman_sidecar <operation> <pod-id> <script-file> <output-file>
manage_podman_sidecar() {
    local OPERATION="$1"  # "backup" or "restore"
    local POD_ID="$2"
    local SCRIPT_FILE="$3"
    local OUTPUT_FILE="$4"
    
    local SIDECAR_NAME="opensearch-${OPERATION}-sidecar-$$"
    
    print_info "Starting sidecar container with Python and opensearch-py..."
    
    # Start sidecar in same pod
    podman run -d \
        --name "$SIDECAR_NAME" \
        --pod "$POD_ID" \
        --rm \
        -e OPENSEARCH_PASSWORD="${OPENSEARCH_PASSWORD}" \
        registry.access.redhat.com/ubi9/python-312-minimal:9.8 \
        sleep 3600
    
    if [ $? -ne 0 ]; then
        print_error "Failed to start sidecar container"
        rm -f "$SCRIPT_FILE"
        exit 1
    fi
    
    print_info "Installing dependencies in sidecar..."
    podman exec "$SIDECAR_NAME" pip install --no-cache-dir opensearch-py==2.3.1
    
    if [ $? -ne 0 ]; then
        print_error "Failed to install dependencies"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f "$SCRIPT_FILE"
        exit 1
    fi
    
    # Copy script to sidecar
    local SCRIPT_NAME=$(basename "$SCRIPT_FILE")
    print_info "Copying ${OPERATION} script to sidecar..."
    podman cp "$SCRIPT_FILE" "$SIDECAR_NAME:/${SCRIPT_NAME}"
    
    if [ $? -ne 0 ]; then
        print_error "Failed to copy script"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f "$SCRIPT_FILE"
        exit 1
    fi
    
    # Execute based on operation type
    if [ "$OPERATION" = "backup" ]; then
        print_info "Running backup from sidecar..."
        podman exec "$SIDECAR_NAME" python3 "/${SCRIPT_NAME}" "$APP_NAME" "/tmp/$OUTPUT_FILE"
        
        if [ $? -ne 0 ]; then
            print_error "Backup failed"
            podman stop "$SIDECAR_NAME" 2>/dev/null
            rm -f "$SCRIPT_FILE"
            exit 1
        fi
        
        print_info "Copying backup to host..."
        podman cp "$SIDECAR_NAME:/tmp/$OUTPUT_FILE" "./$OUTPUT_FILE"
    else
        # Restore operation
        print_info "Copying backup to sidecar..."
        podman cp "$OUTPUT_FILE" "$SIDECAR_NAME:/tmp/backup.tar.gz"
        
        if [ $? -ne 0 ]; then
            print_error "Failed to copy backup"
            podman stop "$SIDECAR_NAME" 2>/dev/null
            rm -f "$SCRIPT_FILE"
            exit 1
        fi
        
        print_info "Running restore from sidecar..."
        podman exec "$SIDECAR_NAME" python3 "/${SCRIPT_NAME}" /tmp/backup.tar.gz
        
        if [ $? -ne 0 ]; then
            print_error "Restore failed"
            podman stop "$SIDECAR_NAME" 2>/dev/null
            rm -f "$SCRIPT_FILE"
            exit 1
        fi
    fi
    
    # Cleanup
    cleanup_resources "podman" "$SIDECAR_NAME"
    rm -f "$SCRIPT_FILE"
}

# Get OpenSearch service name
# Usage: get_opensearch_service <app-name> <namespace>
get_opensearch_service() {
    local APP_NAME="$1"
    local NAMESPACE="$2"
    
    local SERVICE=$(oc get svc -n $NAMESPACE -l "ai-services.io/application=${APP_NAME},ai-services.io/component=vectordb" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    
    if [ -z "$SERVICE" ]; then
        echo "opensearch"
    else
        echo "$SERVICE"
    fi
}

# Install OpenSearch dependencies in sidecar pod
# Usage: install_opensearch_dependencies <pod-name> <namespace>
install_opensearch_dependencies() {
    local POD_NAME="$1"
    local NAMESPACE="$2"
    
    print_info "Installing opensearch-py..."
    oc exec $POD_NAME -n $NAMESPACE -- pip install opensearch-py==2.3.1 >/dev/null 2>&1
}

# Install tar/gzip in helper pod
# Usage: install_tar_dependencies <pod-name> <namespace>
install_tar_dependencies() {
    local POD_NAME="$1"
    local NAMESPACE="$2"
    
    print_info "Installing tar in helper pod..."
    oc exec $POD_NAME -n $NAMESPACE -- microdnf install -y tar gzip >/dev/null 2>&1
}

# Export OpenSearch using sidecar container approach (Podman)
export_opensearch_podman() {
    local APP_NAME="$1"
    local OUTPUT_FILE="$2"
    
    local CONTAINER_NAME=$(podman ps --filter "label=ai-services.io/application=${APP_NAME}" --filter "name=opensearch" --format "{{.Names}}" | head -n 1)

    if [ -z "$CONTAINER_NAME" ]; then
        print_error "OpenSearch container not found for app: $APP_NAME"
        print_error "Make sure the container has label 'ai-services.io/application=${APP_NAME}' and name contains 'opensearch'"
        exit 1
    fi

    print_header "OpenSearch Export (Sidecar Container Approach)"
    echo "Container: $CONTAINER_NAME"
    print_operation_details "export" "$APP_NAME" "$OUTPUT_FILE"

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

    # Use helper function to manage sidecar lifecycle
    manage_podman_sidecar "backup" "$POD_ID" "/tmp/backup.py" "$OUTPUT_FILE"
            
    echo ""
    print_success "OpenSearch export completed!"
    echo "Backup file: $OUTPUT_FILE"
    ls -lh "$OUTPUT_FILE"
}

# Export Digitize (Podman)
export_digitize_podman() {
    local APP_NAME="$1"
    local OUTPUT_FILE="$2"

    print_header "Digitize Data Export"
    echo "Container cache path: /var/cache"
    print_operation_details "export" "$APP_NAME" "$OUTPUT_FILE"

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


# Import OpenSearch using sidecar container approach (Podman)
import_opensearch_podman() {
    local APP_NAME="$1"
    local BACKUP_FILE="$2"

    local CONTAINER_NAME=$(podman ps --filter "label=ai-services.io/application=${APP_NAME}" --filter "name=opensearch" --format "{{.Names}}" | head -n 1)

    if [ -z "$CONTAINER_NAME" ]; then
        print_error "OpenSearch container not found for app: $APP_NAME"
        print_error "Make sure the container has label 'ai-services.io/application=${APP_NAME}' and name contains 'opensearch'"
        exit 1
    fi

    print_header "OpenSearch Import (Sidecar Container Approach)"
    echo "Container: $CONTAINER_NAME"
    print_operation_details "import" "$APP_NAME" "$BACKUP_FILE"

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

    # Use helper function to manage sidecar lifecycle
    manage_podman_sidecar "restore" "$POD_ID" "/tmp/restore.py" "$BACKUP_FILE"

    echo ""
    print_success "OpenSearch import completed!"
}

# Import Digitize (Podman)
import_digitize_podman() {
    local APP_NAME="$1"
    local BACKUP_FILE="$2"

    print_header "Digitize Data Import"
    print_operation_details "import" "$APP_NAME" "$BACKUP_FILE"

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


# Export OpenSearch (OpenShift)
export_opensearch_openshift() {
    local APP_NAME="$1"
    local OUTPUT_FILE="$2"

    print_header "OpenSearch Data Export (OpenShift)"
    print_operation_details "export" "$APP_NAME" "$OUTPUT_FILE"

    # Find and validate OpenSearch pod
    find_and_validate_pod "$APP_NAME" "vectordb"
    local OPENSEARCH_POD="$POD_NAME"
    
    # Get OpenSearch service name
    local OPENSEARCH_SERVICE=$(get_opensearch_service "$APP_NAME" "$NAMESPACE")
    echo "  ✓ OpenSearch service: $OPENSEARCH_SERVICE"
    
    # Create sidecar pod
    local SIDECAR_POD="opensearch-backup-sidecar-$(date +%s)"
    
    local SECURITY_CONTEXT=""
    
    local VOLUME_MOUNTS="    env:
    - name: OPENSEARCH_PASSWORD
      value: \"$OPENSEARCH_PASSWORD\"
    - name: OPENSEARCH_HOST
      value: \"$OPENSEARCH_SERVICE\""
    
    local VOLUMES=""
    
    create_openshift_pod "$SIDECAR_POD" "$NAMESPACE" "registry.access.redhat.com/ubi9/python-312:9.7" "$SECURITY_CONTEXT" "$VOLUME_MOUNTS" "$VOLUMES"
    
    install_opensearch_dependencies "$SIDECAR_POD" "$NAMESPACE"
    
    print_info "Running backup..."
    cat << 'EOFPYTHON' | oc exec -i $SIDECAR_POD -n $NAMESPACE -- python3 2>/dev/null
import json, os
from pathlib import Path
from opensearchpy import OpenSearch

class OpenSearchBackup:
    def __init__(self):
        host = os.environ.get("OPENSEARCH_HOST", "opensearch")
        self.client = OpenSearch(
            hosts=[{"host": host, "port": 9200}],
            http_auth=("admin", os.environ.get("OPENSEARCH_PASSWORD", "AiServices@12345")),
            use_ssl=True, verify_certs=False, ssl_show_warn=False
        )
        self.backup_dir = Path("/tmp/opensearch_backup")
        self.backup_dir.mkdir(exist_ok=True)
    
    def export_data(self):
        indices = [idx for idx in self.client.indices.get_alias().keys() if idx.startswith("rag_")]
        total_docs = 0
        for index_name in indices:
            settings = self.client.indices.get_settings(index=index_name)
            mapping = self.client.indices.get_mapping(index=index_name)
            with open(self.backup_dir / f"{index_name}_settings.json", "w") as f:
                json.dump(settings, f)
            with open(self.backup_dir / f"{index_name}_mapping.json", "w") as f:
                json.dump(mapping, f)
            documents = []
            response = self.client.search(index=index_name, body={"query": {"match_all": {}}, "size": 1000}, params={"scroll": "5m"})
            scroll_id = response["_scroll_id"]
            hits = response["hits"]["hits"]
            documents.extend(hits)
            while len(hits) > 0:
                response = self.client.scroll(scroll_id=scroll_id, params={"scroll": "5m"})
                scroll_id = response["_scroll_id"]
                hits = response["hits"]["hits"]
                documents.extend(hits)
            with open(self.backup_dir / f"{index_name}_data.json", "w") as f:
                json.dump(documents, f)
            total_docs += len(documents)
        return len(indices), total_docs
    
    def run(self):
        indices_count, docs_count = self.export_data()
        print(f"  ✓ Backed up {indices_count} indices with {docs_count} documents")

if __name__ == "__main__":
    backup = OpenSearchBackup()
    backup.run()
EOFPYTHON

    print_info "Copying backup from sidecar pod..."
    oc exec $SIDECAR_POD -n $NAMESPACE -- tar czf /tmp/backup.tar.gz -C /tmp opensearch_backup 2>/dev/null
    oc cp $NAMESPACE/$SIDECAR_POD:/tmp/backup.tar.gz "$OUTPUT_FILE" 2>/dev/null
    
    cleanup_resources "openshift" "$SIDECAR_POD" "$NAMESPACE"
    
    echo ""
    print_success "OpenSearch export completed!"
}

# Export Digitize (OpenShift)
export_digitize_openshift() {
    local APP_NAME="$1"
    local OUTPUT_FILE="$2"

    print_header "Digitize Data Export (OpenShift)"
    print_operation_details "export" "$APP_NAME" "$OUTPUT_FILE"

    # Find and validate digitize pod
    find_and_validate_pod "$APP_NAME" "digitize"
    local DIGITIZE_POD="$POD_NAME"
    
    # If pod not found by labels, try fallback strategies
    if [ -z "$DIGITIZE_POD" ]; then
        echo "  Searching for digitize pod with fallback strategies..."
        
        # First, get the namespace from any pod with the app label
        NAMESPACE=$(oc get pods --all-namespaces -l "ai-services.io/application=${APP_NAME}" -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null)
        
        if [ -z "$NAMESPACE" ]; then
            print_error "No pods found for app: $APP_NAME"
            exit 1
        fi
        
        echo "  ✓ Found namespace: $NAMESPACE"
        
        # Strategy 1: By label in namespace
        DIGITIZE_POD=$(timeout 10 oc get pods -n $NAMESPACE -l "ai-services.io/component=digitize" -o name 2>/dev/null | head -n 1 | sed 's|pod/||')
    fi
    
    if [ -z "$DIGITIZE_POD" ]; then
        echo "  Label search failed, trying name pattern..."
        # Strategy 2: By name pattern with backend
        DIGITIZE_POD=$(timeout 10 oc get pods -n $NAMESPACE -o name 2>/dev/null | grep -i digitize | grep -i backend | head -n 1 | sed 's|pod/||')
    fi
    
    if [ -z "$DIGITIZE_POD" ]; then
        echo "  Backend pattern failed, trying any digitize pod..."
        # Strategy 3: Any pod with digitize in name
        DIGITIZE_POD=$(timeout 10 oc get pods -n $NAMESPACE -o name 2>/dev/null | grep -i digitize | head -n 1 | sed 's|pod/||')
    fi
    
    if [ -z "$DIGITIZE_POD" ]; then
        print_error "Digitize pod not found in namespace: $NAMESPACE"
        print_error "Available pods:"
        oc get pods -n $NAMESPACE
        exit 1
    fi

    echo "  ✓ Found pod: $DIGITIZE_POD"
    
    # Get PVC for digitize pod
    print_info "Getting PVC for digitize pod..."
    local PVC_NAME=$(oc get pod $DIGITIZE_POD -n $NAMESPACE -o jsonpath='{.spec.volumes[?(@.persistentVolumeClaim)].persistentVolumeClaim.claimName}' | head -n 1)
    
    if [ -z "$PVC_NAME" ]; then
        print_error "No PVC found for digitize pod"
        exit 1
    fi
    
    echo "  ✓ Found PVC: $PVC_NAME"
    
    # Create helper pod with PVC mount
    local HELPER_POD="digitize-backup-helper-$(date +%s)"
    
    local SECURITY_CONTEXT="  securityContext:
    runAsUser: 0"
    
    local VOLUME_MOUNTS="    volumeMounts:
    - name: data
      mountPath: /data"
    
    local VOLUMES="  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: $PVC_NAME"
    
    create_openshift_pod "$HELPER_POD" "$NAMESPACE" "registry.access.redhat.com/ubi9/ubi-minimal:9.4" "$SECURITY_CONTEXT" "$VOLUME_MOUNTS" "$VOLUMES"
    
    install_tar_dependencies "$HELPER_POD" "$NAMESPACE"
    
    print_info "Creating backup in helper pod..."
    oc exec $HELPER_POD -n $NAMESPACE -- tar czf /tmp/backup.tar.gz -C /data . 2>/dev/null
    
    print_info "Copying backup from helper pod..."
    oc cp $NAMESPACE/$HELPER_POD:/tmp/backup.tar.gz "$OUTPUT_FILE" 2>/dev/null
    
    # Count files from the created backup
    local TOTAL_FILES=$(tar -tzf "$OUTPUT_FILE" 2>/dev/null | grep -v '/$' | wc -l)
    echo "  ✓ Backed up $TOTAL_FILES files from PVC"
    
    cleanup_resources "openshift" "$HELPER_POD" "$NAMESPACE"
    
    echo ""
    print_success "Digitize data export completed!"
    echo "Backup file: $OUTPUT_FILE"
    ls -lh "$OUTPUT_FILE"
}

# Import OpenSearch (OpenShift)
import_opensearch_openshift() {
    local APP_NAME="$1"
    local BACKUP_FILE="$2"

    print_header "OpenSearch Data Import (OpenShift)"
    print_operation_details "import" "$APP_NAME" "$BACKUP_FILE"

    # Find and validate OpenSearch pod
    find_and_validate_pod "$APP_NAME" "vectordb"
    local OPENSEARCH_POD="$POD_NAME"
    
    # Get OpenSearch service name
    local OPENSEARCH_SERVICE=$(get_opensearch_service "$APP_NAME" "$NAMESPACE")
    echo "  ✓ OpenSearch service: $OPENSEARCH_SERVICE"
    
    # Create sidecar pod
    local SIDECAR_POD="opensearch-restore-sidecar-$(date +%s)"
    
    local SECURITY_CONTEXT=""
    
    local VOLUME_MOUNTS="    env:
    - name: OPENSEARCH_PASSWORD
      value: \"$OPENSEARCH_PASSWORD\"
    - name: OPENSEARCH_HOST
      value: \"$OPENSEARCH_SERVICE\""
    
    local VOLUMES=""
    
    create_openshift_pod "$SIDECAR_POD" "$NAMESPACE" "registry.access.redhat.com/ubi9/python-312:9.7" "$SECURITY_CONTEXT" "$VOLUME_MOUNTS" "$VOLUMES"
    
    install_opensearch_dependencies "$SIDECAR_POD" "$NAMESPACE"
    
    print_info "Copying backup to sidecar pod..."
    oc cp "$BACKUP_FILE" $NAMESPACE/$SIDECAR_POD:/tmp/backup.tar.gz 2>/dev/null
    
    print_info "Running restore..."
    cat << 'EOFPYTHON' | oc exec -i $SIDECAR_POD -n $NAMESPACE -- python3 - /tmp/backup.tar.gz
import json, os, sys, tarfile, tempfile
from pathlib import Path
from opensearchpy import OpenSearch, helpers

class OpenSearchRestore:
    def __init__(self, backup_file):
        self.backup_file = backup_file
        host = os.environ.get("OPENSEARCH_HOST", "opensearch")
        password = os.environ.get("OPENSEARCH_PASSWORD", "AiServices@12345")
        print(f"Connecting to OpenSearch at {host}:9200...")
        self.client = OpenSearch(
            hosts=[{"host": host, "port": 9200}],
            http_auth=("admin", password),
            use_ssl=True, verify_certs=False, ssl_show_warn=False, timeout=30
        )
    
    def restore_index(self, index_name, backup_dir):
        print(f"  Restoring index: {index_name}")
        with open(backup_dir / f"{index_name}_settings.json") as f:
            settings = json.load(f)
        with open(backup_dir / f"{index_name}_mapping.json") as f:
            mapping = json.load(f)
        
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
        
        with open(backup_dir / f"{index_name}_data.json") as f:
            documents = json.load(f)
        
        if documents:
            actions = [{"_index": index_name, "_id": doc["_id"], "_source": doc["_source"]} for doc in documents]
            success, _ = helpers.bulk(self.client, actions, stats_only=False, raise_on_error=False, refresh=True)
            print(f"    ✓ {success} documents restored")
            return success
        return 0
    
    def run(self):
        print("Extracting backup...")
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            with tarfile.open(self.backup_file, "r:gz") as tar:
                tar.extractall(temp_path)
            
            # Check for backup/opensearch directory structure (matches export format)
            backup_dir = temp_path / "backup" / "opensearch"
            if not backup_dir.exists():
                # Fallback to old format
                backup_dir = temp_path / "opensearch_backup"
            
            if backup_dir.exists():
                indices = [f.stem.replace("_data", "") for f in backup_dir.glob("*_data.json")]
                print(f"Found {len(indices)} indices to restore")
                total_docs = 0
                for idx in indices:
                    total_docs += self.restore_index(idx, backup_dir)
                print(f"✓ Restore completed: {len(indices)} indices, {total_docs} documents")
            else:
                print("ERROR: No backup data found in archive")
                sys.exit(1)

if __name__ == "__main__":
    try:
        restore = OpenSearchRestore(sys.argv[1])
        restore.run()
    except Exception as e:
        print(f"ERROR: {str(e)}")
        import traceback
        traceback.print_exc()
        sys.exit(1)
EOFPYTHON

    cleanup_resources "openshift" "$SIDECAR_POD" "$NAMESPACE"
    
    echo ""
    print_success "OpenSearch import completed!"
}

# Import Digitize (OpenShift)
import_digitize_openshift() {
    local APP_NAME="$1"
    local BACKUP_FILE="$2"

    print_header "Digitize Data Import (OpenShift)"
    print_operation_details "import" "$APP_NAME" "$BACKUP_FILE"

    # Find and validate digitize pod
    find_and_validate_pod "$APP_NAME" "digitize"
    local DIGITIZE_POD="$POD_NAME"
    
    if [ -z "$DIGITIZE_POD" ]; then
        echo "  Backend pattern failed, trying any digitize pod..."
        # Strategy 3: Any pod with digitize in name
        DIGITIZE_POD=$(timeout 10 oc get pods -n $NAMESPACE -o name 2>/dev/null | grep -i digitize | head -n 1 | sed 's|pod/||')
    fi
    
    if [ -z "$DIGITIZE_POD" ]; then
        print_error "Digitize pod not found in namespace: $NAMESPACE"
        print_error "Available pods:"
        oc get pods -n $NAMESPACE
        exit 1
    fi

    echo "  ✓ Found pod: $DIGITIZE_POD"
    
    # Get PVC for digitize pod
    print_info "Getting PVC for digitize pod..."
    local PVC_NAME=$(oc get pod $DIGITIZE_POD -n $NAMESPACE -o jsonpath='{.spec.volumes[?(@.persistentVolumeClaim)].persistentVolumeClaim.claimName}' | head -n 1)
    
    if [ -z "$PVC_NAME" ]; then
        print_error "No PVC found for digitize pod"
        exit 1
    fi
    
    echo "  ✓ Found PVC: $PVC_NAME"
    
    # Create helper pod with PVC mount
    local HELPER_POD="digitize-restore-helper-$(date +%s)"
    
    local SECURITY_CONTEXT="  securityContext:
    runAsUser: 0"
    
    local VOLUME_MOUNTS="    volumeMounts:
    - name: data
      mountPath: /data"
    
    local VOLUMES="  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: $PVC_NAME"
    
    create_openshift_pod "$HELPER_POD" "$NAMESPACE" "registry.access.redhat.com/ubi9/ubi-minimal:9.4" "$SECURITY_CONTEXT" "$VOLUME_MOUNTS" "$VOLUMES"
    
    install_tar_dependencies "$HELPER_POD" "$NAMESPACE"
    
    print_info "Copying backup to helper pod..."
    oc cp "$BACKUP_FILE" $NAMESPACE/$HELPER_POD:/tmp/restore.tar.gz 2>/dev/null
    
    print_info "Extracting backup in helper pod..."
    oc exec $HELPER_POD -n $NAMESPACE -- tar xzf /tmp/restore.tar.gz -C /data 2>/dev/null
    
    # Count files from the backup archive
    local RESTORED_FILES=$(tar -tzf "$BACKUP_FILE" 2>/dev/null | grep -v '/$' | wc -l)
    echo "  ✓ Restored $RESTORED_FILES files to PVC"
    
    cleanup_resources "openshift" "$HELPER_POD" "$NAMESPACE"
    
    # Restart digitize pod to refresh UI
    print_info "Restarting digitize pod to refresh UI..."
    oc delete pod $DIGITIZE_POD -n $NAMESPACE --wait=false
    echo "  ✓ Digitize pod restart initiated"
    
    echo ""
    print_success "Digitize data import completed!"
    print_info "Wait for pods to restart, then refresh your browser to see documents"
}

# Main command dispatcher
main() {
    if [ $# -eq 0 ]; then
        show_usage
        exit 1
    fi
    
    # Validate OpenSearch password
    validate_opensearch_password

    # Parse --runtime parameter (must be at the end)
    local RUNTIME=""
    local LAST_ARG="${@: -1}"
    local SECOND_LAST="${@: -2:1}"
    
    # Check if --runtime is at the end
    if [ "$SECOND_LAST" = "--runtime" ]; then
        RUNTIME="$LAST_ARG"
        validate_runtime "$RUNTIME"
        # Remove last two arguments (--runtime and its value)
        set -- "${@:1:$(($#-2))}"
    else
        print_error "Missing --runtime parameter at the end"
        echo "Usage: ./backup-restore.sh <command> <target> <app-name> [options] --runtime <podman|openshift>"
        exit 1
    fi
    
    # Now parse the command and validate parameters
    local COMMAND="$1"
    local TARGET="$2"
    local APP_NAME="$3"
    
    case "$COMMAND" in
        export)
            # Validate parameters for export
            validate_app_name "$APP_NAME"
            local OUTPUT_FILE="${4:-${TARGET}_backup_$(date +%Y%m%d_%H%M%S).tar.gz}"
            
            case "$TARGET" in
                opensearch|digitize)
                    dispatch_runtime "$RUNTIME" "$TARGET" "export" "$APP_NAME" "$OUTPUT_FILE"
                    ;;
                *)
                    print_error "Unknown export target: $TARGET"
                    echo "Valid targets: opensearch, digitize"
                    exit 1
                    ;;
            esac
            ;;
        import)
            # Validate parameters for import
            validate_app_name "$APP_NAME"
            local BACKUP_FILE="$4"
            validate_backup_file "$BACKUP_FILE"
            
            case "$TARGET" in
                opensearch|digitize)
                    dispatch_runtime "$RUNTIME" "$TARGET" "import" "$APP_NAME" "$BACKUP_FILE"
                    ;;
                *)
                    print_error "Unknown import target: $TARGET"
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
