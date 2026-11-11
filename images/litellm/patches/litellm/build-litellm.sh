#!/usr/bin/env bash
# Downloads and patches litellm, then compiles it and all its dependencies into
# wheels under /wheels. Independent of the prisma engines, so it runs in its own
# build stage. Expects VERSION and LITELLM_SHA256 in the environment.
#
# To test a custom branch/commit/tag instead of the upstream release, set
# LITELLM_REF (and optionally LITELLM_REPO, default BerriAI/litellm). In that
# case the checksum is skipped and VERSION must match the version declared in
# the pyproject.toml of that ref, since the runtime stage installs
# litellm==${VERSION} from the built wheels.
set -euo pipefail

cd /build

# Compiler variables for ppc64le
export GRPC_PYTHON_BUILD_SYSTEM_OPENSSL=1
export GRPC_PYTHON_BUILD_SYSTEM_ZLIB=1

PYTHON_BIN="python${PYTHON_VERSION}"
export UV_PYTHON="${PYTHON_VERSION}"

# 1. Install pre-requisites
"${PYTHON_BIN}" -m pip install -r pre-requirements.txt \
    --extra-index-url=https://wheels.developerfirst.ibm.com/ppc64le/linux/ \
    --prefer-binary

# 2. Download litellm & patch it
LITELLM_REPO="${LITELLM_REPO:-BerriAI/litellm}"
LITELLM_REF="${LITELLM_REF:-}"

if [ -n "${LITELLM_REF}" ]; then
    echo "Using custom litellm source: ${LITELLM_REPO}@${LITELLM_REF} (checksum verification skipped)"
    # /archive/<ref>.tar.gz resolves branches, tags and commit SHAs alike
    curl -fsSL "https://github.com/${LITELLM_REPO}/archive/${LITELLM_REF}.tar.gz" \
        -o litellm.tar.gz
else
    curl -fsSL "https://github.com/${LITELLM_REPO}/archive/refs/tags/v${VERSION}.tar.gz" \
        -o litellm.tar.gz
    echo "${LITELLM_SHA256}  litellm.tar.gz" | sha256sum -c -
fi

# Extract into a fixed directory name; the archive's top-level directory name
# differs for branches (e.g. litellm-feature-x) and the Containerfile expects
# litellm-${VERSION}.
mkdir "litellm-${VERSION}"
tar -xzf litellm.tar.gz -C "litellm-${VERSION}" --strip-components=1
rm litellm.tar.gz

cd "litellm-${VERSION}"
patch -p1 < ../pyproject-toml.patch
patch -p1 < ../watsonx-passthrough.patch

uv lock

# 3. Export dependencies to a requirements.txt file
uv export --no-hashes --no-dev \
    --extra proxy \
    --extra semantic-router \
    -o requirements.txt
cat requirements.txt

# 4. Compile all dependencies into .whl files
"${PYTHON_BIN}" -m pip wheel -r requirements.txt \
    -w /wheels \
    --extra-index-url=https://wheels.developerfirst.ibm.com/ppc64le/linux/ \
    --prefer-binary
uv build --wheel --out-dir /wheels
