import asyncio
import logging
import os
import uuid
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, HTTPException, Request
from fastapi.openapi.docs import get_swagger_ui_html

import common.db_utils as db
from common.misc_utils import get_model_endpoints, set_log_level, set_request_id
from common.settings import get_settings
from chatbot.backend_utils import validate_query_length
from similarity.similarity_utils import (
    SimilaritySearchRequest,
    SimilaritySearchResponse,
    SimilaritySearchResult,
    error_responses,
    perform_similarity_search,
)

# logging
log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif "info" not in level:
        logging.warning(f"Unknown LOG_LEVEL passed: '{level}', using default INFO level")
set_log_level(log_level)



# init at startup, endpoints are read as imutable state
settings = get_settings()
vectorstore = None
emb_model_dict: dict = {}
reranker_model_dict: dict = {}


def _initialize_models():
    global emb_model_dict, reranker_model_dict
    # get_model_endpoints() also returns llm_model_dict, which we discard —
    # similarity search has no LLM dependency.
    emb_model_dict, _, reranker_model_dict = get_model_endpoints()


def _initialize_vectorstore():
    global vectorstore
    vectorstore = db.get_vector_store()


@asynccontextmanager
async def lifespan(app: FastAPI):
    _initialize_models()
    _initialize_vectorstore()
    yield

tags_metadata = [
    {
        "name": "similarity",
        "description": "Dense vector similarity search operations"
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
        "Pure dense k-NN cosine similarity search against the vector store.\n\n"
        "Unlike `/reference` and `/v1/chat/completions` (which always use hybrid "
        "search + reranking), this endpoint gives consumers direct control:\n"
        "- **`rerank: false`** (default) — fast, cosine similarity scores\n"
        "- **`rerank: true`** — Cohere reranker applied on top of dense results"
    ),
    version="1.0.0",
    openapi_tags=tags_metadata,
)


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
    responses=error_responses,
    tags=["similarity"],
    summary="Dense vector similarity search",
    description=(
        "Performs pure k-NN cosine similarity search (no BM25, no hybrid).\n\n"
        "| `rerank` | Score type | Latency |\n"
        "|----------|------------|---------|\n"
        "| `false` (default) | Cosine similarity (0-1) | Low |\n"
        "| `true` | Cohere relevance score | Medium |\n\n"
        "**`top_k`** defaults to `num_chunks_post_search` from settings (currently "
        f"{settings.num_chunks_post_search}) if not provided."
    ),
    response_description="Documents ranked by descending score, with score_type indicating the scoring method used."
)
async def similarity_search(req: SimilaritySearchRequest) -> SimilaritySearchResponse:
    if not req.query or not req.query.strip():
        raise HTTPException(status_code=400, detail="query is required")

    try:
        emb_model = emb_model_dict["emb_model"]
        emb_endpoint = emb_model_dict["emb_endpoint"]
        emb_max_tokens = emb_model_dict["max_tokens"]

        # reuses the same token-length guard as /reference and /v1/chat/completions.
        # keeps the query-too-long behaviour consistent across all retrieval endpoints rather than each one inventing its own limit.
        is_valid, error_msg = await asyncio.to_thread(
            validate_query_length, req.query, emb_endpoint
        )
        if not is_valid:
            raise HTTPException(status_code=400, detail=error_msg)

        top_k = req.top_k if req.top_k is not None else settings.num_chunks_post_search

        # reranker config when the caller actually asked for it.
        # avoids a KeyError if RERANKER_ENDPOINT / RERANKER_MODEL env vars are not set in a deployment that doesn't need reranking.
        reranker_model = reranker_model_dict.get("reranker_model") if req.rerank else None
        reranker_endpoint = reranker_model_dict.get("reranker_endpoint") if req.rerank else None

        docs, scores, score_type = await asyncio.to_thread(
            perform_similarity_search,
            req.query,
            emb_model,
            emb_endpoint,
            emb_max_tokens,
            vectorstore,
            top_k,
            req.rerank,
            reranker_model,
            reranker_endpoint,
        )

    except db.VectorStoreNotReadyError:
        raise HTTPException(status_code=503, detail="Index is empty. Ingest documents first.")
    except Exception as e:
        raise HTTPException(status_code=500, detail=repr(e))

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

    return SimilaritySearchResponse(score_type=score_type, results=results)


@app.get(
    "/health",
    tags=["monitoring"],
    summary="Health check",
    description="Returns 200 when the service is running.",
)
async def health():
    return {"status": "ok"}


@app.get(
    "/db-status",
    tags=["monitoring"],
    summary="Vector DB status",
    description="Check whether the vector store is initialised and populated.",
)
async def db_status():
    try:
        if vectorstore is None:
            return {"ready": False, "message": "Vector store not initialized"}
        status = await asyncio.to_thread(vectorstore.check_db_populated)
        if status:
            return {"ready": True}
        return {"ready": False, "message": "No data ingested"}
    except Exception as e:
        return {"ready": False, "message": str(e)}


if __name__ == "__main__":
    port = int(os.getenv("PORT", "7000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
