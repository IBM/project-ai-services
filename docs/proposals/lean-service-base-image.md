# Slim Down `service-base` Image Proposal

## Summary

The `service-base` container image previously bundled every Python dependency
required by **all** services — including the heavy ML/document-processing stack
needed only by `digitize` (PyTorch, docling, OpenCV, transformers, etc.) and the
docling models downloaded at build time. This caused every service image
(chatbot, extract, similarity, summarize, translate) to inherit and ship an
unnecessarily large image layer.

This proposal moves the digitize-only Python packages, docling OS packages and
docling models out of `service-base` and into the `digitize` service image
itself. No additional base
image is introduced: an intermediate `digitize-base` image was evaluated and
dropped (see [Alternatives Considered](#alternatives-considered)).

| Image | Tag | Size | Change |
|---|---|---|---|
| `service-base` | `v0.0.45` | ~4 GB | Before: shared base including the full digitize ML stack |
| `service-base` | `v0.0.46` | ~1.9 GB | After: lean shared base for all services |
| `digitize-service` | `v0.0.81` | ~4 GB | Builds on `service-base:v0.0.46`; owns the docling/ML stack and models |

`service-base` shrinks by roughly **2.1 GB (~52%)**, while `digitize-service`
stays at about the same size as before.

---

## Problem

The original `service-base` `Containerfile`:

- Downloaded docling ML models (`ds4sd--docling-models` — Layout & TableFormer)
  at image build time for every service, regardless of whether the service used
  them.
- Carried ~90 Python packages (docling, torch, torchvision, transformers,
  OpenCV, pandas, scipy, …) in the shared layer consumed by chatbot, extract,
  similarity, summarize, and translate.
- Installed OS packages needed only by docling / its ML dependencies
  (`lcms2-devel`, `openblas-devel`, `freetype`, `libicu`, `libjpeg-turbo`,
  `spatialindex-devel` and the EPEL repository they depend on) in every
  service image.
- Included a `docling/` sub-image workaround (patch-based Containerfile +
  `0001-Exclude-source-that-has-commercial-license_v0.22.1.patch` +
  `docling_v2.60.1.patch`) which is no longer needed.

---

## Solution

### `images/service-base`

Lean base image consumed by all services. Contains only:

- **OS:** `python3.12-devel` (plus the `python`/`python3` → `python3.12`
  symlinks). No docling-related OS packages and no EPEL repository.
- **Runtime:** `fastapi`, `pydantic`, `pydantic-settings`, `requests`,
  `starlette`, `uvicorn`, `uvloop`, `python-multipart`, `psutil`
- **Database:** `psycopg2`, `SQLAlchemy`
- **Search / Embeddings:** `cohere`, `numpy`, `opensearch-py`, `tqdm`
- **Language:** `lingua-language-detector`, `sentence-splitter`, `spacy` +
  Japanese model (`ja_core_news_sm`), `SudachiDict-core`, `SudachiPy`
- **Documents:** `pypdfium2`
- **SFTP:** `paramiko`

The `prebuilder` stage supplies pre-compiled `.whl` files (for packages that
require Rust/C compilation on ppc64le) via a bind-mount at build time. `pip` is
stripped from the venv after installation so it does not appear in the runtime
image or SBOM scanners.

```
images/service-base/
├── Containerfile
├── Makefile
├── requirements.txt
└── prebuilder/
    ├── Containerfile
    └── pre-requirements.txt
```

### `services/digitize`

Extends `service-base:v0.0.46` and adds the dependencies exclusive to the
`digitize` service directly in its own `Containerfile`:

- **ML / document processing:** `docling`, `docling-core`, `docling-ibm-models`,
  `docling-parse`, `docling-slim`, `torch`, `torchvision`, `transformers`,
  `accelerate`, `safetensors`, `opencv-python-headless`, `pillow`, `scipy`,
  `pandas`, … (now listed in `services/digitize/requirements.txt`, together with
  the existing `boto3`, `botocore`, `s3transfer`, `paramiko` and `apscheduler`)
- **OS packages (moved from `service-base`):** installed via `microdnf` in the
  `digitize` `Containerfile`, before the Python dependencies are installed:

  | Package | Notes |
  |---|---|
  | `lcms2-devel` | Image/colour handling libs for the docling / OpenCV / Pillow stack |
  | `openblas-devel` | BLAS/LAPACK for the numeric / ML stack |
  | `freetype` | Font rendering |
  | `libicu` | Unicode/ICU runtime library |
  | `libjpeg-turbo` | JPEG codec |
  | `epel-release` | EPEL 9 release RPM (downloaded from `dl.fedoraproject.org`); prerequisite for `spatialindex-devel` |
  | `spatialindex-devel` | Spatial index library, available only from EPEL |

  `python3.12-devel` remains in `service-base`. The EPEL release RPM is
  downloaded, installed and removed in the same `RUN` layer, followed by
  `microdnf clean all`, so no installer artefacts remain in the image.
- **Docling models:** downloaded at build time via `download_docling_models.py`
  (Layout + TableFormer; OCR/EasyOCR/code-formula/picture-classifier excluded)
  into `DOCLING_MODELS_PATH=/var/docling-models`

`hf-xet` is disabled at build time (`HF_HUB_DISABLE_XET=1`) to prevent a fatal
GIL/Tokio runtime re-entrancy crash during model download.

The venv created by `service-base` at `/var/venv` is reused. Because
`service-base` strips `pip` from the venv, it is restored via
`python -m ensurepip` before installing, then stripped again after.

`requirements.txt` and `download_docling_models.py` are picked up by the existing
`COPY digitize/ .` step (the build context is `services/`), so no extra `COPY`
lines are required.

```
services/digitize/
├── Containerfile          # FROM icr.io/ai-services-cicd/service-base:v0.0.46
├── Makefile               # TAG=v0.0.81
├── requirements.txt       # digitize-only deps (docling/ML stack + existing)
├── download_docling_models.py
└── ...
```

---

## Files Changed

| File | Change |
|---|---|
| `images/service-base/Containerfile` | Removed model download step; removed docling OS packages (`lcms2-devel`, `openblas-devel`, `freetype`, `libicu`, `libjpeg-turbo`, `spatialindex-devel`) and the EPEL repository install — only `python3.12-devel` is installed |
| `images/service-base/requirements.txt` | Removed ~90 digitize-only packages |
| `images/service-base/Makefile` | Tag bumped to `v0.0.46` |
| `images/service-base/docling/Containerfile` | **Deleted** (patch-based workaround removed) |
| `images/service-base/docling/docling_v2.60.1.patch` | **Deleted** |
| `images/service-base/docling/0001-Exclude-source-…patch` | **Deleted** |
| `services/digitize/Containerfile` | Installs docling OS packages (+ EPEL), installs docling/ML deps, sets `DOCLING_MODELS_PATH` / `HF_HUB_DISABLE_XET`, downloads docling models; base bumped to `service-base:v0.0.46` |
| `services/digitize/requirements.txt` | Added digitize-only ML / document-processing deps |
| `services/digitize/download_docling_models.py` | **New** (moved from `service-base`) |

---

## Image Dependency Graph

```
registry.access.redhat.com/ubi9/ubi-minimal:9.8
        │
        ▼
  service-base:v0.0.46          ← used by: chatbot, digitize, extract,
        │                                   similarity, summarize,
        │                                   translate
        ▼
  digitize-service:v0.0.81      ← adds docling OS pkgs, ML stack + models
```

---

## Alternatives Considered

### Intermediate `digitize-base` image (rejected)

An earlier iteration of this proposal introduced `images/digitize-base`
(`v0.0.1`, ~4.01 GB) extending `service-base` with the digitize-only OS
packages, Python dependencies and docling models, with `digitize-service`
building on top of it.

It was dropped in favour of building these directly in `services/digitize`:

- **Fewer images to maintain** — one less image to version, build, publish,
  scan and keep in sync with `service-base`.
- **Simpler release flow** — a digitize dependency change no longer requires
  bumping and publishing `digitize-base` before rebuilding `digitize-service`.
- **Trade-off: longer `digitize-service` build** — the heavy ML python pkgs install and model
  download now run as part of the `digitize-service` build instead of being
  cached in a separate base image.

---

## Package Usage by Service

The table below shows which third-party packages are directly imported across
all services (excluding `digitize`), and which service each package is needed by.

| Import Name | PyPI Package Name | chatbot | common | extract | similarity | summarize | translate |
|---|---|:---:|:---:|:---:|:---:|:---:|:---:|
| `cohere` | `cohere` | | ✓ | | | | |
| `fastapi` | `fastapi` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `httpx` | `httpx` | | | | | | ✓ |
| `jsonschema` | `jsonschema` | | | ✓ | | | |
| `lingua` | `lingua-language-detector` | ✓ | ✓ | | | | ✓ |
| `numpy` | `numpy` | | ✓ | | | | |
| `opensearchpy` | `opensearch-py` | | ✓ | | | | |
| `paramiko` | `paramiko` | | ✓ | | | | |
| `psutil` | `psutil` | | ✓ | | | | |
| `psycopg2` | `psycopg2-binary` | | ✓ | | | | |
| `pydantic` | `pydantic` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `pydantic_settings` | `pydantic-settings` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| `pypdfium2` | `pypdfium2` | | | | | ✓ | |
| `requests` | `requests` | ✓ | ✓ | ✓ | | | |
| `sentence_splitter` | `sentence-splitter` | | ✓ | | | ✓ | ✓ |
| `spacy` | `spacy` | | ✓ | | | | |
| `sqlalchemy` | `SQLAlchemy` | | ✓ | ✓ | | ✓ | ✓ |
| `starlette` | `starlette` | ✓ | | | | ✓ | |
| `tqdm` | `tqdm` | | ✓ | | | | |
| `uvicorn` | `uvicorn` | | | ✓ | ✓ | ✓ | ✓ |

**Note:**

- `common` is the heaviest consumer (14 packages) as it is the shared utility layer imported by all other services.
- `fastapi`, `pydantic`, and `pydantic-settings` are the only packages used by every service.
- Packages with a single-service (`httpx`, `jsonschema`, `pypdfium2`) are candidates for service-level requirements files rather than the shared base image if the base image ever needs further trimming.

---

## Benefits

- **Smaller `service-base`** — ~4 GB down to ~1.9 GB, so the 5 non-digitize
  services no longer carry PyTorch, docling, OpenCV, ML model weights, or the
  docling-only OS libraries (and the EPEL repository).
- **Cleaner SBOM / CVE surface** — digitize-specific packages (torch,
  transformers, opencv, …) no longer appear in chatbot/extract/similarity/
  summarize/translate image scans.
- **No extra base image** — the ML stack lives with the only service that uses
  it, avoiding a separate `digitize-base` image lifecycle.
- **Removed patch workaround** — the `docling/` sub-image with commercial
  licence exclusion patches is deleted; docling is now consumed directly as a
  pinned release in `digitize-service`.
