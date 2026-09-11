# Bring Your Own Service (BYOS) — Developer Guide

This guide walks you through creating, packaging, uploading, and managing custom catalog assets using the **Bring Your Own Service (BYOS)** feature. BYOS lets you onboard proprietary AI services and component providers into a live deployment without modifying the platform binary or restarting any pods.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Bundle Structure](#2-bundle-structure)
   - 2.1 [Top-level `metadata.yaml` (shared by both runtimes)](#21-top-level-metadatayaml-shared-by-both-runtimes)
   - 2.2 [Podman Bundle Structure](#22-podman-bundle-structure)
   - 2.3 [OpenShift Bundle Structure](#23-openshift-bundle-structure)
   - 2.4 [Packaging as `.tar.gz`](#24-packaging-as-targz)
3. [Supported Services and Components](#3-supported-services-and-components)
   - 3.1 [Built-in Services](#31-built-in-services)
   - 3.2 [Built-in Components by Runtime](#32-built-in-components-by-runtime)
4. [Template Reference (Podman)](#4-template-reference-podman)
   - 4.1 [Built-in Template Context Variables](#41-built-in-template-context-variables)
   - 4.2 [Lifecycle Labels](#42-lifecycle-labels)
   - 4.3 [Routing Annotations (Services)](#43-routing-annotations-services)
   - 4.4 [Injected Dependency Values (Services)](#44-injected-dependency-values-services)
   - 4.5 [`podTemplateExecutions` Execution Order](#45-podtemplateexecutions-execution-order)
   - 4.6 [`@generate` Directive](#46-generate-directive)
   - 4.7 [`values.schema.json` — User-Configurable Parameters](#47-valuesschemajson--user-configurable-parameters)
5. [Managing Bundles with the CLI](#5-managing-bundles-with-the-cli)
   - 5.1 [Prerequisites](#51-prerequisites)
   - 5.2 [Create a Bundle](#52-create-a-bundle)
   - 5.3 [Validate a Bundle Before Creating](#53-validate-a-bundle-before-creating)
   - 5.4 [Update a Bundle](#54-update-a-bundle)
   - 5.5 [List Bundles](#55-list-bundles)
   - 5.6 [Get Bundle Details](#56-get-bundle-details)
   - 5.7 [Delete a Bundle](#57-delete-a-bundle)
6. [Viewing Custom Services in the Catalog UI](#6-viewing-custom-services-in-the-catalog-ui)
7. [Reserved IDs](#7-reserved-ids)
8. [Tips and Troubleshooting](#8-tips-and-troubleshooting)

---

## 1. Overview

### What is a Service?

A **service** is a deployable AI workload — the top-level thing a user launches from the catalog. It represents a complete functional unit such as a chatbot, a document summarizer, or a similarity-search API. A service can declare **dependencies** on one or more components (e.g. an LLM, an embedding model, a vector store) that the platform resolves and wires up automatically at deploy time.

### What is a Component?

A **component** is a reusable infrastructure provider that a service depends on. Components are not deployed standalone — they are always launched as part of a service deployment. There are four component types:

| Type | Role |
|---|---|
| `llm` | Large language model inference endpoint (e.g. vLLM on CPU, vLLM on Spyre, watsonx.ai) |
| `embedding` | Text embedding model endpoint used for vector indexing and retrieval |
| `reranker` | Reranking model endpoint used to re-score retrieved passages |
| `vector_db` | Vector database for storing and searching embeddings (e.g. OpenSearch) |

When a service declares `dependencies: [{id: llm}]`, the platform asks the user to choose an LLM component provider at deploy time and injects its connection details into the service's template automatically.

### How BYOS fits in

The platform's built-in catalog is compiled into the binary at build time. BYOS introduces a **bundle** mechanism: you package your service or component definition as a `.tar.gz` archive and upload it to the running catalog backend over HTTPS. The platform validates, registers, and hot-reloads the new asset immediately — no pod restart or platform rebuild required.

```
You (author)                  Catalog Backend             Catalog Provider
     │                              │                            │
     │  POST /api/v1/catalog/bundles│                            │
     │  (my-bundle.tar.gz)          │                            │
     │─────────────────────────────>│                            │
     │                              │── validate + extract ──>   │
     │                              │── insert DB row ────────>  │
     │                              │── CatalogProvider.Reload() │
     │                              │<── hot-reload complete ─── │
     │<── 201 Created ──────────────│                            │
```

**Key facts:**
- Both Podman and OpenShift deployments are supported.
- Bundles are stored on a dedicated named volume (`catalog-bundles`) so they survive pod restarts.
- At most one bundle per `(catalog_type, catalog_id)` pair is active at any time — use `bundle update` to upgrade a version.
- Built-in platform service IDs are protected; uploading a bundle with a reserved ID is rejected with `422`.

---

## 2. Bundle Structure

A bundle contains exactly **one** catalog item. The archive format is `.tar.gz`. The top-level directory name inside the archive is **irrelevant** — the server strips it during extraction. All identity information is read from `metadata.yaml` inside the archive.

The structure of a bundle **diverges between Podman and OpenShift** once past the shared top-level `metadata.yaml`. The table below summarises where the two runtimes differ:

| | Podman | OpenShift |
|---|---|---|
| **Runtime directory** | `podman/` | `openshift/` |
| **Template format** | Go templates (`.yaml.tmpl`) rendered to Podman pod specs | Helm chart — standard Kubernetes YAML |
| **Extra required file** | _(none)_ | `Chart.yaml` — Helm chart descriptor |
| **Runtime `metadata.yaml`** | Requires `podTemplateExecutions` listing every template file | Only `name`, `version`, `resources` — no `podTemplateExecutions` |
| **Deployment ordering** | Controlled by `podTemplateExecutions` layers | Managed by Helm — all templates applied as a single release |
| **Template variable syntax** | `{{ .InstanceSlug }}`, `{{ .TemplateID }}`, `{{ .Values.x }}` | `{{ .Release.Name }}`, `{{ .Chart.AppVersion }}`, `{{ .Values.x }}` |

> A bundle can include **both** `podman/` and `openshift/` subdirectories to support both runtimes in a single archive. Include only the directory for the runtime(s) you are targeting.

---

### 2.1 Top-level `metadata.yaml` (shared by both runtimes)

The top-level `metadata.yaml` sits at the archive root and is **identical regardless of runtime**. It is always required and declares the identity of the catalog item.

**Service:**

```yaml
id: my-service                   # unique; must not conflict with built-in IDs
name: "My Custom Service"        # human-readable display name
description: "A custom AI service for internal workloads"
type: service                    # must be "service"
certified_by: "Custom"
dependencies:
  - id: llm                      # component types this service requires
standalone: true
```

> **Note:** Custom services cannot be attached to the built-in Digital Assistant (`rag`) architecture. The `architectures` field is omitted intentionally — only platform-built services are wired into named architectures.

**Component:**

```yaml
id: my-provider
name: "My Custom LLM Provider"
type: component
component_type: llm               # required: llm | embedding | reranker | vector_db
version: "1.0.0"
```

> The `component_type` field is required for components. Missing or unrecognised values are rejected with `422`.

**Component — on-disk storage mapping:**

| `component_type` | `catalog_id` in DB       | On-disk directory              |
|------------------|--------------------------|--------------------------------|
| `llm`            | `llm--my-provider`       | `llm--my-provider-1.0.0`       |
| `embedding`      | `embedding--my-provider` | `embedding--my-provider-1.0.0` |
| `reranker`       | `reranker--my-provider`  | `reranker--my-provider-1.0.0`  |
| `vector_db`      | `vector_db--my-provider` | `vector_db--my-provider-1.0.0` |

Two bundles with the same bare `id` but different `component_type` values are stored as entirely independent records and can coexist simultaneously.

> See [`assets/services/summarize/metadata.yaml`](assets/services/summarize/metadata.yaml) for a real service example — `summarize` declares `dependencies: [{id: llm}]` and `standalone: true`.

---

### 2.2 Podman Bundle Structure

Under `podman/`, templates are **Go template files** (`.yaml.tmpl`) that render directly to Podman pod specs. The runtime `metadata.yaml` must include a `podTemplateExecutions` list naming **every template file** and controlling deployment order.

**Service bundle (Podman):**

```
my-service/
├── metadata.yaml                    ← shared top-level (see §2.1)
└── podman/
    ├── metadata.yaml                ← required: name, version, resources, podTemplateExecutions
    ├── values.yaml                  ← required: default parameter values
    ├── values.schema.json           ← optional: user-configurable parameters for the UI
    └── templates/
        └── my-service.yaml.tmpl     ← Go template; every file here must appear in podTemplateExecutions
```

**Component bundle (Podman):**

```
my-provider/
├── metadata.yaml                    ← shared top-level with component_type (see §2.1)
└── podman/
    ├── metadata.yaml                ← required
    ├── values.yaml                  ← required
    ├── values.schema.json           ← optional
    └── templates/
        └── my-provider.yaml.tmpl    ← Go template
```

**Runtime `metadata.yaml`** (Podman):

```yaml
name: my-service
version: "1.0.0"
podTemplateExecutions:
  - [dependency-secret.yaml.tmpl]   # layer 1 – credential secrets (runs first)
  - [my-service.yaml.tmpl]          # layer 2 – service pod (runs after layer 1 completes)
resources:
  cpu: 4
  memory: 8589934592               # bytes (8 GiB)
  storage: 10737418240             # bytes (10 GiB)
```

> See [`assets/services/summarize/podman/metadata.yaml`](assets/services/summarize/podman/metadata.yaml) for the real three-layer pattern (secret → postgres → service pod).

**`values.yaml`** (Podman):

```yaml
myservice:
  image: registry.example.com/my-org/my-service:v1.0.0
  port: ""
  log_level: "INFO"
```

> See [`assets/services/summarize/podman/values.yaml`](assets/services/summarize/podman/values.yaml) for a real example including a `# @generate:password` field.

> **Reference — full Podman file tree for `summarize`:**
> [`assets/services/summarize/podman/metadata.yaml`](assets/services/summarize/podman/metadata.yaml) ·
> [`assets/services/summarize/podman/values.yaml`](assets/services/summarize/podman/values.yaml) ·
> [`assets/services/summarize/podman/values.schema.json`](assets/services/summarize/podman/values.schema.json) ·
> [`assets/services/summarize/podman/templates/postgres-secret.yaml.tmpl`](assets/services/summarize/podman/templates/postgres-secret.yaml.tmpl) ·
> [`assets/services/summarize/podman/templates/postgres.yaml.tmpl`](assets/services/summarize/podman/templates/postgres.yaml.tmpl) ·
> [`assets/services/summarize/podman/templates/summarize-api.yaml.tmpl`](assets/services/summarize/podman/templates/summarize-api.yaml.tmpl)

---

### 2.3 OpenShift Bundle Structure

Under `openshift/`, templates are a **Helm chart** — standard Kubernetes YAML files rendered by Helm. A `Chart.yaml` is required. There is **no `podTemplateExecutions`** in the runtime `metadata.yaml`; Helm manages all templates as a single release.

**Service bundle (OpenShift):**

```
my-service/
├── metadata.yaml                    ← shared top-level (see §2.1)
└── openshift/
    ├── Chart.yaml                   ← required: Helm chart descriptor (no Podman equivalent)
    ├── metadata.yaml                ← required: name, version, resources — no podTemplateExecutions
    ├── values.yaml                  ← required: default Helm values
    ├── values.schema.json           ← optional: user-configurable parameters for the UI
    └── templates/
        ├── my-service-deployment.yaml
        ├── my-service-service.yaml
        └── my-service-route.yaml
```

**Component bundle (OpenShift):**

```
my-provider/
├── metadata.yaml                    ← shared top-level with component_type (see §2.1)
└── openshift/
    ├── Chart.yaml                   ← required
    ├── metadata.yaml                ← required
    ├── values.yaml                  ← required
    ├── values.schema.json           ← optional
    └── templates/
        └── my-provider-inferenceservice.yaml
```

**`Chart.yaml`** (OpenShift only — no Podman equivalent):

```yaml
apiVersion: v2
name: my-service
description: A Helm chart for deploying My Custom Service on OpenShift
type: application
version: "1.0.0"
appVersion: "1.0.0"
```

> See [`assets/services/summarize/openshift/Chart.yaml`](assets/services/summarize/openshift/Chart.yaml) for the real example.

**Runtime `metadata.yaml`** (OpenShift — note: no `podTemplateExecutions`):

```yaml
name: my-service
version: "1.0.0"
resources:
  cpu: 4
  memory: 8589934592               # bytes (8 GiB)
  storage: 10737418240             # bytes (10 GiB)
```

> Unlike Podman, there is no `podTemplateExecutions` field — Helm applies all files in `templates/` as one release. See [`assets/services/summarize/openshift/metadata.yaml`](assets/services/summarize/openshift/metadata.yaml).

**`values.yaml`** (OpenShift — Helm values, typically includes resource requests/limits):

```yaml
myservice:
  image: registry.example.com/my-org/my-service:v1.0.0
  log_level: "INFO"
  resources:
    requests:
      cpu: "500m"
      memory: "512Mi"
    limits:
      cpu: "500m"
      memory: "512Mi"
```

> Unlike the Podman `values.yaml`, the OpenShift version typically embeds Kubernetes-style `resources.requests/limits` directly in values. See [`assets/services/summarize/openshift/values.yaml`](assets/services/summarize/openshift/values.yaml).

**Template variable differences (Podman vs OpenShift):**

| Concept | Podman (`.yaml.tmpl`) | OpenShift (Helm) |
|---|---|---|
| Instance identifier | `{{ .InstanceSlug }}` | `{{ .Release.Name }}` |
| Template identity label | `{{ .TemplateID }}` | `{{ .Values.templateID }}` |
| Chart/app version | _(not available)_ | `{{ .Chart.AppVersion }}` |
| Values access | `{{ .Values.x }}` | `{{ .Values.x }}` |
| Conditional | `{{- if .Values.x }}` | `{{- if .Values.x }}` |

> **Reference — full OpenShift file tree for `summarize`:**
> [`assets/services/summarize/openshift/Chart.yaml`](assets/services/summarize/openshift/Chart.yaml) ·
> [`assets/services/summarize/openshift/metadata.yaml`](assets/services/summarize/openshift/metadata.yaml) ·
> [`assets/services/summarize/openshift/values.yaml`](assets/services/summarize/openshift/values.yaml) ·
> [`assets/services/summarize/openshift/templates/`](assets/services/summarize/openshift/templates/) — Deployment, Service, Route, StatefulSet, PVC, Secret

---

### 2.4 Packaging as `.tar.gz`

Pack the service or component directory into a `.tar.gz` archive. The archive must contain **exactly one top-level directory** — the top-level directory name does not matter.

```bash
tar -czf my-bundle.tar.gz my-service/
```

**Size limits:**
- Maximum compressed archive: **20 MB**
- Maximum uncompressed per-file size: **50 MB**

---

## 3. Supported Services and Components

### 3.1 Built-in Services

The following services ship with the platform and are loaded from the embedded catalog. You cannot create a bundle with any of these IDs (they are reserved — see [§7](#7-reserved-ids)).

| Service ID   | Description                             | Assets |
|--------------|-----------------------------------------|--------|
| `chat`       | Question and answer chatbot             | [`assets/services/chat/`](assets/services/chat/) |
| `digitize`   | Document digitization and indexing      | [`assets/services/digitize/`](assets/services/digitize/) |
| `similarity` | Semantic similarity search              | [`assets/services/similarity/`](assets/services/similarity/) |
| `summarize`  | Document summarization                  | [`assets/services/summarize/`](assets/services/summarize/) |
| `extract`    | Information extraction                  | [`assets/services/extract/`](assets/services/extract/) |
| `translate`  | Language translation                    | [`assets/services/translate/`](assets/services/translate/) |

Custom services can reference built-in architectures (e.g. `rag`) and declare dependencies on any of the four supported component types: `llm`, `embedding`, `reranker`, `vector_db`.

### 3.2 Built-in Components by Runtime

The platform ships the following component providers. These IDs are reserved and cannot be overwritten by a bundle.

#### LLM providers (`component_type: llm`)

| Provider ID  | Runtime  | Default Port | Description                                         | Assets |
|--------------|----------|--------------|-----------------------------------------------------|--------|
| `vllm-cpu`   | Podman / OpenShift | `8000` | vLLM inference on CPU / ppc64le                | [`assets/components/llm/vllm-cpu/`](assets/components/llm/vllm-cpu/) |
| `vllm-spyre` | Podman / OpenShift | `8000` | vLLM inference on IBM Spyre accelerator cards  | [`assets/components/llm/vllm-spyre/`](assets/components/llm/vllm-spyre/) |
| `watsonx`    | Podman / OpenShift | `8000` | LiteLLM proxy forwarding to IBM watsonx.ai     | [`assets/components/llm/watsonx/`](assets/components/llm/watsonx/) |

**Key `values.yaml` fields for LLM providers:**

| Provider     | Configurable values | Reference `values.yaml` |
|--------------|---------------------|-------------------------|
| `vllm-cpu`   | `image`, `model`, `apiKey`, `maxNumBatchedTokens`, `maxModelLen`, `maxBatchSize`   | [`assets/components/llm/vllm-cpu/podman/values.yaml`](assets/components/llm/vllm-cpu/podman/values.yaml) |
| `vllm-spyre` | `image`, `model`, `apiKey`, `maxModelLen`, `maxBatchSize`                           | [`assets/components/llm/vllm-spyre/podman/values.yaml`](assets/components/llm/vllm-spyre/podman/values.yaml) |
| `watsonx`    | `image`, `model`, `watsonxApiKey`, `watsonxProjectId`, `watsonxUrl`, `maxModelLen`, `maxBatchSize` | [`assets/components/llm/watsonx/podman/values.yaml`](assets/components/llm/watsonx/podman/values.yaml) |

#### Embedding providers (`component_type: embedding`)

| Provider ID | Runtime  | Default Port | Description                    | Assets |
|-------------|----------|--------------|--------------------------------|--------|
| `vllm-cpu`  | Podman / OpenShift | `8001` | vLLM embedding on CPU | [`assets/components/embedding/vllm-cpu/`](assets/components/embedding/vllm-cpu/) |

**Key `values.yaml` fields:** `image`, `model`, `maxModelLen` — see [`assets/components/embedding/vllm-cpu/podman/values.yaml`](assets/components/embedding/vllm-cpu/podman/values.yaml)

#### Reranker providers (`component_type: reranker`)

| Provider ID  | Runtime  | Default Port | Description                               | Assets |
|--------------|----------|--------------|-------------------------------------------|--------|
| `vllm-cpu`   | Podman / OpenShift | `8002` | vLLM reranker on CPU             | [`assets/components/reranker/vllm-cpu/`](assets/components/reranker/vllm-cpu/) |
| `vllm-spyre` | Podman / OpenShift | `8002` | vLLM reranker on Spyre cards     | [`assets/components/reranker/vllm-spyre/`](assets/components/reranker/vllm-spyre/) |

**Key `values.yaml` fields:** `image`, `model` — see [`assets/components/reranker/vllm-cpu/podman/values.yaml`](assets/components/reranker/vllm-cpu/podman/values.yaml)

#### Vector-store providers (`component_type: vector_db`)

| Provider ID  | Runtime  | Default Port | Description                         | Assets |
|--------------|----------|--------------|-------------------------------------|--------|
| `opensearch` | Podman / OpenShift | `9200` | OpenSearch vector database  | [`assets/components/vector_db/opensearch/`](assets/components/vector_db/opensearch/) |

**Key `values.yaml` fields:** `image`, `memoryLimit`, `auth.username`, `auth.password` (auto-generated) — see [`assets/components/vector_db/opensearch/podman/values.yaml`](assets/components/vector_db/opensearch/podman/values.yaml)

---

## 4. Template Reference (Podman)

> **Scope: Podman only.** The template values, labels, and annotations in this section apply to the Podman runtime, which uses Go-template `.yaml.tmpl` files. OpenShift custom service templates use Helm charts with a different rendering pipeline.

### 4.1 Built-in Template Context Variables

These variables are injected into every `.yaml.tmpl` file by the template engine. They are **not** defined in `values.yaml` and cannot be overridden.

| Variable | Type | Description |
|---|---|---|
| `{{ .InstanceSlug }}` | string | Unique slug for the deployed instance (e.g. `my-service-abc123`). Use it to name all pods, secrets, and volumes. |
| `{{ .TemplateID }}` | string | Fully qualified template identifier (e.g. `services/my-service`). Stamped onto every resource as the `ai-services.io/template` label. |
| `{{ .BaseDir }}` | string | Host filesystem base directory (e.g. `/opt/ai-services`). Use to resolve host-path mounts such as `models/`. |
| `{{ .Values }}` | object | Root values object populated from `values.yaml` and user overrides. Access with `{{ .Values.<key> }}`. |

### 4.2 Lifecycle Labels

Place these labels on every Pod and Secret `metadata.labels`. They are read by the runtime to manage the resource lifecycle.

| Label | Required | Description |
|---|---|---|
| `ai-services.io/template` | **yes** | Must be present on every resource emitted by a template. Set to `"{{ .TemplateID }}"`. |
| `ai-services.io/secret` | **required to enable cleanup** | Comma-separated secret name(s) owned by this pod. The runtime registers these secrets for provisioning on startup and **deletion on teardown. If this label is absent, the runtime has no knowledge of the secret and will not delete it when the service is removed.** |
| `ai-services.io/secret-skip-cleanup` | no | Set to `"true"` on the same pod to prevent the listed secrets from being deleted on teardown. Use for credentials that must survive service restarts (e.g. database passwords). |
| `ai-services.io/volume` | **required to enable cleanup** | Comma-separated PVC or secret-volume name(s) owned by this pod. The runtime provisions these volumes before startup and **deprovisions them on teardown. If this label is absent, the runtime has no knowledge of the volume and will not remove it when the service is removed.** |

> ⚠️ **`ai-services.io/secret` and `ai-services.io/volume` are the runtime's only source of truth for what to clean up.** Any secret or volume not listed in these labels is invisible to the runtime — it will not be created, managed, or deleted on your behalf. Always declare every secret and volume your template creates.

**Example — service pod that owns its own database secret and volume:**

```yaml
labels:
  ai-services.io/template: "{{ .TemplateID }}"
  ai-services.io/secret: "myservice-db-secret-{{ .InstanceSlug }}"
  ai-services.io/secret-skip-cleanup: "true"
  ai-services.io/volume: "postgres-myservice-{{ .InstanceSlug }},myservice-db-secret-{{ .InstanceSlug }}"
```

> The `summarize` secret template uses this exact pattern. See [`assets/services/summarize/podman/templates/postgres-secret.yaml.tmpl`](assets/services/summarize/podman/templates/postgres-secret.yaml.tmpl).

**Example — simple service pod with no secrets or volumes:**

```yaml
labels:
  ai-services.io/template: "{{ .TemplateID }}"
```

> The `summarize` main service pod (`summarize-api`) creates no secrets or volumes of its own, so only the required `ai-services.io/template` label is present — no `secret` or `volume` label needed. See [`assets/services/summarize/podman/templates/summarize-api.yaml.tmpl`](assets/services/summarize/podman/templates/summarize-api.yaml.tmpl).

### 4.3 Routing Annotations (Services)

Only service pods carry routing annotations. The Caddy reverse-proxy reads these to wire up UI and API endpoints.

| Annotation | Description |
|---|---|
| `ai-services.io/routes` | Comma-separated `<containerPort>:<caddy-upstream-name>:<role>` tuples. `role` is `ui` (browser-facing) or `api` (machine-facing). |
| `ai-services.io/ports` | Comma-separated `<hostPort>:<containerPort>` tuples. Exposes the port directly on the host, bypassing Caddy. Commented out by default. |

**Example — service with a UI on port 3000 and an API on port 5000:**

```yaml
annotations:
  ai-services.io/routes: "3000:my-service-ui-{{ .InstanceSlug }}:ui,5000:my-service-api-{{ .InstanceSlug }}:api"
  # ai-services.io/ports: "{{ .Values.myservice.uiPort }}:3000,{{ .Values.myservice.apiPort }}:5000"
```

**Example — API-only service:**

```yaml
annotations:
  ai-services.io/routes: "8080:my-service-api-{{ .InstanceSlug }}:api"
```

> The `summarize` service is API-only and routes port `6000` to the `summarize-api` upstream. See the `annotations` block in [`assets/services/summarize/podman/templates/summarize-api.yaml.tmpl`](assets/services/summarize/podman/templates/summarize-api.yaml.tmpl).

### 4.4 Injected Dependency Values (Services)

When a service declares dependencies in its top-level `metadata.yaml`, the engine injects connection details for each resolved component under well-known keys inside `.Values`. These keys are **read-only** — do not define them in `values.yaml`.

This mechanism works identically whether the resolved component is a **built-in provider** (e.g. `vllm-cpu`) or a **custom bundle component** (e.g. your own `llm--my-provider`). The injection key is always the **component type**, not the specific provider ID. So if a user selects your custom LLM provider, the service template still accesses it via `.Values.llm.host`, `.Values.llm.port`, etc. — the same paths as for any other LLM provider.

**LLM** (`dependencies: [{id: llm}]`)

| Value path | Type | Description |
|---|---|---|
| `.Values.llm.host` | string | Hostname of the running LLM pod. |
| `.Values.llm.port` | string | Container port (default `8000`). |
| `.Values.llm.model` | string | Served model name. |
| `.Values.llm.maxModelLen` | int | Maximum token context length. |
| `.Values.llm.maxBatchSize` | int | Maximum concurrent batch size. |
| `.Values.llm.apiKey` | string | Optional API key (empty string when not set). |
| `.Values.llm.instanceSlug` | string | Instance slug of the resolved LLM component. |

**Embedding** (`dependencies: [{id: embedding}]`)

| Value path | Type | Description |
|---|---|---|
| `.Values.embedding.host` | string | Hostname of the embedding pod. |
| `.Values.embedding.port` | string | Container port (default `8001`). |
| `.Values.embedding.model` | string | Served embedding model name. |
| `.Values.embedding.maxModelLen` | int | Maximum sequence length. |

**Reranker** (`dependencies: [{id: reranker}]`)

| Value path | Type | Description |
|---|---|---|
| `.Values.reranker.host` | string | Hostname of the reranker pod. |
| `.Values.reranker.port` | string | Container port (default `8002`). |
| `.Values.reranker.model` | string | Served reranker model name. |

**Vector store** (`dependencies: [{id: vector_store}]`)

| Value path | Type | Description |
|---|---|---|
| `.Values.vector_store.host` | string | Hostname of the vector-store pod. |
| `.Values.vector_store.port` | string | Container port (default `9200` for OpenSearch). |
| `.Values.vector_store.instanceSlug` | string | Instance slug of the resolved vector-store component. |

> **Custom components work the same way.** If a user deploys a service backed by a custom `llm--my-provider` bundle, the service template still accesses it via `.Values.llm.host`, `.Values.llm.port`, etc. The injection key is always the component **type** (`llm`, `embedding`, `reranker`, `vector_store`), never the provider ID. Your service template needs no changes to work with built-in or custom components interchangeably.

### 4.5 `podTemplateExecutions` Execution Order

`podTemplateExecutions` in the runtime `metadata.yaml` tells the engine **which template files to apply and in what order** when a service or component is deployed. It is a list of layers, where each layer is itself a list of template filenames:

- Templates in the **same layer** (inner list) are applied **concurrently**.
- Layers are applied **sequentially** — the next layer only starts once all templates in the current layer have completed.
- **Every `.yaml.tmpl` file in your `templates/` directory must be named here.** Any template file not listed in `podTemplateExecutions` is never rendered and never applied.

```yaml
# Three-layer pattern — secret, then dependency, then service
podTemplateExecutions:
  - [myservice-db-secret.yaml.tmpl]   # layer 1: credential secret created first
  - [postgres.yaml.tmpl]              # layer 2: database pod starts after secret is ready
  - [myservice.yaml.tmpl]             # layer 3: service pod starts after database is ready
```

> This is exactly the pattern used by `summarize`. See [`assets/services/summarize/podman/metadata.yaml`](assets/services/summarize/podman/metadata.yaml):
> ```yaml
> podTemplateExecutions:
>   - [postgres-secret.yaml.tmpl]   # layer 1
>   - [postgres.yaml.tmpl]          # layer 2
>   - [summarize-api.yaml.tmpl]     # layer 3
> ```
> All three files under [`assets/services/summarize/podman/templates/`](assets/services/summarize/podman/templates/) are listed — none are omitted.

Templates in the same layer run concurrently:

```yaml
# Two secrets created in parallel, then two pods started in parallel
podTemplateExecutions:
  - [secret-a.yaml.tmpl, secret-b.yaml.tmpl]
  - [service-a.yaml.tmpl, service-b.yaml.tmpl]
```

> ⚠️ **If you add a new template file to `templates/`, you must also add it to `podTemplateExecutions`.** A file that is present on disk but absent from this list will never be rendered.

Services with only a single template file still require it to be listed:

```yaml
podTemplateExecutions:
  - [my-service.yaml.tmpl]
```

### 4.6 `@generate` Directive

A `# @generate` comment immediately before a `values.yaml` field instructs the engine to produce a value at deploy time.

| Directive | Behaviour |
|---|---|
| `# @generate:password` | Generates a cryptographically random password before template rendering. The value is persisted so restarts reuse the same credential. |

```yaml
# values.yaml — auto-generate a database password
postgres:
  username: "postgres"
  # @generate:password
  password: ""
```

> The `summarize` service uses `@generate:password` on its postgres `password` field. See [`assets/services/summarize/podman/values.yaml`](assets/services/summarize/podman/values.yaml).

### 4.7 `values.schema.json` — User-Configurable Parameters

An optional `values.schema.json` (JSON Schema draft-07) declares which fields the user can configure through the catalog UI. Fields absent from the schema are treated as internal defaults and are not surfaced in the UI.

Three custom `x-ui-*` extensions control form rendering:

| Keyword | Description |
|---|---|
| `x-ui-only` | Rendered in the UI form but **not** passed into templates. Use for toggle controls. |
| `x-ui-controls` | Names the field whose UI visibility this field gates. |
| `x-ui-controlled-by` | This field is hidden in the UI unless the named field is `true`. |

**Example:**

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "myservice": {
      "type": "object",
      "properties": {
        "enableAdvanced": {
          "type": "boolean",
          "title": "Enable advanced configuration",
          "x-ui-only": true,
          "x-ui-controls": "log_level"
        },
        "log_level": {
          "type": "string",
          "title": "Log level",
          "default": "INFO",
          "enum": ["DEBUG", "INFO", "WARN", "ERROR"],
          "x-ui-controlled-by": "enableAdvanced"
        }
      }
    }
  }
}
```

---

## 5. Managing Bundles with the CLI

The `ai-services catalog bundle` subcommand group handles all bundle lifecycle operations. All subcommands require an active catalog session (see Step 1 below).

### 5.1 Prerequisites

**Step 1 — Log in to the catalog backend (once per session):**

```bash
./bin/ai-services catalog login \
  --server https://<catalog-backend-endpoint> \
  --username admin \
  --runtime podman \
  --insecure
```

Credentials are stored locally; subsequent `bundle` commands pick them up automatically — no `--server` or `--username` flags needed.

**Step 2 — Package your bundle:**

```bash
tar -czf my-bundle.tar.gz my-service/
```

### 5.2 Create a Bundle

Creates a new bundle from a `.tar.gz` archive. The command is synchronous — it blocks until the server returns `201 Created` and the bundle is `active`. All metadata (`id`, `type`, `version`) is read from `metadata.yaml` inside the archive; no extra flags are needed.

```bash
./bin/ai-services catalog bundle create --file my-bundle.tar.gz
```

**Example output (service bundle):**

```
Creating bundle from my-bundle.tar.gz...
✓ Bundle created successfully
  ID:           550e8400-e29b-41d4-a716-446655440000
  Catalog type: service
  Catalog ID:   my-service
  Dir name:     my-service-1.0.0
  Version:      1.0.0
  Status:       active
  Size:         280 KB
```

**Example output (component bundle with `component_type: llm`):**

```
Creating bundle from my-provider-bundle.tar.gz...
✓ Bundle created successfully
  ID:             c3d4e5f6-...
  Catalog type:   component
  Component type: llm
  Catalog ID:     llm--my-provider
  Version:        1.0.0
  Status:         active
  Size:           192 KB
```

**Common errors:**

| Condition | Error |
|---|---|
| Bundle with same `catalog_id` already exists | `409 Conflict` — use `bundle update` instead |
| `metadata.yaml` missing, malformed, or ID is reserved | `422 Unprocessable Entity` |
| File not found or not a `.tar.gz` | Exit `1` immediately |

### 5.3 Validate a Bundle Before Creating

Validates the archive structure and `metadata.yaml` without writing anything to disk or the database. Use this in CI/CD pipelines before promoting to a live deployment.

```bash
./bin/ai-services catalog bundle validate --file my-bundle.tar.gz
```

**Example output (valid service bundle):**

```
Validating bundle from my-bundle.tar.gz...
✓ Bundle is valid
  Catalog type: service
  Catalog ID:   my-service
  Dir name:     my-service-1.0.0
  Version:      1.0.0
  Name:         My Custom Service
```

**Example output (valid component bundle):**

```
Validating bundle from my-provider-bundle.tar.gz...
✓ Bundle is valid
  Catalog type:   component
  Component type: llm
  Catalog ID:     llm--my-provider
  Version:        1.0.0
  Name:           My Custom LLM Provider
```

**Example output (invalid bundle):**

```
Validating bundle from broken-bundle.tar.gz...
✗ Bundle validation failed: metadata.yaml is missing required field "component_type"
```

### 5.4 Update a Bundle

Replaces an existing bundle by its internal UUID. The `bundle_id` is the UUID shown in `bundle list` or the `bundle create` output. The replacement `metadata.yaml` inside the archive must carry the same `id`, `type`, and (for components) `component_type` as the existing record — mismatches are rejected with `422`.

> **Important:** If any service or component is currently running that was deployed from this bundle, the update is rejected with `409 Conflict`. Stop or delete those running instances first, then retry.

```bash
./bin/ai-services catalog bundle update <bundle_id> --file my-bundle-v2.tar.gz
```

**Example:**

```bash
./bin/ai-services catalog bundle update 550e8400-e29b-41d4-a716-446655440000 \
  --file my-bundle-v2.tar.gz
```

**Example output:**

```
Updating bundle 550e8400-e29b-41d4-a716-446655440000 from my-bundle-v2.tar.gz...
  Catalog type: service  |  Catalog ID: my-service  |  Version: 2.0.0
  Dir name:     my-service-2.0.0
✓ Bundle updated successfully (status: active)
```

### 5.5 List Bundles

Prints a table of all registered bundles, ordered by creation time (most recent first).

```bash
./bin/ai-services catalog bundle list
```

**Example output:**

```
ID                                     CATALOG TYPE  CATALOG ID        VERSION  STATUS  CREATED AT
550e8400-e29b-41d4-a716-446655440000   service       my-service        1.0.0    active  2026-05-12 09:14:02
a1b2c3d4-e5f6-7890-abcd-ef1234567890   component     llm--my-provider  1.0.0    active  2026-05-13 11:30:00
```

### 5.6 Get Bundle Details

Prints full details for a single bundle by its UUID.

```bash
./bin/ai-services catalog bundle get <bundle_id>
```

**Example output:**

```
ID:           550e8400-e29b-41d4-a716-446655440000
Name:         My Custom Service
Dir name:     my-service-1.0.0
Catalog type: service
Catalog ID:   my-service
Version:      1.0.0
Status:       active
Size:         280 KB
Created by:   admin
Created at:   2026-05-12 09:14:02
```

### 5.7 Delete a Bundle

Permanently removes a bundle: deletes the on-disk directory, triggers a `CatalogProvider` reload so the item is no longer served, and removes the database record.

> **Important:** If any service or component is currently running from this bundle, the delete is rejected with `409 Conflict`. Stop or delete those running instances first.

```bash
./bin/ai-services catalog bundle delete <bundle_id>
```

Use `--yes` to skip the confirmation prompt (useful in scripts):

```bash
./bin/ai-services catalog bundle delete 550e8400-e29b-41d4-a716-446655440000 --yes
```

**Example output:**

```
Delete bundle 550e8400-e29b-41d4-a716-446655440000 (my-service-1.0.0)? [y/N] y
✓ Bundle deleted.
```

> Deleting a bundle does **not** affect any applications that were already deployed from it — existing running pods are independent of the catalog once launched.

---

## 6. Viewing Custom Services in the Catalog UI

Once a bundle is successfully created and its status is `active`, the custom service or component is immediately available in the platform — no restart is needed.

**Step 1 — Open the Catalog UI.**

Navigate to the Catalog UI endpoint (output when you ran `catalog configure`):

```bash
./bin/ai-services catalog info --runtime podman
```

This prints the Catalog UI URL.

**Step 2 — Go to the Services Catalog page.**

In the Catalog UI, click **Services Catalog** in the left navigation. Custom services appear alongside the built-in ones, each showing the `name` from your `metadata.yaml`.

**Step 3 — Deploy an application from the custom service.**

Select your custom service from the catalog, configure any parameters exposed by `values.schema.json`, and deploy. You can also use the CLI:

```bash
# List all available service templates, including custom ones
./bin/ai-services application templates --runtime podman

# View parameters for your custom service
./bin/ai-services application templates parameters \
  --template my-service \
  --runtime podman

# Create an application
./bin/ai-services application create my-deployment \
  --template my-service \
  --runtime podman
```

**Verifying the bundle is loaded via the API:**

```bash
# List all services — custom ones appear alongside built-in services
curl -s https://<catalog-backend-endpoint>/api/v1/services \
  -H "Authorization: Bearer $(cat token.txt)" | jq '.[].id'
# "chat"
# "digitize"
# "similarity"
# "summarize"
# "my-service"   ← your custom service
```

---

## 7. Reserved IDs

A bundle whose `catalog_id` matches a built-in catalog item is rejected with `422 Unprocessable Entity`. Choose a unique `id` that does not appear in the following lists.

**Reserved service IDs:** `chat`, `digitize`, `similarity`, `summarize`, `extract`, `translate`, `rag`

**Reserved component IDs** (composite `<component_type>--<id>` format):

| `component_type` | Reserved IDs |
|---|---|
| `llm` | `llm--vllm-cpu`, `llm--vllm-spyre`, `llm--watsonx` |
| `embedding` | `embedding--vllm-cpu` |
| `reranker` | `reranker--vllm-cpu`, `reranker--vllm-spyre` |
| `vector_db` | `vector_db--opensearch` |

---

## 8. Tips and Troubleshooting

**Bundle rejected with `409 Conflict` on `bundle create`**

A bundle with the same `catalog_id` already exists. Use `bundle list` to find its ID and `bundle update` to replace it:

```bash
./bin/ai-services catalog bundle list
./bin/ai-services catalog bundle update <bundle_id> --file my-bundle-v2.tar.gz
```

**Bundle rejected with `422 Unprocessable Entity`**

Common causes:
- `metadata.yaml` is missing from the archive root.
- A required field (`id`, `type`, `version`, or `component_type` for components) is missing.
- The `catalog_id` matches a reserved built-in ID (see [§7](#7-reserved-ids)).
- The `podman/metadata.yaml` or `values.yaml` is missing.
- A template file has a syntax error.

Run `bundle validate` first to catch errors before uploading:

```bash
./bin/ai-services catalog bundle validate --file my-bundle.tar.gz
```

**Bundle rejected with `409 Conflict` on `bundle update` or `bundle delete`**

A running service or component is using this bundle's `catalog_id`. Stop or delete the running application first:

```bash
./bin/ai-services application ps --runtime podman
./bin/ai-services application delete <app-name> --runtime podman
# Then retry the update or delete
```

**Custom service does not appear in the UI after upload**

Check that the bundle status is `active`:

```bash
./bin/ai-services catalog bundle list
```

If the status is `failed`, get more details:

```bash
./bin/ai-services catalog bundle get <bundle_id>
```

A `failed` status means an error occurred during extraction or reload after the archive was accepted. Fix the issue and re-upload using `bundle update`.

**Getting help**

```bash
./bin/ai-services catalog bundle --help
./bin/ai-services catalog bundle create --help
./bin/ai-services catalog bundle validate --help
```
