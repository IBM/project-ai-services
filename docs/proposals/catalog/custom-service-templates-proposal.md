# Custom Service Templates for Customer Asset Onboarding

**Version:** 1.0
**Date:** Aug 2026
**Status:** Draft

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Background and Motivation](#2-background-and-motivation)
3. [Catalog Pod Architecture](#3-catalog-pod-architecture)
4. [New Asset Structure](#4-new-asset-structure)
5. [Template Provider Design](#5-template-provider-design)
6. [CatalogProvider Integration](#6-catalogprovider-integration)
7. [API Upload](#7-api-upload)
8. [Custom Template Directory Structure](#8-custom-template-directory-structure)
9. [Remote Deployment](#9-remote-deployment)
10. [Template Values Reference](#10-template-values-reference)
    - 10.1 [Shared (services and components)](#101-shared-services-and-components)
    - 10.2 [Services](#102-services)
    - 10.3 [Components](#103-components)
11. [Usage Examples](#11-usage-examples)
12. [Backward Compatibility](#12-backward-compatibility)
13. [Future Enhancements](#13-future-enhancements)

---

## 1. Executive Summary

Enterprise customers deploying AI Services on their own infrastructure often bring proprietary workloads, domain-specific models, and internal service patterns that are not represented in the platform's built-in catalog. Today there is no supported path for customers to introduce their own services into a running deployment without modifying the platform binary itself — a process that is impractical at scale and incompatible with air-gapped or regulated environments.

This proposal introduces **Custom Service Templates** — a first-class mechanism for customers to onboard their own AI service assets into the catalog at runtime. A customer packages their service definition as a `.tar.gz` bundle and uploads it to the running catalog backend over HTTPS. The platform validates, registers, and hot-reloads the new service immediately — with no pod restart, no host filesystem access, and no changes to the platform binary required. The mechanism is identical on Podman single-VM deployments and OpenShift clusters.

Built-in platform services are protected: a bundle whose `catalog_id` conflicts with an embedded service is rejected at validation time, ensuring the integrity of the core catalog is never compromised.

| Property | Detail |
|---|---|
| **Use case** | Onboard customer-authored service assets into a live catalog deployment |
| **Delivery** | `POST /api/v1/catalog/bundles` — `.tar.gz` archive uploaded over HTTPS to the running catalog |
| **Podman** | ✅ — bundle stored in dedicated named volume `ai-services-bundles` |
| **OpenShift** | ✅ — bundle stored in dedicated PVC `catalog-bundles-pvc` |
| **Live reload** | Automatic — `CatalogProvider` hot-reloads after successful extraction, no pod restart |
| **Audit trail** | `catalog_bundles` table in PostgreSQL — every upload is recorded with uploader identity and timestamp |
| **Best for** | Enterprise customers, air-gapped deployments, regulated environments, CI/CD-driven asset promotion |

---

## 2. Background and Motivation

### 2.1 Current Asset Architecture

The catalog uses a **service-oriented** decomposition. All platform assets are compiled into the binary at build time via `go:embed`, living under three roots in `assets.CatalogFS` (declared in [`ai-services/assets/fs.go`](ai-services/assets/fs.go:11)):

| Root | Purpose |
|---|---|
| `ai-services/assets/architectures/` | Architecture metadata (e.g. `rag`) declaring which services compose it |
| `ai-services/assets/services/` | Per-service assets: `chat`, `digitize`, `similarity`, `summarize` |
| `ai-services/assets/components/` | Reusable component providers: `llm`, `embedding`, `vector_store`, `reranker` |

All loading flows through [`CatalogProvider`](ai-services/internal/pkg/catalog/catalog.go:32), which walks `CatalogFS` at startup and caches every `metadata.yaml` it finds. Deployment is driven by the catalog **apiserver** running inside a pod — not by the CLI host process. Because the catalog is embedded in the binary, adding a new service today requires a full platform build and redeployment.

### 2.2 Problem Statement

Enterprise customers operate AI Services in environments where the built-in service catalog does not fully represent their workloads. Key pain points include:

- **Proprietary services** — customers have internal AI workloads (custom RAG pipelines, domain-specific inference services, internal tooling) that need to be deployable through the same catalog-driven workflow as built-in services.
- **Operational constraints** — air-gapped environments, regulated industries, and on-premises VM deployments make it impractical to request a platform rebuild each time a customer needs to register a new service.
- **CI/CD asset promotion** — teams need to promote service definitions through staging and production deployments programmatically, without manual intervention on each host.
- **Partner and ISV onboarding** — system integrators and technology partners need a supported path to register their own service assets alongside IBM-certified ones.

Currently, there is no supported mechanism to extend the catalog at runtime. Custom assets are delivered by uploading a `.tar.gz` bundle to the running catalog API over HTTPS. The apiserver extracts it to isolated storage, validates it, and hot-reloads `CatalogProvider` — no pod restart, no CLI host access required, and no changes to the platform binary needed.

### 2.3 Goals

1. Provide a secure, authenticated API endpoint (`POST /api/v1/catalog/bundles`) through which customers can register new service assets into a live deployment without platform downtime.
2. At apiserver startup, compose customer-uploaded bundles with the embedded `CatalogFS` via a `CompositeCatalogFS`, presenting a unified catalog that includes both platform and customer services.
3. Protect the integrity of built-in platform services — bundles that attempt to use a reserved `catalog_id` are rejected; the embedded catalog is immutable at runtime.
4. Maintain full backward compatibility — in the absence of any uploaded bundles, behaviour is identical to the current release.

---

## 3. Catalog Pod Architecture

Understanding how the catalog runs as pods is essential context for where custom templates plug in.

### 3.1 Catalog pod topology

```mermaid
flowchart TD
    subgraph HOST["VM Host"]
        DIR_BASE["$AI_SERVICES_BASE_DIR<br/>common/caddy · caddy-config · models<br/>hostPath volume"]
        SOCK["/run/podman/podman.sock<br/>hostPath socket"]
        IOMMU["/sys/kernel/iommu_groups<br/>hostPath read-only"]
    end

    BUNDLE_VOL["Podman named volume<br/>ai-services-bundles<br/>mount: /data/catalog-bundles"]

    subgraph PODS["Podman pods — shared pod network"]
        subgraph CADDY_POD["ai-services--caddy pod"]
            C["container: caddy<br/>:443 HTTPS external<br/>:2019 admin API host-only"]
        end

        subgraph DB_POD["ai-services--db pod"]
            PG["container: postgresql<br/>:5432"]
        end

        subgraph CAT_POD["ai-services--catalog pod"]
            INIT["initContainer: db-migration<br/>gates UI and backend startup"]
            UI["container: ui :8081"]
            BE["container: backend :8080 apiserver<br/>mount: /data/catalog-bundles"]
        end
    end

    EXT["External client"]

    DIR_BASE    -- "volume mount" --> C
    DIR_BASE    -- "volume mount" --> BE
    SOCK        -- "volume mount" --> BE
    IOMMU       -- "volume mount ro" --> BE
    BUNDLE_VOL  -- "named volume → /data/catalog-bundles" --> BE

    INIT -- "gates" --> UI
    INIT -- "gates" --> BE
    BE -- "SQL :5432" --> PG
    BE -- "Admin API :2019" --> C

    EXT -- "HTTPS :443" --> C
    C   -- "catalog-ui route" --> UI
    C   -- "catalog-api route" --> BE
```

### 3.2 Why this matters for custom templates

The catalog backend's `CatalogProvider` runs **inside the `ai-services--catalog` container**, not on the CLI host. `assets.CatalogFS` is baked into the binary at build time (via `go:embed`). For custom templates to be visible at runtime they must reach the container and be overlaid onto the embedded FS.

Custom assets are delivered via the running catalog API: the client POSTs a `.tar.gz` bundle over HTTPS, and the apiserver writes the extracted contents to a dedicated named volume (`ai-services-bundles` on Podman, `catalog-bundles-pvc` on OpenShift) that it already owns. Both runtimes mount the volume at the well-known path `/data/catalog-bundles` inside the container. Bundles are stored under `<catalog_type>/<name>/` where `name = <catalog_id>-<version>` (e.g. `service/chat-2.0.0/`). At startup, `CatalogProvider` queries the DB for all `status = 'active'` rows, resolves each to its named directory, and builds a `CompositeCatalogFS`. Hot-reload happens in-process after every successful upload; no pod restart is needed.

### 3.3 OpenShift path

For OpenShift, `catalog configure` runs [`openshift.DeployCatalog`](ai-services/internal/pkg/catalog/cli/configure/openshift/configure.go:24), which uses Helm to install/upgrade the catalog chart from `assets/catalog/openshift/`. No chart change is required for bundle support: once the catalog is deployed, users POST bundles to the Route-exposed API endpoint. The backend writes to the `catalog-bundles-pvc` PVC it already mounts (see §7.6).

---

## 4. New Asset Structure

### 4.1 Built-in service assets (existing)

Each service under `ai-services/assets/services/` has the following layout, shown for `chat`:

```
ai-services/assets/services/chat/
├── metadata.yaml              # Service identity & dependencies (id: chat)
├── podman/
│   ├── metadata.yaml          # Runtime metadata: version, resources, podTemplateExecutions
│   ├── values.yaml            # Default parameter values
│   ├── values.schema.json     # JSON Schema for parameter validation
│   ├── templates/
│   │   └── chat-bot.yaml.tmpl # Pod spec template
│   └── steps/
│       ├── info.md
│       └── vars_file.yaml
└── openshift/
    ├── Chart.yaml
    ├── metadata.yaml
    ├── values.yaml
    └── templates/
        ├── chat-bot-backend-deployment.yaml
        └── ...
```

Key fields from [`assets/services/chat/metadata.yaml`](ai-services/assets/services/chat/metadata.yaml:1):

```yaml
id: chat
name: "Question and answer"
type: service
certified_by: "IBM"
architectures:
  - rag
dependencies:
  - id: vector_store
  - id: embedding
  - id: llm
standalone: false
```

### 4.2 Architecture assets (existing)

`assets/architectures/rag/metadata.yaml` declares which services compose the `rag` architecture and which components are shared globally across all services:

```yaml
id: rag
name: "Digital Assistant"
type: architecture
global_components:
  - type: vector_store
  - type: embedding
services:
  - id: chat
  - id: digitize
  - id: similarity
```

### 4.3 Custom service assets (proposed)

A user-supplied bundle mirrors the same `services/` root. Only the entries present in the bundle are overlaid; everything else falls through to the embedded assets.

```
my-bundle.tar.gz
└── services/
    └── my-service/
        ├── metadata.yaml           # required
        └── podman/
            ├── metadata.yaml       # required (version, resources, podTemplateExecutions)
            ├── values.yaml         # required
            ├── values.schema.json  # optional
            └── templates/
                └── my-service.yaml.tmpl
```

Only `services/` is a valid top-level directory in a bundle; any other roots are silently skipped (forward-compatible for future `components/` support). The `catalog_id` inside the bundle must **not** match any built-in service — if it does, validation rejects the bundle with a `422` error.

---

## 5. Template Provider Design

### 5.1 Provider hierarchy

```mermaid
classDiagram
    class fs_ReadDirFS {
        <<interface>>
        +ReadDir(name string) []DirEntry
    }

    class CatalogFS {
        <<interface>>
        +Open(name string) File
        +ReadFile(name string) []byte
    }

    class EmbeddedCatalogFS {
        -fs embed.FS
        +Open() File
        +ReadFile() []byte
        +ReadDir() []DirEntry
    }

    class FilesystemCatalogFS {
        -root string
        +Open() File
        +ReadFile() []byte
        +ReadDir() []DirEntry
    }

    class CompositeCatalogFS {
        -sources []CatalogFS
        +Open() File
        +ReadFile() []byte
        +ReadDir() []DirEntry
    }

    fs_ReadDirFS <|-- CatalogFS : embeds
    CatalogFS <|.. EmbeddedCatalogFS : implements
    CatalogFS <|.. FilesystemCatalogFS : implements
    CatalogFS <|.. CompositeCatalogFS : implements
    CompositeCatalogFS o-- EmbeddedCatalogFS : fallback
    CompositeCatalogFS o-- FilesystemCatalogFS : priority
```

### 5.2 Interface

```go
// CatalogFS abstracts the filesystem used by CatalogProvider.
type CatalogFS interface {
    fs.ReadDirFS
    Open(name string) (fs.File, error)
    ReadFile(name string) ([]byte, error)
}
```

### 5.3 FilesystemCatalogFS

```go
// FilesystemCatalogFS reads catalog assets from a local directory.
// Inside the container this is the active bundle directory written by BundleService.
type FilesystemCatalogFS struct {
    root string // e.g. "/data/catalog-bundles/service/chat-2.0.0"
}
```

Validates at construction that `services/` exists under `root`, providing an actionable early error. Entries other than `services/` are ignored.

### 5.4 CompositeCatalogFS

```go
// CompositeCatalogFS merges multiple CatalogFS instances.
// Lookup checks each source in order; the first hit wins.
// WalkDir visits all sources and deduplicates paths.
type CompositeCatalogFS struct {
    sources []CatalogFS // [bundleFS, embeddedFS]
}
```

When `WalkDir` encounters the same relative path (e.g. `services/chat/metadata.yaml`) in both sources, the first source (bundle) wins and the embedded version is silently skipped.

### 5.5 Factory function

At startup the apiserver builds one `FilesystemCatalogFS` per active bundle (see §6.3). The factory below is the helper used when there is exactly one bundle path to overlay — for example in tests or single-bundle tooling:

```go
// NewCatalogFS returns the CatalogFS to use.
// bundlePath="" → returns the embedded FS only (no active bundle).
// bundlePath set → returns a composite that overlays that single bundle directory.
// For multiple active bundles use NewCompositeCatalogFS directly (see §6.3).
func NewCatalogFS(bundlePath string) (CatalogFS, error) {
    embedded := &EmbeddedCatalogFS{fs: &assets.CatalogFS}
    if bundlePath == "" {
        return embedded, nil
    }
    bundle, err := NewFilesystemCatalogFS(bundlePath)
    if err != nil {
        logger.Warningf("bundle path '%s' invalid, using built-in only: %v", bundlePath, err)
        return embedded, nil
    }
    return NewCompositeCatalogFS(bundle, embedded), nil
}
```

### 5.6 Resolution priority

At startup, `CatalogProvider` reads active bundle versions from the DB and constructs one `FilesystemCatalogFS` per active item, all layered before the embedded FS:

| Priority | Source | Condition |
|---|---|---|
| 1..N | `FilesystemCatalogFS` (one per active bundle) | DB has `status = 'active'` rows; paths are `/data/catalog-bundles/<catalog_type>/<name>/` |
| N+1 | `EmbeddedCatalogFS` (built-in) | Always present as fallback |

---

## 6. CatalogProvider Integration

### 6.1 Current loading (single embedded FS)

[`loadCatalogItems`](ai-services/internal/pkg/catalog/catalog.go:56) today walks `assets.CatalogFS` directly, dispatching on the first path segment (`"architectures"`, `"services"`, `"components"`) and storing results in `sharedItems`:

```go
err := fs.WalkDir(&assets.CatalogFS, ".", func(path string, d fs.DirEntry, err error) error {
    return processMetadataFile(ctx, path, items)
})
```

### 6.2 Proposed: inject CatalogFS

`NewCatalogProvider` gains an optional functional option:

```go
func NewCatalogProvider(opts ...Option) (*CatalogProvider, error)

// WithBundlePath overlays the active bundle directory on top of the embedded catalog.
func WithBundlePath(dir string) Option
```

When provided, `loadCatalogItems` receives the `CompositeCatalogFS` instead of `&assets.CatalogFS`. The `processMetadataFile`, `parseService`, `parseArchitecture`, `parseComponent` functions are unchanged — they work against any `CatalogFS`.

### 6.3 Apiserver startup loads active bundle paths from the DB

The bundle volume is always mounted at `/data/catalog-bundles`. No env var is needed. At startup, the apiserver queries the `catalog_bundles` table for all `status = 'active'` rows, resolves each one to its versioned directory on disk, and builds a `CompositeCatalogFS` with one `FilesystemCatalogFS` per active item:

```go
// catalogBundlesDir is the well-known container mount path for the bundles volume.
// It is fixed by the pod spec (Podman named volume / OpenShift PVC).
const catalogBundlesDir = "/data/catalog-bundles"

// In the apiserver main/start path:
activeBundles, err := bundleRepo.ListActive(ctx) // SELECT WHERE status='active'
var fsList []CatalogFS
for _, b := range activeBundles {
    // path: /data/catalog-bundles/<catalog_type>/<name>/
    // name is derived server-side as <catalog_id>-<version>
    p := filepath.Join(catalogBundlesDir, b.CatalogType, b.Name)
    if fs, err := NewFilesystemCatalogFS(p); err == nil {
        fsList = append(fsList, fs)
    }
}
fsList = append(fsList, &EmbeddedCatalogFS{fs: &assets.CatalogFS})
provider, err := catalog.NewCatalogProvider(catalog.WithCompositeCatalogFS(fsList...))
```

If there are no active bundles, only `EmbeddedCatalogFS` is used — behaviour is identical to today.

---

## 7. API Upload

Custom catalog assets are delivered by uploading a `.tar.gz` bundle to the running catalog backend over its existing HTTPS endpoint. A bundle is a generic container — it can carry any mix of catalog root types (`services/`, `components/`, and others in future). The archive is extracted, validated per root type, and hot-reloaded into `CatalogProvider` — with no pod restart required for either Podman or OpenShift.

> **Scope for this release:** only `services/` is processed. `components/` and other roots are accepted in the archive but skipped by the dispatcher — they will be activated in a future release without any change to the bundle format or API contract.

### 7.1 Design goals

| Goal | Detail |
|---|---|
| No restart required | `CatalogProvider` reloads the custom layer in-process after a successful upload |
| Idempotent | Re-uploading the same `catalog_id` + `version` replaces the existing directory in-place |
| Independent bundles | Each uploaded bundle exists separately; multiple bundles for different `catalog_id` values are all active simultaneously |
| Authenticated | Uses the existing JWT `BearerAuth` middleware; only admin-role tokens are accepted |
| Consistent across runtimes | Same API endpoint works for Podman and OpenShift; only the storage backend differs |
| Bounded size | Configurable `MAX_BUNDLE_SIZE` (default 50 MB); enforced at the HTTP layer before extraction |

---

### 7.2 New API endpoints

Three endpoints are added to the existing router in [`apiserver/router.go`](ai-services/internal/pkg/catalog/apiserver/router.go:18) under the authenticated `catalog` group:

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/catalog/bundles` | Upload a new bundle (`.tar.gz`). Each bundle is registered independently and activated on success. |
| `GET` | `/api/v1/catalog/bundles` | List all uploaded bundles (id, status, uploaded_at, size). |
| `GET` | `/api/v1/catalog/bundles/:id` | Get the status of a specific bundle by ID. Used to poll after a `202` upload response. |

#### 7.2.1 Upload bundle — `POST /api/v1/catalog/bundles`

The request uses `multipart/form-data` so that the binary `.tar.gz` file and the text fields can travel together in the same request. The server validates `catalog_type` and `version` before touching the archive, then extracts directly into the versioned directory on the bundle volume.

```
POST /api/v1/catalog/bundles
Content-Type: multipart/form-data
Authorization: Bearer <admin-jwt>

Form fields:
  file         (required)  — .tar.gz archive containing the catalog item
                             assets; max 50 MB compressed
  catalog_type (required)  — type of the catalog item in this bundle;
                             accepted values: "service", "component"
  version      (required)  — semantic version of this bundle, e.g. "1.0.0";
                             used as the directory name under
                             /data/catalog-bundles/<type>/<name>/
                             where name = <catalog_id>-<version>
  dry_run      (optional)  — "true" validates the archive without activating it
```

**Example (curl):**
```bash
curl -X POST https://catalog-api.<domain>/api/v1/catalog/bundles \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@my-bundle.tar.gz" \
  -F "catalog_type=service" \
  -F "version=1.0.0"
```

**Responses:**

| Status | Meaning |
|---|---|
| `202 Accepted` | Bundle accepted; extraction and validation running asynchronously. Use the `Location` header to poll for status. |
| `400 Bad Request` | Missing `file`, `catalog_type`, or `version` field; unrecognised `catalog_type`; invalid version format; wrong content-type; or archive exceeds size limit. |
| `401 Unauthorized` | Missing or invalid JWT. |
| `403 Forbidden` | Token does not carry admin role. |
| `422 Unprocessable Entity` | Archive extracted but validation failed (structured error list returned). |

The response includes a `Location` header pointing to the status resource:

```
HTTP/1.1 202 Accepted
Location: /api/v1/catalog/bundles/bnd_01JW4X9K2M8VQRP3T5YZ
```

`catalog_type` is known immediately from the request field. `catalog_id` is resolved from the directory name inside the archive during extraction and populated once the archive is unpacked. Both are present in the `202` body:

```json
// 202 response body
{
  "id":           "bnd_01JW4X9K2M8VQRP3T5YZ",
  "name":         "my-service-1.0.0",
  "status":       "processing",
  "uploaded_at":  "2026-05-12T09:14:02Z",
  "size_bytes":   143360,
  "catalog_type": "service",
  "catalog_id":   "my-service",
  "version":      "1.0.0",
  "uploaded_by":  "admin"
}
```

Once the bundle reaches `active` or `failed` status, the polled response reflects the final outcome:

```json
// after activation — GET /api/v1/catalog/bundles/bnd_01JW4X9K2M8VQRP3T5YZ
{
  "id":           "bnd_01JW4X9K2M8VQRP3T5YZ",
  "name":         "my-service-1.0.0",
  "status":       "active",
  "uploaded_at":  "2026-05-12T09:14:02Z",
  "size_bytes":   143360,
  "catalog_type": "service",
  "catalog_id":   "my-service",
  "version":      "1.0.0",
  "uploaded_by":  "admin"
}
```

#### 7.2.2 List bundles — `GET /api/v1/catalog/bundles`

Each bundle record carries `catalog_type` (the type declared by the uploader — `"service"` or `"component"`) and `catalog_id` (the item id within that type). Multiple bundles for different catalog items are all active simultaneously and each is listed independently.

```json
{
  "bundles": [
    {
      "id":           "bnd_01JW4X9K2M8VQRP3T5YZ",
      "status":       "active",
      "uploaded_at":  "2026-05-12T09:14:02Z",
      "size_bytes":   143360,
      "name":         "my-service-1.0.0",
      "catalog_type": "service",
      "catalog_id":   "my-service",
      "version":      "1.0.0",
      "uploaded_by":  "admin"
    },
    {
      "id":           "bnd_02CD8Y3NF1P9WQS4U6VA",
      "name":         "my-llm-provider-1.0.0",
      "status":       "active",
      "uploaded_at":  "2026-05-13T11:30:00Z",
      "size_bytes":   98304,
      "catalog_type": "component",
      "catalog_id":   "my-llm-provider",
      "version":      "1.0.0",
      "uploaded_by":  "admin"
    }
  ]
}
```

---

### 7.3 Bundle format

A bundle is scoped to **one catalog item** — the type is declared by the client as a form field (`catalog_type`), and the `catalog_id` is derived from the top-level directory inside the archive. The archive must be a gzip-compressed tar (`.tar.gz`). The `version` form field combines with the `catalog_id` to form the unique on-disk directory name (`<catalog_id>-<version>`), ensuring each bundle has an isolated location on the volume.

```
my-bundle.tar.gz
└── my-service/                     ← single item; directory name = catalog_id
    ├── metadata.yaml
    └── podman/
        ├── metadata.yaml
        ├── values.yaml
        └── templates/
            └── my-service.yaml.tmpl
```

The server derives `catalog_id` from this top-level directory name and validates it matches the `catalog_type` form field. The `version` form field (e.g. `"1.0.0"`) determines where the extracted contents are stored on the volume.

**Rules:**
- Paths containing `..` or absolute paths are rejected immediately (path-traversal guard, same principle as [`SanitizeFilePath`](ai-services/internal/pkg/catalog/utils/common.go:90)).
- The archive must contain exactly one top-level directory; multiple items per archive are not supported.
- Total uncompressed size must not exceed `MAX_BUNDLE_SIZE_UNCOMPRESSED` (default 200 MB).
- The `catalog_id` derived from the top-level directory name must not match any built-in service already present in `assets.CatalogFS` — if it does, validation returns `422` and the extracted directory is deleted.
- All `metadata.yaml` files must pass validation for the declared `catalog_type` before the bundle is marked `active`.

---

### 7.4 Server-side processing pipeline

```mermaid
flowchart TD
    REQ["POST /api/v1/catalog/bundles<br/>multipart/form-data<br/>file, catalog_type, version, dry_run"]
    AUTH["AuthMiddleware<br/>JWT + admin role check"]
    TYPECHECK["Validate catalog_type and version<br/>reject unknown type → 400"]
    RESP["202 Accepted immediately<br/>bundle_id, status: processing<br/>Location: /api/v1/catalog/bundles/:id"]
    SIZE["Size guard<br/>max 50 MB compressed"]
    EXTRACT["Extract to<br/>/data/catalog-bundles/type/name/<br/>name = catalog_id-version"]
    PATHGUARD["Path-traversal guard<br/>reject .. and absolute paths<br/>verify exactly one top-level dir"]
    VALIDATE["CatalogProvider.ValidateFS<br/>parse metadata for declared catalog_type<br/>collect all errors"]

    subgraph ASYNC["Goroutine — async after 202"]
        SIZE --> EXTRACT --> PATHGUARD --> VALIDATE

        subgraph ACTIVATE["Activate — success path"]
            direction LR
            DBBUNDLE["Insert bundle record<br/>status = active"]
            RELOAD["CatalogProvider.Reload()<br/>re-query active bundles from DB<br/>rebuild CompositeCatalogFS"]
            DBBUNDLE --> RELOAD
        end

        FAIL["Delete type/name/ directory<br/>update DB status = failed<br/>return 422 on poll"]

        VALIDATE -->|"valid"| ACTIVATE
        VALIDATE -->|"invalid"| FAIL
    end

    REQ --> AUTH --> TYPECHECK --> RESP
    RESP -.-> ASYNC
```

**Key implementation notes:**

- Extraction and validation run in a goroutine; the HTTP handler returns `202` immediately. The client polls `GET /api/v1/catalog/bundles/:id` until `status` is `active` or `failed`.
- Each bundle gets its own named directory on the volume (`<type>/<name>/`) — uploading `service/my-service-1.0.0` and `service/chat-1.0.0` are entirely independent; neither touches the other.
- Extraction goes directly into `<type>/<name>/` — no staging directory needed. Since the named directory is new and unique, there is nothing live to corrupt.
- If validation fails, the newly written `<type>/<name>/` directory is deleted. All other active bundles remain unaffected.
- The DB never marks a bundle `active` until validation passes — so even a partial extraction (e.g. process killed mid-way) is safe: `CatalogProvider` will not load a directory that has no `active` DB row.
- `CatalogProvider.Reload()` re-queries the DB for all `status = 'active'` rows and rebuilds the `CompositeCatalogFS` under `sync.RWMutex`.
- Bundle files are stored in a **dedicated named Podman volume** (`ai-services-bundles`) or **separate PVC** (`catalog-bundles-pvc`) — isolated from `$BASE_DIR` so that a `catalog delete --skip-cleanup` affecting application data never touches bundle storage.
- The `catalog_bundles` table is added via a new Goose migration following the same pattern as [`20260430094502_create_applications_table.sql`](ai-services/internal/pkg/catalog/db/migrations/assets/20260430094502_create_applications_table.sql).

---

### 7.5 New database migration

Each row in `catalog_bundles` represents one uploaded bundle. The `name` column is derived server-side as `<catalog_id>-<version>` (e.g. `chat-2.0.0`) — it uniquely identifies a versioned bundle in a human-readable form and is used as the directory name on the volume. The `status` column tracks lifecycle: `processing → active` on success, `failed` on validation error. Multiple bundles for different `catalog_id` values are all `active` simultaneously; each exists independently.

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TYPE bundle_status AS ENUM (
    'processing',
    'active',
    'failed'
);

CREATE TABLE catalog_bundles (
    id               VARCHAR(30)    PRIMARY KEY,
    -- e.g. bnd_01JW4X9K2M8VQRP3T5YZ

    -- Human-readable versioned name: <catalog_id>-<version>, e.g. "chat-2.0.0"
    -- Used as the directory name on the bundle volume.
    name             VARCHAR(200)   NOT NULL,

    status           bundle_status  NOT NULL DEFAULT 'processing',
    size_bytes       BIGINT,

    -- The catalog item type declared by the uploader: "service", "component", …
    catalog_type     VARCHAR(50)    NOT NULL,
    -- The id of the catalog item: e.g. "my-service", "my-llm-provider"
    catalog_id       VARCHAR(100)   NOT NULL,
    -- Semantic version of this bundle: e.g. "1.0.0", "2.1.0"
    version          VARCHAR(50)    NOT NULL,

    error            TEXT,
    uploaded_by      VARCHAR(100),
    uploaded_at      TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    activated_at     TIMESTAMPTZ
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE  IF EXISTS catalog_bundles;
DROP TYPE   IF EXISTS bundle_status;
-- +goose StatementEnd
```

---

### 7.6 Storage per runtime

Bundle storage is **intentionally isolated** from `$AI_SERVICES_BASE_DIR`. This prevents a `catalog delete` or application-data wipe from destroying uploaded bundles, and makes the storage unit independently snapshotable.

#### Volume directory layout

The volume is organised as `<catalog_type>/<name>/` where `name` is `<catalog_id>-<version>` (e.g. `chat-2.0.0`). Each uploaded bundle gets its own named directory. Bundles for different `catalog_id` values coexist on disk and are each independently `active`. Extraction writes directly into the named directory; no staging or rename step is needed.

```
/data/catalog-bundles/
├── service/
│   ├── chat-2.0.0/              ← active
│   │   ├── metadata.yaml
│   │   └── podman/...
│   └── my-service-1.0.0/        ← active (independent)
│       ├── metadata.yaml
│       └── podman/...
└── component/
    └── my-llm-provider-1.0.0/   ← active (independent)
        └── metadata.yaml
```

The `CatalogProvider` resolves each active item's path as:
```
/data/catalog-bundles/<catalog_type>/<name>/
```
where `name = <catalog_id>-<version>`.

#### Podman — named volume `ai-services-bundles`

A dedicated Podman named volume is created by `catalog configure` and mounted into the catalog backend container at `/data/catalog-bundles`. Because Podman named volumes are managed independently of `hostPath` directories, they survive `catalog delete --skip-cleanup` and do not depend on any host filesystem path.

```
Volume name:  ai-services-bundles
Mount point (inside container):  /data/catalog-bundles/
```

**`catalog.yaml.tmpl` addition** (new volume entry alongside the existing `ai-services-data` mount):

```yaml
# new volume declaration
- name: catalog-bundles
  persistentVolumeClaim:
    claimName: "ai-services-bundles"   # Podman named volume, treated as PVC in pod spec
```

```yaml
# new container volumeMount on backend container
- mountPath: /data/catalog-bundles
  name: catalog-bundles
```

#### OpenShift — dedicated PVC `catalog-bundles-pvc`

A separate `PersistentVolumeClaim` is added to the catalog Helm chart (`assets/catalog/openshift/`) rather than reusing the existing `catalog-db` PVC. This keeps bundle lifecycle independent of the database and allows different storage classes (e.g. `ReadWriteMany` for multi-replica deployments in future).

```yaml
# new PVC in catalog Helm chart
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: catalog-bundles-pvc
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi     # tunable via chart values: catalog.bundleStorage
```

```yaml
# volumeMount on catalog-backend Deployment
- mountPath: /data/catalog-bundles
  name: catalog-bundles

# corresponding volume
- name: catalog-bundles
  persistentVolumeClaim:
    claimName: catalog-bundles-pvc
```

**Layout inside the PVC** is identical to the Podman volume layout above, so `BundleService` needs no runtime-specific code paths. Both runtimes mount at `/data/catalog-bundles` — the same well-known constant the apiserver uses.

---

### 7.7 Flow diagrams

#### Podman — upload flow

```mermaid
flowchart TD
    USER["User / CI pipeline<br/>POST /api/v1/catalog/bundles"]

    subgraph CADDY_POD["ai-services--caddy pod"]
        PROXY["caddy reverse proxy<br/>:443 HTTPS"]
    end

    subgraph CAT_POD["ai-services--catalog pod"]
        BE["backend container :8080"]
        HANDLER["BundleHandler.UploadBundle()"]
        PIPELINE["Dispatch → Validate → Swap"]
        CP["CatalogProvider.Reload()"]
    end

    subgraph VOL["Podman named volume — ai-services-bundles"]
        BUNDLES["mount: /data/catalog-bundles<br/>service/name-version/<br/>component/name-version/"]
    end

    PG["ai-services--db<br/>postgresql<br/>catalog_bundles table"]

    USER -- "HTTPS POST multipart" --> PROXY
    PROXY -- "reverse proxy" --> BE
    BE --> HANDLER --> PIPELINE
    PIPELINE -- "extract + write isolated from BASE_DIR" --> BUNDLES
    PIPELINE --> CP
    PIPELINE -- "persist bundle record" --> PG
    CP -- "reads templates from" --> BUNDLES
```

#### OpenShift — upload flow

```mermaid
flowchart TD
    USER["User / CI pipeline<br/>POST /api/v1/catalog/bundles"]

    subgraph OCP["OpenShift cluster"]
        ROUTE["OpenShift Route<br/>TLS termination"]

        subgraph NS["catalog namespace"]
            SVC["catalog-backend Service :8080"]
            DEP["catalog-backend Deployment"]
            BE["backend container"]
            HANDLER["BundleHandler.UploadBundle()"]
            PIPELINE["Dispatch → Validate → Swap"]
            CP["CatalogProvider.Reload()"]
            PVC["PVC: catalog-bundles-pvc<br/>dedicated, separate from catalog-db PVC<br/>mount: /data/catalog-bundles"]
            PG["catalog-db StatefulSet<br/>postgresql :5432"]
        end
    end

    USER -- "HTTPS POST" --> ROUTE
    ROUTE --> SVC --> DEP --> BE
    BE --> HANDLER --> PIPELINE
    PIPELINE -- "extract + write to dedicated PVC" --> PVC
    PIPELINE --> CP
    PIPELINE -- "persist bundle record" --> PG
    CP -- "reads templates from" --> PVC
```

---

### 7.8 Handler skeleton (Go)

This shows how `BundleHandler` fits the existing handler pattern in [`apiserver/handlers/`](ai-services/internal/pkg/catalog/apiserver/handlers):

```go
// BundleHandler handles catalog bundle upload and listing.
// It follows the same pattern as ApplicationHandler and CatalogHandler.
type BundleHandler struct {
    bundleService BundleServiceInterface
}

// UploadBundle godoc
//
//  @Summary     Upload a custom catalog bundle
//  @Description Accepts a .tar.gz archive for a single catalog item. The
//               caller declares the catalog_type ("service" or "component").
//               The archive is validated and, if valid, hot-reloaded into
//               the running CatalogProvider.
//  @Tags        Catalog
//  @Accept      multipart/form-data
//  @Produce     json
//  @Security    BearerAuth
//  @Param       file         formData  file    true   ".tar.gz bundle archive (max 50 MB)"
//  @Param       catalog_type formData  string  true   "Catalog item type: service or component"
//  @Param       version      formData  string  true   "Semantic version of this bundle, e.g. 1.0.0"
//  @Param       dry_run      formData  string  false  "Validate only, do not apply (default: false)"
//  @Success     202  {object}  BundleResponse   "Bundle accepted and processing"
//  @Failure     400  {object}  ErrorResponse    "Invalid request or archive too large"
//  @Failure     401  {object}  ErrorResponse    "Unauthorized"
//  @Failure     403  {object}  ErrorResponse    "Admin role required"
//  @Failure     422  {object}  BundleErrorResponse "Validation failed"
//  @Router      /catalog/bundles [post]
func (h *BundleHandler) UploadBundle(c *gin.Context) {
    // 1. Enforce size limit before reading into memory
    c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBundleSizeBytes)

    // 2. Validate catalog_type before touching the archive
    catalogType := c.PostForm("catalog_type")
    if catalogType == "" {
        c.JSON(http.StatusBadRequest, ErrorResponse{Error: "catalog_type is required"})
        return
    }
    if !isValidCatalogType(catalogType) {
        c.JSON(http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("unsupported catalog_type %q; accepted: service, component", catalogType)})
        return
    }

    version := c.PostForm("version")
    if version == "" {
        c.JSON(http.StatusBadRequest, ErrorResponse{Error: "version is required"})
        return
    }

    file, header, err := c.Request.FormFile("file")
    if err != nil {
        c.JSON(http.StatusBadRequest, ErrorResponse{Error: "missing or unreadable file field"})
        return
    }
    defer file.Close()

    if !strings.HasSuffix(header.Filename, ".tar.gz") {
        c.JSON(http.StatusBadRequest, ErrorResponse{Error: "file must be a .tar.gz archive"})
        return
    }

    dryRun := c.PostForm("dry_run") == "true"
    userID := c.GetString(middleware.CtxUserIDKey)

    resp, err := h.bundleService.ProcessBundle(c.Request.Context(), file, header.Size, userID, catalogType, version, dryRun)
    if err != nil {
        if valErr, ok := err.(*ValidationError); ok {
            c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})
            return
        }
        c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "bundle processing failed"})
        return
    }

    c.JSON(http.StatusAccepted, resp)
}
```

---

## 8. Custom Template Directory Structure

### 8.1 Minimum layout for a new service

```
services/
└── <service-id>/
    ├── metadata.yaml                    # required
    └── podman/                          # for podman runtime
        ├── metadata.yaml                # required (version, resources, podTemplateExecutions)
        ├── values.yaml                  # required
        ├── values.schema.json           # optional – enables param validation
        └── templates/
            └── <service>.yaml.tmpl      # at least one required
```

Package as a `.tar.gz` with `services/` at the top level:

```bash
tar -czf my-bundle.tar.gz services/
```

### 8.2 Service top-level `metadata.yaml`

```yaml
id: my-service                   # unique across built-in + custom services
name: "My Custom Service"
description: "..."
type: service                    # must be "service"
certified_by: "Custom"
architectures:
  - rag                          # reference an existing built-in architecture
dependencies:
  - id: llm                      # component types this service requires
standalone: true
```

### 8.3 Runtime `metadata.yaml` (Podman)

```yaml
name: my-service
version: "1.0.0"
podTemplateExecutions:
  - [dependency-secret.yaml.tmpl] # layer 1 – runs first
  - [my-service.yaml.tmpl]        # layer 2 – runs after layer 1 is ready
resources:
  cpu: 4
  memory: 8589934592              # bytes
  storage: 10737418240            # bytes
```

### 8.4 Built-in service IDs are reserved

A bundle whose top-level directory name matches an existing built-in service (`chat`, `digitize`, `similarity`, `summarize`) will be rejected by the validation step with a `422` error. Custom bundles must use a unique `catalog_id` that does not conflict with any embedded service or architecture.

Built-in IDs reserved at this time: `chat`, `digitize`, `similarity`, `summarize`, `rag`.

---

## 9. Remote Deployment

The control-plane catalog server acts as the authoritative bundle registry. Custom service assets are uploaded once to the control plane and stored there. Remote agents do not need to read assets directly — the control-plane catalog backend orchestrates all template resolution and service rendering on their behalf. No `.tar.gz` retention is required; only the extracted asset files are kept on the control-plane volume.

---

## 10. Template Values Reference

> **Scope: Podman only.**  The template values, `@generate` directives, and `ai-services.io/` labels/annotations described in this section apply to the **Podman** runtime, which uses Go-template `.yaml.tmpl` files.  OpenShift custom service templates use Helm charts and a different rendering pipeline; a full reference for that runtime is deferred.
>
> **TODO:** document the equivalent Helm-based template values, labels, and annotations for the OpenShift runtime.

This section is split into three parts:

- **§9.1 Shared** — context variables and lifecycle labels available in every `.yaml.tmpl` file, regardless of whether it belongs to a service or a component.
- **§9.2 Services** — values, annotations, injected dependency keys, and schema extensions specific to service templates.
- **§9.3 Components** — values, annotations, and env-override patterns specific to component templates.

---

### 9.1 Shared (services and components)

#### 9.1.1 Built-in template context variables

These variables are injected directly by the template engine into every `.yaml.tmpl` file. They are **not** defined in `values.yaml` and cannot be overridden by the user.

| Variable | Type | Description |
|---|---|---|
| `{{ .InstanceSlug }}` | string | Unique slug for the deployed instance (e.g. `chat-abc123`). Used to name pods, secrets, volumes, and services so multiple instances can co-exist. |
| `{{ .TemplateID }}` | string | Fully qualified template identifier (e.g. `services/chat`). Stamped onto every resource as the `ai-services.io/template` label. |
| `{{ .BaseDir }}` | string | Host filesystem base directory for the deployment (e.g. `/opt/ai-services`). Used to resolve host-path mounts such as the shared `models/` directory. |
| `{{ .Values }}` | object | Root object for all values sourced from `values.yaml` and any user-supplied overrides. Access fields with `{{ .Values.<key> }}`. |

#### 9.1.2 Lifecycle labels

These labels are placed on Pod `metadata.labels` and read by the runtime to manage lifecycle, secrets, and volumes. They apply equally to service and component pods.

| Label | Required | Value | Description |
|---|---|---|---|
| `ai-services.io/template` | **yes** | `"{{ .TemplateID }}"` | Identifies which template produced this pod or secret. Must be present on every resource emitted by a template — used for ownership tracking and cleanup. |
| `ai-services.io/secret` | no | Secret name(s), comma-separated | Marks the listed secrets as owned by this pod. The runtime creates and deletes them alongside the pod. |
| `ai-services.io/secret-skip-cleanup` | no | `"true"` | Prevents the named secrets from being deleted on teardown. Set alongside `ai-services.io/secret` for credentials that must survive restarts (e.g. database passwords). |
| `ai-services.io/volume` | no | PVC / secret-volume name(s), comma-separated | Declares persistent volumes or secret-backed volumes that the runtime provisions before starting the pod and deprovisions on teardown. |

#### 9.1.3 `@generate` directive

A `# @generate` comment immediately before a `values.yaml` field instructs the engine to produce a value at deploy time.

| Directive | Behaviour |
|---|---|
| `# @generate:password` | Generates a cryptographically random password before template rendering. The value is persisted so restarts reuse the same credential. |

#### 9.1.4 `podTemplateExecutions` execution order

The `podTemplateExecutions` list in a runtime `metadata.yaml` controls template application order. Each inner list is a batch applied in parallel; batches run sequentially.

```yaml
# General pattern — sequential layers
podTemplateExecutions:
  - [secret.yaml.tmpl]       # layer 1: credential secret created first
  - [dependency.yaml.tmpl]   # layer 2: dependency pod waits for secret
  - [service.yaml.tmpl]      # layer 3: service pod waits for dependency
```

Templates in the same inner list run concurrently:

```yaml
podTemplateExecutions:
  - [opensearch-secret.yaml.tmpl, vllm-secret.yaml.tmpl]  # both secrets in parallel
  - [opensearch.yaml.tmpl, vllm-server.yaml.tmpl]          # both pods in parallel
```

---

### 9.2 Services

#### 9.2.1 `values.yaml` — service-owned values

Each service defines its own `values.yaml` with the configuration defaults for its containers. Fields that require a generated credential use the shared `@generate` directive (§9.1.3):

```yaml
# services/digitize/podman/values.yaml
digitize:
  image: icr.io/ai-services-cicd/digitize-service:v0.0.34
  log_level: "INFO"
  database: "digitize_metadata"

postgres:
  image: icr.io/ai-services-cicd/postgres:18-4
  username: "postgres"
  # @generate:password
  password: ""
```

```yaml
# services/chat/podman/values.yaml
ui:
  port: ""
  image: icr.io/ai-services-cicd/chatbot-ui:v0.0.48
backend:
  port: ""
  image: icr.io/ai-services-cicd/chatbot-service:v0.0.25
  log_level: "INFO"
  chatbot:
    searchMode: "hybrid"
    numChunksPostReranker: 3
    rerank: true
    systemPrompt: ""
```

#### 9.2.2 Labels

Service pods use the shared lifecycle labels from §9.1.2. The `ai-services.io/secret` and `ai-services.io/volume` labels appear on the sub-pods (e.g. the postgres pod) that own a credential secret or a PVC, not on the main service pod itself.

```yaml
# digitize postgres sub-pod — credential secret survives teardown, PVC owned by runtime
labels:
  ai-services.io/template: "{{ .TemplateID }}"
  ai-services.io/secret: "digitize-db-secret-{{ .InstanceSlug }}"
  ai-services.io/secret-skip-cleanup: "true"
  ai-services.io/volume: "postgres-digitize-{{ .InstanceSlug }},digitize-db-secret-{{ .InstanceSlug }}"

# summarize postgres sub-pod — same pattern
labels:
  ai-services.io/template: "{{ .TemplateID }}"
  ai-services.io/secret: "summarize-db-secret-{{ .InstanceSlug }}"
  ai-services.io/secret-skip-cleanup: "true"
  ai-services.io/volume: "postgres-summarize-{{ .InstanceSlug }},summarize-db-secret-{{ .InstanceSlug }}"

# chat / similarity / summarize main pods — no secrets or volumes owned at pod level
labels:
  ai-services.io/template: "{{ .TemplateID }}"
```

#### 9.2.3 Routing annotations

Components run headlessly and are not directly reachable by users. Only service pods carry routing annotations — the Caddy reverse-proxy reads these to wire up UI and API endpoints for each deployed service instance.

| Annotation | Description |
|---|---|
| `ai-services.io/routes` | Comma-separated `<containerPort>:<caddy-upstream-name>:<role>` tuples. `role` is `ui` (browser-facing) or `api` (machine-facing). |
| `ai-services.io/ports` | Comma-separated `<hostPort>:<containerPort>` tuples. Exposes the port directly on the host, bypassing Caddy. Commented out by default — remove the comment to activate. |

```yaml
# chat — UI on 3000, API on 5000
annotations:
  ai-services.io/routes: "3000:chat-bot-ui-{{ .InstanceSlug }}:ui,5000:chat-bot-backend-{{ .InstanceSlug }}:api"
  # ai-services.io/ports: "{{ .Values.ui.port }}:3000,{{ .Values.backend.port }}:5000"

# digitize — UI on 4001, API on 4000
annotations:
  ai-services.io/routes: "4001:digitize-ui-{{ .InstanceSlug }}:ui,4000:digitize-backend-{{ .InstanceSlug }}:api"
  # ai-services.io/ports: "{{ .Values.digitizeUi.port }}:4001,{{ .Values.digitize.port }}:4000"

# similarity — API only on 7000
annotations:
  ai-services.io/routes: "7000:similarity-api-{{ .InstanceSlug }}:api"
  # ai-services.io/ports: "{{ or .Values.similarity.port }}:7000"

# summarize — API only on 6000
annotations:
  ai-services.io/routes: "6000:summarize-api-{{ .InstanceSlug }}:api"
  # ai-services.io/ports: "{{ or .Values.summarize.port }}:6000"
```

#### 9.2.4 Injected dependency values (`.Values.<component>`)

When a service declares dependencies in its top-level `metadata.yaml`, the engine injects connection details for each resolved component under well-known keys inside `.Values`. These keys are **read-only** and absent in component templates — they are populated by the runtime from the component's own running deployment.

**LLM** (`dependencies: [{id: llm}]`)

| Value path | Type | Description |
|---|---|---|
| `.Values.llm.host` | string | Hostname of the running LLM pod. |
| `.Values.llm.port` | string | Container port (default `8000`). |
| `.Values.llm.model` | string | Served model name as passed to vLLM / LiteLLM. |
| `.Values.llm.maxModelLen` | int | Maximum token context length. |
| `.Values.llm.maxBatchSize` | int | Maximum concurrent batch size. |
| `.Values.llm.apiKey` | string | Optional API key; empty string when not configured. When non-empty, the secret is mounted at `/etc/secret/vllm-secret/apiKey`. |
| `.Values.llm.instanceSlug` | string | Instance slug of the resolved LLM component (e.g. for referencing `vllm-secret-{{ .Values.llm.instanceSlug }}`). |

**Embedding** (`dependencies: [{id: embedding}]`)

| Value path | Type | Description |
|---|---|---|
| `.Values.embedding.host` | string | Hostname of the running embedding pod. |
| `.Values.embedding.port` | string | Container port (default `8001`). |
| `.Values.embedding.model` | string | Served embedding model name. |
| `.Values.embedding.maxModelLen` | int | Maximum sequence length for the embedding model. |

**Reranker** (`dependencies: [{id: reranker}]`)

| Value path | Type | Description |
|---|---|---|
| `.Values.reranker.host` | string | Hostname of the running reranker pod. |
| `.Values.reranker.port` | string | Container port (default `8002`). |
| `.Values.reranker.model` | string | Served reranker model name. |

**Vector store** (`dependencies: [{id: vector_store}]`)

| Value path | Type | Description |
|---|---|---|
| `.Values.vector_store.host` | string | Hostname of the running vector-store pod (e.g. `opensearch-<slug>`). |
| `.Values.vector_store.port` | string | Container port (default `9200` for OpenSearch). |
| `.Values.vector_store.instanceSlug` | string | Instance slug of the resolved vector-store component (e.g. for referencing `opensearch-secret-{{ .Values.vector_store.instanceSlug }}`). |

#### 9.2.5 `values.schema.json` — user-configurable parameters

An optional `values.schema.json` (JSON Schema draft-07) alongside a service's `values.yaml` declares which fields the user can configure through the catalog UI. Fields absent from the schema are treated as internal defaults and not surfaced in the UI.

Three custom `x-ui-*` extensions control form rendering:

| Extension keyword | Description |
|---|---|
| `x-ui-only` | Field is rendered in the UI form but **not** passed into templates. Used for toggle controls that gate visibility of other fields. |
| `x-ui-controls` | Names the field whose UI visibility this field controls. The named field is only shown when this field is `true`. |
| `x-ui-controlled-by` | This field is hidden in the UI unless the field named here is `true`. |

```json
// services/chat/podman/values.schema.json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "backend": {
      "type": "object",
      "properties": {
        "editSystemPrompt": {
          "type": "boolean",
          "title": "Edit system prompt for queries",
          "x-ui-only": true,
          "x-ui-controls": "systemPrompt"
        },
        "systemPrompt": {
          "type": "string",
          "title": "Prompt text",
          "default": "You are a helpful, conversational AI assistant...",
          "minLength": 10,
          "maxLength": 5000,
          "x-ui-controlled-by": "editSystemPrompt"
        }
      }
    }
  }
}
```

| Built-in service | Configurable parameters |
|---|---|
| `chat` | `backend.systemPrompt` (gated by `backend.editSystemPrompt` toggle) |
| `digitize` | _(empty schema — no user-configurable parameters)_ |
| `similarity` | _(empty schema — no user-configurable parameters)_ |
| `summarize` | _(empty schema — no user-configurable parameters)_ |

#### 9.2.6 `podTemplateExecutions` — service examples

Services with a bundled database follow a three-layer pattern:

```yaml
# services/digitize/podman/metadata.yaml
podTemplateExecutions:
  - [postgres-secret.yaml.tmpl]   # layer 1: credential secret
  - [postgres.yaml.tmpl]          # layer 2: postgres pod
  - [digitize.yaml.tmpl]          # layer 3: digitize service pod

# services/summarize/podman/metadata.yaml
podTemplateExecutions:
  - [postgres-secret.yaml.tmpl]
  - [postgres.yaml.tmpl]
  - [summarize-api.yaml.tmpl]
```

Services without a bundled database (chat, similarity) have no `podTemplateExecutions` — a single template is applied directly.

---

### 9.3 Components

#### 9.3.1 Built-in component catalog

The following components are shipped with ai-services and can be referenced as dependencies in a service's `metadata.yaml`. Values listed apply to the **Podman** runtime; OpenShift Helm values differ and are covered by the TODO above.

**LLM providers** (`component_type: llm`)

| ID | Default port | `values.yaml` keys | Spyre label |
|---|---|---|---|
| `vllm-cpu` | `8000` | `image`, `model`, `apiKey`, `maxNumBatchedTokens`, `maxModelLen`, `maxBatchSize` | — |
| `vllm-spyre` | `8000` | `image`, `model`, `apiKey`, `maxModelLen`, `maxBatchSize` | `ai-services.io/llm--spyre-cards: "4"` |
| `watsonx` | `8000` | `image`, `model`, `watsonxApiKey`, `watsonxProjectId`, `watsonxUrl`, `maxModelLen`, `maxBatchSize` | — |

**Embedding providers** (`component_type: embedding`)

| ID | Default port | `values.yaml` keys | Spyre label |
|---|---|---|---|
| `vllm-cpu` | `8001` | `image`, `model`, `maxModelLen` | — |

**Reranker providers** (`component_type: reranker`)

| ID | Default port | `values.yaml` keys | Spyre label |
|---|---|---|---|
| `vllm-cpu` | `8002` | `image`, `model` | — |
| `vllm-spyre` | `8002` | `image`, `model` | `ai-services.io/reranker--spyre-cards: "1"` |

**Vector-store providers** (`component_type: vector_db`)

| ID | Default port | `values.yaml` keys | Credential handling |
|---|---|---|---|
| `opensearch` | `9200` | `image`, `memoryLimit`, `auth.username`, `auth.password` | `@generate:password` on `auth.password`; secret persists across restarts (`secret-skip-cleanup`) |

#### 9.3.2 `values.yaml` — component-owned values

Each component defines its own `values.yaml`. Fields that need auto-generated credentials use the `@generate` directive (§9.1.3):

```yaml
# components/vector_db/opensearch/podman/values.yaml
image: icr.io/ppc64le-oss/opensearch-ppc64le:3.5.0
memoryLimit: 8Gi
auth:
  username: "admin"
  # @generate:password
  password: ""
```

```yaml
# components/llm/vllm-cpu/podman/values.yaml
image: icr.io/ppc64le-oss/vllm-ppc64le:0.19.1
model: ""
apiKey: ""
maxNumBatchedTokens: 26208
maxModelLen: 26208
maxBatchSize: 32
```

```yaml
# components/llm/watsonx/podman/values.yaml
image: "icr.io/ai-services-cicd/litellm:v1.89.3-1"
model: "ibm/granite-4-h-small"
watsonxApiKey: ""
watsonxProjectId: ""
watsonxUrl: ""
maxModelLen: 26208
maxBatchSize: 32
```

#### 9.3.3 Labels

Component pods use the shared lifecycle labels from §9.1.2. In addition, Spyre-based component pods carry accelerator labels that the runtime uses for resource scheduling.

| Label | Required | Value | Used by |
|---|---|---|---|
| `ai-services.io/llm--spyre-cards` | no | Quoted integer (e.g. `"4"`) | `vllm-spyre` LLM component only |
| `ai-services.io/reranker--spyre-cards` | no | Quoted integer (e.g. `"1"`) | `vllm-spyre` reranker component only |

```yaml
# opensearch — secret and volume owned by runtime, credential persists on teardown
labels:
  ai-services.io/template: "{{ .TemplateID }}"
  ai-services.io/secret: "opensearch-secret-{{ .InstanceSlug }}"
  ai-services.io/secret-skip-cleanup: "true"
  ai-services.io/volume: "opensearch-{{ .InstanceSlug }},opensearch-secret-{{ .InstanceSlug }}"

# vllm-cpu LLM — secret and volume only when an API key is set
labels:
  ai-services.io/template: "{{ .TemplateID }}"
  {{- if .Values.apiKey }}
  ai-services.io/secret: "vllm-secret-{{ .InstanceSlug }}"
  ai-services.io/volume: "vllm-secret-{{ .InstanceSlug }}"
  {{- end }}

# watsonx LLM — secret always present (API key is required)
labels:
  ai-services.io/template: "{{ .TemplateID }}"
  ai-services.io/secret: "watsonx-secret-{{ .InstanceSlug }}"
  ai-services.io/volume: "watsonx-secret-{{ .InstanceSlug }}"

# vllm-spyre LLM — four Spyre cards required
labels:
  ai-services.io/template: "{{ .TemplateID }}"
  ai-services.io/llm--spyre-cards: "4"

# vllm-spyre reranker — one Spyre card required
labels:
  ai-services.io/template: "{{ .TemplateID }}"
  ai-services.io/reranker--spyre-cards: "1"

# vllm-cpu embedding / reranker-cpu — no secrets or volumes
labels:
  ai-services.io/template: "{{ .TemplateID }}"
```

#### 9.3.4 `io.podman.*` and OCI annotations

These annotations tune low-level container behaviour and are passed straight through to the Podman runtime. They appear on component pods only — service pods do not use them.

| Annotation | Used by | Value | Description |
|---|---|---|---|
| `io.podman.annotations.ulimit` | `vllm-cpu` LLM, `vllm-spyre` LLM, `vllm-cpu` embedding, `vllm-cpu` reranker, `vllm-spyre` reranker | `"nofile=134217728:134217728,memlock=-1:-1"` | Raises the open-file-descriptor and locked-memory ulimits required by vLLM. |
| `io.podman.annotations.pids-limit/<container>` | `opensearch`, PostgreSQL (note: postgres pods are defined in service templates but the annotation applies to the postgres container within) | `"4096"` | Sets the PID limit for the named container within the pod. |
| `io.podman.annotations.userns` | `vllm-spyre` LLM, `vllm-spyre` reranker | `"keep-id"` | Preserves the host user namespace mapping; required for Spyre device access. |
| `run.oci.keep_original_groups` | `vllm-spyre` LLM, `vllm-spyre` reranker | `"1"` | Preserves host supplemental groups, enabling `/dev/vfio` access. |

```yaml
# vllm-cpu LLM / embedding / reranker-cpu — ulimit only
annotations:
  io.podman.annotations.ulimit: "nofile=134217728:134217728,memlock=-1:-1"

# opensearch component — PID limit on the opensearch container
annotations:
  io.podman.annotations.pids-limit/opensearch: "4096"

# vllm-spyre LLM / reranker — full Spyre set
annotations:
  io.podman.annotations.ulimit: "nofile=134217728:134217728,memlock=-1:-1"
  io.podman.annotations.userns: "keep-id"
  run.oci.keep_original_groups: "1"
```

#### 9.3.5 `podTemplateExecutions` — component examples

Components that manage a credential secret run a two-layer pattern. Components without a secret apply a single template directly.

```yaml
# components/vector_db/opensearch/podman/metadata.yaml — two layers
podTemplateExecutions:
  - [opensearch-secret.yaml.tmpl]   # layer 1: credential secret
  - [opensearch.yaml.tmpl]          # layer 2: opensearch pod

# components/llm/vllm-cpu/podman/metadata.yaml — optional secret (empty if apiKey unset)
podTemplateExecutions:
  - [vllm-secret.yaml.tmpl]
  - [vllm-server.yaml.tmpl]

# components/llm/vllm-spyre/podman/metadata.yaml
podTemplateExecutions:
  - [vllm-secret.yaml.tmpl]
  - [vllm-server.yaml.tmpl]

# components/llm/watsonx/podman/metadata.yaml
podTemplateExecutions:
  - [watsonx-secret.yaml.tmpl]
  - [watsonx-server.yaml.tmpl]

# components/embedding/vllm-cpu and components/reranker/vllm-cpu — no secret template
# podTemplateExecutions is omitted; the single pod template is applied directly
```

#### 9.3.6 `{{- with .env.<component> }}` — env injection override

Spyre-based component templates support an env-override mechanism that lets the deployment layer inject extra environment variables into a container without modifying `values.yaml`.

The Go template `with` action is scoped to a top-level `.env` map. If `.env.<component>` is absent or empty the block renders nothing; if it is a non-empty map every key–value pair is appended as an additional `env` entry.

```yaml
# vllm-spyre LLM template — env block with override hook
env:
  - name: MAX_MODEL_LEN
    value: "{{ .Values.maxModelLen }}"
  - name: MASTER_PORT
    value: "12355"
  {{- with .env.llm }}
    {{- range $k, $v := . }}
  - name: {{ $k }}
    value: "{{ $v }}"
    {{- end }}
  {{- end }}
```

| Template | `.env` key |
|---|---|
| `components/llm/vllm-spyre/podman/templates/vllm-server.yaml.tmpl` | `.env.llm` |
| `components/reranker/vllm-spyre/podman/templates/reranker-server.yaml.tmpl` | `.env.reranker` |
| `applications/rag/podman/templates/vllm-server.yaml.tmpl` | `.env.instruct`, `.env.reranker` |
| `applications/rag-dev/podman/templates/vllm-server.yaml.tmpl` | `.env.instruct` |

Custom component templates may adopt the same pattern for any key name. The `.env` object is always safe to query with `with` — if the key is absent the block is silently skipped.

---

## 11. Usage Examples

### 10.1 Upload a custom service bundle

```bash
# Authenticate
curl -X POST https://catalog-api.<domain>/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"<password>"}' \
  | jq -r .access_token > token.txt

# Package the custom service directory
tar -czf my-bundle.tar.gz services/

# Upload the bundle (catalog_type and version are required fields)
curl -X POST https://catalog-api.<domain>/api/v1/catalog/bundles \
  -H "Authorization: Bearer $(cat token.txt)" \
  -F "file=@my-bundle.tar.gz" \
  -F "catalog_type=service" \
  -F "version=1.0.0"

# Poll for activation using the bundle ID from the 202 response
curl -s https://catalog-api.<domain>/api/v1/catalog/bundles/bnd_01JW4X9K2M8VQRP3T5YZ \
  -H "Authorization: Bearer $(cat token.txt)" | jq .status
# "active"
```

### 10.2 Create an application from the custom service

The application creation endpoint (`POST /api/v1/applications/`) accepts a `CreateApplicationRequest` with `name`, `catalog_id`, `version`, and a `services` array. Each service entry requires its own `catalog_id`, `version`, and `components` list.

```bash
curl -X POST https://catalog-api.<domain>/api/v1/applications/ \
  -H "Authorization: Bearer $(cat token.txt)" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-deployment",
    "catalog_id": "my-service",
    "version": "1.0.0",
    "services": [
      {
        "catalog_id": "my-service",
        "version": "1.0.0",
        "components": [
          {
            "component_type": "llm",
            "provider_id": "vllm-cpu",
            "version": "1.0.0",
            "params": { "model": "granite-3.3-8b-instruct" }
          }
        ]
      }
    ]
  }'
# Returns 202 Accepted with {"id": "<application-uuid>"}
```

### 10.3 Attempt to use a reserved built-in ID (rejected)

Uploading a bundle whose `catalog_id` matches a built-in service is rejected at validation time — the extracted directory is cleaned up and no activation occurs:

```bash
# Attempting to upload a bundle with catalog_id "chat" (a built-in service)
tar -czf chat-bundle.tar.gz services/chat

curl -X POST https://catalog-api.<domain>/api/v1/catalog/bundles \
  -H "Authorization: Bearer $(cat token.txt)" \
  -F "file=@chat-bundle.tar.gz" \
  -F "catalog_type=service" \
  -F "version=2.0.0"

# Poll for result — validation rejects the bundle
curl -s https://catalog-api.<domain>/api/v1/catalog/bundles/bnd_03EF9Z2GH4Q7RST5V8WX \
  -H "Authorization: Bearer $(cat token.txt)" | jq '{status, error}'
# {
#   "status": "failed",
#   "error":  "catalog_id \"chat\" is reserved by a built-in service and cannot be overridden"
# }
```

### 10.4 List custom services via the catalog API

After uploading a bundle, custom services appear alongside built-in ones. The `GET /api/v1/services` endpoint returns an array of `ServiceSummary` objects (not a wrapped object), so the `jq` filter uses `.[].id`:

```bash
curl -s https://catalog-api.<domain>/api/v1/services \
  -H "Authorization: Bearer $(cat token.txt)" | jq '.[].id'

# "chat"
# "digitize"
# "similarity"
# "summarize"
# "my-service"   ← custom
```

### 10.5 Dry-run validation before applying

```bash
# Validate without activating — useful in CI before promoting to production
# All required fields (catalog_type, version) must be present even for dry_run
curl -X POST https://catalog-api.<domain>/api/v1/catalog/bundles \
  -H "Authorization: Bearer $(cat token.txt)" \
  -F "file=@my-bundle.tar.gz" \
  -F "catalog_type=service" \
  -F "version=1.0.0" \
  -F "dry_run=true"
# Returns 202 with status: "validated" (not promoted to active)
```

---

## 12. Backward Compatibility

| Scenario | Behaviour |
|---|---|
| No bundle uploaded yet | Identical to current — `EmbeddedCatalogFS` only |
| Volume mounted but no `status='active'` rows in DB | Only `EmbeddedCatalogFS` is used; behaviour identical to today |
| Custom service has same `id` as built-in | Bundle is rejected with `422`; built-in is never shadowed |
| Multiple bundles for different `catalog_id` values | All are `active` simultaneously; each is fully independent |
| Existing applications in the database | Unaffected; records reference `catalog_id` strings which remain stable |
| `catalog_type` value not in the accepted list | Rejected immediately with `400` before the archive is touched |
| New `catalog_type` value introduced in a future release | Existing clients that receive an unfamiliar `catalog_type` string are unaffected — they simply don't render that bundle type in their UI |
| `assets.ApplicationFS` and existing `application/` packages | Not touched; independent of `CatalogProvider` |
| OpenShift deployment | Same API endpoint and bundle format; only the storage backend differs (PVC vs named volume) |

---

## 13. Future Enhancements

1. **Scaffolding generator** — `ai-services catalog scaffold --service my-service --runtime podman` emits a minimal but correct directory skeleton ready to be tar'd and uploaded.
2. **Template validation command** — `dry_run=true` already supported in §7.2.1; a dedicated CLI command `ai-services catalog validate --bundle <file>` wraps this for local use.
3. **Remote catalog repositories** — fetch a bundle from an OCI registry or HTTPS URL; the server pulls and applies it directly, removing the need for a client upload.
4. **Schema enforcement on custom `metadata.yaml`** — reuse the existing [`validators.ApplicationValidator`](ai-services/internal/pkg/catalog/validators/validation.go) to reject malformed custom metadata at validation time.
5. **Version compatibility checks** — validate that a custom service's `version` satisfies any `>=x.y.z` constraint declared by the built-in architecture that references it.
6. **Role-based upload access** — introduce a `catalog-editor` JWT role that can upload bundles but cannot perform `DELETE /applications` or other destructive operations.
7. **Component support in bundles** — when `components/` is promoted from reserved to active in the bundle processor, users can ship custom component providers (e.g. a private LLM backend) alongside their services in the same archive.

