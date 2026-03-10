import asyncio
import json
from functools import partial
from pathlib import Path
from typing import List, Optional
import uuid

from common.misc_utils import get_logger
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


def initialize_job_state(job_id: str, operation: str, documents_info: list[str]) -> dict[str, str]:
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
        create_document_metadata(doc, doc_id, job_id, operation, submitted_at, DOCS_DIR)

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

    Args:
        file_path: Path to the job status JSON file.

    Returns:
        JobState object, or None if the file cannot be read/parsed.
    """
    try:
        with open(file_path, "r") as f:
            data = json.load(f)
        
        # Validate required fields
        required_fields = ["job_id", "operation", "status", "submitted_at"]
        missing_fields = [field for field in required_fields if field not in data]
        if missing_fields:
            logger.warning(f"Job file {file_path.name} missing required fields: {missing_fields}")
            return None
        
        # Parse documents list
        documents = []
        if "documents" in data and isinstance(data["documents"], list):
            for doc_data in data["documents"]:
                try:
                    if isinstance(doc_data, dict) and all(k in doc_data for k in ["id", "name", "status"]):
                        documents.append(JobDocumentSummary(
                            id=doc_data["id"],
                            name=doc_data["name"],
                            status=doc_data["status"]
                        ))
                except Exception as e:
                    logger.warning(f"Failed to parse document in {file_path.name}: {e}")
                    continue
        
        # Parse stats
        stats = JobStats()
        if "stats" in data and isinstance(data["stats"], dict):
            stats_data = data["stats"]
            stats = JobStats(
                total_documents=stats_data.get("total_documents", 0),
                completed=stats_data.get("completed", 0),
                failed=stats_data.get("failed", 0),
                in_progress=stats_data.get("in_progress", 0)
            )
        
        # Parse status enum
        try:
            job_status = JobStatus(data["status"])
        except ValueError:
            logger.warning(f"Invalid status '{data['status']}' in {file_path.name}, defaulting to ACCEPTED")
            job_status = JobStatus.ACCEPTED
        
        # Create JobState object
        job_state = JobState(
            job_id=data["job_id"],
            operation=data["operation"],
            status=job_status,
            submitted_at=data["submitted_at"],
            completed_at=data.get("completed_at"),
            documents=documents,
            stats=stats,
            error=data.get("error")
        )
        
        return job_state
        
    except json.JSONDecodeError as e:
        logger.warning(f"Failed to parse job file {file_path.name}: {e}")
        return None
    except (IOError, OSError) as e:
        logger.warning(f"Failed to read job file {file_path.name}: {e}")
        return None
    except KeyError as e:
        logger.warning(f"Missing required field in job file {file_path.name}: {e}")
        return None
    except Exception as e:
        logger.warning(f"Unexpected error parsing job file {file_path.name}: {e}")
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