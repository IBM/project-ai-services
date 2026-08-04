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
   - 6.6 [List Supported Datasource Provider Types](#66-list-supported-datasource-provider-types)
   - 6.7 [Get Datasource Provider Input Schema](#67-get-datasource-provider-input-schema)
   - 6.8 [Get Connected Services for Datasource](#68-get-connected-services-for-datasource)
   - 6.9 [Connect Datasource to Application](#69-connect-datasource-to-application)
   - 6.10 [Disconnect Datasource from Application](#610-disconnect-datasource-from-application)
   - 6.11 [Get Datasource Status for Application](#611-get-datasource-status-for-application)
7. [Digitize Service Integration](#7-digitize-service-integration)
   - 7.1 [Connector API Overview](#71-connector-api-overview)
   - 7.2 [Connect Flow](#72-connect-flow)
   - 7.3 [Update Flow](#73-update-flow)
   - 7.4 [Disconnect Flow](#74-disconnect-flow)
   - 7.5 [Status Fetch Flow](#75-status-fetch-flow)
8. [Datasource Type Providers](#8-datasource-type-providers)
   - 8.1 [Provider Interface](#81-provider-interface)
   - 8.2 [S3 Provider](#82-s3-provider)
   - 8.3 [Remote SSH Provider](#83-remote-ssh-provider)
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

This proposal introduces **Datasource Connectors** to the AI Services Catalog UI. A datasource represents a remote content source (e.g., S3 bucket, remote SSH server) that can be registered centrally and connected to one or more deployed applications. Once connected, the services within that application can consume files from the datasource.

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

| Type       | Digitize `type` value | Key Credentials                                                     |
| ---------- | --------------------- | ------------------------------------------------------------------- |
| Amazon S3  | `s3`                  | `endpoint_url`, `bucket_name`, `access_key_id`, `secret_access_key` |
| Remote SSH | `ssh`                 | `host`, `username`, `private_key` (Ed25519), `remote_path`          |

Additional provider types can be added by implementing the provider interface described in Section 8.

### 3.3 Component Integration

Datasources are stored in the existing `components` table using:

- `type = "datasource"`
- `provider` = the datasource type identifier (e.g., `"s3"`, `"ssh"`)
- `source = "remote"` — a new column distinguishing remotely-managed components from catalog-installed ones
- Sensitive credential fields within `metadata` are encrypted at rest (see Section 9)

This approach reuses the existing `components` infrastructure (repository, status enum, JSONB metadata) without introducing a separate table for datasources.

---

## 4. Database Design

### 4.1 Reuse of Components Table

The `components` table is extended with two new columns:

```sql
-- Migration: add source and instance_name columns to components
ALTER TABLE components ADD COLUMN source        VARCHAR(50)  NOT NULL DEFAULT 'catalog';
-- Possible values: 'catalog' | 'remote'
ALTER TABLE components ADD COLUMN instance_name VARCHAR(255) NULL;
-- Stores the user-supplied display name for remotely-managed components (e.g. datasource name).
-- NULL for catalog-installed components, which are identified by type + provider instead.
```

A datasource record in the `components` table looks like:

| Column          | Example Value                                                                         |
| --------------- | ------------------------------------------------------------------------------------- |
| `id`            | `uuid`                                                                                |
| `type`          | `"datasource"`                                                                        |
| `provider`      | `"s3"`                                                                                |
| `source`        | `"remote"`                                                                            |
| `instance_name` | `"My S3 Bucket"`                                                                      |
| `status`        | `"Connected"` / `"Offline"`                                                      |
| `message`       | `""` / `"Permission denied on bucket"`                                                |
| `metadata`      | `{"bucket": "my-docs", "access_key_id": "AK...", "secret_access_key": "<encrypted>"}` |
| `version`       | `"1"`                                                                                 |
| `created_at`    | timestamp                                                                             |
| `updated_at`    | timestamp                                                                             |

### 4.2 Reuse of `service_dependencies` for Datasource Links

No new join table is required. Datasource links are recorded in the existing `service_dependencies` table using `dependency_type = 'component'` — the same type already used for catalog-installed components.

**How datasource links are distinguished from install-time component links:**

Both use `dependency_type = 'component'`. Datasource components are distinguished by joining to `components` and filtering on `type = 'datasource'` AND `source = 'remote'`. Install-time components have `source = 'catalog'`.

```sql
-- Query: find all datasource dependencies for a service
SELECT sd.dependency_id AS datasource_id
FROM service_dependencies sd
JOIN components c ON c.id = sd.dependency_id
WHERE sd.service_id   = $1
  AND sd.dependency_type = 'component'
  AND c.type         = 'datasource'
  AND c.source       = 'remote';
```

**Cascade behaviour** is already correct: `ON DELETE CASCADE` on `service_id` means deleting a service automatically removes its datasource links.

### 4.3 New Enum Values

Two new values are added to the existing `component_status` enum to represent the connectors-page status of a datasource:

```sql
-- Migration: extend component_status with datasource connectivity states
ALTER TYPE component_status ADD VALUE 'Connected';
ALTER TYPE component_status ADD VALUE 'Offline';
```

| Value          | Meaning                                                                                                                                                 |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Connected`    | The sync job successfully reached the datasource — credentials valid, files/folders accessible                                                          |
| `Offline`      | The sync job failed to reach the datasource — could be invalid credentials, permission error, network issue, etc. `message` contains the specific error |

### 4.4 Migration Plan

Two new goose migration files, numbered after the current highest (`20260430094506`).

| File                                              | Purpose                                                   |
| ------------------------------------------------- | --------------------------------------------------------- |
| `20260430094507_add_source_to_components.sql`     | Adds `source` and `instance_name` columns to `components` |
| `20260430094508_extend_component_status_enum.sql` | Adds `Connected` and `Offline` to `component_status` |

---

## 5. API Specification

### 5.1 Datasource CRUD APIs

All routes are under `/api/v1` and protected by the existing `AuthMiddleware`.

| Method   | Path                                                        | Description                                                                                |
| -------- | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| `GET`    | `/connectors/datasources`                                   | List all datasources (paginated, filterable by status)                                     |
| `GET`    | `/connectors/datasources/:id`                               | Get a single datasource by ID                                                              |
| `POST`   | `/connectors/datasources`                                   | Create a new datasource (validates connectivity first)                                     |
| `PUT`    | `/connectors/datasources/:id`                               | Update datasource credentials                                                              |
| `DELETE` | `/connectors/datasources/:id`                               | Delete a datasource (only if not connected to any application)                             |
| `GET`    | `/connectors/datasources/:id/services`                      | List all services connected to a datasource with sync status                               |
| `GET`    | `/components?type=datasource`                               | List all supported datasource provider types (reuses existing API)                         |
| `GET`    | `/components/:component_type/providers/:provider_id/params` | Get the input schema for a provider (reuses existing API; use `component_type=datasource`) |

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
| `status`    | string | No       | Filter by status: `Connected`, `Offline`          |
| `provider`  | string | No       | Filter by provider type: `s3`, `ssh`          |

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
    "endpoint_url": "https://s3.us-east-1.amazonaws.com",
    "bucket_name": "my-docs",
    "access_key_id": "AKIAIOSFODNN7EXAMPLE",
    "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    "prefix": "reports/2026/",
    "delimiter": "/",
    "allowed_extensions": [".pdf", ".docx"]
  }
}
```

**Request body (SSH example):**

```json
{
  "name": "Production SSH",
  "provider": "ssh",
  "metadata": {
    "host": "192.168.1.100",
    "username": "datauser",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
    "remote_path": "/data/documents",
    "allowed_extensions": [".pdf", ".docx"]
  }
}
```

**Validation rules:**

- `name` must be non-empty and unique. It is stored in the `components.instance_name` column and surfaced as `name` in all API responses.
- `provider` must be a registered provider identifier (`s3` or `ssh`).
- All required fields for the given provider must be present (validated via the provider interface).
- A live connectivity check must succeed before the datasource is persisted. If the connection test fails, return `422 Unprocessable Entity` with a descriptive error message.
- Sensitive fields (`secret_access_key` for S3; `private_key` for SSH) are stored securely as described in Section 9.

**Response `201 Created`:** Datasource object without secret fields.

**Response `422 Unprocessable Entity`:**

```json
{
  "error": "connection test failed: dial tcp 192.168.1.100:22: connection refused"
}
```

### 6.4 Update Datasource

**`PUT /api/v1/connectors/datasources/:id`**

Only the credential fields for the datasource's provider may be updated. Structural fields (bucket, host, path, etc.) are immutable after creation. Any non-updatable field present in the request body is ignored.

| Provider | Updatable fields                     |
| -------- | ------------------------------------ |
| `s3`     | `access_key_id`, `secret_access_key` |
| `ssh`    | `username`, `private_key`            |

The connectivity check is always re-run with the new credentials before saving. If the check fails, return `422 Unprocessable Entity` and leave the existing record unchanged.

**Request body (S3 example):**

```json
{
  "metadata": {
    "access_key_id": "AKIANEWKEYEXAMPLE",
    "secret_access_key": "newSecretKeyValue"
  }
}
```

**Request body (SSH example):**

```json
{
  "metadata": {
    "username": "new_user",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n..."
  }
}
```

**Response `200 OK`:** Updated datasource object without secret fields.

When a datasource is updated, for every application it is currently connected to, the catalog backend calls `PUT /v1/connectors/:connectorid` on the Digitize service to propagate the updated credentials.

### 6.5 Delete Datasource

**`DELETE /api/v1/connectors/datasources/:id`**

**Rules:**

- The datasource must not be connected to any application at the time of deletion. If it is, return `409 Conflict`.

**Response `204 No Content`:** Datasource deleted.

**Response `409 Conflict`:**

```json
{ "error": "datasource is connected to 2 application(s) and cannot be deleted" }
```

### 6.6 List Supported Datasource Provider Types

**Reuses:** `GET /api/v1/components?type=datasource`

The existing [`GET /components`](../../ai-services/internal/pkg/catalog/apiserver/router.go) endpoint is extended to accept a `type` query parameter. When `type=datasource` is passed, it returns only the registered datasource provider types from the provider registry. The UI uses this to populate the "Add Datasource" provider picker.

This same pattern can be extended to future connector types (ex. vector store, model etc).

**Example:** `GET /api/v1/components?type=datasource`

**Response `200 OK`:**

```json
[
  {
    "component_type": "datasource",
    "provider_id": "s3",
    "display_name": "Amazon S3",
    "description": "Amazon S3 bucket via AWS credentials"
  },
  {
    "component_type": "datasource",
    "provider_id": "ssh",
    "display_name": "Remote SSH",
    "description": "Remote server accessible via SSH private key"
  }
]
```

### 6.7 Get Datasource Provider Input Schema

**Reuses:** `GET /api/v1/components/:component_type/providers/:provider_id/params`

The existing [`CatalogHandler.GetComponentProviderParams`](../../ai-services/internal/pkg/catalog/apiserver/handlers/catalog.go) handler already accepts a `component_type` path parameter. Datasource schemas are served by calling this endpoint with `component_type=datasource`.

**Example:** `GET /api/v1/components/datasource/providers/s3/params`

The schema is derived from the datasource provider's `RequiredFields()` and `SensitiveFields()` declarations and stored as a JSON Schema file in the catalog, following the same convention as existing component schemas. Sensitive fields are marked `"x-sensitive": true` so the UI renders them as password inputs.

**Response `200 OK` (S3 example):**

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "bucket_name": { "type": "string", "title": "Bucket Name" },
    "access_key_id": { "type": "string", "title": "Access Key ID" },
    "secret_access_key": {
      "type": "string",
      "title": "Secret Access Key",
      "x-sensitive": true
    },
    "endpoint_url": {
      "type": "string",
      "title": "Endpoint URL",
      "description": "Defaults to https://s3.amazonaws.com"
    },
    "prefix": { "type": "string", "title": "Key Prefix" },
    "delimiter": { "type": "string", "title": "Delimiter", "default": "/" },
    "allowed_extensions": {
      "type": "array",
      "title": "Allowed File Extensions",
      "items": { "type": "string" }
    }
  },
  "required": ["bucket_name", "secret_access_key"]
}
```

**Response `404 Not Found`:**

```json
{
  "error": "component type or provider not found: datasource/my_unknown_provider"
}
```

### 6.8 Get Connected Services for Datasource

**`GET /api/v1/connectors/datasources/:id/services`**

Returns the list of services currently connected to a datasource, enriched with live sync status fetched from each service's Digitize pod.

**Response `200 OK`:**

```json
{
  "datasource_id": "550e8400-e29b-41d4-a716-446655440000",
  "services": [
    {
      "service_id": "svc-uuid-1",
      "service_name": "Digitize",
      "service_type": "digitize",
      "application_id": "app-uuid-1",
      "application_name": "My RAG App",
      "sync_status": "up to date",
      "last_sync_at": "2026-06-01T11:00:00Z"
    },
    {
      "service_id": "svc-uuid-2",
      "service_name": "Digitize",
      "service_type": "digitize",
      "application_id": "app-uuid-2",
      "application_name": "My Summarise App",
      "sync_status": "out of sync",
      "last_sync_at": "2026-06-01T09:00:00Z"
    }
  ]
}
```

| Field                         | Source                                       | Description                                                                                                  |
| ----------------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `datasource_id`               | path param                                   | The datasource component UUID                                                                                |
| `services[].service_id`       | catalog (`services.id`)                      | ID of the connected service                                                                                  |
| `services[].service_name`     | catalog (`services.name`)                    | Display name of the service                                                                                  |
| `services[].service_type`     | catalog (`services.type`)                    | Catalog type of the service, e.g. `"digitize"`                                                               |
| `services[].application_id`   | catalog (`applications.id`)                  | ID of the application the service belongs to                                                                 |
| `services[].application_name` | catalog (`applications.name`)                | Display name of the application                                                                              |
| `services[].sync_status`      | Digitize (`GET /v1/connectors/:connectorid`) | Current sync state for this service: `"up to date"`, `"out of sync"`, `"started"`, `"completed"`, `"failed"` |
| `services[].last_sync_at`     | Digitize (`GET /v1/connectors/:connectorid`) | Timestamp of the last completed sync for this service, or `null`                                             |

**Response `404 Not Found`:**

```json
{ "error": "datasource not found" }
```

---

### 6.10 Connect Datasource to Application

**`PUT /api/v1/applications/:id/connectors/datasources/:datasource_id`**

Creates a link in `service_dependencies` and calls `POST /v1/connectors` on each eligible service.

**Rules:**

- The application must exist and be in `Running` status.
- The datasource must exist and be in `Connected` status.
- The application must have been deployed as an architecture (`deployment_type = "architectures"`), and at least one of its services must declare `accepts_connectors: [datasource]` for that architecture in `architecture_config`. Applications deployed as standalone services never satisfy this condition.
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

**Response `422 Unprocessable Entity` (standalone deployment):**

```json
{
  "error": "no services in this application accept datasource connectors"
}
```

**Response `422 Unprocessable Entity` (not Running):**

```json
{
  "error": "application must be in Running status before connecting a datasource"
}
```

### 6.10 Disconnect Datasource from Application

**`DELETE /api/v1/applications/:id/connectors/datasources/:datasource_id`**

Removes the link from `application_datasources` and calls `DELETE /v1/connectors/:connectorid` on the Digitize service.

**Response `204 No Content`:** Disconnected.

**Response `404 Not Found`:**

```json
{ "error": "datasource is not connected to this application" }
```

### 6.11 Get Datasource Status for Application

**`GET /api/v1/applications/:id/connectors/datasources/:datasource_id`**

The catalog backend resolves the `connector_id` from `service_dependencies`, fetches the datasource record from `components`, and calls `GET /v1/connectors/:connectorid` on each linked Digitize service pod. The response merges the Digitize sync fields at the top level with catalog identity fields nested under a `datasource` object. `provider_name` is resolved from the provider registry using `components.provider`.

**Response `200 OK`:**

```json
{
  "connector_id": "550e8400-e29b-41d4-a716-446655440000",
  "sync_status": "up to date",
  "total_files": 150,
  "new_files": 2,
  "removed_files": 0,
  "failed_files": 8,
  "last_sync_at": "2026-06-01T11:00:00Z",
  "last_sync_error": null,
  "datasource": {
    "name": "My S3 Bucket",
    "type": "datasource",
    "provider": "s3",
    "provider_name": "Amazon S3",
    "status": "Connected"
  }
}
```

| Field                      | Source                                 | Description                                                                                 |
| -------------------------- | -------------------------------------- | ------------------------------------------------------------------------------------------- |
| `connector_id`             | Digitize (echoed from `components.id`) | The component UUID used as the Digitize connector ID                                        |
| `sync_status`              | Digitize                               | Current sync state: `"up to date"`, `"out of sync"`, `"started"`, `"completed"`, `"failed"` |
| `total_files`              | Digitize                               | Total files known to the connector                                                          |
| `new_files`                | Digitize                               | Files added since the last tick                                                             |
| `removed_files`            | Digitize                               | Files removed since the last tick                                                           |
| `failed_files`             | Digitize                               | Files that failed to process in the last tick                                               |
| `last_sync_at`             | Digitize                               | Timestamp of the last completed sync                                                        |
| `last_sync_error`          | Digitize                               | Error string from the last failed sync, or `null`                                           |
| `datasource.name`          | catalog (`components.instance_name`)   | User-supplied display name of the datasource                                                |
| `datasource.type`          | catalog (`components.type`)            | Always `"datasource"`                                                                       |
| `datasource.provider`      | catalog (`components.provider`)        | Provider identifier, e.g. `"s3"`, `"ssh"`                                                   |
| `datasource.provider_name` | provider registry (`DisplayName()`)    | Human-readable provider name, e.g. `"Amazon S3"`, `"Remote SSH"`                            |
| `datasource.status`        | catalog (`components.status`)          | Catalog-side connectivity health: `Connected` or `Offline`                                  |

**Response `404 Not Found`:** Returned if the datasource is not connected to this application (no row in `service_dependencies`).

---

## 7. Digitize Service Integration

### 7.1 Connector API Overview

The Digitize service exposes a `/v1/connectors` API that manages the active linkage between a deployed application and a datasource. The catalog service acts as the client.

| Operation                         | Catalog Trigger                                                  | Digitize API Call                                                                                         |
| --------------------------------- | ---------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Connect datasource to application | `PUT /applications/:id/connectors/datasources/:datasource_id`    | `POST /v1/connectors` on each eligible service (using the datasource's `components.id` as `connector_id`) |
| Update datasource credentials     | `PUT /connectors/datasources/:id`                                | `PUT /v1/connectors/<component_id>` on each service linked in `service_dependencies`                      |
| Disconnect datasource             | `DELETE /applications/:id/connectors/datasources/:datasource_id` | `DELETE /v1/connectors/<component_id>` on each linked service; remove `service_dependencies` row          |
| Fetch sync status                 | `GET /applications/:id/connectors/datasources/:datasource_id`    | `GET /v1/connectors/<component_id>` on each linked service                                                |

### 7.2 Connect Flow

When `PUT /api/v1/applications/:id/connectors/datasources/:datasource_id` is called:

1. Validate application is `Running`.
2. Validate datasource is `Connected`.
3. Verify the application was deployed as an architecture (`deployment_type = "architectures"`). Look up all services in the application whose catalog metadata declares `accepts_connectors: [datasource]` and which do not already have this datasource in `service_dependencies`. If `deployment_type = "services"` (standalone), reject immediately with `422 Unprocessable Entity`.
4. For each eligible service, call `POST /v1/connectors` on its downstream pod, passing the datasource's `components.id` as `connector_id`.
5. On success, insert a `service_dependencies` row: `(service_id, datasource_id, dependency_type='component')`.

**`POST /v1/connectors` payload — S3 example:**

The payload shape is defined by the digitize proposal. The catalog service is responsible for constructing it exactly as follows before calling the endpoint.

```json
{
  "connector_id": "<components.id of the datasource>",
  "connector_name": "<components.instance_name of the datasource>",
  "type": "s3",
  "allowed_extensions": [".pdf", ".docx"],
  "connection_details": {
    "endpoint_url": "https://s3.us-east-1.amazonaws.com",
    "bucket_name": "my-docs",
    "prefix": "reports/2026/",
    "delimiter": "/",
    "access_key_id": "AKIAIOSFODNN7EXAMPLE",
    "secret_access_key": "<secret access key>"
  }
}
```

**`POST /v1/connectors` payload — SSH example:**

```json
{
  "connector_id": "<components.id of the datasource>",
  "connector_name": "<components.instance_name of the datasource>",
  "type": "ssh",
  "allowed_extensions": [".pdf", ".docx"],
  "connection_details": {
    "host": "ssh.example.com",
    "username": "sync_user",
    "remote_path": "/exports/reports",
    "private_key": "<private key>"
  }
}
```

The digitize service responds with `202 Accepted` (no response body). Nothing new needs to be stored — the `connector_id` sent was already the datasource's `components.id`.

### 7.3 Update Flow

When `PUT /api/v1/connectors/datasources/:id` is called:

1. Extract only the updatable credential fields for the provider (`access_key_id`, `secret_access_key` for S3; `username`, `private_key` for SSH). Ignore any other fields in the request body.
2. Validate connectivity using the new credentials merged with the existing immutable metadata (bucket, host, path, etc.).
3. If connectivity fails, return `422 Unprocessable Entity` — do not persist any changes.
4. Update only the updatable fields in the `components` record, leaving all other metadata intact.
5. Query `service_dependencies` joined with `services` to find all services linked to this datasource (filtering by `type='datasource'` and `source='remote'` on the `components` side).
6. For each link, call `PUT /v1/connectors/:connectorid` on the Digitize service with the updated credential payload.
   - If the call fails, retry **once**.
   - If the retry also fails: record the error for that link and continue to the next — do **not** roll back the datasource record update.
7. The digitize service performs a partial update — only the credential fields sent in the payload are overwritten.
8. Return `200 OK`. If any propagations failed, include a `propagation_errors` array in the response so the UI can show an inline error against each affected application. The user retriggers by re-submitting the datasource update from the UI.

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

When `DELETE /api/v1/applications/:id/connectors/datasources/:datasource_id` is called:

1. Query `service_dependencies` to find all services in this application linked to the datasource. Return `404` if none found.
2. For each service, call `DELETE /v1/connectors/<component_id>` on its downstream pod, where `component_id` is the datasource's `components.id`.
   - If the call returns `404` (connector already gone on the downstream side), treat as success and continue to the next service.
   - If the call returns `409 Conflict` (a sync tick is currently in progress on the Digitize side), record the error for that service and continue to the next.
   - If the call fails with any other error: record the error for that service and continue to the next — do **not** skip the remaining services.
3. For every service whose downstream call succeeded (or returned `404`), delete the corresponding `service_dependencies` row.
4. Return `200 OK`. If any downstream calls failed, include a `propagation_errors` array so the UI can show an inline error against each affected service. The user retriggers by re-submitting the disconnect from the UI.

**Response when all disconnections succeed:**

```json
{
  "application_id": "app-uuid-1",
  "datasource_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

**Response when one or more disconnections fail:**

```json
{
  "application_id": "app-uuid-1",
  "datasource_id": "550e8400-e29b-41d4-a716-446655440000",
  "propagation_errors": [
    {
      "service_id": "svc-uuid-1",
      "service_name": "Digitize",
      "error": "failed to reach digitize service: connection refused"
    }
  ]
}
```

### 7.5 Connected Services Fetch Flow

When `GET /api/v1/connectors/datasources/:id/services` is called:

1. Return `404` if no record exists in `components` for the given `id`.
2. Query `service_dependencies` joined with `services` and `applications` to find all services linked to this datasource, collecting `service_id`, `service_name`, `service_type`, `application_id`, `application_name`.
3. For each linked service, call `GET /v1/connectors/<component_id>` on its downstream Digitize pod, where `component_id` is the datasource's `components.id`. Extract `sync_status` and `last_sync_at` from the response.
4. If the Digitize call fails for a service, set `sync_status: null` and `last_sync_at: null` for that entry and continue — do not fail the entire request.
5. Return the assembled list.

### 7.6 Application-Scoped Status Fetch Flow

When `GET /api/v1/applications/:id/connectors/datasources/:datasource_id` is called:

1. Query `service_dependencies` to find all services in this application linked to the datasource. Return `404` if none found.
2. Fetch the datasource record from the `components` table using `datasource_id`. Look up `provider_name` from the provider registry via `DatasourceProvider.DisplayName()`.
3. For each service, call `GET /v1/connectors/<component_id>` on its downstream pod, where `component_id` is the datasource's `components.id`.
4. Merge the Digitize sync fields at the top level and nest the catalog identity fields (`name`, `type`, `provider`, `provider_name`, `status`) under a `datasource` key.

---

## 8. Datasource Type Providers

### 8.1 Provider Interface

Each datasource type implements a `DatasourceProvider` interface responsible for:

- Declaring the required and optional metadata fields.
- Identifying which metadata fields contain sensitive data.
- Providing display metadata used by `GET /api/v1/components?type=datasource`.
- Performing a live connectivity test using the provided credentials.

```go
// DatasourceProvider defines the contract for a datasource type.
type DatasourceProvider interface {
    // ProviderID returns the unique identifier for this provider type (e.g. "s3", "ssh").
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

Providers are registered in a provider registry at startup. The `POST /connectors/datasources` and `PUT /connectors/datasources/:id` handlers look up the provider by `provider` field value to run field validation and the connectivity test. The existing `GET /api/v1/components?type=datasource` and `GET /api/v1/components/datasource/providers/:provider_id/params` endpoints serve discovery and schema — no new endpoints and no database access required.

### 8.2 S3 Provider

**Provider ID:** `s3`

Field names below match the `connection_details` keys used in the digitize push payload.

| Field                | Required | Sensitive | Updatable | Notes                                                                                                   |
| -------------------- | -------- | --------- | --------- | ------------------------------------------------------------------------------------------------------- |
| `endpoint_url`       | Yes      | No        | No        | Full S3 endpoint URL, e.g. `https://s3.us-east-1.amazonaws.com`. Region is auto-detected from this URL. |
| `bucket_name`        | Yes      | No        | No        |                                                                                                         |
| `access_key_id`      | Yes      | No        | **Yes**   | IAM key ID (AWS) or HMAC key ID (IBM COS)                                                               |
| `secret_access_key`  | Yes      | **Yes**   | **Yes**   | IAM secret (AWS) or HMAC secret (IBM COS)                                                               |
| `prefix`             | No       | No        | No        | Key prefix to scope the sync to a folder; empty means bucket root                                       |
| `delimiter`          | No       | No        | No        | Set `"/"` for non-recursive listing (immediate children only)                                           |
| `allowed_extensions` | No       | No        | No        | e.g. `[".pdf", ".docx"]`; if omitted, all files are synced                                              |

Connectivity test: List objects in the bucket (scoped to `prefix` if provided) with a zero-item limit (`list_objects_v2` with `MaxKeys=0`). A successful call confirms valid credentials and bucket/prefix access.

### 8.3 Remote SSH Provider

**Provider ID:** `ssh`

Field names below match the `connection_details` keys used in the digitize push payload.

| Field                | Required | Sensitive | Updatable | Notes                                                      |
| -------------------- | -------- | --------- | --------- | ---------------------------------------------------------- |
| `host`               | Yes      | No        | No        |                                                            |
| `username`           | Yes      | No        | **Yes**   |                                                            |
| `private_key`        | Yes      | **Yes**   | **Yes**   | Ed25519 private key                                        |
| `remote_path`        | Yes      | No        | No        | Absolute path on the remote server to sync from            |
| `allowed_extensions` | No       | No        | No        | e.g. `[".pdf", ".docx"]`; if omitted, all files are synced |

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

---

## 10. Sync Service

The **catalog-side sync service** is a common, connector-agnostic heartbeat that validates the health of any remotely-managed component registered in the catalog. It is designed to serve datasource connectors today and to be extended for future connector types (e.g., vector stores, models) without structural change.

### 10.1 Sync Job Design

A single periodic background job (`ConnectorSyncJob`) runs in the catalog backend process on a configurable schedule (default: every 15 minutes). It queries the `components` table for all records where `source = "remote"`, regardless of `type`. For each such component:

1. Looks up the registered provider for the component's `type` and `provider` fields.
2. Calls `provider.TestConnection(ctx, metadata)`.
3. If the test passes: sets `status = "Connected"`, clears `message`.
4. If the test fails: sets `status = "Offline"`, stores the specific error string in `message` (e.g., credential failure, permission denied, network unreachable).
5. Writes the updated status back to the `components` table via `UpdateStatus()`.

New connector types (vector stores, models, etc.) plug in by implementing the same `ConnectorProvider` interface (see Section 8.1) and registering with the provider registry. No changes to the sync job itself are required.

### 10.2 Status Updates

The sync job uses the existing `component_repo.UpdateStatus(ctx, id, status, message)` method from [`ai-services/internal/pkg/catalog/db/repository/component_repo.go`](../../ai-services/internal/pkg/catalog/db/repository/component_repo.go).

There are two distinct status spaces:

**Connectors page** (`components.status`, driven by `ConnectorSyncJob`):

| Status         | Meaning                                                                                                                                                    |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Connected`    | Sync job successfully reached the datasource — credentials valid, files/folders accessible                                                                 |
| `Offline`      | Sync job could not reach the datasource — invalid credentials, permission error on folder/files, network issue, etc. `message` contains the specific error |

**Application-datasources page** (`application_datasources.status`, driven by Digitize service):

The catalog backend fetches status from `GET /v1/connectors/:connectorid` (see §7.5) and cascades all fields — `status`, `message`, `files_processed`, `last_synced_at`, etc. — to the UI verbatim. No mapping or transformation is applied.

---

## 11. Application Lifecycle Integration

### 11.1 Datasource Connection During Create Flow

#### How the UI Obtains Datasource IDs

The UI populates the connector selection step by calling `GET /api/v1/connectors/datasources` — the standard paginated list endpoint (Section 6.1). It filters to `status=Connected` so only healthy datasources are shown. The user picks one or more from that list. Their UUIDs are then included in the create application request.

#### Request Body Change

Datasource connectors are folded into the existing `components` array on the [`Service`](../../ai-services/internal/pkg/catalog/apiserver/models/create_application.go) struct — no separate top-level field is needed. A datasource entry uses `component_type: "datasource"` and supplies the UUID of the pre-registered datasource via a new optional `id` field on `Component`. Freshly-provisioned components (LLM, vector DB, etc.) leave `id` empty as today.

```go
// Service represents a service configuration in the application.
type Service struct {
    CatalogID  string         `json:"catalog_id"  binding:"required"`
    Version    string         `json:"version"     binding:"required"`
    Components []Component    `json:"components"  binding:"required,dive"`
    Params     map[string]any `json:"params"`
}

// Component represents a component configuration for a service.
// For remotely-managed components (e.g. datasources), ID references
// the pre-registered record; ProviderID and Version are still required
// for the deploy-options schema lookup but Params may be empty.
type Component struct {
    ID            string         `json:"id"`             // UUID of a pre-registered component (datasources only)
    ComponentType string         `json:"component_type"  binding:"required"`
    ProviderID    string         `json:"provider_id"     binding:"required"`
    Version       string         `json:"version"         binding:"required"`
    Params        map[string]any `json:"params"`
}
```

This keeps a single, uniform shape for all component entries. The handler distinguishes datasource components from install-time components by `component_type == "datasource"` — the same discriminator used in the database.

**Which component types a service accepts is declared in its catalog YAML** via an `accepts_connectors` list. The `deploy-options` API surfaces this as a `datasource` component entry (see below) so the UI knows to show a connector picker rather than a provider configuration form.

**Example request body:**

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
          "version": "1.0.0",
          "params": { "model_id": "ibm/granite-13b-chat-v2" }
        },
        {
          "id": "550e8400-e29b-41d4-a716-446655440000",
          "component_type": "datasource",
          "provider_id": "s3",
          "version": "1"
        },
        {
          "id": "661f9511-f30c-52e5-b827-557766551111",
          "component_type": "datasource",
          "provider_id": "ssh",
          "version": "1"
        }
      ]
    },
    {
      "catalog_id": "summarize",
      "version": "2.0.0",
      "components": [
        {
          "id": "550e8400-e29b-41d4-a716-446655440000",
          "component_type": "datasource",
          "provider_id": "s3",
          "version": "1"
        }
      ]
    }
  ]
}
```

- Datasource entries are distinguished by `component_type: "datasource"`. The `id` field is required for these entries and must reference a pre-registered datasource in `Connected` status.
- Non-datasource entries (LLM, vector DB, etc.) leave `id` empty, exactly as today.
- Any `id` that fails validation (not found, wrong type, not `Connected`) is returned as `400 Bad Request` before deployment begins.
- The same datasource `id` can appear under multiple services — each produces an independent `service_dependencies` row.

#### Service Catalog YAML Change

Each service that accepts connectors declares this as a flat list in its catalog YAML:

```yaml
id: digitize
name: "Digitize documents"
type: service
architectures:
  - rag
standalone: true
dependencies:
  - id: vector_store
  - id: embedding
  - id: llm

# Connector types this service can accept when connected via the catalog.
accepts_connectors:
  - datasource
```

Services that do not accept connectors omit the field entirely (treated as an empty list).

The corresponding Go type addition to [`types.Service`](../../ai-services/internal/pkg/catalog/types/types.go):

```go
// In Service struct, add:
AcceptsConnectors []string `yaml:"accepts_connectors,omitempty" json:"accepts_connectors,omitempty"`
```

#### Architecture Catalog YAML Change

The architecture metadata mirrors which connector types it supports. This is used by the connect API to verify the application was deployed as part of an architecture that permits connectors, without needing to inspect individual service metadata at validation time:

```yaml
# assets/architectures/rag/metadata.yaml
id: rag
name: "Digital Assistant"
# ...existing fields...

# Connector types supported by this architecture.
# Services listed under this architecture that declare the same connector type
# in their own accepts_connectors field will be eligible for connection.
accepts_connectors:
  - datasource
```

Architectures that do not support any connectors omit the field (treated as an empty list). The corresponding Go type addition to [`types.Architecture`](../../ai-services/internal/pkg/catalog/types/types.go):

```go
// In Architecture struct, add:
AcceptsConnectors []string `yaml:"accepts_connectors,omitempty" json:"accepts_connectors,omitempty"`
```

#### Deploy-Options API Response Change

**Endpoint:** `GET /api/v1/architectures/{id}/deploy-options`

The `components` array within each service entry is extended: for every type listed under `accepts_connectors` in the service's catalog YAML, the backend appends a component object of that type to the service's `components` list. The UI uses the presence of a `datasource` component entry to decide whether to render a connector picker for that service.

The standalone deploy-options path (`GET /api/v1/services/{id}/deploy-options`) calls [`GetServiceDeployOptions`](../../ai-services/internal/pkg/catalog/deploy_options.go:147), which does **not** append connector entries — it only builds components from [`service.Dependencies`](../../ai-services/internal/pkg/catalog/types/types.go:113). The connector picker therefore never appears for standalone service deployments.

**How the build works in [`buildSingleService`](../../ai-services/internal/pkg/catalog/deploy_options.go:74):**

```go
// After building dependency-based components, append connector placeholders:
for _, connectorType := range service.AcceptsConnectors {
    components = append(components, buildConnectorPlaceholderComponent(connectorType))
}
```

[`GetServiceDeployOptions`](../../ai-services/internal/pkg/catalog/deploy_options.go:147) does not call `buildSingleService` — it builds components inline from `service.Dependencies` only — so connector entries are never appended on the standalone path.

**Full response example (after this change):**

```json
{
  "id": "rag",
  "name": "Digital Assistant",
  "version": "1.0.0",
  "global_components": [
    {
      "type": "vector_db",
      "name": "Vector store",
      "providers": [
        {
          "id": "opensearch",
          "name": "OpenSearch",
          "description": "Distributed search and analytics engine",
          "default": true,
          "schema": "/api/v1/components/vector_db/providers/opensearch/params"
        }
      ]
    }
  ],
  "services": [
    {
      "id": "digitize",
      "name": "Digitize documents",
      "version": "1.2.0",
      "schema": "/api/v1/services/digitize/params",
      "components": [
        {
          "type": "llm",
          "name": "LLM Model",
          "providers": [
            {
              "id": "watsonx",
              "name": "IBM watsonx.ai Instruct",
              "description": "Configure watsonx.ai for instruct models",
              "schema": "/api/v1/components/llm/providers/watsonx/params"
            },
            {
              "id": "vllm",
              "name": "vLLM Instruct",
              "description": "Deploy new instruct model on vLLM",
              "schema": "/api/v1/components/llm/providers/vllm/params"
            }
          ]
        },
        {
          "type": "datasource",
          "name": "Datasource",
          "providers": [
            {
              "id": "s3",
              "name": "Amazon S3",
              "description": "Amazon S3 bucket via AWS credentials",
              "schema": "/api/v1/components/datasource/providers/s3/params"
            },
            {
              "id": "ssh",
              "name": "Remote SSH",
              "description": "Remote server accessible via SSH private key",
              "schema": "/api/v1/components/datasource/providers/ssh/params"
            }
          ]
        }
      ]
    },
    {
      "id": "chat",
      "name": "Chat",
      "version": "2.0.0",
      "schema": "/api/v1/services/chat/params",
      "components": [
        {
          "type": "llm",
          "name": "LLM Model",
          "providers": [
            {
              "id": "watsonx",
              "name": "IBM watsonx.ai Instruct",
              "description": "Configure watsonx.ai for instruct models",
              "schema": "/api/v1/components/llm/providers/watsonx/params"
            }
          ]
        }
      ]
    }
  ]
}
```

The `datasource` component entry in `digitize.components` signals the UI to render a connector picker (populated from `GET /api/v1/connectors/datasources?status=Connected`) rather than a provider configuration form. The `chat` service has no `datasource` entry, so no picker is shown for it.

**Updated `DeployOptionsService` response schema:**

| Field        | Type   | Description                                                                                                                                                                                                                       |
| ------------ | ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`         | string | Service identifier                                                                                                                                                                                                                |
| `name`       | string | Service display name                                                                                                                                                                                                              |
| `version`    | string | Service version                                                                                                                                                                                                                   |
| `schema`     | string | URL to fetch service-level parameters                                                                                                                                                                                             |
| `components` | array  | Array of component objects with providers. Includes a `datasource` entry for services that declare `accepts_connectors: [datasource]` in their catalog YAML. Only present in architecture deploy-options responses. **Extended.** |
| `resources`  | object | Optional resource requirements for this service                                                                                                                                                                                   |

> **Backward compatibility:** The `datasource` component entry is purely additive. Existing clients that do not read it are unaffected. The standalone `GET /api/v1/services/{id}/deploy-options` path is completely unchanged — no `datasource` entry ever appears there.

#### Post-Deploy Connect Flow

1. The deployment proceeds normally. Connector attachment is **not** attempted until the application reaches `Running` status.
2. Once the application transitions to `Running`, the deployment completion callback scans all services' `components` arrays for entries where `component_type == "datasource"`, collects the unique datasource `id` values, and calls `PUT /api/v1/applications/:id/connectors/datasources/:datasource_id` once per unique ID. The fan-out to individual services and the `service_dependencies` insertions happen inside that handler.
3. If any downstream service call fails after one retry, the application remains `Running` (deployment was successful). The `service_dependencies` row is still inserted so the link is tracked, but the UI surfaces the failure against the specific service so the user can retrigger via a datasource update.

> **Note:** Step 2 only runs for applications where `deployment_type = "architectures"`. The callback for standalone deployments skips datasource attachment entirely.

### 11.2 Datasource Connection Post-Creation

Users can connect or disconnect datasources from any `Running` application at any time via the Catalog UI, using the `PUT` and `DELETE` endpoints described in Section 6. No re-deployment is required.

> **Restriction:** The connect endpoint (`PUT /applications/:id/connectors/datasources/:datasource_id`) enforces the architecture deployment guard server-side. When called for an application with `deployment_type = "services"` (standalone), the handler returns `422 Unprocessable Entity`:
>
> ```json
> { "error": "no services in this application accept datasource connectors" }
> ```
>
> This guard is independent of the UI — it prevents direct API calls from bypassing the restriction.

---

## 12. Error Handling

The feature follows the existing error response convention established in the application handler:

```json
{ "error": "<human-readable description>" }
```

| Scenario                                                                                           | HTTP Status                                                                                                      |
| -------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Datasource not found                                                                               | `404 Not Found`                                                                                                  |
| Application not found                                                                              | `404 Not Found`                                                                                                  |
| Invalid request body or missing required fields                                                    | `400 Bad Request`                                                                                                |
| Connectivity test failed on create/update (triggered internally by create and update handlers)     | `422 Unprocessable Entity`                                                                                       |
| Datasource already connected to application                                                        | `409 Conflict`                                                                                                   |
| Delete attempted while datasource has active connections                                           | `409 Conflict`                                                                                                   |
| Application not in `Running` status during connect                                                 | `422 Unprocessable Entity`                                                                                       |
| Connect attempted on a standalone deployment (`deployment_type = "services"`)                      | `422 Unprocessable Entity`                                                                                       |
| Connect attempted and architecture does not include any service with matching `accepts_connectors` | `422 Unprocessable Entity`                                                                                       |
| Digitize `POST /v1/connectors` returns `409 Conflict`                                              | `409 Conflict` — connector already exists; catalog should use `PUT`                                              |
| Digitize `PUT /v1/connectors` failed after 1 retry                                                 | `200 OK` — datasource update succeeds; `propagation_errors` array in response lists affected applications        |
| Digitize `DELETE /v1/connectors` failed (non-404)                                                  | `200 OK` — succeeded services are disconnected; `propagation_errors` array lists failed services; user can retry |
| Digitize service API call failed (non-404, non-PUT)                                                | `502 Bad Gateway` (with `error` field describing the downstream failure)                                         |

---

## 13. Security Considerations

- **Credentials never returned in API responses.** Fields identified as sensitive by `DatasourceProvider.SensitiveFields()` are filtered out before any API response is serialised. For SSH connectors the **public key** is returned (it is not a secret); the private key ciphertext is never returned. For S3 the `access_key_id` is returned; the `secret_access_key` is never returned.
- **IAM least-privilege for S3.** Only `s3:GetObject` and `s3:ListBucket` on the target bucket should be granted to the IAM user whose keys are stored.
- **Connectivity test before storage.** A live connectivity test is run internally by the create and update handlers before any record is persisted.
- **Auth middleware on all routes.** All datasource and application-datasource endpoints are protected by the existing `AuthMiddleware` following the pattern in [`router.go`](../../ai-services/internal/pkg/catalog/apiserver/router.go).
