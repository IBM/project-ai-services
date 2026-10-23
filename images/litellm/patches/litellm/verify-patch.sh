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

mkdir -p \
    "${VERIFY_TMP}/orig/litellm/proxy/pass_through_endpoints" \
    "${VERIFY_TMP}/patched/litellm/proxy/pass_through_endpoints"

echo "==> Fetching upstream sources for v${VERSION}..."
curl -fsSL "${BASE_URL}/pyproject.toml" \
    -o "${VERIFY_TMP}/orig/pyproject.toml"
curl -fsSL "${BASE_URL}/litellm/proxy/pass_through_endpoints/pass_through_endpoints.py" \
    -o "${VERIFY_TMP}/orig/litellm/proxy/pass_through_endpoints/pass_through_endpoints.py"
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
diff -u "${VERIFY_TMP}/orig/pyproject.toml" \
        "${VERIFY_TMP}/patched/pyproject.toml" || true
diff -u \
    "${VERIFY_TMP}/orig/litellm/proxy/pass_through_endpoints/pass_through_endpoints.py" \
    "${VERIFY_TMP}/patched/litellm/proxy/pass_through_endpoints/pass_through_endpoints.py" \
    || true
