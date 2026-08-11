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
- `/run/secrets/connector_encryption_key` is mounted before pod start.
- The `document_checksum` table and `DocumentChecksum` ORM model are already implemented (user-submitted documents only). Connector code must never read from or write to it — connector dedup is handled exclusively via `connector_document_checksum` (see §4).

---

## 2. System Overview

### 2.1 Architecture Diagram

![Architecture Diagram](architecture-diagram.svg)

### 2.2 Runtime Flow

```text
── Attach (POST /v1/connectors) ─────────────────────────────────────
  → validate + encrypt credentials
  → INSERT connectors row
  → schedule worker startup (background task)
  → worker thread starts and runs first tick immediately

── Config update (PUT /v1/connectors/{id}) ──────────────────────────
  → merge + re-encrypt changed fields
  → UPDATE connectors row
  → running worker re-reads config from DB before its next tick

── Each sync tick (connector-sourced) ───────────────────────────────
  Step 1 — load state
    → SELECT known_checksums FROM connector_document_checksum
           WHERE connector_id = :connector_id
    → SELECT DISTINCT checksum FROM connector_document_checksum
      (all checksums across all connectors — for cross-connector dedup)

  Step 2 — file walk + classify
    → scanner walks remote source, computes (remote_path, checksum) per file
    → for each file:
        if checksum IN known_checksums:
          → already owned by this connector — skip entirely
        elif checksum IN all_checksums:
          → already ingested by a different connector — no download, no ingest;
            existing_doc_id = lookup_connector_content_by_checksum(checksum)
            add_connector_checksum_entry(connector_id, checksum, existing_doc_id)
        else:
          → brand new to all connectors — place on ingest_list

  Step 3 — ingest new files (ingest_list)
    → for each (remote_path, checksum) in ingest_list:
        download file → run create_job(
                            connector_id=connector_id,
                            checksum=checksum ← pre-computed by scanner; skips re-hash in pipeline
                        )
        on job success:
          add_connector_checksum_entry(connector_id, checksum, doc_id)

  Step 4 — orphan detection + removal
    → orphan_checksums = known_checksums − {checksum for (_, checksum) in scanned_files}
    → for each orphan_checksum (after all Step 3 writes finish):
        remove_connector_checksum_entry(connector_id, orphan_checksum)
        if remaining_owner_count == 0:
          DELETE /v1/documents/{doc_id}

  Step 5 — finalise tick
    → UPDATE connector_sync_logs (total_files, new_files, removed_files, failed_files, status)
    → UPDATE connectors (last_sync_at, sync_status)

── Detach (DELETE /v1/connectors/{id}) ──────────────────────────────
  → guard: reject with 409 if a tick is currently running
  → list all checksums owned by this connector
  → for each checksum: remove_connector_checksum_entry → delete doc if last owner
  → DELETE connectors row
  → cleanup staging dirs
  → stop worker thread
```

### 2.3 Main Components

- `connectors`: current connector configuration and top-level sync state
- `connector_document_checksum`: **connector-sourced documents only** — one row per `(checksum, connector_id)` pair; carries the `doc_id` for deletion
- `connector_sync_logs`: one row per worker tick
- `ConnectorWorkerManager`: owns worker thread lifecycle
- `ConnectorSyncWorker`: executes periodic sync logic
- Scanner implementations: transport-specific remote access for SFTP and S3; S3 scanner derives the checksum from the S3 ETag returned by `list_objects_v2`; SFTP scanner uses a remotely-computed MD5 — both stored as `checksum` in `connector_document_checksum`

---

## 3. API Contract


### 3.1 `POST /v1/connectors`

Creates a connector, stores encrypted credentials, persists config, and schedules worker startup asynchronously. The worker runs its first tick immediately after the thread starts.

#### Request body

Common fields:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `connector_id` | `string (UUID)` | ✅ | Stable catalog ID |
| `connector_name` | `string` | ✅ | Human-readable unique name for the connector (e.g. `"prod-sftp-reports"`). Used as a stable display label. |
| `type` | `string` | ✅ | `ssh` or `s3` |
| `allowed_extensions` | `array[string]` | ✅ | Non-matching files are ignored |
| `connection_details` | `object` | ✅ | Type-specific fields |

> **Note:** `sync_interval_seconds` is not accepted in the API payload. It is read from the `CONNECTOR_SYNC_INTERVAL_SECONDS` environment variable (default `300`) and applies uniformly to all connectors.

`connection_details` for `ssh`:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `host` | `string` | ✅ | SFTP hostname |
| `username` | `string` | ✅ | |
| `remote_path` | `string` | ✅ | |
| `private_key` | `string` | ✅ | |

`connection_details` for `s3`:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `endpoint_url` | `string` | ✅ | Full S3 endpoint URL. AWS S3: `https://s3.<region>.amazonaws.com`. IBM COS: `https://s3.<region>.cloud-object-storage.appdomain.cloud`. Provider and region are auto-detected from this URL — no separate `region` field needed. |
| `bucket_name` | `string` | ✅ | |
| `access_key_id` | `string` | ✅ | IAM key ID (AWS) or HMAC key ID (IBM COS) |
| `secret_access_key` | `string` | ✅ | IAM secret (AWS) or HMAC secret (IBM COS) |
| `prefix` | `string` | ❌ | Key prefix to scope listing — empty means bucket root |
| `delimiter` | `string` | ❌ | Set `"/"` for non-recursive (immediate children only) |

> **Checksum-based dedup:** For S3 connectors, `list_objects_v2` returns the object ETag at no extra API cost — stored as `checksum` in `connector_document_checksum`. If the checksum is already present for this connector the file is **never downloaded**.

#### Example payloads

```json
{
  "connector_id": "c7f3a2d1-...",
  "connector_name": "prod-sftp-reports",
  "type": "ssh",
  "allowed_extensions": [".pdf", ".docx"],
  "connection_details": {
    "host": "sftp.example.com",
    "username": "sync_user",
    "remote_path": "/exports/reports",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n..."
  }
}
```

```json
{
  "connector_id": "a1b2c3d4-...",
  "connector_name": "prod-s3-rag-docs",
  "type": "s3",
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
  "connector_name": "prod-cos-ai-services",
  "type": "s3",
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
| `409 Conflict` | Connector already exists (`connector_id` or `connector_name` already in use) |

### 3.2 `PUT /v1/connectors/{connector_id}`

Updates an existing connector's config in the database. The running worker is not restarted — it reads the latest config from the DB before entering the next tick.

Rules:

- All fields are optional.
- Omitted fields remain unchanged.
- `type` cannot change.
- `connector_name` can be updated; the new value must be unique across all connectors (`409 Conflict` otherwise).
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

> **Constraint — active sync tick:** DELETE is accepted **only when no sync tick is currently in progress** for the connector. If the worker is mid-tick, the request is rejected with `409 Conflict`. Once job cancellation is supported, DELETE will be allowed at any point in the connector lifecycle.

Delete flow:

1. **Guard:** check whether a sync tick is actively running for the connector. If yes, return `409 Conflict` — no state is modified.
2. Stop the worker.
3. Snapshot the connector's known checksums.
4. Remove membership rows checksum by checksum.
5. Delete documents only when the remaining reference count reaches zero.
6. Delete the `connectors` row.
7. Best-effort cleanup of staging directories.

#### Delete sequence diagram

```text
DELETE /v1/connectors/{connector_id}
  → check if sync tick is in progress
      if YES → 409 Conflict (no state modified)
  → stop worker
  → list checksums owned by this connector (connector_document_checksum WHERE connector_id = :connector_id)
  → for each checksum:
       remove_connector_checksum_entry(connector_id, checksum)
         → DELETE row WHERE checksum = :checksum AND connector_id = :connector_id
         → returns (remaining_owner_count, doc_id)
       if remaining_owner_count == 0:
         DELETE /v1/documents/{doc_id}
  → delete connector row
  → cleanup staging dirs: glob staging/connectors/<connector_id>-* and remove each match
```

Document deletion is best-effort: `200`, `204`, `404` from `DELETE /v1/documents/{doc_id}` are treated as success; `5xx` or network failures are logged and cleanup continues.

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
- `sync_status`, `last_sync_at`, `last_sync_error`, `attached_at`, `total_files`

#### Example response

```json
[
  {
    "connector_id": "c7f3a2d1-4e5b-4c6d-8f9a-0b1c2d3e4f5a",
    "connector_name": "prod-sftp-reports",
    "type": "ssh",
    "attached_at": "2025-01-10T08:00:00Z",
    "last_sync_at": "2025-01-15T14:32:10Z",
    "sync_status": "up to date",
    "last_sync_error": null,
    "total_files": 42
  },
  {
    "connector_id": "a1b2c3d4-5e6f-7a8b-9c0d-1e2f3a4b5c6d",
    "connector_name": "prod-s3-rag-docs",
    "type": "s3",
    "attached_at": "2025-01-12T09:15:00Z",
    "last_sync_at": "2025-01-15T14:30:00Z",
    "sync_status": "out of sync",
    "last_sync_error": "remote object listing timed out",
    "total_files": 150
  }
]
```

### 3.5 `GET /v1/connectors/{connector_id}`

Returns one connector plus the latest file-processing counters:

- `total_files`, `new_files`, `removed_files`, `failed_files`

Only non-secret `connection_details` are returned.

#### Example response

```json
{
  "connector_id": "c7f3a2d1-4e5b-4c6d-8f9a-0b1c2d3e4f5a",
  "connector_name": "prod-sftp-reports",
  "type": "ssh",
  "allowed_extensions": [".pdf", ".docx"],
  "sync_interval_seconds": 300,
  "attached_at": "2025-01-10T08:00:00Z",
  "last_sync_at": "2025-01-15T14:32:10Z",
  "sync_status": "up to date",
  "connection_details": {
    "host": "sftp.example.com",
    "username": "sync_user",
    "remote_path": "/exports/reports"
  },
  "total_files": 42,
  "new_files": 2,
  "removed_files": 0,
  "failed_files": 2
}
```

### 3.6 `GET /v1/connectors/{connector_id}/syncs`

Returns paginated tick history.

Query params:

| Param | Default | Notes |
| --- | --- | --- |
| `limit` | `50` | capped at `200` |
| `offset` | `0` | zero-based |

Each item contains: `id`, `started_at`, `finished_at`, `total_files`, `new_files`, `removed_files`, `failed_files`, `status`, `error`.

Status values: `started`, `completed`, `failed`.

At most one in-progress `syncing` row exists per connector.

#### Example response

```json
{
  "total": 3,
  "limit": 50,
  "offset": 0,
  "items": [
    {
      "id": 3,
      "started_at": "2025-01-15T14:32:00Z",
      "finished_at": "2025-01-15T14:32:10Z",
      "total_files": 42,
      "new_files": 0,
      "removed_files": 0,
      "failed_files": 0,
      "status": "completed",
      "error": ""
    },
    {
      "id": 2,
      "started_at": "2025-01-15T14:27:00Z",
      "finished_at": "2025-01-15T14:27:18Z",
      "total_files": 41,
      "new_files": 3,
      "removed_files": 0,
      "failed_files": 3,
      "status": "failed",
      "error": "3 files could not be processed"
    },
    {
      "id": 1,
      "started_at": "2025-01-15T14:22:00Z",
      "finished_at": null,
      "total_files": 0,
      "new_files": 5,
      "removed_files": 0,
      "failed_files": 0,
      "status": "started",
      "error": ""
    }
  ]
}
```

### 3.7 Digitize Document & Job API — Connector Visibility Rules

#### Document APIs (`/v1/documents`)

| Endpoint | Behaviour |
| --- | --- |
| `GET /v1/documents` | Returns **user-submitted documents only** — connector-sourced docs are excluded |
| `GET /v1/documents/{doc_id}` | Returns the document only if it was user-submitted; returns `404` for connector-sourced docs |
| `DELETE /v1/documents/{doc_id}` | Deletes the document only if it was user-submitted; returns `404` for connector-sourced docs |

**Rationale:** connector-sourced documents are managed exclusively through their data source. Exposing them via user-facing APIs would allow deletion without removal from the source, causing the file to be re-ingested on the next tick.

**Implementation:** a document is identified as connector-sourced when a row exists in `connector_document_checksum` for its `doc_id`. The DB query for user-facing document endpoints must add a `NOT EXISTS (SELECT 1 FROM connector_document_checksum WHERE doc_id = ...)` filter.

#### Job APIs (`/v1/jobs`)

| Endpoint | Behaviour |
| --- | --- |
| `GET /v1/jobs` (list) | Returns **all jobs** — both connector-sourced and user-submitted |
| `GET /v1/jobs/{job_id}` | Returns **all jobs** — connector job details are accessible |
| `DELETE /v1/jobs/{job_id}` | Deletes only job records — same rules apply regardless of origin |

The job list intentionally includes connector-initiated jobs so operators can observe sync progress and diagnose failures.

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
document_checksum (checksum PK) ───────────────────> documents

── Connector-sourced path ───────────────────────────────────────────
connectors
  └─< connector_document_checksum (connector_id, checksum, doc_id) ─> documents

connectors
  └─< connector_sync_logs
```

The two registries are **intentionally separate**: a file with the same content can legitimately exist in both — one row representing the user-uploaded copy and one row representing the connector-synced copy.

### 4.2 `connectors`

Stores connector config, encrypted credential blobs, and top-level sync state. The list endpoint `GET /v1/connectors` reads from this table alone.

```sql
CREATE TABLE IF NOT EXISTS connectors (
    id                      TEXT        PRIMARY KEY,
    name                    TEXT        NOT NULL UNIQUE,
    type                    TEXT        NOT NULL,
    connection_details      JSONB       NOT NULL DEFAULT '{}',
    allowed_extensions      JSONB       NOT NULL DEFAULT '[]',
    sync_interval_seconds   INTEGER     NOT NULL DEFAULT 300,
    attached_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_sync_at            TIMESTAMPTZ,
    sync_status             TEXT        NOT NULL DEFAULT 'up to date',
    last_sync_error         TEXT,
    total_files             INTEGER     NOT NULL DEFAULT 0,
    CONSTRAINT chk_connector_type CHECK (type IN ('ssh', 's3'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_connectors_name
    ON connectors (name);
```

> **Note:** `sync_interval_seconds` is stored per-connector for future extensibility but is not accepted via the API today. On `POST`, it is populated from the `CONNECTOR_SYNC_INTERVAL_SECONDS` environment variable (default `300`). The worker reads the value from the DB before each tick.

### 4.3 `connector_document_checksum`

**Connector-sourced documents only.** This table is the sole dedup and reference-counting store for all content ingested via connectors.

Each row represents **one connector's ownership of one checksum**. One checksum can appear in multiple rows (shared across connectors); one connector can appear in multiple rows (owns many files). `doc_id` is stored on every row so that deletion can proceed without a join.

```sql
CREATE TABLE IF NOT EXISTS connector_document_checksum (
    checksum     TEXT NOT NULL,
    connector_id TEXT NOT NULL,
    doc_id       TEXT NOT NULL,
    PRIMARY KEY (checksum, connector_id)
);

CREATE INDEX IF NOT EXISTS idx_cdc_connector_id
    ON connector_document_checksum (connector_id);
```

**Why `(checksum, connector_id)` is the PK:** the same file can be owned by multiple connectors simultaneously. The composite PK enforces a connector cannot register the same checksum twice, while still allowing multiple connectors to reference the same checksum.

**Why no `ON DELETE CASCADE` on `doc_id`:** deletion is an intentional, reference-counted operation managed in application code. Cascade deletion would bypass the reference-count check and potentially double-delete shared documents.

**Why `idx_cdc_connector_id`:** every sync tick and every detach queries all rows owned by a given connector. Without this index Postgres falls back to a full sequential scan over the entire table on every tick.

**Membership invariants:**

- **New file:** download and ingest, then insert `(checksum, connector_id, doc_id)` once the job completes.
- **Cross-connector duplicate:** look up the existing `doc_id`, skip download and ingest, insert `(checksum, connector_id, <existing_doc_id>)`.
- **Same-connector duplicate:** skip — no download, no ingest, no DB write.
- **Orphan:** delete the `(checksum, connector_id)` row; if no other rows remain for that checksum, delete the associated document.

**Checksum format reference:**

| Source | Value stored in `checksum` |
| --- | --- |
| S3 single-part | S3 ETag = `MD5(file_bytes)` — 32-char hex, no suffix |
| S3 multi-part | S3 ETag = `MD5(MD5(p₁)‖…‖MD5(pₙ))-N` — hex + `-N` suffix |
| SFTP | `md5sum` output from remote host — 32-char hex |

> **Document metadata:** when a document row is created the scanner writes the fingerprint into `documents.metadata`:
> ```json
> {
>   "source_checksum": "0234031ed6cb7d686152f45c38f41bc6-13",
>   "source_type": "s3",
>   "bucket": "ai-services",
>   "key": "reports/sg248590-2.pdf"
> }
> ```

### 4.4 `connector_sync_logs`

Persistent per-tick history backing the syncs API.

```sql
CREATE TABLE IF NOT EXISTS connector_sync_logs (
    connector_id     TEXT        NOT NULL,
    seq              INTEGER     NOT NULL,
    started_at       TIMESTAMPTZ NOT NULL,
    finished_at      TIMESTAMPTZ,
    total_files      INTEGER     NOT NULL DEFAULT 0,
    new_files        INTEGER     NOT NULL DEFAULT 0,
    removed_files    INTEGER     NOT NULL DEFAULT 0,
    failed_files     INTEGER     NOT NULL DEFAULT 0,
    status           TEXT        NOT NULL DEFAULT 'started',
    error            TEXT        NOT NULL DEFAULT '',
    PRIMARY KEY (connector_id, seq),
    CONSTRAINT fk_csh_connector
        FOREIGN KEY (connector_id)
        REFERENCES connectors(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_csl_connector_started
    ON connector_sync_logs (connector_id, started_at DESC);
```

### 4.5 ORM

`DocumentChecksum` already exists in `services/digitize/db/models.py` and remains unchanged. Three new models were added to the same file:

- `Connector` — maps to `connectors`; fields: `id` (PK), `name` (UNIQUE), `type`, `connection_details` (JSONB), `allowed_extensions` (JSONB), `sync_interval_seconds`, `attached_at`, `last_sync_at`, `sync_status`, `last_sync_error`, `total_files`; has a one-to-many relationship `sync_logs → ConnectorSyncLog`
- `ConnectorDocumentChecksum` — maps to `connector_document_checksum`; fields: `checksum` (NOT NULL), `connector_id` (NOT NULL), `doc_id` (NOT NULL); composite PK `(checksum, connector_id)`
- `ConnectorSyncLog` — maps to `connector_sync_logs`; fields: `connector_id` (FK → `connectors`, CASCADE DELETE), `seq` (auto-generated per connector — see §5.1); composite PK `(connector_id, seq)`, `started_at`, `finished_at`, `total_files`, `new_files`, `removed_files`, `failed_files`, `status`, `error`

---

## 5. Database Operations Layer

**Modified file:** `services/digitize/db/manager.py`

The DB layer stores and returns ciphertext only. Encryption happens in the API layer; decryption happens in scanners.

**Create-job `connector_id` routing rule:**

| Scenario | `connector_id` | Dedup table | Registry written |
| --- | --- | --- | --- |
| User-submitted | absent | `document_checksum` | `document_checksum (checksum, doc_id)` |
| Connector-sourced | present | `connector_document_checksum` | `connector_document_checksum (checksum, connector_id, doc_id)` |

**Connector-sourced job naming convention:**

Jobs created by a connector sync use the format `{connector_id} - {sync_number} - {batch_number}` as their `job_name`. For example: `sftp-prod-01 - 3 - 1`.

### 5.1 New connector DB functions

| Function | Purpose |
| --- | --- |
| `insert_connector()` | create connector on first-time `POST /v1/connectors`; `ON CONFLICT (id) DO NOTHING` — returns `409` if already exists |
| `upsert_connector()` | partial update on `PUT /v1/connectors/{id}`; accepts only the fields present in the request and merges `connection_details` at the key level, leaving omitted keys untouched |
| `get_active_connector()` | fetch one connector |
| `list_connectors()` | fetch all connectors |
| `delete_active_connector()` | delete connector |
| `lookup_connector_content_by_checksum(checksum)` | connector dedup lookup — queries `connector_document_checksum`, returns `doc_id` or `None` |
| `list_connector_checksums(connector_id)` | all checksums currently owned by this connector |
| `list_all_checksums()` | all distinct checksums in `connector_document_checksum` across all connectors |
| `add_connector_checksum_entry(connector_id, checksum, doc_id)` | insert a new `(checksum, connector_id, doc_id)` row; no-op if the row already exists |
| `remove_connector_checksum_entry(connector_id, checksum)` | delete the `(checksum, connector_id)` row; return remaining owner count and `doc_id` |
| `open_new_sync_log(connector_id)` | create tick row; auto-generates `seq` as `COALESCE(MAX(seq), 0) + 1` scoped to the connector; sets `connectors.sync_status = 'syncing'` in the same transaction; returns the new `seq` value |
| `close_sync_log()` | finalize tick row; sets `connectors.last_sync_at = NOW()` and `connectors.sync_status = :final_status` in the same transaction |
| `update_sync_log()` | live progress updates |
| `list_sync_logs()` | paginated logs query |
| `set_document_metadata(doc_id, metadata)` | write `source_checksum` + S3 key into `documents.metadata` |

### 5.2 DB-layer stub

All connector DB functions are implemented as `@staticmethod` methods on `DatabaseManager` in `services/digitize/db/manager.py`, using the shared `get_db_session()` context manager from `services/digitize/db/connection.py`. The pattern mirrors the existing job/document methods in that file:

```python
@staticmethod
def lookup_connector_content_by_checksum(checksum: str) -> str | None:
    """Return doc_id if checksum is already in connector_document_checksum, else None."""
    with get_db_session() as session:
        row = session.execute(
            select(ConnectorDocumentChecksum.doc_id)
            .where(ConnectorDocumentChecksum.checksum == checksum)
            .limit(1)
        ).one_or_none()
    return row[0] if row else None


@staticmethod
def add_connector_checksum_entry(connector_id: str, checksum: str, doc_id: str) -> None:
    """Insert a new (checksum, connector_id, doc_id) row; no-op if already exists."""
    with get_db_session() as session:
        stmt = (
            insert(ConnectorDocumentChecksum)
            .values(checksum=checksum, connector_id=connector_id, doc_id=doc_id)
            .on_conflict_do_nothing(index_elements=["checksum", "connector_id"])
        )
        session.execute(stmt)


@staticmethod
def remove_connector_checksum_entry(connector_id: str, checksum: str) -> tuple[int, str | None]:
    """Delete the (checksum, connector_id) row; return (remaining_owner_count, doc_id)."""
    with get_db_session() as session:
        deleted = session.execute(
            delete(ConnectorDocumentChecksum)
            .where(
                ConnectorDocumentChecksum.checksum == checksum,
                ConnectorDocumentChecksum.connector_id == connector_id,
            )
            .returning(ConnectorDocumentChecksum.doc_id)
        ).one_or_none()
        if deleted is None:
            return 0, None
        doc_id = deleted[0]
        remaining = session.scalar(
            select(func.count())
            .where(ConnectorDocumentChecksum.checksum == checksum)
        ) or 0
    return remaining, doc_id
```

### 5.3 Connector Lifecycle DB Operations

This section describes the **DB-only operations** for each phase of the connector lifecycle.

---

#### 5.3.1 Attach — `POST /v1/connectors`

```text
DB operations (Attach)
────────────────────────────────────────────────────────────────────
1. INSERT INTO connectors
       (id, type, connection_details, allowed_extensions,
        sync_interval_seconds, attached_at, sync_status)
   VALUES (:connector_id, :type, :encrypted_details, :exts,
           :interval, NOW(), 'up to date')
   ON CONFLICT (id) DO NOTHING          ← 409 if already exists

Result: one row in connectors; worker thread starts.
connector_document_checksum is empty for this connector — populated on first tick.
```

---

#### 5.3.2 Sync Tick — DB operations only

```text
DB operations (Sync Tick)
────────────────────────────────────────────────────────────────────

Phase 1 — open tick record  [open_new_sync_log]
  INSERT INTO connector_sync_logs
      (connector_id, seq, started_at, status)
  SELECT :connector_id,
         COALESCE(MAX(seq), 0) + 1,
         NOW(),
         'started'
  FROM connector_sync_logs
  WHERE connector_id = :connector_id
  RETURNING seq          ← caller stores this as sync_seq

  UPDATE connectors SET sync_status = 'syncing' WHERE id = :connector_id
  ↑ both writes happen in a single transaction inside open_new_sync_log()

Phase 2 — load known state
  ┌─ ACQUIRE ingest_lock (process-wide) ──────────────────────────┐
  │  SELECT checksum FROM connector_document_checksum             │
  │         WHERE connector_id = :connector_id                    │
  │  → produces: known_checksums                                  │
  │                                                               │
  │  ┌─ scanner file walk happens here (no DB) ─────────────────┐ │
  │  │  yields: scanned_files = [(remote_path, checksum), ...]  │ │
  │  └──────────────────────────────────────────────────────────┘ │
  │                                                               │
  │  Phase 3 — classify files + register cross-connector dups    │
  │    skip_list   = []  ← checksum IN known_checksums           │
  │    ingest_list = []  ← checksum NOT IN known_checksums AND   │
  │                        not cross-connector                    │
  │                                                               │
  │    for each (remote_path, checksum) in scanned_files         │
  │            (intra-tick dedup applied):                        │
  │      elif checksum IN all_checksums:                          │
  │        existing_doc_id =                                      │
  │            lookup_connector_content_by_checksum(checksum)    │
  │        INSERT INTO connector_document_checksum               │
  │            (checksum, connector_id, doc_id)                  │
  │        VALUES (:checksum, :connector_id, :existing_doc_id)   │
  │        ON CONFLICT (checksum, connector_id) DO NOTHING       │
  │                                                               │
  │  Phase 4a — register each genuinely new file                 │
  │    (after successful create_job / session creation)          │
  │    INSERT INTO connector_document_checksum                   │
  │        (checksum, connector_id, doc_id)                      │
  │    VALUES (:checksum, :connector_id, :doc_id)                │
  │    ON CONFLICT (checksum, connector_id) DO NOTHING           │
  │                                                              │
  │    UPDATE documents SET metadata = metadata || :source_meta  │
  │    WHERE doc_id = :doc_id                                     │
  └─ RELEASE ingest_lock ─────────────────────────────────────────┘

Phase 4b — orphan detection + removal
  (runs once, after ALL Phase 4a writes complete)

  orphan_checksums = known_checksums − {checksum for (_, checksum) in scanned_files}

  for orphan_checksum in orphan_checksums:
    DELETE FROM connector_document_checksum
    WHERE checksum = :orphan_checksum AND connector_id = :connector_id
    RETURNING doc_id

    SELECT COUNT(*) AS remaining FROM connector_document_checksum WHERE checksum = :orphan_checksum

    if remaining == 0:
      DELETE /v1/documents/{orphan_doc_id}   ← 200/204/404 = success; 5xx logged and skipped

Phase 5 — close tick record  [close_sync_log]
  UPDATE connector_sync_logs
  SET finished_at = NOW(), total_files = :n, new_files = :n,
      removed_files = :n, failed_files = :n, status = :final_status
  WHERE connector_id = :connector_id AND seq = :seq

  UPDATE connectors SET last_sync_at = NOW(), sync_status = :final_status
  WHERE id = :connector_id
  ↑ both writes happen in a single transaction inside close_sync_log()
```

**Ordering guarantee:** Phase 4b (orphan removal) runs only after Phase 4a (all ingest jobs) completes.

**Concurrency lock:** A process-wide `ingest_lock` (e.g. `threading.Lock`) **must be held continuously from the `SELECT checksum` DB read in Phase 2 through the end of Phase 4a** (i.e. until all `INSERT INTO connector_document_checksum` rows for newly created sessions are committed). Without this lock, two connector workers racing on the same brand-new file would both observe it absent from `all_checksums`, both call `create_job`, and produce duplicate documents. The lock is released before Phase 4b begins so orphan removal does not block other ticks unnecessarily.

---

#### 5.3.3 Detach — `DELETE /v1/connectors/{connector_id}`

```text
DB operations (Detach)
────────────────────────────────────────────────────────────────────

Pre-condition check: if worker tick is currently running → return 409 Conflict

Step 1 — stop worker thread (not a DB operation)
  stop_event.set() → thread.join(timeout=30s)

Step 2 — snapshot owned checksums
  SELECT checksum, doc_id FROM connector_document_checksum WHERE connector_id = :connector_id

Step 3 — remove ownership row by row
  for (checksum, doc_id) in owned_rows:
    DELETE FROM connector_document_checksum
    WHERE checksum = :checksum AND connector_id = :connector_id

    SELECT COUNT(*) AS remaining FROM connector_document_checksum WHERE checksum = :checksum

    if remaining == 0:
      DELETE /v1/documents/{doc_id}      ← best-effort; 200/204/404 = success

Step 4 — delete connector row
  DELETE FROM connectors WHERE id = :connector_id
  -- CASCADE deletes connector_sync_logs rows automatically.

Step 5 — cleanup staging dirs (not a DB operation)
  rm -rf {staging_dir}/{connector_id}/
```

**Invariant:** after Step 4, no row in `connector_document_checksum` has `connector_id = :connector_id`.

---

## 6. Scanner Abstraction

**New file:** `services/digitize/connectors/base_scanner.py`

### 6.1 Responsibility split

Base scanner responsibilities: hold connector config, decrypt encrypted credentials, define the interface used by the worker.

Subclass responsibilities: remote listing (yields `(remote_path, checksum)` pairs for **all** files found), file download on demand, connection lifecycle.

> Dedup classification (skip vs ingest) and orphan detection are performed in the worker's `_classify()` method (§8.4), not in the scanner.

### 6.2 Class diagram

```text
BaseScanner
  ├─ connect()
  ├─ scan()              → list[(remote_path, checksum)]   # ALL remote files, no dedup filtering
  ├─ download_to(remote_path, local_path)  → str           # returns local hex digest
  ├─ verify_integrity(local_checksum, remote_checksum) → bool   # concrete, overridable
  └─ close()

BaseScanner
  ├─ SFTPScanner   (checksum = remotely-computed MD5 via md5sum)
  └─ S3Scanner     (checksum = S3 ETag from list_objects_v2;
                    overrides verify_integrity to skip multi-part ETags)
```

### 6.3 Interface stub

```python
import logging
from abc import ABC, abstractmethod
from pathlib import Path

logger = logging.getLogger(__name__)


class BaseScanner(ABC):
    def __init__(self, config: object) -> None:
        self._config = config

    @abstractmethod
    def connect(self) -> None: ...

    @abstractmethod
    def scan(self) -> list[tuple[str, str]]:
        """Return (remote_path, checksum) for ALL files found on the remote source.
        No dedup filtering is applied here — the worker's _classify() splits the result.
        """
        ...

    @abstractmethod
    def download_to(self, remote_path: str, local_path: Path) -> str:
        """Download the file and return its local hex digest (computed inline,
        no second file read).  The caller can pass this to verify_integrity().
        """
        ...

    @abstractmethod
    def close(self) -> None: ...

    def verify_integrity(self, local_checksum: str, remote_checksum: str) -> bool:
        """Concrete base implementation — direct equality check.
        Correct for any transport whose checksum is a plain hex digest (e.g. SFTP md5sum).
        Subclasses may override for format-specific logic (see S3Scanner).
        """
        match = local_checksum == remote_checksum
        if not match:
            logger.error("verify_integrity FAILED — local=%r, remote=%r",
                         local_checksum, remote_checksum)
        return match
```

### 6.4 Factory dispatch

Factory dispatch lives in `services/digitize/connectors/scanner_factory.py`:

- `ssh` → `SFTPScanner`  *(placeholder — implemented in a future PR)*
- `s3` → `S3Scanner`

---

## 7. Concrete Scanners

### 7.1 SFTP scanner

**New file:** `services/digitize/connectors/sftp_scanner.py`

Behavior:

- Decrypt private key per tick
- Connect with Paramiko (SFTP channel for listing/download, SSH channel for hashing)
- Recursively walk the remote path
- Ignore files whose extension is not in `allowed_extensions`
- Compute MD5 **on the remote host** via `ssh.exec_command()` — no file bytes are transferred during hashing
- Download selected files into staging

MD5 computation:

```python
def _remote_md5(self, remote_file_path: str) -> str:
    _, stdout, _ = self._ssh.exec_command(f'md5sum "{remote_file_path}"')
    output = stdout.read().decode().strip()
    return output.split()[0]
```

SFTP scan sketch:

```python
def scan(self) -> list[tuple[str, str]]:
    found = []
    for remote_file in self._walk_remote_tree():
        if not self._is_allowed(remote_file.path):
            continue
        remote_path: str = remote_file.path
        checksum = self._remote_md5(remote_path)
        found.append((remote_path, checksum))
    return found
```

### 7.2 S3 scanner

**File:** `services/digitize/connectors/s3_scanner.py`

**Detailed design:** [S3 Scanner — Detailed Design Proposal](./s3-scanner-proposal.md)

Behavior:

- Auto-detect provider (AWS S3 or IBM COS) from `endpoint_url` hostname
- Build boto3 client per tick in `connect()`
- List objects via `list_objects_v2` paginator — yields `(key, checksum)` where checksum = S3 ETag; **the full list is returned without filtering**
- Download files on demand (only those the worker places on `ingest_list`)
- `download_to()` streams through `_HashingWriter` (inline MD5) and returns the local hex digest — no second file read
- `verify_integrity()` overrides the base: skips the check for multi-part ETags (`<hex>-N`) since the ETag is `MD5(raw_part_digests)` and cannot be reproduced locally; delegates single-part ETags to the base equality check
- Store only `source_checksum` in `connector_document_checksum.checksum` and `documents.metadata.source_checksum`

S3 scan + download sketch:

```python
def scan(self) -> list[tuple[str, str]]:
    self._require_connected()
    return list(self._list_document_keys())


def download_to(self, remote_path: str, local_path: Path) -> str:
    self._require_connected()
    with open(local_path, "wb") as fh:
        writer = _HashingWriter(fh)
        self._client.download_fileobj(
            Bucket=self._cfg.bucket_name, Key=remote_path, Fileobj=writer,
        )
    return writer.hexdigest   # local MD5, returned to caller for integrity check


def verify_integrity(self, local_checksum: str, remote_checksum: str) -> bool:
    if "-" in remote_checksum:   # multi-part ETag — cannot verify locally
        return True
    return super().verify_integrity(local_checksum, remote_checksum)
```

**boto3 client construction** — `endpoint_url` always forwarded when present; `addressing_style` set per provider to avoid boto3 path-style/virtual-hosted conflicts:

```python
addressing_style = "virtual" if self._cfg.is_aws else "path"

session = boto3.Session(
    aws_access_key_id=self._cfg.access_key_id or None,
    aws_secret_access_key=self._cfg.secret_access_key or None,
    region_name=self._cfg.effective_region,
)
client_kwargs = {
    "service_name": "s3",
    "config": botocore.config.Config(
        signature_version="s3v4",
        s3={"addressing_style": addressing_style},
    ),
    "verify": self._cfg.verify_ssl,
}
if self._cfg.endpoint_url:
    client_kwargs["endpoint_url"] = self._cfg.endpoint_url

client = session.client(**client_kwargs)
```

| Provider | `addressing_style` | `endpoint_url` forwarded |
|---|---|---|
| AWS S3 | `"virtual"` — `<bucket>.s3.<region>.amazonaws.com/<key>` | Yes (when supplied) |
| IBM COS | `"path"` — `<host>/<bucket>/<key>` | Yes |

---

## 8. Sync Worker

**New file:** `services/digitize/connectors/sync_worker.py`

`ConnectorSyncWorker` owns the end-to-end sync loop for one connector.

### 8.1 Tick flow

```text
_run_tick()
│
├─ [Phase 1] INSERT connector_sync_logs (status='started')
│            UPDATE connectors (sync_status='syncing')
│
├─ [Phase 2] ── ACQUIRE ingest_lock ────────────────────────────────────────────
│            known_checksums ← SELECT checksum FROM connector_document_checksum
│                               WHERE connector_id = :connector_id
│            all_checksums   ← SELECT DISTINCT checksum FROM connector_document_checksum
│
│            ┌── scanner.connect() + scanner.scan() ─────────────────┐
│            │   walks remote source; returns ALL (remote_path, checksum) │
│            └──────────────────────────────────────────────────────── ┘
│
├─ [Phase 3] _classify(scanned_files, known_checksums, all_checksums)
│            → skip_list   checksum IN known_checksums (no action)
│            → ingest_list checksum NOT IN all_checksums (brand new)
│            → cross-connector dup: lookup_connector_content_by_checksum(checksum)
│                                   add_connector_checksum_entry(connector_id, checksum, existing_doc_id)
│                                   (DB write happens inline, no separate list)
│
├─ [Phase 4a] _process_new_files(ingest_list)
│             for each (batch_number, remote_path, checksum):
│               batch_dir_name = f"{connector_id}-{job_id}-{batch_number}"
│               download file → staging/connectors/<batch_dir_name>/<filename>
│               create_job(connector_id, checksum) → ingest → doc_id (session creation)
│               add_connector_checksum_entry(connector_id, checksum, doc_id)
│               UPDATE documents.metadata
│               cleanup_staging_directory(batch_dir_name,
│                 staging_dir/"connectors", ignore_errors=True)  ← per-file, after ingest
│            ── RELEASE ingest_lock (after all Phase 4a membership rows committed) ─
│
├─ [Phase 4b] _delete_orphans(orphan_checksums)
│   ← RUNS AFTER all Phase 4a writes finish ←
│             orphan_checksums = known_checksums − scanned_checksums
│             remove_connector_checksum_entry → if remaining==0: DELETE /v1/documents/{doc_id}
│
└─ [Phase 5] UPDATE connector_sync_logs (finished_at, counters, status)
             UPDATE connectors (last_sync_at, sync_status)
```

### 8.2 Worker rules

- Overlapping ticks are skipped by a tick guard.
- `new_files` is updated live during staging and download.
- Each file in a tick gets its own uniquely-named staging directory: `staging/connectors/<connector_id>-<job_id>-<batch_number>/`. The `job_id` is the UUID returned by `create_job()`, and `batch_number` is the zero-based index of the file within the tick's `ingest_list`. This naming makes every staging directory traceable to a specific connector, job, and position in the batch.
- The staging directory is created immediately before `scanner.download()` and removed in the `finally` block after ingest, regardless of success or failure — before the next file is downloaded. No two batch directories exist simultaneously.
- Download and ingest are blocking operations.
- Fatal errors mark the tick as failed.
- Per-file failures are counted and summarized instead of failing the whole connector. Staging cleanup still runs for each file even when ingest fails.
- Cross-connector duplicates are registered inline during Phase 3 classification — no deferred list.
- **Phase 4b (orphan removal) always runs after Phase 4a (all new-file ingest jobs) completes.**
- **A process-wide `ingest_lock` must be held from the Phase 2 DB read (`list_all_checksums`) through the end of Phase 4a (last `add_connector_checksum_entry` call, i.e. session creation complete).** This prevents two concurrent workers from both classifying the same brand-new checksum as absent and independently spawning duplicate ingest jobs.
- Staging cleanup uses `cleanup_staging_directory(batch_dir_name, staging_dir / "connectors", ignore_errors=True)` from `common.misc_utils` — the same helper used by the job API, with `ignore_errors=True` for best-effort semantics.
- On `DELETE /v1/connectors/{connector_id}`, any residual staging directories (e.g. left by a crash mid-tick) are swept by globbing `staging/connectors/<connector_id>-*` and removing each match.

### 8.3 Worker stub

```python
def _run_tick(self) -> None:
    self.config = get_active_connector(self.connector_id)
    sync_seq = open_new_sync_log(self.connector_id)  # seq auto-generated by DB
    scanner = build_scanner(self.config)
    try:
        scanner.connect()
        scanned_files: list[tuple[str, str]] = scanner.scan()

        # Lock must be held from the DB read through the end of session creation
        # (Phase 4a) to prevent concurrent workers from both seeing the same new
        # checksum as absent and spawning duplicate ingest jobs.
        with _ingest_lock:
            known_checksums: set[str] = set(list_connector_checksums(self.connector_id))
            all_checksums: set[str] = set(list_all_checksums())

            ingest_list, orphan_checksums = self._classify(
                scanned_files, known_checksums, all_checksums
            )
            self._process_new_files(sync_seq, scanner, ingest_list)
        # Lock released — orphan removal does not need it
        self._delete_orphans(orphan_checksums)
        self._complete_tick(sync_seq)
    except Exception as exc:
        self._fail_tick(sync_seq, exc)
    finally:
        scanner.close()

def _process_new_files(
    self,
    sync_seq: int,
    scanner: BaseScanner,
    ingest_list: list[tuple[str, str]],
) -> None:
    staging_base = settings.digitize.staging_dir / "connectors"
    for batch_number, (remote_path, checksum) in enumerate(ingest_list):
        job_id = generate_job_id()  # UUID generated before download so the dir name is known upfront
        batch_dir_name = f"{self.connector_id}-{job_id}-{batch_number}"
        batch_dir = staging_base / batch_dir_name
        batch_dir.mkdir(parents=True, exist_ok=True)
        try:
            scanner.download(remote_path, batch_dir)
            doc_id = create_job(self.connector_id, checksum, staging_dir=batch_dir)
            add_connector_checksum_entry(self.connector_id, checksum, doc_id)
        except Exception as exc:
            logger.warning(f"Failed to ingest {remote_path!r}: {exc}")
            self._increment_failed(sync_seq)
        finally:
            # Remove this batch's staging directory immediately — before the
            # next file is downloaded — regardless of success or failure.
            cleanup_staging_directory(
                batch_dir_name,
                staging_base,
                ignore_errors=True,
            )
```

### 8.4 Classify

`_classify` receives the full scanner output, `known_checksums` (this connector's owned checksums), and `all_checksums` (all checksums across all connectors). It produces:

| Collection | Type | Contents |
| --- | --- | --- |
| `ingest_list` | `list[tuple[str, str]]` | Brand new to all connectors — download, ingest, register |
| `orphan_checksums` | `set[str]` | Previously owned by this connector, no longer on remote source |

Cross-connector duplicates (`checksum IN all_checksums but NOT known_checksums`) are handled inline: `_classify` immediately calls `lookup_connector_content_by_checksum` and `add_connector_checksum_entry` before moving on. Intra-tick dedup still applies — only the first occurrence of a checksum triggers the DB write.

```python
def _classify(
    self,
    scanned_files: list[tuple[str, str]],
    known_checksums: set[str],
    all_checksums: set[str],
) -> tuple[list[tuple[str, str]], set[str]]:
    scanned_checksums: set[str] = set()
    seen_this_tick: set[str] = set()
    ingest_list: list[tuple[str, str]] = []

    for remote_path, checksum in scanned_files:
        scanned_checksums.add(checksum)
        if checksum in known_checksums:
            pass  # already owned by this connector → skip
        elif checksum in all_checksums:
            if checksum not in seen_this_tick:
                seen_this_tick.add(checksum)
                existing_doc_id = lookup_connector_content_by_checksum(checksum)
                add_connector_checksum_entry(self.connector_id, checksum, existing_doc_id)
        else:
            if checksum not in seen_this_tick:
                seen_this_tick.add(checksum)
                ingest_list.append((remote_path, checksum))

    orphan_checksums = known_checksums - scanned_checksums
    return ingest_list, orphan_checksums
```

> **Ordering invariant:** `_delete_orphans(orphan_checksums)` is called only after all Phase 4a writes complete, guaranteeing a checksum registered in Phase 4a is never simultaneously processed as an orphan in Phase 4b.

---

## 9. Worker Manager

**New file:** `services/digitize/connectors/worker_manager.py`

`ConnectorWorkerManager` owns worker thread lifecycle.

### 9.1 Responsibilities

- Maintain `connector_id -> (thread, worker, stop_event)`
- Start one daemon thread per connector on POST
- Stop workers cooperatively on DELETE
- Recover workers for persisted connectors on startup

PUT does not restart workers — the running thread re-reads config from the DB before entering the next tick.

### 9.2 Lifecycle

![Worker Manager Lifecycle and Thread Lifecycle](thread-lifecycle-combined.svg)

---

## 10. Thread Lifecycle and Resilience

Thread behavior is cooperative, observable, and restartable.

### 10.1 Threading Model

`ConnectorWorkerManager` is a module-level singleton inside the single Uvicorn process. Every sync thread is a `daemon=True` `threading.Thread`.

A sync thread crash affects only that connector — the asyncio event loop and other connector threads keep running. The dead thread's slot in `_workers` remains with `stop_event` unset, which is the crash state detected by §10.4.

### 10.2 Thread Crash & Status

A crash is an unhandled exception that escapes the outer `while` loop in `ConnectorSyncWorker.run()` — `thread.is_alive() == False` with `stop_event` not set.

The crash guard must write `"crashed: <error>"` to `connectors.sync_status` and close any open `"syncing"` history row.

```python
# ConnectorSyncWorker.run() — outer crash guard
try:
    while not stop_event.is_set():
        ...  # tick loop
except Exception as exc:
    log.error(f"Worker {connector_id} crashed: {exc}", exc_info=True)
    if _tick_running and _current_sync_id:
        close_sync_log(…, sync_status=f"crashed: {exc}")
    update_connector_sync_status(…, f"crashed: {exc}")
```

### 10.3 Respawn Logic

When the monitor detects a crashed thread it respawns the worker with exponential back-off, bounded to 5 attempts.

Respawn steps: remove stale `_workers` entry → read config from DB (skip if connector was deleted) → increment `crash_count` → if `crash_count > 5` write `"respawn limit reached"` and stop → sleep `min(2^crash_count, 300)` seconds → call `start_worker(config)`.

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

One daemon thread (`connector-monitor`) watches all workers, polling every 30 s:

- `stop_event` set → graceful stop in progress; skip.
- `thread.is_alive()` → healthy.
- Thread dead, `stop_event` not set → crash detected; call `schedule_respawn()`.

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
4. Closes the sync-logs row as `"interrupted: connector deleted"`.

Check points:

```
_run_tick():
  step 1 — connect & scan       → _cancel_if_requested()
  step 2 — dedup pass           → _cancel_if_requested()
  step 3 — batch ingest loop    → _cancel_if_requested() after each batch
  step 4 — orphan deletes       → _cancel_if_requested()
  step 5 — finalise (normal path)
```

In-tick tracking collections:

| Variable | Holds | Populated |
| --- | --- | --- |
| `pending_checksums` | checksum values pre-written with `NULL doc_id` | Before each download; removed on success |
| `ingested_this_tick` | `(checksum, doc_id)` pairs | After each successful ingest |

DELETE flow: `stop_event.set()` → `thread.join(timeout=30 s)` → if still alive: log warning and proceed best-effort.

Key invariants:

| Invariant | Mechanism |
| --- | --- |
| Interval never interrupts a running tick | Timer checked only after `_run_tick()` returns |
| DELETE cooperatively interrupts at next boundary | `_cancel_if_requested()` after each phase |
| No concurrent ticks per connector | Single-threaded loop + tick guard |
| Monitor skips intentionally stopped threads | `if stop_event.is_set(): skip` |

### 10.6 Lifespan Recovery

On FastAPI startup: load `connectors`, recreate workers, start the monitor. No catalog re-push needed.

---

## 11. Implementation Plan — Digitize Connector PRs

Each PR is independently testable.

---

### PR 1 — DB Schema + ORM Models + Settings

**Files touched:** `services/digitize/db/scripts/init_schema.sql`, `services/digitize/db/models.py`, `services/digitize/settings.py`

**What was built:**
- 3 new tables: `connectors`, `connector_document_checksum`, `connector_sync_logs`
  - `connector_document_checksum` schema: `checksum TEXT NOT NULL`, `connector_id TEXT NOT NULL`, `doc_id TEXT NOT NULL`, `PRIMARY KEY (checksum, connector_id)` — no FK constraints, no `ON DELETE CASCADE`
  - Index `idx_cdc_connector_id` (B-tree on `connector_id`) — see §4.3 for rationale
  - `connector_sync_logs` index: `idx_csl_connector_started` on `(connector_id, started_at DESC)`
- 3 new ORM models added to `services/digitize/db/models.py`: `Connector`, `ConnectorDocumentChecksum`, `ConnectorSyncLog`
  - `Connector.sync_interval_seconds` stores the value populated from `CONNECTOR_SYNC_INTERVAL_SECONDS` (default `300`) at attach time
- `settings.py` (`services/digitize/settings.py`) already contains `DigitizeConfig` with `staging_dir` property; no new connector-specific settings class was needed — connector sync interval is read from the env var directly at attach time

**How to test:**
- Run `init_schema.sql` against a test DB and assert all 3 new tables exist with correct columns, constraints, and indexes
- Assert `connector_document_checksum` has composite PK `(checksum, connector_id)` and no FK / `ON DELETE CASCADE`
- Assert `idx_cdc_connector_id` exists as a B-tree index on `connector_document_checksum (connector_id)`
- Unit test: instantiate ORM models and map them against the schema

---

### PR 2 — DB Operations Layer

**Files touched:** `services/digitize/db/manager.py`

**What's to build:**
All connector DB functions from §5.1:
`insert_connector`, `upsert_connector`, `get_active_connector`, `list_connectors`, `delete_active_connector`, `update_connector_sync_status`, `lookup_connector_content_by_checksum`, `list_connector_checksums`, `list_all_checksums`, `add_connector_checksum_entry`, `remove_connector_checksum_entry`, `open_new_sync_log`, `close_sync_log`, `update_sync_log`, `list_sync_logs`, `set_document_metadata`

**How to test:**
- Unit tests per function against a test DB
- Assert `insert_connector` inserts a new row and returns `409` on a duplicate `id`
- Assert `upsert_connector` updates only the supplied fields and merges `connection_details` keys without clobbering untouched fields
- Assert `add_connector_checksum_entry` inserts a new row on first call and appends the connector_id on subsequent calls with the same checksum
- Assert `remove_connector_checksum_entry` returns `remaining=0` when the last connector is removed, and `remaining>0` when others still own the checksum
- Assert `lookup_connector_content_by_checksum` queries `connector_document_checksum` and never touches `document_checksum`

---

### PR 3 — REST API Endpoints

**Files touched:** connector router/handler file(s)

**What's to build:**
- `POST /v1/connectors` — validate body, encrypt secrets, call `insert_connector`, return `202`
- `PUT /v1/connectors/{id}` — partial update, re-encrypt if credentials included, call `upsert_connector`, return `200`
- `DELETE /v1/connectors/{id}` — stub only (stops at "would stop worker" + calls DB delete), no worker logic yet
- `GET /v1/connectors` and `GET /v1/connectors/{id}` — read from DB, strip secret fields
- `GET /v1/connectors/{id}/syncs` — paginated query with `limit`/`offset`
- **Update `GET /v1/documents` and `GET /v1/documents/{doc_id}`** to exclude connector-sourced docs via `NOT EXISTS (SELECT 1 FROM connector_document_checksum WHERE doc_id = ...)` filter
- **Update `DELETE /v1/documents/{doc_id}`** to return `404` for connector-sourced docs

**How to test:**
- Integration tests using `httpx.AsyncClient` + test DB
- Assert secrets are never returned in GET responses
- Assert `PUT` with partial `connection_details` only overwrites provided keys
- Assert correct HTTP status codes for 404/409/401 paths
- Assert `GET /v1/documents` does not return docs whose `doc_id` appears in `connector_document_checksum`
- Assert `GET /v1/documents/{doc_id}` and `DELETE /v1/documents/{doc_id}` return `404` for connector-sourced docs
- Assert `GET /v1/jobs` returns all jobs including connector-initiated ones

---

### PR 4 — Scanner Abstraction + SFTP Scanner

**Files touched:** `connectors/base_scanner.py`, `connectors/scanner_factory.py`, `connectors/sftp_scanner.py`

**What's to build:**
- `BaseScanner` ABC with `connect()`, `scan()`, `download_to() -> str`, `close()`, and concrete `verify_integrity()`
- `build_scanner()` factory in `scanner_factory.py` — dispatches on `type`; SFTP placeholder commented until implemented
- `SFTPScanner` — Paramiko connection (SFTP + SSH), recursive walk, extension filter, remote MD5 via `ssh.exec_command(f'md5sum "{remote_file_path}"')`, staged download; `scan()` returns **all** files; inherits base `verify_integrity()` (plain equality check is correct for SFTP MD5)

**How to test:**
- Unit test against a local mock SFTP server (e.g. `pytest-sftpserver` or `paramiko.SFTPServer` in a thread)
- Assert extension filtering works correctly
- Assert MD5 is computed via remote `md5sum` exec (not by streaming bytes)
- Assert `scan()` returns ALL allowed remote files without filtering against known_checksums
- Assert `build_scanner("ssh", config)` returns an `SFTPScanner` instance

---

### PR 5 — S3 Scanner ✅ Implemented

**Files touched:** `connectors/s3_scanner.py`, `connectors/config.py`, `connectors/scanner_factory.py`, `connectors/__init__.py`, `tests/test_connector_scanners.py`, `scripts/test_s3_connector.py`

**What was built:**
- `S3Scanner` — boto3 `list_objects_v2` paginator, extension filter, `download_fileobj()` with inline MD5 via `_HashingWriter`; `scan()` returns **all** allowed objects without dedup filtering; `download_to()` returns local MD5 hex digest; `verify_integrity()` overrides base to skip multi-part ETags; checksum (S3 ETag) registered via `add_connector_to_membership()` on job completion; `set_document_metadata()` stores **only `source_checksum`**; `upsert_file_checksum()` is NOT called
- `S3ConnectorConfig` — pydantic model with provider auto-detection, `effective_region` (includes IBM COS cross-region alias resolution: `us→us-south`, `eu→eu-de`, `ap→jp-tok`), `from_connection_details()` factory
- `build_scanner()` factory in `scanner_factory.py` — accepts dict or ORM row
- 39 unit tests — all passing (`tests/test_connector_scanners.py`)
- Manual smoke-test script (`scripts/test_s3_connector.py`) — connect → list → download

**Key decisions vs original design:**
- `download_to()` return type changed `None → str` (returns local MD5 for integrity verification)
- No sidecar `.meta.json` — ingestion pipeline uses other means for checksum handling
- `endpoint_url` always forwarded to boto3; `addressing_style` set explicitly per provider (`"virtual"` for AWS, `"path"` for COS) instead of conditionally withholding the URL
- `verify_integrity()` added to `BaseScanner` as a concrete overridable method (not in original design)

---

### PR 6 — Sync Worker

**Files touched:** `connectors/sync_worker.py`

**What's to build:**
- `ConnectorSyncWorker` with full `_run_tick()`: config refresh, scan, classify, ingest new files, register cross-connector dups, orphan deletion, tick finalize
- `_classify()`: see §8.4 — `skip_list` is implicit (not returned)
- Pass `connector_id` and `checksum` (pre-computed by scanner) to each create-job call; `add_connector_checksum_entry` called on job completion — `upsert_file_checksum` must NOT be called for connector jobs
- `_process_new_files()`: for each file, generate a `job_id` upfront, create `staging/connectors/<connector_id>-<job_id>-<batch_number>/`, download into it, ingest, then `cleanup_staging_directory(batch_dir_name, ..., ignore_errors=True)` in a `finally` block — staging is cleared after each individual file, not at the end of the batch
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
- `_classify` — assert intra-tick dedup (two paths with same checksum → first path wins)
- `_classify` — assert absent checksums appear in `orphan_checksums`
- Assert `_delete_orphans()` is called only after `_process_new_files()` returns
- `_process_new_files` — assert staging dir is named `<connector_id>-<job_id>-<batch_number>` and is created before `scanner.download()`
- `_process_new_files` — assert `cleanup_staging_directory` is called with `batch_dir_name` in the `finally` block, including when ingest raises
- `_process_new_files` — assert staging directory is absent between two consecutive file downloads (i.e. cleanup happens before the next `scanner.download` call)

---

### PR 7 — Worker Manager + Lifespan Recovery + Monitor

**Files touched:** `connectors/worker_manager.py`, `app.py` (lifespan hook)

**What's to build:**
- `ConnectorWorkerManager` singleton: `start_worker()`, `stop_worker()`, `_workers` map
- `connector-monitor` daemon thread: 30s poll, crash detection, `schedule_respawn()` with exponential back-off up to 5 attempts
- Lifespan recovery: on startup load all `connectors`, recreate workers, start monitor; on shutdown stop monitor
- Wire `DELETE /v1/connectors` to call `stop_worker()` (completing the stub from PR 3)

**How to test:**
- Unit test `start_worker()` / `stop_worker()` with a no-op worker
- Assert `stop_event.set()` + `thread.join()` is called on DELETE
- Test monitor: inject a dead thread with `stop_event` unset, assert `schedule_respawn()` is called
- Assert back-off delays are computed correctly per crash count
- Assert crash counter resets after first clean tick post-respawn
- Integration test: start the full app with a test connector, confirm the worker starts and the lifespan hook restores it on simulated restart
