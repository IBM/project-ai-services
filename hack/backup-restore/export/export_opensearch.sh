#!/bin/bash
# Export OpenSearch Vector Database
# Usage: ./export_opensearch.sh [app-name] [output-file]

set -e

APP_NAME="${1:-rag-dev}"
OUTPUT_FILE="${2:-opensearch_backup_$(date +%Y%m%d_%H%M%S).tar.gz}"
CONTAINER_NAME=$(podman ps | grep opensearch | awk '{print $1}')

if [ -z "$CONTAINER_NAME" ]; then
    echo "Error: OpenSearch container not found"
    exit 1
fi

echo "============================================================"
echo "OpenSearch Export Tool"
echo "============================================================"
echo "Container: $CONTAINER_NAME"
echo "App name: $APP_NAME"
echo "Output: $OUTPUT_FILE"
echo ""

# Create temporary Python script in container
echo " Creating backup script..."
podman exec $CONTAINER_NAME bash -c 'cat > /tmp/backup.py << '\''EOFPYTHON'\''
#!/usr/bin/env python3
import json, os, sys, tarfile, tempfile
from datetime import datetime
from pathlib import Path
from opensearchpy import OpenSearch

class BackupExporter:
    def __init__(self, app_name, output_file):
        self.app_name = app_name
        self.output_file = output_file
        self.client = OpenSearch(
            hosts=[{"host": "localhost", "port": 9200}],
            http_compress=True, use_ssl=True,
            http_auth=("admin", os.getenv("OPENSEARCH_PASSWORD", "AiServices@12345")),
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
        print(f"✓ Connected to OpenSearch {info['\''version'\'']['\''number'\'']}")
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

# Run backup
echo "Running OpenSearch backup..."
podman exec -e OPENSEARCH_PASSWORD=AiServices@12345 \
    $CONTAINER_NAME python3 /tmp/backup.py "$APP_NAME" /tmp/backup.tar.gz

# Copy OpenSearch backup to host
echo "Copying backup to host..."
podman cp $CONTAINER_NAME:/tmp/backup.tar.gz "./$OUTPUT_FILE"

# Cleanup container
podman exec $CONTAINER_NAME rm -f /tmp/backup.py /tmp/backup.tar.gz

echo ""
echo "============================================================"
echo "✅ OpenSearch export completed successfully!"
echo "============================================================"
echo "Backup file: $OUTPUT_FILE"
ls -lh "$OUTPUT_FILE"

# Made with Bob
