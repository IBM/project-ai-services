import asyncio
from datetime import datetime, timezone
from functools import partial
import json
from pathlib import Path
from typing import List
import uuid
from common.misc_utils import get_logger
from digitize.types import OutputFormat, OperationType, JobStatus, DocStatus
from digitize.document import DocumentMetadata, TimingInfo
from digitize.job import JobState, JobDocumentSummary

CACHE_DIR = "/var/cache"
DOCS_DIR = f"{CACHE_DIR}/docs"
JOBS_DIR = f"{CACHE_DIR}/jobs"

logger = get_logger("digitize_utils")

def generate_job_id():
    # Generate a random UUID
    job_id = uuid.uuid4()
    logger.debug(f"job id : {job_id}")
    return str(job_id)


def generate_document_id(filename):
    """
    Generate UUID based document_id based on filename, helps preventing duplicate document records 
    """
    # Define a fixed Namespace: use any valid UUID
    NAMESPACE_INGESTION = uuid.UUID('6ba7b810-9dad-11d1-80b4-00c04fd430c8')

    # Generate deterministic UUID
    document_id = uuid.uuid5(NAMESPACE_INGESTION, filename)
    return str(document_id)


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
        # Generate a deterministic document id from the filename
        doc_id = generate_document_id(doc)
        doc_id_dict[doc] = doc_id
        logger.debug(f"Generated document id {doc_id} for the file: {doc}")

        # Build and persist the DocumentMetadata object
        doc_metadata = DocumentMetadata(
            id=doc_id,
            name=doc,
            type=operation,
            status=DocStatus.ACCEPTED,
            output_format=OutputFormat.JSON,
            timing_in_secs=TimingInfo(),
        )
        doc_meta_path = doc_metadata.save(DOCS_DIR)
        logger.debug(f"Created document metadata file: {doc_meta_path}")

        # Collect a compact summary for the job status file
        job_doc_summaries.append(
            JobDocumentSummary(id=doc_id, name=doc, status=JobStatus.ACCEPTED)
        )

    # Build and persist the JobState object
    job_state = JobState(
        job_id=job_id,
        operation=operation,
        submitted_at=submitted_at,
        status=JobStatus.ACCEPTED,
        documents=job_doc_summaries,
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
