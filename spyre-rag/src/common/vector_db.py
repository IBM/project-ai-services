from abc import ABC, abstractmethod
from typing import List, Dict, Any, Optional

class VectorStore(ABC):
    @abstractmethod
    def insert_chunks(
        self,
        chunks: List[Dict],
        vectors: Optional[List[List[float]]] = None,
        embedding: Optional[Any] = None,
        batch_size: int = 10
    ):
        """Supports 1. Pre-computed vectors OR 2. Chunks + Embedding instance"""
        pass

    @abstractmethod
    def search(
        self,
        query_text: str,
        vector: Optional[List[float]] = None,
        embedding: Optional[Any] = None,
        top_k: int = 5
    ) -> List[Dict]:
        """Supports 1. Pure vector search OR 2. Text query + Embedding instance"""
        pass

    @abstractmethod
    def reset_index(self):
        pass

class VectorStoreNotReadyError():
    """Raised when the database is unreachable or initializing."""
    pass
