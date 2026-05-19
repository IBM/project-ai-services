#!/bin/bash

# Accept version as first argument, or exit with error
if [ -z "$1" ]; then
    echo "Error: Version argument required"
    echo "Usage: $0 <version>"
    echo "Example: $0 1.84.0"
    exit 1
fi

VERSION="$1"
URL="https://raw.githubusercontent.com/BerriAI/litellm/refs/tags/v${VERSION}/pyproject.toml"
TOML_FILE="pyproject.toml"
BACKUP_FILE="pyproject.toml.bkp"
PATCH_FILE="pyproject-toml.patch"

echo "=================================================="
echo "Step 1: Downloading pyproject.toml (v${VERSION})..."
echo "=================================================="

# Check for curl or wget and download the file
if command -v curl >/dev/null 2>&1; then
    curl -sSL "$URL" -o "$TOML_FILE"
elif command -v wget >/dev/null 2>&1; then
    wget -q "$URL" -O "$TOML_FILE"
else
    echo "Error: Neither curl nor wget found."
    exit 1
fi

# Safeguard: Verify the file exists and is not empty
if [ ! -f "$TOML_FILE" ] || [ ! -s "$TOML_FILE" ]; then
    echo "Error: Failed to download $TOML_FILE."
    exit 1
fi

echo "Successfully downloaded $TOML_FILE."

# CRITICAL: Create the pristine backup copy right after download for the diff
cp "$TOML_FILE" "$BACKUP_FILE"
echo "Pristine backup created at: $BACKUP_FILE"
echo ""

echo "=================================================="
echo "Step 2: Applying ppc64le architecture patches..."
echo "=================================================="

# Define the array of packages to target
PACKAGES=(
    "litellm-enterprise=="
    "pyroscope-io=="
)

# Loop through each package and apply the inline sed modification
for PKG in "${PACKAGES[@]}"; do
    if grep -q "$PKG" "$TOML_FILE"; then
        echo "Match found! Patching constraint for: $PKG"
        
        sed -i -E '/'"$PKG"'/ {
            /;/ s/("[^"]+)(".*)/\1 and platform_machine != '\''ppc64le'\''\2/
            /;/! s/("[^"]+)(".*)/\1; platform_machine != '\''ppc64le'\''\2/
        }' "$TOML_FILE"
    else
        echo "Skipping: $PKG (Not found in this version)"
    fi
done
echo ""

echo "=================================================="
echo "Step 3: Generating Patch File..."
echo "=================================================="

# Generate the patch file
# '|| true' keeps the script alive since diff exits with 1 when changes exist
diff -u "${BACKUP_FILE}" "${TOML_FILE}" > "${PATCH_FILE}" || true

echo "Patch file successfully generated: ${PATCH_FILE}"
echo ""
rm -rf ${TOML_FILE}*

echo "=================================================="
echo "Process Complete! Reviewing Generated Patch:"
echo "=================================================="
cat "${PATCH_FILE}"
