# Digitize — Data Source Connector Processing: Detailed Proposal

> **Scope:** This document focuses exclusively on the internals of the `digitize` service — what it does after it receives a connector push payload from catalog. Catalog-side concerns (key generation, connector CRUD, deployment wiring, TLS provisioning, TLS termination strategy, and API-level bearer-token enforcement details) are treated as a resolved black box and marked **[abstract – to be detailed separately]** where they appear.

---

## Table of Contents

1. [Assumptions & Preconditions](#1-assumptions--preconditions)
2. [System Overview Diagram](#2-system-overview-diagram)
3. [Catalog-to-Digitize API Reference](#3-catalog-to-digitize-api-reference) — A. POST · B. PUT · C. DELETE · D. GET (list) · E. GET (single) · **F. GET sync-history**
4. [Connector Runtime API](#4-connector-runtime-api)
5. [Database Schema](#5-database-schema) — 5.1 `active_connectors` · 5.2 `file_checksum_registry` & `connector_file_membership` · 5.3 SQLAlchemy ORM Models · **5.4 `connector_sync_history`**
6. [Database Operations Layer](#6-database-operations-layer)
7. [SFTP Scanner](#7-sftp-scanner)
8. [Sync Worker](#8-sync-worker) — 8.1 Per-tick Flow · 8.2 Tick Guard · 8.3 Staging Layout · 8.4 Error Handling Matrix · 8.5 Blocking Semantics of File Download & Ingest · 8.6 Sync Tick Retry — Exponential Backoff · **8.7 Sync History**
9. [Worker Manager](#9-worker-manager)
10. [Startup Recovery](#10-startup-recovery)
11. [Settings Changes](#11-settings-changes)
12. [File & Module Map](#12-file--module-map)
13. [Decision Log](#13-decision-log)

---

## 1. Assumptions & Preconditions

Catalog has already performed all of the following before any digitize endpoint is called:

- For **SSH/SFTP connectors:** Generated an Ed25519 key pair per connector, stored the AES-256-GCM encrypted private key in the catalog DB; validated remote SFTP connectivity; re-encrypted the private key under a per-connector DEK and wrapped that DEK under the pod's `digitize_KEK` — producing `private_key_ciphertext` and `encrypted_dek` — before sending the push payload.
- For **S3 connectors:** Validated that the supplied Access Key ID + Secret Access Key can successfully list objects in the target bucket and region; stored the AES-256-GCM encrypted secret access key in the catalog DB; produced `secret_access_key_ciphertext` and `encrypted_dek` before sending the push payload.
- Before pod start: provisioned `/run/secrets/connector_kek` and `/run/secrets/connector_api_token` as Podman secret mounts.
- **[abstract]** Bearer-token enforcement mechanism and TLS listener configuration are resolved at the infrastructure layer. The API assumes those are in place.

---

## 2. System Overview Diagram

```
Catalog (external)
  │
  │  POST   /v1/connectors        { connector_id, type, host,
  │                                  allowed_extensions, sync_interval_seconds,
  │                                  connection_details: { <type-specific fields> } }
  │  PUT    /v1/connectors/{id}   { host, allowed_extensions,
  │                                  sync_interval_seconds,
  │                                  connection_details: { <type-specific fields> } }
  │  DELETE /v1/connectors/{id}
  │  GET    /v1/connectors
  ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Digitize Pod                                                       │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │  Connector Runtime API    api/v1/connectors.py               │  │
│  │  [abstract] bearer-token middleware + TLS listener          │  │
│  └────────────────────────┬─────────────────────────────────────┘  │
│                            │  upsert / delete                       │
│  ┌─────────────────────────▼───────────────────────────────────┐  │
│  │  active_connectors table  (Postgres)                        │  │
│  │  connection_details JSONB  (type-specific encrypted fields)  │  │
│  │  + allowed_extensions + sync config                         │  │
│  └─────────────────────────┬───────────────────────────────────┘  │
│                            │  load on startup / on push            │
│  ┌─────────────────────────▼───────────────────────────────────┐  │
│  │  ConnectorWorkerManager   connector/worker_manager.py        │  │
│  │  { connector_id → (Thread, stop_event, running_flag) }      │  │
│  └─────────────────────────┬───────────────────────────────────┘  │
│                            │  one daemon thread per connector      │
│  ┌─────────────────────────▼───────────────────────────────────┐  │
│  │  ConnectorSyncWorker      connector/sync_worker.py           │  │
│  │                                                              │  │
│  │  [tick guard] if already running → skip tick                │  │
│  │                                                              │  │
│  │  ┌──────────────────────────────────────────────────────┐   │  │
│  │  │  Scanner  (type-dispatched per connector)            │   │  │
│  │  │  ssh_sftp → SFTPScanner  connector/sftp_scanner.py   │   │  │
│  │  │    KEK → DEK → privkey_pem  (in-memory, per tick)    │   │  │
│  │  │    paramiko + AutoAddPolicy                          │   │  │
│  │  │  s3      → S3Scanner     connector/s3_scanner.py     │   │  │
│  │  │    KEK → DEK → secret_access_key  (in-memory)        │   │  │
│  │  │    boto3 list_objects_v2 → list[RemoteFile]          │   │  │
│  │  │  → list[RemoteFile]  +  streaming SHA-256 per file   │   │  │
│  │  └──────────────────────────────────────────────────────┘   │  │
│  │                                                              │  │
│  │  ┌──────────────────────────────────────────────────────┐   │  │
│  │  │  Change Detector                                     │   │  │
│  │  │  diff scanned hashes vs connector_file_membership    │   │  │
│  │  │  → to_ingest, orphan_hashes                          │   │  │
│  │  └──────────────────────────────────────────────────────┘   │  │
│  │                                                              │  │
│  │  ┌──────────────────────────────────────────────────────┐   │  │
│  │  │  Staging & Batch Ingest (D-10: Option B)             │   │  │
│  │  │  sort diff list by last_modified DESC                │   │  │
│  │  │  copy diff files → staging/connector-<id>-<tick>/    │   │  │
│  │  │  batch into groups of 10 (most-recent-first)         │   │  │
│  │  │  per-batch: copy to tmp → one /v1/jobs call          │   │  │
│  │  │  job_name = "{connectorID}-{syncID}-{batchCount}"    │   │  │
│  │  └──────────────────────────────────────────────────────┘   │  │
│  │                                                              │  │
│  │  ┌──────────────────────────────────────────────────────┐   │  │
│  │  │  Delete Dispatcher                                   │   │  │
│  │  │  VDB delete per removed file (treat missing = ok)   │   │  │
│  │  │  delete_document() in DB + VDB                       │   │  │
│  │  └──────────────────────────────────────────────────────┘   │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │  file_checksum_registry  (sha256 PK, global)    (Postgres)   │  │
│  │  connector_file_membership  (connector_id + sha256 PK)       │  │
│  └──────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 3. Catalog-to-Digitize API Reference

This section documents every HTTP endpoint that catalog calls on the digitize pod. These are the only external-facing surfaces of the connector subsystem. All endpoints are bearer-token authenticated and served over TLS (both **[abstract — resolved at infrastructure layer]**).

### A. `POST /v1/connectors` — Register a new connector

**Purpose:** Called by catalog when a new data-source connector is created and fully validated. Digitize stores the encrypted credentials, provisions the `active_connectors` row, starts a sync worker thread for the connector, and **immediately triggers a manual sync tick** so that the first batch of documents is ingested without waiting for the first scheduled interval.

**When catalog calls it:** After credential validation and DEK/KEK wrapping are complete on the catalog side. This is the "activate" signal — the connector will begin syncing immediately after this call.

**Thread spawning on POST:** The handler registers a FastAPI `BackgroundTask` that calls `connector_worker_manager.start_worker(config)` *after* the 202 response is flushed to the client — the HTTP response is therefore never blocked by thread creation. Inside `start_worker()`, under `self._lock`, a `threading.Event` (the stop signal) is created, a `ConnectorSyncWorker` is constructed, a `daemon=True` `threading.Thread` is created with `target=worker.run`, and `thread.start()` is called. From that point the OS thread is live and the triple `(thread, worker, stop_event)` is registered in the `_workers` dict. There is **no dedicated spawner thread** — the Uvicorn process itself is the spawner, via the BackgroundTask mechanism.

**Manual sync on POST:** After `insert_active_connector` writes the row and `connector_worker_manager.start_worker()` starts the background thread, the handler enqueues an **immediate manual tick** by calling `worker.trigger_now()` (see §9.2). This sets a `threading.Event` on the worker so that the sleep at the bottom of the tick loop is interrupted and the next tick starts at once rather than waiting `sync_interval_seconds`.

**Request body (`application/json`):**

Fields common to **all** connector types are at the top level. All connector-specific credential and connection fields are nested inside `connection_details`.

**Example — `ssh_sftp`:**

```json
{
  "connector_id":          "c7f3a2d1-...",
  "type":                  "ssh_sftp",
  "host":                  "sftp.example.com",
  "allowed_extensions":    [".pdf", ".docx", ".xlsx"],
  "sync_interval_seconds": 300,
  "connection_details": {
    "port":                   22,
    "username":               "sync_user",
    "remote_path":            "/exports/reports",
    "private_key_ciphertext": "<base64-AES-256-GCM ciphertext of Ed25519 PEM>",
    "encrypted_dek":          "<base64-AES-256-GCM ciphertext of 32-byte DEK under pod KEK>"
  }
}
```

**Example — `s3`:**

```json
{
  "connector_id":          "a1b2c3d4-...",
  "type":                  "s3",
  "host":                  "s3.amazonaws.com",
  "allowed_extensions":    [".pdf", ".docx", ".xlsx"],
  "sync_interval_seconds": 300,
  "connection_details": {
    "region":                        "us-east-1",
    "bucket_name":                   "my-rag-documents",
    "access_key_id":                 "AKIAIOSFODNN7EXAMPLE",
    "secret_access_key_ciphertext":  "<base64-AES-256-GCM ciphertext of secret access key>",
    "encrypted_dek":                 "<base64-AES-256-GCM ciphertext of 32-byte DEK under pod KEK>"
  }
}
```

> **Note on S3 credentials:** Catalog should provision **read-only IAM keys** (i.e. a policy granting `s3:GetObject` and `s3:ListBucket` on the target bucket only). The `access_key_id` is stored in plaintext; `secret_access_key_ciphertext` is AES-256-GCM encrypted under the per-connector DEK, which is itself wrapped under the pod KEK — identical to the SFTP private key wrapping pattern.

**Top-level fields (all connector types):**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `connector_id` | `string (UUID)` | ✅ | Catalog's stable UUID for this connector. Used as the primary key in `active_connectors`. |
| `type` | `string` | ✅ | Connector type. One of `"ssh_sftp"` or `"s3"`. |
| `host` | `string` | ✅ | Primary server address. For SFTP: hostname or IP of the SFTP server. For S3: `"s3.amazonaws.com"` (or a custom endpoint URL for S3-compatible stores). |
| `allowed_extensions` | `array[string]` | ✅ | File extensions to include during scan (e.g. `[".pdf", ".docx"]`). Files not matching are ignored entirely — not downloaded, not checksummed. |
| `sync_interval_seconds` | `integer` | ✅ | Polling interval for the sync worker in seconds. |
| `connection_details` | `object` | ✅ | Connector-type-specific fields. Shape varies by `type` — see tables below. |

**`connection_details` — `ssh_sftp`:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `port` | `integer` | ✅ | SFTP port (typically `22`). |
| `username` | `string` | ✅ | SSH username for the SFTP session. |
| `remote_path` | `string` | ✅ | Absolute path on the remote server to recursively scan (e.g. `/var/www/documents/`). |
| `private_key_ciphertext` | `string (base64)` | ✅ | AES-256-GCM encrypted Ed25519 private key PEM. Encrypted under the per-connector DEK. |
| `encrypted_dek` | `string (base64)` | ✅ | AES-256-GCM encrypted 32-byte DEK. Encrypted under the pod's `digitize_KEK` secret mount. |

**`connection_details` — `s3`:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `region` | `string` | ✅ | AWS region where the bucket resides (e.g. `"us-east-1"`). |
| `bucket_name` | `string` | ✅ | Exact name of the S3 bucket containing the files (e.g. `"my-rag-documents"`). |
| `access_key_id` | `string` | ✅ | IAM Access Key ID used for API authentication. Stored in plaintext (not a secret). |
| `secret_access_key_ciphertext` | `string (base64)` | ✅ | AES-256-GCM encrypted IAM Secret Access Key. Encrypted under the per-connector DEK. |
| `encrypted_dek` | `string (base64)` | ✅ | AES-256-GCM encrypted 32-byte DEK. Encrypted under the pod's `digitize_KEK` secret mount. |

**Response:**

| Status | Body | Meaning |
| --- | --- | --- |
| `202 Accepted` | `{ "connector_id": "c7f3a2d1-..." }` | Connector row created; worker thread started asynchronously via `BackgroundTask`. |
| `409 Conflict` | Error detail | A connector with this `connector_id` already exists. Catalog should use `PUT` to update. |
| `401 Unauthorized` | Error detail | Missing or invalid bearer token. |

---

### B. `PUT /v1/connectors/{connector_id}` — Update an existing connector

**Purpose:** Called by catalog when the configuration of an existing connector changes — for example, credentials are rotated, the remote path or bucket changes, the sync interval is adjusted, or `allowed_extensions` is updated. Digitize stops the existing sync worker, applies the updated config via `upsert_active_connector`, restarts a fresh worker with the new settings, and **immediately triggers a manual sync tick** on the new worker so that any changes take effect at once.

**Thread stop/restart on PUT:** The handler calls `connector_worker_manager.stop_worker(connector_id, timeout=30.0)` on the old thread (cooperative stop via `stop_event.set()` + `thread.join()`), then calls `start_worker(new_config)` to spawn a fresh thread with the updated configuration. The new thread picks up the updated `sync_interval_seconds` from its own `_config` dict — there is no shared scheduler state to update. The interval change takes effect from the first tick of the new thread.

**Manual sync on PUT:** After the worker is restarted, the handler calls `worker.trigger_now()` on the newly started worker. This is especially important for credential rotations (the new credentials need to be tested) and for path/extension changes (the updated filter must be applied immediately).

**When catalog calls it:** After any connector-level update is saved on the catalog side and re-validated (e.g. new key pair generated, new IAM keys rotated, or settings edited by the user).

**Path parameter:**

| Parameter | Type | Description |
| --- | --- | --- |
| `connector_id` | `string (UUID)` | The ID of the connector to update. Must already exist in `active_connectors`. |

**Request body (`application/json`):**

All fields are optional — only the fields that changed need to be sent. Fields omitted from the payload are left unchanged in the database. The `type` field cannot be changed on an existing connector.

**Example — `ssh_sftp` (credential rotation + path change):**

```json
{
  "host":                  "sftp2.example.com",
  "allowed_extensions":    [".pdf", ".docx", ".csv"],
  "sync_interval_seconds": 600,
  "connection_details": {
    "port":                   2222,
    "username":               "new_user",
    "remote_path":            "/exports/v2/reports",
    "private_key_ciphertext": "<base64-AES-256-GCM ciphertext of new Ed25519 PEM>",
    "encrypted_dek":          "<base64-AES-256-GCM ciphertext of new 32-byte DEK under pod KEK>"
  }
}
```

**Example — `s3` (key rotation only):**

```json
{
  "connection_details": {
    "access_key_id":                "AKIANEWKEYEXAMPLE",
    "secret_access_key_ciphertext": "<base64-AES-256-GCM ciphertext of new secret access key>",
    "encrypted_dek":                "<base64-AES-256-GCM ciphertext of new 32-byte DEK>"
  }
}
```

**Top-level fields (all connector types):**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `host` | `string` | ❌ | New server address. |
| `allowed_extensions` | `array[string]` | ❌ | Replacement extension list (full replace, not merge). |
| `sync_interval_seconds` | `integer` | ❌ | New sync interval. Takes effect from the next tick. |
| `connection_details` | `object` | ❌ | Partial or full replacement of connector-type-specific fields. Only the keys present are written. |

**`connection_details` — `ssh_sftp` (all optional):**

| Field | Type | Description |
| --- | --- | --- |
| `port` | `integer` | New SFTP port. |
| `username` | `string` | New SSH username. |
| `remote_path` | `string` | New remote scan root (e.g. `/var/www/documents/`). |
| `private_key_ciphertext` | `string (base64)` | New encrypted private key (send only when credentials are rotated). |
| `encrypted_dek` | `string (base64)` | New encrypted DEK — required whenever `private_key_ciphertext` is updated. |

**`connection_details` — `s3` (all optional):**

| Field | Type | Description |
| --- | --- | --- |
| `region` | `string` | New AWS region. |
| `bucket_name` | `string` | New bucket name. |
| `access_key_id` | `string` | New IAM Access Key ID. |
| `secret_access_key_ciphertext` | `string (base64)` | New encrypted Secret Access Key (send only when credentials are rotated). |
| `encrypted_dek` | `string (base64)` | New encrypted DEK — required whenever `secret_access_key_ciphertext` is updated. |

> **Partial-update semantics:** The handler performs a targeted `UPDATE` — only top-level fields and `connection_details` keys present in the payload are written. For `connection_details`, the stored JSONB is merged at the key level (not replaced wholesale), so sending `{ "connection_details": { "region": "eu-west-1" } }` updates only the region without clearing the other S3 fields.

**Response:**

| Status | Body | Meaning |
| --- | --- | --- |
| `200 OK` | `{ "connector_id": "c7f3a2d1-..." }` | Config updated; existing worker stopped and restarted with new config. |
| `404 Not Found` | Error detail | No connector with this ID exists. Catalog should use `POST` to create it first. |
| `401 Unauthorized` | Error detail | Missing or invalid bearer token. |

---

### C. `DELETE /v1/connectors/{connector_id}` — Remove a connector

**Purpose:** Called by catalog when a connector is deleted or deactivated. Digitize executes a **full stop-sync sequence**: it signals the sync worker to stop, iterates every entry in the connector's checksum registry to delete the corresponding ingested documents, purges all registry rows, removes the `active_connectors` row, and cleans up any in-progress staging directories for that connector.

**When catalog calls it:** When a user deletes a connector in the catalog UI, or when a connector is permanently disabled. After this call the digitize pod will no longer sync that connector, and all state associated with it is removed — including the documents that were ingested from it.

**Path parameter:**

| Parameter | Type | Description |
| --- | --- | --- |
| `connector_id` | `string (UUID)` | The ID of the connector to remove. |

#### Stop Sync Sequence (DELETE handler — step by step)

The handler performs the following steps in order. Each step is logged independently so that partial failures are observable without repeating already-completed work.

```
DELETE /v1/connectors/{connector_id}

1. STOP WORKER
   connector_worker_manager.stop_worker(connector_id, timeout=30.0)
   ← signals stop_event; joins thread with 30 s timeout
   ← if thread is still alive after timeout → log warning and continue
      (thread will be reaped when the pod exits; stop proceeding with cleanup)

2. LOAD MEMBERSHIP SNAPSHOT
   known_hashes = list_connector_hashes(connector_id)
   ← returns set of sha256 values this connector holds
   ← read once, before any deletions, to avoid a partially-mutated cursor

3. DELETE INGESTED DOCUMENTS (per-hash loop, reference-counted)
   for sha256 in known_hashes:
       doc_id = lookup_content_by_sha256(sha256)
       remaining = delete_connector_membership_atomic(connector_id, sha256)
       ← deletes membership row + counts remaining refs, inside a transaction
       if remaining == 0 and doc_id is not None:
           response = DELETE /v1/documents/{doc_id}
           if response.status_code not in (200, 204, 404):
               log warning(f"Failed to delete doc {doc_id}: {response.status_code}")
               # 404 = already gone — treat as success; proceed
               # other errors → log + continue (best-effort)
           # ON DELETE CASCADE on doc_id removes the file_checksum_registry row automatically
           # when DELETE /v1/documents/{doc_id} deletes the documents row — no explicit
           # DELETE FROM file_checksum_registry is needed.
      # if remaining > 0: another connector still holds this content — leave doc intact

4. DELETE active_connectors ROW
  delete_active_connector(connector_id)
  ← ON DELETE CASCADE removes any remaining connector_file_membership rows
    for this connector automatically (guards against any rows missed in step 3)
  ← file_checksum_registry rows are removed automatically via the doc_id FK CASCADE
    when their documents row is deleted; shared content survives until the last
    connector releases it

6. CLEANUP STAGING DIRECTORIES (best-effort)
   for staging_dir in glob(f"{settings.staging_dir}/connector-{connector_id}-*/"):
       shutil.rmtree(staging_dir, ignore_errors=True)
   ← Removes any per-tick staging directories that were left behind by
     an in-progress or failed tick. Uses ignore_errors=True so a locked
     file does not abort the handler.
```

**Error handling policy for the document-deletion loop (step 3):**

| HTTP status from `DELETE /v1/documents/{doc_id}` | Action |
| --- | --- |
| `200 OK` / `204 No Content` | Success — proceed to next entry |
| `404 Not Found` | Document already absent — treat as success; proceed |
| `5xx` / network error | Log warning with `doc_id` and `remote_path`; continue loop (best-effort cleanup) |

Because step 5's `ON DELETE CASCADE` removes the registry unconditionally, a document-deletion failure means the document may remain in the VDB/storage but its registry row is gone. This is a known trade-off: a hard delete of the connector cannot be held hostage by a flaky document-deletion call. Operators can find and clean up orphaned documents via the `GET /v1/documents` listing filtered by `connector_id`.

**Response:**

| Status | Body | Meaning |
| --- | --- | --- |
| `204 No Content` | — | Connector stopped, all documents deleted, registry purged. |
| `404 Not Found` | Error detail | No connector with this ID exists. |
| `401 Unauthorized` | Error detail | Missing or invalid bearer token. |

---

### D. `GET /v1/connectors` — List active connectors

**Purpose:** Called by catalog to verify which connectors are currently active in the digitize pod, inspect their sync status and last sync timestamp, and detect stale connectors that need to be re-pushed after a pod restart.

**When catalog calls it:** On demand (e.g. health dashboard, connector status polling) and automatically after a pod restart event to reconcile which connectors need re-activating.

**Query parameters:** None.

**Response `200 OK` (`application/json`):**

Non-sensitive `connection_details` are returned so catalog can display configuration to the user. All credential fields (`private_key_ciphertext`, `encrypted_dek`, `secret_access_key_ciphertext`) are **never included** in the response.

**Example — `ssh_sftp`:**

```json
[
  {
    "connector_id":          "c7f3a2d1-...",
    "type":                  "ssh_sftp",
    "host":                  "sftp.example.com",
    "allowed_extensions":    [".pdf", ".docx"],
    "sync_interval_seconds": 300,
    "sync_status":           "success",
    "last_sync_at":          "2025-07-10T14:32:00Z",
    "last_sync_error":       null,
    "attached_at":           "2025-07-01T09:00:00Z",
    "connection_details": {
      "port":        22,
      "username":    "sync_user",
      "remote_path": "/exports/reports",
      "public_key":  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA..."
    }
  }
]
```

**Example — `s3`:**

```json
[
  {
    "connector_id":          "a1b2c3d4-...",
    "type":                  "s3",
    "host":                  "s3.amazonaws.com",
    "allowed_extensions":    [".pdf", ".docx"],
    "sync_interval_seconds": 300,
    "sync_status":           "failed",
    "last_sync_at":          "2025-07-10T14:32:00Z",
    "last_sync_error":       "Connection timed out while listing bucket objects.",
    "attached_at":           "2025-07-01T09:00:00Z",
    "connection_details": {
      "region":         "us-east-1",
      "bucket_name":    "my-rag-documents",
      "access_key_id":  "AKIAIOSFODNN7EXAMPLE"
    }
  }
]
```

> **Security:** All credential fields (`private_key_ciphertext`, `encrypted_dek`, `secret_access_key_ciphertext`) are **never included** in the list response. For SSH connectors the **public key** is included — it is not a secret and catalog needs to display it to the user so they can authorise the key on the remote server. For S3 connectors the `access_key_id` is included (not a secret) but `secret_access_key_ciphertext` is withheld.

**Top-level fields:**

| Field | Type | Description |
| --- | --- | --- |
| `connector_id` | `string` | The connector's UUID. |
| `type` | `string` | Connector type: `"ssh_sftp"` or `"s3"`. |
| `host` | `string` | Primary server address. |
| `allowed_extensions` | `array[string]` | Active extension filter. |
| `sync_interval_seconds` | `integer` | Current sync interval. |
| `sync_status` | `string` | One of `"idle"`, `"running"`, `"success"`, `"failed"`. `"idle"` means no tick has run yet; `"running"` means a tick is currently in progress; `"success"` means the last completed tick finished without errors; `"failed"` means the last tick encountered a fatal error. |
| `last_sync_at` | `string (ISO 8601)` \| `null` | Timestamp of the last completed sync tick (success or failure). `null` if no tick has run yet. |
| `last_sync_error` | `string` \| `null` | Human-readable error message from the last failed sync tick. `null` when `sync_status` is not `"failed"`. |
| `attached_at` | `string (ISO 8601)` | Timestamp when the connector was first registered with this pod. |
| `connection_details` | `object` | Non-sensitive, type-specific connection info (see below). |

**`connection_details` in response — `ssh_sftp`:**

| Field | Type | Description |
| --- | --- | --- |
| `port` | `integer` | SFTP port. |
| `username` | `string` | SSH username. |
| `remote_path` | `string` | Remote scan root path. |
| `public_key` | `string` | The Ed25519 public key in OpenSSH format. Displayed to the user so they can authorise it on the remote server. |

**`connection_details` in response — `s3`:**

| Field | Type | Description |
| --- | --- | --- |
| `region` | `string` | AWS region of the bucket. |
| `bucket_name` | `string` | Name of the S3 bucket. |
| `access_key_id` | `string` | IAM Access Key ID (not a secret). |

---

### E. `GET /v1/connectors/{connector_id}` — Get a single connector

**Purpose:** Fetch the full current state of one connector by its UUID, including file-processing statistics from the most recent sync tick. Catalog uses this for per-connector detail views and to poll sync status after triggering a manual sync.

**When catalog calls it:** On demand — connector detail page load, post-sync status refresh, or targeted health checks.

**Path parameters:**

| Parameter | Type | Description |
| --- | --- | --- |
| `connector_id` | `string (UUID)` | The connector to retrieve. |

**Response `200 OK` (`application/json`):**

The response includes the connector's current sync state, flat file-statistic fields for the most recently completed tick, and a `connection_details` object with non-secret connection fields. All credential fields (`private_key_ciphertext`, `secret_access_key_ciphertext`, `encrypted_dek`) are **always withheld** from the response.

**Response field definitions:**

| Field | Type | Description |
| --- | --- | --- |
| `sync_status` | `string` | Current sync state: `"Syncing"`, `"Completed"`, or `"Failed"`. |
| `files_found` | `integer` | Total number of files discovered on the remote source during the last scan. |
| `files_syncing` | `integer` | Files currently being downloaded or staged this tick. |
| `files_completed` | `integer` | Files successfully processed and ingested. |
| `files_failed` | `integer` | Files that could not be processed due to a download or staging error. |
| `connection_details` | `object` | Non-secret connection fields for this connector. Shape varies by `type` — see tables below. |

**`connection_details` response fields — `ssh_sftp`:**

| Field | Type | Description |
| --- | --- | --- |
| `port` | `integer` | SFTP port. |
| `username` | `string` | SSH username. |
| `remote_path` | `string` | Absolute path on the remote server being scanned. |

**`connection_details` response fields — `s3`:**

| Field | Type | Description |
| --- | --- | --- |
| `region` | `string` | AWS region where the bucket resides. |
| `bucket_name` | `string` | Name of the S3 bucket being scanned. |
| `access_key_id` | `string` | IAM Access Key ID (not a secret — stored and returned in plaintext). |

**Example — `ssh_sftp` (last tick succeeded):**

```json
{
  "connector_id":    "c7f3a2d1-...",
  "type":            "ssh_sftp",
  "host":            "sftp.example.com",
  "sync_status":     "Completed",
  "last_sync_at":    "2025-07-10T14:32:00Z",
  "last_sync_error": null,
  "attached_at":     "2025-07-01T09:00:00Z",
  "files_found":     42,
  "files_syncing":    0,
  "files_completed": 40,
  "files_failed":     0,
  "connection_details": {
    "port":        22,
    "username":    "sync_user",
    "remote_path": "/exports/reports"
  }
}
```

**Example — `s3` (last tick failed):**

```json
{
  "connector_id":    "a1b2c3d4-...",
  "type":            "s3",
  "host":            "s3.amazonaws.com",
  "sync_status":     "Failed",
  "last_sync_at":    "2025-07-10T14:32:00Z",
  "last_sync_error": "Connection timed out while listing bucket objects.",
  "attached_at":     "2025-07-01T09:00:00Z",
  "files_found":      0,
  "files_syncing":    0,
  "files_completed":  0,
  "files_failed":     0,
  "connection_details": {
    "region":        "us-east-1",
    "bucket_name":   "my-rag-documents",
    "access_key_id": "AKIAIOSFODNN7EXAMPLE"
  }
}
```

> **Note:** When `sync_status` is `"Failed"` and the scan itself did not complete, all file counters are `0`. Encrypted fields (`private_key_ciphertext`, `secret_access_key_ciphertext`, `encrypted_dek`) are never returned.

**Error responses:**

| Status | Condition |
| --- | --- |
| `404 Not Found` | No connector with `connector_id` is currently active in this pod. |

---

### F. `GET /v1/connectors/{connector_id}/sync-history` — Retrieve sync tick history

**Purpose:** Returns the chronological history of every completed and in-progress sync tick for a connector. Catalog uses this to render a per-connector activity timeline, debug recurring failures, and inspect file-count progression over time.

**When catalog calls it:** On demand — connector detail page load, post-sync status refresh, or a dedicated history / audit view.

**Path parameters:**

| Parameter | Type | Description |
| --- | --- | --- |
| `connector_id` | `string (UUID)` | The connector whose sync history to retrieve. |

**Query parameters:**

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `limit` | `integer` | `50` | Maximum number of history rows to return, newest first. Capped at `200`. |
| `offset` | `integer` | `0` | Pagination offset (zero-based). |

**Response `200 OK` (`application/json`):**

```json
{
  "connector_id": "c7f3a2d1-...",
  "total":        7,
  "items": [
    {
      "sync_id":      7,
      "started_at":   "2025-07-10T15:00:00Z",
      "finished_at":  "2025-07-10T15:00:42Z",
      "files_found":  44,
      "files_syncing": 0,
      "files_completed": 42,
      "files_failed":  2,
      "sync_status":  "partial_error"
    },
    {
      "sync_id":      6,
      "started_at":   "2025-07-10T14:55:00Z",
      "finished_at":  "2025-07-10T14:55:38Z",
      "files_found":  42,
      "files_syncing": 0,
      "files_completed": 42,
      "files_failed":  0,
      "sync_status":  "completed"
    },
    {
      "sync_id":      5,
      "started_at":   "2025-07-10T14:50:00Z",
      "finished_at":  null,
      "files_found":  42,
      "files_syncing": 3,
      "files_completed": 0,
      "files_failed":  0,
      "sync_status":  "syncing"
    }
  ]
}
```

**Response field definitions — top level:**

| Field | Type | Description |
| --- | --- | --- |
| `connector_id` | `string` | Echoes the path parameter. |
| `total` | `integer` | Total number of history rows for this connector across all pages. |
| `items` | `array` | Ordered list of sync tick records, **newest first**. |

**Response field definitions — each item in `items`:**

| Field | Type | Description |
| --- | --- | --- |
| `sync_id` | `integer` | Auto-incrementing monotonic counter scoped to this connector. First tick = `1`; each subsequent tick increments by `1`. Never reused; never reset on pod restart. |
| `started_at` | `string (ISO 8601)` | UTC timestamp recorded the instant the tick body begins executing (before scan). |
| `finished_at` | `string (ISO 8601)` \| `null` | UTC timestamp recorded the instant the tick body finishes (success, partial error, or fatal error). `null` when a tick is currently in progress (`sync_status = "syncing"`). |
| `files_found` | `integer` | Total files discovered on the remote source during this tick's scan phase. `0` if the scan did not complete. |
| `files_syncing` | `integer` | Number of files currently being downloaded or staged. Non-zero only while `sync_status = "syncing"`. |
| `files_completed` | `integer` | Number of files successfully ingested (or de-duplicated) during this tick. |
| `files_failed` | `integer` | Number of files that could not be processed due to download, staging, or ingest error during this tick. |
| `sync_status` | `string` | One of `"syncing"`, `"completed"`, `"partial_error"`, `"failed"`. See status semantics below. |

**`sync_status` value semantics:**

| Value | Meaning |
| --- | --- |
| `"syncing"` | Tick is currently in progress. `finished_at` is `null`; file counters reflect live progress. |
| `"completed"` | Tick finished with zero file errors and no fatal exception. |
| `"partial_error"` | Tick finished but one or more individual files failed to download, stage, or ingest. `files_failed > 0`. |
| `"failed"` | The entire tick failed with a fatal exception (e.g. SFTP connection refused, S3 credential error) after all retries were exhausted. File counters may all be `0`. |

> **Note on in-progress rows:** A `"syncing"` row represents the currently running tick. There is at most one such row per connector at any time (enforced by the tick guard). If the pod is killed mid-tick the row remains with `sync_status = "syncing"` and `finished_at = null` indefinitely — this is the observable evidence of an interrupted tick. Startup recovery does **not** retroactively update orphaned `"syncing"` rows; callers should treat a `"syncing"` row older than a reasonable threshold (e.g. `> 2 × sync_interval_seconds`) as likely stale.

**Error responses:**

| Status | Condition |
| --- | --- |
| `404 Not Found` | No connector with `connector_id` is currently active in this pod. |

---

## 4. Connector Runtime API

> ⚠️ **TO BE DECIDED** — The design of the Connector Runtime API (endpoint behaviour, request/response contracts, authentication enforcement, TLS listener configuration, and router registration) has not yet been finalised. This entire phase is pending a dedicated design session before implementation begins.

---

## 5. Database Schema

**Modified file:** `services/digitize/db/scripts/init_schema.sql`

Three new tables are added following the existing `IF NOT EXISTS` / idempotent DDL pattern already used in [`init_schema.sql`](../../services/digitize/db/scripts/init_schema.sql). All three tables also need corresponding SQLAlchemy ORM model classes added to [`db/models.py`](../../services/digitize/db/models.py), matching the `Job` / `Document` pattern there, so `Base.metadata.create_all()` on startup creates them automatically.

### 5.1 `active_connectors`

> **Security note:** There are **no plaintext secret columns**. All credential material (`private_key_ciphertext`, `secret_access_key_ciphertext`, `encrypted_dek`) is stored as AES-256-GCM ciphertext inside `connection_details`. A Postgres breach alone exposes nothing without the KEK.

The `connection_details` JSONB column stores all connector-type-specific fields — both non-sensitive config and encrypted credential blobs. The exact keys differ by `type`:

| `type` | Keys stored in `connection_details` |
| --- | --- |
| `ssh_sftp` | `port`, `username`, `remote_path`, `private_key_ciphertext`, `encrypted_dek` |
| `s3` | `region`, `bucket_name`, `access_key_id`, `secret_access_key_ciphertext`, `encrypted_dek` |

```sql
CREATE TABLE IF NOT EXISTS active_connectors (
    id                      TEXT        PRIMARY KEY,       -- catalog connector UUID
    type                    TEXT        NOT NULL,          -- "ssh_sftp" | "s3"
    host                    TEXT        NOT NULL,          -- SFTP host or S3 endpoint
    connection_details      JSONB       NOT NULL DEFAULT '{}',  -- type-specific fields (see above)
    allowed_extensions      JSONB       NOT NULL DEFAULT '[]',  -- e.g. [".pdf", ".docx"]
    sync_interval_seconds   INTEGER     NOT NULL DEFAULT 300,
    attached_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_sync_at            TIMESTAMPTZ,
    sync_status             TEXT        NOT NULL DEFAULT 'idle',
    CONSTRAINT chk_connector_type   CHECK (type IN ('ssh_sftp', 's3')),
    CONSTRAINT chk_sync_status      CHECK (sync_status IN ('idle', 'running', 'partial_error'))
);
```

### 5.2 `file_checksum_registry` and `connector_file_membership`

The original single `connector_file_checksums` table has been replaced by two purpose-built tables. This separation is required to support both sync change-detection and global content de-duplication (Decision D-16):

- **`file_checksum_registry`** — a global, content-addressed store keyed on `sha256`. There is exactly one row per unique piece of content, regardless of which connector or how many connectors have seen it. It is the authoritative source for "has this content been ingested, and what is its `doc_id`?".
- **`connector_file_membership`** — a per-connector membership table. Each row records that a specific connector currently holds a file with a given `sha256`. This is the only table scoped to a connector, and it is the source for orphan detection ("which hashes did this connector see last tick?").

#### 5.2.1 `file_checksum_registry`

```sql
CREATE TABLE IF NOT EXISTS file_checksum_registry (
    sha256        TEXT        PRIMARY KEY,
    doc_id        TEXT        NOT NULL UNIQUE REFERENCES documents(doc_id) ON DELETE CASCADE
);
```

`sha256` is the sole natural key — content identity is independent of source. `doc_id` is a FK to `documents(doc_id)` with `ON DELETE CASCADE`: if the document row is deleted (e.g. via `DELETE /v1/documents/{doc_id}`), the registry entry is automatically removed. The `UNIQUE` constraint prevents two registry entries from pointing at the same document.

#### 5.2.2 `connector_file_membership`

```sql
CREATE TABLE IF NOT EXISTS connector_file_membership (
    connector_id  TEXT  NOT NULL,
    sha256        TEXT  NOT NULL,
    PRIMARY KEY (connector_id, sha256),
    FOREIGN KEY (connector_id)
        REFERENCES active_connectors(id) ON DELETE CASCADE,
    FOREIGN KEY (sha256)
        REFERENCES file_checksum_registry(sha256)
);
```

`ON DELETE CASCADE` on `connector_id` means that when a connector is removed from `active_connectors`, all its membership rows are automatically dropped in a single SQL statement. Crucially, this cascade does **not** touch `file_checksum_registry` — the content and its `doc_id` survive as long as any other connector (or membership row) still references that hash.

#### 5.2.3 Cross-connector de-duplication contract

When a new file is scanned by any connector, the sync worker checks `file_checksum_registry` for the computed `sha256` **before** downloading or ingesting:

- **Hash already in `file_checksum_registry`** — content has been ingested before (by any connector). Skip the download and ingest entirely. Insert a `connector_file_membership` row pointing to the existing `doc_id`. Zero bytes transferred, zero ingest pipeline cost.
- **Hash not in `file_checksum_registry`** — first time this content has been seen globally. Download, ingest, insert into `file_checksum_registry`, then insert into `connector_file_membership`.

This means `doc_id` is shared across connectors when the content is identical. A document is physically stored once and referenced by multiple connectors through their membership rows.

#### 5.2.4 Orphan detection and reference-counted deletion

Because a `doc_id` may be shared, the delete path must be reference-counted. The orphan detection logic runs **inside a transaction** to avoid a race condition where two connectors simultaneously attempt to delete the last reference to the same hash (Decision D-17):

```sql
-- Step 1 (inside a transaction): remove this connector's membership row.
DELETE FROM connector_file_membership
 WHERE connector_id = $1 AND sha256 = $2;

-- Step 2 (same transaction): count remaining references after our row is gone.
SELECT COUNT(*) FROM connector_file_membership WHERE sha256 = $2;

-- Application logic after commit:
-- if count == 0 → no other connector holds this content:
--     DELETE /v1/documents/{doc_id}            (HTTP — VDB + files + DB record)
--     ↳ ON DELETE CASCADE on doc_id automatically removes the file_checksum_registry row.
--       No explicit DELETE FROM file_checksum_registry is required.
-- if count > 0  → another connector still holds it; stop here.
```

Deleting the membership row *before* checking the count, all within a single transaction, ensures no two concurrent ticks can both observe `count = 0` for the same hash. The last connector to drop a file always performs the actual document delete; all earlier removals are no-ops for the document itself. Because `doc_id` carries `ON DELETE CASCADE`, the `DELETE /v1/documents/{doc_id}` HTTP call implicitly removes the registry row — the application does not need a separate `DELETE FROM file_checksum_registry` step.

#### 5.2.5 Lifecycle summary

| Event | `connector_file_membership` | `file_checksum_registry` | Document |
| --- | --- | --- | --- |
| New file, hash not seen globally | Insert `(connector_id, sha256)` | Insert `(sha256, doc_id)` | Ingested |
| New file, hash already exists globally | Insert `(connector_id, sha256)` | No change | Reused — no re-ingest |
| File disappears, other connectors still hold it | Remove this connector's row (txn) | No change | Survives |
| File disappears, last connector to hold it | Remove row (txn), count = 0 | Delete row | Deleted |
| Connector deleted via `DELETE /v1/connectors` | CASCADE removes all membership rows | No change | Survives if any other connector holds the hash |

### 5.3 SQLAlchemy ORM Models

Add `ActiveConnector`, `FileChecksumRegistry`, `ConnectorFileMembership`, and `ConnectorSyncHistory` classes to `db/models.py` following the `Job` / `Document` pattern — `DeclarativeBase`, `Mapped` columns, `ForeignKey`, `CheckConstraint`, and `Index`. `FileChecksumRegistry.doc_id` maps to `ForeignKey("documents.doc_id", ondelete="CASCADE")` to mirror the DDL FK. This ensures `Base.metadata.create_all(bind=engine)` in the startup lifespan creates these tables alongside the existing `jobs` and `documents` tables.

---

### 5.4 `connector_sync_history`

This table records one row per sync tick execution for each connector. It is the persistent store backing the `GET /v1/connectors/{connector_id}/sync-history` endpoint (§3F) and gives a full audit trail of when each tick ran, how long it took, how many files were found/processed/failed, and what the final outcome was.

**Design principles:**

- **Append-only.** Rows are only ever inserted (at tick start) and updated (at tick end). They are never deleted by application logic. History is retained for the full lifetime of the connector.
- **Scoped `sync_id`.** The `sync_id` counter is per-connector and starts at `1`. It is derived by `MAX(sync_id) + 1` inside an `INSERT` (or a `SEQUENCE` per connector — see DDL note). It never resets on pod restart, providing a stable monotonic identity for each tick.
- **Cascade delete.** When a connector is removed via `DELETE /v1/connectors/{id}`, all history rows for that connector are deleted automatically via `ON DELETE CASCADE`. This keeps the table clean without application-side cleanup logic.

```sql
CREATE TABLE IF NOT EXISTS connector_sync_history (
    id               BIGSERIAL   PRIMARY KEY,           -- global surrogate PK, auto-increment
    connector_id     TEXT        NOT NULL,
    sync_id          INTEGER     NOT NULL,              -- per-connector monotonic counter (1-based)
    started_at       TIMESTAMPTZ NOT NULL,              -- set when tick body begins (step 0 below)
    finished_at      TIMESTAMPTZ,                       -- set when tick body ends; NULL while in progress
    files_found      INTEGER     NOT NULL DEFAULT 0,
    files_syncing    INTEGER     NOT NULL DEFAULT 0,
    files_completed  INTEGER     NOT NULL DEFAULT 0,
    files_failed     INTEGER     NOT NULL DEFAULT 0,
    sync_status      TEXT        NOT NULL DEFAULT 'syncing',
    CONSTRAINT fk_csh_connector
        FOREIGN KEY (connector_id)
        REFERENCES active_connectors(id) ON DELETE CASCADE,
    CONSTRAINT chk_csh_status
        CHECK (sync_status IN ('syncing', 'completed', 'partial_error', 'failed')),
    CONSTRAINT uq_csh_connector_sync
        UNIQUE (connector_id, sync_id)
);

CREATE INDEX IF NOT EXISTS idx_csh_connector_started
    ON connector_sync_history (connector_id, started_at DESC);
```

**Column-by-column notes:**

| Column | Notes |
| --- | --- |
| `id` | Global surrogate PK. `BIGSERIAL` to avoid overflow on very long-lived deployments with many connectors and short sync intervals. |
| `connector_id` | FK to `active_connectors(id)`. `ON DELETE CASCADE` removes all history when the connector is deleted. |
| `sync_id` | Per-connector counter. Computed at insert time as `COALESCE(MAX(sync_id), 0) + 1` in a subquery `SELECT … FROM connector_sync_history WHERE connector_id = $1`. The `UNIQUE (connector_id, sync_id)` constraint prevents duplicates if two ticks somehow race to insert (should not happen given the tick guard, but the DB constraint is a safe belt-and-suspenders). |
| `started_at` | Written once at tick start. Never updated. |
| `finished_at` | `NULL` during the tick; set to `NOW()` in the UPDATE call at tick end. |
| `files_found` | Written in the UPDATE call after the scan phase completes. `0` if the tick failed before the scan finished. |
| `files_syncing` | Live counter; updated incrementally during the download/staging phase while `sync_status = 'syncing'`. Set to `0` in the final UPDATE when the tick ends. |
| `files_completed` | Written in the final UPDATE at tick end. |
| `files_failed` | Written in the final UPDATE at tick end. |
| `sync_status` | Starts as `'syncing'` on INSERT. Updated to `'completed'`, `'partial_error'`, or `'failed'` in the final UPDATE at tick end. |

**Index `idx_csh_connector_started`:** Supports the history query (`ORDER BY started_at DESC` + `WHERE connector_id = $1`) efficiently without a full-table scan. Composite index on `(connector_id, started_at DESC)` covers both the filter and the sort.

---

## 6. Database Operations Layer

**Modified file:** `services/digitize/utils/db.py`

New CRUD functions appended to the existing module. These functions operate on **ciphertext blobs only** — they never decrypt or re-encrypt. Decryption is the exclusive responsibility of the scanner (`SFTPScanner` or `S3Scanner`) at sync time.

The `connection_details` JSONB column is written and read as a plain Python `dict`. For `PUT` partial updates the handler merges the incoming `connection_details` keys into the existing stored dict (key-level merge, not full replacement) so that a credential rotation does not inadvertently clear unrelated fields such as `port` or `region`.

| Function | Table | Description |
| --- | --- | --- |
| `insert_active_connector(config: dict)` | `active_connectors` | `INSERT` — strict create. Raises `409 Conflict` if a row with the same `id` already exists. Called exclusively by the `POST` handler. `connection_details` is stored as-is from `config["connection_details"]`. |
| `upsert_active_connector(config: dict)` | `active_connectors` | `INSERT … ON CONFLICT (id) DO UPDATE` — idempotent overwrite. Called by the `PUT` handler and by startup recovery (§10). `connection_details` is stored as-is from `config["connection_details"]`. |
| `get_active_connector(connector_id: str) → dict \| None` | `active_connectors` | Returns full row including `connection_details` with all ciphertext fields (used by scanner at sync time) |
| `list_active_connectors() → list[dict]` | `active_connectors` | Returns all rows — used at startup for connector recovery |
| `delete_active_connector(connector_id: str)` | `active_connectors` + cascade | Deletes connector row; FK cascade removes all `connector_file_membership` rows for this connector automatically |
| `update_connector_sync_status(connector_id: str, status: str, last_sync_at: datetime)` | `active_connectors` | Called at end of each sync tick |
| `merge_connection_details(connector_id: str, partial: dict)` | `active_connectors` | Key-level merge of `partial` into the existing `connection_details` JSONB — used by the `PUT` handler to avoid overwriting untouched fields |
| `lookup_content_by_sha256(sha256: str) → str \| None` | `file_checksum_registry` | Returns `doc_id` if this content hash has been ingested before (by any connector) — the dedup check, called before download |
| `insert_checksum_registry(sha256: str, doc_id: str)` | `file_checksum_registry` | Called once per unique content after a successful ingest |
| `delete_checksum_registry(sha256: str)` | `file_checksum_registry` | Explicit fallback delete of the registry row by `sha256`. In the normal path this row is removed automatically via `ON DELETE CASCADE` on `doc_id` when the document is deleted; this function is retained for defensive cleanup (e.g. if the document was already absent) |
| `list_connector_hashes(connector_id: str) → set[str]` | `connector_file_membership` | Returns the set of `sha256` values this connector currently holds — used at the start of each tick for orphan/diff computation |
| `insert_connector_membership(connector_id: str, sha256: str)` | `connector_file_membership` | Called after a file is confirmed ingested (or deduped) for this connector |
| `delete_connector_membership_atomic(connector_id: str, sha256: str) → int` | `connector_file_membership` | Deletes the membership row inside a transaction and returns the remaining reference count for that `sha256`. Caller deletes the document and registry row if count == 0. |
| `insert_sync_history(connector_id: str, started_at: datetime) → int` | `connector_sync_history` | Inserts a new history row with `sync_status = 'syncing'`, `started_at = started_at`, and `sync_id = COALESCE(MAX(sync_id), 0) + 1` for this connector. Returns the generated `sync_id`. |
| `update_sync_history(connector_id: str, sync_id: int, finished_at: datetime, files_found: int, files_syncing: int, files_completed: int, files_failed: int, sync_status: str)` | `connector_sync_history` | Final UPDATE call at tick end. Sets `finished_at`, all file counters, and `sync_status`. `files_syncing` is always written as `0` from this call (tick is done). |
| `update_sync_history_files_syncing(connector_id: str, sync_id: int, files_syncing: int)` | `connector_sync_history` | Incremental live update of the `files_syncing` counter during the download/staging phase. Called once per file as it enters the staging queue. Lightweight single-column UPDATE. |
| `list_sync_history(connector_id: str, limit: int, offset: int) → tuple[list[dict], int]` | `connector_sync_history` | Returns `(rows, total_count)`. Rows are ordered by `started_at DESC`. Each dict maps directly to the API response item shape. `total_count` is the `COUNT(*)` for pagination. |

> **Note on `documents` table extension:** The `documents` table no longer needs a `connector_id` column — connector membership is tracked exclusively through `connector_file_membership`. Document cleanup on connector delete is handled by the per-connector membership snapshot loop (§3 DELETE handler) followed by `ON DELETE CASCADE` removing membership rows automatically.

---

## 7. SFTP Scanner

**New file:** `services/digitize/connector/sftp_scanner.py`

Handles all in-memory cryptographic operations and the paramiko SFTP session for `ssh_sftp` connectors. This is one of two components in digitize that reads `/run/secrets/connector_kek` (the other being `S3Scanner`).

### 7.1 Decryption Chain (per tick, not cached)

All credential fields are read from `connection_details` (the JSONB column), not from top-level columns.

```
/run/secrets/connector_kek   (32-byte KEK, Podman secret mount)
     │
     │  AES-256-GCM decrypt(connection_details["encrypted_dek"], KEK)
     ▼
DEK  (32 bytes, in-memory, not retained between ticks)
     │
     │  AES-256-GCM decrypt(connection_details["private_key_ciphertext"], DEK)
     ▼
privkey_pem  (Ed25519 PEM string, in-memory only)
     │
     │  paramiko.Ed25519Key.from_private_key(io.StringIO(privkey_pem))
     │  privkey_pem string overwritten immediately after key load
     ▼
paramiko SSHClient  →  open SFTP session
     │
     ▼  close() — DEK not retained
```

**Key zeroization:** After calling `from_private_key()`, the `privkey_pem` variable is overwritten with `"\x00" * len(privkey_pem)` and set to `None`. The DEK is not retained between ticks — re-derived fresh on every tick to minimise in-memory exposure.

**KEK load timing (Decision D-6 — recommended):** The KEK is read fresh inside `SFTPScanner.connect()` on every tick. This keeps KEK residence time in memory minimal and allows future KEK rotation without a pod restart.

### 7.2 File Scanning — Streaming SHA-256, Filtered by Extension

File type filtering is applied at scan time using `allowed_extensions` from the connector config. This avoids SFTP transfers for files the ingest pipeline would reject. The extension list is populated by catalog (Decision D-8).

`scan()` performs four phases:

1. **Walk** — recursively enumerate every regular file under `remote_path`.
2. **Filter + Hash** — skip files whose extension is not in `allowed_extensions`; compute a streaming SHA-256 for every file that passes the filter. File identity is purely content-based — `remote_path` is used only as a walk cursor and is never stored.
3. **Membership diff** — compare each computed hash against `known_hashes` (the set of `sha256` values this connector held at the start of the tick, from `list_connector_hashes`):
   - **Hash in `known_hashes`** → file is unchanged; skip it entirely.
   - **Hash not in `known_hashes`** → file is new to this connector; add it to `to_ingest`.
4. **Orphan detection** — any hash in `known_hashes` that was *not* seen in the walk (the file was deleted or renamed on the remote) is added to `orphan_hashes`. These are handled by the sync worker after the scan returns (§8.1 step 6).
5. Return `(to_ingest, orphan_hashes)`.

```python
@dataclass
class RemoteFile:
    path: str
    size: int
    checksum: str        # hex SHA-256
    last_modified: float # POSIX timestamp from st_mtime; used to sort the diff list

def scan(self, known_hashes: set[str]) -> tuple[list[RemoteFile], set[str]]:
    """
    Recursively walk remote_path; filter by allowed_extensions; compute streaming SHA-256.

    Args:
        known_hashes: set of sha256 values from list_connector_hashes(connector_id) —
                      the hashes this connector held at the start of this tick.

    Returns:
        to_ingest:    files whose hash is new to this connector (new or content-changed),
                      unsorted — caller sorts by last_modified DESC before batching.
        orphan_hashes: hashes that were in known_hashes but not seen during this walk
                       (file deleted or renamed on the remote).
    """
    seen_hashes: set[str] = set()
    to_ingest: list[RemoteFile] = []
    self._walk(self.remote_path, known_hashes, seen_hashes, to_ingest)

    orphan_hashes = known_hashes - seen_hashes
    return to_ingest, orphan_hashes

def _walk(
    self,
    path: str,
    known_hashes: set[str],
    seen_hashes: set[str],
    to_ingest: list[RemoteFile],
) -> None:
    for entry in self._sftp.listdir_attr(path):
        full_path = f"{path}/{entry.filename}"
        if stat.S_ISDIR(entry.st_mode):
            self._walk(full_path, known_hashes, seen_hashes, to_ingest)  # recursive DFS (Decision D-7 — recommended)
        elif stat.S_ISREG(entry.st_mode):
            ext = Path(entry.filename).suffix.lower()
            if ext not in self._allowed_extensions:
                continue

            # Compute streaming SHA-256.
            sha = hashlib.sha256()
            with self._sftp.open(full_path, "rb") as fh:
                for block in iter(lambda: fh.read(65_536), b""):
                    sha.update(block)
            checksum = sha.hexdigest()

            seen_hashes.add(checksum)

            # Skip if this connector already holds this hash — file is unchanged.
            if checksum in known_hashes:
                continue

            to_ingest.append(RemoteFile(
                path=full_path,
                size=entry.st_size,
                checksum=checksum,
                last_modified=float(entry.st_mtime or 0),
                # st_mtime is always set for regular SFTP files; guard with 0 for
                # edge-case servers that omit it to keep RemoteFile sortable.
            ))
```

**Caller responsibility:** Before invoking `scan()`, the sync worker fetches the connector's current membership set:

```python
known_hashes = list_connector_hashes(connector_id)   # single DB round-trip
to_ingest, orphan_hashes = scanner.scan(known_hashes)
```

This keeps the scanner free of direct DB imports. The `digitize_base_url` (e.g. `http://localhost:8000`) is injected into `SFTPScanner.__init__` from settings so that the worker can reach the documents endpoint for orphan cleanup.

### 7.4 File Download for Staging

```python
def download_to(self, remote_path: str, local_path: Path) -> None:
    """Stream a single file from SFTP to a local staging path (no full-file buffer)."""
    local_path.parent.mkdir(parents=True, exist_ok=True)
    with self._sftp.open(remote_path, "rb") as src, open(local_path, "wb") as dst:
        for block in iter(lambda: src.read(65_536), b""):
            dst.write(block)
```

---

## 8. Sync Worker

**New file:** `services/digitize/connector/sync_worker.py`

A `threading.Thread` per connector. Each worker is fully isolated — one connector's failure never affects another's loop.

### 8.1 Per-tick Flow

```
ConnectorSyncWorker.run() — main loop

while not stop_event.is_set():

  ┌─── TICK GUARD ──────────────────────────────────────────────────────────┐
  │  if self._tick_running:                                                 │
  │      logger.info(f"Connector {id}: previous tick still running, skip") │
  │      stop_event.wait(sync_interval_seconds)                             │
  │      continue                                                           │
  └─────────────────────────────────────────────────────────────────────────┘

  self._tick_running = True
  try:

    0. tick_start = now()
       sync_id = insert_sync_history(id, started_at=tick_start)
         ← inserts row with sync_status='syncing', files_* = 0,
           sync_id = COALESCE(MAX(sync_id), 0) + 1 for this connector

    1. update_connector_sync_status(id, "running", tick_start)

    2. scanner = build_scanner(connector_config)   ← dispatch on connector_config["type"]
                                                      ssh_sftp → SFTPScanner
                                                      s3       → S3Scanner
       scanner.connect()           ← decrypt KEK→DEK→credential in-memory

    3. known_hashes = list_connector_hashes(id)
                      ← set[sha256] this connector held at end of last tick
       to_ingest, orphan_hashes = scanner.scan(known_hashes)
       scanner.close()   ← privkey zeroized; DEK discarded
       files_found = len(to_ingest) + len(orphan_hashes) + <unchanged count>
         ← total files seen on the remote this tick

       # Sort diff list: most-recently-modified files are processed first.
       # This ensures the freshest content reaches the ingestion pipeline
       # before older revisions when batches are processed sequentially.
       to_ingest.sort(key=lambda f: f.last_modified, reverse=True)

       update_sync_history_files_syncing(id, sync_id, files_syncing=len(to_ingest))
         ← snapshot the number of files that will need work this tick

    ┌─── ORPHAN DELETES ──────────────────────────────────────────────────┐
    │  4. For each sha256 in orphan_hashes:                               │
    │       doc_id = lookup_content_by_sha256(sha256)                     │
    │       remaining = delete_connector_membership_atomic(id, sha256)    │
    │         ← deletes membership row + counts remaining refs, in a txn  │
    │       if remaining == 0 and doc_id:                                 │
    │           DELETE /v1/documents/{doc_id}                             │
    │             → treat 404 as success                                  │
    │           delete_checksum_registry(sha256)                          │
    │       # if remaining > 0: another connector holds it — leave intact │
    │         → on hard error: log, membership row already removed        │
    └─────────────────────────────────────────────────────────────────────┘

    ┌─── DEDUP + BATCH INGEST (D-10: Option B) ───────────────────────────┐
    │                                                                     │
    │  5. Dedup pass — identify which files genuinely need ingest:        │
    │                                                                     │
    │       needs_ingest: list[RemoteFile] = []                           │
    │                                                                     │
    │       For each file in to_ingest (sorted, most-recent-first):       │
    │           existing_doc_id = lookup_content_by_sha256(file.checksum) │
    │                                                                     │
    │           if existing_doc_id:                                        │
    │               ← content already ingested by another connector        │
    │               insert_connector_membership(id, file.checksum)         │
    │               ← no download, no ingest; membership row is enough     │
    │               files_completed += 1                                   │
    │               update_sync_history_files_syncing(                     │
    │                   id, sync_id, files_syncing=remaining_to_process)   │
    │               continue  (counted as files_deduped)                   │
    │                                                                      │
    │           ← first time this content is seen globally: must ingest    │
    │           needs_ingest.append(file)                                  │
    │                                                                     │
    │  6. Store computed checksums for all diff-list files in the DB      │
    │     before download begins, so that a crash mid-batch does not      │
    │     leave the registry in a permanently inconsistent state.          │
    │                                                                     │
    │     NOTE: only files in needs_ingest reach this step — deduped      │
    │     files already have a registry row from the connector that        │
    │     originally ingested them.                                        │
    │                                                                     │
    │     For each file in needs_ingest:                                  │
    │         upsert_pending_checksum(id, file.checksum)                  │
    │           ← lightweight write; doc_id is NULL until ingest succeeds │
    │           ← purpose: prevent a second concurrent connector tick      │
    │             (possible after a retry) from re-queuing the same file   │
    │                                                                     │
    │  7. if needs_ingest:                                                │
    │                                                                     │
    │    tick_staging_dir = staging_dir / f"connector-{id}-{tick_ts}/"   │
    │    tick_staging_dir.mkdir(parents=True, exist_ok=True)              │
    │                                                                     │
    │    BATCH_SIZE = 10                                                   │
    │    batches = [needs_ingest[i:i+BATCH_SIZE]                          │
    │               for i in range(0, len(needs_ingest), BATCH_SIZE)]     │
    │                                                                     │
    │    For batch_idx, batch in enumerate(batches, start=1):             │
    │      ┌── Per-batch processing ────────────────────────────────────┐ │
    │      │                                                            │ │
    │      │  job_name = f"{id}-{sync_id}-{batch_idx}"                 │ │
    │      │    ← e.g. "connector-abc123-7-1", "connector-abc123-7-2"  │ │
    │      │                                                            │ │
    │      │  7a. For each file in batch:                              │ │
    │      │        stage_path = tick_staging_dir / sanitize(file.path)│ │
    │      │        try:                                               │ │
    │      │            scanner.download_to(file.path, stage_path)     │ │
    │      │              ← re-opens SFTP connection with fresh decrypt │ │
    │      │                if needed                                  │ │
    │      │        except:                                            │ │
    │      │            log + skip this file                           │ │
    │      │            delete_checksum_registry(file.checksum)        │ │
    │      │              ← SHA was pre-written in step 6; remove it   │ │
    │      │                so the next tick re-detects this file      │ │
    │      │            files_failed += 1; remove from batch           │ │
    │      │                                                            │ │
    │      │  7b. Copy staged batch files to tmp before pipeline:      │ │
    │      │        batch_tmp_dir = tmp_dir /                          │ │
    │      │            f"connector-{id}-{sync_id}-batch{batch_idx}/"  │ │
    │      │        batch_tmp_dir.mkdir(parents=True, exist_ok=True)   │ │
    │      │        For each successfully downloaded file in batch:    │ │
    │      │            try:                                           │ │
    │      │                shutil.copy2(stage_path, batch_tmp_dir /   │ │
    │      │                             stage_path.name)              │ │
    │      │            except:                                        │ │
    │      │                log + skip this file                       │ │
    │      │                delete_checksum_registry(file.checksum)    │ │
    │      │                  ← SHA was pre-written in step 6; remove  │ │
    │      │                    it so the next tick re-detects the file│ │
    │      │                files_failed += 1; remove from batch       │ │
    │      │          ← staging dir stays intact for reference;        │ │
    │      │            pipeline operates on the tmp copy              │ │
    │      │                                                            │ │
    │      │  7c. job_id = create_connector_ingest_job(                │ │
    │      │              connector_id=id,                             │ │
    │      │              job_name=job_name)                           │ │
    │      │        ← creates a Job row (operation = "ingestion")      │ │
    │      │                                                            │ │
    │      │  7d. doc_id_dict = register_documents_for_staged_files(   │ │
    │      │              batch_tmp_dir, job_id)                       │ │
    │      │        ← creates Document rows for each file in tmp dir   │ │
    │      │                                                            │ │
    │      │  7e. ingest(batch_tmp_dir, job_id, doc_id_dict)           │ │
    │      │        ← blocking call on this worker thread              │ │
    │      │        ← same pipeline as api/v1/jobs.py:_run_ingest()   │ │
    │      │        ← pipeline reads from batch_tmp_dir (tmp copy)    │ │
    │      │                                                            │ │
    │      │  7f. On batch ingest completion:                          │ │
    │      │        for each successfully ingested file:               │ │
    │      │            insert_checksum_registry(file.checksum, doc_id)│ │
    │      │              ← updates the pending row (NULL doc_id →     │ │
    │      │                real doc_id) written in step 6             │ │
    │      │            insert_connector_membership(id, file.checksum) │ │
    │      │            files_completed += 1                           │ │
    │      │        for each failed ingest:                            │ │
    │      │            delete_checksum_registry(file.checksum)        │ │
    │      │              ← ingest failed; remove the pending SHA so   │ │
    │      │                the next tick re-detects and retries it    │ │
    │      │            files_failed += 1                              │ │
    │      │        cleanup tmp copy: shutil.rmtree(batch_tmp_dir)     │ │
    │      └────────────────────────────────────────────────────────────┘ │
    │                                                                     │
    │    cleanup staging dir: shutil.rmtree(tick_staging_dir)             │
    └─────────────────────────────────────────────────────────────────────┘

    8. final_status = "partial_error" if files_failed > 0 else "completed"
       update_connector_sync_status(id, final_status if files_failed else "idle", now())
       update_sync_history(
           connector_id=id,
           sync_id=sync_id,
           finished_at=now(),
           files_found=files_found,
           files_syncing=0,          ← always 0 at tick end
           files_completed=files_completed,
           files_failed=files_failed,
           sync_status=final_status
       )

  except paramiko.SSHException as e:
    logger.error(f"SFTP connection failure for connector {id}: {e}")
    update_connector_sync_status(id, "partial_error", now())
    update_sync_history(id, sync_id, finished_at=now(),
        files_found=0, files_syncing=0, files_completed=0,
        files_failed=0, sync_status="failed")
    raise   ← re-raised so the retry wrapper can catch it

  except S3ScannerError as e:        ← raised by S3Scanner on credential/bucket errors
    logger.error(f"S3 connection failure for connector {id}: {e}")
    update_connector_sync_status(id, "partial_error", now())
    update_sync_history(id, sync_id, finished_at=now(),
        files_found=0, files_syncing=0, files_completed=0,
        files_failed=0, sync_status="failed")
    raise   ← re-raised so the retry wrapper can catch it

  except Exception as e:
    logger.error(f"Unexpected error in sync tick for connector {id}: {e}", exc_info=True)
    update_connector_sync_status(id, "partial_error", now())
    update_sync_history(id, sync_id, finished_at=now(),
        files_found=0, files_syncing=0, files_completed=0,
        files_failed=0, sync_status="failed")
    raise   ← re-raised so the retry wrapper can catch it

  finally:
    self._tick_running = False          ← reset even on fatal error

  # Interruptible sleep: wakes on stop_event OR manual trigger.
  # Each worker is its own timer — sync_interval_seconds is read from
  # self._config, so no shared scheduler exists. A PUT that changes the
  # interval simply stops this worker and starts a fresh one with the
  # updated config; the new interval takes effect from the first tick.
  stop_event.wait(sync_interval_seconds)   ← interruptible sleep
```

> **Note:** The try/except blocks shown above are the *inner* tick body. They are wrapped by the retry logic described in §8.6; exceptions are re-raised so the retry wrapper can decide whether to retry or give up.

**Ordering guarantee:** The `to_ingest.sort(key=lambda f: f.last_modified, reverse=True)` call in step 3 means batches are always submitted to `/v1/jobs` in most-recently-modified-first order. Within a single batch, files are also in that order. The ordering is applied once after the full scan/diff and before any download or batch creation, so it is consistent across all batches of the tick.

**Job naming:** Each batch's job name is `"{connectorID}-{syncID}-{batchCount}"` — for example, a connector with `id = "abc123"` running its 7th tick will produce jobs named `"abc123-7-1"`, `"abc123-7-2"`, etc. The `sync_id` is stable per tick (inserted once at step 0) so it ties every batch job back to a single, auditable tick record in `connector_sync_history`.

**tmp copy before pipeline (step 7b):** Each batch's files are copied from the tick staging directory to a dedicated `batch_tmp_dir` immediately before the pipeline call. This gives the ingestion pipeline an isolated, stable directory to operate on — it cannot accidentally observe a partially-downloaded state from a concurrent batch. The staging directory is kept intact throughout the tick and only torn down after all batches complete (step 7, outer cleanup), so per-batch tmp dirs are short-lived and cleaned up immediately after `ingest()` returns for that batch.

**SHA cleanup invariant:** Step 6 pre-writes a pending checksum row (with `NULL` `doc_id`) for every file in `needs_ingest` before any download begins. This row is the "in-flight" marker. The invariant is: **if a file does not reach a successful ingest completion, its pending SHA row must be deleted before the tick ends.** The three failure points that enforce this are:

- **Download failure (step 7a):** `delete_checksum_registry(sha256)` called immediately on the per-file download exception.
- **Copy-to-tmp failure (step 7b):** `delete_checksum_registry(sha256)` called immediately on the per-file `shutil.copy2` exception.
- **Ingest failure (step 7f):** `delete_checksum_registry(sha256)` called for each file the pipeline marks as FAILED.

In all three cases the result is that the file's SHA is absent from `file_checksum_registry` at tick end. The next tick's scan will recompute the same SHA, find it absent from `known_hashes`, and re-add the file to `to_ingest` — giving the file a clean retry without any manual intervention.

### 8.2 Tick Guard — Skipping Overlapping Ticks

If the previous tick's sync process (SFTP walk + staging + ingest) is still running when the next interval fires, the new tick is **skipped entirely**. No second worker is spawned; the timer simply resets for the next interval.

```python
class ConnectorSyncWorker:
    def __init__(self, connector_id: str, connector_config: dict,
                 stop_event: threading.Event) -> None:
        self.connector_id = connector_id
        self._config = connector_config
        self._stop_event = stop_event
        self._tick_running = False   # ← thread-local flag; guarded on the single worker thread
```

### 8.3 Staging Layout

```
/var/cache/staging/
  connector-<connector_id>-<tick_timestamp>/   ← per-tick staging dir (all diff files)
    subdir/
      report.pdf
    summary.docx

/tmp/
  connector-<connector_id>-<sync_id>-batch1/  ← tmp copy for batch 1 pipeline
    report.pdf
  connector-<connector_id>-<sync_id>-batch2/  ← tmp copy for batch 2 pipeline
    summary.docx
```

The staging dir is created fresh each tick and persists until all batches complete, then removed via `shutil.rmtree()`. Before each batch enters the ingestion pipeline, its files are copied from the staging dir into a dedicated `batch_tmp_dir` under `/tmp/`. The pipeline operates exclusively on the tmp copy. Each `batch_tmp_dir` is torn down via `shutil.rmtree()` immediately after `ingest()` returns for that batch. Files are placed in sub-paths that mirror the remote structure for traceability. The existing `cleanup_staging_directory` utility in `common.misc_utils` can be reused for both the per-batch tmp dirs and the final staging dir cleanup.

### 8.4 Error Handling Matrix

| Scenario | Action | Checksum record |
| --- | --- | --- |
| Download of new/modified file fails | Log + skip file; file not staged; `delete_checksum_registry(sha256)` called | **Removed** — pending SHA deleted; next tick re-detects the file |
| Copy to tmp fails (`shutil.copy2` I/O error) | Log + skip file; file not passed to pipeline; `delete_checksum_registry(sha256)` called | **Removed** — pending SHA deleted; next tick re-detects the file |
| Ingest of staged file fails | `pipeline.ingest` marks doc as FAILED in Job; log; `delete_checksum_registry(sha256)` called | **Removed** — pending SHA deleted; next tick re-detects and retries the file |
| Modified file: delete old doc fails (already absent) | Treat as success; proceed with ingest | Updated after ingest |
| Deleted file: VDB delete → doc already absent | Treat as success | Deleted |
| Deleted file: VDB hard error | Log; keep checksum for retry next tick | Retained |
| SFTP connection failure (whole tick) — attempt 1–3 | Retry with exponential backoff (§8.6); `sync_status = partial_error` between attempts | Unchanged |
| SFTP connection failure (whole tick) — all retries exhausted | `sync_status = partial_error`; sleep full interval then retry next tick | Unchanged |
| S3 credential/bucket error (whole tick) — attempt 1–3 | Retry with exponential backoff (§8.6); `sync_status = partial_error` between attempts | Unchanged |
| S3 credential/bucket error (whole tick) — all retries exhausted | `sync_status = partial_error`; sleep full interval then retry next tick | Unchanged |
| Unexpected exception (whole tick) — attempt 1–3 | Retry with exponential backoff (§8.6); `sync_status = partial_error` between attempts | Unchanged |
| Unexpected exception (whole tick) — all retries exhausted | `sync_status = partial_error`; sleep full interval then retry next tick | Unchanged |
| Tick fired while previous tick running | Skip — log info; reset timer | Unchanged |
| Retry backoff sleep interrupted by `stop_event` | Abort remaining retries; exit worker loop cleanly | Unchanged |

### 8.5 Blocking Semantics of File Download & Ingest

All work inside a single tick — including file downloads (step 5a) and the `ingest()` call (step 5d) — executes **synchronously and sequentially on the connector's dedicated worker thread**. There is no internal thread pool spawned for downloads, and `ingest()` is called as a blocking function, not scheduled as a background coroutine.

**Why blocking is correct here:**

- **The tick is an atomic unit.** The tick guard (`self._tick_running`) relies on the entire tick — scan, download, ingest — being a single sequential operation. If downloads were parallelised into sub-threads, the guard logic would need to coordinate across them, adding complexity with no correctness benefit for a single-connector worker.
- **Each connector already has its own thread.** That thread's time is entirely dedicated to one connector. There is no other useful work it could be doing concurrently for the same connector; blocking is the correct model.
- **The GIL is not a factor during I/O.** Although downloads and ingest run on the thread synchronously, the Python GIL is released during every network call: paramiko socket reads/writes (SFTP), boto3 HTTP calls (S3), and psycopg2 queries (Postgres). This means all other connector worker threads and the Uvicorn API handler threads continue running concurrently in the OS while a download or ingest is in progress — the blocking is local to that one worker thread, not to the whole process.

**Is a process better for blocking I/O?** No. A `multiprocessing.Process` would move the blocking work to a separate OS process, but the GIL is already irrelevant for I/O — the process boundary adds memory overhead and IPC complexity without enabling any parallelism that threads don't already provide. See §9.1 for the full analysis.

**Stuck-download edge case:** If an SFTP or S3 download hangs indefinitely (network partition, remote server stall), the worker thread will remain blocked on the socket read. The tick guard prevents a second tick from starting. The `paramiko` and `boto3` clients both honour socket-level timeouts configurable at construction time — these should be set to a reasonable value (e.g. 60 s) in `SFTPScanner` and `S3Scanner` to bound the maximum tick duration and ensure `stop_event` is eventually checked. See §8.4 (Error Handling Matrix) for the per-scenario recovery policy.


### 8.6 Sync Tick Retry — Exponential Backoff

If the sync tick body raises an exception (SFTP failure, S3 credential error, or any unexpected error), the worker retries the **entire tick** up to **3 times** before giving up and sleeping until the next scheduled interval.

#### Retry Parameters

| Parameter | Value | Rationale |
| --- | --- | --- |
| Max attempts | 3 | Balances resilience against transient failures without masking persistent misconfiguration |
| Base delay | 2 s | Short enough not to block the worker thread for an unreasonable time |
| Multiplier | 2× (exponential) | Delays: 2 s → 4 s → 8 s (total extra wait ≤ 14 s before giving up) |
| Jitter | ±10 % of computed delay | Avoids thundering-herd if many connectors fail simultaneously on restart |
| Stop-event aware | Yes | Each backoff sleep is `stop_event.wait(delay)` — the worker exits cleanly mid-retry if shutdown is requested |

#### Retry Pseudocode

```
def _run_tick_with_retry(self) -> None:
    MAX_ATTEMPTS = 3
    BASE_DELAY   = 2.0   # seconds

    for attempt in range(1, MAX_ATTEMPTS + 1):
        try:
            self._run_tick()   ← the full tick body (§8.1)
            return             ← success — no retry needed

        except Exception as e:
            if attempt == MAX_ATTEMPTS:
                logger.error(
                    f"Connector {id}: tick failed after {MAX_ATTEMPTS} attempts, "
                    f"giving up until next interval. Last error: {e}"
                )
                return         ← exhausted; outer loop sleeps sync_interval_seconds

            delay = BASE_DELAY * (2 ** (attempt - 1))   # 2 s, 4 s, 8 s
            jitter = delay * random.uniform(-0.1, 0.1)
            sleep_for = delay + jitter

            logger.warning(
                f"Connector {id}: tick attempt {attempt}/{MAX_ATTEMPTS} failed "
                f"({type(e).__name__}), retrying in {sleep_for:.1f} s — {e}"
            )

            # Interruptible: respects shutdown signal during backoff sleep
            if self._stop_event.wait(sleep_for):
                return   ← stop_event was set; abort retries immediately
```

#### Integration with `run()` loop

The `while not stop_event.is_set()` loop in `run()` calls `_run_tick_with_retry()` instead of `_run_tick()` directly:

```python
while not self._stop_event.is_set():
    if self._tick_running:
        logger.info(f"Connector {self.connector_id}: previous tick still running, skip")
        self._stop_event.wait(self._config["sync_interval_seconds"])
        continue

    self._tick_running = True
    try:
        self._run_tick_with_retry()
    finally:
        self._tick_running = False

    self._stop_event.wait(self._config["sync_interval_seconds"])
```

> **Scope of retry:** Retries cover whole-tick failures (scanner connection errors, unexpected exceptions). Per-file download errors inside step 5a are **not** retried within the same tick — they are skipped individually (§8.4). The failed files will be re-attempted on the next scheduled tick when the diff computation re-detects them as new/modified (their checksums were never written).

### 8.7 Sync History — Insert/Update Logic and Corner Cases

This section enumerates precisely when a `connector_sync_history` row is created or mutated, and how every edge case is handled.

#### Normal tick lifecycle (two writes)

| Phase | DB call | What is written |
| --- | --- | --- |
| **Tick start** (step 0, before any scan) | `insert_sync_history(connector_id, started_at)` | New row: `sync_status='syncing'`, `files_*=0`, `finished_at=NULL`. `sync_id = MAX(sync_id)+1` for this connector. |
| **After scan** (step 3, after `scanner.close()`) | `update_sync_history_files_syncing(connector_id, sync_id, len(to_ingest))` | Live `files_syncing` snapshot showing how many files need processing. |
| **Dedup/ingest progress** (steps 4–5) | `update_sync_history_files_syncing(…, remaining_to_process)` | Decrements `files_syncing` as each file is resolved (deduped or downloaded). |
| **Tick end** (step 7) | `update_sync_history(connector_id, sync_id, finished_at=now(), files_found, files_syncing=0, files_completed, files_failed, sync_status)` | Final state. `files_syncing` forced to `0`. `sync_status` set to `'completed'` or `'partial_error'`. |

#### Corner cases

**1. Fatal exception before scan completes (connection refused, credential error)**

The tick body raises before step 3 completes. The `except` block writes the final UPDATE with all counters at `0` and `sync_status='failed'`. The row is not left in `'syncing'` indefinitely — even a whole-tick failure closes the history row cleanly.

```
insert_sync_history → row: syncing / 0 / 0 / 0 / 0 / NULL
... connect() raises SSHException / S3ScannerError ...
update_sync_history → row: failed / 0 / 0 / 0 / 0 / finished_at=now()
```

**2. Fatal exception mid-tick (after scan, during download or ingest)**

`files_found` was computed after `scanner.close()`. If the exception is caught, the except block writes `files_found=0` because `files_found` may not yet be bound in the local scope if the exception was raised very early (before `files_found` was assigned). To ensure correctness, `files_found` defaults to `0` at the top of the tick body; it is overwritten only after the scan phase completes. If it remains `0` in the except block, the history row will reflect `0` — consistent with the scan not completing.

**3. Retry attempts and the history row**

A retried tick (§8.6) is one logical tick that failed and is being re-attempted. There is **one `insert_sync_history` call per logical tick attempt, not one per retry**. The INSERT happens at the start of `_run_tick()`, which is called once per attempt by `_run_tick_with_retry()`. This means:

- If attempt 1 fails → its history row gets `sync_status='failed'`, `finished_at` set.
- Attempt 2 starts → a **new** history row is inserted with the next `sync_id`.
- This gives a clear audit trail: each attempt is its own record. The caller can see a sequence of `'failed'` rows followed eventually by a `'completed'` row if a later attempt succeeded.

**4. Pod killed mid-tick (SIGKILL / OOM)**

If the process is killed between the `insert_sync_history` INSERT and the final `update_sync_history` UPDATE, the row is left with `sync_status='syncing'` and `finished_at=NULL` permanently. This is intentional — it is the only observable signal that the pod was interrupted mid-tick. On pod restart, the startup recovery code does **not** attempt to close orphaned `'syncing'` rows; doing so would require knowing what the final file counts should have been, which is impossible without re-running the scan. Callers reading the history endpoint should treat any `'syncing'` row whose `started_at` is older than `2 × sync_interval_seconds` as a likely stale/interrupted row.

**5. Tick skipped by tick guard**

When `_tick_running` is `True` and the guard fires (§8.2), the tick body is not entered at all. No `insert_sync_history` call is made. Skipped ticks produce **no history row** — a gap in `sync_id` values is not possible, and the sequence remains contiguous. Only actually-started ticks appear in the history.

**6. `sync_id` sequence continuity across pod restarts**

`sync_id` is computed as `COALESCE(MAX(sync_id), 0) + 1` from `connector_sync_history` at tick start. Since the table persists in Postgres across pod restarts, the counter continues from the last recorded value. A pod restart followed by connector recovery will produce `sync_id = N+1` where `N` was the last `sync_id` before the restart — no gaps, no resets.

**7. Connector deleted while a tick is in progress**

If `DELETE /v1/connectors/{id}` is called while a tick is actively running for that connector, the DELETE handler signals `stop_event` and waits for the worker thread to exit (per §3C stop-sync sequence). Once the thread exits — completing or aborting the tick — the tick's final `update_sync_history` call will attempt to UPDATE a history row for a connector that is about to be (or has just been) cascade-deleted. Two outcomes are possible:

- **Thread exits before the connector row is deleted:** The final UPDATE succeeds normally; the history row is then removed by `ON DELETE CASCADE` when `delete_active_connector()` runs.
- **Connector row deleted before the thread's final UPDATE:** The FK cascade removes all history rows first. The subsequent UPDATE finds no matching `(connector_id, sync_id)` row and is a no-op (zero rows updated). No error is raised; the worker thread exits cleanly.

Either outcome is safe. No orphaned history rows are left behind.

**8. Connector re-registered after deletion (re-push from catalog)**

If catalog calls `POST /v1/connectors` with the same `connector_id` as a previously deleted connector (e.g. reconfiguring an SFTP source), `ON DELETE CASCADE` will have removed all previous history rows. The `sync_id` sequence restarts from `1` for the re-registered connector, as `MAX(sync_id)` returns `NULL` → `COALESCE(NULL, 0) + 1 = 1`.

---

## 9. Worker Manager

**New file:** `services/digitize/connector/worker_manager.py`

A module-level singleton, following the same pattern as the existing [`workers/concurrency.py`](../../services/digitize/workers/concurrency.py) `ConcurrencyManager`.

### 9.1 Background Thread vs Background Process — Analysis

Before presenting the implementation, this section works through the trade-offs between the two primary options for running long-lived sync workers in a Python service, and explains why threads are chosen here.

#### A. `threading.Thread` (background thread, same process)

A thread runs inside the Uvicorn/FastAPI process. It shares the same memory space, the same open database connections, and the same in-process state as the API handlers.

| Factor | Detail |
| --- | --- |
| **Memory overhead** | ~8 KB stack per thread. Ten connectors costs ~80 KB. Negligible. |
| **Startup latency** | Microseconds — OS thread creation, no process fork. |
| **Shared memory access** | Direct, zero-copy. The worker can call `connector_worker_manager`, read settings, and use DB connection objects already initialised by the main process without any IPC. |
| **Shutdown / stop signalling** | `threading.Event.set()` + `thread.join()` — clean cooperative stop with a timeout. |
| **Python GIL** | The GIL serialises pure-Python bytecode across threads. However, the sync worker spends the majority of its time in **I/O-bound operations**: paramiko socket reads/writes, SFTP transfers, and Postgres queries. The GIL is released during all system calls and C-extension I/O, so in practice the GIL is **not a bottleneck** for this workload. |
| **Crash isolation** | An unhandled exception in a thread does not kill the process, but a thread that calls `os.abort()` or triggers a segfault in a C extension (e.g. a bug in a native crypto library) would crash the whole pod. In practice, the sync worker is pure Python + paramiko + psycopg2 — all mature libraries with no known crash-inducing bugs in this usage pattern. |
| **Forceful kill** | Python provides no `Thread.kill()` or `Thread.terminate()`. A thread that is genuinely stuck (e.g. blocked on a hung socket beyond the configured timeout) cannot be forcefully terminated — it must be left until the pod restarts. The tick guard ensures no second tick starts, and `daemon=True` ensures the thread is reaped at pod exit. |
| **Secrets access** | Reads `/run/secrets/connector_kek` directly via `Path.read_bytes()` — same as the main process. No IPC needed to pass secrets. |
| **Dependency footprint** | None — `threading` is in the Python standard library. |

#### B. `multiprocessing.Process` (background process, separate process)

Each worker runs as a separate OS process with its own memory space, its own GIL, and its own Python interpreter.

| Factor | Detail |
| --- | --- |
| **Memory overhead** | ~15–50 MB per process (full Python interpreter + imported modules + SQLAlchemy + paramiko). Ten connectors costs ~150–500 MB. Significant in a constrained pod. |
| **Startup latency** | Tens of milliseconds per worker — OS `fork()` or `spawn()`, module re-import, DB connection re-establishment. |
| **Shared memory access** | None. Every shared object (DB connections, settings, in-memory config) must be passed via pickle serialisation across the process boundary or re-initialised inside the child. |
| **Shutdown / stop signalling** | Must use `multiprocessing.Event` (backed by shared memory) or OS signals (`SIGTERM`). Slightly more complex than `threading.Event`. |
| **Python GIL** | Each process has its own GIL — true parallelism for CPU-bound code. **Irrelevant here** — the sync worker is I/O-bound, not CPU-bound. The ingest pipeline (`pipeline.ingest.ingest()`) does use CPU (PDF parsing, embedding calls), but it already offloads heavy work to worker threads internally and makes network I/O calls to external LLM/embedding services. There is no CPU-bound tight loop in the sync worker itself. |
| **Crash isolation** | A crash or segfault in a child process does not affect the API server. This is the main genuine advantage of processes over threads. |
| **Secrets access** | Child process inherits open file descriptors from the parent if using `fork`. With `spawn` (safer, avoids fork-safety issues with SQLAlchemy connection pools), secrets must be re-read from disk in the child. Still straightforward — `/run/secrets/connector_kek` is readable by any process in the pod. |
| **Forceful kill** | `process.kill()` (SIGKILL) is available — immediate, unconditional termination of a stuck worker without waiting for cooperative shutdown or pod restart. |
| **Dependency footprint** | `multiprocessing` is standard library, but `fork`-based spawning has well-known conflicts with SQLAlchemy connection pools and asyncio event loops — requiring `set_start_method("spawn")` and careful re-initialisation of all resources in each child. |

#### Side-by-Side Summary

| Concern | Thread | Process | Winner |
| --- | --- | --- | --- |
| Memory per worker | ~8 KB | ~15–50 MB | **Thread** |
| Startup cost | µs | tens of ms | **Thread** |
| Shared state access | Direct (zero-copy) | Pickle / IPC | **Thread** |
| Stop signalling | `threading.Event` | `multiprocessing.Event` / signal | **Thread** (simpler) |
| GIL impact | Released during all I/O | N/A (separate GIL) | **Tie** — workload is I/O-bound |
| Crash isolation | Pod crashes if thread calls `os.abort()` | Child crash doesn't affect API | **Process** |
| Forceful kill | No `Thread.kill()` — stuck thread waits for pod exit | `process.kill()` (SIGKILL) available | **Process** |
| Fork-safety with SQLAlchemy | No issue | Requires `spawn` + re-init | **Thread** |
| Secrets access | Direct read | Direct read (with `spawn`) | **Tie** |
| Extra dependencies | None | None (stdlib) | **Tie** |

#### Decision D-12 — Chosen: Background Thread

**Background threads (`threading.Thread`) are chosen.** The reasoning:

1. **The workload is I/O-bound, not CPU-bound.** The GIL — the only scenario where processes would outperform threads — is released for every paramiko socket operation, every SFTP read, and every psycopg2 query. Multiple connector workers run concurrently in practice even under the GIL.
2. **Memory is constrained inside a pod.** Processes cost 15–50 MB each; threads cost 8 KB each. With potentially several connectors active per application, the difference is material.
3. **Shared state is an asset, not a liability here.** Workers legitimately share the DB connection pool, settings, and the `connector_worker_manager` registry. Replicating this across processes via IPC adds complexity with no benefit.
4. **The crash isolation argument is weak for this workload.** The sync worker is pure Python calling well-tested I/O libraries. There is no native code path that would produce a segfault. Unhandled Python exceptions in a thread are caught at the `while` loop level and logged without killing the process.
5. **Fork-safety.** The service uses SQLAlchemy with a connection pool and asyncio. Using `multiprocessing` with `fork` in this environment is explicitly unsafe (SQLAlchemy's own docs warn against it). Using `spawn` is safe but requires full re-initialisation of all resources in each child — adding complexity that threads do not impose.

The two real advantages of processes — crash isolation and forceful kill — do not apply to this workload. The sync worker is pure Python calling well-tested I/O libraries with no native crash-inducing paths, and socket-level timeouts in the scanners bound the maximum tick duration so a stuck thread remains a recoverable condition. Threads are simpler, cheaper, and correct.

#### When would processes be the right choice instead?

Processes would be the correct model if any of these conditions held:

1. **CPU-bound workload.** If workers ran local ML inference, on-device OCR, or heavy compression in a tight Python loop, the GIL would serialise them and per-process GIL isolation would give true parallelism.
2. **Untrusted or crash-prone native extensions.** If the connector code executed third-party C extensions with known crash risks, process-level crash isolation would prevent a segfault from taking down the API server.
3. **Immediate forced termination required.** If a stuck worker must be killed unconditionally (bypassing the 30 s `thread.join` timeout), `process.kill()` (SIGKILL) makes that possible. Threads have no equivalent.
4. **No shared in-process state needed.** If workers required no access to the DB connection pool, settings, or manager registry, the process boundary would impose no extra IPC cost and the isolation would be a net win.

None of these conditions hold for the connector sync workload. The decision remains threads.

### 9.2 Implementation

```python
import threading
from common.misc_utils import get_logger

logger = get_logger("connector_worker_manager")


class ConnectorSyncWorker:
    def __init__(self, connector_id: str, connector_config: dict,
                 stop_event: threading.Event) -> None:
        self.connector_id = connector_id
        self._config = connector_config
        self._stop_event = stop_event
        self._tick_running = False       # guarded on the single worker thread
        self._manual_trigger = threading.Event()  # set to interrupt the sleep for an immediate tick

    def trigger_now(self) -> None:
        """Signal the worker to start a tick immediately, bypassing the scheduled sleep.

        Safe to call from any thread. Has no effect if a tick is already running
        (the tick guard in run() will skip the queued tick and the next one will
        fire after the normal interval).
        """
        self._manual_trigger.set()

    def run(self) -> None:
        while not self._stop_event.is_set():
            # --- TICK GUARD ---
            if self._tick_running:
                logger.info(f"Connector {self.connector_id}: previous tick still running, skip")
                # Wait for either the interval or a manual trigger, then loop.
                self._stop_event.wait(self._config["sync_interval_seconds"])
                self._manual_trigger.clear()
                continue

            self._manual_trigger.clear()  # consume any pending trigger before starting
            self._tick_running = True
            try:
                self._run_tick()
            finally:
                self._tick_running = False

            # Interruptible sleep: wakes on stop_event OR manual trigger.
            # threading.Event.wait() returns True if the event was set, False on timeout.
            self._manual_trigger.wait(timeout=self._config["sync_interval_seconds"])
            self._manual_trigger.clear()

    def _run_tick(self) -> None:
        """Execute one sync tick (scan → diff → stage → ingest → delete)."""
        # ... (full tick logic as documented in §8.1) ...
        pass


class ConnectorWorkerManager:
    def __init__(self) -> None:
        # Maps connector_id → (Thread, ConnectorSyncWorker, stop_event)
        self._workers: dict[str, tuple[threading.Thread, ConnectorSyncWorker, threading.Event]] = {}
        self._lock = threading.Lock()

    def start_worker(self, connector_config: dict) -> ConnectorSyncWorker:
        """Start a new sync worker thread for the given connector.

        Returns the ConnectorSyncWorker instance so the caller can immediately
        call trigger_now() to fire a manual sync.
        """
        connector_id = connector_config["id"]
        with self._lock:
            if connector_id in self._workers:
                logger.info(f"Worker for connector {connector_id} already running — skipping")
                _, worker, _ = self._workers[connector_id]
                return worker
            stop_event = threading.Event()
            worker = ConnectorSyncWorker(connector_id, connector_config, stop_event)
            thread = threading.Thread(
                target=worker.run,
                daemon=True,
                name=f"connector-sync-{connector_id[:8]}",
            )
            thread.start()
            self._workers[connector_id] = (thread, worker, stop_event)
            logger.info(f"Started sync worker for connector {connector_id}")
            return worker

    def stop_worker(self, connector_id: str, timeout: float = 30.0) -> None:
        with self._lock:
            entry = self._workers.pop(connector_id, None)
        if entry is None:
            logger.warning(f"No active worker found for connector {connector_id}")
            return
        thread, _worker, stop_event = entry
        stop_event.set()
        thread.join(timeout=timeout)
        if thread.is_alive():
            logger.warning(
                f"Worker for connector {connector_id} did not stop within {timeout}s"
            )
        else:
            logger.info(f"Worker for connector {connector_id} stopped cleanly")

    def list_workers(self) -> list[str]:
        with self._lock:
            return list(self._workers.keys())


# Module-level singleton — mirrors concurrency_manager pattern
connector_worker_manager = ConnectorWorkerManager()
```

**`trigger_now()` mechanics:**

- `_manual_trigger` is a second `threading.Event` kept alongside `_stop_event` in the worker.
- At the bottom of the tick loop the worker calls `self._manual_trigger.wait(timeout=sync_interval_seconds)` instead of `stop_event.wait(...)`. This means the sleep returns early when either `_manual_trigger` is set (manual trigger) or `_stop_event` is set (shutdown).
- After waking, both events are `.clear()`-ed before the next iteration to avoid spurious re-triggers.
- `trigger_now()` is safe to call from the API handler thread (the handler does not hold the worker lock at that point).

`daemon=True` ensures threads are reaped automatically when the main Uvicorn process exits — no manual cleanup needed at pod shutdown.

---

## 10. Startup Recovery

**Modified file:** `services/digitize/app.py`

After the existing zombie job recovery block in the `lifespan()` context manager, add connector worker recovery. This is a **self-healing path** — if all connector configs are in the DB, the pod recovers fully without any catalog involvement.

**Spawner identity at startup:** Thread spawning at startup runs on the **main Uvicorn startup path**, inside the `lifespan()` async context manager, before the HTTP server begins accepting requests. There is no dedicated background thread or process responsible for spawning — the lifespan hook calls `start_worker()` synchronously in a loop, and `start_worker()` creates and starts each daemon thread inline. No manual trigger (`trigger_now()`) is called during recovery — the worker threads fire their first tick after their initial `sync_interval_seconds` sleep. This is intentional: at pod startup all connectors recovered simultaneously, and an immediate flood of SFTP/S3 connections would impose unnecessary load; the staggered natural interval is preferred.

```python
# In lifespan(), after zombie job recovery:
from digitize.utils.db import list_active_connectors
from digitize.connector.worker_manager import connector_worker_manager

try:
    connectors = list_active_connectors()
    for config in connectors:
        connector_worker_manager.start_worker(config)
        logger.info(f"✅ Restarted sync worker for connector {config['id']}")
    if connectors:
        logger.info(f"Connector recovery: {len(connectors)} worker(s) started")
    else:
        logger.info("Connector recovery: no active connectors in DB")
except Exception as exc:
    # Non-fatal: pod continues. Catalog can re-push if needed.
    logger.error(f"Connector recovery failed: {exc}", exc_info=True)
```

**Recovery failure policy (Decision D-13 — recommended):** Log and continue. A single bad connector config does not abort pod startup. The pod's health check passes; catalog can detect stale connectors via `GET /v1/connectors` and re-push.

---

## 11. Settings Changes

**Modified file:** `services/digitize/settings.py`

Remove any SSH/connector env-var fields that were previously read from the environment (e.g. `SSH_HOST`, `SSH_USERNAME`, `SSH_PRIVATE_KEY_PEM`, `SSH_REMOTE_PATH`, `SSH_SYNC_INTERVAL_SECONDS`). Connector config is no longer injected via environment — it arrives exclusively through the authenticated runtime API.

Add a `ConnectorConfig` nested settings class with paths that default to the Podman secret mount locations but can be overridden in unit tests without real secret files:

```python
class ConnectorConfig(BaseSettings):
    """Paths to Podman secret mounts used by the connector subsystem."""

    kek_path: Path = Field(
        default=Path("/run/secrets/connector_kek"),
        description=(
            "Path to the 32-byte pod KEK used for AES-256-GCM envelope decryption "
            "of connector private keys. Delivered as a Podman secret mount — never an env var."
        ),
    )
    api_token_path: Path = Field(
        default=Path("/run/secrets/connector_api_token"),
        description=(
            "Path to the bearer token used to authenticate the connector runtime API. "
            "Delivered as a Podman secret mount — never an env var."
        ),
    )

    model_config = SettingsConfigDict(env_prefix="CONNECTOR_")
```

Then add `connector: ConnectorConfig = Field(default_factory=ConnectorConfig)` to the top-level `Settings` class.

---

## 12. File & Module Map

| File | Status | Responsibility |
| --- | --- | --- |
| `api/v1/connectors.py` | **NEW** | POST/PUT/DELETE/GET /v1/connectors; bearer-token dependency; 202 + BackgroundTask worker start/restart; manual sync trigger on POST and PUT; stop-sync document-cleanup sequence on DELETE |
| `connector/__init__.py` | **NEW** | Makes `connector/` a Python sub-package |
| `connector/sftp_scanner.py` | **NEW** | KEK→DEK→privkey decryption per tick; paramiko SFTP; streaming SHA-256; `download_to()` |
| `connector/s3_scanner.py` | **NEW** | KEK→DEK→secret_access_key decryption per tick; boto3 `list_objects_v2` walk; streaming SHA-256; `S3ScannerError`; `download_to()` |
| `connector/scanner.py` | **NEW** | `build_scanner(config)` factory — dispatches on `config["type"]` to return the appropriate scanner instance |
| `connector/sync_worker.py` | **NEW** | Per-connector tick loop; tick guard; `build_scanner()` dispatch; diff → sort by `last_modified` DESC; orphan deletes; dedup pass; store checksums in DB; batch into groups of 10 (most-recent-first); per-batch: download → copy to tmp → create job (`{connectorID}-{syncID}-{batchCount}`) → register docs → ingest from tmp → cleanup; error handling |
| `connector/worker_manager.py` | **NEW** | Daemon thread pool singleton (`connector_worker_manager`); start/stop/list; `threading.Lock` |
| `db/scripts/init_schema.sql` | **MODIFIED** | Add `active_connectors` (with `connection_details` JSONB), `file_checksum_registry`, `connector_file_membership`, and `connector_sync_history` tables |
| `db/models.py` | **MODIFIED** | Add `ActiveConnector`, `FileChecksumRegistry`, `ConnectorFileMembership`, `ConnectorSyncHistory` ORM models |
| `utils/db.py` | **MODIFIED** | Add 15 new CRUD functions for connector tables (includes `merge_connection_details`, `lookup_content_by_sha256`, `delete_connector_membership_atomic`, `insert_sync_history`, `update_sync_history`, `update_sync_history_files_syncing`, `list_sync_history`) |
| `app.py` | **MODIFIED** | Register `connectors_router`; add connector startup recovery block in `lifespan()`; register `GET /v1/connectors/{connector_id}/sync-history` route in `connectors_router` |
| `settings.py` | **MODIFIED** | Remove SSH env-var fields; add `ConnectorConfig` with overridable secret paths |

> **Package naming:** The existing empty `connectors/` directory at `services/digitize/connectors/` (currently only containing `__pycache__`) is used as the package root. Rename to `connector/` (singular) to match the module names above and avoid confusion with the catalog-side "connectors" concept.

---

## 13. Decision Log

| ID | Decision | Choice Made | Notes |
| --- | --- | --- | --- |
| D-1 | Bearer-token enforcement point | **[abstract — circle back]** | Deferred; mechanism not yet chosen |
| D-2 | Worker start on POST /v1/connectors | **FastAPI `BackgroundTask`; return 202** | Response not held; worker thread started asynchronously |
| D-3 | TLS termination point | **[abstract — circle back]** | Deferred; Uvicorn `--ssl-certfile` is the candidate |
| D-4 | Schema management strategy | **`init_schema.sql` + SQLAlchemy ORM models** | Consistent with existing `init_schema.sql` + `Base.metadata.create_all()` pattern |
| D-5 | Store `doc_id` in global registry? | **Yes — `doc_id` stored in `file_checksum_registry`** | The global registry is the single source of truth for content-to-document mapping. `lookup_content_by_sha256` is called both during ingest (dedup check) and during orphan deletion (to retrieve the `doc_id` before reference-counted removal). |
| D-6 | KEK load timing | **Fresh per tick inside scanner `.connect()`** | Minimises KEK memory residence; enables future rotation without restart. Applies to both `SFTPScanner` and `S3Scanner`. |
| D-7 | File traversal depth | **Recursive DFS** | Full subtree of `remote_path` scanned |
| D-8 | File type filtering | **Catalog sends `allowed_extensions` list in push payload** | Stored in `active_connectors.allowed_extensions` (JSONB); applied at scan time |
| D-9 | Job tracking for connector syncs | **Create a `Job` row per batch of 10 files** | Connector syncs visible in `GET /v1/jobs`; `operation = "ingestion"`, `job_name = "{connectorID}-{syncID}-{batchCount}"` (e.g. `"abc123-7-2"`). Multiple Job rows are created per tick — one per 10-file batch. Each job name encodes the connector, the tick (`sync_id`), and the batch sequence number, making per-batch traceability straightforward. |
| D-10 | Staging strategy | **Option B extended: per-tick staging dir + per-batch tmp copy before pipeline** | All diff files are downloaded into a single per-tick staging dir ordered by `last_modified` DESC. The list is then sliced into batches of 10. Each batch is copied to a dedicated `batch_tmp_dir` under `/tmp/` immediately before `ingest()` is called — the pipeline operates on the tmp copy, not the staging dir. The `batch_tmp_dir` is torn down after each batch completes. The staging dir is torn down after all batches complete. This gives each pipeline call an isolated, stable input directory and avoids the pipeline observing partial downloads from concurrent batch operations. |
| D-11 | Change detection | **SHA-256 only** | Content-accurate; immune to mtime precision / server clock skew |
| D-12 | Threading model — Thread vs Process | **`daemon=True` `threading.Thread` per connector** | Workload is I/O-bound (GIL released during all network/DB calls); threads are 8 KB vs 15–50 MB per process; shared DB pool + state is an asset; fork-safety issues make `multiprocessing` with SQLAlchemy/asyncio complex; crash isolation and forceful-kill advantages of processes do not apply to this pure-Python I/O workload; socket-level timeouts in scanners bound maximum tick duration making stuck threads recoverable at pod restart. Full analysis in §9.1. |
| D-13 | Recovery failure policy | **Log and continue** | Matches existing zombie-job recovery behaviour; pod health check not blocked |
| D-14 | Manual sync on POST/PUT | **`trigger_now()` on `ConnectorSyncWorker` — sets `_manual_trigger` event to interrupt the sleep** | Fires the first tick immediately without waiting `sync_interval_seconds`; safe to call from the API handler thread; no-op if a tick is already running (tick guard skips it and the next tick fires after the normal interval) |
| D-15 | Document cleanup on DELETE | **Best-effort per-hash loop over `connector_file_membership` with reference-counted deletion** | Reads membership snapshot once via `list_connector_hashes`; for each hash calls `delete_connector_membership_atomic` (removes membership row + counts remaining refs in a single transaction); only calls `DELETE /v1/documents/{doc_id}` and `delete_checksum_registry` when remaining count == 0 (last connector to hold that content); treats 404 as success; logs but does not abort on 5xx; `ON DELETE CASCADE` on `connector_id` removes any remaining membership rows after the loop so the connector row delete is not held hostage by per-hash failures. |
| D-16 | Registry table design — single table vs split | **Split into `file_checksum_registry` (global, `sha256` PK) and `connector_file_membership` (per-connector, `(connector_id, sha256)` PK)** | A single table keyed on `(connector_id, sha256)` cannot serve global dedup — the dedup check needs a `sha256`-only lookup across all connectors. Splitting into two tables gives each concern its own shape: `file_checksum_registry` is a pure content store (one row per unique document), and `connector_file_membership` is the per-connector view used for orphan detection and safe reference-counted deletion. `ON DELETE CASCADE` on `connector_id` in the membership table safely removes a connector's membership rows without touching the shared content registry. |
| D-17 | Orphan delete race condition | **Atomic transaction: delete membership row first, then count remaining refs in the same transaction** | Two concurrent connector ticks could both observe `remaining == 0` for the same orphaned hash if the check is not atomic. By deleting the membership row and counting remaining rows inside a single `BEGIN/COMMIT`, the database serialises the decision — only one tick can observe `remaining == 0` and proceed to delete the document and registry entry. All others will see `remaining >= 1` at check time and leave the document intact. |
| D-18 | Sync history retention policy | **Retain all rows for connector lifetime; delete via `ON DELETE CASCADE` when connector is removed** | History rows are never deleted by application logic during normal operation. Retention is unbounded while a connector exists. When the connector is removed all its history is cascade-deleted. This avoids a separate purge job, keeps the API simple, and matches the "connector lifetime = history lifetime" expectation. If table growth becomes a concern in long-lived deployments a scheduled TRUNCATE or a `LIMIT`-based API cap (`max 200 rows`) provides an operational escape hatch without a schema change. |
| D-19 | `sync_id` scoping — per-connector vs global | **Per-connector, 1-based, `COALESCE(MAX(sync_id), 0) + 1`** | A global auto-increment PK (`BIGSERIAL id`) already exists for row identity. A per-connector `sync_id` is what catalog needs to display "tick #7 of connector X" in the UI. The per-connector counter restarts at 1 after a connector is deleted and re-registered, which is correct because the history for the old registration was also wiped by `ON DELETE CASCADE`. |
| D-20 | Stale `'syncing'` rows after pod crash | **Leave as-is; document threshold heuristic; do not retroactively update on recovery** | Updating orphaned `'syncing'` rows on startup would require knowing the final file counts, which is unknowable without re-running the scan. The stale row is the accurate record of an incomplete tick. Callers are documented to treat `started_at > 2 × sync_interval_seconds` ago with `sync_status = 'syncing'` as likely stale. |
| D-21 | One history row per retry attempt vs one row per logical interval | **One row per attempt** | Each call to `_run_tick()` (including retry attempts) inserts its own history row. This gives a complete audit trail: a sequence of `'failed'` rows followed by a `'completed'` row makes it immediately visible that a tick succeeded on the third attempt. Collapsing retries into one row would lose that information and complicate the update logic (would need to update the same row's `sync_status` across multiple attempts without a clean "done" signal until all retries are exhausted). |
