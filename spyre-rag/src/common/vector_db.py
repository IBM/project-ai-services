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
    ) -> bool:
        """
        Inserts document chunks and their corresponding embeddings into the vector database.

        This method is flexible to handles two workflows:
        1. If 'vectors' is provided, it uses those pre-computed embeddings directly.
        2. If 'vectors' is None but an 'embedding' instance is provided, it uses that
           instance to generate embeddings from the 'page_content' within the chunks.

        Args:
            chunks: A list of dictionaries containing text content and metadata for a single document.
            vectors: A list of pre-computed vector arrays.
            embedding: An instance of the Embedding class to generate vectors.
            batch_size: Number of chunks to process in a single bulk operation.

        Returns:
            bool: True if indexing succeeded, False if it failed
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
    def remove_docs_from_index(self, doc_ids: list[str]) -> int:
        """
        Delete all chunks associated with the specified list of document IDs from the index.

        This performs a targeted deletion of documents rather than wiping the entire index.
        Uses batch deletion for efficiency.

        Args:
            doc_ids: List of document IDs whose chunks should be deleted from the index

        Returns:
            Number of chunks deleted
        """
        pass

    @abstractmethod
    def check_db_populated(self) -> bool:
        """
        Check if the vector database is populated with data.

        Returns:
            bool: True if the database contains indexed documents, False otherwise.
        """
        pass

    @abstractmethod
    def delete_document_by_id(self, doc_id: str) -> int:
        """
        Delete all chunks associated with a specific document from the index.
        
        Args:
            doc_id: The unique identifier of the document to delete
            
        Returns:
            Number of chunks deleted
        """
        pass

class VectorStoreNotReadyError(Exception):
    """Raised when the database is unreachable or initializing."""
    pass
