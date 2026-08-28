# Digitize — Data Source Connector Processing Proposal

> **Scope:** Internal `digitize` behavior after catalog sends connector payloads. Catalog-side concerns such as key management, connector CRUD, deployment wiring and TLS provisioning remain out of scope and are treated as infrastructure-level prerequisites.

---

## 1. Preconditions

Before any `digitize` connector endpoint is called:

- Catalog has already validated the remote connector configuration.
- Catalog sends secret material in plaintext via API calls:
  - `file_system`: `private_key`
  - `object_storage`: `secret_access_key`
- `digitize` encrypts those secret fields at rest using `/run/secrets/connector_encryption_key` before persisting them in the DB.
- `/run/secrets/connector_encryption_key` is mounted before pod start.
- The `document_checksum` table and `DocumentChecksum` ORM model are used exclusively for user-submitted documents. Connector code must never read from or write to it — connector dedup is handled exclusively via `connector_document_checksum` (§2.2).

---

## 2. Architecture & Data Model

### 2.1 System Overview & Components

![Architecture Diagram](architecture-diagram.svg)

**Main Components:**
- `connectors`: Stores connector configuration, encrypted credentials, and top-level sync status.
- `connector_document_checksum`: Dedup registry for connector-sourced content. Composite PK `(checksum, connector_id)` tracks connector ownership and maps to `doc_id`.
- `connector_sync_logs`: Per-tick execution log and counters backing history queries. Composite PK `(connector_id, seq)`.
- `BaseScanner` / Scanners: Transport-specific remote I/O abstraction (`S3Scanner`, `SSHScanner`).
- `Sync Execution Engine`: `run_tick()` async coroutine executing the multi-phase sync cycle; blocking I/O offloaded to thread pool via `asyncio.to_thread`.

---

### 2.2 Schema Definitions

**File:** `services/digitize/db/scripts/init_schema.sql`

```sql
-- Connectors table
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
    error                   TEXT,
    total_files             INTEGER     NOT NULL DEFAULT 0,
    CONSTRAINT chk_connector_type CHECK (type IN ('file_system', 'object_storage'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_connectors_name ON connectors (name);

-- Connector document checksum registry (Connector-sourced documents ONLY)
-- No FK constraints, no ON DELETE CASCADE — deletion is managed by application code.
CREATE TABLE IF NOT EXISTS connector_document_checksum (
    checksum     TEXT NOT NULL,
    connector_id TEXT NOT NULL,
    doc_id       TEXT NOT NULL,
    PRIMARY KEY (checksum, connector_id)
);

CREATE INDEX IF NOT EXISTS idx_cdc_connector_id ON connector_document_checksum (connector_id);

-- Connector sync logs table
CREATE TABLE IF NOT EXISTS connector_sync_logs (
    connector_id     TEXT        NOT NULL,
    seq              INTEGER     NOT NULL,
    started_at       TIMESTAMPTZ NOT NULL,
    finished_at      TIMESTAMPTZ,
    total_files      INTEGER     NOT NULL DEFAULT 0,
    new_files        INTEGER     NOT NULL DEFAULT 0,
    removed_files    INTEGER     NOT NULL DEFAULT 0,
    status           TEXT        NOT NULL DEFAULT 'started',
    error            TEXT        NOT NULL DEFAULT '',
    CONSTRAINT pk_csl PRIMARY KEY (connector_id, seq),
    CONSTRAINT fk_csh_connector
        FOREIGN KEY (connector_id)
        REFERENCES connectors(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_csl_connector_started ON connector_sync_logs (connector_id, started_at DESC);
```

---

### 2.3 ORM Mapping

**File:** `services/digitize/db/models.py`

```python
class Connector(Base):
    __tablename__ = "connectors"

    id = mapped_column(Text, primary_key=True)
    name = mapped_column(Text, nullable=False, unique=True)
    type = mapped_column(Text, nullable=False)
    connection_details = mapped_column(JSONB, nullable=False, default={})
    allowed_extensions = mapped_column(JSONB, nullable=False, default=[])
    sync_interval_seconds = mapped_column(Integer, nullable=False, default=300)
    attached_at = mapped_column(DateTime(timezone=True), nullable=False,
                                default=lambda: datetime.now(timezone.utc))
    last_sync_at = mapped_column(DateTime(timezone=True), nullable=True)
    sync_status = mapped_column(Text, nullable=False, default=ConnectorStatus.UP_TO_DATE)
    error = mapped_column(Text, nullable=True)   # ← teardown error / credential error / last sync error
    total_files = mapped_column(Integer, nullable=False, default=0)

    sync_logs = relationship("ConnectorSyncLog", back_populates="connector",
                             cascade="all, delete-orphan")

    __table_args__ = (
        CheckConstraint("type IN ('file_system', 'object_storage')", name="chk_connector_type"),
    )


class ConnectorDocumentChecksum(Base):
    __tablename__ = "connector_document_checksum"

    checksum = mapped_column(Text, nullable=False, primary_key=True)
    connector_id = mapped_column(Text, nullable=False, primary_key=True)
    doc_id = mapped_column(Text, nullable=False)


class ConnectorSyncLog(Base):
    __tablename__ = "connector_sync_logs"

    connector_id = mapped_column(Text, ForeignKey("connectors.id", ondelete="CASCADE"),
                                 nullable=False, primary_key=True)
    seq = mapped_column(Integer, nullable=False, primary_key=True)
    started_at = mapped_column(DateTime(timezone=True), nullable=False)
    finished_at = mapped_column(DateTime(timezone=True), nullable=True)
    total_files = mapped_column(Integer, nullable=False, default=0)
    new_files = mapped_column(Integer, nullable=False, default=0)
    removed_files = mapped_column(Integer, nullable=False, default=0)
    status = mapped_column(Text, nullable=False, default=SyncLogStatus.STARTED)
    error = mapped_column(Text, nullable=False, default="")

    connector = relationship("Connector", back_populates="sync_logs")
    # PRIMARY KEY (connector_id, seq) — composite, no auto-increment id column
```

---

### 2.4 Data Model Design & Invariants

1. **Separate Registries:** `connector_document_checksum` is completely isolated from `document_checksum`. A user-submitted file and a connector file with identical hashes exist independently.
2. **Composite Primary Key `(checksum, connector_id)`:**
   - Enforces that a single connector cannot register the exact same checksum twice.
   - Allows multiple connectors to reference the same content (`doc_id`), enabling cross-connector deduplication without redundant file processing.
3. **No `ON DELETE CASCADE` on `doc_id`:** Document deletion is reference-counted in application logic. A document row in `documents` is only removed when `remaining_owner_count == 0`.
4. **Checksum Formats:**
   - **S3 single-part:** 32-character hex ETag (`MD5(file_bytes)`).
   - **S3 multi-part:** S3 multi-part ETag (`<hex>-N`).
   - **SFTP:** Remote host MD5 (`md5sum` command output — 32-char hex).

---

## 3. API Contract & Visibility Rules

### 3.1 `POST /v1/connectors`

Creates a connector, encrypts secrets at rest, persists configuration. Returns `202 Accepted` immediately. Before any write, two explicit duplicate-check SELECTs are performed (`get_connector_by_id`, `get_connector_by_name`), returning `409 Conflict` if either match. The scheduler job is registered (`fire_immediately=True`) before the DB insert so that a scheduler failure aborts the creation cleanly.

#### Request Parameters
- Common: `connector_id` (UUID, optional — auto-generated if omitted), `connector_name` (string, unique), `type` (`"file_system"` | `"object_storage"`), `allowed_extensions` (`array[string]`), `connection_details` (object).
- `sync_interval_seconds`: Read from `CONNECTOR_SYNC_INTERVAL_SECONDS` env var (default `300`). Not settable per-request.
- `file_system` `connection_details`: `host`, `username`, `remote_path`, `private_key`. Optional: `port` (default `22`).
- `object_storage` `connection_details`: `endpoint_url` (full URL with scheme), `bucket_name`, `access_key_id`, `secret_access_key`. Optional: `prefix`, `delimiter`, `download_concurrency`, `verify_ssl`.

#### Example Payloads
```json
{
  "connector_id": "c7f3a2d1-4e5b-4c6d-8f9a-0b1c2d3e4f5a",
  "connector_name": "prod-sftp-reports",
  "type": "file_system",
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
  "connector_id": "a1b2c3d4-5e6f-7a8b-9c0d-1e2f3a4b5c6d",
  "connector_name": "prod-s3-rag-docs",
  "type": "object_storage",
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

#### Responses
- `202 Accepted`: Connector created and scheduler job registered immediately (`fire_immediately=True`).
- `409 Conflict`: `connector_id` or `connector_name` already exists.

---

### 3.2 `PUT /v1/connectors/{connector_id}`

Updates connector configuration in place.
- Secrets are re-encrypted if provided.
- `connection_details` is merged key-by-key (untouched encrypted keys are preserved via `merge_and_encrypt_partial`).
- `type` and `sync_interval_seconds` cannot be modified.
- Does not restart the scheduler — the next scheduled tick reads updated config directly from DB.
- If `connection_details` is supplied, `asyncio.create_task(dispatch_sync(connector_id))` is dispatched immediately to validate the new credentials and update the connector/sync-log status.
- Returns `409 Conflict` if the connector is `DELETE_PENDING`.

#### Responses
- `200 OK`: Updated successfully.
- `404 Not Found`: Connector does not exist.
- `409 Conflict`: Duplicate `connector_name` or connector is `DELETE_PENDING`.

---

### 3.3 `DELETE /v1/connectors/{connector_id}`

Fast, non-blocking detachment. Returns `204 No Content` immediately; teardown runs asynchronously in a background task.

#### Handler Flow

```text
DELETE /v1/connectors/{connector_id} (API Handler)
  │
  ├─ 1. SELECT connector → 404 if not found
  ├─ 2. mark_connector_delete_pending(connector_id)
  │      (unconditional UPDATE connectors SET sync_status='delete pending')
  │
  ├─ [Case A] connector.sync_status was 'syncing' (read before step 2)
  │     └─ Return 204 No Content immediately
  │          └─ Running tick hits _check_interrupt_call at its next checkpoint,
  │             detects DELETE_PENDING → raises CancelledError →
  │             _handle_interrupt calls _cancel_tick then _run_teardown (from connectors.py)
  │
  └─ [Case B] connector.sync_status != 'syncing'
        ├─ asyncio.create_task(_run_teardown(connector_id))
        └─ Return 204 No Content immediately
```

#### Teardown Flow

```text
_run_teardown(connector_id)  (connectors.py — used for BOTH Case A and Case B)
  │
  ├─ Step 1: remove_connector_job(connector_id) — unregister scheduler so no new ticks fire
  │            on failure → append to deletion_errors (non-fatal; proceed)
  ├─ Step 2+3: list_connector_checksums → _remove_checksums(connector_id, owned_checksums)
  │              for each checksum: remove_connector_checksum_entry(connector_id, checksum)
  │              if remaining == 0 and doc_id → _best_effort_delete_document(doc_id)
  │              on any failure → append to deletion_errors
  ├─ Step 4: _sweep_staging_dir(connector_id, staging/connectors) — clean residual batch dirs
  │            on failure → append to deletion_errors
  ├─ [if deletion_errors] → set_connector_error(connector_id, msg) + return early
  │                          (connector row kept in 'delete pending' with error message)
  └─ Step 5: delete_active_connector(connector_id) (cascades to sync_logs)
```

> **Note:** `_run_teardown` first removes the scheduler job (Step 1) before any document cleanup, ensuring no new ticks are dispatched after teardown begins. If any step from 2–4 fails, the connector row is kept with an `error` message and the DB row is **not** deleted, allowing operators to retry or inspect. The same `_run_teardown` function handles both Case A (awaited from `_handle_interrupt` in `sync_tick.py`) and Case B (via `asyncio.create_task`).

![Delete Flow](delete-flow.svg)

---

### 3.4 `GET /v1/connectors` & `GET /v1/connectors/{connector_id}`

- `GET /v1/connectors`: Returns all connectors ordered by `attached_at` descending. Excludes secret fields. Includes `error` field (covers teardown errors, credential errors, and last-sync error messages).
- `GET /v1/connectors/{id}`: Returns full connector detail including `connection_details` (secrets stripped), `allowed_extensions`, `sync_interval_seconds`, and file processing counters (`total_files`).

---

### 3.5 Sync Endpoints — `/v1/connectors/{connector_id}/syncs`

#### `GET /v1/connectors/{connector_id}/syncs`
Returns paginated execution history from `connector_sync_logs` (`limit` default 50, max 200, `offset` 0). Results ordered newest first.

#### `GET /v1/connectors/{connector_id}/syncs/{sync_seq}`
Returns a single sync log entry identified by its sequence number. Returns `404` if the connector or the specific `sync_seq` does not exist.

#### `POST /v1/connectors/{connector_id}/syncs`
Triggers an immediate manual sync tick. Implemented via the shared `dispatch_sync(connector_id)` helper (also used by the scheduler).
- Guards: returns `404` if connector not found; raises `SyncLocked` (→ `409`) if connector is `DELETE_PENDING` or if the active sync-log row is already `CANCEL_PENDING`.
- `CANCEL_PENDING` check: reads `get_active_sync_seq()` first; if a row exists, calls `get_sync_log_status(connector_id, active_seq)` — raises `SyncLocked` only if that status is `CANCEL_PENDING`.
- If lock acquired via `try_acquire_sync_lock`: calls `init_sync_log_and_update_connector` **synchronously in the caller** (no polling), dispatches `asyncio.create_task(run_tick(connector_id, sync_seq))`, and returns `202 Accepted` with `{"sync_seq": <n>}` immediately.
- If lock unavailable (already syncing): reads `get_active_sync_seq()` and returns `202 Accepted` with the existing seq (idempotent — no duplicate tick started).

```text
POST /v1/connectors/{connector_id}/syncs  →  dispatch_sync(connector_id)
  │
  ├─ 1. get_connector_by_id() → SyncNotFound (404) if not found
  ├─ 2. connector.sync_status == DELETE_PENDING → SyncLocked (409)
  ├─ 3. get_active_sync_seq()
  │      └─ if active_seq exists: get_sync_log_status(connector_id, active_seq)
  │           if CANCEL_PENDING → SyncLocked (409)
  ├─ 4. try_acquire_sync_lock(connector_id)
  │      ├─ Acquired → init_sync_log_and_update_connector() (sync, returns seq immediately)
  │      │             asyncio.create_task(run_tick(connector_id, sync_seq))
  │      └─ Not acquired (already syncing) → get_active_sync_seq()
  │           None → RuntimeError (should not happen under normal operation)
  └─ 5. Return 202 Accepted { "sync_seq": <seq> }
```

#### `POST /v1/connectors/{connector_id}/syncs/{sync_seq}/stop`
Stops the currently running sync tick without deleting the connector.
- `sync_seq` must match the currently-active sync row; returns `409 Conflict` on a stale or absent seq, or if no sync is running.
- If the seq matches: writes `connector_sync_logs.status = 'cancel pending'` via `mark_sync_cancel_pending()` (connector row stays `'syncing'`); returns `204 No Content` immediately.
- The tick's `_check_interrupt_call(connector_id, sync_seq)` detects `CANCEL_PENDING` on the log row at its next checkpoint, raises `CancelledError`, and `_cancel_tick` closes the sync log with `status = 'cancelled'`.
- `finalize_sync_log_and_update_connector` transitions `connectors.sync_status` to `'out of sync'` so the scheduler can acquire the lock on the next interval.

```text
POST /v1/connectors/{connector_id}/syncs/{sync_seq}/stop
  │
  ├─ 1. get_connector_by_id() → 404 if not found
  ├─ 2. Check connector.sync_status == 'syncing' → 409 if not syncing
  ├─ 3. get_active_sync_seq() → 409 if None or != sync_seq
  ├─ 4. mark_sync_cancel_pending(connector_id)
  │      → writes connector_sync_logs.status='cancel pending' (connector stays 'syncing')
  │      └─ Returns False (no started row) → 409 Conflict
  └─ 5. Return 204 No Content immediately
           └─ Running tick hits _check_interrupt_call checkpoint →
              CANCEL_PENDING detected on sync-log row →
              raises CancelledError → _cancel_tick writes sync log
              status='cancelled', connector sync_status resets to 'out of sync'
```

---

### 3.6 Visibility & Isolation Rules

#### Document APIs (`/v1/documents`)
- Connector-sourced documents are **excluded** from `GET /v1/documents`, `GET /v1/documents/{doc_id}`, and `DELETE /v1/documents/{doc_id}` (returns `404`).
- **Implementation (in place):** `is_connector_sourced_document(doc_id)` checks for presence in `connector_document_checksum`. `get_all_documents_paginated()` accepts `exclude_connector_sourced=True` which filters via `NOT EXISTS (SELECT 1 FROM connector_document_checksum WHERE doc_id = documents.doc_id)`.

#### Job APIs (`/v1/jobs`)
- All jobs (user-submitted and connector-initiated) are visible in `GET /v1/jobs` and `GET /v1/jobs/{job_id}`.
- Connector jobs use the naming convention: `Connector-{connector_name}-{sync_seq}-{batch_number}`.

---

## 4. Database Operations Layer

**Files:** `services/digitize/db/manager.py` & `services/digitize/utils/db.py`

### 4.1 Functions Reference

| Function (`utils/db.py` wrapper → `manager.py` method) | Purpose / Behavior |
| --- | --- |
| `insert_connector()` → `insert_connector()` | Create new connector row. Raises `IntegrityError` on duplicate id/name. |
| `upsert_connector()` → `update_connector()` | Partial update: only non-None kwargs written. Raises `FileNotFoundError` if not found. |
| `get_connector_by_id()` → `get_connector_by_id()` | Fetch single connector by id. Returns `None` if not found. |
| `get_connector_by_name()` → `get_connector_by_name()` | Fetch single connector by name. Returns `None` if not found. |
| `list_connectors()` → `get_all_connectors()` | All connectors ordered by `attached_at` desc. |
| `delete_active_connector()` → `delete_connector()` | Delete connector row (cascades to `connector_sync_logs`). Returns `bool`. |
| `get_connector_sync_status()` → `get_connector_sync_status()` | Minimal `SELECT sync_status` query. Returns `None` if not found. |
| `mark_connector_delete_pending()` → `mark_connector_delete_pending()` | Unconditional `UPDATE connectors SET sync_status='delete pending'`. Returns `True` if row found. |
| `mark_sync_cancel_pending()` → `mark_sync_cancel_pending()` | Sets `connector_sync_logs.status='cancel pending'` on the active `STARTED` log row. Connector row stays `'syncing'`. Returns `bool`. |
| `try_acquire_sync_lock()` → `try_acquire_sync_lock()` | Atomic `UPDATE ... WHERE sync_status != 'syncing' RETURNING id`. Returns `bool`. |
| `get_active_sync_seq()` → `get_active_sync_seq()` | Returns `seq` of active (`started` \| `cancel pending`) log row, or `None`. |
| `lookup_connector_content_by_checksum()` → `find_connector_doc_by_checksum()` | Lookup existing `doc_id` for a checksum across all connectors. |
| `list_connector_checksums()` → `get_connector_checksums()` | All checksums owned by a given connector. |
| `list_all_checksums()` → `get_all_connector_checksums()` | All distinct checksums across all connectors. |
| `add_connector_checksum_entry()` → `insert_connector_checksum()` | Insert `(checksum, connector_id, doc_id)` with `ON CONFLICT DO NOTHING`. |
| `remove_connector_checksum_entry()` → `delete_connector_checksum()` + `count_checksum_owners()` | Delete `(checksum, connector_id)` row; return `(remaining_owner_count, doc_id)`. Two separate DB calls: delete returns `doc_id`; count is a separate query. |
| `set_connector_error()` → `update_connector()` | Persist or clear error string on connector row (best-effort; logs on failure). Pass `None` to clear. |
| `update_connector_total_files()` → `update_connector()` | Update `total_files` count on connector row. Called after `_process_new_files` returns, adjusted by `invalid_file_count`. |
| `init_sync_log_and_update_connector()` → `insert_sync_log()` + `set_connector_sync_status_syncing()` | Insert new log row (seq = `COALESCE(MAX(seq),0)+1`) with `status='started'`; then set connector `sync_status='syncing'`. Returns `seq`. Two separate DB calls. |
| `update_sync_log()` → `update_sync_log_progress()` | Write `total_files`, `new_files`, `removed_files` before I/O phase. Takes `connector_id` + `seq`. Returns `bool`. |
| `finalize_sync_log_and_update_connector()` → `finalize_sync_log()` + `update_connector_after_sync()` | Finalize log row (status, finished_at, optional counts/error); then update connector `last_sync_at`, `sync_status`, and `error`. `CANCELLED`/`FAILED` map to `OUT_OF_SYNC`; `COMPLETED` clears `error` to `NULL`; `FAILED`/`CANCELLED` sets `error = f"Error from last sync: {error}"`. Two separate DB calls. |
| `get_sync_log()` → `get_sync_log()` | Single sync-log row by `(connector_id, seq)`. |
| `get_sync_log_status()` → `get_sync_log_status()` | Minimal `SELECT status` for `(connector_id, seq)`. Used by `_check_interrupt_call`. |
| `list_sync_logs()` → `get_sync_logs()` + `count_sync_logs()` | Paginated log history. Returns `(items, total_count)`. |
| `reset_syncing_connectors()` → `reset_syncing_connectors()` | Bulk `UPDATE connectors SET sync_status='out of sync' WHERE sync_status='syncing'`. Returns list of affected IDs. Used on startup crash recovery. |
| `close_open_sync_log()` → `close_open_sync_log()` | Closes the open sync-log row (`started` \| `cancel pending`) to `failed` with error string. Used on startup crash recovery. |

---

### 4.2 DB Methods Implementation Code

```python
# utils/db.py — thin wrappers that delegate to db_manager

def init_sync_log_and_update_connector(connector_id: str, started_at=None) -> int:
    """Two DB calls: insert log row + set connector sync_status=SYNCING. Returns seq."""
    seq = db_manager.insert_sync_log(connector_id, started_at=started_at)
    db_manager.set_connector_sync_status_syncing(connector_id)
    return seq


def finalize_sync_log_and_update_connector(
    connector_id, seq, status, finished_at=None, total_files=None,
    new_files=None, removed_files=None, error=None,
) -> bool:
    """
    Two DB calls: finalize log row + update connector.
    CANCELLED/FAILED both map to OUT_OF_SYNC on the connector row.
    COMPLETED clears the connector's error to NULL.
    FAILED/CANCELLED stamps connector.error = "Error from last sync: {error}".
    Returns True on success, False if the sync-log row was not found.
    """
    now = finished_at or datetime.now(timezone.utc)
    found = db_manager.finalize_sync_log(connector_id, seq, status, now,
                                         total_files, new_files, removed_files, error)
    if not found:
        return False
    connector_error = f"Error from last sync: {error}" if error else error
    db_manager.update_connector_after_sync(
        connector_id, status=status, last_sync_at=now, error=connector_error
    )
    return True


def remove_connector_checksum_entry(connector_id: str, checksum: str) -> tuple[int, str | None]:
    """
    Two DB calls: delete_connector_checksum (returns doc_id) +
    count_checksum_owners (counts remaining rows for that checksum).
    Returns (remaining_owner_count, doc_id) or (0, None) if not found.
    """
    doc_id = db_manager.delete_connector_checksum(connector_id, checksum)
    if doc_id is None:
        return 0, None
    remaining = db_manager.count_checksum_owners(checksum)
    return remaining, doc_id


# manager.py — key implementations

class DatabaseManager:
    @staticmethod
    def mark_connector_delete_pending(connector_id: str) -> bool:
        """
        Unconditional UPDATE connectors SET sync_status='delete pending'.
        Returns True if the connector row was found and updated, False otherwise.
        """
        with get_db_session() as session:
            result = session.execute(
                update(Connector)
                .where(Connector.id == connector_id)
                .values(sync_status=ConnectorStatus.DELETE_PENDING)
                .returning(Connector.id)
            ).one_or_none()
            return result is not None

    @staticmethod
    def mark_sync_cancel_pending(connector_id: str) -> bool:
        """
        Writes 'cancel pending' to connector_sync_logs.status on the STARTED row.
        Connector row stays 'syncing'. Returns True if a started log row was found.
        """
        with get_db_session() as session:
            result = session.execute(
                update(ConnectorSyncLog)
                .where(
                    ConnectorSyncLog.connector_id == connector_id,
                    ConnectorSyncLog.status == SyncLogStatus.STARTED,
                )
                .values(status=SyncLogStatus.CANCEL_PENDING)
                .returning(ConnectorSyncLog.seq)
            ).one_or_none()
            return result is not None

    @staticmethod
    def try_acquire_sync_lock(connector_id: str) -> bool:
        with get_db_session() as session:
            result = session.execute(
                update(Connector)
                .where(
                    Connector.id == connector_id,
                    Connector.sync_status != ConnectorStatus.SYNCING,
                )
                .values(sync_status=ConnectorStatus.SYNCING)
                .returning(Connector.id)
            ).one_or_none()
            return result is not None

    @staticmethod
    def get_active_sync_seq(connector_id: str) -> Optional[int]:
        """Return seq of active (started | cancel pending) sync-log row, or None."""
        with get_db_session() as session:
            row = session.execute(
                select(ConnectorSyncLog.seq)
                .where(
                    ConnectorSyncLog.connector_id == connector_id,
                    ConnectorSyncLog.status.in_([SyncLogStatus.STARTED, SyncLogStatus.CANCEL_PENDING]),
                )
                .order_by(ConnectorSyncLog.seq.desc())
                .limit(1)
            ).one_or_none()
            return row[0] if row else None

    @staticmethod
    def update_connector_after_sync(connector_id, status, last_sync_at=None, error=None) -> None:
        """CANCELLED/FAILED both map to OUT_OF_SYNC; COMPLETED clears error to NULL."""
        connector_sync_status = (
            ConnectorStatus.OUT_OF_SYNC
            if status in (SyncLogStatus.CANCELLED, SyncLogStatus.FAILED)
            else status
        )
        session.execute(
            update(Connector)
            .where(Connector.id == connector_id)
            .values(last_sync_at=last_sync_at, sync_status=connector_sync_status, error=error)
        )
```

---

## 5. Scanner Abstraction Layer

**Files:** `services/digitize/connectors/scanners/` (`base_scanner.py`, `s3_scanner.py`, `ssh_scanner.py`, `scanner_factory.py`, `config.py`, `hashing.py`)

### 5.1 Base Contract (`base_scanner.py`)

```python
class BaseScanner(ABC):
    def __init__(self, config: object) -> None:
        self._config = config

    @abstractmethod
    def connect(self) -> None: ...

    @abstractmethod
    def scan(self) -> list[tuple[str, str]]:
        """Return (remote_path, checksum) for ALL remote files with within-walk dedup."""
        ...

    @abstractmethod
    def download_to(self, remote_path: str, local_path: Path) -> str:
        """Download remote file to local_path; return hex digest computed inline."""
        ...

    @abstractmethod
    def close(self) -> None: ...

    def verify_integrity(self, local_checksum: str, remote_checksum: str) -> bool:
        return local_checksum == remote_checksum
```

---

### 5.2 Concrete Implementations

#### 1. `S3Scanner` (`s3_scanner.py`)
- Auto-detects provider (AWS S3 vs IBM COS) from `endpoint_url` hostname. `is_aws` is `True` when the URL contains `amazonaws.com` or when no URL is set.
- For AWS: `endpoint_url` is **not** forwarded to boto3 (prevents double-domain URL issue); `region_name` is derived from the endpoint URL. Addressing style = `"auto"`.
- For IBM COS: `endpoint_url` is forwarded; path-style addressing (`"path"`) is used. IBM COS cross-region alias segments (`us`, `eu`, `ap`) are resolved to canonical SigV4 regions (`us-south`, `eu-de`, `jp-tok`).
- `connect()` calls `head_bucket` as a pre-flight connectivity check; raises `ConnectionError` on failure.
- `scan()` uses `list_objects_v2` paginator; uses S3 ETag (quotes stripped) as checksum. Within-walk dedup: first occurrence of each ETag wins.
- `download_to()` streams payload through `HashingWriter` to compute local MD5 inline (no second read).
- Overrides `verify_integrity()` to bypass check for multi-part ETags (`"-" in remote_checksum`).

```python
def verify_integrity(self, local_checksum: str, remote_checksum: str) -> bool:
    if "-" in remote_checksum:  # multi-part ETag cannot be verified locally
        return True
    return super().verify_integrity(local_checksum, remote_checksum)
```

#### 2. `SSHScanner` (`ssh_scanner.py`)
- Paramiko-based directory walk and file transfer.
- `connect()`: loads PEM key (tries RSA → ECDSA → Ed25519 in order); opens SSH + SFTP sessions. Raises `ConnectionError` on auth/network failure.
- `scan()`: recursively walks `remote_path` via `sftp.listdir_attr`; computes remote MD5 via SSH `md5sum "{remote_path}"` command. Within-walk dedup on MD5.
- `download_to()`: uses `sftp.getfo()` streaming into `HashingWriter`.

#### 3. Scanner Factory (`scanner_factory.py`)
```python
_REGISTRY: dict[str, tuple[type[BaseScanner], type]] = {
    "object_storage": (S3Scanner, S3ConnectorConfig),
    "file_system": (SSHScanner, SSHConnectorConfig),
}

def build_scanner(connector_row: Any) -> BaseScanner:
    """Decrypt secrets, build config dataclass, return scanner instance."""
    # 1. Decrypt secrets from stored connection_details
    connection_details = decrypt_secrets(connector_type, connection_details)
    # 2. Build typed config (accepts ORM row or dict)
    config = S3ConnectorConfig.from_connection_details(connection_details, allowed_extensions)
    # or SSHConnectorConfig.from_connection_details(...)
    # 3. Return scanner instance
    return S3Scanner(config)  # or SSHScanner(config)
```

#### 4. Config Models (`config.py`)

`S3ConnectorConfig` — fields: `bucket_name`, `access_key_id`, `secret_access_key`, `endpoint_url`, `prefix`, `delimiter`, `download_concurrency`, `verify_ssl`, `allowed_extensions`. Computed properties: `provider`, `is_aws`, `effective_region`. Validators: require scheme on `endpoint_url`; require non-empty credentials for IBM COS (`_check_credentials` model validator); extensions normalised to lowercase with leading dot (`_normalise_extensions`).

`SSHConnectorConfig` — fields: `host`, `port`, `username`, `private_key`, `remote_path`, `allowed_extensions`. Validators: non-empty host/username/private_key; `"PRIVATE KEY"` substring check on PEM; extensions normalised to lowercase with leading dot (`_normalise_extensions`).

---

## 6. Sync Execution Engine

**File:** `services/digitize/connectors/sync_tick.py` ✅ **Implemented**

### 6.1 Status Enums

**File:** `services/digitize/connectors/models.py`

```python
class ConnectorStatus(str, Enum):
    """Values for connectors.sync_status column."""
    UP_TO_DATE = "up to date"
    SYNCING = "syncing"
    OUT_OF_SYNC = "out of sync"
    DELETE_PENDING = "delete pending"


class SyncLogStatus(str, Enum):
    """Values for connector_sync_logs.status column."""
    STARTED = "started"
    CANCEL_PENDING = "cancel pending"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"


class ConnectorError(str, Enum):
    """Well-known error message sentinel written to the connector row."""
    CREDENTIAL_ERROR_MSG = "Authentication failed: unable to connect with the provided credentials"


class ConnectorType(str, Enum):
    """Allowed connector transport types."""
    FILE_SYSTEM = "file_system"
    OBJECT_STORAGE = "object_storage"
```

---

### 6.2 Execution Flow

![Sync Tick Flow](sync-worker-tick-flow.svg)

- **Phase 0 (Caller — Lock + Log):** `dispatch_sync()` (in `connectors.py`) acquires the lock and opens the sync-log row **before** `run_tick` is called. `init_sync_log_and_update_connector(connector_id)` is called synchronously in the caller; the returned `sync_seq` is passed directly into `run_tick(connector_id, sync_seq)`. This eliminates any need for polling inside the endpoint.
- **Phase 1b (Interrupt Check):** `_check_interrupt_call(connector_id, sync_seq)` immediately before `scanner.connect`. If any interrupt detected, raises `asyncio.CancelledError`.
- **Connection Error Handling:** `scanner.connect()` is wrapped in a `try/except ConnectionError`. On failure, `_fail_tick` is called with `ConnectorError.CREDENTIAL_ERROR_MSG` sentinel and the function returns (no `CancelledError` raised). A `ValidationError` from `build_scanner` (config validation) is also caught in the outer `except Exception` block and calls `_fail_tick` with the same credential sentinel.
- **Phase 2 (Scan & State):** Offload blocking calls (`connect`, `scan`) to thread pool via `asyncio.to_thread(...)`. Query `known_checksums` and `all_checksums` after scan.
- **Phase 3 (Classify):** `_classify()` separates `scanned_files` into `ingest_list` and cross-connector duplicates. Cross-connector duplicates execute `add_connector_checksum_entry` inline.
- **Phase 3b (Count Commit):** `update_sync_log(connector_id, sync_seq, total_files=..., new_files=..., removed_files=...)` called before any I/O-heavy work.
- **Phase 4a (Ingest New Files):** Download and ingest `ingest_list` in batches of `_BATCH_SIZE = 10`:
  - Staging directory: `staging/connectors/{connector_id}-{sync_seq}-{batch_number}` (1-based).
  - Interrupt checkpoint **before** each batch starts.
  - Offload download to executor (`asyncio.to_thread(scanner.download_to, ...)`).
  - Files failing `verify_integrity` are **skipped** (warned), not fatal.
  - Each downloaded file is validated via `validate_document_file(filename, file_bytes)` — files failing validation are removed from the staging dir and counted in `invalid_file_count`; they do not enter the ingest pipeline.
  - Interrupt checkpoint **after** batch downloads complete.
  - Call `initialize_job_state()` with `job_name = f"Connector-{connector_name}-{sync_seq}-{batch_number}"`.
  - Call `ingest(batch_dir, job_id, doc_id_dict)`, then `_wait_for_job(job_id, connector_id, sync_seq)`.
  - After job completes: `get_job_document_stats(job_id)` is called; `add_connector_checksum_entry` is called **only for `completed_docs`** (not before ingest). If `failed_count > 0`, `batch_failed = True`.
  - `_wait_for_job()` polls every `_JOB_POLL_INTERVAL=10s` and calls `_check_interrupt_call` on each wake-up.
  - Cleanup staging directory in `finally`; batch exception sets `batch_failed=True`.
  - Returns `invalid_file_count` to the caller.
- **Phase 4a (total_files adjustment):** After `_process_new_files` returns, if `invalid_file_count > 0`, `total_files` is decremented and both `update_connector_total_files` and `update_sync_log(total_files=...)` are called again to reflect the adjusted count. If no invalid files, only `update_connector_total_files` is called.
- **After all batches:** if `batch_failed`, raises `RuntimeError` (message references the batch job names) → tick closes as `FAILED` → connector set to `OUT_OF_SYNC`.
- **Phase 4b (Orphan Removal):** After Phase 4a completes. `_delete_orphans` offloads `_remove_checksums(connector_id, orphan_checksums)` via `asyncio.to_thread`. For each checksum in `orphan_checksums = known_checksums − scanned_checksums`: call `remove_connector_checksum_entry`; if `remaining == 0` call `_best_effort_delete_document(doc_id)`. `_delete_orphans` raises `RuntimeError` if any checksum removal or document deletion failures occur.
- **Phase 5 (Finalize):** Call `finalize_sync_log_and_update_connector` with terminal status. `scanner.close()` in top-level `finally` regardless of outcome (guarded with `if scanner is not None` for the case where `build_scanner` itself raises).

#### Interrupt Handling

`_check_interrupt_call(connector_id, sync_seq)` checks **two independent DB sources** and returns an `InterruptType` enum value or `None`:

1. **`connectors.sync_status == DELETE_PENDING`** → `InterruptType.DELETE_CONNECTOR`
   - Triggered when `DELETE /v1/connectors/{id}` arrives (via `mark_connector_delete_pending`).
   - `_handle_interrupt` calls `_cancel_tick` then `await _run_teardown(connector_id)` (imported from `connectors.py`). Full teardown: remove scheduler job, remove all checksum ownership rows, delete orphaned docs, delete connector row, sweep staging dirs.

2. **`connector_sync_logs.status == CANCEL_PENDING`** → `InterruptType.SYNC_CANCEL`
   - Triggered when `POST /syncs/{seq}/stop` arrives (`mark_sync_cancel_pending`).
   - `_handle_interrupt` calls `_cancel_tick` (log → `'cancelled'`, connector → `'out of sync'`) + `_sweep_staging_dir(connector_id, ..., sync_seq=sync_seq)` (only this sync's batch dirs).
   - Connector remains and re-syncs on the next interval.

`_check_interrupt_call()` is invoked at **four checkpoints**: (1) after `build_scanner` / before `scanner.connect` (Phase 1b), (2) before each batch starts, (3) after batch downloads complete, (4) inside `_wait_for_job` on every poll cycle.

---

### 6.3 Implementation Code

> **Note on caller contract:** The sync lock and log-row creation are handled by `dispatch_sync()` in `connectors.py` **before** `run_tick` is invoked. `run_tick` receives `sync_seq` as a parameter and never calls `init_sync_log_and_update_connector` internally.

```python
class InterruptType(str, Enum):
    SYNC_CANCEL = "sync_cancel"           # CANCEL_PENDING on connector_sync_logs row
    DELETE_CONNECTOR = "delete_connector" # DELETE_PENDING on connectors row


def _check_interrupt_call(connector_id: str, sync_seq: int) -> Optional[InterruptType]:
    """Check both sources; return the appropriate InterruptType or None."""
    connector_status = get_connector_sync_status(connector_id)
    if connector_status == ConnectorStatus.DELETE_PENDING:
        return InterruptType.DELETE_CONNECTOR
    sync_log_status = get_sync_log_status(connector_id, sync_seq)
    if sync_log_status == SyncLogStatus.CANCEL_PENDING:
        return InterruptType.SYNC_CANCEL
    return None


async def run_tick(connector_id: str, sync_seq: int) -> None:
    """
    Execute one full sync tick.

    The caller (dispatch_sync) is responsible for:
      1. Acquiring the sync lock via try_acquire_sync_lock()
      2. Creating the sync-log row via init_sync_log_and_update_connector()
         and passing the returned seq here as sync_seq.
    """
    config = get_connector_by_id(connector_id)
    if config is None:
        logger.error(f"Connector {connector_id!r} not found; tick aborted")
        _fail_tick(sync_seq, connector_id, RuntimeError(f"Connector {connector_id!r} not found"))
        return

    scanner = None
    try:
        scanner = build_scanner(config)

        interrupt = _check_interrupt_call(connector_id, sync_seq)
        if interrupt:
            raise asyncio.CancelledError(
                f"Connector {connector_id!r} interrupted (type={interrupt.value})"
            )

        try:
            await asyncio.to_thread(scanner.connect)
        except ConnectionError as conn_exc:
            _fail_tick(sync_seq, connector_id, conn_exc,
                       error_msg=ConnectorError.CREDENTIAL_ERROR_MSG.value)
            return

        scanned_files: list[tuple[str, str]] = await asyncio.to_thread(scanner.scan)

        known_checksums: set[str] = set(list_connector_checksums(connector_id))
        all_checksums: set[str] = set(list_all_checksums())

        ingest_list, orphan_checksums = _classify(
            connector_id, scanned_files, known_checksums, all_checksums
        )

        total_files = len(scanned_files)
        update_sync_log(
            connector_id, sync_seq,
            total_files=total_files,
            new_files=len(ingest_list),
            removed_files=len(orphan_checksums),
        )

        invalid_count = await _process_new_files(
            sync_seq, connector_id, config.name, scanner, ingest_list
        )

        if invalid_count > 0:
            total_files -= invalid_count
            update_connector_total_files(connector_id, total_files)
            update_sync_log(connector_id, sync_seq, total_files=total_files)
        else:
            update_connector_total_files(connector_id, total_files)

        await _delete_orphans(connector_id, orphan_checksums)
        _complete_tick(sync_seq, connector_id)

    except asyncio.CancelledError as ce:
        logger.info(f"Tick cancelled for connector {connector_id!r}: {ce}")
        interrupt = _check_interrupt_call(connector_id, sync_seq)
        await _handle_interrupt(sync_seq, connector_id, interrupt)
        raise

    except Exception as exc:
        if isinstance(exc, ValidationError):
            _fail_tick(sync_seq, connector_id, exc,
                       error_msg=ConnectorError.CREDENTIAL_ERROR_MSG.value)
        else:
            _fail_tick(sync_seq, connector_id, exc)

    finally:
        if scanner is not None:
            await asyncio.to_thread(scanner.close)


_BATCH_SIZE = 10
_JOB_POLL_INTERVAL = 10  # seconds

async def _process_new_files(sync_seq, connector_id, connector_name, scanner, ingest_list) -> int:
    """Returns invalid_file_count (files rejected by validate_document_file)."""
    staging_base = settings.digitize.staging_dir / "connectors"
    batch_failed = False
    invalid_file_count = 0

    for batch_number, batch_offset in enumerate(range(0, len(ingest_list), _BATCH_SIZE), start=1):
        batch = ingest_list[batch_offset : batch_offset + _BATCH_SIZE]

        interrupt = _check_interrupt_call(connector_id, sync_seq)
        if interrupt:
            raise asyncio.CancelledError(...)

        job_id = generate_uuid()
        batch_dir_name = f"{connector_id}-{sync_seq}-{batch_number}"
        batch_dir = staging_base / batch_dir_name
        batch_dir.mkdir(parents=True, exist_ok=True)
        filename_to_checksum: dict[str, str] = {}

        try:
            for remote_path, checksum in batch:
                filename = Path(remote_path).name
                local_checksum = await asyncio.to_thread(
                    scanner.download_to, remote_path, batch_dir / filename
                )
                if not scanner.verify_integrity(local_checksum, checksum):
                    continue  # skip — integrity failed
                try:
                    validate_document_file(filename, (batch_dir / filename).read_bytes())
                except ValueError:
                    (batch_dir / filename).unlink(missing_ok=True)
                    invalid_file_count += 1
                    continue  # skip — unsupported file type
                filename_to_checksum[filename] = checksum

            interrupt = _check_interrupt_call(connector_id, sync_seq)
            if interrupt:
                raise asyncio.CancelledError(...)

            if not filename_to_checksum:
                continue

            filenames = list(filename_to_checksum.keys())
            job_name = f"Connector-{connector_name}-{sync_seq}-{batch_number}"
            doc_id_dict = initialize_job_state(
                job_id=job_id, operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=filenames, job_name=job_name,
            )
            doc_id_to_checksum = {
                doc_id: filename_to_checksum[filename]
                for filename, doc_id in doc_id_dict.items()
                if filename in filename_to_checksum
            }

            await asyncio.to_thread(ingest, batch_dir, job_id, doc_id_dict)
            await _wait_for_job(job_id, connector_id, sync_seq)

            # Register checksum entries only for successfully completed documents.
            job_stats = get_job_document_stats(job_id)
            for doc in job_stats["completed_docs"]:
                checksum = doc_id_to_checksum.get(doc["id"])
                if checksum is not None:
                    add_connector_checksum_entry(connector_id, checksum, doc["id"])
            if job_stats["failed_count"] > 0:
                batch_failed = True

        except asyncio.CancelledError:
            raise
        except Exception as exc:
            batch_failed = True
        finally:
            cleanup_staging_directory(batch_dir_name, staging_base, ignore_errors=True)

    if batch_failed:
        raise RuntimeError(
            f"One or more documents failed to sync. See more details in digitize jobs "
            f"Connector-{connector_name}-{sync_seq}-*"
        )

    return invalid_file_count


async def _handle_interrupt(sync_seq, connector_id, interrupt_type) -> None:
    """_handle_interrupt is async — it awaits _run_teardown for DELETE_CONNECTOR."""
    if interrupt_type is None:
        # Unexpected CancelledError — treat as sync cancel
        _cancel_tick(sync_seq, connector_id)
        return

    if interrupt_type == InterruptType.SYNC_CANCEL:
        _cancel_tick(sync_seq, connector_id)
        from digitize.api.v1.connectors import _sweep_staging_dir
        _sweep_staging_dir(connector_id, settings.digitize.staging_dir / "connectors",
                           sync_seq=sync_seq)

    elif interrupt_type == InterruptType.DELETE_CONNECTOR:
        _cancel_tick(sync_seq, connector_id)
        from digitize.api.v1.connectors import _run_teardown
        await _run_teardown(connector_id)  # same function as Case B
```

---

## 7. Scheduler & Task Coordination

**Files:** `services/digitize/connectors/scheduler.py`, `services/digitize/utils/recovery.py`, `services/digitize/app.py` ✅ **Implemented**

### 7.1 Architecture & State Tracking

The scheduler uses an APScheduler v4 `AsyncScheduler` singleton backed by `SQLAlchemyDataStore` (schema `"scheduler"`) in the shared Postgres database.

**No in-process registries.** Delete state is stored exclusively in the `connectors` table (`sync_status = 'delete pending'`). All cancellation and teardown decisions are driven by live DB reads.

The scheduler registers `dispatch_sync` (from `connectors.py`) directly as the recurring job callable. `dispatch_sync` handles lock-acquisition and idempotency — there is no separate `_run_tick_wrapped` wrapper.

---

### 7.2 Scheduler Implementation

```python
# connectors/scheduler.py

_scheduler: AsyncScheduler | None = None  # assigned by _connector_scheduler_lifespan in app.py


async def register_connector_job(
    connector_id: str,
    interval_seconds: int,
    fire_immediately: bool = False,
) -> None:
    """Schedule (or reschedule) a recurring sync job for connector_id.

    fire_immediately=True fires the first tick at now() instead of waiting
    one full interval.  Used when attaching a new connector.
    Uses ConflictPolicy.replace so re-registration on lifespan restart is safe.
    """
    from digitize.api.v1.connectors import dispatch_sync

    now = datetime.now(timezone.utc)
    start_time = now if fire_immediately else now + timedelta(seconds=interval_seconds)

    sched = _get_scheduler()
    await sched.add_schedule(
        func_or_task_id=dispatch_sync,
        trigger=IntervalTrigger(seconds=interval_seconds, start_time=start_time),
        args=[connector_id],
        id=connector_id,
        conflict_policy=ConflictPolicy.replace,
    )


async def remove_connector_job(connector_id: str) -> None:
    """Remove the scheduled job for connector_id, if it exists. Silently ignores missing jobs."""
    sched = _get_scheduler()
    try:
        await sched.remove_schedule(connector_id)
    except Exception as exc:
        logger.warning(f"Could not remove scheduler job for {connector_id!r}: {exc}")
```

---

### 7.3 Crash Recovery for Connector Sync State

`recover_connector_sync_state()` in `services/digitize/utils/recovery.py` ✅ **Implemented**.

Behavior on startup:
1. `reset_syncing_connectors()` — bulk `UPDATE connectors SET sync_status='out of sync' WHERE sync_status='syncing'`. Returns list of affected connector IDs.
2. For each affected connector: `close_open_sync_log(connector_id, error)` — closes the open `connector_sync_logs` row (status = `'started'` or `'cancel pending'`) to `status='failed'` with `error='Service restarted during sync tick'`.

Returns number of connectors recovered.

Note: `recover_zombie_jobs()` for regular (non-connector) jobs and `recover_conversion_tasks()` for Docling conversion tasks are also implemented in `recovery.py` and called from `lifespan()`.

---

### 7.4 Lifespan Integration

`app.py` `lifespan()` starts the scheduler via `_connector_scheduler_lifespan()`:

```python
# app.py — _connector_scheduler_lifespan()

data_store = SQLAlchemyDataStore(db_engine, schema="scheduler")
async with AsyncScheduler(data_store=data_store) as sched:
    scheduler_module._scheduler = sched

    # Connector crash recovery — unlock connectors stuck in 'syncing'.
    recovered = recover_connector_sync_state()

    # Re-register all existing connectors.
    # For connectors in DELETE_PENDING: re-trigger _run_teardown instead.
    connectors = list_connectors()
    for connector in connectors:
        if connector.sync_status == ConnectorStatus.DELETE_PENDING:
            asyncio.create_task(_run_teardown(connector.id))
            continue

        await scheduler_module.register_connector_job(
            connector.id,
            connector.sync_interval_seconds,
            fire_immediately=False,  # don't double-fire connectors already up-to-date
        )

    await sched.start_in_background()
    yield
```

`POST /v1/connectors` calls `register_connector_job(connector_id, sync_interval, fire_immediately=True)` **before** the DB insert (fail-fast order). If scheduler registration fails, a `RuntimeError` is raised and the connector row is never written.

`_run_teardown` removes the scheduler job as Step 1, ensuring no new tick fires after teardown begins. The `remove_connector_job` call is best-effort (failures are appended to `deletion_errors` and logged, but do not abort teardown).

---

## 8. Concurrency & Thread Offloading

### 8.1 Why Offload to Thread Pool?
Scanners perform synchronous blocking I/O (`boto3` calls, Paramiko SFTP operations). Running these on the main event loop thread would freeze Uvicorn request handling and prevent cancellation signals from being processed.

### 8.2 Execution & Cancellation Model
- All blocking scanner methods (`connect`, `scan`, `download_to`) are called via `await asyncio.to_thread(scanner.<method>, *args)`.
- Event loop remains fully responsive.
- Interrupt state is stored exclusively in the DB — no in-memory sets or task registries:
  - **`connectors.sync_status = 'delete pending'`** — set by `mark_connector_delete_pending` when DELETE arrives (unconditionally).
  - **`connector_sync_logs.status = 'cancel pending'`** — set by `mark_sync_cancel_pending` when stop-sync is requested.
- `_check_interrupt_call(connector_id, sync_seq)` reads both sources on every call. Invoked at **four checkpoints**:
  - In `run_tick` — immediately before `scanner.connect` (Phase 1b).
  - In `_process_new_files` — before each batch starts (Checkpoint 2).
  - In `_process_new_files` — after each batch of downloads completes (Checkpoint 3).
  - In `_wait_for_job` — on every poll cycle (Checkpoint 4).
- `CancelledError` is raised in the coroutine at those checkpoints, never inside the thread.
- A download already in flight runs to natural completion. Interrupts are caught at the next checkpoint.
- `run_tick`'s `except asyncio.CancelledError` block calls `await _handle_interrupt` which dispatches:
  - `_cancel_tick` + `_sweep_staging_dir(sync_seq=sync_seq)` for `SYNC_CANCEL`.
  - `_cancel_tick` + `await _run_teardown(connector_id)` for `DELETE_CONNECTOR` (same `_run_teardown` used for Case B).
- No safety-net timeout. No waiting for the tick to exit before teardown begins.


---

## 9. Implementation Plan & PR Breakdown

### Implemented PRs — All Complete ✅

- **PR 1 — DB Schema + ORM Models + Settings ✅:** `connectors`, `connector_document_checksum`, `connector_sync_logs` tables & ORM models (`db/scripts/init_schema.sql`, `db/models.py`). `connector_sync_logs` uses composite PK `(connector_id, seq)`. `Connector` ORM has an `error` field (used for teardown errors, credential errors, and last-sync error messages). `ConnectorStatus`, `SyncLogStatus`, `ConnectorError`, and `ConnectorType` string enums in `connectors/models.py`. Scanner config models (`S3ConnectorConfig`, `SSHConnectorConfig`) in `connectors/scanners/config.py`. Connector type values are `'file_system'` and `'object_storage'` (not `'ssh'`/`'s3'`).

- **PR 2 — DB Operations Layer ✅:** `manager.py` & `utils/db.py` fully implemented. All checksum helpers (`insert_connector_checksum`, `delete_connector_checksum` returning `doc_id`, `count_checksum_owners`, `find_connector_doc_by_checksum`, `get_connector_checksums`, `get_all_connector_checksums`), sync-lock/signal helpers (`try_acquire_sync_lock`, `mark_sync_cancel_pending`, `mark_connector_delete_pending`), sync-log helpers (`init_sync_log_and_update_connector`, `finalize_sync_log_and_update_connector`, `update_sync_log`, `update_connector_total_files`, `set_connector_error`, `list_sync_logs`, `get_sync_log`, `get_sync_log_status`, `get_active_sync_seq`). `get_connector_by_name` added for explicit duplicate-check on `POST`. Crash-recovery helpers `reset_syncing_connectors` and `close_open_sync_log` also implemented. All tested in `test_connector_db.py`.

- **PR 3 — REST API Endpoints ✅:** Full CRUD in `connectors.py` (`POST`, `PUT`, `DELETE`, `GET`, `GET /{id}`, `GET /{id}/syncs`, `GET /{id}/syncs/{seq}`, `POST /{id}/syncs`, `POST /{id}/syncs/{seq}/stop`). `POST` returns `202 Accepted` (not `201`). Shared `dispatch_sync()` helper used by both the HTTP handler and APScheduler; raises `SyncNotFound` / `SyncLocked` (not HTTP exceptions) so callers control error mapping. `PUT` dispatches `asyncio.create_task(dispatch_sync(connector_id))` (not a credential probe) when `connection_details` is updated. Connector visibility filtering in `documents.py` (`is_connector_sourced_document`, `exclude_connector_sourced` in `get_all_documents_paginated`). `_remove_checksums` extracted as a standalone helper shared between `_run_teardown` and `_delete_orphans`.

- **PR 4a — Scanner Abstraction ✅:** `BaseScanner` interface (`base_scanner.py`) & `scanner_factory.py` with `_REGISTRY` for `"object_storage"` and `"file_system"` types. Factory decrypts secrets before building the typed config. Accepts both ORM rows and plain dicts.

- **PR 4b — SSH/SFTP Scanner ✅:** `SSHScanner` implemented in `connectors/scanners/ssh_scanner.py`. Supports RSA, ECDSA, Ed25519 key types.

- **PR 5 — S3 Scanner ✅:** `S3Scanner` implementation with AWS/IBM COS provider auto-detection, cross-region alias resolution, `head_bucket` pre-flight check, `HashingWriter` inline MD5, multi-part ETag integrity bypass.

- **PR 6 — Core Sync Engine ✅:** `sync_tick.py` fully implemented. `run_tick(connector_id, sync_seq)` — `sync_seq` is provided by the caller (`dispatch_sync`), not generated internally. `ConnectionError` from `scanner.connect()` and `ValidationError` from `build_scanner` both call `_fail_tick` with `ConnectorError.CREDENTIAL_ERROR_MSG`. `_classify`, `_process_new_files` (batch download, `validate_document_file` skip, integrity skip, `batch_failed` flag, 1-based `batch_number`, job named `Connector-{connector_name}-{sync_seq}-{batch_number}`, checksum rows inserted post-job-completion from `get_job_document_stats`), `_delete_orphans` (offloads `_remove_checksums` via `asyncio.to_thread`; raises `RuntimeError` on failure), `async _handle_interrupt` (awaits `_run_teardown` for DELETE path), `_cancel_tick`, `_fail_tick`, `_complete_tick`. `_check_interrupt_call(connector_id, sync_seq)` reads from both tables using `ConnectorStatus`/`SyncLogStatus`. Tests in `test_sync_tick.py`.

- **PR 7 — Scheduler + Lifespan + Crash Recovery ✅:**
  - `connectors/scheduler.py` created: `register_connector_job` (uses `ConflictPolicy.replace`), `remove_connector_job`. APScheduler v4 `AsyncScheduler` + `SQLAlchemyDataStore` (schema `"scheduler"`) backed by the shared Postgres engine. `dispatch_sync` registered directly as the scheduler callable (no `_run_tick_wrapped` wrapper).
  - `recover_connector_sync_state()` added to `utils/recovery.py`: bulk-resets connectors stuck in `'syncing'` → `'out of sync'`; closes open `connector_sync_logs` rows to `'failed'`. `recover_conversion_tasks()` also added.
  - `app.py` `lifespan()` updated via `_connector_scheduler_lifespan()` context manager: starts `AsyncScheduler`, calls `recover_connector_sync_state()`, re-registers all existing connector jobs (`fire_immediately=False`); connectors found in `DELETE_PENDING` on startup have `_run_teardown` re-triggered via `asyncio.create_task` instead of being re-registered.
  - `POST /v1/connectors` calls `register_connector_job(connector_id, sync_interval, fire_immediately=True)` **before** the DB insert (fail-fast order).
  - `_run_teardown` calls `remove_connector_job(connector_id)` as Step 1.
  - Unit tests in `tests/test_connector_scheduler.py`.

- **PR 8 — Cancel Sync Endpoint ✅:** `POST /syncs/{seq}/stop` implemented with `sync_status` pre-check + seq validation + `mark_sync_cancel_pending`. `SyncLogStatus.CANCEL_PENDING` and `InterruptType.SYNC_CANCEL` handle the cancel path. `finalize_sync_log_and_update_connector` maps `CANCELLED` → `OUT_OF_SYNC`.

- **PR 9 — Non-Blocking DELETE ✅:** `DELETE /v1/connectors/{id}` uses `mark_connector_delete_pending` (unconditional) + checks `sync_status` to decide Case A vs B. Case B dispatches `asyncio.create_task(_run_teardown(...))`. Case A tick detects `DELETE_PENDING` via `_check_interrupt_call` → `await _run_teardown` in `_handle_interrupt`. Single `_run_teardown` function in `connectors.py` handles both paths. Teardown failures leave the connector row with an `error` message in `DELETE_PENDING` state rather than forcing a partial delete.
