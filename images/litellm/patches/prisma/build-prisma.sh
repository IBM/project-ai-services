#!/usr/bin/env bash
# Builds the Prisma engines (query-engine, schema-engine, libquery_engine).
# Independent of the litellm wheels, so it runs in its own build stage.
set -euo pipefail

# Load arch env written by the base stage (PROTOC_ARCH, SKIP_CHECKSUM)
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

# 2. Patch & build Prisma engines (rust toolchain comes from the base stage)
cd "${WORKDIR}"
git clone --depth 1 --branch 5.4.2 https://github.com/prisma/prisma-engines.git
curl -sSL https://static.crates.io/crates/ring/ring-0.16.20.crate | tar -xvzf -

cd "${WORKDIR}/ring-0.16.20"
patch -p1 < ../prisma/ring.patch

cd "${WORKDIR}/prisma-engines"
export RUSTFLAGS="--cap-lints allow -A ambiguous_glob_imports"
patch -p1 < ../prisma/prisma.patch
cargo update -p time@0.3.25
cargo build --release --bin query-engine
cargo build --release --bin schema-engine
cargo build --release -p query-engine-node-api
