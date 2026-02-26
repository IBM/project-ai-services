import common.db_utils as db
from common.misc_utils import get_logger

logger = get_logger("cleanup")

def reset_db():
    vector_store = db.get_vector_store()
    vector_store.reset_index()
    logger.info(f"✅ DB Cleaned successfully!")
