---
name: bump-service-version
description: Use when the user wants to bump, update, or change the version/image tag of a specific service, UI component, or the catalog backend (ai-services) itself inside the ai-services project. Services live under services/; UI components live under ui/; the catalog backend Makefile is ai-services/Makefile. Steps: bump the Makefile TAG, sync the manifest values.yaml, then (for service/UI bumps) also bump the catalog backend version.
metadata:
  argument-hint: "<service-or-ui-or-catalog-name> <new-version>"
---

# Bump Service Version

This skill operates on three types of components:

1. **Backend services** — live under `services/`
2. **UI components** — live under `ui/`
3. **Catalog backend (`ai-services`)** — the main platform image; Makefile at `ai-services/Makefile`

Tracks A and B (service and UI bumps) always finish with the shared Steps 6–8, which bump the
catalog backend as a follow-on. Track C is used when the user explicitly wants to bump the
catalog backend directly — in that case there is no further follow-on step.

Do not skip any step.

---

## Step 1 — Identify the component

If the user has not supplied a component name, ask using `ask_followup_question`.

Determine whether the user is asking about a **service** or a **UI component** from the name.

### Backend services

| Service name | Service Makefile                    | IMAGE value in Makefile  | Manifest values.yaml key |
|--------------|-------------------------------------|--------------------------|--------------------------|
| `chatbot`    | `services/chatbot/Makefile`         | `chatbot-service`        | `backend.image`          |
| `digitize`   | `services/digitize/Makefile`        | `digitize-service`       | `digitize.image`         |
| `summarize`  | `services/summarize/Makefile`       | `summarize-service`      | `summarize.image`        |
| `similarity` | `services/similarity/Makefile`      | `similarity-service`     | `similarity.image`       |

Registry for all **service** images: `icr.io/ai-services-cicd/`
> Even if an existing image line uses `icr.io/ai-services/`, always write the updated line with
> `icr.io/ai-services-cicd/`.

### UI components

| UI name           | UI Makefile               | IMAGE value in Makefile | Manifest file                                            | Manifest image key   | Registry                  |
|-------------------|---------------------------|-------------------------|----------------------------------------------------------|----------------------|---------------------------|
| `catalog` (ui)    | `ui/catalog/Makefile`     | `catalog-ui`            | `ai-services/assets/catalog/podman/values.yaml`          | `ui.image`           | `icr.io/ai-services/`     |
| `chatbot` (ui)    | `ui/chatbot/Makefile`     | `chatbot-ui`            | `ai-services/assets/services/chat/podman/values.yaml`    | `ui.image`           | `icr.io/ai-services/`     |
| `digitize` (ui)   | `ui/digitize/Makefile`    | `digitize-ui`           | `ai-services/assets/services/digitize/podman/values.yaml`| `digitizeUi.image`   | `icr.io/ai-services/`     |

Registry for all **UI** images: `icr.io/ai-services/`
> UI images always use `icr.io/ai-services/` — do **not** switch to `icr.io/ai-services-cicd/`.

**Disambiguation rules:**
- Phrasing includes "ui" (e.g. "bump catalog ui", "bump chatbot ui", "bump digitize ui") → **Track B**.
- Phrasing refers to a named backend service without "ui" (e.g. "bump chatbot", "bump digitize service") → **Track A**.
- Phrasing refers to the catalog backend or the platform itself — any of: "bump catalog version", "bump catalog backend", "bump ai-services", "bump catalog service" → **Track C**. Do **not** run Steps 6–8 after Track C.

If the user has not supplied a new version tag, **do not ask** — read the current `TAG?=` (or `TAG=`)
from the component Makefile and increment the patch segment by 1 (e.g. `v0.0.42` → `v0.0.43`).

Version tags must follow the pattern `vX.Y.Z`. Validate the format before proceeding.

---

## Track A — Bumping a backend service (Step 2A–5A)

### Step 2A — Read the service Makefile

Use `read_file` on `services/<name>/Makefile` to get the exact current `TAG?=` value.
This is the version being replaced in all subsequent steps.

### Step 3A — Bump the service Makefile

Update `TAG?=<old>` → `TAG?=<new>` in `services/<name>/Makefile` using `search_and_replace`.

Example — bumping `chatbot` service to `v0.0.22`:
```
search:  TAG?=v0.0.21
replace: TAG?=v0.0.22
```

### Step 4A — Read the service manifest values.yaml

Use `read_file` on the corresponding manifest to confirm the exact current image line:

| Service     | Manifest file                                                     |
|-------------|-------------------------------------------------------------------|
| `chatbot`   | `ai-services/assets/services/chat/podman/values.yaml`            |
| `digitize`  | `ai-services/assets/services/digitize/podman/values.yaml`        |
| `summarize` | `ai-services/assets/services/summarize/podman/values.yaml`       |
| `similarity`| `ai-services/assets/services/similarity/podman/values.yaml`      |

### Step 5A — Update the service manifest values.yaml

Replace the old image line with the new one using `search_and_replace`. Always use
`icr.io/ai-services-cicd/` as the registry, regardless of what the current line contains.

Example — bumping `chatbot` service to `v0.0.22`:
```
search:  image: icr.io/ai-services/chatbot-service:v0.0.21
replace: image: icr.io/ai-services-cicd/chatbot-service:v0.0.22
```

The YAML key to update per service:
- `chatbot`    → `backend.image`
- `digitize`   → `digitize.image`
- `summarize`  → `summarize.image`
- `similarity` → `similarity.image`

After completing Step 5A, continue to **Step 6**.

---

## Track B — Bumping a UI component (Step 2B–5B)

### Step 2B — Read the UI Makefile

Use `read_file` on `ui/<name>/Makefile` to get the exact current `TAG?=` (or `TAG=`) value.
Note: the chatbot UI Makefile uses `TAG?=` without spaces (`TAG?=v0.0.46`). Match the exact
whitespace when searching.

### Step 3B — Bump the UI Makefile

Update the TAG line in `ui/<name>/Makefile` using `search_and_replace`.

Examples:
```
# catalog UI to v0.0.43
search:  TAG ?= v0.0.42
replace: TAG ?= v0.0.43

# chatbot UI to v0.0.47
search:  TAG?=v0.0.46
replace: TAG?=v0.0.47

# digitize UI to v0.0.27
search:  TAG ?= v0.0.26
replace: TAG ?= v0.0.27
```

### Step 4B — Read the UI asset values.yaml

Use `read_file` on the corresponding asset values.yaml to confirm the exact current image line:

| UI component | Asset values.yaml                                                   |
|--------------|---------------------------------------------------------------------|
| `catalog`    | `ai-services/assets/catalog/podman/values.yaml`                    |
| `chatbot`    | `ai-services/assets/services/chat/podman/values.yaml`              |
| `digitize`   | `ai-services/assets/services/digitize/podman/values.yaml`          |

### Step 5B — Update the UI asset values.yaml

Replace the old image line with the new one using `search_and_replace`. Always keep
`icr.io/ai-services/` as the registry for UI images.

Examples:
```
# catalog UI to v0.0.43  (key: ui.image in ai-services/assets/catalog/podman/values.yaml)
search:  image: icr.io/ai-services/catalog-ui:v0.0.42
replace: image: icr.io/ai-services/catalog-ui:v0.0.43

# chatbot UI to v0.0.47  (key: ui.image in ai-services/assets/services/chat/podman/values.yaml)
search:  image: icr.io/ai-services/chatbot-ui:v0.0.46
replace: image: icr.io/ai-services/chatbot-ui:v0.0.47

# digitize UI to v0.0.27  (key: digitizeUi.image in ai-services/assets/services/digitize/podman/values.yaml)
search:  image: icr.io/ai-services/digitize-ui:v0.0.26
replace: image: icr.io/ai-services/digitize-ui:v0.0.27
```

After completing Step 5B, continue to **Step 6**.

---

## Track C — Bumping the catalog backend (ai-services) directly (Steps 2C–4C)

Use this track **only** when the user explicitly asks to bump the catalog backend version,
catalog service version, or `ai-services` directly (e.g. "bump catalog version",
"bump catalog backend", "bump ai-services"). Do **not** run Steps 6–8 after this track.

### Step 2C — Read the ai-services Makefile

Use `read_file` on `ai-services/Makefile` to get the exact current `TAG?=` value.

If the user has not supplied a new version tag, increment the patch segment by 1
(e.g. `v0.0.161` → `v0.0.162`).

### Step 3C — Bump the ai-services Makefile

Update `TAG?=<old>` → `TAG?=<new>` in `ai-services/Makefile` using `search_and_replace`.

Example:
```
search:  TAG?=v0.0.161
replace: TAG?=v0.0.162
```

### Step 4C — Update ai-services/assets/catalog/podman/values.yaml

Use `read_file` first to confirm the exact current `backend.image` line, then replace it using
`search_and_replace`. The YAML key is `backend.image`. Registry is always
`icr.io/ai-services-cicd/ai-services`.

Example:
```
search:  image: icr.io/ai-services-cicd/ai-services:v0.0.161
replace: image: icr.io/ai-services-cicd/ai-services:v0.0.162
```

After completing Step 4C, skip directly to **Step 9** (Verify).

---

## Step 6 — Read the ai-services Makefile and catalog values.yaml

> **Tracks A and B only.** Skip entirely for Track C.

After updating any service or UI component, always bump the ai-services catalog version too.

Read both files to get the current `ai-services` version:
1. `read_file` → `ai-services/Makefile` (look for `TAG?=`)
2. `read_file` → `ai-services/assets/catalog/podman/values.yaml` (look for `backend.image`)

Increment the `ai-services` patch version by 1 (e.g. `v0.0.161` → `v0.0.162`).

## Step 7 — Bump the ai-services Makefile

> **Tracks A and B only.**

Update `TAG?=<old>` → `TAG?=<new>` in `ai-services/Makefile` using `search_and_replace`.

Example:
```
search:  TAG?=v0.0.161
replace: TAG?=v0.0.162
```

## Step 8 — Update ai-services/assets/catalog/podman/values.yaml

> **Tracks A and B only.**

Replace the `backend.image` line using `search_and_replace`. Registry is always
`icr.io/ai-services-cicd/ai-services`.

Example:
```
search:  image: icr.io/ai-services-cicd/ai-services:v0.0.161
replace: image: icr.io/ai-services-cicd/ai-services:v0.0.162
```

## Step 9 — Verify

**Track A (service bumps)** — grep for the old service image tag:
```
grep pattern: <IMAGE-name>:<old-service-version>
path: services/<name>/
```

**Track B (UI bumps)** — grep for the old UI image tag:
```
grep pattern: <IMAGE-name>:<old-ui-version>
path: ui/<name>/
```

**All tracks** — grep for the old ai-services version in `ai-services/Makefile`:
```
grep pattern: TAG\?=<old-ai-services-version>
path: ai-services/Makefile
```

If any unexpected hits remain outside the legacy `ai-services/assets/applications/` tree,
investigate and fix before reporting.

## Step 10 — Report

Summarise all changes in a single table:

**Track A — service bump:**

| File | Changed value |
|------|---------------|
| `services/<name>/Makefile` | `TAG?=<old>` → `TAG?=<new>` |
| `ai-services/assets/services/<manifest-path>/values.yaml` | `<image>:<old>` → `<image>:<new>` |
| `ai-services/Makefile` | `TAG?=<old>` → `TAG?=<new>` |
| `ai-services/assets/catalog/podman/values.yaml` | `backend.image: ai-services:<old>` → `ai-services:<new>` |

**Track B — UI bump:**

| File | Changed value |
|------|---------------|
| `ui/<name>/Makefile` | `TAG?=<old>` → `TAG?=<new>` |
| `<ui-asset-values.yaml>` | `<ui-image>:<old>` → `<ui-image>:<new>` |
| `ai-services/Makefile` | `TAG?=<old>` → `TAG?=<new>` |
| `ai-services/assets/catalog/podman/values.yaml` | `backend.image: ai-services:<old>` → `ai-services:<new>` |

**Track C — catalog backend bump:**

| File | Changed value |
|------|---------------|
| `ai-services/Makefile` | `TAG?=<old>` → `TAG?=<new>` |
| `ai-services/assets/catalog/podman/values.yaml` | `backend.image: ai-services:<old>` → `ai-services:<new>` |

Confirm no stale references were found.
