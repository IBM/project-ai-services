# Proposal: Service–Model Contracts (Recommendation & Filtering Strategy)

- **Status:** Recommended (Draft / Proposal)
- **Created**: October 2026
- **Prerequisite**: [model-profiles.md](./model-profiles.md) — the hardware profile vocabulary
  (`standard`, `balanced`, `compact-4k`, `compact-8k`, `compact-16k`, `minimal`) defined in
  the component providers is referenced here.

---

## 1. Problem Statement

The [hardware-profile proposal](./model-profiles.md) standardizes how hardware parameters
are resolved for a given profile within a component provider (`vllm-spyre`, `vllm-cpu`, etc.).
However, component providers are shared across all services, leading to three operational gaps:

### Gap 1 — Workload mismatch on hardware profiles

A component provider like `llm/vllm-spyre` defines multiple profiles for a model (e.g. 4-card
`standard` for long-context RAG vs 1-card `compact-4k` for entity extraction). There is currently
no mechanism for a specialized service (such as `extract` or `translate`) to restrict multi-card
heavy profiles that do not match its single-card execution requirements.

### Gap 2 — Incompatible or unsuited models

Certain models registered under a provider (e.g. specialized code generation models like
`granite-20b-code`) may not be suitable or functional for conversational RAG or entity extraction
services. Today, services have no declarative way to disallow specific incompatible models.

### Gap 3 — Absence of service-specific recommendations and profile overrides

Component providers define a generic default profile for each model (e.g. `standard` 4-card).
However, different services have different optimal configurations for the exact same model:
- The **Chat** service needs `granite-3.3-8b-instruct` on 4 cards (`standard`) or 2 cards (`balanced`).
- The **Extract** service running the exact same model needs it on 1 card (`compact-4k`).

Services need a declarative way to **override the recommended profile per model** to match their
workload, while still allowing users to pick an alternative valid profile if desired.

---

## 2. Core Architectural Principle: Default-All with Service-Specific Guidance

To eliminate maintenance overhead, ensure zero-friction onboarding of new models, and provide guided user experience:

1. **Services DO NOT define or enumerate supported models.** The component providers
   (`values.schema.json` and `profiles.yaml`) remain the single canonical source of truth for
   all available models and hardware profiles.
2. **By default, every service supports all providers, all models, and all profiles.** When a
   new model or profile is added to `vllm-spyre`, all services inherit it immediately without
   requiring edits to service metadata files.
3. **Services declare `exclusions` in `metadata.yaml` for internal backend filtering:**
   - Exclusions are read by the backend when computing the params schema for a `?service=<id>` call.
   - They are applied server-side and **never returned to the UI** — the UI always receives a single clean, already-filtered schema.
   - If a service cannot work with a specific model → add an entry with `modelId` and no `profiles` key.
   - If a service cannot run certain hardware profiles for a model → add an entry with `modelId` and a `profiles` list.
4. **`recommendations` is purely additive guidance — not a definition of supported models:**
   - If a service declares no `recommendations`, all models and profiles from the provider are offered to the user unchanged.
   - If `recommendations` are declared, **all models and profiles still appear** in the params schema; the recommendations only add `model.default` and `x-recommended-profile` annotations to guide the user.
   - The **first entry** acts as the primary recommendation — its `modelId` is set as `model.default`; the UI renders a **"★ Recommended"** badge on it.
   - **All entries** (including the first) have `x-recommended-profile` set on their schema entry so the UI can pre-select the right profile whenever that model is picked.
5. **User Flexibility:** Recommendations pre-select the model and profile, but users are free to select any model or profile at deploy time (subject only to `exclusions`).
6. **Zero-configuration for open services:** If a service has no filtering needs and requires
   no custom recommendations, its `models` section is omitted entirely — all models and profiles from the provider are served as-is.

---

## 3. Where does the contract live?

### Same `metadata.yaml`, not a new file

Model recommendations and filtering rules belong in the service's top-level metadata:

```
services/<name>/metadata.yaml            ← service-level (identity, dependencies, recommendations, exclusions)
services/<name>/podman/metadata.yaml     ← runtime-level (resources, deploy steps)
services/<name>/openshift/metadata.yaml  ← runtime-level
```

Model compatibility and recommendations are functional properties of the service logic, not of a
specific container runtime. Therefore, the optional `models` block is defined in `services/<name>/metadata.yaml`
alongside `dependencies`.

---

## 4. Proposed Design

### 4.1 Schema & All Introduced Keys

Each service may declare an optional top-level `models` list in `services/<name>/metadata.yaml`.

```yaml
models:
  - componentType: <llm | embedding | reranker>
    recommendedProvider: <provider-id>         # optional: pre-selects this provider tab in UI
    providers:
      # Local Provider Example
      - providerId: <vllm-spyre | vllm-cpu>
        recommendations:
          - modelId: <primary-model-id>        # first entry = primary (pre-selected in UI)
            profile: <profile-id>
          - modelId: <other-model-id>          # per-model profile override
            profile: <profile-id>
        exclusions:
          - modelId: <model-id-to-exclude>     # no profiles key = exclude the model entirely
          - modelId: <model-id>
            profiles:                           # only these profiles are excluded for this model
              - <profile-id-to-exclude>

      # Remote Provider Example (WatsonX, hosted_vllm — no hardware profiles)
      - providerId: <watsonx | hosted_vllm>
        recommendations:
          - modelId: <primary-model-id>        # no profile for remote; first entry = primary
        exclusions:
          - modelId: <model-id-to-exclude>     # no profiles key = model excluded entirely
```

#### Complete Key Reference

| Key | Level | Type | Required? | Description |
| :--- | :--- | :--- | :--- | :--- |
| `models` | Root | `list` | Optional | Top-level section declaring recommendations and/or exclusions. If omitted (or if no `exclusions` are declared), all models and profiles from the provider are served unchanged. `recommendations` are purely additive — they do not restrict the model set. |
| `componentType` | `models[]` | `string` | **Yes** (if `models` present) | The component dependency type (`llm`, `embedding`, `reranker`). Must match one of `dependencies`. |
| `recommendedProvider` | `models[]` | `string` | Optional | The `providerId` to pre-select when the service deploy form opens. Must match an entry in the sibling `providers` list. |
| `providers` | `models[]` | `list` | **Yes** | List of provider-specific contract entries. |
| `providerId` | `providers[]` | `string` | **Yes** | Registered provider identifier (local: `vllm-spyre`, `vllm-cpu`; remote: `watsonx`, `hosted_vllm`). |
| `recommendations` | `providers[]` | `list<object>` | Optional | Ordered list of model+profile recommendations. The **first entry** is the primary — its `modelId` becomes `model.default` and its `profile` becomes the initial `x-recommended-profile`. All entries set `x-recommended-profile` on their schema entry for dynamic profile pre-selection. |
| `modelId` | `recommendations[]` | `string` | **Yes** | The model ID to recommend. |
| `profile` | `recommendations[]` | `string` | Optional | The recommended profile ID (local providers only; omitted for remote). |
| `exclusions` | `providers[]` | `list<object>` | Optional | List of model-level or profile-level exclusions. An entry without `profiles` excludes the model entirely; an entry with `profiles` restricts only those profiles for that model. |
| `modelId` | `exclusions[]` | `string` | **Yes** | The model ID to exclude or restrict. |
| `profiles` | `exclusions[]` | `list<string>` | Optional | Profile IDs to exclude for this model (e.g. `standard`, `balanced`). Omit to exclude the model entirely. |

---

### 4.2 Updated `metadata.yaml` for Services

#### `services/chat/metadata.yaml`

Chat is a conversational RAG workload. On Spyre it recommends `granite-4.1-8b-fp8` at `standard`. For `granite-3.3-8b-instruct`, it overrides the recommended profile to `balanced` (2 cards). It excludes specialized code models:

```yaml
# Added to services/chat/metadata.yaml
models:
  - componentType: llm
    recommendedProvider: vllm-spyre
    providers:
      - providerId: vllm-spyre
        recommendations:
          - modelId: ibm-granite/granite-4.1-8b-fp8    # primary — pre-selected in UI
            profile: standard
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profile: balanced
        exclusions:
          - modelId: ibm-granite/granite-20b-code       # no profiles = exclude model entirely
      - providerId: watsonx
        recommendations:
          - modelId: ibm/granite-3-8b-instruct
```

#### `services/summarize/metadata.yaml`

Summarize operates over long documents. It supports all models and profiles, recommending `granite-4.1-8b-fp8` at `standard`:

```yaml
# Added to services/summarize/metadata.yaml
models:
  - componentType: llm
    recommendedProvider: vllm-spyre
    providers:
      - providerId: vllm-spyre
        recommendations:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            profile: standard
```

#### `services/extract/metadata.yaml`

Extract is a short-context, structured extraction workload validated for single-card execution. It recommends `compact-4k` for all models and excludes multi-card profiles so only `compact-4k` survives:

```yaml
# Added to services/extract/metadata.yaml
models:
  - componentType: llm
    recommendedProvider: vllm-spyre
    providers:
      - providerId: vllm-spyre
        recommendations:
          - modelId: ibm-granite/granite-4.1-8b-fp8    # primary — pre-selected in UI
            profile: compact-4k
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profile: compact-4k
        exclusions:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            profiles: [standard, balanced]
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profiles: [standard, balanced]
```

#### `services/translate/metadata.yaml`

Translate processes token chunks in short context on 1 card. It recommends `compact-4k` and excludes multi-card profiles:

```yaml
# Added to services/translate/metadata.yaml
models:
  - componentType: llm
    recommendedProvider: vllm-spyre
    providers:
      - providerId: vllm-spyre
        recommendations:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            profile: compact-4k
        exclusions:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            profiles: [standard, balanced]
```

#### `services/similarity/metadata.yaml`

Similarity depends on `embedding` and `reranker`. It provides recommendations for each:

```yaml
# Added to services/similarity/metadata.yaml
models:
  - componentType: embedding
    recommendedProvider: vllm-cpu
    providers:
      - providerId: vllm-cpu
        recommendations:
          - modelId: ibm-granite/granite-embedding-278m-multilingual
            profile: standard
  - componentType: reranker
    recommendedProvider: vllm-spyre
    providers:
      - providerId: vllm-spyre
        recommendations:
          - modelId: BAAI/bge-reranker-v2-m3
            profile: standard
```

---

## 5. How the backend uses the contract

### Step A — Schema computation and enrichment (params serve time)

When the UI requests component params for a provider in the context of a service
(`GET /api/v1/components/{type}/providers/{id}/params?service=<serviceId>`):

1. **Load base schema:** Read the provider's `values.schema.json` — all models in `model.oneOf` and all profiles in each model's `x-profiles`. This is the default: every model and every profile is available unless a subsequent step removes it.
2. **Apply exclusions (server-side, not returned):** If `exclusions` are declared in `services/<id>/metadata.yaml`, filter in-memory:
   - Drop any `model.oneOf` entry whose `modelId` appears in `exclusions` with no `profiles` key.
   - For each surviving model, subtract any profiles listed under `exclusions[modelId].profiles` from its `x-profiles`.
   - Exclusions are **never exposed** in the returned schema — the UI receives only the post-filter result.
   - If no `exclusions` are declared, the full base schema passes through unchanged.
3. **Annotate recommendations (returned as schema annotations):** If `recommendations` are declared in `services/<id>/metadata.yaml`, annotate the schema in-memory — **this never removes any model or profile from the schema**:
   - First entry: set `model.default` to its `modelId`; set `"x-recommended-profile": "<profile>"` on its `model.oneOf` entry.
   - Each subsequent entry: set `"x-recommended-profile": "<profile>"` on its `model.oneOf` entry so the UI can update the profile pre-selection when the user switches models.
   - The UI derives the initial profile pre-selection from `x-recommended-profile` on the `model.default` entry — no separate `profile.default` is needed.
4. **Profile selector visibility:**
   - If a model has $>1$ surviving profile: UI renders a profile dropdown; it reads `x-recommended-profile` from the selected model's `oneOf` entry to pre-select the right profile.
   - If a model has exactly $1$ surviving profile: Backend removes the `profile` field from the schema — it is auto-injected at deploy time without user interaction.
5. **Return one clean enriched schema** to the UI. No separate metadata block, no raw contract data — just the standard JSON Schema shape with the above annotations baked in.

### Step B — Deployment validation and profile resolution (deploy time)

When the user submits an application or service deployment:

1. **Model Check:** Backend re-reads `exclusions` from `metadata.yaml` and verifies the submitted `model` is not excluded. If it is, returns `HTTP 400 Bad Request` (`MODEL_NOT_SUPPORTED_BY_SERVICE`).
2. **Profile Check:** Backend verifies the submitted `profile` is not in `exclusions[modelId].profiles`. If it is, returns `HTTP 400 Bad Request` (`PROFILE_NOT_SUPPORTED_BY_SERVICE`). Any valid non-excluded profile the user selected is honored, even if it differs from `x-recommended-profile`.
3. **Auto-Resolution / Default Injection:** If the user did not provide a `profile` (because only 1 profile remained valid, or the user submitted default parameters):
   - If only 1 profile survives for that model $\rightarrow$ inject that single profile.
   - If `recommendations` contains an entry for this model $\rightarrow$ inject that entry's `profile`.
   - Otherwise $\rightarrow$ inject the provider's default profile from `profiles.yaml`.

### Step C — Bundle upload validation (BYOS only)

For user-defined service bundles uploaded via `POST /api/v1/catalog/bundles`, the validator checks:

1. `componentType` matches one of the bundle's declared `dependencies`.
2. `providerId` matches a known provider.
3. `recommendedProvider` (if set) matches one of the `providerId` values in the sibling `providers` list.
4. Every `recommendations[*].modelId` exists and does NOT appear in `exclusions` as a model-level exclusion.
5. Every `recommendations[*].profile` exists in `profiles.yaml` and is NOT listed under `exclusions` for that model.
6. Every entry in `exclusions` references valid catalog items.

### Enriched Schema Response Shape

When `?service=<id>` is provided, the returned schema for a local provider looks like:

```json
{
  "properties": {
    "model": {
      "default": "<first-recommendations-modelId>",
      "oneOf": [
        {
          "const": "<recommended-model-id>",
          "title": "<model display name>",
          "x-recommended-profile": "<profile>",
          "x-profiles": ["<surviving-profile>", "..."]
        },
        {
          "const": "<other-model-id>",
          "title": "<model display name>",
          "x-recommended-profile": "<profile>",
          "x-profiles": ["<surviving-profile>", "..."]
        }
      ]
    },
    "profile": {
      // no default — UI reads x-recommended-profile from the selected model's oneOf entry
      // field omitted entirely if only 1 profile survives for the selected model
    }
  }
}
```

**Schema annotation reference:**
- `model.default` — the primary recommended model; UI renders **"★ Recommended"** badge and pre-selects it.
- `x-recommended-profile` — profile the UI pre-selects when the user picks that model (including on initial load). Present on every model in `recommendations`. No separate `profile.default` needed.
- `x-profiles` — surviving profiles after exclusions (all profiles when no exclusions declared for that model).
- Excluded models are absent from `model.oneOf` entirely — the UI has no visibility into what was filtered.
- `profile` field omitted when only 1 profile survives — auto-injected at deploy time, no dropdown rendered.

### Implementation Go Structs

```go
// In internal/pkg/catalog/apiserver/services/bundle/validate/metadata/bundle.go

type ServiceMetadataYAML struct {
    // ... existing metadata fields ...
    Models []ServiceModelContract `yaml:"models,omitempty" json:"models,omitempty"`
}

type ServiceModelContract struct {
    ComponentType       string                    `yaml:"componentType" json:"componentType"`
    RecommendedProvider string                    `yaml:"recommendedProvider,omitempty" json:"recommendedProvider,omitempty"`
    Providers           []ServiceProviderContract `yaml:"providers" json:"providers"`
}

type ServiceProviderContract struct {
    ProviderID      string                `yaml:"providerId" json:"providerId"`
    Recommendations []ModelRecommendation `yaml:"recommendations,omitempty" json:"recommendations,omitempty"`
    Exclusions      []ModelExclusion      `yaml:"exclusions,omitempty" json:"exclusions,omitempty"`
}

type ModelRecommendation struct {
    ModelID string `yaml:"modelId" json:"modelId"`
    Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
}

type ModelExclusion struct {
    ModelID  string   `yaml:"modelId" json:"modelId"`
    Profiles []string `yaml:"profiles,omitempty" json:"profiles,omitempty"` // empty = exclude model entirely
}
```

---

## 6. What changes

| Asset | Change |
|---|---|
| `assets/services/*/metadata.yaml` | Optionally add `models:` block with `recommendations` and/or `exclusions` |
| Catalog backend — deploy-options endpoint | Surface `recommendedProvider` per component type in the deploy-options response |
| Catalog backend — params endpoint | Accept `?service=<id>`; apply exclusions server-side and bake `model.default` + `x-recommended-profile` + `x-profiles` annotations into the returned schema |
| Catalog backend — deploy handler | Validate submitted model/profile is not excluded; honor user profile override or auto-inject recommended profile |
| Bundle upload validator — `ServiceMetadataYAML` | Add `Models []ServiceModelContract` struct and validate exclusion/recommendation references |
| `assets/components/*/*/*/values.schema.json` | No change — remains canonical source of all models |
| `assets/components/*/*/profiles.yaml` | No change — provider-level source of truth for all hardware profiles |
| Templates | No change — consume resolved flat parameters |

---

## 7. What this does NOT change

- **Component independence:** Adding a new model to `vllm-spyre` immediately makes it available across all services without updating any service metadata files.
- **Provider schema integrity:** The on-disk `values.schema.json` is never modified; filtering and recommendation annotations are computed in-memory when `?service=<id>` is provided.
- **Standalone model deployment:** Standalone model serving without a service context (`/api/v1/components/llm/providers/vllm-spyre/params`) returns all models and all profiles without exclusions or service-specific recommendations.

---

## 8. Schema endpoint changes

### Schema Endpoint Changes for Local & Remote Providers

Both local component params and remote connector params support the `?service=<id>` query parameter. When omitted, the raw unmodified schema is returned with no service-specific filtering or annotations.

#### 0. Service-Level Deploy Options: `GET /api/v1/services/{id}/deploy-options`
- Returns `recommendedProvider` per `componentType` in the component entry so the UI can pre-select the correct provider tab before loading params.
- Does **not** return `exclusions` or `recommendations` — those are internal to the params computation.

#### 1. Local Component Params: `GET /api/v1/components/{type}/providers/{id}/params?service=<id>`
- Reads `exclusions` from `services/<id>/metadata.yaml`; applies them server-side (models and profiles dropped in-memory — **not returned**).
- Reads `recommendations` from `services/<id>/metadata.yaml`; bakes annotations directly into the returned schema:
  - `model.default` → first recommendation's `modelId` (UI badge + pre-selection)
  - `x-recommended-profile: <profile>` on each recommended model's `oneOf` entry (UI reads this to pre-select profile on model pick)
  - `x-profiles` on each model's `oneOf` entry reflects only surviving profiles after exclusions
- If a model has exactly 1 surviving profile, the `profile` field is **omitted** from the schema entirely (auto-injected at deploy time).
- Returns one clean enriched JSON Schema — the UI renders directly from it, with no second lookup needed.

#### 2. Remote Connector Params: `GET /api/v1/connectors/models/providers/{id}/params?service=<id>`
- Reads `exclusions` from `services/<id>/metadata.yaml`; drops excluded models from `model_name.oneOf` server-side (**not returned**).
- Reads `recommendations`; annotates the first entry's `modelId` as `model_name.default`.

---

## 9. Worked examples end-to-end

### Scenario A — Recommendation + Per-Model Override + Exclusion (Chat service)

1. Provider `vllm-spyre` defines `granite-4.1-8b-fp8`, `granite-3.3-8b-instruct`, and `granite-20b-code`.
2. `services/chat/metadata.yaml` specifies `recommendedProvider: vllm-spyre`, two `recommendations` entries, and `exclusions: [{ modelId: ibm-granite/granite-20b-code }]`.
3. User opens Chat deploy form → deploy-options response carries `recommendedProvider: vllm-spyre`; UI pre-selects the Spyre tab.
4. UI calls `GET .../vllm-spyre/params?service=chat`. Backend computes the enriched schema:
   - `granite-20b-code` dropped server-side (exclusion, no `profiles` key) — **not present in response**.
   - `granite-4.1-8b-fp8` → `model.default`, `x-recommended-profile: standard`.
   - `granite-3.3-8b-instruct` → `x-recommended-profile: balanced`.
5. UI renders: `granite-4.1-8b-fp8` pre-selected with **"★ Recommended"** badge (`model.default`); profile dropdown pre-set to `standard` (from `x-recommended-profile`).
6. User switches model to `granite-3.3-8b-instruct` → UI reads `x-recommended-profile: balanced` from that entry and pre-selects `balanced` in the profile dropdown.
7. User overrides to `standard` and submits `{ model: granite-3.3-8b-instruct, profile: standard }`.
8. Backend re-validates: `granite-3.3-8b-instruct` not in model exclusions, no profile exclusions declared for it → Pass. Template renders with 4 cards.

### Scenario B — Recommendation + Profile Exclusion (Extract service)

1. `granite-4.1-8b-fp8` in `vllm-spyre` has `x-profiles: [standard, balanced, compact-4k]`.
2. `services/extract/metadata.yaml` specifies `recommendations: [{ modelId: granite-4.1-8b-fp8, profile: compact-4k }]` and `exclusions: [{ modelId: granite-4.1-8b-fp8, profiles: [standard, balanced] }]`.
3. User opens Extract deploy form → UI calls `GET .../vllm-spyre/params?service=extract`. Backend computes:
   - `standard` and `balanced` dropped from `granite-4.1-8b-fp8`'s `x-profiles` server-side — **not in response**.
   - Returned schema: `model.default: granite-4.1-8b-fp8`, `x-recommended-profile: compact-4k`, `x-profiles: [compact-4k]`.
   - Because only 1 profile survives, `profile` field **omitted** from schema entirely.
4. UI shows `granite-4.1-8b-fp8` with **"★ Recommended"** badge; no profile dropdown (single profile, hidden).
5. User submits. Backend auto-injects `profile: compact-4k` → pod launches with 1 Spyre card.

---

## 10. Future Scope: Database-Persisted Exclusions & Recommendations

If dynamic runtime customization is needed in the future (e.g. allowing cluster administrators to update recommendations or exclude newly deprecated models via an Admin UI without redeploying code):

### 10.1 `service_model_contracts` table (Future Scope)

```sql
CREATE TABLE service_model_contracts (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id           VARCHAR(100) NOT NULL,          -- e.g. 'chat', 'extract'
    component_type       VARCHAR(50)  NOT NULL,          -- 'llm', 'embedding', 'reranker'
    recommended_provider VARCHAR(50),                    -- e.g. 'vllm-spyre'; pre-selects provider tab in UI
    provider_id          VARCHAR(50)  NOT NULL,          -- 'vllm-spyre', 'vllm-cpu', 'watsonx'
    recommendations      JSONB,                          -- JSON array: [{"modelId": "...", "profile": "standard"}]; first entry = primary
    exclusions           JSONB,                          -- JSON array: [{"modelId": "...", "profiles": ["standard"]}]; no profiles key = model excluded
    is_builtin           BOOLEAN      NOT NULL DEFAULT false, -- true for seed rows from YAML
    created_by           VARCHAR(100) NOT NULL DEFAULT 'system',
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (service_id, component_type, provider_id)
);
```

### 10.2 Lifecycle & API

- **Seed on Startup:** Seeded from `services/*/metadata.yaml` with `is_builtin = true`.
- **Admin Overrides:** Administrators can add or adjust exclusions and recommendations dynamically via `PUT /api/v1/services/:service_id/contracts`. Built-in baseline entries remain read-only.
