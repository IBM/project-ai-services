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
  → register IntervalTrigger job in APScheduler for this connector_id
  → fire first tick immediately (misfire_grace_time = 0)

── Manual sync trigger (POST /v1/connectors/{id}/sync) ──────────────
  → 404 if connector does not exist
  → atomic DB check-and-set:
      UPDATE connectors SET sync_status = 'syncing'
      WHERE id = :connector_id AND sync_status != 'syncing'
      RETURNING id
      if no row returned → already syncing; return 202 immediately (no-op)
  → dispatch _run_tick(connector_id) as a background async task
  → return 202 Accepted

── Config update (PUT /v1/connectors/{id}) ──────────────────────────
  → merge + re-encrypt changed fields
  → UPDATE connectors row
  → scheduler job re-reads config from DB at the start of every tick

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
  → remove APScheduler job for this connector_id
  → list all checksums owned by this connector
  → for each checksum: remove_connector_checksum_entry → delete doc if last owner
  → DELETE connectors row
  → cleanup staging dirs
```

### 2.3 Main Components

- `connectors`: current connector configuration and top-level sync state
- `connector_document_checksum`: **connector-sourced documents only** — one row per `(checksum, connector_id)` pair; carries the `doc_id` for deletion
- `connector_sync_logs`: one row per scheduled tick
- `ConnectorScheduler`: APScheduler `AsyncScheduler` singleton — registers one `IntervalTrigger` job per connector and manages job lifecycle
- `ConnectorSyncTask`: async function owning the end-to-end tick logic; dispatched as a coroutine by APScheduler or by `POST /v1/connectors/{id}/sync`
- Scanner implementations: transport-specific remote access for SFTP and S3; S3 scanner derives the checksum from the S3 ETag returned by `list_objects_v2`; SFTP scanner uses a remotely-computed MD5 — both stored as `checksum` in `connector_document_checksum`

---

## 3. API Contract


### 3.1 `POST /v1/connectors`

Creates a connector, stores encrypted credentials, persists config, and registers a periodic APScheduler job. The first tick fires immediately on job registration.

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
| `202 Accepted` | Connector created; first sync tick scheduled |
| `409 Conflict` | Connector already exists (`connector_id` or `connector_name` already in use) |

### 3.2 `PUT /v1/connectors/{connector_id}`

Updates an existing connector's config in the database. The scheduler is not restarted — it reads the latest config from the DB before entering the next tick.

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
| `200 OK` | Connector updated; changes are picked up on the next tick |
| `404 Not Found` | Connector does not exist |

### 3.3 `DELETE /v1/connectors/{connector_id}`

Removes a connector and its runtime state.

> **Constraint — active sync tick:** DELETE is accepted **only when no sync tick is currently in progress** for the connector. If a tick is running, the request is rejected with `409 Conflict`. The APScheduler job is removed before any DB state is touched, so no new tick can start during the delete sequence.

Delete flow:

1. **Guard:** check whether a sync tick is actively running for the connector (`sync_status == 'syncing'`). If yes, return `409 Conflict` — no state is modified.
2. Remove the APScheduler job for this connector so no new tick can be scheduled.
3. Snapshot the connector's known checksums.
4. Remove membership rows checksum by checksum.
5. Delete documents only when the remaining reference count reaches zero.
6. Delete the `connectors` row.
7. Best-effort cleanup of staging directories.

#### Delete sequence diagram

```text
DELETE /v1/connectors/{connector_id}
  → check if sync tick is in progress (sync_status == 'syncing')
      if YES → 409 Conflict (no state modified)
  → remove APScheduler job (scheduler.remove_job(connector_id))
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

> **Future:** when job cancellation is supported, the `409` guard will be replaced by waiting for the running tick to finish (or cancelling it) so that DELETE can succeed at any stage of the lifecycle.

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

### 3.7 `POST /v1/connectors/{connector_id}/sync`

Manually triggers an immediate sync tick for the connector. Safe to call at any time — concurrent or duplicate calls are collapsed by an atomic DB guard and never spawn two ticks simultaneously.

#### Trigger steps

1. **Existence check:** `GET active_connector(connector_id)` — return `404` if not found.
2. **Atomic lock acquisition:** execute the following in a single DB round-trip:
   ```sql
   UPDATE connectors
   SET    sync_status = 'syncing'
   WHERE  id          = :connector_id
     AND  sync_status != 'syncing'
   RETURNING id
   ```
   - If **no row is returned** the connector is already mid-tick. Return `202 Accepted` immediately — no task is dispatched, no state is modified.
   - If **a row is returned** this call won the lock. Proceed to step 3.
3. **Open sync-log row:** call `open_new_sync_log(connector_id)` — inserts a `connector_sync_logs` row with `status='started'` and captures the new `seq` value. (The `sync_status='syncing'` write in step 2 and the log INSERT happen in separate transactions; step 2 must succeed before step 3 is attempted.)
4. **Dispatch background task:** schedule `_run_tick(connector_id)` as an `asyncio` background task. The HTTP response is returned before the tick completes.
5. **Return `202 Accepted`.**

> `_run_tick` is responsible for closing the sync-log row and updating `sync_status` on both success and failure (see §8.1 Phase 5 and §10.2). The endpoint itself does not wait for the tick to finish.

#### Sequence diagram

```text
POST /v1/connectors/{connector_id}/sync
  │
  ├─ 1. get_active_connector(connector_id)
  │        └─ None → 404 Not Found
  │
  ├─ 2. UPDATE connectors
  │      SET    sync_status = 'syncing'
  │      WHERE  id = :connector_id AND sync_status != 'syncing'
  │      RETURNING id
  │        │
  │        ├─ no row returned (already syncing)
  │        │     └─ return 202 Accepted  [no-op]
  │        │
  │        └─ row returned (lock acquired)
  │              │
  ├─ 3.          open_new_sync_log(connector_id)
  │              │  INSERT connector_sync_logs (status='started')
  │              │  → sync_seq
  │              │
  ├─ 4.          asyncio.create_task(_run_tick(connector_id))
  │              │
  └─ 5.          return 202 Accepted
                 ↓
           [background]
           _run_tick(connector_id)
             → scanner.connect() + scan()
             → _classify(...)
             → _process_new_files(...)
             → _delete_orphans(...)
             → close_sync_log(sync_seq, status='completed'|'failed')
             → UPDATE connectors SET sync_status = :final_status
```

#### Interaction with the APScheduler periodic job

`POST /sync` and the APScheduler `IntervalTrigger` job share the same `_run_tick` coroutine and the same atomic lock. If the scheduler fires at the same moment as a `POST /sync` call, exactly one of them wins the `UPDATE … WHERE sync_status != 'syncing'` check. The loser returns (or is no-op'd by APScheduler's `max_instances=1`) without duplicating any state.

#### Response codes

| Status | Meaning |
| --- | --- |
| `202 Accepted` | Tick dispatched in the background |
| `202 Accepted` | Tick already running — no duplicate spawned (idempotent) |
| `404 Not Found` | Connector does not exist |

---

### 3.8 Digitize Document & Job API — Connector Visibility Rules

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

> **Note:** `sync_interval_seconds` is stored per-connector for future extensibility but is not accepted via the API today. On `POST`, it is populated from the `CONNECTOR_SYNC_INTERVAL_SECONDS` environment variable (default `300`). The scheduler reads the value from the DB before each tick.

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
| `try_acquire_sync_lock(connector_id)` | atomic `UPDATE connectors SET sync_status='syncing' WHERE id=:id AND sync_status!='syncing' RETURNING id`; returns the `id` if the lock was acquired, `None` if the connector is already syncing — used by `POST /sync` before calling `open_new_sync_log` |
| `open_new_sync_log(connector_id)` | create tick row; auto-generates `seq` as `COALESCE(MAX(seq), 0) + 1` scoped to the connector; **does not** set `sync_status` — caller must have already acquired the lock via `try_acquire_sync_lock` or `open_new_sync_log` is called only from within `_run_tick` after the APScheduler job fires; returns the new `seq` value |
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

Result: one row in connectors; APScheduler `IntervalTrigger` job registered; first tick fires immediately.
connector_document_checksum is empty for this connector — populated on first tick.
```

---

#### 5.3.2 Sync Tick — DB operations only

```text
DB operations (Sync Tick)
────────────────────────────────────────────────────────────────────

Phase 0 — acquire sync lock  [APScheduler-driven path only]
  APScheduler enforces max_instances=1 — no explicit DB lock needed before Phase 1.
  For POST /sync path: lock is acquired by try_acquire_sync_lock() before _run_tick is dispatched.

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

  ↑ sync_status is already 'syncing' at this point (set either by APScheduler job
    entry or by try_acquire_sync_lock() in the POST /sync path)

Phase 2 — load known state
  SELECT checksum FROM connector_document_checksum
         WHERE connector_id = :connector_id
  → produces: known_checksums

  ┌─ scanner file walk happens here (no DB) ──────────────────────┐
  │  yields: scanned_files = [(remote_path, checksum), ...]       │
  └───────────────────────────────────────────────────────────────┘

  Phase 3 — classify files + register cross-connector dups
    skip_list   = []  ← checksum IN known_checksums
    ingest_list = []  ← checksum NOT IN known_checksums AND
                        not cross-connector

    for each (remote_path, checksum) in scanned_files
            (intra-tick dedup applied):
      elif checksum IN all_checksums:
        existing_doc_id =
            lookup_connector_content_by_checksum(checksum)
        INSERT INTO connector_document_checksum
            (checksum, connector_id, doc_id)
        VALUES (:checksum, :connector_id, :existing_doc_id)
        ON CONFLICT (checksum, connector_id) DO NOTHING

  Phase 4a — register each genuinely new file
    (after successful create_job / session creation)
    INSERT INTO connector_document_checksum
        (checksum, connector_id, doc_id)
    VALUES (:checksum, :connector_id, :doc_id)
    ON CONFLICT (checksum, connector_id) DO NOTHING

    UPDATE documents SET metadata = metadata || :source_meta
    WHERE doc_id = :doc_id

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

**Concurrency guard:** same-connector overlap is prevented entirely by the DB-level `sync_status` check. The APScheduler `max_instances=1` policy ensures a new scheduled tick is not dispatched while the connector's `sync_status` is already `'syncing'`. `POST /sync` uses `try_acquire_sync_lock()` — an atomic `UPDATE … WHERE sync_status != 'syncing' RETURNING id` — before dispatching `_run_tick`; if no row is returned, the request is a no-op. Cross-connector duplicate ingestion of the same brand-new checksum is handled at the DB level by `ON CONFLICT (checksum, connector_id) DO NOTHING` on every `INSERT INTO connector_document_checksum` row, so at most one `doc_id` is ever registered per `(checksum, connector_id)` pair.

---

#### 5.3.3 Detach — `DELETE /v1/connectors/{connector_id}`

```text
DB operations (Detach)
────────────────────────────────────────────────────────────────────

Pre-condition check: if sync_status == 'syncing' → return 409 Conflict

Step 1 — remove APScheduler job (not a DB operation)
  scheduler.remove_job(connector_id)
  ← prevents any new tick from starting during teardown

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
  glob staging/connectors/<connector_id>-* → rm -rf each match
```

**Invariant:** after Step 4, no row in `connector_document_checksum` has `connector_id = :connector_id`.

---

#### 5.3.4 Manual Sync — `POST /v1/connectors/{connector_id}/sync`

```text
DB operations (Manual Sync)
────────────────────────────────────────────────────────────────────

Step 1 — existence check
  SELECT * FROM connectors WHERE id = :connector_id
  → None → 404 Not Found (no DB write)

Step 2 — atomic lock acquisition  [try_acquire_sync_lock]
  UPDATE connectors
  SET    sync_status = 'syncing'
  WHERE  id          = :connector_id
    AND  sync_status != 'syncing'
  RETURNING id

  → None returned  (connector already syncing)
       → return immediately; no further DB writes
  → id returned    (lock acquired)
       → proceed to Step 3

Step 3 — open sync-log row  [open_new_sync_log]
  INSERT INTO connector_sync_logs
      (connector_id, seq, started_at, status)
  SELECT :connector_id,
         COALESCE(MAX(seq), 0) + 1,
         NOW(),
         'started'
  FROM connector_sync_logs
  WHERE connector_id = :connector_id
  RETURNING seq          ← stored as sync_seq

Step 4 — dispatch background task (not a DB operation)
  asyncio.create_task(_run_tick(connector_id))
  → _run_tick proceeds through Phases 2–5 (see §5.3.2)
  → on completion: close_sync_log(sync_seq, status='completed'|'failed')
                   UPDATE connectors SET last_sync_at=NOW(), sync_status=:final_status
```

**Race safety:** Step 2 is a single `UPDATE … RETURNING` — Postgres serialises concurrent writers at the row level. Two simultaneous `POST /sync` calls cannot both acquire the lock.

---

## 6. Scanner Abstraction

**New file:** `services/digitize/connectors/base_scanner.py`

### 6.1 Responsibility split

Base scanner responsibilities: hold connector config, decrypt encrypted credentials, define the interface used by the sync tick.

Subclass responsibilities: remote listing (yields `(remote_path, checksum)` pairs for **all** files found), file download on demand, connection lifecycle.

> Dedup classification (skip vs ingest) and orphan detection are performed in `_classify()` (§8.4), not in the scanner.

### 6.2 Class diagram

```text
BaseScanner
  ├─ connect()
  ├─ scan()    → list[(remote_path, checksum)]   # ALL remote files, no dedup filtering
  ├─ download_to(remote_path, local_path)
  └─ close()

BaseScanner
  ├─ SFTPScanner   (checksum = remotely-computed MD5)
  └─ S3Scanner     (checksum = S3 ETag from list_objects_v2)
```

### 6.3 Interface stub

```python
from abc import ABC, abstractmethod
from pathlib import Path


class BaseScanner(ABC):
    def __init__(self, config: dict) -> None:
        self._config = config

    @abstractmethod
    def connect(self) -> None: ...

    @abstractmethod
    def scan(self) -> list[tuple[str, str]]:
        """Return (remote_path, checksum) for ALL files found on the remote source.
        No dedup filtering is applied here — _classify() splits the result.
        """
        ...

    @abstractmethod
    def download_to(self, remote_path: str, local_path: Path) -> None: ...

    @abstractmethod
    def close(self) -> None: ...
```

### 6.4 Factory dispatch

Factory dispatch lives in `services/digitize/connectors/scanner.py`:

- `ssh` → `SFTPScanner`
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

**New file:** `services/digitize/connectors/s3_scanner.py`

**Detailed design:** [S3 Scanner — Detailed Design Proposal](./s3-scanner-proposal.md)

Behavior:

- Auto-detect provider (AWS S3 or IBM COS) from `endpoint_url` hostname
- Build boto3 client per tick using `IBMCOSConnector._build_client()`
- List objects via `list_objects_v2` paginator — yields `(key, checksum)` where checksum = S3 ETag; **the full list is returned without filtering**
- Download files on demand (only those placed on `ingest_list` by `_run_tick`)
- Store checksum in `connector_document_checksum.checksum` and `documents.metadata.source_checksum`

S3 scan sketch:

```python
def scan(self) -> list[tuple[str, str]]:
    all_files = []
    for key, checksum in self._list_document_keys():
        all_files.append((key, checksum))
    return all_files


def download_and_register(self, key: str, checksum: str, staging_dir: Path) -> str:
    local_path = staging_dir / Path(key).name
    with open(local_path, "wb") as fh:
        self._client.download_fileobj(Bucket=self._cfg.bucket_name, Key=key, Fileobj=fh)
    doc_id = run_ingest_pipeline(local_path, connector_id=self._connector_id)
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
    **({} if is_aws else {"endpoint_url": cfg.endpoint_url}),
)
```

---

## 8. Sync Tick

**New file:** `services/digitize/connectors/sync_tick.py`

`_run_tick()` is an async coroutine that owns the end-to-end sync logic for one connector tick. It is dispatched by the APScheduler job or by `POST /v1/connectors/{id}/sync`.

### 8.1 Tick flow

![Sync Tick Flow](sync-worker-tick-flow.svg)

```text
_run_tick(connector_id)
│
├─ [Phase 1] INSERT connector_sync_logs (status='started')
│            sync_status already 'syncing' (set before dispatch)
│
├─ [Phase 2] known_checksums ← SELECT checksum FROM connector_document_checksum
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
│             download → create_job(connector_id, checksum) → doc_id (session creation)
│             add_connector_checksum_entry(connector_id, checksum, doc_id)
│             UPDATE documents.metadata
│
├─ [Phase 4b] _delete_orphans(orphan_checksums)
│   ← RUNS AFTER all Phase 4a writes finish ←
│             orphan_checksums = known_checksums − scanned_checksums
│             remove_connector_checksum_entry → if remaining==0: DELETE /v1/documents/{doc_id}
│
└─ [Phase 5] UPDATE connector_sync_logs (finished_at, counters, status)
             UPDATE connectors (last_sync_at, sync_status)
```

### 8.2 Tick rules

- **Overlapping ticks for the same connector are prevented at the DB level.** APScheduler's `max_instances=1` will not dispatch a new tick while `sync_status == 'syncing'`. `POST /sync` uses an atomic `UPDATE … WHERE sync_status != 'syncing' RETURNING id` — if no row is returned the request is a no-op and `_run_tick` is never called.
- Cross-connector duplicate ingestion is handled by `ON CONFLICT (checksum, connector_id) DO NOTHING` on every `INSERT INTO connector_document_checksum`. No process-level lock is required.
- `new_files` is updated live during staging and download.
- Each file in a tick gets its own uniquely-named staging directory: `staging/connectors/<connector_id>-<job_id>-<batch_number>/`. The `job_id` is the UUID returned by `create_job()`, and `batch_number` is the zero-based index of the file within the tick's `ingest_list`. This naming makes every staging directory traceable to a specific connector, job, and position in the batch.
- The staging directory is created immediately before `scanner.download()` and removed in the `finally` block after ingest, regardless of success or failure — before the next file is downloaded. No two batch directories exist simultaneously.
- Download and ingest are `await`-able operations; the event loop is not blocked.
- Fatal errors (unhandled exceptions escaping `_run_tick`) are caught by the top-level exception handler, which writes `failed: <error>` to `connectors.sync_status` and closes any open sync-log row.
- Per-file failures are counted and summarised instead of failing the whole connector. Staging cleanup still runs for each file even when ingest fails.
- Cross-connector duplicates are registered inline during Phase 3 classification — no deferred list.
- **Phase 4b (orphan removal) always runs after Phase 4a (all new-file ingest jobs) completes.**

### 8.3 Tick stub

```python
async def _run_tick(connector_id: str) -> None:
    config = get_active_connector(connector_id)
    sync_seq = open_new_sync_log(connector_id)  # seq auto-generated by DB
    scanner = build_scanner(config)
    try:
        scanner.connect()
        scanned_files: list[tuple[str, str]] = scanner.scan()

        known_checksums: set[str] = set(list_connector_checksums(connector_id))
        all_checksums: set[str] = set(list_all_checksums())

        ingest_list, orphan_checksums = _classify(
            connector_id, scanned_files, known_checksums, all_checksums
        )
        await _process_new_files(sync_seq, connector_id, scanner, ingest_list)
        # Orphan removal runs only after all Phase 4a writes complete
        await _delete_orphans(connector_id, orphan_checksums)
        _complete_tick(sync_seq, connector_id)
    except Exception as exc:
        logger.error(f"Tick failed for connector {connector_id!r}: {exc}", exc_info=True)
        _fail_tick(sync_seq, connector_id, exc)
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

`_classify` is a module-level function. It receives `connector_id`, the full scanner output, `known_checksums` (this connector's owned checksums), and `all_checksums` (all checksums across all connectors). It produces:

| Collection | Type | Contents |
| --- | --- | --- |
| `ingest_list` | `list[tuple[str, str]]` | Brand new to all connectors — download, ingest, register |
| `orphan_checksums` | `set[str]` | Previously owned by this connector, no longer on remote source |

Cross-connector duplicates (`checksum IN all_checksums but NOT known_checksums`) are handled inline: `_classify` immediately calls `lookup_connector_content_by_checksum` and `add_connector_checksum_entry` before moving on. Intra-tick dedup still applies — only the first occurrence of a checksum triggers the DB write.

```python
def _classify(
    connector_id: str,
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
                add_connector_checksum_entry(connector_id, checksum, existing_doc_id)
        else:
            if checksum not in seen_this_tick:
                seen_this_tick.add(checksum)
                ingest_list.append((remote_path, checksum))

    orphan_checksums = known_checksums - scanned_checksums
    return ingest_list, orphan_checksums
```

> **Ordering invariant:** `_delete_orphans(orphan_checksums)` is called only after all Phase 4a writes complete, guaranteeing a checksum registered in Phase 4a is never simultaneously processed as an orphan in Phase 4b.

---

## 9. Connector Scheduler

**New file:** `services/digitize/connectors/scheduler.py`

`ConnectorScheduler` is a thin wrapper around an APScheduler `AsyncScheduler` singleton backed by a `PostgresDataStore`. It manages one `IntervalTrigger` job per connector and is the sole entry point for starting, stopping, and recovering sync jobs.

### 9.1 Responsibilities

- Register one `IntervalTrigger` job per connector (`job_id = connector_id`) on `POST /v1/connectors`
- Remove the job on `DELETE /v1/connectors/{id}`
- Recover jobs for all persisted connectors on application startup (lifespan hook)
- Provide `trigger_now(connector_id)` for use by `POST /v1/connectors/{id}/sync`

PUT does not reschedule the job — `_run_tick` reads config from the DB at the start of every tick.

### 9.2 Scheduler setup stub

Jobs are stored in Postgres via APScheduler's `AsyncSQLAlchemyDataStore` so that job state survives process restarts. An event broker is not configured — this is intentional for the current single-instance deployment. It can be added later (e.g. `AsyncpgEventBroker`) when scaling to multiple instances.

```python
from apscheduler import AsyncScheduler
from apscheduler.datastores.async_sqlalchemy import AsyncSQLAlchemyDataStore
from apscheduler.triggers.interval import IntervalTrigger
from sqlalchemy.ext.asyncio import create_async_engine

from common.db.connection import get_database_url

def _make_async_db_url() -> str:
    """Convert the shared PostgreSQL URL to an asyncpg-compatible URL."""
    return get_database_url().replace("postgresql://", "postgresql+asyncpg://", 1)


# Module-level objects — initialised in lifespan()
_async_engine = create_async_engine(_make_async_db_url())
_data_store = AsyncSQLAlchemyDataStore(_async_engine)
_scheduler: AsyncScheduler = AsyncScheduler(data_store=_data_store)


async def register_connector_job(connector_id: str, interval_seconds: int) -> None:
    """Register a periodic sync job. Fires once immediately on registration."""
    await _scheduler.add_job(
        _run_tick,
        IntervalTrigger(seconds=interval_seconds),
        args=[connector_id],
        id=connector_id,
        max_instances=1,        # prevents overlapping ticks for the same connector
        replace_existing=True,  # safe for lifespan recovery
        next_run_time=datetime.now(UTC),  # fire first tick immediately
    )


async def remove_connector_job(connector_id: str) -> None:
    """Remove the periodic job. Called on DELETE /v1/connectors/{id}."""
    await _scheduler.remove_job(connector_id)


async def trigger_now(connector_id: str) -> None:
    """Fire a one-shot tick immediately. Used by POST /v1/connectors/{id}/sync."""
    await _scheduler.add_job(
        _run_tick,
        args=[connector_id],
        id=f"{connector_id}-manual",
        replace_existing=True,
    )
```

**New dependencies** (add to `services/digitize/requirements.txt`):

```
apscheduler[asyncpg]
```

> `asyncpg` is the async Postgres driver required by `AsyncSQLAlchemyDataStore`. The `[asyncpg]` extra installs it alongside APScheduler.

### 9.3 Lifecycle

Started and stopped in the FastAPI `lifespan()` hook. Because job state lives in Postgres, the scheduler rehydrates all jobs from the data store on startup automatically. `register_connector_job()` is still called for each known connector with `replace_existing=True` to re-register any connector whose interval or next-run-time may have drifted while the instance was down.

```python
@asynccontextmanager
async def lifespan(app: FastAPI):
    async with AsyncScheduler(data_store=_data_store) as scheduler:
        _scheduler = scheduler
        # Re-register jobs so interval/next_run_time stay consistent with DB config.
        # replace_existing=True makes this a no-op for jobs that are already current.
        for connector in list_connectors():
            await register_connector_job(connector.id, connector.sync_interval_seconds)
        yield
    # Scheduler shuts down automatically on async-context exit.
```

![Scheduler Job Lifecycle](scheduler-lifecycle.svg)

---

## 10. Scheduler Resilience

### 10.1 Concurrency Model

APScheduler runs jobs as coroutines on the same asyncio event loop as FastAPI/Uvicorn — fully async-native, no thread-pool overhead. `max_instances=1` on each job ensures no two ticks for the same connector overlap within the process. Cross-connector concurrency (two different connectors ticking simultaneously) is safe — all shared state is protected by DB-level `ON CONFLICT DO NOTHING` constraints on `connector_document_checksum`.

A failing tick for one connector does not affect other connectors' scheduled jobs.

### 10.2 Tick Failure Handling

An unhandled exception inside `_run_tick` is caught by the outer `try/except` in the stub (§8.3). The handler:

1. Logs the error with full traceback.
2. Calls `_fail_tick(sync_seq, connector_id, exc)`, which writes `failed: <error>` to `connectors.sync_status` and closes any open sync-log row.

APScheduler's `IntervalTrigger` will fire the next scheduled tick regardless — failed ticks do not stop the schedule.

### 10.3 Lifespan Recovery

Because job state is persisted in Postgres, the instance that restarts automatically picks up the correct next-run-times from the data store. `register_connector_job()` is still called during lifespan startup with `replace_existing=True` to ensure interval and next-run-time are consistent with the current DB config; for jobs that are already up-to-date this is a cheap no-op.

### 10.4 No-Overlap Guard on `POST /sync`

`POST /v1/connectors/{id}/sync` must not start a tick if one is already running. The guard is a DB-level atomic operation:

```sql
UPDATE connectors
SET sync_status = 'syncing'
WHERE id = :connector_id
  AND sync_status != 'syncing'
RETURNING id
```

If no row is returned, the endpoint returns `202` immediately without dispatching a task. This is race-safe: two concurrent requests both executing the `UPDATE` are serialised by Postgres row locking — only one wins.

### 10.5 DELETE During Idle vs. Running Tick

| Connector state at DELETE time | Behaviour |
| --- | --- |
| `sync_status != 'syncing'` | `remove_connector_job()` → proceed with DB teardown |
| `sync_status == 'syncing'` | Return `409 Conflict`; caller retries after the tick completes |

> **Future:** when task cancellation is supported, DELETE will cancel the running task and wait for cleanup rather than returning `409`.

### 10.6 Multi-Instance Consideration

The current deployment runs a **single `digitize` instance**. The `AsyncSQLAlchemyDataStore` already persists jobs to Postgres, which is the prerequisite for scaling. When multiple instances are needed, add an `AsyncpgEventBroker` to the scheduler construction (§9.2) — this enables `LISTEN`/`NOTIFY`-based coordination so instances are notified of job changes in real time rather than polling. APScheduler's data-store advisory locks will then prevent duplicate ticks across instances with no other infrastructure changes required.

---

## 11. Implementation Plan — Digitize Connector PRs

Each PR is independently testable.

---

### PR 1 — DB Schema + ORM Models + Settings ✅

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

### PR 2 — DB Operations Layer ✅

**Files touched:** `services/digitize/db/manager.py`

**What was built:**
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

### PR 3 — REST API Endpoints ✅

**Files touched:** connector router/handler file(s)

**What was built:**
- `POST /v1/connectors` — validate body, encrypt secrets, call `insert_connector`, return `202` (scheduler wiring added in PR 7)
- `PUT /v1/connectors/{id}` — partial update, re-encrypt if credentials included, call `upsert_connector`, return `200`
- `DELETE /v1/connectors/{id}` — guard check, DB teardown (checksum removal, document deletion, connector row delete), staging sweep (scheduler wiring added in PR 7)
- `GET /v1/connectors` and `GET /v1/connectors/{id}` — read from DB, strip secret fields
- `GET /v1/connectors/{id}/syncs` — paginated query with `limit`/`offset`
- **Updated `GET /v1/documents` and `GET /v1/documents/{doc_id}`** to exclude connector-sourced docs via `NOT EXISTS (SELECT 1 FROM connector_document_checksum WHERE doc_id = ...)` filter
- **Updated `DELETE /v1/documents/{doc_id}`** to return `404` for connector-sourced docs

> **Note:** `POST /v1/connectors/{id}/sync` route exists but the `_run_tick` dispatch is **not yet wired** — completed in PR 7.

**How to test:**
- Integration tests using `httpx.AsyncClient` + test DB
- Assert secrets are never returned in GET responses
- Assert `PUT` with partial `connection_details` only overwrites provided keys
- Assert correct HTTP status codes for 404/409/401 paths
- Assert `GET /v1/documents` does not return docs whose `doc_id` appears in `connector_document_checksum`
- Assert `GET /v1/documents/{doc_id}` and `DELETE /v1/documents/{doc_id}` return `404` for connector-sourced docs
- Assert `GET /v1/jobs` returns all jobs including connector-initiated ones

---

### PR 4 — Scanner Abstraction + SFTP Scanner ✅

**Files touched:** `connectors/base_scanner.py`, `connectors/scanner.py`, `connectors/sftp_scanner.py`

**What was built:**
- `BaseScanner` ABC with `connect()`, `scan()`, `download_to()`, `close()`
- `build_scanner()` factory — dispatches on `type`
- `SFTPScanner` — Paramiko connection (SFTP + SSH), recursive walk, extension filter, remote MD5 via `ssh.exec_command(f'md5sum "{remote_file_path}"')`, staged download; `scan()` returns **all** files

**How to test:**
- Unit test against a local mock SFTP server (e.g. `pytest-sftpserver` or `paramiko.SFTPServer` in a thread)
- Assert extension filtering works correctly
- Assert MD5 is computed via remote `md5sum` exec (not by streaming bytes)
- Assert `scan()` returns ALL allowed remote files without filtering against known_checksums
- Assert `build_scanner("ssh", config)` returns an `SFTPScanner` instance

---

### PR 5 — S3 Scanner ✅

**Files touched:** `connectors/s3_scanner.py`

**What was built:**
- `S3Scanner` — boto3 `list_objects_v2` paginator, extension filter, `download_fileobj()`, staged download; `scan()` returns **all** allowed objects without dedup filtering; checksum (S3 ETag) registered via `add_connector_checksum_entry()` on job completion; `set_document_metadata()` stores checksum and key. `upsert_file_checksum()` must NOT be called.

**How to test:**
- Unit test with `moto` (mock AWS) — assert listing, extension filtering, and download
- Assert `scan()` returns ALL allowed objects without pre-filtering against known_checksums
- Assert `build_scanner("s3", config)` returns an `S3Scanner` instance

---

### PR 6 — `_classify()` + `_run_tick()` core logic

**Files touched:** `services/digitize/connectors/sync_tick.py`

This PR implements the two central coroutine-level pieces of the sync task: the classification function and the tick entry point. It has no scheduler dependency and can be reviewed and tested entirely in isolation.

**Step 1 — Module skeleton**

Create `sync_tick.py`. No module-level lock is needed — overlap prevention is handled entirely at the DB layer:

- **Same-connector overlap:** the `sync_status = 'syncing'` row state prevents a second tick from ever being dispatched (APScheduler `max_instances=1`; `POST /sync` atomic guard via `try_acquire_sync_lock()`).

**Step 2 — `_classify()`**

Implement the pure classification function (§8.4):

- Input: `connector_id`, `scanned_files`, `known_checksums`, `all_checksums`
- Output: `(ingest_list, orphan_checksums)`
- Handles cross-connector dups inline (`lookup_connector_content_by_checksum` + `add_connector_checksum_entry`)
- Intra-tick dedup: only first occurrence of a checksum per tick acts

**Step 3 — `_process_new_files()`**

Implement the ingest helper:

- Iterates `ingest_list`; for each `(remote_path, checksum)`:
  - Downloads file into `staging/connectors/<connector_id>-<sync_seq>-<batch_n>/`
  - Calls `create_job(connector_id=connector_id, checksum=checksum)` — **do not** call `upsert_file_checksum`
  - On success: calls `add_connector_checksum_entry(connector_id, checksum, doc_id)`
  - On per-file failure: increments `failed_files` counter, logs, continues
  - Calls `update_sync_log(sync_seq, new_files=..., failed_files=...)` live

**Step 4 — `_delete_orphans()`**

Implement the orphan removal helper:

- Iterates `orphan_checksums`; for each:
  - Calls `remove_connector_checksum_entry(connector_id, checksum)` → returns `(remaining, doc_id)`
  - If `remaining == 0`: calls `DELETE /v1/documents/{doc_id}` — treat `200`, `204`, `404` as success; log and continue on `5xx`

**Step 5 — `_run_tick()`**

Assemble the full async coroutine (§8.3):

- Loads config via `get_active_connector(connector_id)`
- Calls `open_new_sync_log(connector_id)` → `sync_seq`
- Builds scanner via `build_scanner(config)`
- Reads `known_checksums` + `all_checksums`; calls `_classify()`; calls `_process_new_files()`
- Calls `_delete_orphans()`
- Calls `close_sync_log(sync_seq, status='completed', ...)` + `UPDATE connectors SET last_sync_at, sync_status`
- Outer `try/except`: on unhandled exception calls `_fail_tick(sync_seq, connector_id, exc)` which writes `failed: <error>` to `connectors.sync_status` and closes the open sync-log row
- `finally`: calls `scanner.close()`

**How to test:**
- `_classify` — two scanned paths with the same checksum: assert only the first is placed on `ingest_list`
- `_classify` — checksum in `known_checksums`: assert it appears in neither `ingest_list` nor triggers a DB write
- `_classify` — checksum in `all_checksums` but not `known_checksums`: assert `add_connector_checksum_entry` is called once (intra-tick dedup)
- `_classify` — checksum in `known_checksums` but absent from `scanned_files`: assert it appears in `orphan_checksums`
- `_process_new_files` — per-file exception: assert `failed_files` incremented, remaining files still processed
- `_delete_orphans` — `remaining == 0` path: assert `DELETE /v1/documents/{doc_id}` is called
- `_delete_orphans` — `remaining > 0` path: assert document deletion is skipped
- `_run_tick` — unhandled exception escaping Phase 2: assert `_fail_tick` writes `failed: <error>` to DB and closes sync-log
- `_run_tick` — assert `_delete_orphans` is called only after `_process_new_files` returns (ordering invariant)
- `_run_tick` — assert `_run_tick` is never called when `sync_status == 'syncing'` (DB guard enforced before dispatch, not inside the tick itself)

---

### PR 7 — Scheduler + Lifespan Recovery + `POST /sync` Dispatch

**Files touched:** `services/digitize/connectors/scheduler.py`, `services/digitize/app.py` (lifespan hook), connector router/handler, `services/digitize/requirements.txt`

This PR wires the scheduler into the application and completes the last unimplemented endpoint piece.

**Step 0 — Dependency**

Add `apscheduler[asyncpg]` to `services/digitize/requirements.txt`. This pulls in `asyncpg` alongside APScheduler, which is required by `AsyncSQLAlchemyDataStore` and `AsyncpgEventBroker`.

**Step 1 — `ConnectorScheduler` module**

Create `scheduler.py` with the APScheduler `AsyncScheduler` singleton backed by `AsyncSQLAlchemyDataStore` + `AsyncpgEventBroker`, and three public functions (§9.2):

- `_make_async_db_url()` — converts the shared `postgresql://` URL from `get_database_url()` to `postgresql+asyncpg://`
- Module-level `_async_engine`, `_data_store`, `_scheduler` constructed at import time
- `register_connector_job(connector_id, interval_seconds)` — `IntervalTrigger`, `max_instances=1`, `replace_existing=True`, `next_run_time=datetime.now(UTC)`
- `remove_connector_job(connector_id)` — `await _scheduler.remove_job(connector_id)`
- `trigger_now(connector_id)` — one-shot `add_job` with `replace_existing=True`; used only by `POST /sync`

**Step 2 — Lifespan hook**

In `app.py`, use the scheduler as an `async with` context manager (it starts and shuts down automatically):

```python
async with AsyncScheduler(data_store=_data_store) as scheduler:
    _scheduler = scheduler
    for connector in list_connectors():
        await register_connector_job(connector.id, connector.sync_interval_seconds)
    yield
```

- The `async with` block replaces the old `start_in_background()` / `shutdown()` pair.
- `register_connector_job()` is called for every known connector with `replace_existing=True` so that interval/next-run-time are up-to-date; it is a no-op when the job is already current in the data store.

**Step 3 — Wire `POST /v1/connectors`**

In the connector router: after `insert_connector(...)` succeeds, call `await register_connector_job(connector_id, interval_seconds)`.

**Step 4 — Wire `DELETE /v1/connectors/{id}`**

In the connector router: after the `sync_status != 'syncing'` guard passes, call `await remove_connector_job(connector_id)` **before** any DB teardown begins.

**Step 5 — Wire `POST /v1/connectors/{id}/sync`**

In the connector router, complete the endpoint (§3.7):

1. `get_active_connector(connector_id)` — `404` if missing
2. `try_acquire_sync_lock(connector_id)` — if `None` returned, return `202` immediately (no-op)
3. `open_new_sync_log(connector_id)` → `sync_seq`
4. `asyncio.create_task(_run_tick(connector_id))` — background, non-blocking
5. Return `202 Accepted`

**How to test:**
- `register_connector_job` — assert `add_job` called with `max_instances=1`, `replace_existing=True`, `next_run_time` set to now
- `register_connector_job` called twice with same `connector_id` — assert no error raised (idempotent via `replace_existing=True`)
- `remove_connector_job` — assert `remove_job` called with the correct `connector_id`
- `trigger_now` — assert `add_job` called with `replace_existing=True` and no `IntervalTrigger`
- Lifespan hook — start app with 2 connectors in DB: assert `register_connector_job` called for each on startup
- Lifespan hook — assert scheduler context exits cleanly (no explicit `shutdown()` call needed)
- `POST /v1/connectors` integration test — assert scheduler job registered after successful attach; job is visible in the data store (survives a scheduler restart)
- `DELETE /v1/connectors/{id}` integration test — assert job removed from data store before DB teardown; assert `409` returned if `sync_status == 'syncing'`
- `POST /sync` — idle connector: assert `202`, `_run_tick` dispatched as a background task, sync-log row opened
- `POST /sync` — already syncing: assert `202` returned immediately, no second task dispatched, no new sync-log row inserted
- `POST /sync` — unknown connector: assert `404`
