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
- The `file_checksum_registry` table and the `FileChecksumRegistry` ORM model already exist in the codebase (merged in the de-duplication PR). The table uses `sha256` as its primary key — connector code must reference this column by that name.

These assumptions are referenced once here and not repeated below.

---

## 2. System Overview

### 2.1 Architecture Diagram

![Architecture Diagram](architecture-diagram.svg)

### 2.2 Runtime Flow

```text
POST
  → store connector config
  → start worker
  → worker runs first tick immediately

PUT
  → update connector config in DB
  → running thread re-reads config from DB before next tick

Each tick
  → fetch latest connector config from DB
  → scan source — yields (remote_path, checksum) per file
  → compare checksums with known_checksums from registry
  → skip files whose checksum is already registered (zero bytes transferred)
  → ingest only new content
  → store checksum + doc_id in file_checksum_registry
  → store source checksum in document metadata
  → remove orphaned content by ref count
  → write sync status + history
```

### 2.3 Main Components

- `active_connectors`: current connector configuration and top-level sync state
- `file_checksum_registry`: global content registry keyed by `sha256` — stores a content fingerprint and a reference to the ingested `doc_id`. **Already implemented** — table and ORM model exist.
- `connector_file_membership`: which connector currently references which checksum
- `connector_sync_history`: one row per worker tick
- `ConnectorWorkerManager`: owns worker thread lifecycle
- `ConnectorSyncWorker`: executes periodic sync logic
- scanner implementations: transport-specific remote access for SFTP and S3; S3 scanner derives the checksum from the S3 ETag returned by `list_objects_v2`; SFTP scanner uses a remotely-computed MD5 — both stored as `sha256` in `file_checksum_registry`

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

> **Checksum-based dedup:** For S3 connectors, `list_objects_v2` returns the object ETag at no extra API cost — the scanner stores this as the checksum in `file_checksum_registry.sha256`. If the checksum is already registered the file is **never downloaded**. The checksum is also stored in document `metadata.source_checksum` for traceability. See [§4.3](#43-file_checksum_registry) and [§7.2](#72-s3-scanner).

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
  → list connector checksums
  → for each checksum:
       remove membership in transaction
       count remaining refs for this checksum
       if refs == 0:
         DELETE /v1/documents/{doc_id}
         DELETE FROM file_checksum_registry WHERE sha256 = $1
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

---

## 4. Data Model

**Modified file:** `services/digitize/db/scripts/init_schema.sql`

### 4.1 Table Relationships

```text
active_connectors
  └─< connector_file_membership >─┐
                                  └─ file_checksum_registry ─> documents

active_connectors
  └─< connector_sync_history
```

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

> **Already implemented.** The table and ORM model were merged in the de-duplication PR. The existing schema uses `sha256` as the primary key (not `checksum`). The connector implementation must use `sha256` as the column/field name throughout.

Current schema (already in `init_schema.sql` and `db/models.py`):

```sql
CREATE TABLE IF NOT EXISTS file_checksum_registry (
    sha256   TEXT PRIMARY KEY,
    doc_id   TEXT NOT NULL UNIQUE REFERENCES documents(doc_id) ON DELETE CASCADE
);
```

Current ORM model (already in `db/models.py`):

```python
class FileChecksumRegistry(Base):
    __tablename__ = "file_checksum_registry"

    sha256: Mapped[str] = mapped_column(Text, primary_key=True)
    doc_id: Mapped[str] = mapped_column(
        Text,
        ForeignKey("documents.doc_id", ondelete="CASCADE"),
        nullable=False,
        unique=True,
    )
```

**Connector usage:** For S3 sources the scanner stores the S3 ETag returned by `list_objects_v2` (quotes stripped) as `sha256`. For SFTP sources the scanner stores the remotely-computed MD5 hex digest as `sha256`. Both are stored in `file_checksum_registry.sha256` with no schema difference — they are treated as opaque content fingerprints.

> **Document metadata:** When a document row is created the scanner also writes the fingerprint into `documents.metadata` as `metadata.source_checksum`. This makes the content fingerprint visible in the document record without a registry join.
>
> ```json
> {
>   "source_checksum": "0234031ed6cb7d686152f45c38f41bc6-13",
>   "source_type": "s3",
>   "bucket": "ai-services",
>   "key": "reports/sg248590-2.pdf"
> }
> ```

**Checksum format reference:**

| Source | Value stored in `sha256` | Dedup property |
| --- | --- | --- |
| S3 single-part | S3 ETag = `MD5(file_bytes)` — 32-char hex, no suffix | Unique per file content (for this upload method) |
| S3 multi-part | S3 ETag = `MD5(MD5(p₁)‖…‖MD5(pₙ))-N` — hex + `-N` suffix | Unique per (file content + part size) |
| SFTP | `md5sum` output from remote host — 32-char hex | Unique per file content, protocol-independent |

### 4.4 `connector_file_membership`

Maps connectors to the checksums they currently own.

```sql
CREATE TABLE IF NOT EXISTS connector_file_membership (
    connector_id  TEXT NOT NULL,
    checksum      TEXT NOT NULL,
    PRIMARY KEY (connector_id, checksum),
    FOREIGN KEY (connector_id) REFERENCES active_connectors(id) ON DELETE CASCADE,
    FOREIGN KEY (checksum) REFERENCES file_checksum_registry(sha256) ON DELETE CASCADE
);
```

Deduplication and deletion rules:

- Existing checksum in `file_checksum_registry` → reuse `doc_id`, add membership row only — **no download, no ingest**.
- New checksum → download file, run ingest pipeline, insert registry row with `doc_id`, then add membership.
- On removal, delete membership first, count remaining references for this checksum inside the same transaction, and delete the document and registry row only when the count reaches zero.

#### Reference-counted delete stub

```sql
BEGIN;

DELETE FROM connector_file_membership
WHERE connector_id = $1 AND checksum = $2;

-- Returns 0 when this was the last owner → safe to delete document + registry row
SELECT COUNT(*)
FROM connector_file_membership
WHERE checksum = $2;

COMMIT;
```

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

**`FileChecksumRegistry` already exists** in `services/digitize/db/models.py`. Add the remaining three models to that file:

- `ActiveConnector` — fields: `id` (PK), `type`, `host`, `connection_details` (JSONB), `allowed_extensions` (JSONB), `sync_interval_seconds`, `attached_at`, `last_sync_at`, `sync_status`
- `ConnectorFileMembership` — fields: `connector_id` (FK → `active_connectors`), `checksum` (FK → `file_checksum_registry.sha256`)
- `ConnectorSyncHistory` — fields: `id` (PK), `connector_id` (FK → `active_connectors`), `sync_id`, `started_at`, `finished_at`, `files_found`, `files_syncing`, `files_completed`, `files_failed`, `sync_status`

---

## 5. Database Operations Layer

**Modified file:** `services/digitize/utils/db.py`

The DB layer stores and returns ciphertext only. Encryption happens in the API layer; decryption happens in scanners.

### 5.1 Core functions

The following functions already exist in `services/digitize/db/manager.py` and cover the per-job de-duplication path — **do not re-implement them**:

| Existing function | Purpose |
| --- | --- |
| `upsert_file_checksum(sha256, doc_id)` | Register newly ingested content by sha256 |
| `find_completed_document_by_hash(sha256)` | Dedup lookup — returns `Document` or `None` |

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
| `lookup_content_by_checksum(checksum)` | connector dedup lookup — returns `doc_id` or `None` |
| `list_connector_checksums(connector_id)` | current checksums owned by this connector |
| `insert_connector_membership(connector_id, checksum)` | add ownership row |
| `delete_connector_membership_atomic(connector_id, checksum)` | remove ownership, return remaining ref count |
| `insert_sync_history()` | create tick row |
| `update_sync_history()` | finalize tick row |
| `update_sync_history_files_syncing()` | live progress updates |
| `list_sync_history()` | paginated history query |
| `set_document_metadata(doc_id, metadata)` | write `source_checksum` + S3 key into `documents.metadata` |

### 5.2 DB-layer stub

```python
def lookup_content_by_checksum(checksum: str) -> str | None:
    """Return doc_id if checksum is already registered, else None."""
    return tx.scalar(
        "SELECT doc_id FROM file_checksum_registry WHERE sha256 = :checksum",
        {"checksum": checksum},
    )


def delete_connector_membership_atomic(connector_id: str, checksum: str) -> int:
    """Delete one membership row and return remaining reference count."""
    with transaction() as tx:
        tx.execute(
            "DELETE FROM connector_file_membership WHERE connector_id = :cid AND checksum = :checksum",
            {"cid": connector_id, "checksum": checksum},
        )
        remaining = tx.scalar(
            "SELECT COUNT(*) FROM connector_file_membership WHERE checksum = :checksum",
            {"checksum": checksum},
        )
    return int(remaining)
```

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

- remote listing — yields `(remote_path, checksum)` pairs
- dedup check against `known_checksums` before downloading (strategy is transport-specific — see §7.1 and §7.2)
- file download (only when checksum is not already known)
- storing the checksum in `file_checksum_registry.sha256` and in `documents.metadata` after ingest
- connection lifecycle

### 6.2 Class diagram

```text
BaseScanner
  ├─ connect()
  ├─ scan(known_checksums)    → list[(remote_path, checksum)]
  ├─ download_to(remote_path, local_path)
  └─ close()

BaseScanner
  ├─ SFTPScanner          (checksum = remotely-computed MD5, stored in sha256 column)
  └─ S3Scanner            (checksum = S3 ETag from list_objects_v2, stored in sha256 column)
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
    def scan(self, known_checksums: set[str]) -> list[tuple[str, str]]:
        """Yield (remote_path, checksum) for files not in known_checksums.

        Implementations must skip any file whose checksum is already present
        in known_checksums — no download should occur for those files.
        Returns only files that need to be ingested.
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

> **Note — SFTP checksum is an MD5 digest.** The checksum for SFTP files is a hex MD5 digest (e.g. `"d41d8cd98f00b204e980..."`), computed remotely via `md5sum`, stored in `file_checksum_registry.sha256`. For S3 files the same column stores the S3 ETag returned by `list_objects_v2` — both are treated uniformly as an opaque content fingerprint (see [§7.2](#72-s3-scanner)).

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
def scan(self, known_checksums: set[str]):
    """Return (remote_file, checksum) for files whose checksum is not yet registered.

    For SFTP sources the checksum is a hex MD5 digest computed on the
    remote host via md5sum — stored in file_checksum_registry.sha256
    alongside S3 checksums (S3 ETags) with no schema difference.
    """
    found = []
    for remote_file in self._walk_remote_tree():
        if not self._is_allowed(remote_file.path):
            continue
        checksum = self._remote_md5(remote_file.path)
        if checksum in known_checksums:
            continue   # already ingested — skip download
        found.append((remote_file, checksum))
    return found
```

### 7.2 S3 scanner

**Modified file:** `services/digitize/connector/s3_scanner.py`

**Detailed design:** [S3 Scanner — Detailed Design Proposal](./s3-scanner-proposal.md)

Behavior:

- Auto-detect provider (AWS S3 or IBM COS) from `endpoint_url` hostname — no separate region field needed
- Build boto3 client per tick using `IBMCOSConnector._build_client()` (pure-Python, ppc64le compatible)
- List objects via `list_objects_v2` paginator — yields `(key, checksum)` where checksum = S3 ETag, at no extra API cost
- **Checksum pre-check before download:** if checksum is already in `known_checksums` → skip file entirely (zero bytes transferred)
- Download only new files via `download_fileobj()`
- Store checksum in `file_checksum_registry.sha256` and in `documents.metadata.source_checksum`

**Checksum dedup flow:**

```
list_objects_v2  →  (key, checksum)          # checksum = S3 ETag
                          │
          checksum in known_checksums?
                    │
          YES ──────┘                    NO
           ▼                              ▼
      skip — zero bytes             download_fileobj()
      add membership only           + _HashingWriter (inline MD5)
                                    ingest pipeline
                                    INSERT file_checksum_registry(checksum, ...)
                                    SET documents.metadata.source_checksum = checksum
```

S3 scan sketch:

```python
def scan(self, known_checksums: set[str]) -> list[tuple[str, str]]:
    """Return (key, checksum) for objects whose checksum is not yet registered."""
    to_ingest = []
    for key, checksum in self._list_document_keys():   # checksum = S3 ETag, free from list_objects_v2
        if checksum in known_checksums:
            continue                                    # skip — already ingested
        to_ingest.append((key, checksum))
    return to_ingest


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
    # checksum = S3 ETag from list_objects_v2 — stored in file_checksum_registry.sha256.
    # For single-part: local MD5 == checksum (sanity check available).
    # For multi-part:  local MD5 != checksum (different formula — expected).
    doc_id = run_ingest_pipeline(local_path)
    db_manager.upsert_file_checksum(sha256=checksum, doc_id=doc_id)
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

![Sync Worker Tick Flow](sync-worker-tick-flow.svg)

### 8.2 Worker rules

- Overlapping ticks are skipped by a tick guard.
- `files_syncing` is updated live during staging and download.
- Staging uses per-tick temporary directories.
- Download and ingest are blocking operations.
- Fatal errors mark the tick as failed.
- Per-file failures are counted and summarized instead of failing the whole connector permanently.

### 8.3 Worker stub

```python
def _run_tick(self) -> None:
    self.config = get_active_connector(self.connector_id)  # refresh before each tick
    sync_id = insert_sync_history(self.connector_id)
    scanner = build_scanner(self.config)
    try:
        scanner.connect()

        # known_checksums: set of checksums already in file_checksum_registry
        # for this connector. Passed to scanner.scan() so the scanner can skip
        # files whose checksum is already registered — no download occurs.
        known_checksums = set(list_connector_checksums(self.connector_id))

        # scan() returns only (remote_path, checksum) pairs not in known_checksums.
        remote_files = scanner.scan(known_checksums)

        to_ingest, orphan_checksums = self._diff(remote_files, known_checksums)
        self._process_new_files(sync_id, scanner, to_ingest)
        self._delete_orphans(orphan_checksums)
        self._complete_tick(sync_id)
    except Exception as exc:
        self._fail_tick(sync_id, exc)
    finally:
        scanner.close()
```

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
- `file_checksum_registry` table (with `sha256` PK) exists in `init_schema.sql`
- `FileChecksumRegistry` ORM model exists in `db/models.py`

**What's to build:**
- 3 new tables: `active_connectors`, `connector_file_membership`, `connector_sync_history`
- 3 new ORM models: `ActiveConnector`, `ConnectorFileMembership`, `ConnectorSyncHistory`
- Settings entries: staging directory, worker stop timeout, monitor poll interval, respawn back-off cap, `CONNECTOR_SYNC_INTERVAL_SECONDS` (default `300`, written into `active_connectors.sync_interval_seconds` on connector creation)

**How to test:**
- Run `init_schema.sql` against a local/test DB and assert all 3 new tables exist with correct columns, constraints, and indexes
- Unit test: instantiate ORM models and map them against the schema

---

### PR 2 — DB Operations Layer

**Files touched:** `services/digitize/utils/db.py`

**What's already done:**
- `upsert_file_checksum(sha256, doc_id)` in `db/manager.py`
- `find_completed_document_by_hash(sha256)` in `db/manager.py`

**What's to build:**
All connector DB functions from §5.1:
`insert_active_connector`, `upsert_active_connector`, `get_active_connector`, `list_active_connectors`, `delete_active_connector`, `update_connector_sync_status`, `merge_connection_details`, `lookup_content_by_checksum`, `list_connector_checksums`, `insert_connector_membership`, `delete_connector_membership_atomic`, `insert_sync_history`, `update_sync_history`, `update_sync_history_files_syncing`, `list_sync_history`

**How to test:**
- Unit tests per function against a test DB (real or in-memory with pg_testcontainer / SQLite shim)
- Assert `delete_connector_membership_atomic` returns correct remaining ref counts
- Assert `merge_connection_details` correctly merges keys without clobbering untouched fields

---

### PR 3 — REST API Endpoints

**Files touched:** connector router/handler file(s)

**What's to build:**
- `POST /v1/connectors` — validate body, encrypt secrets, call `insert_active_connector`, return `202`
- `PUT /v1/connectors/{id}` — partial update, re-encrypt if credentials included, `merge_connection_details`, return `200`
- `DELETE /v1/connectors/{id}` — stub only (stops at "would stop worker" + calls DB delete), no worker logic yet
- `GET /v1/connectors` and `GET /v1/connectors/{id}` — read from DB, strip secret fields
- `GET /v1/connectors/{id}/sync-history` — paginated query with `limit`/`offset`

**How to test:**
- Integration tests using `httpx.AsyncClient` + test DB
- Assert secrets are never returned in GET responses
- Assert `PUT` with partial `connection_details` only overwrites provided keys
- Assert correct HTTP status codes for 404/409/401 paths

---

### PR 4 — Scanner Abstraction + SFTP Scanner

**Files touched:** `connector/base_scanner.py`, `connector/scanner.py`, `connector/sftp_scanner.py`

**What's to build:**
- `BaseScanner` ABC with `connect()`, `scan()`, `download_to()`, `close()`
- `build_scanner()` factory — dispatches on `type`
- `SFTPScanner` — Paramiko connection (SFTP + SSH), recursive walk, extension filter, remote MD5 via `ssh.exec_command(f'md5sum "{remote_file_path}"')`, staged download

**How to test:**
- Unit test `SFTPScanner` against a local mock SFTP server (e.g. `pytest-sftpserver` or `paramiko.SFTPServer` in a thread)
- Assert extension filtering works correctly
- Assert MD5 is computed via remote `md5sum` exec (not by streaming bytes) and matches expected digest
- Assert `build_scanner("ssh", config)` returns an `SFTPScanner` instance

---

### PR 5 — S3 Scanner

**Files touched:** `connector/s3_scanner.py`

**What's to build:**
- `S3Scanner` — boto3 `list_objects_v2` paginator, extension filter, **checksum dedup pre-check** (skip objects whose checksum is already in `known_checksums`), `download_fileobj()`, staged download, checksum (S3 ETag) registered via `db_manager.upsert_file_checksum()` and stored in `documents.metadata.source_checksum`

**How to test:**
- Unit test with `moto` (mock AWS) — assert listing, filtering, hash computation, and download
- Assert `build_scanner("s3", config)` returns an `S3Scanner` instance
- Reuse same test interface as PR 4 to verify factory dispatch covers both types

---

### PR 6 — Sync Worker

**Files touched:** `connector/sync_worker.py`

**What's to build:**
- `ConnectorSyncWorker` with full `_run_tick()`: config refresh, scan, dedup diff, ingest new files, orphan deletion, tick finalize
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
