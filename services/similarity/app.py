import asyncio
import logging
import os
import uuid
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, HTTPException, Request, Response
from fastapi.openapi.docs import get_swagger_ui_html

from common.misc_utils import set_log_level

# Set log level before importing application modules so their loggers inherit the level
log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif "info" not in level:
        logging.warning(f"Unknown LOG_LEVEL passed: '{level}', using default INFO level")
set_log_level(log_level)

import common.db_utils as db
from common.misc_utils import get_embedding_endpoint, get_reranker_endpoint, set_request_id, create_llm_session
from common.error_utils import APIError, ErrorCode, http_error_responses, http_exception_handler
from common.validation_utils import validate_query_length as _validate_query_length
from common.retry_utils import retry_on_transient_error
from similarity.settings import settings
from similarity.similarity_utils import (
    SimilaritySearchRequest,
    SimilaritySearchResponse,
    SimilaritySearchResult,
    perform_similarity_search,
)

vectorstore = None
emb_model_dict: dict = {}
reranker_model_dict: dict = {}


def _initialize_models():
    global emb_model_dict, reranker_model_dict
    emb_model_dict = get_embedding_endpoint()
    reranker_model_dict = get_reranker_endpoint()


@retry_on_transient_error(max_retries=5, initial_delay=2.0, backoff_multiplier=2.0, max_delay=30.0)
def _initialize_vectorstore():
    """
    Initialize the vector store with retry logic for OpenSearch connection.
    Uses the common retry mechanism to handle cases where OpenSearch is not ready during service startup.
    Retries up to 5 times with exponential backoff (2s, 4s, 8s, 16s, 30s).
    """
    global vectorstore
    vectorstore = db.get_vector_store()
    logging.info("Successfully initialized vector store connection")


@asynccontextmanager
async def lifespan(app: FastAPI):
    create_llm_session(pool_maxsize=10)
    _initialize_models()
    await asyncio.to_thread(_initialize_vectorstore)
    yield

tags_metadata = [
    {
        "name": "similarity",
        "description": "Vector similarity search operations"
    },
    {
        "name": "monitoring",
        "description": "Health checks and service status"
    }
]

app = FastAPI(
    lifespan=lifespan,
    title="AI-Services Similarity Search API",
    description=(
        "Vector similarity search against the vector store with optional reranking."
    ),
    version="1.0.0",
    openapi_tags=tags_metadata,
)

app.add_exception_handler(HTTPException, http_exception_handler)


@app.middleware("http")
async def add_request_id(request: Request, call_next):
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4()))
    set_request_id(request_id)
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    return response


@app.get("/", include_in_schema=False)
def swagger_root():
    return get_swagger_ui_html(
        openapi_url="/openapi.json",
        title="AI-Services Similarity Search API - Swagger UI",
    )


@app.post(
    "/v1/similarity-search",
    response_model=SimilaritySearchResponse,
    responses={400: http_error_responses[400], 500: http_error_responses[500], 503: http_error_responses[503]},
    tags=["similarity"],
    summary="Vector similarity search",
    description=(
        "Performs vector similarity search against the vector store.\n\n"
        "| `mode` | Score type |\n"
        "|--------|------------|\n"
        "| `dense` (default) | Cosine similarity (0-1) |\n"
        "| `sparse` | BM25 |\n"
        "| `hybrid` | Hybrid |\n\n"
        "| `rerank` | Latency |\n"
        "|----------|----------|\n"
        "| `false` (default) | Low |\n"
        "| `true` | Medium |\n\n"
        "**`top_k`** defaults to `NUM_CHUNKS_POST_SEARCH` "
        f"(currently {settings.similarity.num_chunks_post_search}) if not provided.\n\n"
        "## Performance Timing Headers\n\n"
        "The response includes timing information in custom headers:\n\n"
        "- **`X-Retrieve-Time`**: Time taken for document retrieval (seconds)\n"
        "- **`X-Rerank-Time`**: Time taken for reranking (seconds, only if rerank=true)\n"
        "- **`X-Total-Time`**: Total processing time (seconds)\n\n"
        "These headers enable cross-service performance monitoring and can be used by clients "
        "to track and optimize search performance."
    ),
    response_description="Documents ranked by descending score, with score_type indicating the scoring method used. Performance metrics available in response headers."
)
async def similarity_search(req: SimilaritySearchRequest, response: Response) -> SimilaritySearchResponse:
    if not req.query or not req.query.strip():
        APIError.raise_error(ErrorCode.EMPTY_INPUT, "query is required")

    if req.mode not in ["dense", "sparse", "hybrid"]:
        APIError.raise_error(ErrorCode.INVALID_PARAMETER, "mode must be one of: dense, sparse, hybrid")
    try:
        emb_model = emb_model_dict["emb_model"]
        emb_endpoint = emb_model_dict["emb_endpoint"]
        emb_max_model_len = emb_model_dict["max_model_len"]

        # reuses the same token-length guard as /reference and /v1/chat/completions.
        # keeps the query-too-long behaviour consistent across all retrieval endpoints rather than each one inventing its own limit.
        is_valid, error_msg = await asyncio.to_thread(
            _validate_query_length, req.query, emb_endpoint, settings.similarity.max_query_token_length
        )
        if not is_valid:
            APIError.raise_error(ErrorCode.INVALID_REQUEST, error_msg)

        top_k = req.top_k

        # reranker config when the caller actually asked for it.
        # avoids a KeyError if RERANKER_ENDPOINT / RERANKER_MODEL env vars are not set in a deployment that doesn't need reranking.
        reranker_model = reranker_model_dict.get("reranker_model") if req.rerank else None
        reranker_endpoint = reranker_model_dict.get("reranker_endpoint") if req.rerank else None

        docs, scores, score_type, perf_stat_dict = await asyncio.to_thread(
            perform_similarity_search,
            req.query,
            emb_model,
            emb_endpoint,
            emb_max_model_len,
            vectorstore,
            top_k,
            req.rerank,
            req.mode,
            reranker_model,
            reranker_endpoint,
        )

    except db.VectorStoreNotReadyError:
        APIError.raise_error(ErrorCode.VECTOR_STORE_NOT_READY, "Index is empty. Ingest documents first.")
    except Exception as e:
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, repr(e))

    results = [
        SimilaritySearchResult(
            page_content=doc.get("page_content", ""),
            filename=doc.get("filename", ""),
            type=doc.get("type", ""),
            source=doc.get("source", ""),
            chunk_id=str(doc.get("chunk_id", "")),
            score=float(score),
        )
        for doc, score in zip(docs, scores)
    ]

    # Add timing information to response headers
    response.headers["X-Retrieve-Time"] = str(perf_stat_dict.get("retrieve_time", 0.0))
    if "rerank_time" in perf_stat_dict:
        response.headers["X-Rerank-Time"] = str(perf_stat_dict["rerank_time"])
    total_time = sum(v for v in perf_stat_dict.values() if v is not None)
    response.headers["X-Total-Time"] = str(total_time)

    return SimilaritySearchResponse(
        score_type=score_type,
        results=results
    )


@app.get(
    "/health",
    tags=["monitoring"],
    summary="Health check",
    description="Returns 200 when the service is running.",
)
async def health():
    return {"status": "ok"}

if __name__ == "__main__":
    port = int(os.getenv("PORT", "7000"))
    uvicorn.run(app, host="0.0.0.0", port=port)

