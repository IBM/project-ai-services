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
URL="https://raw.githubusercontent.com/BerriAI/litellm/refs/tags/v${VERSION}/pyproject.toml"
TOML_FILE="pyproject.toml"
BACKUP_FILE="pyproject.toml.bkp"
PATCH_FILE="pyproject-toml.patch"

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
    "certifi>=2026.10.19"
    "filelock>=4.2.1"
)

# ---------------------------------------------------------------------------
# 3. Package version bumps — replace the upstream minimum with our minimum
#    Format: "package_prefix|old_floor|new_floor"
#    e.g. "starlette>=|1.0.1|1.3.1" rewrites starlette>=1.0.1,... → starlette>=1.3.1,...
# ---------------------------------------------------------------------------
VERSION_BUMPS=(
    "aiohttp>=|3.14.2|3.14.4"
    "starlette>=|1.0.1|1.3.1"
    "PyJWT>=|2.13.0|2.14.0"
    "python-multipart>=|0.0.27|0.0.30"
    "tornado>=|6.5.8|6.5.9"
)

# ---------------------------------------------------------------------------

echo "=================================================="
echo "Step 1: Downloading pyproject.toml (v${VERSION})..."
echo "=================================================="

curl -fsSL "$URL" -o "$TOML_FILE"

if [ ! -s "$TOML_FILE" ]; then
    echo "Error: Failed to download $TOML_FILE."
    exit 1
fi

echo "Successfully downloaded $TOML_FILE."
cp "$TOML_FILE" "$BACKUP_FILE"
echo ""

echo "=================================================="
echo "Step 2: Adding ppc64le platform_machine markers..."
echo "=================================================="

for PKG in "${PPC64LE_EXCLUDE_PACKAGES[@]}"; do
    if grep -q "$PKG" "$TOML_FILE"; then
        echo "Patching: $PKG"
        sed -i -E '/'"$PKG"'/ {
            /;/ s/("[^"]+)(".*)/\1 and platform_machine != '\''ppc64le'\''\2/
            /;/! s/("[^"]+)(".*)/\1; platform_machine != '\''ppc64le'\''\2/
        }' "$TOML_FILE"
    else
        echo "Skipping (not found): $PKG"
    fi
done
echo ""

echo "=================================================="
echo "Step 3: Injecting extra packages into [proxy]..."
echo "=================================================="

for PKG in "${EXTRA_PACKAGES[@]}"; do
    # Only inject if not already present in the proxy block (idempotent).
    # Use awk to scope the check to proxy = [...] only, so a match in another
    # block (e.g. extra_proxy) doesn't suppress the injection.
    if awk '/^proxy = \[/{in_proxy=1} /^\]/{in_proxy=0} in_proxy && index($0, "'"$PKG"'"){found=1} END{exit !found}' "$TOML_FILE"; then
        echo "Already present in proxy block, skipping: $PKG"
    else
        echo "Injecting: $PKG"
        # Insert before the ] that closes the proxy = [...] extras list.
        # The proxy block starts with "proxy = [" and its closing ] is the
        # first bare ] after that marker — use awk with a state flag.
        awk -v pkg="    \"${PKG}\"," '
            /^proxy = \[/ { in_proxy = 1 }
            in_proxy && /^\]/ {
                print pkg
                in_proxy = 0
            }
            { print }
        ' "$TOML_FILE" > "$TOML_FILE.tmp"
        mv "$TOML_FILE.tmp" "$TOML_FILE"
    fi
done
echo ""

echo "=================================================="
echo "Step 4: Applying package version bumps..."
echo "=================================================="

for BUMP in "${VERSION_BUMPS[@]}"; do
    PKG_PREFIX="${BUMP%%|*}"
    REST="${BUMP#*|}"
    OLD_VER="${REST%%|*}"
    NEW_VER="${REST##*|}"
    if grep -q "${PKG_PREFIX}${OLD_VER}" "$TOML_FILE"; then
        echo "Bumping: ${PKG_PREFIX}${OLD_VER} → ${PKG_PREFIX}${NEW_VER}"
        sed -i "s|${PKG_PREFIX}${OLD_VER}|${PKG_PREFIX}${NEW_VER}|g" "$TOML_FILE"
    else
        echo "WARNING: '${PKG_PREFIX}${OLD_VER}' not found — upstream may have already changed this version."
    fi
done
echo ""

echo "=================================================="
echo "Step 5: Generating patch file..."
echo "=================================================="

diff -u --label "a/pyproject.toml" "$BACKUP_FILE" \
        --label "b/pyproject.toml" "$TOML_FILE" \
    > "$PATCH_FILE" || true
echo "Patch file generated: ${PATCH_FILE}"
echo ""
rm -f "$TOML_FILE" "$BACKUP_FILE"

echo "=================================================="
echo "Process complete! Generated patch:"
echo "=================================================="
cat "$PATCH_FILE"
