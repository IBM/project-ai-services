# Proposal: Similarity Search REST Endpoint

## Problem Statement

The RAG backend currently exposes document retrieval only through `/reference` and `/v1/chat/completions`. Both endpoints always use **hybrid** search (dense k-NN + BM25 keyword matching) followed by **reranking**. There is no way to perform a direct vector similarity search via the API, nor is there any consumer control over whether reranking is applied.

The underlying infrastructure already supports pure dense k-NN cosine similarity search (`OpensearchVectorStore.search()` with `mode="dense"`) and a standalone reranking utility (`rerank_documents()`), but neither capability is independently exposed to consumers.

## Proposed Solution

Add a new `POST /v1/similarity-search` endpoint to the backend server that performs configurable similarity search, returning scored documents directly. The endpoint supports a `mode` parameter (default `"dense"`) to select the search strategy: dense k-NN, sparse BM25 keyword matching, or hybrid (both). An optional `rerank` parameter (default `false`) applies Cohere-based reranking on the results, giving consumers control over the search strategy and relevance/latency tradeoff.

## Architecture

### Request Flow

```
Client
  |
  v
POST /v1/similarity-search
  |
  v
retrieve_documents(query, mode=<user-specified>)
  |
  v
OpensearchVectorStore.search(mode=<user-specified>)
  |
  v
OpenSearch (dense k-NN / sparse BM25 / hybrid)
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
| Search mode | Hybrid (dense + BM25) | Hybrid (dense + BM25) | Configurable: dense, sparse, or hybrid |
| Reranking | Yes (always) | Yes (always) | Optional (default: No) |
| LLM generation | No | Yes | No |
| Score meaning | Reranker relevance | Reranker relevance | Varies by mode (cosine/BM25/combined) or reranker relevance |
| Latency | Medium | High | Low-Medium (varies by mode) or Medium-High (with rerank) |

### API Specification

**Endpoint:** `POST /v1/similarity-search`

**Request Body:**

```json
{
  "query": "How do I configure network settings?",
  "mode": "dense",
  "top_k": 5,
  "rerank": false
}
```

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `query` | string | Yes | - | Natural language search query |
| `mode` | string | No | `"dense"` | Search mode: `"dense"` (k-NN vector similarity), `"sparse"` (BM25 keyword matching), or `"hybrid"` (both combined) |
| `top_k` | integer | No | `num_chunks_post_search` (from settings, default 10) | Number of results to return |
| `rerank` | boolean | No | `false` | When `true`, applies Cohere reranker to re-score and re-order results |

**Success Response (200) — default (`mode: "dense"`, `rerank: false`):**

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
| 400 | Invalid `mode` value | `{"error": "mode must be one of: dense, sparse, hybrid"}` |
| 503 | No documents ingested / DB not ready | `{"error": "Index is empty. Ingest documents first."}` |
| 500 | Unexpected error | `{"error": "<error details>"}` |

## Implementation Details

### Components Reused (no new code needed)

- **`retrieve_documents()`** (`spyre-rag/src/retrieve/retrieval_utils.py`) — already accepts a `mode` parameter; supports `"dense"`, `"sparse"`, and `"hybrid"` modes
- **`OpensearchVectorStore.search()`** (`spyre-rag/src/common/opensearch.py`) — supports all three modes: `"dense"` (k-NN), `"sparse"` (BM25), `"hybrid"` (combined)
- **`get_embedder()`** (`spyre-rag/src/common/emb_utils.py`) — generates query embeddings using the configured embedding model
- **`rerank_documents()`** (`spyre-rag/src/retrieve/reranker_utils.py`) — Cohere-based parallel reranking (up to 8 threads), already used by `/reference` and `/v1/chat/completions`

### File Modified

**`spyre-rag/src/retrieve/backend_server.py`** — add the new route handler. When `rerank=true`, the handler calls `rerank_documents()` using the already-initialized `reranker_model_dict` from server startup

### How Search Modes Work in This System

#### Dense Mode (default)
1. The query text is converted to a vector using the embedding model (`granite-embedding-278m-multilingual`)
2. OpenSearch performs approximate nearest neighbor search using the HNSW index configured with `cosinesimil` space type
3. Results are returned ranked by cosine similarity score (0.0 to 1.0, higher = more similar)
4. Best for semantic similarity and conceptual matching

#### Sparse Mode
1. The query text is analyzed using BM25 keyword matching
2. OpenSearch performs full-text search against the indexed document content
3. Results are returned ranked by BM25 relevance score
4. Best for exact keyword matches and term-based retrieval

#### Hybrid Mode
1. Both dense (k-NN) and sparse (BM25) searches are performed in parallel
2. Results are combined and ranked using a weighted scoring mechanism
3. Provides balanced results leveraging both semantic and keyword matching
4. Best for comprehensive retrieval across different query types

#### Reranking (optional for all modes)
- If `rerank=true`, results from any mode are passed through the Cohere reranker
- The reranker re-scores each document by query relevance and re-orders them accordingly
- Improves result quality at the cost of additional latency

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
   # Dense mode (default, cosine similarity, no reranking)
   curl -X POST http://localhost:5000/v1/similarity-search \
     -H "Content-Type: application/json" \
     -d '{"query": "your search text"}'

   # Sparse mode (BM25 keyword matching)
   curl -X POST http://localhost:5000/v1/similarity-search \
     -H "Content-Type: application/json" \
     -d '{"query": "your search text", "mode": "sparse"}'

   # Hybrid mode (dense + sparse combined)
   curl -X POST http://localhost:5000/v1/similarity-search \
     -H "Content-Type: application/json" \
     -d '{"query": "your search text", "mode": "hybrid"}'

   # With custom top_k
   curl -X POST http://localhost:5000/v1/similarity-search \
     -H "Content-Type: application/json" \
     -d '{"query": "your search text", "mode": "dense", "top_k": 3}'

   # With reranking enabled (works with any mode)
   curl -X POST http://localhost:5000/v1/similarity-search \
     -H "Content-Type: application/json" \
     -d '{"query": "your search text", "mode": "hybrid", "rerank": true}'
   ```
3. **Verify scores:**
   - `mode="dense"`, `rerank=false`: results ordered by descending cosine similarity, `score_type: "cosine"`
   - `mode="sparse"`, `rerank=false`: results ordered by descending BM25 score, `score_type: "bm25"`
   - `mode="hybrid"`, `rerank=false`: results ordered by combined score, `score_type: "hybrid"`
   - `rerank=true` (any mode): results ordered by descending reranker relevance, `score_type: "relevance"`
4. **Regression:** Ensure `/reference` and `/v1/chat/completions` behavior is unchanged

## Future Work: Replacing `/reference` Endpoint

After the `/v1/similarity-search` endpoint is implemented and validated, the `/reference` endpoint can be replaced. Since this is an internal API, the migration can be straightforward:

### Changes Required

1. **Backend API** (`spyre-rag/src/chatbot/app.py`)
   - Remove `/reference` route handler
   - Remove `ReferenceRequest` and `ReferenceResponse` models if not used elsewhere
   - Remove `search_only()` function if it's `/reference`-specific

2. **Chatbot UI Frontend** (`spyre-rag/ui/`)
   - **React Component** (`src/components/customSendMessage.jsx`):
     - Change `/reference` POST to `/v1/similarity-search`
     - Update request body from `{ prompt: userInput }` to `{ query: userInput, mode: "hybrid", rerank: true }`

   - **Nginx Config** (`nginx.conf.tmpl`):
     - Change `location /reference` block to `location /v1/similarity-search`
     - Update proxy target to use new environment variables (see deployment configuration below)

   - **Vite Dev Config** (`vite.config.js`):
     - Change `"/reference"` proxy to `"/v1/similarity-search"`

   - **Express Dev Server** (`src/server/server.js`):
     - Change route from `app.post('/reference', ...)` to `app.post('/v1/similarity-search', ...)`
     - Change target URL from `${targetURL}/reference` to `${targetURL}/v1/similarity-search`
     - Update request body from `{ prompt: prompt }` to `{ query: prompt, mode: "hybrid", rerank: true }`
     - Note: This file is only used during local development

3. **Deployment Configuration** (`ai-services/assets/applications/rag/`)

   Since the similarity-search service will be deployed separately from the chatbot backend, the UI needs new environment variables to route requests to it:

   - **Podman Template** (`podman/templates/chat-bot.yaml.tmpl`):
     - Add environment variables to the `ui` container:
       ```yaml
       - name: SIMILARITY_SEARCH_HOST
         value: "{{ .AppName }}--similarity-api"
       - name: SIMILARITY_SEARCH_PORT
         value: "5000"
       ```

   - **OpenShift Template** (`openshift/templates/ui-deployment.yaml`):
     - Add environment variables to the `ui` container:
       ```yaml
       - name: SIMILARITY_SEARCH_HOST
         value: "similarity-api"
       - name: SIMILARITY_SEARCH_PORT
         value: "5000"
       ```

   - **UI Nginx Config** (`spyre-rag/ui/nginx.conf.tmpl`):
     - Update the `/v1/similarity-search` location block to use the new variables:
       ```nginx
       location /v1/similarity-search {
         proxy_pass http://${SIMILARITY_SEARCH_HOST}:${SIMILARITY_SEARCH_PORT};
         proxy_http_version 1.1;
         proxy_set_header Upgrade $http_upgrade;
         proxy_set_header Connection 'upgrade';
         proxy_set_header Host $host;
         proxy_cache_bypass $http_upgrade;
       }
       ```

   - **UI Containerfile** (`spyre-rag/ui/Containerfile`):
     - Update the `envsubst` command to include the new variables:
       ```bash
       CMD /bin/bash -c "envsubst '\$BACKEND_HOST \$BACKEND_PORT \$SIMILARITY_SEARCH_HOST \$SIMILARITY_SEARCH_PORT' < /etc/nginx.conf.tmpl > /etc/nginx/nginx.conf && nginx -g 'daemon off;'"
       ```

4. **Documentation**
   - Update API documentation and examples to reference new endpoint
   - Document search mode options and reranking behavior

### Equivalent Behavior

To replicate current `/reference` behavior, use:
```json
{
  "query": "search text",
  "mode": "hybrid",
  "rerank": true
}
```

**Note:** The request field changes from `prompt` to `query` to match the new API specification.
