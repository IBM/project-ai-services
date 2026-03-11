import common.db_utils as db
from common.misc_utils import get_logger
from digitize.digitize_utils import cleanup_digitized_files

logger = get_logger("cleanup")

def reset_db():
    """
    Reset the vector database and clean up all digitized content files.

    This function performs a complete cleanup:
    1. Resets the vector database index (removes all indexed chunks)
    2. Deletes all digitized content files from /var/cache/digitized

    Used by the CLI clean-db command.
    """
    # Step 1: Reset vector database
    vector_store = db.get_vector_store()
    vector_store.reset_index()
    logger.info("✅ Vector database index reset successfully!")

    # Step 2: Clean up digitized files
    cleanup_stats = cleanup_digitized_files()

    # Log summary
    if cleanup_stats["errors"]:
        logger.warning(
            f"⚠️  DB cleanup completed with warnings: "
            f"{cleanup_stats['content_files_deleted']} files deleted, "
            f"{len(cleanup_stats['errors'])} errors occurred"
        )
    else:
        logger.info(
            f"✅ DB cleanup completed successfully! "
            f"Removed {cleanup_stats['content_files_deleted']} digitized files"
        )
