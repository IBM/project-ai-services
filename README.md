# AI-Services

AI Services, part of the [IBM Open-Source AI Foundation for Power](https://www.ibm.com/docs/en/aiservices), deliver pre-built AI capabilities and integration with inferencing solutions like Red Hat AI Inference Server. Optimized for IBM Spyre™ on Power, they enable fast deployment and support models such as LLMs, embeddings, and re-rankers—helping enterprises scale AI efficiently.

## 📺 Demo

<video src="https://github.com/user-attachments/assets/958980a7-f653-4474-84a7-28d657b5f7d1" controls="controls" style="max-width: 100%;">
  Your browser does not support the video tag.
</video>

## Quick Start

### Installation

For detailed platform-specific installation instructions, see [Installation Guide](docs/INSTALLATION.md).

**Quick install for your platform:**

```bash
# Set version
VERSION="v0.0.2"

# macOS (Intel)
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-darwin-amd64"
chmod +x ai-services-darwin-amd64
sudo mv ai-services-darwin-amd64 /usr/local/bin/ai-services

# macOS (Apple Silicon)
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-darwin-arm64"
chmod +x ai-services-darwin-arm64
sudo mv ai-services-darwin-arm64 /usr/local/bin/ai-services

# Linux (x86_64)
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-linux-amd64"
chmod +x ai-services-linux-amd64
sudo mv ai-services-linux-amd64 /usr/local/bin/ai-services

# Linux (ppc64le/Power)
curl -LO "https://github.com/IBM/project-ai-services/releases/download/${VERSION}/ai-services-linux-ppc64le"
chmod +x ai-services-linux-ppc64le
sudo mv ai-services-linux-ppc64le /usr/local/bin/ai-services
```

**Supported Platforms:**
- macOS (Intel x86_64, Apple Silicon ARM64) - Client-only mode
- Linux (x86_64) - Client-only mode
- Linux (ppc64le/Power) - Local + Remote mode

**Deployment Modes:**
- **Client-only mode**: CLI connects to remote OpenShift cluster for application deployment
- **Local + Remote mode**: Supports both local Podman deployments and remote OpenShift cluster connections

For signature verification with Cosign, see the [Installation Guide](docs/INSTALLATION.md).

### Run the binary to get started

```bash
% ai-services --help
A CLI tool for managing AI Services infrastructure.

Usage:
  ai-services [command]

Available Commands:
  application   Deploy and monitor the applications
  completion    Generate the autocompletion script for the specified shell
  help          Help about any command
  version       Prints CLI version with more info

Flags:
  -h, --help      help for ai-services
  -v, --version   version for ai-services

Use "ai-services [command] --help" for more information about a command.
```

---

## Repository Structure

```bash
project-ai-services/
├── README.md          # Project documentation
├── ai-services/       # CLI tool for project-ai-services
│   ├── assets/        # Application template files
├── images/            # Helper/Utility image assets
├── spyre-rag/         # Spyre RAG implementation
├── test/              # Test assets
│   ├── golden/        # Golden dataset
```
