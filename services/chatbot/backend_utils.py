import time
import requests
from requests.adapters import HTTPAdapter
from common.misc_utils import get_logger
from common.validation_utils import validate_query_length as _validate_query_length
from chatbot.settings import settings

logger = get_logger("backend_utils")

_similarity_session = None

def get_similarity_session():
    global _similarity_session
    if _similarity_session is None:
        _similarity_session = requests.Session()
        adapter = HTTPAdapter(pool_connections=3, pool_maxsize=settings.common.llm.max_batch_size)
        _similarity_session.mount("http://", adapter)
        _similarity_session.mount("https://", adapter)
    return _similarity_session


def validate_query_length(query, emb_endpoint):
    return _validate_query_length(query, emb_endpoint, settings.chatbot.max_query_token_length)


def search_only(question, top_k, top_r):
    """
    Perform document search by calling the similarity service API endpoint.

    Args:
        question: Search query
        top_k: Number of documents to retrieve before reranking
        top_r: Number of documents to keep after reranking

    Returns:
        tuple: (filtered_docs, perf_stat_dict)
    """
    perf_stat_dict = {}

    # Call similarity service API
    similarity_url = settings.chatbot.similarity_service_url

    try:
        response = get_similarity_session().post(
            f"{similarity_url}/v1/similarity-search",
            json={
                "query": question,
                "mode": settings.chatbot.search_mode,
                "top_k": top_k,
                "rerank": settings.chatbot.rerank
            },
            timeout=30
        )
        response.raise_for_status()

        # Extract timing information from response headers
        if "X-Retrieve-Time" in response.headers:
            perf_stat_dict["retrieve_time"] = float(response.headers["X-Retrieve-Time"])
        if "X-Rerank-Time" in response.headers:
            perf_stat_dict["rerank_time"] = float(response.headers["X-Rerank-Time"])

        logger.info(
            f"Similarity service timing - "
            f"Retrieve: {perf_stat_dict.get('retrieve_time', 0):.3f}s, "
            f"Rerank: {perf_stat_dict.get('rerank_time', 0):.3f}s"
        )

        result = response.json()
        docs = result["results"]
        scores = [doc["score"] for doc in docs]

    except requests.exceptions.RequestException as e:
        logger.error(f"Failed to call similarity service: {e}")
        raise RuntimeError(f"Similarity service unavailable: {e}")

    # Apply chatbot-specific post-processing: top-R selection
    ranked_documents = docs[:top_r]
    ranked_scores = scores[:top_r]

    logger.debug(f"Ranked documents: {ranked_documents}")
    logger.debug(f"Score threshold:  {settings.chatbot.score_threshold}")
    logger.info(f"Document search completed, ranked scores: {ranked_scores}")

    # Apply chatbot-specific score filtering
    filtered_docs = [
        doc for doc, score in zip(ranked_documents, ranked_scores)
        if score >= settings.chatbot.score_threshold
    ]

    return filtered_docs, perf_stat_dict
