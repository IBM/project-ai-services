import asyncio
from datetime import datetime, timezone
from enum import Enum
from functools import partial
import json
from pathlib import Path
from typing import List
import uuid
from common.misc_utils import get_logger

CACHE_DIR = "/var/cache"
DOCS_DIR = f"{CACHE_DIR}/docs"
JOBS_DIR = f"{CACHE_DIR}/jobs"

logger = get_logger("digitize_utils")

class OutputFormat(str, Enum):
    TEXT = "text"
    MD = "md"
    JSON = "json"

class OperationType(str, Enum):
    INGESTION = "ingestion"
    DIGITIZATION = "digitization"

class JobStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"

class DocStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"

def generate_job_id():
    # Generate a random UUID
    job_id = uuid.uuid4()
    job_id_hex = job_id.hex
    print(f"Hex-only ID: {job_id_hex}")
    print(f"job id : {job_id}")
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
    documents_info: List of dicts with {'id': uuid, 'name': filename, 'type': op_type}
    """
    # Create docs and jobs dirs if not present already
    Path(DOCS_DIR).mkdir(parents=True, exist_ok=True)
    Path(JOBS_DIR).mkdir(parents=True, exist_ok=True)

    submitted_at = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")

    # list to store documents in Job file
    job_documents_summary = []

    # dictionary to keep mapping of filename to document id.
    # key -> filename
    # val -> doc_id
    doc_id_dict = {}

    for doc in documents_info:
        # Create unique document id and document metadata for each files before spawning backgroundtask
        doc_id = generate_document_id(doc)
        doc_id_dict[doc] = doc_id
        logger.debug(f"Generated document id {doc_id} for the file: {doc}")

        # Create the document level metadata files (<doc_id>_metadata.json)
        doc_meta_path = Path(DOCS_DIR)/f"{doc_id}_metadata.json"
        doc_initial_data = {
            "id": doc_id,
            "name": doc,
            "type": operation,
            "status": "accepted",
            "output_format": "json",
            "completed_at": None,
            "error": "",
            "pages": 0,
            "tables": 0,
            "chunks": 0,
            "timing_in_secs": {
                "digitizing": None, "processing": None, "chunking": None, "indexing": None
            }
        }
        with open(doc_meta_path, "w") as f:
            json.dump(doc_initial_data, f, indent=4)

        # Add doc's summary to list for the Job file
        job_documents_summary.append({
            "id": doc_id,
            "name": doc,
            "status": "accepted"
        })

    # Create job status file (<job_id>_status.json)
    job_status_path = Path(JOBS_DIR) / f"{job_id}_status.json"

    job_data = {
        "job_id": job_id,
        "operation": operation,
        "status": "accepted",
        "submitted_at": submitted_at,
        "last_updated_at": submitted_at,
        "documents": job_documents_summary,
        "error": ""
    }

    with open(job_status_path, "w") as f:
        json.dump(job_data, f, indent=4)

    return doc_id_dict


async def stage_upload_files(job_id: str, files: List[dict], staging_dir: str, file_contents: List[bytes]):
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
            print(f"Successfully staged file: {filename}")

        except Exception as e:
            logger.error(f"Failed to stage {filename} for job {job_id}: {e}")
            raise
