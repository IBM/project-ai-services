from common.misc_utils import get_logger
from common.validation_utils import validate_query_length as _validate_query_length
from similarity.similarity_utils import perform_similarity_search
from chatbot.settings import settings

logger = get_logger("backend_utils")


def validate_query_length(query, emb_endpoint):
    return _validate_query_length(query, emb_endpoint, settings.chatbot.max_query_token_length)


def search_only(question, emb_model, emb_endpoint, max_tokens, reranker_model, reranker_endpoint, top_k, top_r, vectorstore):
    docs, scores, _, perf_stat_dict = perform_similarity_search(
        query=question,
        emb_model=emb_model,
        emb_endpoint=emb_endpoint,
        emb_max_model_len=max_tokens,
        vectorstore=vectorstore,
        top_k=top_k,
        rerank=True,
        mode="hybrid",
        reranker_model=reranker_model,
        reranker_endpoint=reranker_endpoint,
        return_timings=True,
    )

    start_time = time.time()
    retrieved_documents, retrieved_scores = retrieve_documents(question, emb_model, emb_endpoint, max_tokens,
                                                               vectorstore, top_k, settings.chatbot.search_mode)
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

    filtered_docs = [
        doc for doc, score in zip(ranked_documents, ranked_scores)
        if score >= settings.chatbot.score_threshold
    ]

    return filtered_docs, perf_stat_dict
