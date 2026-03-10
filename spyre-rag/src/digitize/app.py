import asyncio
import json
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
import digitize.types as types
from digitize.digitize import digitize
from digitize.errors import *
import digitize.config as config

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

async def digitize_documents(job_id: str, doc_id_dict: dict, output_format: types.OutputFormat):
    status_mgr = StatusManager(job_id)
    job_staging_path = config.STAGING_DIR / f"{job_id}"

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
    job_staging_path = config.STAGING_DIR / f"{job_id}"

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
            APIError.raise_error(ErrorCode.INVALID_REQUEST, "Only 1 file allowed for digitization.")

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
            # Upload the file byte stream to files in staging directory
            # files are written to disk here before creating background task to avoid OOM crashes in the thread. Useful for retrying the ingestion if background task crashes
            await dg_util.stage_upload_files(job_id, filenames, str(config.STAGING_DIR / job_id), file_contents)
            doc_id_dict = dg_util.initialize_job_state(job_id, operation, output_format, filenames)
            if operation == types.OperationType.INGESTION:
                background_tasks.add_task(ingest_documents, job_id, filenames, doc_id_dict)
            else:
                background_tasks.add_task(digitize_documents, job_id, doc_id_dict, output_format)
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

@app.get("/v1/jobs", response_model=types.JobsListResponse)
async def get_all_jobs(
    latest: bool = Query(False, description="Return only the latest job"),
    limit: int = Query(20, ge=1, le=100, description="Number of records per page"),
    offset: int = Query(0, ge=0, description="Number of records to skip"),
    status: Optional[types.JobStatus] = Query(None, description="Filter by job status"),
    operation: Optional[types.OperationType] = Query(None, description="Filter by operation type")
):
    """Retrieve information about all submitted jobs with pagination and filtering."""
    try:
        # Read all job status files
        all_jobs = dg_util.read_all_job_files()

        # Apply filters in single pass
        filtered_jobs = [
            j for j in all_jobs
            if (status is None or j.status == status) and
               (operation is None or j.operation == operation.value)
        ]

        # sorting by submitted_at
        filtered_jobs = sorted(
            filtered_jobs,
            key=lambda j: j.submitted_at,
            reverse=True
        )

        # Handle latest flag before pagination
        if latest and filtered_jobs:
            filtered_jobs = [filtered_jobs[0]]

        total = len(filtered_jobs)

        # Apply pagination
        paginated_jobs = filtered_jobs[offset : offset + limit]

        # Convert to response format
        jobs_data = [job.to_dict() for job in paginated_jobs]

        return types.JobsListResponse(
            pagination=types.PaginationInfo(total=total, limit=limit, offset=offset),
            data=jobs_data
        )
    except HTTPException as e:
        logger.error(f"Server error in get_all_jobs: {e.status_code} - {e.detail}")
        raise
    except Exception as e:
        logger.error(f"Failed to retrieve jobs: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, "Failed to retrieve jobs")


@app.get("/v1/jobs/{job_id}")
async def get_job_by_id(job_id: str):
    """Retrieve detailed status of a specific job by its ID."""
    try:
        job_status_file = config.JOBS_DIR / f"{job_id}_status.json"

        if not job_status_file.exists():
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"No job found with id '{job_id}'")

        if not job_status_file.is_file():
            APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Job status path for '{job_id}' is not a valid file")

        job_state = dg_util.read_job_file(job_status_file)
        if job_state is None:
            APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Failed to read job status for '{job_id}'")
            return  # This line should never be reached, but helps type checker

        # Convert JobState object to JSON-compatible dictionary
        return job_state.to_dict()
    except HTTPException as e:
        logger.error(f"HTTP error retrieving job {job_id}: "
        f"status={e.status_code}, detail={e.detail}")
        raise
    except Exception as e:
        logger.error(f"Failed to retrieve job {job_id}: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Failed to retrieve job information for '{job_id}'")

@app.delete("/v1/jobs/{job_id}", status_code=status.HTTP_204_NO_CONTENT)
async def delete_job(job_id: str):
    """Deletes a job status file. Does not touch associated document metadata."""
    try:
        job_status_file = config.JOBS_DIR / f"{job_id}_status.json"

        if not job_status_file.exists():
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"No job found with id '{job_id}'")

        # Reject deletion if the job is still active
        job_state = dg_util.read_job_file(job_status_file)
        if job_state is None:
            APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Failed to read job status for '{job_id}'")
            return  # This line should never be reached, but helps type checker

        # Compare with JobStatus enum
        if job_state.status in (types.JobStatus.ACCEPTED, types.JobStatus.IN_PROGRESS):
            APIError.raise_error(ErrorCode.RESOURCE_LOCKED, f"Job '{job_id}' is still active and cannot be deleted")

        # Delete the job status file (missing_ok=True handles race conditions)
        job_status_file.unlink(missing_ok=True)
        logger.info(f"Deleted job status file for job '{job_id}'")
        return
    except HTTPException as e:
        logger.error(f"HTTP error deleting job {job_id}: "
                     f"status={e.status_code}, detail={e.detail}")
        raise
    except Exception as e:
        logger.error(f"Failed to delete job {job_id}: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Failed to delete job '{job_id}'")



@app.get("/v1/documents", response_model=List[types.DocumentListItem])
async def list_documents(
    limit: int = Query(20, ge=1, le=100, description="Number of records to return per page"),
    offset: int = Query(0, ge=0, description="Number of records to skip"),
    status: Optional[str] = Query(None, description="Filter by status: accepted/in_progress/completed/failed"),
    name: Optional[str] = Query(None, description="Filter by document name")
):
    """
    Get high-level information of all documents sorted by submitted_time.

    Query Parameters:
    - limit: Number of records to return per page (default: 20, max: 100)
    - offset: Number of records to skip (default: 0)
    - status: Filter by status (accepted/in_progress/completed/failed)
    - name: Filter by document name (partial match, case-insensitive)

    Returns:
    - pagination: Object with total, limit, and offset
    - data: List of document metadata objects
    """
    try:
        logger.debug(f"Fetching documents with filters: limit={limit}, offset={offset}, status={status}, name={name}")
        # Validate status if provided
        valid_statuses = {s.value for s in types.DocStatus}
        if status and status.lower() not in valid_statuses:
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                f"Invalid status '{status}'. Must be one of: {', '.join(sorted(valid_statuses))}"
            )

        all_documents = dg_util.get_all_documents(status_filter=status, name_filter=name)

        # Calculate pagination
        total = len(all_documents)
        start_idx = offset
        end_idx = offset + limit

        # Apply pagination
        paginated_documents = all_documents[start_idx:end_idx]

        logger.debug(f"Returning {len(paginated_documents)} documents out of {total} total (offset={offset}, limit={limit})")

        # Convert Pydantic models to dicts for response
        return {
            "pagination": {
                "total": total,
                "limit": limit,
                "offset": offset
            },
            "data": [doc.model_dump() for doc in paginated_documents]
        }

    except HTTPException as e:
        logger.error(f"Failed to list documents, HTTP error: {e}")
        # Re-raise HTTPException as-is
        raise
    except Exception as e:
        logger.error(f"Unexpected error in list_documents: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(e))

@app.get("/v1/documents/{doc_id}", response_model=types.DocumentDetailResponse)
async def get_document_metadata(doc_id: str, details: bool = Query(False, description="Include detailed metadata")):
    """
    Get details of a specific document by ID.

    Path Parameters:
    - doc_id: Unique identifier of the document

    Query Parameters:
    - details: If true, includes detailed metadata (pages, tables, timing information)

    Returns:
    - Document metadata with optional detailed information
    """
    try:
        response = dg_util.get_document_by_id(doc_id, include_details=details)
        return response
    except FileNotFoundError as e:
        APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, str(e))
    except json.JSONDecodeError as e:
        logger.error(f"Failed to parse metadata file for document {doc_id}: {e}")
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, "Failed to read document metadata")
    except HTTPException as e:
        logger.error(f"Failed to get document by id {doc_id}, HTTP error: {e}")
        # Re-raise HTTPException as-is
        raise
    except Exception as e:
        logger.error(f"Unexpected error in get_document_metadata: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(e))

@app.get("/v1/documents/{doc_id}/content", response_model=types.DocumentContentResponse)
async def get_document_content(doc_id: str):
    """
    Get the digitized content of a specific document.

    Returns the digitized content stored in /var/cache/digitized/<doc_id>.json
    - For documents submitted via digitization: returns the output_format requested during POST (md/text/json)
    - For documents submitted via ingestion: returns the extracted json representation

    Path Parameters:
    - doc_id: Unique identifier of the document

    Returns:
    - result: Content based on output_format (str for md/text, dict for json)
    - output_format: The format of the returned content (md/text/json)
    """
    try:
        response = dg_util.get_document_content(doc_id)
        return response
    except FileNotFoundError as e:
        APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, str(e))
    except json.JSONDecodeError as e:
        logger.error(f"Failed to parse content file for document {doc_id}: {e}")
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, "Failed to read document content")
    except HTTPException as e:
        logger.error(f"Failed to get document content for id {doc_id}, HTTP error: {e}")
        # Re-raise HTTPException as-is
        raise
    except Exception as e:
        logger.error(f"Unexpected error in get_document_content: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(e))

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
