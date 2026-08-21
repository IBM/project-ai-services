# AI-Services

AI Services, part of the [IBM Open-Source AI Foundation for Power](https://www.ibm.com/docs/en/aiservices), deliver pre-built AI capabilities and integration with inferencing solutions like Red Hat AI Inference Server. Optimized for IBM Spyre™ on Power, they enable fast deployment and support models such as LLMs, embeddings, and re-rankers—helping enterprises scale AI efficiently.

## 📺 Demo

<video src="https://github-production-user-asset-6210df.s3.amazonaws.com/20432587/615272192-155afcc0-1baf-412d-8c39-93ef7df6ecf7.mp4?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAVCODYLSA53PQK4ZA%2F20260701%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Date=20260701T041911Z&X-Amz-Expires=300&X-Amz-Signature=113d1a4f6dd186fd3331cc7feac8d70762598d0f447a6f6354e163cb8ea8ca3f&X-Amz-SignedHeaders=host&response-content-type=video%2Fmp4" controls="controls" style="max-width: 100%;">
  Your browser does not support the video tag.
</video>

## Quick Start

### Installation

For detailed platform-specific installation instructions, see [Installation Guide](docs/INSTALLATION.md).

### Run the binary to get started

```bash
% ai-services --help
A CLI tool for managing AI Services infrastructure.

Usage:
  ai-services [command]

Available Commands:
  application Deploy and monitor the applications
  bootstrap   Initializes AI Services infrastructure
  catalog     Manage the AI Services catalog
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  version     Prints CLI version with more info

Flags:
  -h, --help      help for ai-services
  -v, --version   version for ai-services

Use "ai-services [command] --help" for more information about a command.
```

---

## Repository Structure

```bash
project-ai-services/
├── services/          # Backend microservices
│   ├── common/        # Shared library
│   ├── chatbot/       # RAG chatbot service
│   ├── digitize/      # Document ingestion
│   ├── summarize/     # Summarization service
│   └── similarity/    # Similarity search
├── ui/                # Frontend applications
│   ├── chatbot/       # Chatbot UI
│   ├── digitize/      # Digitize UI
│   └── catalog/       # Catalog UI
├── images/            # Container base images
│   ├── service-base/  # Base image for AI services
│   ├── postgres/      # PostgreSQL image
│   ├── litellm/       # LiteLLM proxy
│   ├── caddy/         # Caddy proxy
│   └── tools/         # Utility tools
└── ai-services/       # CLI tool
```

### Service Architecture

The repository follows a microservices architecture with:

- **Layered Container Images**: `service-base` → `services-common` → individual services
- **Independent Services**: Each service has its own Containerfile, Makefile, and versioning
- **Shared Common Layer**: Common utilities and dependencies in `services/common/`
- **Clean Boundaries**: No cross-service dependencies, all shared code in common layer

**Service Images:**
- `chatbot-service` - RAG chatbot backend
- `digitize-service` - Document ingestion and processing
- `summarize-service` - Text summarization
- `similarity-service` - Semantic similarity search

**UI Images:**
- `chatbot-ui` - Chatbot web interface
- `digitize-ui` - Document upload interface
- `catalog-ui` - Service catalog interface
