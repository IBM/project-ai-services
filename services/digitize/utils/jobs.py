""" Job lifecycle utilities — utils/jobs.py.

Coordinator layer between the API routers and the DB/storage layers:
job initialisation, file staging, active-job guards, document content
retrieval, and bulk deletion helpers.
"""
import asyncio
import uuid
from typing import Optional

from common.misc_utils import get_logger
from digitize.db.models import ConversionTaskStatus
from digitize.models import (
    OutputFormat,
    DocumentContentResponse,
    JobStatus,
    DocStatus,
)
from digitize.parsing.pdf import get_document_page_count
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
        _terminal_ok = (
            DocStatus.COMPLETED.value,
            DocStatus.PARTIALLY_INGESTED.value,
            DocStatus.ALREADY_EXISTS.value,
        )
        completed_docs = [doc for doc in documents if doc.get("status") in _terminal_ok]
        partial_docs = [
            doc for doc in documents
            if doc.get("status") == DocStatus.PARTIALLY_INGESTED.value
        ]

        return {
            "failed_docs": failed_docs,
            "completed_docs": completed_docs,
            "partial_docs": partial_docs,
            "total_docs": len(documents),
            "failed_count": len(failed_docs),
            "completed_count": len(completed_docs),
            "partial_count": len(partial_docs),
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
        create_document(
            doc_name=doc,
            doc_id=doc_id,
            job_id=job_id,
            output_format=output_format,
            operation=operation,
            submitted_at=submitted_at
        )
    logger.info(f"Created job {job_id} with {len(documents_info)} document(s) in database")

    # Record ALREADY_EXISTS entries for files stripped from the batch.
    # Created AFTER the job row exists (foreign key constraint).
    # Written directly as ALREADY_EXISTS in a single DB insert — there is no
    # "accepted" window and no follow-up update_doc_metadata call needed.
    if already_exists_files:
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

    return doc_id_dict


async def enqueue_conversion_tasks(
    job_id: str,
    op_key: str,
    filenames: list[str],
    doc_id_dict: dict[str, str],
    staging_dir,
    output_format: OutputFormat,
    quota: int,
    queued_for_op: int,
) -> None:
    """
    Insert conversion_tasks rows for every file in the job in a single
    atomic DB transaction.

    Page counts are still fetched sequentially (each is a file read that
    must complete before ``is_large`` can be determined), but all INSERT
    rows are batched into one ``session.add_all()`` call so the number of
    DB round-trips is reduced from N (one per file) to 1.

    The batch insert is atomic: if any row fails the entire set is rolled
    back, leaving no partial state for the caller to reason about.

    Slots up to the free quota are inserted as ``queued``; the rest as
    ``pending`` (the dispatcher will promote them as capacity frees up).

    Args:
        job_id:        Job identifier.
        op_key:        ``"ingestion"`` or ``"digitization"``.
        filenames:     Ordered list of filenames being submitted.
        doc_id_dict:   Mapping of filename → document ID.
        staging_dir:   Path to the job's staging directory.
        output_format: Requested output format.
        quota:         Per-operation queue quota from settings.
        queued_for_op: Number of tasks already queued for this operation.
    """
    from digitize.db.manager import db_manager

    slots_free = max(0, quota - queued_for_op)
    tasks = []

    for idx, filename in enumerate(filenames):
        task_id = generate_uuid()
        doc_id = doc_id_dict[filename]
        file_path = staging_dir / filename
        page_count = await asyncio.to_thread(get_document_page_count, str(file_path))
        is_large = page_count >= settings.digitize.heavy_doc_page_threshold
        task_status = ConversionTaskStatus.QUEUED if idx < slots_free else ConversionTaskStatus.PENDING
        tasks.append({
            "task_id":       task_id,
            "job_id":        job_id,
            "doc_id":        doc_id,
            "operation":     op_key,
            "cached_file":   str(file_path),
            "output_format": output_format.value,
            "page_count":    page_count,
            "is_large":      is_large,
            "status":        task_status,
        })
        logger.debug(
            f"Prepared task {task_id} for {filename} "
            f"(op={op_key}, status={task_status}, large={is_large})"
        )

    db_manager.create_conversion_tasks_batch(tasks)


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

