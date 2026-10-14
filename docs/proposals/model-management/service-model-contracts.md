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

1. Declares which **providers** and **models** it supports, scoped per component type.
2. Binds each supported model under a provider to allowed hardware **profile IDs** (from
   `profiles.yaml`) tailored to that service's workload.
3. Accommodates both **granular selection** (letting users pick provider, model, and
   workload-validated profile when choices exist) and **automatic resolution** (injecting
   the single validated profile with zero user friction when only one option is valid).
4. Lets the catalog backend drive `values.schema.json` generation and profile validation
   from this contract, so the UI options a user sees are always strictly consistent with
   what the service has tested and declared.

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
service supports the same models and profiles whether deployed on podman or openshift; only
the template rendering differs. The contract therefore belongs in the top-level
`services/<name>/metadata.yaml` alongside `dependencies` — it is a declaration
about what the service *is*, not about how it runs.

Adding a new file (e.g. `models.yaml`) would require a new file-type contract in
the catalog backend reader and provide no structural benefit over a new top-level
key in the existing file. The existing `metadata.yaml` is the right place.

---

## 4. Proposed Design

### 4.1 Hierarchical `models` section in `services/<name>/metadata.yaml`

Each service gains a top-level `models` key. It is structured hierarchically by
**component type $\rightarrow$ provider $\rightarrow$ model $\rightarrow$ profile(s)**:

**Schema:**

```yaml
models:
  - componentType: <llm | embedding | reranker>
    providers:
      - providerId: <vllm-spyre | vllm-cpu | watsonx | ...>
        default: true                       # optional — marks the pre-selected provider for this service
        supported:
          - modelId: <model id / HF repo>   # must match a const in the provider's values.schema.json or connector schema
            default: true                   # optional — marks the pre-selected model for this provider
            profiles:                       # optional — list of validated profiles allowed for this service workload
              - <profile-id-1>
              - <profile-id-2>
            defaultProfile: <profile-id-1>  # optional — pre-selected profile (defaults to first entry in profiles)
```

**Rules:**

- `componentType` must match one of the service's declared `dependencies`.
- `providerId` must match a registered provider for that component type (e.g. `vllm-spyre`, `vllm-cpu`, `watsonx`).
- `modelId` must match a `const` value already declared in the provider's
  `values.schema.json` `oneOf` (for local components) or `schema.json` (for remote connectors).
  The contract narrows, never extends, the provider's known model list.
- `profiles` is a list of strings matching `id` values in the provider's `profiles.yaml` that
  are also present in the model's `x-profiles`:
  - **Multiple profiles declared:** The UI renders a profile dropdown containing only the
    contract-allowed profiles, pre-selecting `defaultProfile` (or the first entry). This
    enables granular selection (e.g. choosing between `standard` 4-card high-throughput vs
    `balanced` 2-card footprint for Chat).
  - **Single profile declared:** The backend auto-injects this profile at deploy time. No
    profile dropdown is shown in the UI (or it is rendered as a fixed badge), avoiding
    unnecessary clicks.
  - **No profiles (remote / cloud providers):** For remote connectors such as `watsonx` or
    `hosted_vllm` that do not use local hardware profiles, `profiles` and `defaultProfile`
    are omitted.
- Exactly one provider per `componentType` may set `default: true`. If omitted, the first provider
  is treated as default.
- Exactly one model per provider entry may set `default: true`. If omitted, the first model
  is treated as default.

---

### 4.2 Updated `metadata.yaml` for each service

The `models` section is added to each service's **top-level** `metadata.yaml`.
The existing `dependencies`, `about`, and other keys are unchanged.

#### `services/chat/metadata.yaml`

Chat is a RAG inferencing workload. On Spyre it supports full-scale 4-card inferencing (`standard`)
as well as a cost-effective 2-card configuration (`balanced`). It also supports fallback to CPU
and remote watsonx.ai.

```yaml
# Added to services/chat/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        default: true
        supported:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            default: true
            profiles:
              - standard
              - balanced
            defaultProfile: standard
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profiles:
              - standard
              - balanced
            defaultProfile: standard
      - providerId: vllm-cpu
        supported:
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profiles:
              - standard
            defaultProfile: standard
      - providerId: watsonx
        supported:
          - modelId: ibm/granite-3-8b-instruct
            default: true
          - modelId: meta-llama/llama-3-1-70b-instruct
```

#### `services/summarize/metadata.yaml`

Summarize operates over long documents — same RAG inferencing options as chat.

```yaml
# Added to services/summarize/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        default: true
        supported:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            default: true
            profiles:
              - standard
            defaultProfile: standard
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profiles:
              - standard
            defaultProfile: standard
      - providerId: watsonx
        supported:
          - modelId: ibm/granite-3-8b-instruct
            default: true
```

#### `services/extract/metadata.yaml`

Extract is a short-context, structured-output workload — 1 card.
`compact-4k` (4,096 tokens, batch 32) is validated on RHAIIS 3.6+.

```yaml
# Added to services/extract/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        default: true
        supported:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            default: true
            profiles:
              - compact-4k
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profiles:
              - compact-4k
          - modelId: meta-llama/Llama-3.1-8B-Instruct
            profiles:
              - compact-4k
      - providerId: vllm-cpu
        supported:
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profiles:
              - compact-4k
```

#### `services/translate/metadata.yaml`

Translate works sentence- and paragraph-at-a-time via token-aware chunking.
Short-context 1-card workload — same validated configuration as extract.

```yaml
# Added to services/translate/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        default: true
        supported:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            default: true
            profiles:
              - compact-4k
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profiles:
              - compact-4k
```

#### `services/similarity/metadata.yaml`

Similarity depends on `embedding` and `reranker`, not `llm`. Both have a
single validated model and profile — the contract declares them explicitly
so the backend can enforce the constraint.

```yaml
# Added to services/similarity/metadata.yaml
models:
  - componentType: embedding
    providers:
      - providerId: vllm-cpu
        default: true
        supported:
          - modelId: ibm-granite/granite-embedding-278m-multilingual
            profiles:
              - standard
            default: true
  - componentType: reranker
    providers:
      - providerId: vllm-spyre
        default: true
        supported:
          - modelId: BAAI/bge-reranker-v2-m3
            profiles:
              - standard
            default: true
```

> `digitize` has no LLM dependency and is therefore not given a `models`
> section — it uses a document-processing pipeline, not an inference backend.

---

## 5. How the backend uses the contract

The service `metadata.yaml` is already read by the catalog backend when
building deploy-options responses. The `models` section plugs into three
existing steps.

### Step A — Provider and model list filtering (schema serve time)

When the UI requests component params for a provider in the context of a
specific service (`GET /api/v1/components/{type}/providers/{id}/params?service=<serviceId>`):

1. The backend reads the matching `(componentType, providerId)` entry from the
   service's top-level `metadata.yaml`.
2. It filters the provider schema's `model.oneOf` to only those `modelId` values
   declared in that provider's `supported[]` list.
3. For each surviving model, it intersects its `x-profiles` with the contract's
   `profiles[]` list.
4. If a model has $>1$ allowed profile in the contract, the UI displays a profile
   selector filtered to those choices (pre-selecting `defaultProfile`). If it has
   exactly $1$ profile, the `profile` field is omitted from the form (auto-injected).
5. Returns the filtered schema to the UI.

The user only ever sees providers, models, and profiles that the service has tested and approved.

### Step B — Profile resolution and injection (deploy time)

When the user submits an application or service deployment:

1. If the user explicitly selected a profile (when multiple choices were presented in the UI),
   the backend verifies the submitted profile is present in the contract's `profiles[]` list.
2. If the user did not submit a profile (because only 1 profile was valid and the selector was omitted),
   the backend looks up the contract for `(service, provider, model)` and injects the declared
   single profile / `defaultProfile`.
3. For remote connectors (e.g. `watsonx`), no profile is injected.

```
Example 1: Chat service (user chooses balanced 2-card profile):
  Chat + vllm-spyre + granite-4.1-8b-fp8 + profile: balanced
  Backend verifies 'balanced' ∈ [standard, balanced] → Valid!
  Backend resolves balanced → numCards=2, maxModelLen=8192, maxBatchSize=32, memory="150Gi"
  Template renders: AIU_WORLD_SIZE="2", podman.io/device=/dev/vfio: 2

Example 2: Extract service (single profile auto-resolved):
  Extract + vllm-spyre + granite-4.1-8b-fp8 (no profile sent in payload)
  Backend injects profile: compact-4k (from contract)
  Backend resolves compact-4k → numCards=1, maxModelLen=4096, maxBatchSize=32, memory="100Gi"
  Template renders: AIU_WORLD_SIZE="1", podman.io/device=/dev/vfio: 1
```

> **Standalone model deployment** (outside any service) exposes all profiles declared in
> `profiles.yaml` / model `x-profiles`. The service contract only filters options when deploying
> through a named service.

### Step C — Bundle upload validation (BYOS only)

Embedded (IBM-shipped) service assets are trusted by construction — the
`models` section is authored in the same repository as the component schemas
and `profiles.yaml`, so consistency is enforced at review time.

**User-defined service bundles (BYOS)** are validated when `POST /api/v1/catalog/bundles`
is called, before the bundle is written to disk or marked `active`.

**What the validator checks, for each entry in `models[*].providers[*].supported[*]`:**

1. The `componentType` matches one of the service's declared `dependencies`.
2. The `providerId` matches a registered provider for that `componentType`.
3. The `modelId` exists as a `const` in that provider's `values.schema.json` (or connector schema).
4. Each entry in `profiles[]` exists in the provider's `profiles.yaml` and appears in that
   model's `x-profiles` list.
5. If `defaultProfile` is specified, it must be an element of `profiles[]`.

**A validation failure returns HTTP 422** with a message identifying the offending `providerId`,
`modelId`, or `profile` (`validators.ValidationError`).

**Implementation touch point:** [`ServiceMetadataYAML`](../../ai-services/internal/pkg/catalog/apiserver/services/bundle/validate/metadata/bundle.go:24)
gains a `Models` field with supporting structs:

```go
// In bundle/validate/metadata/bundle.go
type ServiceMetadataYAML struct {
    // ... existing fields unchanged ...
    Models []ServiceModelContract `yaml:"models"`
}

type ServiceModelContract struct {
    ComponentType string                    `yaml:"componentType"`
    Providers     []ServiceProviderContract `yaml:"providers"`
}

type ServiceProviderContract struct {
    ProviderID string              `yaml:"providerId"`
    Default    bool                `yaml:"default,omitempty"`
    Supported  []ServiceModelEntry `yaml:"supported"`
}

type ServiceModelEntry struct {
    ModelID        string   `yaml:"modelId"`
    Default        bool     `yaml:"default,omitempty"`
    Profiles       []string `yaml:"profiles,omitempty"`
    DefaultProfile string   `yaml:"defaultProfile,omitempty"`
}
```

---

## 6. What changes

| Asset | Change |
|---|---|
| `assets/services/*/metadata.yaml` | Add hierarchical `models:` section (scoped by `componentType`, `providerId`, `modelId`, `profiles`) |
| Catalog backend — deploy-options endpoint | Filter provider and model lists by service contract before returning schema |
| Catalog backend — param resolution | Validate submitted profile or auto-inject single/default profile from service contract before template render |
| Bundle upload validator — `ServiceMetadataYAML` | Add `Models []ServiceModelContract` struct and cross-validate providers, models, and profiles against catalog assets (BYOS only) |
| `assets/components/llm/*/*/values.schema.json` | No change — remains the source of truth for provider model lists |
| Templates | No change — consume resolved flat profile parameters |
| `DeployOptionsProvider.Schema` URL | Gains `?service=<id>` query param so backend applies contract filtering |
| `GetComponentProviderParams()` | Accepts optional `serviceID` context; filters `model.oneOf` and intersects `x-profiles` per contract |

---

## 7. What this does NOT change

- The hardware-profile proposal is a prerequisite. This proposal sits on top of
  it — the `profile` values referenced here are defined there.
- The component `values.schema.json` and connector `schema.json` files remain the canonical model registry.
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
   service contract's `supported[].modelId` list for that provider.
2. **Replace each model's `x-profiles`** — intersect with the `profiles[]`
   declared for that model in the contract.
3. **Rebuild `profile` field** — union of all surviving `x-profiles`.
   - If the union has $>1$ value, keep the `profile` enum dropdown in the schema with `defaultProfile` as `default`.
   - If the union has only $1$ value, omit the `profile` field from the returned schema (backend will inject it at deploy time).
   - If the provider has no `profiles.yaml` (e.g. remote connector), omit the `profile` field.

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

---

## 9. Worked examples end-to-end

### Scenario A — Granular selection with multiple profiles (Chat service)

1. User opens the **Chat** service deployment form.
2. UI fetches deploy options:
   `GET /api/v1/components/llm/providers/vllm-spyre/params?runtime=podman&service=chat`
3. Backend filters models to `granite-4.1-8b-fp8` and `granite-3.3-8b-instruct`.
4. For `granite-4.1-8b-fp8`, contract declares `profiles: [standard, balanced]`. Since $>1$ profile is supported, the schema includes the `profile` dropdown with `standard` pre-selected.
5. User selects `granite-4.1-8b-fp8` and changes profile to `balanced` (2 Spyre cards).
6. User submits: `{ "model": "ibm-granite/granite-4.1-8b-fp8", "profile": "balanced" }`.
7. Backend validates `balanced` is allowed for `(chat, vllm-spyre, granite-4.1-8b-fp8)` $\rightarrow$ Pass.
8. Backend expands `balanced` $\rightarrow$ `numCards=2, maxModelLen=8192, maxBatchSize=32, memory="150Gi"`.
9. Podman pod starts with 2 Spyre cards.

### Scenario B — Auto-resolved profile with zero configuration (Extract service)

1. User opens the **Extract** service deployment form.
2. UI fetches deploy options:
   `GET /api/v1/components/llm/providers/vllm-spyre/params?runtime=podman&service=extract`
3. Backend filters models to the 3 extract-validated models. Each has `profiles: [compact-4k]`.
4. Since every model has exactly 1 valid profile, the backend omits the `profile` field from the schema.
5. UI renders only the model dropdown; no profile selector is shown.
6. User selects `granite-4.1-8b-fp8` and submits: `{ "model": "ibm-granite/granite-4.1-8b-fp8" }`.
7. Backend validator looks up contract for `(extract, vllm-spyre, granite-4.1-8b-fp8)` $\rightarrow$ auto-injects `profile: compact-4k`.
8. Backend expands `compact-4k` $\rightarrow$ `numCards=1, maxModelLen=4096, maxBatchSize=32, memory="100Gi"`.
9. Podman pod starts with 1 Spyre card.

---

## 10. Future Scope: Database Seeding for Service–Model Contracts

> **Note — Scope Boundary:** In Phase 1, service–model contracts are **file-driven**, authored directly inside each service's top-level `metadata.yaml` (or packaged within BYOS service bundles) and read into memory by the catalog loader.

If dynamic runtime customization of service contracts is required in the future (e.g. allowing administrators to attach newly validated models or custom hardware profiles to existing services without editing asset files or re-packaging bundles), the exact same **Data Seeding** technique defined in [model-profiles.md §12](./model-profiles.md#12-future-scope-user-defined-profiles-via-database-seeding-and-crud-api) can be applied to services:

### 10.1 `service_model_contracts` table (Future Scope)

To clearly differentiate **built-in** (IBM-certified seed) contracts from **user-defined** (custom) extensions, the table uses `created_by` (with `'system'` for built-in seed rows and the user's identity for custom additions) along with an explicit `is_builtin` boolean flag:

```sql
CREATE TABLE service_model_contracts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id      VARCHAR(100) NOT NULL,          -- e.g. 'chat', 'extract'
    component_type  VARCHAR(50)  NOT NULL,          -- 'llm', 'embedding', 'reranker'
    provider_id     VARCHAR(50)  NOT NULL,          -- 'vllm-spyre', 'vllm-cpu', 'watsonx'
    model_id        VARCHAR(255) NOT NULL,          -- e.g. 'ibm-granite/granite-4.1-8b-fp8'
    profiles        JSONB,                          -- JSON array of allowed profile IDs e.g. ["standard", "balanced"]
    default_profile VARCHAR(100),                   -- default profile ID
    is_default      BOOLEAN      NOT NULL DEFAULT false,
    is_builtin      BOOLEAN      NOT NULL DEFAULT false, -- true for IBM-shipped seed rows, false for user-defined
    created_by      VARCHAR(100),                   -- 'system' for built-in seed rows, user email/ID for custom
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (service_id, component_type, provider_id, model_id)
);
CREATE INDEX ON service_model_contracts(service_id, component_type, provider_id);
```

### 10.2 Seeding & Lifecycle Mechanism

1. **Bootstrap via Seeding:** During catalog startup or migration, the seeder reads the built-in `models` section from each `services/<name>/metadata.yaml` and seeds the table with `is_builtin = true` and `created_by = 'system'` using `ON CONFLICT DO NOTHING`.
2. **Differentiating Built-in vs. User-defined & Strict Immutability:**
   - **Built-in (`is_builtin = true` / `created_by = 'system'`):** Baseline tested configurations shipped with the platform. Users and administrators are **strictly prohibited from modifying or deleting built-in service contracts**. Any `PUT` or `DELETE` request against a built-in contract entry returns `403 Forbidden` (`BUILTIN_CONTRACT_READONLY`).
   - **User-defined (`is_builtin = false` / `created_by = '<user_id>'`):** Custom model-to-service attachments added by administrators. Only these user-defined rows can be updated or deleted via the API.
3. **Runtime Schema Filtering:** Instead of reading file structs directly, the backend queries the merged `service_model_contracts` repository, filtering schemas and validating deployments against database-persisted contracts.

### 10.3 API endpoints (Future Scope)

| Method | Path | Description | Response |
|---|---|---|---|
| `GET` | `/api/v1/services/:service_id/models` | List all supported model contract entries for a service (merged built-in and custom). | `200 OK` |
| `POST` | `/api/v1/services/:service_id/models` | Add a supported model with allowed profile(s) and provider to a service. | `201 Created` |
| `PUT` | `/api/v1/services/:service_id/models/:contract_id` | Update allowed profiles or default status of a custom model entry on a service (`403` on built-in rows). | `200 OK` |
| `DELETE` | `/api/v1/services/:service_id/models/:contract_id` | Remove a custom model contract entry from a service (`403` on built-in rows). | `204 No Content` |
