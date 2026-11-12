# Standalone Digitize: Remove OpenSearch, LLM, and Embedding Dependencies
---

## Table of Contents

1. [Background](#1-background)
2. [Problem Statement](#2-problem-statement)
3. [Goals and Non-Goals](#3-goals-and-non-goals)
4. [Current Architecture](#4-current-architecture)
5. [Proposed Changes](#5-proposed-changes)
6. [Detailed Design](#6-detailed-design)
7. [Migration and Compatibility](#7-migration-and-compatibility)
8. [Testing Strategy](#8-testing-strategy)
9. [Acceptance Criteria](#9-acceptance-criteria)

---

## 1. Background

The Digitize service provides two core capabilities:

- **Digitization**: Converts physical/scanned documents (PDF, DOCX) into searchable, editable digital formats — text extraction, table parsing, and structured output.
- **Ingestion**: Processes digitized content into chunks, generates embeddings, and indexes them into a vector database (OpenSearch) for semantic search.

Digitization is a self-contained capability. Ingestion requires three external components: an LLM (for table summarization), an embedding model (for vectorizing chunks), and a vector store (OpenSearch, for indexing).

The Digitize service can be deployed in two ways:

1. **Standalone** (`ai-services application create digitize --template digitize`) — the service is deployed on its own for document conversion use cases.
2. **As part of the Digital Assistant architecture** (`--template rag`) — deployed alongside Chat, Similarity, and shared components for full RAG workflows including ingestion.

## 2. Problem Statement

The standalone Digitize deployment currently installs all three ingestion components (OpenSearch, LLM, embedding model) even though they are only needed within the Digital Assistant architecture. This causes:

- **Unnecessary resource consumption**: ~100+ GB memory, 8+ CPU cores, and 4 Spyre cards consumed by components the standalone user never uses.
- **Deployment complexity**: More containers to configure, more secrets to manage, more things that can fail.
- **Slower deployment**: Downloading and starting three additional component pods adds several minutes.
- **Confusion**: Users deploying standalone Digitize for document conversion are presented with LLM/embedding/OpenSearch configuration they don't need.

## 3. Goals and Non-Goals

### Goals

- Standalone Digitize deploys **only** the Digitize service pod, its UI, PostgreSQL, and MCP sidecar — no OpenSearch, LLM, or embedding.
- Digital Assistant (RAG) architecture continues to install all components and ingestion works end-to-end.
- Ingestion-related API endpoints are hidden in standalone mode (not visible in OpenAPI docs, not routable).
- No regression in existing Digital Assistant deployments.

### Non-Goals

- Changing the ingestion pipeline logic itself.
- Supporting a "bring your own LLM/OpenSearch" model for standalone Digitize (future work).
- Changing how other services (Chat, Similarity, etc.) declare their dependencies.

## 4. Current Architecture

### Digitize Service Metadata (`ai-services/assets/services/digitize/metadata.yaml`)

```yaml
dependencies:
  - id: vector_store
  - id: embedding
  - id: llm

standalone: true
```

These three dependencies cause the deployment planner (`deploy_options.go` → `buildServiceComponents`) to resolve providers for each component type and include them in the deployment plan. For standalone deployments, this means installing OpenSearch, an embedding model server, and an LLM server alongside Digitize.

### RAG Architecture Metadata (`ai-services/assets/architectures/rag/metadata.yaml`)

```yaml
global_components:
  - type: vector_store
  - type: embedding

services:
  - id: chat        # declares dependencies: vector_store, embedding, llm
  - id: digitize    # declares dependencies: vector_store, embedding, llm
  - id: similarity  # declares dependencies: vector_store, embedding, reranker
```

The architecture provides `vector_store` and `embedding` as global components. The `llm` component is installed because `chat` (and currently `digitize`) declare it as a dependency. The planner deduplicates: same component type + provider = single deployment.

### Deployment Template References

The Digitize podman template (`digitize.yaml.tmpl`) hardcodes environment variables for all three backends:

```yaml
- name: EMB_ENDPOINT
  value: "http://{{ .Values.embedding.host }}:{{ .Values.embedding.port }}"
- name: LLM_ENDPOINT
  value: "http://{{ .Values.llm.host }}:{{ .Values.llm.port }}"
- name: OPENSEARCH_HOST
  value: "{{ .Values.vector_store.host }}"
```

These are always rendered, even in standalone mode.

### Dependency Flow

```
Standalone deploy request
  └─ buildServicePayload()
       └─ GetServiceDeployOptions()
            └─ buildServiceComponents(dependencies: [vector_store, embedding, llm])
                 └─ Installs OpenSearch + embedding model + LLM  ← UNNECESSARY
```

## 5. Proposed Changes

### Summary

| Layer | Change | Risk |
|-------|--------|------|
| **Catalog metadata** | Remove `vector_store`, `embedding`, `llm` from Digitize's `dependencies` | Low — only affects standalone path |
| **RAG architecture metadata** | No change — `chat` already declares `llm` dependency | None |
| **Podman template** | Conditionally render embedding/LLM/OpenSearch env vars and volumes | Medium — template logic |
| **OpenShift template** | Same conditional rendering | Medium |
| **Digitize Python service** | Make OpenSearch/LLM/embedding env vars optional; hide ingestion endpoints when backends are absent | Medium — service logic |
| **Metadata `about` section** | Remove LLM/embedding/OpenSearch from standalone resource estimates | Low — documentation |
| **Values files** | Remove `embedding`/`llm`/`vector_store` default values from digitize-specific values.yaml | Low |

### What stays unchanged

- The RAG architecture `global_components` list (already has `vector_store` and `embedding`).
- The `chat` service's dependency on `llm` (ensures LLM is still installed in RAG).
- The `similarity` service's dependency on `vector_store`, `embedding`, `reranker`.
- All other services' metadata.

## 6. Detailed Design

### 6.1 Catalog Metadata Changes

**File: `ai-services/assets/services/digitize/metadata.yaml`**

Remove the `dependencies` block entirely:

```yaml
# Before
dependencies:
  - id: vector_store
  - id: embedding
  - id: llm

# After
# (dependencies block removed — standalone Digitize has no component dependencies)
```

Update the `about` section to reflect standalone-only details:

- Remove "Large language model (LLM)", "Embedding model", "Vector store" from "Service details".
- Remove "Knowledge management indexing status" from outputs.
- Update "External dependencies" to note these are only required in the Digital Assistant architecture.
- Reduce "Expected resource consumption" to reflect only Digitize + Postgres + UI (no LLM/embedding/OpenSearch).

### 6.2 Deployment Template Changes (Podman)

**File: `ai-services/assets/services/digitize/podman/templates/digitize.yaml.tmpl`**

Wrap embedding, LLM, and OpenSearch references in Go template conditionals. The values will only be present when the service is deployed as part of an architecture that provides these components.

```yaml
# Conditionally include embedding env vars
{{- if .Values.embedding }}
- name: EMB_ENDPOINT
  value: "http://{{ .Values.embedding.host }}:{{ .Values.embedding.port }}"
- name: EMB_MODEL
  value: "{{ .Values.embedding.model }}"
- name: EMB_MAX_MODEL_LEN
  value: "{{ .Values.embedding.maxModelLen }}"
{{- end }}

# Conditionally include LLM env vars
{{- if .Values.llm }}
- name: LLM_ENDPOINT
  value: "http://{{ .Values.llm.host }}:{{ .Values.llm.port }}"
- name: LLM_MODEL
  value: "{{ .Values.llm.model }}"
...
{{- end }}

# Conditionally include OpenSearch env vars
{{- if .Values.vector_store }}
- name: OPENSEARCH_HOST
  value: "{{ .Values.vector_store.host }}"
...
{{- end }}
```

Similarly, conditionally include the secret volume mounts for `opensearch-secret` and `vllm-secret`.

### 6.3 Deployment Template Changes (OpenShift)

**File: `ai-services/assets/services/digitize/openshift/templates/digitize-api-deployment.yaml`**

Apply the same conditional rendering pattern as the podman template. The OpenShift template currently hardcodes `embedding-predictor` and `llm-predictor` hostnames — these must be wrapped in conditionals.

### 6.4 Values File Changes

**File: `ai-services/assets/services/digitize/openshift/values.yaml`**

Remove the `embedding:` section (lines 32-34) since standalone deployments won't have an embedding component. When deployed as part of the RAG architecture, embedding values are injected by the planner from the global component.

**File: `ai-services/assets/services/digitize/podman/values.yaml`**

No embedding/llm/vector_store values are defined here (they come from component values injection), so no change needed.

### 6.5 Digitize Python Service Changes

The Digitize service needs to detect whether ingestion backends are available and conditionally expose endpoints.

**File: `services/digitize/app.py`**

Conditionally include the ingestion-related parts of the jobs router. The `operation` parameter on the jobs endpoint accepts `ingestion` or `digitization` — in standalone mode, only `digitization` should be available.

```python
# Detect standalone mode by checking for backend availability.
# All three are required for ingestion: embedding (vectorization), OpenSearch (indexing),
# and LLM (table summarization during the ingest pipeline).
INGESTION_ENABLED = (
    bool(os.environ.get("EMB_ENDPOINT"))
    and bool(os.environ.get("OPENSEARCH_HOST"))
    and bool(os.environ.get("LLM_ENDPOINT"))
)
```

When `INGESTION_ENABLED` is `False`:
- The `/v1/jobs` endpoint should only accept `operation=digitization` (reject `ingestion` requests).
- The connectors router should not be mounted (connectors exist to feed the ingestion pipeline).
- The OpenAPI description should omit ingestion references.

**File: `services/common/settings.py`**

The OpenSearch, embedding, and LLM settings already default to empty strings (`default=""`), so the service will not crash at startup when these env vars are unset. No changes needed to this file.

**File: `services/digitize/pipeline/ingest.py`**

Add a guard at the top of the ingestion pipeline entry points that raises a clear error if backends are not configured. This is a defense-in-depth measure — the API layer should prevent ingestion requests from reaching here in standalone mode.

### 6.6 Resource Estimate Updates

**File: `ai-services/assets/services/digitize/metadata.yaml` — `about` section**

Current standalone resource estimates include LLM/embedding/OpenSearch overhead:
- Compute: 19 CPU cores → reduce to ~11 CPU cores (Digitize backend + Postgres + UI)
- Memory: 162.50 GB → reduce to ~51 GB
- Storage: 90.00 GB → reduce to ~10 GB
- Spyre cards: 4 → 0

**File: `ai-services/assets/services/digitize/podman/metadata.yaml`**

The resources block already shows service-only resources (11 CPU, ~50.5Gi memory, 10Gi storage). This is correct for standalone and needs no change.

## 7. Migration and Compatibility

### Existing standalone Digitize deployments

Users who currently run standalone Digitize with ingestion capabilities will need to redeploy using the Digital Assistant architecture to retain ingestion. This is a **breaking change** for standalone users who rely on ingestion.

**Mitigation**: Document the change in the CHANGELOG and release notes. Provide clear guidance: "To use ingestion (embedding + indexing into OpenSearch), deploy the Digital Assistant architecture instead of standalone Digitize."

### Existing Digital Assistant deployments

No impact. The RAG architecture path is unchanged:
- `vector_store` and `embedding` come from `global_components`.
- `llm` comes from `chat`'s dependency declaration.
- Digitize still gets these components' connection values injected by the planner when deployed as part of the architecture.

### API compatibility

Standalone Digitize will no longer expose ingestion endpoints. Clients that call `POST /v1/jobs` with `operation=ingestion` against a standalone deployment will get a 404 (endpoint hidden) instead of a success or an infrastructure error. This is intentional — ingestion was never functional without the backends.

## 8. Testing Strategy

### Unit Tests

- Verify `GetServiceDeployOptions("digitize")` returns zero components.
- Verify `GetArchitectureDeployOptions("rag")` still returns `vector_store`, `embedding`, and `llm` components.
- Verify the deployment planner creates a plan without component entries for standalone digitize.

### Integration / E2E Tests

- **Standalone Digitize deployment**: Deploy standalone, verify only Digitize pod + Postgres + UI are running. Verify digitization jobs work. Verify ingestion endpoints return 404.
- **Digital Assistant deployment**: Deploy RAG architecture, verify all components are installed. Run an end-to-end ingestion flow through Digitize (upload → digitize → ingest → query via Chat).
- **Template rendering**: Verify podman and OpenShift templates render correctly both with and without embedding/llm/vector_store values.

### Regression Tests

- All existing e2e tests for the Digital Assistant architecture must pass unchanged.
- Verify `ai-services application create digitize --template digitize` completes successfully and does not attempt to start OpenSearch/LLM/embedding.

## 9. Acceptance Criteria

- [ ] Deploying standalone Digitize does not install OpenSearch, LLM, or embedding components.
- [ ] Deploying the Digital Assistant architecture installs OpenSearch, LLM, and embedding components, and ingestion works end-to-end.
- [ ] Standalone Digitize starts successfully without OpenSearch/LLM/embedding env vars.
- [ ] Ingestion API endpoints are not visible in standalone Digitize's OpenAPI spec.
- [ ] Digitization jobs work correctly in standalone mode.
- [ ] No regression in existing Digital Assistant deployments.
- [ ] Resource estimates in metadata reflect standalone-only resources.
- [ ] CHANGELOG documents the breaking change.

