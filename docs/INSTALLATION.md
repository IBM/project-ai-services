# AI Services Installation Guide

Complete installation instructions for AI Services across all supported platforms.

## Table of Contents

1. [Supported Platforms](#supported-platforms)
2. [Prerequisites](#prerequisites)
3. [Quick Installation](#quick-installation)
4. [Verified Installation with Cosign](#verified-installation-with-cosign)
5. [Container Image Verification](#container-image-verification)
6. [Additional Resources](#additional-resources)

---

## Supported Platforms

AI Services provides pre-built binaries for the following platforms:

| Platform | Architecture | Binary Name |
|----------|-------------|-------------|
| macOS | Intel (x86_64) | `ai-services-darwin-amd64` |
| macOS | Apple Silicon (ARM64) | `ai-services-darwin-arm64` |
| Linux | x86_64/AMD64 | `ai-services-linux-amd64` |
| Linux | ppc64le (Power) | `ai-services-linux-ppc64le` |

### Deployment Modes

- **Client-only mode** (macOS, Linux x86_64/AMD64): The CLI acts as a client that connects to a remote OpenShift cluster for application deployment and management.

- **Local + Remote mode** (Linux ppc64le/Power): Supports both local Podman-based deployments and remote OpenShift cluster connections, optimized for IBM Power Systems and IBM Spyre™.

---

## Prerequisites

### All Platforms

- **Internet connection** for downloading binaries
- **Terminal/Command line access**
- **Sudo/Administrator privileges** for system-wide installation

### Optional (Recommended)

- **Podman** or **Docker** for container-based deployments (Linux ppc64le only)
- **Cosign** for signature verification

---

## Quick Installation

Choose your platform and run the appropriate commands:

### macOS (Intel)

```bash
VERSION="v0.2.0"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-darwin-amd64"
chmod +x ai-services-darwin-amd64
sudo mv ai-services-darwin-amd64 /usr/local/bin/ai-services
ai-services version
```

### macOS (Apple Silicon)

```bash
VERSION="v0.2.0"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-darwin-arm64"
chmod +x ai-services-darwin-arm64
sudo mv ai-services-darwin-arm64 /usr/local/bin/ai-services
ai-services version
```

### Linux (x86_64/AMD64)

```bash
VERSION="v0.2.0"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-linux-amd64"
chmod +x ai-services-linux-amd64
sudo mv ai-services-linux-amd64 /usr/local/bin/ai-services
ai-services version
```

### Linux (ppc64le/Power)

**Optimized for IBM Power Systems and IBM Spyre™**

```bash
VERSION="v0.2.0"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-linux-ppc64le"
chmod +x ai-services-linux-ppc64le
sudo mv ai-services-linux-ppc64le /usr/local/bin/ai-services
ai-services version
```

---

## Verified Installation with Cosign

For enhanced security, verify binary signatures before installation.

### Step 1: Install Cosign

**macOS:**
```bash
brew install cosign
```

**Linux (x86_64/AMD64):**
```bash
curl -LO https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-amd64
chmod +x cosign-linux-amd64
sudo mv cosign-linux-amd64 /usr/local/bin/cosign
```

**Linux (ppc64le/Power):**
```bash
curl -LO https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-ppc64le
chmod +x cosign-linux-ppc64le
sudo mv cosign-linux-ppc64le /usr/local/bin/cosign
```

### Step 2: Download and Verify Binary

Replace `BINARY_NAME` with your platform's binary from the table above:

```bash
VERSION="v0.2.0"
BINARY_NAME="ai-services-darwin-amd64"  # Change based on your platform

# Download binary, signature, and public key
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/${BINARY_NAME}"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/${BINARY_NAME}.sig"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/cosign.pub"

# Verify signature
cosign verify-blob \
  --key cosign.pub \
  --signature ${BINARY_NAME}.sig \
  --insecure-ignore-tlog=true \
  ${BINARY_NAME}

# Install if verification succeeds
chmod +x ${BINARY_NAME}
sudo mv ${BINARY_NAME} /usr/local/bin/ai-services
ai-services version
```

---

## Container Image Verification

All AI Services container images are signed with Cosign for enhanced security and supply chain integrity.

### List Available Container Images

> **Note:** This command lists container images which are used in the project including third-party components. All images with the `icr.io/ai-services` registry prefix are built and maintained as part of this project. Only these images are signed and can be verified using the methods described in this document. Verification of third-party or custom images is outside the scope of this documentation.

To see all available container images for a specific application:

```bash
# List images for RAG application
ai-services application image list --runtime podman -t rag

# List images for other applications
ai-services application image list --runtime podman -t rag-cpu
```

### Verify Container Images

Ensure Cosign is installed (see [Verified Installation with Cosign](#verified-installation-with-cosign) section).

**Basic verification:**
```bash
VERSION="v0.2.0"
# Download public key if needed
curl -LO https://github.com/IBM/project-ai-services/releases/download/${VERSION}/cosign.pub

# Verify any image (replace with your image:tag)
cosign verify \
  --key cosign.pub \
  --insecure-ignore-tlog=true \
  icr.io/ai-services/tools:0.7
```

**Expected output on success:**
```
Verification for icr.io/ai-services/tools:0.7 --
The following checks were performed on each of these signatures:
  - The cosign claims were validated
  - The signatures were verified against the specified public key
```

---


## Additional Resources

- [Main README](../README.md) - Project overview and quick start
- [CONTRIBUTING.md](../CONTRIBUTING.md) - Contributing guidelines
- [GitHub Releases](https://github.com/IBM/project-ai-services/releases) - Download binaries
- [Cosign Documentation](https://docs.sigstore.dev/about/overview/) - Signature verification tool
