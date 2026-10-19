# Confluence Data Source Connector Proposal

## 1. Overview
This proposal defines the integration of **Confluence** (Cloud and Data Center / Server) as a first-class data source connector in Digitize alongside S3 (`object_storage`) and SSH/SFTP (`file_system`).

---

## 2. Architecture Diagram

![Confluence Architecture Diagram](confluence-architecture-diagram.svg)

---

## 3. Admin & Infrastructure Prerequisites

To onboard a Confluence instance, the following must be requested from the Admin/IT team:

| Category | Requirements |
| :--- | :--- |
| **Instance Info** | Base URL (`https://<domain>.atlassian.com/wiki` or self-hosted URL) and deployment type (Cloud vs. Data Center / Server). |
| **Authentication** | Dedicated service account credentials:<br>• **Cloud:** Service account email + API token.<br>• **Data Center / Server:** Personal Access Token (PAT) or Bearer token. |
| **Permissions** | Read/View permissions for target Space Keys (`space_keys`), page contents, and attachments (`.pdf`, `.docx`). |
| **Network & TLS** | Firewall/egress allowlist from cluster pods to Confluence (port 443). Corporate CA bundle (`.pem`) if using internal self-signed TLS. |

---

## 4. Connector Schema & Configuration Fields

Stored in `connectors.connection_details` and validated at API boundaries:

| Field | S3 (`object_storage`) | SSH (`file_system`) | Confluence (`confluence`) |
| :--- | :--- | :--- | :--- |
| **Target / Host** | `endpoint_url`, `bucket_name` | `host`, `port`, `remote_path` | `url`, `space_keys` (array) |
| **Auth Credentials** | `access_key_id`, `secret_access_key` | `username`, `private_key` (PEM) | `username` (Cloud email / PAT identifier), `api_token` / `personal_access_token` |
| **Scope & Filters** | `prefix`, `allowed_extensions` | `remote_path`, `allowed_extensions` | `cql_filter` (optional), `include_attachments` (bool), `allowed_extensions` |
| **TLS & Security** | `verify_ssl` | Host key validation | `verify_ssl`, `ca_cert_path` (optional) |

---

## 5. REST Client Implementation Evaluation

| Evaluation Axis | Direct REST API Calls (`httpx` / `aiohttp`) | `atlassian-python-api` Package |
| :--- | :--- | :--- |
| **Security & Supply Chain** | **Winner:** Zero / minimal dependencies, reducing vulnerability exposure and supply-chain attack surface. | **Higher risk:** Relies on third-party package release cadence and its transitive dependency tree. |
| **Code Maintenance** | **Higher burden:** Requires maintaining endpoints, query parameters, and payload schemas internally. | **Winner:** Upstream maintainers absorb Confluence API versioning, deprecations, and schema shifts. |
| **Performance (Parallelism)** | **Winner:** Native `asyncio`/`httpx` support enabling high concurrency during page listing and downloads. | **Limited:** Synchronous blocking design; requires thread/process pools to achieve concurrency. |

> **Recommendation:** Use **Direct REST API Calls via `httpx`** for native async compatibility within `sync_tick.py` and minimal dependency footprint.

---

## 6. Checksum & Deduplication Strategy

Confluence content is tracked via composite key `(checksum, connector_id)` in `connector_document_checksum`:

* **Confluence Pages:** Checksum is computed as:
  $$\text{checksum} = \text{MD5}(\text{PageID} + \text{VersionNumber})$$
  Any page edit increments `VersionNumber`, producing a new MD5 and triggering automated re-ingestion.
* **Attachments:** MD5 of the raw attachment byte stream or attachment `(AttachmentID + VersionNumber)`.
* **Canonical Path (`remote_path`):**
  * Pages: `spaces/{spaceKey}/pages/{pageId}`
  * Attachments: `spaces/{spaceKey}/pages/{pageId}/attachments/{attachmentId}`

---

## 7. Ingestion Process & Sync Flow

![Confluence Sync Flow](confluence-sync-flow.svg)

### Step-by-Step Summary

1. **Connect (`scanner.connect()`):** Validates credentials against Confluence REST endpoint (`/wiki/rest/api/space` or `/rest/api/space`).
2. **Scan (`scanner.scan()`):** Queries pages via CQL for configured spaces. Computes $\text{MD5}(\text{PageID} + \text{VersionNumber})$.
3. **Classify (`_classify()`):** Identifies new, modified, unchanged, or orphaned (deleted) pages against `connector_document_checksum`.
4. **Download & Transform (`scanner.download_to()`):**
   * **Page Content:** Fetches page export (native PDF export or HTML/storage format converted to `.docx`/`.pdf`) into the batch staging folder.
   * **Attachments:** Downloads `.pdf` and `.docx` attachments directly.
5. **Batch Ingest & Indexing:** Dispatches staged files to `initialize_and_launch()` with `OperationType.INGESTION`. Extracted text and embeddings are indexed directly into the **OpenSearch Vector Store** for downstream RAG retrieval.
