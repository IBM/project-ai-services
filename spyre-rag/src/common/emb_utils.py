import json
import requests
import numpy as np
from common.misc_utils import get_logger

logger = get_logger("embed")

from common.misc_utils import get_logger

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

    def _post_embedding(self, texts):
        try:
            payload = {
                "input": texts,
                "model": self.emb_model,
                "truncate_prompt_tokens": self.max_tokens-1,
            }
            headers = {
                "accept": "application/json",
                "Content-type": "application/json"
            }
            response = requests.post(
                f"{self.emb_endpoint}/v1/embeddings",
                data=json.dumps(payload),
                headers=headers
            )
            response.raise_for_status()
            r = response.json()
            embeddings = [data['embedding'] for data in r['data']]
            return [np.array(embed, dtype=np.float32) for embed in embeddings]
        except requests.exceptions.RequestException as e:
            error_details = str(e)
            if e.response is not None:
                error_details += f", Response Text: {e.response.text}"
            logger.error(f"Error calling embedding API: {error_details}")
            raise e
        except Exception as e:
            logger.error(f"Error calling embedding API: {e}")
            raise e

def get_embedder(emb_model, emb_endpoint, max_tokens) -> Embedding:
    """
    Returns an instance of the Embedding class.
    """
    global _embedder_instance
    if _embedder_instance is None:
        _embedder_instance = Embedding(emb_model, emb_endpoint, max_tokens)
    return _embedder_instance
