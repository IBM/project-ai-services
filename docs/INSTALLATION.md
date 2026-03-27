# AI Services Installation Guide

Complete installation instructions for AI Services across all supported platforms.

## Table of Contents

1. [Supported Platforms](#supported-platforms)
2. [Prerequisites](#prerequisites)
3. [Installation by Platform](#installation-by-platform)
   - [macOS (Intel)](#macos-intel)
   - [macOS (Apple Silicon)](#macos-apple-silicon)
   - [Linux (x86_64/AMD64)](#linux-x86_64amd64)
   - [Linux (ppc64le/Power)](#linux-ppc64lepower)

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

## Installation by Platform

### macOS (Intel)

#### Quick Install

```bash
# Set version (check latest at https://github.com/IBM/project-ai-services/releases)
VERSION="v0.0.2"

# Download binary
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-darwin-amd64"

# Make executable
chmod +x ai-services-darwin-amd64

# Move to system path
sudo mv ai-services-darwin-amd64 /usr/local/bin/ai-services

# Verify installation
ai-services version
```

#### Verified Install (with Cosign)

```bash
VERSION="v0.0.2"

# Download binary, signature, and public key
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-darwin-amd64"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-darwin-amd64.sig"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/cosign.pub"

# Install Cosign (if not already installed)
brew install cosign

# Verify signature using public key
cosign verify-blob \
  --key cosign.pub \
  --signature ai-services-darwin-amd64.sig \
  --insecure-ignore-tlog=true \
  ai-services-darwin-amd64

# Install
chmod +x ai-services-darwin-amd64
sudo mv ai-services-darwin-amd64 /usr/local/bin/ai-services

# Verify
ai-services version
```


---

### macOS (Apple Silicon)

#### Quick Install

```bash
VERSION="v0.0.2"

# Download binary
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-darwin-arm64"

# Make executable
chmod +x ai-services-darwin-arm64

# Move to system path
sudo mv ai-services-darwin-arm64 /usr/local/bin/ai-services

# Verify installation
ai-services version
```

#### Verified Install (with Cosign)

```bash
VERSION="v0.0.2"

# Download binary, signature, and public key
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-darwin-arm64"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-darwin-arm64.sig"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/cosign.pub"

# Install Cosign
brew install cosign

# Verify signature using public key
cosign verify-blob \
  --key cosign.pub \
  --signature ai-services-darwin-arm64.sig \
  --insecure-ignore-tlog=true \
  ai-services-darwin-arm64

# Install
chmod +x ai-services-darwin-arm64
sudo mv ai-services-darwin-arm64 /usr/local/bin/ai-services

# Verify
ai-services version
```

---

### Linux (x86_64/AMD64)

#### Quick Install

```bash
VERSION="v0.0.2"

# Download binary
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-linux-amd64"

# Make executable
chmod +x ai-services-linux-amd64

# Move to system path
sudo mv ai-services-linux-amd64 /usr/local/bin/ai-services

# Verify installation
ai-services version
```

#### Verified Install (with Cosign)

```bash
VERSION="v0.0.2"

# Download binary, signature, and public key
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-linux-amd64"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-linux-amd64.sig"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/cosign.pub"

# Install Cosign
curl -LO https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-amd64
chmod +x cosign-linux-amd64
sudo mv cosign-linux-amd64 /usr/local/bin/cosign

# Verify signature using public key
cosign verify-blob \
  --key cosign.pub \
  --signature ai-services-linux-amd64.sig \
  --insecure-ignore-tlog=true \
  ai-services-linux-amd64

# Install
chmod +x ai-services-linux-amd64
sudo mv ai-services-linux-amd64 /usr/local/bin/ai-services

# Verify
ai-services version
```

---

### Linux (ppc64le/Power)

**Optimized for IBM Power Systems and IBM Spyre™**

#### Quick Install

```bash
VERSION="v0.0.2"

# Download binary
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-linux-ppc64le"

# Make executable
chmod +x ai-services-linux-ppc64le

# Move to system path
sudo mv ai-services-linux-ppc64le /usr/local/bin/ai-services

# Verify installation
ai-services version
```

#### Verified Install (with Cosign)

```bash
VERSION="v0.0.2"

# Download binary, signature, and public key
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-linux-ppc64le"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-linux-ppc64le.sig"
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/cosign.pub"

# Install Cosign
curl -LO https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-ppc64le
chmod +x cosign-linux-ppc64le
sudo mv cosign-linux-ppc64le /usr/local/bin/cosign

# Verify signature using public key
cosign verify-blob \
  --key cosign.pub \
  --signature ai-services-linux-ppc64le.sig \
  --insecure-ignore-tlog=true \
  ai-services-linux-ppc64le

# Install
chmod +x ai-services-linux-ppc64le
sudo mv ai-services-linux-ppc64le /usr/local/bin/ai-services

# Verify
ai-services version
```


---


## Additional Resources

- [Main README](../README.md) - Project overview and quick start
- [CONTRIBUTING.md](../CONTRIBUTING.md) - Contributing guidelines
- [GitHub Releases](https://github.com/IBM/project-ai-services/releases) - Download binaries
- [Cosign Documentation](https://docs.sigstore.dev/cosign/overview/) - Signature verification tool
