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
        """
        Inserts document chunks and their corresponding embeddings into the vector database.

        This method is flexible to handles two workflows:
        1. If 'vectors' is provided, it uses those pre-computed embeddings directly.
        2. If 'vectors' is None but an 'embedding' instance is provided, it uses that
           instance to generate embeddings from the 'page_content' within the chunks.

        Args:
            chunks: A list of dictionaries containing text content and metadata.
            vectors: A list of pre-computed vector arrays.
            embedding: An instance of the Embedding class to generate vectors.
            batch_size: Number of documents to process in a single bulk operation.
        """
        pass

    @abstractmethod
    def search(
        self,
        query_text: str,
        vector: Optional[List[float]] = None,
        embedding: Optional[Any] = None,
        top_k: int = 5,
        mode: Optional[str] = ""
    ) -> List[Dict]:
        """
        Retrieves the top-k most relevant documents from the vector database.

        This method supports multiple search modes (Dense, Sparse, or Hybrid):
        1. If 'vector' is provided, it performs a direct k-NN similarity search.
        2. If 'vector' is None but an 'embedding' instance is provided, it first
           vectorizes the 'query_text' before performing the search.
        3. If using Hybrid mode, 'query_text' is also used for keyword (BM25) matching.

        Args:
            query_text: The natural language query string from the user.
            vector: A pre-computed query vector.
            embedding: An instance of the Embedding class to vectorize the query.
            top_k: The number of similar documents to return.

        Returns:
            List[Dict]: A list of the most relevant document sources and metadata.
        """
        pass

    @abstractmethod
    def reset_index(self):
        """
        Deletes the existing index/collection and clears any associated local cache.

        This is typically used during development or when re-indexing a completely
        new set of documents. It should remove the index from the database and
        cleanup any persistent storage artifacts.
        """
        pass

class VectorStoreNotReadyError():
    """Raised when the database is unreachable or initializing."""
    pass
