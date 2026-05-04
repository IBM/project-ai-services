#!/bin/bash
# Script to copy database initialization scripts to the deployment location
# This should be run during application deployment/setup

set -e

# Get the application name from argument or use default
APP_NAME="${1:-rag-app}"

# Get runtime environment (podman or openshift) from environment variable or default to podman
RUNTIME="${RUNTIME:-podman}"

# Construct scripts directory path based on runtime
if [ "$RUNTIME" = "openshift" ]; then
    # OpenShift uses PVC mounts, typically under /mnt or /data
    SCRIPTS_DIR="${SCRIPTS_BASE_PATH:-/mnt/db-scripts}"
else
    # Podman uses host path mounts
    SCRIPTS_DIR="/var/lib/ai-services/applications/${APP_NAME}/db-scripts"
fi

echo "Setting up database initialization scripts for application: ${APP_NAME}"
echo "Runtime: ${RUNTIME}"

# Create the scripts directory if it doesn't exist
mkdir -p "${SCRIPTS_DIR}"

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Copy the initialization scripts
echo "Copying init_db.sh..."
cp "${SCRIPT_DIR}/init_db.sh" "${SCRIPTS_DIR}/"
chmod +x "${SCRIPTS_DIR}/init_db.sh"

echo "Copying init_schema.sql..."
cp "${SCRIPT_DIR}/init_schema.sql" "${SCRIPTS_DIR}/"

echo "✅ Database initialization scripts setup complete!"
echo "Scripts location: ${SCRIPTS_DIR}"
ls -la "${SCRIPTS_DIR}"
