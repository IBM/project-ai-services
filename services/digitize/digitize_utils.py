import asyncio
import json
from functools import partial
from pathlib import Path
import shutil
from typing import List, Optional, Dict, Any, Mapping
import uuid
from datetime import datetime, timezone

from common.misc_utils import get_logger
from digitize.models import (
    OutputFormat,
    DocumentListItem,
    DocumentDetailResponse,
    DocumentContentResponse,
    JobStatus,
    JobState,
    DocStatus
)
from digitize.settings import settings
from digitize.db.manager import db_manager
from digitize.db.connection import engine

logger = get_logger("digitize_utils")


def get_utc_timestamp() -> str:
    """
    Generate UTC timestamp in ISO format with 'Z' suffix.

    Returns:
        ISO 8601 formatted timestamp string with 'Z' suffix
    """
    from datetime import datetime, timezone
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
    from digitize.db.connection import engine
    from digitize.db.manager import db_manager
    from datetime import datetime

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
    from digitize.db.connection import engine
    from digitize.db.manager import db_manager
    from digitize.models import DocStatus
    from datetime import datetime

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




def get_job(job_id: str) -> Optional[dict]:
    """
    Get job data from database.

    Args:
        job_id: Unique identifier for the job

    Returns:
        Job data dictionary or None if not found
    """
    from digitize.db.connection import engine
    from digitize.db.manager import db_manager

    # Database is the primary and only source
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve job without database connection.")

    try:
        job = db_manager.get_job_by_id(job_id)
        if job:
            # Convert SQLAlchemy model to dictionary
            from digitize.models import JobState, JobDocumentSummary, JobStats

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
    from digitize.db.connection import engine
    from digitize.db.manager import db_manager

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
        from digitize.models import JobState, JobDocumentSummary, JobStats

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
    from digitize.db.connection import engine
    from digitize.db.manager import db_manager

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
    from digitize.db.connection import engine
    from digitize.db.manager import db_manager

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


# ============================================================================
# Utility Functions
# ============================================================================

def generate_uuid():
    """
    Generate a random UUID: can be used for job IDs and document IDs.

    Returns:
        Random UUID string
    """
    # Generate a random UUID (uuid4)
    generated_uuid = uuid.uuid4()
    logger.debug(f"Generated UUID: {generated_uuid}")
    return str(generated_uuid)


def get_all_document_ids() -> list[str]:
    """
    Read all document IDs from the database.

    Returns:
        List of document IDs found in database
    """
    from digitize.db.connection import engine
    from digitize.db.manager import db_manager

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


def initialize_job_state(job_id: str, operation: str, output_format: OutputFormat, documents_info: list[str], job_name: Optional[str] = None) -> dict[str, str]:
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
    submitted_at = get_utc_timestamp()

    # Generate document IDs upfront
    doc_id_dict = {doc: generate_uuid() for doc in documents_info}

    # CRITICAL: Create job FIRST before documents (foreign key constraint)
    create_job(
        job_id=job_id,
        operation=operation,
        submitted_at=submitted_at,
        doc_id_dict=doc_id_dict,
        documents_info=documents_info,
        job_name=job_name
    )

    # Now create document metadata in both database and file system
    for doc in documents_info:
        doc_id = doc_id_dict[doc]
        logger.debug(f"Generated document id {doc_id} for file: {doc}")
        create_document(
            doc_name=doc,
            doc_id=doc_id,
            job_id=job_id,
            output_format=output_format,
            operation=operation,
            submitted_at=submitted_at
        )

    return doc_id_dict


async def stage_upload_files(job_id: str, files: List[str], staging_dir: str, file_contents: List[bytes]):
    base_stage_path = Path(staging_dir)
    base_stage_path.mkdir(parents=True, exist_ok=True)

    def save_sync(file_path: Path, content: bytes):
        with open(file_path, "wb") as f:
            f.write(content)
        return str(file_path)

    loop = asyncio.get_running_loop()

    for filename, content in zip(files, file_contents):
        target_path = base_stage_path / filename

        try:
            await loop.run_in_executor(
                None,
                partial(save_sync, target_path, content)
            )
            logger.debug(f"Successfully staged file: {filename}")

        except PermissionError as e:
            logger.error(f"Permission denied while staging {filename} for job {job_id}: {e}")
            raise
        except FileNotFoundError as e:
            logger.error(f"Target path not found while staging {filename} for job {job_id}: {e}")
            raise
        except IsADirectoryError as e:
            logger.error(f"Target path is a directory, cannot write file {filename} for job {job_id}: {e}")
            raise
        except MemoryError as e:
            logger.error(f"Insufficient memory to read/write {filename} for job {job_id}: {e}")
            raise
        except Exception as e:
            logger.error(f"Unexpected error while staging {filename} for job {job_id}: {e}")
            raise



def get_all_documents(
    status_filter: Optional[str] = None,
    name_filter: Optional[str] = None
) -> List[DocumentListItem]:
    """
    Read all document metadata, apply filters, and sort by submitted time.
    Returns minimal document information (id, name, type, status) as Pydantic models.

    Args:
        status_filter: Optional status to filter by (case-insensitive)
        name_filter: Optional name to filter by (case-insensitive partial match)

    Returns:
        List of DocumentListItem models sorted by submitted_at (most recent first)
    """
    logger.debug(f"Fetching documents with filters: status={status_filter}, name={name_filter}")

    try:
        documents_data, _ = get_all_documents_paginated(
            status=status_filter,
            name=name_filter,
            limit=10000,
            offset=0
        )
        
        result = [DocumentListItem(**doc) for doc in documents_data]
        logger.debug(f"Returning {len(result)} documents after filtering")
        return result
    except Exception as e:
        logger.error(f"Failed to get documents from database: {e}", exc_info=True)
        return []




def get_document_content(doc_id: str) -> DocumentContentResponse:
    """
    Read the digitized content of a document from the local cache.

    For documents submitted via digitization, this returns the output_format requested during POST (md/text/json).
    For documents submitted via ingestion, this defaults to returning the extracted json representation.

    Args:
        doc_id: Unique identifier of the document
        docs_dir: Directory containing document metadata files

    Returns:
        DocumentContentResponse model with result and output_format

    Raises:
        FileNotFoundError: If document metadata or content file doesn't exist
        json.JSONDecodeError: If metadata or content file is corrupted
        ValidationError: If metadata doesn't match expected schema
    """
    logger.debug(f"Fetching content for document {doc_id}")

    # Read document metadata from database
    doc_response = get_document(doc_id, include_details=False)

    # Get the output format from the response
    output_format = doc_response.output_format

    # Determine file extension based on output format
    file_extension = output_format  # json, md, or text
    content_file = settings.digitize.digitized_docs_dir / f"{doc_id}.{file_extension}"

    if not content_file.exists():
        logger.error(f"Document content file not found: {content_file}")
        raise FileNotFoundError(f"Content file for document '{doc_id}' not found")

    # Read content based on output format
    try:
        with open(content_file, "r", encoding="utf-8") as f:
            if output_format == "json":
                # For JSON format, parse as JSON
                content_data = json.load(f)
            else:
                # For md/text format, read as plain text
                content_data = f.read()
    except json.JSONDecodeError as e:
        logger.error(f"Failed to parse JSON content file for document {doc_id}: {e}")
        raise
    except Exception as e:
        logger.error(f"Failed to read content file for document {doc_id}: {e}")
        raise

    # The content is already in the requested format
    # For json: content_data is a dict (DoclingDocument JSON)
    # For md/text: content_data is a string (already converted during digitization)
    logger.debug(f"Successfully retrieved content for document {doc_id} in {output_format} format")

    return DocumentContentResponse(
        result=content_data,
        output_format=output_format
    )

def is_document_in_active_job(doc_id: str, job_id: Optional[str]) -> bool:
    """
    Check if a document is part of any active job (in_progress status).
    
    This function checks the database for job status.
    
    Args:
        doc_id: Unique identifier of the document
        job_id: Job ID from document metadata (can be None if document has no associated job)
        
    Returns:
        True if document is in an active job, False otherwise
    """
    logger.debug(f"Checking if document {doc_id} is part of an active job")
    
    # If document has no job_id, it's not part of any job
    if not job_id:
        logger.debug(f"Document {doc_id} has no associated job_id")
        return False
    
    logger.debug(f"Document {doc_id} is associated with job {job_id}")
    
    # Read the job status from database and check if it's in progress
    try:
        job_data = get_job(job_id)
        if job_data is None:
            logger.debug(f"Job {job_id} not found in database")
            return False
        
        job_status = job_data.get("status", "").lower()
        if job_status == JobStatus.IN_PROGRESS.value:
            logger.info(f"Document {doc_id} is part of active job {job_id}")
            return True
        else:
            logger.debug(f"Job {job_id} exists but is not in progress (status: {job_status})")
            return False
            
    except Exception as e:
        logger.error(f"Error reading job {job_id} from database: {e}", exc_info=True)
        return False


def delete_document_files(doc_id: str, output_format: str) -> None:
    """
    Delete digitized content file associated with a document from the cache directory.
    
    Note: Document metadata is stored in PostgreSQL and managed separately via the database.
    This function only handles file system cleanup of digitized content.
    
    Files deleted:
    - /var/cache/digitized/<doc_id>.<extension> (based on output_format)
    
    Args:
        doc_id: Unique identifier of the document
        output_format: Output format of the document (txt, md, or json)
        docs_dir: Directory parameter (kept for backward compatibility, not used)
        
    Raises:
        ValueError: If output_format is invalid
    """
    logger.debug(f"Deleting files for document {doc_id} with format {output_format}")
    
    # Validate output_format against OutputFormat enum
    valid_formats = [fmt.value for fmt in OutputFormat]
    if output_format not in valid_formats:
        raise ValueError(f"Invalid output_format: '{output_format}'. Must be one of: {', '.join(valid_formats)}")

    # Delete digitized content file
    content_file = settings.digitize.digitized_docs_dir / f"{doc_id}.{output_format}"
    if content_file.exists():
        try:
            content_file.unlink()
            logger.debug(f"✓ Deleted content file: {content_file}")
            logger.info(f"✅ Deleted content file for document {doc_id}")
        except Exception as e:
            error_msg = f"Failed to delete content file {content_file}: {e}"
            logger.error(f"✗ {error_msg}")
            raise Exception(f"Failed to delete content file: {error_msg}") from e
    else:
        logger.warning(f"Content file not found (may have been deleted already): {content_file}")


def has_active_jobs(operation: Optional[str] = None) -> tuple[bool, list[str]]:
    """
    Check if there are any active jobs (accepted or in_progress status) in the database.
    Optionally filter by operation type.

    Args:
        operation: Optional operation type to filter by (e.g., 'ingestion', 'digitization')

    Returns:
        Tuple of (has_active, active_job_ids) where has_active is True if any active jobs exist
    """
    filter_msg = f" for operation '{operation}'" if operation else ""
    logger.debug(f"Checking for active jobs{filter_msg}")

    try:
        # Get jobs with ACCEPTED or IN_PROGRESS status
        active_job_ids = []
        
        for status in [JobStatus.ACCEPTED, JobStatus.IN_PROGRESS]:
            jobs_data, _ = get_all_jobs(
                status=status,
                operation=operation,
                limit=10000,
                offset=0
            )
            
            for job_data in jobs_data:
                job_id = job_data.get("job_id")
                if job_id:
                    active_job_ids.append(job_id)
                    logger.debug(f"Found active job: {job_id} with status {status.value}")

        has_active = len(active_job_ids) > 0
        if has_active:
            logger.info(f"Found {len(active_job_ids)} active job(s){filter_msg}: {active_job_ids}")
        else:
            logger.debug(f"No active jobs found{filter_msg}")

        return has_active, active_job_ids
    except Exception as e:
        logger.error(f"Error checking for active jobs: {e}", exc_info=True)
        return False, []

def cleanup_digitized_files() -> dict:
    """
    Delete all digitized content files from the cache directory.
    
    This utility function removes all digitized content files (json, md, text)
    from DIGITIZED_DOCS_DIR (/var/cache/digitized).
    
    Returns:
        Dictionary with deletion statistics containing:
        - content_files_deleted: Number of files successfully deleted
        - errors: List of error messages for failed deletions
    """
    logger.info("Cleaning up digitized content files...")

    cleanup_stats = {
        "content_files_deleted": 0,
        "errors": []
    }

    if settings.digitize.digitized_docs_dir.exists():
        try:
            # Count files before deletion
            file_count = sum(1 for _ in settings.digitize.digitized_docs_dir.iterdir() if _.is_file())
            logger.debug(f"Found {file_count} files in {settings.digitize.digitized_docs_dir}")

            # Delete the entire directory and recreate it
            shutil.rmtree(settings.digitize.digitized_docs_dir)
            settings.digitize.digitized_docs_dir.mkdir(parents=True, exist_ok=True)

            cleanup_stats["content_files_deleted"] = file_count
            logger.info(f"✅ Cleanup completed: {file_count} content files deleted")
        except Exception as e:
            error_msg = f"Failed to clean up digitized directory: {e}"
            logger.error(f"✗ {error_msg}")
            cleanup_stats["errors"].append(error_msg)
    else:
        logger.info(f"Digitized directory {settings.digitize.digitized_docs_dir} does not exist")
    
    if cleanup_stats["errors"]:
        logger.error(f"Cleanup completed with {len(cleanup_stats['errors'])} errors")
    
    return cleanup_stats


def bulk_delete_all_documents() -> dict:
    """
    Delete all digitized content files from the system.

    Note: Document metadata is stored in PostgreSQL and should be managed separately
    via the database. This function only handles file system cleanup of digitized content.

    This function does NOT delete job status files or reset the vector database.
    Those operations should be handled separately by the caller.

    Returns:
        Dictionary with deletion statistics
    """
    logger.info("Starting bulk deletion of all digitized content files...")

    deletion_stats = {
        "metadata_files_deleted": 0,  # Metadata now in PostgreSQL, no files to delete
        "content_files_deleted": 0,
        "errors": []
    }

    # Delete all digitized content files using the utility function
    cleanup_stats = cleanup_digitized_files()
    deletion_stats["content_files_deleted"] = cleanup_stats["content_files_deleted"]
    deletion_stats["errors"].extend(cleanup_stats["errors"])

    # Log summary
    logger.info(
        f"✅ Bulk deletion completed: {deletion_stats['content_files_deleted']} content files deleted"
    )

    if deletion_stats["errors"]:
        logger.error(f"Bulk deletion completed with {len(deletion_stats['errors'])} errors")

    return deletion_stats


def scan_and_recover_orphan_jobs() -> int:
    """
    Boot-up scan to identify and mark orphan jobs as failed.

    An orphan job is one with status 'accepted' or 'in_progress' that exists
    when the application starts, indicating the previous instance crashed
    while processing it.

    This method:
    1. Queries database for active jobs
    2. Updates documents in in-progress states to failed
    3. Updates job status using database status manager

    Returns:
        Number of orphan jobs recovered
    """
    from digitize.models import JobStatus, DocStatus
    from digitize.doc_utils import clean_intermediate_files
    import digitize.settings as config

    orphan_count = 0
    orphan_statuses = [JobStatus.ACCEPTED, JobStatus.IN_PROGRESS]

    try:
        # Scan all jobs with active statuses from database
        for status in orphan_statuses:
            jobs_data, _ = get_all_jobs(
                status=status,
                limit=10000,
                offset=0
            )
            
            for job_data in jobs_data:
                job_id = job_data.get("job_id")
                if not job_id:
                    logger.warning("Skipping job with missing job_id")
                    continue
                    
                try:
                    current_status = job_data.get("status")
                    
                    logger.warning(f"Found orphan job: {job_id} with status '{current_status}'")

                    # Get database-aware status manager
                    status_mgr = get_status_manager(job_id)

                    # Build error message with cleanup instructions
                    error_message = "System restarted during processing"

                    # Step 1: Update document metadata and job progress for each document
                    # Process all documents in in-progress states to failed
                    # Also clean up intermediate files for all documents (even completed ones)
                    if "documents" in job_data and job_data["documents"]:
                        doc_ids = []
                        for doc in job_data["documents"]:
                            doc_status = doc.get("status")
                            doc_id = doc.get("id")
                            
                            if doc_id:
                                # Clean up intermediate files for all documents
                                # This step may have been missed during the last restart
                                try:
                                    clean_intermediate_files(doc_id, config.settings.digitize.digitized_docs_dir)
                                    logger.debug(f"Cleaned intermediate files for document {doc_id}")
                                except Exception as e:
                                    logger.warning(f"Failed to clean intermediate files for {doc_id}: {e}")
                            
                            # Check if document is in any in-progress state
                            if doc_status in {DocStatus.ACCEPTED.value, DocStatus.IN_PROGRESS.value,
                                            DocStatus.DIGITIZED.value, DocStatus.PROCESSED.value,
                                            DocStatus.CHUNKED.value}:
                                if doc_id:
                                    doc_ids.append(doc_id)
                                    
                                    # Update individual document metadata using database-aware manager
                                    status_mgr.update_doc_metadata(
                                        doc_id,
                                        {"status": DocStatus.FAILED},
                                        error=f"System restarted during processing. Use DELETE /v1/documents/{doc_id} to remove the stale document and re-submit the document to process again"
                                    )
                                    
                                    # Update job progress with document status change
                                    # Use IN_PROGRESS for job status temporarily to allow document updates
                                    status_mgr.update_job_progress(
                                        doc_id=doc_id,
                                        doc_status=DocStatus.FAILED,
                                        job_status=JobStatus.IN_PROGRESS,
                                        error=""
                                    )
                                    logger.debug(f"Updated document {doc_id} to FAILED")
                                    
                        # Add document IDs to error message if any were found
                        if doc_ids:
                            error_message += f". Stale documents may exist. Please use DELETE /v1/documents/{{id}} to remove these documents and re-submit to process again: {', '.join(doc_ids)}"

                    # Step 2: Finally update the overall job status to FAILED
                    # Use empty doc_id to only update job-level status
                    status_mgr.update_job_progress(
                        doc_id="",
                        doc_status=DocStatus.FAILED,  # Not used when doc_id is empty
                        job_status=JobStatus.FAILED,
                        error=error_message
                    )

                    logger.info(f"✅ Marked orphan job {job_id} as failed")
                    orphan_count += 1

                    # Clean up staging directory for this orphan job
                    cleanup_staging_directory(job_id, config.settings.digitize.staging_dir)

                except Exception as e:
                    logger.error(f"Error processing orphan job {job_id}: {e}", exc_info=True)

    except Exception as e:
        logger.error(f"Error scanning for orphan jobs: {e}", exc_info=True)

    if orphan_count > 0:
        logger.debug(f"🔄 Recovered {orphan_count} orphan job(s) on startup")
    else:
        logger.debug("✅ No orphan jobs found on startup")
    return orphan_count


def cleanup_staging_directory(job_id: str, staging_base_dir: Path) -> bool:
    """
    Clean up the staging directory for a specific job.

    This helper function safely removes the staging directory and all its contents.
    It's used across multiple places in the codebase to ensure consistent cleanup behavior.

    Args:
        job_id: Unique identifier of the job
        staging_base_dir: Base directory where staging directories are created

    Returns:
        True if cleanup was successful or directory didn't exist, False if cleanup failed
    """
    staging_dir = staging_base_dir / job_id

    if not staging_dir.exists():
        logger.debug(f"Staging directory does not exist (already cleaned up): {staging_dir}")
        return True

    try:
        shutil.rmtree(staging_dir)
        logger.info(f"🗑️  Cleaned up staging directory: {staging_dir}")
        return True
    except Exception as e:
        logger.warning(f"Failed to clean up staging directory {staging_dir}: {e}")
        return False

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
