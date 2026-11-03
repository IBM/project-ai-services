#!/usr/bin/env bash
# Installs Node.js and bakes the Prisma CLI into /build/prisma.
# Needs the litellm wheels (/wheels) and the compiled Prisma engines.
# Avoids nodeenv downloading Node at runtime, eliminating CVEs from bundled npm packages.
set -euo pipefail

# Load arch env written by the base stage (NODE_ARCH, SKIP_CHECKSUM)
. /tmp/arch_env

curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.gz" \
    -o /tmp/node.tar.gz
if [ "${SKIP_CHECKSUM}" != "1" ]; then
    echo "${NODE_SHA256}  /tmp/node.tar.gz" | sha256sum -c -
fi
tar -xz -C /usr/local --strip-components=1 -f /tmp/node.tar.gz
rm /tmp/node.tar.gz

python${PYTHON_VERSION} -m pip install --no-cache-dir --no-index --find-links=/wheels prisma

mkdir -p /build/prisma
HOME=/build/prisma \
XDG_CACHE_HOME=/build/prisma/.cache \
PRISMA_BINARY_CACHE_DIR=/build/prisma/binaries \
PRISMA_QUERY_ENGINE_BINARY=/build/prisma-engines/target/release/query-engine \
PRISMA_SCHEMA_ENGINE_BINARY=/build/prisma-engines/target/release/schema-engine \
PRISMA_QUERY_ENGINE_LIBRARY=/build/prisma-engines/target/release/libquery_engine.so \
PRISMA_ENGINES_CHECKSUM_IGNORE_WARNING=true \
prisma version
