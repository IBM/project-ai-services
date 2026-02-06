from abc import ABC, abstractmethod
from typing import List, Dict, Optional

class VectorStore(ABC):
    @abstractmethod
    def insert_chunks(self, emb_model: str, emb_endpoint: str, max_tokens: int, chunks: List[Dict], batch_size: int = 10):
        pass

    @abstractmethod
    def search(self, emb_model: str, emb_endpoint: str, max_tokens: int, query_text: str, top_k: int = 5, filters: Dict = None) -> List[Dict]:
        pass

    @abstractmethod
    def reset_collection(self):
        pass

class VectorStoreNotReadyError():
    """Raised when the database is unreachable or initializing."""
    pass