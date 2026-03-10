import asyncio
import json
from functools import partial
from pathlib import Path
from typing import List, Optional
import uuid

from common.misc_utils import get_logger
from digitize.types import OutputFormat
from digitize.config import DOCS_DIR, JOBS_DIR
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
