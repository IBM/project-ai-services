import asyncio
from datetime import datetime, timezone
from functools import partial
import json
from pathlib import Path
from typing import List
import uuid
from common.misc_utils import get_logger
from digitize.types import OutputFormat, OperationType, JobStatus, DocStatus
from digitize.document import DocumentMetadata
from digitize.job import JobState, JobDocumentSummary

CACHE_DIR = "/var/cache"
DOCS_DIR = f"{CACHE_DIR}/docs"
JOBS_DIR = f"{CACHE_DIR}/jobs"

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


def initialize_job_state(job_id: str, operation: str, documents_info: list):
    """
    Creates the job status file and individual document metadata files.

    documents_info: List of filenames to be processed under this job.

    Returns:
        doc_id_dict: mapping of filename -> document_id
    """
    submitted_at = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")

    # dictionary to keep mapping of filename to document id.
    # key -> filename, val -> doc_id
    doc_id_dict = {}

    # Collect per-document summaries for the job state
    job_doc_summaries = []

    for doc in documents_info:
        # Generate a random document id
        doc_id = generate_uuid()
        doc_id_dict[doc] = doc_id
        logger.debug(f"Generated document id {doc_id} for the file: {doc}")

        # Build and persist the DocumentMetadata object with new structure
        doc_metadata = DocumentMetadata(
            id=doc_id,
            name=doc,
            type=operation,
            status=DocStatus.ACCEPTED,
            output_format=OutputFormat.JSON,
            submitted_at=submitted_at,
            completed_at=None,
            error=None,
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
        doc_meta_path = doc_metadata.save(DOCS_DIR)
        logger.debug(f"Created document metadata file: {doc_meta_path}")

        # Collect a compact summary for the job status file
        job_doc_summaries.append(
            JobDocumentSummary(id=doc_id, name=doc, status="accepted")
        )

    # Build and persist the JobState object with new structure
    from digitize.job import JobStats

    job_state = JobState(
        job_id=job_id,
        operation=operation,
        status=JobStatus.ACCEPTED,
        submitted_at=submitted_at,
        completed_at=None,
        documents=job_doc_summaries,
        stats=JobStats(
            total_documents=len(documents_info),
            completed=0,
            failed=0,
            in_progress=0
        ),
        error=None
    )
    job_status_path = job_state.save(JOBS_DIR)
    logger.debug(f"Created job status file: {job_status_path}")

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
