# Database Datasource Connector — Design Proposal

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Background and Motivation](#2-background-and-motivation)
3. [Extensibility Model Recap](#3-extensibility-model-recap)
4. [Database Provider Design](#4-database-provider-design)
   - 4.1 [Provider Identity](#41-provider-identity)
   - 4.2 [Connection Parameters](#42-connection-parameters)
   - 4.3 [Connection Test Strategy](#43-connection-test-strategy)
   - 4.4 [Sensitive Fields](#44-sensitive-fields)
   - 4.5 [Updatable Fields](#45-updatable-fields)
5. [Asset Files](#5-asset-files)
   - 5.1 [metadata.yaml](#51-metadatayaml)
   - 5.2 [schema.json](#52-schemajson)

---

## 1. Executive Summary

This document describes how to add a **Database** datasource connector to the AI Services Catalog. The Database connector allows connecting to a database endpoint using standard database drivers or connectivity testing utilities.

The connector follows the **exact same extensibility pattern** already established by the `object_storage` and `file_system` providers: two asset files (`metadata.yaml` + `schema.json`) plus one new `ConnectionTester` implementation (`dbproviderTester`). No other layer of the system requires structural change.

---

## 2. Background and Motivation

### What changes vs. what stays the same

| Concern                        | Already in place — no change needed                                         |
| ------------------------------ | --------------------------------------------------------------------------- |
| `connectors` DB table          | Reused as-is (JSONB `metadata` stores the DB connection params)             |
| `service_dependencies` linking | Reused as-is; `dependency_type = 'connector'` already supports any provider |
| CRUD API routes                | Reused as-is (`POST /datasources`, `PUT`, `DELETE`, …)                      |
| Encryption at rest             | `AES-256-GCM` via `catalogutils.Encrypt` / `Decrypt` — reused as-is         |
| `ConnectorSyncJob`             | Picks up the new provider automatically via the `testers` map               |
| API response DTOs              | Reused as-is (`DatasourceResponse`, `GetDatasourceResponse`, …)             |

| Concern                  | New work in this proposal                                                             |
| ------------------------ | ------------------------------------------------------------------------------------- |
| Asset files              | `assets/connectors/datasource/<dbprovider>/metadata.yaml` + `schema.json`             |
| ConnectionTester         | `databaseTester` in `datasource_service/<dbprovider>.go`                              |
| Tester registration      | One extra map entry in `NewDatasourceService` and `NewConnectorSyncJob`               |
| Provider ID constant     | `DatasourceProviderDatabase = "database"` in `constants/catalog.go`                   |
| Digitize service support | Digitize must implement `type: "<dbprovider>"` connector handling (out of scope here) |

---

## 3. Extensibility Model Recap

The existing codebase was deliberately designed so that adding a new datasource provider requires only:

1. **Two asset files** under `assets/connectors/datasource/<provider_id>/`
2. **One `ConnectionTester` implementation** in the `datasource_service` package
3. **One map entry** in `NewDatasourceService` and `NewConnectorSyncJob`
4. **One constant** in `constants/catalog.go`

No changes are needed to:

- The `connectors` table schema or any DB migration
- The `ConnectorRepository` interface or its PostgreSQL implementation
- Any API handler, router, or request/response model
- The `ConnectorSyncJob` loop — it iterates over `j.testers[c.Provider]` dynamically
- The encryption/decryption pipeline — `SensitiveFieldsFromSchema` derives the sensitive-field set from `schema.json` at runtime

This is proven by the existing pattern: `object_storage` and `file_system` are both fully autonomous; [`constants/catalog.go`](../../ai-services/internal/pkg/catalog/constants/catalog.go) is the only shared file they both touch.

```
assets/connectors/
└── datasource/
    ├── object_storage/          ← existing
    │   ├── metadata.yaml
    │   └── schema.json
    ├── file_system/             ← existing
    │   ├── metadata.yaml
    │   └── schema.json
    └── <dbprovider>/                ← NEW (this proposal)
        ├── metadata.yaml
        └── schema.json
```

```go
// In NewDatasourceService — existing entries + one new line
testers: map[string]ConnectionTester{
    catalogconstants.DatasourceProviderObjectStorage: NewObjectStorageTester(),
    catalogconstants.DatasourceProviderFileSystem:    NewFileSystemTester(),
    catalogconstants.DatasourceProviderDbProvider:      NewDbProviderTester(), // NEW
},
```

---

## 4. Database Provider Design

### 4.1 Provider Identity

| Field            | Value                                                                           |
| ---------------- | ------------------------------------------------------------------------------- |
| `provider_id`    | `dbprovider`                                                                    |
| `connector_type` | `datasource`                                                                    |
| `digitize_type`  | `dbprovider` (value sent in the `type` field of `POST /v1/connectors` payloads) |
| Display name     | `"DB Provider"`                                                                 |
| Description      | `"Connection to a database"`                                                    |

### 4.2 Connection Parameters

The parameter set covers the minimum fields required to open and authenticate a connection to a database.

| Field      | Required | Sensitive | Updatable | Notes                                                                |
| ---------- | -------- | --------- | --------- | -------------------------------------------------------------------- |
| `host`     | Yes      | No        | No        | Hostname or IP address of the database server                        |
| `port`     | No       | No        | No        | Database port; defaults to the database standard port when absent    |
| `database` | Yes      | No        | No        | Name of the database or service schema                               |
| `username` | Yes      | No        | **Yes**   | Database user profile                                                |
| `password` | Yes      | **Yes**   | **Yes**   | Database user password. Marked `"format": "password"` in schema.json |

**Field rationale:**

- `host` + `port` + `database` — identifies the database system, instance, and named database/schema directory.
- `username` + `password` — database uses profile-based authentication. Password is the only sensitive field; `username` is updatable because database environments often rotate service accounts without changing the endpoint.

### 4.3 Connection Test Strategy

The `dbProviderTester.TestConnection` method mirrors the three-phase pattern used by [`fileSystemTester`](../../ai-services/internal/pkg/catalog/apiserver/repository/datasource_service/file_system.go) and [`objectStorageTester`](../../ai-services/internal/pkg/catalog/apiserver/repository/datasource_service/object_storage.go):

**Phase 1 — Network**: TCP dial to `host:port` with a 10-second timeout. Failure → `ConnectionCheckNetwork`.

**Phase 2 — Auth**: Open a database connection with the supplied `username` and `password`. Failure → `ConnectionCheckAuth`.

**Phase 3 — Access**: Execute a simple validation query (such as `SELECT 1` or standard ping) to confirm query access. Failure → `ConnectionCheckAccess`.

The connection check error messages should be clear and actionable:

| Phase   | Example message                                                                |
| ------- | ------------------------------------------------------------------------------ |
| Network | `TCP dial 192.168.1.10:5432 failed: connection refused`                        |
| Auth    | `Database authentication failed for user SVCACCT: check username and password` |
| Access  | `Database query test failed: connection is read-only or permission denied`     |

### 4.4 Sensitive Fields

Exactly one field is sensitive: **`password`**. It carries `"format": "password"` in `schema.json`. The `catalogutils.SensitiveFieldsFromSchema` utility (used by both `DatasourceService` and `ConnectorSyncJob`) will automatically identify it at runtime — no hardcoded list is required.

```go
// catalogutils.SensitiveFieldsFromSchema returns: map[string]bool{"password": true}
// for the dbprovider schema.json
```

Encryption at rest is handled by the existing `AES-256-GCM` pipeline in `catalogutils.Encrypt` / `Decrypt`. The `password` field will be encrypted before storage in the `connectors.metadata` JSONB column and decrypted in-memory only when needed by `TestConnection` or Digitize propagation calls. It is never returned in any API response.

### 4.5 Updatable Fields

Fields whose `"ui:section"` is `"Authentication"` in `schema.json` are updatable after creation:

| Field      | Updatable | Rationale                                                             |
| ---------- | --------- | --------------------------------------------------------------------- |
| `username` | **Yes**   | Database administrators rotate service accounts; endpoint stays same  |
| `password` | **Yes**   | Credential rotation without touching the DB endpoint or schema config |

All other fields (`host`, `port`, `database`) are structural and immutable after creation. The `catalogutils.UpdatableFieldsFromSchema` utility reads `ui:section: "Authentication"` from `schema.json` to derive the updatable set at runtime.

---

## 5. Asset Files

### 5.1 `metadata.yaml`

**Path:** `ai-services/assets/connectors/datasource/<dbprovider>/metadata.yaml`

```yaml
# Connector identification
type: connector
id: <dbprovider>
name: "<DB Provider>"
description: "Connection to a database"

# Connector type this provider belongs to
connector_type: datasource
connector_name: "Data sources"
```

This file is loaded at startup by `CatalogProvider.loadEmbeddedItems` and returned verbatim by `GET /api/v1/connectors?type=datasource` as part of the [`ConnectorResponse`](../../ai-services/internal/pkg/catalog/types/types.go) shape. No code changes are needed in `catalog.go` — the directory scanner discovers new connector providers automatically.

### 5.2 `schema.json`

**Path:** `ai-services/assets/connectors/datasource/<dbprovider>/schema.json`

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "required": ["host", "database", "username", "password"],
  "properties": {
    "host": {
      "type": "string",
      "title": "Hostname or IP address",
      "description": "Hostname or IP address of the database server.",
      "minLength": 1,
      "ui:section": "Location"
    },
    "port": {
      "type": "string",
      "title": "Port (optional)",
      "description": "Database port number.",
      "ui:section": "Location"
    },
    "database": {
      "type": "string",
      "title": "Database name",
      "description": "Name of the database or service schema.",
      "minLength": 1,
      "ui:section": "Location"
    },
    "username": {
      "type": "string",
      "title": "Username",
      "description": "Database username used to authenticate.",
      "minLength": 1,
      "ui:section": "Authentication"
    },
    "password": {
      "type": "string",
      "title": "Password",
      "description": "Database password.",
      "format": "password",
      "minLength": 1,
      "ui:section": "Authentication"
    }
  }
}
```

**Key schema decisions:**

- `"format": "password"` on `password` — consumed by `catalogutils.SensitiveFieldsFromSchema` to derive the encryption set at runtime.
- `"ui:section": "Authentication"` on `username` and `password` — consumed by `catalogutils.UpdatableFieldsFromSchema` to derive the updatable fields at runtime.
- `"ui:section": "Location"` on all structural fields — ensures they are excluded from the updatable set without any hardcoded allowlist.
- `port` is `"type": "string"` rather than integer, matching the `file_system` schema pattern (port is also a string there) so that the validator and UI can handle the optional-with-default pattern uniformly.

---
