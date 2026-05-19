from common.vector_db import VectorStore, VectorStoreNotReadyError
from common.settings import settings

def get_vector_store() -> VectorStore:
    """
    Factory method to initialize the configured Vector Store.
    Controlled by the vector_store_type setting.
    """
    v_store_type = settings.vector_store.vector_store_type.upper()

    if v_store_type == "OPENSEARCH":
        from common.opensearch import OpensearchVectorStore
        return OpensearchVectorStore()
    else:
        raise VectorStoreNotReadyError(f"Unsupported VectorStore type: {v_store_type}")
