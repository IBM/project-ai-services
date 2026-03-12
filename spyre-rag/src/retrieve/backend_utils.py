from common.misc_utils import get_logger
from common.settings import get_settings
from common.llm_utils import tokenize_with_llm
from retrieve.reranker_utils import rerank_documents
from retrieve.retrieval_utils import retrieve_documents
import time

logger = get_logger("backend_utils")
settings = get_settings()

def validate_query_length(query, emb_endpoint):
    
    # Validate that the query length does not exceed the maximum allowed tokens.

    try:
        tokens = tokenize_with_llm(query, emb_endpoint)
        token_count = len(tokens)
        
        if token_count > settings.max_query_token_length:
            error_msg = f"Query length ({token_count} tokens) exceeds maximum allowed length of {settings.max_query_token_length} tokens"
            logger.warning(error_msg)
            return False, error_msg
        
        return True, None
    except Exception as e:
        logger.error(f"Error validating query length: {e}")
        # If tokenization fails, we'll allow the request to proceed
        # to avoid blocking legitimate requests due to tokenization issues
        return True, None

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
    logger.debug(f"Score threshold:  {settings.score_threshold}")
    logger.info(f"Document search completed, ranked scores: {ranked_scores}")

    filtered_docs = []
    for doc, score in zip(ranked_documents, ranked_scores):
        if score >= settings.score_threshold:
            filtered_docs.append(doc)

    return filtered_docs, perf_stat_dict
