from common.db_utils import MilvusVectorStore
from common.misc_utils import get_logger

logger = get_logger("cleanup")

def reset_db():
    vector_store = MilvusVectorStore()
    vector_store.reset_collection()
    logger.info(f"✅ DB Cleaned successfully!")
