"""
Job lifecycle utilities — utils/jobs.py

Coordinator layer between the API routers and the DB/storage layers:
job initialisation, file staging, active-job guards, document content
retrieval, and bulk deletion helpers.
"""
import uuid
from typing import Optional

from common.misc_utils import get_logger
from digitize.models import (
    OutputFormat,
    DocumentContentResponse,
    JobStatus,
    DocStatus,
)
from digitize.settings import settings
from digitize.utils.db import (
    create_job,
    create_document,
    get_job,
    get_all_jobs,
    get_document,
    get_status_manager,
)

from common.misc_utils import get_utc_timestamp, cleanup_staging_directory


logger = get_logger("digitize_utils")

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
        completed_docs = [
            doc for doc in documents
            if doc.get("status") in (DocStatus.COMPLETED.value, DocStatus.ALREADY_EXISTS.value)
        ]

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


def initialize_job_state(
    job_id: str,
    operation: str,
    output_format: OutputFormat,
    documents_info: list[str],
    job_name: Optional[str] = None,
    already_exists_files: Optional[list] = None,   # list[AlreadyExistsFile]
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
        already_exists_files: Optional list of AlreadyExistsFile entries that were
                              stripped from the batch before staging.

    Returns:
        dict[str, str]: Mapping of filename -> document_id
    """
    submitted_at = get_utc_timestamp()

    # Generate document IDs upfront
    doc_id_dict = {doc: generate_uuid() for doc in documents_info}

    # CRITICAL: Create job FIRST before documents (foreign key constraint).
    # total_documents must cover both novel files AND any already-exists files so
    # that the initial stats are accurate before the pipeline touches them.
    all_filenames = list(documents_info) + (
        [f.filename for f in already_exists_files] if already_exists_files else []
    )
    create_job(
        job_id=job_id,
        operation=operation,
        submitted_at=submitted_at,
        documents_info=all_filenames,
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

    # Record ALREADY_EXISTS entries for files stripped from the batch.
    # Created AFTER the job row exists (foreign key constraint).
    # Written directly as ALREADY_EXISTS in a single DB insert — there is no
    # "accepted" window and no follow-up update_doc_metadata call needed.
    if already_exists_files:
        from digitize.utils.db import get_status_manager
        from digitize.models import JobStatus as _JobStatus
        status_mgr = get_status_manager(job_id)
        for skipped in already_exists_files:
            skipped_doc_id = generate_uuid()
            doc_id_dict[skipped.filename] = skipped_doc_id
            logger.debug(
                f"Recording already_exists doc {skipped_doc_id} "
                f"for file '{skipped.filename}'"
            )
            create_document(
                doc_name=skipped.filename,
                doc_id=skipped_doc_id,
                job_id=job_id,
                output_format=output_format,
                operation=operation,
                submitted_at=submitted_at,
                initial_status=DocStatus.ALREADY_EXISTS,
                completed_at=submitted_at,
                extra_metadata={
                    "existing_doc_id": skipped.existing_doc_id,
                    "existing_doc_name": skipped.existing_doc_name,
                    "file_hash": skipped.file_hash,
                },
            )
            # Update job stats to include this resolved doc — no doc-level status
            # write needed because it was already set correctly in create_document.
            status_mgr.update_job_progress(
                "",
                DocStatus.ALREADY_EXISTS,
                _JobStatus.IN_PROGRESS,
            )

    return doc_id_dict


async def stage_upload_files(
    job_id: str,
    files: list[str],
    staging_dir: str,
    file_contents: list[bytes],
) -> None:
    """
    Stage uploaded files to disk before launching the background task.

    Delegates to :class:`~digitize.utils.storage.StorageManager`.

    Args:
        job_id: Unique job identifier.
        files: List of original filenames.
        staging_dir: Target staging directory path (ignored — derived from settings).
        file_contents: Raw bytes per file.
    """
    from digitize.utils.storage import storage_manager

    await storage_manager.stage_upload_files(job_id, files, file_contents)

def get_document_content(doc_id: str) -> DocumentContentResponse:
    """
    Read the digitized content of a document from the local cache.

    Delegates to :class:`~digitize.utils.storage.StorageManager` for all
    file-system operations so that the path-building logic is centralised.

    Args:
        doc_id: Unique identifier of the document

    Returns:
        DocumentContentResponse model with result and output_format

    Raises:
        FileNotFoundError: If document metadata or content file doesn't exist
        json.JSONDecodeError: If metadata or content file is corrupted
    """
    from digitize.utils.storage import storage_manager

    logger.debug(f"Fetching content for document {doc_id}")

    # Resolve output format from database metadata.
    doc_response = get_document(doc_id, include_details=False)
    output_format = doc_response.output_format

    return storage_manager.read_document_content(doc_id, output_format)

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

