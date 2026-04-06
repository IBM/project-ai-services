import json
import numpy as np
from common.misc_utils import get_logger, SESSION
from common.retry_utils import retry_on_transient_error

logger = get_logger("Embedding")

_embedder_instance = None

class Embedding:
    def __init__(self, emb_model, emb_endpoint, max_tokens):
        self.emb_model = emb_model
        self.emb_endpoint = emb_endpoint
        self.max_tokens = int(max_tokens)

    def embed_documents(self, texts):
        return self._post_embedding(texts)

    def embed_query(self, text):
        return self._post_embedding([text])[0]

    @retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
    def _post_embedding(self, texts):
        if SESSION is None:
            raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")
        
        payload = {
            "input": texts,
            "model": self.emb_model,
            "truncate_prompt_tokens": self.max_tokens-1,
        }
        headers = {
            "accept": "application/json",
            "Content-type": "application/json"
        }
        response = SESSION.post(
            f"{self.emb_endpoint}/v1/embeddings",
            data=json.dumps(payload),
            headers=headers
        )
        response.raise_for_status()
        r = response.json()
        embeddings = [data['embedding'] for data in r['data']]
        return [np.array(embed, dtype=np.float32) for embed in embeddings]

def get_embedder(emb_model, emb_endpoint, max_tokens) -> Embedding:
    """
    Returns an instance of the Embedding class.
    """
    global _embedder_instance
    if _embedder_instance is None:
        _embedder_instance = Embedding(emb_model, emb_endpoint, max_tokens)
    return _embedder_instance
