import time

from common.misc_utils import get_logger
from common.reranker_utils import rerank_documents
from common.retrieval_utils import retrieve_documents
from common.validation_utils import validate_query_length as _validate_query_length
from chatbot.settings import settings

logger = get_logger("backend_utils")


def validate_query_length(query, emb_endpoint):
    return _validate_query_length(query, emb_endpoint, settings.chatbot.max_query_token_length)

def search_only(question, emb_model, emb_endpoint, max_tokens, reranker_model, reranker_endpoint, top_k, top_r, vectorstore):
    # Perform retrieval
    perf_stat_dict = {}

    start_time = time.time()
    retrieved_documents, retrieved_scores = retrieve_documents(question, emb_model, emb_endpoint, max_tokens,
                                                               vectorstore, top_k, 'hybrid')
    perf_stat_dict["retrieve_time"] = time.time() - start_time

    start_time = time.time()
    reranked = rerank_documents(question, retrieved_documents, reranker_model, reranker_endpoint)
    perf_stat_dict["rerank_time"] = time.time() - start_time
    
    ranked_documents = []
    ranked_scores = []
    for i, (doc, score) in enumerate(reranked, 1):
        ranked_documents.append(doc)
        ranked_scores.append(score)
        if i == top_r:
            break

    logger.debug(f"Ranked documents: {ranked_documents}")
    logger.debug(f"Score threshold:  {settings.chatbot.score_threshold}")
    logger.info(f"Document search completed, ranked scores: {ranked_scores}")

    filtered_docs = []
    for doc, score in zip(ranked_documents, ranked_scores):
        if score >= settings.chatbot.score_threshold:
            filtered_docs.append(doc)

    return filtered_docs, perf_stat_dict
