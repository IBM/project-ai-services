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
from digitize.digitize import digitize
from digitize.errors import *
from digitize.config import *

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

@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application lifespan events (startup and shutdown)."""
    # Startup
    logger.info("Application starting up...")
    
    yield
    
    # Shutdown
    logger.info("Application shutting down...")


app = FastAPI(title="Digitize Documents Service", lifespan=lifespan)

async def digitize_documents(job_id: str, filenames: List[str], doc_id_dict: dict, output_format: types.OutputFormat):
    status_mgr = StatusManager(job_id)
    job_staging_path = Path(STAGING_DIR) / f"{job_id}"

    try:
        logger.info(f"🚀 Digitization started for job: {job_id}")
        # to_thread prevents the heavy 'digitize' process from blocking the main FastAPI event loop and returns the response to request asynchronously.
        await asyncio.to_thread(digitize, job_staging_path, job_id, doc_id_dict, output_format)
        logger.info(f"Digitization for {job_id} completed successfully")
    except Exception as e:
        logger.error(f"Error in job {job_id}: {e}")
        status_mgr.update_job_progress("", types.DocStatus.FAILED, types.JobStatus.FAILED, error=f"Error occurred while processing digitization pipeline: {str(e)}")
    finally:
       # Always clean up staging directory, even on crashes
        try:
            if job_staging_path.exists():
                shutil.rmtree(job_staging_path)
                logger.debug(f"Cleaned up staging directory: {job_staging_path}")
        except Exception as cleanup_error:
            logger.warning(f"Failed to clean up staging directory {job_staging_path}: {cleanup_error}")

        # Crucial: Always release the semaphore slot back to the API
        digitization_semaphore.release()
        logger.debug(f"Semaphore slot released from digitization job {job_id}")

async def ingest_documents(job_id: str, filenames: List[str], doc_id_dict: dict):
    status_mgr = StatusManager(job_id)
    job_staging_path = STAGING_DIR / f"{job_id}"

    try:
        logger.info(f"🚀 Ingestion started for job: {job_id}")
        # to_thread prevents the heavy 'ingest' process from blocking the main FastAPI event loop and returns the response to request asynchronously.
        await asyncio.to_thread(ingest, job_staging_path, job_id, doc_id_dict)
        logger.info(f"Ingestion for {job_id} completed successfully")
    except Exception as e:
        logger.error(f"Error in job {job_id}: {e}")
        status_mgr.update_job_progress("", types.DocStatus.FAILED, types.JobStatus.FAILED, error=f"Error occurred while processing ingestion pipeline: {str(e)}")
    finally:
        # Always clean up staging directory, even on crashes
        try:
            if job_staging_path.exists():
                shutil.rmtree(job_staging_path)
                logger.debug(f"Cleaned up staging directory: {job_staging_path}")
        except Exception as cleanup_error:
            logger.warning(f"Failed to clean up staging directory {job_staging_path}: {cleanup_error}")
        
        # Mandatory Semaphore Release
        ingestion_semaphore.release()
        logger.debug(f"✅ Job {job_id} done. Semaphore released.")


@app.post("/v1/documents", status_code=status.HTTP_202_ACCEPTED)
async def digitize_document(
    background_tasks: BackgroundTasks,
    files: List[UploadFile] = File(...),
    operation: types.OperationType = Query(types.OperationType.INGESTION),
    output_format: types.OutputFormat = Query(types.OutputFormat.JSON)
):
    try:
        # 0. Early exit if no files submitted
        if not files or len(files) == 0:
            APIError.raise_error(ErrorCode.INVALID_REQUEST, "No files provided. Please submit at least one file.")

        sem = ingestion_semaphore if operation == types.OperationType.INGESTION else digitization_semaphore

        # 1. Fail fast if limit reached
        if sem.locked():
            APIError.raise_error(ErrorCode.RATE_LIMIT_EXCEEDED, f"Too many concurrent {operation} requests.")

        # 2. Validation
        # Validate that all files are PDFs
        allowed_file_types = {'pdf': b'%PDF'}
        for file in files:
            if not file.filename:
                APIError.raise_error(ErrorCode.INVALID_REQUEST, "File must have a filename.")

            if not has_allowed_extension(file.filename, allowed_file_types):
                APIError.raise_error(ErrorCode.UNSUPPORTED_MEDIA_TYPE, f"Only PDF files are allowed. Invalid file: {file.filename}")

            # Check content type if provided
            if file.content_type and file.content_type not in ['application/pdf', 'application/x-pdf']:
                APIError.raise_error(ErrorCode.UNSUPPORTED_MEDIA_TYPE, f"Only PDF files are allowed. Invalid content type for {file.filename}: {file.content_type}")

        # Validate only one file is allowed for digitization
        if operation == types.OperationType.DIGITIZATION and len(files) > 1:
            APIError.raise_error("INVALID_REQUEST", "Only 1 file allowed for digitization.")

        job_id = dg_util.generate_uuid()
        # Filter out None filenames and ensure all files have valid names
        filenames = [f.filename for f in files if f.filename]
        if len(filenames) != len(files):
            APIError.raise_error(ErrorCode.INVALID_REQUEST, "All files must have valid filenames.")
        
        # Read all file buffers concurrently with error handling
        # return_exceptions=True ensures partial failures don't cancel other reads
        file_contents_raw = await asyncio.gather(*[f.read() for f in files], return_exceptions=True)
        
        # Validate all file reads succeeded and filter to bytes only
        failed_reads = []
        file_contents: List[bytes] = []
        for idx, content in enumerate(file_contents_raw):
            if isinstance(content, Exception):
                filename = filenames[idx]
                logger.error(f"Failed to read file {filename}: {content}")
                failed_reads.append(f"{filename}: {str(content)}")
            elif isinstance(content, bytes):
                file_contents.append(content)
        
        if failed_reads:
            error_details = "; ".join(failed_reads)
            APIError.raise_error(ErrorCode.INVALID_REQUEST, f"Failed to read files: {error_details}")

        # 4. acquire the semaphore
        await sem.acquire()

        # 5. Schedule the background pipeline
        try:
            if operation == types.OperationType.INGESTION:
                # Upload the file byte stream to files in staging directory
                # files are written to disk here before creating background task to avoid OOM crashes in the thread. Useful for retrying the ingestion if background task crashes
                await dg_util.stage_upload_files(job_id, filenames, str(STAGING_DIR / job_id), file_contents)

                doc_id_dict = dg_util.initialize_job_state(job_id, types.OperationType.INGESTION, filenames)

                background_tasks.add_task(ingest_documents, job_id, filenames, doc_id_dict)
            else:
                # Upload the file byte stream to files in staging directory
                # files are written to disk here before creating background task to avoid OOM crashes in the thread. Useful for retrying the ingestion if background task crashes
                await dg_util.stage_upload_files(job_id, filenames, str(Path(STAGING_DIR) / job_id), file_contents)

                doc_id_dict = dg_util.initialize_job_state(job_id, types.OperationType.DIGITIZATION, filenames)

                background_tasks.add_task(digitize_documents, job_id, filenames, doc_id_dict, output_format)
        except Exception as e:
            sem.release()
            logger.error(f"Failed to schedule background task for job {job_id}, semaphore released: {e}")
            APIError.raise_error("INTERNAL_SERVER_ERROR", str(e))

        return {"job_id": job_id}
    except HTTPException:
        # Re-raise HTTPException as-is
        raise
    except Exception as e:
        logger.error(f"Unexpected error in digitize_document: {e}")
        APIError.raise_error("INTERNAL_SERVER_ERROR", str(e))

@app.get("/v1/documents/jobs")
async def get_all_jobs(
    latest: bool = False,
    limit: int = 20,
    offset: int = 0,
    status: Optional[types.JobStatus] = None
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
    status: Optional[types.JobStatus] = None,
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
        APIError.raise_error("INVALID_REQUEST", "Confirm parameter required.")
    # 1. Check for active jobs
    # 2. Truncate VDB and wipe cache
    return

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=4000)
