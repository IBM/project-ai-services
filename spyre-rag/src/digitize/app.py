import asyncio
import logging
import os
from pathlib import Path
import shutil
from typing import List, Optional
from contextlib import asynccontextmanager
import uvicorn

from fastapi import FastAPI, UploadFile, File, HTTPException, BackgroundTasks, Query, status
from common.misc_utils import get_logger, set_log_level, has_allowed_extension
import digitize.digitize_utils as dg_util
from digitize import types
from digitize.resource_utils import (
    check_disk_space,
    cleanup_staging_directory,
    cleanup_failed_job,
    cleanup_old_failed_jobs,
    safe_cleanup,
    log_resource_usage,
    DiskSpaceError
)
from digitize.cleanup_scheduler import start_cleanup_scheduler, stop_cleanup_scheduler

log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif not "info" in level:
        logging.warning(f"Unknown LOG_LEVEL passed: '{level}', defaulting to INFO.")

set_log_level(log_level)

from digitize.ingest import ingest
from digitize.status import StatusManager

# Semaphores for concurrency limiting
digitization_semaphore = asyncio.BoundedSemaphore(2)
ingestion_semaphore = asyncio.BoundedSemaphore(1)

logger = get_logger("digitize_server")

CACHE_DIR = "/var/cache"
DOCS_DIR = f"{CACHE_DIR}/docs"
STAGING_DIR = f"{CACHE_DIR}/staging"


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application lifespan events (startup and shutdown)."""
    # Startup
    logger.info("Running startup cleanup tasks...")
    try:
        # Clean up old staging directories from previous crashes
        cleanup_staging_directory(STAGING_DIR, force=False)
        # Clean up old failed jobs
        cleanup_old_failed_jobs(CACHE_DIR)
        # Log initial resource usage
        log_resource_usage(CACHE_DIR)
        # Start periodic cleanup scheduler
        await start_cleanup_scheduler(CACHE_DIR)
        logger.info("Periodic cleanup scheduler started")
    except Exception as e:
        logger.warning(f"Startup cleanup encountered errors: {e}")
    
    yield
    
    # Shutdown
    logger.info("Running shutdown cleanup tasks...")
    try:
        # Stop periodic cleanup scheduler
        await stop_cleanup_scheduler()
        logger.info("Periodic cleanup scheduler stopped")
    except Exception as e:
        logger.warning(f"Shutdown cleanup encountered errors: {e}")


app = FastAPI(title="Digitize Documents Service", lifespan=lifespan)

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
    job_staging_path = Path(STAGING_DIR) / f"{job_id}"

    try:
        logger.info(f"🚀 Ingestion started for job: {job_id}")
        # to_thread prevents the heavy 'ingest' process from blocking the main FastAPI event loop and returns the response to request asynchronously.
        await asyncio.to_thread(ingest, job_staging_path, job_id, doc_id_dict)
        logger.info(f"Ingestion for {job_id} completed successfully")
    except Exception as e:
        logger.error(f"Error in job {job_id}: {e}")
        status_mgr.update_job_progress("", types.DocStatus.FAILED, types.JobStatus.FAILED, error=f"Error occurred while processing ingestion pipeline: {str(e)}")
        # Clean up failed job resources
        cleanup_failed_job(job_id, CACHE_DIR)
    finally:
        # Always clean up staging directory, even on crashes
        safe_cleanup(str(job_staging_path))
        
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
    # Validate that all files are PDFs
    allowed_file_types = {'pdf': b'%PDF'}
    for file in files:
        if not file.filename:
            raise HTTPException(status_code=400, detail="File must have a filename.")

        if not has_allowed_extension(file.filename, allowed_file_types):
            raise HTTPException(
                status_code=400,
                detail=f"Only PDF files are allowed. Invalid file: {file.filename}"
            )

        # Check content type if provided
        if file.content_type and file.content_type not in ['application/pdf', 'application/x-pdf']:
            raise HTTPException(
                status_code=400,
                detail=f"Only PDF files are allowed. Invalid content type for {file.filename}: {file.content_type}"
            )

    if operation == dg_util.OperationType.DIGITIZATION and len(files) > 1:
        raise HTTPException(status_code=400, detail="Only 1 file allowed for digitization.")

    # 3. Check disk space before processing
    try:
        file_sizes = [f.size for f in files if f.size]
        total_size_gb = sum(file_sizes) / (1024 ** 3)
        # Estimate required space (3x for intermediate files)
        required_space = max(5.0, total_size_gb * 3)
        check_disk_space(CACHE_DIR, required_gb=required_space)
        log_resource_usage(CACHE_DIR)
    except DiskSpaceError as e:
        logger.error(f"Disk space check failed: {e}")
        raise HTTPException(
            status_code=status.HTTP_507_INSUFFICIENT_STORAGE,
            detail=str(e)
        )
    except Exception as e:
        logger.warning(f"Could not check disk space: {e}")

    job_id = dg_util.generate_job_id()
    filenames = [f.filename for f in files]
    # asyncio.gather allows us to read all file buffers concurrently
    file_contents = await asyncio.gather(*[f.read() for f in files], return_exceptions=True)

    # 4. acquire the semaphore
    await sem.acquire()

    # 5. Schedule the background pipeline
    try:
        if operation == dg_util.OperationType.INGESTION:
            # Upload the file byte stream to files in staging directory
            # files are written to disk here before creating background task to avoid OOM crashes in the thread. Useful for retrying the ingestion if background task crashes
            await dg_util.stage_upload_files(job_id, filenames, str(Path(STAGING_DIR) / job_id), file_contents)

            doc_id_dict = dg_util.initialize_job_state(job_id, dg_util.OperationType.INGESTION, filenames)

            background_tasks.add_task(ingest_documents, job_id, filenames, doc_id_dict)
        else:
            background_tasks.add_task(digitize_documents, job_id, filenames, output_format)
    except Exception as e:
        # release the semaphore in case of exception
        sem.release()
        logger.debug(f"Semaphore slot released from the job {job_id}")
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
