# Split `service-base` Image Proposal

## Summary

The `service-base` container image previously bundled every Python dependency
required by **all** services — including the heavy ML/document-processing stack
needed only by `digitize` (PyTorch, docling, OpenCV, transformers, etc.). This
caused every service image (chatbot, extract, similarity, summarize, translate)
to inherit and ship an unnecessarily large image layer.

This proposal describes the split into two purpose-built base images:

| Image | Registry tag | Size | Purpose |
|---|---|---|---|
| `service-base` | `v0.0.46` | 1.65 GB | Shared base for all non-digitize services |
| `digitize-base` | `v0.0.1` | 4.01 GB | Extends `service-base`; adds digitize-only dependencies |

---

## Problem

The original `service-base` `Containerfile`:

- Installed system packages needed only by digitize (`lcms2-devel`,
  `openblas-devel`, `freetype`, `libicu`, `libjpeg-turbo`, `spatialindex-devel`)
  into every service image.
- Downloaded docling ML models (`ds4sd--docling-models` — Layout & TableFormer)
  at image build time for every service, regardless of whether the service used
  them.
- Carried ~90 Python packages (docling, torch, torchvision, transformers,
  OpenCV, pandas, scipy, …) in the shared layer consumed by chatbot, extract,
  similarity, summarize, and translate.
- Removed the `docling/` sub-image workaround (patch-based Containerfile +
  `0001-Exclude-source-that-has-commercial-license_v0.22.1.patch` +
  `docling_v2.60.1.patch`) which is no longer needed.

---

## Solution

### `images/service-base`

Lean base image consumed by all services. Contains only:

- **Runtime:** `fastapi`, `httpx`, `pydantic`, `pydantic-settings`, `requests`,
  `starlette`, `uvicorn`, `uvloop`, `python-multipart`, `psutil`
- **Database:** `psycopg2`, `SQLAlchemy`
- **Search / Embeddings:** `cohere`, `numpy`, `opensearch-py`, `tqdm`
- **Language:** `lingua-language-detector`, `sentence-splitter`, `spacy` +
  Japanese model (`ja_core_news_sm`), `SudachiDict-core`, `SudachiPy`
- **Documents:** `pypdfium2`
- **Validation:** `jsonschema`
- **SFTP:** `paramiko`
- Pinned transitive dependencies (72 packages)

System packages installed: **Python 3.12 only** (`python3.12-devel`).

The `prebuilder` stage supplies pre-compiled `.whl` files (for packages that
require Rust/C compilation on ppc64le) via a bind-mount at build time. `pip` is
stripped from the venv after installation so it does not appear in the runtime
image or SBOM scanners.

```
images/service-base/
├── Containerfile
├── Makefile               # TAG=v0.0.46
├── requirements.txt
├── download_docling_models.py   # kept for reference; not executed here
└── prebuilder/
    ├── Containerfile
    └── pre-requirements.txt
```

### `images/digitize-base`

Extends `service-base:v0.0.46`. Adds only the dependencies exclusive to the
`digitize` service:

- **System packages:** `lcms2-devel`, `openblas-devel`, `freetype`, `libicu`,
  `libjpeg-turbo`, `spatialindex-devel` (via EPEL)
- **ML / document processing:** `docling`, `docling-core`, `docling-ibm-models`,
  `docling-parse`, `docling-slim`, `torch`, `torchvision`, `transformers`,
  `accelerate`, `safetensors`, `opencv-python`, `pillow`, `scipy`, `pandas`, …
- **Docling models:** downloaded at build time via `download_docling_models.py`
  (Layout + TableFormer; OCR/EasyOCR/code-formula/picture-classifier excluded)
  into `DOCLING_MODELS_PATH=/var/docling-models`

`hf-xet` is disabled at build time (`HF_HUB_DISABLE_XET=1`) to prevent a fatal
GIL/Tokio runtime re-entrancy crash during model download.

The venv created by `service-base` at `/var/venv` is reused. Because
`service-base` strips `pip` from the venv, it is restored via
`python -m ensurepip` before installing, then stripped again after.

```
images/digitize-base/
├── Containerfile          # FROM icr.io/ai-services-cicd/service-base:v0.0.46
├── Makefile               # TAG=v0.0.1
├── requirements.txt
└── download_docling_models.py
```

---

## Files Changed

| File | Change |
|---|---|
| `images/service-base/Containerfile` | Removed digitize system deps, EPEL install, model download step |
| `images/service-base/requirements.txt` | Removed ~90 digitize-only packages |
| `images/service-base/Makefile` | Minor update |
| `images/service-base/docling/Containerfile` | **Deleted** (patch-based workaround removed) |
| `images/service-base/docling/docling_v2.60.1.patch` | **Deleted** |
| `images/service-base/docling/0001-Exclude-source-…patch` | **Deleted** |
| `images/digitize-base/Containerfile` | **New** |
| `images/digitize-base/Makefile` | **New** |
| `images/digitize-base/requirements.txt` | **New** (digitize-only deps, no overlap with service-base) |
| `images/digitize-base/download_docling_models.py` | **New** (moved from service-base) |

---

## Image Dependency Graph

```
registry.access.redhat.com/ubi9/ubi-minimal:9.8
        │
        ▼
  service-base:v0.0.46          ← used by: chatbot, extract,
        │                                   similarity, summarize,
        │                                   translate
        ▼
  digitize-base:v0.0.1          ← used by: digitize
```

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

- **Smaller images** for the 5 non-digitize services — they no longer carry
  PyTorch, docling, OpenCV, or ML model weights.
- **Faster builds** for non-digitize services — the heavy ML stack and model
  download are isolated to `digitize-base`.
- **Cleaner SBOM / CVE surface** — digitize-specific packages (torch,
  transformers, opencv, …) no longer appear in chatbot/extract/similarity/
  summarize/translate image scans.
- **Removed patch workaround** — the `docling/` sub-image with commercial
  licence exclusion patches is deleted; docling is now consumed directly as a
  pinned release in `digitize-base`.
