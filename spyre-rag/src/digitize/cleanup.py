import common.db_utils as db
from common.misc_utils import get_logger
from digitize.digitize_utils import bulk_delete_all_documents

logger = get_logger("cleanup")

def reset_db():
    """
    Reset the vector database and clean up all document files.

    This function performs a complete cleanup:
    1. Resets the vector database index (removes all indexed chunks)
    2. Deletes all digitized content files from /var/cache/digitized
    3. Deletes all document metadata files from /var/cache/docs

    Used by the CLI clean-db command.

    Raises:
        Exception: If vector database reset fails or file deletion fails completely
    """
    # Step 1: Reset vector database FIRST
    # This ensures documents are removed from search even if file deletion fails
    try:
        vector_store = db.get_vector_store()
        vector_store.reset_index()
        logger.info("✓ Vector database index reset successfully")
    except Exception as e:
        error_msg = f"Failed to reset vector database: {str(e)}"
        logger.error(f"✗ {error_msg}")
        # Raise error immediately - VDB reset is critical
        raise Exception(error_msg) from e

    # Step 2: Delete all document files LAST
    try:
        logger.debug("Deleting all document files...")
        deletion_stats = bulk_delete_all_documents()

        total_deleted = deletion_stats["metadata_files_deleted"] + deletion_stats["content_files_deleted"]
        logger.info(
            f"✓ Deleted {total_deleted} files "
            f"({deletion_stats['metadata_files_deleted']} metadata, "
            f"{deletion_stats['content_files_deleted']} content)"
        )

        # If there were any file deletion errors, raise an error
        if deletion_stats["errors"]:
            error_summary = "; ".join(deletion_stats["errors"][:3])  # Limit to first 3 errors
            logger.error(f"File deletion completed with errors: {error_summary}")
            raise Exception(
                f"Partial deletion: vector database reset but some files failed to delete. {error_summary}"
            )

    except Exception as e:
        error_msg = f"Failed to delete document files: {str(e)}"
        logger.error(f"✗ {error_msg}")
        # VDB was reset but file deletion failed completely
        raise Exception(
            f"Partial deletion: vector database reset but file deletion failed. {error_msg}"
        ) from e

    # Success - both VDB and files deleted without errors
    logger.info("✅ DB cleanup completed successfully")
    return deletion_stats