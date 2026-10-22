#!/usr/bin/env bash
set -euo pipefail

# Load arch env written by step 1 (NODE_ARCH, SKIP_CHECKSUM, PROTOC_ARCH)
. /tmp/arch_env

WORKDIR=/build

# 1. Install protoc from GitHub releases
curl -fsSL "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-${PROTOC_ARCH}.zip" \
    -o /tmp/protoc.zip
if [ "${SKIP_CHECKSUM}" != "1" ]; then
    echo "${PROTOC_SHA256}  /tmp/protoc.zip" | sha256sum -c -
fi
unzip -q /tmp/protoc.zip -d /tmp/protoc
install -m 0755 /tmp/protoc/bin/protoc /usr/local/bin/protoc
cp -r /tmp/protoc/include/* /usr/local/include/
rm -rf /tmp/protoc /tmp/protoc.zip

# 2. Patch & build Prisma engines
cd "${WORKDIR}"
git clone --depth 1 --branch 5.4.2 https://github.com/prisma/prisma-engines.git
curl -sSL https://static.crates.io/crates/ring/ring-0.16.20.crate | tar -xvzf -

cd "${WORKDIR}/ring-0.16.20"
patch -p1 < ../prisma/ring.patch

cd "${WORKDIR}/prisma-engines"
source "$HOME/.cargo/env"
rustup install 1.94.1
rustup default 1.94.1
export RUSTFLAGS="--cap-lints allow -A ambiguous_glob_imports"
patch -p1 < ../prisma/prisma.patch
cargo update -p time@0.3.25
cargo build --release --bin query-engine
cargo build --release --bin schema-engine
cargo build --release -p query-engine-node-api

# 3. Install Node.js and bake Prisma CLI into /build/prisma
cd "${WORKDIR}"
curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.gz" \
    -o /tmp/node.tar.gz
if [ "${SKIP_CHECKSUM}" != "1" ]; then
    echo "${NODE_SHA256}  /tmp/node.tar.gz" | sha256sum -c -
fi
tar -xz -C /usr/local --strip-components=1 -f /tmp/node.tar.gz
rm /tmp/node.tar.gz

python3.12 -m pip install --no-cache-dir --no-index --find-links=/wheels prisma

mkdir -p /build/prisma
HOME=/build/prisma \
XDG_CACHE_HOME=/build/prisma/.cache \
PRISMA_BINARY_CACHE_DIR=/build/prisma/binaries \
PRISMA_QUERY_ENGINE_BINARY=/build/prisma-engines/target/release/query-engine \
PRISMA_SCHEMA_ENGINE_BINARY=/build/prisma-engines/target/release/schema-engine \
PRISMA_QUERY_ENGINE_LIBRARY=/build/prisma-engines/target/release/libquery_engine.so \
PRISMA_ENGINES_CHECKSUM_IGNORE_WARNING=true \
prisma version
