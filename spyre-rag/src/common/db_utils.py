import os
from common.vector_db import VectorStore, VectorStoreNotReadyError

def get_vector_store() -> VectorStore:
    """
    Factory method to initialize the configured Vector Store.
    Controlled by the VECTOR_STORE_TYPE environment variable.
    """
    v_store_type = os.getenv("VECTOR_STORE_TYPE", "OPENSEARCH").upper()
    
    if v_store_type == "OPENSEARCH":
        from common.opensearch import OpensearchVectorStore
        return OpensearchVectorStore()
    else:
        raise VectorStoreNotReadyError(f"Unsupported VectorStore type: {v_store_type}")

def get_vector_store_not_ready() -> VectorStoreNotReadyError:
    """
    Factory method to initialize the configured VectorStoreNotReadyError.
    Controlled by the VECTOR_STORE_TYPE environment variable.
    """
    v_store_type = os.getenv("VECTOR_STORE_TYPE", "OPENSEARCH").upper()

    if v_store_type == "OPENSEARCH":
        from common.opensearch import OpensearchNotReadyError
        return OpensearchNotReadyError()
    else:
        raise VectorStoreNotReadyError(f"Unsupported VectorStore type: {v_store_type}")