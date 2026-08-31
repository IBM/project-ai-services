# Catalog Datasource Connectors Feature Proposal

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Background and Motivation](#2-background-and-motivation)
   - 2.1 [Current State](#21-current-state)
   - 2.2 [Problem Statement](#22-problem-statement)
   - 2.3 [Goals](#23-goals)
3. [Architecture Overview](#3-architecture-overview)
   - 3.1 [Key Concepts](#31-key-concepts)
   - 3.2 [Datasource Types](#32-datasource-types)
   - 3.3 [Connectors Table](#33-connectors-table)
4. [Database Design](#4-database-design)
   - 4.1 [New Connectors Table](#41-new-connectors-table)
   - 4.2 [Reuse of `service_dependencies` for Datasource Links](#42-reuse-of-service_dependencies-for-datasource-links)
   - 4.3 [Connector Status Values](#43-connector-status-values)
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
   - 6.6 [Get Provider Input Schema](#66-get-provider-input-schema)
   - 6.7 [List Providers for a Connector Type](#67-list-providers-for-a-connector-type)
   - 6.8 [Get Connected Services for Datasource](#68-get-connected-services-for-datasource)
   - 6.10 [Connect Datasource to Application](#610-connect-datasource-to-application)
   - 6.11 [Disconnect Datasource from Application](#611-disconnect-datasource-from-application)
   - 6.12 [Get Datasource Status for Application](#612-get-datasource-status-for-application)
   - 6.13 [List All Datasources for an Application](#613-list-all-datasources-for-an-application)
7. [Digitize Service Integration](#7-digitize-service-integration)
   - 7.1 [Connector API Overview](#71-connector-api-overview)
   - 7.2 [Connect Flow](#72-connect-flow)
   - 7.3 [Update Flow](#73-update-flow)
   - 7.4 [Disconnect Flow](#74-disconnect-flow)
   - 7.5 [Connected Services Fetch Flow](#75-connected-services-fetch-flow)
   - 7.6 [Application-Scoped Status Fetch Flow](#76-application-scoped-status-fetch-flow)
8. [Datasource Type Providers](#8-datasource-type-providers)
   - 8.1 [Provider Interface](#81-provider-interface)
   - 8.2 [Object Storage Provider](#82-object-storage-provider)
   - 8.3 [File System Provider](#83-file-system-provider)
   - 8.4 [Connector Asset Directory](#84-connector-asset-directory)
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

The implementation uses the following provider IDs. The catalog provider IDs **must** match exactly so the payload the catalog sends to digitize is consistent.

| Type                | Catalog `provider` value | Digitize `type` value | Key Credentials                                                     |
| ------------------- | ------------------------ | --------------------- | ------------------------------------------------------------------- |
| Object Storage (S3) | `object_storage`         | `object_storage`      | `endpoint_url`, `bucket_name`, `access_key_id`, `secret_access_key` |
| File System (SSH)   | `file_system`            | `file_system`         | `host`, `username`, `private_key`, `remote_path`                    |

Additional provider types can be added by implementing the provider interface described in Section 8.

### 3.3 Connectors Table

Datasources are stored in a new dedicated `connectors` table. This keeps connector records fully decoupled from catalog-installed `components` (LLMs, vector stores, etc.), giving the table a schema that exactly matches connector requirements without widening the `components` table with fields that only apply to remote datasources.

---

## 4. Database Design

### 4.1 New Connectors Table

A new `connectors` table stores all datasource registrations. It is entirely separate from the `components` table, which continues to hold only catalog-installed infrastructure components (LLMs, vector stores, etc.).

```sql
-- Migration: create connectors table
CREATE TYPE connector_status AS ENUM ('connected', 'offline');

CREATE TABLE connectors (
    id         UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(255)     NOT NULL UNIQUE,
    type       VARCHAR(64)      NOT NULL,     -- e.g. 'datasource'
    provider   VARCHAR(64)      NOT NULL,     -- e.g. 'object_storage', 'file_system'
    status     connector_status NOT NULL DEFAULT 'offline',
    message    TEXT,
    metadata   JSONB            NOT NULL DEFAULT '{}',
    created_by VARCHAR(100)     NOT NULL,
    created_at TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_connectors_type     ON connectors (type);
CREATE INDEX idx_connectors_provider ON connectors (provider);
CREATE INDEX idx_connectors_status   ON connectors (status);
```

A datasource record in the `connectors` table looks like:

| Column       | Example Value                                                                              |
| ------------ | ------------------------------------------------------------------------------------------ |
| `id`         | `uuid`                                                                                     |
| `name`       | `"My S3 Bucket"` (unique, case-insensitive)                                                |
| `type`       | `"datasource"`                                                                             |
| `provider`   | `"object_storage"` / `"file_system"`                                                       |
| `status`     | `"connected"` / `"offline"` (lowercase enum values)                                        |
| `message`    | `null` / `"Permission denied on bucket"`                                                   |
| `metadata`   | `{"bucket_name": "my-docs", "access_key_id": "AK...", "secret_access_key": "<encrypted>"}` |
| `created_by` | `"admin@example.com"`                                                                      |
| `created_at` | timestamp                                                                                  |
| `updated_at` | timestamp                                                                                  |

The Go `Connector` DB model struct does **not** include a `ConnectedServices` field. Connected-service counts are fetched from `service_dependencies` via a separate `svcDepRepo.GetServiceCountByDependency` call in the `DatasourceService` list path and injected into the API response DTO. The model itself never carries a count.

### 4.2 Reuse of `service_dependencies` for Datasource Links

No new join table is required. Datasource links are recorded in the existing `service_dependencies` table using a new `dependency_type = 'connector'` value. This distinguishes them unambiguously from install-time component links (`dependency_type = 'component'`) without any additional join.

Because a single datasource can be connected to multiple services, the `connector_id` passed to Digitize must be **unique per `(service_id, datasource_id)` pair**. The `dependency_id` of the `service_dependencies` row is the datasource UUID — and since the table enforces `UNIQUE (service_id, dependency_id)`, each `(service_id, datasource_id)` pair is guaranteed to be unique. The `dependency_id` is therefore used directly as the Digitize `connector_id` for that specific service-datasource link.

```sql
-- Migration: add 'connector' to the dependency_type enum
ALTER TYPE dependency_type ADD VALUE IF NOT EXISTS 'connector';
```

**Query: find all datasource links for a service (with their Digitize connector IDs)**

```sql
SELECT sd.dependency_id AS digitize_connector_id,  -- used as connector_id in Digitize API calls
       sd.service_id
FROM service_dependencies sd
WHERE sd.service_id      = $1
  AND sd.dependency_type = 'connector';
```

**Query: find all datasource links for an application (across all its services)**

```sql
SELECT sd.dependency_id AS digitize_connector_id,
       sd.service_id,
       s.app_id
FROM service_dependencies sd
JOIN services s ON s.id = sd.service_id
WHERE s.app_id           = $1
  AND sd.dependency_type = 'connector';
```

**Cascade behaviour** is already correct: `ON DELETE CASCADE` on `service_id` means deleting a service automatically removes its datasource links.

### 4.3 Connector Status Values

The `connectors.status` column is a dedicated `connector_status` enum with **lowercase** values.

| Value       | Meaning                                                                                                                                                 |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `connected` | The connectivity test successfully reached the datasource — credentials valid, files/folders accessible                                                 |
| `offline`   | The connectivity test failed to reach the datasource — invalid credentials, permission error, network issue, etc. `message` contains the specific error |

### 4.4 Migration Plan

Two new goose migration files, numbered after the current highest (`20260430094506`).

| File                                               | Purpose                                                                 |
| -------------------------------------------------- | ----------------------------------------------------------------------- |
| `20260430094507_create_connectors_table.sql`       | Creates the `connector_status` enum and `connectors` table with indexes |
| `20260430094508_add_connector_dependency_type.sql` | Adds `'connector'` value to the `dependency_type` enum                  |

---

## 5. API Specification

### 5.1 Datasource CRUD APIs

All routes are under `/api/v1` and protected by the existing `AuthMiddleware`.

| Method   | Path                                                        | Description                                                                                    |
| -------- | ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `GET`    | `/datasources`                                              | List all datasources (paginated, filterable by status)                                         |
| `GET`    | `/datasources/:id`                                          | Get a single datasource by ID (includes connected services inline)                             |
| `POST`   | `/datasources`                                              | Create a new datasource (validates connectivity first)                                         |
| `PUT`    | `/datasources/:id`                                          | Update datasource credentials                                                                  |
| `DELETE` | `/datasources/:id`                                          | Delete a datasource (only if not connected to any application)                                 |
| `GET`    | `/connectors/:connector_type/providers/:provider_id/params` | Get the input schema for a provider (use `connector_type=datasource`)                          |
| `GET`    | `/connectors[?type=datasource]`                             | List all supported datasource provider types (use `type=datasource` to filter for datasources) |

### 5.2 Application-Datasource Connection APIs

| Method   | Path                                           | Description                                                         |
| -------- | ---------------------------------------------- | ------------------------------------------------------------------- |
| `PUT`    | `/applications/:id/datasources/:datasource_id` | Connect a datasource to an application                              |
| `DELETE` | `/applications/:id/datasources/:datasource_id` | Disconnect a datasource from an application                         |
| `GET`    | `/applications/:id/datasources/:datasource_id` | Get datasource status/details for a specific application connection |
| `GET`    | `/applications/:id/datasources`                | Get all the datasources for a specific application connection       |

---

## 6. API Endpoint Details

### 6.1 List Datasources

**`GET /api/v1/datasources`**

Query parameters:

| Parameter   | Type   | Required | Description                                         |
| ----------- | ------ | -------- | --------------------------------------------------- |
| `page`      | int    | No       | Page number (default: 1)                            |
| `page_size` | int    | No       | Items per page (default: 20, max: 100)              |
| `status`    | string | No       | Filter by status: `connected`, `offline`            |
| `provider`  | string | No       | Filter by provider: `object_storage`, `file_system` |

**Response `200 OK`:**

```json
{
  "data": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "My S3 Bucket",
      "type": "datasource",
      "provider": { "id": "object_storage", "name": "Object storage" },
      "status": "connected",
      "message": "",
      "connected_services": 2,
      "created_at": "2026-06-01T10:00:00Z",
      "updated_at": "2026-06-01T10:05:00Z"
    }
  ],
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total_items": 1,
    "total_pages": 1,
    "has_next": false,
    "has_prev": false
  }
}
```

> **Note:** `secret_access_key` and `private_key` fields are never returned in API responses. Metadata is **not** included in list items — only the fields shown above.
>
> The `provider` field is a JSON object with `id` (the catalog provider identifier, e.g. `"object_storage"`) and `name` (display name resolved from `metadata.yaml`, e.g. `"Object storage"`). The `connected_services` count is fetched via a separate `GetServiceCountByDependency` query — not a `LEFT JOIN` in the list query itself.

### 6.2 Get Datasource

**`GET /api/v1/datasources/:id`**

**Response `200 OK`:** Single datasource object with non-sensitive `metadata` fields, the `provider` object `{"id": "...", "name": "..."}`, and a `services` array listing all connected services enriched with live Digitize sync state (see §6.8 for the shape of each entry). The `connected_services` count field is **not** included on the single-get response — use the list endpoint for counts.

**Response `404 Not Found`:**

```json
{ "error": "datasource not found" }
```

### 6.3 Create Datasource

**`POST /api/v1/datasources`**

The request body varies by `provider`. The catalog backend validates the connection using the provided credentials before persisting.

**Request body (object_storage example):**

```json
{
  "name": "My S3 Bucket",
  "provider_id": "object_storage",
  "params": {
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

**Request body (file_system example):**

```json
{
  "name": "Production SSH",
  "provider_id": "file_system",
  "params": {
    "host": "192.168.1.100",
    "username": "datauser",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
    "remote_path": "/data/documents",
    "allowed_extensions": [".pdf", ".docx"]
  }
}
```

> **Request field names:** The create request body uses `provider_id` and `params`. These map to `CreateDatasourceRequest.ProviderID` and `CreateDatasourceRequest.Params` in the Go model.

**Validation rules:**

- `name` must be 3–100 characters and unique (case-insensitive). Stored in `connectors.name`.
- `provider_id` must be a registered provider identifier (`object_storage` or `file_system`).
- All required fields for the given provider must be present (validated via the provider interface).
- A live connectivity check must succeed before the datasource is persisted. If the connection test fails, return `422 Unprocessable Entity` with a descriptive error message.
- `allowed_extensions` is a **required** field for both providers (must include at least one extension, e.g. `[".pdf", ".docx"]`).
- Sensitive fields (`secret_access_key` for object_storage; `private_key` for file_system) are stored encrypted as described in Section 9.

**Response `201 Created`:**

```json
{ "id": "550e8400-e29b-41d4-a716-446655440000" }
```

**Response `422 Unprocessable Entity`:**

```json
{
  "error": "connection test failed: dial tcp 192.168.1.100:22: connection refused"
}
```

### 6.4 Update Datasource

**`PUT /api/v1/datasources/:id`**

Only the credential fields for the datasource's provider may be updated. Structural fields (bucket, host, path, etc.) are immutable after creation. Any non-updatable field present in the request body is ignored.

| Provider         | Updatable fields                     |
| ---------------- | ------------------------------------ |
| `object_storage` | `access_key_id`, `secret_access_key` |
| `file_system`    | `username`, `private_key`            |

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

**`DELETE /api/v1/datasources/:id`**

**Rules:**

- The datasource must not be connected to any application at the time of deletion. If it is, return `409 Conflict`.

**Response `204 No Content`:** Datasource deleted.

**Response `409 Conflict`:**

```json
{ "error": "datasource is connected to 2 application(s) and cannot be deleted" }
```

### 6.6 Get Provider Input Schema

**`GET /api/v1/connectors/:connector_type/providers/:provider_id/params`**

Returns the JSON Schema for the connector creation/edit form, read directly from `assets/connectors/<connector_type>/<provider_id>/schema.json` (see Section 8.4). Sensitive fields carry `"format": "password"` so the UI renders them as password inputs; the catalog server uses the same marker to identify fields requiring encryption at rest.

**Example:** `GET /api/v1/connectors/datasource/providers/object_storage/params`

**Response `200 OK` (object_storage example):**

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "required": [
    "endpoint_url",
    "bucket_name",
    "access_key_id",
    "secret_access_key",
    "allowed_extensions"
  ],
  "properties": {
    "endpoint_url": {
      "type": "string",
      "title": "Endpoint URL",
      "description": "S3 endpoint URL (e.g. https://s3.us-east-1.amazonaws.com).",
      "format": "uri",
      "ui:section": "Location"
    },
    "bucket_name": {
      "type": "string",
      "title": "Bucket name",
      "minLength": 1,
      "ui:section": "Location"
    },
    "prefix": {
      "type": "string",
      "title": "Prefix (optional)",
      "ui:section": "Location"
    },
    "delimiter": {
      "type": "string",
      "title": "Delimiter (optional)",
      "ui:section": "Location"
    },
    "access_key_id": {
      "type": "string",
      "title": "Access key ID",
      "minLength": 1,
      "ui:section": "Authentication"
    },
    "secret_access_key": {
      "type": "string",
      "title": "Secret access key",
      "format": "password",
      "minLength": 1,
      "ui:section": "Authentication"
    },
    "allowed_extensions": {
      "type": "array",
      "title": "Include files by type",
      "minItems": 1,
      "uniqueItems": true,
      "ui:section": "File filters",
      "items": { "type": "string", "enum": [".pdf", ".docx"] }
    }
  }
}
```

> **Note:** `allowed_extensions` is a **required** field. Sensitive fields use `"format": "password"`.

**Response `404 Not Found`:**

```json
{ "error": "connector provider not found: datasource/my_unknown_provider" }
```

### 6.7 List Providers for a Connector Type

**`GET /api/v1/connectors[?type=datasource]`**

Returns all registered providers for the given connector type, discovered from the `assets/connectors/<connector_type>/` directory (see Section 8.4). The UI uses this to populate the provider picker when creating a new connector.

**Query parameters:**

| Parameter | Type   | Required | Description                                                       |
| --------- | ------ | -------- | ----------------------------------------------------------------- |
| `type`    | string | No       | Filter by connector type (e.g. `datasource`). Omit to return all. |

**Example:** `GET /api/v1/connectors?type=datasource`

**Response `200 OK`:**

```json
[
  {
    "type": "datasource",
    "name": "Data sources",
    "provider": {
      "id": "object_storage",
      "name": "Object storage",
      "description": "HTTPS based connection with cloud S3-compatible object storage support",
      "schema": "/api/v1/connectors/datasource/providers/object_storage/params"
    }
  },
  {
    "type": "datasource",
    "name": "Data sources",
    "provider": {
      "id": "file_system",
      "name": "File system",
      "description": "SSH based connection with IBM i, AIX, Linux and Windows support.",
      "schema": "/api/v1/connectors/datasource/providers/file_system/params"
    }
  }
]
```

**Response `404 Not Found`:**

```json
{ "error": "connector type not found: datasource" }
```

### 6.8 Get Connected Services for Datasource

**`GET /api/v1/datasources/:id/services`**

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

**`PUT /api/v1/applications/:id/datasources/:datasource_id`**

Creates a link in `service_dependencies` and calls `POST /v1/connectors` on each eligible service.

**Rules:**

- The application must exist and be in `Running` status.
- The datasource must exist and be in `connected` status.
- The target service in the application must declare `accepts_datasource: true` in its catalog metadata.
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

**Response `422 Unprocessable Entity` (service does not accept datasource connectors):**

```json
{
  "error": "service does not accept datasource connectors"
}
```

**Response `422 Unprocessable Entity` (not Running):**

```json
{
  "error": "application must be in Running status before connecting a datasource"
}
```

### 6.11 Disconnect Datasource from Application

**`DELETE /api/v1/applications/:id/datasources/:datasource_id`**

Removes the link from `service_dependencies` and calls `DELETE /v1/connectors/:connectorid` on the Digitize service.

**Response `204 No Content`:** Disconnected.

**Response `404 Not Found`:**

```json
{ "error": "datasource is not connected to this application" }
```

### 6.12 Get Datasource Status for Application

**`GET /api/v1/applications/:id/datasources/:datasource_id`**

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
    "provider": "object_storage",
    "provider_name": "Object storage",
    "status": "connected"
  }
}
```

| Field                      | Source                                    | Description                                                                                 |
| -------------------------- | ----------------------------------------- | ------------------------------------------------------------------------------------------- |
| `connector_id`             | Digitize (echoed from `connectors.id`)    | The connector UUID used as the Digitize connector ID                                        |
| `sync_status`              | Digitize                                  | Current sync state: `"up to date"`, `"out of sync"`, `"started"`, `"completed"`, `"failed"` |
| `total_files`              | Digitize                                  | Total files known to the connector                                                          |
| `new_files`                | Digitize                                  | Files added since the last tick                                                             |
| `removed_files`            | Digitize                                  | Files removed since the last tick                                                           |
| `failed_files`             | Digitize                                  | Files that failed to process in the last tick                                               |
| `last_sync_at`             | Digitize                                  | Timestamp of the last completed sync                                                        |
| `last_sync_error`          | Digitize                                  | Error string from the last failed sync, or `null`                                           |
| `datasource.name`          | catalog (`connectors.name`)               | User-supplied display name of the datasource                                                |
| `datasource.type`          | catalog (`connectors.type`)               | Always `"datasource"`                                                                       |
| `datasource.provider`      | catalog (`connectors.provider`)           | Provider identifier, e.g. `"object_storage"`, `"file_system"`                               |
| `datasource.provider_name` | catalog (`CatalogProvider.LoadConnector`) | Human-readable provider name from `metadata.yaml`, e.g. `"Object storage"`, `"File system"` |
| `datasource.status`        | catalog (`connectors.status`)             | Catalog-side connectivity health: `"connected"` or `"offline"`                              |

**Response `404 Not Found`:** Returned if the datasource is not connected to this application (no row in `service_dependencies`).

---

### 6.13 List All Datasources for an Application

**`GET /api/v1/applications/:id/datasources`**

Returns all datasources currently connected to the given application, enriched with live sync status fetched from each service's Digitize pod.

**Path parameters:**

| Parameter | Type   | Description          |
| --------- | ------ | -------------------- |
| `id`      | `uuid` | The application UUID |

**Response `200 OK`:**

```json
{
  "application_id": "0d2de05d-...",
  "application_name": "My RAG App",
  "datasources": [
    {
      "datasource_id": "550e8400-e29b-41d4-a716-446655440000",
      "name": "My S3 Bucket",
      "type": "datasource",
      "provider": "object_storage",
      "provider_name": "Object storage",
      "status": "connected",
      "connector_id": "550e8400-e29b-41d4-a716-446655440000",
      "sync_status": "up to date",
      "total_files": 150,
      "new_files": 2,
      "removed_files": 0,
      "failed_files": 8,
      "last_sync_at": "2026-06-01T11:00:00Z",
      "last_sync_error": null
    },
    {
      "datasource_id": "661f9511-f30c-52e5-b827-557766551111",
      "name": "My SSH Share",
      "type": "datasource",
      "provider": "file_system",
      "provider_name": "File system",
      "status": "connected",
      "connector_id": "661f9511-f30c-52e5-b827-557766551111",
      "sync_status": "out of sync",
      "total_files": 40,
      "new_files": 0,
      "removed_files": 3,
      "failed_files": 1,
      "last_sync_at": "2026-06-01T08:00:00Z",
      "last_sync_error": "connection timeout on last tick"
    }
  ]
}
```

| Field                           | Source                                       | Description                                                                                 |
| ------------------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `application_id`                | path param                                   | The application UUID                                                                        |
| `application_name`              | catalog (`applications.name`)                | Display name of the application                                                             |
| `datasources[].datasource_id`   | catalog (`connectors.id`)                    | UUID of the datasource connector                                                            |
| `datasources[].name`            | catalog (`connectors.name`)                  | User-supplied display name of the datasource                                                |
| `datasources[].type`            | catalog (`connectors.type`)                  | Always `"datasource"`                                                                       |
| `datasources[].provider`        | catalog (`connectors.provider`)              | Provider identifier, e.g. `"object_storage"`, `"file_system"`                               |
| `datasources[].provider_name`   | catalog (`CatalogProvider.LoadConnector`)    | Human-readable provider name from `metadata.yaml`                                           |
| `datasources[].status`          | catalog (`connectors.status`)                | Catalog-side connectivity health: `"connected"` or `"offline"`                              |
| `datasources[].connector_id`    | Digitize (echoed from `connectors.id`)       | The connector UUID used as the Digitize connector ID                                        |
| `datasources[].sync_status`     | Digitize (`GET /v1/connectors/:connectorid`) | Current sync state: `"up to date"`, `"out of sync"`, `"started"`, `"completed"`, `"failed"` |
| `datasources[].total_files`     | Digitize                                     | Total files known to the connector                                                          |
| `datasources[].new_files`       | Digitize                                     | Files added since the last tick                                                             |
| `datasources[].removed_files`   | Digitize                                     | Files removed since the last tick                                                           |
| `datasources[].failed_files`    | Digitize                                     | Files that failed to process in the last tick                                               |
| `datasources[].last_sync_at`    | Digitize                                     | Timestamp of the last completed sync, or `null`                                             |
| `datasources[].last_sync_error` | Digitize                                     | Error string from the last failed sync, or `null`                                           |

**Backend logic:**

1. Look up the application by `id` — return `404` if not found.
2. Query `service_dependencies` for all rows where `dependency_type = 'connector'` linked to any service of this application.
3. For each unique `dependency_id` (datasource connector UUID), fetch the `connectors` row and resolve `provider_name` from the provider registry.
4. For each linked Digitize service pod, call `GET /v1/connectors/:connectorid` and merge the sync fields into the response entry.
5. Return the merged list. If Digitize is unreachable for a given entry, populate sync fields with `null` and `sync_status: "unknown"` rather than failing the whole request.

**Response `200 OK` (no datasources connected):**

```json
{
  "application_id": "0d2de05d-...",
  "application_name": "My RAG App",
  "datasources": []
}
```

**Response `404 Not Found`:**

```json
{ "error": "application not found" }
```

---

## 7. Digitize Service Integration

### 7.1 Connector API Overview

The Digitize service exposes a `/v1/connectors` API that manages the active linkage between a deployed application and a datasource. The catalog service acts as the client.

| Operation                         | Catalog Trigger                                                  | Digitize API Call                                                                                         |
| --------------------------------- | ---------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Connect datasource to application | `PUT /applications/:id/connectors/datasources/:datasource_id`    | `POST /v1/connectors` on each eligible service (using the datasource's `connectors.id` as `connector_id`) |
| Update datasource credentials     | `PUT /connectors/datasources/:id`                                | `PUT /v1/connectors/<connector_id>` on each service linked in `service_dependencies`                      |
| Disconnect datasource             | `DELETE /applications/:id/connectors/datasources/:datasource_id` | `DELETE /v1/connectors/<connector_id>` on each linked service; remove `service_dependencies` row          |
| Fetch sync status                 | `GET /applications/:id/connectors/datasources/:datasource_id`    | `GET /v1/connectors/<connector_id>` on each linked service                                                |

### 7.2 Connect Flow

When `PUT /api/v1/applications/:id/connectors/datasources/:datasource_id` is called:

1. Validate application is `Running`.
2. Validate datasource is `connected` (lookup in `connectors` table).
3. Resolve the target service in the application and verify its catalog metadata declares `accepts_datasource: true`. Also verify this datasource is not already linked to that `(service_id, datasource_id)` pair in `service_dependencies`.
4. Insert a `service_dependencies` row for the target service: `(service_id, dependency_id=datasource_id, dependency_type='connector')`. The inserted `dependency_id` is the Digitize `connector_id` for that service-datasource pair.
5. Call `POST /v1/connectors` on the target service's downstream pod, passing `dependency_id` (the datasource UUID) as `id`.

**`POST /v1/connectors` payload — S3 example:**

The payload shape is defined by the digitize proposal. The catalog service is responsible for constructing it exactly as follows before calling the endpoint.

```json
{
  "id": "<service_dependencies.dependency_id — the datasource UUID, unique per service>",
  "name": "<connectors.name of the datasource>",
  "type": "object_storage",
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
  "id": "<service_dependencies.dependency_id — the datasource UUID, unique per service>",
  "name": "<connectors.name of the datasource>",
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

The digitize service responds with `202 Accepted` (no response body). Nothing new needs to be stored — the `connector_id` sent was the `dependency_id` from the `service_dependencies` row, which is the datasource UUID scoped to that service.

### 7.3 Update Flow

When `PUT /api/v1/connectors/datasources/:id` is called:

1. Extract only the updatable credential fields for the provider (`access_key_id`, `secret_access_key` for `object_storage`; `username`, `private_key` for `file_system`). Ignore any other fields in the request body.
2. Validate connectivity using the new credentials merged with the existing immutable metadata (bucket, host, path, etc.).
3. If connectivity fails, return `422 Unprocessable Entity` — do not persist any changes.
4. Update only the updatable fields in the `connectors` record, leaving all other metadata intact.
5. Query `service_dependencies` to find all rows where `dependency_id = datasource_id` and `dependency_type = 'connector'`, joined to `services` to resolve the downstream pod.
6. For each link, call `PUT /v1/connectors/<dependency_id>` on the Digitize service with the updated credential payload, using `dependency_id` as the `connector_id`.
   - If the call fails, retry **once**.
   - If the retry also fails: record the error for that link and continue to the next — do **not** roll back the datasource record update.
7. The digitize service performs a partial update — only the credential fields sent in the payload are overwritten.
8. Return `200 OK`. If any propagations failed, include a `propagation_errors` array in the response so the UI can show an inline error against each affected application. The user retriggers by re-submitting the datasource update from the UI.

**Response when all propagations succeed:**

```json
{
  "id": "550e8400-...",
  "name": "My S3 Bucket",
  "provider": { "id": "object_storage", "name": "Object storage" },
  "status": "connected"
}
```

**Response when one or more propagations fail:**

```json
{
  "id": "550e8400-...",
  "name": "My S3 Bucket",
  "provider": { "id": "object_storage", "name": "Object storage" },
  "status": "connected",
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

1. Query `service_dependencies` where `dependency_id = datasource_id` and `dependency_type = 'connector'`, joined to `services` to filter to this application. Return `404` if no rows found.
2. For each row, call `DELETE /v1/connectors/<dependency_id>` on its downstream Digitize pod, using the row's `dependency_id` as the `connector_id`.
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

1. Return `404` if no record exists in `connectors` for the given `id`.
2. Query `service_dependencies` where `dependency_id = id` and `dependency_type = 'connector'`, joined with `services` and `applications`, collecting `application_id`, `application_name`, `catalog_id`, and the `endpoints` JSONB column.
3. For each linked service, call `GET /v1/connectors/<dependency_id>` on its downstream Digitize pod, using `dependency_id` as the `connector_id`. Extract `sync_status` and `last_sync_at` from the response.
4. If the Digitize call fails for a service, set `sync_status: null` and `last_sync_at: null` for that entry and continue — do not fail the entire request.
5. Return the assembled list.

### 7.6 Application-Scoped Status Fetch Flow

When `GET /api/v1/applications/:id/connectors/datasources/:datasource_id` is called:

1. Query `service_dependencies` where `dependency_id = datasource_id` and `dependency_type = 'connector'`, joined to `services` to filter to this application. Return `404` if no rows found.
2. Fetch the datasource record from the `connectors` table using `datasource_id`. Resolve `provider_name` via `catalogProvider.LoadConnector(connectorType, provider)`.
3. For each row, call `GET /v1/connectors/<dependency_id>` on its downstream Digitize pod, using `dependency_id` as the `connector_id`.
4. Merge the Digitize sync fields at the top level and nest the catalog identity fields (`name`, `type`, `provider`, `provider_name`, `status`) under a `datasource` key.

---

## 8. Datasource Type Providers

### 8.1 Provider Interface

The implementation uses a lean `ConnectionTester` interface. Display metadata, required-field lists, and sensitive-field identification are driven by the asset files (`metadata.yaml` + `schema.json`) loaded by `CatalogProvider` — not by interface methods.

```go
// ConnectionTester is the interface implemented by each datasource provider.
type ConnectionTester interface {
    // TestConnection runs provider-specific connectivity checks (network → auth → access).
    // Returns nil when all checks pass, or a *ConnectionCheckError on the first failure.
    TestConnection(ctx context.Context, params map[string]any) error
}
```

Testers are registered in a `map[string]ConnectionTester` inside `DatasourceService` keyed by provider ID:

```go
testers: map[string]ConnectionTester{
    "object_storage": NewObjectStorageTester(),
    "file_system":    NewFileSystemTester(),
}
```

Sensitive fields are identified at runtime by inspecting the provider's `schema.json` for properties whose `"format"` is `"password"` — not from a `sensitive_fields` list in `metadata.yaml` and not from a `SensitiveFields()` method. This logic lives in `sensitiveFieldsFromSchema(schema map[string]any) map[string]bool`.

Connection failures are returned as typed `*ConnectionCheckError` values carrying a `CheckType` (`network`, `auth`, or `access`) and a human-readable message, so the caller can surface a specific error phase to the user.

### 8.2 Object Storage Provider

**Provider ID:** `object_storage` (Digitize `type`: `s3`)

| Field                | Required | Sensitive | Updatable | Notes                                                                                                   |
| -------------------- | -------- | --------- | --------- | ------------------------------------------------------------------------------------------------------- |
| `endpoint_url`       | Yes      | No        | No        | Full S3 endpoint URL, e.g. `https://s3.us-east-1.amazonaws.com`. Region is auto-detected from this URL. |
| `bucket_name`        | Yes      | No        | No        |                                                                                                         |
| `access_key_id`      | Yes      | No        | **Yes**   | IAM key ID (AWS) or HMAC key ID (IBM COS)                                                               |
| `secret_access_key`  | Yes      | **Yes**   | **Yes**   | IAM secret (AWS) or HMAC secret (IBM COS). Marked `"format": "password"` in schema.json.                |
| `prefix`             | No       | No        | No        | Key prefix to scope the sync to a folder; empty means bucket root                                       |
| `delimiter`          | No       | No        | No        | Set `"/"` for non-recursive listing (immediate children only)                                           |
| `allowed_extensions` | **Yes**  | No        | No        | e.g. `[".pdf", ".docx"]`. Required field; at least one extension must be supplied.                      |

Connectivity test (`objectStorageTester.TestConnection`): Issues `ListObjectsV2(MaxKeys=0)` against the bucket. Region is derived from the endpoint URL using a regex that handles both AWS S3 and IBM COS patterns; defaults to `us-east-1` for non-matching URLs (e.g. MinIO). AWS S3 uses virtual-hosted-style addressing; IBM COS and other S3-compatible stores use path-style.

### 8.3 File System Provider

**Provider ID:** `file_system` (Digitize `type`: `ssh`)

| Field                | Required | Sensitive | Updatable | Notes                                                                              |
| -------------------- | -------- | --------- | --------- | ---------------------------------------------------------------------------------- |
| `host`               | Yes      | No        | No        | Hostname or IP address of the remote server                                        |
| `username`           | Yes      | No        | **Yes**   |                                                                                    |
| `private_key`        | Yes      | **Yes**   | **Yes**   | PEM-encoded private key. Marked `"format": "password"` in schema.json.             |
| `remote_path`        | Yes      | No        | No        | Absolute path on the remote file system server                                     |
| `port`               | No       | No        | No        | SSH port; defaults to `22` when absent                                             |
| `allowed_extensions` | **Yes**  | No        | No        | e.g. `[".pdf", ".docx"]`. Required field; at least one extension must be supplied. |

Connectivity test (`fileSystemTester.TestConnection`): Three sequential checks — (1) TCP dial to `host:port`; (2) SSH handshake with PEM private key (`ssh.InsecureIgnoreHostKey()` is acceptable for the diagnostic probe); (3) SFTP `Stat(remote_path)` to confirm the path exists and is a directory.

### 8.4 Connector Asset Directory

Connector provider metadata and input schemas live under a new top-level `assets/connectors/` directory, mirroring the `assets/components/<component_type>/<provider>/` pattern. Because connectors are pre-registered, externally managed resources — not deployed workloads — there are **no platform sub-directories** (`openshift/`, `podman/`). Each provider folder contains exactly two files:

| File            | Purpose                                                                               |
| --------------- | ------------------------------------------------------------------------------------- |
| `metadata.yaml` | Provider identity: `id`, `name`, `description`, `connector_type`, `connector_name`    |
| `schema.json`   | JSON Schema for the creation/edit form; sensitive fields carry `"format": "password"` |

> **No `sensitive_fields` in metadata.yaml.** The sensitive-field list is not stored in the YAML. The server derives it at runtime from `schema.json` properties whose `"format"` equals `"password"`.

**Directory layout:**

```
assets/connectors/
└── datasource/
    ├── object_storage/
    │   ├── metadata.yaml
    │   └── schema.json
    └── file_system/
        ├── metadata.yaml
        └── schema.json
```

Adding a new connector type requires only a new sub-directory under `assets/connectors/` with its own provider folders — no code changes to the router or loader.

**`assets/connectors/datasource/object_storage/metadata.yaml`:**

```yaml
type: connector
id: object_storage
name: "Object storage"
description: "HTTPS based connection with cloud S3-compatible object storage support"
connector_type: datasource
connector_name: "Data sources"
```

**`assets/connectors/datasource/file_system/metadata.yaml`:**

```yaml
type: connector
id: file_system
name: "File system"
description: "SSH based connection with IBM i, AIX, Linux and Windows support."
connector_type: datasource
connector_name: "Data sources"
```

The Go type used when loading connector assets, mirroring [`types.Component`](../../ai-services/internal/pkg/catalog/types/types.go):

```go
// ConnectorProvider is loaded from assets/connectors/<connector_type>/<provider>/metadata.yaml.
type ConnectorProvider struct {
    Type            string   `yaml:"type"`             // always "connector"
    ID              string   `yaml:"id"`               // e.g. "s3"
    Name            string   `yaml:"name"`
    Description     string   `yaml:"description"`
    ConnectorType   string   `yaml:"connector_type"`   // e.g. "datasource"
    ConnectorName   string   `yaml:"connector_name"`   // human-readable category label, e.g. "Data Source"
    SensitiveFields []string `yaml:"sensitive_fields"` // never returned in API responses
}
```

The `schema.json` for each provider is served verbatim by `GET /api/v1/connectors/:connector_type/providers/:provider_id/schema`.

---

## 9. Secret Encryption

### 9.1 Encryption Scheme Overview

A unique **Key Encryption Key (KEK)** is generated at application deploy time and deployed as a Kubernetes/Podman Secret. It is mounted into each Digitize service pod in that application. There is no DEK layer — the KEK is the only key.

The catalog backend passes plaintext credentials to the Digitize service over the connector API; encryption at rest is the responsibility of the receiving service.

### 9.2 Deployment Changes

- At application deploy time, generate a cryptographically secure random 32-byte KEK.
- Deploy it as a Kubernetes/Podman Secret scoped to that application's namespace.
- Mount the secret into each Digitize service pod (at `/run/secrets/connector_kek`).

---

## 10. Sync Service

The **catalog-side sync service** is a connector-agnostic heartbeat that validates the health of every record in the `connectors` table. It is designed to serve datasource connectors today and to be extended for future connector types without structural change.

### 10.1 Sync Job Design

A single periodic background job (`ConnectorSyncJob`) runs in the catalog backend process on a configurable schedule (default: every 5 minutes). It queries the `connectors` table for all records. For each record:

1. Looks up the registered provider for the connector's `type` and `provider` fields.
2. Calls `provider.TestConnection(ctx, metadata)`.
3. If the test passes: sets `status = "connected"`, clears `message`.
4. If the test fails: sets `status = "offline"`, stores the specific error string in `message` (e.g., credential failure, permission denied, network unreachable).
5. Writes the updated status back to the `connectors` table via `connector_repo.UpdateStatus()`.

New connector types plug in by implementing the `ConnectorProvider` interface (see Section 8.1) and registering with the provider registry. No changes to the sync job itself are required.

### 10.2 Status Updates

The sync job uses a new `connector_repo.UpdateStatus(ctx, id, status, message)` method on the `connectors` repository.

There are two distinct status spaces:

**Connectors page** (`connectors.status`, driven by `ConnectorSyncJob`):

| Status      | Meaning                                                                                                                                                    |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `connected` | Sync job successfully reached the datasource — credentials valid, files/folders accessible                                                                 |
| `offline`   | Sync job could not reach the datasource — invalid credentials, permission error on folder/files, network issue, etc. `message` contains the specific error |

**Application-datasources page** (live from Digitize, via `dependency_id` as `connector_id`):

The catalog backend fetches status from `GET /v1/connectors/:connectorid` (see §7.5) and cascades all fields — `status`, `message`, `files_processed`, `last_synced_at`, etc. — to the UI verbatim. No mapping or transformation is applied.

---

## 11. Application Lifecycle Integration

### 11.1 Datasource Connection During Create Flow

#### How the UI Obtains Datasource IDs

The UI populates the connector selection step by calling `GET /api/v1/connectors/datasources` — the standard paginated list endpoint (Section 6.1). It filters to `status=connected` so only healthy datasources are shown. The user picks one or more from that list. Their UUIDs are then included in the create application request.

#### Request Body Change

Connectors are expressed as a dedicated `connectors` key on each [`Service`](../../ai-services/internal/pkg/catalog/apiserver/models/create_application.go) in the request body — **separate from and alongside `components`**. The `Component` struct and `components` array are **not modified**. Connectors are never expressed as components.

Each connector entry carries the `type` of connector (e.g. `"datasource"`) and the `id` of the pre-registered record in the `connectors` table. Keeping `type` explicit on every entry means the field is extensible: future connector kinds (e.g. `"vector_store"`, `"model"`) are added without any schema change.

```go
// Service represents a service configuration in the application.
// Connectors is a new optional field; Components is unchanged.
type Service struct {
    CatalogID  string         `json:"catalog_id"  binding:"required"`
    Version    string         `json:"version"     binding:"required"`
    Components []Component    `json:"components"  binding:"required,dive"`
    Connectors []ConnectorRef `json:"connectors,omitempty"`
    Params     map[string]any `json:"params"`
}

// ConnectorRef references a pre-registered connector record by its ID.
// Type identifies the connector kind and must match the record's type in
// the connectors table (e.g. "datasource").
type ConnectorRef struct {
    ID   string `json:"id"   binding:"required"` // UUID from the connectors table
    Type string `json:"type" binding:"required"` // e.g. "datasource"
}

// Component is unchanged — no ID field, no connector entries.
type Component struct {
    ComponentType string         `json:"component_type" binding:"required"`
    ProviderID    string         `json:"provider_id"    binding:"required"`
    Version       string         `json:"version"        binding:"required"`
    Params        map[string]any `json:"params"`
}
```

**Which connector types a service accepts is declared in its catalog YAML** via the `accepts_datasource` boolean flag (see below). The handler validates each `ConnectorRef` against this flag before deployment begins.

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
        }
      ],
      "connectors": [
        { "type": "datasource", "id": "550e8400-e29b-41d4-a716-446655440000" },
        { "type": "datasource", "id": "661f9511-f30c-52e5-b827-557766551111" }
      ]
    },
    {
      "catalog_id": "summarize",
      "version": "2.0.0",
      "components": [
        {
          "component_type": "llm",
          "provider_id": "watsonx",
          "version": "1.0.0",
          "params": { "model_id": "ibm/granite-13b-chat-v2" }
        }
      ],
      "connectors": [
        { "type": "datasource", "id": "550e8400-e29b-41d4-a716-446655440000" }
      ]
    }
  ]
}
```

**Validation rules applied per service before deployment begins:**

- Each `ConnectorRef.type` of `"datasource"` is only valid for services whose catalog YAML declares `accepts_datasource: true`. A datasource ref on a service without this flag is rejected with `400 Bad Request`.
- Each `ConnectorRef.id` must reference a record in the `connectors` table whose `type` matches `ConnectorRef.type` and whose `status` is `"connected"`. Any unknown, type-mismatched, or non-`connected` ID is rejected with `400 Bad Request`.
- The same `id` may not appear more than once within a single service's `connectors` list.
- Services that omit `connectors` (or pass an empty list) are deployed without any connector attachment — this is valid.
- The same connector `id` may appear in multiple services' `connectors` lists; each produces an independent `service_dependencies` row, and the `dependency_id` is used as the Digitize `connector_id` for that specific service.

#### Service Catalog YAML Change

Each service that accepts datasource connectors declares this with a boolean flag in its catalog YAML:

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

# Set to true if this service can receive datasource connectors via the catalog.
accepts_datasource: true
```

Services that do not accept datasource connectors omit the field entirely (equivalent to `false`).

The corresponding Go field on [`types.Service`](../../ai-services/internal/pkg/catalog/types/types.go):

```go
// In Service struct (already present):
AcceptsDatasource bool `yaml:"accepts_datasource,omitempty" json:"accepts_datasource,omitempty"`
```

#### Deploy-Options API Response Change

**Endpoint:** `GET /api/v1/architectures/{id}/deploy-options`

Because connectors are not components, the `components` array inside each service entry **is not modified**. Instead, each service entry gains a new `accepts_datasource` boolean that mirrors the flag from the service's catalog YAML. The UI uses this to decide whether to render a connector picker for that service.

The standalone deploy-options path (`GET /api/v1/services/{id}/deploy-options`) also returns `accepts_datasource`, so connector attachment is available for standalone service deployments when the selected service supports it.

**How the build works in [`buildSingleService`](../../ai-services/internal/pkg/catalog/deploy_options.go:74):**

```go
// Copy accepts_datasource from the catalog YAML directly onto the response object.
// No changes to the components slice.
deployService.AcceptsDatasource = service.AcceptsDatasource
```

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
      "accepts_datasource": true,
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
        }
      ]
    },
    {
      "id": "chat",
      "name": "Chat",
      "version": "2.0.0",
      "schema": "/api/v1/services/chat/params",
      "accepts_datasource": false,
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

The `accepts_datasource: true` on the `digitize` service signals the UI to render a connector picker (populated from `GET /api/v1/connectors/datasources?status=connected`) alongside the component configuration for that service. The `chat` service has `accepts_datasource: false`, so no picker is shown for it. The `components` arrays are completely unchanged — no connector entries appear inside them.

**Updated `DeployOptionsService` response schema:**

| Field                | Type   | Description                                                                                                                          |
| -------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------ |
| `id`                 | string | Service identifier                                                                                                                   |
| `name`               | string | Service display name                                                                                                                 |
| `version`            | string | Service version                                                                                                                      |
| `schema`             | string | URL to fetch service-level parameters                                                                                                |
| `accepts_datasource` | bool   | `true` if this service accepts datasource connectors. Present on both architecture and standalone deploy-options responses. **New.** |
| `components`         | array  | Array of deployable component objects with providers. Unchanged — no connector entries are added here.                               |
| `resources`          | object | Optional resource requirements for this service                                                                                      |

> **Backward compatibility:** `accepts_datasource` is a purely additive new field. Existing clients that do not read it are unaffected. The `components` arrays are completely unchanged.

#### Post-Deploy Connect Flow

1. The deployment proceeds normally. Connector attachment is **not** attempted until the application reaches `Running` status.
2. Once the application transitions to `Running`, the deployment completion callback iterates over each service's `connectors` list as supplied in the original create request. For each `ConnectorRef`, it calls `PUT /api/v1/applications/:id/services/:service_id/connectors/datasources/:datasource_id`. The `service_dependencies` insertion happens inside that handler.
3. If any downstream service call fails after one retry, the application remains `Running` (deployment was successful). The `service_dependencies` row is still inserted so the link is tracked, but the UI surfaces the failure against the specific service so the user can retrigger via a datasource update.

### 11.2 Datasource Connection Post-Creation

Users can connect or disconnect datasources from any `Running` application at any time via the Catalog UI, using the `PUT` and `DELETE` endpoints described in Section 6. No re-deployment is required. The connect endpoint is:

```
PUT /api/v1/applications/:id/connectors/datasources/:datasource_id
```

The handler validates:

- The application exists and is `Running`.
- The target service(s) in the application declare `accepts_datasource: true` in their catalog YAML.
- The datasource exists in the `connectors` table with `status = "connected"`.
- The `(service_id, datasource_id)` pair does not already exist in `service_dependencies`.

> **Restriction:** The connect endpoint enforces the service-level guard server-side. When called for a service whose catalog YAML does not have `accepts_datasource: true`, the handler returns `422 Unprocessable Entity`:
>
> ```json
> { "error": "service does not accept datasource connectors" }
> ```
>
> This guard is independent of the UI — it prevents direct API calls from bypassing the restriction.

---

## 12. Error Handling

The feature follows the existing error response convention established in the application handler:

```json
{ "error": "<human-readable description>" }
```

| Scenario                                                                                        | HTTP Status                                                                                                      |
| ----------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Datasource not found                                                                            | `404 Not Found`                                                                                                  |
| Application not found                                                                           | `404 Not Found`                                                                                                  |
| Invalid request body or missing required fields                                                 | `400 Bad Request`                                                                                                |
| Connectivity test failed on create/update (triggered internally by create and update handlers)  | `422 Unprocessable Entity`                                                                                       |
| Datasource already connected to application                                                     | `409 Conflict`                                                                                                   |
| Delete attempted while datasource has active connections                                        | `409 Conflict`                                                                                                   |
| Application not in `Running` status during connect                                              | `422 Unprocessable Entity`                                                                                       |
| Connect attempted for a service whose catalog metadata does not have `accepts_datasource: true` | `422 Unprocessable Entity`                                                                                       |
| Digitize `POST /v1/connectors` returns `409 Conflict`                                           | `409 Conflict` — connector already exists; catalog should use `PUT`                                              |
| Digitize `PUT /v1/connectors` failed after 1 retry                                              | `200 OK` — datasource update succeeds; `propagation_errors` array in response lists affected applications        |
| Digitize `DELETE /v1/connectors` failed (non-404)                                               | `200 OK` — succeeded services are disconnected; `propagation_errors` array lists failed services; user can retry |
| Digitize service API call failed (non-404, non-PUT)                                             | `502 Bad Gateway` (with `error` field describing the downstream failure)                                         |

---

## 13. Security Considerations

- **Credentials never returned in API responses.** Sensitive fields are identified at runtime from each provider's `schema.json` (properties with `"format": "password"`) via `sensitiveFieldsFromSchema`, then stripped using `catalogutils.StripSensitiveFields` before any API response is serialised. For `file_system` connectors, `private_key` is never returned. For `object_storage` connectors, `secret_access_key` is never returned; `access_key_id` is returned as it is not sensitive.
- **IAM least-privilege for S3.** Only `s3:GetObject` and `s3:ListBucket` on the target bucket should be granted to the IAM user whose keys are stored.
- **Connectivity test before storage.** A live connectivity test is run internally by the create and update handlers before any record is persisted.
- **Auth middleware on all routes.** All datasource and application-datasource endpoints are protected by the existing `AuthMiddleware` following the pattern in [`router.go`](../../ai-services/internal/pkg/catalog/apiserver/router.go).
