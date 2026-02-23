import asyncio
from enum import Enum
from functools import partial
import uuid

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

def generate_job_id():
    # Generate a random UUID
    job_id = uuid.uuid4()
    job_id_hex = job_id.hex
    print(f"Hex-only ID: {job_id_hex}")


# Use file filename to compute uuid, helps preventing duplicate document records 
def generate_document_id(filename):
    """
    Generate UUID based document_id based on filename, helps preventing duplicate document records 
    """
    # Define a fixed Namespace: use any valid UUID
    NAMESPACE_INGESTION = uuid.UUID('6ba7b810-9dad-11d1-80b4-00c04fd430c8')

    # Generate deterministic UUID
    document_id = uuid.uuid5(NAMESPACE_INGESTION, filename)
    return str(document_id)

# Initializes document dictionary for given file which gets encompassed in status.json
def initialize_document(filename):
    doc_entry = {
        filename: {
            "status": "processing",
            "stats": {"num_pages": 0, "num_tables": 0, "num_chunks": 0, "timings": {}}
        }
    }
    return {"documents": doc_entry}

async def stage_upload_files(job_id: str, files: List[dict], staging_dir: str):
    def save_sync(path, content):
        with open(path, "wb") as f:
            f.write(content)

    loop = asyncio.get_running_loop()

    for file in files:
        # offload the blocking file write to a thread pools
        await loop.run_in_executor(
            None, 
            partial(save_sync, staging_dir / file["filename"], file["content"])
        )
