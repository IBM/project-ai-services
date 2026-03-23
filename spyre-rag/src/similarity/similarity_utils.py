"""
Utility models and core logic for the similarity search service.

Why this file exists:
- Mirrors the pattern from summarize/summ_utils.py: keep models + core logic
  separate from the FastAPI wiring in app.py.
- `app.py` stays thin (just HTTP plumbing); this file holds everything
  that could be unit-tested without starting a server.
"""

from typing import Any, Dict, Optional

from pydantic import BaseModel, Field

from chatbot.retrieval_utils import retrieve_documents
from chatbot.reranker_utils import rerank_documents



class SimilaritySearchRequest(BaseModel):
    """Request body for POST /v1/similarity-search"""
    query: str = Field(..., description="Natural language search query")
    top_k: Optional[int] = Field(
        default=None,
        description="Number of results to return. Defaults to num_chunks_post_search from settings."
    )
    rerank: bool = Field(
        default=False,
        description="When true, applies Cohere reranker to re-score and re-order results."
    )


class SimilaritySearchResult(BaseModel):
    """A single document result with its score."""
    page_content: str = Field(..., description="Text content of the chunk")
    filename: str = Field(..., description="Source filename")
    type: str = Field(..., description="Document type: text, image, or table")
    source: str = Field(..., description="Source path or HTML content")
    chunk_id: str = Field(..., description="Unique chunk identifier")
    score: float = Field(..., description="Cosine similarity (rerank=false) or relevance score (rerank=true)")


class SimilaritySearchResponse(BaseModel):
    """Response from POST /v1/similarity-search"""
    score_type: str = Field(
        ...,
        description="'cosine' for dense-only results, 'relevance' when reranked"
    )
    results: list[SimilaritySearchResult] = Field(
        ...,
        description="Documents ranked by descending score"
    )

    model_config = {
        "json_schema_extra": {
            "example": {
                "score_type": "cosine",
                "results": [
                    {
                        "page_content": "To configure network settings, navigate to system preferences...",
                        "filename": "admin-guide.pdf",
                        "type": "text",
                        "source": "admin-guide.pdf",
                        "chunk_id": "8374619250",
                        "score": 0.8742
                    },
                    {
                        "page_content": "Network troubleshooting can be performed by checking connection status...",
                        "filename": "troubleshooting.pdf",
                        "type": "text",
                        "source": "troubleshooting.pdf",
                        "chunk_id": "5091837264",
                        "score": 0.7518
                    }
                ]
            }
        }
    }


class _ErrorDetail(BaseModel):
    code: str = Field(..., description="Machine-readable error code")
    message: str = Field(..., description="Human-readable error message")
    status: int = Field(..., description="HTTP status code")


class _ErrorResponse(BaseModel):
    error: _ErrorDetail


error_responses: Dict[int | str, Dict[str, Any]] = {
    400: {
        "description": "Bad request — missing or empty query",
        "model": _ErrorResponse,
        "content": {
            "application/json": {
                "example": {
                    "error": {
                        "code": "MISSING_QUERY",
                        "message": "query is required",
                        "status": 400
                    }
                }
            }
        }
    },
    503: {
        "description": "Vector store not ready — no documents ingested yet",
        "model": _ErrorResponse,
        "content": {
            "application/json": {
                "example": {
                    "error": {
                        "code": "DB_NOT_READY",
                        "message": "Index is empty. Ingest documents first.",
                        "status": 503
                    }
                }
            }
        }
    },
    500: {
        "description": "Internal server error",
        "model": _ErrorResponse,
        "content": {
            "application/json": {
                "example": {
                    "error": {
                        "code": "INTERNAL_ERROR",
                        "message": "Unexpected error during similarity search",
                        "status": 500
                    }
                }
            }
        }
    },
}


# ---------------------------------------------------------------------------
# Core logic
# ---------------------------------------------------------------------------
# Why extract perform_similarity_search() here instead of inlining in app.py?
# - Unit-testable without HTTP: call it directly with mock deps in tests.
# - app.py stays focused on HTTP concerns (auth headers, status codes, request
#   parsing). Business logic stays here.
# - Same reason summ_utils.py has build_messages() / compute_target_and_max_tokens()
#   instead of inlining them in handle_summarize().

def perform_similarity_search(
    query: str,
    emb_model: str,
    emb_endpoint: str,
    emb_max_tokens: int,
    vectorstore,
    top_k: int,
    rerank: bool,
    reranker_model: Optional[str] = None,
    reranker_endpoint: Optional[str] = None,
) -> tuple[list[dict], list[float], str]:
    """
    Run dense k-NN similarity search, with optional Cohere reranking.

    Returns:
        docs       - list of document dicts (page_content, filename, type, source, chunk_id)
        scores     - parallel list of float scores
        score_type - "cosine" or "relevance"

    Why mode="dense" only?
    - Dense search returns cosine similarity scores (0–1), which are meaningful
      on their own without an LLM or keyword pass.
    - Hybrid mixes in BM25 scores via RRF fusion, producing scores that don't
      map cleanly to cosine similarity — wrong for this endpoint's contract.
    """
    docs, scores = retrieve_documents(
        query,
        emb_model,
        emb_endpoint,
        emb_max_tokens,
        vectorstore,
        top_k,
        mode="dense",
    )

    score_type = "cosine"

    if rerank:
        # Reranking re-scores by query relevance replacing cosine similarity
        reranked = rerank_documents(query, docs, reranker_model, reranker_endpoint)
        docs = [d for d, _ in reranked]
        scores = [s for _, s in reranked]
        score_type = "relevance"

    return docs, scores, score_type
