import asyncio
import logging
import os
from pathlib import Path
import shutil
import time
from typing import List, Optional
import uvicorn

from fastapi import FastAPI, UploadFile, File, HTTPException, BackgroundTasks, Query, status
from common.misc_utils import get_logger, set_log_level
import common.digitize_utils as dg_util
from status import StatusManager, update_status
from common.misc_utils import get_logger

log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif not "info" in level:
        logging.warning(f"Unknown LOG_LEVEL passed: '{level}', defaulting to INFO.")

set_log_level(log_level)
logger = get_logger("app")

import common.digitize_utils as dg_util
from common.misc_utils import *
from ingest import ingest 
from status import StatusManager, update_status

app = FastAPI(title="Digitize Documents Service")

# Semaphores for concurrency limiting
digitization_semaphore = asyncio.BoundedSemaphore(2)
ingestion_semaphore = asyncio.BoundedSemaphore(1)

logger = get_logger("digitize_server")

CACHE_DIR = "/var/cache"
DOCS_DIR = f"{CACHE_DIR}/docs"
STAGING_DIR = f"{CACHE_DIR}/staging"

async def digitize_documents(job_id: str, filenames: List[str], output_format: dg_util.OutputFormat):
    try:
        # Business logic for document conversion.
        pass
    except Exception as e:
        logger.error(f"Error in job {job_id}: {e}")
    finally:
        # Crucial: Always release the semaphore slot back to the API
        digitization_semaphore.release()
        logger.debug(f"Semaphore slot released from digitization job {job_id}")

async def ingest_documents(job_id: str, filenames: List[str], doc_id_dict: dict):
    status_mgr = StatusManager(job_id)
    job_staging_path = Path(STAGING_DIR) / "jobs"/job_id

    try:
        logger.info(f"🚀 Ingestion started for {job_id}")
        ingest(job_staging_path)
    except Exception as e:
        logger.error(f"Error in job {job_id}: {e}")
        status_mgr.update_status(job_id, dg_util.JobStatus.FAILED, error=f"Error occurred while processing ingestion pipeline. {str(e)}")
        status_mgr.update({"status": "error", "error_message": str(e)})
    
    finally:
        if job_staging_path.exists():
            shutil.rmtree(job_staging_path)
        
        # Mandatory Semaphore Release
        ingestion_semaphore.release()
        logger.debug(f"✅ Job {job_id} done. Semaphore released.")


@app.post("/v1/documents", status_code=status.HTTP_202_ACCEPTED)
async def digitize_document(
    background_tasks: BackgroundTasks,
    files: List[UploadFile] = File(...),
    operation: dg_util.OperationType = Query(dg_util.OperationType.INGESTION),
    output_format: dg_util.OutputFormat = Query(dg_util.OutputFormat.JSON)
):
    sem = ingestion_semaphore if operation == dg_util.OperationType.INGESTION else digitization_semaphore
    
    # 1. Fail fast if limit reached
    if sem.locked():
        raise HTTPException(
            status_code=status.HTTP_429_TOO_MANY_REQUESTS,
            detail=f"Too many concurrent {operation} requests."
        )

    # 2. Validation
    if operation == dg_util.OperationType.DIGITIZATION and len(files) > 1:
        raise HTTPException(status_code=400, detail="Only 1 file allowed for digitization.")

    job_id = dg_util.generate_job_id()
    filenames = [f.filename for f in files]

    # 3. acquire the semaphore
    await sem.acquire()

    # 4. Schedule the background pipeline
    try:
        if operation == dg_util.OperationType.INGESTION:
            # Upload the file byte stream to files in staging directory
            # files are written to disk here before creating background task to avoid OOM crashes in the thread. Useful for retrying the ingestion if background task crashes
            dg_util.stage_upload_files(job_id, filenames, Path(STAGING_DIR) / job_id)

            doc_id_dict = dg_util.initialize_job_state(job_id, dg_util.OperationType.INGESTION, filenames)

            background_tasks.add_task(ingest_documents, job_id, filenames, doc_id_dict)
        else:
            background_tasks.add_task(digitize_documents, job_id, filenames, output_format)

            await asyncio.sleep(10)
            update_status(job_id, "completed", "Done")
    except Exception as e:
        update_status(job_id, "failed", str(e))
        raise HTTPException(status_code=500, detail=str(e))

    return {"job_id": job_id}

@app.get("/v1/documents/jobs")
async def get_all_jobs(
    latest: bool = False,
    limit: int = 20,
    offset: int = 0,
    status: Optional[dg_util.JobStatus] = None
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
    status: Optional[dg_util.JobStatus] = None,
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
