# Catalog Datasource Connectors Feature Proposal

**Version:** 1.0
**Date:** June 2026
**Status:** Draft

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Background and Motivation](#2-background-and-motivation)
   - 2.1 [Current State](#21-current-state)
   - 2.2 [Problem Statement](#22-problem-statement)
   - 2.3 [Goals](#23-goals)
3. [Architecture Overview](#3-architecture-overview)
   - 3.1 [Key Concepts](#31-key-concepts)
   - 3.2 [Datasource Types](#32-datasource-types)
   - 3.3 [Component Integration](#33-component-integration)
4. [Database Design](#4-database-design)
   - 4.1 [Reuse of Components Table](#41-reuse-of-components-table)
   - 4.2 [Application-Datasource Join Table](#42-application-datasource-join-table)
   - 4.3 [New Enum Values](#43-new-enum-values)
   - 4.4 [Migration Plan](#44-migration-plan)
5. [API Specification](#5-api-specification)
   - 5.1 [Datasource CRUD APIs](#51-datasource-crud-apis)
   - 5.2 [Application-Datasource Connection APIs](#52-application-datasource-connection-apis)
6. [API Endpoint Details](#6-api-endpoint-details)
   - 6.1 [List Datasources](#61-list-datasources)
   - 6.2 [Get Datasource](#62-get-datasource)
   - 6.3 [Create Datasource](#63-create-datasource)
   - 6.4 [Update Datasource](#64-update-datasource)
   - 6.5 [Delete Datasource](#65-delete-datasource)
   - 6.6 [Connect Datasource to Application](#66-connect-datasource-to-application)
   - 6.7 [Disconnect Datasource from Application](#67-disconnect-datasource-from-application)
   - 6.8 [Get Datasource Status for Application](#68-get-datasource-status-for-application)
7. [Digitize Service Integration](#7-digitize-service-integration)
   - 7.1 [Connector API Overview](#71-connector-api-overview)
   - 7.2 [Connect Flow](#72-connect-flow)
   - 7.3 [Update Flow](#73-update-flow)
   - 7.4 [Disconnect Flow](#74-disconnect-flow)
   - 7.5 [Status Fetch Flow](#75-status-fetch-flow)
   - 7.6 [Bearer Token Authentication](#76-bearer-token-authentication)
8. [Datasource Type Providers](#8-datasource-type-providers)
   - 8.1 [Provider Interface](#81-provider-interface)
   - 8.2 [S3 Provider](#82-s3-provider)
   - 8.3 [Remote SSH/SFTP Provider](#83-remote-sshsftp-provider)
9. [Secret Encryption](#9-secret-encryption)
   - 9.1 [Encryption Scheme Overview](#91-encryption-scheme-overview)
   - 9.2 [Catalog Backend Encryption](#92-catalog-backend-encryption)
   - 9.3 [Digitize Service Encryption](#93-digitize-service-encryption)
   - 9.4 [Deployment Changes](#94-deployment-changes)
10. [Sync Service](#10-sync-service)
    - 10.1 [Sync Job Design](#101-sync-job-design)
    - 10.2 [Status Updates](#102-status-updates)
11. [Application Lifecycle Integration](#11-application-lifecycle-integration)
    - 11.1 [Datasource Connection During Create Flow](#111-datasource-connection-during-create-flow)
    - 11.2 [Datasource Connection Post-Creation](#112-datasource-connection-post-creation)
12. [Error Handling](#12-error-handling)
13. [Security Considerations](#13-security-considerations)

---

## 1. Executive Summary

This proposal introduces **Datasource Connectors** to the AI Services Catalog UI. A datasource represents a remote content source (e.g., S3 bucket, remote SSH/SFTP server) that can be registered centrally and connected to one or more deployed applications. Once connected, the services within that application can consume files from the datasource.

The feature adds CRUD management for datasources, a many-to-many relationship between datasources and applications, integration with the downstream Digitize service's `/v1/connectors` API, a periodic sync job to verify connectivity, and deployment-level secret management for credential encryption.

---

## 2. Background and Motivation

### 2.1 Current State

Applications deployed through the Catalog are self-contained: services consume data that is uploaded directly (e.g., via the digitize document upload API). There is no mechanism to connect a deployed application to a persistent external content source.

### 2.2 Problem Statement

Users need to:

- Register remote datasources once and reuse them across multiple applications.
- Have application services automatically consume files from those datasources.
- Verify that datasource connectivity remains healthy over time.
- Keep datasource credentials encrypted at rest.

### 2.3 Goals

1. Provide a datasource registry with full CRUD operations accessible from the Catalog UI.
2. Enable connecting a datasource to one or many applications (and vice-versa) via a dedicated API.
3. Integrate with the Digitize service `/v1/connectors` API to propagate connector details downstream.
4. Implement a periodic sync job that validates datasource connectivity and updates status.
5. Encrypt sensitive credentials before storage in the catalog database and before forwarding to the Digitize service.
6. Support the datasource connection step as part of the application create flow (post successful deployment).

---

## 3. Architecture Overview

### 3.1 Key Concepts

**Datasource**: A named, typed, remote content store with connection credentials. Datasources are tenant-level resources — they are not owned by a single application.

**Connector**: The Digitize service's representation of an active link between a deployed application and a datasource. A connector is created in the Digitize service when a datasource is connected to an application, and stores the `connector_id`.

**Application-Datasource Link**: The catalog-level record of which datasources are connected to which applications. This is a many-to-many relationship. Each link stores the `connector_id` used by the Digitize service.

### 3.2 Datasource Types

The digitize proposal uses the following canonical type identifiers. The catalog provider IDs **must** match exactly so the payload the catalog sends to digitize is consistent.

| Type            | Digitize `type` value | Key Credentials                                                    |
| --------------- | --------------------- | ------------------------------------------------------------------ |
| Amazon S3       | `s3`                  | `access_key_id`, `secret_access_key`, `region`, `bucket_name`      |
| Remote SSH/SFTP | `ssh_sftp`            | `host`, `port`, `username`, `private_key` (Ed25519), `remote_path` |

Additional provider types can be added by implementing the provider interface described in Section 8.

### 3.3 Component Integration

Datasources are stored in the existing `components` table using:

- `type = "datasource"`
- `provider` = the datasource type identifier (e.g., `"s3"`, `"sftp"`)
- `source = "remote"` — a new column distinguishing remotely-managed components from catalog-installed ones
- Sensitive credential fields within `metadata` are encrypted at rest (see Section 9)

This approach reuses the existing `components` infrastructure (repository, status enum, JSONB metadata) without introducing a separate table for datasources.

---

## 4. Database Design

### 4.1 Reuse of Components Table

The `components` table is extended with a `source` column to distinguish datasource components from catalog-installed components.

```sql
-- Migration: add source column to components
ALTER TABLE components ADD COLUMN source VARCHAR(50) NOT NULL DEFAULT 'catalog';
-- Possible values: 'catalog' | 'remote'
```

A datasource record in the `components` table looks like:

| Column       | Example Value                                                                                                |
| ------------ | ------------------------------------------------------------------------------------------------------------ |
| `id`         | `uuid`                                                                                                       |
| `type`       | `"datasource"`                                                                                               |
| `provider`   | `"s3"`                                                                                                       |
| `source`     | `"remote"`                                                                                                   |
| `status`     | `"Initializing"` / `"Running"` / `"Error"`                                                                   |
| `message`    | `""` / `"Permission denied on bucket"`                                                                       |
| `metadata`   | `{"bucket": "my-docs", "region": "us-east-1", "access_key_id": "AK...", "secret_access_key": "<encrypted>"}` |
| `version`    | `"1"`                                                                                                        |
| `created_at` | timestamp                                                                                                    |
| `updated_at` | timestamp                                                                                                    |

### 4.2 Application-Datasource Join Table

A new join table `application_datasources` captures the many-to-many relationship between applications and datasources. It also stores the `connector_id` returned by the Digitize service for each link.

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE application_datasources (
    application_id  UUID NOT NULL,
    datasource_id   UUID NOT NULL,
    connector_id    VARCHAR(255),           -- ID returned by Digitize /v1/connectors
    status          component_status NOT NULL DEFAULT 'Initializing',
    message         TEXT,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (application_id, datasource_id),
    CONSTRAINT fk_application_id FOREIGN KEY (application_id)
        REFERENCES applications(id) ON DELETE CASCADE,
    CONSTRAINT fk_datasource_id FOREIGN KEY (datasource_id)
        REFERENCES components(id)
);

CREATE TRIGGER update_application_datasources_updated_at
    BEFORE UPDATE ON application_datasources
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS update_application_datasources_updated_at ON application_datasources;
DROP TABLE IF EXISTS application_datasources;
-- +goose StatementEnd
```

**Key design decisions:**

- `(application_id, datasource_id)` is the composite primary key — this enforces uniqueness of the link.
- `connector_id` is nullable: it is `NULL` until the Digitize service successfully creates the connector and returns an ID.
- `ON DELETE CASCADE` on `application_id` — deleting an application automatically removes its datasource links.
- No cascade on `datasource_id` — a datasource cannot be deleted while it is linked to an application (enforced at the service layer, not by a DB constraint).

### 4.3 New Enum Values

No new enum types are required. The existing `component_status` enum (`Initializing`, `Running`, `Error`) covers all datasource and connector link states.

### 4.4 Migration Plan

Two new goose migration files, numbered after the current highest (`20260430094506`):

| File                                                      | Purpose                              |
| --------------------------------------------------------- | ------------------------------------ |
| `20260430094507_add_source_to_components.sql`             | Adds `source` column to `components` |
| `20260430094508_create_application_datasources_table.sql` | Creates the join table               |

---

## 5. API Specification

### 5.1 Datasource CRUD APIs

All routes are under `/api/v1` and protected by the existing `AuthMiddleware`.

| Method   | Path               | Description                                                    |
| -------- | ------------------ | -------------------------------------------------------------- |
| `GET`    | `/datasources`     | List all datasources (paginated, filterable by status)         |
| `GET`    | `/datasources/:id` | Get a single datasource by ID                                  |
| `POST`   | `/datasources`     | Create a new datasource (validates connectivity first)         |
| `PUT`    | `/datasources/:id` | Update datasource metadata / credentials                       |
| `DELETE` | `/datasources/:id` | Delete a datasource (only if not connected to any application) |

### 5.2 Application-Datasource Connection APIs

| Method   | Path                                           | Description                                                         |
| -------- | ---------------------------------------------- | ------------------------------------------------------------------- |
| `PUT`    | `/applications/:id/datasources/:datasource_id` | Connect a datasource to an application                              |
| `DELETE` | `/applications/:id/datasources/:datasource_id` | Disconnect a datasource from an application                         |
| `GET`    | `/applications/:id/datasources/:datasource_id` | Get datasource status/details for a specific application connection |

---

## 6. API Endpoint Details

### 6.1 List Datasources

**`GET /api/v1/datasources`**

Query parameters:

| Parameter   | Type   | Required | Description                                          |
| ----------- | ------ | -------- | ---------------------------------------------------- |
| `page`      | int    | No       | Page number (default: 1)                             |
| `page_size` | int    | No       | Items per page (default: 20, max: 100)               |
| `status`    | string | No       | Filter by status: `Initializing`, `Running`, `Error` |
| `provider`  | string | No       | Filter by provider type: `s3`, `sftp`                |

**Response `200 OK`:**

```json
{
  "datasources": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "My S3 Bucket",
      "type": "datasource",
      "provider": "s3",
      "status": "Running",
      "message": "",
      "metadata": {
        "bucket": "my-docs",
        "region": "us-east-1",
        "access_key_id": "AKIAIOSFODNN7EXAMPLE"
      },
      "created_at": "2026-06-01T10:00:00Z",
      "updated_at": "2026-06-01T10:05:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

> **Note:** `secret_access_key` and `private_key` fields are never returned in API responses. Only non-sensitive metadata fields are surfaced.

### 6.2 Get Datasource

**`GET /api/v1/datasources/:id`**

**Response `200 OK`:** Single datasource object (same structure as list item above, without secrets).

**Response `404 Not Found`:**

```json
{ "error": "datasource not found" }
```

### 6.3 Create Datasource

**`POST /api/v1/datasources`**

The request body varies by `provider`. The catalog backend validates the connection using the provided credentials before persisting.

**Request body (S3 example):**

```json
{
  "name": "My S3 Bucket",
  "provider": "s3",
  "metadata": {
    "bucket": "my-docs",
    "region": "us-east-1",
    "access_key_id": "AKIAIOSFODNN7EXAMPLE",
    "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
  }
}
```

**Request body (SSH/SFTP example):**

```json
{
  "name": "Production SFTP",
  "provider": "ssh_sftp",
  "metadata": {
    "host": "192.168.1.100",
    "port": 22,
    "username": "datauser",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
    "remote_path": "/data/documents"
  }
}
```

**Validation rules:**

- `name` must be non-empty and unique.
- `provider` must be a registered provider identifier (`s3` or `ssh_sftp`).
- All required fields for the given provider must be present (validated via the provider interface).
- A live connectivity check must succeed before the datasource is persisted. If the connection test fails, return `422 Unprocessable Entity` with a descriptive error message.
- Sensitive fields (`secret_access_key` for S3; `private_key` for SSH/SFTP) are encrypted before storage using the two-layer DEK/KEK scheme described in Section 9.

**Response `201 Created`:** Datasource object without secret fields.

**Response `422 Unprocessable Entity`:**

```json
{
  "error": "connection test failed: dial tcp 192.168.1.100:22: connection refused"
}
```

### 6.4 Update Datasource

**`PUT /api/v1/datasources/:id`**

Same request body structure as create. If credential fields are provided, the connectivity check is re-run before saving. If credential fields are omitted, the existing encrypted credentials are preserved.

**Response `200 OK`:** Updated datasource object without secret fields.

When a datasource is updated, for every application it is currently connected to, the catalog backend calls `PUT /v1/connectors/:connectorid` on the Digitize service to propagate the updated configuration.

### 6.5 Delete Datasource

**`DELETE /api/v1/datasources/:id`**

**Rules:**

- The datasource must not be connected to any application at the time of deletion. If it is, return `409 Conflict`.

**Response `204 No Content`:** Datasource deleted.

**Response `409 Conflict`:**

```json
{ "error": "datasource is connected to 2 application(s) and cannot be deleted" }
```

### 6.6 Connect Datasource to Application

**`PUT /api/v1/applications/:id/datasources/:datasource_id`**

Creates a link in `application_datasources` and calls `POST /v1/connectors` on the Digitize service.

**Rules:**

- The application must exist and be in `Running` status.
- The datasource must exist and be in `Running` status.
- The link must not already exist.

**Response `200 OK`:**

```json
{
  "application_id": "...",
  "datasource_id": "...",
  "connector_id": "conn-abc123",
  "status": "Initializing"
}
```

**Response `409 Conflict`:**

```json
{ "error": "datasource is already connected to this application" }
```

**Response `422 Unprocessable Entity`:**

```json
{
  "error": "application must be in Running status before connecting a datasource"
}
```

### 6.7 Disconnect Datasource from Application

**`DELETE /api/v1/applications/:id/datasources/:datasource_id`**

Removes the link from `application_datasources` and calls `DELETE /v1/connectors/:connectorid` on the Digitize service.

**Response `204 No Content`:** Disconnected.

**Response `404 Not Found`:**

```json
{ "error": "datasource is not connected to this application" }
```

### 6.8 Get Datasource Status for Application

**`GET /api/v1/applications/:id/datasources/:datasource_id`**

Fetches live status by calling `GET /v1/connectors/:connectorid` on the Digitize service and merges it with the local link record.

**Response `200 OK`:**

```json
{
  "application_id": "...",
  "datasource_id": "...",
  "connector_id": "conn-abc123",
  "status": "Running",
  "message": "",
  "files_processed": 142,
  "last_synced_at": "2026-06-01T11:00:00Z"
}
```

---

## 7. Digitize Service Integration

### 7.1 Connector API Overview

The Digitize service exposes a `/v1/connectors` API that manages the active linkage between a deployed application and a datasource. The catalog service acts as the client.

| Operation                         | Catalog Trigger                                       | Digitize API Call                                          |
| --------------------------------- | ----------------------------------------------------- | ---------------------------------------------------------- |
| Connect datasource to application | `PUT /applications/:id/datasources/:datasource_id`    | `POST /v1/connectors`                                      |
| Update datasource credentials     | `PUT /datasources/:id`                                | `PUT /v1/connectors/:connectorid` (for each connected app) |
| Disconnect datasource             | `DELETE /applications/:id/datasources/:datasource_id` | `DELETE /v1/connectors/:connectorid`                       |
| Fetch sync status                 | `GET /applications/:id/datasources/:datasource_id`    | `GET /v1/connectors/:connectorid`                          |

### 7.2 Connect Flow

When `PUT /applications/:id/datasources/:datasource_id` is called:

1. Validate application is `Running`.
2. Validate datasource is `Running`.
3. Validate the link does not already exist.
4. Decrypt the datasource credentials from the catalog database.
5. Call `POST /v1/connectors` on the Digitize service with the connector payload (see payload format below).
6. On success, store the returned `connector_id` in `application_datasources`.
7. Set link `status = "Initializing"`.

**`POST /v1/connectors` payload — S3 example:**

The payload shape is defined by the digitize proposal. The catalog service is responsible for constructing it exactly as follows before calling the endpoint.

```json
{
  "connector_id": "<generated UUID>",
  "type": "s3",
  "host": "s3.amazonaws.com",
  "allowed_extensions": [".pdf", ".docx", ".xlsx"],
  "sync_interval_seconds": 300,
  "connection_details": {
    "region": "us-east-1",
    "bucket_name": "my-docs",
    "access_key_id": "AKIAIOSFODNN7EXAMPLE",
    "secret_access_key": "<secret access key>"
  }
}
```

**`POST /v1/connectors` payload — SSH/SFTP example:**

```json
{
  "connector_id": "<generated UUID>",
  "type": "ssh_sftp",
  "host": "sftp.example.com",
  "allowed_extensions": [".pdf", ".docx", ".xlsx"],
  "sync_interval_seconds": 300,
  "connection_details": {
    "port": 22,
    "username": "sync_user",
    "remote_path": "/exports/reports",
    "private_key": "<private key>"
  }
}
```

The digitize service responds with `202 Accepted` and `{ "connector_id": "<UUID>" }`. The `connector_id` in the response **echoes back** the UUID that was sent in the request body; it is not a new ID generated by digitize. Catalog stores this in the `application_datasources.connector_id` column.

**`allowed_extensions` and `sync_interval_seconds`:** These are connector-level settings that the catalog must expose to the user on the datasource create/update form and include in both the `POST` and `PUT` payloads. They are stored alongside the other datasource metadata in the `components.metadata` JSONB column.

### 7.3 Update Flow

When `PUT /datasources/:id` is called:

1. Validate connectivity with new credentials.
2. Encrypt new credentials and update the `components` record.
3. Query `application_datasources` for all links to this datasource that have a non-null `connector_id`.
4. For each link, call `PUT /v1/connectors/:connectorid` with the updated payload.
5. The digitize service performs a partial update — only the fields sent in the payload are overwritten. Catalog may send only the changed `connection_details` keys and top-level fields.
6. If any call fails, log the error and continue — do not roll back the datasource record update.

### 7.4 Disconnect Flow

When `DELETE /applications/:id/datasources/:datasource_id` is called:

1. Look up the `connector_id` from `application_datasources`.
2. If `connector_id` is non-null, call `DELETE /v1/connectors/:connectorid` on the Digitize service.
3. If the Digitize call returns `404` (connector already gone), treat as success and continue.
4. If the Digitize call fails with any other error, log the error and continue with local deletion — the local link record must be removed regardless to keep catalog state consistent.
5. Delete the row from `application_datasources`.

### 7.5 Status Fetch Flow

When `GET /applications/:id/datasources/:datasource_id` is called:

1. Retrieve the link record from `application_datasources` to get `connector_id`.
2. Call `GET /v1/connectors/:connectorid` on the Digitize service.
3. Map the digitize response fields to the catalog response. The digitize `GET /v1/connectors/:id` response includes:
   - `sync_status` — free-form string: `"Syncing"`, `"Completed"`, `"Failed"`, or a descriptive error message
   - `files_found`, `files_syncing`, `files_completed`, `files_failed` — file counters from the last tick
   - `last_sync_at` — ISO 8601 timestamp of the last completed tick
   - `last_sync_error` — `null` or the error string from the last failed tick
4. Return the merged response to the caller.

A `GET /api/v1/applications/:id/datasources/:datasource_id/sync-history` endpoint may also be added to proxy the digitize `GET /v1/connectors/:connectorid/sync-history` endpoint, which returns a paginated history of every sync tick.

### 7.6 Bearer Token Authentication

The digitize connector API requires a bearer token for all requests (`Authorization: Bearer <token>`). Per the digitize proposal, this token is mounted as a Podman secret at `/run/secrets/connector_api_token` on the digitize pod. Catalog must store this token and include it as the `Authorization` header in all `DigitizeConnectorClient` calls.

---

## 8. Datasource Type Providers

### 8.1 Provider Interface

Each datasource type implements a `DatasourceProvider` interface responsible for:

- Declaring the required and optional metadata fields.
- Identifying which metadata fields contain sensitive data (to be encrypted).
- Performing a live connectivity test using the provided credentials.

```go
// DatasourceProvider defines the contract for a datasource type.
type DatasourceProvider interface {
    // ProviderID returns the unique identifier for this provider type (e.g. "s3", "sftp").
    ProviderID() string

    // RequiredFields returns the list of metadata field names required for this provider.
    RequiredFields() []string

    // SensitiveFields returns the list of metadata field names that contain secret values.
    SensitiveFields() []string

    // TestConnection attempts a live connection using the provided metadata.
    // Returns nil if the connection succeeds, or an error describing the failure.
    TestConnection(ctx context.Context, metadata map[string]any) error
}
```

Providers are registered in a provider registry at startup. The `POST /datasources` and `PUT /datasources/:id` handlers look up the provider by `provider` field value to run field validation and the connectivity test.

### 8.2 S3 Provider

**Provider ID:** `s3`

Field names below match the `connection_details` keys used in the digitize push payload.

| Field               | Required | Sensitive | Digitize payload key                               |
| ------------------- | -------- | --------- | -------------------------------------------------- |
| `bucket_name`       | Yes      | No        | `bucket_name`                                      |
| `region`            | Yes      | No        | `region`                                           |
| `access_key_id`     | Yes      | No        | `access_key_id`                                    |
| `secret_access_key` | Yes      | **Yes**   | `secret_access_key_ciphertext`                     |
| `endpoint_url`      | No       | No        | `host` (top-level, defaults to `s3.amazonaws.com`) |

Connectivity test: List objects in the bucket with a zero-item limit (`list_objects_v2` with `MaxKeys=0`). A successful call (even if empty) confirms valid credentials and bucket access. The digitize proposal confirms the same approach (§1 preconditions for S3).

### 8.3 Remote SSH/SFTP Provider

**Provider ID:** `ssh_sftp`

Field names below match the `connection_details` keys used in the digitize push payload.

| Field         | Required | Sensitive | Digitize payload key     |
| ------------- | -------- | --------- | ------------------------ |
| `host`        | Yes      | No        | `host` (top-level)       |
| `port`        | Yes      | No        | `port`                   |
| `username`    | Yes      | No        | `username`               |
| `private_key` | Yes      | **Yes**   | `private_key_ciphertext` |
| `remote_path` | Yes      | No        | `remote_path`            |

Connectivity test: Establish an SSH connection using the Ed25519 private key and attempt to list the contents of `remote_path`. A successful directory listing confirms valid credentials and folder access permissions.

---

## 9. Secret Encryption

The digitize proposal defines a two-layer **DEK/KEK envelope** scheme. The catalog side must produce payloads that conform to this exact scheme.

### 9.1 Encryption Scheme Overview

```
Catalog side (Go):
  catalog_KEK  (32-byte AES key, mounted from /run/secrets/catalog_kek or env var)
      │
      │  For each sensitive field on create/update:
      │  1. Generate a fresh 32-byte random DEK
      │  2. AES-256-GCM encrypt(plaintext_field, DEK)  → field_ciphertext
      │  3. AES-256-GCM encrypt(DEK, catalog_KEK)      → encrypted_dek (for storage)
      │  4. Store field_ciphertext + encrypted_dek in components.metadata JSONB
      ▼
```

### 9.2 Catalog Backend Encryption

The catalog backend pod is deployed with a **Key Encryption Key (KEK)** mounted as a secret file (e.g., `/run/secrets/catalog_kek`). This KEK is used in a two-layer scheme:

- A fresh 32-byte **Data Encryption Key (DEK)** is generated for each sensitive field on create or credential update.
- The sensitive field value is AES-256-GCM encrypted under the DEK → stored as `<field>_ciphertext`.
- The DEK is AES-256-GCM encrypted under the catalog KEK → stored as `encrypted_dek`.
- Both ciphertext values are stored as base64-encoded strings inside `components.metadata` JSONB.

This is implemented in a new `ai-services/internal/pkg/catalog/utils/crypto.go` utility package, following the convention of the existing [`utils/password.go`](../../ai-services/internal/pkg/catalog/utils/password.go).

### 9.3 Digitize Service Encryption

Each Digitize service deployment pod has its own unique KEK mounted at `/run/secrets/connector_kek`.

### 9.4 Deployment Changes

**Catalog backend deployment:**

- Mount a Kubernetes/Podman Secret containing the 32-byte catalog KEK at `/run/secrets/catalog_kek` (or an equivalent env var `CATALOG_CONNECTOR_KEK`). Generated once per environment with a cryptographically secure random source.
- Mount the digitize connector bearer token at `/run/secrets/connector_api_token` so the `DigitizeConnectorClient` can authenticate against the digitize pod's connector API.

**Digitize service deployment:**

- Each Digitize deployment pod receives its own unique 32-byte KEK mounted at `/run/secrets/connector_kek` (as defined in the digitize proposal §12).
- A bearer token for the connector API is mounted at `/run/secrets/connector_api_token`.

---

## 10. Sync Service

The **catalog-side sync service** is a lightweight credential-validation heartbeat.

| Job                         | Where           | What it does                                                                                                                                  |
| --------------------------- | --------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Catalog `DatasourceSyncJob` | Catalog backend | Periodically tests that the stored credentials can still open a connection to the remote source; updates `component_status` in the catalog DB |

### 10.1 Sync Job Design

A periodic background job (`DatasourceSyncJob`) runs in the catalog backend process on a configurable schedule (default: every 15 minutes). For each datasource in the `components` table where `type = "datasource"` and `source = "remote"`, it:

1. Loads the datasource metadata and decrypts sensitive fields using the catalog KEK/DEK scheme.
2. Looks up the registered provider for this datasource's `provider` field.
3. Calls `provider.TestConnection(ctx, metadata)`.
4. If the test passes: sets `status = "Running"`, clears `message`.
5. If the test fails: sets `status = "Error"`, stores the error string in `message`.
6. Writes the updated status back to the `components` table via `UpdateStatus()`.

### 10.2 Status Updates

The sync job uses the existing `component_repo.UpdateStatus(ctx, id, status, message)` method from [`ai-services/internal/pkg/catalog/db/repository/component_repo.go`](../../ai-services/internal/pkg/catalog/db/repository/component_repo.go).

Datasource status values (catalog side):

| Status         | Meaning                                                      |
| -------------- | ------------------------------------------------------------ |
| `Initializing` | Just created, first connectivity check not yet run           |
| `Running`      | Last connectivity check passed                               |
| `Error`        | Last connectivity check failed; `message` contains the error |

The per-application sync status (file ingestion progress) is tracked on the digitize side via `sync_status`, `files_found`, `files_syncing`, `files_completed`, `files_failed`, and `last_sync_at` fields returned by `GET /v1/connectors/:connectorid` (see §7.5).

---

## 11. Application Lifecycle Integration

### 11.1 Datasource Connection During Create Flow

When a user selects datasources during the application create flow in the UI:

1. The create application request (`POST /api/v1/applications`) includes the list of `datasource_ids` to connect after deployment.
2. The catalog backend stores the requested datasource IDs in memory (or in the application record) alongside the in-progress deployment.
3. The application deployment proceeds normally. The datasource connection step is **not** attempted until the application reaches `Running` status.
4. Once the application transitions to `Running`, the existing deployment completion callback triggers the datasource connection flow: for each requested datasource ID, call the connect logic as if `PUT /applications/:id/datasources/:datasource_id` had been called.
5. If any connector API call to the Digitize service fails, the application remains `Running` (the deployment was successful) and the failed datasource link is recorded with `status = "Error"` and the error in `message`. The UI surfaces this to the user.

### 11.2 Datasource Connection Post-Creation

Users can connect or disconnect datasources from any `Running` application at any time via the Catalog UI, using the `PUT` and `DELETE` endpoints described in Section 6. No re-deployment is required.

---

## 12. Error Handling

The feature follows the existing error response convention established in the application handler:

```json
{ "error": "<human-readable description>" }
```

| Scenario                                                 | HTTP Status                                                              |
| -------------------------------------------------------- | ------------------------------------------------------------------------ |
| Datasource not found                                     | `404 Not Found`                                                          |
| Application not found                                    | `404 Not Found`                                                          |
| Invalid request body or missing required fields          | `400 Bad Request`                                                        |
| Connectivity test failed on create/update                | `422 Unprocessable Entity`                                               |
| Datasource already connected to application              | `409 Conflict`                                                           |
| Delete attempted while datasource has active connections | `409 Conflict`                                                           |
| Application not in `Running` status during connect       | `422 Unprocessable Entity`                                               |
| Digitize `POST /v1/connectors` returns `409 Conflict`    | `409 Conflict` — connector already exists; catalog should use `PUT`      |
| Digitize service API call failed (non-404)               | `502 Bad Gateway` (with `error` field describing the downstream failure) |

---

## 13. Security Considerations

- **Credentials never returned in API responses.** Fields identified as sensitive by `DatasourceProvider.SensitiveFields()` are filtered out before any API response is serialised. For SSH/SFTP connectors the **public key** is returned (it is not a secret); the private key ciphertext is never returned. For S3 the `access_key_id` is returned; the `secret_access_key` is never returned.
- **IAM least-privilege for S3.** Only `s3:GetObject` and `s3:ListBucket` on the target bucket should be granted to the IAM user whose keys are stored.
- **Bearer token for digitize connector API.** All `DigitizeConnectorClient` calls include a bearer token (`Authorization: Bearer <token>`) sourced from a secret mount, never from an environment variable or hardcoded value.
- **Connectivity test before storage.** A datasource is only persisted after a live connection test succeeds, reducing the risk of storing incorrect credentials.
- **Auth middleware on all routes.** All datasource and application-datasource endpoints are protected by the existing `AuthMiddleware` following the pattern in [`router.go`](../../ai-services/internal/pkg/catalog/apiserver/router.go).
