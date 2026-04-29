# Backend Reorganization: Execution Plan (Dev 1)

Branch: `code-reorg-backend-infra`

---

## Architectural Notes (Read First)

- **Similarity service** is missing from the original proposal — it must be included as `services/similarity/`
- **Python imports** work today because `spyre-rag/src/` is the Python root. After the split, each service Containerfile must set `PYTHONPATH=/opt/services` so `from common.X import` keeps resolving
- **Deployment templates** hardcode `chatbot.app:app`, `digitize.app:app` etc. — after the split these become `app:app` since each service is its own root
- **Do not touch `spyre-rag/ui/`** — that is Dev 2's scope
- **Always run `git mv`** — never `mv`. Preserves blame and log history

---

## Target Structure

```
project-ai-services/
├── images/
│   └── python-base/          # renamed from images/rag-base
│
├── services/
│   ├── common/               # from spyre-rag/src/common
│   ├── chatbot/              # from spyre-rag/src/chatbot
│   ├── digitize/             # from spyre-rag/src/digitize
│   ├── summarize/            # from spyre-rag/src/summarize
│   └── similarity/           # from spyre-rag/src/similarity (missing from proposal)
│
└── spyre-rag/
    └── ui/                   # untouched — Dev 2's scope
```

---

## Commit 1 — Rename `rag-base` → `python-base`

### Commands
```bash
git mv images/rag-base images/python-base
git mv .github/workflows/spyre-rag-base.yml .github/workflows/python-base-image.yml
```

### File edits

**`images/python-base/Makefile`**
- Change `IMAGE=rag-base` → `IMAGE=python-base`

**`.github/workflows/python-base-image.yml`**
- `name: RAG Base` → `name: Python Base`
- `working-directory: images/rag-base` → `working-directory: images/python-base` (x2)
- `- 'images/rag-base/**'` → `- 'images/python-base/**'` (x2, in push + pull_request triggers)
- `path: 'images/rag-base'` → `path: 'images/python-base'`
- `image: 'rag-base'` → `image: 'python-base'`

### Verify
```bash
git status
grep -r "rag-base" .github/ images/   # should return nothing
```

### Commit
```bash
git commit -m "feat: rename rag-base to python-base

- git mv images/rag-base to images/python-base
- Update Makefile IMAGE variable
- Rename and update CI workflow paths and image name"
```

---

## Commit 2 — Move `common` and create its Containerfile

### Commands
```bash
mkdir -p services/common
git mv spyre-rag/src/common/* services/common/
```

### Files to create

**`services/common/Containerfile`**
```dockerfile
FROM python-base:latest

WORKDIR /opt/services

# Copy common library — placed at /opt/services/common/
# so services can import it via PYTHONPATH=/opt/services
COPY . common/
```

**`services/common/requirements.txt`**
```
# common-specific dependencies beyond python-base
# Leave empty — all heavy deps live in images/python-base/requirements.txt
```

### Verify
```bash
# Test import resolution locally (simulates container PYTHONPATH)
cd /path/to/project-ai-services
PYTHONPATH=services python -c "from common.settings import get_settings; print('ok')"
PYTHONPATH=services python -c "from common.db_utils import get_db; print('ok')"

# Verify git history survived the move
git log --follow --oneline services/common/settings.py
```

### Commit
```bash
git commit -m "feat: move common library to services/common

- git mv spyre-rag/src/common to services/common
- Add Containerfile extending python-base
- Add empty requirements.txt for service-specific deps"
```

---

## Commit 3 — Move `chatbot` and create its Containerfile

### Commands
```bash
mkdir -p services/chatbot
git mv spyre-rag/src/chatbot/* services/chatbot/
```

### Files to create

**`services/chatbot/Containerfile`**
```dockerfile
FROM services-common:latest

WORKDIR /opt/services/chatbot

ENV PYTHONPATH=/opt/services

COPY requirements.txt .
RUN source /var/venv/bin/activate && pip install -r requirements.txt && pip cache purge

COPY . .

CMD ["/var/venv/bin/python", "-m", "uvicorn", "app:app", \
     "--host", "0.0.0.0", "--port", "5000", \
     "--loop", "uvloop", "--http", "httptools"]
```

**`services/chatbot/requirements.txt`**
```
# chatbot-specific dependencies beyond services-common
```

**`services/chatbot/Makefile`**
```makefile
REGISTRY?=icr.io/ai-services-private
IMAGE=chatbot
TAG?=v0.0.1
CONTAINER_BUILDER?=podman

build:
	$(CONTAINER_BUILDER) build -t $(REGISTRY)/$(IMAGE):$(TAG) .
.PHONY: build

push:
	$(CONTAINER_BUILDER) push $(REGISTRY)/$(IMAGE):$(TAG)
.PHONY: push

run:
	PYTHONPATH=/opt/services /var/venv/bin/python -m uvicorn app:app --host 0.0.0.0 --port 5000
.PHONY: run
```

### Verify
```bash
git log --follow --oneline services/chatbot/app.py
```

### Commit
```bash
git commit -m "feat: move chatbot service to services/chatbot

- git mv spyre-rag/src/chatbot to services/chatbot
- Add Containerfile extending services-common
- Add Makefile and requirements.txt"
```

---

## Commit 4 — Move `digitize` and create its Containerfile

### Commands
```bash
mkdir -p services/digitize
git mv spyre-rag/src/digitize/* services/digitize/
```

### Files to create

**`services/digitize/Containerfile`** — same pattern as chatbot, port `4000`:
```dockerfile
FROM services-common:latest

WORKDIR /opt/services/digitize

ENV PYTHONPATH=/opt/services

COPY requirements.txt .
RUN source /var/venv/bin/activate && pip install -r requirements.txt && pip cache purge

COPY . .

CMD ["/var/venv/bin/python", "-m", "uvicorn", "app:app", \
     "--host", "0.0.0.0", "--port", "4000", \
     "--loop", "uvloop", "--http", "httptools"]
```

**`services/digitize/requirements.txt`** — empty initially

**`services/digitize/Makefile`** — same as chatbot, swap `IMAGE=digitize` and port `4000`

### Verify
```bash
git log --follow --oneline services/digitize/app.py
```

### Commit
```bash
git commit -m "feat: move digitize service to services/digitize

- git mv spyre-rag/src/digitize to services/digitize
- Add Containerfile extending services-common
- Add Makefile and requirements.txt"
```

---

## Commit 5 — Move `summarize` and create its Containerfile

### Commands
```bash
mkdir -p services/summarize
git mv spyre-rag/src/summarize/* services/summarize/
```

**`services/summarize/Containerfile`** — port `6000`, otherwise identical pattern

**`services/summarize/requirements.txt`** — empty initially

**`services/summarize/Makefile`** — `IMAGE=summarize`, port `6000`

### Commit
```bash
git commit -m "feat: move summarize service to services/summarize

- git mv spyre-rag/src/summarize to services/summarize
- Add Containerfile extending services-common
- Add Makefile and requirements.txt"
```

---

## Commit 6 — Move `similarity` and create its Containerfile

> Note: similarity is absent from the original proposal — it is included here intentionally.

### Commands
```bash
mkdir -p services/similarity
git mv spyre-rag/src/similarity/* services/similarity/
```

**`services/similarity/Containerfile`** — port `7000`, otherwise identical pattern

**`services/similarity/requirements.txt`** — empty initially

**`services/similarity/Makefile`** — `IMAGE=similarity`, port `7000`

### Commit
```bash
git commit -m "feat: move similarity service to services/similarity

- git mv spyre-rag/src/similarity to services/similarity
- Add Containerfile extending services-common
- Add Makefile and requirements.txt
- Note: similarity was missing from original proposal, added here"
```

---

## Commit 7 — Update deployment templates

All templates use the module-path form (`chatbot.app:app`) which breaks once each
service is its own container root. Replace with direct form (`app:app`).

### Files to update

| Template | Change |
|---|---|
| `rag/openshift/templates/backend-deployment.yaml` | `chatbot.app:app` → `app:app` |
| `rag/podman/templates/chat-bot.yaml.tmpl` | `chatbot.app:app` → `app:app` |
| `rag-dev/openshift/templates/backend-deployment.yaml` | `chatbot.app:app` → `app:app` |
| `rag-dev/podman/templates/chat-bot.yaml.tmpl` | `chatbot.app:app` → `app:app` |
| `rag-cpu/podman/templates/chat-bot.yaml.tmpl` | `chatbot.app:app` → `app:app` |
| `services/chat/podman/templates/chat-bot.yaml.tmpl` | `chatbot.app:app` → `app:app` |
| `rag/openshift/templates/digitize-api-deployment.yaml` | `digitize.app:app` → `app:app` |
| `rag/podman/templates/digitize.yaml.tmpl` | `digitize.app:app` → `app:app` |
| `rag-dev/openshift/templates/digitize-api-deployment.yaml` | `digitize.app:app` → `app:app` |
| `rag-dev/podman/templates/digitize.yaml.tmpl` | `digitize.app:app` → `app:app` |
| `rag-cpu/podman/templates/digitize.yaml.tmpl` | `digitize.app:app` → `app:app` |
| `services/digitize/podman/templates/digitize.yaml.tmpl` | `digitize.app:app` → `app:app` |
| `rag/openshift/templates/summarize-api-deployment.yaml` | `summarize.app:app` → `app:app` |
| `rag/podman/templates/summarize-api.yaml.tmpl` | `summarize.app:app` → `app:app` |
| `rag-dev/openshift/templates/summarize-api-deployment.yaml` | `summarize.app:app` → `app:app` |
| `rag-dev/podman/templates/summarize-api.yaml.tmpl` | `summarize.app:app` → `app:app` |
| `rag-cpu/podman/templates/summarize-api.yaml.tmpl` | `summarize.app:app` → `app:app` |
| `services/summarize/podman/templates/summarize-api.yaml.tmpl` | `summarize.app:app` → `app:app` |
| `rag/openshift/templates/similarity-api-deployment.yaml` | `similarity.app:app` → `app:app` |
| `rag/podman/templates/similarity-api.yaml.tmpl` | `similarity.app:app` → `app:app` |
| `rag-dev/openshift/templates/similarity-api-deployment.yaml` | `similarity.app:app` → `app:app` |
| `rag-dev/podman/templates/similarity-api.yaml.tmpl` | `similarity.app:app` → `app:app` |
| `rag-cpu/podman/templates/similarity-api.yaml.tmpl` | `similarity.app:app` → `app:app` |

### Verify
```bash
grep -r "chatbot.app:app\|digitize.app:app\|summarize.app:app\|similarity.app:app" \
  ai-services/ --include="*.yaml" --include="*.tmpl"
# Should return nothing
```

### Commit
```bash
git commit -m "feat: update deployment templates for per-service entrypoints

- Change module-path uvicorn targets (chatbot.app:app) to direct form (app:app)
- Applies to rag, rag-dev, rag-cpu openshift + podman templates
- Required because each service is now its own container root"
```

---

## Commit 8 — Remove monolith artifacts

```bash
git rm spyre-rag/src/Containerfile
git rm spyre-rag/src/Makefile
```

### Verify
```bash
ls spyre-rag/src/   # should be empty (ui/ is still at spyre-rag/ui/ — leave it)
git status
```

### Commit
```bash
git commit -m "chore: remove monolithic spyre-rag/src Containerfile and Makefile

- Single image build replaced by per-service images in services/
- spyre-rag/ui left intact for Dev 2 (frontend stream)"
```

---

## Commit 9 — Update spyre-rag-image CI workflow

```bash
git mv .github/workflows/spyre-rag-image.yml .github/workflows/services-image.yml
```

Update the file contents to trigger on `services/**` and build each service after `services-common`.

### Commit
```bash
git commit -m "feat: replace monolithic spyre-rag CI workflow with per-service builds

- Rename spyre-rag-image.yml to services-image.yml
- Trigger on services/** instead of spyre-rag/src/**
- Build services-common first, then per-service images"
```

---

## End-to-End Verification Checklist

Run these before opening the PR:

```bash
# 1. Git history survived all moves
git log --follow --oneline services/chatbot/app.py
git log --follow --oneline services/similarity/similarity_utils.py
git log --follow --oneline services/common/settings.py

# 2. Python import resolution (simulates container PYTHONPATH)
PYTHONPATH=services python -c "from common.settings import get_settings; print('imports ok')"

# 3. No stale module-path entrypoints
grep -r "chatbot.app:app\|digitize.app:app\|summarize.app:app\|similarity.app:app" \
  ai-services/ --include="*.yaml" --include="*.tmpl"

# 4. No stale references to old paths
grep -r "spyre-rag/src" .github/ ai-services/
grep -r "rag-base" .github/ images/

# 5. Container builds (requires podman)
cd images/python-base && podman build -t python-base:latest .
cd services/common && podman build -t services-common:latest .
cd services/chatbot && podman build -t chatbot:latest .
podman run --rm chatbot:latest /var/venv/bin/python -c \
  "from common.settings import get_settings; print('container import ok')"
```

---

## Dependency Reminder

Do not delete or move `spyre-rag/ui/` — that directory is Dev 2's scope and will be
handled in the integration phase after both streams complete.
