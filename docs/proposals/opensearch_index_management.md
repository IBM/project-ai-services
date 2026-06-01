# Proposal: Soft Deletes with Alias-Based Reindexing

## Problem

Document deletion uses OpenSearch's `delete_by_query`, which creates Lucene tombstones rather than truly removing data. These tombstones accumulate over time and degrade search performance, increase disk usage, and cause unpredictable merge pressure. We have no way to control when cleanup happens.

## Proposed Solution

Three changes that work together:

1. **Soft deletes in OpenSearch** -- Flag chunks as deleted instead of using `delete_by_query`. Search filters exclude flagged chunks automatically. Filesystem files are still hard-deleted immediately (no behavior change there).

2. **Alias-based index management** -- All reads/writes go through an OpenSearch alias instead of targeting a concrete index. This enables zero-downtime index swaps.

3. **API-triggered reindex** -- A new endpoint creates a clean index (without soft-deleted chunks), atomically swaps the alias, and drops the old index. This is how tombstones are actually reclaimed.

```
Reindex cycle:

  rag_{hash}_v{N}  --[copy non-deleted docs]--> rag_{hash}_v{N+1}
       |                                               |
       +------------ alias swap (atomic) ------------>+
       |
       +------------ drop old index
```

## Design

### Index Naming

The existing hash-based index name (`rag_{hash}`) becomes an alias. Backing indexes are versioned:

| Concept | Current | Proposed |
|---------|---------|----------|
| Concrete index | `rag_{hash}` | `rag_{hash}_v{N}` |
| Alias | _(none)_ | `rag_{hash}` |
| Read/write target | Concrete index | Alias |

### Mapping Changes

Two fields added to `metadata.properties`:

```json
"deleted":    { "type": "boolean" },
"deleted_at": { "type": "date" }
```

`deleted` drives the search filter. `deleted_at` enables replay of deletes that happen during a reindex window.

### Soft Delete

`delete_by_query` is replaced with `update_by_query` using a Painless script:

```python
update_query = {
    "script": {
        "source": "ctx._source.metadata.deleted = true; ctx._source.metadata.deleted_at = params.now",
        "lang": "painless",
        "params": {"now": "<utc_iso>"}
    },
    "query": {"term": {"metadata.doc_id": doc_id}}
}
```

All search modes (dense, sparse, hybrid) add a filter to exclude soft-deleted chunks:

```python
"must_not": [{"term": {"metadata.deleted": true}}]
```

**Filesystem behavior is unchanged** -- content and metadata files are still deleted immediately on every delete request. The soft-delete flag only exists in OpenSearch to avoid tombstone accumulation.

### Reindex Process

Triggered via `POST /v1/admin/reindex`, runs as a background task:

1. Record `reindex_start_time`
2. Create `rag_{hash}_v{N+1}` with same mapping as current index
3. `_reindex` OpenSearch API: copy only non-deleted docs from `v{N}` to `v{N+1}` (runs server-side)
4. Replay any soft-deletes with `deleted_at > reindex_start_time` to new index
5. Atomic alias swap (single `_aliases` API call)
6. Delete old `v{N}` index (reclaims disk)
7. Clean up any orphaned `v*` indexes not pointed to by the alias

### Bulk Reset

`DELETE /v1/documents` drops the backing index entirely and creates a fresh `_v1`. No soft-delete overhead for a full reset.

### Concurrency

| Scenario | Handling |
|----------|----------|
| Reindex during active ingestion | Rejected (checks active jobs + holds ingestion semaphore) |
| Soft-deletes during reindex | Replayed via `deleted_at` timestamp comparison |
| Large index | `wait_for_completion=false` + task polling |
| Long-lived service instances | Alias resolves transparently -- no restart needed |

## API Changes

### New

- **`POST /v1/admin/reindex`** -- Triggers reindex. Returns `202 Accepted`. Rejects `409` if ingestion is active.
- **`GET /v1/admin/index-info`** _(optional)_ -- Returns alias, backing index, version, doc count, soft-deleted count.

### Unchanged contracts

- `DELETE /v1/documents/{doc_id}` -- Same request/response. Internally uses soft-delete in OpenSearch instead of `delete_by_query`.
- `DELETE /v1/documents` -- Same request/response. Internally drops and recreates the index.

## Migration

Automatic and one-time on first startup:

1. If legacy index `rag_{hash}` exists (no alias):
   - Create `rag_{hash}_v1` with updated mapping
   - `_reindex` all docs from old index to `v1`
   - Delete old index, create alias pointing to `v1`
2. If nothing exists: starts fresh on first insert

Sub-second search interruption during the swap. If migration fails partway, old index still exists and retries on next startup.

## Files to Modify

| File | Scope |
|------|-------|
| `common/opensearch.py` | Major -- alias management, soft-delete, reindex, search filters, migration |
| `common/vector_db.py` | Minor -- add `reindex()` and `reset_index()` abstract methods |
| `digitize/app.py` | Minor -- add reindex and index-info endpoints |
| `digitize/cleanup.py` | Minor -- use `reset_index()` for bulk reset |
| `digitize/types.py` | Minor -- add `DELETED` to `DocStatus` enum |
