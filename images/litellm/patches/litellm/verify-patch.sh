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

PATCH_FILES=("${SCRIPT_DIR}"/*.patch)
FILES_TO_VERIFY=()
NEW_FILES=()

is_new=0
while IFS= read -r line; do
    if [[ "$line" =~ ^---[[:space:]]+/dev/null ]]; then
        is_new=1
    elif [[ "$line" =~ ^\+\+\+[[:space:]]+(b/)?(.+) ]]; then
        file="${BASH_REMATCH[2]}"
        file="${file%%[[:space:]]*}"
        if [[ "$file" != "/dev/null" ]]; then
            if ! [[ " ${FILES_TO_VERIFY[*]:-} " =~ " ${file} " ]]; then
                FILES_TO_VERIFY+=("$file")
            fi
            if [ "$is_new" -eq 1 ]; then
                NEW_FILES+=("$file")
            fi
        fi
        is_new=0
    else
        is_new=0
    fi
done < <(cat "${PATCH_FILES[@]}")

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
for PATCH_PATH in "${PATCH_FILES[@]}"; do
    PATCH_NAME="$(basename "$PATCH_PATH")"
    patch -p1 -d "${VERIFY_TMP}/patched" < "$PATCH_PATH" \
        && echo "  [OK] $PATCH_NAME" \
        || echo "  [FAIL] $PATCH_NAME"
done
