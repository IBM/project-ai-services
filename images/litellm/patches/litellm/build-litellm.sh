#!/usr/bin/env bash
# Downloads and patches litellm, then compiles it and all its dependencies into
# wheels under /wheels. Independent of the prisma engines, so it runs in its own
# build stage. Expects VERSION and LITELLM_SHA256 in the environment.
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
curl -fsSL "https://github.com/BerriAI/litellm/archive/refs/tags/v${VERSION}.tar.gz" \
    -o "v${VERSION}.tar.gz"
echo "${LITELLM_SHA256}  v${VERSION}.tar.gz" | sha256sum -c -
tar -xzf "v${VERSION}.tar.gz"
rm "v${VERSION}.tar.gz"

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
