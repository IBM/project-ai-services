from concurrent.futures import ThreadPoolExecutor, as_completed
from common.misc_utils import get_logger
from typing import List, Tuple
from cohere import ClientV2

logger = get_logger("reranker")

def rerank_helper(co2_client: ClientV2, query: str, document: List[dict], model: str) -> Tuple[List[dict], float]:
    """
    Rerank a single LangChain Document with respect to the query.
    Returns a (Document, score) tuple.
    """
    try:
        result = co2_client.rerank(
            model=model,
            query=query,
            documents=[document.get("page_content")],
            max_tokens_per_doc=512,
        )
        score = result.results[0].relevance_score
        return document, score
    except Exception as e:
        logger.error(f"Rerank Error {e}")
        return document, 0.0


def rerank_documents(query: str, documents: List[dict], model: str, endpoint: str, max_workers: int = 8) -> List[Tuple[dict, float]]:
    """
    Rerank LangChain Documents for a given query using vLLM-compatible Cohere API.

    Returns:
        List of (Document, score) sorted by descending score.
    """
    co2 = ClientV2(api_key="sk-fake-key", base_url=endpoint)
    reranked: List[Tuple[dict, float]] = []

    with ThreadPoolExecutor(max_workers=max(1, min(max_workers, len(documents)))) as executor:
        futures = {
            executor.submit(rerank_helper, co2, query, doc, model): doc
            for doc in documents
        }

        for future in as_completed(futures):
            doc = futures[future]
            try:
                reranked.append(future.result())
            except Exception as e:
                logger.error(f"Thread error: {e}")
                reranked.append((doc, 0.0))

    return sorted(reranked, key=lambda x: x[1], reverse=True)
