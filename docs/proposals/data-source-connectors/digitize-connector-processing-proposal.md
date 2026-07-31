# Digitize — Data Source Connector Processing Proposal

> **Scope:** Internal `digitize` behavior after catalog sends connector payloads. Catalog-side concerns such as key management, connector CRUD, deployment wiring and TLS provisioning remain out of scope and are treated as infrastructure-level prerequisites.

---

## 1. Preconditions

Before any `digitize` connector endpoint is called:

- Catalog has already validated the remote connector configuration.
- Catalog sends secret material in plaintext via API calls:
  - `ssh`: `private_key`
  - `s3`: `secret_access_key`
- `digitize` encrypts those secret fields at rest using `/run/secrets/connector_encryption_key` before persisting them.
- `/run/secrets/connector_api_token` and `/run/secrets/connector_encryption_key` are mounted before pod start.
- The `file_checksum_registry` table and the `FileChecksumRegistry` ORM model already exist in the codebase (merged in the de-duplication PR). This table is **user-submitted documents only** — connector code must never read from or write to it. Connector dedup is handled exclusively via `connector_file_membership` (see §4.4).

These assumptions are referenced once here and not repeated below.

---

## 2. System Overview

### 2.1 Architecture Diagram

![Architecture Diagram](architecture-diagram.svg)

### 2.2 Runtime Flow

```text
── Attach (POST /v1/connectors) ─────────────────────────────────────
  → validate + encrypt credentials
  → INSERT active_connectors row
  → schedule worker startup (background task)
  → worker thread starts and runs first tick immediately

── Config update (PUT /v1/connectors/{id}) ──────────────────────────
  → merge + re-encrypt changed fields
  → UPDATE active_connectors row
  → running worker re-reads config from DB before its next tick

── Each sync tick (connector-sourced) ───────────────────────────────
  Step 1 — load state
    → SELECT known_checksums FROM connector_file_membership
           WHERE connector_ids @> [connector_id]
      (all checksums this connector already owns)

  Step 2 — file walk + classify
    → scanner walks remote source, computes (remote_path, checksum) per file
    → for each file:
        if checksum IN known_checksums:
          → already owned by this connector — add connector_id to connector_ids
            (idempotent JSONB upsert) and place on skip_list  ← no download
        else:
          → new to this connector — place on ingest_list

  Step 3 — ingest new files (ingest_list)
    → for each (remote_path, checksum) in ingest_list:
        download file → run create_job(connector_id=connector_id)
        on job success:
          add_connector_to_membership(connector_id, checksum, doc_id)
            → INSERT (checksum, [connector_id], doc_id) on first owner
            → JSONB array append if another connector already owns checksum
              (shared-content case — no second download)

  Step 4 — orphan detection + removal
    → orphan_checksums = known_checksums − {checksum for (_, checksum) in scanned_files}
      (checksums this connector previously owned that are no longer on the remote source)
    → for each orphan_checksum in orphan_checksums (after all batch ingest jobs finish):
        remove_connector_from_membership(connector_id, orphan_checksum)
          → removes connector_id from connector_ids array
          → returns (remaining_owner_count, doc_id)
        if remaining_owner_count == 0:
          DELETE FROM connector_file_membership WHERE checksum = orphan_checksum
          DELETE /v1/documents/{doc_id}

  Step 5 — finalise tick
    → UPDATE connector_sync_history (files_found, completed, failed, status)
    → UPDATE active_connectors (last_sync_at, sync_status)

── Detach (DELETE /v1/connectors/{id}) ──────────────────────────────
  → guard: reject with 409 if a tick is currently running
  → stop worker thread
  → list all checksums owned by this connector
  → for each checksum: remove_connector_from_membership → delete doc if last owner
  → DELETE active_connectors row
  → cleanup staging dirs

── User-submitted files (no connector_id) ───────────────────────────
  → create-job flow detects absence of connector_id
  → dedup check against file_checksum_registry (checksum PK) only
  → on success: INSERT INTO file_checksum_registry (checksum, doc_id)
  → connector_file_membership is never written for user-submitted docs
```

### 2.3 Main Components

- `active_connectors`: current connector configuration and top-level sync state
- `file_checksum_registry`: **user-submitted documents only** — keyed by `checksum`, stores a content fingerprint and the `doc_id`. **Already implemented** — table and ORM model exist. Connector code must never write to this table.
- `connector_file_membership`: **connector-sourced documents only** — keyed by `checksum` (PK), carries the list of connector IDs that reference this file and the `doc_id` for deletion. User-submitted docs are never written here.
- `connector_sync_history`: one row per worker tick
- `ConnectorWorkerManager`: owns worker thread lifecycle
- `ConnectorSyncWorker`: executes periodic sync logic
- scanner implementations: transport-specific remote access for SFTP and S3; S3 scanner derives the checksum from the S3 ETag returned by `list_objects_v2`; SFTP scanner uses a remotely-computed MD5 — both stored as `checksum` in `connector_file_membership`

---

## 3. API Contract

All endpoints are bearer-token protected and served over TLS.

### 3.1 `POST /v1/connectors`

Creates a connector, stores encrypted credentials, persists config, and schedules worker startup asynchronously. The worker runs its first tick immediately after the thread starts.

#### Request body

Common fields:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `connector_id` | `string (UUID)` | ✅ | Stable catalog ID |
| `type` | `string` | ✅ | `ssh` or `s3` |
| `host` | `string` | ✅ | SFTP host or S3 endpoint |
| `allowed_extensions` | `array[string]` | ✅ | Non-matching files are ignored |
| `connection_details` | `object` | ✅ | Type-specific fields |

> **Note:** `sync_interval_seconds` is not accepted in the API payload. It is read from the `CONNECTOR_SYNC_INTERVAL_SECONDS` environment variable (default `300`) and applies uniformly to all connectors.

`connection_details` for `ssh`:

| Field | Type | Required |
| --- | --- | --- |
| `username` | `string` | ✅ |
| `remote_path` | `string` | ✅ |
| `private_key` | `string` | ✅ |

`connection_details` for `s3`:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `endpoint_url` | `string` | ✅ | Full S3 endpoint URL. AWS S3: `https://s3.<region>.amazonaws.com`. IBM COS: `https://s3.<region>.cloud-object-storage.appdomain.cloud`. Provider and region are auto-detected from this URL — no separate `region` field needed. |
| `bucket_name` | `string` | ✅ | |
| `access_key_id` | `string` | ✅ | IAM key ID (AWS) or HMAC key ID (IBM COS) |
| `secret_access_key` | `string` | ✅ | IAM secret (AWS) or HMAC secret (IBM COS) |
| `prefix` | `string` | ❌ | Key prefix to scope listing — empty means bucket root |
| `delimiter` | `string` | ❌ | Set `"/"` for non-recursive (immediate children only) |

> **Checksum-based dedup (connector path):** For S3 connectors, `list_objects_v2` returns the object ETag at no extra API cost — the scanner stores this as `checksum` in `connector_file_membership`. If the checksum is already present in `connector_file_membership` for this connector the file is **never downloaded**. The checksum is also stored in document `metadata.source_checksum` for traceability. See [§4.4](#44-connector_file_membership) and [§7.2](#72-s3-scanner).

#### Example payloads

```json
{
  "connector_id": "c7f3a2d1-...",
  "type": "ssh",
  "host": "sftp.example.com",
  "allowed_extensions": [".pdf", ".docx"],
  "connection_details": {
    "username": "sync_user",
    "remote_path": "/exports/reports",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n..."
  }
}
```

```json
{
  "connector_id": "a1b2c3d4-...",
  "type": "s3",
  "host": "s3.us-east-1.amazonaws.com",
  "allowed_extensions": [".pdf", ".docx"],
  "connection_details": {
    "endpoint_url": "https://s3.us-east-1.amazonaws.com",
    "bucket_name": "my-rag-documents",
    "prefix": "reports/",
    "delimiter": "/",
    "access_key_id": "AKIAIOSFODNN7EXAMPLE",
    "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
  }
}
```

IBM COS example:

```json
{
  "connector_id": "b5c6d7e8-...",
  "type": "s3",
  "host": "s3.us.cloud-object-storage.appdomain.cloud",
  "allowed_extensions": [".pdf", ".docx"],
  "connection_details": {
    "endpoint_url": "https://s3.us.cloud-object-storage.appdomain.cloud",
    "bucket_name": "ai-services",
    "access_key_id": "<hmac-key-id>",
    "secret_access_key": "<hmac-secret>"
  }
}
```

#### Response codes

| Status | Meaning |
| --- | --- |
| `202 Accepted` | Connector created; worker start scheduled in a background task |
| `409 Conflict` | Connector already exists |

### 3.2 `PUT /v1/connectors/{connector_id}`

Updates an existing connector's config in the database. The running worker is not restarted — it reads the latest config from the DB before entering the next tick.

Rules:

- All fields are optional.
- Omitted fields remain unchanged.
- `type` cannot change.
- `connection_details` is merged by key, not replaced wholesale.
- If credentials are included, they are re-encrypted before storage.
- `sync_interval_seconds` cannot be set via this endpoint; change the env variable and redeploy.

Example partial update:

```json
{
  "connection_details": {
    "remote_path": "/exports/v2/reports",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n..."
  }
}
```

Response codes:

| Status | Meaning |
| --- | --- |
| `200 OK` | Connector updated; worker picks up changes on next tick |
| `404 Not Found` | Connector does not exist |

### 3.3 `DELETE /v1/connectors/{connector_id}`

Removes a connector and its runtime state.

> **Constraint — active sync tick:** DELETE is accepted **only when no sync tick is currently in progress** for the connector. If the worker is mid-tick, the request is rejected with `409 Conflict`. This restriction exists because the system does not yet have the ability to cancel or interrupt a running job. Once job cancellation is supported, DELETE will be allowed at any point in the connector lifecycle.

Delete flow:

1. **Guard:** check whether a sync tick is actively running for the connector. If yes, return `409 Conflict` immediately — no state is modified.
2. Stop the worker.
3. Snapshot the connector's known checksums.
4. Remove membership rows checksum by checksum.
5. Delete documents only when the remaining reference count reaches zero.
6. Delete the `active_connectors` row.
7. Best-effort cleanup of staging directories.

#### Delete sequence diagram

```text
DELETE /v1/connectors/{connector_id}
  → check if sync tick is in progress
      if YES → 409 Conflict (no state modified)
  → stop worker
  → list checksums owned by this connector (connector_file_membership WHERE connector_ids @> [connector_id])
  → for each checksum:
       remove_connector_from_membership(connector_id, checksum)
         → removes connector_id from connector_ids array
         → returns (remaining_owner_count, doc_id)
       if remaining_owner_count == 0:
         DELETE FROM connector_file_membership WHERE checksum = $1
         DELETE /v1/documents/{doc_id}
  → delete connector row
  → cleanup staging dirs
```

Document deletion is best-effort:

- `200`, `204`, `404` from `DELETE /v1/documents/{doc_id}` are treated as success.
- `5xx` or network failures are logged and cleanup continues.

Response codes:

| Status | Meaning |
| --- | --- |
| `204 No Content` | Connector removed |
| `404 Not Found` | Connector does not exist |
| `409 Conflict` | A sync tick is currently running; retry after the tick completes |

> **Future:** when job cancellation is introduced, the `409` guard will be replaced by an interrupt call so that DELETE can succeed at any stage of the lifecycle.

### 3.4 `GET /v1/connectors`

Lists active connectors with non-secret configuration and current sync state.

Returned fields include:

- connector identity and config
- `sync_status`, `last_sync_at`, `last_sync_error`, `attached_at`

#### Example response

```json
[
  {
    "connector_id": "c7f3a2d1-4e5b-4c6d-8f9a-0b1c2d3e4f5a",
    "type": "ssh",
    "host": "sftp.example.com",
    "allowed_extensions": [".pdf", ".docx"],
    "attached_at": "2025-01-10T08:00:00Z",
    "last_sync_at": "2025-01-15T14:32:10Z",
    "sync_status": "idle",
    "last_sync_error": null,
  },
  {
    "connector_id": "a1b2c3d4-5e6f-7a8b-9c0d-1e2f3a4b5c6d",
    "type": "s3",
    "host": "s3.amazonaws.com",
    "allowed_extensions": [".pdf", ".docx"],
    "attached_at": "2025-01-12T09:15:00Z",
    "last_sync_at": "2025-01-15T14:30:00Z",
    "sync_status": "idle",
    "last_sync_error": "3 files failed to ingest",
  }
]
```

### 3.5 `GET /v1/connectors/{connector_id}`

Returns one connector plus the latest file-processing counters:

- `files_found`
- `files_syncing`
- `files_completed`
- `files_failed`

Only non-secret `connection_details` are returned.

#### Example response

```json
{
  "connector_id": "c7f3a2d1-4e5b-4c6d-8f9a-0b1c2d3e4f5a",
  "type": "ssh",
  "host": "sftp.example.com",
  "allowed_extensions": [".pdf", ".docx"],
  "sync_interval_seconds": 300,
  "attached_at": "2025-01-10T08:00:00Z",
  "last_sync_at": "2025-01-15T14:32:10Z",
  "sync_status": "idle",
  "last_sync_error": null,
  "connection_details": {
    "username": "sync_user",
    "remote_path": "/exports/reports"
  },
  "files_found": 42,
  "files_syncing": 0,
  "files_completed": 40,
  "files_failed": 2
}
```

### 3.6 `GET /v1/connectors/{connector_id}/sync-history`

Returns paginated tick history.

Query params:

| Param | Default | Notes |
| --- | --- | --- |
| `limit` | `50` | capped at `200` |
| `offset` | `0` | zero-based |

Each item contains:

- `sync_id`
- `started_at`
- `finished_at`
- `files_found`
- `files_syncing`
- `files_completed`
- `files_failed`
- `sync_status`

Status values:

- `syncing`
- `completed`
- `N files failed to ingest`
- `N orphan deletes failed`
- `failed: <reason>`

At most one in-progress `syncing` row exists per connector.

#### Example response

```json
{
  "total": 3,
  "limit": 50,
  "offset": 0,
  "items": [
    {
      "sync_id": 3,
      "started_at": "2025-01-15T14:32:00Z",
      "finished_at": "2025-01-15T14:32:10Z",
      "files_found": 42,
      "files_syncing": 0,
      "files_completed": 42,
      "files_failed": 0,
      "sync_status": "completed"
    },
    {
      "sync_id": 2,
      "started_at": "2025-01-15T14:27:00Z",
      "finished_at": "2025-01-15T14:27:18Z",
      "files_found": 41,
      "files_syncing": 0,
      "files_completed": 38,
      "files_failed": 3,
      "sync_status": "3 files failed to ingest"
    },
    {
      "sync_id": 1,
      "started_at": "2025-01-15T14:22:00Z",
      "finished_at": null,
      "files_found": 0,
      "files_syncing": 5,
      "files_completed": 0,
      "files_failed": 0,
      "sync_status": "syncing"
    }
  ]
}
```

### 3.7 Digitize Document & Job API — Connector Visibility Rules

The Digitize APIs for documents and jobs enforce a strict visibility boundary between user-submitted content and connector-sourced content.

#### Document APIs (`/v1/documents`)

| Endpoint | Behaviour |
| --- | --- |
| `GET /v1/documents` | Returns **user-submitted documents only** — connector-sourced docs are excluded |
| `GET /v1/documents/{doc_id}` | Returns the document only if it was user-submitted; returns `404` for connector-sourced docs |
| `DELETE /v1/documents/{doc_id}` | Deletes the document only if it was user-submitted; returns `404` for connector-sourced docs |

**Rationale:** connector-sourced documents are managed exclusively through their data source (S3 bucket, SFTP share). Users interact with those files by adding or removing them at the source; the next connector sync tick handles ingest or deletion automatically. Exposing connector-sourced docs via user-facing APIs would allow deletion without removal from the source, causing the file to be re-ingested on the next tick.

**Implementation:** a document is identified as connector-sourced when a row exists in `connector_file_membership` for its `doc_id`. The DB query for user-facing document endpoints must add a `NOT EXISTS (SELECT 1 FROM connector_file_membership WHERE doc_id = ...)` filter (or equivalent LEFT JOIN / subquery).

#### Job APIs (`/v1/jobs`)

| Endpoint | Behaviour |
| --- | --- |
| `GET /v1/jobs` (list) | Returns **all jobs** — both connector-sourced and user-submitted |
| `GET /v1/jobs/{job_id}` | Returns **all jobs** — connector job details are accessible |
| `DELETE /v1/jobs/{job_id}` | Deletes only job records — same rules apply regardless of origin |

The job list intentionally includes connector-initiated jobs so operators can observe sync progress and diagnose failures in a single view.

#### Detection mechanism

The presence of `connector_id` on a job (stored in job metadata at create time) is the authoritative signal:

- `connector_id` present on job → connector-sourced → excluded from document APIs.
- `connector_id` absent → user-submitted → included in document APIs.

---

## 4. Data Model

**Modified file:** `services/digitize/db/scripts/init_schema.sql`

### 4.1 Table Relationships

```text
── User-submitted path ──────────────────────────────────────────────
file_checksum_registry (checksum PK) ───────────────────> documents

── Connector-sourced path ───────────────────────────────────────────
active_connectors
  └─< connector_file_membership (checksum PK, connector_ids[], doc_id) ─> documents

active_connectors
  └─< connector_sync_history
```

The two registries are **intentionally separate**:
- `file_checksum_registry` → user-submitted dedup only; never touched by connector code.
- `connector_file_membership` → connector-sourced dedup only; never touched by user-submitted code.

A file with the same content can legitimately exist in **both** registries — one row representing the user-uploaded copy and one row representing the connector-synced copy. This is a known and accepted duplicate; the plan is to retire the user-submitted workflow in the future at which point `file_checksum_registry` will no longer be needed (see §4.7).

### 4.2 `active_connectors`

Stores connector config, encrypted credential blobs, and top-level sync state.

```sql
CREATE TABLE IF NOT EXISTS active_connectors (
    id                      TEXT        PRIMARY KEY,
    type                    TEXT        NOT NULL,
    host                    TEXT        NOT NULL,
    connection_details      JSONB       NOT NULL DEFAULT '{}',
    allowed_extensions      JSONB       NOT NULL DEFAULT '[]',
    sync_interval_seconds   INTEGER     NOT NULL DEFAULT 300,
    attached_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_sync_at            TIMESTAMPTZ,
    sync_status             TEXT        NOT NULL DEFAULT 'idle',
    CONSTRAINT chk_connector_type CHECK (type IN ('ssh', 's3'))
);
```

> **Note:** `sync_interval_seconds` is stored per-connector for future extensibility but is not accepted via the API today. On `POST`, it is populated from the `CONNECTOR_SYNC_INTERVAL_SECONDS` environment variable (default `300`). The worker reads the value from the DB before each tick.

### 4.3 `file_checksum_registry`

> **Already implemented. User-submitted documents only.** The table and ORM model were merged in the de-duplication PR. The existing schema uses `checksum` as the primary key. **Connector code must never read from or write to this table.** All connector dedup is handled exclusively via `connector_file_membership`.

Current schema (already in `init_schema.sql` and `db/models.py`):

```sql
CREATE TABLE IF NOT EXISTS file_checksum_registry (
    checksum TEXT PRIMARY KEY,
    doc_id   TEXT NOT NULL UNIQUE REFERENCES documents(doc_id) ON DELETE CASCADE
);
```

Current ORM model (already in `db/models.py`):

```python
class FileChecksumRegistry(Base):
    __tablename__ = "file_checksum_registry"

    checksum: Mapped[str] = mapped_column(Text, primary_key=True)
    doc_id: Mapped[str] = mapped_column(
        Text,
        ForeignKey("documents.doc_id", ondelete="CASCADE"),
        nullable=False,
        unique=True,
    )
```

**User-submitted flow:** when a user submits a file via the API or the Digitize UI:

1. Compute MD5 checksum of the file content.
2. Check `file_checksum_registry` — if already present, return `409 / already_exists`.
3. On successful ingest, insert `(checksum, doc_id)` into `file_checksum_registry`.

**Connector code must not write here.** The connector path uses `connector_file_membership` exclusively.

### 4.4 `connector_file_membership`

**Connector-sourced documents only.** This table is the sole dedup and reference-counting store for all content ingested via connectors. User-submitted code must never write to this table.

The schema uses `checksum` as the primary key. The `connector_ids` column is a JSON array of all connector IDs that currently reference this file (cross-connector shared-content tracking). `doc_id` is stored here — not in `file_checksum_registry` — so that deletion can proceed without a registry join.

```sql
CREATE TABLE IF NOT EXISTS connector_file_membership (
    checksum      TEXT        PRIMARY KEY,
    connector_ids JSONB       NOT NULL DEFAULT '[]',
    doc_id        TEXT        NOT NULL REFERENCES documents(doc_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cfm_doc_id
    ON connector_file_membership (doc_id);
```

**Why `checksum` is the PK:**

A given file (identified by its content checksum) can be owned by multiple connectors at once — e.g. the same PDF is stored in both an S3 bucket connector and an SFTP share connector. Rather than creating one row per `(connector_id, checksum)` pair, a single row per checksum is maintained and the `connector_ids` array is updated atomically. This makes reference counting a simple array-length check rather than a `COUNT(*)` join.

**Connector IDs array invariants:**

- When a connector first encounters a checksum (new content): insert a new row with `connector_ids = [connector_id]` and `doc_id` from the completed ingest job.
- When a second connector encounters the same checksum (already ingested by another connector): append `connector_id` to `connector_ids`, reuse the existing `doc_id` — **no download, no ingest**.
- When a connector removes a file (orphan detected): remove `connector_id` from the array. If the array becomes empty, delete the row and the associated document.

**Dedup check (per tick):**

```sql
-- Known checksums for this connector: all checksums where connector_id appears in the array
SELECT checksum
FROM connector_file_membership
WHERE connector_ids @> jsonb_build_array(:connector_id::text);
```

**Reference-counted delete stub:**

```sql
BEGIN;

-- Remove this connector from the owners array
UPDATE connector_file_membership
SET connector_ids = connector_ids - :connector_id
WHERE checksum = :checksum;

-- Check how many owners remain
SELECT jsonb_array_length(connector_ids), doc_id
FROM connector_file_membership
WHERE checksum = :checksum;

-- If jsonb_array_length == 0: delete the row and the document
-- DELETE FROM connector_file_membership WHERE checksum = :checksum;
-- DELETE /v1/documents/{doc_id}

COMMIT;
```

**Checksum format reference:**

| Source | Value stored in `checksum` | Dedup property |
| --- | --- | --- |
| S3 single-part | S3 ETag = `MD5(file_bytes)` — 32-char hex, no suffix | Unique per file content (for this upload method) |
| S3 multi-part | S3 ETag = `MD5(MD5(p₁)‖…‖MD5(pₙ))-N` — hex + `-N` suffix | Unique per (file content + part size) |
| SFTP | `md5sum` output from remote host — 32-char hex | Unique per file content, protocol-independent |

> **Document metadata:** When a document row is created the scanner also writes the fingerprint into `documents.metadata` as `metadata.source_checksum`. This makes the content fingerprint visible in the document record without a membership join.
>
> ```json
> {
>   "source_checksum": "0234031ed6cb7d686152f45c38f41bc6-13",
>   "source_type": "s3",
>   "bucket": "ai-services",
>   "key": "reports/sg248590-2.pdf"
> }
> ```

### 4.5 `connector_sync_history`

Persistent per-tick history backing the sync-history API.

```sql
CREATE TABLE IF NOT EXISTS connector_sync_history (
    id               BIGSERIAL   PRIMARY KEY,
    connector_id     TEXT        NOT NULL,
    sync_id          INTEGER     NOT NULL,
    started_at       TIMESTAMPTZ NOT NULL,
    finished_at      TIMESTAMPTZ,
    files_found      INTEGER     NOT NULL DEFAULT 0,
    files_syncing    INTEGER     NOT NULL DEFAULT 0,
    files_completed  INTEGER     NOT NULL DEFAULT 0,
    files_failed     INTEGER     NOT NULL DEFAULT 0,
    sync_status      TEXT        NOT NULL DEFAULT 'syncing',
    CONSTRAINT fk_csh_connector
        FOREIGN KEY (connector_id)
        REFERENCES active_connectors(id) ON DELETE CASCADE,
    CONSTRAINT uq_csh_connector_sync
        UNIQUE (connector_id, sync_id)
);

CREATE INDEX IF NOT EXISTS idx_csh_connector_started
    ON connector_sync_history (connector_id, started_at DESC);
```

### 4.6 ORM

**`FileChecksumRegistry` already exists** in `services/digitize/db/models.py` and remains unchanged — it covers user-submitted docs only. Add the remaining three models to that file:

- `ActiveConnector` — fields: `id` (PK), `type`, `host`, `connection_details` (JSONB), `allowed_extensions` (JSONB), `sync_interval_seconds`, `attached_at`, `last_sync_at`, `sync_status`
- `ConnectorFileMembership` — fields: `checksum` (PK), `connector_ids` (JSONB array), `doc_id` (FK → `documents`)
- `ConnectorSyncHistory` — fields: `id` (PK), `connector_id` (FK → `active_connectors`), `sync_id`, `started_at`, `finished_at`, `files_found`, `files_syncing`, `files_completed`, `files_failed`, `sync_status`

### 4.7 Future: Retiring the User-Submitted Workflow

The user-submitted ingestion path (upload via API or Digitize UI) is planned to be retired in a future release. When that happens, `file_checksum_registry` will no longer be needed and can be dropped. The connector-sourced path via `connector_file_membership` is the long-term dedup mechanism.

Until that migration is complete:

- The same file may exist as **both** a `file_checksum_registry` row (user-submitted copy) **and** a `connector_file_membership` row (connector-synced copy). This cross-origin duplicate is intentional and accepted.
- Dedup operates strictly within each path: user-submitted files are deduped only against `file_checksum_registry`; connector files are deduped only against `connector_file_membership`. There is no cross-path dedup.
- User-facing document and job APIs (GET, DELETE on `/v1/documents`, doc-specific GET on `/v1/jobs`) expose only user-submitted documents; connector-sourced documents are excluded from these responses (see §3).

---

## 5. Database Operations Layer

**Modified file:** `services/digitize/utils/db.py`

The DB layer stores and returns ciphertext only. Encryption happens in the API layer; decryption happens in scanners.

### 5.1 Core functions

The following functions already exist in `services/digitize/db/manager.py` and cover the user-submitted de-duplication path — **do not re-implement them, and do not call them from connector code**:

| Existing function | Purpose | Used by |
| --- | --- | --- |
| `upsert_file_checksum(checksum, doc_id)` | Register a user-submitted doc by checksum into `file_checksum_registry` | User-submitted create-job flow only |
| `find_completed_document_by_hash(checksum)` | Dedup lookup against `file_checksum_registry` — returns `Document` or `None` | User-submitted create-job flow only |

**Create-job `connector_id` routing rule:**

When the create-job flow is invoked it accepts an optional `connector_id` parameter. The presence or absence of this parameter determines which dedup table is consulted and which registry is written:

| Scenario | `connector_id` | Dedup table | Registry written | Notes |
| --- | --- | --- | --- | --- |
| User-submitted | absent | `file_checksum_registry` | `file_checksum_registry (checksum, doc_id)` | Connector tables untouched |
| Connector-sourced | present | `connector_file_membership` | `connector_file_membership (checksum, connector_ids, doc_id)` | `file_checksum_registry` untouched |

New connector-specific functions to add:

| Function | Purpose |
| --- | --- |
| `insert_active_connector()` | create connector |
| `upsert_active_connector()` | insert/update connector |
| `get_active_connector()` | fetch one connector |
| `list_active_connectors()` | fetch all connectors |
| `delete_active_connector()` | delete connector |
| `update_connector_sync_status()` | write top-level sync state |
| `merge_connection_details()` | key-level JSON merge for PUT |
| `lookup_connector_content_by_checksum(checksum)` | connector dedup lookup — queries `connector_file_membership`, returns `doc_id` or `None` |
| `list_connector_checksums(connector_id)` | all checksums currently owned by this connector (array containment query on `connector_ids`) |
| `add_connector_to_membership(connector_id, checksum, doc_id)` | insert new membership row OR append `connector_id` to existing row's `connector_ids` array |
| `remove_connector_from_membership(connector_id, checksum)` | remove `connector_id` from `connector_ids` array; return remaining owner count and `doc_id` |
| `insert_sync_history()` | create tick row |
| `update_sync_history()` | finalize tick row |
| `update_sync_history_files_syncing()` | live progress updates |
| `list_sync_history()` | paginated history query |
| `set_document_metadata(doc_id, metadata)` | write `source_checksum` + S3 key into `documents.metadata` |

### 5.2 DB-layer stub

```python
def lookup_connector_content_by_checksum(checksum: str) -> str | None:
    """Return doc_id if checksum is already in connector_file_membership, else None.

    Used exclusively by connector sync workers — never by user-submitted code.
    """
    with session() as s:
        row = s.execute(
            text("SELECT doc_id FROM connector_file_membership WHERE checksum = :checksum"),
            {"checksum": checksum},
        ).one_or_none()
    return row.doc_id if row else None


def add_connector_to_membership(connector_id: str, checksum: str, doc_id: str) -> None:
    """Insert a new membership row or append connector_id to an existing row's connector_ids array.

    - If checksum is new: INSERT (checksum, [connector_id], doc_id).
    - If checksum already exists (shared content): append connector_id to connector_ids array.
    """
    with transaction() as tx:
        tx.execute(
            text("""
                INSERT INTO connector_file_membership (checksum, connector_ids, doc_id)
                VALUES (:checksum, jsonb_build_array(:connector_id::text), :doc_id)
                ON CONFLICT (checksum) DO UPDATE
                  SET connector_ids = connector_file_membership.connector_ids
                      || jsonb_build_array(EXCLUDED.connector_ids->0)
                WHERE NOT (connector_file_membership.connector_ids
                           @> jsonb_build_array(:connector_id::text))
            """),
            {"checksum": checksum, "connector_id": connector_id, "doc_id": doc_id},
        )


def remove_connector_from_membership(connector_id: str, checksum: str) -> tuple[int, str | None]:
    """Remove connector_id from the owners array; return (remaining_owner_count, doc_id).

    Caller must delete the membership row and associated document when remaining_owner_count == 0.
    """
    with transaction() as tx:
        tx.execute(
            text("""
                UPDATE connector_file_membership
                SET connector_ids = connector_ids - :connector_id
                WHERE checksum = :checksum
            """),
            {"connector_id": connector_id, "checksum": checksum},
        )
        row = tx.execute(
            text("""
                SELECT jsonb_array_length(connector_ids) AS remaining, doc_id
                FROM connector_file_membership
                WHERE checksum = :checksum
            """),
            {"checksum": checksum},
        ).one_or_none()
    if row is None:
        return 0, None
    return int(row.remaining), row.doc_id
```

---

### 5.3 Connector Lifecycle DB Operations

This section describes the **DB-only operations** for each phase of the connector lifecycle. Transport concerns (SSH connections, boto3 calls, file downloads) are out of scope here — this is purely the sequence of reads and writes to PostgreSQL.

---

#### 5.3.1 Attach — `POST /v1/connectors`

Goal: persist connector config so the worker can start and begin syncing.

```text
DB operations (Attach)
────────────────────────────────────────────────────────────────────
1. INSERT INTO active_connectors
       (id, type, host, connection_details, allowed_extensions,
        sync_interval_seconds, attached_at, sync_status)
   VALUES (:connector_id, :type, :host, :encrypted_details, :exts,
           :interval, NOW(), 'idle')
   ON CONFLICT (id) DO NOTHING          ← 409 if already exists

Result: one row in active_connectors; worker thread starts.
connector_file_membership is empty for this connector — populated on first tick.
```

---

#### 5.3.2 Sync Tick — DB operations only

A tick has five DB phases. Transport (file listing, download) happens between phases 2 and 3.

```text
DB operations (Sync Tick)
────────────────────────────────────────────────────────────────────

Phase 1 — open tick record
  INSERT INTO connector_sync_history
      (connector_id, sync_id, started_at, sync_status)
  VALUES (:connector_id, :next_sync_id, NOW(), 'syncing')

  UPDATE active_connectors
  SET    sync_status = 'syncing'
  WHERE  id = :connector_id

Phase 2 — load known state
  SELECT checksum
  FROM   connector_file_membership
  WHERE  connector_ids @> jsonb_build_array(:connector_id::text)
  → produces: known_checksums  (set of strings)

  ┌─ scanner file walk happens here (no DB) ──────────────────────┐
  │  yields: scanned_files = [(remote_path, checksum), ...]       │
  └───────────────────────────────────────────────────────────────┘

Phase 3 — classify files into two lists (no DB reads)
  skip_list   = []   ← checksum IN known_checksums
  ingest_list = []   ← checksum NOT IN known_checksums

  for (remote_path, checksum) in scanned_files:
    if checksum in known_checksums:
      skip_list.append((remote_path, checksum))
    else:
      ingest_list.append((remote_path, checksum))    ← dedup: first path wins

  Note: files on skip_list have their connector_id already in connector_ids —
  no DB write is needed for them (idempotency guaranteed by existing membership row).

  ┌─ download + ingest loop for ingest_list (transport + create_job) ─┐
  │  for each (remote_path, checksum) in ingest_list:                 │
  │    download → create_job(connector_id) → doc_id                   │
  │    ↓ (DB write, see below)                                        │
  └───────────────────────────────────────────────────────────────────┘

Phase 4a — register each newly ingested file
  (called per file, after each successful create_job)

  INSERT INTO connector_file_membership (checksum, connector_ids, doc_id)
  VALUES (:checksum, jsonb_build_array(:connector_id::text), :doc_id)
  ON CONFLICT (checksum) DO UPDATE
    SET connector_ids = connector_file_membership.connector_ids
                     || jsonb_build_array(:connector_id::text)
  WHERE NOT (connector_file_membership.connector_ids
             @> jsonb_build_array(:connector_id::text))
  -- First owner:  inserts new row.
  -- Shared-content case (another connector already owns this checksum):
  --   appends connector_id to array; doc_id from the existing row is reused.
  --   The file was NOT downloaded again — the ingest was skipped upstream.

  UPDATE documents
  SET    metadata = metadata || :source_metadata   -- source_checksum, source_type, etc.
  WHERE  doc_id   = :doc_id

Phase 4b — orphan detection + removal
  (runs once, after ALL ingest jobs for this tick have completed)

  -- Compute orphan set purely in application memory:
  scanned_checksums  = {checksum for (_, checksum) in scanned_files}
  orphan_checksums   = known_checksums − scanned_checksums
  -- These are checksums this connector owned at the start of the tick but whose
  -- corresponding file is no longer present on the remote source.

  for orphan_checksum in orphan_checksums:

    -- Step A: remove this connector from the owners array
    UPDATE connector_file_membership
    SET    connector_ids = connector_ids - :connector_id
    WHERE  checksum = :orphan_checksum

    -- Step B: check remaining owners
    SELECT jsonb_array_length(connector_ids) AS remaining, doc_id
    FROM   connector_file_membership
    WHERE  checksum = :orphan_checksum

    -- Step C: if no owners remain, delete the row and the document
    if remaining == 0:
      DELETE FROM connector_file_membership
      WHERE  checksum = :orphan_checksum

      DELETE /v1/documents/{doc_id}         ← calls digitize document API
      -- 200 / 204 / 404 all treated as success; 5xx logged and skipped

Phase 5 — close tick record
  UPDATE connector_sync_history
  SET    finished_at     = NOW(),
         files_found     = :files_found,
         files_completed = :files_completed,
         files_failed    = :files_failed,
         sync_status     = :final_status     -- 'completed' | 'N files failed to ingest' | ...
  WHERE  connector_id = :connector_id
    AND  sync_id      = :sync_id

  UPDATE active_connectors
  SET    last_sync_at = NOW(),
         sync_status  = :final_status
  WHERE  id = :connector_id
```

**Ordering guarantee:** Phase 4b (orphan removal) runs only after Phase 4a (all ingest jobs) completes. This prevents a race where a newly ingested file appears as an orphan because its membership row has not yet been written.

---

#### 5.3.3 Detach — `DELETE /v1/connectors/{connector_id}`

Goal: cleanly remove all connector-owned content and the connector row itself.

```text
DB operations (Detach)
────────────────────────────────────────────────────────────────────

Pre-condition check (not a DB operation)
  if worker tick is currently running → return 409 Conflict immediately

Step 1 — stop worker thread (not a DB operation)
  stop_event.set() → thread.join(timeout=30s)

Step 2 — snapshot owned checksums
  SELECT checksum, doc_id
  FROM   connector_file_membership
  WHERE  connector_ids @> jsonb_build_array(:connector_id::text)
  → produces: owned_rows = [(checksum, doc_id), ...]

Step 3 — remove ownership row by row
  for (checksum, doc_id) in owned_rows:

    BEGIN
      UPDATE connector_file_membership
      SET    connector_ids = connector_ids - :connector_id
      WHERE  checksum = :checksum

      SELECT jsonb_array_length(connector_ids) AS remaining
      FROM   connector_file_membership
      WHERE  checksum = :checksum
    COMMIT

    if remaining == 0:
      DELETE FROM connector_file_membership WHERE checksum = :checksum
      DELETE /v1/documents/{doc_id}      ← best-effort; 200/204/404 = success

Step 4 — delete connector row
  DELETE FROM active_connectors WHERE id = :connector_id
  -- CASCADE deletes connector_sync_history rows automatically.
  -- Any remaining connector_file_membership rows for this connector_id
  --   are already cleaned up in Step 3.

Step 5 — cleanup staging dirs (not a DB operation)
  rm -rf {staging_dir}/{connector_id}/
```

**Invariant:** after Step 4 completes, no row in `connector_file_membership` has `:connector_id` in its `connector_ids` array.

---

## 6. Scanner Abstraction

**New file:** `services/digitize/connector/base_scanner.py`

The worker uses a transport-agnostic scanner interface so sync logic does not depend on SFTP- or S3-specific code.

### 6.1 Responsibility split

Base scanner responsibilities:

- hold connector config
- decrypt encrypted credentials using the pod secret
- define the interface used by the worker

Subclass responsibilities:

- remote listing — yields `(remote_path, checksum)` pairs for **all** files found; classification into skip vs ingest is the worker's responsibility (see §8.4)
- file download on demand — `remote_path` is taken directly from the tuple passed by the worker
- connection lifecycle

> **Note:** dedup classification (skip vs ingest) and orphan detection are performed in the worker's `_classify()` method (§8.4), not in the scanner. The scanner returns the full remote file list; the worker decides what to do with each entry.

### 6.2 Class diagram

```text
BaseScanner
  ├─ connect()
  ├─ scan()    → list[(remote_path, checksum)]   # ALL remote files, no dedup filtering
  ├─ download_to(remote_path, local_path)
  └─ close()

BaseScanner
  ├─ SFTPScanner          (checksum = remotely-computed MD5, stored in connector_file_membership.checksum)
  └─ S3Scanner            (checksum = S3 ETag from list_objects_v2, stored in connector_file_membership.checksum)
```

### 6.3 Interface stub

```python
from abc import ABC, abstractmethod
from pathlib import Path


class BaseScanner(ABC):
    def __init__(self, config: dict) -> None:
        self._config = config

    @abstractmethod
    def connect(self) -> None:
        ...

    @abstractmethod
    def scan(self) -> list[tuple[str, str]]:
        """Return (remote_path, checksum) for ALL files found on the remote source.

        No dedup filtering is applied here — the full list is returned.
        The worker's _classify() method splits the result into skip_list,
        ingest_list, and orphan_checksums (see §8.4).
        """
        ...

    @abstractmethod
    def download_to(self, remote_path: str, local_path: Path) -> None:
        ...

    @abstractmethod
    def close(self) -> None:
        ...
```

### 6.4 Factory dispatch

Factory dispatch lives in `services/digitize/connector/scanner.py`:

- `ssh` → `SFTPScanner`
- `s3` → `S3Scanner`

This keeps `ConnectorSyncWorker` independent of transport type.

---

## 7. Concrete Scanners

### 7.1 SFTP scanner

**Modified file:** `services/digitize/connector/sftp_scanner.py`

Behavior:

- decrypt private key per tick
- connect with Paramiko (SFTP channel for listing/download, SSH channel for hashing)
- recursively walk the remote path
- ignore files whose extension is not allowed
- compute MD5 **on the remote host** via `ssh.exec_command()` — no file bytes are transferred during hashing
- download selected files into staging

> **Note — SFTP checksum is an MD5 digest.** The checksum for SFTP files is a hex MD5 digest (e.g. `"d41d8cd98f00b204e980..."`), computed remotely via `md5sum`, stored in `connector_file_membership.checksum`. For S3 files the same column stores the S3 ETag returned by `list_objects_v2` — both are treated uniformly as an opaque content fingerprint (see [§7.2](#72-s3-scanner)).

MD5 is obtained by running `md5sum` on the remote side:

```python
def _remote_md5(self, remote_file_path: str) -> str:
    """Execute md5sum on the remote host and return the hex digest."""
    _, stdout, _ = self._ssh.exec_command(f'md5sum "{remote_file_path}"')
    output = stdout.read().decode().strip()
    # md5sum output format: "<hex_digest>  <filename>"
    return output.split()[0]
```

SFTP scan sketch:

```python
def scan(self) -> list[tuple[str, str]]:
    """Return (remote_path, checksum) for ALL allowed files on the remote host.

    No filtering against known_checksums is done here.
    The worker's _classify() method determines which files to skip and which
    to ingest based on the connector's current connector_file_membership state.

    remote_path is the absolute string path on the remote host.
    checksum is a hex MD5 digest computed on the remote host via md5sum —
    stored in connector_file_membership.checksum alongside S3 ETags with
    no schema difference.
    """
    found = []
    for remote_file in self._walk_remote_tree():
        if not self._is_allowed(remote_file.path):
            continue
        remote_path: str = remote_file.path   # extract string path from SFTPAttributes
        checksum = self._remote_md5(remote_path)
        found.append((remote_path, checksum))
    return found
```

### 7.2 S3 scanner

**Modified file:** `services/digitize/connector/s3_scanner.py`

**Detailed design:** [S3 Scanner — Detailed Design Proposal](./s3-scanner-proposal.md)

Behavior:

- Auto-detect provider (AWS S3 or IBM COS) from `endpoint_url` hostname — no separate region field needed
- Build boto3 client per tick using `IBMCOSConnector._build_client()` (pure-Python, ppc64le compatible)
- List objects via `list_objects_v2` paginator — yields `(key, checksum)` where checksum = S3 ETag, at no extra API cost; **the full list is returned without filtering**
- Download files on demand (only those the worker places on `ingest_list`)
- Store checksum in `connector_file_membership.checksum` and in `documents.metadata.source_checksum`

**Classification flow (performed by worker, not scanner):**

```
list_objects_v2  →  (key, checksum) for ALL objects   # checksum = S3 ETag
                          │
                    _classify(scanned_files, known_checksums)  ← in worker
                          │
          checksum IN known_checksums?
                    │
          YES ──────┘                    NO
           ▼                              ▼
      skip_list                      ingest_list
      (no download,                  download_fileobj() + create_job()
       no DB write)                  add_connector_to_membership(connector_id, checksum, doc_id)
                                     SET documents.metadata.source_checksum = checksum
```

S3 scan sketch:

```python
def scan(self) -> list[tuple[str, str]]:
    """Return (key, checksum) for ALL allowed objects in the bucket/prefix.

    No filtering is applied — the full list is returned.
    The worker's _classify() method determines what to skip and what to ingest.

    key is the S3 object key (remote path). checksum is the S3 ETag from
    list_objects_v2 — stored in connector_file_membership.checksum.
    """
    all_files = []
    for key, checksum in self._list_document_keys():   # checksum = S3 ETag, free from list_objects_v2
        all_files.append((key, checksum))
    return all_files


def download_and_register(self, key: str, checksum: str, staging_dir: Path) -> str:
    """Download key, run ingest, register checksum + metadata, return doc_id."""
    local_path = staging_dir / Path(key).name
    with open(local_path, "wb") as fh:
        writer = _HashingWriter(fh)                # inline MD5 — no second read
        self._client.download_fileobj(
            Bucket=self._cfg.bucket_name,
            Key=key,
            Fileobj=writer,
        )
    # checksum = S3 ETag from list_objects_v2 — stored in connector_file_membership.checksum.
    # For single-part: local MD5 == checksum (sanity check available).
    # For multi-part:  local MD5 != checksum (different formula — expected).
    doc_id = run_ingest_pipeline(local_path, connector_id=self._connector_id)
    # connector_file_membership insert is performed by the main thread via the
    # create-job flow (connector_id is forwarded) — do NOT call add_connector_to_membership
    # here; it is handled after job completion. file_checksum_registry is never touched
    # by connector code.
    set_document_metadata(doc_id, {
        "source_checksum": checksum,
        "source_type":     "s3",
        "bucket":          self._cfg.bucket_name,
        "key":             key,
    })
    return doc_id
```

**boto3 client construction** (provider auto-detected from `endpoint_url`):

```python
is_aws = "amazonaws.com" in cfg.endpoint_url

client = boto3.Session(
    aws_access_key_id=cfg.access_key_id,
    aws_secret_access_key=cfg.secret_access_key,
    region_name=_region_from_endpoint(cfg.endpoint_url),
).client(
    "s3",
    config=botocore.config.Config(
        signature_version="s3v4",
        s3={"addressing_style": "auto" if is_aws else "path"},
    ),
    # AWS S3: omit endpoint_url — boto3 auto-resolves virtual-hosted style.
    # IBM COS: forward endpoint_url — required for path-style addressing.
    **({} if is_aws else {"endpoint_url": cfg.endpoint_url}),
)
```

---

## 8. Sync Worker

**Modified file:** `services/digitize/connector/sync_worker.py`

`ConnectorSyncWorker` owns the end-to-end sync loop for one connector.

### 8.1 Tick flow

```text
_run_tick()
│
├─ [Phase 1] INSERT connector_sync_history (status='syncing')
│            UPDATE active_connectors (sync_status='syncing')
│
├─ [Phase 2] known_checksums ← SELECT FROM connector_file_membership
│                               WHERE connector_ids @> [connector_id]
│
│            ┌── scanner.connect() + scanner.scan() ─────────────────┐
│            │   walks remote source, computes (remote_path, checksum)│
│            │   per file; returns ALL remote files (no filtering)    │
│            └─────────────────────────────────────────────────────── ┘
│
├─ [Phase 3] _classify(scanned_files, known_checksums)
│            → skip_list   [(remote_path, checksum)] checksum IN known_checksums
│            → ingest_list [(remote_path, checksum)] checksum NOT IN known_checksums
│                          (intra-tick dedup: first path wins per checksum)
│
├─ [Phase 4a] _process_new_files(ingest_list)
│             for each (remote_path, checksum) in ingest_list:
│               download → create_job(connector_id) → doc_id
│               add_connector_to_membership(connector_id, checksum, doc_id)
│                 INSERT … ON CONFLICT DO UPDATE (JSONB array append)
│               UPDATE documents.metadata (source_checksum, source_type, …)
│
├─ [Phase 4b] _delete_orphans(orphan_checksums)
│   ← RUNS AFTER all Phase 4a jobs finish ←
│             orphan_checksums = known_checksums − scanned_checksums
│             for each orphan:
│               remove_connector_from_membership(connector_id, orphan)
│                 UPDATE connector_ids array  (- connector_id)
│               if remaining_owners == 0:
│                 DELETE FROM connector_file_membership WHERE checksum = orphan
│                 DELETE /v1/documents/{doc_id}
│
└─ [Phase 5] UPDATE connector_sync_history (finished_at, counters, status)
             UPDATE active_connectors (last_sync_at, sync_status)
```

### 8.2 Worker rules

- Overlapping ticks are skipped by a tick guard.
- `files_syncing` is updated live during staging and download.
- Staging uses per-tick temporary directories.
- Download and ingest are blocking operations.
- Fatal errors mark the tick as failed.
- Per-file failures are counted and summarized instead of failing the whole connector permanently.
- **Phase 4b (orphan removal) always runs after Phase 4a (all ingest jobs) completes.** This prevents a file ingested in the current tick from appearing as an orphan before its membership row is written.

### 8.3 Worker stub

```python
def _run_tick(self) -> None:
    self.config = get_active_connector(self.connector_id)  # refresh before each tick
    sync_id = insert_sync_history(self.connector_id)
    scanner = build_scanner(self.config)
    try:
        scanner.connect()

        # Phase 2: load known state from connector_file_membership.
        # Array-containment query returns all checksums this connector owns.
        known_checksums: set[str] = set(list_connector_checksums(self.connector_id))

        # scanner.scan() walks the remote source and returns ALL (remote_path, checksum)
        # pairs — it does NOT pre-filter against known_checksums here.
        # _classify() does the split into skip_list / ingest_list (Phase 3).
        scanned_files: list[tuple[str, str]] = scanner.scan()

        # Phase 3: classify into skip_list (already owned) and ingest_list (new).
        # Also produces orphan_checksums (owned but no longer on remote source).
        ingest_list, orphan_checksums = self._classify(scanned_files, known_checksums)

        # Phase 4a: download + ingest + register membership for each new file.
        # Runs BEFORE orphan removal so membership rows exist before we compute orphans.
        # Each successful job calls add_connector_to_membership(connector_id, checksum, doc_id).
        # file_checksum_registry is never touched.
        self._process_new_files(sync_id, scanner, ingest_list)

        # Phase 4b: remove orphaned files — runs only after 4a is fully complete.
        self._delete_orphans(orphan_checksums)

        self._complete_tick(sync_id)
    except Exception as exc:
        self._fail_tick(sync_id, exc)
    finally:
        scanner.close()
```

### 8.4 Classify — skip list, ingest list, and orphan set

`_classify` (previously `_diff`) receives the full scanner output and the connector's `known_checksums` snapshot, then produces three collections:

| Collection | Type | Contents |
| --- | --- | --- |
| `skip_list` | `list[tuple[str, str]]` | `(remote_path, checksum)` pairs where checksum is already owned — **no download, no ingest** |
| `ingest_list` | `list[tuple[str, str]]` | `(remote_path, checksum)` pairs for genuinely new files that must be downloaded and ingested |
| `orphan_checksums` | `set[str]` | checksums this connector previously owned that are no longer present on the remote source |

**Skip list:** files whose checksum is already in `known_checksums`. These are placed on `skip_list` and never reach `create_job`. Because `connector_ids` already contains this connector's ID (it is part of `known_checksums`), no DB write is required for them.

**Ingest list:** files whose checksum is NOT in `known_checksums`. Intra-tick dedup is applied: if two remote paths share the same checksum, only the **first** `(remote_path, checksum)` pair is placed on `ingest_list`; duplicates are dropped silently. This prevents double-ingestion of identical content within one tick.

**Orphan set:** computed purely in application memory as `known_checksums − scanned_checksums`. These are checksums that were registered at the start of the tick but whose file no longer exists on the remote source. Orphan removal (Phase 4b) runs **after** all Phase 4a ingest jobs complete, so a file that was just ingested this tick can never incorrectly appear as an orphan.

```python
def _classify(
    self,
    scanned_files: list[tuple[str, str]],
    known_checksums: set[str],
) -> tuple[list[tuple[str, str]], set[str]]:
    """Split the scanner output into (ingest_list, orphan_checksums).

    skip_list is not returned — callers do not need it; files on skip_list
    require no further action (membership row already up-to-date).

    ingest_list — (remote_path, checksum) pairs to download and ingest.
      - Checksum is NOT in known_checksums (genuinely new to this connector).
      - Intra-tick dedup: if two remote paths share the same checksum,
        only the first occurrence is included; subsequent duplicates are dropped.

    orphan_checksums — checksums in known_checksums that are absent from
      the current scan. Phase 4b removes connector_id from their ownership
      arrays and deletes docs whose array becomes empty.
    """
    scanned_checksums: set[str] = set()
    seen_this_tick: set[str] = set()
    ingest_list: list[tuple[str, str]] = []

    for remote_path, checksum in scanned_files:
        scanned_checksums.add(checksum)
        if checksum not in known_checksums:
            if checksum not in seen_this_tick:   # intra-tick dedup
                seen_this_tick.add(checksum)
                ingest_list.append((remote_path, checksum))
        # files in known_checksums → skip_list (not returned, no action needed)

    orphan_checksums = known_checksums - scanned_checksums
    return ingest_list, orphan_checksums
```

> **Dedup invariant:** after `_classify`, every checksum in `ingest_list` is unique. The chosen remote path is the first path encountered during the scan walk (depth-first for SFTP, iteration order of `list_objects_v2` for S3).
>
> **Ordering invariant:** `_delete_orphans(orphan_checksums)` is called only after `_process_new_files(ingest_list)` returns. This guarantees that a checksum added to `connector_file_membership` in Phase 4a is never simultaneously processed as an orphan in Phase 4b within the same tick.

---

## 9. Worker Manager

**Modified file:** `services/digitize/connector/worker_manager.py`

`ConnectorWorkerManager` owns worker thread lifecycle.

### 9.1 Responsibilities

- maintain `connector_id -> (thread, worker, stop_event)`
- start one daemon thread per connector on POST
- stop workers cooperatively on DELETE
- recover workers for persisted connectors on startup

PUT does not restart workers — the running thread re-reads config from the DB before entering the next tick.

### 9.2 Lifecycle

![Worker Manager Lifecycle and Thread Lifecycle](thread-lifecycle-combined.svg)

---

## 10. Thread Lifecycle and Resilience

Thread behavior is cooperative, observable, and restartable.

### 10.1 Threading Model

`ConnectorWorkerManager` is a module-level singleton inside the single Uvicorn process. Every sync thread is a `daemon=True` `threading.Thread` within that same process.

A sync thread crash affects only that connector — the asyncio event loop and other connector threads keep running. The dead thread's slot in `_workers` remains with `stop_event` unset, which is the crash state detected by §10.4.

### 10.2 Thread Crash & Status

A crash is an unhandled exception that escapes the outer `while` loop in `ConnectorSyncWorker.run()` — `thread.is_alive() == False` with `stop_event` not set.

The crash guard lives outside the loop and must:

1. Write `"crashed: <error>"` to `active_connectors.sync_status`.
2. Close any open `"syncing"` history row with the same status.

```python
# ConnectorSyncWorker.run() — outer crash guard
try:
    while not stop_event.is_set():
        ...  # tick loop
except Exception as exc:
    log.error(f"Worker {connector_id} crashed: {exc}", exc_info=True)
    if _tick_running and _current_sync_id:
        update_sync_history(…, sync_status=f"crashed: {exc}")
    update_connector_sync_status(…, f"crashed: {exc}")
    # thread exits; is_alive() → False
```

`sync_status` is a free-form `TEXT` column — no schema change needed.

### 10.3 Respawn Logic

When the monitor detects a crashed thread it respawns the worker with exponential back-off, bounded to 5 attempts.

Respawn steps:

1. Remove stale `_workers` entry.
2. Read config from DB — skip if the connector was deleted.
3. Increment `crash_count`.
4. If `crash_count > 5`: write `"respawn limit reached"` and stop.
5. Sleep `min(2^crash_count, 300)` seconds.
6. Call `start_worker(config)` — new thread starts fresh.

Back-off schedule:

| Crash # | Delay |
| --- | --- |
| 1st | 2 s |
| 2nd | 4 s |
| 3rd | 8 s |
| 4th | 16 s |
| 5th | 32 s |
| > 5th | No respawn |

The crash counter resets to `0` after the first clean tick following a respawn.

### 10.4 Generic Thread Monitor

One daemon thread (`connector-monitor`) watches all workers, polling every 30 s.

For each entry in `_workers`:
- `stop_event` set → graceful stop in progress; skip.
- `thread.is_alive()` → healthy.
- thread dead, `stop_event` not set → crash detected; call `schedule_respawn()`.

Log levels: `DEBUG` for healthy, `ERROR` for crash or respawn limit.

Started and stopped in `lifespan()`:

```python
_monitor_stop = threading.Event()
monitor_thread = threading.Thread(
    target=connector_worker_manager.run_monitor,
    args=(_monitor_stop,),
    daemon=True,
    name="connector-monitor",
)
monitor_thread.start()
yield
_monitor_stop.set()
monitor_thread.join(timeout=10)
```

### 10.5 Interrupt Thread on DELETE

`_run_tick()` checks `stop_event` at each inter-phase boundary via `_cancel_if_requested()`. When set mid-tick, the helper:

1. Closes active connections.
2. Deletes pending (in-flight) checksum rows.
3. Removes membership rows and OpenSearch docs for files ingested this tick.
4. Closes the sync-history row as `"interrupted: connector deleted"`.

Check points:

```
_run_tick():
  step 1 — connect & scan       → _cancel_if_requested()
  step 2 — dedup pass           → _cancel_if_requested()
  step 3 — batch ingest loop    → _cancel_if_requested() after each batch
  step 4 — orphan deletes       → _cancel_if_requested()
  step 5 — finalise (normal path)
```

Two in-tick tracking collections:

| Variable | Holds | Populated |
| --- | --- | --- |
| `pending_checksums` | checksum values pre-written with `NULL doc_id` | Before each download; removed on success |
| `ingested_this_tick` | `(checksum, doc_id)` pairs | After each successful ingest |

DELETE flow:

1. `stop_event.set()` — sleeping thread wakes immediately; ticking thread stops at next boundary.
2. `thread.join(timeout=30 s)`.
3. If still alive after timeout: log warning and proceed with cleanup best-effort.

Key invariants:

| Invariant | Mechanism |
| --- | --- |
| Interval never interrupts a running tick | Timer checked only after `_run_tick()` returns |
| DELETE cooperatively interrupts at next boundary | `_cancel_if_requested()` after each phase |
| No concurrent ticks per connector | Single-threaded loop + tick guard |
| Monitor skips intentionally stopped threads | `if stop_event.is_set(): skip` |

### 10.6 Lifespan Recovery

On FastAPI startup: load `active_connectors`, recreate workers, start the monitor. No catalog re-push needed.


---

## 11. Implementation Plan — Digitize Connector PRs

Each PR is independently testable — no PR leaves things in a broken or untestable state.

---

### PR 1 — DB Schema + ORM Models + Settings

**Files touched:** `init_schema.sql`, `db/models.py`, `config/settings.py`

**What's already done:**
- `file_checksum_registry` table exists in `init_schema.sql` — rename the `sha256` column to `checksum` (MD5); it remains user-submitted only
- `FileChecksumRegistry` ORM model exists in `db/models.py` — rename the `sha256` field to `checksum`

**What's to build:**
- 3 new tables: `active_connectors`, `connector_file_membership`, `connector_sync_history`
  - `connector_file_membership` schema: `checksum TEXT PRIMARY KEY`, `connector_ids JSONB NOT NULL DEFAULT '[]'`, `doc_id TEXT NOT NULL REFERENCES documents(doc_id)` — no FK to `file_checksum_registry`
- 3 new ORM models: `ActiveConnector`, `ConnectorFileMembership` (checksum PK, connector_ids JSONB, doc_id FK), `ConnectorSyncHistory`
- Settings entries: staging directory, worker stop timeout, monitor poll interval, respawn back-off cap, `CONNECTOR_SYNC_INTERVAL_SECONDS` (default `300`, written into `active_connectors.sync_interval_seconds` on connector creation)

**How to test:**
- Run `init_schema.sql` against a local/test DB and assert all 3 new tables exist with correct columns, constraints, and indexes
- Assert `connector_file_membership` has no FK to `file_checksum_registry`
- Assert `connector_file_membership.checksum` is the sole PK and `connector_ids` is a JSONB column
- Unit test: instantiate ORM models and map them against the schema

---

### PR 2 — DB Operations Layer

**Files touched:** `services/digitize/utils/db.py`

**What's already done:**
- `upsert_file_checksum(checksum, doc_id)` in `db/manager.py` — **do not modify** beyond the parameter rename; user-submitted only
- `find_completed_document_by_hash(checksum)` in `db/manager.py` — **do not modify** beyond the parameter rename; user-submitted only

**What's to build:**
All connector DB functions from §5.1:
`insert_active_connector`, `upsert_active_connector`, `get_active_connector`, `list_active_connectors`, `delete_active_connector`, `update_connector_sync_status`, `merge_connection_details`, `lookup_connector_content_by_checksum`, `list_connector_checksums`, `add_connector_to_membership`, `remove_connector_from_membership`, `insert_sync_history`, `update_sync_history`, `update_sync_history_files_syncing`, `list_sync_history`

**How to test:**
- Unit tests per function against a test DB (real or in-memory with pg_testcontainer / SQLite shim)
- Assert `add_connector_to_membership` inserts a new row on first call and appends the connector_id on subsequent calls with the same checksum
- Assert `remove_connector_from_membership` returns `remaining=0` when the last connector is removed, and `remaining>0` when others still own the checksum
- Assert `list_connector_checksums` uses array-containment (`@>`) and returns only checksums owned by the given connector
- Assert `merge_connection_details` correctly merges keys without clobbering untouched fields
- Assert that `lookup_connector_content_by_checksum` queries `connector_file_membership` and never touches `file_checksum_registry`

---

### PR 3 — REST API Endpoints

**Files touched:** connector router/handler file(s)

**What's to build:**
- `POST /v1/connectors` — validate body, encrypt secrets, call `insert_active_connector`, return `202`
- `PUT /v1/connectors/{id}` — partial update, re-encrypt if credentials included, `merge_connection_details`, return `200`
- `DELETE /v1/connectors/{id}` — stub only (stops at "would stop worker" + calls DB delete), no worker logic yet
- `GET /v1/connectors` and `GET /v1/connectors/{id}` — read from DB, strip secret fields
- `GET /v1/connectors/{id}/sync-history` — paginated query with `limit`/`offset`
- **Update `GET /v1/documents` and `GET /v1/documents/{doc_id}`** to exclude connector-sourced docs via `NOT EXISTS (SELECT 1 FROM connector_file_membership WHERE doc_id = ...)` filter
- **Update `DELETE /v1/documents/{doc_id}`** to return `404` for connector-sourced docs

**How to test:**
- Integration tests using `httpx.AsyncClient` + test DB
- Assert secrets are never returned in GET responses
- Assert `PUT` with partial `connection_details` only overwrites provided keys
- Assert correct HTTP status codes for 404/409/401 paths
- Assert `GET /v1/documents` does not return docs whose `doc_id` appears in `connector_file_membership`
- Assert `GET /v1/documents/{doc_id}` returns `404` for a connector-sourced doc
- Assert `DELETE /v1/documents/{doc_id}` returns `404` for a connector-sourced doc
- Assert `GET /v1/jobs` returns **all** jobs including connector-initiated ones

---

### PR 4 — Scanner Abstraction + SFTP Scanner

**Files touched:** `connector/base_scanner.py`, `connector/scanner.py`, `connector/sftp_scanner.py`

**What's to build:**
- `BaseScanner` ABC with `connect()`, `scan()`, `download_to()`, `close()`
- `build_scanner()` factory — dispatches on `type`
- `SFTPScanner` — Paramiko connection (SFTP + SSH), recursive walk, extension filter, remote MD5 via `ssh.exec_command(f'md5sum "{remote_file_path}"')`, staged download; `scan()` returns **all** files (no filtering)

**How to test:**
- Unit test `SFTPScanner` against a local mock SFTP server (e.g. `pytest-sftpserver` or `paramiko.SFTPServer` in a thread)
- Assert extension filtering works correctly
- Assert MD5 is computed via remote `md5sum` exec (not by streaming bytes) and matches expected digest
- Assert `scan()` returns ALL allowed remote files without filtering against any known_checksums
- Assert `build_scanner("ssh", config)` returns an `SFTPScanner` instance

---

### PR 5 — S3 Scanner

**Files touched:** `connector/s3_scanner.py`

**What's to build:**
- `S3Scanner` — boto3 `list_objects_v2` paginator, extension filter, `download_fileobj()`, staged download; `scan()` returns **all** allowed objects without dedup filtering; checksum (S3 ETag) registered via `add_connector_to_membership()` (called by main thread on job completion) and stored in `documents.metadata.source_checksum`. `upsert_file_checksum()` must NOT be called.

**How to test:**
- Unit test with `moto` (mock AWS) — assert listing, extension filtering, and download
- Assert `scan()` returns ALL allowed objects without pre-filtering against known_checksums
- Assert `build_scanner("s3", config)` returns an `S3Scanner` instance
- Reuse same test interface as PR 4 to verify factory dispatch covers both types

---

### PR 6 — Sync Worker

**Files touched:** `connector/sync_worker.py`

**What's to build:**
- `ConnectorSyncWorker` with full `_run_tick()`: config refresh, scan, diff, ingest new files, orphan deletion, tick finalize
- `_classify()`: splits `scanned_files` into `ingest_list` (new files, intra-tick dedup: first path wins) and `orphan_checksums` (known checksums absent from current scan) — see §8.4; `skip_list` is implicit (not returned)
- Pass `connector_id` to each create-job call inside `_process_new_files()`; the main thread calls `add_connector_to_membership(connector_id, checksum, doc_id)` on job completion (see §5.1) — `upsert_file_checksum` must NOT be called for connector jobs
- Tick guard to prevent overlapping ticks
- Crash guard (outer `try/except` in `run()`) — writes `crashed:` status to DB
- `_cancel_if_requested()` check points at each phase boundary
- `pending_checksums` / `ingested_this_tick` rollback tracking for mid-tick DELETE interruption

**How to test:**
- Unit tests with mocked scanner and mocked DB layer
- Assert tick guard skips when a tick is already running
- Assert crash guard writes `"crashed: <error>"` to DB on unhandled exception
- Assert `stop_event.set()` before a tick results in no DB writes
- Assert mid-tick cancellation cleans up `pending_checksums` and `ingested_this_tick`
- `_classify` — assert that when two remote paths share the same checksum only the first is included in `ingest_list`
- `_classify` — assert that checksums absent from the current scan appear in `orphan_checksums`
- `_classify` — assert that a checksum present in both `scanned_files` and `known_checksums` does not appear in `ingest_list` and does appear in neither `ingest_list` nor `orphan_checksums`
- Assert `_delete_orphans()` is called only after `_process_new_files()` returns (Phase 4b ordering invariant)

---

### PR 7 — Worker Manager + Lifespan Recovery + Monitor

**Files touched:** `connector/worker_manager.py`, `main.py` (lifespan hook)

**What's to build:**
- `ConnectorWorkerManager` singleton: `start_worker()`, `stop_worker()`, `_workers` map
- `connector-monitor` daemon thread: 30s poll, crash detection, `schedule_respawn()` with exponential back-off up to 5 attempts
- Lifespan recovery: on startup load all `active_connectors`, recreate workers, start monitor; on shutdown stop monitor
- Wire `DELETE /v1/connectors` to call `stop_worker()` (completing the stub from PR 3)

**How to test:**
- Unit test `start_worker()` / `stop_worker()` with a no-op worker
- Assert `stop_event.set()` + `thread.join()` is called on DELETE
- Test monitor: inject a dead thread with `stop_event` unset, assert `schedule_respawn()` is called
- Assert back-off delays are computed correctly per crash count
- Assert crash counter resets after first clean tick post-respawn
- Integration test: start the full app with a test connector, confirm the worker starts and the lifespan hook restores it on simulated restart
