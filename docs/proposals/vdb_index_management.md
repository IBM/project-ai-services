# Proposal: Vector Store Index Maintenance

## Problem

`delete_by_query` (in `remove_docs_from_index` and `delete_document_by_id`) only marks
chunks as deleted. Disk is reclaimed later by background merges, so heavy delete churn
can leave the index bloated and slower to search. Operators have no way to see this or
to trigger cleanup.

## Approach

Use each backend's **native** cleanup. No change to delete behavior, index naming, or
data layout.

| | OpenSearch | Milvus |
|---|---|---|
| Stats | `_stats`: `docs.count`, `docs.deleted` | Row count and deleted rows, where exposed (else `deleted = 0`) |
| Cleanup | `_forcemerge?only_expunge_deletes=true` | `compact()` |

Two new abstract methods on `VectorStore` (`services/common/vector_db.py`) keep the
API layer backend-agnostic:

- `get_index_stats() -> {total_chunks, deleted_chunks}`
- `reindex() -> None` — reclaims space; blocks until done

Each backend implements them however is fastest on its platform.

## Endpoints (`services/digitize/api/v1/admin.py`)

**`GET /v1/admin/index-strategy`** — advisory only.

```json
{
  "total_chunks": 84200,
  "deleted_chunks": 31500,
  "deleted_ratio": 0.374,
  "recommendation": "reindex_recommended"
}
```

`recommendation`: `ok` (below threshold), `reindex_recommended` (above threshold),
`reindex_required` (above hard ceiling). Thresholds are settings.

**`POST /v1/admin/reindex`** — returns `202`, runs `reindex()` as a background task.

- `409` if any digitize job (user or connector) is active, or a reindex is running.
- Holds `reindex_lock` (PostgreSQL advisory lock, same pattern as `import_export_lock`):
  `POST /v1/jobs` returns `409` and connector sync ticks skip scheduling until it is released.
- On failure the lock is released and the index is unchanged.

## Files to Change

| File | Change |
|---|---|
| `services/common/vector_db.py` | Add `get_index_stats()`, `reindex()` |
| `services/common/opensearch.py` | Implement both |
| `services/digitize/utils/db.py` | `reindex_lock` acquire/release/is-in-progress |
| `services/digitize/api/v1/jobs.py` | Gate `create_job` on the lock |
| `services/digitize/connectors/sync_tick.py` | Skip scheduling while locked |
| `services/digitize/api/v1/admin.py` | The two endpoints |

A Milvus store does not exist yet; when added it implements the same two methods.

## Delivery

Work is split into three PRs. Each is independently mergeable and leaves the system in
a working state. They are ordered so that the read-only visibility lands first, the
safety mechanism (lock) lands second, and the destructive operation (reindex) lands last,
only once everything it depends on is in place.

### PR 1 — Index stats and `GET /v1/admin/index-strategy` (read-only)

**Goal:** give operators visibility into index bloat. Nothing in this PR modifies data
or changes existing behavior.

**Scope:**

- Add abstract `get_index_stats()` and `reindex()` to `VectorStore` in
  `services/common/vector_db.py`, defining the backend-agnostic contract. `reindex()` is
  declared here so the interface is stable, but nothing calls it yet.
- Implement both in `services/common/opensearch.py`:
  - `get_index_stats()` reads `_stats` and returns `docs.count` as `total_chunks` and
    `docs.deleted` as `deleted_chunks`.
  - `reindex()` calls `_forcemerge?only_expunge_deletes=true` and blocks until it
    completes.
- Add the configurable thresholds (`reindex_recommended` and `reindex_required`) to
  settings.
- Add `GET /v1/admin/index-strategy` in `services/digitize/api/v1/admin.py`. It calls
  `get_index_stats()`, computes `deleted_ratio`, and maps it to `ok`,
  `reindex_recommended` or `reindex_required`.

**Why first:** it is zero-risk, immediately useful (operators can see the problem), and
it fixes the `VectorStore` interface that PRs 2 and 3 build on.

**Tests:**

- Endpoint contract against a fake `VectorStore`: response shape, `deleted_ratio`
  calculation, each recommendation level at and around the thresholds, and the empty
  index case (`total_chunks == 0` must not divide by zero).
- OpenSearch integration: index documents, delete some, and assert
  `deleted_chunks > 0`.

### PR 2 — `reindex_lock` and job/connector gating (no user-visible endpoint)

**Goal:** build the mutual-exclusion mechanism that makes a reindex safe, before any
code can trigger one. Ingestion must not write to the index while it is being compacted.

**Scope:**

- In `services/digitize/utils/db.py`, add `reindex_lock` helpers: acquire, release and
  is-in-progress. This is a PostgreSQL advisory lock that follows the existing
  `import_export_lock` pattern, so it works across workers and processes and is released
  automatically if the holder's connection drops.
- In `services/digitize/api/v1/jobs.py`, make `create_job` (`POST /v1/jobs`) return
  `409` while the lock is held.
- In `services/digitize/connectors/sync_tick.py`, make the connector sync tick skip
  scheduling new jobs while the lock is held. The tick is not failed or errored; it
  retries on the next interval.

**Why separate:** the lock has no caller yet, so this PR cannot change runtime behavior
on its own. It can be reviewed and tested on its own, and PR 3 then only has to acquire
and release it.

**Tests:**

- With the lock held, `POST /v1/jobs` returns `409`. After release it succeeds again.
- With the lock held, a sync tick schedules nothing. After release it schedules as
  normal.
- Lock semantics: a second acquire fails while held, and release is idempotent.

### PR 3 — `POST /v1/admin/reindex` (the destructive operation)

**Goal:** let operators trigger cleanup safely, using the pieces from PRs 1 and 2.

**Scope:**

- Add `POST /v1/admin/reindex` in `services/digitize/api/v1/admin.py`. It returns
  `202 Accepted` immediately and runs `reindex()` as a background task.
- Pre-checks, in order, each returning `409`:
  - a reindex is already running (lock is held);
  - any digitize job, user-initiated or connector-initiated, is active.
- Acquire `reindex_lock` before starting the background task so new jobs and sync ticks
  are blocked for the full duration.
- Release the lock in a `finally` block, so it is released on both success and failure.
  On failure the index is left unchanged and the error is logged.

**Why last:** this is the only PR that changes index contents and blocks ingestion, so
it depends on the stats (PR 1) to confirm the result and on the lock (PR 2) to be safe.

**Tests:**

- `409` when a job is active, `409` when a reindex is already running, `202` otherwise.
- Lock is released after success and after a simulated `reindex()` failure.
- OpenSearch integration: after reindex `deleted_chunks == 0`, and search results for a
  fixed set of queries are identical before and after.
