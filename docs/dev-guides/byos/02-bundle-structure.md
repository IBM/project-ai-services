# BYOS — Bundle Structure and Templates

This guide covers how to structure and package a custom catalog bundle. For background on services, components, and reserved IDs, see [01 — Overview](01-overview.md). Once your `.tar.gz` is ready, see [03 — Integrating Bundles into AI-Services](03-integrating-bundles.md).

---

## Table of Contents

1. [Bundle Structure](#1-bundle-structure)
   - 1.1 [Top-level `metadata.yaml`](#11-top-level-metadatayaml-shared-by-both-runtimes)
   - 1.2 [Podman Bundle Structure](#12-podman-bundle-structure)
   - 1.3 [OpenShift Bundle Structure](#13-openshift-bundle-structure)
   - 1.4 [Steps Folder (Optional)](#14-steps-folder-optional)
   - 1.5 [Packaging as `.tar.gz`](#15-packaging-as-targz)
2. [Template Reference (Podman)](#2-template-reference-podman)
   - 2.1 [Built-in Template Context Variables](#21-built-in-template-context-variables)
   - 2.2 [Lifecycle Labels](#22-lifecycle-labels)
   - 2.3 [Routing Annotations (Services)](#23-routing-annotations-services)
   - 2.4 [Injected Dependency Values (Services)](#24-injected-dependency-values-services)
   - 2.5 [`podTemplateExecutions` Execution Order](#25-podtemplateexecutions-execution-order)
   - 2.6 [`@generate` Directive](#26-generate-directive)
   - 2.7 [`values.schema.json` — User-Configurable Parameters](#27-valuesschemajson--user-configurable-parameters)

---

## 1. Bundle Structure

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

### 1.1 Top-level `metadata.yaml` (shared by both runtimes)

The top-level `metadata.yaml` sits at the archive root and is **identical regardless of runtime**. It is always required and declares the identity of the catalog item.

**Service:**

```yaml
id: my-service                   # unique; must not conflict with built-in IDs
name: "My Custom Service"        # human-readable display name
description: "A custom AI service for internal workloads"
type: service                    # must be "service"
certified_by: "ISV"
dependencies:
  - id: llm                      # component types this service requires
standalone: true
```

> **Note:** Custom services cannot be attached to the built-in Digital Assistant (`rag`) architecture. The `architectures` field is omitted intentionally — only platform-built services are wired into named architectures.

> **Note:** `certified_by: "IBM"` is not supported for ISV bundles.

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

### 1.2 Podman Bundle Structure

Under `podman/`, templates are **Go template files** (`.yaml.tmpl`) that render directly to Podman pod specs. The runtime `metadata.yaml` must include a `podTemplateExecutions` list naming **every template file** and controlling deployment order.

**Service bundle (Podman):**

```
my-service/
├── metadata.yaml                    ← shared top-level (see §1.1)
└── podman/
    ├── metadata.yaml                ← required: name, version, resources, podTemplateExecutions
    ├── values.yaml                  ← required: default parameter values
    ├── values.schema.json           ← optional: user-configurable parameters for the UI
    ├── steps/                       ← optional: post-deploy messaging (see §1.4)
    │   ├── next.md                  ← shown after deploy
    │   ├── info.md                  ← shown by the info command
    │   └── vars_file.yaml           ← variable bindings for the templates above
    └── templates/
        └── my-service.yaml.tmpl     ← Go template; every file here must appear in podTemplateExecutions
```

**Component bundle (Podman):**

```
my-provider/
├── metadata.yaml                    ← shared top-level with component_type (see §1.1)
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

### 1.3 OpenShift Bundle Structure

Under `openshift/`, templates are a **Helm chart** — standard Kubernetes YAML files rendered by Helm. A `Chart.yaml` is required. There is **no `podTemplateExecutions`** in the runtime `metadata.yaml`; Helm manages all templates as a single release.

**Service bundle (OpenShift):**

```
my-service/
├── metadata.yaml                    ← shared top-level (see §1.1)
└── openshift/
    ├── Chart.yaml                   ← required: Helm chart descriptor (no Podman equivalent)
    ├── metadata.yaml                ← required: name, version, resources — no podTemplateExecutions
    ├── values.yaml                  ← required: default Helm values
    ├── values.schema.json           ← optional: user-configurable parameters for the UI
    ├── steps/                       ← optional: post-deploy messaging (see §1.4)
    │   ├── next.md                  ← shown after deploy
    │   ├── info.md                  ← shown by the info command
    │   └── vars_file.yaml           ← variable bindings for the templates above
    └── templates/
        ├── my-service-deployment.yaml
        ├── my-service-service.yaml
        └── my-service-route.yaml
```

**Component bundle (OpenShift):**

```
my-provider/
├── metadata.yaml                    ← shared top-level with component_type (see §1.1)
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

### 1.4 Steps Folder (Optional)

A `steps/` directory can be placed under `podman/` or `openshift/` (or both) to display contextual messages to users immediately after deployment and when they run `ai-services application info`. If the directory is absent the platform skips this feature gracefully — no error is produced.

**Files inside `steps/`:**

| File | Purpose |
|---|---|
| `next.md` | Printed once right after a successful deploy, under a **Next Steps** heading. |
| `info.md` | Printed by `ai-services application info …`, under an **Info** heading. |
| `vars_file.yaml` | Declares variable bindings (pod ports, container statuses, route URLs, host IP) that are injected into the templates above before they are rendered. |

All three files are optional — include only the ones you need. The `steps/` directory itself is only meaningful when at least one of these files is present.

#### `next.md` and `info.md` — Go templates

Both files are rendered as `text/template` Go templates. The following variables are always available:

| Variable | Description |
|---|---|
| `{{ .SERVICE_NAME }}` | The `type` of the service as registered in the catalog. |
| `{{ .AppName }}` | The application instance name (e.g. `my-deployment`). |
| `{{ .UI_URL }}` / `{{ .API_URL }}` | Populated automatically from the stored service endpoints when their `type` is `ui` or `api` respectively. |

Any additional variable declared in `vars_file.yaml` under a `pods`, `containers`, or `hosts` entry is also available by its `alias`.

**Example `next.md` — API service:**

```markdown
- {{ .SERVICE_NAME }} API is available at {{ .API_URL }}.

- Run "ai-services application info {{ .AppName }} --runtime podman" to view live endpoint status.
```

**Example `info.md` — service with conditional status:**

```markdown
Day N:

{{- if eq .API_STATUS "running" }}

- {{ .SERVICE_NAME }} API is available at {{ .API_URL }}.
{{- else }}

- {{ .SERVICE_NAME }} API is unavailable. Please make sure the 'my-service' pod is running.
{{- end }}
```

> See [`assets/services/chat/podman/steps/info.md`](assets/services/chat/podman/steps/info.md) and [`assets/services/summarize/podman/steps/info.md`](assets/services/summarize/podman/steps/info.md) for real examples.

#### `vars_file.yaml` — variable bindings

`vars_file.yaml` tells the platform which runtime values to collect and under what alias to expose them in your templates. It is a YAML file with three optional top-level keys: `pods`, `containers`, and `hosts`.

**Podman vs OpenShift differences:**

| Field | Podman | OpenShift |
|---|---|---|
| `pods[].name` | Full Podman pod name; may use `{{ .AppName }}` | Not applicable — omit `pods` entirely |
| `containers[].name` | Full container name; may use `{{ .InstanceSlug }}` | Container name inside the pod (short, e.g. `"ui"`) |
| `containers[].workload` | Not used | **Required** — Deployment name used to prefix-match the OCP pod name (e.g. `"chat-bot-ui"`) |
| `hosts[].type: "ip"` | Resolves the host machine IP into `HOST_IP` | Not applicable |
| `hosts[].type: "route"` | Not applicable | Fetches all OpenShift Routes and exposes each as `<ROUTE_NAME>_ROUTE` |

**`pods` (Podman only) — expose port numbers:**

Each entry inspects a named Podman pod and evaluates a `--format`-style expression to extract a value (typically a host-mapped port).

```yaml
pods:
  - name: "{{ .AppName }}--my-service"       # Podman pod name; AppName is the instance name
    format: "index .Ports \"8080/tcp\" 0"    # extract the first mapped host port for 8080/tcp
    default: ""                              # value to use when the pod is missing or the port is unmapped
    alias: MY_SERVICE_PORT                   # becomes {{ .MY_SERVICE_PORT }} in templates
```

> See [`assets/applications/rag/podman/steps/vars_file.yaml`](assets/applications/rag/podman/steps/vars_file.yaml) for a real multi-service example.

**`containers` — expose container status:**

Each entry inspects a named container and evaluates a format expression. The most common use is polling `.Status` to derive a `"running"` / `""` value for conditional rendering in `info.md`.

_Podman:_

```yaml
containers:
  - name: "my-service-{{ .InstanceSlug }}-my-container"   # full container name
    format: ".Status"
    alias: MY_SERVICE_STATUS   # "running" when healthy, "" otherwise
```

_OpenShift:_

```yaml
containers:
  - name: "my-container"          # container name inside the pod spec (ContainerStatus.Name)
    workload: "my-service"        # Deployment name — OCP pods are named "{workload}-{hash}-{hash}"
    format: ".Status"
    alias: MY_SERVICE_STATUS
```

> See [`assets/services/summarize/podman/steps/vars_file.yaml`](assets/services/summarize/podman/steps/vars_file.yaml) (Podman) and [`assets/services/summarize/openshift/steps/vars_file.yaml`](assets/services/summarize/openshift/steps/vars_file.yaml) (OpenShift) for real examples.

**`hosts` — expose route URLs or host IP:**

_Podman — resolve host machine IP:_

```yaml
hosts:
  - fetch: HOST_IP   # populates {{ .HOST_IP }} in templates
    type: ip
```

_OpenShift — fetch all Routes:_

```yaml
hosts:
  - fetch: MY_SERVICE_ROUTE   # populates {{ .MY_SERVICE_ROUTE }} in templates
    type: route               # fetches all routes; each route name is uppercased and suffixed with _ROUTE
```

> When `type: route` is used, the platform fetches every OpenShift Route in the namespace and exposes them as `<ROUTE_NAME_UPPERCASED>_ROUTE`. For example a Route named `my-service-api` becomes `{{ .MY_SERVICE_API_ROUTE }}`. See [`assets/catalog/openshift/steps/vars_file.yaml`](assets/catalog/openshift/steps/vars_file.yaml) for a real example.

**Complete `vars_file.yaml` — Podman service:**

```yaml
pods:
  - name: "{{ .AppName }}--my-service"
    format: "index .Ports \"8080/tcp\" 0"
    default: ""
    alias: MY_SERVICE_PORT

containers:
  - name: "my-service-{{ .InstanceSlug }}-server"
    format: ".Status"
    alias: MY_SERVICE_STATUS

hosts:
  - fetch: HOST_IP
    type: ip
```

**Complete `vars_file.yaml` — OpenShift service:**

```yaml
containers:
  - name: "server"
    workload: "my-service"
    format: ".Status"
    alias: MY_SERVICE_STATUS

hosts:
  - fetch: MY_SERVICE_ROUTE
    type: route
```

> **Reference — full steps trees:**
> Podman: [`assets/services/chat/podman/steps/`](assets/services/chat/podman/steps/) · [`assets/services/summarize/podman/steps/`](assets/services/summarize/podman/steps/)
> OpenShift: [`assets/services/chat/openshift/steps/`](assets/services/chat/openshift/steps/) · [`assets/services/summarize/openshift/steps/`](assets/services/summarize/openshift/steps/)
> Application (multi-service): [`assets/applications/rag/podman/steps/`](assets/applications/rag/podman/steps/) · [`assets/applications/rag/openshift/steps/`](assets/applications/rag/openshift/steps/)

---

### 1.5 Packaging as `.tar.gz`

Pack the service or component directory into a `.tar.gz` archive. The archive must contain **exactly one top-level directory** — the top-level directory name does not matter.

```bash
tar -czf my-bundle.tar.gz my-service/
```

**Size limits:**
- Maximum compressed archive: **20 MB**
- Maximum uncompressed per-file size: **50 MB**

---

## 2. Template Reference (Podman)

> **Scope: Podman only.** The template values, labels, and annotations in this section apply to the Podman runtime, which uses Go-template `.yaml.tmpl` files. OpenShift custom service templates use Helm charts with a different rendering pipeline.

### 2.1 Built-in Template Context Variables

These variables are injected into every `.yaml.tmpl` file by the template engine. They are **not** defined in `values.yaml` and cannot be overridden.

| Variable | Type | Description |
|---|---|---|
| `{{ .InstanceSlug }}` | string | Unique slug for the deployed instance (e.g. `my-service-abc123`). Use it to name all pods, secrets, and volumes. |
| `{{ .TemplateID }}` | string | Fully qualified template identifier (e.g. `services/my-service`). Stamped onto every resource as the `ai-services.io/template` label. |
| `{{ .BaseDir }}` | string | Host filesystem base directory (e.g. `/opt/ai-services`). Use to resolve host-path mounts such as `models/`. |
| `{{ .Values }}` | object | Root values object populated from `values.yaml` and user overrides. Access with `{{ .Values.<key> }}`. |

### 2.2 Lifecycle Labels

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

> The `summarize` main service pod (`summarize-api`) creates no secrets or volumes of its own, so only the required `ai-services.io/template` label is present. See [`assets/services/summarize/podman/templates/summarize-api.yaml.tmpl`](assets/services/summarize/podman/templates/summarize-api.yaml.tmpl).

### 2.3 Routing Annotations (Services)

Only service pods carry routing annotations. The Caddy reverse-proxy reads these to wire up UI and API endpoints.

| Annotation | Description |
|---|---|
| `ai-services.io/routes` | Comma-separated `<containerPort>:<caddy-upstream-name>:<role>` tuples. |
| `ai-services.io/ports` | Comma-separated `<hostPort>:<containerPort>` tuples. Exposes the port directly on the host, bypassing Caddy. Commented out by default. |

Each tuple in `ai-services.io/routes` has exactly three colon-separated fields:

| Field | Description |
|---|---|
| `containerPort` | The port the container listens on inside the pod (e.g. `3000`). Caddy forwards inbound HTTPS traffic to this port on the pod. |
| `caddy-upstream-name` | The subdomain Caddy registers for this endpoint. Must be unique per deployment — include `{{ .InstanceSlug }}` to avoid collisions between instances (e.g. `my-service-ui-{{ .InstanceSlug }}`). |
| `role` | `ui` for browser-facing endpoints; `api` for machine-facing endpoints. The platform uses this to classify and store the endpoint URL after deploy. |

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

### 2.4 Injected Dependency Values (Services)

When a service declares dependencies in its top-level `metadata.yaml`, the engine injects connection details for each resolved component under well-known keys inside `.Values`. These keys are **read-only** — do not define them in `values.yaml`.

This mechanism works identically whether the resolved component is a **built-in provider** (e.g. `vllm-cpu`) or a **custom bundle component** (e.g. your own `llm--my-provider`). The injection key is always the **component type**, not the specific provider ID.

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

> **Custom components work the same way.** The injection key is always the component **type** (`llm`, `embedding`, `reranker`, `vector_store`), never the provider ID. Your service template needs no changes to work with built-in or custom components interchangeably.

### 2.5 `podTemplateExecutions` Execution Order

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

### 2.6 `@generate` Directive

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

### 2.7 `values.schema.json` — User-Configurable Parameters

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
