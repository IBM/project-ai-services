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
    "litellm/proxy/_types.py"
    "litellm/proxy/pass_through_endpoints/llm_passthrough_endpoints.py"
    "litellm/proxy/pass_through_endpoints/llm_provider_handlers/watsonx_passthrough_handler.py"
)

NEW_FILES=(
    "litellm/proxy/pass_through_endpoints/llm_provider_handlers/watsonx_passthrough_handler.py"
)

is_new_file() {
    local path="$1"
    for new in "${NEW_FILES[@]}"; do
        [[ "$new" == "$path" ]] && return 0
    done
    return 1
}

echo "==> Fetching upstream sources for v${VERSION}..."
for REL_PATH in "${FILES_TO_VERIFY[@]}"; do
    if is_new_file "$REL_PATH"; then
        echo "  [SKIP fetch] $REL_PATH (new file introduced by patch)"
        continue
    fi
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
    if is_new_file "$REL_PATH"; then
        echo "  [NEW] $REL_PATH"
        cat "${VERIFY_TMP}/patched/${REL_PATH}" || true
        continue
    fi
    diff -u "${VERIFY_TMP}/orig/${REL_PATH}" \
            "${VERIFY_TMP}/patched/${REL_PATH}" || true
done
