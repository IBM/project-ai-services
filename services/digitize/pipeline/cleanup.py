"""
System reset / cleanup pipeline.

Deletes all user-submitted documents and jobs: VDB reset → PostgreSQL wipe →
filesystem cleanup.  Connector-sourced documents and connector jobs are left
untouched.
"""
import common.db_utils as db
from common.misc_utils import get_logger
from digitize.utils.storage import storage_manager
from digitize.utils.db import get_user_document_ids
from digitize.db.manager import db_manager

logger = get_logger("cleanup")


def reset_db():
    """
    Reset the vector database, PostgreSQL database, and clean up all
    user-submitted document files.

    Connector-sourced documents and connector jobs are preserved.

    This function performs the following steps:
    1. Reads user-submitted document IDs from the database
    2. Deletes their chunks from the vector database index
    3. Deletes user-submitted documents from PostgreSQL
    4. Deletes user-submitted jobs from PostgreSQL
    5. Deletes the corresponding content files from /var/cache/digitized

    Raises:
        Exception: If vector database reset fails or file deletion fails completely
    """
    # Step 1: Read user-submitted document IDs
    doc_ids = get_user_document_ids()

    # Step 2: Delete chunks from vector database FIRST
    # This ensures documents are removed from search even if file deletion fails
    try:
        vector_store = db.get_vector_store()
        if doc_ids:
            deleted_chunks = vector_store.remove_docs_from_index(doc_ids)
            logger.info(f"✓ Vector database index reset successfully: {deleted_chunks} chunks deleted")
        else:
            logger.info(msg="✓ No documents to delete from vector database")
    except Exception as e:
        error_msg = f"Failed to reset vector database: {str(e)}"
        logger.error(f"✗ {error_msg}")
        raise Exception(error_msg) from e

    # Step 3: Delete all records from PostgreSQL database
    try:
        logger.debug("Deleting user-submitted documents from PostgreSQL database...")
        doc_result = db_manager.delete_user_documents()

        if doc_result["success"]:
            logger.info(f"✓ Deleted {doc_result['deleted_count']} documents from PostgreSQL database")
        else:
            error_msg = f"Failed to delete documents from PostgreSQL: {doc_result.get('error', 'Unknown error')}"
            logger.error(f"✗ {error_msg}")
            raise Exception(error_msg)

        logger.debug("Deleting user-submitted jobs from PostgreSQL database...")
        job_result = db_manager.delete_user_jobs()

        if job_result["success"]:
            logger.info(f"✓ Deleted {job_result['deleted_count']} jobs from PostgreSQL database")
        else:
            error_msg = f"Failed to delete jobs from PostgreSQL: {job_result.get('error', 'Unknown error')}"
            logger.error(f"✗ {error_msg}")
            raise Exception(error_msg)

    except Exception as e:
        error_msg = f"Failed to delete PostgreSQL records: {str(e)}"
        logger.error(f"✗ {error_msg}")
        raise Exception(
            f"Partial deletion: vector database reset but PostgreSQL deletion failed. {error_msg}"
        ) from e

    # Step 4: Delete content files for user-submitted docs LAST
    try:
        logger.debug("Deleting document content files...")
        deletion_stats: dict = {"content_files_deleted": 0, "errors": []}
        for doc_id in doc_ids:
            try:
                storage_manager.delete_document_content_by_id(doc_id)
                deletion_stats["content_files_deleted"] += 1
            except Exception as file_exc:
                deletion_stats["errors"].append(str(file_exc))

        total_deleted = deletion_stats["content_files_deleted"]
        logger.info(
            f"✓ Deleted {total_deleted} content files from filesystem "
            f"(metadata is managed in PostgreSQL database)"
        )

        if deletion_stats["errors"]:
            error_summary = "; ".join(deletion_stats["errors"][:3])
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
