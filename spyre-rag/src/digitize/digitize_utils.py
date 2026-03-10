import asyncio
import json
from functools import partial
from pathlib import Path
from typing import List, Optional
import uuid

from common.misc_utils import get_logger
from digitize.types import (
    OutputFormat,
    DocumentListItem,
    DocumentDetailResponse,
    DocumentContentResponse
)
from digitize.config import DOCS_DIR, JOBS_DIR, DIGITIZED_DOCS_DIR
from digitize.status import (
    get_utc_timestamp,
    create_document_metadata,
    create_job_state
)
from digitize.job import JobState, JobDocumentSummary, JobStats
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


def initialize_job_state(job_id: str, operation: str, output_format:OutputFormat, documents_info: list[str]) -> dict[str, str]:
    """
    Creates the job status file and individual document metadata files.

    Args:
        job_id: Unique identifier for the job
        operation: Type of operation (e.g., 'ingestion', 'digitization')
        documents_info: List of filenames to be processed under this job

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
        create_document_metadata(doc, doc_id, job_id, output_format, operation, submitted_at, DOCS_DIR)

    # Create and persist the job state file
    create_job_state(job_id, operation, submitted_at, doc_id_dict, documents_info, JOBS_DIR)

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
        logger.warning(f"Job file does not exist: {file_path}")
        return None
    
    if not file_path.is_file():
        logger.warning(f"Path is not a file: {file_path}")
        return None
    
    try:
        # Read and parse JSON
        with open(file_path, "r", encoding="utf-8") as f:
            data = json.load(f)
        
        # Pydantic handles all validation, type conversion, and required field checks
        return JobState(**data)
        
    except json.JSONDecodeError as e:
        logger.warning(f"Invalid JSON in job file {file_path.name}: {e}")
        return None
    except (IOError, OSError, PermissionError) as e:
        logger.warning(f"Failed to read job file {file_path.name}: {e}")
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

    if not JOBS_DIR.exists() or not JOBS_DIR.is_dir():
        return []

    jobs = []
    for file_path in JOBS_DIR.glob("*_status.json"):
        if not file_path.is_file():
            continue
        job_state = read_job_file(file_path)
        if job_state is not None:
            jobs.append(job_state)

    return jobs


def _read_document_metadata(doc_id: str, docs_dir: Path = DOCS_DIR) -> DocumentMetadata:
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
    docs_dir: Path = DOCS_DIR
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
            logger.warning(f"Failed to read metadata file {meta_file}: {e}")
            continue
        except Exception as e:
            logger.warning(f"Error reading metadata file {meta_file}: {e}")
            continue

    # Sort by submitted_at (most recent first) and extract DocumentListItem
    all_documents.sort(key=lambda x: x[0], reverse=True)
    result = [doc_item for _, doc_item in all_documents]

    logger.debug(f"Returning {len(result)} documents after filtering")
    return result


def get_document_by_id(doc_id: str, include_details: bool = False, docs_dir: Path = DOCS_DIR) -> DocumentDetailResponse:
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


def get_document_content(doc_id: str, docs_dir: Path = DOCS_DIR) -> DocumentContentResponse:
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
    content_file = DIGITIZED_DOCS_DIR / f"{doc_id}.{file_extension}"

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

def is_document_in_active_job(doc_id: str, jobs_dir: Path = JOBS_DIR) -> tuple[bool, Optional[str]]:
    """
    Check if a document is part of any active job (in_progress status).
    
    Args:
        doc_id: Unique identifier of the document
        jobs_dir: Directory containing job status files
        
    Returns:
        Tuple of (is_active, job_id) where is_active is True if document is in an active job
    """
    logger.debug(f"Checking if document {doc_id} is part of an active job")
    
    if not jobs_dir.exists():
        logger.debug(f"Jobs directory {jobs_dir} does not exist")
        return False, None
    
    # Get all job status files
    job_files = list(jobs_dir.glob("*_status.json"))
    
    for job_file in job_files:
        try:
            with open(job_file, "r") as f:
                job_data = json.load(f)
            
            # Check if job is in progress
            job_status = job_data.get("status", "").lower()
            if job_status == JobStatus.IN_PROGRESS.value:
                # Check if this document is part of this job
                documents = job_data.get("documents", [])
                for doc in documents:
                    if doc.get("id") == doc_id:
                        job_id = job_data.get("job_id")
                        logger.info(f"Document {doc_id} is part of active job {job_id}")
                        return True, job_id
                        
        except (json.JSONDecodeError, Exception) as e:
            logger.warning(f"Error reading job file {job_file}: {e}")
            continue
    
    logger.debug(f"Document {doc_id} is not part of any active job")
    return False, None


def delete_document_files(doc_id: str, docs_dir: Path = DOCS_DIR) -> None:
    """
    Delete all files associated with a document from the cache directories.
    
    Deletion order (important for crash recovery):
    1. FIRST: Delete digitized content files (.json, .md, .text)
    2. LAST: Delete metadata file
    
    This ensures that if a crash occurs during deletion, the metadata file
    remains as a record, allowing for cleanup retry or manual intervention.
    
    Files deleted:
    - /var/cache/digitized/<doc_id>.json
    - /var/cache/digitized/<doc_id>.md
    - /var/cache/digitized/<doc_id>.text
    - /var/cache/docs/<doc_id>_metadata.json (LAST)
    
    Args:
        doc_id: Unique identifier of the document
        docs_dir: Directory containing document metadata files
        
    Raises:
        FileNotFoundError: If document metadata file doesn't exist
    """
    logger.debug(f"Deleting files for document {doc_id}")
    
    # Check if document exists
    meta_file = docs_dir / f"{doc_id}_metadata.json"
    if not meta_file.exists():
        logger.error(f"Document metadata file not found: {meta_file}")
        raise FileNotFoundError(f"Document with ID '{doc_id}' not found")
    
    files_deleted = []
    files_not_found = []
    content_deletion_errors = []
    
    # STEP 1: Delete digitized content files FIRST (json, md, text)
    for extension in ["json", "md", "text"]:
        content_file = DIGITIZED_DOCS_DIR / f"{doc_id}.{extension}"
        if content_file.exists():
            try:
                content_file.unlink()
                files_deleted.append(str(content_file))
                logger.debug(f"✓ Deleted content file: {content_file}")
            except Exception as e:
                error_msg = f"Failed to delete content file {content_file}: {e}"
                logger.warning(f"✗ {error_msg}")
                content_deletion_errors.append(error_msg)
        else:
            files_not_found.append(str(content_file))
    
    # If content file deletion had errors, raise exception before deleting metadata
    if content_deletion_errors:
        error_summary = "; ".join(content_deletion_errors)
        logger.error(f"Content file deletion failed, preserving metadata file: {error_summary}")
        raise Exception(f"Failed to delete content files: {error_summary}")
    
    # STEP 2: Delete metadata file LAST (only after content files are successfully deleted)
    try:
        meta_file.unlink()
        files_deleted.append(str(meta_file))
        logger.debug(f"✓ Deleted metadata file: {meta_file}")
    except Exception as e:
        logger.error(f"✗ Failed to delete metadata file {meta_file}: {e}")
        raise
    
    logger.info(f"✅ Deleted {len(files_deleted)} files for document {doc_id}")
    if files_not_found:
        logger.debug(f"Files not found (already deleted or never created): {files_not_found}")


def cleanup_orphaned_files(docs_dir: Path = DOCS_DIR) -> dict:
    """
    Cleanup orphaned files from incomplete deletion operations.
    
    This function is designed to run at application startup to handle partial
    deletions that may have occurred due to crashes. It identifies and cleans up:
    
    1. Orphaned digitized files (content exists but no metadata)
    2. Orphaned metadata files (metadata exists but no content files)
    
    The cleanup strategy:
    - If metadata exists but ALL content files are missing: Delete metadata (deletion was nearly complete)
    - If content files exist but metadata is missing: Delete content files (orphaned content)
    
    Args:
        docs_dir: Directory containing document metadata files
        
    Returns:
        Dictionary with cleanup statistics
    """
    logger.info("Starting orphaned files cleanup...")
    
    cleanup_stats = {
        "orphaned_metadata_removed": 0,
        "orphaned_content_removed": 0,
        "errors": []
    }
    
    if not docs_dir.exists():
        logger.warning(f"Documents directory {docs_dir} does not exist, skipping cleanup")
        return cleanup_stats
    
    if not DIGITIZED_DOCS_DIR.exists():
        logger.warning(f"Digitized directory {DIGITIZED_DOCS_DIR} does not exist, skipping cleanup")
        return cleanup_stats
    
    # Get all metadata files
    metadata_files = list(docs_dir.glob("*_metadata.json"))
    logger.debug(f"Found {len(metadata_files)} metadata files to check")
    
    # Check each metadata file for orphaned content
    for meta_file in metadata_files:
        try:
            # Extract doc_id from filename (format: <doc_id>_metadata.json)
            doc_id = meta_file.stem.replace("_metadata", "")
            
            # Check if any content files exist
            content_files_exist = []
            for extension in ["json", "md", "text"]:
                content_file = DIGITIZED_DOCS_DIR / f"{doc_id}.{extension}"
                if content_file.exists():
                    content_files_exist.append(str(content_file))
            
            # If metadata exists but NO content files exist, it's likely a partial deletion
            # Delete the orphaned metadata file
            if not content_files_exist:
                try:
                    meta_file.unlink()
                    cleanup_stats["orphaned_metadata_removed"] += 1
                    logger.info(f"✓ Removed orphaned metadata file: {meta_file.name}")
                except Exception as e:
                    error_msg = f"Failed to remove orphaned metadata {meta_file.name}: {e}"
                    logger.error(f"✗ {error_msg}")
                    cleanup_stats["errors"].append(error_msg)
                    
        except Exception as e:
            error_msg = f"Error processing metadata file {meta_file.name}: {e}"
            logger.error(error_msg)
            cleanup_stats["errors"].append(error_msg)
    
    # Check for orphaned content files (content exists but no metadata)
    for extension in ["json", "md", "text"]:
        content_files = list(DIGITIZED_DOCS_DIR.glob(f"*.{extension}"))
        
        for content_file in content_files:
            try:
                # Extract doc_id from filename
                doc_id = content_file.stem
                
                # Check if metadata file exists
                meta_file = docs_dir / f"{doc_id}_metadata.json"
                
                if not meta_file.exists():
                    # Orphaned content file - delete it
                    try:
                        content_file.unlink()
                        cleanup_stats["orphaned_content_removed"] += 1
                        logger.info(f"✓ Removed orphaned content file: {content_file.name}")
                    except Exception as e:
                        error_msg = f"Failed to remove orphaned content {content_file.name}: {e}"
                        logger.error(f"✗ {error_msg}")
                        cleanup_stats["errors"].append(error_msg)
                        
            except Exception as e:
                error_msg = f"Error processing content file {content_file.name}: {e}"
                logger.error(error_msg)
                cleanup_stats["errors"].append(error_msg)
    
    # Log summary
    total_cleaned = cleanup_stats["orphaned_metadata_removed"] + cleanup_stats["orphaned_content_removed"]
    if total_cleaned > 0:
        logger.info(
            f"✅ Cleanup completed: {cleanup_stats['orphaned_metadata_removed']} metadata files, "
            f"{cleanup_stats['orphaned_content_removed']} content files removed"
        )
    else:
        logger.info("✅ No orphaned files found")
    
    if cleanup_stats["errors"]:
        logger.warning(f"Cleanup completed with {len(cleanup_stats['errors'])} errors")
    
    return cleanup_stats
