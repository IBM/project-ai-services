from typing import Optional

from pydantic import BaseModel, Field

from chatbot.retrieval_utils import retrieve_documents
from chatbot.reranker_utils import rerank_documents
from common.error_utils import http_error_responses



class SimilaritySearchRequest(BaseModel):
    """Request body for POST /v1/similarity-search"""
    query: str = Field(..., description="Natural language search query")
    mode: str = Field(
        default="dense",
        description="A mode parameter to select the search strategy: dense  K-NN, sparse BM25, or hybrid (both)"
    )
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



def perform_similarity_search(
    query: str,
    emb_model: str,
    emb_endpoint: str,
    emb_max_tokens: int,
    vectorstore,
    top_k: int,
    rerank: bool,
    mode:str,
    reranker_model: Optional[str] = None,
    reranker_endpoint: Optional[str] = None,
) -> tuple[list[dict], list[float], str]:
    """
    Run vector similarity search using the specified mode, with optional Cohere reranking.

    Returns:
        docs       - list of document dicts (page_content, filename, type, source, chunk_id)
        scores     - parallel list of float scores
        score_type - "cosine", "bm25", "hybrid", or "relevance" (when reranked)
    """
    docs, scores = retrieve_documents(
        query,
        emb_model,
        emb_endpoint,
        emb_max_tokens,
        vectorstore,
        top_k,
        mode=mode,
    )

    score_type_map = {
        "dense": "cosine",
        "hybrid": "hybrid",
        "sparse": "bm25"
    }
    score_type = score_type_map.get(mode, "cosine")

    if rerank:
        reranked = rerank_documents(query, docs, reranker_model, reranker_endpoint)
        docs = [d for d, _ in reranked]
        scores = [s for _, s in reranked]
        score_type = "relevance"

    return docs, scores, score_type
