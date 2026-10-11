# Proposal: Service–Model Contracts

- **Status:** Draft / Proposal
- **Created**: October 2026
- **Prerequisite**: [model-profiles.md](./model-profiles.md) — the profile vocabulary
  (`standard`, `balanced`, `compact-4k`, `compact-8k`, `compact-16k`, `minimal`) used
  throughout this document is defined there and must be implemented first.

---

## 1. Problem Statement

The [hardware-profile proposal](./model-profiles.md) fixes how parameters are
resolved for a given profile. This proposal addresses a separate, higher-level
gap that profiles alone do not solve:

### Gap 1 — Every service accepts every model, regardless of fit

`values.schema.json` for `llm/vllm-spyre` lists all supported models and is
**shared across all services**. There is nothing that prevents a user configuring
the `extract` service (short-context, single-card workload) with a profile
designed for the `chat` service (long-context, 4-card RAG workload). The IBM
Spyre documentation is explicit that these are different validated configurations:

| Service workload | Cards | Max context | Batch |
|---|---|---|---|
| RAG inferencing (chat, similarity, summarize) | 4 | 32,768 | 32 |
| Entity extraction (extract, translate) | 1 | 3,072–4,096 | 16–32 |

Today there is nothing in `services/extract/metadata.yaml` that records this
distinction or enforces it at deploy time.

### Gap 2 — Services do not own their model list

The set of models a service supports is today implicitly the full model list
from the component schema. If a new model is added to the LLM component
(e.g. `Mistral-Small-3.2-24B`), it immediately becomes selectable in all
services — including ones that have never been tested with it. There is no
place to record "this model is supported by the Extract service at this
profile" as a first-class fact.

---

## 2. Goal

Give each service a `models` section in its **top-level** `metadata.yaml` that:

1. Declares which models it supports, scoped per component type.
2. Binds each supported model to the exact hardware profile ID (from
   `profiles.yaml`) that governs its deployment.
3. Lets the catalog backend drive `values.schema.json` generation from this
   contract, so the model list a user sees is always consistent with what the
   service has declared it supports.

---

## 3. Where does the contract live?

### Same `metadata.yaml`, not a new file

Each service already has two levels of metadata:

```
services/<name>/metadata.yaml            ← service-level (identity, dependencies)
services/<name>/podman/metadata.yaml     ← runtime-level (resources, deploy steps)
services/<name>/openshift/metadata.yaml  ← runtime-level
```

**Model support is service-level, not runtime-level.** The same `extract`
service supports the same models whether deployed on podman or openshift; only
the template rendering differs. The contract therefore belongs in the top-level
`services/<name>/metadata.yaml` alongside `dependencies` — it is a declaration
about what the service *is*, not about how it runs.

Adding a new file (e.g. `models.yaml`) would require a new file-type contract in
the catalog backend reader and provide no structural benefit over a new top-level
key in the existing file. The existing `metadata.yaml` is the right place.

---

## 4. Proposed Design

### 4.1 New `models` section in `services/<name>/metadata.yaml`

Each service gains a top-level `models` key. It is a list because a service can
depend on multiple component types (e.g. `similarity` depends on both `embedding`
and `reranker`).

**Schema:**

```yaml
models:
  - componentType: <llm | embedding | reranker>
    supported:
      - modelId: <huggingface model id>     # must match a const in the component's values.schema.json oneOf
        profile: <profile-id>               # exactly one id from properties.profile.profiles; resolved automatically at deploy time
        default: true                       # optional — marks the pre-selected model in the UI
```

**Rules:**

- `modelId` must match a `const` value already declared in the component's
  `values.schema.json` `oneOf`. The contract narrows, never extends, the
  component's known model list.
- `profile` is a single string that must exactly match an `id` in the component
  provider's `properties.profile.profiles`. It is injected automatically at
  deploy time — the user never selects a profile when deploying through a
  service. No profile dropdown is shown.
- Exactly one entry per `componentType` may set `default: true`. If omitted,
  the first entry is treated as the default.
- For component types with a single validated model and profile (embedding,
  reranker), the contract still declares them explicitly — it acts as a
  constraint gate, not just documentation.

---

### 4.2 Updated `metadata.yaml` for each service

The `models` section is added to each service's **top-level** `metadata.yaml`.
The existing `dependencies`, `about`, and other keys are unchanged.

#### `services/chat/metadata.yaml`

Chat is a RAG inferencing workload — long context, 4 cards.
Valid profiles per `profiles.yaml`: `standard` (4 cards · 32K · batch 32).

```yaml
# Added to services/chat/metadata.yaml
models:
  - componentType: llm
    supported:
      - modelId: ibm-granite/granite-4.1-8b-fp8
        profile: standard
        default: true
      - modelId: ibm-granite/granite-3.3-8b-instruct
        profile: standard
```

#### `services/summarize/metadata.yaml`

Summarize operates over long documents — same RAG inferencing profile as chat.

```yaml
# Added to services/summarize/metadata.yaml
models:
  - componentType: llm
    supported:
      - modelId: ibm-granite/granite-4.1-8b-fp8
        profile: standard
        default: true
      - modelId: ibm-granite/granite-3.3-8b-instruct
        profile: standard
```

#### `services/extract/metadata.yaml`

Extract is a short-context, structured-output workload — 1 card.
`compact-4k` (4,096 tokens, batch 32) is used on RHAIIS 3.6+.

```yaml
# Added to services/extract/metadata.yaml
models:
  - componentType: llm
    supported:
      - modelId: ibm-granite/granite-4.1-8b-fp8
        profile: compact-4k
        default: true
      - modelId: ibm-granite/granite-3.3-8b-instruct
        profile: compact-4k
      - modelId: meta-llama/Llama-3.1-8B-Instruct
        profile: compact-4k
```

#### `services/translate/metadata.yaml`

Translate works sentence- and paragraph-at-a-time via token-aware chunking.
Short-context 1-card workload — same validated configuration as extract.

```yaml
# Added to services/translate/metadata.yaml
models:
  - componentType: llm
    supported:
      - modelId: ibm-granite/granite-4.1-8b-fp8
        profile: compact-4k
        default: true
      - modelId: ibm-granite/granite-3.3-8b-instruct
        profile: compact-4k
```

#### `services/similarity/metadata.yaml`

Similarity depends on `embedding` and `reranker`, not `llm`. Both have a
single validated model and profile — the contract declares them explicitly
so the backend can enforce the constraint.

```yaml
# Added to services/similarity/metadata.yaml
models:
  - componentType: embedding
    supported:
      - modelId: ibm-granite/granite-embedding-278m-multilingual
        profile: standard
        default: true
  - componentType: reranker
    supported:
      - modelId: BAAI/bge-reranker-v2-m3
        profile: standard
        default: true
```

> `digitize` has no LLM dependency and is therefore not given a `models`
> section — it uses a document-processing pipeline, not an inference backend.

---

## 5. How the backend uses the contract

The service `metadata.yaml` is already read by the catalog backend when
building deploy-options responses. The `models` section plugs into two
existing steps.

### Step A — Model list filtering (schema serve time)

When the UI requests component params for a provider in the context of a
specific service, the backend:

1. Loads the component's full `values.schema.json`.
2. Reads `models[componentType].supported[]` from the service's top-level
   `metadata.yaml`.
3. Filters the schema's `model.oneOf` to only those `modelId` values that
   appear in the contract.
4. Returns the filtered schema to the UI.

The user only ever sees models the service has declared. Today this filtering
step does not exist — the full component schema is returned verbatim.

### Step B — Profile injection (deploy time)

The `profile` for a given `(service, model)` pair is read from the contract.
The backend injects it as the effective profile before template rendering.
Because the contract provides the profile, the user never needs to select one
in the service deploy flow — it is an implementation detail resolved
automatically.

```
Service contract says:
  extract + granite-4.1-8b-fp8 → profile: compact-4k

Backend resolves (via model-profiles proposal §5.4):
  compact-4k → numCards=1, maxModelLen=4096, maxBatchSize=32,
               memory="100Gi", shmSize="64Mi"

Template renders:
  AIU_WORLD_SIZE = "1"
  MAX_MODEL_LEN  = "4096"
  MAX_BATCH_SIZE = "32"
  podman.io/device=/dev/vfio: 1
```

> **Standalone LLM deployment** (outside any service) still exposes the
> `profile` dropdown directly, as described in the hardware-profile proposal.
> The service contract only applies when deploying through a named service.

### Step C — Bundle upload validation (BYOS only)

Embedded (IBM-shipped) service assets are trusted by construction — the
`models` section is authored in the same repository as the component schema
and `profiles.yaml`, so consistency is enforced at review time, not at
runtime.

**User-defined service bundles (BYOS)** have no such guarantee. A user could
upload a service bundle whose `models` section references a `modelId` that
does not exist in any loaded component schema, or a `profile` ID that the
named model does not support. These mistakes would silently produce a broken
deploy — wrong hardware, wrong context window, or a template render error.

The check therefore belongs in the **bundle upload validator**, executed when
`POST /api/v1/catalog/bundles` is called, before the bundle is written to disk
or marked `active`. This is already where all other structural validation
happens (see [`bundle/validate/podman.go`](../../ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/podman.go)
and [`bundle/validate/metadata/bundle.go`](../../ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata/bundle.go)).

**What the validator checks, for each entry in `models[*].supported[]`:**

1. The `componentType` matches one of the service's declared `dependencies`.
2. The `modelId` exists as a `const` in the corresponding embedded component's
   `values.schema.json` — a BYOS service bundle cannot declare models the
   platform does not know about.
3. The `profile` ID exists in the component provider's `properties.profile.profiles`.
4. The `profile` ID appears in that model's `x-profiles` list — the
   `(model, profile)` combination is a validated configuration per the
   hardware-profile proposal.

**A validation failure returns HTTP 422** with a message identifying the
offending `modelId` or `profile` — the same error shape used by the rest of
the bundle validator (`validators.ValidationError`).

**Implementation touch point:** [`ServiceMetadataYAML`](../../ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata/bundle.go:24)
gains a `Models` field so the validator can decode and inspect the section:

```go
// In bundle/validate/metadata/bundle.go
type ServiceMetadataYAML struct {
    // ... existing fields unchanged ...
    Models []ServiceModelContract `yaml:"models"`
}

type ServiceModelContract struct {
    ComponentType string              `yaml:"componentType"`
    Supported     []ServiceModelEntry `yaml:"supported"`
}

type ServiceModelEntry struct {
    ModelID  string `yaml:"modelId"`
    Profile  string `yaml:"profile"`
    Default  bool   `yaml:"default"`
}
```

The `parseServiceMetadataYAML` function already decodes and validates the
struct — the cross-reference checks against the embedded component schema and
`properties.profile.profiles` are added as additional validation steps after
the existing field checks, using the same `validators.ValidationError` return
convention.

---

## 6. What changes

| Asset | Change |
|---|---|
| `assets/services/*/metadata.yaml` | Add `models:` section (5 services: chat, summarize, extract, translate, similarity) |
| Catalog backend — deploy-options endpoint | Filter model `oneOf` list by service contract before returning schema |
| Catalog backend — param resolution | Inject `profile` from service contract before template render |
| Bundle upload validator — `ServiceMetadataYAML` | Add `Models []ServiceModelContract` field; cross-check each `modelId` and `profile` against the embedded component schema and `properties.profile.profiles` (BYOS only — embedded assets are trusted) |
| `assets/components/llm/vllm-spyre/*/values.schema.json` | No change — remains the source of truth for model metadata |
| Templates | No change — already consume the flat profile params from the hardware-profile proposal |
| `DeployOptionsProvider.Schema` URL | Gains `?service=<id>` query param so the backend can apply contract filtering |
| `GetComponentProviderParams()` | Accepts optional `serviceID` context; when present, filters `model.oneOf` and injects allowed profiles per contract |

---

## 7. What this does NOT change

- The hardware-profile proposal is a prerequisite. This proposal sits on top of
  it — the `profile` values referenced here are defined there.
- The component `values.schema.json` files remain the canonical model registry.
  The service contract references `modelId` values that already exist there; it
  does not duplicate model descriptions or introduce a parallel registry.
- Existing application-level templates (`applications/rag`, `rag-dev`) are not
  affected; they embed their own fixed configurations.

---

## 8. Schema endpoint changes

### `GET /api/v1/components/{type}/providers/{id}/params`

A new optional `?service=<id>` query param is added. When present, the handler
passes the service ID into `GetComponentProviderParams()`, which applies three
in-memory transformations to the decoded schema before returning it — the
on-disk file is never modified:

1. **Filter `model.oneOf`** — drop entries whose `const` is not in the
   service contract's `modelId` list.
2. **Replace each model's `x-profiles`** — intersect with the `profiles[]`
   declared for that model in the contract.
3. **Rebuild `profile` field** — union of all surviving `x-profiles`. If the
   union has only one value, omit the `profile` field entirely (no dropdown).

When `?service=` is absent, the schema is returned unfiltered — standalone
component deployment is unchanged.

### `DeployOptionsProvider.Schema` URL

[`buildProvider()`](../../ai-services/internal/pkg/catalog/deploy_options.go:268)
appends `&service=<id>` to the schema URL when building providers in the
context of a service:

```
# before (standalone or no service context)
/api/v1/components/llm/providers/vllm-spyre/params?runtime=podman

# after (built inside buildSingleService / buildArchitectureServices)
/api/v1/components/llm/providers/vllm-spyre/params?runtime=podman&service=extract
```

### `profile` field (single string)

The contract field is `profile: <id>` — a single string, not a list. The
service contract prescribes exactly one profile per model; the user never
selects a profile in the service deploy flow, so there is no reason to allow
more than one. The profile dropdown (defined in the hardware-profile proposal)
is only shown in standalone model deployment outside any service context.

---

## 9. Service contracts — complete reference

All per-service YAML blocks use `profile` (single string).

#### `services/chat/metadata.yaml`
```yaml
models:
  - componentType: llm
    supported:
      - modelId: ibm-granite/granite-4.1-8b-fp8
        profile: standard
        default: true
      - modelId: ibm-granite/granite-3.3-8b-instruct
        profile: standard
```

#### `services/summarize/metadata.yaml`
```yaml
models:
  - componentType: llm
    supported:
      - modelId: ibm-granite/granite-4.1-8b-fp8
        profile: standard
        default: true
      - modelId: ibm-granite/granite-3.3-8b-instruct
        profile: standard
```

#### `services/extract/metadata.yaml`
```yaml
models:
  - componentType: llm
    supported:
      - modelId: ibm-granite/granite-4.1-8b-fp8
        profile: compact-4k
        default: true
      - modelId: ibm-granite/granite-3.3-8b-instruct
        profile: compact-4k
      - modelId: meta-llama/Llama-3.1-8B-Instruct
        profile: compact-4k
```

#### `services/translate/metadata.yaml`
```yaml
models:
  - componentType: llm
    supported:
      - modelId: ibm-granite/granite-4.1-8b-fp8
        profile: compact-4k
        default: true
      - modelId: ibm-granite/granite-3.3-8b-instruct
        profile: compact-4k
```

#### `services/similarity/metadata.yaml`
```yaml
models:
  - componentType: embedding
    supported:
      - modelId: ibm-granite/granite-embedding-278m-multilingual
        profile: standard
        default: true
  - componentType: reranker
    supported:
      - modelId: BAAI/bge-reranker-v2-m3
        profile: standard
        default: true
```

---

## 10. Worked example end-to-end

**User deploys the Extract service with `granite-4.1-8b-fp8`.**

1. `buildSingleService("extract")` is called while building deploy options.
2. `buildProvider()` sets the LLM provider schema URL to:
   `/api/v1/components/llm/providers/vllm-spyre/params?runtime=podman&service=extract`
3. UI fetches that URL → backend calls `GetComponentProviderParams(ctx, "llm",
   "vllm-spyre", serviceID="extract")`.
4. Backend loads `services/extract/metadata.yaml` → `models[llm].supported` =
   3 models, each with `profiles: [compact-4k]`.
5. Backend filters `model.oneOf` to those 3 models. Since every model has only
   one profile, the `profile` field is omitted from the returned schema.
6. UI renders model dropdown with 3 options; `granite-4.1-8b-fp8` pre-selected.
   No profile dropdown shown.
7. User submits. `POST /api/v1/applications` body:
   `{ "model": "ibm-granite/granite-4.1-8b-fp8" }`.
8. Backend validator checks `modelId` is in service contract → pass.
9. Backend param resolver looks up `(extract, granite-4.1-8b-fp8)` →
    `profile = compact-4k`.
10. Profile resolver expands `compact-4k` →
    `numCards=1, maxModelLen=4096, maxBatchSize=32, memory="100Gi", shmSize="64Mi"`.
11. Podman pod starts with 1 Spyre card, 100 GB memory, 4K context window.
