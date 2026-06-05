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
set -o pipefail

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
    
    # Check if runtime parameter is provided
    if [[ -z "$RUNTIME" ]]; then
        print_error "--runtime parameter is mandatory"
        echo ""
        exit 1
    fi
    
    # Check if runtime value is valid
    if [[ "$RUNTIME" != "podman" && "$RUNTIME" != "openshift" ]]; then
        print_error "Error: Invalid runtime value '$RUNTIME'. Must be 'podman' or 'openshift'"
        echo ""
        exit 1
    fi
}

# Validate app name parameter
validate_app_name() {
    local APP_NAME="$1"
    if [ -z "$APP_NAME" ] || [[ "$APP_NAME" == --* ]]; then
        print_error "App name is required"
        exit 1
    fi
}

# Validate target parameter
validate_target() {
    local TARGET="$1"
    
    # Check if target parameter is provided
    if [ -z "$TARGET" ] || [[ "$TARGET" == --* ]]; then
        print_error "Target is required"
        echo "Valid targets: opensearch, digitize"
        exit 1
    fi
    
    # Check if target value is valid
    case "$TARGET" in
        opensearch|digitize)
            # Valid target, continue
            ;;
        *)
            print_error "Unknown target: $TARGET"
            echo "Valid targets: opensearch, digitize"
            exit 1
            ;;
    esac
}

# Validate backup file parameter
validate_backup_file() {
    local BACKUP_FILE="$1"
    if [ -z "$BACKUP_FILE" ] || [[ "$BACKUP_FILE" == --* ]]; then
        print_error "Backup file is required"
        exit 1
    fi
    if [ ! -f "$BACKUP_FILE" ]; then
        print_error "Backup file not found: $BACKUP_FILE"
        exit 1
    fi
}

# Check and set OpenSearch password if not set
check_and_set_opensearch_password() {
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

# Find pod in OpenShift using app name as namespace
# Usage: find_pod_openshift <app-name> <component>
# Returns: "namespace pod-name" or empty string if not found
find_pod_openshift() {
    local APP_NAME="$1"
    local COMPONENT="$2"
    
    # Use app name as the namespace (convention: namespace = app-name)
    local NAMESPACE="$APP_NAME"
    
    # For digitize, the actual label is "digitize-api"
    local SEARCH_COMPONENT="$COMPONENT"
    if [ "$COMPONENT" = "digitize" ]; then
        SEARCH_COMPONENT="digitize-api"
    fi
    
    # Search in the specific namespace instead of all namespaces
    local POD_NAME=$(oc get pods -n "$NAMESPACE" -l "ai-services.io/application=${APP_NAME},ai-services.io/component=${SEARCH_COMPONENT}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    
    if [ -z "$POD_NAME" ]; then
        return 1
    fi
    
    echo "$NAMESPACE $POD_NAME"
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

# Start the operation using runtime-specific function
# Usage: start_operation <runtime> <target> <operation> <args...>
start_operation() {
    local RUNTIME="$1"
    local TARGET="$2"
    local OPERATION="$3"
    shift 3
    
    local FUNC_NAME="${OPERATION}_${TARGET}_${RUNTIME}"    
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
# Usage: find_and_validate_pod_openshift <app-name> <component>
# Sets global variables: NAMESPACE and POD_NAME
# Returns: 0 if found, 1 if not found (for digitize, allows fallback)
find_and_validate_pod_openshift() {
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
        print_error "Expected namespace: $APP_NAME (convention: namespace = app-name)"
        print_error "Make sure:"
        print_error "  1. Namespace '$APP_NAME' exists"
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
    if ! oc wait --for=condition=Ready pod/$POD_NAME -n $NAMESPACE --timeout=60s; then
        print_error "Pod failed to become ready within 60s"
        print_info "Fetching pod status and logs for debugging..."
        oc describe pod/$POD_NAME -n $NAMESPACE || true
        oc logs pod/$POD_NAME -n $NAMESPACE --all-containers=true || true
        
        print_warning "Cleaning up failed pod..."
        cleanup_resources "openshift" "$POD_NAME" "$NAMESPACE"
        return 1
    fi
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

# Copy directory from container/pod to host
# Usage: copy_from_runtime <runtime> <source> <dest> [namespace]
copy_from_runtime() {
    local RUNTIME="$1"
    local SOURCE="$2"      # For podman: container:/path, for openshift: pod:/path
    local DEST="$3"
    local NAMESPACE="$4"   # Only used for openshift
    
    if [ "$RUNTIME" = "openshift" ]; then
        oc cp "$NAMESPACE/$SOURCE" "$DEST"
    else
        podman cp "$SOURCE" "$DEST"
    fi
}

# Copy directory from host to container/pod
# Usage: copy_to_runtime <runtime> <source> <dest> [namespace]
copy_to_runtime() {
    local RUNTIME="$1"
    local SOURCE="$2"
    local DEST="$3"        # For podman: container:/path, for openshift: pod:/path
    local NAMESPACE="$4"   # Only used for openshift
    
    if [ "$RUNTIME" = "openshift" ]; then
        oc cp "$SOURCE" "$NAMESPACE/$DEST"
    else
        podman cp "$SOURCE" "$DEST"
    fi
}

# Create tar archive from directory
# Usage: create_tar_archive <source-dir> <output-file>
create_tar_archive() {
    local SOURCE_DIR="$1"
    local OUTPUT_FILE="$2"
    
    cd "$SOURCE_DIR"
    if ! tar -czf "$OUTPUT_FILE" backup/; then
        cd "$OLDPWD"
        return 1
    fi
    cd "$OLDPWD"
    return 0
}

# Extract tar archive to directory
# Usage: extract_tar_archive <backup-file> <dest-dir>
extract_tar_archive() {
    local BACKUP_FILE="$1"
    local DEST_DIR="$2"
    
    tar -xzf "$BACKUP_FILE" -C "$DEST_DIR"
}

# Count files and calculate size in a directory
# Usage: count_backup_files <directory>
# Returns: "file_count|size_string"
count_backup_files() {
    local DIR="$1"
    local FILES=$(find "$DIR" -type f 2>/dev/null | wc -l)
    local SIZE=$(du -sh "$DIR" 2>/dev/null | awk '{print $1}')
    echo "$FILES|$SIZE"
}

# Create helper pod with PVC mount for OpenShift operations
# Usage: create_pvc_helper_pod <pod-name> <namespace> <pvc-name> [mount-path]
# Returns: 0 on success, 1 on failure
create_pvc_helper_pod() {
    local POD_NAME="$1"
    local NAMESPACE="$2"
    local PVC_NAME="$3"
    local MOUNT_PATH="${4:-/data}"
    
    local SECURITY_CONTEXT="  securityContext:
    runAsUser: 0"
    
    local VOLUME_MOUNTS="    volumeMounts:
    - name: data
      mountPath: $MOUNT_PATH"
    
    local VOLUMES="  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: $PVC_NAME"
    
    create_openshift_pod "$POD_NAME" "$NAMESPACE" "registry.access.redhat.com/ubi9/ubi-minimal:9.8" "$SECURITY_CONTEXT" "$VOLUME_MOUNTS" "$VOLUMES"
    
    # Install tar in the helper pod (required for oc cp)
    print_info "Installing tar in helper pod..."
    if ! oc exec -n "$NAMESPACE" "$POD_NAME" -- microdnf install -y tar 2>/dev/null; then
        print_warning "Failed to install tar, trying with microdnf update first..."
        oc exec -n "$NAMESPACE" "$POD_NAME" -- microdnf update -y 2>/dev/null || true
        if ! oc exec -n "$NAMESPACE" "$POD_NAME" -- microdnf install -y tar; then
            print_error "Failed to install tar in helper pod"
            oc delete pod "$POD_NAME" -n "$NAMESPACE" 2>/dev/null || true
            return 1
        fi
    fi
    echo "  ✓ tar installed successfully"
}

# Find digitize pod and create helper pod with PVC mount
# Usage: find_digitize_pod_and_create_helper <app-name> <operation>
# Sets global variables: NAMESPACE, DIGITIZE_POD, HELPER_POD, PVC_NAME
# Returns: 0 on success, exits on failure
find_digitize_pod_and_create_helper() {
    local APP_NAME="$1"
    local OPERATION="$2"  # "backup" or "restore"
    
    # Find and validate digitize pod
    find_and_validate_pod_openshift "$APP_NAME" "digitize"
    DIGITIZE_POD="$POD_NAME"
    
    # If pod not found by labels, try fallback strategies
    if [ -z "$DIGITIZE_POD" ]; then
        echo "  Searching for digitize pod with fallback strategies..."
        
        # Use app name as the namespace (convention: namespace = app-name)
        NAMESPACE="$APP_NAME"
        
        echo "  ✓ Using namespace: $NAMESPACE"
        
        # Strategy 1: By digitize label in namespace
        DIGITIZE_POD=$(timeout 10 oc get pods -n $NAMESPACE -l "ai-services.io/component=digitize" -o name 2>/dev/null | sed -n '1s|pod/||p')
    fi
    
    if [ -z "$DIGITIZE_POD" ]; then
        echo "  Label search failed, trying name pattern..."
        # Strategy 2: By name pattern with backend
        DIGITIZE_POD=$(timeout 10 oc get pods -n $NAMESPACE -o name 2>/dev/null | grep -im1 "digitize.*backend" | sed 's|pod/||')
    fi
    
    if [ -z "$DIGITIZE_POD" ]; then
        echo "  Backend pattern failed, trying any digitize pod..."
        # Strategy 3: Any pod with digitize in name
        DIGITIZE_POD=$(timeout 10 oc get pods -n $NAMESPACE -o name 2>/dev/null | grep -im1 "digitize" | sed 's|pod/||')
    fi
    
    if [ -z "$DIGITIZE_POD" ]; then
        print_error "Digitize pod not found in namespace: $NAMESPACE"
        print_error "Available pods:"
        oc get pods -n $NAMESPACE
        exit 1
    fi

    echo "  ✓ Found pod: $DIGITIZE_POD"
    
    # Get PVC for digitize pod
    print_info "Getting pod details and PVC..."
    PVC_NAME=$(oc get pod $DIGITIZE_POD -n $NAMESPACE -o jsonpath='{.spec.volumes[?(@.persistentVolumeClaim)].persistentVolumeClaim.claimName}' 2>/dev/null | awk '{print $1}')
    
    if [ -z "$PVC_NAME" ]; then
        print_error "No PVC found for digitize pod"
        exit 1
    fi
    
    echo "  ✓ Found PVC: $PVC_NAME"
    
    # Create helper pod with PVC mount
    HELPER_POD="digitize-${OPERATION}-helper-$(date +%s)"
    create_pvc_helper_pod "$HELPER_POD" "$NAMESPACE" "$PVC_NAME" "/var/cache"
}

# Create OpenSearch sidecar pod for backup/restore operations
# Usage: create_opensearch_sidecar_pod <app-name> <operation>
# Sets global variables: NAMESPACE, OPENSEARCH_POD, SIDECAR_POD, OPENSEARCH_SERVICE
# Returns: 0 on success, exits on failure
create_opensearch_sidecar_pod() {
    local APP_NAME="$1"
    local OPERATION="$2"  # "backup" or "restore"
    
    # Find and validate OpenSearch pod
    find_and_validate_pod_openshift "$APP_NAME" "vectordb"
    OPENSEARCH_POD="$POD_NAME"
    
    # Get OpenSearch service name
    OPENSEARCH_SERVICE=$(get_opensearch_service "$APP_NAME" "$NAMESPACE")
    echo "  ✓ OpenSearch service: $OPENSEARCH_SERVICE"
    
    # Create sidecar pod
    SIDECAR_POD="opensearch-${OPERATION}-sidecar-$(date +%s)"
    
    local SECURITY_CONTEXT=""
    
    local VOLUME_MOUNTS="    env:
    - name: OPENSEARCH_PASSWORD
      value: \"$OPENSEARCH_PASSWORD\"
    - name: OPENSEARCH_HOST
      value: \"$OPENSEARCH_SERVICE\""
    
    local VOLUMES=""
    
    create_openshift_pod "$SIDECAR_POD" "$NAMESPACE" "registry.access.redhat.com/ubi9/python-312:9.8" "$SECURITY_CONTEXT" "$VOLUME_MOUNTS" "$VOLUMES"
    
    install_opensearch_dependencies "$SIDECAR_POD" "$NAMESPACE"
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
    if ! podman exec "$SIDECAR_NAME" pip install --no-cache-dir opensearch-py==2.3.1; then
        print_error "Failed to install dependencies"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f "$SCRIPT_FILE"
        exit 1
    fi
    
    # Copy script to sidecar
    local SCRIPT_NAME=$(basename "$SCRIPT_FILE")
    print_info "Copying ${OPERATION} script to sidecar..."
    if ! podman cp "$SCRIPT_FILE" "$SIDECAR_NAME:/${SCRIPT_NAME}"; then
        print_error "Failed to copy script"
        podman stop "$SIDECAR_NAME" 2>/dev/null
        rm -f "$SCRIPT_FILE"
        exit 1
    fi
    
    # Execute based on operation type
    if [ "$OPERATION" = "backup" ]; then
        print_info "Running backup from sidecar..."
        if ! podman exec "$SIDECAR_NAME" python3 "/${SCRIPT_NAME}" "$APP_NAME" "/tmp/$OUTPUT_FILE"; then
            print_error "Backup failed"
            podman stop "$SIDECAR_NAME" 2>/dev/null
            rm -f "$SCRIPT_FILE"
            exit 1
        fi
        
        print_info "Copying backup to host..."
        if ! podman cp "$SIDECAR_NAME:/tmp/$OUTPUT_FILE" "./$OUTPUT_FILE"; then
            print_error "Failed to copy backup from sidecar"
            podman stop "$SIDECAR_NAME" 2>/dev/null
            rm -f "$SCRIPT_FILE"
            return 1
        fi
    else
        # Restore operation
        print_info "Copying backup to sidecar..."
        if ! podman cp "$OUTPUT_FILE" "$SIDECAR_NAME:/tmp/backup.tar.gz"; then
            print_error "Failed to copy backup"
            podman stop "$SIDECAR_NAME" 2>/dev/null
            rm -f "$SCRIPT_FILE"
            exit 1
        fi
        
        print_info "Running restore from sidecar..."
        if ! podman exec "$SIDECAR_NAME" python3 "/${SCRIPT_NAME}" /tmp/backup.tar.gz; then
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
    if ! oc exec $POD_NAME -n $NAMESPACE -- pip install opensearch-py==2.3.1 >/dev/null; then
        print_error "Failed to install opensearch-py"
        return 1
    fi
}


# Create OpenSearch backup Python script
# Usage: create_opensearch_backup_script <output-file>
create_opensearch_backup_script() {
    local SCRIPT_FILE="$1"
    cat > "$SCRIPT_FILE" << 'EOFPYTHON'
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
}

# Create OpenSearch restore Python script
# Usage: create_opensearch_restore_script <output-file>
create_opensearch_restore_script() {
    local SCRIPT_FILE="$1"
    cat > "$SCRIPT_FILE" << 'EOFPYTHON'
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
}

# Export OpenSearch using sidecar container approach (Podman)
export_opensearch_podman() {
    local APP_NAME="$1"
    local OUTPUT_FILE="$2"
    
    local CONTAINER_NAME=$(podman ps --filter "label=ai-services.io/application=${APP_NAME}" --filter "name=opensearch" --format "{{.Names}}" | head -n 1)

    if [ -z "$CONTAINER_NAME" ]; then
        print_error "OpenSearch container not found for app: $APP_NAME"
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

    # Create Python backup script using helper function
    print_info "Creating backup script..."
    create_opensearch_backup_script "/tmp/backup.py"

    # Use helper function to manage sidecar lifecycle
    manage_podman_sidecar "backup" "$POD_ID" "/tmp/backup.py" "$OUTPUT_FILE"
            
    echo ""
    print_success "OpenSearch export completed!"
    echo "Backup file: $OUTPUT_FILE"
    ls -lh "$OUTPUT_FILE"
}

# Unified Digitize Export (works for both Podman and OpenShift)
# Usage: export_digitize <runtime> <app-name> <output-file> <resource-name> <source-path> [namespace]
export_digitize() {
    local RUNTIME="$1"
    local APP_NAME="$2"
    local OUTPUT_FILE="$3"
    local RESOURCE_NAME="$4"  # Container name or Pod name
    local SOURCE_PATH="$5"     # Path in container/pod (e.g., /var/cache or /data)
    local NAMESPACE="$6"       # Only for OpenShift

    print_header "Digitize Data Export ($RUNTIME)"
    print_operation_details "export" "$APP_NAME" "$OUTPUT_FILE"

    # Save current working directory
    local CURRENT_DIR="$PWD"
    
    print_info "Creating backup from $RUNTIME resource ($RESOURCE_NAME)..."
    local TEMP_DIR=$(mktemp -d)
    local BACKUP_DIR="$TEMP_DIR/backup"
    mkdir -p "$BACKUP_DIR"

    # Copy files from container/pod to host
    print_info "Backing up $SOURCE_PATH..."
    if ! copy_from_runtime "$RUNTIME" "$RESOURCE_NAME:$SOURCE_PATH" "$BACKUP_DIR/cache" "$NAMESPACE"; then
        print_error "Failed to copy data from $RUNTIME resource"
        rm -rf "$TEMP_DIR"
        return 1
    fi
    
    # Count files and calculate size
    local STATS=$(count_backup_files "$BACKUP_DIR/cache")
    local TOTAL_FILES=$(echo "$STATS" | cut -d'|' -f1)
    local TOTAL_SIZE=$(echo "$STATS" | cut -d'|' -f2)
    
    if [ "$TOTAL_FILES" -eq "0" ]; then
        print_warning "No files found in $SOURCE_PATH"
    fi

    echo "  ✓ Backed up $TOTAL_FILES files ($TOTAL_SIZE)"

    # Create tar archive on host
    print_info "Creating backup archive..."
    if ! create_tar_archive "$TEMP_DIR" "$CURRENT_DIR/$OUTPUT_FILE"; then
        print_error "Failed to create backup archive"
        rm -rf "$TEMP_DIR"
        return 1
    fi
    rm -rf "$TEMP_DIR"

    echo ""
    print_success "Digitize data export completed!"
    echo "Backup file: $OUTPUT_FILE"
    ls -lh "$CURRENT_DIR/$OUTPUT_FILE" 2>/dev/null || true
}

# Export Digitize (Podman) - wrapper for unified function
export_digitize_podman() {
    local APP_NAME="$1"
    local OUTPUT_FILE="$2"

    local DIGITIZE_CONTAINER=$(podman ps --filter "label=ai-services.io/application=${APP_NAME}" --format "{{.Names}}" | grep -Em1 "digitize.*(backend|server)")

    if [ -z "$DIGITIZE_CONTAINER" ]; then
        print_error "Digitize backend container not found for app: $APP_NAME"
        exit 1
    fi

    export_digitize "podman" "$APP_NAME" "$OUTPUT_FILE" "$DIGITIZE_CONTAINER" "/var/cache"
}


# Import OpenSearch using sidecar container approach (Podman)
import_opensearch_podman() {
    local APP_NAME="$1"
    local BACKUP_FILE="$2"

    local CONTAINER_NAME=$(podman ps --filter "label=ai-services.io/application=${APP_NAME}" --filter "name=opensearch" --format "{{.Names}}" | head -n 1)

    if [ -z "$CONTAINER_NAME" ]; then
        print_error "OpenSearch container not found for app: $APP_NAME"
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

    # Create restore script using helper function
    print_info "Creating restore script..."
    create_opensearch_restore_script "/tmp/restore.py"

    # Use helper function to manage sidecar lifecycle
    manage_podman_sidecar "restore" "$POD_ID" "/tmp/restore.py" "$BACKUP_FILE"

    echo ""
    print_success "OpenSearch import completed!"
}

# Unified Digitize Import (works for both Podman and OpenShift)
# Usage: import_digitize <runtime> <app-name> <backup-file> <resource-name> <dest-path> [namespace]
import_digitize() {
    local RUNTIME="$1"
    local APP_NAME="$2"
    local BACKUP_FILE="$3"
    local RESOURCE_NAME="$4"  # Container name or Pod name
    local DEST_PATH="$5"       # Path in container/pod (e.g., /var/cache or /data)
    local NAMESPACE="$6"       # Only for OpenShift

    print_header "Digitize Data Import ($RUNTIME)"
    print_operation_details "import" "$APP_NAME" "$BACKUP_FILE"

    local TEMP_DIR=$(mktemp -d)

    # Extract backup on host
    print_info "Extracting backup..."
    if ! extract_tar_archive "$BACKUP_FILE" "$TEMP_DIR"; then
        print_error "Failed to extract backup archive"
        rm -rf "$TEMP_DIR"
        return 1
    fi

    if [ ! -d "$TEMP_DIR/backup/cache" ]; then
        print_error "No cache directory found in backup"
        rm -rf "$TEMP_DIR"
        exit 1
    fi

    # Count files in backup
    local STATS=$(count_backup_files "$TEMP_DIR/backup/cache")
    local TOTAL_FILES=$(echo "$STATS" | cut -d'|' -f1)
    local TOTAL_SIZE=$(echo "$STATS" | cut -d'|' -f2)
    
    print_info "Backup contains: $TOTAL_FILES files ($TOTAL_SIZE)"
    
    if [ "$TOTAL_FILES" -eq "0" ]; then
        print_error "No files found in backup!"
        rm -rf "$TEMP_DIR"
        exit 1
    fi
    
    # Copy files to container/pod
    print_info "Restoring files to $RUNTIME resource..."
    if ! copy_to_runtime "$RUNTIME" "$TEMP_DIR/backup/cache/." "$RESOURCE_NAME:$DEST_PATH/" "$NAMESPACE"; then
        print_error "Failed to copy data to $RUNTIME resource"
        rm -rf "$TEMP_DIR"
        return 1
    fi
    
    rm -rf "$TEMP_DIR"
    
    echo "  ✓ Restored $TOTAL_FILES files ($TOTAL_SIZE) to $DEST_PATH"

    echo ""
    print_success "Digitize data import completed!"
    echo "📁 Restored $TOTAL_FILES files to $DEST_PATH"
    echo "🔄 Refresh your browser to see restored documents"
    echo ""
    print_info "Note: Documents require BOTH digitize files AND OpenSearch metadata"
    print_info "If documents don't appear, also restore OpenSearch data:"
    echo "  ./backup-restore.sh import opensearch $APP_NAME opensearch_backup.tar.gz"
}

# Import Digitize (Podman) - wrapper for unified function
import_digitize_podman() {
    local APP_NAME="$1"
    local BACKUP_FILE="$2"

    local DIGITIZE_CONTAINER=$(podman ps --filter "label=ai-services.io/application=${APP_NAME}" --format "{{.Names}}" | grep -Em1 "digitize.*(backend|server)")

    if [ -z "$DIGITIZE_CONTAINER" ]; then
        print_error "Digitize backend container not found for app: $APP_NAME"
        exit 1
    fi

    import_digitize "podman" "$APP_NAME" "$BACKUP_FILE" "$DIGITIZE_CONTAINER" "/var/cache"
}


# Export OpenSearch (OpenShift)
export_opensearch_openshift() {
    local APP_NAME="$1"
    local OUTPUT_FILE="$2"

    print_header "OpenSearch Data Export (OpenShift)"
    print_operation_details "export" "$APP_NAME" "$OUTPUT_FILE"

    # Create sidecar pod (sets NAMESPACE, OPENSEARCH_POD, SIDECAR_POD, OPENSEARCH_SERVICE)
    create_opensearch_sidecar_pod "$APP_NAME" "backup"
    
    print_info "Running backup..."
    if ! cat << 'EOFPYTHON' | oc exec -i $SIDECAR_POD -n $NAMESPACE -- python3
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
    then
        print_error "Failed to run backup script in sidecar pod"
        return 1
    fi

    print_info "Copying backup from sidecar pod..."
    # Create temporary directory for backup
    local TEMP_DIR=$(mktemp -d)
    local BACKUP_DIR="$TEMP_DIR/opensearch_backup"
    
    # Use oc cp to copy the entire backup directory (no tar needed in pod)
    if ! oc cp $NAMESPACE/$SIDECAR_POD:/tmp/opensearch_backup "$BACKUP_DIR"; then
        print_error "Failed to copy backup from sidecar pod"
        rm -rf "$TEMP_DIR"
        return 1
    fi
    
    # Create tar archive on host
    print_info "Creating backup archive..."
    cd "$TEMP_DIR"
    if ! tar -czf "$OLDPWD/$OUTPUT_FILE" opensearch_backup/; then
        print_error "Failed to create backup archive"
        cd "$OLDPWD"
        rm -rf "$TEMP_DIR"
        return 1
    fi
    cd "$OLDPWD"
    rm -rf "$TEMP_DIR"
    
    cleanup_resources "openshift" "$SIDECAR_POD" "$NAMESPACE"
    
    echo ""
    print_success "OpenSearch export completed!"
}

# Export Digitize (OpenShift) - wrapper for unified function
export_digitize_openshift() {
    local APP_NAME="$1"
    local OUTPUT_FILE="$2"

    # Find digitize pod and create helper pod (sets NAMESPACE, DIGITIZE_POD, HELPER_POD, PVC_NAME)
    find_digitize_pod_and_create_helper "$APP_NAME" "backup"
    
    # Use unified export function
    export_digitize "openshift" "$APP_NAME" "$OUTPUT_FILE" "$HELPER_POD" "/var/cache" "$NAMESPACE"
    
    cleanup_resources "openshift" "$HELPER_POD" "$NAMESPACE"
}

# Import OpenSearch (OpenShift)
import_opensearch_openshift() {
    local APP_NAME="$1"
    local BACKUP_FILE="$2"

    print_header "OpenSearch Data Import (OpenShift)"
    print_operation_details "import" "$APP_NAME" "$BACKUP_FILE"

    # Create sidecar pod (sets NAMESPACE, OPENSEARCH_POD, SIDECAR_POD, OPENSEARCH_SERVICE)
    create_opensearch_sidecar_pod "$APP_NAME" "restore"
    
    print_info "Extracting backup archive on host..."
    local TEMP_DIR=$(mktemp -d)
    if ! extract_tar_archive "$BACKUP_FILE" "$TEMP_DIR"; then
        print_error "Failed to extract backup archive"
        rm -rf "$TEMP_DIR"
        return 1
    fi
    
    print_info "Copying backup to sidecar pod..."
    # Use oc cp to copy the entire backup directory (no tar needed in pod)
    if ! oc cp "$TEMP_DIR/opensearch_backup" $NAMESPACE/$SIDECAR_POD:/tmp/opensearch_backup; then
        print_error "Failed to copy backup to sidecar pod"
        rm -rf "$TEMP_DIR"
        return 1
    fi
    rm -rf "$TEMP_DIR"
    
    print_info "Running restore..."
    cat << 'EOFPYTHON' | oc exec -i $SIDECAR_POD -n $NAMESPACE -- python3
import json, os, sys
from pathlib import Path
from opensearchpy import OpenSearch, helpers

class OpenSearchRestore:
    def __init__(self):
        self.backup_dir = Path("/tmp/opensearch_backup")
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
        if not self.backup_dir.exists():
            print(f"ERROR: Backup directory not found: {self.backup_dir}")
            sys.exit(1)
        
        indices = [f.stem.replace("_data", "") for f in self.backup_dir.glob("*_data.json")]
        print(f"Found {len(indices)} indices to restore")
        total_docs = 0
        for idx in indices:
            total_docs += self.restore_index(idx, self.backup_dir)
        print(f"✓ Restore completed: {len(indices)} indices, {total_docs} documents")

if __name__ == "__main__":
    try:
        restore = OpenSearchRestore()
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

# Import Digitize (OpenShift) - wrapper for unified function
import_digitize_openshift() {
    local APP_NAME="$1"
    local BACKUP_FILE="$2"

    # Find digitize pod and create helper pod (sets NAMESPACE, DIGITIZE_POD, HELPER_POD, PVC_NAME)
    find_digitize_pod_and_create_helper "$APP_NAME" "restore"
    
    # Use unified import function
    import_digitize "openshift" "$APP_NAME" "$BACKUP_FILE" "$HELPER_POD" "/var/cache" "$NAMESPACE"
    
    cleanup_resources "openshift" "$HELPER_POD" "$NAMESPACE"
    
    # Restart digitize pod to refresh UI
    print_info "Restarting digitize pod to refresh UI..."
    oc delete pod $DIGITIZE_POD -n $NAMESPACE --wait=false
    echo "  ✓ Digitize pod restart initiated"
}

# Main command dispatcher
main() {
    if [ $# -eq 0 ]; then
        show_usage
        exit 1
    fi
    
    # Parse the command first to check for sub-command
    local COMMAND="$1"
    
    # Validate sub-commands first before runtime (to ensure help and version work)
    case "$COMMAND" in
        help|--help|-h)
            show_usage
            exit 0
            ;;
        version|--version|-v)
            echo "Backup/Restore Tool v${VERSION}"
            exit 0
            ;;
        export|import)
            # Check and set OpenSearch password
            check_and_set_opensearch_password

            # Extract and validate runtime parameter
            RUNTIME=$(echo "$@" | grep -oP '(?<=--runtime\s)\S+' || true)
            validate_runtime "$RUNTIME"
            
            # Parse remaining parameters
            local TARGET="$2"
            local APP_NAME="$3"
            
            # Validate target and app name parameters
            validate_target "$TARGET"
            validate_app_name "$APP_NAME"
            
            # Execute command-specific operations
            case "$COMMAND" in
                export)
                    # Check if $4 is provided and is not a flag (doesn't start with --)
                    if [[ -n "$4" && "$4" != --* ]]; then
                        local OUTPUT_FILE="$4"
                    else
                        local OUTPUT_FILE="${TARGET}_backup_$(date +%Y%m%d_%H%M%S).tar.gz"
                    fi
                    start_operation "$RUNTIME" "$TARGET" "export" "$APP_NAME" "$OUTPUT_FILE"
                    ;;
                import)
                    local BACKUP_FILE="$4"
                    validate_backup_file "$BACKUP_FILE"
                    start_operation "$RUNTIME" "$TARGET" "import" "$APP_NAME" "$BACKUP_FILE"
                    ;;
            esac
            ;;
        *)
            print_error "Unknown command: $COMMAND"
            echo ""
            exit 1
            ;;
    esac
}

# Run main function
main "$@"

# Made with Bob
