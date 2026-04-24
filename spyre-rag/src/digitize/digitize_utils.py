import asyncio
import json
from functools import partial
from pathlib import Path
import shutil
from typing import List, Optional
import uuid

from common.misc_utils import get_logger
from digitize.types import (
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
from digitize.types import JobStatus

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


def get_all_document_ids(docs_dir: Path = settings.digitize.docs_dir) -> list[str]:
    """
    Read all document IDs from metadata files in the docs directory.

    Args:
        docs_dir: Directory containing document metadata files

    Returns:
        List of document IDs found in metadata files
    """
    doc_ids = []
    try:
        logger.debug(f"Reading document IDs from {docs_dir}")
        if docs_dir.exists():
            for metadata_file in docs_dir.glob("*_metadata.json"):
                try:
                    with open(metadata_file, 'r') as f:
                        metadata = json.load(f)
                        doc_id = metadata.get('id')
                        if doc_id:
                            doc_ids.append(doc_id)
                except Exception as e:
                    logger.warning(f"Failed to read metadata from {metadata_file.name}: {e}")
            logger.info(f"Found {len(doc_ids)} document IDs in {docs_dir}")
        else:
            logger.warning(f"Directory {docs_dir} does not exist")
    except Exception as e:
        logger.error(f"Failed to read document IDs from {docs_dir}: {e}")

    return doc_ids


def initialize_job_state(job_id: str, operation: str, output_format:OutputFormat, documents_info: list[str], job_name: Optional[str] = None) -> dict[str, str]:
    """
    Creates the job status file and individual document metadata files.

    Args:
        job_id: Unique identifier for the job
        operation: Type of operation (e.g., 'ingestion', 'digitization')
        output_format: Output format for the documents
        documents_info: List of filenames to be processed under this job
        job_name: Optional human-readable name for the job

    Returns:
        dict[str, str]: Mapping of filename -> document_id
    """
    submitted_at = get_utc_timestamp()
    
    # Generate document IDs upfront using dictionary comprehension
    doc_id_dict = {doc: generate_uuid() for doc in documents_info}

    # Create and persist document metadata files
    for doc in documents_info:
        doc_id = doc_id_dict[doc]
        logger.debug(f"Generated document id {doc_id} for the file: {doc}")
        create_document_metadata(doc, doc_id, job_id, output_format, operation, submitted_at, settings.digitize.docs_dir)

    # Create and persist the job state file
    create_job_state(job_id, operation, submitted_at, doc_id_dict, documents_info, settings.digitize.jobs_dir, job_name)

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

def read_job_file(file_path: Path) -> Optional[JobState]:
    """
    Read and parse a single job status JSON file into a JobState object.
    
    Uses Pydantic for automatic validation and deserialization with built-in
    error handling and type coercion.

    Args:
        file_path: Path to the job status JSON file.

    Returns:
        JobState object if successful, None otherwise.
    """
    # Validate file exists and is readable
    if not file_path.exists():
        logger.error(f"Job file does not exist: {file_path}")
        return None
    
    if not file_path.is_file():
        logger.error(f"Path is not a file: {file_path}")
        return None
    
    try:
        # Read and parse JSON
        with open(file_path, "r", encoding="utf-8") as f:
            data = json.load(f)
        
        # Pydantic handles all validation, type conversion, and required field checks
        return JobState(**data)
        
    except json.JSONDecodeError as e:
        logger.error(f"Invalid JSON in job file {file_path.name}: {e}")
        return None
    except (IOError, OSError, PermissionError) as e:
        logger.error(f"Failed to read job file {file_path.name}: {e}")
        return None
    except Exception as e:
        logger.error(
            f"Failed to parse job file {file_path.name}: {e}",
            exc_info=True
        )
        return None

def read_all_job_files() -> List[JobState]:
    """
    Read all job status JSON files from the jobs directory.

    Args:
        jobs_dir: Path to the directory containing job status files.

    Returns:
        List of JobState objects. Files that fail to parse are skipped.
    """

    if not settings.digitize.jobs_dir.exists() or not settings.digitize.jobs_dir.is_dir():
        return []

    jobs = []
    for file_path in settings.digitize.jobs_dir.glob("*_status.json"):
        if not file_path.is_file():
            continue
        job_state = read_job_file(file_path)
        if job_state is not None:
            jobs.append(job_state)

    return jobs


def _read_document_metadata(doc_id: str, docs_dir: Path = settings.digitize.docs_dir) -> DocumentMetadata:
    """
    Internal helper to read and parse document metadata file into a Pydantic model.

    Args:
        doc_id: Unique identifier of the document
        docs_dir: Directory containing document metadata files

    Returns:
        DocumentMetadata model with validated data

    Raises:
        FileNotFoundError: If document metadata file doesn't exist
        json.JSONDecodeError: If metadata file is corrupted
        ValidationError: If metadata doesn't match expected schema
    """

    # Construct the metadata file path
    meta_file = docs_dir / f"{doc_id}_metadata.json"

    # Check if the document exists
    if not meta_file.exists():
        logger.error(f"Document metadata file not found: {meta_file}")
        raise FileNotFoundError(f"Document with ID '{doc_id}' not found")

    # Read and parse the metadata file using Pydantic
    try:
        with open(meta_file, "r", encoding="utf-8") as f:
            doc_data = json.load(f)

        # Parse and validate using Pydantic model
        return DocumentMetadata(**doc_data)

    except json.JSONDecodeError as e:
        logger.error(f"Failed to parse metadata file for document {doc_id}: {e}")
        raise


def get_all_documents(
    status_filter: Optional[str] = None,
    name_filter: Optional[str] = None,
    docs_dir: Path = settings.digitize.docs_dir
) -> List[DocumentListItem]:
    """
    Read all document metadata files, apply filters, and sort by submitted time.
    Returns minimal document information (id, name, type, status) as Pydantic models.

    Args:
        status_filter: Optional status to filter by (case-insensitive)
        name_filter: Optional name to filter by (case-insensitive partial match)
        docs_dir: Directory containing document metadata files

    Returns:
        List of DocumentListItem models sorted by submitted_at (most recent first)
    """
    logger.debug(f"Fetching documents with filters: status={status_filter}, name={name_filter}")

    if not docs_dir.exists():
        logger.error(f"Documents directory {docs_dir} does not exist")
        return []

    all_documents = []
    metadata_files = list(docs_dir.glob("*_metadata.json"))

    logger.debug(f"Found {len(metadata_files)} metadata files")

    for meta_file in metadata_files:
        # Extract document ID from filename (format: {doc_id}_metadata.json)
        doc_id = meta_file.stem.replace("_metadata", "")

        try:
            doc_metadata = _read_document_metadata(doc_id, docs_dir)

            # Apply status filter
            if status_filter:
                doc_status = doc_metadata.status.value if hasattr(doc_metadata.status, 'value') else str(doc_metadata.status)
                if doc_status.lower() != status_filter.lower():
                    continue

            # Apply name filter (case-insensitive partial match)
            if name_filter:
                if name_filter.lower() not in doc_metadata.name.lower():
                    continue

            doc_item = DocumentListItem(**doc_metadata.model_dump())

            # Store submitted_at for sorting
            all_documents.append((doc_metadata.submitted_at or "", doc_item))

        except (FileNotFoundError, json.JSONDecodeError) as e:
            logger.error(f"Failed to read metadata file {meta_file}: {e}")
            continue
        except Exception as e:
            logger.error(f"Error reading metadata file {meta_file}: {e}")
            continue

    # Sort by submitted_at (most recent first) and extract DocumentListItem
    all_documents.sort(key=lambda x: x[0], reverse=True)
    result = [doc_item for _, doc_item in all_documents]

    logger.debug(f"Returning {len(result)} documents after filtering")
    return result


def get_document_by_id(doc_id: str, include_details: bool = False, docs_dir: Path = settings.digitize.docs_dir) -> DocumentDetailResponse:
    """
    Read a specific document's metadata by ID and return formatted response as Pydantic model.

    Args:
        doc_id: Unique identifier of the document
        include_details: If True, includes metadata fields
        docs_dir: Directory containing document metadata files

    Returns:
        DocumentDetailResponse model with document information

    Raises:
        FileNotFoundError: If document metadata file doesn't exist
        json.JSONDecodeError: If metadata file is corrupted
        ValidationError: If metadata doesn't match expected schema
    """
    logger.debug(f"Fetching document {doc_id} with include_details={include_details}")

    doc_metadata = _read_document_metadata(doc_id, docs_dir)

    doc_dict = doc_metadata.model_dump()

    # Conditionally exclude metadata if not requested
    if not include_details:
        doc_dict.pop('metadata', None)

    # Let Pydantic validate and convert the data
    response = DocumentDetailResponse(**doc_dict)

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
    doc_metadata = _read_document_metadata(doc_id, docs_dir)

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

def is_document_in_active_job(doc_id: str, job_id: Optional[str], jobs_dir: Path = settings.digitize.jobs_dir) -> bool:
    """
    Check if a document is part of any active job (in_progress status).
    
    This function efficiently checks by directly accessing the job file
    at /var/cache/jobs/{job_id}_status.json instead of iterating through all jobs.
    
    Args:
        doc_id: Unique identifier of the document
        job_id: Job ID from document metadata (can be None if document has no associated job)
        jobs_dir: Directory containing job status files
        
    Returns:
        True if document is in an active job, False otherwise
    """
    logger.debug(f"Checking if document {doc_id} is part of an active job")
    
    # If document has no job_id, it's not part of any job
    if not job_id:
        logger.debug(f"Document {doc_id} has no associated job_id")
        return False
    
    logger.debug(f"Document {doc_id} is associated with job {job_id}")
    
    # Check if the job file exists
    if not jobs_dir.exists():
        logger.debug(f"Jobs directory {jobs_dir} does not exist")
        return False
    
    job_file = jobs_dir / f"{job_id}_status.json"
    if not job_file.exists():
        logger.debug(f"Job file {job_file} does not exist")
        return False
    
    # Read the job status and check if it's in progress
    try:
        with open(job_file, "r") as f:
            job_data = json.load(f)
        
        job_status = job_data.get("status", "").lower()
        if job_status == JobStatus.IN_PROGRESS.value:
            logger.info(f"Document {doc_id} is part of active job {job_id}")
            return True
        else:
            logger.debug(f"Job {job_id} exists but is not in progress (status: {job_status})")
            return False
            
    except (json.JSONDecodeError, Exception) as e:
        logger.error(f"Error reading job file {job_file}: {e}")
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


def has_active_jobs(operation: Optional[str] = None, jobs_dir: Path = settings.digitize.jobs_dir) -> tuple[bool, list[str]]:
    """
    Check if there are any active jobs (accepted or in_progress status).
    Optionally filter by operation type.

    Args:
        operation: Optional operation type to filter by (e.g., 'ingestion', 'digitization')
        jobs_dir: Directory containing job status files

    Returns:
        Tuple of (has_active, active_job_ids) where has_active is True if any active jobs exist
    """
    filter_msg = f" for operation '{operation}'" if operation else ""
    logger.debug(f"Checking for active jobs{filter_msg}")

    if not jobs_dir.exists():
        logger.debug(f"Jobs directory {jobs_dir} does not exist")
        return False, []

    active_job_ids = []
    job_files = list(jobs_dir.glob("*_status.json"))

    for job_file in job_files:
        try:
            with open(job_file, "r") as f:
                job_data = json.load(f)

            # Filter by operation if specified
            if operation:
                job_operation = job_data.get("operation", "").lower()
                if job_operation != operation.lower():
                    continue

            job_status = job_data.get("status", "").lower()
            if job_status in [JobStatus.ACCEPTED.value, JobStatus.IN_PROGRESS.value]:
                job_id = job_data.get("job_id", job_file.stem.replace("_status", ""))
                active_job_ids.append(job_id)
                logger.debug(f"Found active job: {job_id} with status {job_status}")

        except (json.JSONDecodeError, Exception) as e:
            logger.error(f"Error reading job file {job_file}: {e}")
            continue

    has_active = len(active_job_ids) > 0
    if has_active:
        logger.info(f"Found {len(active_job_ids)} active job(s){filter_msg}: {active_job_ids}")
    else:
        logger.debug(f"No active jobs found{filter_msg}")

    return has_active, active_job_ids

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


def scan_and_recover_orphan_jobs(jobs_dir: Path = settings.digitize.jobs_dir) -> int:
    """
    Boot-up scan to identify and mark orphan jobs as failed.

    An orphan job is one with status 'accepted' or 'in_progress' that exists
    when the application starts, indicating the previous instance crashed
    while processing it.

    This method:
    1. Updates document metadata files first using update_doc_metadata
    2. Updates documents in in-progress states to failed
    3. Updates job status using update_job_progress

    Args:
        jobs_dir: Directory containing job status JSON files

    Returns:
        Number of orphan jobs recovered
    """
    from digitize.status import StatusManager
    from digitize.types import JobStatus, DocStatus
    from digitize.doc_utils import clean_intermediate_files
    import digitize.settings as config

    if not jobs_dir.exists():
        logger.warning(f"Jobs directory does not exist: {jobs_dir}")
        return 0

    orphan_count = 0
    orphan_statuses = {JobStatus.ACCEPTED.value, JobStatus.IN_PROGRESS.value}

    try:
        # Scan all job statuses(*_status.json) files in the jobs directory
        for job_file in jobs_dir.glob("*_status.json"):
            try:
                with open(job_file, "r") as f:
                    job_data = json.load(f)

                current_status = job_data.get("status")

                # Check if this is an orphan job
                if current_status in orphan_statuses:
                    job_id = job_data.get("job_id", job_file.stem.replace("_status", ""))
                    logger.warning(f"Found orphan job: {job_id} with status '{current_status}'")

                    # Create StatusManager instance for this job
                    status_mgr = StatusManager(job_id)

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
                                    
                                    # Update individual document metadata file using update_doc_metadata
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

            except json.JSONDecodeError as e:
                logger.error(f"Failed to parse job file {job_file}: {e}")
            except Exception as e:
                logger.error(f"Error processing job file {job_file}: {e}")

    except Exception as e:
        logger.error(f"Error scanning jobs directory: {e}")

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
