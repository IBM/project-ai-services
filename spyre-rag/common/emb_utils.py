import json
import requests
import numpy as np
from common.misc_utils import get_logger

logger = get_logger("embed")

from common.misc_utils import get_logger

logger = get_logger("embedding")

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
            logger.error(f"Error while calling embedding API: {e}, {e.response.text}")
            raise e
        except Exception as e:
            logger.error(f"Error while calling embedding API: {e}")
            raise e
