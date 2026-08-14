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
- `Sync Execution Engine`: `_run_tick()` async coroutine executing the multi-phase sync cycle offloaded to thread pool for blocking I/O.

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

    connector_id = Column(String, ForeignKey("connectors.id", ondelete="CASCADE"), primary_key=True)
    seq = Column(Integer, primary_key=True)
    started_at = Column(DateTime(timezone=True), nullable=False)
    finished_at = Column(DateTime(timezone=True), nullable=True)
    total_files = Column(Integer, nullable=False, default=0)
    new_files = Column(Integer, nullable=False, default=0)
    removed_files = Column(Integer, nullable=False, default=0)
    failed_files = Column(Integer, nullable=False, default=default=0)
    status = Column(String, nullable=False, default="started")
    error = Column(String, nullable=False, default="")

    connector = relationship("Connector", back_populates="sync_logs")
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
  ├─ 2. Read current sync_status
  │
  ├─ [Case A] sync_status == 'syncing'  (a tick is actively running)
  │     ├─ UPDATE connectors SET sync_status = 'delete_pending'  (committed)
  │     └─ Return 204 No Content immediately
  │          └─ The running tick will hit _check_delete_pending at its next
  │             checkpoint, raise CancelledError, call _cancel_tick (sync log
  │             status = 'cancelled'), then dispatch asyncio.create_task(_run_teardown)
  │
  └─ [Case B] sync_status != 'syncing'  ('up to date' | 'out of sync')
        ├─ UPDATE connectors SET sync_status = 'delete_pending'  (committed)
        ├─ asyncio.create_task(_run_teardown(connector_id))
        └─ Return 204 No Content immediately
```

#### Teardown Flow

```text
_run_teardown(connector_id) (Background asyncio.Task)
  │
  ├─ Step A: _finalize_open_sync_log(connector_id)
  │            └─ get_last_sync_log → if status not in {completed, failed, cancelled}
  │               → close_sync_log(status='cancelled', update_connector_status=False)
  │               (no-op if last log is already terminal, or if no log exists)
  ├─ Step B: remove_connector_job(connector_id) → unregisters APScheduler job
  ├─ Step C: snapshot owned checksums for connector
  ├─ Step D: remove_connector_checksum_entry row by row
  │            └─ if remaining_owner_count == 0 → best-effort delete_document_internal(doc_id)
  ├─ Step E: DELETE FROM connectors WHERE id = :connector_id (cascades to sync_logs)
  └─ Step F: cleanup_connector_staging_dirs(connector_id)
```

![Delete Flow](delete-flow.svg)

---

### 3.4 `GET /v1/connectors` & `GET /v1/connectors/{connector_id}`

- `GET /v1/connectors`: Returns active connectors list without secrets.
- `GET /v1/connectors/{id}`: Returns connector details + latest file processing counters (`total_files`, `new_files`, `removed_files`, `failed_files`).

---

### 3.5 `GET` & `POST /v1/connectors/{connector_id}/sync`

- `GET`: Returns paginated execution history items from `connector_sync_logs` (`limit` default 50, max 200).
- `POST`: Triggers an immediate manual sync tick.
  - Safe & idempotent: acquires DB lock via atomic `UPDATE connectors SET sync_status='syncing' WHERE id=:id AND sync_status!='syncing' RETURNING id`.
  - If locked, returns `202 Accepted` immediately (no-op).
  - If lock acquired, opens log row (`open_sync_log`), dispatches `asyncio.create_task(_run_tick(connector_id))`, and returns `202 Accepted`.

```text
POST /v1/connectors/{connector_id}/sync
  │
  ├─ 1. Check existence → 404 if not found
  ├─ 2. try_acquire_sync_lock(connector_id) (atomic UPDATE ... RETURNING)
  │      ├─ Lock unavailable (already syncing) → return 202 Accepted (no-op)
  │      └─ Lock acquired → proceed
  ├─ 3. open_new_sync_log(connector_id) → sync_seq
  ├─ 4. asyncio.create_task(_run_tick(connector_id))
  └─ 5. Return 202 Accepted
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
| `delete_connector()` / `mark_connector_delete_pending()` | Set `sync_status = 'delete pending'` or delete row. |
| `find_connector_doc_by_checksum(checksum)` | Lookup existing `doc_id` in `connector_document_checksum`. |
| `get_connector_checksums(connector_id)` | Return set of checksums owned by `connector_id`. |
| `get_all_connector_checksums()` | Return set of all distinct checksums in `connector_document_checksum`. |
| `insert_connector_checksum(connector_id, checksum, doc_id)` | Insert `(checksum, connector_id, doc_id)` with `ON CONFLICT DO NOTHING`. |
| `delete_connector_checksum(connector_id, checksum)` | Remove `(checksum, connector_id)`; returns `(remaining_owner_count, doc_id)`. |
| `get_connector_sync_status(connector_id)` | `SELECT sync_status FROM connectors WHERE id = :id`. Returns the current status string or `None` if not found. Used by `_check_delete_pending`. |
| `try_acquire_sync_lock(connector_id)` | Atomic `UPDATE connectors SET sync_status='syncing' WHERE id=:id AND sync_status!='syncing' RETURNING id`. |
| `open_sync_log(connector_id)` | Create tick log row with `seq = COALESCE(MAX(seq), 0) + 1` and status `'started'`. |
| `close_sync_log(connector_id, seq, status, error, counters, update_connector_status)` | Finalize log row. When `update_connector_status=True` (default), also updates connector `last_sync_at` / `sync_status`. Pass `update_connector_status=False` when closing a log with `'cancelled'` so that `connectors.sync_status` stays `'delete_pending'`. |
| `get_last_sync_log(connector_id)` | `SELECT ... FROM connector_sync_logs WHERE connector_id = :id ORDER BY seq DESC LIMIT 1`. Returns the most-recent log row or `None`. Used by `_run_teardown` to detect open logs. |
| `reset_stale_syncing_connectors()` | On startup: `UPDATE connectors SET sync_status='out of sync' WHERE sync_status='syncing'`. Returns the list of affected `connector_id` values. Used by `recover_connector_sync_state()`. |
| `update_sync_log_progress()` | Increment progress counters during execution. |
| `get_sync_logs(connector_id, limit, offset)` | Paginated log history query. |

---

### 4.2 DB Methods Implementation Code

```python
class DatabaseManager:
    @staticmethod
    def lookup_connector_content_by_checksum(checksum: str) -> str | None:
        with get_db_session() as session:
            row = session.execute(
                select(ConnectorDocumentChecksum.doc_id)
                .where(ConnectorDocumentChecksum.checksum == checksum)
                .limit(1)
            ).one_or_none()
        return row[0] if row else None

    @staticmethod
    def add_connector_checksum_entry(connector_id: str, checksum: str, doc_id: str) -> None:
        with get_db_session() as session:
            stmt = (
                insert(ConnectorDocumentChecksum)
                .values(checksum=checksum, connector_id=connector_id, doc_id=doc_id)
                .on_conflict_do_nothing(index_elements=["checksum", "connector_id"])
            )
            session.execute(stmt)

    @staticmethod
    def remove_connector_checksum_entry(connector_id: str, checksum: str) -> tuple[int, str | None]:
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
                text("""
                    UPDATE connectors
                    SET sync_status = 'syncing'
                    WHERE id = :id AND sync_status != 'syncing'
                    RETURNING id
                """),
                {"id": connector_id},
            ).one_or_none()
            return result is not None

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

- **Phase 0 (Lock):** Acquire lock via `try_acquire_sync_lock(connector_id)`.
- **Phase 1 (Log):** Open tick row (`open_sync_log`).
- **Phase 2 (Scan & State):** Query owned `known_checksums` and `all_checksums`. Offload blocking network calls (`connect`, `scan`) to thread pool via `run_in_executor(None, ...)`.
- **Phase 3 (Classify):** `_classify()` separates `scanned_files` into `skip_list`, `ingest_list`, and cross-connector duplicates. Cross-connector duplicates execute `insert_connector_checksum` inline using existing `doc_id`.
- **Phase 4a (Ingest New Files):** Sequentially download files in `ingest_list`:
  - Dedicated per-file staging directory: `staging/connectors/{connector_id}-{job_id}-{batch_number}`.
  - Offload download to executor thread (`run_in_executor(None, scanner.download_to, ...)`).
  - Call `create_job()` → obtain `doc_id`, insert checksum row, write `documents.metadata`.
  - Cleanup staging directory immediately in `finally` block before downloading next file.
- **Phase 4b (Orphan Removal):** Runs **strictly after Phase 4a completes**. Delete checksum rows for `orphan_checksums = known_checksums − scanned_checksums`. If remaining count is `0`, call `DELETE /v1/documents/{doc_id}`.
- **Phase 5 (Finalize):** Finalize logs and update `sync_status` to `'completed'` or `'out of sync'`. Call `scanner.close()` in top-level `finally`.

---

### 6.2 Implementation Code

```python
async def _run_tick(connector_id: str) -> None:
    loop = asyncio.get_running_loop()
    config = get_connector_by_id(connector_id)
    sync_seq = open_sync_log(connector_id)
    scanner = build_scanner(config)
    try:
        _check_delete_pending(connector_id)  # Phase boundary: before scan
        await loop.run_in_executor(None, scanner.connect)
        scanned_files: list[tuple[str, str]] = await loop.run_in_executor(None, scanner.scan)

        _check_delete_pending(connector_id)  # Phase boundary: after scan, before classify
        known_checksums = set(get_connector_checksums(connector_id))
        all_checksums = set(get_all_connector_checksums())

        ingest_list, orphan_checksums = _classify(connector_id, scanned_files, known_checksums, all_checksums)

        await _process_new_files(sync_seq, connector_id, scanner, ingest_list)
        await _delete_orphans(connector_id, orphan_checksums)
        _complete_tick(sync_seq, connector_id)
    except asyncio.CancelledError:
        _cancel_tick(sync_seq, connector_id)  # status='cancelled', sync_status='delete_pending'
        asyncio.create_task(_run_teardown(connector_id))  # hand off to teardown
        raise
    except Exception as exc:
        logger.error(f"Tick failed for connector {connector_id!r}: {exc}", exc_info=True)
        _fail_tick(sync_seq, connector_id, exc)  # status='failed', sync_status='out of sync'
    finally:
        scanner.close()


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
            pass  # Already owned by this connector
        elif checksum in all_checksums:
            if checksum not in seen_this_tick:
                seen_this_tick.add(checksum)
                doc_id = find_connector_doc_by_checksum(checksum)
                insert_connector_checksum(connector_id, checksum, doc_id)
        else:
            if checksum not in seen_this_tick:
                seen_this_tick.add(checksum)
                ingest_list.append((remote_path, checksum))

    orphan_checksums = known_checksums - scanned_checksums
    return ingest_list, orphan_checksums


async def _process_new_files(sync_seq: int, connector_id: str, scanner: BaseScanner, ingest_list: list[tuple[str, str]]) -> None:
    loop = asyncio.get_running_loop()
    staging_base = settings.digitize.staging_dir / "connectors"
    for batch_number, (remote_path, checksum) in enumerate(ingest_list):
        _check_delete_pending(connector_id)  # checkpoint: before starting each download
        job_id = generate_job_id()
        batch_dir = staging_base / f"{connector_id}-{job_id}-{batch_number}"
        batch_dir.mkdir(parents=True, exist_ok=True)
        try:
            local_path = batch_dir / Path(remote_path).name
            await loop.run_in_executor(None, scanner.download_to, remote_path, local_path)
            _check_delete_pending(connector_id)  # checkpoint: after download returns, before job creation
            doc_id = await create_job(connector_id, checksum, staging_dir=batch_dir)
            insert_connector_checksum(connector_id, checksum, doc_id)
        except asyncio.CancelledError:
            raise  # propagates to _run_tick's except block → _cancel_tick + _run_teardown
        except Exception as exc:
            logger.warning(f"Failed to ingest {remote_path!r}: {exc}")
            increment_failed_files(sync_seq)
        finally:
            cleanup_staging_directory(batch_dir.name, staging_base, ignore_errors=True)
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
# All delete state is read from the connectors table.

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
    1. _check_delete_pending — skips tick entirely if connector is being deleted.
    2. try_acquire_sync_lock — skips tick if one is already running.
    """
    _check_delete_pending(connector_id)   # raises CancelledError → APScheduler suppresses, tick skipped
    if not try_acquire_sync_lock(connector_id):
        return
    await _run_tick(connector_id)


def _check_delete_pending(connector_id: str) -> None:
    """Query the DB and raise CancelledError if the connector is marked for deletion.

    Called at every phase boundary inside _run_tick and _process_new_files.
    Raises asyncio.CancelledError — caught by _run_tick's except block which
    calls _cancel_tick and dispatches _run_teardown.
    """
    status = get_connector_sync_status(connector_id)   # SELECT sync_status FROM connectors WHERE id = :id
    if status == "delete_pending":
        raise asyncio.CancelledError(f"Connector {connector_id!r} is pending deletion")


async def _run_teardown(connector_id: str) -> None:
    """Performs full connector teardown. Dispatched either:
    - By the DELETE handler directly (when no tick was running at delete time).
    - By _run_tick's except CancelledError block (when a tick was interrupted).
    """
    # Guard: ensure the last sync log (if any) is in a terminal state before
    # delete_connector cascades the connector_sync_logs rows away.
    # Case A (tick was running): _cancel_tick already closed the log as
    #   'cancelled' before dispatching this task — the check is a no-op.
    # Case B (no tick was running): the last log may be stuck in 'started'
    #   from a previous crash. Close it to 'cancelled' now.
    _finalize_open_sync_log(connector_id)

    await remove_connector_job(connector_id)

    owned_rows = get_connector_checksums_with_docs(connector_id)
    for checksum, doc_id in owned_rows:
        remaining, _ = remove_connector_checksum_entry(connector_id, checksum)
        if remaining == 0:
            delete_document_internal(doc_id)

    delete_connector(connector_id)          # cascades to connector_sync_logs
    cleanup_connector_staging_dirs(connector_id)


_TERMINAL_SYNC_LOG_STATUSES = {SyncStatus.COMPLETED, SyncStatus.FAILED, SyncStatus.CANCELLED}


def _finalize_open_sync_log(connector_id: str) -> None:
    """Close the most-recent sync log to 'cancelled' if it is not already in a
    terminal state (completed | failed | cancelled).

    Uses update_connector_status=False so that connectors.sync_status remains
    'delete_pending' — _run_teardown will remove the connector row entirely
    moments later, so no further status update is needed.
    """
    last_log = get_last_sync_log(connector_id)
    if last_log is not None and last_log.status not in _TERMINAL_SYNC_LOG_STATUSES:
        close_sync_log(
            connector_id,
            seq=last_log.seq,
            status=SyncStatus.CANCELLED,
            update_connector_status=False,
        )
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
   handles regular jobs. `update_connector_status=False` is passed because
   `reset_stale_syncing_connectors()` already set the connector status correctly.

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
                update_connector_status=False,  # reset_stale_syncing_connectors already set it
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
- All blocking scanner methods (`connect`, `scan`, `download_to`) are called via `await loop.run_in_executor(None, scanner.<method>, *args)`.
- Event loop remains fully responsive.
- Delete state is stored in the DB (`sync_status = 'delete_pending'`). There are no in-memory sets or task registries.
- `_check_delete_pending(connector_id)` does a live `SELECT sync_status FROM connectors WHERE id = :id` and raises `asyncio.CancelledError` if the result is `'delete_pending'`. It is called:
  - In `_run_tick_wrapped` — skips the tick entirely if delete is already set when the scheduler fires.
  - In `_run_tick` — at the phase boundary before scan, and after scan before classify.
  - In `_process_new_files` — before each download starts, and after each download returns.
- `CancelledError` is raised in the coroutine at those checkpoints, never inside the thread.
- A download already in flight runs to natural completion. Deletes that arrive between files are caught at the next checkpoint.
- `_run_tick`'s `except asyncio.CancelledError` block calls `_cancel_tick` (sets sync log `status = 'cancelled'`) then dispatches `asyncio.create_task(_run_teardown(connector_id))`.
- No safety-net timeout. No waiting for the tick to exit before teardown begins.

### 8.3 Thread Pool Sizing
Each ticking connector holds at most **1 thread at a time**.
$$\text{pool\_size} = \max(N + 4, 8) \quad \text{(where } N = \text{total configured connectors)}$$

---

## 9. Implementation Plan & PR Breakdown

### Implemented PRs
- **PR 1 — DB Schema + ORM Models + Settings ✅:** `connectors`, `connector_document_checksum`, `connector_sync_logs` tables & ORM models created (`db/scripts/init_schema.sql`, `db/models.py`).
- **PR 2 — DB Operations Layer ✅:** `manager.py` & `utils/db.py` methods fully implemented with tests in `test_connector_db.py`. All checksum helpers (`insert_connector_checksum`, `delete_connector_checksum`, `find_connector_doc_by_checksum`, `get_connector_checksums`, `get_all_connector_checksums`) and sync-log helpers (`open_sync_log`, `close_sync_log`, `update_sync_log_progress`, `get_sync_logs`) are present. (*Pending minor addition: `get_connector_sync_status(connector_id)` — `SELECT sync_status FROM connectors WHERE id = :id` — not yet in `manager.py` or `utils/db.py`*).
- **PR 3 — REST API Endpoints ✅:** Full CRUD in `connectors.py` (`POST`, `PUT`, `DELETE`, `GET`, `GET /{id}`, `GET /{id}/syncs`). Connector-sourced document visibility filtering in `documents.py` (`exclude_connector_sourced=True` on list, `is_connector_sourced_document` guard on GET/DELETE).
- **PR 4a — Scanner Abstraction ✅:** `BaseScanner` interface (`base_scanner.py`) & `scanner_factory.py` with `_REGISTRY` for `"s3"` and `"ssh"` types.
- **PR 4b — SSH/SFTP Scanner ✅:** `SSHScanner` implemented in `connectors/scanners/ssh_scanner.py` (subclasses `BaseScanner`, uses Paramiko for SFTP walk + SSH `md5sum` hashing, registered in `scanner_factory.py` under `"ssh"`). Full unit-test coverage in `test_connector_scanners.py` (connect/close, scan, download, `_load_private_key`, factory registration). *(Note: file is named `ssh_scanner.py` rather than `sftp_scanner.py` as originally proposed — this is the accepted name.)*
- **PR 5 — S3 Scanner ✅:** `S3Scanner` implementation & tests in `test_connector_scanners.py`.

---

### Pending PRs

#### PR 6 — Core Sync Engine (`_classify` + `_run_tick`) ❌
- **Target File:** `services/digitize/connectors/sync_tick.py`
- **Deliverables:** Implement `_classify()`, `_process_new_files()`, `_delete_orphans()`, and `_run_tick()`. Wire DB helper functions and staging cleanup. Unit & integration tests in `tests/test_sync_tick.py` (test file exists but module is absent).
- **Cancellation helper:** Add `_check_delete_pending(connector_id: str) -> None` (imported from `scheduler.py`) that does a live DB query — raises `asyncio.CancelledError()` if `connectors.sync_status == 'delete_pending'` for the given `connector_id`. No in-memory set. Call it:
  - At the start of `_run_tick` after acquiring the lock (phase boundary before scan).
  - At the top of the `_process_new_files` loop (before each download starts).
  - Once after `run_in_executor` returns (after each download completes).

#### PR 7 — Scheduler + Lifespan + `POST /sync` Dispatch ❌
- **Target Files:** `services/digitize/connectors/scheduler.py`, `app.py`, `connectors.py`
- **Deliverables:**
  - Create `scheduler.py` (job registration, `_run_tick_wrapped`, `_check_delete_pending`, `_run_teardown`, `_finalize_open_sync_log`). No `_live_tasks` or `_pending_deletions` dicts.
  - Wire `lifespan()` in `app.py` to start `AsyncScheduler` and recover connector sync state (`fire_immediately=False`). Currently `app.py` only calls `recover_zombie_jobs()` — connector scheduler startup is absent.
  - Wire `POST /v1/connectors/{id}/sync` in `connectors.py`: DB lock check → open log → `asyncio.create_task(_run_tick)`. Currently the endpoint does not exist.
  - Add `get_last_sync_log(connector_id)` to `manager.py` / `utils/db.py` (not yet present).
  - Add `reset_stale_syncing_connectors()` to `manager.py` / `utils/db.py` (not yet present).
  - Add `update_connector_status: bool = True` parameter to `close_sync_log` in `manager.py`; skip the `UPDATE connectors SET sync_status` statement when `False` (parameter absent from current signature).
  - Add `SyncStatus.CANCELLED = "cancelled"` and `SyncStatus.DELETE_PENDING = "delete pending"` to `connectors/models.py` (only `UP_TO_DATE`, `SYNCING`, `OUT_OF_SYNC`, `STARTED`, `COMPLETED`, `FAILED` exist today).
  - Add `recover_connector_sync_state()` to `utils/recovery.py`; call it in `lifespan()` in `app.py` before scheduler job registration (currently only `recover_zombie_jobs()` is called).

#### PR 9 — Non-Blocking DELETE Wiring ❌
- **Target Files:** `services/digitize/api/v1/connectors.py`, `scheduler.py`
- **Deliverables:**
  - Replace the current blocking DELETE implementation (synchronous teardown in `connectors.py`) with the non-blocking flow:
    - Read `sync_status` before marking delete.
    - If `'syncing'`: `UPDATE sync_status = 'delete_pending'` → return `204`. Tick handles teardown via `_check_delete_pending`.
    - If not `'syncing'`: `UPDATE sync_status = 'delete_pending'` → `asyncio.create_task(_run_teardown(id))` → return `204`.
  - Implement `_run_teardown()` in `scheduler.py`: `_finalize_open_sync_log` → `remove_connector_job` → checksum snapshot → per-checksum delete + doc cleanup → `delete_connector` → staging sweep.

#### PR 8 — Cancelled Job Status (Enhancement) ❌
- **Target Files:** `db/scripts/init_schema.sql`, `models.py`, `db/manager.py`, `connectors.py`
- **Deliverables:**
  - Update `jobs.status` check constraint in `init_schema.sql` to include `'cancelled'` (currently: `'accepted', 'in_progress', 'completed', 'failed'`).
  - Add `JobStatus.CANCELLED = "cancelled"` to `models.py` (enum currently has only `ACCEPTED`, `IN_PROGRESS`, `COMPLETED`, `FAILED`).
  - Add DB helper `cancel_connector_jobs(connector_id)` to `manager.py` / `utils/db.py` — sets `accepted`/`in_progress` jobs for a connector to `cancelled`. Call during teardown.
