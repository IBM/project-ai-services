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

Removes a connector and its runtime state. The HTTP response is **always `204` and is returned immediately** — the caller never waits for teardown to finish. All cleanup work (tick cancellation, scheduler job removal, checksum row removal, document deletion, staging sweep) runs in a fire-and-forget `asyncio` background task that is dispatched before the response is sent.

Delete flow (API handler):

1. Existence check — `GET active_connector(connector_id)` → `404` if not found.
2. Signal cancellation via `signal_connector_delete(connector_id)` — sets a `deletion_requested` flag in the in-process registry so any live tick sees it at the next await boundary. Does **not** wait for the tick to stop.
3. Dispatch `_run_teardown(connector_id)` as an `asyncio.create_task` background task.
4. Return `204 No Content` immediately.

`_run_teardown` (background, not awaited by the handler):

1. Call `cancel_connector_tick(connector_id)` — fires `task.cancel()` on the live tick (if any) and awaits its exit with a safety-net timeout. The tick writes `status='cancelled'` and resets `sync_status='up to date'` before exiting.
2. Remove the APScheduler job (`remove_connector_job(connector_id)`) — no new tick can start from this point.
3. Snapshot the connector's known checksums.
4. Remove membership rows checksum by checksum; delete documents when the remaining reference count reaches zero.
5. Delete the `connectors` row (cascades to `connector_sync_logs`).
6. Best-effort cleanup of staging directories.

#### Delete sequence diagram

```text
DELETE /v1/connectors/{connector_id}   [API handler — fast path]
  │
  ├─ 1. get_active_connector(connector_id) → None: 404
  │
  ├─ 2. signal_connector_delete(connector_id)
  │        sets _pending_deletions.add(connector_id)
  │        ← tick polls this flag; raises DeleteRequestedError at next await boundary
  │
  ├─ 3. asyncio.create_task(_run_teardown(connector_id))
  │
  └─ 4. return 204 No Content  ← response sent immediately
                ↓
         [background — _run_teardown]
           cancel_connector_tick(connector_id)
             → task.cancel()
             → await task (safety-net timeout=30 s; on timeout: abandon, log warning)
             ← tick writes status='cancelled', sync_status='up to date'
           remove_connector_job(connector_id)
             ← no new tick can start from this point
           list checksums owned by this connector
           for each checksum:
             remove_connector_checksum_entry(connector_id, checksum)
               → DELETE row WHERE checksum = :checksum AND connector_id = :connector_id
               → returns (remaining_owner_count, doc_id)
             if remaining_owner_count == 0:
               DELETE /v1/documents/{doc_id}
           delete connector row
           cleanup staging dirs: glob staging/connectors/<connector_id>-* and remove each match
```

> **Note:** any processing jobs that the cancelled tick had already dispatched are left in their current state (`accepted` or `in_progress`). They will eventually time out or be cleaned up by the job-cancellation enhancement (PR 8). The connector row and all checksum ownership records are fully removed regardless.

Document deletion is best-effort: `200`, `204`, `404` from `DELETE /v1/documents/{doc_id}` are treated as success; `5xx` or network failures are logged and cleanup continues.

Response codes:

| Status | Meaning |
| --- | --- |
| `204 No Content` | Teardown initiated — connector will be fully removed in the background |
| `404 Not Found` | Connector does not exist |

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

`POST /sync` and the APScheduler `IntervalTrigger` job both gate on the same DB-level lock — `try_acquire_sync_lock()`. The APScheduler path calls this on entry to `_run_tick_wrapped` and exits silently if the lock cannot be acquired (connector already syncing). `POST /sync` calls the same function in the request handler before dispatching `asyncio.create_task(_run_tick(connector_id))` — if the lock is unavailable it returns `202` immediately as a no-op, with no APScheduler involvement at all.

`POST /sync` does **not** add or modify any APScheduler job. It acquires the DB lock, opens the sync-log row, and dispatches `_run_tick` as a bare `asyncio.create_task`. APScheduler's `trigger_now()` helper is **not used** — keeping `POST /sync` entirely off the APScheduler dispatch path eliminates the second concurrent-dispatch channel and ensures both paths share the same single lock.

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

Phase 0 — acquire sync lock  [both paths — APScheduler and POST /sync]
  try_acquire_sync_lock(connector_id)
    UPDATE connectors SET sync_status = 'syncing'
    WHERE id = :connector_id AND sync_status != 'syncing'
    RETURNING id

  APScheduler path: called at the top of _run_tick_wrapped, before any other work.
    → None returned → connector already syncing; exit _run_tick_wrapped immediately (no-op).
    → id returned   → lock acquired; proceed to Phase 1.

  POST /sync path: called in the request handler before asyncio.create_task is dispatched.
    → None returned → return 202 immediately (no-op); _run_tick is never dispatched.
    → id returned   → proceed to open_new_sync_log and asyncio.create_task.

  Note: APScheduler max_instances=1 remains as a secondary guard against scheduler-level
  overlap, but the DB lock is the authoritative single source of truth for both paths.

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

**Concurrency guard:** same-connector overlap is prevented by the DB-level `sync_status` check — `try_acquire_sync_lock()` — which is called on entry to `_run_tick_wrapped` (APScheduler path) and in the `POST /sync` handler before `asyncio.create_task` is dispatched. Both paths share this single lock. APScheduler's `max_instances=1` is a secondary guard that prevents the scheduler from queuing a second APScheduler-dispatched job when one is already running, but the DB lock is the authoritative gate. `POST /sync` dispatches `asyncio.create_task(_run_tick(connector_id))` directly — it does not touch the APScheduler job queue. Cross-connector duplicate ingestion of the same brand-new checksum is handled at the DB level by `ON CONFLICT (checksum, connector_id) DO NOTHING` on every `INSERT INTO connector_document_checksum` row, so at most one `doc_id` is ever registered per `(checksum, connector_id)` pair.

---

#### 5.3.3 Detach — `DELETE /v1/connectors/{connector_id}`

The API handler returns `204` immediately. All DB teardown runs inside `_run_teardown(connector_id)`, an `asyncio.Task` dispatched before the response is sent.

```text
API handler (fast path — synchronous, no await on teardown)
────────────────────────────────────────────────────────────────────

Step 0 — existence check (DB read)
  SELECT * FROM connectors WHERE id = :connector_id
  → None → 404 Not Found

Step 1 — signal deletion (in-process registry, not a DB operation)
  _pending_deletions.add(connector_id)
  ← any live tick polls this set at each phase boundary; raises DeleteRequestedError

Step 2 — dispatch background teardown (not a DB operation)
  asyncio.create_task(_run_teardown(connector_id))

Step 3 — return 204 No Content immediately


_run_teardown (background task — runs after 204 is sent)
────────────────────────────────────────────────────────────────────

Step A — cancel in-flight tick (not a DB operation)
  cancel_connector_tick(connector_id)
    → _pending_deletions already set (from Step 1)
    → task.cancel() on the live asyncio.Task (from _live_tasks registry)
    → await task — no fixed timeout enforced by the caller;
      safety net is inside cancel_connector_tick itself (timeout=30 s)
    ← _cancel_tick() writes status='cancelled', resets sync_status='up to date'
    ← if safety-net fires: task is abandoned; log warning and proceed

  Note: any processing jobs the tick had already dispatched remain in their
  current state (accepted/in_progress). They are not cancelled here — that is
  deferred to the PR 8 enhancement. The connector row and all ownership records
  are fully removed regardless.

Step B — remove APScheduler job (not a DB operation)
  scheduler.remove_job(connector_id)
  ← called after tick exit; prevents any new tick from starting during teardown

Step C — snapshot owned checksums
  SELECT checksum, doc_id FROM connector_document_checksum WHERE connector_id = :connector_id

Step D — remove ownership row by row
  for (checksum, doc_id) in owned_rows:
    DELETE FROM connector_document_checksum
    WHERE checksum = :checksum AND connector_id = :connector_id

    SELECT COUNT(*) AS remaining FROM connector_document_checksum WHERE checksum = :checksum

    if remaining == 0:
      DELETE /v1/documents/{doc_id}      ← best-effort; 200/204/404 = success

Step E — delete connector row
  DELETE FROM connectors WHERE id = :connector_id
  -- CASCADE deletes connector_sync_logs rows automatically.

Step F — cleanup staging dirs (not a DB operation)
  _pending_deletions.discard(connector_id)
  glob staging/connectors/<connector_id>-* → rm -rf each match
```

**Invariant:** after Step E, no row in `connector_document_checksum` has `connector_id = :connector_id`.

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
        No dedup filtering is applied here — _classify() splits the result.
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
- Remove the job on `DELETE /v1/connectors/{id}` (from inside `_run_teardown`)
- Recover jobs for all persisted connectors on application startup (lifespan hook)
- `POST /v1/connectors/{id}/sync` does **not** interact with APScheduler — it acquires the DB lock and dispatches `asyncio.create_task(_run_tick(connector_id))` directly

PUT does not reschedule the job — `_run_tick` reads config from the DB at the start of every tick.

### 9.2 Scheduler setup stub

Jobs are stored in Postgres via APScheduler's `AsyncSQLAlchemyDataStore` so that job state survives process restarts. An event broker is not configured — this is intentional for the current single-instance deployment. It can be added later (e.g. `AsyncpgEventBroker`) when scaling to multiple instances.

> **Module-level construction:** `_async_engine`, `_data_store`, and `_scheduler` are declared as `None` at module level and constructed inside `lifespan()`. This defers engine creation to the moment the event loop is running and avoids binding the async engine to the wrong loop. All call sites that use `_scheduler` or `_loop` access them through getter functions rather than module-level names directly.

```python
import asyncio
from datetime import datetime, timezone
from typing import Optional

from apscheduler import AsyncScheduler
from apscheduler.datastores.async_sqlalchemy import AsyncSQLAlchemyDataStore
from apscheduler.triggers.interval import IntervalTrigger
from sqlalchemy.ext.asyncio import create_async_engine

from common.db.connection import get_database_url

UTC = timezone.utc

def _make_async_db_url() -> str:
    """Convert the shared PostgreSQL URL to an asyncpg-compatible URL."""
    return get_database_url().replace("postgresql://", "postgresql+asyncpg://", 1)


# Module-level handles — all set to None at import time; initialised inside lifespan().
_scheduler: Optional[AsyncScheduler] = None
_loop: Optional[asyncio.AbstractEventLoop] = None

# In-process deletion registry — connector_ids currently undergoing teardown.
# A live tick polls this set at phase boundaries and exits early when its id is present.
_pending_deletions: set[str] = set()

# Live-task registry — maps connector_id → currently-running asyncio.Task.
# Populated synchronously in _run_tick_wrapped before the first await; cleared in finally.
_live_tasks: dict[str, asyncio.Task] = {}


def _get_scheduler() -> AsyncScheduler:
    if _scheduler is None:
        raise RuntimeError("Scheduler not initialised — call from inside lifespan()")
    return _scheduler


async def register_connector_job(
    connector_id: str,
    interval_seconds: int,
    fire_immediately: bool = False,
) -> None:
    """Register a periodic sync job.

    fire_immediately=True sets next_run_time=now() — use only for brand-new connectors
    (POST /v1/connectors).  During lifespan recovery, pass fire_immediately=False so
    APScheduler rehydrates the persisted next_run_time instead of restarting all ticks
    on every process restart.
    """
    kwargs: dict = dict(
        func=_run_tick_wrapped,
        trigger=IntervalTrigger(seconds=interval_seconds),
        args=[connector_id],
        id=connector_id,
        max_instances=1,        # secondary guard: scheduler won't queue a 2nd APScheduler job
        replace_existing=True,
    )
    if fire_immediately:
        kwargs["next_run_time"] = datetime.now(UTC)
    await _get_scheduler().add_job(**kwargs)


async def remove_connector_job(connector_id: str) -> None:
    """Remove the periodic job. Called from _run_teardown after the tick has exited."""
    try:
        await _get_scheduler().remove_job(connector_id)
    except Exception:
        pass  # job may already be absent if the scheduler was restarted


async def _run_tick_wrapped(connector_id: str) -> None:
    """APScheduler entry point. Acquires the DB sync lock before doing any work;
    exits silently if the lock is unavailable (connector already syncing or pending delete).
    Registers the running asyncio.Task in _live_tasks so DELETE can cancel it."""
    # Early-exit if a DELETE is already in progress for this connector.
    if connector_id in _pending_deletions:
        return

    # Acquire the DB lock — this is the authoritative single gate shared with POST /sync.
    acquired = try_acquire_sync_lock(connector_id)  # UPDATE … WHERE sync_status != 'syncing'
    if not acquired:
        return  # already syncing; APScheduler max_instances=1 is a secondary guard only

    # Register task handle synchronously before the first await so cancel_connector_tick
    # can always find it in _live_tasks, even if DELETE arrives immediately after dispatch.
    _live_tasks[connector_id] = asyncio.current_task()
    try:
        await _run_tick(connector_id)
    finally:
        _live_tasks.pop(connector_id, None)


async def cancel_connector_tick(connector_id: str) -> None:
    """Cancel the running tick for connector_id and wait for it to exit.
    Called by _run_teardown. Does not block the DELETE response — _run_teardown
    runs in a background task that the caller does not await."""
    task = _live_tasks.get(connector_id)
    if task and not task.done():
        task.cancel()
        try:
            # No asyncio.shield — we want to wait for the real task, not a wrapper.
            # Safety-net timeout guards against a scanner thread that hangs indefinitely
            # (e.g. stalled SFTP connection). On timeout: log a warning, abandon the task,
            # and let _run_teardown proceed with DB cleanup regardless.
            await asyncio.wait_for(task, timeout=30.0)
        except asyncio.CancelledError:
            pass   # expected — task exited via CancelledError
        except asyncio.TimeoutError:
            logger.warning(
                "cancel_connector_tick: safety-net timeout for connector %r — "
                "executor thread still running in background; proceeding with teardown",
                connector_id,
            )


async def signal_connector_delete(connector_id: str) -> None:
    """Mark connector_id as pending deletion in the in-process registry.
    Called synchronously (no await) in the DELETE handler before create_task.
    The live tick polls _pending_deletions at phase boundaries and exits early."""
    _pending_deletions.add(connector_id)
```

**New dependencies** (add to `services/digitize/requirements.txt`):

```
apscheduler[asyncpg]
```

> `asyncpg` is the async Postgres driver required by `AsyncSQLAlchemyDataStore`. The `[asyncpg]` extra installs it alongside APScheduler.

### 9.3 Lifecycle

Started and stopped in the FastAPI `lifespan()` hook. The engine, data store, and scheduler are all constructed inside `lifespan()` — not at module import time — so they are bound to the event loop that Uvicorn has already started.

**Recovery behaviour:** because job state lives in Postgres, APScheduler rehydrates all jobs from the data store on startup automatically — including their persisted `next_run_time`. `register_connector_job()` is still called for each known connector (with `replace_existing=True`) to ensure `interval_seconds` stays consistent with the DB config. Critically, `fire_immediately=False` is passed during recovery so APScheduler respects the stored schedule rather than firing every connector immediately on every process restart.

```python
import scheduler as scheduler_module  # the module containing _scheduler, _live_tasks, etc.

@asynccontextmanager
async def lifespan(app: FastAPI):
    # Construct engine and data store here, after the event loop is running.
    async_engine = create_async_engine(scheduler_module._make_async_db_url())
    data_store = AsyncSQLAlchemyDataStore(async_engine)

    async with AsyncScheduler(data_store=data_store) as sched:
        # Write back to the module so all helper functions (register_connector_job,
        # remove_connector_job, cancel_connector_tick, etc.) pick up the live instance.
        scheduler_module._scheduler = sched

        # Recover existing connectors — fire_immediately=False preserves stored schedules.
        for connector in list_connectors():
            await register_connector_job(
                connector.id,
                connector.sync_interval_seconds,
                fire_immediately=False,   # ← do NOT reset next_run_time on restart
            )

        # Size the thread pool for blocking scanner I/O.
        connector_count = len(list_connectors())
        pool_size = max(connector_count + 4, 8)
        loop = asyncio.get_running_loop()
        loop.set_default_executor(ThreadPoolExecutor(max_workers=pool_size))

        yield
    # Scheduler shuts down automatically on async-context exit.
    scheduler_module._scheduler = None
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

### 10.5 DELETE Cancellation Model

The HTTP response (`204`) is returned immediately — the DELETE handler does not wait for tick cancellation or DB teardown. All cleanup runs inside `_run_teardown`, a background `asyncio.Task` dispatched by the handler before the response is sent.

| Connector state at DELETE time | _run_teardown behaviour |
| --- | --- |
| No live tick (`connector_id` not in `_live_tasks`) | Skip `cancel_connector_tick()`; proceed directly to Step B |
| Live tick exists | `cancel_connector_tick()` → await tick exit (safety-net 30 s) → proceed to Step B |

In both cases the APScheduler job is removed in Step B after tick exit, so no new tick can start during teardown.

#### 10.5.1 Tick cancellation — `asyncio` cancellation via thread-pool offload

`_run_tick` is an `asyncio` coroutine dispatched as an `asyncio.Task` on the same event loop as FastAPI. The cancellation mechanism relies on `Task.cancel()`, which injects `asyncio.CancelledError` at the next `await` point. However, this only works cleanly if the scanner operations that dominate tick duration are themselves genuinely `await`-able — i.e. they yield control back to the event loop while waiting for I/O.

**Why scanner calls must be offloaded to a thread pool**

Both concrete scanners use synchronous blocking I/O:

- `S3Scanner.scan()` and `S3Scanner.download_to()` call `boto3` (`list_objects_v2` paginator, `download_fileobj`) — boto3 is entirely synchronous and blocks the calling thread for the full duration of the network transfer. A large file download can take minutes.
- `SFTPScanner.scan()` calls Paramiko's `exec_command()` and iterates a remote directory tree — also synchronous blocking.

When these calls run directly on the asyncio event loop thread, the entire event loop is frozen for their duration. `task.cancel()` queues a cancellation, but `CancelledError` cannot be raised until the blocking call returns and the event loop regains control. This does not affect the DELETE response — the handler already returned `204` — but it does delay how quickly `_run_teardown` can advance to Step B.

The fix is to run every blocking scanner call inside `asyncio.get_running_loop().run_in_executor(None, ...)`, which moves the work to the default `ThreadPoolExecutor`. The event loop remains responsive, and `CancelledError` is injected at the `await` wrapping each executor call the moment `task.cancel()` fires.

**Blocking calls to offload**

| Call site | Blocking operation | Offload pattern |
| --- | --- | --- |
| `scanner.connect()` | SFTP handshake / boto3 `head_bucket` | `await loop.run_in_executor(None, scanner.connect)` |
| `scanner.scan()` | S3 paginator / SFTP directory walk + `md5sum` | `await loop.run_in_executor(None, scanner.scan)` |
| `scanner.download_to(path, local)` | `boto3.download_fileobj` / Paramiko SFTP get | `await loop.run_in_executor(None, scanner.download_to, path, local)` |

With this change all three calls become `await` expressions, so `CancelledError` can land between any of them — between pages during a scan, or mid-download.

**Updated `_run_tick` stub**

```python
async def _run_tick(connector_id: str) -> None:
    loop = asyncio.get_running_loop()   # get_running_loop() — not deprecated, safe inside coroutine
    config = get_active_connector(connector_id)
    sync_seq = open_new_sync_log(connector_id)
    scanner = build_scanner(config)
    try:
        await loop.run_in_executor(None, scanner.connect)
        scanned_files: list[tuple[str, str]] = await loop.run_in_executor(None, scanner.scan)

        known_checksums = set(list_connector_checksums(connector_id))
        all_checksums = set(list_all_checksums())
        ingest_list, orphan_checksums = _classify(connector_id, scanned_files, known_checksums, all_checksums)

        await _process_new_files(sync_seq, connector_id, scanner, ingest_list)
        await _delete_orphans(connector_id, orphan_checksums)
        _complete_tick(sync_seq, connector_id)
    except asyncio.CancelledError:
        # Interrupted by DELETE — record a clean cancellation and propagate.
        _cancel_tick(sync_seq, connector_id)   # writes status='cancelled', resets sync_status='up to date'
        raise                                  # must re-raise; swallowing CancelledError breaks asyncio
    except Exception as exc:
        logger.error(f"Tick failed for connector {connector_id!r}: {exc}", exc_info=True)
        _fail_tick(sync_seq, connector_id, exc)  # writes sync_status='out of sync'
    finally:
        scanner.close()
```

`_process_new_files` also wraps `scanner.download_to()` in `run_in_executor`:

```python
async def _process_new_files(sync_seq, connector_id, scanner, ingest_list):
    loop = asyncio.get_running_loop()
    staging_base = settings.digitize.staging_dir / "connectors"
    for batch_number, (remote_path, checksum) in enumerate(ingest_list):
        job_id = generate_job_id()
        batch_dir = staging_base / f"{connector_id}-{job_id}-{batch_number}"
        batch_dir.mkdir(parents=True, exist_ok=True)
        try:
            local_path = batch_dir / Path(remote_path).name
            await loop.run_in_executor(None, scanner.download_to, remote_path, local_path)
            doc_id = await create_job(connector_id, checksum, staging_dir=batch_dir)
            add_connector_checksum_entry(connector_id, checksum, doc_id)
        except asyncio.CancelledError:
            raise  # let CancelledError propagate to _run_tick's except branch
        except Exception as exc:
            logger.warning(f"Failed to ingest {remote_path!r}: {exc}")
            _increment_failed(sync_seq)
        finally:
            cleanup_staging_directory(batch_dir.name, staging_base, ignore_errors=True)
```

`CancelledError` is explicitly re-raised inside `_process_new_files` so it is not accidentally swallowed by the per-file `except Exception` handler.

**`_cancel_tick` and `_fail_tick` DB helpers**

`_cancel_tick` writes `status='cancelled'` to the open `connector_sync_logs` row and resets `connectors.sync_status` to `'up to date'`.

`_fail_tick` writes `status='failed'` to the log row and sets `connectors.sync_status` to `'out of sync'`. The string `'out of sync'` matches the values permitted by the `connectors` data model — never `'failed: <message>'`, which would violate the DB `CHECK` constraint.

**Staging directory safety during cancellation**

If `CancelledError` fires while `scanner.download_to()` is running inside the executor, the executor thread continues until the blocking call returns naturally — the cancellation only stops the coroutine from `await`-ing the result. The `finally` block in `_process_new_files` runs immediately after the `await` unblocks and removes the in-progress batch directory unconditionally, so no orphaned staging files are left behind.

**Live-task registry and race-free task handle registration**

`_run_tick_wrapped` registers `asyncio.current_task()` into `_live_tasks` **synchronously before the first `await`** — this guarantees the handle is visible to `cancel_connector_tick()` from the very first event-loop iteration the coroutine runs, closing the narrow window where a DELETE could arrive between dispatch and registration. The entry is removed in the `finally` block after `_run_tick` returns or raises.

`POST /sync` dispatches `asyncio.create_task(_run_tick(connector_id))` directly — it does not go through `_run_tick_wrapped` and therefore does not register in `_live_tasks`. Since `POST /sync` only fires when there is no live tick (the DB lock would be unavailable otherwise), this is not a gap.

**Ordering guarantee**

`remove_connector_job()` is called (Step B of `_run_teardown`) **after** `cancel_connector_tick()` returns. This closes the race window: if `remove_job` were called first, APScheduler could theoretically dispatch one more tick before the cancellation was processed. By cancelling first and only removing the job once the coroutine has fully exited, no new tick can start during teardown.

**Effective cancellation latency**

With all scanner calls offloaded to the thread pool, `CancelledError` lands at the `await` wrapping the currently-running executor call as soon as that blocking call completes in its thread. The event loop itself is never blocked, so:

- If cancellation arrives between scanner calls (between `connect`, `scan`, a `download_to`, or `create_job`) it takes effect immediately — within microseconds.
- If cancellation arrives while a blocking call is in progress in the executor thread, the coroutine is cancelled as soon as that thread finishes its current call.

The safety-net timeout (`30 s`) inside `cancel_connector_tick()` guards against a scanner implementation that hangs indefinitely (e.g. a stalled SFTP connection with no TCP keepalive). If it fires, the executor thread continues running in the background but `_run_teardown` advances past Step A and completes DB cleanup regardless. The `204` response was already sent to the client before any of this began.

**Thread pool**

One shared `ThreadPoolExecutor` is used for all three blocking call sites (`connect`, `scan`, `download_to`) across all connectors. There is no benefit to separate pools for scan vs. download — both are network-bound I/O waits, and a single pool lets threads be reused between the two phases rather than lying idle in a second pool.

*Why not dedicated threads per connector (old `SFTPPoller` model)?*
The old poller ran one `threading.Thread` per connector and bridged back to the event loop with `asyncio.run_coroutine_threadsafe()` — visibly the messiest part of that code. `run_in_executor` keeps the codebase in a single concurrency model (async/await throughout), lets async DB calls work without bridging, and eliminates per-connector thread lifecycle management. Cancellation latency is identical: both approaches wait for the current blocking call to finish before stopping.

*Pool sizing*

Python's default pool size is `min(32, os.cpu_count() + 4)`. Scanner threads are pure network I/O — they sleep in a kernel syscall the entire time, consuming no CPU. The ceiling that matters is therefore **maximum concurrent blocking threads**, not cores.

Formula: `max_workers = max_simultaneous_ticking_connectors × calls_in_flight_per_tick`

Each tick holds at most **one thread at a time** (connect → scan → one download_to at a time — sequential, not parallel). So:

```
max_workers = number of connectors that can tick simultaneously
```

`max_instances=1` on each APScheduler job means one connector uses at most one thread. With N connectors the worst case is N connectors all ticking at once, each blocked in `download_to`. Set the pool to `N + a small headroom`:

```python
# In lifespan(), after connector list is loaded:
import os
from concurrent.futures import ThreadPoolExecutor

connector_count = len(list_connectors())
pool_size = max(connector_count + 4, 8)   # headroom for growth; floor of 8
loop.set_default_executor(ThreadPoolExecutor(max_workers=pool_size))
```

*Verifying resource usage*

| What to check | How |
| --- | --- |
| Active threads at peak | `len(threading.enumerate())` in a `/debug/threads` endpoint, or attach `py-spy` |
| Pool thread count | `executor._max_workers` and `executor._threads` (CPython internals) |
| Event loop responsiveness | Time a `GET /health` during an active sync; should stay < 50 ms |
| Memory per thread | Each idle pool thread costs ~8 MB stack by default; `N=20` connectors ≈ 160 MB overhead |

*Cancelled thread behaviour*

A cancelled executor thread cannot be forcibly stopped — it runs to completion in the background. For S3 this means the in-flight `download_fileobj` call finishes its current TCP transfer. For SFTP the current `md5sum` or file-get runs to its natural end. This is the same behaviour as the old thread-per-connector poller checking a `_stop_event` flag between files — the current blocking operation always completes. The `finally` block in `_process_new_files` handles staging cleanup, and the DELETE response is not held waiting for the thread.

**Limitations**

- The live-task registry is **process-local**. Sufficient for single-instance deployment (§10.6). When scaling to multiple instances, supplement with a `cancellation_requested` boolean column on `connectors` that each tick polls at phase boundaries — a DELETE on any instance reaches the tick on any other instance via the next DB read.

### 10.6 Multi-Instance Consideration

The current deployment runs a **single `digitize` instance**. The `AsyncSQLAlchemyDataStore` already persists jobs to Postgres, which is the prerequisite for scaling. When multiple instances are needed, add an `AsyncpgEventBroker` to the scheduler construction (§9.2) — this enables `LISTEN`/`NOTIFY`-based coordination so instances are notified of job changes in real time rather than polling. APScheduler's data-store advisory locks will then prevent duplicate ticks across instances with no other infrastructure changes required.

---

## 11. Implementation Plan — Digitize Connector PRs

Each PR is independently testable. Based on the current implementation in the Digitize service, the implementation status is:

---

### Implemented PRs

### PR 1 — DB Schema + ORM Models + Settings ✅ Implemented

**Implemented in code:** [`init_schema.sql`](services/digitize/db/scripts/init_schema.sql), [`models.py`](services/digitize/db/models.py)

**What is implemented:**
- The connector tables [`connectors`](services/digitize/db/scripts/init_schema.sql), [`connector_document_checksum`](services/digitize/db/scripts/init_schema.sql), and [`connector_sync_logs`](services/digitize/db/scripts/init_schema.sql) exist in [`init_schema.sql`](services/digitize/db/scripts/init_schema.sql).
- The ORM models [`Connector`](services/digitize/db/models.py:185), [`ConnectorDocumentChecksum`](services/digitize/db/models.py:225), and [`ConnectorSyncLog`](services/digitize/db/models.py:250) are implemented.
- [`Connector.sync_interval_seconds`](services/digitize/db/models.py:198) is persisted on the connector row.
- Connector settings are already consumed from [`settings.digitize.connector.sync_interval_seconds`](services/digitize/api/v1/connectors.py:79) during connector creation.

**Verification notes:**
- The schema includes the expected indexes [`idx_cdc_connector_id`](services/digitize/db/scripts/init_schema.sql:73) and [`idx_csl_connector_started`](services/digitize/db/scripts/init_schema.sql:99).
- [`connector_document_checksum`](services/digitize/db/scripts/init_schema.sql:66) has no FK constraints and no `ON DELETE CASCADE`, matching the intended ownership model.

---

### PR 2 — DB Operations Layer ✅ Implemented

**Implemented in code:** [`manager.py`](services/digitize/db/manager.py), [`db.py`](services/digitize/utils/db.py), [`test_connector_db.py`](services/digitize/tests/test_connector_db.py)

**What is implemented:**
- Connector CRUD helpers are implemented: [`DatabaseManager.insert_connector()`](services/digitize/db/manager.py:752), [`DatabaseManager.update_connector()`](services/digitize/db/manager.py:798), [`DatabaseManager.get_connector_by_id()`](services/digitize/db/manager.py:847), [`DatabaseManager.get_all_connectors()`](services/digitize/db/manager.py:873), [`DatabaseManager.delete_connector()`](services/digitize/db/manager.py:898).
- Checksum membership helpers are implemented: [`DatabaseManager.find_connector_doc_by_checksum()`](services/digitize/db/manager.py:923), [`DatabaseManager.get_connector_checksums()`](services/digitize/db/manager.py:941), [`DatabaseManager.get_all_connector_checksums()`](services/digitize/db/manager.py:955), [`DatabaseManager.insert_connector_checksum()`](services/digitize/db/manager.py:967), [`DatabaseManager.delete_connector_checksum()`](services/digitize/db/manager.py:993).
- Sync-log helpers are implemented: [`DatabaseManager.open_sync_log()`](services/digitize/db/manager.py:1038), [`DatabaseManager.close_sync_log()`](services/digitize/db/manager.py:1079), [`DatabaseManager.update_sync_log_progress()`](services/digitize/db/manager.py:1140), [`DatabaseManager.get_sync_logs()`](services/digitize/db/manager.py:1183).
- The public wrapper functions exist in [`db.py`](services/digitize/utils/db.py:1240).
- Unit coverage exists in [`test_connector_db.py`](services/digitize/tests/test_connector_db.py:1).

**Verification notes:**
- The code implements `update_connector` rather than the proposal name `upsert_connector`, but the wrapper exposed by [`upsert_connector()`](services/digitize/utils/db.py) is present and used by the API.
- The sync-log functions are implemented and already update connector sync state when opening and closing logs.

---

### PR 3 — REST API Endpoints ✅ Implemented (except `POST /sync`)

**Implemented in code:** [`connectors.py`](services/digitize/api/v1/connectors.py), [`documents.py`](services/digitize/api/v1/documents.py), [`test_connector_endpoints.py`](services/digitize/tests/test_connector_endpoints.py)

**What is implemented:**
- [`create_connector()`](services/digitize/api/v1/connectors.py:74) implements `POST /v1/connectors` with secret encryption and DB insert.
- [`update_connector()`](services/digitize/api/v1/connectors.py:136) implements `PUT /v1/connectors/{connector_id}` with partial updates and merge-encryption flow.
- [`delete_connector()`](services/digitize/api/v1/connectors.py:212) implements `DELETE /v1/connectors/{connector_id}` with sync guard, checksum cleanup, best-effort document deletion, connector deletion, and staging sweep.
- [`list_connectors()`](services/digitize/api/v1/connectors.py:332), [`get_connector()`](services/digitize/api/v1/connectors.py:373), and [`get_sync_history()`](services/digitize/api/v1/connectors.py:417) are implemented.
- Connector visibility rules are implemented in [`list_documents()`](services/digitize/api/v1/documents.py:77), [`get_document_metadata()`](services/digitize/api/v1/documents.py:144), and [`delete_document()`](services/digitize/api/v1/documents.py:212).
- Integration coverage exists in [`test_connector_endpoints.py`](services/digitize/tests/test_connector_endpoints.py:1).

**Verification notes:**
- The implementation currently returns `201` from [`create_connector()`](services/digitize/api/v1/connectors.py:96), even though the route decorator declares `202` at [`@router.post`](services/digitize/api/v1/connectors.py:58). The tests also assert `201` in [`TestPostConnector.test_returns_201_on_success()`](services/digitize/tests/test_connector_endpoints.py:192).
- There is currently no implemented `POST /v1/connectors/{connector_id}/sync` handler in [`connectors.py`](services/digitize/api/v1/connectors.py), so the manual sync part remains unimplemented.

---

### PR 4a — Scanner Abstraction ✅ Implemented

**Implemented in code:** [`base_scanner.py`](services/digitize/connectors/scanners/base_scanner.py), [`scanner_factory.py`](services/digitize/connectors/scanners/scanner_factory.py)

**What is implemented:**
- [`BaseScanner`](services/digitize/connectors/scanners/base_scanner.py:43) is implemented with abstract lifecycle methods and concrete [`BaseScanner.verify_integrity()`](services/digitize/connectors/scanners/base_scanner.py:149).
- [`build_scanner()`](services/digitize/connectors/scanners/scanner_factory.py:43) exists and builds scanners from either dict rows or ORM-like objects.
- The scanner registry currently supports only S3 via [`_REGISTRY`](services/digitize/connectors/scanners/scanner_factory.py:37).

**Verification notes:**
- The abstraction layer is in place and working.
- SFTP support is explicitly still deferred via the commented placeholder in [`_REGISTRY`](services/digitize/connectors/scanners/scanner_factory.py:39).

---

### PR 5 — S3 Scanner ✅ Implemented

**Implemented in code:** [`s3_scanner.py`](services/digitize/connectors/scanners/s3_scanner.py), [`config.py`](services/digitize/connectors/scanners/config.py), [`scanner_factory.py`](services/digitize/connectors/scanners/scanner_factory.py), [`test_connector_scanners.py`](services/digitize/tests/test_connector_scanners.py)

**What is implemented:**
- [`S3Scanner`](services/digitize/connectors/scanners/s3_scanner.py:56) is implemented with [`connect()`](services/digitize/connectors/scanners/s3_scanner.py:78), [`scan()`](services/digitize/connectors/scanners/s3_scanner.py:113), [`download_to()`](services/digitize/connectors/scanners/s3_scanner.py:138), and [`verify_integrity()`](services/digitize/connectors/scanners/s3_scanner.py:188).
- [`S3ConnectorConfig`](services/digitize/connectors/scanners/config.py) is used by [`build_scanner()`](services/digitize/connectors/scanners/scanner_factory.py:87).
- Scanner-focused tests exist in [`test_connector_scanners.py`](services/digitize/tests/test_connector_scanners.py:1).

**Verification notes:**
- This is the only concrete scanner currently wired into the factory.
- The code and tests consistently describe classification and checksum ownership writes as work for the not-yet-implemented sync worker rather than for the scanner itself.

---

### Not yet implemented PRs

### PR 4b — SFTP Scanner ❌ Not implemented

**Expected files:** `services/digitize/connectors/scanners/sftp_scanner.py`, scanner factory registration

**What still has to be implemented:**
- Add an SFTP scanner module implementing the same contract as [`BaseScanner`](services/digitize/connectors/scanners/base_scanner.py:43).
- Add SFTP connector config parsing if needed.
- Register the scanner in [`_REGISTRY`](services/digitize/connectors/scanners/scanner_factory.py:37) for connector type `ssh`.
- Add scanner tests covering recursive listing, extension filtering, remote checksum handling, and staged downloads.

**Verification notes:**
- No [`sftp_scanner.py`](services/digitize/connectors/scanners) file exists in the codebase.
- No `paramiko` usage was found in the Digitize service.

---

### PR 6 — `_classify()` + `_run_tick()` core logic ❌ Not implemented

**Expected files:** `services/digitize/connectors/sync_tick.py` or equivalent worker/tick module

**What still has to be implemented:**
- Add a sync-tick module containing `_classify()` and `_run_tick()`.
- Implement ingest-path orchestration using the existing DB helpers such as [`open_new_sync_log()`](services/digitize/utils/db.py:1310), [`close_sync_log()`](services/digitize/utils/db.py:1322), [`update_sync_log()`](services/digitize/utils/db.py:1351), [`add_connector_checksum_entry()`](services/digitize/utils/db.py:1284), and [`remove_connector_checksum_entry()`](services/digitize/utils/db.py:1295).
- Implement orphan detection and deletion flow.
- Wire scanner usage through [`build_scanner()`](services/digitize/connectors/scanners/scanner_factory.py:43).
- Add focused tests for classification, per-file failure handling, orphan cleanup, and tick finalization.

**Verification notes:**
- No `sync_tick.py`, `_run_tick()`, or `_classify()` implementation exists in the Digitize service.
- Existing scanner documentation explicitly references the worker as not yet implemented in [`base_scanner.py`](services/digitize/connectors/scanners/base_scanner.py:20) and [`__init__.py`](services/digitize/connectors/scanners/__init__.py).

---

### PR 7 — Scheduler + Lifespan Recovery + `POST /sync` Dispatch ❌ Not implemented

**Expected files:** `services/digitize/connectors/scheduler.py`, app lifespan integration, manual sync route wiring

**What still has to be implemented:**
- Add a scheduler module (`scheduler.py`) containing: module-level `None` handles for `_scheduler`, `_loop`; `_pending_deletions` set; `_live_tasks` dict; `_get_scheduler()` guard; `register_connector_job(fire_immediately=)`, `remove_connector_job()`, `_run_tick_wrapped()`, `cancel_connector_tick()`, `signal_connector_delete()`.
- `_run_tick_wrapped()` must call `try_acquire_sync_lock()` on entry (DB lock gate for APScheduler path) and register `asyncio.current_task()` in `_live_tasks` **before** the first `await`.
- Integrate scheduler startup into [`lifespan()`](services/digitize/app.py:47): construct engine/data store/scheduler inside the hook; write back to module globals via `scheduler_module._scheduler = sched`; call `register_connector_job(..., fire_immediately=False)` for recovery (no restart storm).
- Call `register_connector_job(..., fire_immediately=True)` from [`create_connector()`](services/digitize/api/v1/connectors.py:74) after the DB insert.
- Move DELETE teardown from the synchronous handler body into `_run_teardown(connector_id)` (background `asyncio.Task`): replace the current 409 guard with `signal_connector_delete()` + `asyncio.create_task(_run_teardown(...))` + immediate `204` return.
- Add `_run_teardown()` to the scheduler module: calls `cancel_connector_tick()`, `remove_connector_job()`, checksum cleanup, connector row delete, staging sweep, `_pending_deletions.discard()`.
- Implement `POST /v1/connectors/{connector_id}/sync`: call `try_acquire_sync_lock()` in handler; if lock acquired, call `open_new_sync_log()`, then `asyncio.create_task(_run_tick(connector_id))`. **No APScheduler involvement.**
- Size the `ThreadPoolExecutor` in `lifespan()` based on connector count (see §10.5.1 pool sizing).
- Add scheduler and lifespan integration tests.

**Verification notes:**
- No APScheduler usage, `AsyncScheduler`, `register_connector_job`, `remove_connector_job`, `signal_connector_delete`, `cancel_connector_tick`, or `try_acquire_sync_lock` implementation exists in the current codebase.
- [`lifespan()`](services/digitize/app.py:47) currently performs DB setup and zombie-job recovery only; it does not register connector jobs.
- [`connectors.py`](services/digitize/api/v1/connectors.py) does not expose a manual sync endpoint yet.
- The current `delete_connector()` returns `409` when `sync_status == 'syncing'` — this is the old behaviour. PR 7 replaces it with the fire-and-forget teardown model (§3.3, §5.3.3).

---

---

### PR 9 — Live-code DELETE 409 Guard Fix ❌ Not implemented

**Prerequisite:** PR 7 (scheduler module) must be merged first — `signal_connector_delete`, `_run_teardown`, and `cancel_connector_tick` must exist before the handler can call them.

**Context:**

The current [`delete_connector()`](services/digitize/api/v1/connectors.py:212) handler returns `409 Conflict` when `sync_status == 'syncing'`. This was the original safe placeholder in PR 3 (no worker existed at the time). Now that the scheduler and cancellation infrastructure are in place (PR 7), the handler must be updated to the fire-and-forget model: return `204` immediately, and delegate all cancellation and teardown to the background `_run_teardown` task.

**What still has to be implemented:**

1. **Remove the 409 guard** — delete the block in [`delete_connector()`](services/digitize/api/v1/connectors.py:231) that checks `sync_status == SyncStatus.SYNCING` and raises `409`.

2. **Add `signal_connector_delete` call** — immediately after the existence check and before `asyncio.create_task`, call `await signal_connector_delete(connector_id)` (or `scheduler_module._pending_deletions.add(connector_id)` directly).

3. **Move all teardown logic** — remove the inline checksum loop, `delete_active_connector`, and staging sweep from the handler body. This logic belongs inside `_run_teardown(connector_id)` in the scheduler module.

4. **Dispatch background task** — replace the removed teardown body with:
   ```python
   asyncio.create_task(_run_teardown(connector_id))
   return Response(status_code=204)
   ```

5. **Update tests** — update [`test_connector_endpoints.py`](services/digitize/tests/test_connector_endpoints.py) to assert:
   - `DELETE` returns `204` when the connector is idle.
   - `DELETE` returns `204` (not `409`) when `sync_status == 'syncing'`.
   - After a small `asyncio.sleep`, the connector row is gone.

**Verification notes:**
- [`delete_connector()`](services/digitize/api/v1/connectors.py:231) currently raises `409` on syncing — intentional until this PR.
- The API spec in `@router.delete` already declares `status_code=204` with no `409` response.
- Existing tests assert `409` on syncing-state delete — those assertions must be updated to `204`.

---

### Enhancement PRs

---

### PR 8 — Cancelled Job Status (Enhancement) ❌ Not implemented

**Expected files:** `services/digitize/db/scripts/init_schema.sql`, `services/digitize/models.py`, `services/digitize/db/manager.py`, `services/digitize/utils/db.py`, `services/digitize/api/v1/connectors.py`, `services/digitize/tests/test_connector_endpoints.py`

**Context:**

`DELETE /v1/connectors/{connector_id}` is fully functional without this PR — the connector row, scheduler job, checksum ownership, and associated documents are all removed, and any running sync tick is cancelled. This PR is a follow-on UX improvement only.

Each sync tick spawns one processing job per new file via `_process_new_files`. If a tick is cancelled mid-flight, any jobs it had already dispatched remain in `accepted` or `in_progress` state indefinitely. Callers polling `GET /v1/jobs/{job_id}` would see a permanently-stuck, terminal-less state. This PR introduces a `cancelled` terminal status and wires it into the delete path so those jobs are correctly resolved.

**Prerequisite:** PR 6 (`_run_tick` / `_process_new_files`) must be merged first, since the `connector_id` metadata on jobs is written there.

**What still has to be implemented:**

1. **Schema migration** — widen the `CHECK` constraint on `jobs.status`:
   ```sql
   ALTER TABLE jobs DROP CONSTRAINT chk_job_status;
   ALTER TABLE jobs ADD CONSTRAINT chk_job_status
       CHECK (status IN ('accepted', 'in_progress', 'completed', 'failed', 'cancelled'));
   ```
   Update the `CREATE TABLE jobs` statement in [`init_schema.sql`](services/digitize/db/scripts/init_schema.sql) to match.

2. **Enum** — add `CANCELLED = "cancelled"` to [`JobStatus`](services/digitize/models.py) in [`models.py`](services/digitize/models.py):
   ```python
   class JobStatus(str, Enum):
       ACCEPTED    = "accepted"
       IN_PROGRESS = "in_progress"
       COMPLETED   = "completed"
       FAILED      = "failed"
       CANCELLED   = "cancelled"   # new — set when a connector delete interrupts a running tick
   ```

3. **DB helper** — add `cancel_connector_jobs(connector_id: str)` to [`DatabaseManager`](services/digitize/db/manager.py) and expose it via a wrapper in [`utils/db.py`](services/digitize/utils/db.py):
   ```python
   def cancel_connector_jobs(connector_id: str) -> int:
       """
       Mark all accepted/in_progress jobs belonging to connector_id as cancelled.
       Returns the number of rows updated.
       """
       result = session.execute(
           text("""
               UPDATE jobs
               SET    status       = 'cancelled',
                      completed_at = NOW()
               WHERE  metadata->>'connector_id' = :connector_id
                 AND  status IN ('accepted', 'in_progress')
           """),
           {"connector_id": connector_id},
       )
       return result.rowcount
   ```

4. **`create_job` connector metadata** — confirm (or implement) that [`create_job()`](services/digitize/utils/db.py:175) accepts and persists a `connector_id` key in `jobs.metadata` when called from `_process_new_files`. If not yet stored, add a `connector_id: Optional[str] = None` parameter and write it into the `metadata` JSONB column at insert time. This is the field the `UPDATE` in step 3 filters on.

5. **Delete handler** — call `cancel_connector_jobs(connector_id)` inside [`delete_connector()`](services/digitize/api/v1/connectors.py:212) immediately after `cancel_connector_tick()` returns and before `remove_connector_job()`. Wrap in a `try/except` and log on failure; do not propagate — this is best-effort.

6. **Tests** — add to [`test_connector_endpoints.py`](services/digitize/tests/test_connector_endpoints.py):
   - `test_delete_connector_cancels_accepted_jobs` — seed a job with `status='accepted'` and `metadata={'connector_id': cid}`, delete the connector, assert the job row has `status='cancelled'`.
   - `test_delete_connector_cancels_in_progress_jobs` — same but with `status='in_progress'`.
   - `test_delete_connector_ignores_completed_jobs` — seed a completed job, delete the connector, assert it is untouched.
   - `test_cancelled_job_is_deletable` — assert `DELETE /v1/jobs/{job_id}` on a `cancelled` job returns `204` (the active-job guard blocks only `accepted`/`in_progress` — `cancelled` passes through without any guard changes).

**Verification notes:**
- [`JobStatus`](services/digitize/models.py) currently has `ACCEPTED`, `IN_PROGRESS`, `COMPLETED`, `FAILED` only — no `CANCELLED`.
- The `chk_job_status` constraint in [`init_schema.sql`](services/digitize/db/scripts/init_schema.sql:14) does not include `'cancelled'`.
- [`delete_connector()`](services/digitize/api/v1/connectors.py:212) does not call any job-cancellation helper today — this is intentional until this PR is implemented.
- The `delete_job` guard in [`jobs.py`](services/digitize/api/v1/jobs.py:434) blocks `accepted` and `in_progress` only — `cancelled` will pass through without changes to that guard.
