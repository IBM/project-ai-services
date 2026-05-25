# Repository Reorganization Proposal

## Proposed Folder Structure

```
project-ai-services/
├── README.md
├── LICENSE
├── CONTRIBUTING.md
├── .gitignore
│
├── ai-services/                    # CLI tool (unchanged)
│   ├── cmd/
│   ├── assets/
│   └── ...
│
├── services/                       # Backend microservices
│   ├── common/                     # Shared foundation library
│   │   ├── Containerfile
│   │   ├── requirements.txt
│   │   ├── __init__.py
│   │   ├── db_utils.py
│   │   ├── emb_utils.py
│   │   ├── lang_utils.py
│   │   ├── llm_utils.py
│   │   ├── misc_utils.py
│   │   ├── opensearch.py
│   │   ├── perf_utils.py
│   │   ├── retry_utils.py
│   │   ├── settings.py
│   │   ├── thread_utils.py
│   │   └── vector_db.py
│   │
│   ├── chatbot/                    # RAG chatbot service
│   │   ├── Containerfile
│   │   ├── requirements.txt
│   │   ├── settings.json          # Service-level config
│   │   ├── __init__.py
│   │   ├── app.py
│   │   ├── backend_utils.py
│   │   ├── reranker_utils.py
│   │   ├── response_utils.py
│   │   └── retrieval_utils.py
│   │
│   ├── digitize/                   # Document ingestion service
│   │   ├── Containerfile
│   │   ├── requirements.txt
│   │   ├── __init__.py
│   │   ├── app.py
│   │   ├── cleanup.py
│   │   ├── cli.py
│   │   ├── config.py
│   │   ├── digitize.py
│   │   ├── digitize_utils.py
│   │   ├── doc_utils.py
│   │   ├── document.py
│   │   ├── errors.py
│   │   ├── ingest.py
│   │   ├── job.py
│   │   ├── pdf_utils.py
│   │   ├── status.py
│   │   └── types.py
│   │
│   └── summarize/                  # Summarization service
│       ├── Containerfile
│       ├── requirements.txt
│       ├── __init__.py
│       ├── app.py
│       └── summ_utils.py
│
├── ui/                            # Frontend applications
│   ├── chatbot/                   # RAG chatbot UI (from spyre-rag/ui)
│   │   ├── Containerfile
│   │   ├── package.json
│   │   ├── nginx.conf.tmpl
│   │   └── src/
│   │
│   ├── digitize/                  # Document ingestion UI (from digitize-ui)
│   │   ├── Containerfile
│   │   ├── package.json
│   │   ├── nginx.conf.tmpl
│   │   └── src/
│   │
│   └── catalog/                   # Catalog UI (from catalog-ui)
│       ├── Containerfile
│       ├── package.json
│       ├── nginx.conf.tmpl
│       └── src/
│
├── images/                        # Base images and utilities
│   ├── service-base/             # Renamed from rag-base
│   │   ├── Containerfile
│   │   ├── requirements.txt
│   │   ├── download_docling_models.py
│   │   └── prebuilder/
│   │
│   └── tools/
│       ├── Containerfile
│       └── requirements.txt
│
├── docs/                          # Documentation
│   ├── INSTALLATION.md
│   ├── Catalog-API-Guide.md
│   └── implementation-plans/
│
├── test/                          # Test assets
│   └── golden/
│
└── hack/                          # Development scripts

```

## Rationale by Top-Level Directory

### `services/` - Backend Microservices
**Purpose:** Contains all independently deployable backend services with clear separation of concerns.

**Key Changes:**
- **`services/common/`**: Promoted from `spyre-rag/src/common/` to be a true foundation library. Gets its own Containerfile and requirements.txt for building a base image that other services extend.
- **`services/chatbot/`**: Moved from `spyre-rag/src/chatbot/`. Now owns its Containerfile, service-specific requirements, and settings.json. This is a service that can be composed with others (like digitize) to create RAG solutions.
- **`services/digitize/`**: Moved from `spyre-rag/src/digitize/`. Independent service with its own build artifacts.
- **`services/summarize/`**: Moved from `spyre-rag/src/summarize/`. Standalone summarization service.

**Why this structure:**
- Each service directory is self-contained with its own Containerfile and dependencies
- No cross-service imports (enforced by structure)
- Clear ownership and independent deployability
- `common/` is explicitly a shared library, not a peer service
- Chatbot is a service that can be composed with digitize and summarize to create complete RAG applications

### `ui/` - Frontend Applications
**Purpose:** All frontend/UI applications in one place with consistent structure.

**Key Changes:**
- **`ui/chatbot/`**: Moved from `spyre-rag/ui/`
- **`ui/digitize/`**: Moved from `digitize-ui/` (already at root)
- **`ui/catalog/`**: Moved from `catalog-ui/` (already at root)

**Why this structure:**
- Consolidates all UI code under one parent directory
- Each UI owns its Containerfile and build configuration
- Parallel structure to services/ for easy navigation
- Clear separation of frontend from backend concerns

### `images/` - Base Images
**Purpose:** Foundation images that services build upon.

**Key Changes:**
- **`images/service-base/`**: Renamed from `images/rag-base/` to reflect its general-purpose nature
- Contains shared Python dependencies, docling models, system packages

**Why this structure:**
- Base image is no longer RAG-specific in name
- Clear location for shared build artifacts
- Supports the layered container build strategy

### `ai-services/`, `docs/`, `test/`, `hack/`
**No changes** - These directories remain at root as they serve cross-cutting concerns.

## Specific File Movements

### From `spyre-rag/src/` to `services/`:
```
spyre-rag/src/common/*           → services/common/*
spyre-rag/src/chatbot/*          → services/chatbot/*
spyre-rag/src/digitize/*         → services/digitize/*
spyre-rag/src/summarize/*        → services/summarize/*
spyre-rag/src/settings.json      → services/chatbot/settings.json
spyre-rag/src/Containerfile      → DELETE (replaced by per-service Containerfiles)
spyre-rag/src/Makefile           → DELETE (replaced by per-service Makefiles)
```

### From `spyre-rag/ui/` to `ui/chatbot/`:
```
spyre-rag/ui/*                   → ui/chatbot/*
```

### From root to `ui/`:
```
digitize-ui/*                    → ui/digitize/*
catalog-ui/*                     → ui/catalog/*
```

### In `images/`:
```
images/rag-base/*                → images/service-base/*
```

### Delete entire directory:
```
spyre-rag/                       → DELETE (contents redistributed)
```

## Container Build Strategy

### Base Image Layer (`images/service-base/`)
```dockerfile
# images/service-base/Containerfile
FROM registry.access.redhat.com/ubi9/ubi:9.7

# Install system dependencies
RUN yum install -y python3.12-devel python3.12-pip ...

# Install common Python packages
COPY requirements.txt .
RUN pip install -r requirements.txt

# Download docling models
COPY download_docling_models.py .
RUN python download_docling_models.py

# This image is tagged as: icr.io/ai-services-cicd/service-base:latest
```

**Contains:**
- System packages (Python, libraries)
- Common Python dependencies (fastapi, opensearch-py, docling, etc.)
- Docling models
- Virtual environment setup

### Common Library Layer (`services/common/`)
```dockerfile
# services/common/Containerfile
FROM icr.io/ai-services-cicd/service-base:latest

WORKDIR /opt/services/common

# Install any common-specific dependencies
COPY requirements.txt .
RUN pip install -r requirements.txt

# Copy common library code
COPY . .

# This image is tagged as: icr.io/ai-services-cicd/services-common:latest
```

**Contains:**
- Base image + common library code
- Shared utilities, clients, models
- Can be used as base for all services

### Per-Service Images
```dockerfile
# services/chatbot/Containerfile
FROM icr.io/ai-services-cicd/services-common:latest

WORKDIR /opt/services/chatbot

# Install chatbot-specific dependencies only
COPY requirements.txt .
RUN pip install -r requirements.txt

# Copy service code and config
COPY . .

CMD ["/var/venv/bin/python", "-m", "uvicorn", "app:app", "--host", "0.0.0.0", "--port", "5000"]
```

**Each service (chatbot, digitize, summarize):**
- Extends `icr.io/ai-services-cicd/services-common:latest`
- Adds only service-specific dependencies
- Copies only its own code
- Defines its own entrypoint

### Build Order
```bash
# 1. Build base image with system deps and common Python packages
cd images/service-base && podman build -t icr.io/ai-services-cicd/service-base:latest -f Containerfile .

# 2. Build common library layer
cd services/common && podman build -t icr.io/ai-services-cicd/services-common:latest -f Containerfile .

# 3. Build individual services (can be parallel)
cd services/chatbot && podman build -t chatbot-service:latest -f Containerfile .
cd services/digitize && podman build -t digitize-service:latest -f Containerfile .
cd services/summarize && podman build -t summarize-service:latest -f Containerfile .

# 4. Build UI images (independent, can be parallel)
cd ui/chatbot && podman build -t chatbot-ui:latest -f Containerfile .
cd ui/digitize && podman build -t digitize-ui:latest -f Containerfile .
```

### Benefits of This Strategy
1. **Layer caching**: Common dependencies built once, reused by all services
2. **Fast rebuilds**: Changing one service only rebuilds that service
3. **Clear dependencies**: Build order reflects actual dependency graph
4. **Smaller images**: Each service only includes what it needs beyond common
5. **Independent deployment**: Each service can be versioned and deployed separately
6. **No cross-service coupling**: Structure enforces architectural boundaries

## Migration Impact

### Deployment Templates
Update paths in `ai-services/assets/applications/rag/`:
- Command paths change: `chatbot.app:app` → `app:app` (since each service is now root of its context)
- Update environment variable references if needed

### Import Statements
No changes needed - Python imports remain the same:
```python
from common.db_utils import ...
from common.settings import get_settings
```

### CI/CD
Update build pipelines to:
1. Build base image first
2. Build common layer second
3. Build services in parallel
4. Tag and push each independently

### Configuration
- `settings.json` moves to `services/chatbot/` since it's primarily used by the chatbot service
- Other services can have their own config files as needed
- Shared configuration can be managed via environment variables or mounted config maps in deployment