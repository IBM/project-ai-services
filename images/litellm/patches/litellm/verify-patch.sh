#!/bin/bash
set -euo pipefail

if [ -z "${1:-}" ]; then
    echo "Error: Version argument required"
    echo "Usage: $0 <version>"
    echo "Example: $0 1.106.2"
    exit 1
fi

VERSION="$1"
BASE_URL="https://raw.githubusercontent.com/BerriAI/litellm/refs/tags/v${VERSION}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERIFY_TMP="$(mktemp -d)"
trap 'rm -rf "$VERIFY_TMP"' EXIT

FILES_TO_VERIFY=(
    "pyproject.toml"
    "packaging/litellm-core/pyproject.toml"
    "litellm/proxy/pass_through_endpoints/pass_through_endpoints.py"
)

echo "==> Fetching upstream sources for v${VERSION}..."
for REL_PATH in "${FILES_TO_VERIFY[@]}"; do
    mkdir -p "${VERIFY_TMP}/orig/$(dirname "$REL_PATH")"
    curl --connect-timeout 10 --max-time 30 -fsSL "${BASE_URL}/${REL_PATH}" \
        -o "${VERIFY_TMP}/orig/${REL_PATH}"
done

cp -r "${VERIFY_TMP}/orig/." "${VERIFY_TMP}/patched/"

echo ""
echo "==> Applying patches..."
patch -p1 -d "${VERIFY_TMP}/patched" < "${SCRIPT_DIR}/pyproject-toml.patch" \
    && echo "  [OK] pyproject-toml.patch" \
    || echo "  [FAIL] pyproject-toml.patch"

patch -p1 -d "${VERIFY_TMP}/patched" < "${SCRIPT_DIR}/watsonx-passthrough.patch" \
    && echo "  [OK] watsonx-passthrough.patch" \
    || echo "  [FAIL] watsonx-passthrough.patch"

echo ""
echo "==> Diff of patched files:"
for REL_PATH in "${FILES_TO_VERIFY[@]}"; do
    diff -u "${VERIFY_TMP}/orig/${REL_PATH}" \
            "${VERIFY_TMP}/patched/${REL_PATH}" || true
done
