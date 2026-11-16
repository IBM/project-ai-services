# Proposal: Service–Model Contracts (Recommendation Strategy)

- **Status:** Recommended (Draft / Proposal)
- **Created**: October 2026
- **Prerequisite**: [model-profiles.md](./model-profiles.md) — the hardware profile vocabulary
  (`standard`, `balanced`, `compact-4k`, `compact-8k`, `compact-16k`, `minimal`) defined in
  the component providers is referenced here.

---

## 1. Problem Statement

The [hardware-profile proposal](./model-profiles.md) standardizes how hardware parameters
are resolved for a given profile within a component provider (`vllm-spyre`, `vllm-cpu`, etc.).
However, component providers are shared across all services, which leaves one operational gap
that this proposal addresses (two more are deferred — see [Future Scope](#10-future-scope)).

### Gap — Absence of service-specific recommendations and profile overrides

Component providers define a generic default profile for each model (e.g. `standard` 4-card).
However, different services have different optimal configurations for the exact same model:
- The **Chat** service needs `granite-3.3-8b-instruct` on 4 cards (`standard`) or 2 cards (`balanced`).
- The **Extract** service running the exact same model needs it on 1 card (`compact-4k`).

Services need a declarative way to **recommend a model and override the recommended profile per model**
to match their workload, while still allowing users to pick an alternative valid profile if desired.

### Deferred for now

Restricting models or profiles per service (e.g. hiding `granite-20b-code` from conversational
services, or hiding multi-card profiles from single-card services such as `extract`) is **not part of
this proposal**. Every service offers all models and all profiles; see [Future Scope](#10-future-scope).

---

## 2. Core Architectural Principle: Default-All with Service-Specific Guidance

To eliminate maintenance overhead, ensure zero-friction onboarding of new models, and provide guided user experience:

1. **Services DO NOT define or enumerate supported models.** The component providers
   (`models.yaml` and `profiles.yaml`) remain the single canonical source of truth for
   all available models and hardware profiles.
2. **By default, every service supports all providers, all models, and all profiles.** When a
   new model or profile is added to `vllm-spyre`, all services inherit it immediately without
   requiring edits to service metadata files.
3. **`recommendations` is purely additive guidance — not a definition of supported models:**
   - If a service declares no `recommendations`, all models and profiles from the provider are offered to the user unchanged.
   - If `recommendations` are declared, **all models and profiles are still selectable**; the recommendations only guide pre-selection.
   - The **first entry** acts as the primary recommendation — the UI pre-selects its `modelId` and renders a **"★ Recommended"** badge on it.
   - **All entries** (including the first) carry a `profile` so the UI can pre-select the right profile whenever that model is picked.
4. **User Flexibility:** Recommendations pre-select the model and profile, but users are free to select any model or profile at deploy time.
5. **Zero-configuration for open services:** If a service has no custom recommendations, its `models` section is omitted entirely — all models and profiles from the provider are served as-is.

---

## 3. Where does the contract live?

### Same `metadata.yaml`, not a new file

Model recommendations belong in the service's top-level metadata:

```
services/<name>/metadata.yaml            ← service-level (identity, dependencies, recommendations)
services/<name>/podman/metadata.yaml     ← runtime-level (resources, deploy steps)
services/<name>/openshift/metadata.yaml  ← runtime-level
```

Model recommendations are functional properties of the service logic, not of a
specific container runtime. Therefore, the optional `models` block is defined in `services/<name>/metadata.yaml`
alongside `dependencies`.

---

## 4. Proposed Design

### 4.1 Schema & All Introduced Keys

Each service may declare an optional top-level `models` list in `services/<name>/metadata.yaml`.

```yaml
models:
  - componentType: <llm | embedding | reranker>
    providers:
      # Local Provider Example
      - providerId: <vllm-spyre | vllm-cpu>
        recommendations:
          - modelId: <primary-model-id>        # first entry = primary (pre-selected in UI)
            profile: <profile-id>
          - modelId: <other-model-id>          # per-model profile override
            profile: <profile-id>

      # Remote Provider Example (WatsonX, hosted_vllm — no hardware profiles)
      - providerId: <watsonx | hosted_vllm>
        recommendations:
          - modelId: <primary-model-id>        # no profile for remote; first entry = primary
```

#### Complete Key Reference

| Key | Level | Type | Required? | Description |
| :--- | :--- | :--- | :--- | :--- |
| `models` | Root | `list` | Optional | Top-level section declaring recommendations. If omitted, all models and profiles from the provider are offered with no pre-selection hints. `recommendations` are purely additive — they do not restrict the model set. |
| `componentType` | `models[]` | `string` | **Yes** (if `models` present) | The component dependency type (`llm`, `embedding`, `reranker`). Must match one of `dependencies`. |
| `providers` | `models[]` | `list` | **Yes** | List of provider-specific contract entries. |
| `providerId` | `providers[]` | `string` | **Yes** | Registered provider identifier (local: `vllm-spyre`, `vllm-cpu`; remote: `watsonx`, `hosted_vllm`). |
| `recommendations` | `providers[]` | `list<object>` | Optional | Ordered list of model+profile recommendations. The **first entry** is the primary (pre-selected, "★ Recommended"). Every entry's `profile` is pre-selected when that model is picked. Returned as-is in the service / architecture `deploy-options` response. |
| `modelId` | `recommendations[]` | `string` | **Yes** | The model ID to recommend. |
| `profile` | `recommendations[]` | `string` | Optional | The recommended profile ID (local providers only; omitted for remote). |

---

### 4.2 Updated `metadata.yaml` for Services

#### `services/chat/metadata.yaml`

Chat is a conversational RAG workload. On Spyre it recommends `granite-4.1-8b-fp8` at `standard`. For `granite-3.3-8b-instruct`, it overrides the recommended profile to `balanced` (2 cards):

```yaml
# Added to services/chat/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        recommendations:
          - modelId: ibm-granite/granite-4.1-8b-fp8    # primary — pre-selected in UI
            profile: standard
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profile: balanced
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
    providers:
      - providerId: vllm-spyre
        recommendations:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            profile: standard
```

#### `services/extract/metadata.yaml`

Extract is a short-context, structured extraction workload validated for single-card execution. It recommends `compact-4k` for all models:

```yaml
# Added to services/extract/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        recommendations:
          - modelId: ibm-granite/granite-4.1-8b-fp8    # primary — pre-selected in UI
            profile: compact-4k
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profile: compact-4k
```

#### `services/translate/metadata.yaml`

Translate processes token chunks in short context on 1 card. It recommends `compact-4k`:

```yaml
# Added to services/translate/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        recommendations:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            profile: compact-4k
```

#### `services/similarity/metadata.yaml`

Similarity depends on `embedding` and `reranker`. It provides recommendations for each:

```yaml
# Added to services/similarity/metadata.yaml
models:
  - componentType: embedding
    providers:
      - providerId: vllm-cpu
        recommendations:
          - modelId: ibm-granite/granite-embedding-278m-multilingual
            profile: standard
  - componentType: reranker
    providers:
      - providerId: vllm-spyre
        recommendations:
          - modelId: BAAI/bge-reranker-v2-m3
            profile: standard
```

---

## 5. How the backend uses the contract

### Step A — Delivering the contract to the UI (service / architecture deploy-options)

`values.schema.json` no longer contains `model` or `profile`; models come from each provider's `models.yaml` and profiles from `profiles.yaml`, exposed through the generic catalog endpoints (`GET /api/v1/catalog/models`, `GET /api/v1/catalog/models/:id/deploy-options`). These are **service-agnostic**: they take no `service` parameter and are never filtered or annotated.

The service contract reaches the UI through the deploy-options response of the service (`GET /api/v1/services/:id/deploy-options`) and of architectures that include it (`GET /api/v1/architectures/:id/deploy-options`). For each model component, the backend copies the provider's `recommendations` list from the matching `services/<id>/metadata.yaml` `models[]` entry onto the provider in the `catalog` object, as `{ model_id, profile }` entries (`profile` omitted for remote providers), in declared order. The key is omitted when the service declares none.

The UI then:

1. Calls `models.url` for the full model list and pre-selects the **first** `recommendations` entry's `model_id`, rendering the **"★ Recommended"** badge on it.
2. When the user picks a model, calls `deploy_options.url` for its profiles and pre-selects the `profile` from that model's `recommendations` entry (falling back to the profile marked `default`).
3. If a model has exactly one profile, hides the profile dropdown — the profile is auto-injected at deploy time.

### Step B — Profile resolution (deploy time)

When the user submits an application or service deployment, any valid model and profile the user selected is honored, even if it differs from the recommendation.

**Auto-Resolution / Default Injection:** If the user did not provide a `profile` (because only 1 profile exists for the model, or the user submitted default parameters):
- If only 1 profile exists for that model $\rightarrow$ inject that single profile.
- If `recommendations` contains an entry for this model $\rightarrow$ inject that entry's `profile`.
- Otherwise $\rightarrow$ inject the provider's default profile from `profiles.yaml`.

### Step C — Bundle upload validation (BYOS only)

For user-defined service bundles uploaded via `POST /api/v1/catalog/bundles`, the validator checks:

1. `componentType` matches one of the bundle's declared `dependencies`.
2. `providerId` matches a known provider.
3. Every `recommendations[*].modelId` exists in the provider's `models.yaml`.
4. Every `recommendations[*].profile` exists in `profiles.yaml` and is allowed for that model.

### Deploy-options response shape

For a model provider inside a service component entry (`GET /api/v1/services/:id/deploy-options`):

```json
{
  "type": "llm",
  "providers": [
    {
      "id": "vllm-spyre",
      "category": "model",
      "models":         { "url": "/api/v1/catalog/models?component_type=llm" },
      "deploy_options": { "url": "/api/v1/catalog/models/{model_id}/deploy-options?provider_id=vllm-spyre" },
      "recommendations": [
        { "model_id": "<primary-model-id>", "profile": "<profile-id>" },
        { "model_id": "<other-model-id>",   "profile": "<profile-id>" }
      ]
    }
  ]
}
```

**Field reference:**
- `recommendations[0].model_id` — the primary recommended model; UI renders the **"★ Recommended"** badge and pre-selects it.
- `recommendations[*].profile` — profile the UI pre-selects when the user picks that model (including on initial load).
- `recommendations` is optional per provider; when absent the provider's full model and profile lists are offered with no pre-selection hint.

### Implementation Go Structs

```go
// In internal/pkg/catalog/apiserver/services/bundle/validate/metadata/bundle.go

type ServiceMetadataYAML struct {
    // ... existing metadata fields ...
    Models []ServiceModelContract `yaml:"models,omitempty" json:"models,omitempty"`
}

type ServiceModelContract struct {
    ComponentType string                    `yaml:"componentType" json:"componentType"`
    Providers     []ServiceProviderContract `yaml:"providers" json:"providers"`
}

type ServiceProviderContract struct {
    ProviderID      string                `yaml:"providerId" json:"providerId"`
    Recommendations []ModelRecommendation `yaml:"recommendations,omitempty" json:"recommendations,omitempty"`
}

type ModelRecommendation struct {
    ModelID string `yaml:"modelId" json:"modelId"`
    Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
}
```

---

## 6. What changes

| Asset | Change |
|---|---|
| `assets/services/*/metadata.yaml` | Optionally add `models:` block with `recommendations` |
| Catalog backend — service / architecture `deploy-options` | Copy each provider's `recommendations` from `services/<id>/metadata.yaml` into the `catalog` object of the matching model provider |
| Catalog backend — `GET /catalog/models` and `GET /catalog/models/:id/deploy-options` | No change — remain generic, service-agnostic, unfiltered |
| Catalog backend — deploy handler | Honor user profile override or auto-inject the recommended profile |
| Bundle upload validator — `ServiceMetadataYAML` | Add `Models []ServiceModelContract` struct and validate recommendation references |
| `assets/components/*/*/models.yaml` | No change — remains canonical source of all models |
| `assets/components/*/*/profiles.yaml` | No change — provider-level source of truth for all hardware profiles |
| Templates | No change — consume resolved flat parameters |

---

## 7. What this does NOT change

- **Component independence:** Adding a new model to `vllm-spyre` immediately makes it available across all services without updating any service metadata files.
- **Provider asset integrity:** The on-disk `models.yaml` and `profiles.yaml` are never modified; the contract is only surfaced alongside them in the service / architecture deploy-options response.
- **Standalone model deployment:** Standalone model serving without a service context uses the generic catalog endpoints, which return all models and all profiles without service-specific recommendations.

---

## 8. Endpoint changes

#### 1. Service deploy-options: `GET /api/v1/services/{id}/deploy-options`
#### 2. Architecture deploy-options: `GET /api/v1/architectures/{id}/deploy-options`
- For each `llm` / `embedding` / `reranker` component, adds `recommendations` (as declared in `services/<id>/metadata.yaml`) to each model provider's `catalog` object. Omitted when not declared.
- Remote providers (`watsonx`, `hosted_vllm`) have no profiles, so their `recommendations` entries carry only `model_id`.

#### 3. Unchanged
- `GET /api/v1/catalog/models` and `GET /api/v1/catalog/models/:id/deploy-options` take no `service` parameter and return unfiltered, unannotated data.
- `GET /api/v1/components/{type}/providers/{id}/params` and `GET /api/v1/connectors/models/providers/{id}/params` are unchanged.

---

## 9. Worked examples end-to-end

### Scenario A — Recommendation + Per-Model Override (Chat service)

1. Provider `vllm-spyre` defines `granite-4.1-8b-fp8`, `granite-3.3-8b-instruct`, and `granite-20b-code`.
2. `services/chat/metadata.yaml` specifies two `recommendations` entries.
3. User opens Chat deploy form → UI calls `GET /api/v1/services/chat/deploy-options`. The `vllm-spyre` provider carries `recommendations: [{ granite-4.1-8b-fp8, standard }, { granite-3.3-8b-instruct, balanced }]`.
4. UI calls `models.url` (unfiltered — all three models are listed) and pre-selects `granite-4.1-8b-fp8` with a **"★ Recommended"** badge. It calls `deploy_options.url` for that model and pre-sets the profile dropdown to `standard`.
5. User switches model to `granite-3.3-8b-instruct` → UI pre-selects `balanced` from that model's recommendation entry.
6. User overrides to `standard` and submits `{ model: granite-3.3-8b-instruct, profile: standard }`.
7. Backend honors the selection. Template renders with 4 cards.

### Scenario B — Recommendation Only (Extract service)

1. `granite-4.1-8b-fp8` in `vllm-spyre` allows profiles `[standard, balanced, compact-4k]`.
2. `services/extract/metadata.yaml` specifies `recommendations: [{ modelId: granite-4.1-8b-fp8, profile: compact-4k }]`.
3. User opens Extract deploy form → `GET /api/v1/services/extract/deploy-options` returns that `recommendations` on the `vllm-spyre` provider.
4. UI pre-selects `granite-4.1-8b-fp8` with the **"★ Recommended"** badge, fetches its profiles from `deploy_options.url`, and pre-selects `compact-4k` in the profile dropdown. The user may still pick `standard` or `balanced`.
5. User submits with the default selection → pod launches with 1 Spyre card.

---

## 10. Future Scope

### 10.1 Service-level exclusions (deferred)

A service could declare `exclusions` per provider in `services/<name>/metadata.yaml` to restrict what the user can pick:

```yaml
exclusions:
  - modelId: <model-id-to-exclude>     # no profiles key = exclude the model entirely
  - modelId: <model-id>
    profiles: [<profile-id-to-exclude>] # only these profiles are excluded for this model
```

Use cases: hiding specialized code models (e.g. `granite-20b-code`) from conversational services, and hiding multi-card profiles from single-card services such as `extract` and `translate`. If added, exclusions would be returned next to `recommendations` in the service / architecture deploy-options response for the client to apply, and re-validated by the backend at deploy time (`MODEL_NOT_SUPPORTED_BY_SERVICE` / `PROFILE_NOT_SUPPORTED_BY_SERVICE`).

### 10.2 Database-persisted recommendations

If dynamic runtime customization is needed in the future (e.g. allowing cluster administrators to update recommendations via an Admin UI without redeploying code):

```sql
CREATE TABLE service_model_contracts (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id           VARCHAR(100) NOT NULL,          -- e.g. 'chat', 'extract'
    component_type       VARCHAR(50)  NOT NULL,          -- 'llm', 'embedding', 'reranker'
    provider_id          VARCHAR(50)  NOT NULL,          -- 'vllm-spyre', 'vllm-cpu', 'watsonx'
    recommendations      JSONB,                          -- JSON array: [{"modelId": "...", "profile": "standard"}]; first entry = primary
    is_builtin           BOOLEAN      NOT NULL DEFAULT false, -- true for seed rows from YAML
    created_by           VARCHAR(100) NOT NULL DEFAULT 'system',
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (service_id, component_type, provider_id)
);
```

- **Seed on Startup:** Seeded from `services/*/metadata.yaml` with `is_builtin = true`.
- **Admin Overrides:** Administrators can adjust recommendations dynamically via `PUT /api/v1/services/:service_id/contracts`. Built-in baseline entries remain read-only.
