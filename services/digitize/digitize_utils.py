import asyncio
import json
from functools import partial
from pathlib import Path
import shutil
from typing import List, Optional
import uuid

from common.misc_utils import get_logger
from digitize.models import (
    OutputFormat,
    DocumentListItem,
    DocumentDetailResponse,
    DocumentContentResponse
)
from digitize.settings import settings
from digitize.status import (
    get_utc_timestamp,
    create_document_metadata,
    create_job_state
)
from digitize.job import JobState
from digitize.document import DocumentMetadata
from digitize.models import JobStatus

logger = get_logger("digitize_utils")

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
    from digitize.db.database import engine
    from digitize.db.repository import db_repo
    
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve document IDs without database connection.")
    
    try:
        logger.debug("Reading document IDs from database")
        documents, _ = db_repo.get_all_documents(limit=10000, offset=0)
        doc_ids = [doc.doc_id for doc in documents]
        logger.info(f"Found {len(doc_ids)} document IDs in database")
        return doc_ids
    except Exception as e:
        logger.error(f"Failed to read document IDs from database: {e}", exc_info=True)
        raise


def initialize_job_state(job_id: str, operation: str, output_format:OutputFormat, documents_info: list[str], job_name: Optional[str] = None) -> dict[str, str]:
    """
    Creates the job status file and individual document metadata files.
    Also persists to database if available.

    Args:
        job_id: Unique identifier for the job
        operation: Type of operation (e.g., 'ingestion', 'digitization')
        output_format: Output format for the documents
        documents_info: List of filenames to be processed under this job
        job_name: Optional human-readable name for the job

    Returns:
        dict[str, str]: Mapping of filename -> document_id
    """
    # Use database-aware initialization
    from digitize.db.db_utils import initialize_job_state_with_db
    
    return initialize_job_state_with_db(
        job_id=job_id,
        operation=operation,
        output_format=output_format,
        documents_info=documents_info,
        job_name=job_name
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

def read_job_from_db(job_id: str) -> Optional[JobState]:
    """
    Read and parse a single job from the database into a JobState object.

    Args:
        job_id: Unique identifier for the job.

    Returns:
        JobState object if successful, None otherwise.
    """
    from digitize.db.db_utils import get_job_from_db
    
    try:
        job_data = get_job_from_db(job_id)
        if job_data:
            return JobState(**job_data)
        return None
    except Exception as e:
        logger.error(f"Failed to read job {job_id} from database: {e}", exc_info=True)
        return None

def read_all_jobs_from_db() -> List[JobState]:
    """
    Read all jobs from the database.

    Returns:
        List of JobState objects.
    """
    from digitize.db.db_utils import get_all_jobs_from_db
    
    try:
        jobs_data, _ = get_all_jobs_from_db(limit=10000, offset=0)
        return [JobState(**job) for job in jobs_data]
    except Exception as e:
        logger.error(f"Failed to read jobs from database: {e}", exc_info=True)
        return []


def _read_document_metadata_from_db(doc_id: str) -> DocumentMetadata:
    """
    Internal helper to read and parse document metadata from database into a Pydantic model.

    Args:
        doc_id: Unique identifier of the document

    Returns:
        DocumentMetadata model with validated data

    Raises:
        FileNotFoundError: If document doesn't exist in database
        RuntimeError: If database is not available
    """
    from digitize.db.db_utils import get_document_from_db
    
    try:
        doc_data = get_document_from_db(doc_id)
        if doc_data is None:
            raise FileNotFoundError(f"Document with ID '{doc_id}' not found")
        
        # Parse and validate using Pydantic model
        return DocumentMetadata(**doc_data)
    except FileNotFoundError:
        raise
    except Exception as e:
        logger.error(f"Failed to read document {doc_id} from database: {e}", exc_info=True)
        raise


def get_all_documents(
    status_filter: Optional[str] = None,
    name_filter: Optional[str] = None
) -> List[DocumentListItem]:
    """
    Read all document metadata from database, apply filters, and sort by submitted time.
    Returns minimal document information (id, name, type, status) as Pydantic models.

    Args:
        status_filter: Optional status to filter by (case-insensitive)
        name_filter: Optional name to filter by (case-insensitive partial match)

    Returns:
        List of DocumentListItem models sorted by submitted_at (most recent first)
    """
    from digitize.db.db_utils import get_all_documents_from_db
    
    logger.debug(f"Fetching documents with filters: status={status_filter}, name={name_filter}")

    try:
        documents_data, _ = get_all_documents_from_db(
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


def get_document_by_id(doc_id: str, include_details: bool = False) -> DocumentDetailResponse:
    """
    Read a specific document's metadata by ID from database and return formatted response as Pydantic model.

    Args:
        doc_id: Unique identifier of the document
        include_details: If True, includes metadata fields

    Returns:
        DocumentDetailResponse model with document information

    Raises:
        FileNotFoundError: If document doesn't exist in database
        RuntimeError: If database is not available
    """
    from digitize.db.db_utils import get_document_from_db
    
    logger.debug(f"Fetching document {doc_id} with include_details={include_details}")

    doc_data = get_document_from_db(doc_id)
    if doc_data is None:
        raise FileNotFoundError(f"Document with ID '{doc_id}' not found")

    # Conditionally exclude metadata if not requested
    if not include_details:
        doc_data.pop('metadata', None)

    # Let Pydantic validate and convert the data
    response = DocumentDetailResponse(**doc_data)

    logger.debug(f"Successfully retrieved document for {doc_id}")
    return response


def get_document_content(doc_id: str, docs_dir: Path = settings.digitize.docs_dir) -> DocumentContentResponse:
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


    # Read document metadata using the common helper (returns DocumentMetadata)
    doc_metadata = _read_document_metadata_from_db(doc_id)

    # Get the output format from metadata
    output_format = doc_metadata.output_format.value if hasattr(doc_metadata.output_format, 'value') else str(doc_metadata.output_format)

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
    from digitize.db.db_utils import get_job_from_db
    
    logger.debug(f"Checking if document {doc_id} is part of an active job")
    
    # If document has no job_id, it's not part of any job
    if not job_id:
        logger.debug(f"Document {doc_id} has no associated job_id")
        return False
    
    logger.debug(f"Document {doc_id} is associated with job {job_id}")
    
    # Read the job status from database and check if it's in progress
    try:
        job_data = get_job_from_db(job_id)
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


def delete_document_files(doc_id: str, output_format: str, docs_dir: Path = settings.digitize.docs_dir) -> None:
    """
    Delete all files associated with a document from the cache directories.
    
    Deletion order (important for crash recovery):
    1. FIRST: Delete digitized content file
    2. LAST: Delete metadata file
    
    This ensures that if a crash occurs during deletion, the metadata file
    remains as a record, allowing for cleanup retry or manual intervention.
    
    Files deleted:
    - /var/cache/digitized/<doc_id>.<extension> (based on output_format)
    - /var/cache/docs/<doc_id>_metadata.json (LAST)
    
    Args:
        doc_id: Unique identifier of the document
        output_format: Output format of the document (txt, md, or json)
        docs_dir: Directory containing document metadata files
        
    Raises:
        FileNotFoundError: If document metadata file doesn't exist
        ValueError: If output_format is invalid
    """
    logger.debug(f"Deleting files for document {doc_id} with format {output_format}")
    
    # Check if document exists
    meta_file = docs_dir / f"{doc_id}_metadata.json"
    if not meta_file.exists():
        logger.error(f"Document metadata file not found: {meta_file}")
        raise FileNotFoundError(f"Document with ID '{doc_id}' not found")
    
    # Validate output_format against OutputFormat enum
    valid_formats = [fmt.value for fmt in OutputFormat]
    if output_format not in valid_formats:
        raise ValueError(f"Invalid output_format: '{output_format}'. Must be one of: {', '.join(valid_formats)}")

    files_deleted = []
    
    # STEP 1: Delete digitized content file FIRST
    content_file = settings.digitize.digitized_docs_dir / f"{doc_id}.{output_format}"
    if content_file.exists():
        try:
            content_file.unlink()
            files_deleted.append(str(content_file))
            logger.debug(f"✓ Deleted content file: {content_file}")
        except Exception as e:
            error_msg = f"Failed to delete content file {content_file}: {e}"
            logger.error(f"✗ {error_msg}")
            # Preserve metadata file if content deletion fails
            raise Exception(f"Failed to delete content file: {error_msg}") from e
    else:
        logger.warning(f"Content file not found (may have been deleted already): {content_file}")
    
    # STEP 2: Delete metadata file LAST (only after content files are successfully deleted)
    try:
        meta_file.unlink()
        files_deleted.append(str(meta_file))
        logger.debug(f"✓ Deleted metadata file: {meta_file}")
    except Exception as e:
        logger.error(f"✗ Failed to delete metadata file {meta_file}: {e}")
        raise
    
    logger.info(f"✅ Deleted {len(files_deleted)} files for document {doc_id}")


def has_active_jobs(operation: Optional[str] = None) -> tuple[bool, list[str]]:
    """
    Check if there are any active jobs (accepted or in_progress status) in the database.
    Optionally filter by operation type.

    Args:
        operation: Optional operation type to filter by (e.g., 'ingestion', 'digitization')

    Returns:
        Tuple of (has_active, active_job_ids) where has_active is True if any active jobs exist
    """
    from digitize.db.db_utils import get_all_jobs_from_db
    
    filter_msg = f" for operation '{operation}'" if operation else ""
    logger.debug(f"Checking for active jobs{filter_msg}")

    try:
        # Get jobs with ACCEPTED or IN_PROGRESS status
        active_job_ids = []
        
        for status in [JobStatus.ACCEPTED, JobStatus.IN_PROGRESS]:
            jobs_data, _ = get_all_jobs_from_db(
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


def bulk_delete_all_documents(docs_dir: Path = settings.digitize.docs_dir) -> dict:
    """
    Delete all documents from the system including:
    1. All digitized content files from /var/cache/digitized
    2. All document metadata files from /var/cache/docs

    This function does NOT delete job status files or reset the vector database.
    Those operations should be handled separately by the caller.

    Args:
        docs_dir: Directory containing document metadata files

    Returns:
        Dictionary with deletion statistics
    """
    logger.info("Starting bulk deletion of all documents...")

    deletion_stats = {
        "metadata_files_deleted": 0,
        "content_files_deleted": 0,
        "errors": []
    }

    # Step 1: Delete all digitized content files using the utility function
    cleanup_stats = cleanup_digitized_files()
    deletion_stats["content_files_deleted"] = cleanup_stats["content_files_deleted"]
    deletion_stats["errors"].extend(cleanup_stats["errors"])

    # Step 2: Delete all document metadata files
    if docs_dir.exists():
        try:
            # Count metadata files before deletion
            metadata_files = list(docs_dir.glob("*_metadata.json"))
            file_count = len(metadata_files)
            logger.debug(f"Found {file_count} metadata files in {docs_dir}")

            # Delete the entire directory and recreate it
            shutil.rmtree(docs_dir)
            docs_dir.mkdir(parents=True, exist_ok=True)

            deletion_stats["metadata_files_deleted"] = file_count
            logger.info(f"✓ Deleted {file_count} metadata files from {docs_dir}")
        except Exception as e:
            error_msg = f"Failed to clean up documents directory: {e}"
            logger.error(f"✗ {error_msg}")
            deletion_stats["errors"].append(error_msg)
    else:
        logger.error(f"Documents directory {docs_dir} does not exist")

    # Log summary
    total_deleted = deletion_stats["metadata_files_deleted"] + deletion_stats["content_files_deleted"]
    logger.info(
        f"✅ Bulk deletion completed: {deletion_stats['metadata_files_deleted']} metadata files, "
        f"{deletion_stats['content_files_deleted']} content files deleted (total: {total_deleted})"
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
    from digitize.db.db_status_manager import get_status_manager
    from digitize.models import JobStatus, DocStatus
    from digitize.doc_utils import clean_intermediate_files
    from digitize.db.db_utils import get_all_jobs_from_db
    import digitize.settings as config

    orphan_count = 0
    orphan_statuses = [JobStatus.ACCEPTED, JobStatus.IN_PROGRESS]

    try:
        # Scan all jobs with active statuses from database
        for status in orphan_statuses:
            jobs_data, _ = get_all_jobs_from_db(
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
