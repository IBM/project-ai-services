# IBM i DB2 Database Datasource Connector — Design Proposal

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Background and Motivation](#2-background-and-motivation)
3. [Extensibility Model Recap](#3-extensibility-model-recap)
4. [IBM i DB2 Provider Design](#4-ibmi-db2-provider-design)
   - 4.1 [Provider Identity](#41-provider-identity)
   - 4.2 [Connection Parameters](#42-connection-parameters)
   - 4.3 [Connection Test Strategy](#43-connection-test-strategy)
   - 4.4 [Sensitive Fields](#44-sensitive-fields)
   - 4.5 [Updatable Fields](#45-updatable-fields)
5. [Asset Files](#5-asset-files)
   - 5.1 [metadata.yaml](#51-metadatayaml)
   - 5.2 [schema.json](#52-schemajson)
6. [Adding Future Database Providers](#6-adding-future-database-providers)

---

## 1. Executive Summary

This document describes how to add an **IBM i DB2 Database** datasource connector to the AI Services Catalog. IBM i DB2 is the native relational database engine on IBM Power Systems running IBM i. It exposes a wire-compatible DRDA/JDBC endpoint over TCP that can be reached from a Linux host using the [IBM i Access ODBC driver](https://www.ibm.com/support/pages/ibm-i-access-client-solutions) or, for connectivity testing, a pure-Go database driver.

The connector follows the **exact same extensibility pattern** already established by the `object_storage` and `file_system` providers: two asset files (`metadata.yaml` + `schema.json`) plus one new `ConnectionTester` implementation (`ibmiDB2Tester`). No other layer of the system requires structural change.

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

| Concern                  | New work in this proposal                                                         |
| ------------------------ | --------------------------------------------------------------------------------- |
| Asset files              | `assets/connectors/datasource/ibmi_db2/metadata.yaml` + `schema.json`             |
| ConnectionTester         | `ibmiDB2Tester` in `datasource_service/ibmi_db2.go`                               |
| Tester registration      | One extra map entry in `NewDatasourceService` and `NewConnectorSyncJob`           |
| Provider ID constant     | `DatasourceProviderIBMiDB2 = "ibmi_db2"` in `constants/catalog.go`                |
| Digitize service support | Digitize must implement `type: "ibmi_db2"` connector handling (out of scope here) |

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
    └── ibmi_db2/                ← NEW (this proposal)
        ├── metadata.yaml
        └── schema.json
```

```go
// In NewDatasourceService — existing entries + one new line
testers: map[string]ConnectionTester{
    catalogconstants.DatasourceProviderObjectStorage: NewObjectStorageTester(),
    catalogconstants.DatasourceProviderFileSystem:    NewFileSystemTester(),
    catalogconstants.DatasourceProviderIBMiDB2:       NewIBMiDB2Tester(), // NEW
},
```

---

## 4. IBM i DB2 Provider Design

### 4.1 Provider Identity

| Field            | Value                                                                         |
| ---------------- | ----------------------------------------------------------------------------- |
| `provider_id`    | `ibmi_db2`                                                                    |
| `connector_type` | `datasource`                                                                  |
| `digitize_type`  | `ibmi_db2` (value sent in the `type` field of `POST /v1/connectors` payloads) |
| Display name     | `"IBM i DB2"`                                                                 |
| Description      | `"DRDA/JDBC connection to IBM i Db2 for i (DB2/400) over TCP"`                |

### 4.2 Connection Parameters

The parameter set covers the minimum fields required to open and authenticate a DRDA connection to IBM i DB2, plus optional SQL-level scoping fields that allow the Digitize service to read table data and build documents.

| Field                | Required | Sensitive | Updatable | Notes                                                                                 |
| -------------------- | -------- | --------- | --------- | ------------------------------------------------------------------------------------- |
| `host`               | Yes      | No        | No        | Hostname or IP address of the IBM i system                                            |
| `port`               | No       | No        | No        | DRDA port; defaults to `446` (IBM i default) when absent                              |
| `database`           | Yes      | No        | No        | IBM i relational database directory name (typically the system name or `*LOCAL`)      |
| `username`           | Yes      | No        | **Yes**   | IBM i user profile                                                                    |
| `password`           | Yes      | **Yes**   | **Yes**   | IBM i user password. Marked `"format": "password"` in schema.json                     |
| `schema`             | No       | No        | No        | Library/schema to scope ingestion to (e.g. `PRODLIB`). Empty = all accessible schemas |
| `table_filter`       | No       | No        | No        | Comma-separated list of table names or SQL `LIKE` patterns (e.g. `ORDERS,INVOICE%`)   |
| `query`              | No       | No        | No        | Optional custom SQL SELECT to use instead of full-table scanning                      |
| `ssl`                | No       | No        | No        | Boolean — enable TLS for the DRDA connection; defaults to `false`                     |
| `allowed_extensions` | **Yes**  | No        | No        | Must include at least one value, e.g. `[".txt", ".csv", ".json"]`                     |

**Field rationale:**

- `host` + `port` + `database` — identifies the IBM i system and the named relational database directory. On IBM i there can be multiple named databases on a single partition.
- `username` + `password` — IBM i uses profile-based authentication. Password is the only sensitive field; `username` is updatable because IBM i sites often rotate service accounts without changing the endpoint.
- `schema` — limits which DB2 library the Digitize sync scanner reads, preventing accidental ingestion of system libraries (`QSYS`, `QUSRSYS`, etc.).
- `table_filter` — further narrows which tables are scanned, enabling targeted ingestion without requiring a custom SQL query.
- `query` — escape hatch for complex JOINs, VIEWs, or filtered projections when table-level access is too broad.
- `ssl` — IBM i 7.3+ supports TLS on the DRDA port; this flag activates it. Defaults to `false` for backward compatibility with older IBM i releases.
- `allowed_extensions` — consistent with all other providers; the Digitize service uses this to decide output file format for rows converted to documents (e.g. `.json` for structured row data, `.txt` for text columns).

### 4.3 Connection Test Strategy

The `ibmiDB2Tester.TestConnection` method mirrors the three-phase pattern used by [`fileSystemTester`](../../ai-services/internal/pkg/catalog/apiserver/repository/datasource_service/file_system.go) and [`objectStorageTester`](../../ai-services/internal/pkg/catalog/apiserver/repository/datasource_service/object_storage.go):

**Phase 1 — Network**: TCP dial to `host:port` with a 10-second timeout. Failure → `ConnectionCheckNetwork`.

**Phase 2 — Auth**: Open a DRDA/JDBC database connection with the supplied `username` and `password`. Failure → `ConnectionCheckAuth`.

**Phase 3 — Access**: Execute `SELECT 1 FROM SYSIBM.SYSDUMMY1` (IBM i's single-row dummy table, equivalent to `DUAL` in Oracle). If a `schema` is supplied, additionally run `SELECT COUNT(*) FROM QSYS.SYSTABLES WHERE TABLE_SCHEMA = '<schema>' FETCH FIRST 1 ROW ONLY` to verify the schema exists and the user has read access. Failure → `ConnectionCheckAccess`.

> **Dependency note:** No new Go modules are added to `go.mod` in the default build. The IBM i DB2 provider is guarded by the `ibmi_db2` build tag. Container image variants that include the IBM i Access ODBC driver will set this tag at build time.

The connection check error messages should be IBM i-specific and actionable:

| Phase   | Example message                                                                            |
| ------- | ------------------------------------------------------------------------------------------ |
| Network | `TCP dial 192.168.1.10:446 failed: connection refused`                                     |
| Auth    | `DB2 authentication failed for user SVCACCT: check username and password for IBM i system` |
| Access  | `schema "PRODLIB" not found or not accessible: verify the user profile has *USE authority` |

### 4.4 Sensitive Fields

Exactly one field is sensitive: **`password`**. It carries `"format": "password"` in `schema.json`. The `catalogutils.SensitiveFieldsFromSchema` utility (used by both `DatasourceService` and `ConnectorSyncJob`) will automatically identify it at runtime — no hardcoded list is required.

```go
// catalogutils.SensitiveFieldsFromSchema returns: map[string]bool{"password": true}
// for the ibmi_db2 schema.json
```

Encryption at rest is handled by the existing `AES-256-GCM` pipeline in `catalogutils.Encrypt` / `Decrypt`. The `password` field will be encrypted before storage in the `connectors.metadata` JSONB column and decrypted in-memory only when needed by `TestConnection` or Digitize propagation calls. It is never returned in any API response.

### 4.5 Updatable Fields

Fields whose `"ui:section"` is `"Authentication"` in `schema.json` are updatable after creation:

| Field      | Updatable | Rationale                                                             |
| ---------- | --------- | --------------------------------------------------------------------- |
| `username` | **Yes**   | IBM i sites rotate service accounts; the endpoint does not change     |
| `password` | **Yes**   | Credential rotation without touching the DB endpoint or schema config |

All other fields (`host`, `port`, `database`, `schema`, `table_filter`, `query`, `ssl`) are structural and immutable after creation. The `catalogutils.UpdatableFieldsFromSchema` utility reads `ui:section: "Authentication"` from `schema.json` to derive the updatable set at runtime.

---

## 5. Asset Files

### 5.1 `metadata.yaml`

**Path:** `ai-services/assets/connectors/datasource/ibmi_db2/metadata.yaml`

```yaml
# Connector identification
type: connector
id: ibmi_db2
name: "IBM i DB2"
description: "DRDA/JDBC connection to IBM i Db2 for i (DB2/400) over TCP"

# Connector type this provider belongs to
connector_type: datasource
connector_name: "Data sources"
```

This file is loaded at startup by `CatalogProvider.loadEmbeddedItems` and returned verbatim by `GET /api/v1/connectors?type=datasource` as part of the [`ConnectorResponse`](../../ai-services/internal/pkg/catalog/types/types.go) shape. No code changes are needed in `catalog.go` — the directory scanner discovers new connector providers automatically.

### 5.2 `schema.json`

**Path:** `ai-services/assets/connectors/datasource/ibmi_db2/schema.json`

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "required": [
    "host",
    "database",
    "username",
    "password",
    "allowed_extensions"
  ],
  "properties": {
    "host": {
      "type": "string",
      "title": "Hostname or IP address",
      "description": "Hostname or IP address of the IBM i system.",
      "minLength": 1,
      "ui:section": "Location"
    },
    "port": {
      "type": "string",
      "title": "Port (optional)",
      "description": "DRDA port on the IBM i system. Defaults to 446 when not specified.",
      "ui:section": "Location"
    },
    "database": {
      "type": "string",
      "title": "Database name",
      "description": "IBM i relational database directory name (e.g. the system name or *LOCAL).",
      "minLength": 1,
      "ui:section": "Location"
    },
    "schema": {
      "type": "string",
      "title": "Schema / Library (optional)",
      "description": "Restrict ingestion to tables in this DB2 library or schema. Leave blank to scan all accessible schemas.",
      "ui:section": "Location"
    },
    "table_filter": {
      "type": "string",
      "title": "Table filter (optional)",
      "description": "Comma-separated table names or SQL LIKE patterns to restrict which tables are ingested (e.g. ORDERS,INVOICE%).",
      "ui:section": "Location"
    },
    "query": {
      "type": "string",
      "title": "Custom SQL query (optional)",
      "description": "Optional SELECT statement to use instead of full-table scanning. If provided, schema and table_filter are ignored.",
      "ui:section": "Location"
    },
    "ssl": {
      "type": "boolean",
      "title": "Enable TLS",
      "description": "Enable TLS for the DRDA connection. Supported on IBM i 7.3 and later. Defaults to false.",
      "ui:section": "Location"
    },
    "username": {
      "type": "string",
      "title": "Username",
      "description": "IBM i user profile used to authenticate.",
      "minLength": 1,
      "ui:section": "Authentication"
    },
    "password": {
      "type": "string",
      "title": "Password",
      "description": "IBM i user password.",
      "format": "password",
      "minLength": 1,
      "ui:section": "Authentication"
    },
    "allowed_extensions": {
      "type": "array",
      "title": "Include output formats",
      "description": "Output document formats for rows extracted from DB2 tables.",
      "ui:section": "File filters",
      "minItems": 1,
      "uniqueItems": true,
      "items": {
        "type": "string",
        "enum": [".txt", ".csv", ".json"]
      }
    }
  }
}
```

**Key schema decisions:**

- `"format": "password"` on `password` — consumed by `catalogutils.SensitiveFieldsFromSchema` to derive the encryption set at runtime. This is the only change needed vs. the `file_system` schema pattern.
- `"ui:section": "Authentication"` on `username` and `password` — consumed by `catalogutils.UpdatableFieldsFromSchema` to derive the updatable fields at runtime.
- `"ui:section": "Location"` on all structural fields — ensures they are excluded from the updatable set without any hardcoded allowlist.
- `allowed_extensions` enum values `.txt`, `.csv`, `.json` — appropriate for tabular row data extracted from DB2; PDF and DOCX are not applicable for database connectors.
- `port` is `"type": "string"` rather than integer, matching the `file_system` schema pattern (port is also a string there) so that the validator and UI can handle the optional-with-default pattern uniformly.
- `ssl` is `"type": "boolean"` — the connection tester reads this field directly from `params["ssl"].(bool)` with a safe default of `false`.

---

## 6. Adding Future Database Providers

This proposal also serves as the template for adding further database connectors (e.g. Oracle, SQL Server, PostgreSQL external, MySQL). The pattern is identical for each:

1. **Create two asset files** under `assets/connectors/datasource/<provider_id>/`.
2. **Add a provider ID constant** to `constants/catalog.go`.
3. **Implement `ConnectionTester`** in a new `datasource_service/<provider_id>.go` file. Use build tags to gate native driver dependencies the same way the IBM i DB2 provider does.
4. **Register** the tester in `NewDatasourceService` and the sync job call site.

No changes are needed to the DB schema, API routes, handlers, DTOs, encryption pipeline, or `ConnectorSyncJob` for any future provider. The schema-driven approach (`SensitiveFieldsFromSchema`, `UpdatableFieldsFromSchema`) means even the sensitive-field and updatable-field sets are derived automatically from each provider's `schema.json`.
