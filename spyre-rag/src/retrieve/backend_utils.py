from common.misc_utils import get_logger
from common.settings import get_settings
from retrieve.reranker_utils import rerank_documents
from retrieve.retrieval_utils import retrieve_documents

logger = get_logger("backend_utils")
settings = get_settings()

def search_only(question, emb_model, emb_endpoint, max_tokens, reranker_model, reranker_endpoint, top_k, top_r, use_reranker, vectorstore):
    # Perform retrieval

    retrieved_documents, retrieved_scores = retrieve_documents(question, emb_model, emb_endpoint, max_tokens,
                                                               vectorstore, top_k, 'hybrid')

    if use_reranker:
        reranked = rerank_documents(question, retrieved_documents, reranker_model, reranker_endpoint)
        ranked_documents = []
        ranked_scores = []
        for i, (doc, score) in enumerate(reranked, 1):
            ranked_documents.append(doc)
            ranked_scores.append(score)
            if i == top_r:
                break
    else:
        ranked_documents = retrieved_documents[:top_r]
        ranked_scores = retrieved_scores[:top_r]

    logger.info(f"Ranked documents: {ranked_documents}")
    logger.info(f"Ranked scores:    {ranked_scores}")
    logger.info(f"Score threshold:  {settings.score_threshold}")

    filtered_docs = []
    for doc, score in zip(ranked_documents, ranked_scores):
        if score >= settings.score_threshold:
            filtered_docs.append(doc)

    return filtered_docs
