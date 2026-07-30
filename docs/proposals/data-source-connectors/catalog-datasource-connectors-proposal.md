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
   - 6.6 [List Supported Datasource Providers](#66-list-supported-datasource-providers)
   - 6.7 [Get Datasource Provider Input Schema](#67-get-datasource-provider-input-schema)
   - 6.8 [Connect Datasource to Application](#68-connect-datasource-to-application)
   - 6.9 [Disconnect Datasource from Application](#69-disconnect-datasource-from-application)
   - 6.10 [Get Datasource Status for Application](#610-get-datasource-status-for-application)
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
   - 9.2 [Deployment Changes](#92-deployment-changes)
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

**Datasource Connector**: A named, typed, remote content store with connection credentials. Datasources are tenant-level resources — they are not owned by a single application.

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
| `status`     | `"Connected"` / `"Disconnected"`                                                                             |
| `message`    | `""` / `"Permission denied on bucket"`                                                                       |
| `metadata`   | `{"bucket": "my-docs", "region": "us-east-1", "access_key_id": "AK...", "secret_access_key": "<encrypted>"}` |
| `version`    | `"1"`                                                                                                        |
| `created_at` | timestamp                                                                                                    |
| `updated_at` | timestamp                                                                                                    |

### 4.2 Application-Datasource Join Table

A new join table `application_datasources` captures the many-to-many relationship between applications and datasources. It stores only the `connector_id` returned by the Digitize service — no status, message, or file counters are cached here. All per-link runtime state is fetched live from the Digitize service on demand.

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE application_datasources (
    application_id  UUID NOT NULL,
    datasource_id   UUID NOT NULL,
    connector_id    VARCHAR(255),           -- ID returned by Digitize /v1/connectors
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (application_id, datasource_id),
    CONSTRAINT fk_application_id FOREIGN KEY (application_id)
        REFERENCES applications(id) ON DELETE CASCADE,
    CONSTRAINT fk_datasource_id FOREIGN KEY (datasource_id)
        REFERENCES components(id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS application_datasources;
-- +goose StatementEnd
```

**Key design decisions:**

- `(application_id, datasource_id)` is the composite primary key — this enforces uniqueness of the link.
- `connector_id` is nullable: it is `NULL` until the Digitize service successfully creates the connector and returns an ID.
- `ON DELETE CASCADE` on `application_id` — deleting an application automatically removes its datasource links.
- No cascade on `datasource_id` — a datasource cannot be deleted while it is linked to an application (enforced at the service layer, not by a DB constraint).

### 4.3 New Enum Values

Two new values are added to the existing `component_status` enum to represent the connectors-page status of a datasource:

```sql
-- Migration: extend component_status with datasource connectivity states
ALTER TYPE component_status ADD VALUE 'Connected';
ALTER TYPE component_status ADD VALUE 'Disconnected';
```

| Value          | Meaning                                                                                                                                                 |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Connected`    | The sync job successfully reached the datasource — credentials valid, files/folders accessible                                                          |
| `Disconnected` | The sync job failed to reach the datasource — could be invalid credentials, permission error, network issue, etc. `message` contains the specific error |

### 4.4 Migration Plan

Three new goose migration files, numbered after the current highest (`20260430094506`):

| File                                                      | Purpose                                                   |
| --------------------------------------------------------- | --------------------------------------------------------- |
| `20260430094507_add_source_to_components.sql`             | Adds `source` column to `components`                      |
| `20260430094508_extend_component_status_enum.sql`         | Adds `Connected` and `Disconnected` to `component_status` |
| `20260430094509_create_application_datasources_table.sql` | Creates the join table                                    |

---

## 5. API Specification

### 5.1 Datasource CRUD APIs

All routes are under `/api/v1` and protected by the existing `AuthMiddleware`.

| Method   | Path                                                    | Description                                                    |
| -------- | ------------------------------------------------------- | -------------------------------------------------------------- |
| `GET`    | `/connectors/datasources`                               | List all datasources (paginated, filterable by status)         |
| `GET`    | `/connectors/datasources/:id`                           | Get a single datasource by ID                                  |
| `POST`   | `/connectors/datasources`                               | Create a new datasource (validates connectivity first)         |
| `PUT`    | `/connectors/datasources/:id`                           | Update datasource metadata / credentials                       |
| `DELETE` | `/connectors/datasources/:id`                           | Delete a datasource (only if not connected to any application) |
| `GET`    | `/connectors/datasources/providers`                     | List all supported datasource provider types                   |
| `GET`    | `/connectors/datasources/providers/:provider_id/params` | Get the input schema for a specific provider type              |

### 5.2 Application-Datasource Connection APIs

| Method   | Path                                                      | Description                                                         |
| -------- | --------------------------------------------------------- | ------------------------------------------------------------------- |
| `PUT`    | `/applications/:id/connectors/datasources/:datasource_id` | Connect a datasource to an application                              |
| `DELETE` | `/applications/:id/connectors/datasources/:datasource_id` | Disconnect a datasource from an application                         |
| `GET`    | `/applications/:id/connectors/datasources/:datasource_id` | Get datasource status/details for a specific application connection |

---

## 6. API Endpoint Details

### 6.1 List Datasources

**`GET /api/v1/connectors/datasources`**

Query parameters:

| Parameter   | Type   | Required | Description                                   |
| ----------- | ------ | -------- | --------------------------------------------- |
| `page`      | int    | No       | Page number (default: 1)                      |
| `page_size` | int    | No       | Items per page (default: 20, max: 100)        |
| `status`    | string | No       | Filter by status: `Connected`, `Disconnected` |
| `provider`  | string | No       | Filter by provider type: `s3`, `sftp`         |

**Response `200 OK`:**

```json
{
  "data": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "My S3 Bucket",
      "type": "datasource",
      "provider": "s3",
      "status": "Connected",
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

**`GET /api/v1/connectors/datasources/:id`**

**Response `200 OK`:** Single datasource object (same structure as list item above, without secrets).

**Response `404 Not Found`:**

```json
{ "error": "datasource not found" }
```

### 6.3 Create Datasource

**`POST /api/v1/connectors/datasources`**

The request body varies by `provider`. The catalog backend validates the connection using the provided credentials before persisting.

**Request body (S3 example):**

```json
{
  "name": "My S3 Bucket",
  "provider": "s3",
  "metadata": {
    "bucket_name": "my-docs",
    "region": "us-east-1",
    "access_key_id": "AKIAIOSFODNN7EXAMPLE",
    "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    "endpoint_url": "https://s3.amazonaws.com",
    "prefix": "reports/2026/",
    "delimiter": "/",
    "allowed_extensions": [".pdf", ".docx", ".xlsx"]
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
    "username": "datauser",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
    "remote_path": "/data/documents",
    "allowed_extensions": [".pdf", ".docx", ".txt"]
  }
}
```

**Validation rules:**

- `name` must be non-empty and unique.
- `provider` must be a registered provider identifier (`s3` or `ssh_sftp`).
- All required fields for the given provider must be present (validated via the provider interface).
- A live connectivity check must succeed before the datasource is persisted. If the connection test fails, return `422 Unprocessable Entity` with a descriptive error message.
- Sensitive fields (`secret_access_key` for S3; `private_key` for SSH/SFTP) are stored securely as described in Section 9.

**Response `201 Created`:** Datasource object without secret fields.

**Response `422 Unprocessable Entity`:**

```json
{
  "error": "connection test failed: dial tcp 192.168.1.100:22: connection refused"
}
```

### 6.4 Update Datasource

**`PUT /api/v1/connectors/datasources/:id`**

Same request body structure as create. If credential fields are provided, the connectivity check is re-run before saving. If credential fields are omitted, the existing encrypted credentials are preserved.

**Response `200 OK`:** Updated datasource object without secret fields.

When a datasource is updated, for every application it is currently connected to, the catalog backend calls `PUT /v1/connectors/:connectorid` on the Digitize service to propagate the updated configuration.

### 6.5 Delete Datasource

**`DELETE /api/v1/connectors/datasources/:id`**

**Rules:**

- The datasource must not be connected to any application at the time of deletion. If it is, return `409 Conflict`.

**Response `204 No Content`:** Datasource deleted.

**Response `409 Conflict`:**

```json
{ "error": "datasource is connected to 2 application(s) and cannot be deleted" }
```

### 6.6 List Supported Datasource Providers

**`GET /api/v1/connectors/datasources/providers`**

Returns all registered datasource provider types. The UI uses this to populate the "Add Datasource" provider picker. Reuses the same provider registry queried by `DatasourceProvider.ProviderID()`.

**Response `200 OK`:**

```json
{
  "providers": [
    {
      "provider_id": "s3",
      "display_name": "Amazon S3",
      "description": "Amazon S3 bucket via AWS credentials"
    },
    {
      "provider_id": "ssh_sftp",
      "display_name": "Remote SSH / SFTP",
      "description": "Remote server accessible via SSH private key"
    }
  ]
}
```

> This endpoint mirrors the pattern of `GET /api/v1/services` — it reads from the in-process provider registry, not the database. No new storage is required.

### 6.7 Get Datasource Provider Input Schema

**`GET /api/v1/connectors/datasources/providers/:provider_id/params`**

Returns the JSON Schema describing the input fields required to create or update a datasource of the given provider type. The UI renders the form dynamically from this schema — exactly as `GET /api/v1/components/:component_type/providers/:provider_id/params` does for component configuration.

The schema is derived from the provider's `RequiredFields()` and `SensitiveFields()` declarations. Sensitive fields are marked with `"x-sensitive": true` so the UI can render them as password inputs.

**Response `200 OK` (S3 example):**

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "bucket_name": { "type": "string", "title": "Bucket Name" },
    "region": { "type": "string", "title": "AWS Region" },
    "access_key_id": { "type": "string", "title": "Access Key ID" },
    "secret_access_key": {
      "type": "string",
      "title": "Secret Access Key",
      "x-sensitive": true
    },
    "endpoint_url": { "type": "string", "title": "Endpoint URL (optional)" }
  },
  "required": ["bucket_name", "region", "access_key_id", "secret_access_key"]
}
```

**Response `404 Not Found`:**

```json
{ "error": "unknown provider: my_unknown_provider" }
```

> This endpoint mirrors `GET /api/v1/components/:component_type/providers/:provider_id/params`. Implementation follows the same pattern as [`CatalogHandler.GetComponentProviderParams`](../../ai-services/internal/pkg/catalog/apiserver/handlers/catalog.go).

### 6.8 Connect Datasource to Application

**`PUT /api/v1/applications/:id/connectors/datasources/:datasource_id`**

Creates a link in `application_datasources` and calls `POST /v1/connectors` on the Digitize service.

**Rules:**

- The application must exist and be in `Running` status.
- The datasource must exist and be in `Connected` status.
- The link must not already exist.

**Response `200 OK`:**

```json
{
  "application_id": "...",
  "datasource_id": "...",
  "connector_id": "conn-abc123"
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

### 6.9 Disconnect Datasource from Application

**`DELETE /api/v1/applications/:id/connectors/datasources/:datasource_id`**

Removes the link from `application_datasources` and calls `DELETE /v1/connectors/:connectorid` on the Digitize service.

**Response `204 No Content`:** Disconnected.

**Response `404 Not Found`:**

```json
{ "error": "datasource is not connected to this application" }
```

### 6.10 Get Datasource Status for Application

**`GET /api/v1/applications/:id/connectors/datasources/:datasource_id`**

The `application_id` in the path is used only to resolve the `connector_id` from `application_datasources`. The response is the raw payload returned by `GET /v1/connectors/:connectorid` on the Digitize service, passed through verbatim — no catalog fields are added or merged.

**Response `200 OK`:**

```json
{
  "connector_id": "conn-abc123",
  "type": "s3",
  "sync_status": "Completed",
  "files_found": 150,
  "files_syncing": 0,
  "files_completed": 150,
  "files_failed": 8,
  "last_sync_at": "2026-06-01T11:00:00Z",
  "last_sync_error": null
}
```

**Response `404 Not Found`:** Returned if the datasource is not connected to this application (no row in `application_datasources`).

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
2. Validate datasource is `Connected`.
3. Validate the link does not already exist.
4. Call `POST /v1/connectors` on the Digitize service with the connector payload (see payload format below).
5. On success, store the returned `connector_id` in `application_datasources`.

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

When `PUT /api/v1/connectors/datasources/:id` is called:

1. Validate connectivity with new credentials.
2. Update the `components` record with the new credentials.
3. Query `application_datasources` for all links to this datasource that have a non-null `connector_id`.
4. For each link, call `PUT /v1/connectors/:connectorid` on the Digitize service with the updated payload.
   - If the call fails, retry **once**.
   - If the retry also fails: record the error for that link and continue to the next — do **not** roll back the datasource record update.
5. The digitize service performs a partial update — only the fields sent in the payload are overwritten.
6. Return `200 OK`. If any propagations failed, include a `propagation_errors` array in the response so the UI can show an inline error against each affected application. The user retriggers by re-submitting the datasource update from the UI.

**Response when all propagations succeed:**

```json
{
  "id": "550e8400-...",
  "name": "My S3 Bucket",
  "provider": "s3",
  "status": "Connected"
}
```

**Response when one or more propagations fail:**

```json
{
  "id": "550e8400-...",
  "name": "My S3 Bucket",
  "provider": "s3",
  "status": "Connected",
  "propagation_errors": [
    {
      "application_id": "app-uuid-1",
      "application_name": "My App",
      "error": "failed to reach digitize service after 1 retry: connection refused"
    }
  ]
}
```

### 7.4 Disconnect Flow

When `DELETE /applications/:id/datasources/:datasource_id` is called:

1. Look up the `connector_id` from `application_datasources`.
2. If `connector_id` is non-null, call `DELETE /v1/connectors/:connectorid` on the Digitize service.
3. If the Digitize call returns `404` (connector already gone), treat as success and continue.
4. If the Digitize call fails with any other error, log the error and continue with local deletion — the local link record must be removed regardless to keep catalog state consistent.
5. Delete the row from `application_datasources`.

### 7.5 Status Fetch Flow

When `GET /api/v1/applications/:id/connectors/datasources/:datasource_id` is called:

1. Look up the link row in `application_datasources` using `(application_id, datasource_id)`. Return `404` if not found.
2. Call `GET /v1/connectors/:connectorid` on the Digitize service using the `connector_id` from that row.
3. Return the Digitize response body verbatim to the caller. No mapping, merging, or transformation is applied.

### 7.6 Bearer Token Authentication

The digitize connector API requires a bearer token for all requests (`Authorization: Bearer <token>`). Per the digitize proposal, this token is mounted as a Podman secret at `/run/secrets/connector_api_token` on the digitize pod. Catalog must store this token and include it as the `Authorization` header in all `DigitizeConnectorClient` calls.

---

## 8. Datasource Type Providers

### 8.1 Provider Interface

Each datasource type implements a `DatasourceProvider` interface responsible for:

- Declaring the required and optional metadata fields.
- Identifying which metadata fields contain sensitive data.
- Providing display metadata used by the `GET /connectors/datasources/providers` endpoint.
- Performing a live connectivity test using the provided credentials.

```go
// DatasourceProvider defines the contract for a datasource type.
type DatasourceProvider interface {
    // ProviderID returns the unique identifier for this provider type (e.g. "s3", "ssh_sftp").
    ProviderID() string

    // DisplayName returns the human-readable name shown in the UI provider picker.
    DisplayName() string

    // Description returns a short description of the provider type for the UI.
    Description() string

    // RequiredFields returns the list of metadata field names required for this provider.
    RequiredFields() []string

    // SensitiveFields returns the list of metadata field names that contain secret values.
    // These are marked x-sensitive:true in the params schema so the UI renders them as password inputs.
    SensitiveFields() []string

    // TestConnection attempts a live connection using the provided metadata.
    // Returns nil if the connection succeeds, or an error describing the failure.
    TestConnection(ctx context.Context, metadata map[string]any) error
}
```

Providers are registered in a provider registry at startup. The `POST /connectors/datasources` and `PUT /connectors/datasources/:id` handlers look up the provider by `provider` field value to run field validation and the connectivity test. The `GET /connectors/datasources/providers` and `GET /connectors/datasources/providers/:provider_id/params` endpoints read directly from the same registry — no database access required.

### 8.2 S3 Provider

**Provider ID:** `s3`

Field names below match the `connection_details` keys used in the digitize push payload.

| Field                | Required | Sensitive | Notes                                                               |
| -------------------- | -------- | --------- | ------------------------------------------------------------------- |
| `bucket_name`        | Yes      | No        |                                                                     |
| `region`             | Yes      | No        |                                                                     |
| `access_key_id`      | Yes      | No        |                                                                     |
| `secret_access_key`  | Yes      | **Yes**   |                                                                     |
| `endpoint_url`       | No       | No        | Defaults to `https://s3.amazonaws.com`                              |
| `prefix`             | No       | No        | Key prefix to scope the sync to a folder                            |
| `delimiter`          | No       | No        | Delimiter for hierarchical listing (typically `/`)                  |
| `allowed_extensions` | No       | No        | e.g. `[".pdf", ".docx", ".xlsx"]`; if omitted, all files are synced |

Connectivity test: List objects in the bucket (scoped to `prefix` if provided) with a zero-item limit (`list_objects_v2` with `MaxKeys=0`). A successful call confirms valid credentials and bucket/prefix access.

### 8.3 Remote SSH/SFTP Provider

**Provider ID:** `ssh_sftp`

Field names below match the `connection_details` keys used in the digitize push payload.

| Field                | Required | Sensitive | Notes                                                     |
| -------------------- | -------- | --------- | --------------------------------------------------------- |
| `host`               | Yes      | No        |                                                           |
| `username`           | Yes      | No        |                                                           |
| `private_key`        | Yes      | **Yes**   | Ed25519 private key                                       |
| `remote_path`        | Yes      | No        | Absolute path on the remote server to sync from           |
| `allowed_extensions` | No       | No        | e.g. `[".pdf", ".txt"]`; if omitted, all files are synced |

Connectivity test: Establish an SSH connection using the private key and attempt to list the contents of `remote_path`. A successful directory listing confirms valid credentials and folder/file access permissions.

---

## 9. Secret Encryption

### 9.1 Encryption Scheme Overview

A unique **Key Encryption Key (KEK)** is generated at application deploy time and deployed as a Kubernetes/Podman Secret. It is mounted into each Digitize service pod in that application. There is no DEK layer — the KEK is the only key.

The catalog backend passes plaintext credentials to the Digitize service over the authenticated connector API; encryption at rest is the responsibility of the receiving service.

### 9.2 Deployment Changes

- At application deploy time, generate a cryptographically secure random 32-byte KEK.
- Deploy it as a Kubernetes/Podman Secret scoped to that application's namespace.
- Mount the secret into each Digitize service pod (at `/run/secrets/connector_kek`).
- Mount the digitize connector bearer token at `/run/secrets/connector_api_token` so the `DigitizeConnectorClient` can authenticate against each Digitize pod's connector API.

---

## 10. Sync Service

The **catalog-side sync service** is a common, connector-agnostic heartbeat that validates the health of any remotely-managed component registered in the catalog. It is designed to serve datasource connectors today and to be extended for future connector types (e.g., vector stores, models) without structural change.

### 10.1 Sync Job Design

A single periodic background job (`ConnectorSyncJob`) runs in the catalog backend process on a configurable schedule (default: every 15 minutes). It queries the `components` table for all records where `source = "remote"`, regardless of `type`. For each such component:

1. Looks up the registered provider for the component's `type` and `provider` fields.
2. Calls `provider.TestConnection(ctx, metadata)`.
3. If the test passes: sets `status = "Connected"`, clears `message`.
4. If the test fails: sets `status = "Disconnected"`, stores the specific error string in `message` (e.g., credential failure, permission denied, network unreachable).
5. Writes the updated status back to the `components` table via `UpdateStatus()`.

New connector types (vector stores, models, etc.) plug in by implementing the same `ConnectorProvider` interface (see Section 8.1) and registering with the provider registry. No changes to the sync job itself are required.

### 10.2 Status Updates

The sync job uses the existing `component_repo.UpdateStatus(ctx, id, status, message)` method from [`ai-services/internal/pkg/catalog/db/repository/component_repo.go`](../../ai-services/internal/pkg/catalog/db/repository/component_repo.go).

There are two distinct status spaces:

**Connectors page** (`components.status`, driven by `ConnectorSyncJob`):

| Status         | Meaning                                                                                                                                                    |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Connected`    | Sync job successfully reached the datasource — credentials valid, files/folders accessible                                                                 |
| `Disconnected` | Sync job could not reach the datasource — invalid credentials, permission error on folder/files, network issue, etc. `message` contains the specific error |

**Application-datasources page** (`application_datasources.status`, driven by Digitize service):

The catalog backend fetches status from `GET /v1/connectors/:connectorid` (see §7.5) and cascades all fields — `status`, `message`, `files_processed`, `last_synced_at`, etc. — to the UI verbatim. No mapping or transformation is applied.

---

## 11. Application Lifecycle Integration

### 11.1 Datasource Connection During Create Flow

#### Request Body Change

The existing [`CreateApplicationRequest`](../../ai-services/internal/pkg/catalog/apiserver/models/create_application.go) is extended with an optional `datasource_ids` field:

```go
// CreateApplicationRequest represents the request body for creating a new application.
type CreateApplicationRequest struct {
    Name          string    `json:"name"           binding:"required,min=3,max=100"`
    CatalogID     string    `json:"catalog_id"     binding:"required"`
    Version       string    `json:"version"        binding:"required"`
    Services      []Service `json:"services"       binding:"required,dive"`
    DatasourceIDs []string  `json:"datasource_ids"` // Optional: UUIDs of datasources to connect post-deploy
    CreatedBy     string    `json:"-"`              // Set from auth context, not from request body
}
```

**Example request body with datasources:**

```json
{
  "name": "My App",
  "catalog_id": "rag-pattern",
  "version": "1.0.0",
  "services": [
    {
      "catalog_id": "digitize",
      "version": "1.2.0",
      "components": [
        {
          "component_type": "llm",
          "provider_id": "watsonx",
          "version": "1.0.0"
        }
      ]
    }
  ],
  "datasource_ids": [
    "550e8400-e29b-41d4-a716-446655440000",
    "661f9511-f30c-52e5-b827-557766551111"
  ]
}
```

- `datasource_ids` is optional. If omitted or empty, the create flow proceeds unchanged.
- Each ID must reference an existing datasource in `Connected` status. Any ID that fails validation is returned as a `400 Bad Request` before deployment begins.

#### Post-Deploy Connect Flow

1. The deployment proceeds normally. The datasource connection step is **not** attempted until the application reaches `Running` status.
2. Once the application transitions to `Running`, the deployment completion callback triggers the connect flow: for each `datasource_id`, call the same logic as `PUT /api/v1/applications/:id/connectors/datasources/:datasource_id`.
3. If any Digitize `POST /v1/connectors` call fails after one retry, the application remains `Running` (deployment was successful). The failed link is recorded in `application_datasources` with a `NULL` `connector_id`. The UI surfaces this to the user so they can retrigger the connection manually from the application-datasources page.

### 11.2 Datasource Connection Post-Creation

Users can connect or disconnect datasources from any `Running` application at any time via the Catalog UI, using the `PUT` and `DELETE` endpoints described in Section 6. No re-deployment is required.

---

## 12. Error Handling

The feature follows the existing error response convention established in the application handler:

```json
{ "error": "<human-readable description>" }
```

| Scenario                                                 | HTTP Status                                                                                               |
| -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Datasource not found                                     | `404 Not Found`                                                                                           |
| Application not found                                    | `404 Not Found`                                                                                           |
| Invalid request body or missing required fields          | `400 Bad Request`                                                                                         |
| Connectivity test failed on create/update                | `422 Unprocessable Entity`                                                                                |
| Datasource already connected to application              | `409 Conflict`                                                                                            |
| Delete attempted while datasource has active connections | `409 Conflict`                                                                                            |
| Application not in `Running` status during connect       | `422 Unprocessable Entity`                                                                                |
| Digitize `POST /v1/connectors` returns `409 Conflict`    | `409 Conflict` — connector already exists; catalog should use `PUT`                                       |
| Digitize `PUT /v1/connectors` failed after 1 retry       | `200 OK` — datasource update succeeds; `propagation_errors` array in response lists affected applications |
| Digitize service API call failed (non-404, non-PUT)      | `502 Bad Gateway` (with `error` field describing the downstream failure)                                  |

---

## 13. Security Considerations

- **Credentials never returned in API responses.** Fields identified as sensitive by `DatasourceProvider.SensitiveFields()` are filtered out before any API response is serialised. For SSH/SFTP connectors the **public key** is returned (it is not a secret); the private key ciphertext is never returned. For S3 the `access_key_id` is returned; the `secret_access_key` is never returned.
- **IAM least-privilege for S3.** Only `s3:GetObject` and `s3:ListBucket` on the target bucket should be granted to the IAM user whose keys are stored.
- **Bearer token for digitize connector API.** All `DigitizeConnectorClient` calls include a bearer token (`Authorization: Bearer <token>`) sourced from a secret mount, never from an environment variable or hardcoded value.
- **Connectivity test before storage.** A datasource is only persisted after a live connection test succeeds, reducing the risk of storing incorrect credentials.
- **Auth middleware on all routes.** All datasource and application-datasource endpoints are protected by the existing `AuthMiddleware` following the pattern in [`router.go`](../../ai-services/internal/pkg/catalog/apiserver/router.go).
