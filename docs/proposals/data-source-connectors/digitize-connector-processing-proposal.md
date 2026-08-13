# Digitize — Data Source Connector Processing Proposal

> **Scope:** Internal `digitize` behavior after catalog sends connector payloads. Catalog-side concerns such as key management, connector CRUD, deployment wiring and TLS provisioning remain out of scope and are treated as infrastructure-level prerequisites.

---

## 1. Preconditions

Before any `digitize` connector endpoint is called:

- Catalog has already validated the remote connector configuration.
- Catalog sends secret material in plaintext via API calls:
  - `ssh`: `private_key`
  - `s3`: `secret_access_key`
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
- `connector_sync_logs`: Per-tick execution log and counters backing history queries.
- `ConnectorScheduler`: APScheduler `AsyncScheduler` singleton backed by `PostgresDataStore`.
- `BaseScanner` / Scanners: Transport-specific remote I/O abstraction (`S3Scanner`, `SFTPScanner`).
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
    sync_status             TEXT        NOT NULL DEFAULT 'up to date'
                                        CHECK (sync_status IN ('up to date', 'syncing', 'out of sync', 'delete pending')),
    last_sync_error         TEXT,
    total_files             INTEGER     NOT NULL DEFAULT 0,
    CONSTRAINT chk_connector_type CHECK (type IN ('ssh', 's3'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_connectors_name ON connectors (name);

-- Connector document checksum registry (Connector-sourced documents ONLY)
CREATE TABLE IF NOT EXISTS connector_document_checksum (
    checksum     TEXT NOT NULL,
    connector_id TEXT NOT NULL,
    doc_id       TEXT NOT NULL,
    PRIMARY KEY (checksum, connector_id)
);

CREATE INDEX IF NOT EXISTS idx_cdc_connector_id ON connector_document_checksum (connector_id);

-- Connector sync logs table
CREATE TABLE IF NOT EXISTS connector_sync_logs (
    id               SERIAL      PRIMARY KEY,
    connector_id     TEXT        NOT NULL,
    seq              INTEGER     NOT NULL,
    started_at       TIMESTAMPTZ NOT NULL,
    finished_at      TIMESTAMPTZ,
    total_files      INTEGER     NOT NULL DEFAULT 0,
    new_files        INTEGER     NOT NULL DEFAULT 0,
    removed_files    INTEGER     NOT NULL DEFAULT 0,
    status           TEXT        NOT NULL DEFAULT 'started',
    error            TEXT        NOT NULL DEFAULT '',
    UNIQUE (connector_id, seq),
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

    id = Column(String, primary_key=True)
    name = Column(String, nullable=False, unique=True)
    type = Column(String, nullable=False)
    connection_details = Column(JSONB, nullable=False, default={})
    allowed_extensions = Column(JSONB, nullable=False, default=[])
    sync_interval_seconds = Column(Integer, nullable=False, default=300)
    attached_at = Column(DateTime(timezone=True), nullable=False, server_default=func.now())
    last_sync_at = Column(DateTime(timezone=True), nullable=True)
    sync_status = Column(String, nullable=False, default="up to date")
    last_sync_error = Column(String, nullable=True)
    total_files = Column(Integer, nullable=False, default=0)

    sync_logs = relationship("ConnectorSyncLog", back_populates="connector", cascade="all, delete-orphan")


class ConnectorDocumentChecksum(Base):
    __tablename__ = "connector_document_checksum"

    checksum = Column(String, primary_key=True, nullable=False)
    connector_id = Column(String, primary_key=True, nullable=False)
    doc_id = Column(String, nullable=False)


class ConnectorSyncLog(Base):
    __tablename__ = "connector_sync_logs"

    id = Column(Integer, primary_key=True, autoincrement=True)
    connector_id = Column(String, ForeignKey("connectors.id", ondelete="CASCADE"), nullable=False)
    seq = Column(Integer, nullable=False)
    started_at = Column(DateTime(timezone=True), nullable=False)
    finished_at = Column(DateTime(timezone=True), nullable=True)
    total_files = Column(Integer, nullable=False, default=0)
    new_files = Column(Integer, nullable=False, default=0)
    removed_files = Column(Integer, nullable=False, default=0)
    status = Column(String, nullable=False, default="started")
    error = Column(String, nullable=False, default="")

    connector = relationship("Connector", back_populates="sync_logs")
    # UNIQUE (connector_id, seq) — enforced by uq_csh_connector_seq constraint
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

Creates a connector, encrypts secrets at rest, persists configuration, and registers an APScheduler job. The first tick fires immediately (`next_run_time = NOW()`).

#### Request Parameters
- Common: `connector_id` (UUID), `connector_name` (string, unique), `type` (`"ssh"` | `"s3"`), `allowed_extensions` (`array[string]`), `connection_details` (object).
- `sync_interval_seconds`: Read from `CONNECTOR_SYNC_INTERVAL_SECONDS` env var (default `300`).
- SSH `connection_details`: `host`, `username`, `remote_path`, `private_key`.
- S3 `connection_details`: `endpoint_url` (full URL), `bucket_name`, `access_key_id`, `secret_access_key`, `prefix` (optional), `delimiter` (optional).

#### Example Payloads
```json
{
  "connector_id": "c7f3a2d1-4e5b-4c6d-8f9a-0b1c2d3e4f5a",
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
  "connector_id": "a1b2c3d4-5e6f-7a8b-9c0d-1e2f3a4b5c6d",
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

#### Responses
- `201 Created` / `202 Accepted`: Connector created and periodic tick scheduled.
- `409 Conflict`: `connector_id` or `connector_name` already exists.

---

### 3.2 `PUT /v1/connectors/{connector_id}`

Updates connector configuration in place.
- Secrets are re-encrypted if provided.
- `connection_details` is merged key-by-key (omitted fields remain unchanged).
- `type` cannot be modified.
- Does not restart the scheduler — the next scheduled tick reads updated config directly from DB.

#### Responses
- `200 OK`: Updated successfully.
- `404 Not Found`: Connector does not exist.
- `409 Conflict`: Duplicate `connector_name`.

---

### 3.3 `DELETE /v1/connectors/{connector_id}`

Fast, non-blocking detachment. Returns `204 No Content` immediately; teardown runs asynchronously in a background task. No `409 Conflict` guard for running ticks.

#### Handler Flow

```text
DELETE /v1/connectors/{connector_id} (API Handler)
  │
  ├─ 1. SELECT connector → 404 if not found
  ├─ 2. mark_sync_delete_pending(connector_id)
  │      (atomic UPDATE connectors SET sync_status='delete pending'
  │       WHERE sync_status='syncing')
  │
  ├─ [Case A] sync_status was 'syncing'  (mark_sync_delete_pending returned True)
  │     └─ Return 204 No Content immediately
  │          └─ Running tick hits _check_interrupt_call at its next checkpoint,
  │             detects DELETE_PENDING → raises CancelledError →
  │             _handle_interrupt calls _cancel_tick then _run_delete_teardown
  │
  └─ [Case B] sync_status != 'syncing'  (mark_sync_delete_pending returned False)
        ├─ asyncio.create_task(_run_teardown(connector_id))
        └─ Return 204 No Content immediately
```

#### Teardown Flow

```text
_run_teardown(connector_id) (Background asyncio.Task — Case B only)
  │
  ├─ Step C+D: list_connector_checksums → for each checksum:
  │              remove_connector_checksum_entry(connector_id, checksum)
  │              if remaining == 0 and doc_id → _best_effort_delete_document(doc_id)
  ├─ Step E: delete_active_connector(connector_id) (cascades to sync_logs)
  └─ Step F: _sweep_staging_dir(connector_id, staging/connectors)

_run_delete_teardown(connector_id) (Inline in sync_tick.py — Case A only)
  │  Called after _cancel_tick inside CancelledError handler when DELETE_PENDING
  ├─ Step 1: list_connector_checksums → remove ownership rows + delete orphaned docs
  ├─ Step 2: delete_active_connector(connector_id) (cascades to sync_logs)
  └─ Step 3: _sweep_staging_dir(connector_id, staging/connectors)
```

![Delete Flow](delete-flow.svg)

---

### 3.4 `GET /v1/connectors` & `GET /v1/connectors/{connector_id}`

- `GET /v1/connectors`: Returns active connectors list without secrets.
- `GET /v1/connectors/{id}`: Returns connector details + latest file processing counters (`total_files`, `new_files`, `removed_files`).

---

### 3.5 `GET`, `POST` & `POST /syncs/{sync_seq}/stop` — `/v1/connectors/{connector_id}/syncs`

- `GET /v1/connectors/{connector_id}/syncs`: Returns paginated execution history items from `connector_sync_logs` (`limit` default 50, max 200).
- `POST /v1/connectors/{connector_id}/syncs`: Triggers an immediate manual sync tick.
  - Safe & idempotent: acquires DB lock via atomic `UPDATE connectors SET sync_status='syncing' WHERE id=:id AND sync_status!='syncing' RETURNING id`.
  - If lock acquired, dispatches `asyncio.create_task(run_tick(connector_id))` (open log happens *inside* `run_tick`); polls `get_active_sync_seq()` in a loop until the row appears, then returns `202 Accepted` with the `sync_seq`.
  - If locked (already syncing), fetches the current active `sync_seq` via `get_active_sync_seq()` and returns `202 Accepted` with it (no duplicate tick started).

```text
POST /v1/connectors/{connector_id}/syncs
  │
  ├─ 1. Check existence → 404 if not found
  ├─ 2. try_acquire_sync_lock(connector_id) (atomic UPDATE ... RETURNING)
  │      ├─ Lock acquired → asyncio.create_task(run_tick(connector_id))
  │      │    └─ _wait_for_sync_seq() polls until open_new_sync_log row appears
  │      └─ Lock unavailable (already syncing) → get_active_sync_seq()
  └─ 3. Return 202 Accepted { "sync_seq": <seq> }
```
- `POST /v1/connectors/{connector_id}/syncs/{sync_seq}/stop`: Stops the currently running sync tick without deleting the connector. The connector remains and resumes its normal schedule on the next interval.
  - `sync_seq` must match the currently-active sync row; returns `409 Conflict` on a stale or absent seq.
  - If the seq matches: writes `connector_sync_logs.status = 'cancel pending'` via `mark_sync_cancel_pending()` (connector row stays `'syncing'`); returns `204 No Content` immediately.
  - The tick's `_check_interrupt_call(connector_id, sync_seq)` detects `CANCEL_PENDING` on the log row at its next checkpoint, raises `CancelledError`, and `_cancel_tick` closes the sync log with `status = 'cancelled'`.
  - `close_sync_log` transitions `connectors.sync_status` to `'out of sync'` so the scheduler can acquire the lock on the next interval.

```text
POST /v1/connectors/{connector_id}/syncs/{sync_seq}/stop
  │
  ├─ 1. Check existence → 404 if not found
  ├─ 2. get_active_sync_seq() → 409 if None or != sync_seq
  ├─ 3. mark_sync_cancel_pending(connector_id)
  │      → writes connector_sync_logs.status='cancel pending' (connector stays 'syncing')
  │      ├─ Returns False (not syncing) → 409 Conflict
  │      └─ Returns True → proceed
  └─ 4. Return 204 No Content immediately
           └─ Running tick hits _check_interrupt_call checkpoint →
              CANCEL_PENDING detected on sync-log row →
              raises CancelledError → _cancel_tick writes sync log
              status='cancelled', connector sync_status resets to 'out of sync'
```

---

### 3.6 Visibility & Isolation Rules

#### Document APIs (`/v1/documents`)
- Connector-sourced documents are **excluded** from `GET /v1/documents`, `GET /v1/documents/{doc_id}`, and `DELETE /v1/documents/{doc_id}` (returns `404`).
- **Implementation:** SQL queries for user document endpoints include:
  ```sql
  WHERE NOT EXISTS (
      SELECT 1 FROM connector_document_checksum 
      WHERE doc_id = documents.doc_id
  )
  ```

#### Job APIs (`/v1/jobs`)
- All jobs (user-submitted and connector-initiated) are visible in `GET /v1/jobs` and `GET /v1/jobs/{job_id}`.
- Connector jobs store `connector_id` in `jobs.metadata` and use job naming convention: `{connector_id} - {sync_seq} - {batch_number}`.

---

## 4. Database Operations Layer

**Files:** `services/digitize/db/manager.py` & `services/digitize/utils/db.py`

### 4.1 Functions Reference

| Function | Purpose / Behavior |
| --- | --- |
| `insert_connector()` | Create new connector row. |
| `update_connector()` / `upsert_connector()` | Merge configuration fields & re-encrypt secrets. |
| `get_connector_by_id()` / `get_all_connectors()` | Fetch single or all connectors. |
| `delete_connector()` / `delete_active_connector()` | Delete connector row (cascades to sync_logs). Returns `bool`. |
| `mark_sync_delete_pending(connector_id)` | Atomic `UPDATE connectors SET sync_status='delete pending' WHERE sync_status='syncing'`. Returns `True` if a running tick was signalled. |
| `mark_sync_cancel_pending(connector_id)` | Verifies `connectors.sync_status='syncing'`, then sets `connector_sync_logs.status='cancel pending'` on the active log row. Connector row stays `'syncing'`. Returns `bool`. |
| `find_connector_doc_by_checksum(checksum)` | Lookup existing `doc_id` in `connector_document_checksum`. |
| `get_connector_checksums(connector_id)` / `list_connector_checksums()` | Return list of checksums owned by `connector_id`. |
| `get_all_connector_checksums()` / `list_all_checksums()` | Return list of all distinct checksums in `connector_document_checksum`. |
| `insert_connector_checksum(connector_id, checksum, doc_id)` | Insert `(checksum, connector_id, doc_id)` with `ON CONFLICT DO NOTHING`. |
| `delete_connector_checksum(connector_id, checksum)` | Remove `(checksum, connector_id)`; returns `(remaining_owner_count, doc_id)`. |
| `get_connector_sync_status(connector_id)` | `SELECT sync_status FROM connectors WHERE id = :id`. Returns the current status string or `None`. Used by `_check_interrupt_call`. |
| `get_active_sync_seq(connector_id)` | Returns `seq` of the active sync-log row (status in `{started, cancel pending}`), or `None` if no tick is running. Used by `POST /syncs` and `POST /syncs/{seq}/stop`. |
| `try_acquire_sync_lock(connector_id)` | Atomic `UPDATE connectors SET sync_status='syncing' WHERE id=:id AND sync_status!='syncing' RETURNING id`. Returns `bool`. |
| `open_sync_log(connector_id)` | Create tick log row with `seq = COALESCE(MAX(seq), 0) + 1` and status `'started'`. Also sets connector `sync_status='syncing'`. Returns `seq`. |
| `update_sync_log_progress(log_id, total_files, new_files, removed_files)` | Write all three file counts in a single DB update immediately after `_classify()` returns. All counts are derived from classify output and known before any I/O begins. (Exposed via `update_sync_log` in `utils/db.py` which takes `seq`.) |
| `close_sync_log(connector_id, seq, status, error)` | Finalize log row with terminal status and `finished_at`. Updates connector `last_sync_at` and `sync_status` (`CANCELLED`/`FAILED` both map to `OUT_OF_SYNC`). Does **not** carry file counts — those are already persisted by `update_sync_log`. |
| `get_sync_log_status(connector_id, seq)` | Returns `status` of a specific `(connector_id, seq)` row or `None`. Used by `_check_interrupt_call` to detect `CANCEL_PENDING`. |
| `get_sync_logs(connector_id, limit, offset)` | Paginated log history query. Returns `(items, total_count)`. |
| `reset_stale_syncing_connectors()` | On startup: `UPDATE connectors SET sync_status='out of sync' WHERE sync_status='syncing'`. Returns the list of affected `connector_id` values. Used by `recover_connector_sync_state()`. |

---

### 4.2 DB Methods Implementation Code

```python
class DatabaseManager:
    @staticmethod
    def find_connector_doc_by_checksum(checksum: str) -> str | None:
        with get_db_session() as session:
            row = session.execute(
                select(ConnectorDocumentChecksum.doc_id)
                .where(ConnectorDocumentChecksum.checksum == checksum)
                .limit(1)
            ).one_or_none()
        return row[0] if row else None

    @staticmethod
    def insert_connector_checksum(connector_id: str, checksum: str, doc_id: str) -> None:
        with get_db_session() as session:
            stmt = (
                pg_insert(ConnectorDocumentChecksum)
                .values(checksum=checksum, connector_id=connector_id, doc_id=doc_id)
                .on_conflict_do_nothing(index_elements=["checksum", "connector_id"])
            )
            session.execute(stmt)

    @staticmethod
    def delete_connector_checksum(connector_id: str, checksum: str) -> tuple[int, str | None]:
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

    @staticmethod
    def try_acquire_sync_lock(connector_id: str) -> bool:
        with get_db_session() as session:
            result = session.execute(
                update(Connector)
                .where(
                    Connector.id == connector_id,
                    Connector.sync_status != SyncStatus.SYNCING,
                )
                .values(sync_status=SyncStatus.SYNCING)
                .returning(Connector.id)
            ).one_or_none()
            return result is not None

    @staticmethod
    def mark_sync_cancel_pending(connector_id: str) -> bool:
        """
        Writes 'cancel pending' to connector_sync_logs.status — NOT connectors.
        Connector row stays 'syncing' until the tick's close_sync_log() call.
        Returns True only when connectors.sync_status='syncing' (a tick is running).
        """
        with get_db_session() as session:
            connector_row = session.execute(
                select(Connector.id)
                .where(Connector.id == connector_id, Connector.sync_status == SyncStatus.SYNCING)
            ).one_or_none()
            if connector_row is None:
                return False
            result = session.execute(
                update(ConnectorSyncLog)
                .where(
                    ConnectorSyncLog.connector_id == connector_id,
                    ConnectorSyncLog.status == SyncStatus.STARTED,
                )
                .values(status=SyncStatus.CANCEL_PENDING)
                .returning(ConnectorSyncLog.seq)
            ).one_or_none()
            return result is not None

    @staticmethod
    def mark_sync_delete_pending(connector_id: str) -> bool:
        """
        Sets connectors.sync_status='delete pending' only when currently 'syncing'.
        Returns True if a running tick was signalled, False otherwise.
        """
        with get_db_session() as session:
            result = session.execute(
                update(Connector)
                .where(Connector.id == connector_id, Connector.sync_status == SyncStatus.SYNCING)
                .values(sync_status=SyncStatus.DELETE_PENDING)
                .returning(Connector.id)
            ).one_or_none()
            return result is not None

    @staticmethod
    def get_active_sync_seq(connector_id: str) -> int | None:
        """Return seq of the active (started | cancel pending) sync-log row, or None."""
        with get_db_session() as session:
            row = session.execute(
                select(ConnectorSyncLog.seq)
                .where(
                    ConnectorSyncLog.connector_id == connector_id,
                    ConnectorSyncLog.status.in_([SyncStatus.STARTED, SyncStatus.CANCEL_PENDING]),
                )
                .order_by(ConnectorSyncLog.seq.desc())
                .limit(1)
            ).one_or_none()
            return row[0] if row else None

    @staticmethod
    def close_sync_log(connector_id, seq, status, error=None, finished_at=None, ...) -> bool:
        """
        CANCELLED and FAILED both transition connector to OUT_OF_SYNC.
        Only COMPLETED writes the status value through verbatim.
        """
        ...
        connector_sync_status = (
            SyncStatus.OUT_OF_SYNC
            if status in (SyncStatus.CANCELLED, SyncStatus.FAILED)
            else status  # COMPLETED → "completed"
        )
        session.execute(
            update(Connector)
            .where(Connector.id == connector_id)
            .values(last_sync_at=now, sync_status=connector_sync_status)
        )

    @staticmethod
    def reset_stale_syncing_connectors() -> list[str]:
        """
        Called once on startup. Any connector still marked 'syncing' was
        interrupted by a crash — the tick will never self-recover because
        try_acquire_sync_lock guards with sync_status != 'syncing'.

        Resets them to 'out of sync' so the scheduler can acquire the lock
        on the next tick. Returns the list of affected connector_id values
        so the caller can close their open sync logs.
        """
        with get_db_session() as session:
            result = session.execute(
                update(Connector)
                .where(Connector.sync_status == SyncStatus.SYNCING)
                .values(sync_status=SyncStatus.OUT_OF_SYNC)
                .returning(Connector.id)
            )
            return [row[0] for row in result]
```

---

## 5. Scanner Abstraction Layer

**Files:** `services/digitize/connectors/scanners/` (`base_scanner.py`, `s3_scanner.py`, `sftp_scanner.py`, `scanner_factory.py`)

### 5.1 Base Contract (`base_scanner.py`)

```python
class BaseScanner(ABC):
    def __init__(self, config: object) -> None:
        self._config = config

    @abstractmethod
    def connect(self) -> None: ...

    @abstractmethod
    def scan(self) -> list[tuple[str, str]]:
        """Return (remote_path, checksum) for ALL remote files. No dedup filtering here."""
        ...

    @abstractmethod
    def download_to(self, remote_path: str, local_path: Path) -> str:
        """Download remote file to local_path and return computed local hex digest."""
        ...

    @abstractmethod
    def close(self) -> None: ...

    def verify_integrity(self, local_checksum: str, remote_checksum: str) -> bool:
        return local_checksum == remote_checksum
```

---

### 5.2 Concrete Implementations

#### 1. `S3Scanner` (`s3_scanner.py`)
- Auto-detects provider (AWS S3 vs IBM COS) from `endpoint_url` hostname and configures boto3 `addressing_style` (`"virtual"` for AWS, `"path"` for IBM COS).
- `scan()` uses `list_objects_v2` paginator; uses S3 ETag as checksum.
- `download_to()` streams payload through `_HashingWriter` to compute local MD5 in a single pass.
- Overrides `verify_integrity()` to bypass check for multi-part ETags (`"-" in remote_checksum`).

```python
def verify_integrity(self, local_checksum: str, remote_checksum: str) -> bool:
    if "-" in remote_checksum:  # multi-part ETag cannot be verified locally
        return True
    return super().verify_integrity(local_checksum, remote_checksum)
```

#### 2. `SFTPScanner` (`sftp_scanner.py`)
- Paramiko-based directory walk and file transfer.
- Computes remote MD5 via SSH `md5sum "{remote_path}"` command (hashing executes on remote host; no file bytes transferred during scan).

#### 3. Scanner Factory (`scanner_factory.py`)
```python
def build_scanner(config: ConnectorConfig) -> BaseScanner:
    if config.type == "s3":
        return S3Scanner(config)
    elif config.type == "ssh":
        return SFTPScanner(config)
    raise ValueError(f"Unsupported connector type: {config.type}")
```

---

## 6. Sync Execution Engine

**File:** `services/digitize/connectors/sync_tick.py`

### 6.1 Execution Flow

![Sync Tick Flow](sync-worker-tick-flow.svg)

- **Phase 0 (Lock):** Acquire lock via `try_acquire_sync_lock(connector_id)`. Called externally (by scheduler or `POST /syncs`); `run_tick` assumes the lock is already held.
- **Phase 1 (Log):** `open_new_sync_log(connector_id)` is called **inside** `run_tick`, not the caller. Returns `sync_seq` used for all subsequent interrupt checks.
- **Phase 1b (Interrupt Check):** `_check_interrupt_call(connector_id, sync_seq)` is called immediately before any remote I/O. If `DELETE_PENDING` or `CANCEL_PENDING`, raises `asyncio.CancelledError`.
- **Phase 2 (Scan & State):** Query owned `known_checksums` and `all_checksums`. Offload blocking network calls (`connect`, `scan`) to thread pool via `asyncio.to_thread(...)`.
- **Phase 3 (Classify):** `_classify()` separates `scanned_files` into `ingest_list` and cross-connector duplicates. Cross-connector duplicates execute `add_connector_checksum_entry` inline using existing `doc_id`.
- **Phase 3b (Count Commit):** All three counters (`total_files`, `new_files`, `removed_files`) are fully determined from classify output. A single `update_sync_log()` call persists them before any I/O-heavy work begins. No further counter writes happen during batch processing.
- **Phase 4a (Ingest New Files):** Download and ingest `ingest_list` in batches of `_BATCH_SIZE = 10`:
  - Dedicated per-batch staging directory: `staging/connectors/{connector_id}-{sync_seq}-{batch_number}`.
  - Interrupt checkpoint **before** each batch starts.
  - Offload download to executor thread (`asyncio.to_thread(scanner.download_to, ...)`).
  - Files failing `verify_integrity` are **skipped** (logged as warnings), not fatal.
  - Interrupt checkpoint **after** batch downloads complete.
  - Call `initialize_job_state()` → obtain `doc_id` map, insert checksum rows, call `ingest()`, then `_wait_for_job()`.
  - `_wait_for_job()` polls every `_JOB_POLL_INTERVAL=10s` and calls `_check_interrupt_call` on each wake-up.
  - Cleanup staging directory in `finally` per batch; batch failure sets `batch_failed=True` flag.
  - After all batches: if `batch_failed`, raises `RuntimeError` → tick closes as `FAILED` → connector set to `OUT_OF_SYNC`.
- **Phase 4b (Orphan Removal):** Runs **strictly after Phase 4a completes**. Delete checksum rows for `orphan_checksums = known_checksums − scanned_checksums`. If remaining count is `0`, call `_best_effort_delete_document(doc_id)`.
- **Phase 5 (Finalize):** Write terminal status (`'completed'`/`'failed'`/`'cancelled'`) and update `sync_status` on the connector row. `scanner.close()` is called in top-level `finally` regardless of outcome.

#### Interrupt Handling

The tick uses `_check_interrupt_call(connector_id, sync_seq)` which checks **two independent DB sources** and returns an `InterruptType` enum value or `None`:

1. **`connectors.sync_status == DELETE_PENDING`** → `InterruptType.DELETE_CONNECTOR`
   - Triggered when `DELETE /v1/connectors/{id}` arrives while a sync is running (`mark_sync_delete_pending` succeeds).
   - When the tick detects this, `CancelledError` is raised; `_handle_interrupt` calls `_cancel_tick` then `_run_delete_teardown`:
     - Remove all checksum ownership rows; delete documents when they lose their last owner
     - `delete_active_connector` (cascades to all sync_logs)
     - Sweep all residual batch staging directories

2. **`connector_sync_logs.status == CANCEL_PENDING`** → `InterruptType.SYNC_CANCEL`
   - Triggered when `POST /syncs/{seq}/stop` arrives (`mark_sync_cancel_pending` writes to the log row; connector stays `'syncing'`).
   - When detected, `_handle_interrupt` calls `_cancel_tick` (log → `'cancelled'`, connector → `'out of sync'`), then sweeps staging directories for this sync's batches only.
   - The connector remains and re-syncs on the next scheduler interval.

`_check_interrupt_call()` is invoked at **four checkpoints**: (1) after `open_new_sync_log`, (2) before each batch starts, (3) after batch downloads complete, and (4) inside `_wait_for_job` on every poll cycle.

---

### 6.2 Implementation Code

```python
class InterruptType(str, Enum):
    SYNC_CANCEL = "sync_cancel"           # CANCEL_PENDING on connector_sync_logs row
    DELETE_CONNECTOR = "delete_connector" # DELETE_PENDING on connectors row

def _check_interrupt_call(connector_id: str, sync_seq: int) -> Optional[InterruptType]:
    """Check both sources; return the appropriate InterruptType or None."""
    # Priority 1: check connectors table for DELETE_PENDING
    connector_status = get_connector_sync_status(connector_id)
    if connector_status == SyncStatus.DELETE_PENDING:
        return InterruptType.DELETE_CONNECTOR
    # Priority 2: check sync-log row for CANCEL_PENDING
    sync_log_status = get_sync_log_status(connector_id, sync_seq)
    if sync_log_status == SyncStatus.CANCEL_PENDING:
        return InterruptType.SYNC_CANCEL
    return None


async def run_tick(connector_id: str) -> None:
    """
    Execute one full sync tick. Caller must have acquired the sync lock
    (try_acquire_sync_lock) before calling this coroutine.
    """
    config = get_active_connector(connector_id)
    if config is None:
        logger.error(f"Connector {connector_id!r} not found; tick aborted")
        return

    sync_seq: int = open_new_sync_log(connector_id)   # Phase 1
    scanner = build_scanner(config)

    try:
        # Phase 1b: interrupt checkpoint before any remote I/O
        interrupt = _check_interrupt_call(connector_id, sync_seq)
        if interrupt == InterruptType.DELETE_CONNECTOR:
            raise asyncio.CancelledError(f"Connector {connector_id!r} marked for deletion")
        elif interrupt == InterruptType.SYNC_CANCEL:
            raise asyncio.CancelledError(f"Sync cancel requested for connector {connector_id!r}")

        await asyncio.to_thread(scanner.connect)
        scanned_files = await asyncio.to_thread(scanner.scan)

        known_checksums = set(list_connector_checksums(connector_id))
        all_checksums = set(list_all_checksums())

        ingest_list, orphan_checksums = _classify(           # Phase 3
            connector_id, scanned_files, known_checksums, all_checksums
        )

        update_sync_log(                                     # Phase 3b
            sync_seq,
            total_files=len(scanned_files),
            new_files=len(ingest_list),
            removed_files=len(orphan_checksums),
        )

        await _process_new_files(sync_seq, connector_id, scanner, ingest_list)  # Phase 4a
        await _delete_orphans(connector_id, orphan_checksums)                    # Phase 4b
        _complete_tick(sync_seq, connector_id)                                   # Phase 5

    except asyncio.CancelledError as ce:
        logger.info(f"Tick cancelled for connector {connector_id!r}: {ce}")
        interrupt = _check_interrupt_call(connector_id, sync_seq)
        _handle_interrupt(sync_seq, connector_id, interrupt)
        raise

    except Exception as exc:
        logger.error(f"Tick failed for connector {connector_id!r}: {exc}", exc_info=True)
        _fail_tick(sync_seq, connector_id, exc)   # → close_sync_log(FAILED) → OUT_OF_SYNC

    finally:
        await asyncio.to_thread(scanner.close)


def _classify(...) -> tuple[list[tuple[str, str]], set[str]]:
    # Same logic as before — no changes to classification algorithm.
    ...
    orphan_checksums = known_checksums - scanned_checksums
    return ingest_list, orphan_checksums


_BATCH_SIZE = 10
_JOB_POLL_INTERVAL = 10  # seconds

async def _process_new_files(sync_seq, connector_id, scanner, ingest_list) -> None:
    """
    Download and ingest ingest_list in batches of up to BATCH_SIZE=10.

    Integrity failures skip the file (logged warning).
    Batch-level exceptions set batch_failed=True but processing continues.
    After all batches: raises RuntimeError if batch_failed → _fail_tick → OUT_OF_SYNC.
    """
    staging_base = settings.digitize.staging_dir / "connectors"
    batch_failed = False

    for batch_number in range(0, len(ingest_list), _BATCH_SIZE):
        batch = ingest_list[batch_number : batch_number + _BATCH_SIZE]

        # Checkpoint 2: interrupt check before each batch
        interrupt = _check_interrupt_call(connector_id, sync_seq)
        if interrupt:
            raise asyncio.CancelledError(...)

        batch_dir_name = f"{connector_id}-{sync_seq}-{batch_number}"
        ...
        try:
            for remote_path, checksum in batch:
                local_checksum = await asyncio.to_thread(scanner.download_to, ...)
                if not scanner.verify_integrity(local_checksum, checksum):
                    logger.warning(f"Integrity check failed for {remote_path!r}; skipping")
                    continue                 # ← skip, not fatal
                checksum_to_filename[checksum] = filename

            # Checkpoint 3: interrupt check after downloads
            interrupt = _check_interrupt_call(connector_id, sync_seq)
            if interrupt:
                raise asyncio.CancelledError(...)

            doc_id_dict = initialize_job_state(...)
            for checksum, filename in checksum_to_filename.items():
                add_connector_checksum_entry(connector_id, checksum, doc_id_dict[filename])
            await asyncio.to_thread(ingest, batch_dir, job_id, doc_id_dict)
            await _wait_for_job(job_id, connector_id, sync_seq)  # Checkpoint 4: polls + interrupt

        except asyncio.CancelledError:
            raise  # propagate upward to run_tick's CancelledError handler
        except Exception as exc:
            logger.warning(f"Batch {batch_number!r} failed: {exc}")
            batch_failed = True
        finally:
            cleanup_staging_directory(batch_dir_name, staging_base, ignore_errors=True)

    if batch_failed:
        raise RuntimeError("One or more batches failed; connector marked as out of sync")


def _handle_interrupt(sync_seq, connector_id, interrupt_type) -> None:
    if interrupt_type == InterruptType.SYNC_CANCEL:
        _cancel_tick(sync_seq, connector_id)   # log → 'cancelled', connector → 'out of sync'
        _sweep_staging_dir(connector_id, ..., sync_seq=sync_seq)  # sweep only this sync's dirs
    elif interrupt_type == InterruptType.DELETE_CONNECTOR:
        _cancel_tick(sync_seq, connector_id)
        _run_delete_teardown(connector_id)     # full teardown inline in sync_tick.py
    else:  # None (unexpected CancelledError)
        _cancel_tick(sync_seq, connector_id)
```

---

## 7. Scheduler & Task Coordination

**File:** `services/digitize/connectors/scheduler.py`

### 7.1 Architecture & State Tracking

`ConnectorScheduler` uses an APScheduler `AsyncScheduler` singleton backed by `AsyncSQLAlchemyDataStore` in Postgres.

**No in-process registries.** Delete state is stored exclusively in the `connectors` table (`sync_status = 'delete_pending'`). There is no `_pending_deletions` set and no `_live_tasks` dict. All cancellation and teardown decisions are driven by live DB reads.

---

### 7.2 Scheduler Implementation

```python
# No module-level _pending_deletions or _live_tasks.
# All interrupt state is read from DB: connectors.sync_status and
# connector_sync_logs.status polled by _check_interrupt_call.

async def register_connector_job(connector_id: str, interval_seconds: int, fire_immediately: bool = False) -> None:
    kwargs = dict(
        func=_run_tick_wrapped,
        trigger=IntervalTrigger(seconds=interval_seconds),
        args=[connector_id],
        id=connector_id,
        max_instances=1,
        replace_existing=True,
    )
    if fire_immediately:
        kwargs["next_run_time"] = datetime.now(timezone.utc)
    await _get_scheduler().add_job(**kwargs)


async def _run_tick_wrapped(connector_id: str) -> None:
    """APScheduler entry point for each scheduled tick.

    Guards:
    1. Check for DELETE_PENDING — skip tick entirely if connector is being deleted.
    2. try_acquire_sync_lock — skip tick if one is already running.
    """
    status = get_connector_sync_status(connector_id)
    if status == SyncStatus.DELETE_PENDING:
        return  # connector is being torn down; skip tick
    if not try_acquire_sync_lock(connector_id):
        return  # another tick is already running
    await run_tick(connector_id)  # run_tick opens the log row internally
```

---

### 7.3 Crash Recovery (`utils/recovery.py`)

**File:** `services/digitize/utils/recovery.py`

`recover_connector_sync_state()` is called once at startup, before the scheduler
begins firing ticks. It addresses two invariants broken by a mid-tick crash:

1. **Stuck `sync_status`** — a connector left as `'syncing'` will never tick again
   because `try_acquire_sync_lock` guards with `sync_status != 'syncing'`. Reset to
   `'out of sync'` so the next scheduled tick can acquire the lock.
2. **Open sync log** — a log row left as `'started'` has no terminal state. Close it
   to `'failed'` with a crash error message, mirroring how `recover_zombie_jobs()`
   handles regular jobs. The connector `sync_status` is left as `'out of sync'` —
   already set correctly by `reset_stale_syncing_connectors()`.

```python
def recover_connector_sync_state() -> int:
    """
    Startup crash recovery for connector sync state.

    For every connector that was left in 'syncing' by a previous crash:
      1. Reset connectors.sync_status → 'out of sync'  (single bulk UPDATE)
      2. Close the open sync log row → status='failed', error='...'

    Returns the number of connectors recovered.
    """
    stale_ids = reset_stale_syncing_connectors()   # bulk UPDATE, returns affected ids
    if not stale_ids:
        logger.debug("✅ No stale syncing connectors found on startup")
        return 0

    for connector_id in stale_ids:
        logger.warning(f"Recovering stale sync state for connector {connector_id!r}")
        last_log = get_last_sync_log(connector_id)
        if last_log is not None and last_log.status not in _TERMINAL_SYNC_LOG_STATUSES:
            close_sync_log(
                connector_id,
                seq=last_log.seq,
                status=SyncStatus.FAILED,
                error="Service restarted during sync tick",
                # connector sync_status already set to 'out of sync' by reset_stale_syncing_connectors
            )

    logger.info(f"🔄 Recovered {len(stale_ids)} stale connector(s) on startup")
    return len(stale_ids)
```

---

### 7.4 Lifespan Integration (`app.py`)

```python
@asynccontextmanager
async def lifespan(app: FastAPI):
    async_engine = create_async_engine(_make_async_db_url())
    data_store = AsyncSQLAlchemyDataStore(async_engine)

    async with AsyncScheduler(data_store=data_store) as sched:
        scheduler_module._scheduler = sched

        # Crash recovery: close any open sync logs and unblock stale 'syncing' connectors.
        recover_connector_sync_state()

        # Recover existing connectors without resetting schedules (fire_immediately=False)
        for connector in get_all_connectors():
            await register_connector_job(connector.id, connector.sync_interval_seconds, fire_immediately=False)

        pool_size = max(len(get_all_connectors()) + 4, 8)
        asyncio.get_running_loop().set_default_executor(ThreadPoolExecutor(max_workers=pool_size))

        yield
    scheduler_module._scheduler = None
```

---

## 8. Concurrency & Thread Offloading

### 8.1 Why Offload to Thread Pool?
Scanners perform synchronous blocking I/O (`boto3` calls, Paramiko SFTP operations). Running these on the main event loop thread would freeze Uvicorn request handling and prevent cancellation signals from being processed.

### 8.2 Execution & Cancellation Model
- All blocking scanner methods (`connect`, `scan`, `download_to`) are called via `await asyncio.to_thread(scanner.<method>, *args)`.
- Event loop remains fully responsive.
- Interrupt state is stored exclusively in the DB — no in-memory sets or task registries:
  - **`connectors.sync_status = 'delete pending'`** — set by `mark_sync_delete_pending` when DELETE arrives mid-sync.
  - **`connector_sync_logs.status = 'cancel pending'`** — set by `mark_sync_cancel_pending` when stop-sync is requested.
- `_check_interrupt_call(connector_id, sync_seq)` reads both sources on every call. It is invoked at **four checkpoints**:
  - In `run_tick` — immediately after `open_new_sync_log` (Phase 1b).
  - In `_process_new_files` — before each batch starts (Checkpoint 2).
  - In `_process_new_files` — after each batch of downloads completes (Checkpoint 3).
  - In `_wait_for_job` — on every poll cycle (Checkpoint 4).
- `CancelledError` is raised in the coroutine at those checkpoints, never inside the thread.
- A download already in flight runs to natural completion. Interrupts arriving between files are caught at the next checkpoint.
- `run_tick`'s `except asyncio.CancelledError` block calls `_handle_interrupt` which dispatches:
  - `_cancel_tick` + `_sweep_staging_dir(sync_seq=sync_seq)` for `SYNC_CANCEL`.
  - `_cancel_tick` + `_run_delete_teardown(connector_id)` (synchronous, runs inline) for `DELETE_CONNECTOR`.
- `_run_teardown` in `connectors.py` handles the background-task path (Case B: no tick running at delete time).
- No safety-net timeout. No waiting for the tick to exit before teardown begins.

### 8.3 Thread Pool Sizing
Each ticking connector holds at most **1 thread at a time**.
$$\text{pool\_size} = \max(N + 4, 8) \quad \text{(where } N = \text{total configured connectors)}$$

---

## 9. Implementation Plan & PR Breakdown

### Implemented PRs

- **PR 1 — DB Schema + ORM Models + Settings ✅:** `connectors`, `connector_document_checksum`, `connector_sync_logs` tables & ORM models created (`db/scripts/init_schema.sql`, `db/models.py`). `connector_sync_logs` now has auto-increment `id` PK with `UNIQUE (connector_id, seq)`.
- **PR 2 — DB Operations Layer ✅:** `manager.py` & `utils/db.py` fully implemented. All checksum helpers (`insert_connector_checksum`, `delete_connector_checksum`, `find_connector_doc_by_checksum`, `get_connector_checksums`, `get_all_connector_checksums`), sync-lock/signal helpers (`try_acquire_sync_lock`, `mark_sync_cancel_pending`, `mark_sync_delete_pending`), sync-log helpers (`open_sync_log`, `close_sync_log`, `update_sync_log_progress`, `get_sync_logs`, `get_sync_log_status`, `get_active_sync_seq`), and startup helper (`reset_stale_syncing_connectors`) are all present and tested in `test_connector_db.py`.
- **PR 3 — REST API Endpoints ✅:** Full CRUD in `connectors.py` (`POST`, `PUT`, `DELETE`, `GET`, `GET /{id}`, `GET /{id}/syncs`, `POST /{id}/syncs`, `POST /{id}/syncs/{seq}/stop`). Connector-sourced document visibility filtering in `documents.py`.
- **PR 4a — Scanner Abstraction ✅:** `BaseScanner` interface (`base_scanner.py`) & `scanner_factory.py` with `_REGISTRY` for `"s3"` and `"ssh"` types.
- **PR 4b — SSH/SFTP Scanner ✅:** `SSHScanner` implemented in `connectors/scanners/ssh_scanner.py`. *(Named `ssh_scanner.py` rather than `sftp_scanner.py` — accepted naming.)*
- **PR 5 — S3 Scanner ✅:** `S3Scanner` implementation & tests.
- **PR 6 — Core Sync Engine ✅:** `sync_tick.py` fully implemented. `run_tick`, `_classify`, `_process_new_files` (batch download, integrity skip, `batch_failed` flag, `_wait_for_job`), `_delete_orphans`, `_handle_interrupt`, `_run_delete_teardown`. `_check_interrupt_call(connector_id, sync_seq)` replaces the old `_check_delete_pending` — now reads from both tables. Tests in `test_sync_tick.py`.
- **PR 7 — Scheduler + Lifespan + `POST /syncs` Dispatch ✅:** `scheduler.py` created. `lifespan()` in `app.py` starts `AsyncScheduler`, calls `recover_connector_sync_state()`, and registers all connector jobs. `POST /syncs` in `connectors.py` dispatches `run_tick` and returns `sync_seq`. `SyncStatus` enum extended with `CANCEL_PENDING`, `DELETE_PENDING`, `CANCELLED`. `recover_connector_sync_state()` added to `utils/recovery.py`.
- **PR 9 — Non-Blocking DELETE ✅:** `DELETE /v1/connectors/{id}` uses `mark_sync_delete_pending` + `_run_teardown` (background task). Tick handles teardown via `_check_interrupt_call` → `_run_delete_teardown` in `sync_tick.py`. Teardown paths are split: `_run_teardown` in `connectors.py` (Case B) and `_run_delete_teardown` in `sync_tick.py` (Case A).
- **PR 8 — Cancel Sync Endpoint ✅:** `POST /syncs/{seq}/stop` implemented. `mark_sync_cancel_pending` writes to `connector_sync_logs.status`. `SyncStatus.CANCEL_PENDING` and `InterruptType.SYNC_CANCEL` handle the cancel path in `_check_interrupt_call`. `close_sync_log` maps `CANCELLED` → `OUT_OF_SYNC` so the connector resumes on the next interval. `failed_files` counter removed from schema, models, and API.

---

### Notes on Divergences from Original Proposal

- `connector_sync_logs` PK changed from composite `(connector_id, seq)` to auto-increment `id`; uniqueness enforced by `UNIQUE (connector_id, seq)`. `seq` remains the logical identifier for API purposes.
- `POST /v1/connectors/{id}/syncs` returns `{"sync_seq": <int>}` — both new and already-running syncs return the active seq, making the response useful for callers regardless of idempotency path.
- Cancel-sync endpoint is `POST /{id}/syncs/{seq}/stop` (not `DELETE /{id}/sync`). Requires `sync_seq` to prevent stale cancels.
- `mark_sync_cancel_pending` writes to `connector_sync_logs.status`, not `connectors.sync_status`. The connector row stays `'syncing'` until `close_sync_log` transitions it.
- `_check_interrupt_call` (not `_check_delete_pending`) is the single interrupt-check entry point; it combines both DELETE and CANCEL detection.
- `_run_delete_teardown` (inline in `sync_tick.py`) handles teardown for the Case A path instead of dispatching `_run_teardown` as a new task.
