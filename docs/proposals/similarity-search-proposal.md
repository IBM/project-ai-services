# Proposal: Similarity Search REST Endpoint

## Problem Statement

The RAG backend currently exposes document retrieval only through `/reference` and `/v1/chat/completions`. Both endpoints always use **hybrid** search (dense k-NN + BM25 keyword matching) followed by **reranking**. There is no way to perform a direct vector similarity search via the API, nor is there any consumer control over whether reranking is applied.

The underlying infrastructure already supports pure dense k-NN cosine similarity search (`OpensearchVectorStore.search()` with `mode="dense"`) and a standalone reranking utility (`rerank_documents()`), but neither capability is independently exposed to consumers.

## Proposed Solution

Add a new `POST /v1/similarity-search` endpoint to the backend server that performs vector similarity search (dense k-NN), returning scored documents directly. The endpoint supports an optional `rerank` parameter (default `false`) to apply Cohere-based reranking on the dense results, giving consumers control over the relevance/latency tradeoff.

## Architecture

### Request Flow

```
Client
  |
  v
POST /v1/similarity-search
  |
  v
retrieve_documents(query, mode="dense")
  |
  v
OpensearchVectorStore.search(mode="dense")
  |
  v
OpenSearch k-NN (cosine similarity via HNSW index)
  |
  v
rerank=true? ──Yes──> rerank_documents() ──> Reranked results (relevance scores)
  |
  No
  |
  v
Cosine similarity results returned to client
```

### Comparison with Existing Endpoints

| Aspect | `/reference` | `/v1/chat/completions` | `/v1/similarity-search` (proposed) |
|---|---|---|---|
| Search mode | Hybrid (dense + BM25) | Hybrid (dense + BM25) | Dense only (k-NN) |
| Reranking | Yes (always) | Yes (always) | Optional (default: No) |
| LLM generation | No | Yes | No |
| Score meaning | Reranker relevance | Reranker relevance | Cosine similarity (default) or reranker relevance |
| Latency | Medium | High | Low (default) or Medium (with rerank) |

### API Specification

**Endpoint:** `POST /v1/similarity-search`

**Request Body:**

```json
{
  "query": "How do I configure network settings?",
  "top_k": 5,
  "rerank": false
}
```

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `query` | string | Yes | - | Natural language search query |
| `top_k` | integer | No | `num_chunks_post_search` (from settings, default 10) | Number of results to return |
| `rerank` | boolean | No | `false` | When `true`, applies Cohere reranker to re-score and re-order results |

**Success Response (200) — default (`rerank: false`):**

```json
{
  "score_type": "cosine",
  "results": [
    {
      "page_content": "To configure network settings, navigate to the system preferences and select the network adapter...",
      "filename": "admin-guide.pdf",
      "type": "text",
      "source": "admin-guide.pdf",
      "chunk_id": "8374619250",
      "score": 0.8742
    },
    {
      "page_content": "Network troubleshooting can be performed by checking the connection status and verifying DNS resolution...",
      "filename": "troubleshooting.pdf",
      "type": "text",
      "source": "troubleshooting.pdf",
      "chunk_id": "5091837264",
      "score": 0.7518
    }
  ]
}
```

**Success Response (200) — with `rerank: true`:**

```json
{
  "score_type": "relevance",
  "results": [
    {
      "page_content": "To configure network settings, navigate to the system preferences and select the network adapter...",
      "filename": "admin-guide.pdf",
      "type": "text",
      "source": "admin-guide.pdf",
      "chunk_id": "8374619250",
      "score": 0.9215
    },
    {
      "page_content": "Network troubleshooting can be performed by checking the connection status and verifying DNS resolution...",
      "filename": "troubleshooting.pdf",
      "type": "text",
      "source": "troubleshooting.pdf",
      "chunk_id": "5091837264",
      "score": 0.6803
    }
  ]
}
```

**Error Responses:**

| Status | Condition | Body |
|---|---|---|
| 400 | Missing or empty `query` | `{"error": "query is required"}` |
| 503 | No documents ingested / DB not ready | `{"error": "Index is empty. Ingest documents first."}` |
| 500 | Unexpected error | `{"error": "<error details>"}` |

## Implementation Details

### Components Reused (no new code needed)

- **`retrieve_documents()`** (`spyre-rag/src/retrieve/retrieval_utils.py`) — already accepts a `mode` parameter; passing `mode="dense"` triggers pure k-NN search
- **`OpensearchVectorStore.search()`** (`spyre-rag/src/common/opensearch.py`) — `mode="dense"` executes a k-NN query against the HNSW index with cosine similarity
- **`get_embedder()`** (`spyre-rag/src/common/emb_utils.py`) — generates query embeddings using the configured embedding model
- **`rerank_documents()`** (`spyre-rag/src/retrieve/reranker_utils.py`) — Cohere-based parallel reranking (up to 8 threads), already used by `/reference` and `/v1/chat/completions`

### File Modified

**`spyre-rag/src/retrieve/backend_server.py`** — add the new route handler. When `rerank=true`, the handler calls `rerank_documents()` using the already-initialized `reranker_model_dict` from server startup

### How k-NN Similarity Search Works in This System

1. The query text is converted to a vector using the embedding model (`granite-embedding-278m-multilingual`)
2. OpenSearch performs approximate nearest neighbor search using the HNSW index configured with `cosinesimil` space type
3. Results are returned ranked by cosine similarity score (0.0 to 1.0, higher = more similar)
4. By default, no reranking or keyword matching is applied — results reflect pure semantic similarity
5. If `rerank=true`, results are passed through the Cohere reranker which re-scores each document by query relevance and re-orders them accordingly

### Index Configuration (already in place)

```yaml
embedding:
  type: knn_vector
  method:
    name: hnsw
    space_type: cosinesimil
    engine: lucene
    parameters:
      ef_construction: 128
      m: 24
```

## Verification Plan

1. **Unit test:** Ensure the endpoint returns 400 for missing query, 503 when DB is empty, and valid results when documents are ingested
2. **Integration test:**
   ```bash
   # Basic search (cosine similarity, no reranking)
   curl -X POST http://localhost:5000/v1/similarity-search \
     -H "Content-Type: application/json" \
     -d '{"query": "your search text"}'

   # With custom top_k
   curl -X POST http://localhost:5000/v1/similarity-search \
     -H "Content-Type: application/json" \
     -d '{"query": "your search text", "top_k": 3}'

   # With reranking enabled
   curl -X POST http://localhost:5000/v1/similarity-search \
     -H "Content-Type: application/json" \
     -d '{"query": "your search text", "rerank": true}'
   ```
3. **Verify scores:**
   - `rerank=false` (default): results ordered by descending cosine similarity, `score_type: "cosine"`
   - `rerank=true`: results ordered by descending reranker relevance, `score_type: "relevance"` (order may differ from non-reranked)
4. **Regression:** Ensure `/reference` and `/v1/chat/completions` behavior is unchanged
