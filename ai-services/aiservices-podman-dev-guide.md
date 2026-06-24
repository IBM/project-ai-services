# AI Services - Deployment Guide (Podman)

This guide provides step-by-step instructions for deploying and managing the AI Services Catalog in a Podman environment. The catalog-based deployment workflow enables streamlined application management through both UI and CLI interfaces.

## Getting Started

### 1. Clone the Repository

```bash
git clone https://github.com/IBM/project-ai-services.git
cd ai-services
```

### 2. Build the Binary

Build the `ai-services` binary using the provided Makefile:

```bash
make bin
```

This creates the binary at `./bin/ai-services`.

### 3. Rename Binary (Optional)

If the binary has a different name, rename it:

```bash
mv ./bin/<original-name> ./bin/ai-services
```

### 4. Bootstrap the Environment

Bootstrap the AI Services environment for Podman:

```bash
./bin/ai-services bootstrap --runtime podman
```

Use `-h` for more information.

### 5. Podman Login

Login to the container registry (required to pull from rhaii image and private icr images for dev):

```bash
podman login <registry-url>
```

### 6. Configure the Catalog

Configure and deploy the Catalog service:

```bash
./bin/ai-services catalog configure --runtime podman
```

This command outputs:
- **Catalog UI endpoint** - Web interface for browsing and managing the catalog
- **Catalog Backend endpoint** - API server for programmatic access

Use `-h` for more configuration options.

### 7. Deploy an Application

#### Option A: Using Catalog UI

1. Open the **Catalog UI endpoint** in your web browser
2. Browse and deploy applications through the graphical interface

#### Option B: Using CLI

**Step 1: Login to Catalog Backend**

```bash
./bin/ai-services catalog login --server <catalog_backend_endpoint> --username admin --runtime podman --insecure
```

Use `-h` for more information.

**Step 2: Create an Application**

List available templates:
```bash
./bin/ai-services application templates --runtime podman
```

View parameters available for a template to be customized:
```bash
./bin/ai-services application templates parameters --template <template-name> --runtime podman
```

Create an application:
```bash
./bin/ai-services application create <app-name> --template <template-name> --runtime podman
```

Examples:
```bash
# Deploy RAG application (By default llm and reranker runs with Spyre)
./bin/ai-services application create <app-name> --template rag --runtime podman

# Deploy with custom parameters (running reranker in CPU)
./bin/ai-services application create <app-name> --template rag --runtime podman --params reranker.vllm-cpu=true
```

Use `-h` for more information.

**Step 3: Manage Applications**

List all applications:
```bash
./bin/ai-services application ps [app-name] --runtime podman
```

Get application details:
```bash
./bin/ai-services application info <app-name> --runtime podman
```

**Step 4: Delete Application**

```bash
./bin/ai-services application delete <app-name> --runtime podman
```

## Additional Commands

**Catalog commands:**
```bash
# Get catalog service information
./bin/ai-services catalog info --runtime podman

# Check current login status
./bin/ai-services catalog whoami --runtime podman

# Logout from catalog
./bin/ai-services catalog logout --runtime podman

# Uninstall catalog service (removes catalog pods and data)
./bin/ai-services catalog uninstall --runtime podman
```

**Application commands:**
```bash
./bin/ai-services application start <app-name> --runtime podman
./bin/ai-services application stop <app-name> --runtime podman
```

## Getting Help

Use `-h` with any command for detailed help:

```bash
./bin/ai-services -h
./bin/ai-services catalog -h
./bin/ai-services application -h
./bin/ai-services bootstrap -h
```

## Developer Notes

### Modifying Templates

To modify or customize templates, work with the following directories:

**Services:** `assets/services/`
- Contains service-level templates and configurations
- Each service has its own directory with metadata, templates, and parameter schemas

**Components:** `assets/components/`
- Contains reusable component templates (LLM, embedding, reranker, vector_db, etc.)
- Each component type has provider-specific implementations

**Structure:**
```
assets/
├── services/
│   └── <service-name>/
│       ├── metadata.yaml
│       ├── podman/
│       │   ├── values.yaml
│       │   ├── values.schema.json
│       │   └── templates/
└── components/
    └── <component-type>/
        └── <provider-name>/
            ├── metadata.yaml
            └── podman/
                ├── values.yaml
                ├── values.schema.json
                └── templates/
```

**After modifying templates:**
1. Rebuild the catalog image: `make build`
2. Update the image reference in `assets/catalog/podman/values.yaml`
3. Rebuild the binary: `make bin`
4. Delete all existing applications: `./bin/ai-services application delete <app-name> --runtime podman`
5. Verify no applications exist: `./bin/ai-services application ps --runtime podman`
6. Uninstall existing catalog: `./bin/ai-services catalog uninstall --runtime podman`
7. Reconfigure catalog: `./bin/ai-services catalog configure --runtime podman`

## Environment Notes

- This guide is specifically for **Podman environments**
- All commands assume you're running in a Podman-compatible setup
- Ensure Podman is properly configured before running bootstrap commands
