from common.vector_db import VectorStore, VectorStoreNotReadyError
from common.config import VECTOR_STORE_TYPE

def get_vector_store() -> VectorStore:
    """
    Factory method to initialize the configured Vector Store.
    Controlled by the VECTOR_STORE_TYPE environment variable.
    """
    v_store_type = VECTOR_STORE_TYPE.upper()
    
    if v_store_type == "OPENSEARCH":
        from common.opensearch import OpensearchVectorStore
        return OpensearchVectorStore()
    else:
        raise VectorStoreNotReadyError(f"Unsupported VectorStore type: {v_store_type}")
