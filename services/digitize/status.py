"""
Utility functions for status management.

Provides common utilities used across the digitize service.
"""

from datetime import datetime, timezone
from typing import Optional

from common.misc_utils import get_logger

logger = get_logger("status")


def get_utc_timestamp() -> str:
    """
    Generate UTC timestamp in ISO format with 'Z' suffix.
    
    Returns:
        ISO 8601 formatted timestamp string with 'Z' suffix
    """
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def get_job_document_stats(job_id: str) -> dict:
    """
    Get statistics about documents in a job by reading from the database.

    Args:
        job_id: Unique identifier for the job

    Returns:
        Dictionary containing:
        - failed_docs: List of failed document objects with id, name, status
        - completed_docs: List of completed document objects with id, name, status
        - total_docs: Total number of documents
        - failed_count: Number of failed documents
        - completed_count: Number of completed documents
    """
    from digitize.digitize_utils import get_job
    from digitize.models import DocStatus

    try:
        job_data = get_job(job_id)
        
        if job_data is None:
            error_msg = f"Job not found in database: {job_id}"
            logger.error(error_msg)
            raise FileNotFoundError(error_msg)

        documents = job_data.get("documents", [])
        failed_docs = [doc for doc in documents if doc.get("status") == DocStatus.FAILED.value]
        completed_docs = [doc for doc in documents if doc.get("status") == DocStatus.COMPLETED.value]

        return {
            "failed_docs": failed_docs,
            "completed_docs": completed_docs,
            "total_docs": len(documents),
            "failed_count": len(failed_docs),
            "completed_count": len(completed_docs)
        }
    except Exception as e:
        logger.error(f"Error reading job {job_id} from database: {e}", exc_info=True)
        raise

# Made with Bob
