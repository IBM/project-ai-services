# Proposal: Service–Model Contracts (Exclusion & Recommendation Strategy)

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

## 2. Core Architectural Principle: Default-All (Exclusion & Recommendation)

To eliminate maintenance overhead, ensure zero-friction onboarding of new models, and provide guided user experience:

1. **Services DO NOT define or enumerate supported models.** The component providers
   (`values.schema.json` and `profiles.yaml`) remain the single canonical source of truth for
   all available models and hardware profiles.
2. **By default, every service supports all providers, all models, and all profiles.** When a
   new model or profile is added to `vllm-spyre`, all services inherit it immediately without
   requiring edits to service metadata files.
3. **Services ONLY declare exclusions when needed:**
   - If a service cannot work with a specific model $\rightarrow$ list it in `excludeModels`.
   - If a service cannot run certain hardware profiles for a model $\rightarrow$ list them in `excludeProfiles`.
4. **Services can override recommended profiles per model:**
   - **Service Primary Recommendation:** `recommended: { modelId, profile }` pre-selects the primary model & profile when the service form opens.
   - **Per-Model Recommended Profile Override:** `recommendedProfiles: [{ modelId, profile }]` overrides the component's default profile for specific models when selected in this service.
5. **User Flexibility:** Recommendations guide the user with **"★ Recommended"** badges and pre-selections, but users are free to override the profile at deploy time with any other non-excluded profile.
6. **Zero-configuration for open services:** If a service has no incompatibilities and requires
   no custom recommendations, its `models` section is omitted entirely.

---

## 3. Where does the contract live?

### Same `metadata.yaml`, not a new file

Model recommendations and profile restrictions belong in the service's top-level metadata:

```
services/<name>/metadata.yaml            ← service-level (identity, dependencies, exclusions, recommendations)
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
    # Optional: Exclude entire providers (e.g. if service cannot run on remote models)
    excludeProviders:
      - <provider-id-to-exclude>

    providers:
      # Local Provider Example (includes hardware profile recommendations and exclusions)
      - providerId: <vllm-spyre | vllm-cpu>
        recommended:
          modelId: <recommended-model-id>
          profile: <recommended-profile-id>
        recommendedProfiles:
          - modelId: <model-id>
            profile: <recommended-profile-id>
        excludeModels:
          - <model-id-to-exclude>
        excludeProfiles:
          - modelId: <model-id>
            profiles:
              - <profile-id-to-exclude>

      # Remote Provider Example (WatsonX, hosted_vllm — no hardware profiles)
      - providerId: <watsonx | hosted_vllm>
        recommended:
          modelId: <recommended-model-id>    # profile is omitted for remote
        excludeModels:
          - <model-id-to-exclude>             # excludes unsuited remote models
```

#### Complete Key Reference

| Key | Level | Type | Required? | Description |
| :--- | :--- | :--- | :--- | :--- |
| `models` | Root | `list` | Optional | Top-level section declaring model restrictions and recommendations. If omitted, all models/profiles are supported. |
| `componentType` | `models[]` | `string` | **Yes** (if `models` present) | The component dependency type (`llm`, `embedding`, `reranker`). Must match one of `dependencies`. |
| `excludeProviders` | `models[]` | `list<string>` | Optional | List of provider IDs (e.g. `watsonx`, `hosted_vllm`) **completely excluded** by this service. |
| `providers` | `models[]` | `list` | **Yes** | List of provider-specific contract entries. |
| `providerId` | `providers[]` | `string` | **Yes** | Registered provider identifier (local: `vllm-spyre`, `vllm-cpu`; remote: `watsonx`, `hosted_vllm`). |
| `recommended` | `providers[]` | `object` | Optional | Primary recommended model (and profile for local) for this service under this provider. |
| `modelId` | `recommended` | `string` | **Yes** (if `recommended` used) | The primary recommended model ID (pre-selected in UI with "★ Recommended" badge). |
| `profile` | `recommended` | `string` | Optional | The primary recommended profile ID for local providers (omitted for remote connectors). |
| `recommendedProfiles` | `providers[]` | `list<object>` | Optional | List of per-model profile recommendation overrides (local providers only). |
| `modelId` | `recommendedProfiles[]` | `string` | **Yes** | The target model ID whose recommended profile is being overridden. |
| `profile` | `recommendedProfiles[]` | `string` | **Yes** | The recommended profile ID for that model in this service. |
| `excludeModels` | `providers[]` | `list<string>` | Optional | List of model IDs from this provider that are **not supported** by the service (works for both local models and remote models). |
| `excludeProfiles` | `providers[]` | `list<object>` | Optional | List of model-specific profile exclusions (local providers only). |
| `modelId` | `excludeProfiles[]` | `string` | **Yes** (if `excludeProfiles` used) | The model ID whose profiles are being restricted. |
| `profiles` | `excludeProfiles[]` | `list<string>` | **Yes** | List of profile IDs (e.g., `standard`, `balanced`) to exclude for this specific model. |

---

### 4.2 Updated `metadata.yaml` for Services

#### `services/chat/metadata.yaml`

Chat is a conversational RAG workload. On Spyre it recommends `granite-4.1-8b-fp8` at `standard`. For `granite-3.3-8b-instruct`, it overrides the recommended profile to `balanced` (2 cards). It excludes specialized code models:

```yaml
# Added to services/chat/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        # Primary recommended model + profile
        recommended:
          modelId: ibm-granite/granite-4.1-8b-fp8
          profile: standard
        # Override recommended profile when granite-3.3 is selected
        recommendedProfiles:
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profile: balanced
        excludeModels:
          - ibm-granite/granite-20b-code
      - providerId: watsonx
        recommended:
          modelId: ibm/granite-3-8b-instruct
```

#### `services/summarize/metadata.yaml`

Summarize operates over long documents. It supports all models and profiles, recommending `granite-4.1-8b-fp8` at `standard`:

```yaml
# Added to services/summarize/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        recommended:
          modelId: ibm-granite/granite-4.1-8b-fp8
          profile: standard
```

#### `services/extract/metadata.yaml`

Extract is a short-context, structured extraction workload validated for single-card execution. It sets the primary recommendation to `compact-4k`, overrides the recommended profile for other models to `compact-4k`, and excludes multi-card profiles:

```yaml
# Added to services/extract/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        recommended:
          modelId: ibm-granite/granite-4.1-8b-fp8
          profile: compact-4k
        recommendedProfiles:
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profile: compact-4k
        excludeProfiles:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            profiles:
              - standard
              - balanced
          - modelId: ibm-granite/granite-3.3-8b-instruct
            profiles:
              - standard
              - balanced
```

#### `services/translate/metadata.yaml`

Translate processes token chunks in short context on 1 card. It recommends `compact-4k` and excludes multi-card profiles:

```yaml
# Added to services/translate/metadata.yaml
models:
  - componentType: llm
    providers:
      - providerId: vllm-spyre
        recommended:
          modelId: ibm-granite/granite-4.1-8b-fp8
          profile: compact-4k
        excludeProfiles:
          - modelId: ibm-granite/granite-4.1-8b-fp8
            profiles:
              - standard
              - balanced
```

#### `services/similarity/metadata.yaml`

Similarity depends on `embedding` and `reranker`. It provides recommendations for each:

```yaml
# Added to services/similarity/metadata.yaml
models:
  - componentType: embedding
    providers:
      - providerId: vllm-cpu
        recommended:
          modelId: ibm-granite/granite-embedding-278m-multilingual
          profile: standard
  - componentType: reranker
    providers:
      - providerId: vllm-spyre
        recommended:
          modelId: BAAI/bge-reranker-v2-m3
          profile: standard
```

---

## 5. How the backend uses the contract

### Step A — Schema filtering, defaults, and recommendations (schema serve time)

When the UI requests component params for a provider in the context of a service
(`GET /api/v1/components/{type}/providers/{id}/params?service=<serviceId>`):

1. The backend reads the component provider's base schema (`values.schema.json`) containing all models in `model.oneOf`.
2. **Apply Exclusions:**
   - **Filter models:** Drop any `model.oneOf` entry whose `const` is listed in `excludeModels`.
   - **Filter profiles:** For each surviving model, subtract any profiles listed under `excludeProfiles` for that `modelId` from its `x-profiles`.
3. **Apply Recommendations & Profile Overrides:**
   - If `recommended.modelId` is specified and not excluded, set it as the schema `model.default` and mark it with `x-recommended: true`.
   - If `recommended.profile` is specified for that model, set it as the initial `profile.default`.
   - Embed a `recommendedProfiles` lookup map in the returned schema metadata. When the user switches the selected model in the UI dropdown, the UI dynamically changes the pre-selected and badged profile to that model's recommended profile.
4. **Profile Selector Visibility:**
   - If profile filtering leaves $>1$ profile for the selected model: UI displays a profile dropdown allowing the user to accept the recommended profile or override it with another valid profile.
   - If profile filtering leaves exactly $1$ profile: Backend omits the profile selector from the schema (auto-injected at deploy time).
5. Returns the filtered and annotated schema to the UI.

### Step B — Deployment validation and profile resolution (deploy time)

When the user submits an application or service deployment:

1. **Model Check:** Backend verifies that the submitted `model` is NOT in `excludeModels`. If excluded, returns `HTTP 400 Bad Request` (`MODEL_NOT_SUPPORTED_BY_SERVICE`).
2. **Profile Check:** Backend verifies that the submitted `profile` is NOT in `excludeProfiles` for that model. If excluded, returns `HTTP 400 Bad Request` (`PROFILE_NOT_SUPPORTED_BY_SERVICE`).
3. **User Profile Override:** If the user selected a valid non-recommended profile (e.g. user selected `balanced` instead of recommended `standard`), the user's explicit selection is honored.
4. **Auto-Resolution / Default Injection:** If the user did not provide a `profile` (because only 1 profile remained valid, or the user submitted default parameters):
   - If only 1 profile survives for that model $\rightarrow$ inject that single profile.
   - If `recommendedProfiles` defines an override for this model $\rightarrow$ inject that recommended profile.
   - If `recommended.profile` applies to this model $\rightarrow$ inject `recommended.profile`.
   - Otherwise $\rightarrow$ inject the provider's default profile from `profiles.yaml`.

### Step C — Bundle upload validation (BYOS only)

For user-defined service bundles uploaded via `POST /api/v1/catalog/bundles`, the validator checks:

1. `componentType` matches one of the bundle's declared `dependencies`.
2. `providerId` matches a known provider.
3. Any `recommended.modelId` or `recommendedProfiles[*].modelId` exists and is NOT in `excludeModels`.
4. Any `recommended.profile` or `recommendedProfiles[*].profile` exists in `profiles.yaml` and is NOT in `excludeProfiles`.
5. Every entry in `excludeModels` and `excludeProfiles` references valid catalog items.

#### Implementation Go Structs

```go
// In internal/pkg/catalog/apiserver/services/bundle/validate/metadata/bundle.go

type ServiceMetadataYAML struct {
    // ... existing metadata fields ...
    Models []ServiceModelContract `yaml:"models,omitempty" json:"models,omitempty"`
}

type ServiceModelContract struct {
    ComponentType    string                    `yaml:"componentType" json:"componentType"`
    ExcludeProviders []string                  `yaml:"excludeProviders,omitempty" json:"excludeProviders,omitempty"`
    Providers        []ServiceProviderContract `yaml:"providers" json:"providers"`
}

type ServiceProviderContract struct {
    ProviderID          string                   `yaml:"providerId" json:"providerId"`
    Recommended         *ModelRecommendation    `yaml:"recommended,omitempty" json:"recommended,omitempty"`
    RecommendedProfiles []ModelRecommendation   `yaml:"recommendedProfiles,omitempty" json:"recommendedProfiles,omitempty"`
    ExcludeModels       []string                 `yaml:"excludeModels,omitempty" json:"excludeModels,omitempty"`
    ExcludeProfiles     []ModelProfileExclusions `yaml:"excludeProfiles,omitempty" json:"excludeProfiles,omitempty"`
}

type ModelRecommendation struct {
    ModelID string `yaml:"modelId" json:"modelId"`
    Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
}

type ModelProfileExclusions struct {
    ModelID  string   `yaml:"modelId" json:"modelId"`
    Profiles []string `yaml:"profiles" json:"profiles"`
}
```

---

## 6. What changes

| Asset | Change |
|---|---|
| `assets/services/*/metadata.yaml` | Optionally add `models:` block with `recommended`, `recommendedProfiles`, and/or `excludeModels` / `excludeProfiles` |
| Catalog backend — deploy-options endpoint | Pass `?service=<id>` to params endpoint; subtract exclusions and attach recommendations |
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

Both local component params and remote connector params support the `?service=<id>` query parameter:

#### 1. Local Component Params: `GET /api/v1/components/{type}/providers/{id}/params?service=<id>`
- Reads `models` block from `services/<id>/metadata.yaml`.
- Strips `excludeModels` from `model.oneOf`.
- Strips `excludeProfiles` from `x-profiles` of matching models.
- Annotates `recommended.modelId` as `model.default` and `recommended.profile` as `profile.default`.
- Includes `recommendedProfiles` map in schema metadata.
- If a model has only 1 surviving profile, hides the `profile` field for that model in the UI schema.

#### 2. Remote Connector Params: `GET /api/v1/connectors/models/providers/{id}/params?service=<id>`
- Reads `models` block from `services/<id>/metadata.yaml`.
- Strips `excludeModels` from `model_name.oneOf` (e.g. for `watsonx` or `hosted_vllm`).
- Annotates `recommended.modelId` as `model_name.default` with `"x-recommended": true`.
- If the entire provider is in `excludeProviders`, returns `403 Forbidden` (`PROVIDER_NOT_SUPPORTED_BY_SERVICE`).

---

## 9. Worked examples end-to-end

### Scenario A — Recommendation + Per-Model Override + Exclusion (Chat service)

1. Provider `vllm-spyre` defines `granite-4.1-8b-fp8`, `granite-3.3-8b-instruct`, and `granite-20b-code`.
2. `services/chat/metadata.yaml` specifies:
   - `recommended: { modelId: granite-4.1-8b-fp8, profile: standard }`
   - `recommendedProfiles: [{ modelId: granite-3.3-8b-instruct, profile: balanced }]`
   - `excludeModels: [ibm-granite/granite-20b-code]`
3. User opens Chat deploy form $\rightarrow$ UI calls `GET .../params?service=chat`.
4. Backend filters out `granite-20b-code`.
5. UI displays `granite-4.1-8b-fp8` pre-selected with a **"★ Recommended"** badge and profile selector pre-selected to `standard`.
6. User switches model to `granite-3.3-8b-instruct`:
   - UI dynamically pre-selects `balanced` (marked with **"★ Recommended"** chip for this model).
   - User decides to override it to `standard` (4-card) $\rightarrow$ User submits `{ model: granite-3.3-8b-instruct, profile: standard }`.
7. Backend validates `standard` is not excluded $\rightarrow$ Pass. Template renders with 4 cards.

### Scenario B — Recommendation + Exclusion of multi-card profiles (Extract service)

1. Model `granite-4.1-8b-fp8` defines `x-profiles: [standard, balanced, compact-4k]`.
2. `services/extract/metadata.yaml` specifies:
   - `recommended: { modelId: granite-4.1-8b-fp8, profile: compact-4k }`
   - `excludeProfiles: [{ modelId: granite-4.1-8b-fp8, profiles: [standard, balanced] }]`
3. User opens Extract deploy form $\rightarrow$ UI calls `GET .../params?service=extract`.
4. Backend subtracts `[standard, balanced]`, leaving only `compact-4k`.
5. Because only 1 profile survives, backend hides the profile selector dropdown.
6. User selects `granite-4.1-8b-fp8` (shown with **"★ Recommended"** badge) and submits.
7. Backend auto-injects `profile: compact-4k` $\rightarrow$ pod launches with 1 Spyre card.

---

## 10. Future Scope: Database-Persisted Exclusions & Recommendations

If dynamic runtime customization is needed in the future (e.g. allowing cluster administrators to update recommendations or exclude newly deprecated models via an Admin UI without redeploying code):

### 10.1 `service_model_contracts` table (Future Scope)

```sql
CREATE TABLE service_model_contracts (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id           VARCHAR(100) NOT NULL,          -- e.g. 'chat', 'extract'
    component_type       VARCHAR(50)  NOT NULL,          -- 'llm', 'embedding', 'reranker'
    provider_id          VARCHAR(50)  NOT NULL,          -- 'vllm-spyre', 'vllm-cpu', 'watsonx'
    recommended_model_id VARCHAR(255),                   -- e.g. 'ibm-granite/granite-4.1-8b-fp8'
    recommended_profile  VARCHAR(100),                   -- e.g. 'standard'
    recommended_profiles JSONB,                          -- JSON array: [{"modelId": "...", "profile": "balanced"}]
    exclude_models       JSONB,                          -- JSON array: ["ibm-granite/granite-20b-code"]
    exclude_profiles     JSONB,                          -- JSON array: [{"modelId": "...", "profiles": ["standard"]}]
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
