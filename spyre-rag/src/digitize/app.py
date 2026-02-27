import asyncio
import logging
import os
import uuid
from enum import Enum
from typing import List, Optional
import uvicorn

from fastapi import FastAPI, UploadFile, File, HTTPException, BackgroundTasks, Query, status
from common.misc_utils import get_logger, set_log_level

log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif not "info" in level:
        logging.warning(f"Unknown LOG_LEVEL passed: '{level}', defaulting to INFO.")

set_log_level(log_level)
logger = get_logger("app")

app = FastAPI(title="Digitize Documents Service")

# Semaphores for concurrency limiting
digitization_semaphore = asyncio.BoundedSemaphore(2)
ingestion_semaphore = asyncio.BoundedSemaphore(1)

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

async def digitize_documents(job_id: str, filenames: List[str], output_format: OutputFormat):
    try:
        # Business logic for document conversion.
        pass
    except Exception as e:
        logger.error(f"Error in job {job_id}: {e}")
    finally:
        # Crucial: Always release the semaphore slot back to the API
        digitization_semaphore.release()
        logger.debug(f"Semaphore slot released from digitization job {job_id}")


async def ingest_documents(job_id: str, filenames: List[str]):
    try:
        # Business logic for document conversion.
        pass
    except Exception as e:
        logger.error(f"Error in job {job_id}: {e}")
    finally:
        # Crucial: Always release the semaphore slot back to the API
        ingestion_semaphore.release()
        logger.debug(f"Semaphore slot released from ingestionjob {job_id}")

@app.post("/v1/documents", status_code=status.HTTP_202_ACCEPTED)
async def digitize_document(
    background_tasks: BackgroundTasks,
    files: List[UploadFile] = File(...),
    operation: OperationType = Query(OperationType.INGESTION),
    output_format: OutputFormat = Query(OutputFormat.JSON)
):
    sem = ingestion_semaphore if operation == OperationType.INGESTION else digitization_semaphore
    
    # 1. Fail fast if limit reached
    if sem.locked():
        raise HTTPException(
            status_code=status.HTTP_429_TOO_MANY_REQUESTS,
            detail=f"Too many concurrent {operation} requests."
        )

    # 2. Validation
    if operation == OperationType.DIGITIZATION and len(files) > 1:
        raise HTTPException(status_code=400, detail="Only 1 file allowed for digitization.")

    # 3. Reserve the slot
    await sem.acquire()

    job_id = str(uuid.uuid4())
    filenames = [f.filename for f in files]

    # 4. Schedule the background pipeline
    if operation == OperationType.INGESTION:
        background_tasks.add_task(ingest_documents, job_id, filenames)
    else:
        background_tasks.add_task(digitize_documents, job_id, filenames, output_format)
    
    return {"job_id": job_id}

@app.get("/v1/documents/jobs")
async def get_all_jobs(
    latest: bool = False,
    limit: int = 20,
    offset: int = 0,
    status: Optional[JobStatus] = None
):
    return {"pagination": {"total": 0, "limit": limit, "offset": offset}, "data": []}

@app.get("/v1/documents/jobs/{job_id}")
async def get_job_by_id(job_id: str):
    # Logic to read /var/cache/{job_id}_status.json
    return {}

@app.get("/v1/documents")
async def list_documents(
    limit: int = 20,
    offset: int = 0,
    status: Optional[JobStatus] = None,
    name: Optional[str] = None
):
    return {"pagination": {"total": 0, "limit": limit, "offset": offset}, "data": []}

@app.get("/v1/documents/{doc_id}")
async def get_document_metadata(doc_id: str, details: bool = False):
    return {"id": doc_id, "status": "completed"}

@app.get("/v1/documents/{doc_id}/content")
async def get_document_content(doc_id: str):
    # Logic to fetch from local cache (json/md/text)
    return {"result": "Digitized content placeholder"}

@app.delete("/v1/documents/{doc_id}", status_code=status.HTTP_204_NO_CONTENT)
async def delete_document(doc_id: str):
    # 1. Check if part of active job (409 Conflict)
    # 2. Remove from VDB and local cache
    return

@app.delete("/v1/documents", status_code=status.HTTP_204_NO_CONTENT)
async def bulk_delete_documents(confirm: bool = Query(...)):
    if not confirm:
        raise HTTPException(status_code=400, detail="Confirm parameter required.")
    # 1. Check for active jobs
    # 2. Truncate VDB and wipe cache
    return

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=4000)
