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
> **`compact-{context}`** = 1 card at that context window.
> **`minimal`** = 1 card · smallest context (legacy RHAIIS ≤ 3.5 configs).

Context window suffixes on `compact` are always in lowercase K units:
`compact-3k`, `compact-4k`, `compact-8k`, `compact-16k`.

`standard` and `balanced` never need a context suffix because IBM has only one
validated config at 4 cards and one at 2 cards respectively.

### 2.2 Profile derivation table

This is the authoritative mapping from IBM Spyre RHAIIS validated use-case
configurations (source: IBM docs, last updated 2026-09-22) to profile IDs.

| Profile ID | Cards | Max context | Batch | Memory | shm | IBM use case / notes |
|---|---|---|---|---|---|---|
| `standard` | 4 | 32,768 | 32 | 200 GB | 2 GB | RAG inferencing — all supported LLMs |
| `balanced` | 2 | 8,192 | 32 | 150 GB | 2 GB | Entity extraction — Mistral-Small-24B (RHAIIS 3.5+); Ministral-14b-BF16 2-card option (RHAIIS 3.6) |
| `compact-4k` | 1 | 4,096 | 32 | 100 GB | default | Entity extraction — Granite-3.3, Granite-4.1-fp8, Llama-3.1-8B (RHAIIS 3.6); Ministral-14b-BF16 1-card option |
| `compact-4k-150` | 1 | 4,096 | 32 | 150 GB | 2 GB | Entity extraction — Ministral-14b-BF16 1-card option (higher memory variant) |
| `compact-8k` | 1 | 8,192 | 16 | 100 GB | 2 GB | Entity extraction — Granite-vision-3.3-2b 1-card option (RHAIIS 3.6) |
| `compact-16k` | 1 | 16,384 | 16 | 100 GB | 2 GB | Entity extraction — Granite-vision-3.3-2b 2nd option (RHAIIS 3.6), treated as 1-card wide-context |
| `minimal` | 1 | 3,072 | 16 | 100 GB | default | Entity extraction — Granite-3.3, Granite-4.1-fp8, Llama-3.1-8B (RHAIIS 3.3–3.5 only) |

> **`balanced-16k`** would be the profile for the Granite-vision 2-card / 16K
> config if it were split out; for now it is folded into `compact-16k` since the
> IBM table treats both 1-card and 2-card Granite-vision options under the same
> entity-extraction row. If a distinct 2-card / 16K use case is validated for a
> non-vision model in future, `balanced-16k` is the natural name.

**Reranker and embedding profiles** — these component types each have a single
validated configuration so no naming distinction is needed:

| Component | Profile ID | Cards | Max context | Batch | Memory | shm |
|---|---|---|---|---|---|---|
| `reranker/vllm-spyre` | `standard` | 1 | 8,192 | 4 | 50 GB | default |
| `embedding/vllm-cpu` | `standard` | — | 512 | 256 | 50 GB | default |

**CPU LLM profiles** — no card dimension; names follow context window only:

| Profile ID | Max context | Batch | Notes |
|---|---|---|---|
| `standard` | 8,096 | 32 | Default CPU config |
| `compact-3k` | 3,072 | 16 | Short-context CPU workloads |

---

## 3. Solution: Named profiles embedded in `values.schema.json`, models reference by ID

### 3.1 Design

A **profile** is a named, globally-unique preset defined inside `properties.profile`
in `values.schema.json` as a `profiles` array. Each model entry's `oneOf` references
only the profile IDs it supports via an `x-profiles` list. No separate `profiles.yaml`
file is needed.

This means:

- Hardware params are defined **once** inside `properties.profile.profiles` — no
  duplication when multiple models share the same configuration (e.g. `standard`
  is used by Granite-3.3, Granite-4.1, Llama, and Mistral for RAG inferencing).
- Everything lives in one file — the profile definitions sit inside the `profile`
  property that they configure, making `values.schema.json` fully self-contained.
- Service contracts (§ in `service-model-contracts.md`) reference profile IDs
  directly by their stable global name — `profile: compact-4k` is unambiguous
  regardless of which model is selected.
- Adding a new IBM-validated configuration is a new entry in
  `properties.profile.profiles` — no separate file to create.

### 3.2 `llm/vllm-spyre/values.schema.json` — both `podman/` and `openshift/`

Profile definitions are stored inside `properties.profile` as a `profiles` array,
co-located with the field they configure. `params` keys match `values.yaml` exactly
— flat camelCase. `numCards`, `memory`, and `shmSize` are new keys added to
`values.yaml` as part of this proposal; their value format matches the template
usage (`"200Gi"` string for memory, integer for card count).

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
          "x-profiles": ["standard", "compact-4k", "minimal"]
        },
        {
          "const": "ibm-granite/granite-4.1-8b-fp8",
          "title": "granite-4.1-8b-fp8",
          "description": "...",
          "x-profiles": ["standard", "compact-4k", "minimal"]
        },
        {
          "const": "meta-llama/Llama-3.1-8B-Instruct",
          "title": "Llama-3.1-8B-Instruct",
          "description": "...",
          "x-profiles": ["standard", "compact-4k", "minimal"]
        },
        {
          "const": "mistralai/Mistral-Small-3.2-24B-Instruct",
          "title": "Mistral-Small-3.2-24B-Instruct",
          "description": "...",
          "x-profiles": ["standard", "balanced"]
        },
        {
          "const": "mistralai/Ministral-3-14B-Instruct-2512-BF16",
          "title": "Ministral-3-14B-Instruct-2512-BF16",
          "description": "...",
          "x-profiles": ["standard", "compact-4k", "balanced"]
        },
        {
          "const": "ibm-granite/granite-vision-3.3-2b",
          "title": "granite-vision-3.3-2b",
          "description": "...",
          "x-profiles": ["compact-8k", "compact-16k"]
        }
      ],
      "default": "ibm-granite/granite-4.1-8b-fp8"
    },
    "profile": {
      "type": "string",
      "title": "Deployment profile",
      "description": "Hardware sizing preset — options depend on the selected model.",
      "x-profile-filter": "model",
      "profiles": [
        {
          "id": "standard",
          "default": true,
          "title": "Standard  (4 cards · 32K context · batch 32)",
          "description": "Full 4-card RAG inferencing configuration. Validated for all supported LLMs (Granite-3.3, Granite-4.1-fp8, Llama-3.1-8B, Mistral-Small-24B, Ministral-14b-BF16).",
          "params": { "numCards": 4, "maxModelLen": 32768, "maxBatchSize": 32, "memory": "200Gi", "shmSize": "2Gi" }
        },
        {
          "id": "balanced",
          "title": "Balanced  (2 cards · 8K context · batch 32)",
          "description": "2-card mid-context configuration. Validated for Mistral-Small-24B entity extraction and Ministral-14b-BF16 entity extraction 2-card option.",
          "params": { "numCards": 2, "maxModelLen": 8192, "maxBatchSize": 32, "memory": "150Gi", "shmSize": "2Gi" }
        },
        {
          "id": "compact-4k",
          "title": "Compact  (1 card · 4K context · batch 32)",
          "description": "Single-card short-context configuration. Validated for Granite-3.3, Granite-4.1-fp8, and Llama-3.1-8B entity extraction (RHAIIS 3.6); Ministral-14b-BF16 entity extraction 1-card option.",
          "params": { "numCards": 1, "maxModelLen": 4096, "maxBatchSize": 32, "memory": "100Gi", "shmSize": "64Mi" }
        },
        {
          "id": "compact-8k",
          "title": "Compact  (1 card · 8K context · batch 16)",
          "description": "Single-card 8K context configuration. Validated for Granite-vision-3.3-2b entity extraction 1-card option.",
          "params": { "numCards": 1, "maxModelLen": 8192, "maxBatchSize": 16, "memory": "100Gi", "shmSize": "2Gi" }
        },
        {
          "id": "compact-16k",
          "title": "Compact  (1 card · 16K context · batch 16)",
          "description": "Single-card 16K context configuration. Validated for Granite-vision-3.3-2b entity extraction 2nd hardware option.",
          "params": { "numCards": 1, "maxModelLen": 16384, "maxBatchSize": 16, "memory": "100Gi", "shmSize": "2Gi" }
        },
        {
          "id": "minimal",
          "title": "Minimal  (1 card · 3K context · batch 16)",
          "description": "Smallest single-card configuration. Validated for Granite-3.3, Granite-4.1-fp8, and Llama-3.1-8B entity extraction on RHAIIS 3.3–3.5. Superseded by compact-4k on RHAIIS 3.6.",
          "params": { "numCards": 1, "maxModelLen": 3072, "maxBatchSize": 16, "memory": "100Gi", "shmSize": "64Mi" }
        }
      ]
    },
    "apiKey": {
      "type": "string",
      "title": "API key (optional)",
      "format": "password"
    }
  }
}
```

> `x-profile-filter: "model"` tells the backend to intersect the `profiles`
> array (inside `properties.profile`) with the selected model's `x-profiles`
> array before returning the profile `oneOf` options to the UI. The user first
> picks a model; the profile dropdown then shows only the profiles valid for
> that model.

### 3.3 `llm/vllm-cpu/values.schema.json` — both `podman/` and `openshift/`

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
      "x-profile-filter": "model",
      "profiles": [
        {
          "id": "standard",
          "default": true,
          "title": "Standard  (8K context · batch 32)",
          "description": "Default CPU token-budget configuration for most workloads.",
          "params": { "maxNumBatchedTokens": 8096, "maxModelLen": 8096, "maxBatchSize": 32 }
        },
        {
          "id": "compact-3k",
          "title": "Compact  (3K context · batch 16)",
          "description": "Reduced token-budget for short-context CPU workloads.",
          "params": { "maxNumBatchedTokens": 3072, "maxModelLen": 3072, "maxBatchSize": 16 }
        }
      ]
    },
    "apiKey": { "...": "..." }
  }
}
```

### 3.4 `reranker/vllm-spyre/values.schema.json`

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
          "x-profiles": ["standard"]
        }
      ],
      "default": "BAAI/bge-reranker-v2-m3"
    },
    "profile": {
      "type": "string",
      "title": "Deployment profile",
      "x-profile-filter": "model",
      "profiles": [
        {
          "id": "standard",
          "default": true,
          "title": "Standard  (1 card · 8K context · batch 4)",
          "description": "Single validated configuration for bge-reranker-v2-m3.",
          "params": { "numCards": 1, "maxModelLen": 8192, "maxBatchSize": 4, "memory": "50Gi", "shmSize": "64Mi" }
        }
      ]
    }
  }
}
```

### 3.5 `embedding/vllm-cpu/values.schema.json`

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
          "x-profiles": ["standard"]
        }
      ],
      "default": "ibm-granite/granite-embedding-278m-multilingual"
    },
    "profile": {
      "type": "string",
      "title": "Deployment profile",
      "x-profile-filter": "model",
      "profiles": [
        {
          "id": "standard",
          "default": true,
          "title": "Standard  (512 context · batch 256)",
          "description": "Single validated configuration for Granite Embedding and Multilingual-E5 models.",
          "params": { "maxModelLen": 512, "maxBatchSize": 256, "memory": "50Gi" }
        }
      ]
    }
  }
}
```

---

## 4. Changes to existing files per provider

### 4.1 `values.yaml` — remove derived fields, no `profile` default

The individual parameter fields (`maxModelLen`, `maxBatchSize`, etc.) are
removed from `values.yaml` because they are now authoritative in
`properties.profile.profiles` inside `values.schema.json`.
No `profile` default is written here — the hardware params are already encoded
inside each model's `x-profiles` list in `values.schema.json`, so a static
`"standard"` default in `values.yaml` would be incorrect for models that don't
support `standard` (e.g. `granite-vision-3.3-2b` only supports `compact-8k` /
`compact-16k`). The backend resolves the default at deploy time by selecting the
first profile in the chosen model's `x-profiles` list that has `"default": true`
in `properties.profile.profiles` (see §5.4).

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

### 4.2 `values.schema.json` — profile definitions + model `x-profiles`

The `profile` field and its embedded `profiles` array are shown in §3.2.
The checked-in file is self-contained — `properties.profile.profiles` holds all
profile data and `x-profile-filter` signals the backend to build the `oneOf`
dynamically. Each model's `oneOf` entry carries `x-profiles` listing only the
profile IDs valid for that model.

The backend uses these extension fields to:
1. Read the full list from `properties.profile.profiles` in the schema it already holds — no extra file read.
2. Intersect it with the selected model's `x-profiles` array (`x-profile-filter: "model"`).
3. Return only the intersected set as the `oneOf` for the `profile` field.

For providers with a single profile (reranker, embedding), the backend omits
the `profile` field from the returned schema entirely — no dropdown is shown.

### 4.3 Templates — read from resolved profile values

The template no longer contains if/else branches for profiles. The backend
resolves the selected profile from `properties.profile.profiles` and merges its `params` into
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

### 5.2 `loadProfilesFromSchema()` — new private helper

A new private helper extracts the `profiles` array from inside `properties.profile`
in the already-parsed `values.schema.json` map. No extra file read is needed — the
schema is already in memory when this is called.

```go
func loadProfilesFromSchema(schema map[string]any) ([]types.Profile, error) {
    props, _ := schema["properties"].(map[string]any)
    profileField, _ := props["profile"].(map[string]any)
    raw, ok := profileField["profiles"]
    if !ok {
        return nil, nil // no profiles defined for this provider — not an error
    }
    entries, ok := raw.([]any)
    if !ok {
        return nil, fmt.Errorf("properties.profile.profiles must be an array")
    }
    profiles := make([]types.Profile, 0, len(entries))
    for _, e := range entries {
        m, ok := e.(map[string]any)
        if !ok {
            continue
        }
        var pr types.Profile
        // marshal back to JSON and unmarshal into the typed struct
        b, _ := json.Marshal(m)
        if err := json.Unmarshal(b, &pr); err != nil {
            return nil, fmt.Errorf("failed to parse profile entry: %w", err)
        }
        profiles = append(profiles, pr)
    }
    return profiles, nil
}
```

### 5.3 `GetComponentProviderParams()` — schema injection

[`GetComponentProviderParams()`](../../ai-services/internal/pkg/catalog/deploy_options.go)
already reads and returns `values.schema.json`. After decoding the schema, it
is extended to inject the filtered profile `oneOf` before returning.

The injection logic:
1. Calls `loadProfilesFromSchema(schema)` — reads `properties.profile.profiles` from the already-decoded schema map; no extra file read.
2. Reads the selected model's `x-profiles` list from the model's `oneOf` entry.
3. Intersects: keeps only profiles whose `id` appears in the model's `x-profiles`.
4. Injects the filtered list as the `oneOf` on the `profile` field.
5. For providers with a single profile, removes the `profile` field entirely.

```go
// After decoding values.schema.json into `schema`:
allProfiles, err := loadProfilesFromSchema(schema)
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
// If the user did not supply a profile, selectedID is "" and the branch
// pr.Default == true selects the first profile in profiles.yaml marked default.
// This is model-aware because GetComponentProviderParams already constrained
// which profiles are valid for the chosen model.
profiles, err := loadProfilesFromSchema(schema) // schema was decoded from values.schema.json earlier in this function
if err != nil {
    return nil, fmt.Errorf("failed to load profiles: %w", err)
}
if len(profiles) > 0 {
    selectedID, _ := values["profile"].(string)
    for i, pr := range profiles {
        if pr.ID == selectedID || (selectedID == "" && pr.Default) {
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
              │       → reads llm/vllm-spyre/<runtime>/profiles.yaml
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
| 1 | Schema validation | `values.schema.json` (`properties.profile.profiles` + `x-profiles` per model) | `profile` must be one of the declared profile IDs; unknown keys rejected → HTTP 400 |
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
| `components/llm/vllm-spyre/podman/values.schema.json` | Add `profiles` array inside `properties.profile` with `x-profile-filter`; add `x-profiles` to each model `oneOf` entry |
| `components/llm/vllm-spyre/openshift/values.schema.json` | Same |
| `components/llm/vllm-cpu/podman/values.schema.json` | Add `profiles` array inside `properties.profile`; add `x-profiles` to each model `oneOf` entry |
| `components/llm/vllm-cpu/openshift/values.schema.json` | Same |
| `components/reranker/vllm-spyre/podman/values.schema.json` | Add single-entry `profiles` array inside `properties.profile` |
| `components/reranker/vllm-spyre/openshift/values.schema.json` | Same |
| `components/embedding/vllm-cpu/podman/values.schema.json` | Add single-entry `profiles` array inside `properties.profile` |
| `components/embedding/vllm-cpu/openshift/values.schema.json` | Same |
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
| Catalog backend — asset loader | Add `loadProfilesFromSchema()`, profile schema injection, profile param merge |

---

## 8. Why profiles in `values.schema.json` rather than template branches or a separate file

The previous approach put profile resolution as if/else branches at the top of
each Go template. This has two problems:

1. **Adding a profile means editing a template** — not a data file. Templates
   are harder to review for correctness than YAML.
2. **Profile definitions are duplicated** across podman and openshift variants
   of the same component since both need the same if/else logic.

An intermediate approach used a separate `profiles.yaml` file alongside
`values.schema.json`. Embedding the definitions directly in `values.schema.json`
improves on that by:

- **One file per provider** — `values.schema.json` is already the authoritative
  source for field definitions, validation rules, and UI rendering hints.
  Profile data belongs there too; there is no reason to split it out.
- **No extra file read** — the backend already decodes `values.schema.json`;
  `loadProfilesFromSchema()` reads `properties.profile.profiles` from the parsed
  map with zero additional I/O.
- **Atomic changes** — adding a new profile and extending a model's `x-profiles`
  list is a single commit to one file, not two coordinated changes across
  `profiles.yaml` and `values.schema.json`.
- **Schema tooling compatible** — `x-` prefixed keys are ignored by JSON Schema
  validators, so the file remains valid and can be linted by existing tooling.
- The template body stays stable and does not grow with each new profile.

---

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
- Valid profile values are model-dependent — the schema returns only the profiles listed in the selected model's `x-profiles` array; the validator rejects any value not in that set → HTTP 400.
- The endpoint's schema validation delegates to the provider's `values.schema.json` (whose `profile` `oneOf` is injected from `properties.profile.profiles` at serve time) — no handler-level change needed.

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
