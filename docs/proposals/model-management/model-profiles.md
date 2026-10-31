# Proposal: Model Hardware Profiles

- **Status:** Draft / Proposal
- **Created**: October 2026

---

## 1. Problem Statement

Three hardware-tuning parameters govern how vLLM runs at deployment time:

| Parameter | vllm-spyre env / resource | vllm-cpu CLI flag |
|---|---|---|
| Number of AIU cards (tensor-parallelism degree) | `AIU_WORLD_SIZE` · `podman.io/device=/dev/vfio` · annotation `ai-services.io/llm--spyre-cards` | N/A |
| Maximum context length | `MAX_MODEL_LEN` → `--max-model-len` | `--max-model-len` |
| Maximum batch size | `MAX_BATCH_SIZE` → `--max-num-seqs` | `--max-num-batched-tokens` |

Today these parameters are scattered and disconnected:

**1. Card count is hardcoded in three unconnected places** inside
[`vllm-spyre/podman/templates/vllm-server.yaml.tmpl`](../../ai-services/assets/components/llm/vllm-spyre/podman/templates/vllm-server.yaml.tmpl):

```
annotation  ai-services.io/llm--spyre-cards: "4"   ← line 15
env         AIU_WORLD_SIZE: "4"                     ← line 60
resource    podman.io/device=/dev/vfio: 4           ← line 79
```

Changing one without the others produces a pod that requests the wrong number
of device handles for its tensor-parallelism degree — this crashes at model load
time and is difficult to diagnose.

**2. `maxModelLen` and `maxBatchSize` exist in `values.yaml` but not in
`values.schema.json`.** Because `additionalProperties: false` is set, the UI
never shows them and the validator rejects any client that sends them. They are
effectively invisible and cannot be changed without editing files directly.

**3. IBM Spyre validates distinct configurations per use case.** The same
`granite-4.1-8b-fp8` model runs at 4 cards / 32 768 context for RAG
inferencing and 1 card / 3 072 context for entity extraction. There is no way
to represent this distinction today — the component always uses RAG-inferencing
defaults regardless of the actual workload.

**4. `metadata.yaml` `accelerators.ibm.com/spyre_pf: 4` is static**, diverging
from what the template actually requests and breaking the resource-requirements
display in the UI when a smaller configuration is used.

---

## 2. Profile naming system

### 2.1 Design rule

Profile names encode **card count** (the primary hardware dimension) and
**context window** (the secondary differentiator when multiple configurations
share the same card count). The rule is:

> **`standard`** = 4 cards · full context.
> **`balanced`** = 2 cards · mid context.
> **`compact-4k`** = 1 card · 4 096 context.

`standard` and `balanced` never need a context suffix because IBM has only one
validated config at 4 cards and one at 2 cards respectively.

### 2.2 Profile derivation table

This is the authoritative mapping from IBM Spyre RHAIIS 3.6 validated use-case
configurations (source: IBM docs, last updated 2026-09-22) to profile IDs.

| Profile ID | Cards | Max context | Batch | Memory | shm | IBM use case / notes |
|---|---|---|---|---|---|---|
| `standard` | 4 | 32,768 | 32 | 200 GB | 2 GB | RAG inferencing — all supported LLMs |
| `balanced` | 2 | 8,192 | 32 | 150 GB | 2 GB | Entity extraction — Mistral-Small-24B, Ministral-14b-BF16 2-card option (RHAIIS 3.6) |
| `compact-4k` | 1 | 4,096 | 32 | 100 GB | default | Entity extraction — Granite-3.3, Granite-4.1-fp8, Llama-3.1-8B, Ministral-14b-BF16 1-card option (RHAIIS 3.6) |

**Reranker and embedding profiles** — these component types each have a single
validated configuration so no naming distinction is needed:

| Component | Profile ID | Cards | Max context | Batch | Memory | shm |
|---|---|---|---|---|---|---|
| `reranker/vllm-spyre` | `standard` | 1 | 8,192 | 4 | 50 GB | default |
| `reranker/vllm-cpu` | `standard` | — | 8,192 | 4 | 50 GB | default |
| `embedding/vllm-cpu` | `standard` | — | 512 | 256 | 50 GB | default |

**CPU LLM profiles** — no card dimension:

| Profile ID | Max context | Batch | Notes |
|---|---|---|---|
| `standard` | 8,096 | 32 | Default CPU config |

---

## 3. Solution: Standalone `profiles.yaml` per provider + GET endpoint

### 3.1 Design

Profiles apply to any provider whose `metadata.yaml` carries `category: model` — covering `llm` (`vllm-spyre`, `vllm-cpu`), `reranker` (`vllm-spyre`, `vllm-cpu`), and `embedding` (`vllm-cpu`). Providers without `category: model` return `400` on profile endpoints.

Each model-category provider carries a single, runtime-agnostic `profiles.yaml` directly at the **provider level** (e.g. `assets/components/llm/vllm-spyre/profiles.yaml`), shared across both `podman/` and `openshift/` runtimes. Profile definitions live in that provider-level file; `values.schema.json` retains only the `x-profiles` list and `x-default-profile` on each model entry. Hardware params (`numCards`, `maxModelLen`, etc.) are never sent by the client — the backend reads and merges them from the provider's `profiles.yaml` at deploy time.

```
assets/components/
  llm/
    vllm-spyre/
      profiles.yaml            ← provider-level (shared across podman & openshift)
      podman/
        values.schema.json     ← x-profiles & x-default-profile per model
        values.yaml
      openshift/
        values.schema.json
        values.yaml
    vllm-cpu/
      profiles.yaml            ← provider-level (shared across podman & openshift)
      podman/
        values.schema.json
        values.yaml
      openshift/
        values.schema.json
        values.yaml
  reranker/
    vllm-spyre/
      profiles.yaml            ← provider-level
      podman/
        values.schema.json
        values.yaml
    vllm-cpu/
      profiles.yaml            ← provider-level
      podman/
        values.schema.json
        values.yaml
  embedding/
    vllm-cpu/
      profiles.yaml            ← provider-level
      podman/
        values.schema.json
        values.yaml
```

### 3.2 `profiles.yaml` structure (example: `llm/vllm-spyre`)

```yaml
- id: standard
  default: false
  title: "Standard (4 cards)"
  description: "Full-context RAG inferencing — 4 Spyre cards, 32 768 context."
  params:
    numCards: 4
    maxModelLen: 32768
    maxBatchSize: 32
    memory: "200Gi"
    shmSize: "2Gi"

- id: compact-4k
  default: true
  title: "Compact 4 k (1 card)"
  description: "Short-context entity extraction — 1 Spyre card, 4 096 context."
  params:
    numCards: 1
    maxModelLen: 4096
    maxBatchSize: 32
    memory: "100Gi"
    shmSize: "2Gi"

# … balanced follows the same shape (2 cards, 8 192 context)
```

`params` keys match `values.yaml` exactly (flat camelCase). The profile with `"default": true` is used when the caller omits `profile` from the deploy request.

### 3.3 `llm/vllm-spyre/values.schema.json` — both `podman/` and `openshift/`

The schema retains the `profile` field for validation and the `x-profiles` list on each model `oneOf` entry. The embedded `profiles` array is removed — definitions now live in `profiles.yaml`.


```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "model": {
      "type": "string",
      "title": "Large language model (LLM)",
      "oneOf": [
        {
          "const": "ibm-granite/granite-3.3-8b-instruct",
          "title": "granite-3.3-8b-instruct",
          "description": "...",
          "x-profiles": ["standard", "balanced", "compact-4k"],
          "x-default-profile": "standard"
        },
        {
          "const": "ibm-granite/granite-4.1-8b-fp8",
          "title": "granite-4.1-8b-fp8",
          "description": "...",
          "x-profiles": ["standard", "balanced", "compact-4k"],
          "x-default-profile": "standard"
        },
        {
          "const": "meta-llama/Llama-3.1-8B-Instruct",
          "title": "Llama-3.1-8B-Instruct",
          "description": "...",
          "x-profiles": ["standard", "compact-4k"],
          "x-default-profile": "standard"
        },
        {
          "const": "mistralai/Mistral-Small-3.2-24B-Instruct",
          "title": "Mistral-Small-3.2-24B-Instruct",
          "description": "...",
          "x-profiles": ["standard", "balanced"],
          "x-default-profile": "balanced"
        },
        {
          "const": "mistralai/Ministral-3-14B-Instruct-2512-BF16",
          "title": "Ministral-3-14B-Instruct-2512-BF16",
          "description": "...",
          "x-profiles": ["standard", "balanced", "compact-4k"],
          "x-default-profile": "compact-4k"
        }
      ],
      "default": "ibm-granite/granite-4.1-8b-fp8"
    },
    "profile": {
      "type": "string",
      "title": "Deployment profile",
      "x-profile-source": {
        "type": "api",
        "endpoint": "/api/v1/components/llm/providers/vllm-spyre/profiles",
        "x-profile-filter": "model"
      }
    },
    "apiKey": {
      "type": "string",
      "title": "API key (optional)",
      "format": "password"
    }
  }
}
```

> `x-profile-source` is a UI hint. It tells the frontend to call the given `endpoint` (appending `?{x-profile-filter}=<selected_model>`) after the user picks a model, and render the returned list as a dropdown. The backend validates the submitted value against the merged `profiles.yaml` list for that model — unknown values return `400`.

### 3.4 `llm/vllm-cpu/values.schema.json` — both `podman/` and `openshift/`

CPU has no cards. Profile names follow context window only.

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "model": { "...": "..." },
    "profile": {
      "type": "string",
      "title": "Deployment profile",
      "x-profile-source": {
        "type": "api",
        "endpoint": "/api/v1/components/llm/providers/vllm-cpu/profiles",
        "x-profile-filter": "model"
      }
    },
    "apiKey": { "...": "..." }
  }
}
```

### 3.5 `reranker/vllm-spyre/values.schema.json`

Single validated configuration — no dropdown shown in the UI (backend omits the
`profile` field from the returned schema entirely when only one profile is defined).

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "model": {
      "type": "string",
      "title": "Reranker model",
      "oneOf": [
        {
          "const": "BAAI/bge-reranker-v2-m3",
          "title": "bge-reranker-v2-m3",
          "description": "...",
          "x-profiles": ["standard"],
          "x-default-profile": "standard"
        }
      ],
      "default": "BAAI/bge-reranker-v2-m3"
    },
    "profile": {
      "type": "string",
      "title": "Deployment profile",
      "x-profile-source": {
        "type": "api",
        "endpoint": "/api/v1/components/reranker/providers/vllm-spyre/profiles",
        "x-profile-filter": "model"
      }
    }
  }
}
```

### 3.6 `reranker/vllm-cpu/values.schema.json`

Single validated configuration — no dropdown shown in the UI.

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "model": {
      "type": "string",
      "title": "Reranker model",
      "oneOf": [
        {
          "const": "BAAI/bge-reranker-v2-m3",
          "title": "bge-reranker-v2-m3",
          "description": "...",
          "x-profiles": ["standard"],
          "x-default-profile": "standard"
        }
      ],
      "default": "BAAI/bge-reranker-v2-m3"
    },
    "profile": {
      "type": "string",
      "title": "Deployment profile",
      "x-profile-source": {
        "type": "api",
        "endpoint": "/api/v1/components/reranker/providers/vllm-cpu/profiles",
        "x-profile-filter": "model"
      }
    }
  }
}
```

### 3.7 `embedding/vllm-cpu/values.schema.json`

Single validated configuration — no dropdown shown in the UI.

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "model": {
      "type": "string",
      "title": "Embedding model",
      "oneOf": [
        {
          "const": "ibm-granite/granite-embedding-278m-multilingual",
          "title": "granite-embedding-278m-multilingual",
          "description": "...",
          "x-profiles": ["standard"],
          "x-default-profile": "standard"
        }
      ],
      "default": "ibm-granite/granite-embedding-278m-multilingual"
    },
    "profile": {
      "type": "string",
      "title": "Deployment profile",
      "x-profile-source": {
        "type": "api",
        "endpoint": "/api/v1/components/embedding/providers/vllm-cpu/profiles",
        "x-profile-filter": "model"
      }
    }
  }
}
```

---

## 4. Changes to existing files per provider

### 4.1 `values.yaml` — remove derived fields, no `profile` default

The individual parameter fields (`maxModelLen`, `maxBatchSize`, etc.) are
removed from `values.yaml` because they are now authoritative in `profiles.yaml`.
No `profile` default is written here — the hardware params are already encoded
inside each model's `x-profiles` list in `values.schema.json`. The backend resolves
the correct profile at deploy time using the chosen model's `x-default-profile`
(falling back to the first entry in its `x-profiles` list).

**`llm/vllm-spyre/podman/values.yaml`**
```yaml
# BEFORE
image: registry.redhat.io/rhaii/vllm-spyre-rhel9:3.5.0
model: ""
apiKey: ""
maxModelLen: 32768
maxBatchSize: 32

# AFTER
image: registry.redhat.io/rhaii/vllm-spyre-rhel9:3.5.0
model: ""
apiKey: ""
# profile is intentionally absent — default is resolved per-model at deploy time
```

**`llm/vllm-cpu/podman/values.yaml`**
```yaml
# BEFORE
image: icr.io/ppc64le-oss/vllm-ppc64le:0.28.0
model: ""
apiKey: ""
maxNumBatchedTokens: 8096
maxModelLen: 8096
maxBatchSize: 32

# AFTER
image: icr.io/ppc64le-oss/vllm-ppc64le:0.28.0
model: ""
apiKey: ""
# profile is intentionally absent — default is resolved per-model at deploy time
```

**`llm/vllm-spyre/openshift/values.yaml`**
```yaml
# BEFORE
model: ""
apiKey: ""
maxModelLen: 32768
maxBatchSize: 32
resources:
  requests:
    cpu: "12"
    memory: "200Gi"
    ibm.com/spyre_pf: "4"
  limits:
    cpu: "12"
    memory: "200Gi"
    ibm.com/spyre_pf: "4"

# AFTER
model: ""
apiKey: ""
# profile is intentionally absent — default is resolved per-model at deploy time
# all hardware params are derived from profiles.yaml at render time
```

### 4.2 `values.schema.json` — model list and `x-profiles` only

The `profile` field remains for request validation (unknown value → `400`). The embedded `profiles` array and `x-profile-filter` are replaced by a single `x-profile-source` block:

```json
"x-profile-source": {
  "type": "api",
  "endpoint": "/api/v1/components/{component_type}/providers/{provider_id}/profiles",
  "x-profile-filter": "model"
}
```

This is a standard UI extension key — `x-` prefixed so JSON Schema validators ignore it. It tells the frontend where to fetch profile options and which query parameter carries the selected model. After the user picks a model, the UI calls that endpoint and renders the response as the profile dropdown. Profile definitions live in `profiles.yaml`, not in the schema.

### 4.3 Templates — read from resolved profile values

The template no longer contains if/else branches for profiles. The backend
resolves the selected profile from `profiles.yaml` and merges its `params` into
the values map before rendering. The template references these as ordinary
`.Values.*` keys.

**`llm/vllm-spyre/podman/templates/vllm-server.yaml.tmpl`** — replacements:

| Location | Before | After |
|---|---|---|
| Annotation (line 15) | `ai-services.io/llm--spyre-cards: "4"` | `ai-services.io/llm--spyre-cards: "{{ .Values.numCards }}"` |
| `volumes[dshm].sizeLimit` (line 21) | `sizeLimit: 64Gi` | `sizeLimit: {{ .Values.shmSize }}` |
| Env `AIU_WORLD_SIZE` (line 60) | `value: "4"` | `value: "{{ .Values.numCards }}"` |
| Env `MAX_MODEL_LEN` (line 64) | `value: "{{ .Values.maxModelLen }}"` | `value: "{{ .Values.maxModelLen }}"` (no change — key name stays, value now comes from profile) |
| Env `MAX_BATCH_SIZE` (line 65) | `value: "{{ .Values.maxBatchSize }}"` | `value: "{{ .Values.maxBatchSize }}"` (same) |
| `resources.requests.memory` (line 80) | `memory: "200Gi"` | `memory: "{{ .Values.memoryGB }}Gi"` |
| `resources.limits.memory` (line 82) | `memory: "200Gi"` | `memory: "{{ .Values.memoryGB }}Gi"` |
| `resources…/dev/vfio` (line 79) | `podman.io/device=/dev/vfio: 4` | `podman.io/device=/dev/vfio: {{ .Values.numCards }}` |

**`llm/vllm-spyre/openshift/templates/instruct-inferenceservice.yaml`** — replacements:

```yaml
# args
- '--tensor-parallel-size={{ .Values.numCards }} '
- '--max-model-len={{ .Values.maxModelLen }} '
- '--max-num-seqs={{ .Values.maxBatchSize }}'

# resources
requests:
  cpu: "12"
  memory: "{{ .Values.memoryGB }}Gi"
  ibm.com/spyre_pf: "{{ .Values.numCards }}"
limits:
  cpu: "12"
  memory: "{{ .Values.memoryGB }}Gi"
  ibm.com/spyre_pf: "{{ .Values.numCards }}"
```

**`llm/vllm-cpu/podman/templates/vllm-server.yaml.tmpl`** — replacements in `args`:

```
--max-num-batched-tokens={{ .Values.maxNumBatchedTokens }} \
--max-model-len={{ .Values.maxModelLen }} \
--max-num-seqs={{ .Values.maxBatchSize }}
```

**`llm/vllm-cpu/openshift/templates/instruct-inferenceservice.yaml`** — same replacements in `args`.

### 4.4 `metadata.yaml` — add `profiles` resource map

The `metadata.yaml` files carry a static `resources` block for the UI
resource-requirements tile. A `profiles` map is added so the tile updates
dynamically when the user changes the profile dropdown:

```yaml
# llm/vllm-spyre/podman/metadata.yaml  (openshift/metadata.yaml same)
resources:
  cpu: 8
  memory: 214748364800   # 200Gi — upper bound (standard profile)
  storage: 53687091200
  accelerators:
    ibm.com/spyre_pf: 4  # upper bound (standard profile)

# NEW — per-profile resource overrides keyed by profile ID.
# Matches every entry in llm/vllm-spyre/profiles.yaml.
profiles:
  standard:
    memory: 214748364800   # 200Gi
    accelerators:
      ibm.com/spyre_pf: 4
  balanced:
    memory: 161061273600   # 150Gi
    accelerators:
      ibm.com/spyre_pf: 2
  compact-4k:
    memory: 107374182400   # 100Gi
    accelerators:
      ibm.com/spyre_pf: 1
  compact-8k:
    memory: 107374182400   # 100Gi
    accelerators:
      ibm.com/spyre_pf: 1
  compact-16k:
    memory: 107374182400   # 100Gi
    accelerators:
      ibm.com/spyre_pf: 1
  minimal:
    memory: 107374182400   # 100Gi
    accelerators:
      ibm.com/spyre_pf: 1
```

> The top-level `resources` block stays at the maximum (standard — 4 cards,
> 200Gi) so the tile never understates needs before a profile is chosen. Once
> the user selects a model and profile, the UI overrides the display with the
> matching entry from `profiles`.

---

## 5. Backend changes

### 5.1 Data types (`catalog/types/`)

Two new types are added:

```go
// ProfileParam holds the hardware parameters for one profile entry.
// Field names and types match values.yaml keys exactly so mergeProfileParams()
// can copy them into the values map without any transformation.
type ProfileParam struct {
    NumCards            int    `yaml:"numCards"`            // new — maps to AIU_WORLD_SIZE / spyre_pf resource
    MaxModelLen         int    `yaml:"maxModelLen"`         // existing values.yaml key
    MaxBatchSize        int    `yaml:"maxBatchSize"`        // existing values.yaml key
    MaxNumBatchedTokens int    `yaml:"maxNumBatchedTokens"` // existing values.yaml key (vllm-cpu only)
    Memory              string `yaml:"memory"`              // new — "200Gi" string format, matches template/OCP resource
    ShmSize             string `yaml:"shmSize"`             // new — "2Gi" / "64Mi" string format, matches sizeLimit
}

// Profile is one entry in a provider's profiles.yaml.
type Profile struct {
    ID          string       `yaml:"id"`
    Default     bool         `yaml:"default"`
    Title       string       `yaml:"title"`
    Description string       `yaml:"description"`
    Params      ProfileParam `yaml:"params"`
}
```

### 5.2 `loadProfilesFromFile()` — new private helper

A new private helper reads `profiles.yaml` from the provider's top-level asset directory (e.g., `assets/components/<type>/<provider>/profiles.yaml`). Because profiles are runtime-agnostic, they are read once from the provider root, rather than duplicated inside runtime subdirectories. If no `profiles.yaml` exists the function returns `nil, nil` — not an error.

```go
func loadProfilesFromFile(providerRootDir string) ([]types.Profile, error) {
    data, err := os.ReadFile(filepath.Join(providerRootDir, "profiles.yaml"))
    if os.IsNotExist(err) {
        return nil, nil // provider has no profiles — not an error
    }
    if err != nil {
        return nil, fmt.Errorf("reading profiles.yaml: %w", err)
    }
    var profiles []types.Profile
    if err := yaml.Unmarshal(data, &profiles); err != nil {
        return nil, fmt.Errorf("parsing profiles.yaml: %w", err)
    }
    return profiles, nil
}
```

### 5.3 `GetComponentProviderParams()` — schema injection

[`GetComponentProviderParams()`](../../ai-services/internal/pkg/catalog/deploy_options.go)
already reads and returns `values.schema.json`. After decoding the schema, it
is extended to inject the filtered profile `oneOf` before returning.

The injection logic:
1. Calls `loadProfilesFromFile(providerDir)` — reads `profiles.yaml` from the provider's asset directory.
2. Reads the selected model's `x-profiles` list from the model's `oneOf` entry.
3. Intersects: keeps only profiles whose `id` appears in the model's `x-profiles`.
4. Injects the filtered list as the `oneOf` on the `profile` field.
5. For providers with a single profile, removes the `profile` field entirely.

```go
// After decoding values.schema.json into `schema`:
allProfiles, err := loadProfilesFromFile(providerDir)
if err == nil && len(allProfiles) > 0 {
    props, _ := schema["properties"].(map[string]any)

    // Build a lookup map: profile id → Profile
    profileByID := make(map[string]types.Profile, len(allProfiles))
    for _, pr := range allProfiles {
        profileByID[pr.ID] = pr
    }

    // Collect the x-profiles list for the currently selected model.
    // The model oneOf entries are in schema["properties"]["model"]["oneOf"].
    // At schema-serve time we don't know which model the user will pick, so
    // we return ALL profiles that appear in ANY model's x-profiles list —
    // the UI narrows the dropdown client-side once a model is selected.
    validIDs := make(map[string]struct{})
    if modelField, ok := props["model"].(map[string]any); ok {
        if oneOfList, ok := modelField["oneOf"].([]any); ok {
            for _, entry := range oneOfList {
                if m, ok := entry.(map[string]any); ok {
                    if xp, ok := m["x-profiles"].([]any); ok {
                        for _, id := range xp {
                            if s, ok := id.(string); ok {
                                validIDs[s] = struct{}{}
                            }
                        }
                    }
                }
            }
        }
    }

    if len(validIDs) <= 1 {
        // Only one distinct profile across all models — no choice needed.
        delete(props, "profile")
    } else {
        // Build ordered oneOf preserving profiles.yaml order, filtered to validIDs.
        oneOf := make([]map[string]any, 0, len(validIDs))
        for _, pr := range allProfiles {
            if _, ok := validIDs[pr.ID]; !ok {
                continue
            }
            oneOf = append(oneOf, map[string]any{
                "const":       pr.ID,
                "title":       pr.Title,
                "description": pr.Description,
            })
        }
        if profileField, ok := props["profile"].(map[string]any); ok {
            profileField["oneOf"] = oneOf
        }
    }
}
```

The UI's existing `oneOf` → Carbon Dropdown rendering in `schemaParser.ts`
works unchanged. When the user changes the model dropdown, the UI re-filters
the profile options using the selected model's `x-profiles` array (already
present in the schema response) — no additional API call needed.

### 5.4 `LoadComponentValues()` — profile resolution at deploy time

[`LoadComponentValues()`](../../ai-services/internal/pkg/catalog/catalog.go)
is the single function that builds the final values map passed to every
template. It is called from
[`ParamBuilder.buildComponentParams()`](../../ai-services/internal/pkg/catalog/apiserver/services/params/param_builder.go)
with the user-supplied `argParams` (containing `"profile": "compact-4k"` if the
user sent it) already applied via `SetNestedValue`.

The extension is inserted **after** the existing `argParams` merge so
`values["profile"]` already holds the user's selection at resolution time:

```go
// Existing steps (unchanged):
//   1. Read values.yaml       → values["profile"] is absent (no static default)
//   2. Apply argParams        → values["profile"] = "compact-4k" (user wins, if supplied)
//
// NEW step 3 — resolve selected profile and flatten its params into values:
// If the user did not supply a profile, selectedID is "" and the backend resolves
// the default profile for the chosen model from its `x-default-profile` in `values.schema.json`
// (falling back to the first profile in its `x-profiles` list).
profiles, err := loadProfilesFromFile(providerDir) // providerDir is the provider's asset directory, known at this call site
if err != nil {
    return nil, fmt.Errorf("failed to load profiles: %w", err)
}
if len(profiles) > 0 {
    selectedID, _ := values["profile"].(string)
    if selectedID == "" {
        selectedID = resolveModelDefaultProfile(schema, values["model"])
    }
    for i, pr := range profiles {
        if pr.ID == selectedID {
            p.mergeProfileParams(values, profiles[i].Params)
            break
        }
    }
}
return values, nil
```

```go
// mergeProfileParams writes non-zero profile params into the values map as
// flat top-level keys. Key names match values.yaml exactly so templates can
// reference them as .Values.numCards, .Values.maxModelLen, .Values.memory, etc.
func (p *CatalogProvider) mergeProfileParams(values map[string]any, params types.ProfileParam) {
    if params.NumCards > 0            { values["numCards"]            = params.NumCards }
    if params.MaxModelLen > 0         { values["maxModelLen"]         = params.MaxModelLen }
    if params.MaxBatchSize > 0        { values["maxBatchSize"]        = params.MaxBatchSize }
    if params.MaxNumBatchedTokens > 0 { values["maxNumBatchedTokens"] = params.MaxNumBatchedTokens }
    if params.Memory != ""            { values["memory"]              = params.Memory }
    if params.ShmSize != ""           { values["shmSize"]             = params.ShmSize }
}
```

### 5.5 Full request-to-template flow

```
POST /api/v1/applications
  { "services": [{ "components": [{ "component_type": "llm",
                                    "provider_id": "vllm-spyre",
                                    "params": { "model": "ibm-granite/granite-4.1-8b-fp8",
                                                "profile": "compact-4k" } }] }] }
  │
  ▼
application_handler.go  CreateApplication()
  │
  ▼
ParamBuilder.BuildServiceParams()
  └─► ParamBuilder.buildComponentParams(comp)
        └─► CatalogProvider.LoadComponentValues("llm", "vllm-spyre",
                                                 {"model": "...", "profile": "compact-4k"})
              │
              │  1. Read values.yaml
              │       → {image: "...", model: "", apiKey: "", profile: "standard"}
              │
              │  2. Apply argParams via SetNestedValue
              │       → profile overwritten: "compact-4k"
              │          model overwritten:  "ibm-granite/granite-4.1-8b-fp8"
              │
              │  3. loadProfiles("llm", "vllm-spyre")
              │       → reads llm/vllm-spyre/profiles.yaml (provider root)
              │       → finds entry id="compact-4k"
              │
              │  4. mergeProfileParams()
              │       → numCards=1, maxModelLen=4096, maxBatchSize=32,
              │          memory="100Gi", shmSize="64Mi"
              │
              └─► returns values map:
                    { profile: "compact-4k", model: "ibm-granite/...", apiKey: "",
                      numCards: 1, maxModelLen: 4096, maxBatchSize: 32,
                      memory: "100Gi", shmSize: "64Mi" }

DeploymentPlanner.calculateAndAllocateSpyreCards()
  └─► CollectSpyreCardsFromTemplates(templates, comp.Values)
        pre-renders vllm-server.yaml.tmpl → reads annotation
          ai-services.io/llm--spyre-cards: "{{ .Values.numCards }}" → "1"
        checks host has ≥ 1 free card → HTTP 422 if not

PodmanDeployer renders vllm-server.yaml.tmpl with the values map above.
  annotation  ai-services.io/llm--spyre-cards: "1"
  env         AIU_WORLD_SIZE  = "1"
  env         MAX_MODEL_LEN   = "4096"
  env         MAX_BATCH_SIZE  = "32"
  resource    podman.io/device=/dev/vfio: 1
  memory      "100Gi"  (from .Values.memory)
  shm         "64Mi"   (from .Values.shmSize)

No if/else branches in the template. Profile resolution is done entirely
inside LoadComponentValues() before the template is ever touched.
```

`additionalProperties: false` in `values.schema.json` ensures `numCards`,
`maxModelLen` etc. are never accepted from a client — they are not declared
in the schema, so the validator rejects them before `LoadComponentValues()` is
even called. Only `model`, `apiKey`, and `profile` can come in from the
request.

---

## 6. Validation & enforcement

| # | Layer | File | What it enforces |
|---|---|---|---|
| 1 | Schema validation | `values.schema.json` (`profile` field + `x-profiles` per model `oneOf` entry) | `profile` must be a non-empty string; the backend cross-checks it against the merged `profiles.yaml` list for that model → HTTP 400 if not found |
| 2 | Param merge | `LoadComponentValues()` | Profile params merged into values map; user cannot override individual hardware fields |
| 3 | Spyre pre-check | `planner.go` `calculateAndAllocateSpyreCards()` | Template pre-rendered to read `ai-services.io/llm--spyre-cards` annotation; deploy rejected if host lacks free cards → HTTP 422 |
| 4 | Pod annotation | `vllm-server.yaml.tmpl` | `ai-services.io/llm--spyre-cards: "{{ .Values.numCards }}"` — authoritative signal for card allocator |
| 5 | Container env | `vllm-server.yaml.tmpl` | `AIU_WORLD_SIZE`, `MAX_MODEL_LEN`, `MAX_BATCH_SIZE` resolved from profile |
| 6 | Resource request | `vllm-server.yaml.tmpl` | `podman.io/device=/dev/vfio: {{ .Values.numCards }}` — Podman device injection |
| 7 | UI resource tile | `StepTwo.tsx` + `metadata.yaml profiles` | Displays card/memory for the selected profile |

---

## 7. File change summary

| File | Change |
|---|---|
| `components/llm/vllm-spyre/profiles.yaml` | **New** — profile definitions for Spyre LLM (shared across Podman & OpenShift) |
| `components/llm/vllm-cpu/profiles.yaml` | **New** — `standard` and `compact-3k` for CPU LLM (shared across runtimes) |
| `components/reranker/vllm-spyre/profiles.yaml` | **New** — profile definitions for Spyre Reranker |
| `components/reranker/vllm-cpu/profiles.yaml` | **New** — profile definitions for CPU Reranker |
| `components/embedding/vllm-cpu/profiles.yaml` | **New** — profile definitions for CPU Embedding |
| All LLM provider `values.schema.json` | Remove `profiles` array from `properties.profile`; keep `x-profiles` on each model `oneOf` entry |
| `components/llm/vllm-spyre/podman/values.yaml` | Remove `maxModelLen`/`maxBatchSize` |
| `components/llm/vllm-spyre/openshift/values.yaml` | Remove resource params |
| `components/llm/vllm-cpu/podman/values.yaml` | Remove `maxNumBatchedTokens`/`maxModelLen`/`maxBatchSize` |
| `components/llm/vllm-cpu/openshift/values.yaml` | Same |
| `components/llm/vllm-spyre/podman/templates/vllm-server.yaml.tmpl` | Replace 8 hardcoded occurrences with `.Values.*` keys |
| `components/llm/vllm-spyre/openshift/templates/instruct-inferenceservice.yaml` | Replace args + resource values |
| `components/llm/vllm-cpu/podman/templates/vllm-server.yaml.tmpl` | Replace 3 CLI flag values |
| `components/llm/vllm-cpu/openshift/templates/instruct-inferenceservice.yaml` | Replace args values |
| `components/llm/vllm-spyre/podman/metadata.yaml` | Add `profiles` resource map |
| `components/llm/vllm-spyre/openshift/metadata.yaml` | Add `profiles` resource map |
| Catalog backend — asset loader | Add `loadProfilesFromFile()`, profile param merge; add `GetComponentProviderProfiles` handler + route |
| `router.go` | Register `GET /components/:component_type/providers/:provider_id/profiles` |


## 8. Why a standalone `profiles.yaml` rather than embedded in `values.schema.json`

The previous design embedded profile definitions as a `profiles` array inside `properties.profile` in `values.schema.json`. A standalone YAML improves on that:

- **Readable** — profile hardware numbers are easier to review in plain YAML than inside a nested JSON Schema structure.
- **Decoupled** — adding or changing a profile touches only `profiles.yaml`; `values.schema.json` (field rules, UI hints, `x-profiles`) is unchanged.
- **Explicit API** — `GET /api/v1/components/:component_type/providers/:provider_id/profiles` gives the UI a direct, typed endpoint to fetch profiles for a model; no schema parsing required.
- **`category: model` scope** — profiles apply to any provider with `category: model` in its `metadata.yaml`. Keeping definitions out of `values.schema.json` makes this constraint explicit in the file layout rather than implicit in code.


## 9. What is out of scope

- Application-level templates (`applications/rag`, `rag-dev`) embed their own
  fixed configurations and are not touched.
- The question of which models each service supports, and which profile applies
  per service, is addressed in
  [service-model-contracts.md](./service-model-contracts.md).

---

## 10. Impact on [`model-management-LLD.md`](./model-management-LLD.md)

The following changes are required in the LLD once profiling is introduced. Each bullet is a single targeted delta.

### §4.1 Models — concept update
- A **Model** now has a `profile` alongside a `model_name` — the profile determines the hardware configuration used for its deployment.

### §4.3 Supported Provider Params — `vllm-cpu` / `vllm-spyre`
- The `params` table for `vllm-cpu` and `vllm-spyre` gains a `profile` field — required for Spyre, optional (defaults to `standard`) for CPU.
- `model` (or `model_name`) remains the only other required field; `maxModelLen` / `maxBatchSize` are removed from params entirely since they are now derived from the selected profile.

### §5.2 `components.metadata` JSONB — store resolved profile
- The `metadata` JSONB for local model rows must include `"profile"` alongside `"model"` so that `GET /api/v1/models/:id` can surface the profile the model was deployed with.
- Example after change:
  ```json
  { "model": "ibm-granite/granite-4.1-8b-fp8", "profile": "compact-4k" }
  ```

### §6.1 Model Endpoints — `POST /api/v1/models`
- `profile` is added as an accepted field in the request `params` object for local providers (`vllm-cpu`, `vllm-spyre`).
- Valid profile values are model-dependent — the UI fetches them from `GET /api/v1/components/:component_type/providers/:provider_id/profiles?model=<model>` and the backend validates the submitted value against the merged profile list → HTTP 400 if not found.
- The endpoint validates the submitted `profile` value against the provider's merged `profiles.yaml` (built-in + any active bundle). The `values.schema.json` retains the `profile` field for structural validation only.

### §7.1 Deploy a Local Model — request body example
- The example request body gains `"profile": "compact-4k"` under `params`:
  ```json
  {
    "type": "llm",
    "name": "granite-extract-llm",
    "provider_id": "vllm-spyre",
    "params": {
      "model": "ibm-granite/granite-4.1-8b-fp8",
      "profile": "compact-4k"
    }
  }
  ```
- The `params` required-fields table gains a `profile` row (optional, defaults to `standard`).
- Profile values follow the naming system documented in `model-profiles.md §2`: `standard`, `balanced`, `compact-4k`, `compact-8k`, `compact-16k`, `minimal`.

### §7.1 Deploy a Local Model — polymorphic `params` table
- `profile` is added as an optional field for `vllm-cpu` and `vllm-spyre` with a note that it defaults to `standard` when omitted and that valid values are model-dependent.

### §7.2 List Local Models — response shape
- `metadata` in the list item response includes `"profile"` so operators can see at a glance which configuration a deployed model is running.

### §7.3 Get Model Details — response shape
- `metadata` block in the `GET /api/v1/models/:id` response includes `"profile"`:
  ```json
  "metadata": { "model": "ibm-granite/granite-4.1-8b-fp8", "profile": "compact-4k" }
  ```

### §8. Pre-flight Resource Check
- The pre-flight check for Spyre card count now reads `numCards` from the resolved profile — e.g. `compact-4k` requests 1 card, `balanced` requests 2, `standard` requests 4.
- Pre-flight output should include the resolved `profile` ID and its `numCards` / `memoryGB` in the constraint list so the operator sees the concrete numbers before confirming deployment.

### §8. LiteLLM Route ID (§10 Key Design Decision #8)
- The LiteLLM route ID `{model_name}--{provider}` should become `{model_name}--{provider}--{profile}` (e.g. `granite-4.1-8b-fp8--vllm-spyre--compact-4k`) so that the same model deployed at different profiles gets distinct routes and keys, allowing both to coexist simultaneously (e.g. one `compact-4k` deployment for Extract and one `standard` deployment for Chat).

### §10. Key Design Decision #8 — Route ID uniqueness
- Update the decision note: the profile suffix is added to the double-dash route ID to guarantee uniqueness when the same model is deployed under different profiles. Profile names use only alphanumeric characters and hyphens — no ambiguity with the double-dash separator.

### §13. Future Considerations
- Add: **Profile-aware pre-flight** — surface the selected profile's resource requirement (`numCards`, `memoryGB`) in the pre-flight UI tile so the operator sees the concrete numbers before confirming deployment.
- Add: **Profile upgrade/downgrade** — `PUT /api/v1/models/:id/profile` to atomically swap the profile of a running model (stop pod at old profile, redeploy at new profile, re-register LiteLLM route), gated behind a `409` if the model is in use by active applications.
- Add: **`minRhaiisVersion` enforcement** — gate profile availability at deploy time against the detected RHAIIS image version on the target worker; warn or block if the selected profile requires a newer RHAIIS than what is running.

### §14.1 CLI — `model deploy` command
- `--profile` flag is added to `ai-services model deploy`:
  ```
  ai-services model deploy granite-extract-llm \
    --type llm \
    --provider vllm-spyre \
    --params model=ibm-granite/granite-4.1-8b-fp8 \
    --profile compact-4k \
    --runtime podman
  ```
- `--profile` is optional; omitting it uses the provider's default profile (`standard`).
- Valid profile values for the chosen model are shown in `--help` output, sourced from the model's `x-profiles` list in `values.schema.json`.
- The `model deploy` help text prints the full profile derivation table (from `model-profiles.md §2.2`) so operators know what each profile name means without needing to read the docs.

---

## 11. GET `/api/v1/components/:component_type/providers/:provider_id/profiles`

Returns all hardware profiles for a model-category provider, optionally filtered to one model.

**Scope:** providers with `category: model` in `metadata.yaml` (`llm`, `reranker`, `embedding`). Calls for providers without `category: model` return `400`.

**Query parameter:** `?model=<full_model_name>` (optional). When supplied, only profiles listed in that model's `x-profiles` are returned. When omitted, all profiles in the provider's `profiles.yaml` are returned.

**Examples:**

```
GET /api/v1/components/llm/providers/vllm-spyre/profiles
    ?model=ibm-granite/granite-4.1-8b-fp8

GET /api/v1/components/reranker/providers/vllm-spyre/profiles
    ?model=BAAI/bge-reranker-v2-m3

GET /api/v1/components/reranker/providers/vllm-cpu/profiles
    ?model=BAAI/bge-reranker-v2-m3

GET /api/v1/components/embedding/providers/vllm-cpu/profiles
    ?model=ibm-granite/granite-embedding-278m-multilingual
```

**Response `200 OK`:**

```json
[
  {
    "id": "standard",
    "title": "Standard (4 cards)",
    "description": "Full-context RAG inferencing — 4 Spyre cards, 32 768 context.",
    "default": false,
    "params": { "numCards": 4, "maxModelLen": 32768, "maxBatchSize": 32, "memory": "200Gi", "shmSize": "2Gi" }
  },
  {
    "id": "compact-4k",
    "title": "Compact 4 k (1 card)",
    "description": "Short-context entity extraction — 1 Spyre card, 4 096 context.",
    "default": true,
    "params": { "numCards": 1, "maxModelLen": 4096, "maxBatchSize": 32, "memory": "100Gi", "shmSize": "2Gi" }
  }
]
```

The entry with `"default": true` is pre-selected by the UI. An empty array means no profiles exist for this provider (non-LLM types, or a provider without a `profiles.yaml`).

**Errors:** `404` for unknown `component_type` or `provider_id`. `400` if the `model` value is not recognised in the provider's schema.

**Backend:** `GetComponentProviderProfiles()` reads `profiles.yaml` via `loadProfilesFromFile()`, then intersects with the model's `x-profiles` list (from `values.schema.json`). If no `profiles.yaml` exists, it returns `[]`.


---

## 12. Future Scope: User-defined Profiles via Database Seeding and CRUD API

> **Note — Scope Boundary:** In the initial implementation (Phase 1), hardware profiles are **file-driven** (`profiles.yaml`) and read directly from provider asset directories as described in §3–§11. User-defined (custom) profiles and dynamic CRUD operations are reserved for **Future Scope (Phase 2)**.

When dynamic, user-defined profiles are enabled in Phase 2, **Data seeding into the database** is the technique used to bootstrap the system. The built-in profiles from `profiles.yaml` are seeded into a PostgreSQL `model_profiles` table on catalog startup/migration with `created_by = 'system'`. Custom profiles created by users are stored alongside them in the same table, managed through a dedicated CRUD API.

Built-in profiles remain read-only (`403` on `PUT`/`DELETE`), while custom profiles can be updated or deleted. The `GET /profiles` endpoint queries this table to return the merged set of built-in and custom profiles.

### 12.1 `model_profiles` table

To differentiate **built-in** profiles (seeded from `profiles.yaml`) from **user-defined** profiles, the table includes an explicit `is_builtin` boolean along with `created_by`:

```sql
CREATE TABLE model_profiles (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id   VARCHAR(50)  NOT NULL,           -- e.g. 'vllm-spyre', 'vllm-cpu'
    profile_id    VARCHAR(100) NOT NULL,            -- stable name e.g. 'my-compact-6k'
    title         VARCHAR(255) NOT NULL,
    description   TEXT,
    is_default    BOOLEAN      NOT NULL DEFAULT false,
    is_builtin    BOOLEAN      NOT NULL DEFAULT false, -- true for built-in seed rows, false for custom
    params        JSONB        NOT NULL,            -- numCards, maxModelLen, maxBatchSize, memory, shmSize
    created_by    VARCHAR(100),                     -- 'system' for built-ins, user ID/email for custom
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (provider_id, profile_id)
);
CREATE INDEX ON model_profiles(provider_id);
```

`params` follows the same keys as `profiles.yaml` (`numCards`, `maxModelLen`, `maxBatchSize`, `memory`, `shmSize`). The `(provider_id, profile_id)` unique constraint prevents duplicate IDs per provider.

### 12.2 Data Seeding Technique and Migration

**Data seeding into the database** is executed during catalog startup and migration:
1. The migration script / startup seeder reads `profiles.yaml` for each provider.
2. It inserts each built-in profile into `model_profiles` with `created_by = 'system'`.
3. Subsequent startups are idempotent — seed rows use `ON CONFLICT (provider_id, profile_id) DO NOTHING`.
4. If a platform update introduces new built-in profiles or alters seed defaults, the seeder synchronizes those rows without overwriting user-created custom entries.

Migration script:
```
20260901000001_create_model_profiles_table.sql
```

### 12.3 API endpoints

| Method | Path | Description | Response |
|---|---|---|---|
| `POST` | `/api/v1/components/:component_type/providers/:provider_id/profiles` | Create a custom profile | `201 Created` |
| `PUT` | `/api/v1/components/:component_type/providers/:provider_id/profiles/:profile_id` | Update a **custom** profile. Built-in profiles (`created_by = 'system'`) cannot be updated — `403` | `200 OK` |
| `DELETE` | `/api/v1/components/:component_type/providers/:provider_id/profiles/:profile_id` | Delete a **custom** profile. Built-in profiles cannot be deleted — `403` | `204 No Content` |

`GET /api/v1/components/:component_type/providers/:provider_id/profiles` (§11) is unchanged — in Phase 2 it reads from the DB instead of directly from file.

**Example — create:**

```bash
curl -X POST https://<catalog>/api/v1/components/llm/providers/vllm-spyre/profiles \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "profile_id": "my-compact-6k",
    "title": "Custom compact 6 k (1 card)",
    "description": "Team-specific 1-card / 6 144 context configuration.",
    "params": { "numCards": 1, "maxModelLen": 6144, "maxBatchSize": 32,
                "memory": "100Gi", "shmSize": "2Gi" }
  }'
```

Response `201 Created`:

```json
{
  "id": "e4f5a6b7-...",
  "provider_id": "vllm-spyre",
  "profile_id": "my-compact-6k",
  "title": "Custom compact 6 k (1 card)",
  "is_default": false,
  "params": { "numCards": 1, "maxModelLen": 6144, "maxBatchSize": 32, "memory": "100Gi", "shmSize": "2Gi" },
  "created_by": "user@example.com"
}
```

### 12.4 Backend changes

| Component | Change |
|---|---|
| `loadProfilesFromFile()` | Called once at startup / migration to seed the DB; not used for request-time resolution in Phase 2 |
| `GetComponentProviderProfiles()` | Reads from `model_profiles` table; filters by `provider_id` and model `x-profiles` list |
| `ProfileRepository` | New repo — `Create`, `Update`, `Delete`, `ListByProvider` |
| Migration | `20260901000001_create_model_profiles_table.sql` — creates table and executes data seeding for built-ins |

### 12.5 Immutability & Constraints

- **Strict Immutability for Built-ins:** Users and administrators are **not allowed to modify or delete built-in profiles** (`is_builtin = true` / `created_by = 'system'`). Any `PUT` or `DELETE` request targeting a built-in profile returns `403 Forbidden` with error code `BUILTIN_PROFILE_READONLY`. To use a different configuration, users must create a new custom profile with a distinct `profile_id`.
- **Scope Restriction:** Profiles apply only to providers with **`category: model`** in `metadata.yaml` (`llm`, `reranker`, `embedding`). Requests for providers without `category: model` return `400 Bad Request`.
- **No live migration:** Updated custom profile parameters apply to new deployments only; running pods continue with their provisioned parameters.
