"""
Database operations module for digitize service.

This module contains all functions that directly interact with the database
using db_manager. It provides a clean separation between database operations
and other utility functions.
"""

from datetime import datetime, timezone
from typing import List, Optional, Dict, Any, Mapping

from common.misc_utils import get_logger
from digitize.models import (
    OutputFormat,
    DocumentDetailResponse,
    JobStatus,
    JobState,
    DocStatus,
    JobDocumentSummary,
    JobStats
)
from digitize.db.manager import db_manager
from digitize.db.connection import engine

logger = get_logger("db_operations")


# ============================================================================
# Job Operations
# ============================================================================

def create_job(
    job_id: str,
    operation: str,
    submitted_at: str,
    doc_id_dict: dict[str, str],
    documents_info: list[str],
    job_name: Optional[str] = None
) -> None:
    """
    Create job in database.

    Args:
        job_id: Unique identifier for the job
        operation: Type of operation (ingestion/digitization)
        submitted_at: ISO timestamp when job was submitted
        doc_id_dict: Mapping of document names to their IDs
        documents_info: List of document filenames
        job_name: Optional human-readable name for the job
    """
    if engine is None:
        raise RuntimeError("Database not available. Cannot create job without database connection.")

    try:
        # Parse ISO timestamp to datetime
        submitted_dt = datetime.fromisoformat(submitted_at.replace("Z", "+00:00"))

        # Create job in database
        db_manager.create_job(
            job_id=job_id,
            operation=operation,
            status=JobStatus.ACCEPTED,
            job_name=job_name,
            submitted_at=submitted_dt,
            stats={
                "total_documents": len(documents_info),
                "completed": 0,
                "failed": 0,
                "in_progress": 0
            }
        )
        logger.info(f"Created job {job_id} in database")

    except Exception as e:
        logger.error(f"Failed to create job {job_id} in database: {e}", exc_info=True)
        raise


def get_job(job_id: str) -> Optional[dict]:
    """
    Get job data from database.

    Args:
        job_id: Unique identifier for the job

    Returns:
        Job data dictionary or None if not found
    """
    # Database is the primary and only source
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve job without database connection.")

    try:
        job = db_manager.get_job_by_id(job_id)
        if job:
            # Get documents for this job
            documents = db_manager.get_documents_by_job_id(job_id)
            doc_summaries = [
                JobDocumentSummary(
                    id=doc.doc_id,
                    name=doc.name,
                    status=doc.status
                )
                for doc in documents
            ]

            # Create JobState object
            job_state = JobState(
                job_id=job.job_id,
                job_name=job.job_name,
                operation=job.operation,
                status=JobStatus(job.status),
                submitted_at=job.submitted_at.isoformat().replace("+00:00", "Z"),
                completed_at=job.completed_at.isoformat().replace("+00:00", "Z") if job.completed_at else None,
                documents=doc_summaries,
                stats=JobStats(**job.stats),
                error=job.error
            )

            logger.debug(f"Retrieved job {job_id} from database")
            return job_state.to_dict()
        else:
            logger.debug(f"Job {job_id} not found in database")
            return None
    except Exception as e:
        logger.error(f"Failed to get job {job_id} from database: {e}", exc_info=True)
        raise


def get_all_jobs(
    status: Optional[JobStatus] = None,
    operation: Optional[str] = None,
    limit: int = 20,
    offset: int = 0
) -> tuple[List[dict], int]:
    """
    Get all jobs from database.

    Args:
        status: Filter by job status
        operation: Filter by operation type
        limit: Maximum number of jobs to return
        offset: Number of jobs to skip

    Returns:
        Tuple of (list of job dictionaries, total count)
    """
    # Database is the primary and only source
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve jobs without database connection.")

    try:
        jobs, total = db_manager.get_all_jobs(
            status=status,
            operation=operation,
            limit=limit,
            offset=offset
        )

        # Convert SQLAlchemy models to dictionaries
        job_dicts = []
        for job in jobs:
            # Get documents for this job
            documents = db_manager.get_documents_by_job_id(job.job_id)
            doc_summaries = [
                JobDocumentSummary(
                    id=doc.doc_id,
                    name=doc.name,
                    status=doc.status
                )
                for doc in documents
            ]

            # Create JobState object
            job_state = JobState(
                job_id=job.job_id,
                job_name=job.job_name,
                operation=job.operation,
                status=JobStatus(job.status),
                submitted_at=job.submitted_at.isoformat().replace("+00:00", "Z"),
                completed_at=job.completed_at.isoformat().replace("+00:00", "Z") if job.completed_at else None,
                documents=doc_summaries,
                stats=JobStats(**job.stats),
                error=job.error
            )
            job_dicts.append(job_state.to_dict())

        logger.debug(f"Retrieved {len(job_dicts)} jobs from database (total: {total})")
        return job_dicts, total
    except Exception as e:
        logger.error(f"Failed to get jobs from database: {e}", exc_info=True)
        raise


# ============================================================================
# Document Operations
# ============================================================================

def create_document(
    doc_name: str,
    doc_id: str,
    job_id: str,
    output_format: OutputFormat,
    operation: str,
    submitted_at: str
) -> None:
    """
    Create document metadata in database.

    Args:
        doc_name: Name of the document file
        doc_id: Unique identifier for the document
        job_id: ID of the parent job
        output_format: Output format for the document
        operation: Type of operation (ingestion/digitization)
        submitted_at: ISO timestamp when document was submitted
    """
    if engine is None:
        raise RuntimeError("Database not available. Cannot create document without database connection.")

    try:
        # Parse ISO timestamp to datetime
        submitted_dt = datetime.fromisoformat(submitted_at.replace("Z", "+00:00"))

        # Create document in database
        db_manager.create_document(
            doc_id=doc_id,
            name=doc_name,
            doc_type=operation,
            status=DocStatus.ACCEPTED,
            output_format=output_format.value,
            submitted_at=submitted_dt,
            job_id=job_id,
            metadata={
                "pages": 0,
                "tables": 0,
                "timing_in_secs": {
                    "digitizing": None,
                    "processing": None,
                    "chunking": None,
                    "indexing": None
                }
            }
        )
        logger.info(f"Created document {doc_id} in database")

    except Exception as e:
        logger.error(f"Failed to create document {doc_id} in database: {e}", exc_info=True)
        raise


def get_document(doc_id: str, include_details: bool = True) -> DocumentDetailResponse:
    """
    Get document data from database and return as Pydantic model.

    Args:
        doc_id: Unique identifier for the document
        include_details: If True, includes metadata fields; if False, excludes them

    Returns:
        DocumentDetailResponse model with document information

    Raises:
        FileNotFoundError: If document doesn't exist in database
        RuntimeError: If database is not available
    """
    logger.debug(f"Fetching document {doc_id} with include_details={include_details}")

    # Database is the primary and only source
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve document without database connection.")

    try:
        doc = db_manager.get_document_by_id(doc_id)
        if doc:
            # Convert SQLAlchemy model to dictionary
            doc_dict = {
                "id": doc.doc_id,
                "job_id": doc.job_id,
                "name": doc.name,
                "type": doc.type,
                "status": doc.status,
                "output_format": doc.output_format,
                "submitted_at": doc.submitted_at.isoformat().replace("+00:00", "Z"),
                "completed_at": doc.completed_at.isoformat().replace("+00:00", "Z") if doc.completed_at else None,
                "error": doc.error,
                "metadata": doc.doc_metadata
            }

            # Conditionally exclude metadata if not requested
            if not include_details:
                doc_dict.pop('metadata', None)

            # Let Pydantic validate and convert the data
            response = DocumentDetailResponse(**doc_dict)
            logger.debug(f"Successfully retrieved document {doc_id}")
            return response
        else:
            logger.debug(f"Document {doc_id} not found in database")
            raise FileNotFoundError(f"Document with ID '{doc_id}' not found")
    except FileNotFoundError:
        raise
    except Exception as e:
        logger.error(f"Failed to get document {doc_id} from database: {e}", exc_info=True)
        raise


def get_all_documents_paginated(
    status: Optional[str] = None,
    name: Optional[str] = None,
    limit: int = 20,
    offset: int = 0
) -> tuple[List[dict], int]:
    """
    Get all documents from database.

    Args:
        status: Filter by document status
        name: Filter by document name (partial match)
        limit: Maximum number of documents to return
        offset: Number of documents to skip

    Returns:
        Tuple of (list of document dictionaries, total count)
    """
    # Database is the primary and only source
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve documents without database connection.")

    try:
        documents, total = db_manager.get_all_documents(
            status=status,
            name=name,
            limit=limit,
            offset=offset
        )

        # Convert SQLAlchemy models to dictionaries
        doc_dicts = [
            {
                "id": doc.doc_id,
                "name": doc.name,
                "type": doc.type,
                "status": doc.status,
                "submitted_at": doc.submitted_at.isoformat().replace("+00:00", "Z")
            }
            for doc in documents
        ]
        logger.debug(f"Retrieved {len(doc_dicts)} documents from database (total: {total})")
        return doc_dicts, total
    except Exception as e:
        logger.error(f"Failed to get documents from database: {e}", exc_info=True)
        raise


def get_all_document_ids() -> list[str]:
    """
    Read all document IDs from the database.

    Returns:
        List of document IDs found in database
    """
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve document IDs without database connection.")

    try:
        logger.debug("Reading document IDs from database")
        documents, _ = db_manager.get_all_documents(limit=10000, offset=0)
        doc_ids = [doc.doc_id for doc in documents]
        logger.info(f"Found {len(doc_ids)} document IDs in database")
        return doc_ids
    except Exception as e:
        logger.error(f"Failed to read document IDs from database: {e}", exc_info=True)
        raise


# ============================================================================
# Database Status Manager
# ============================================================================

class DatabaseStatusManager:
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
        self.job_id = job_id

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
            self._update_document(doc_id, details, error)
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
            self._update_job(doc_id, doc_status, job_status, error)
        except Exception as e:
            logger.error(f"Failed to update job {self.job_id} in database: {e}", exc_info=True)
            raise

    def _update_document(
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
        metadata_fields, top_level_fields = _categorize_fields(details)

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
            existing_doc = db_manager.get_document_by_id(doc_id)
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
            success = db_manager.update_document(doc_id, **update_params)
            if success:
                logger.debug(f"Updated document {doc_id} in database")
            else:
                logger.warning(f"Document {doc_id} not found in database for update")

    def _update_job(
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
            db_manager.update_document(doc_id, status=doc_status)

        # Get current job to recalculate stats
        job = db_manager.get_job_by_id(self.job_id)
        if not job:
            logger.warning(f"Job {self.job_id} not found in database")
            return

        # Get all documents for this job to recalculate stats
        documents = db_manager.get_documents_by_job_id(self.job_id)

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
        success = db_manager.update_job(self.job_id, **update_params)
        if success:
            logger.debug(f"Updated job {self.job_id} in database")
        else:
            logger.warning(f"Job {self.job_id} not found in database for update")


def get_status_manager(job_id: str) -> DatabaseStatusManager:
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


# ============================================================================
# Helper Functions
# ============================================================================

def _categorize_fields(details: Mapping[str, Any]) -> tuple[dict[str, Any], dict[str, Any]]:
    """
    Separate fields into metadata wrapper and top-level categories.

    Args:
        details: Dictionary of fields to categorize

    Returns:
        Tuple of (metadata_fields, top_level_fields)
    """
    METADATA_KEYS = {"pages", "tables", "chunks", "timing_in_secs"}

    metadata_fields = {
        k: v if k == "timing_in_secs" and isinstance(v, dict) else _extract_value(v)
        for k, v in details.items() if k in METADATA_KEYS
    }

    top_level_fields = {
        k: _extract_value(v)
        for k, v in details.items() if k not in METADATA_KEYS
    }

    return metadata_fields, top_level_fields


def _extract_value(v: Any) -> Any:
    """Extract .value from enums, return raw value otherwise."""
    return v.value if hasattr(v, "value") else v

# Made with Bob
