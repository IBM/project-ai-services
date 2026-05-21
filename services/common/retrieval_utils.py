from common.emb_utils import get_embedder


def retrieve_documents(query, emb_model, emb_endpoint, max_tokens, vectorstore, top_k, mode="hybrid", language='en'):
    embedding = get_embedder(emb_model, emb_endpoint, max_tokens)
    results = vectorstore.search(query, embedding=embedding, top_k=top_k, mode=mode, language=language)

    retrieved_documents = []
    scores = []

    for hit in results:
        doc = {
            "page_content": hit.get("page_content", ""),
            "filename": hit.get("filename", ""),
            "type": hit.get("type", ""),
            "source": hit.get("source", ""),
            "chunk_id": hit.get("chunk_id", "")
        }
        retrieved_documents.append(doc)

        score = hit.get("rrf_score") or hit.get("score") or hit.get("distance") or 0.0
        scores.append(score)

    return retrieved_documents, scores
