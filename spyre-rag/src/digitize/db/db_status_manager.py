"""
Database-only status manager.

Uses PostgreSQL database as the single source of truth for job and document status.
"""

from datetime import datetime, timezone
from typing import Dict, Any, Mapping, Optional
from pathlib import Path

from common.misc_utils import get_logger
from digitize.status import StatusManager, get_utc_timestamp
from digitize.types import JobStatus, DocStatus
from digitize.db.repository import db_repo
from digitize.db.database import engine
from digitize.settings import settings

logger = get_logger("db_status_manager")


class DatabaseStatusManager(StatusManager):
    """
    Database-only StatusManager that persists to PostgreSQL database.
    
    - Storage: PostgreSQL database only (required)
    - Raises error if database unavailable
    """
    
    def __init__(self, job_id: str):
        """
        Initialize database-first status manager.
        
        Args:
            job_id: Unique identifier for the job
            
        Raises:
            RuntimeError: If database is not available
        """
        super().__init__(job_id)
        
        if engine is None:
            raise RuntimeError(f"Database not available for job {job_id}. Cannot proceed without database.")
        
        self.db_enabled = True
    
    def update_doc_metadata(
        self,
        doc_id: str,
        details: Mapping[str, Any],
        error: str = ""
    ) -> None:
        """
        Update document metadata in database.
        
        Args:
            doc_id: Document identifier
            details: Dictionary of fields to update
            error: Optional error message
        """
        try:
            self._update_document_in_db(doc_id, details, error)
        except Exception as e:
            logger.error(f"Failed to update document {doc_id} in database: {e}", exc_info=True)
            raise
    
    def update_job_progress(
        self,
        doc_id: str,
        doc_status: DocStatus,
        job_status: JobStatus,
        error: str = ""
    ) -> None:
        """
        Update job progress in database.
        
        Args:
            doc_id: Document identifier (empty string for job-level updates)
            doc_status: New document status
            job_status: New job status
            error: Optional error message
        """
        try:
            self._update_job_in_db(doc_id, doc_status, job_status, error)
        except Exception as e:
            logger.error(f"Failed to update job {self.job_id} in database: {e}", exc_info=True)
            raise
    
    def _update_document_in_db(
        self,
        doc_id: str,
        details: Mapping[str, Any],
        error: str
    ) -> None:
        """
        Update document in database.
        
        Args:
            doc_id: Document identifier
            details: Dictionary of fields to update
            error: Optional error message
        """
        # Separate metadata fields from top-level fields
        metadata_fields, top_level_fields = self._categorize_fields(details)
        
        # Prepare update parameters
        update_params: Dict[str, Any] = {}
        
        # Handle status update
        if "status" in top_level_fields:
            status_value = top_level_fields["status"]
            try:
                update_params["status"] = DocStatus(status_value)
            except (ValueError, TypeError):
                logger.warning(f"Invalid status value: {status_value}")
        
        # Handle completed_at
        if "completed_at" in top_level_fields:
            completed_at_str = top_level_fields["completed_at"]
            if completed_at_str:
                try:
                    update_params["completed_at"] = datetime.fromisoformat(
                        completed_at_str.replace("Z", "+00:00")
                    )
                except (ValueError, TypeError) as e:
                    logger.warning(f"Invalid completed_at format: {completed_at_str}, {e}")
        
        # Handle error
        if error:
            update_params["error"] = error
        
        # Handle metadata updates
        if metadata_fields:
            # Get existing document to merge metadata
            existing_doc = db_repo.get_document_by_id(doc_id)
            if existing_doc:
                merged_metadata = existing_doc.doc_metadata.copy()
                
                # Merge timing updates
                if "timing_in_secs" in metadata_fields:
                    merged_metadata.setdefault("timing_in_secs", {})
                    merged_metadata["timing_in_secs"].update(metadata_fields["timing_in_secs"])
                
                # Update other metadata fields
                for key, value in metadata_fields.items():
                    if key != "timing_in_secs" and value is not None:
                        merged_metadata[key] = value
                
                update_params["metadata"] = merged_metadata
        
        # Perform database update
        if update_params:
            success = db_repo.update_document(doc_id, **update_params)
            if success:
                logger.debug(f"Updated document {doc_id} in database")
            else:
                logger.warning(f"Document {doc_id} not found in database for update")
    
    def _update_job_in_db(
        self,
        doc_id: str,
        doc_status: DocStatus,
        job_status: JobStatus,
        error: str
    ) -> None:
        """
        Update job and associated document in database.
        
        Args:
            doc_id: Document identifier (empty for job-level updates)
            doc_status: New document status
            job_status: New job status
            error: Optional error message
        """
        # Update document status if doc_id provided
        if doc_id:
            db_repo.update_document(doc_id, status=doc_status)
        
        # Get current job to recalculate stats
        job = db_repo.get_job_by_id(self.job_id)
        if not job:
            logger.warning(f"Job {self.job_id} not found in database")
            return
        
        # Get all documents for this job to recalculate stats
        documents = db_repo.get_documents_by_job_id(self.job_id)
        
        # Recalculate statistics
        stats = {
            "total_documents": len(documents),
            "completed": sum(1 for d in documents if d.status == DocStatus.COMPLETED.value),
            "failed": sum(1 for d in documents if d.status == DocStatus.FAILED.value),
            "in_progress": sum(
                1 for d in documents if d.status in [
                    DocStatus.IN_PROGRESS.value,
                    DocStatus.DIGITIZED.value,
                    DocStatus.PROCESSED.value,
                    DocStatus.CHUNKED.value
                ]
            )
        }
        
        # Prepare job update parameters
        update_params: Dict[str, Any] = {
            "status": job_status,
            "stats": stats
        }
        
        # Set completed_at if job is finished
        if job_status in [JobStatus.COMPLETED, JobStatus.FAILED]:
            total_docs = stats["total_documents"]
            completed_docs = stats["completed"]
            failed_docs = stats["failed"]
            
            if total_docs > 0 and (completed_docs + failed_docs) == total_docs:
                update_params["completed_at"] = datetime.now(timezone.utc)
        
        # Set error if provided
        if error and job_status == JobStatus.FAILED:
            update_params["error"] = error
        
        # Perform database update
        success = db_repo.update_job(self.job_id, **update_params)
        if success:
            logger.debug(f"Updated job {self.job_id} in database")
        else:
            logger.warning(f"Job {self.job_id} not found in database for update")


def get_status_manager(job_id: str) -> StatusManager:
    """
    Factory function to get database-first status manager.
    
    Returns DatabaseStatusManager which requires database to be available.
    
    Args:
        job_id: Unique identifier for the job
        
    Returns:
        DatabaseStatusManager instance
        
    Raises:
        RuntimeError: If database is not available
    """
    return DatabaseStatusManager(job_id)

# Made with Bob
