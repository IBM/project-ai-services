import asyncio
import json
from functools import partial
from pathlib import Path
from typing import List, Optional
import uuid

from common.misc_utils import get_logger
from digitize.types import OutputFormat
from digitize.config import DOCS_DIR, JOBS_DIR, DIGITIZED_DOCS_DIR
from digitize.status import (
    get_utc_timestamp,
    create_document_metadata,
    create_job_state
)
from digitize.job import JobState, JobDocumentSummary, JobStats
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


def get_all_documents(
    status_filter: Optional[str] = None,
    name_filter: Optional[str] = None,
    docs_dir: Path = DOCS_DIR
) -> List[dict]:
    """
    Read all document metadata files, apply filters, and sort by submitted time.
    Returns minimal document information (id, name, type, status).

    Args:
        status_filter: Optional status to filter by (case-insensitive)
        name_filter: Optional name to filter by (case-insensitive partial match)
        docs_dir: Directory containing document metadata files

    Returns:
        List of minimal document info dictionaries sorted by submitted_at (most recent first)
    """
    logger.debug(f"Fetching documents with filters: status={status_filter}, name={name_filter}")

    if not docs_dir.exists():
        logger.error(f"Documents directory {docs_dir} does not exist")
        return []

    all_documents = []
    metadata_files = list(docs_dir.glob("*_metadata.json"))

    logger.debug(f"Found {len(metadata_files)} metadata files")

    for meta_file in metadata_files:
        try:
            with open(meta_file, "r") as f:
                doc_data = json.load(f)

                # Apply status filter
                if status_filter:
                    doc_status = doc_data.get("status", "").lower()
                    if doc_status != status_filter.lower():
                        continue

                # Apply name filter (case-insensitive partial match)
                if name_filter:
                    doc_name = doc_data.get("name", "").lower()
                    if name_filter.lower() not in doc_name:
                        continue

                doc_info = {
                    "id": doc_data.get("id"),
                    "name": doc_data.get("name"),
                    "type": doc_data.get("type"),
                    "status": doc_data.get("status")
                }
                # Store submitted_at for sorting but don't include in response
                doc_info["_submitted_at"] = doc_data.get("submitted_at")
                all_documents.append(doc_info)

        except json.JSONDecodeError as e:
            logger.warning(f"Failed to parse metadata file {meta_file}: {e}")
            continue
        except Exception as e:
            logger.warning(f"Error reading metadata file {meta_file}: {e}")
            continue

    # Sort by submitted_at (most recent first)
    all_documents.sort(key=lambda x: x.get("_submitted_at") or "", reverse=True)

    # Remove the temporary sorting field
    for doc in all_documents:
        doc.pop("_submitted_at", None)

    logger.debug(f"Returning {len(all_documents)} documents after filtering")
    return all_documents


def _read_document_metadata(doc_id: str, docs_dir: Path = DOCS_DIR) -> dict:
    """
    Internal helper to read and parse document metadata file.

    Args:
        doc_id: Unique identifier of the document
        docs_dir: Directory containing document metadata files

    Returns:
        Dictionary containing the parsed metadata

    Raises:
        FileNotFoundError: If document metadata file doesn't exist
        json.JSONDecodeError: If metadata file is corrupted
    """
    # Construct the metadata file path
    meta_file = docs_dir / f"{doc_id}_metadata.json"

    # Check if the document exists
    if not meta_file.exists():
        logger.error(f"Document metadata file not found: {meta_file}")
        raise FileNotFoundError(f"Document with ID '{doc_id}' not found")

    # Read the metadata file
    try:
        with open(meta_file, "r") as f:
            doc_data = json.load(f)
    except json.JSONDecodeError as e:
        logger.error(f"Failed to parse metadata file for document {doc_id}: {e}")
        raise

    return doc_data


def get_document_by_id(doc_id: str, include_details: bool = False, docs_dir: Path = DOCS_DIR) -> dict:
    """
    Read a specific document's metadata by ID and return formatted response.

    Args:
        doc_id: Unique identifier of the document
        include_details: If True, includes job_id and metadata fields
        docs_dir: Directory containing document metadata files

    Returns:
        Dictionary with document information

    Raises:
        FileNotFoundError: If document metadata file doesn't exist
        json.JSONDecodeError: If metadata file is corrupted
    """
    logger.debug(f"Fetching document {doc_id} with include_details={include_details}")

    # Read document metadata using the common helper
    doc_data = _read_document_metadata(doc_id, docs_dir)

    # Build the base response
    response = {
        "id": doc_data.get("id"),
        "job_id": doc_data.get("job_id"),
        "name": doc_data.get("name"),
        "type": doc_data.get("type"),
        "status": doc_data.get("status"),
        "output_format": doc_data.get("output_format"),
        "submitted_at": doc_data.get("submitted_at"),
        "completed_at": doc_data.get("completed_at"),
        "error": doc_data.get("error")
    }

    # If details flag is true, include additional metadata
    if include_details:
        response["metadata"] = doc_data.get("metadata", {})

    logger.debug(f"Successfully retrieved document for {doc_id}")
    return response


def get_document_content(doc_id: str, docs_dir: Path = DOCS_DIR) -> dict:
    """
    Read the digitized content of a document from the local cache.

    For documents submitted via digitization, this returns the output_format requested during POST (md/text/json).
    For documents submitted via ingestion, this defaults to returning the extracted json representation.

    Args:
        doc_id: Unique identifier of the document
        docs_dir: Directory containing document metadata files

    Returns:
        Dictionary with result and output_format

    Raises:
        FileNotFoundError: If document metadata or content file doesn't exist
        json.JSONDecodeError: If metadata or content file is corrupted
    """
    logger.debug(f"Fetching content for document {doc_id}")

    # Read document metadata using the common helper
    doc_data = _read_document_metadata(doc_id, docs_dir)

    # Get the output format from metadata (defaults to json if not specified)
    output_format = doc_data.get("output_format", "json")

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
    result = content_data

    logger.debug(f"Successfully retrieved content for document {doc_id} in {output_format} format")

    return {
        "result": result,
        "output_format": output_format
    }
