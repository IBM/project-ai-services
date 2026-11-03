#!/bin/bash
set -euo pipefail

# Accept version as first argument, or exit with error
if [ -z "${1:-}" ]; then
    echo "Error: Version argument required"
    echo "Usage: $0 <version>"
    echo "Example: $0 1.84.0"
    exit 1
fi

VERSION="$1"
BASE_URL="https://raw.githubusercontent.com/BerriAI/litellm/refs/tags/v${VERSION}"
PATCH_FILE="pyproject-toml.patch"

TOML_FILES=(
    "pyproject.toml"
    "packaging/litellm-core/pyproject.toml"
)

# ---------------------------------------------------------------------------
# 1. Packages that are unavailable on ppc64le — add platform_machine marker
# ---------------------------------------------------------------------------
PPC64LE_EXCLUDE_PACKAGES=(
    "litellm-enterprise=="
    "pyroscope-io>="
)

# ---------------------------------------------------------------------------
# 2. Extra packages to inject at the end of the [proxy] extras list
#    Format: one package specifier per entry, e.g. "prisma>=0.11.0,<1.0"
# ---------------------------------------------------------------------------
EXTRA_PACKAGES=(
    "prisma>=0.11.0,<1.0"
)

# ---------------------------------------------------------------------------
# 3. Package version bumps — replace upstream specifier with our specifier
#    Format: "old_specifier|new_specifier"
#    e.g. "filelock>=3.16.1,<4.0|filelock>=4.2.1"
# ---------------------------------------------------------------------------
VERSION_BUMPS=(
    "filelock>=3.16.1,<4.0|filelock>=4.2.1"
    'requires-python = ">=3.10|requires-python = ">=3.11'
)

# ---------------------------------------------------------------------------

echo "=================================================="
echo "Step 1: Downloading pyproject.toml files (v${VERSION})..."
echo "=================================================="

for REL_PATH in "${TOML_FILES[@]}"; do
    mkdir -p "$(dirname "$REL_PATH")"
    URL="${BASE_URL}/${REL_PATH}"
    curl --connect-timeout 10 --max-time 30 -fsSL "$URL" -o "$REL_PATH"

    if [ ! -s "$REL_PATH" ]; then
        echo "Error: Failed to download $REL_PATH."
        exit 1
    fi
    echo "Successfully downloaded $REL_PATH."
    cp "$REL_PATH" "${REL_PATH}.bkp"
done
echo ""

echo "=================================================="
echo "Step 2: Adding ppc64le platform_machine markers..."
echo "=================================================="

if [ "${#PPC64LE_EXCLUDE_PACKAGES[@]}" -gt 0 ]; then
for REL_PATH in "${TOML_FILES[@]}"; do
    for PKG in "${PPC64LE_EXCLUDE_PACKAGES[@]}"; do
        if grep -q "$PKG" "$REL_PATH"; then
            echo "Patching in $REL_PATH: $PKG"
            sed -i -E '/'"$PKG"'/ {
                /;/ s/("[^"]+)(".*)/\1 and platform_machine != '\''ppc64le'\''\2/
                /;/! s/("[^"]+)(".*)/\1; platform_machine != '\''ppc64le'\''\2/
            }' "$REL_PATH"
        fi
    done
done
fi
echo ""

echo "=================================================="
echo "Step 3: Injecting extra packages into [proxy]..."
echo "=================================================="

if [ "${#EXTRA_PACKAGES[@]}" -gt 0 ]; then
for REL_PATH in "${TOML_FILES[@]}"; do
    if grep -q '^proxy = \[' "$REL_PATH"; then
        for PKG in "${EXTRA_PACKAGES[@]}"; do
            if awk '/^proxy = \[/{in_proxy=1} /^\]/{in_proxy=0} in_proxy && index($0, "'"$PKG"'"){found=1} END{exit !found}' "$REL_PATH"; then
                echo "Already present in $REL_PATH proxy block, skipping: $PKG"
            else
                echo "Injecting into $REL_PATH: $PKG"
                awk -v pkg="    \"${PKG}\"," '
                    /^proxy = \[/ { in_proxy = 1 }
                    in_proxy && /^\]/ {
                        print pkg
                        in_proxy = 0
                    }
                    { print }
                ' "$REL_PATH" > "$REL_PATH.tmp"
                mv "$REL_PATH.tmp" "$REL_PATH"
            fi
        done
    fi
done
fi
echo ""

echo "=================================================="
echo "Step 4: Applying package version bumps..."
echo "=================================================="

if [ "${#VERSION_BUMPS[@]}" -gt 0 ]; then
for REL_PATH in "${TOML_FILES[@]}"; do
    for BUMP in "${VERSION_BUMPS[@]}"; do
        OLD_STR="${BUMP%%|*}"
        NEW_STR="${BUMP#*|}"
        if grep -q "${OLD_STR}" "$REL_PATH"; then
            echo "Bumping in $REL_PATH: ${OLD_STR} → ${NEW_STR}"
            sed -i "s|${OLD_STR}|${NEW_STR}|g" "$REL_PATH"
        fi
    done
done
fi
echo ""

echo "=================================================="
echo "Step 5: Generating patch file..."
echo "=================================================="

> "$PATCH_FILE"
for REL_PATH in "${TOML_FILES[@]}"; do
    diff -u --label "a/${REL_PATH}" "${REL_PATH}.bkp" \
            --label "b/${REL_PATH}" "${REL_PATH}" \
        >> "$PATCH_FILE" || true
    rm -f "${REL_PATH}" "${REL_PATH}.bkp"
done
# Clean up empty packaging dir if created
rm -rf packaging

echo "Patch file generated: ${PATCH_FILE}"
echo ""

echo "=================================================="
echo "Process complete! Generated patch:"
echo "=================================================="
cat "$PATCH_FILE"
