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

## 3. Solution: Standalone `models.yaml` and `profiles.yaml` Decoupled from Schema

### 3.1 Design

To provide clean separation of concerns, **`model` and `profile` are completely removed from `values.schema.json`**. Each provider manages its own supported models and hardware profiles directly inside its provider asset folder:

1. **`models.yaml` (per provider)**: Authoritative list of models supported by that specific provider (e.g. `assets/components/llm/vllm-spyre/models.yaml`), declaring model IDs, metadata, default profile, and supported profile IDs.
2. **`profiles.yaml` (per provider)**: Authoritative list of hardware profile definitions for that provider (e.g. `assets/components/llm/vllm-spyre/profiles.yaml`), defining resource footprints (`resources.memory`, `resources.accelerators`) and inference parameters (`maxModelLen`, `maxBatchSize`, `shmSize`).
3. **`values.schema.json` (per runtime)**: Authoritative only for runtime provider parameters (e.g. `apiKey`). It contains **neither `model` nor `profile`**.

Hardware profiles apply to any provider whose `metadata.yaml` carries `category: model` — covering `llm` (`vllm-spyre`, `vllm-cpu`), `reranker` (`vllm-spyre`, `vllm-cpu`), and `embedding` (`vllm-cpu`).

```
assets/components/
  llm/
    vllm-spyre/
      profiles.yaml            ← Provider-level hardware profile definitions
      models.yaml              ← Models supported by vllm-spyre & their profiles
      podman/
        values.schema.json     ← Provider params ONLY (e.g. apiKey) — NO model, NO profile
        values.yaml
      openshift/
        values.schema.json
        values.yaml
    vllm-cpu/
      profiles.yaml            ← Provider-level hardware profile definitions
      models.yaml              ← Models supported by vllm-cpu & their profiles
      podman/
        values.schema.json     ← Provider params ONLY (e.g. apiKey) — NO model, NO profile
        values.yaml
      openshift/
        values.schema.json
        values.yaml
  reranker/
    vllm-spyre/
      profiles.yaml            ← Provider-level hardware profile definitions
      models.yaml              ← Models supported by vllm-spyre reranker
      podman/
        values.schema.json     ← Provider params ONLY
        values.yaml
    vllm-cpu/
      profiles.yaml            ← Provider-level hardware profile definitions
      models.yaml              ← Models supported by vllm-cpu reranker
      podman/
        values.schema.json
        values.yaml
  embedding/
    vllm-cpu/
      profiles.yaml            ← Provider-level hardware profile definitions
      models.yaml              ← Models supported by vllm-cpu embedding
      podman/
        values.schema.json     ← Provider params ONLY
        values.yaml
```

### 3.2 `models.yaml` structure (Provider-level)

Since `models.yaml` exists directly inside each provider folder, it declares the models supported by that provider along with catalog metadata (`certified_by`, `license`) and an `about` section for display in the UI.

**Local providers** (`vllm-spyre`, `vllm-cpu`, `reranker/vllm-*`, `embedding/vllm-cpu`) also include `default_profile` and `profiles` — the list of hardware profiles the model has been validated against. **Remote connectors** (`hosted_vllm`, `watsonx`) omit `default_profile` and `profiles` since hardware profiles do not apply to remotely-hosted models.

**`assets/components/llm/vllm-spyre/models.yaml`** (local — includes `default_profile` and `profiles`)
```yaml
- id: ibm-granite/granite-3.3-8b-instruct
  name: Granite 3.3 8B Instruct
  description: "An 8b-parameter instruction-tuned LLM for reasoning, summarization, and tool-use."
  license: Apache-2.0
  certified_by: IBM
  default_profile: standard
  profiles:
    - standard
    - balanced
    - compact-4k
  about:
    - title: "Model strengths"
      sections:
        - title: "Strengths"
          values:
            - Reliable instruction-following
            - Natural conversation and dialogue
            - Strong reasoning and tool calling
            - Enterprise-grade accuracy
    - title: "Supported languages"
      sections:
        - title: "Languages"
          values:
            - English
            - Spanish
            - French
            - German
            - Portuguese
            - Italian
            - Japanese
            - Korean
            - Chinese
            - Arabic

- id: ibm-granite/granite-4.1-8b-fp8
  name: Granite 4.1 8B FP8
  description: "An 8b-parameter instruction-tuned LLM used for natural language understanding and generation tasks."
  license: Apache-2.0
  certified_by: IBM
  default_profile: standard
  profiles:
    - standard
    - balanced
    - compact-4k
  about:
    - title: "Model strengths"
      sections:
        - title: "Strengths"
          values:
            - Reliable instruction-following
            - Natural conversation and dialogue
            - Strong reasoning and tool calling
            - Enterprise-grade accuracy
    - title: "Supported languages"
      sections:
        - title: "Languages"
          values:
            - English
            - Spanish
            - French
            - German
            - Portuguese
            - Italian
            - Japanese
            - Korean
            - Chinese
            - Arabic

- id: meta-llama/Llama-3.1-8B-Instruct
  name: Llama 3.1 8B Instruct
  description: "Meta Llama 3.1 8B instruction-tuned model for general-purpose chat and reasoning."
  license: Llama-3.1
  certified_by: Meta
  default_profile: standard
  profiles:
    - standard
    - compact-4k
  about:
    - title: "Model strengths"
      sections:
        - title: "Strengths"
          values:
            - Strong general-purpose reasoning
            - Multilingual capability
            - Open-weight flexibility
    - title: "Supported languages"
      sections:
        - title: "Languages"
          values:
            - English
            - German
            - French
            - Italian
            - Portuguese
            - Spanish
            - Thai

- id: mistralai/Mistral-Small-3.2-24B-Instruct
  name: Mistral Small 3.2 24B Instruct
  description: "Mistral Small 24B instruct model balancing capability and efficiency."
  license: Apache-2.0
  certified_by: Mistral AI
  default_profile: balanced
  profiles:
    - standard
    - balanced
  about:
    - title: "Model strengths"
      sections:
        - title: "Strengths"
          values:
            - Strong reasoning at mid-size scale
            - Efficient for enterprise deployment
            - Function calling support
    - title: "Supported languages"
      sections:
        - title: "Languages"
          values:
            - English
            - French
            - German
            - Spanish
            - Italian
            - Portuguese
```

**`assets/components/llm/vllm-cpu/models.yaml`** (local — includes `default_profile` and `profiles`)
```yaml
- id: ibm-granite/granite-3.3-8b-instruct
  name: Granite 3.3 8B Instruct
  description: "An 8b-parameter instruction-tuned LLM optimized for CPU execution."
  license: Apache-2.0
  certified_by: IBM
  default_profile: standard
  profiles:
    - standard
  about:
    - title: "Model strengths"
      sections:
        - title: "Strengths"
          values:
            - Reliable instruction-following
            - Natural conversation and dialogue
            - Strong reasoning and tool calling
            - Enterprise-grade accuracy
    - title: "Supported languages"
      sections:
        - title: "Languages"
          values:
            - English
            - Spanish
            - French
            - German
            - Portuguese
            - Italian
            - Japanese
            - Korean
            - Chinese
            - Arabic

- id: meta-llama/Llama-3.1-8B-Instruct
  name: Llama 3.1 8B Instruct
  description: "Meta Llama 3.1 8B instruction-tuned model for CPU-based inferencing."
  license: Llama-3.1
  certified_by: Meta
  default_profile: standard
  profiles:
    - standard
  about:
    - title: "Model strengths"
      sections:
        - title: "Strengths"
          values:
            - Strong general-purpose reasoning
            - Multilingual capability
            - Open-weight flexibility
    - title: "Supported languages"
      sections:
        - title: "Languages"
          values:
            - English
            - German
            - French
            - Italian
            - Portuguese
            - Spanish
            - Thai
```

**`assets/connectors/models/hosted_vllm/models.yaml`** (remote — no `default_profile` or `profiles`)
```yaml
- id: ibm-granite/granite-4.1-8b-fp8
  name: Granite 4.1 8B FP8
  description: "An 8b-parameter instruction-tuned LLM used for natural language understanding and generation tasks."
  license: Apache-2.0
  certified_by: IBM
  component_type: llm
  about:
    - title: "Model strengths"
      sections:
        - title: "Strengths"
          values:
            - Reliable instruction-following
            - Natural conversation and dialogue
            - Strong reasoning and tool calling
            - Enterprise-grade accuracy
    - title: "Supported languages"
      sections:
        - title: "Languages"
          values:
            - English
            - Spanish
            - French
            - German
            - Portuguese
            - Italian
            - Japanese
            - Korean
            - Chinese
            - Arabic
```

### 3.3 `profiles.yaml` structure (example: `llm/vllm-spyre`)

```yaml
- id: standard
  default: false
  name: "Standard"
  description: "4 cards, 200 GiB RAM, 32k context"
  params:
    resources:
      memory: 214748364800   # 200Gi in bytes
      accelerators:
        ibm.com/spyre_pf: 4
    maxModelLen: 32768
    maxBatchSize: 32
    shmSize: "2Gi"

- id: compact-4k
  default: true
  name: "Compact 4k"
  description: "1 card, 100 GiB RAM, 4k context"
  params:
    resources:
      memory: 107374182400   # 100Gi in bytes
      accelerators:
        ibm.com/spyre_pf: 1
    maxModelLen: 4096
    maxBatchSize: 32
    shmSize: "2Gi"

- id: balanced
  default: false
  name: "Balanced"
  description: "2 cards, 150 GiB RAM, 8k context"
  params:
    resources:
      memory: 161061273600   # 150Gi in bytes
      accelerators:
        ibm.com/spyre_pf: 2
    maxModelLen: 8192
    maxBatchSize: 32
    shmSize: "2Gi"
```

`params` keys map directly to `.Values` in templates. `resources` injects as `.Values.resources` — a nested map with `memory` and `accelerators` — so templates reference `{{ .Values.resources.memory }}` and `{{ index .Values.resources.accelerators "ibm.com/spyre_pf" }}`. The profile flagged with `"default": true` in `profiles.yaml` acts as the fallback default when neither `models.yaml` nor the deploy request specifies a profile.

### 3.4 `llm/vllm-spyre/values.schema.json` — both `podman/` and `openshift/`

The schema contains **no `model` or `profile` property**. It declares only provider configuration fields:

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "apiKey": {
      "type": "string",
      "title": "API key (optional)",
      "format": "password"
    }
  }
}
```

### 3.5 `llm/vllm-cpu/values.schema.json` — both `podman/` and `openshift/`

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "apiKey": {
      "type": "string",
      "title": "API key (optional)",
      "format": "password"
    }
  }
}
```

### 3.6 `reranker/vllm-spyre` and `reranker/vllm-cpu` `values.schema.json`

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}
```

### 3.7 `embedding/vllm-cpu/values.schema.json`

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}
```

---

## 4. Changes to existing files per provider

### 4.1 `values.yaml` — remove hardware params, no `profile` default

The individual hardware parameter fields (`maxModelLen`, `maxBatchSize`, `maxNumBatchedTokens`, `resources`, etc.) are
removed from `values.yaml` because they are authoritative in `profiles.yaml`.
`model` and `profile` are resolved by the deployment engine from `models.yaml` and `profiles.yaml` at deploy time.

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
apiKey: ""
# model, profile, and hardware params are injected at deploy time from models.yaml / profiles.yaml
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
apiKey: ""
# model, profile, and hardware params are injected at deploy time from models.yaml / profiles.yaml
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
apiKey: ""
# model, profile, and hardware resources are injected at deploy time from models.yaml / profiles.yaml
```

### 4.2 `values.schema.json` — provider configuration only

`model` and `profile` are removed completely from `values.schema.json`. The schema only validates provider-specific properties (such as `apiKey` or connection credentials). Model selection and profile selection are validated against `models.yaml` and `profiles.yaml` at the API boundary.

### 4.3 Templates — read from resolved profile values

The template no longer contains if/else branches for profiles. The backend
resolves the selected profile from `profiles.yaml` and merges its `params` into
the values map before rendering. The template references these as ordinary
`.Values.*` keys.

**`llm/vllm-spyre/podman/templates/vllm-server.yaml.tmpl`** — replacements:

| Location | Before | After |
|---|---|---|
| Annotation (line 15) | `ai-services.io/llm--spyre-cards: "4"` | `ai-services.io/llm--spyre-cards: '{{ index .Values.resources.accelerators "ibm.com/spyre_pf" }}'` |
| `volumes[dshm].sizeLimit` (line 21) | `sizeLimit: 64Gi` | `sizeLimit: {{ .Values.shmSize }}` |
| Env `AIU_WORLD_SIZE` (line 60) | `value: "4"` | `value: '{{ index .Values.resources.accelerators "ibm.com/spyre_pf" }}'` |
| Env `MAX_MODEL_LEN` (line 64) | `value: "{{ .Values.maxModelLen }}"` | `value: "{{ .Values.maxModelLen }}"` (no change) |
| Env `MAX_BATCH_SIZE` (line 65) | `value: "{{ .Values.maxBatchSize }}"` | `value: "{{ .Values.maxBatchSize }}"` (no change) |
| `resources.requests.memory` (line 80) | `memory: "200Gi"` | `memory: "{{ .Values.resources.memory }}"` |
| `resources.limits.memory` (line 82) | `memory: "200Gi"` | `memory: "{{ .Values.resources.memory }}"` |
| `resources…/dev/vfio` (line 79) | `podman.io/device=/dev/vfio: 4` | `podman.io/device=/dev/vfio: '{{ index .Values.resources.accelerators "ibm.com/spyre_pf" }}'` |

**`llm/vllm-spyre/openshift/templates/instruct-inferenceservice.yaml`** — replacements:

```yaml
# args
- '--tensor-parallel-size={{ index .Values.resources.accelerators "ibm.com/spyre_pf" }} '
- '--max-model-len={{ .Values.maxModelLen }} '
- '--max-num-seqs={{ .Values.maxBatchSize }}'

# resources
requests:
  cpu: "12"
  memory: "{{ .Values.resources.memory }}"
  ibm.com/spyre_pf: '{{ index .Values.resources.accelerators "ibm.com/spyre_pf" }}'
limits:
  cpu: "12"
  memory: "{{ .Values.resources.memory }}"
  ibm.com/spyre_pf: '{{ index .Values.resources.accelerators "ibm.com/spyre_pf" }}'
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
// ProfileResources holds the resource footprint declared inside a profile's params.
// Injected as .Values.resources so templates reference .Values.resources.memory
// and {{ index .Values.resources.accelerators "ibm.com/spyre_pf" }}.
type ProfileResources struct {
    Memory       int64          `yaml:"memory"`       // bytes, e.g. 214748364800 (200Gi) — matches deploy-options format
    Accelerators map[string]int `yaml:"accelerators"` // e.g. {"ibm.com/spyre_pf": 4}
}

// ProfileParam holds the hardware parameters for one profile entry.
// All fields inject as flat .Values.<key> except Resources which injects as .Values.resources.
type ProfileParam struct {
    Resources           ProfileResources `yaml:"resources"`
    MaxModelLen         int              `yaml:"maxModelLen"`         // vLLM --max-model-len
    MaxBatchSize        int              `yaml:"maxBatchSize"`        // vLLM --max-num-seqs
    MaxNumBatchedTokens int              `yaml:"maxNumBatchedTokens"` // vllm-cpu only
    ShmSize             string           `yaml:"shmSize"`             // shared memory volume sizeLimit
}

// Profile is one entry in a provider's profiles.yaml.
type Profile struct {
    ID          string       `yaml:"id"`
    Default     bool         `yaml:"default"`
    Name        string       `yaml:"name"`
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

### 5.3 `loadProviderModels()` and Deploy Options Assembly

The catalog backend reads `models.yaml` from each provider's directory (`loadProviderModels()`).

1. `GET /api/v1/catalog/models`: Aggregates all models found across all providers' `models.yaml` files, grouping by model `id`.
2. `GET /api/v1/catalog/models/:id/deploy-options`:
   - Gathers all providers containing model `:id` in their `models.yaml`.
   - Reads the model's `profiles` array and `default_profile` from the provider's `models.yaml`.
   - Resolves the profile details (`id`, `title`, `description`, `default`, `params`) by cross-referencing with `profiles.yaml`.
   - Attaches the provider parameter schema (from `values.schema.json`), which contains clean runtime fields without `model` or `profile`.

### 5.4 `LoadComponentValues()` — profile resolution at deploy time

[`LoadComponentValues()`](../../ai-services/internal/pkg/catalog/catalog.go)
is the single function that builds the final values map passed to every
template. It is called with the user-supplied deploy request containing top-level `model` and `profile`.

```go
// 1. Read values.yaml (contains runtime defaults, no hardware parameters)
// 2. Apply argParams (e.g. apiKey)
// 3. Resolve profile:
profiles, err := loadProfilesFromFile(providerDir)
if err != nil {
    return nil, fmt.Errorf("failed to load profiles: %w", err)
}
if len(profiles) > 0 {
    selectedProfile := comp.Profile
    if selectedProfile == "" {
        // Resolve default_profile from provider's models.yaml for this model
        selectedProfile = resolveModelDefaultProfile(providerDir, comp.Model)
    }
    for _, pr := range profiles {
        if pr.ID == selectedProfile {
            p.mergeProfileParams(values, pr.Params)
            break
        }
    }
}
return values, nil
```

```go
// mergeProfileParams writes profile params into the values map so templates
// can reference them as .Values.<key>. resources injects as a nested map
// (.Values.resources.memory, .Values.resources.accelerators).
func (p *CatalogProvider) mergeProfileParams(values map[string]any, params types.ProfileParam) {
    if params.Resources.Memory != "" || len(params.Resources.Accelerators) > 0 {
        res := map[string]any{}
        if params.Resources.Memory != ""           { res["memory"]       = params.Resources.Memory }
        if len(params.Resources.Accelerators) > 0 { res["accelerators"] = params.Resources.Accelerators }
        values["resources"] = res
    }
    if params.MaxModelLen > 0         { values["maxModelLen"]         = params.MaxModelLen }
    if params.MaxBatchSize > 0        { values["maxBatchSize"]        = params.MaxBatchSize }
    if params.MaxNumBatchedTokens > 0 { values["maxNumBatchedTokens"] = params.MaxNumBatchedTokens }
    if params.ShmSize != ""           { values["shmSize"]             = params.ShmSize }
}
```

### 5.5 Full request-to-template flow

```
POST /api/v1/models
  {
    "component_type": "llm",
    "provider_id": "vllm-spyre",
    "model": "ibm-granite/granite-4.1-8b-fp8",
    "profile": "compact-4k",
    "params": {
      "apiKey": "..."
    }
  }
  │
  ▼
model_handler.go  CreateModel()
  │
  ▼
ParamBuilder.BuildModelParams()
  └─► CatalogProvider.LoadComponentValues("llm", "vllm-spyre", comp)
        │
        │  1. Read values.yaml (image, default apiKey)
        │  2. Apply params (apiKey from user)
        │  3. Read llm/vllm-spyre/profiles.yaml
        │       → finds entry id="compact-4k"
        │  4. mergeProfileParams()
        │       → resources={memory:107374182400, accelerators:{"ibm.com/spyre_pf":1}},
        │         maxModelLen=4096, maxBatchSize=32, shmSize="2Gi"
        │
        └─► returns values map:
              { model: "ibm-granite/granite-4.1-8b-fp8", apiKey: "...",
                resources: { memory: 107374182400, accelerators: { "ibm.com/spyre_pf": 1 } },
                maxModelLen: 4096, maxBatchSize: 32, shmSize: "2Gi" }

DeploymentPlanner.calculateAndAllocateSpyreCards()
  └─► CollectSpyreCardsFromTemplates(templates, comp.Values)
        pre-renders vllm-server.yaml.tmpl → reads annotation
          ai-services.io/llm--spyre-cards: '{{ index .Values.resources.accelerators "ibm.com/spyre_pf" }}' → "1"
        checks host has ≥ 1 free card → HTTP 422 if not

PodmanDeployer renders vllm-server.yaml.tmpl with the values map above.
  annotation  ai-services.io/llm--spyre-cards: "1"
  env         AIU_WORLD_SIZE  = "1"
  env         MAX_MODEL_LEN   = "4096"
  env         MAX_BATCH_SIZE  = "32"
  resource    podman.io/device=/dev/vfio: 1
  memory      107374182400  (from .Values.resources.memory)
  shm         "2Gi"    (from .Values.shmSize)

No if/else branches in the template. Profile resolution is done entirely
inside LoadComponentValues() before the template is rendered.
```

`additionalProperties: false` in `values.schema.json` ensures profile-injected keys (`resources`, `maxModelLen` etc.) cannot be supplied inside the user-facing `params` — `params` only accepts declared provider fields. `model` and `profile` are top-level request fields.

---

## 6. Validation & enforcement

| # | Layer | File | What it enforces |
|---|---|---|---|
| 1 | Model & Profile validation | API Handler (`POST /api/v1/models`) | `model` must exist in provider's `models.yaml`; `profile` must be in the model's supported `profiles` list in `models.yaml` and defined in `profiles.yaml` → HTTP 400 if invalid |
| 2 | Schema validation | `values.schema.json` | Validates provider-specific configuration inside `params` (e.g. `apiKey`) |
| 3 | Param merge | `LoadComponentValues()` | Profile params merged into values map; client cannot override individual hardware fields |
| 4 | Spyre pre-check | `planner.go` `calculateAndAllocateSpyreCards()` | Template pre-rendered to read `ai-services.io/llm--spyre-cards` annotation; deploy rejected if host lacks free cards → HTTP 422 |
| 5 | Pod annotation | `vllm-server.yaml.tmpl` | `ai-services.io/llm--spyre-cards: '{{ index .Values.resources.accelerators "ibm.com/spyre_pf" }}'` — authoritative signal for card allocator |
| 6 | Container env | `vllm-server.yaml.tmpl` | `AIU_WORLD_SIZE`, `MAX_MODEL_LEN`, `MAX_BATCH_SIZE` resolved from profile |
| 7 | Resource request | `vllm-server.yaml.tmpl` | `podman.io/device=/dev/vfio: '{{ index .Values.resources.accelerators "ibm.com/spyre_pf" }}'` — Podman device injection |
| 8 | UI resource tile | `StepTwo.tsx` + `metadata.yaml profiles` | Displays card/memory for the selected profile |

---

## 7. File change summary

| File | Change |
|---|---|
| `components/llm/vllm-spyre/models.yaml` | **New** — lists models supported by `vllm-spyre`, with `default_profile` and allowed `profiles` |
| `components/llm/vllm-spyre/profiles.yaml` | **New** — profile definitions for Spyre LLM (shared across Podman & OpenShift) |
| `components/llm/vllm-cpu/models.yaml` | **New** — lists models supported by `vllm-cpu` |
| `components/llm/vllm-cpu/profiles.yaml` | **New** — `standard` profile definition for CPU LLM |
| `components/reranker/vllm-spyre/models.yaml` | **New** — models supported by Spyre Reranker |
| `components/reranker/vllm-spyre/profiles.yaml` | **New** — profile definitions for Spyre Reranker |
| `components/reranker/vllm-cpu/models.yaml` | **New** — models supported by CPU Reranker |
| `components/reranker/vllm-cpu/profiles.yaml` | **New** — profile definitions for CPU Reranker |
| `components/embedding/vllm-cpu/models.yaml` | **New** — models supported by CPU Embedding |
| `components/embedding/vllm-cpu/profiles.yaml` | **New** — profile definitions for CPU Embedding |
| All provider `values.schema.json` | Remove `model`, `profile`, and all `x-profiles` properties; retain only runtime provider fields |
| `components/llm/vllm-spyre/podman/values.yaml` | Remove `maxModelLen`/`maxBatchSize` and `model` |
| `components/llm/vllm-spyre/openshift/values.yaml` | Remove resource params and `model` |
| `components/llm/vllm-cpu/podman/values.yaml` | Remove `maxNumBatchedTokens`/`maxModelLen`/`maxBatchSize` and `model` |
| `components/llm/vllm-cpu/openshift/values.yaml` | Same |
| `components/llm/vllm-spyre/podman/templates/vllm-server.yaml.tmpl` | Replace 8 hardcoded occurrences with `.Values.*` keys |
| `components/llm/vllm-spyre/openshift/templates/instruct-inferenceservice.yaml` | Replace args + resource values |
| `components/llm/vllm-cpu/podman/templates/vllm-server.yaml.tmpl` | Replace 3 CLI flag values |
| `components/llm/vllm-cpu/openshift/templates/instruct-inferenceservice.yaml` | Replace args values |
| `components/llm/vllm-spyre/podman/metadata.yaml` | Add `profiles` resource map |
| `components/llm/vllm-spyre/openshift/metadata.yaml` | Add `profiles` resource map |
| Catalog backend — asset loader | Add `loadProfilesFromFile()`, `loadProviderModels()`, deploy options aggregator |
| `router.go` | Register `GET /catalog/models`, `GET /catalog/models/:id`, `GET /catalog/models/:id/deploy-options`, `GET /components/:component_type/providers/:provider_id/profiles` |


## 8. Why standalone `models.yaml` and `profiles.yaml` rather than embedded in `values.schema.json`

Embedding model definitions and profile definitions inside `values.schema.json` created deep schema bloat and coupled runtime input validation to catalog metadata. Decoupling them into `models.yaml` and `profiles.yaml` provides key architectural benefits:

- **Clean Schema**: `values.schema.json` remains strictly for validating provider runtime parameters (e.g. `apiKey`), without leaking model lists or hardware tuning flags into user-facing schemas.
- **Provider Autonomy**: Each provider declares precisely which models it supports in its own `models.yaml` and what hardware configurations it validated in `profiles.yaml`.
- **Composable Catalog**: The catalog service aggregates `models.yaml` across all providers to construct the unified catalog and deploy options dynamically.
- **Explicit APIs**: `GET /api/v1/catalog/models/:id/deploy-options` gives the UI typed provider and profile lists directly without schema parsing.

---

## 9. What is out of scope

- Application-level templates (`applications/rag`, `rag-dev`) embed their own
  fixed configurations and are not touched.
- The question of which models each service supports, and which profile applies
  per service, is addressed in
  [service-model-contracts.md](./service-model-contracts.md).

---

## 10. Impact on [`model-management-LLD.md`](./model-management-LLD.md)

### §4.1 Models — concept update
- A **Model** deployment specifies `model` and `profile` as top-level fields — the profile determines the hardware configuration used for deployment.

### §4.3 Supported Provider Params
- Hardware parameters (`resources.accelerators`, `resources.memory`, `maxModelLen`, `maxBatchSize`) are removed from provider `params` and `values.schema.json` entirely, derived instead from `profiles.yaml`.

### §5.2 `components.metadata` JSONB — store resolved profile
- The `metadata` JSONB for local model rows includes `"profile"` and `"model"`:
  ```json
  { "model": "ibm-granite/granite-4.1-8b-fp8", "profile": "compact-4k" }
  ```

### §6.1 Model Endpoints — `POST /api/v1/models`
- `model` and `profile` are top-level request fields (outside `params`).
- The backend validates `model` against the provider's `models.yaml` and `profile` against `profiles.yaml`.

### §7.1 Deploy a Local Model — request body example
```json
{
  "component_type": "llm",
  "name": "granite-extract-llm",
  "provider_id": "vllm-spyre",
  "model": "ibm-granite/granite-4.1-8b-fp8",
  "profile": "compact-4k",
  "params": {}
}
```

### §8. Pre-flight Resource Check
- The pre-flight check reads `resources.accelerators` from the resolved profile — e.g. `compact-4k` requests 1 card, `balanced` requests 2, `standard` requests 4.

### §8. LiteLLM Route ID
- The LiteLLM route ID uses the deployment ID (`components.id` / `connectors.id`) for unique, unambiguous routing across multiple deployments of the same model and profile.

---

## 11. GET `/api/v1/components/:component_type/providers/:provider_id/profiles`

Returns all hardware profiles for a model-category provider, optionally filtered to one model.

**Query parameter:** `?model=<full_model_name>` (optional). When supplied, only profiles listed in that model's `profiles` array in the provider's `models.yaml` are returned. When omitted, all profiles in the provider's `profiles.yaml` are returned.

**Response `200 OK`:**

```json
[
  {
    "id": "standard",
    "name": "Standard",
    "description": "4 cards, 200 GiB RAM, 32k context",
    "default": false,
    "params": {
      "resources": { "memory": 214748364800, "accelerators": { "ibm.com/spyre_pf": 4 } },
      "maxModelLen": 32768, "maxBatchSize": 32, "shmSize": "2Gi"
    }
  },
  {
    "id": "compact-4k",
    "name": "Compact 4k",
    "description": "1 card, 100 GiB RAM, 4k context",
    "default": true,
    "params": {
      "resources": { "memory": 107374182400, "accelerators": { "ibm.com/spyre_pf": 1 } },
      "maxModelLen": 4096, "maxBatchSize": 32, "shmSize": "2Gi"
    }
  }
]
```


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
    params        JSONB        NOT NULL,            -- resources:{memory,accelerators}, maxModelLen, maxBatchSize, shmSize
    created_by    VARCHAR(100),                     -- 'system' for built-ins, user ID/email for custom
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (provider_id, profile_id)
);
CREATE INDEX ON model_profiles(provider_id);
```

`params` follows the same shape as `profiles.yaml` (`resources.memory`, `resources.accelerators`, `maxModelLen`, `maxBatchSize`, `shmSize`). The `(provider_id, profile_id)` unique constraint prevents duplicate IDs per provider.

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
    "name": "Custom compact 6k",
    "description": "Team-specific 1-card / 6 144 context configuration.",
    "params": {
      "resources": { "memory": 107374182400, "accelerators": { "ibm.com/spyre_pf": 1 } },
      "maxModelLen": 6144, "maxBatchSize": 32, "shmSize": "2Gi"
    }
  }'
```

Response `201 Created`:

```json
{
  "id": "e4f5a6b7-...",
  "provider_id": "vllm-spyre",
  "profile_id": "my-compact-6k",
  "name": "Custom compact 6k",
  "is_default": false,
  "params": {
    "resources": { "memory": 107374182400, "accelerators": { "ibm.com/spyre_pf": 1 } },
    "maxModelLen": 6144, "maxBatchSize": 32, "shmSize": "2Gi"
  },
  "created_by": "user@example.com"
}
```

### 12.4 Backend changes

| Component | Change |
|---|---|
| `loadProfilesFromFile()` | Called once at startup / migration to seed the DB; not used for request-time resolution in Phase 2 |
| `GetComponentProviderProfiles()` | Reads from `model_profiles` table; filters by `provider_id` and model's supported `profiles` list in `models.yaml` |
| `ProfileRepository` | New repo — `Create`, `Update`, `Delete`, `ListByProvider` |
| Migration | `20260901000001_create_model_profiles_table.sql` — creates table and executes data seeding for built-ins |

### 12.5 Immutability & Constraints

- **Strict Immutability for Built-ins:** Users and administrators are **not allowed to modify or delete built-in profiles** (`is_builtin = true` / `created_by = 'system'`). Any `PUT` or `DELETE` request targeting a built-in profile returns `403 Forbidden` with error code `BUILTIN_PROFILE_READONLY`. To use a different configuration, users must create a new custom profile with a distinct `profile_id`.
- **Scope Restriction:** Profiles apply only to providers with **`category: model`** in `metadata.yaml` (`llm`, `reranker`, `embedding`). Requests for providers without `category: model` return `400 Bad Request`.
- **No live migration:** Updated custom profile parameters apply to new deployments only; running pods continue with their provisioned parameters.
