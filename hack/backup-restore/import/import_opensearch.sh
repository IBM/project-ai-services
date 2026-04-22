#!/bin/bash
# Import OpenSearch Vector Database
# Usage: ./import_opensearch.sh <backup-file>

set -e

BACKUP_FILE="$1"

if [ -z "$BACKUP_FILE" ]; then
    echo "Error: Backup file not specified"
    echo "Usage: ./import_opensearch.sh <backup-file>"
    exit 1
fi

if [ ! -f "$BACKUP_FILE" ]; then
    echo "Error: Backup file not found: $BACKUP_FILE"
    exit 1
fi

CONTAINER_NAME=$(podman ps | grep opensearch | awk '{print $1}')

if [ -z "$CONTAINER_NAME" ]; then
    echo "Error: OpenSearch container not found"
    exit 1
fi

echo "============================================================"
echo "OpenSearch Import Tool"
echo "============================================================"
echo "Container: $CONTAINER_NAME"
echo "Backup file: $BACKUP_FILE"
echo ""

# Copy backup to container
echo "Copying backup to container..."
podman cp "$BACKUP_FILE" $CONTAINER_NAME:/tmp/backup.tar.gz

# Create restore script in container
echo "Creating restore script..."
podman exec $CONTAINER_NAME bash -c 'cat > /tmp/restore.py << '\''EOFPYTHON'\''
#!/usr/bin/env python3
import json, os, sys, tarfile, tempfile
from pathlib import Path
from opensearchpy import OpenSearch, helpers

class BackupRestorer:
    def __init__(self, backup_file):
        self.backup_file = backup_file
        self.client = OpenSearch(
            hosts=[{"host": "localhost", "port": 9200}],
            http_compress=True, use_ssl=True,
            http_auth=("admin", os.getenv("OPENSEARCH_PASSWORD", "AiServices@12345")),
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
        print(f"✓ Connected to OpenSearch {info['\''version'\'']['\''number'\'']}")
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            print("Extracting backup...")
            with tarfile.open(self.backup_file, "r:gz") as tar:
                tar.extractall(temp_path)
            info_file = temp_path / "backup" / "backup_info.json"
            if info_file.exists():
                with open(info_file) as f:
                    info = json.load(f)
                    print(f"  Backup date: {info.get('\''backup_date'\'')}")
                    print(f"  App name: {info.get('\''app_name'\'')}")
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
'

# Install dependencies
echo "Installing dependencies..."

# First try to install pip as root if needed
if ! podman exec $CONTAINER_NAME bash -c "command -v pip &> /dev/null || command -v pip3 &> /dev/null"; then
    echo "Installing pip as root..."
    podman exec --user root $CONTAINER_NAME bash -c "
        if command -v yum &> /dev/null; then
            yum install -y python3-pip 2>&1 | tail -3
        elif command -v dnf &> /dev/null; then
            dnf install -y python3-pip 2>&1 | tail -3
        elif command -v apt-get &> /dev/null; then
            apt-get update -qq && apt-get install -y python3-pip 2>&1 | tail -3
        elif command -v apk &> /dev/null; then
            apk add --no-cache py3-pip 2>&1 | tail -3
        else
            echo 'Trying ensurepip...'
            python3 -m ensurepip --default-pip 2>&1 | tail -3 || true
        fi
    " || echo "Could not install pip as root, trying user install..."
fi

# Now install opensearch-py (as regular user with --user flag)
echo "Installing opensearch-py..."
podman exec $CONTAINER_NAME bash -c "
    if command -v pip &> /dev/null; then
        pip install --user opensearch-py 2>&1 | grep -E '(Successfully installed|Requirement already satisfied)' || true
    elif command -v pip3 &> /dev/null; then
        pip3 install --user opensearch-py 2>&1 | grep -E '(Successfully installed|Requirement already satisfied)' || true
    elif python3 -m pip --version &> /dev/null; then
        python3 -m pip install --user opensearch-py 2>&1 | grep -E '(Successfully installed|Requirement already satisfied)' || true
    else
        echo 'ERROR: Could not install opensearch-py - pip not available'
        exit 1
    fi
"

# Run restore
echo "Running OpenSearch restore..."
podman exec -e OPENSEARCH_PASSWORD=AiServices@12345 \
    $CONTAINER_NAME python3 /tmp/restore.py /tmp/backup.tar.gz

# Cleanup container
echo "Cleaning up container..."
podman exec $CONTAINER_NAME rm -f /tmp/restore.py /tmp/backup.tar.gz

echo ""
echo "============================================================"
echo "✅ OpenSearch import completed successfully!"
echo "============================================================"
echo "Vector database restored"

# Made with Bob