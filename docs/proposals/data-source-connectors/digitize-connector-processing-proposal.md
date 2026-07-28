# Digitize — Data Source Connector Processing: Detailed Proposal

> **Scope:** This document focuses exclusively on the internals of the `digitize` service — what it does after it receives a connector push payload from catalog. Catalog-side concerns (key generation, connector CRUD, deployment wiring, TLS provisioning, TLS termination strategy, and API-level bearer-token enforcement details) are treated as a resolved black box and marked **[abstract – to be detailed separately]** where they appear.

---

## Table of Contents

1. [Assumptions & Preconditions](#1-assumptions--preconditions)
2. [System Overview Diagram](#2-system-overview-diagram)
3. [Catalog-to-Digitize API Reference](#3-catalog-to-digitize-api-reference) — A. POST · B. PUT · C. DELETE · D. GET (list) · E. GET (single) · **F. GET sync-history**
4. [Database Schema](#4-database-schema) — 4.1 `active_connectors` · 4.2 `file_checksum_registry` & `connector_file_membership` · 4.3 SQLAlchemy ORM Models · **4.4 `connector_sync_history`**
5. [Database Operations Layer](#5-database-operations-layer)
6. [Scanner Interface](#6-scanner-interface) — 6.1 Design Rationale · 6.2 Class Hierarchy · 6.3 BaseScanner Contract · 6.4 Shared vs Per-Implementation · 6.5 Per-Tick Sequence · 6.6 Credential Decryption Flow · 6.7 Factory
7. [SFTP Scanner](#7-sftp-scanner)
8. [Sync Worker](#8-sync-worker) — 8.1 Per-tick Flow · 8.2 Tick Guard · 8.3 Staging Layout · 8.4 Error Handling Matrix · 8.5 Blocking Semantics of File Download & Ingest · **8.6 Sync History**
9. [Worker Manager](#9-worker-manager)
10. [Startup Recovery](#10-startup-recovery)
11. [Settings Changes](#11-settings-changes)
12. [File & Module Map](#12-file--module-map)
13. [Decision Log](#13-decision-log)
14. [Thread Lifecycle & Resilience](#14-thread-lifecycle--resilience) — 14.1 Threading Model · 14.2 Thread Crash & Status · 14.3 Respawn Logic · 14.4 Generic Thread Monitor · 14.5 Interrupt on DELETE · 14.6 FastAPI Lifespan Recovery

---

## 1. Assumptions & Preconditions

Catalog has already performed all of the following before any digitize endpoint is called:

- For **SSH/SFTP connectors:** Generated an Ed25519 key pair per connector, stored the private key in the catalog DB; validated remote SFTP connectivity; sends the private key **in plaintext** over the POST/PUT call to digitize (protected in transit by TLS). Digitize encrypts the received private key at rest using the pod's encryption key before storing it in the DB.
- For **S3 connectors:** Validated that the supplied Access Key ID + Secret Access Key can successfully list objects in the target bucket and region; sends the secret access key **in plaintext** over the POST/PUT call to digitize (protected in transit by TLS). Digitize encrypts the received secret access key at rest using the pod's encryption key before storing it in the DB.
- Before pod start: provisioned `/run/secrets/connector_encryption_key` (a 32-byte AES-256-GCM key used to encrypt and decrypt connector credentials at rest) and `/run/secrets/connector_api_token` as Podman secret mounts.
- **[abstract]** Bearer-token enforcement mechanism and TLS listener configuration are resolved at the infrastructure layer. The API assumes those are in place.

---

## 2. System Overview Diagram

```
Catalog (external)
  │
  │  POST   /v1/connectors        { connector_id, type, host,
  │                                  allowed_extensions, sync_interval_seconds,
  │                                  connection_details: { <type-specific fields,
  │                                    private_key or secret_access_key in plaintext> } }
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
│  │  Connector API              api/v1/connectors.py             │  │
│  │  [abstract] bearer-token middleware + TLS listener          │  │
│  │  on POST/PUT: encrypts credential fields using the pod       │  │
│  │  encryption key before persisting to the DB                 │  │
│  └────────────────────────┬─────────────────────────────────────┘  │
│                            │  upsert / delete                       │
│  ┌─────────────────────────▼───────────────────────────────────┐  │
│  │  active_connectors table  (Postgres)                        │  │
│  │  connection_details JSONB  (credentials encrypted at rest)   │  │
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
│  │  │    enc_key → privkey_pem  (in-memory, per tick)      │   │  │
│  │  │    paramiko + AutoAddPolicy                          │   │  │
│  │  │  s3      → S3Scanner     connector/s3_scanner.py     │   │  │
│  │  │    enc_key → secret_access_key  (in-memory)          │   │  │
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
│  │  /run/secrets/connector_encryption_key  (Podman secret)      │  │
│  │  32-byte AES-256-GCM key — encrypts credentials at rest,     │  │
│  │  decrypts at sync time (read fresh per tick)                 │  │
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

**Purpose:** Called by catalog when a new data-source connector is created and fully validated. Digitize receives the plaintext credentials, encrypts them at rest using the pod's encryption key, provisions the `active_connectors` row, and starts a sync worker thread for the connector. The worker's tick loop begins immediately — the first tick runs as soon as the thread starts, without waiting for the first scheduled interval to elapse.

**When catalog calls it:** After credential validation is complete on the catalog side. Catalog sends the plaintext credentials directly in the request body over TLS. This is the "activate" signal — the connector will begin syncing immediately after this call.

**Thread spawning on POST:** The handler registers a FastAPI `BackgroundTask` that calls `connector_worker_manager.start_worker(config)` *after* the 202 response is flushed to the client — the HTTP response is therefore never blocked by thread creation. Inside `start_worker()`, under `self._lock`, a `threading.Event` (the stop signal) is created, a `ConnectorSyncWorker` is constructed, a `daemon=True` `threading.Thread` is created with `target=worker.run`, and `thread.start()` is called. From that point the OS thread is live and the triple `(thread, worker, stop_event)` is registered in the `_workers` dict. There is **no dedicated spawner thread** — the Uvicorn process itself is the spawner, via the BackgroundTask mechanism.

**First tick on POST:** The worker's `run()` loop executes the first tick immediately upon thread start — there is no initial sleep. Subsequent ticks are separated by `sync_interval_seconds`. This means the first batch of documents is ingested as soon as the thread is live, without any manual signal needed.

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
    "port":        22,
    "username":    "sync_user",
    "remote_path": "/exports/reports",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n..."
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
    "region":            "us-east-1",
    "bucket_name":       "my-rag-documents",
    "access_key_id":     "AKIAIOSFODNN7EXAMPLE",
    "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
  }
}
```

> **Credential handling:** Credentials are sent in plaintext over the POST body (protected in transit by TLS). On receipt, the API handler reads the pod's encryption key from `/run/secrets/connector_encryption_key` and encrypts the credential field(s) using AES-256-GCM before writing to `connection_details` in the DB. The plaintext value is never written to the database. For S3, catalog should provision **read-only IAM keys** (i.e. a policy granting `s3:GetObject` and `s3:ListBucket` on the target bucket only).

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
| `private_key` | `string` | ✅ | Ed25519 private key PEM in plaintext. Sent over TLS; encrypted at rest by digitize using the pod's encryption key before being stored in the DB. |

**`connection_details` — `s3`:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `region` | `string` | ✅ | AWS region where the bucket resides (e.g. `"us-east-1"`). |
| `bucket_name` | `string` | ✅ | Exact name of the S3 bucket containing the files (e.g. `"my-rag-documents"`). |
| `access_key_id` | `string` | ✅ | IAM Access Key ID used for API authentication. Stored in plaintext (not a secret). |
| `secret_access_key` | `string` | ✅ | IAM Secret Access Key in plaintext. Sent over TLS; encrypted at rest by digitize using the pod's encryption key before being stored in the DB. |

**Response:**

| Status | Body | Meaning |
| --- | --- | --- |
| `202 Accepted` | `{ "connector_id": "c7f3a2d1-..." }` | Connector row created; worker thread started asynchronously via `BackgroundTask`. |
| `409 Conflict` | Error detail | A connector with this `connector_id` already exists. Catalog should use `PUT` to update. |
| `401 Unauthorized` | Error detail | Missing or invalid bearer token. |

---

### B. `PUT /v1/connectors/{connector_id}` — Update an existing connector

**Purpose:** Called by catalog when the configuration of an existing connector changes — for example, credentials are rotated, the remote path or bucket changes, the sync interval is adjusted, or `allowed_extensions` is updated. Digitize waits for any in-progress sync tick to finish naturally, then stops the existing worker, applies the updated config via `upsert_active_connector`, and restarts a fresh worker with the new settings. The new worker begins its first tick immediately upon thread start.

**Thread stop/restart on PUT:** The handler calls `connector_worker_manager.stop_worker(connector_id, timeout=30.0)` on the old thread (cooperative stop via `stop_event.set()` + `thread.join()`). If a tick is currently running, `stop_event` does **not** interrupt it mid-flight — the tick runs to completion, then the thread exits at the top of its loop where it checks `stop_event`. Only after `thread.join()` returns does the handler call `start_worker(new_config)` to spawn a fresh thread with the updated configuration. The new thread picks up the updated `sync_interval_seconds` from its own `_config` dict — there is no shared scheduler state to update. Config changes take effect from the first tick of the new thread.

**When catalog calls it:** After any connector-level update is saved on the catalog side and re-validated (e.g. new key pair generated, new IAM keys rotated, or settings edited by the user). When credentials are included in the PUT payload, they are sent in plaintext over TLS and re-encrypted at rest by digitize using the pod's encryption key, exactly as on POST.

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
    "port":        2222,
    "username":    "new_user",
    "remote_path": "/exports/v2/reports",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n..."
  }
}
```

**Example — `s3` (key rotation only):**

```json
{
  "connection_details": {
    "access_key_id":     "AKIANEWKEYEXAMPLE",
    "secret_access_key": "newSecretAccessKeyValue"
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
| `private_key` | `string` | New Ed25519 private key PEM in plaintext (send only when credentials are rotated). Digitize re-encrypts at rest on receipt. |

**`connection_details` — `s3` (all optional):**

| Field | Type | Description |
| --- | --- | --- |
| `region` | `string` | New AWS region. |
| `bucket_name` | `string` | New bucket name. |
| `access_key_id` | `string` | New IAM Access Key ID. |
| `secret_access_key` | `string` | New Secret Access Key in plaintext (send only when credentials are rotated). Digitize re-encrypts at rest on receipt. |

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
   ← if a tick is running: _cancel_if_requested() fires at the next
     inter-phase boundary inside _run_tick() — the thread self-cleans:
       • scanner.close()  (all active connections closed)
       • delete_checksum_registry() for each in-flight pending-SHA row
       • delete_connector_membership_atomic() + DELETE /v1/documents/{doc_id}
         for every file that completed ingest during this tick
       • update_sync_history → "interrupted: connector deleted"
     then _run_tick() returns early and the thread exits cleanly.
     join() returns in seconds (not the full tick duration).
   ← if thread is still alive after timeout (tick blocked inside ingest()):
     log warning and continue — thread exits when ingest() unblocks;
     daemon=True ensures it is reaped at pod exit.

2. LOAD MEMBERSHIP SNAPSHOT
   known_hashes = list_connector_hashes(connector_id)
   ← returns set of sha256 values this connector holds from all previous ticks
   ← docs ingested during the interrupted tick have already been deleted by
     the thread's self-cleanup in step 1; their membership rows are gone.
     list_connector_hashes() therefore returns only pre-existing content.
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
      # This loop is idempotent with the thread's self-cleanup: any rows already
      # deleted in step 1 will be absent from known_hashes or produce remaining > 0.

4. DELETE active_connectors ROW
   delete_active_connector(connector_id)
   ← ON DELETE CASCADE removes any remaining connector_file_membership rows
     for this connector automatically (guards against any rows missed in step 3)
   ← file_checksum_registry rows are removed automatically via the doc_id FK CASCADE
     when their documents row is deleted; shared content survives until the last
     connector releases it

5. CLEANUP STAGING DIRECTORIES (best-effort)
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

Because step 4's `ON DELETE CASCADE` removes the registry unconditionally, a document-deletion failure means the document may remain in the VDB/storage but its registry row is gone. This is a known trade-off: a hard delete of the connector cannot be held hostage by a flaky document-deletion call. Operators can find and clean up orphaned documents via the `GET /v1/documents` listing filtered by `connector_id`.

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

Non-sensitive `connection_details` are returned so catalog can display configuration to the user. All credential fields (`private_key`, `secret_access_key`) are **never included** in the response.

**Example — `ssh_sftp`:**

```json
[
  {
    "connector_id":          "c7f3a2d1-...",
    "type":                  "ssh_sftp",
    "host":                  "sftp.example.com",
    "allowed_extensions":    [".pdf", ".docx"],
    "sync_interval_seconds": 300,
    "sync_status":           "completed",
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
    "sync_status":           "failed: Connection timed out while listing bucket objects.",
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

> **Security:** All credential fields (`private_key`, `secret_access_key`) are **never included** in the list response — they are stored encrypted at rest and are never returned over the API. For SSH connectors the **public key** is included — it is not a secret and catalog needs to display it to the user so they can authorise the key on the remote server. For S3 connectors the `access_key_id` is included (not a secret) but `secret_access_key` is withheld.

**Top-level fields:**

| Field | Type | Description |
| --- | --- | --- |
| `connector_id` | `string` | The connector's UUID. |
| `type` | `string` | Connector type: `"ssh_sftp"` or `"s3"`. |
| `host` | `string` | Primary server address. |
| `allowed_extensions` | `array[string]` | Active extension filter. |
| `sync_interval_seconds` | `integer` | Current sync interval. |
| `sync_status` | `string` | A human-readable message describing the last completed tick outcome. `"idle"` means no tick has run yet; `"running"` means a tick is currently in progress; `"completed"` means the last tick finished without errors; any other value describes what went wrong (e.g. `"2 files failed to ingest"`, `"failed: SFTP connection refused"`). |
| `last_sync_at` | `string (ISO 8601)` \| `null` | Timestamp of the last completed sync tick (success or failure). `null` if no tick has run yet. |
| `last_sync_error` | `string` \| `null` | Human-readable error message from the last failed sync tick. `null` when `sync_status` is `"idle"`, `"running"`, or `"completed"`. |
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

**Purpose:** Fetch the full current state of one connector by its UUID, including file-processing statistics from the most recent sync tick. Catalog uses this for per-connector detail views and to poll sync status.

**When catalog calls it:** On demand — connector detail page load, post-sync status refresh, or targeted health checks.

**Path parameters:**

| Parameter | Type | Description |
| --- | --- | --- |
| `connector_id` | `string (UUID)` | The connector to retrieve. |

**Response `200 OK` (`application/json`):**

The response includes the connector's current sync state, flat file-statistic fields for the most recently completed tick, and a `connection_details` object with non-secret connection fields. All credential fields (`private_key`, `secret_access_key`) are **always withheld** from the response — they are stored encrypted at rest and are never returned over the API.

**Response field definitions:**

| Field | Type | Description |
| --- | --- | --- |
| `sync_status` | `string` | A human-readable message describing the current or last tick outcome. `"Syncing"` while a tick is in progress; `"Completed"` when the last tick finished without errors; any other value describes what went wrong (e.g. `"2 files failed to ingest"`, `"Failed: SFTP connection refused"`). |
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

> **Note:** When `sync_status` is `"Failed"` and the scan itself did not complete, all file counters are `0`. Credential fields (`private_key`, `secret_access_key`) are never returned — they are stored encrypted at rest and are not surfaced through the API.

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
      "sync_status":  "2 files failed to ingest"
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
| `finished_at` | `string (ISO 8601)` \| `null` | UTC timestamp recorded the instant the tick body finishes. `null` when a tick is currently in progress (`sync_status = "syncing"`). |
| `files_found` | `integer` | Total files discovered on the remote source during this tick's scan phase. `0` if the scan did not complete. |
| `files_syncing` | `integer` | Number of files currently being downloaded or staged. Non-zero only while `sync_status = "syncing"`. |
| `files_completed` | `integer` | Number of files successfully ingested (or de-duplicated) during this tick. |
| `files_failed` | `integer` | Number of files that could not be processed due to download, staging, or ingest error during this tick. |
| `sync_status` | `string` | A human-readable message. `"syncing"` while in progress; `"completed"` on clean finish; a descriptive message otherwise (e.g. `"2 files failed to ingest"`, `"failed: SFTP connection refused"`). |

**`sync_status` message patterns:**

| Pattern | Meaning |
| --- | --- |
| `"syncing"` | Tick is currently in progress. `finished_at` is `null`; file counters reflect live progress. |
| `"completed"` | Tick finished with zero file errors and no fatal exception. |
| `"N files failed to ingest"` | Tick finished but N individual files failed to download, stage, or ingest. `files_failed > 0`. |
| `"N orphan deletes failed"` | Tick finished but N orphan document deletions encountered a hard error from the documents API. |
| `"failed: <reason>"` | The entire tick failed with a fatal exception (e.g. `"failed: SFTP connection refused"`, `"failed: S3 credential error"`) after all retries were exhausted. File counters are `0`. |

> **Note on in-progress rows:** A `"syncing"` row represents the currently running tick. There is at most one such row per connector at any time (enforced by the tick guard). If the pod is killed mid-tick the row remains with `sync_status = "syncing"` and `finished_at = null` indefinitely — this is the observable evidence of an interrupted tick. Startup recovery does **not** retroactively update orphaned `"syncing"` rows; callers should treat a `"syncing"` row with `started_at` older than `2 × sync_interval_seconds` as likely stale.

**Error responses:**

| Status | Condition |
| --- | --- |
| `404 Not Found` | No connector with `connector_id` is currently active in this pod. |

---

## 4. Database Schema

**Modified file:** `services/digitize/db/scripts/init_schema.sql`

Three new tables are added following the existing `IF NOT EXISTS` / idempotent DDL pattern already used in [`init_schema.sql`](../../services/digitize/db/scripts/init_schema.sql). All three tables also need corresponding SQLAlchemy ORM model classes added to [`db/models.py`](../../services/digitize/db/models.py), matching the `Job` / `Document` pattern there, so `Base.metadata.create_all()` on startup creates them automatically.

### 4.1 `active_connectors`

> **Security note:** There are **no plaintext secret columns**. All credential material (`private_key`, `secret_access_key`) is stored as AES-256-GCM ciphertext inside `connection_details` — encrypted at rest by the API handler using the pod's encryption key from `/run/secrets/connector_encryption_key` before the row is written. A Postgres breach alone exposes nothing without the pod encryption key.

The `connection_details` JSONB column stores all connector-type-specific fields — both non-sensitive config and encrypted credential blobs. The exact keys differ by `type`:

| `type` | Keys stored in `connection_details` |
| --- | --- |
| `ssh_sftp` | `port`, `username`, `remote_path`, `private_key` (AES-256-GCM ciphertext, base64) |
| `s3` | `region`, `bucket_name`, `access_key_id`, `secret_access_key` (AES-256-GCM ciphertext, base64) |

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
    CONSTRAINT chk_connector_type   CHECK (type IN ('ssh_sftp', 's3'))
);
```

### 4.2 `file_checksum_registry` and `connector_file_membership`

The original single `connector_file_checksums` table has been replaced by two purpose-built tables. This separation is required to support both sync change-detection and global content de-duplication (Decision D-16):

- **`file_checksum_registry`** — a global, content-addressed store keyed on `sha256`. There is exactly one row per unique piece of content, regardless of which connector or how many connectors have seen it. It is the authoritative source for "has this content been ingested, and what is its `doc_id`?".
- **`connector_file_membership`** — a per-connector membership table. Each row records that a specific connector currently holds a file with a given `sha256`. This is the only table scoped to a connector, and it is the source for orphan detection ("which hashes did this connector see last tick?").

#### 4.2.1 `file_checksum_registry`

```sql
CREATE TABLE IF NOT EXISTS file_checksum_registry (
    sha256        TEXT        PRIMARY KEY,
    doc_id        TEXT        NOT NULL UNIQUE REFERENCES documents(doc_id) ON DELETE CASCADE
);
```

`sha256` is the sole natural key — content identity is independent of source. `doc_id` is a FK to `documents(doc_id)` with `ON DELETE CASCADE`: if the document row is deleted (e.g. via `DELETE /v1/documents/{doc_id}`), the registry entry is automatically removed. The `UNIQUE` constraint prevents two registry entries from pointing at the same document.

#### 4.2.2 `connector_file_membership`

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

#### 4.2.3 Cross-connector de-duplication contract

When a new file is scanned by any connector, the sync worker checks `file_checksum_registry` for the computed `sha256` **before** downloading or ingesting:

- **Hash already in `file_checksum_registry`** — content has been ingested before (by any connector). Skip the download and ingest entirely. Insert a `connector_file_membership` row pointing to the existing `doc_id`. Zero bytes transferred, zero ingest pipeline cost.
- **Hash not in `file_checksum_registry`** — first time this content has been seen globally. Download, ingest, insert into `file_checksum_registry`, then insert into `connector_file_membership`.

This means `doc_id` is shared across connectors when the content is identical. A document is physically stored once and referenced by multiple connectors through their membership rows.

#### 4.2.4 Orphan detection and reference-counted deletion

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

#### 4.2.5 Lifecycle summary

| Event | `connector_file_membership` | `file_checksum_registry` | Document |
| --- | --- | --- | --- |
| New file, hash not seen globally | Insert `(connector_id, sha256)` | Insert `(sha256, doc_id)` | Ingested |
| New file, hash already exists globally | Insert `(connector_id, sha256)` | No change | Reused — no re-ingest |
| File disappears, other connectors still hold it | Remove this connector's row (txn) | No change | Survives |
| File disappears, last connector to hold it | Remove row (txn), count = 0 | Delete row | Deleted |
| Connector deleted via `DELETE /v1/connectors` | CASCADE removes all membership rows | No change | Survives if any other connector holds the hash |

### 4.3 SQLAlchemy ORM Models

Add `ActiveConnector`, `FileChecksumRegistry`, `ConnectorFileMembership`, and `ConnectorSyncHistory` classes to `db/models.py` following the `Job` / `Document` pattern — `DeclarativeBase`, `Mapped` columns, `ForeignKey`, `CheckConstraint`, and `Index`. `FileChecksumRegistry.doc_id` maps to `ForeignKey("documents.doc_id", ondelete="CASCADE")` to mirror the DDL FK. This ensures `Base.metadata.create_all(bind=engine)` in the startup lifespan creates these tables alongside the existing `jobs` and `documents` tables.

---

### 4.4 `connector_sync_history`

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
    -- sync_status is a free-form message string (e.g. 'syncing', 'completed',
    -- '2 files failed to ingest', 'failed: SFTP connection refused').
    -- No CHECK constraint is applied so the message content is unrestricted.
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
| `sync_status` | Starts as `'syncing'` on INSERT. Updated to a human-readable message in the final UPDATE at tick end: `'completed'` on clean finish, `'N files failed to ingest'` if per-file errors occurred, or `'failed: <error>'` if the whole tick raised a fatal exception. |

**Index `idx_csh_connector_started`:** Supports the history query (`ORDER BY started_at DESC` + `WHERE connector_id = $1`) efficiently without a full-table scan. Composite index on `(connector_id, started_at DESC)` covers both the filter and the sort.

---

## 5. Database Operations Layer

**Modified file:** `services/digitize/utils/db.py`

New CRUD functions appended to the existing module. These functions operate on **ciphertext blobs only** — they never decrypt or re-encrypt. Encryption at rest is performed by the API handler before calling these functions; decryption is the exclusive responsibility of the scanner (`SFTPScanner` or `S3Scanner`) at sync time.

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

## 6. Scanner Interface

**New file:** `services/digitize/connector/base_scanner.py`

Both `SFTPScanner` and `S3Scanner` share an identical logical contract with `ConnectorSyncWorker`: decrypt credentials, walk a remote source, compute SHA-256 per file, produce a diff list, and stream files to a local staging path. Only the transport layer (paramiko vs boto3) and the credential field name differ. The `BaseScanner` ABC codifies this contract so that `ConnectorSyncWorker` is completely unaware of the underlying transport type.

### 6.1 Design Rationale

**Key principle:** `ConnectorSyncWorker` calls only `scanner.connect()`, `scanner.scan()`, `scanner.download_to()`, and `scanner.close()`. All transport logic is fully encapsulated inside the concrete subclass. The `_decrypt_credential()` helper is the one piece of shared *implementation* — the AES-256-GCM decrypt step using the pod encryption key is identical for both connector types; only what is done with the resulting plaintext afterwards differs.

Adding a third connector type (e.g. `sharepoint`) requires only: a new `SharePointScanner(BaseScanner)` file and a single dict entry in `scanner.py`'s `_REGISTRY`. No changes to `sync_worker.py` or any other file.

### 6.2 Class Hierarchy

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  connector/                                                                  │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐  │
│  │  base_scanner.py                                                       │  │
│  │                                                                        │  │
│  │  @dataclass RemoteFile                                                 │  │
│  │    checksum: str          # hex SHA-256                                │  │
│  │    last_modified: float   # POSIX timestamp; used to sort diff list    │  │
│  │                                                                        │  │
│  │  class BaseScanner(ABC)                        «abstract»              │  │
│  │    ├── @abstractmethod  connect() → None                               │  │
│  │    ├── @abstractmethod  close() → None                                 │  │
│  │    ├── @abstractmethod  scan(known_hashes) → tuple[list, set]          │  │
│  │    ├── @abstractmethod  download_to(remote_path, local_path) → None    │  │
│  │    └── _decrypt_credential(ciphertext_b64) → str  # shared, not abs   │  │
│  └────────────────────────────────────────────────────────────────────────┘  │
│                  ▲ inherits                            ▲ inherits            │
│  ┌───────────────┴────────────────┐    ┌──────────────┴─────────────────┐   │
│  │  sftp_scanner.py               │    │  s3_scanner.py                  │   │
│  │                                │    │                                  │   │
│  │  SFTPScanner(BaseScanner)      │    │  S3Scanner(BaseScanner)          │   │
│  │    connect() → paramiko SFTP   │    │    connect() → boto3 S3 client   │   │
│  │    close()   → sftp + ssh      │    │    close()   → set _s3 = None    │   │
│  │    scan()    → DFS listdir_attr│    │    scan()    → list_objects_v2   │   │
│  │    download_to() → sftp.open() │    │    download_to() → download_obj  │   │
│  └────────────────────────────────┘    └─────────────────────────────────┘   │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────────┐ │
│  │  scanner.py                                                             │ │
│  │                                                                         │ │
│  │  _REGISTRY = { "ssh_sftp": SFTPScanner, "s3": S3Scanner }              │ │
│  │  build_scanner(config: dict) → BaseScanner                             │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                │ dispatches to                                               │
│  ┌─────────────▼───────────────────────────────────────────────────────────┐ │
│  │  sync_worker.py   ConnectorSyncWorker                                   │ │
│  │    scanner = build_scanner(config)    # type: BaseScanner only          │ │
│  │    scanner.connect()                                                    │ │
│  │    to_ingest, orphans = scanner.scan(known_hashes)                      │ │
│  │    scanner.download_to(f.path, staging_path)                            │ │
│  │    scanner.close()                                                      │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────────┘
```

### 6.3 BaseScanner Contract

```python
from abc import ABC, abstractmethod
from dataclasses import dataclass
from pathlib import Path


@dataclass
class RemoteFile:
    checksum: str         # hex SHA-256
    last_modified: float  # POSIX timestamp; used to sort the diff list


class BaseScanner(ABC):
    """
    Abstract contract every connector scanner must implement.
    ConnectorSyncWorker only ever talks to this interface.
    """

    def __init__(self, config: dict) -> None:
        self._config = config
        self._allowed_extensions: set[str] = set(config["allowed_extensions"])

    # ── ABSTRACT (transport-specific) ─────────────────────────────────────

    @abstractmethod
    def connect(self) -> None:
        """Open session; decrypt credentials fresh on each call (D-6)."""

    @abstractmethod
    def close(self) -> None:
        """Close session; zeroize any in-memory credential bytes."""

    @abstractmethod
    def scan(
        self, known_hashes: set[str]
    ) -> tuple[list[RemoteFile], set[str]]:
        """
        Walk the remote source, filter by allowed_extensions, compute SHA-256.

        Returns:
            to_ingest:     files whose hash is new to this connector.
            orphan_hashes: hashes present in known_hashes but absent from the walk.
        """

    @abstractmethod
    def download_to(self, remote_path: str, local_path: Path) -> None:
        """Stream a single remote file to a local staging path."""

    # ── SHARED (concrete implementation, not overridden) ──────────────────

    def _decrypt_credential(self, ciphertext_b64: str) -> str:
        """
        Shared credential decryption using the pod's encryption key (AES-256-GCM).
        Reads /run/secrets/connector_encryption_key fresh on each call (D-6).
        Returns the plaintext credential string; caller is responsible for zeroizing.
        """
        enc_key_path = self._config.get(
            "encryption_key_path", "/run/secrets/connector_encryption_key"
        )
        with open(enc_key_path, "rb") as fh:
            enc_key = fh.read()
        return _aes_gcm_decrypt(enc_key, ciphertext_b64).decode()  # shared util

    # ── Optional context manager protocol ─────────────────────────────────

    def __enter__(self):
        self.connect()
        return self

    def __exit__(self, *_):
        self.close()
```

`__enter__`/`__exit__` are concrete on `BaseScanner` so `sync_worker.py` can use a `with build_scanner(config) as scanner:` block, guaranteeing `close()` is called even if an exception escapes mid-tick. `close()` remains callable standalone as well for callers that manage the lifecycle explicitly.

### 6.4 Shared vs Per-Implementation

| Piece | Lives in | Shared? | Notes |
| --- | --- | --- | --- |
| `RemoteFile` dataclass | `base_scanner.py` | **Shared** | One definition; both scanners produce it |
| Credential decryption (`_decrypt_credential`) | `base_scanner.py` | **Shared** | Identical AES-256-GCM decrypt using pod encryption key, regardless of connector type |
| `allowed_extensions` set construction | `BaseScanner.__init__` | **Shared** | Both scanners filter on the same list from config |
| Context manager (`__enter__`/`__exit__`) | `base_scanner.py` | **Shared** | Guarantees `close()` is called even on tick exceptions |
| Membership diff (orphan detection) | `sync_worker.py` | **Shared** | `scan(known_hashes)` returns `(to_ingest, orphan_hashes)`; worker does the diff — identical for both types |
| Sort by `last_modified` DESC, batch into 10s | `sync_worker.py` | **Shared** | Pure Python list logic on `RemoteFile` |
| Staging dir layout / cleanup | `sync_worker.py` | **Shared** | `shutil.rmtree` on the same staging path pattern |
| DB dedup + membership writes | `utils/db.py` + worker | **Shared** | Same `file_checksum_registry` + `connector_file_membership` calls |
| Tick guard, sync history write | `sync_worker.py` | **Shared** | Independent of transport |
| `connect()` — session open + credential decrypt + transport init | `SFTPScanner` / `S3Scanner` | **Per-impl** | SFTP: `enc_key → privkey_pem → paramiko`; S3: `enc_key → secret_key → boto3` |
| `close()` — session teardown + credential zeroization | `SFTPScanner` / `S3Scanner` | **Per-impl** | SFTP: `sftp.close()` + `ssh.close()`; S3: set `_s3 = None` |
| `scan()` — remote walk + streaming SHA-256 | `SFTPScanner` / `S3Scanner` | **Per-impl** | SFTP: `listdir_attr` recursive DFS; S3: `list_objects_v2` paginator |
| `download_to()` — file streaming to staging | `SFTPScanner` / `S3Scanner` | **Per-impl** | SFTP: `sftp.open().read(65536)`; S3: `s3.download_fileobj()` |
| Scanner instantiation | `scanner.py` factory | **Factory only** | `build_scanner(config)` dispatches on `config["type"]` |

### 6.5 Per-Tick Sequence

The sequence below shows a single tick from `ConnectorSyncWorker`'s perspective. The worker calls only the four abstract methods; it never branches on connector type.

```
ConnectorSyncWorker          BaseScanner           SFTPScanner / S3Scanner
─────────────────────────    ──────────────────    ──────────────────────────
STEP 0: insert_sync_history()
        │
STEP 1: build_scanner(config) ──────────────────► factory dispatches:
        │                                          type=ssh_sftp → SFTPScanner(config)
        │                                          type=s3       → S3Scanner(config)
        │                     ◄── : BaseScanner ──
        │
        scanner.connect() ───────────────────────► _decrypt_credential()   [shared]
        │                                          then:
        │                                          ssh_sftp → privkey_pem → paramiko
        │                                          s3       → secret_key → boto3
        │
STEP 2: scanner.scan(known_hashes) ──────────────► ssh_sftp → DFS listdir_attr + SHA-256
        │                     ◄── (to_ingest,      s3       → list_objects_v2 + SHA-256
        │                          orphan_hashes)
        │
        [dedup pass — worker, no scanner call]
        │
STEP 3: loop over to_ingest batches:
        │  scanner.download_to(path, staging) ───► ssh_sftp → sftp.open().read(65536)
        │                                          s3       → s3.download_fileobj()
        │  [ingest + membership writes — worker]
        │
STEP 4: [orphan deletes — worker, no scanner call]
        │
STEP 5: scanner.close() ─────────────────────────► ssh_sftp → sftp.close() + ssh.close()
        │                                          s3       → _s3 = None
        update_sync_history(done)
```

### 6.6 Credential Decryption Flow

Both connector types use the same single-step decrypt: AES-256-GCM with the pod's encryption key read from `/run/secrets/connector_encryption_key`. They diverge only in what the resulting plaintext is used for, which is what `connect()` is responsible for per-implementation.

```
/run/secrets/connector_encryption_key   (32-byte key, Podman secret mount)
     │
     │  _decrypt_credential(ciphertext_b64)  ←  shared method on BaseScanner
     │  AES-256-GCM decrypt(ciphertext_b64, encryption_key)
     ▼
plaintext credential  (string, in-memory, not retained between ticks)
     │
     ├─── ssh_sftp path (SFTPScanner.connect) ──────────────────────────────────┐
     │    ciphertext = connection_details["private_key"]                         │
     │    → privkey_pem (Ed25519 PEM string, in-memory only)                     │
     │    → paramiko.Ed25519Key.from_private_key(io.StringIO(privkey_pem))       │
     │    → privkey_pem overwritten with "\x00" * len(privkey_pem)  (zeroized)   │
     │    → paramiko SSHClient → open SFTP session                               │
     └───────────────────────────────────────────────────────────────────────────┘
     │
     └─── s3 path (S3Scanner.connect) ─────────────────────────────────────────┐
          ciphertext = connection_details["secret_access_key"]                  │
          → secret_access_key (string, in-memory only)                          │
          → boto3.client("s3", aws_access_key_id=...,                           │
                         aws_secret_access_key=secret_access_key, ...)          │
          → secret_access_key overwritten with "\x00" * len(...)  (zeroized)    │
          → boto3 S3 client (no persistent TCP session held)                    │
          └────────────────────────────────────────────────────────────────────┘
```

**Credential lifetime:** The decrypted plaintext is never stored on `self`. It is a local variable inside `connect()` and falls out of scope immediately after the transport session is established. The encryption key itself is read fresh from the secret mount on every call to `_decrypt_credential()`, keeping its residence time in memory minimal (Decision D-6).

### 6.7 Factory — scanner.py

`connector/scanner.py` is the only file that knows the concrete class names. All other modules import only `BaseScanner` or call `build_scanner()`.

```python
from .base_scanner import BaseScanner
from .sftp_scanner import SFTPScanner
from .s3_scanner   import S3Scanner

_REGISTRY: dict[str, type[BaseScanner]] = {
    "ssh_sftp": SFTPScanner,
    "s3":       S3Scanner,
}

def build_scanner(config: dict) -> BaseScanner:
    connector_type = config["type"]
    cls = _REGISTRY.get(connector_type)
    if cls is None:
        raise ValueError(f"Unknown connector type: {connector_type!r}")
    return cls(config)
```

`ConnectorSyncWorker` calls `build_scanner(self._config)` at the start of each tick (step 1 in §8.1). The returned object is typed as `BaseScanner` — the worker never inspects the concrete type again.

---

## 7. SFTP Scanner

**New file:** `services/digitize/connector/sftp_scanner.py`

Handles all in-memory cryptographic operations and the paramiko SFTP session for `ssh_sftp` connectors. This is one of two components in digitize that reads `/run/secrets/connector_encryption_key` (the other being `S3Scanner`).

### 7.1 Decryption Chain (per tick, not cached)

All credential fields are read from `connection_details` (the JSONB column), not from top-level columns.

```
/run/secrets/connector_encryption_key   (32-byte key, Podman secret mount)
     │
     │  AES-256-GCM decrypt(connection_details["private_key"], encryption_key)
     ▼
privkey_pem  (Ed25519 PEM string, in-memory only)
     │
     │  paramiko.Ed25519Key.from_private_key(io.StringIO(privkey_pem))
     │  privkey_pem string overwritten immediately after key load
     ▼
paramiko SSHClient  →  open SFTP session
     │
     ▼  close() — plaintext credential not retained
```

**Key zeroization:** After calling `from_private_key()`, the `privkey_pem` variable is overwritten with `"\x00" * len(privkey_pem)` and set to `None`. The plaintext credential is not retained between ticks — decrypted fresh on every tick to minimise in-memory exposure.

**Encryption key load timing (Decision D-6 — recommended):** The encryption key is read fresh inside `SFTPScanner.connect()` on every tick. This keeps encryption key residence time in memory minimal and allows future key rotation without a pod restart.

### 7.2 File Scanning — Streaming SHA-256, Filtered by Extension

File type filtering is applied at scan time using `allowed_extensions` from the connector config. This avoids SFTP transfers for files the ingest pipeline would reject. The extension list is populated by catalog (Decision D-8).

`scan()` performs four phases:

1. **Walk** — recursively enumerate every regular file under `remote_path`.
2. **Filter + Hash** — skip files whose extension is not in `allowed_extensions`; compute a streaming SHA-256 for every file that passes the filter. File identity is purely content-based — `remote_path` is used only as a walk cursor and is never stored.
3. **Membership diff** — compare each computed hash against `known_hashes` (the set of `sha256` values this connector held at the start of the tick, from `list_connector_hashes`):
   - **Hash in `known_hashes`** → file is unchanged; skip it entirely.
   - **Hash not in `known_hashes`** → file is new to this connector; add it to `to_ingest`.
4. **Orphan detection** — any hash in `known_hashes` that was *not* seen in the walk (the file was deleted or renamed on the remote) is added to `orphan_hashes`. These are handled by the sync worker after the scan returns (§8.1 step 4).

Returns `(to_ingest, orphan_hashes)`.

```python
@dataclass
class RemoteFile:
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

### 7.3 File Download for Staging

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

Each connector's worker thread executes the following flow on every tick. Steps are sequential and blocking on the worker thread.

```
┌──────────────────────────────────────────────────────────────────────────┐
│  ConnectorSyncWorker  —  one tick                                        │
└──────────────────────┬───────────────────────────────────────────────────┘
                       │
          ┌────────────▼────────────┐
          │  TICK GUARD             │
          │  already running?       │
          └────────────┬────────────┘
               YES │   │ NO
                   │   ▼
                   │  ┌─────────────────────────────────────┐
                   │  │  0. Record tick start               │
                   │  │     insert_sync_history → syncing   │
                   │  └──────────────┬──────────────────────┘
                   │                 │
                   │  ┌──────────────▼──────────────────────┐
                   │  │  1. Connect & Scan                  │
                   │  │     enc_key → credential            │
                   │  │     walk remote; SHA-256 per file   │
                   │  └──────────────┬──────────────────────┘
                   │                 │ (to_ingest, orphan_hashes)
                   │  ┌──────────────▼──────────────────────┐
                   │  │  2. Dedup Pass                      │
                   │  │     for each file in to_ingest:     │
                   │  │       hash in registry?             │
                   │  │         YES → add membership only   │
                   │  │         NO  → add to needs_ingest   │
                   │  └──────────────┬──────────────────────┘
                   │                 │ (needs_ingest)
                   │  ┌──────────────▼──────────────────────┐
                   │  │  3. Batch Ingest                    │
                   │  │     pre-write pending checksums     │
                   │  │     for each batch of 10 files:     │
                   │  │       create batch tmp dir          │
                   │  │       download files → tmp dir      │
                   │  │       create job + register docs    │
                   │  │       ingest() ← blocking           │
                   │  │       update registry + membership  │
                   │  │       clean up tmp dir              │
                   │  └──────────────┬──────────────────────┘
                   │                 │
                   │  ┌──────────────▼──────────────────────┐
                   │  │  4. Orphan Deletes                  │
                   │  │     for each hash in orphan_hashes: │
                   │  │       delete membership (atomic txn)│
                   │  │       if last ref → delete document │
                   │  └──────────────┬──────────────────────┘
                   │                 │
                   │  ┌──────────────▼──────────────────────┐
                   │  │  5. Close Connection                │
                   │  │     scanner.close()                 │
                   │  │     zeroize in-memory creds         │
                   │  └──────────────┬──────────────────────┘
                   │                 │
                   │  ┌──────────────▼──────────────────────┐
                   │  │  6. Finalise                        │
                   │  │     build sync_status message       │
                   │  │     update_sync_history → done      │
                   │  └──────────────┬──────────────────────┘
                   │                 │
          skip ◄───┘                 ▼
                        ┌────────────────────────┐
                        │  sleep(interval) or    │
                        │  wake on stop_event    │
                        └────────────────────────┘
```

**Fatal error path (connection failure, credential error, unexpected exception):** Any unhandled exception from steps 1–4 is caught and logged, the history row is closed with `sync_status = "failed: <error message>"`, the connector status is updated to `"failed: <error message>"`, and the worker loop continues normally to sleep the full `sync_interval_seconds` before the next tick. The `finally` block resets `_tick_running = False` unconditionally.

**Step 3 — SHA cleanup invariant:** A pending checksum row (with `NULL` `doc_id`) is pre-written for every file before download begins. If download or ingest fails for a file, its pending SHA row is deleted immediately so the next tick re-detects the file as new and retries it cleanly.

**Step 6 — `sync_status` message:** `sync_status` is a free-form string, not a fixed enum. Examples: `"completed"`, `"2 files failed to ingest"`, `"failed: SFTP connection refused"`. This allows callers to display a human-readable outcome without needing a separate error field. See §8.4 for the full outcome-to-message mapping.

**Ordering guarantee:** Files in `to_ingest` are sorted by `last_modified` descending before batching, so the most-recently-modified content reaches the ingestion pipeline first. The ordering is applied once after the full scan/diff and is consistent across all batches of the tick.

**Job naming:** Each batch's job name is `"{connectorID}-{syncID}-{batchCount}"` — for example, a connector with `id = "abc123"` running its 7th tick will produce jobs named `"abc123-7-1"`, `"abc123-7-2"`, etc. The `sync_id` is stable per tick so it ties every batch job back to a single, auditable tick record in `connector_sync_history`.

**tmp dir per batch (batch ingest step 3):** A dedicated `batch_tmp_dir` under `/tmp/` is created immediately before each batch is processed. Files are downloaded directly into that directory — no intermediate staging dir exists. The pipeline operates exclusively on the tmp copy. Each `batch_tmp_dir` is torn down via `shutil.rmtree()` immediately after `ingest()` returns for that batch, regardless of success or failure.

**SHA cleanup invariant:** A pending checksum row (with `NULL` `doc_id`) is pre-written for every file in `needs_ingest` before any download begins. This row is the "in-flight" marker. The invariant is: **if a file does not reach a successful ingest completion, its pending SHA row must be deleted before the tick ends.** The two failure points that enforce this are:

- **Download failure:** `delete_checksum_registry(sha256)` called immediately on the per-file download exception.
- **Ingest failure:** `delete_checksum_registry(sha256)` called for each file the pipeline marks as FAILED.

In both cases the file's SHA is absent from `file_checksum_registry` at tick end. The next tick's scan will recompute the same SHA, find it absent from `known_hashes`, and re-add the file to `to_ingest` — giving the file a clean retry without any manual intervention.

### 8.2 Tick Guard — Skipping Overlapping Ticks

If the previous tick's sync process (SFTP walk + staging + ingest) is still running when the next **scheduled** interval fires, the new tick is **skipped entirely**. No second worker is spawned; the timer simply resets for the next interval. There is no manual trigger mechanism — ticks fire only on the schedule.

**Connector deletion** (`DELETE /v1/connectors`) is the only path that can interrupt a running tick. It sets `stop_event`, which `_run_tick()` checks via `_cancel_if_requested()` at each **inter-phase boundary** (post-scan, post-dedup, post-each-batch, post-orphans). When the event is detected, the tick self-cleans — closes connections, purges in-flight SHA rows, deletes OpenSearch documents for already-ingested files — then returns early, allowing the thread to exit without completing the remainder of the tick. See §14.5 for the full interrupt protocol.

**`PUT /v1/connectors` while a tick is running:** The PUT handler calls `stop_worker()`, which sets `stop_event` and calls `thread.join()`. The running tick is **not interrupted** — it runs to completion, then the thread exits because `stop_event` is set. Only after `join()` returns does the handler start a fresh worker with the new config. Config changes therefore take effect from the first tick of the new thread, never mid-tick.

#### Flow — Scheduled interval fires while previous tick still running (skip)

```
[Worker thread — single timeline]
────────────────────────────────────────────────────────────────
_tick_running = True
┌────────────────────────────────────────────────────────────┐
│  _run_tick()  ← still executing (long scan / slow ingest)  │
│                                                            │
│  (scheduled interval timer would have fired here)         │
│                                                            │
└────────────────────────────────────────────────────────────┘
while not stop_event.is_set():
  if _tick_running:          ← True
    log "previous tick still running, skip"
    stop_event.wait(interval)   ← wait a full interval then re-check
    continue                    ← no tick run, no history row written
```

```python
class ConnectorSyncWorker:
    def __init__(self, connector_id: str, connector_config: dict,
                 stop_event: threading.Event) -> None:
        self.connector_id = connector_id
        self._config = connector_config
        self._stop_event = stop_event
        self._tick_running = False    # ← guarded on the single worker thread
```

### 8.3 Tmp Layout

There is no persistent staging directory. Files are downloaded directly into a short-lived per-batch tmp directory, created immediately before the batch job is submitted and wiped immediately after `ingest()` returns.

```
/tmp/
  connector-<connector_id>-<sync_id>-batch1/  ← created just before batch 1 job; wiped after ingest()
    subdir/
      report.pdf
  connector-<connector_id>-<sync_id>-batch2/  ← created just before batch 2 job; wiped after ingest()
    summary.docx
```

**Lifecycle per batch:**

1. `batch_tmp_dir` created under `/tmp/`.
2. Files for this batch downloaded directly into `batch_tmp_dir`, preserving remote sub-path structure for traceability.
3. Job created and documents registered against `batch_tmp_dir`.
4. `ingest()` called — blocking.
5. `shutil.rmtree(batch_tmp_dir)` — unconditional, in a `finally` block, so the dir is always removed whether ingest succeeds or fails.

No tick-level staging directory is created or maintained. The existing `cleanup_staging_directory` utility in `common.misc_utils` can be reused for the per-batch tmp dir teardown.

### 8.4 Error Handling Matrix

| Scenario | Action | `sync_status` message | Checksum record |
| --- | --- | --- | --- |
| Download of new/modified file fails | Log + skip file; file not staged; `delete_checksum_registry(sha256)` called | `"N files failed to ingest"` (if any failures) | **Removed** — next tick re-detects the file |
| Download to tmp fails (I/O error writing to `batch_tmp_dir`) | Log + skip file; file not passed to pipeline; `delete_checksum_registry(sha256)` called | `"N files failed to ingest"` (if any failures) | **Removed** — next tick re-detects the file |
| Ingest of staged file fails | `pipeline.ingest` marks doc as FAILED in Job; log; `delete_checksum_registry(sha256)` called | `"N files failed to ingest"` | **Removed** — next tick re-detects and retries the file |
| Modified file: delete old doc fails (already absent) | Treat as success; proceed with ingest | `"completed"` | Updated after ingest |
| Deleted file: VDB delete → doc already absent | Treat as success | `"completed"` | Deleted |
| Deleted file: VDB hard error | Log; keep checksum for retry next tick | `"N orphan deletes failed"` | Retained |
| SFTP connection failure (whole tick) | Catch exception; log error; history row closed with `"failed: <error>"`; sleep full interval then next tick | `"failed: <error>"` | Unchanged |
| S3 credential/bucket error (whole tick) | Catch exception; log error; history row closed with `"failed: <error>"`; sleep full interval then next tick | `"failed: <error>"` | Unchanged |
| Unexpected exception (whole tick) | Catch exception; log error; history row closed with `"failed: <error>"`; sleep full interval then next tick | `"failed: <error>"` | Unchanged |
| Tick fired while previous tick running (scheduled interval) | Skip — log info; reset timer | (no row written) | Unchanged |

### 8.5 Blocking Semantics of File Download & Ingest

All work inside a single tick — including file downloads and the `ingest()` call (both sub-steps of step 3, Batch Ingest) — executes **synchronously and sequentially on the connector's dedicated worker thread**. There is no internal thread pool spawned for downloads, and `ingest()` is called as a blocking function, not scheduled as a background coroutine.

**Why blocking is correct here:**

- **The tick is an atomic unit.** The tick guard (`self._tick_running`) relies on the entire tick — scan, download, ingest — being a single sequential operation. If downloads were parallelised into sub-threads, the guard logic would need to coordinate across them, adding complexity with no correctness benefit for a single-connector worker.
- **Each connector already has its own thread.** That thread's time is entirely dedicated to one connector. There is no other useful work it could be doing concurrently for the same connector; blocking is the correct model.
- **The GIL is not a factor during I/O.** Although downloads and ingest run on the thread synchronously, the Python GIL is released during every network call: paramiko socket reads/writes (SFTP), boto3 HTTP calls (S3), and psycopg2 queries (Postgres). This means all other connector worker threads and the Uvicorn API handler threads continue running concurrently in the OS while a download or ingest is in progress — the blocking is local to that one worker thread, not to the whole process.

**Is a process better for blocking I/O?** No. A `multiprocessing.Process` would move the blocking work to a separate OS process, but the GIL is already irrelevant for I/O — the process boundary adds memory overhead and IPC complexity without enabling any parallelism that threads don't already provide. See §9.1 for the full analysis (§9 is the Worker Manager section).

**Stuck-download edge case:** If an SFTP or S3 download hangs indefinitely (network partition, remote server stall), the worker thread will remain blocked on the socket read. The tick guard prevents a second tick from starting. The `paramiko` and `boto3` clients both honour socket-level timeouts configurable at construction time — these should be set to a reasonable value (e.g. 60 s) in `SFTPScanner` and `S3Scanner` to bound the maximum tick duration and ensure `stop_event` is eventually checked. See §8.4 (Error Handling Matrix) for the per-scenario recovery policy.


### 8.6 Sync History — Insert/Update Logic and Corner Cases

This section enumerates precisely when a `connector_sync_history` row is created or mutated, and how every edge case is handled.

#### Normal tick lifecycle (two writes)

| Phase | DB call | What is written |
| --- | --- | --- |
| **Tick start** (step 0, before any scan) | `insert_sync_history(connector_id, started_at)` | New row: `sync_status='syncing'`, `files_*=0`, `finished_at=NULL`. `sync_id = MAX(sync_id)+1` for this connector. |
| **After scan** (step 1) | `update_sync_history_files_syncing(connector_id, sync_id, len(to_ingest))` | Live `files_syncing` snapshot showing how many files need processing. |
| **Dedup/ingest progress** (steps 2–3) | `update_sync_history_files_syncing(…, remaining_to_process)` | Decrements `files_syncing` as each file is resolved (deduped or downloaded). |
| **Tick end** (step 6) | `update_sync_history(connector_id, sync_id, finished_at=now(), files_found, files_syncing=0, files_completed, files_failed, sync_status)` | Final state. `files_syncing` forced to `0`. `sync_status` set to `'completed'`, `'N files failed to ingest'`, or `'failed: <error>'`. |

#### Corner cases

**1. Fatal exception before scan completes (connection refused, credential error)**

The tick body raises before step 1 (scan) completes. The `except` block writes the final UPDATE with all counters at `0` and `sync_status='failed: <error message>'`. The row is not left in `'syncing'` indefinitely — even a whole-tick failure closes the history row cleanly.

```
insert_sync_history → row: syncing / 0 / 0 / 0 / 0 / NULL
... connect() raises SSHException / S3ScannerError ...
update_sync_history → row: "failed: SFTP connection refused" / 0 / 0 / 0 / 0 / finished_at=now()
```

**2. Fatal exception mid-tick (after scan, during download or ingest)**

`files_found` was computed after `scanner.close()`. If the exception is caught, the except block writes `files_found=0` because `files_found` may not yet be bound in the local scope if the exception was raised very early (before `files_found` was assigned). To ensure correctness, `files_found` defaults to `0` at the top of the tick body; it is overwritten only after the scan phase completes. If it remains `0` in the except block, the history row will reflect `0` — consistent with the scan not completing.

**3. Pod killed mid-tick (SIGKILL / OOM)**

If the process is killed between the `insert_sync_history` INSERT and the final `update_sync_history` UPDATE, the row is left with `sync_status='syncing'` and `finished_at=NULL` permanently. This is intentional — it is the only observable signal that the pod was interrupted mid-tick. On pod restart, the startup recovery code does **not** attempt to close orphaned `'syncing'` rows; doing so would require knowing what the final file counts should have been, which is impossible without re-running the scan. Callers reading the history endpoint should treat any `'syncing'` row whose `started_at` is older than `2 × sync_interval_seconds` as a likely stale/interrupted row.

**4. Tick skipped by tick guard**

When `_tick_running` is `True` and the guard fires (§8.2), the tick body is not entered at all. No `insert_sync_history` call is made. Skipped ticks produce **no history row** — a gap in `sync_id` values is not possible, and the sequence remains contiguous. Only actually-started ticks appear in the history.

**5. `sync_id` sequence continuity across pod restarts**

`sync_id` is computed as `COALESCE(MAX(sync_id), 0) + 1` from `connector_sync_history` at tick start. Since the table persists in Postgres across pod restarts, the counter continues from the last recorded value. A pod restart followed by connector recovery will produce `sync_id = N+1` where `N` was the last `sync_id` before the restart — no gaps, no resets.

**6. Connector deleted while a tick is in progress**

If `DELETE /v1/connectors/{id}` is called while a tick is actively running for that connector, the DELETE handler signals `stop_event` and waits for the worker thread to exit (per §3C and §14.5). The tick detects the event at its next inter-phase boundary via `_cancel_if_requested()`, which:

1. Closes all active connections.
2. Deletes any in-flight pending-SHA rows (pre-written `NULL doc_id` entries).
3. Issues `DELETE /v1/documents/{doc_id}` for every file that completed ingest during this tick, and removes their membership rows via `delete_connector_membership_atomic`.
4. Writes a final `update_sync_history` with `sync_status = "interrupted: connector deleted"`.

After this self-cleanup, `_run_tick()` returns early. The `finally` block resets `_tick_running = False`; the `while not stop_event.is_set()` condition is `False`; the thread exits cleanly.

The DELETE handler's `thread.join()` returns (in seconds, not the full tick duration). The §3C step 3 membership loop then runs for any content ingested in **previous ticks** — those rows are still in `connector_file_membership` and require the same reference-counted document-deletion treatment. The in-tick cleanup and the §3C loop are idempotent: `delete_connector_membership_atomic` is transactional, and the handler loop's `remaining > 0` / `404` checks treat already-deleted rows as no-ops.

The sync-history row is closed by `_cancel_if_requested()` before the thread exits. The subsequent `ON DELETE CASCADE` triggered by `delete_active_connector()` (§3C step 4) removes it along with all remaining history rows — whether the thread wrote the row first or the cascade fires first, the result is the same: no orphaned history rows.

**7. Connector re-registered after deletion (re-push from catalog)**

If catalog calls `POST /v1/connectors` with the same `connector_id` as a previously deleted connector (e.g. reconfiguring an SFTP source), `ON DELETE CASCADE` will have removed all previous history rows. The `sync_id` sequence restarts from `1` for the re-registered connector, as `MAX(sync_id)` returns `NULL` → `COALESCE(NULL, 0) + 1 = 1`.

---

## 9. Worker Manager

**New file:** `services/digitize/connector/worker_manager.py`

A module-level singleton, following the same pattern as the existing [`workers/concurrency.py`](../../services/digitize/workers/concurrency.py) `ConcurrencyManager`.

### 9.1 Design Decision — Background Thread

Background threads (`threading.Thread`) were chosen over separate processes (`multiprocessing.Process`) for sync workers. The sync workload is entirely I/O-bound — time is spent in paramiko socket transfers, SFTP reads, and psycopg2 queries — so the Python GIL is released for all meaningful work and offers no disadvantage. Threads are dramatically cheaper (~8 KB stack vs ~15–50 MB per process), start in microseconds rather than tens of milliseconds, and share the DB connection pool, settings, and `connector_worker_manager` registry directly with no IPC overhead. The main arguments for processes — crash isolation and forceful kill — do not apply here: the worker is pure Python calling well-tested I/O libraries with no native crash-inducing paths, and socket-level timeouts in the scanners bound the maximum tick duration to a recoverable condition. Using `multiprocessing` with `fork` is also explicitly unsafe alongside SQLAlchemy connection pools and asyncio, and `spawn` would require full resource re-initialisation in every child. Threads are simpler, cheaper, and correct for this workload.

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
        self._tick_running = False    # guarded on the single worker thread

    def run(self) -> None:
        while not self._stop_event.is_set():
            # --- TICK GUARD (scheduled-interval overlap only) ---
            if self._tick_running:
                logger.info(f"Connector {self.connector_id}: previous tick still running, skip")
                self._stop_event.wait(self._config["sync_interval_seconds"])
                continue

            self._tick_running = True
            try:
                self._run_tick()
            finally:
                self._tick_running = False

            # Post-tick sleep — interruptible only by stop_event (DELETE path).
            self._stop_event.wait(timeout=self._config["sync_interval_seconds"])

    def _run_tick(self) -> None:
        """Execute one sync tick (scan → diff → stage → ingest → delete)."""
        # ... (full tick logic as documented in §8.1) ...
        pass


class ConnectorWorkerManager:
    def __init__(self) -> None:
        # Maps connector_id → (Thread, ConnectorSyncWorker, stop_event)
        self._workers: dict[str, tuple[threading.Thread, ConnectorSyncWorker, threading.Event]] = {}
        self._lock = threading.Lock()

    def start_worker(self, connector_config: dict) -> None:
        """Start a new sync worker thread for the given connector.

        The worker's run() loop fires the first tick immediately upon thread
        start — no external signal is needed.
        """
        connector_id = connector_config["id"]
        with self._lock:
            if connector_id in self._workers:
                logger.info(f"Worker for connector {connector_id} already running — skipping")
                return
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

**Sleep mechanics:**

- At the bottom of the tick loop the worker calls `self._stop_event.wait(timeout=sync_interval_seconds)`. This means the sleep returns early only if `stop_event` is set (DELETE / shutdown path).
- There is no second event and no manual wakeup mechanism — the interval is the only scheduler.

`daemon=True` ensures threads are reaped automatically when the main Uvicorn process exits — no manual cleanup needed at pod shutdown.

---

## 10. Startup Recovery

**Modified file:** `services/digitize/app.py`

After the existing zombie job recovery block in the `lifespan()` context manager, add connector worker recovery. This is a **self-healing path** — if all connector configs are in the DB, the pod recovers fully without any catalog involvement.

**Spawner identity at startup:** Thread spawning at startup runs on the **main Uvicorn startup path**, inside the `lifespan()` async context manager, before the HTTP server begins accepting requests. There is no dedicated background thread or process responsible for spawning — the lifespan hook calls `start_worker()` synchronously in a loop, and `start_worker()` creates and starts each daemon thread inline. The worker threads fire their first tick after their initial `sync_interval_seconds` sleep. This is intentional: at pod startup all connectors recovered simultaneously, and an immediate flood of SFTP/S3 connections would impose unnecessary load; the staggered natural interval is preferred.

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

Remove any SSH/connector env-var fields that were previously read from the environment (e.g. `SSH_HOST`, `SSH_USERNAME`, `SSH_PRIVATE_KEY_PEM`, `SSH_REMOTE_PATH`, `SSH_SYNC_INTERVAL_SECONDS`). Connector config is no longer injected via environment — it arrives exclusively through the authenticated API.

Add a `ConnectorConfig` nested settings class with paths that default to the Podman secret mount locations but can be overridden in unit tests without real secret files:

```python
class ConnectorConfig(BaseSettings):
    """Paths to Podman secret mounts used by the connector subsystem."""

    encryption_key_path: Path = Field(
        default=Path("/run/secrets/connector_encryption_key"),
        description=(
            "Path to the 32-byte AES-256-GCM key used to encrypt connector credentials "
            "at rest (on write) and decrypt them at sync time (on read). "
            "Delivered as a Podman secret mount — never an env var."
        ),
    )
    api_token_path: Path = Field(
        default=Path("/run/secrets/connector_api_token"),
        description=(
            "Path to the bearer token used to authenticate the connector API. "
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
| `api/v1/connectors.py` | **NEW** | POST/PUT/DELETE/GET /v1/connectors; bearer-token dependency; 202 + BackgroundTask worker start; on POST/PUT encrypts credential fields using pod encryption key before DB write; worker stop/restart on PUT (waits for running tick to finish); stop-sync document-cleanup sequence on DELETE |
| `connector/__init__.py` | **NEW** | Makes `connector/` a Python sub-package |
| `connector/base_scanner.py` | **NEW** | `RemoteFile` dataclass; `BaseScanner` ABC with four abstract methods (`connect`, `close`, `scan`, `download_to`); shared `_decrypt_credential()` implementation (reads pod encryption key, AES-256-GCM decrypts); context manager (`__enter__`/`__exit__`) |
| `connector/sftp_scanner.py` | **NEW** | `SFTPScanner(BaseScanner)` — `connect()` reads pod encryption key → decrypts `connection_details["private_key"]` → paramiko; `scan()` does DFS `listdir_attr` + streaming SHA-256; `download_to()` streams via `sftp.open()` |
| `connector/s3_scanner.py` | **NEW** | `S3Scanner(BaseScanner)` — `connect()` reads pod encryption key → decrypts `connection_details["secret_access_key"]` → boto3; `scan()` uses `list_objects_v2` paginator + streaming SHA-256; `download_to()` uses `s3.download_fileobj()`; `S3ScannerError` |
| `connector/scanner.py` | **NEW** | `build_scanner(config)` factory — `_REGISTRY` dict dispatches on `config["type"]` to return the appropriate `BaseScanner` subclass instance |
| `connector/sync_worker.py` | **NEW** | Per-connector tick loop; tick guard; `build_scanner()` dispatch; diff → sort by `last_modified` DESC; dedup pass; store checksums in DB; batch into groups of 10 (most-recent-first); per-batch: download → copy to tmp → create job (`{connectorID}-{syncID}-{batchCount}`) → register docs → ingest from tmp → cleanup; orphan deletes after all batches; message-based `sync_status` finalisation; error handling |
| `connector/worker_manager.py` | **NEW** | Daemon thread pool singleton (`connector_worker_manager`); start/stop/list; `threading.Lock` |
| `db/scripts/init_schema.sql` | **MODIFIED** | Add `active_connectors` (with `connection_details` JSONB), `file_checksum_registry`, `connector_file_membership`, and `connector_sync_history` tables |
| `db/models.py` | **MODIFIED** | Add `ActiveConnector`, `FileChecksumRegistry`, `ConnectorFileMembership`, `ConnectorSyncHistory` ORM models |
| `utils/db.py` | **MODIFIED** | Add 15 new CRUD functions for connector tables (includes `merge_connection_details`, `lookup_content_by_sha256`, `delete_connector_membership_atomic`, `insert_sync_history`, `update_sync_history`, `update_sync_history_files_syncing`, `list_sync_history`) |
| `app.py` | **MODIFIED** | Register `connectors_router`; add connector startup recovery block in `lifespan()`; register `GET /v1/connectors/{connector_id}/sync-history` route in `connectors_router` |
| `settings.py` | **MODIFIED** | Remove SSH env-var fields; add `ConnectorConfig` with `encryption_key_path` and `api_token_path` overridable secret paths |

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
| D-6 | Encryption key load timing | **Fresh per tick inside scanner `.connect()`** | Minimises encryption key memory residence; enables future key rotation without pod restart. Applies to both `SFTPScanner` and `S3Scanner`. The same key is used by the API handler to encrypt credentials at write time (POST/PUT) and by the scanner to decrypt them at sync time. |
| D-7 | File traversal depth | **Recursive DFS** | Full subtree of `remote_path` scanned |
| D-8 | File type filtering | **Catalog sends `allowed_extensions` list in push payload** | Stored in `active_connectors.allowed_extensions` (JSONB); applied at scan time |
| D-9 | Job tracking for connector syncs | **Create a `Job` row per batch of 10 files** | Connector syncs visible in `GET /v1/jobs`; `operation = "ingestion"`, `job_name = "{connectorID}-{syncID}-{batchCount}"` (e.g. `"abc123-7-2"`). Multiple Job rows are created per tick — one per 10-file batch. Each job name encodes the connector, the tick (`sync_id`), and the batch sequence number, making per-batch traceability straightforward. |
| D-10 | Staging strategy | **Option B extended: per-tick staging dir + per-batch tmp copy before pipeline** | All diff files are downloaded into a single per-tick staging dir ordered by `last_modified` DESC. The list is then sliced into batches of 10. Each batch is copied to a dedicated `batch_tmp_dir` under `/tmp/` immediately before `ingest()` is called — the pipeline operates on the tmp copy, not the staging dir. The `batch_tmp_dir` is torn down after each batch completes. The staging dir is torn down after all batches complete. This gives each pipeline call an isolated, stable input directory and avoids the pipeline observing partial downloads from concurrent batch operations. |
| D-11 | Change detection | **SHA-256 only** | Content-accurate; immune to mtime precision / server clock skew |
| D-12 | Threading model — Thread vs Process | **`daemon=True` `threading.Thread` per connector** | Workload is I/O-bound (GIL released during all network/DB calls); threads are 8 KB vs 15–50 MB per process; shared DB pool + state is an asset; fork-safety issues make `multiprocessing` with SQLAlchemy/asyncio complex; crash isolation and forceful-kill advantages of processes do not apply to this pure-Python I/O workload; socket-level timeouts in scanners bound maximum tick duration making stuck threads recoverable at pod restart. Full analysis in §9.1. |
| D-13 | Recovery failure policy | **Log and continue** | Matches existing zombie-job recovery behaviour; pod health check not blocked |
| D-14 | First tick on POST; PUT waits for tick completion | **Worker `run()` loop fires first tick immediately on thread start; PUT uses `stop_event` + `thread.join()` to wait for any running tick before replacing the worker** | First tick begins as soon as the thread is live — no external signal needed. PUT config changes take effect from the first tick of the new thread, never mid-tick. |
| D-15 | Document cleanup on DELETE | **Best-effort per-hash loop over `connector_file_membership` with reference-counted deletion** | Reads membership snapshot once via `list_connector_hashes`; for each hash calls `delete_connector_membership_atomic` (removes membership row + counts remaining refs in a single transaction); only calls `DELETE /v1/documents/{doc_id}` and `delete_checksum_registry` when remaining count == 0 (last connector to hold that content); treats 404 as success; logs but does not abort on 5xx; `ON DELETE CASCADE` on `connector_id` removes any remaining membership rows after the loop so the connector row delete is not held hostage by per-hash failures. |
| D-16 | Registry table design — single table vs split | **Split into `file_checksum_registry` (global, `sha256` PK) and `connector_file_membership` (per-connector, `(connector_id, sha256)` PK)** | A single table keyed on `(connector_id, sha256)` cannot serve global dedup — the dedup check needs a `sha256`-only lookup across all connectors. Splitting into two tables gives each concern its own shape: `file_checksum_registry` is a pure content store (one row per unique document), and `connector_file_membership` is the per-connector view used for orphan detection and safe reference-counted deletion. `ON DELETE CASCADE` on `connector_id` in the membership table safely removes a connector's membership rows without touching the shared content registry. |
| D-17 | Orphan delete race condition | **Atomic transaction: delete membership row first, then count remaining refs in the same transaction** | Two concurrent connector ticks could both observe `remaining == 0` for the same orphaned hash if the check is not atomic. By deleting the membership row and counting remaining rows inside a single `BEGIN/COMMIT`, the database serialises the decision — only one tick can observe `remaining == 0` and proceed to delete the document and registry entry. All others will see `remaining >= 1` at check time and leave the document intact. |
| D-18 | Sync history retention policy | **Retain all rows for connector lifetime; delete via `ON DELETE CASCADE` when connector is removed** | History rows are never deleted by application logic during normal operation. Retention is unbounded while a connector exists. When the connector is removed all its history is cascade-deleted. This avoids a separate purge job, keeps the API simple, and matches the "connector lifetime = history lifetime" expectation. If table growth becomes a concern in long-lived deployments a scheduled TRUNCATE or a `LIMIT`-based API cap (`max 200 rows`) provides an operational escape hatch without a schema change. |
| D-19 | `sync_id` scoping — per-connector vs global | **Per-connector, 1-based, `COALESCE(MAX(sync_id), 0) + 1`** | A global auto-increment PK (`BIGSERIAL id`) already exists for row identity. A per-connector `sync_id` is what catalog needs to display "tick #7 of connector X" in the UI. The per-connector counter restarts at 1 after a connector is deleted and re-registered, which is correct because the history for the old registration was also wiped by `ON DELETE CASCADE`. |
| D-20 | Stale `'syncing'` rows after pod crash | **Leave as-is; document threshold heuristic; do not retroactively update on recovery** | Updating orphaned `'syncing'` rows on startup would require knowing the final file counts, which is unknowable without re-running the scan. The stale row is the accurate record of an incomplete tick. Callers are documented to treat `started_at` older than `2 × sync_interval_seconds` with `sync_status = 'syncing'` as likely stale. |
| D-21 | Crash detection mechanism | **Monitor polls `thread.is_alive()` every 30 s** — no signals, no callbacks. Simple and correct for daemon threads. |
| D-22 | `sync_status` as free-form message, not enum | **`sync_status` is an unrestricted `TEXT` field; no `CHECK` constraint** | Avoids the need for a "partial error" enum value and the mapping logic required to translate per-file failure counts into a fixed state. The message itself (`"2 files failed to ingest"`, `"failed: SFTP connection refused"`) carries the diagnostic information directly. The only fixed values are `'syncing'` (written on INSERT, before the outcome is known) and `'completed'` (clean finish). All other outcomes are descriptive strings. |
| D-23 | Respawn back-off strategy | **Exponential: `min(2^n, 300)` s, cap at 5 attempts** — prevents a persistently-crashing connector from hammering the DB or the remote source. Manual intervention (re-push from catalog) required after cap. |
| D-24 | Crash-count reset trigger | **Reset on first successful tick after respawn** — cumulative crash count does not accumulate across healthy intervals. |
| D-25 | Monitor stop on pod shutdown | **`_monitor_stop.set()` + `join(10 s)` in lifespan shutdown** — monitor exits cleanly; no orphaned polling after pod stop. |
| D-26 | DELETE interrupt when tick in progress | **Cooperative stop only: `stop_event.set()` + `join(30 s)`; proceed with cleanup on timeout** — consistent with D-12 (no `Thread.kill()`); worst-case thread exits after socket timeout (~60 s). Document cleanup proceeds regardless. |
| D-27 | Scanner abstraction — ABC vs Protocol vs duck typing | **`BaseScanner` ABC with four `@abstractmethod` methods** | An ABC gives a hard instantiation guard at import time — attempting to instantiate a partial subclass raises `TypeError` before the first tick runs, not silently at call time. A `typing.Protocol` would be structurally typed (no guard) and provides no place to put the shared `_decrypt_credential()` implementation. Duck typing gives neither guard nor reuse. The ABC approach groups `RemoteFile`, `_decrypt_credential()`, and the context manager protocol in a single file, keeping the shared surface explicit. |

---

## 14. Thread Lifecycle & Resilience

### 14.1 Threading Model — Where `ConnectorWorkerManager` Lives

`ConnectorWorkerManager` is **not a separate OS process, a separate Python interpreter, or a Uvicorn worker**. It is a module-level singleton that lives inside the single Uvicorn process, on the main Python interpreter. Every sync thread it spawns is a `daemon=True` `threading.Thread` within that same process.

```
┌──────────────────────────────────────────────────────────────────────┐
│  OS Process  (uvicorn)                                               │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  Main thread  (asyncio event loop — FastAPI / Uvicorn)       │   │
│  │  · handles all HTTP requests                                 │   │
│  │  · runs lifespan() startup / shutdown hooks                  │   │
│  │  · calls connector_worker_manager.start_worker() on POST/PUT │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐     │
│  │  daemon thread  │  │  daemon thread  │  │  daemon thread  │     │
│  │  connector A    │  │  connector B    │  │  connector C    │     │
│  │  (sync worker)  │  │  (sync worker)  │  │  (sync worker)  │     │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘     │
│                                                                      │
│  ConnectorWorkerManager  (module-level singleton)                   │
│  _workers = { A: (thread, worker, stop_event), … }                 │
└──────────────────────────────────────────────────────────────────────┘
```

Consequence: a sync thread crash (unhandled exception that escapes `worker.run()`) affects only that one thread. The asyncio event loop on the main thread keeps accepting HTTP requests; other connector threads keep running. The dead thread's slot in `_workers` remains, pointing at a thread that is no longer alive — this is the crash state that §14.2 and §14.4 must detect and handle.

---

### 14.2 Thread Crash & Status

A "crash" is defined as: the `Thread` object is no longer alive (`thread.is_alive() == False`) but `stop_event` was never set — i.e. the thread exited without being asked to stop. This can only happen if an exception escaped the outer `while` loop in `ConnectorSyncWorker.run()`.

#### Crash observable states

```
                           ┌────────────────────────────────────────────┐
                           │  _workers dict (ConnectorWorkerManager)    │
                           │                                            │
  Normal running ──────►  │  connector_id →  (thread ✅alive,          │
                           │                   worker,                  │
                           │                   stop_event ⬜not set)    │
                           │                                            │
  Graceful stop ──────►   │  (entry removed by stop_worker())          │
                           │                                            │
  CRASH  ─────────────►   │  connector_id →  (thread ❌dead,           │
                           │                   worker,                  │
                           │                   stop_event ⬜not set)    │
                           │                                            │
  Stop timed out ──────►  │  (entry removed; thread still alive=warn)  │
                           └────────────────────────────────────────────┘
```

#### Status fields written on crash

The outermost `try/except` in `ConnectorSyncWorker.run()` must catch the escape. The current design (§9.2) only catches exceptions inside `_run_tick()`, then continues the loop. The crash guard lives **outside** the loop:

```
ConnectorSyncWorker.run()
│
├─ while not stop_event.is_set():         ← loop
│    ├─ tick guard check
│    ├─ _run_tick()                       ← exceptions here are caught inside
│    └─ sleep / wait
│
└─ [only exits loop if stop_event is set OR an exception escapes the while body]
   ← escape here = crash
```

If an exception escapes the `while` loop, two things must happen before the thread exits:

1. **Write crash status to DB** — `update_connector_sync_status(connector_id, "crashed: <error>", now())`.
2. **Write open sync-history row to DB** — if a tick was in progress at crash time (`_tick_running == True`), call `update_sync_history(…, sync_status="crashed: <error>", finished_at=now())` to close any open `"syncing"` row.

```
┌──────────────────────────────────────────────────────────────────────┐
│  ConnectorSyncWorker.run()  — crash guard                           │
│                                                                      │
│  try:                                                                │
│    while not stop_event.is_set():                                   │
│       ... tick loop ...                                              │
│  except Exception as exc:                      ← escape caught here │
│    log.error(f"Worker {connector_id} crashed: {exc}", exc_info=True)│
│    if _tick_running and _current_sync_id:                           │
│        update_sync_history(… "crashed: {exc}" …)                   │
│    update_connector_sync_status(… "crashed: {exc}" …)              │
│    ← thread exits; is_alive() → False                               │
└──────────────────────────────────────────────────────────────────────┘
```

#### `sync_status` values added for crash

| Value | Written by | Meaning |
| --- | --- | --- |
| `"crashed: <error>"` | Crash guard (`except` outside `while`) | Thread exited without a cooperative stop |

> **Note on `active_connectors.sync_status`:** The existing column already stores `sync_status` as a free-form `TEXT` (§4.1 / D-22). No schema change is needed — `"crashed: <error>"` is just another message value.

---

### 14.3 Respawn Logic

When the thread monitor (§14.4) detects a crashed thread it attempts to respawn the worker using the config already in the DB. Respawn is cooperative and bounded — it does not loop forever.

#### Respawn flow

```
Monitor detects: thread dead, stop_event not set
         │
         ▼
┌─────────────────────────────────────────────────────────────────────┐
│  RESPAWN ATTEMPT  (inside ConnectorWorkerManager)                   │
│                                                                      │
│  1. Remove stale entry from _workers dict (under _lock)             │
│                                                                      │
│  2. Read connector config from DB                                   │
│       get_active_connector(connector_id)                            │
│       ↳ if None → connector was deleted; skip respawn              │
│                                                                      │
│  3. Increment crash_count for this connector_id                     │
│       (in-memory counter on ConnectorWorkerManager)                 │
│                                                                      │
│  4. Check back-off gate                                             │
│       if crash_count > MAX_RESPAWN_ATTEMPTS (default 5):           │
│           log.error("connector {id}: max respawns reached")        │
│           update_connector_sync_status(… "respawn limit reached")  │
│           ← do NOT respawn; connector requires manual intervention  │
│           return                                                     │
│                                                                      │
│  5. Back-off delay  =  min(2^crash_count, 300) seconds             │
│       sleep(back_off)   ← blocking on the monitor thread            │
│                                                                      │
│  6. start_worker(config)  → new thread spawned                     │
│       crash_count reset to 0 after first successful tick completes  │
└─────────────────────────────────────────────────────────────────────┘
```

#### Back-off schedule

| Crash # | Delay before respawn |
| --- | --- |
| 1st | 2 s |
| 2nd | 4 s |
| 3rd | 8 s |
| 4th | 16 s |
| 5th | 32 s |
| > 5th | No respawn — manual intervention required |

#### Crash-count reset

The crash counter for a connector is reset to `0` the first time a tick completes **successfully** (`sync_status == "completed"`) after a respawn. This prevents a connector that occasionally crashes from being permanently blocked after 5 cumulative crashes spaced far apart.

```
ConnectorSyncWorker._run_tick()
  └─ on clean finish:
       connector_worker_manager.reset_crash_count(self.connector_id)
```

---

### 14.4 Generic Thread Monitor

The monitor is a single long-lived daemon thread started during `lifespan()` startup. It is **not** per-connector — one monitor watches all workers. It polls the `_workers` dict on a fixed cadence and drives the respawn logic from §14.3.

#### Monitor thread state machine

```
lifespan() start
     │
     ▼
┌──────────────────────────────────────────────────────────────────────┐
│  MONITOR THREAD  (daemon=True, name="connector-monitor")            │
│                                                                      │
│  loop every MONITOR_INTERVAL_SECONDS (default 30 s):               │
│                                                                      │
│    snapshot = list(_workers.items())   ← copy under _lock          │
│                                                                      │
│    for connector_id, (thread, worker, stop_event) in snapshot:     │
│                                                                      │
│      ┌──────────────────────────────────────────────────────────┐  │
│      │  is stop_event set?                                       │  │
│      │    YES → graceful stop in progress; skip                  │  │
│      │    NO  → check thread health                              │  │
│      └──────────────────┬───────────────────────────────────────┘  │
│                         │                                           │
│      ┌──────────────────▼───────────────────────────────────────┐  │
│      │  thread.is_alive()?                                       │  │
│      │    YES → healthy; log debug; skip                         │  │
│      │    NO  → CRASH DETECTED                                   │  │
│      │            schedule_respawn(connector_id)                 │  │
│      └──────────────────────────────────────────────────────────┘  │
│                                                                      │
│    sleep(MONITOR_INTERVAL_SECONDS)                                  │
│    ← wakes early if stop_event (pod shutdown) is set               │
└──────────────────────────────────────────────────────────────────────┘
     │
lifespan() shutdown
     │ _monitor_stop.set()
     ▼
  monitor thread exits
```

#### Monitor health log output

| Condition | Log level | Message |
| --- | --- | --- |
| Thread alive, tick not running | `DEBUG` | `"monitor: connector {id} healthy (idle)"` |
| Thread alive, tick running | `DEBUG` | `"monitor: connector {id} healthy (running tick)"` |
| Thread dead, stop_event not set | `ERROR` | `"monitor: connector {id} crashed — scheduling respawn #{n}"` |
| Thread dead, stop_event was set | `DEBUG` | `"monitor: connector {id} stopped cleanly — skipping"` |
| Respawn limit reached | `ERROR` | `"monitor: connector {id} exceeded max respawns — manual fix required"` |

#### Where the monitor is started

```
# app.py  lifespan()

_monitor_stop = threading.Event()
monitor_thread = threading.Thread(
    target=connector_worker_manager.run_monitor,
    args=(_monitor_stop,),
    daemon=True,
    name="connector-monitor",
)
monitor_thread.start()

yield   # ← FastAPI serves requests here

_monitor_stop.set()
monitor_thread.join(timeout=10)
```

---

### 14.5 Interrupt Thread on DELETE

When `DELETE /v1/connectors/{connector_id}` is called, the DELETE handler stops the sync thread as quickly as possible before proceeding with document cleanup. Rather than waiting for a running tick to finish naturally, `_run_tick()` checks `stop_event` at each **inter-phase boundary** — between scan, dedup, each ingest batch, and orphan deletes. When the event is set mid-tick, the thread performs an **immediate cooperative self-cleanup**: it closes all active connections, removes any in-flight pending-SHA rows, deletes OpenSearch documents (and their membership rows) for every file that already completed ingest during the current tick, closes the sync-history row, and then returns — exiting the loop and the thread.

**Relationship to the sleep:** The DELETE path sets `stop_event`. When no tick is running, `stop_event.set()` wakes the `stop_event.wait()` sleep immediately and the thread exits on the next loop iteration. When a tick is running, the tick detects the event at the next inter-phase boundary and self-cleans before returning, so the thread exits well within the 30 s join timeout rather than running to natural completion.

#### Cancellation check helper — `_cancel_if_requested()`

`_run_tick()` calls this helper after each phase. It is a no-op when `stop_event` is clear.

```python
def _cancel_if_requested(
    self,
    scanner,
    pending_shas: list[str],
    ingested_this_tick: list[tuple[str, str]],  # (sha256, doc_id)
    sync_id: int,
) -> bool:
    """
    Returns True if stop_event is set (caller must return immediately).
    Performs full self-cleanup before returning True:
      1. Close all active connections
      2. Delete pending (in-flight) SHA rows
      3. Delete membership rows + OpenSearch docs for already-ingested files
      4. Close the sync-history row as "interrupted: connector deleted"
    """
    if not self._stop_event.is_set():
        return False

    logger.info(
        f"Connector {self.connector_id}: stop event received mid-tick — interrupting"
    )

    # 1. Close active connections (same as normal step 5)
    try:
        scanner.close()
    except Exception:
        pass  # best-effort; connection may already be gone

    # 2. Delete pending SHA rows (pre-written NULLs for in-flight files)
    for sha256 in pending_shas:
        try:
            delete_checksum_registry(sha256)
        except Exception as exc:
            logger.warning(
                f"Connector {self.connector_id}: "
                f"failed to remove pending SHA {sha256} during interrupt: {exc}"
            )

    # 3. Delete membership rows + OpenSearch docs for files ingested this tick
    for sha256, doc_id in ingested_this_tick:
        try:
            remaining = delete_connector_membership_atomic(self.connector_id, sha256)
            if remaining == 0 and doc_id is not None:
                resp = requests.delete(f"{settings.ingest_url}/v1/documents/{doc_id}",
                                       headers={"Authorization": f"Bearer {token}"})
                if resp.status_code not in (200, 204, 404):
                    logger.warning(
                        f"Connector {self.connector_id}: interrupt cleanup — "
                        f"failed to delete doc {doc_id}: HTTP {resp.status_code}"
                    )
        except Exception as exc:
            logger.warning(
                f"Connector {self.connector_id}: "
                f"interrupt cleanup failed for sha={sha256}: {exc}"
            )

    # 4. Close sync-history row
    update_sync_history(
        self.connector_id, sync_id,
        finished_at=datetime.utcnow(),
        files_found=0, files_syncing=0, files_completed=0, files_failed=0,
        sync_status="interrupted: connector deleted",
    )

    return True
```

#### Cancellation check points inside `_run_tick()`

```
_run_tick():
  step 0 — record tick start (insert_sync_history)
  step 1 — connect & scan
  ← _cancel_if_requested() ──── if set: close conn + purge in-flight + exit ──▶ return
  step 2 — dedup pass
  ← _cancel_if_requested() ──── if set: close conn + purge in-flight + exit ──▶ return
  step 3 — batch ingest loop
    for each batch:
      download files
      ingest()
      update registry + membership  ← batch added to ingested_this_tick
      ← _cancel_if_requested() ─── if set: close conn + purge in-flight
                                            + delete docs ingested so far
                                            + exit ─────────────────────────▶ return
  step 4 — orphan deletes
  ← _cancel_if_requested() ──── if set: close conn + purge in-flight + exit ──▶ return
  step 5 — close connection (normal path)
  step 6 — finalise
```

**Tracking state for the helper — two in-tick collections (local variables in `_run_tick()`):**

| Variable | Type | What it holds | When populated |
| --- | --- | --- | --- |
| `pending_shas` | `list[str]` | SHA-256 values pre-written with `NULL doc_id` (in-flight) | Added just before each file's download begins; removed on successful ingest |
| `ingested_this_tick` | `list[tuple[str, str]]` | `(sha256, doc_id)` pairs that completed ingest within this tick | Appended after each successful `ingest()` + registry update per batch |

These collections mirror state that the existing SHA-cleanup invariant already tracks implicitly; making them explicit allows the cancel helper to act on them without re-querying the DB.

#### DELETE interrupt flow

```
DELETE /v1/connectors/{id}
         │
         ▼
┌─────────────────────────────────────────────────────────────────────┐
│  INTERRUPT SEQUENCE                                                 │
│                                                                      │
│  1. stop_event.set()                                                │
│       ← if worker is sleeping: sleep returns immediately            │
│       ← if worker is in _run_tick(): next inter-phase boundary      │
│            fires _cancel_if_requested() → self-cleanup → return     │
│                                                                      │
│  2. thread.join(timeout=30 s)                                       │
│       ← in practice returns in seconds (boundary hit quickly)      │
│                                                                      │
│  3a. thread.is_alive() == False  ← clean stop                      │
│       → proceed with §3C steps 2–5                                 │
│         (step 3 loop is idempotent — already-cleaned rows are       │
│          no-ops; pre-existing ticks' rows are still cleaned here)  │
│                                                                      │
│  3b. thread.is_alive() == True   ← timeout (tick blocked in I/O)   │
│       → log warning: "thread did not stop within 30 s"             │
│       → proceed with §3C cleanup anyway (best-effort)              │
│       → thread exits when socket timeout fires (≤ 60 s); daemon    │
└─────────────────────────────────────────────────────────────────────┘
```

#### Flow D — DELETE while tick is running

```
[DELETE handler — API thread]        [Worker thread]
──────────────────────────           ─────────────────────────────────────────
DELETE /v1/connectors/{id}           while not stop_event.is_set():
                                       _tick_running = True
1. stop_event.set()                    ┌──────────────────────────────────┐
   ────────────────────────────────▶   │  _run_tick()  ← running          │
                                       │                                  │
2. thread.join(timeout=30 s)           │  ... phase N completes ...       │
   ← DELETE handler blocks here        │  _cancel_if_requested() fires:   │
      (returns quickly in practice     │    scanner.close()               │
       once a phase boundary hits)     │    delete pending SHA rows       │
                                       │    delete docs ingested this tick│
                                       │    close sync-history row        │
                                       │    ← returns True                │
                                       │  _run_tick() returns early       │
                                       └──────────────────────────────────┘
                                       finally: _tick_running = False
                                        │
                                        ▼
                                      while not stop_event.is_set():
                                        stop_event IS set → condition False
                                        → thread exits loop cleanly

   join() returns (thread exited) ◀──
3a. thread.is_alive() == False
    → proceed with §3C steps 2–5
      (step 3 loop handles pre-existing ticks' content;
       content from this tick already cleaned by the thread)

─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ OR (I/O blocked at 30 s boundary) ─ ─ ─ ─ ─ ─

3b. thread.is_alive() == True (timeout)
    → log warning "thread did not stop within 30 s"
    → proceed with §3C cleanup anyway (best-effort)
    → thread exits when socket timeout fires (≤ 60 s); daemon=True
```

#### Flow E — DELETE while worker is sleeping (no tick running)

```
[DELETE handler — API thread]        [Worker thread]
──────────────────────────           ─────────────────────────────────────────
                                     _tick_running = False
                                     ┌─────────────────────────────────────┐
1. stop_event.set()                  │ stop_event.wait(interval)           │
   ────────────────────────────────▶ │  ← returns immediately              │
                                     └─────────────────────────────────────┘
2. thread.join(timeout=30 s)         while not stop_event.is_set():
                                       stop_event IS set → condition False
   join() returns quickly ◀──────      → thread exits

3a. thread.is_alive() == False
    → proceed with doc cleanup (§3C)
```

#### Signal comparison

| Scenario | Signal | Running tick interrupted? | What happens after tick | Thread continues? |
| --- | --- | --- | --- | --- |
| Scheduled interval, no tick running | timer expires | N/A | — | Yes — starts next tick |
| Scheduled interval, tick still running | timer expires | **No** — skipped | Waits another full interval | Yes — starts next tick |
| `DELETE`, no tick running | `stop_event.set()` | N/A | Sleep wakes; loop exits | **No** — thread exits |
| `DELETE`, tick currently running | `stop_event.set()` | **Yes** — at next inter-phase boundary | Thread self-cleans; loop condition False; thread exits | **No** — thread exits |

#### Key design invariants

| Invariant | How it is upheld |
| --- | --- |
| A running tick is **never interrupted by a scheduled interval** | The scheduled timer is not checked inside `_run_tick()`; it is acted on only after `_run_tick()` returns |
| A DELETE **cooperatively interrupts** a running tick at the next inter-phase boundary | `stop_event` is checked by `_cancel_if_requested()` after each phase; the tick self-cleans and returns early rather than running to completion |
| DELETE always produces a **clean thread exit** | `_cancel_if_requested()` closes connections, purges in-flight state, and closes the history row before returning; `stop_event` at the loop boundary guarantees no re-entry |
| No two ticks for the same connector run **concurrently** | The tick guard and the single-threaded loop guarantee at most one active tick per connector at any time |
| In-tick cleanup is **idempotent with §3C step 3** | `delete_connector_membership_atomic` is transactional; the DELETE handler's membership loop treats already-deleted rows as no-ops via `remaining > 0` / `404` checks |

#### Why no `Thread.kill()`?

Python provides no `Thread.kill()`. The cooperative stop (`stop_event`) is the only safe mechanism. With `_cancel_if_requested()` now checking `stop_event` at every inter-phase boundary, the thread exits seconds after the signal is set in the common case. The 30 s join timeout is a safety net for the rare scenario where `stop_event` is set while `ingest()` — the longest-running blocking call — is in progress mid-batch; the thread will exit at the batch boundary immediately after `ingest()` returns.

#### Interaction with the monitor during DELETE

The monitor must not respawn a thread that is being intentionally stopped. The check `if stop_event.is_set(): skip` (§14.4) covers this exactly — once the DELETE handler sets `stop_event`, the monitor will see it and skip respawn for that connector.

```
Timeline (tick running at t=0, phase boundary hit quickly):
  t=0   DELETE handler calls stop_event.set()
  t=0   thread.join(30s) starts
  t=~2  _cancel_if_requested() fires at next boundary:
          scanner.close() → delete pending SHAs → delete ingested docs
          → history row closed as "interrupted: connector deleted"
          → _run_tick() returns; finally: _tick_running = False
          → while condition False → thread exits
  t=~2  join() returns (thread already dead)
  t=~2  §3C continues: steps 2–5 (pre-existing tick content cleaned)
  t=30  monitor next poll: stop_event.is_set() == True → skip (no respawn)

Timeline (I/O blocked — ingest() mid-batch at t=0):
  t=0   DELETE handler calls stop_event.set()
  t=0   thread.join(30s) starts
  t=30  join returns (timeout — ingest() still running)
  t=30  §3C cleanup proceeds (best-effort; thread still alive)
  t=30  monitor next poll: sees stop_event.is_set() == True → skip
  t=~X  ingest() returns; _cancel_if_requested() fires → thread exits
  (connector row already deleted → no longer in _workers)
```

---

### 14.6 FastAPI Lifespan Recovery

The `lifespan()` context manager in [`app.py`](../../services/digitize/app.py) is the single recovery entry point on pod start. The existing recovery block (§10 Startup Recovery) restarts workers for all connectors in the DB. The additions in this section cover:

1. Starting the monitor thread (§14.4).
2. Graceful shutdown of all workers and the monitor on pod stop.

#### Full lifespan recovery sequence

```
POD START
   │
   ▼
lifespan()  [async generator, runs in main thread before Uvicorn accepts requests]
   │
   ├─ 1. DB connection pool init  (existing)
   │
   ├─ 2. Zombie job recovery  (existing)
   │
   ├─ 3. Connector worker recovery
   │       connectors = list_active_connectors()
   │       for config in connectors:
   │           start_worker(config)   ← spawns daemon thread per connector
   │           log "✅ Restarted connector {id}"
   │       ← no immediate trigger; staggered first tick is intentional
   │
   ├─ 4. Monitor thread start
   │       _monitor_stop = threading.Event()
   │       start daemon thread → connector_worker_manager.run_monitor()
   │
   ▼
  yield   ← FastAPI serves HTTP requests
   │
   ▼
POD STOP  (SIGTERM / SIGINT)
   │
   ├─ 5. Stop monitor thread
   │       _monitor_stop.set()
   │       monitor_thread.join(timeout=10 s)
   │
   ├─ 6. Stop all connector workers
   │       for connector_id in connector_worker_manager.list_workers():
   │           connector_worker_manager.stop_worker(connector_id, timeout=30 s)
   │
   └─ 7. DB pool dispose  (existing)
```

#### Lifespan crash-safety

If `list_active_connectors()` raises (e.g. DB unreachable at startup), the entire recovery block is caught and logged. The pod starts without any workers — catalog can detect this via `GET /v1/connectors` returning an empty list and re-push connectors as needed. The monitor is **not** started if connector recovery fails (no workers to monitor).

```
  ┌─────────────────────────────────────────────────────────────────┐
  │  try:                                                           │
  │    connectors = list_active_connectors()                       │
  │    for config in connectors: start_worker(config)             │
  │    start_monitor()                                             │
  │  except Exception as exc:                                      │
  │    log.error(f"Connector recovery failed: {exc}")              │
  │    ← pod continues; no workers; no monitor                    │
  └─────────────────────────────────────────────────────────────────┘
```

#### Recovery + monitor together — full picture

```
┌─────────────────────────────────────────────────────────────────────────┐
│  POD STARTUP                                                            │
│                                                                         │
│  lifespan()                                                             │
│     │                                                                   │
│     ├──► list_active_connectors() ──► [A, B, C]                        │
│     │                                                                   │
│     ├──► start_worker(A)  ──► thread-A spawned (daemon)                │
│     ├──► start_worker(B)  ──► thread-B spawned (daemon)                │
│     ├──► start_worker(C)  ──► thread-C spawned (daemon)                │
│     │                                                                   │
│     └──► start monitor   ──► monitor-thread spawned (daemon)           │
│                                    │                                    │
│                                    │  polls every 30 s                  │
│                                    ▼                                    │
│                           ┌─────────────────┐                          │
│                           │  all threads OK │                          │
│                           └────────┬────────┘                          │
│                                    │                                    │
│                           thread-B crashes                              │
│                                    │                                    │
│                           ┌────────▼────────────────────────────────┐  │
│                           │  monitor detects: B dead, stop not set  │  │
│                           │  → remove B from _workers               │  │
│                           │  → read config from DB                  │  │
│                           │  → back-off wait                        │  │
│                           │  → start_worker(B) → thread-B' spawned │  │
│                           └─────────────────────────────────────────┘  │
│                                                                         │
│  POD STOP  (SIGTERM)                                                    │
│     │                                                                   │
│     ├──► _monitor_stop.set() → monitor exits                           │
│     ├──► stop_worker(A) → stop_event set → thread-A exits              │
│     ├──► stop_worker(B') → stop_event set → thread-B' exits            │
│     └──► stop_worker(C) → stop_event set → thread-C exits              │
└─────────────────────────────────────────────────────────────────────────┘
```

