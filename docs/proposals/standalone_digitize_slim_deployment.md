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
| **Podman template** | Conditionally render container args (secret exports), embedding/LLM/OpenSearch env vars, volume mounts, and secret volumes | Medium — template logic |
| **OpenShift template** | Same conditional rendering for env vars and secrets | Medium |
| **Digitize Python service** | Make OpenSearch/LLM/embedding env vars optional; conditionally skip connector DB tables and scheduler; hide ingestion endpoints; expose `GET /v1/config` | Medium — service logic |
| **Digitize UI** | Query `GET /v1/config` on startup; conditionally hide ingestion options in IngestSidePanel, enforce digitization-only flow, and adapt UI labels/empty states | Low — UI logic |
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

Wrap embedding, LLM, and OpenSearch references in Go template conditionals. Crucially, this must include container `args` (exporting secrets before app startup), environment variables, volume mounts, and secret volumes. The values will only be present when the service is deployed as part of an architecture that provides these components.

```yaml
# 1. Conditionally export secrets in backend-server container args
args:
  - |
    export POSTGRES_USER=$(cat /etc/secret/digitize-db-secret/username)
    export POSTGRES_PASSWORD=$(cat /etc/secret/digitize-db-secret/password)
    {{- if .Values.vector_store }}
    export OPENSEARCH_USERNAME=$(cat /etc/secret/opensearch-secret/username)
    export OPENSEARCH_PASSWORD=$(cat /etc/secret/opensearch-secret/password)
    {{- end }}
    export DB_ENCRYPTION_KEY=$(cat /etc/secret/digitize-db-encryption-secret/db-encryption-key)
    {{- if and .Values.llm .Values.llm.apiKey }}
    export LLM_API_KEY=$(cat /etc/secret/vllm-secret/apiKey)
    {{- end }}
    exec /var/venv/bin/python -m uvicorn app:app \
      --host 0.0.0.0 \
      --port 4000 \
      --loop uvloop \
      --http httptools

# 2. Conditionally include environment variables
{{- if .Values.embedding }}
- name: EMB_ENDPOINT
  value: "http://{{ .Values.embedding.host }}:{{ .Values.embedding.port }}"
- name: EMB_MODEL
  value: "{{ .Values.embedding.model }}"
- name: EMB_MAX_MODEL_LEN
  value: "{{ .Values.embedding.maxModelLen }}"
{{- end }}

{{- if .Values.llm }}
- name: LLM_ENDPOINT
  value: "http://{{ .Values.llm.host }}:{{ .Values.llm.port }}"
- name: LLM_MODEL
  value: "{{ .Values.llm.model }}"
- name: LLM_MAX_MODEL_LEN
  value: "{{ .Values.llm.maxModelLen }}"
- name: LLM_MAX_BATCH_SIZE
  value: "{{ .Values.llm.maxBatchSize }}"
{{- end }}

{{- if .Values.vector_store }}
- name: OPENSEARCH_HOST
  value: "{{ .Values.vector_store.host }}"
- name: OPENSEARCH_PORT
  value: "{{ .Values.vector_store.port }}"
- name: OPENSEARCH_DB_PREFIX
  value: "RAG_DB"
- name: OPENSEARCH_INDEX_NAME
  value: "RAG_INDEX"
- name: OPENSEARCH_NUM_SHARDS
  value: "1"
{{- end }}

# 3. Conditionally include secret volume mounts
{{- if .Values.vector_store }}
- mountPath: /etc/secret/opensearch-secret
  name: opensearch-secret
  readOnly: true
{{- end }}
{{- if and .Values.llm .Values.llm.apiKey }}
- mountPath: /etc/secret/vllm-secret
  name: vllm-secret
  readOnly: true
{{- end }}

# 4. Conditionally include secret volumes
{{- if .Values.vector_store }}
- name: opensearch-secret
  secret:
    secretName: "opensearch-secret-{{ .Values.vector_store.instanceSlug }}"
{{- end }}
{{- if and .Values.llm .Values.llm.apiKey }}
- name: vllm-secret
  secret:
    secretName: "vllm-secret-{{ .Values.llm.instanceSlug }}"
{{- end }}
```

### 6.3 Deployment Template Changes (OpenShift)

**File: `ai-services/assets/services/digitize/openshift/templates/digitize-api-deployment.yaml`**

Apply the same conditional rendering pattern as the podman template. The OpenShift template currently hardcodes `embedding-predictor` and `llm-predictor` hostnames and credentials — these must be wrapped in `{{- if .Values.embedding }}`, `{{- if .Values.llm }}`, and `{{- if .Values.vector_store }}` conditionals.

### 6.4 Values File Changes

**File: `ai-services/assets/services/digitize/openshift/values.yaml`**

Remove the `embedding:` section (lines 32-34) since standalone deployments won't have an embedding component. When deployed as part of the RAG architecture, embedding values are injected by the planner from the global component.

**File: `ai-services/assets/services/digitize/podman/values.yaml`**

No embedding/llm/vector_store values are defined here (they come from component values injection), so no change needed.

### 6.5 Digitize Python Service Changes

The Digitize service detects whether ingestion backends are available and conditionally adjusts DB schema initialization, background schedulers, endpoints, and pipeline execution.

**File: `services/digitize/settings.py`**

Define the computed feature flag at startup:

```python
# Detect standalone mode by checking for backend availability.
# All three backends are required for ingestion: embedding (vectorization),
# OpenSearch (indexing), and LLM (table summarization during the ingest pipeline).
INGESTION_ENABLED = (
    bool(os.environ.get("EMB_ENDPOINT"))
    and bool(os.environ.get("OPENSEARCH_HOST"))
    and bool(os.environ.get("LLM_ENDPOINT"))
)
```

**File: `services/digitize/db/scripts/init_db.py` & `services/digitize/app.py` (Database Initialization)**

Conditionally skip database initialization for all connector-related tables when `INGESTION_ENABLED` is `False`:

- **Core tables (always created)**: `jobs`, `documents`, `document_checksum`, `conversion_tasks`.
- **Connector tables (skipped in standalone mode)**: `connectors`, `connector_document_checksum`, `connector_sync_logs`, and the `scheduler` schema.
- In `app.py` `_init_database()`: When `INGESTION_ENABLED` is `False`, invoke `Base.metadata.create_all(bind=engine, tables=[...])` targeting only the core tables (`Job`, `Document`, `DocumentChecksum`, `ConversionTask`).
- In `init_db.py`: Execute only core schema SQL and verify core expected tables when `INGESTION_ENABLED` is `False`.
- In `app.py` lifespan: Skip starting `_connector_scheduler_lifespan()` and `recover_connector_sync_state()` when `INGESTION_ENABLED` is `False`, ensuring APScheduler does not attempt to access connector tables or the `scheduler` schema.

**File: `services/digitize/api/v1/config.py` (Runtime Config Endpoint)**

Expose the computed feature flag via a new config endpoint:

```python
@router.get("/config", response_model=ConfigResponse, tags=["config"])
async def get_config() -> ConfigResponse:
    return ConfigResponse(ingestion_enabled=INGESTION_ENABLED)
```

**File: `services/digitize/app.py` (Router Registration & Job Validation)**

When `INGESTION_ENABLED` is `False`:
- Mount the new `/v1/config` router.
- Do **not** mount the `/v1/connectors` router (connectors exist only to feed the ingestion pipeline).
- The `/v1/jobs` endpoint validates `operation` and rejects `operation=ingestion` requests with a `400 Bad Request` (`INVALID_REQUEST` error code: "Ingestion operation is not supported in standalone mode. Only 'digitization' is enabled.").

**File: `services/common/settings.py`**

The OpenSearch, embedding, and LLM settings already default to empty strings (`default=""`), so the service will not crash at startup when these env vars are unset. No changes needed to this file.

**File: `services/digitize/pipeline/ingest.py`**

Add a guard at the top of the ingestion pipeline entry points (`ingest()` and `create_indexing_handler()`) raising a clear runtime error if backends are not configured. This is a defense-in-depth measure — the API layer and UI prevent ingestion requests from reaching here in standalone mode.

### 6.6 Digitize UI Changes

The Digitize frontend UI detects standalone mode and blocks ingestion features accordingly.

**File: `ui/digitize/src/services/api.ts` & `ui/digitize/src/contexts/`**

1. **Config API & Provider**:
   - Add `getConfig()` in `api.ts` calling `GET /v1/config`.
   - Implement `ConfigContext` and `ConfigProvider` (with `useConfig` hook) that fetches service configuration on application startup and provides `ingestionEnabled: boolean` to all UI components.

2. **Job Creation Panel (`IngestSidePanel`)**:
   - When `ingestionEnabled` is `False`:
     - Default the operation state to `"digitization"`.
     - Hide the "Ingestion" option from the operation selector (or hide the operation selector entirely, locking it to digitization).
     - Always display the Output Format selector (`json`, `md`, `txt`) which is required for digitization.
     - Enforce the single-file upload constraint in the UI with instant feedback if more than one file is selected.

3. **Job Monitor & Document List Pages**:
   - Dynamically adjust empty states, help text, and subtitles:
     - Change "Start by ingesting a document" / "To ingest a document, click Create" to "Start by digitizing a document" / "To digitize a document, click Create".
     - Adjust document empty state guidance to reflect digitization rather than ingestion.

### 6.7 Resource Estimate Updates

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
- [ ] Podman and OpenShift templates conditionally include container args (secrets export), environment variables, volume mounts, and secret volumes.
- [ ] Standalone Digitize starts successfully without OpenSearch/LLM/embedding env vars.
- [ ] Database initialization conditionally skips all connector-related tables (`connectors`, `connector_document_checksum`, `connector_sync_logs`, `scheduler` schema) in standalone mode.
- [ ] Connector scheduler and sync recovery are skipped on startup when ingestion is disabled.
- [ ] `GET /v1/config` endpoint returns `{"ingestion_enabled": false}` in standalone mode and `{"ingestion_enabled": true}` in RAG architecture.
- [ ] Ingestion API endpoints return `400 Bad Request` or are not mounted in standalone Digitize.
- [ ] Digitize UI detects `ingestion_enabled: false` via `/v1/config` and hides ingestion options in `IngestSidePanel`, enforces 1-file limit for digitization, and updates empty state descriptions.
- [ ] Deploying the Digital Assistant architecture installs OpenSearch, LLM, and embedding components, and ingestion works end-to-end.
- [ ] Digitization jobs work correctly in standalone mode.
- [ ] No regression in existing Digital Assistant deployments.
- [ ] Resource estimates in metadata reflect standalone-only resources.
- [ ] CHANGELOG documents the breaking change.

