"""
Database utility functions for job and document management.

Provides functions that use database as the primary source of truth.
File system operations are kept only for backward compatibility during migration.
"""

from datetime import datetime
from pathlib import Path
from typing import Optional, List, Dict

from common.misc_utils import get_logger
from digitize.models import OutputFormat, JobStatus, DocStatus
from digitize.settings import settings
from digitize.status import (
    get_utc_timestamp,
    create_document_metadata,
    create_job_state
)
from digitize.db.repository import db_repo
from digitize.db.database import engine

logger = get_logger("db_utils")


def create_job_with_db(
    job_id: str,
    operation: str,
    submitted_at: str,
    doc_id_dict: dict[str, str],
    documents_info: list[str],
    jobs_dir: Path = settings.digitize.jobs_dir,
    job_name: Optional[str] = None
) -> None:
    """
    Create job in database (primary) and optionally in file system (migration compatibility).
    
    Args:
        job_id: Unique identifier for the job
        operation: Type of operation (ingestion/digitization)
        submitted_at: ISO timestamp when job was submitted
        doc_id_dict: Mapping of document names to their IDs
        documents_info: List of document filenames
        jobs_dir: Directory where job status files are stored
        job_name: Optional human-readable name for the job
    """
    # Create database entry (primary storage)
    if engine is None:
        raise RuntimeError("Database not available. Cannot create job without database connection.")
    
    try:
        # Parse ISO timestamp to datetime
        submitted_dt = datetime.fromisoformat(submitted_at.replace("Z", "+00:00"))
        
        # Create job in database
        db_repo.create_job(
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
        
        # Also create file system entry for migration compatibility (optional)
        try:
            create_job_state(
                job_id=job_id,
                operation=operation,
                submitted_at=submitted_at,
                doc_id_dict=doc_id_dict,
                documents_info=documents_info,
                jobs_dir=jobs_dir,
                job_name=job_name
            )
            logger.debug(f"Created job {job_id} file for migration compatibility")
        except Exception as e:
            logger.warning(f"Failed to create job file (non-critical): {e}")
            
    except Exception as e:
        logger.error(f"Failed to create job {job_id} in database: {e}", exc_info=True)
        raise


def create_document_with_db(
    doc_name: str,
    doc_id: str,
    job_id: str,
    output_format: OutputFormat,
    operation: str,
    submitted_at: str,
    docs_dir: Path = settings.digitize.docs_dir
) -> None:
    """
    Create document metadata in database (primary) and optionally in file system (migration compatibility).
    
    Args:
        doc_name: Name of the document file
        doc_id: Unique identifier for the document
        job_id: ID of the parent job
        output_format: Output format for the document
        operation: Type of operation (ingestion/digitization)
        submitted_at: ISO timestamp when document was submitted
        docs_dir: Directory where metadata files are stored
    """
    # Create database entry (primary storage)
    if engine is None:
        raise RuntimeError("Database not available. Cannot create document without database connection.")
    
    try:
        # Parse ISO timestamp to datetime
        submitted_dt = datetime.fromisoformat(submitted_at.replace("Z", "+00:00"))
        
        # Create document in database
        db_repo.create_document(
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
        
        # Also create file system entry for migration compatibility (optional)
        try:
            create_document_metadata(
                doc_name=doc_name,
                doc_id=doc_id,
                job_id=job_id,
                output_format=output_format,
                operation=operation,
                submitted_at=submitted_at,
                docs_dir=docs_dir
            )
            logger.debug(f"Created document {doc_id} file for migration compatibility")
        except Exception as e:
            logger.warning(f"Failed to create document file (non-critical): {e}")
            
    except Exception as e:
        logger.error(f"Failed to create document {doc_id} in database: {e}", exc_info=True)
        raise


def initialize_job_state_with_db(
    job_id: str,
    operation: str,
    output_format: OutputFormat,
    documents_info: list[str],
    job_name: Optional[str] = None
) -> dict[str, str]:
    """
    Initialize job state with both database and file system persistence.
    
    Creates job status file, document metadata files, and database entries.
    IMPORTANT: Job must be created BEFORE documents due to foreign key constraint.
    
    Args:
        job_id: Unique identifier for the job
        operation: Type of operation (ingestion/digitization)
        output_format: Output format for documents
        documents_info: List of filenames to be processed
        job_name: Optional human-readable name for the job
        
    Returns:
        dict[str, str]: Mapping of filename -> document_id
    """
    from digitize.digitize_utils import generate_uuid
    
    submitted_at = get_utc_timestamp()
    
    # Generate document IDs upfront
    doc_id_dict = {doc: generate_uuid() for doc in documents_info}
    
    # CRITICAL: Create job FIRST before documents (foreign key constraint)
    create_job_with_db(
        job_id=job_id,
        operation=operation,
        submitted_at=submitted_at,
        doc_id_dict=doc_id_dict,
        documents_info=documents_info,
        jobs_dir=settings.digitize.jobs_dir,
        job_name=job_name
    )
    
    # Now create document metadata in both database and file system
    for doc in documents_info:
        doc_id = doc_id_dict[doc]
        logger.debug(f"Generated document id {doc_id} for file: {doc}")
        create_document_with_db(
            doc_name=doc,
            doc_id=doc_id,
            job_id=job_id,
            output_format=output_format,
            operation=operation,
            submitted_at=submitted_at,
            docs_dir=settings.digitize.docs_dir
        )
    
    return doc_id_dict


def get_job_from_db(job_id: str) -> Optional[Dict]:
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
        job = db_repo.get_job_by_id(job_id)
        if job:
            # Convert SQLAlchemy model to dictionary
            from digitize.job import JobState, JobDocumentSummary, JobStats
            
            # Get documents for this job
            documents = db_repo.get_documents_by_job_id(job_id)
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


def get_all_jobs_from_db(
    status: Optional[JobStatus] = None,
    operation: Optional[str] = None,
    limit: int = 20,
    offset: int = 0
) -> tuple[List[Dict], int]:
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
        jobs, total = db_repo.get_all_jobs(
            status=status,
            operation=operation,
            limit=limit,
            offset=offset
        )
        
        # Convert SQLAlchemy models to dictionaries
        from digitize.job import JobState, JobDocumentSummary, JobStats
        
        job_dicts = []
        for job in jobs:
            # Get documents for this job
            documents = db_repo.get_documents_by_job_id(job.job_id)
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


def get_document_from_db(doc_id: str) -> Optional[Dict]:
    """
    Get document data from database.
    
    Args:
        doc_id: Unique identifier for the document
        
    Returns:
        Document data dictionary or None if not found
    """
    # Database is the primary and only source
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve document without database connection.")
    
    try:
        doc = db_repo.get_document_by_id(doc_id)
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
            logger.debug(f"Retrieved document {doc_id} from database")
            return doc_dict
        else:
            logger.debug(f"Document {doc_id} not found in database")
            return None
    except Exception as e:
        logger.error(f"Failed to get document {doc_id} from database: {e}", exc_info=True)
        raise


def get_all_documents_from_db(
    status: Optional[str] = None,
    name: Optional[str] = None,
    limit: int = 20,
    offset: int = 0
) -> tuple[List[Dict], int]:
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
        documents, total = db_repo.get_all_documents(
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

# Made with Bob
