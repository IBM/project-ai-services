"""
Job-related API endpoints.

Handles job creation, listing, retrieval, and deletion.
Extracted from the monolithic app.py following the digitize-api-sample pattern.

Exposes one router:
- ``router`` → mounted at ``/v1/jobs``
"""

import asyncio
from pathlib import Path
from typing import List, Optional

from fastapi import APIRouter, BackgroundTasks, File, HTTPException, Query, UploadFile, status

from common.misc_utils import get_logger, validate_document_file, cleanup_staging_directory
from common.error_utils import APIError, ErrorCode, http_error_responses
import digitize.utils.jobs as dg_util
import digitize.models as models
from digitize.utils.db import get_status_manager
import digitize.utils.db as db_ops
from digitize.pipeline.digitize import digitize
from digitize.pipeline.ingest import ingest
from digitize.settings import settings
from digitize.workers.concurrency import concurrency_manager

router = APIRouter()
logger = get_logger("jobs_router")


# ------------------------------------------------------------------ #
# Background task helpers                                             #
# ------------------------------------------------------------------ #

async def _run_digitize(
    job_id: str,
    doc_id_dict: dict,
    output_format: models.OutputFormat,
) -> None:
    """Run the digitization pipeline and release the semaphore slot."""
    status_mgr = get_status_manager(job_id)
    job_staging_path = settings.digitize.staging_dir / job_id

    try:
        logger.info(f"🚀 Digitization started for job: {job_id}")
        await asyncio.to_thread(digitize, job_staging_path, job_id, doc_id_dict, output_format)
        logger.info(f"Digitization for job {job_id} completed successfully")
    except Exception as exc:
        logger.error(f"Error in digitization job {job_id}: {exc}")
        status_mgr.update_job_progress(
            "",
            models.DocStatus.FAILED,
            models.JobStatus.FAILED,
            error=f"Error occurred while processing digitization pipeline: {exc}",
        )
    finally:
        cleanup_staging_directory(job_id, settings.digitize.staging_dir)
        concurrency_manager.release("digitization")
        logger.debug(f"Semaphore slot released from digitization job {job_id}")


async def _run_ingest(
    job_id: str,
    filenames: List[str],
    doc_id_dict: dict,
) -> None:
    """Run the ingestion pipeline and release the semaphore slot."""
    status_mgr = get_status_manager(job_id)
    job_staging_path = settings.digitize.staging_dir / job_id

    try:
        logger.info(f"🚀 Ingestion started for job: {job_id}")
        await asyncio.to_thread(ingest, job_staging_path, job_id, doc_id_dict)
        logger.info(f"Ingestion for job {job_id} completed successfully")
    except Exception as exc:
        logger.error(f"Error in ingestion job {job_id}: {exc}")
        status_mgr.update_job_progress(
            "",
            models.DocStatus.FAILED,
            models.JobStatus.FAILED,
            error=f"Error occurred while processing ingestion pipeline: {exc}",
        )
    finally:
        cleanup_staging_directory(job_id, settings.digitize.staging_dir)
        concurrency_manager.release("ingestion")
        logger.debug(f"✅ Job {job_id} done. Semaphore released.")


# ------------------------------------------------------------------ #
# File validation helper                                              #
# ------------------------------------------------------------------ #

async def _validate_files(
    files: List[UploadFile],
    file_contents_raw: List[bytes | BaseException],
) -> tuple[List[str], List[bytes]]:
    """Validate uploaded document files; raises ``APIError`` on failure."""
    filenames: List[str] = []
    file_contents: List[bytes] = []

    for idx, file in enumerate(files):
        filename = file.filename or ""
        content = file_contents_raw[idx]

        try:
            await asyncio.to_thread(validate_document_file, filename, content)
        except ValueError as exc:
            APIError.raise_error(ErrorCode.UNSUPPORTED_MEDIA_TYPE, str(exc))

        assert isinstance(content, bytes)
        filenames.append(filename)
        file_contents.append(content)

    return filenames, file_contents


# ------------------------------------------------------------------ #
# Endpoints                                                           #
# ------------------------------------------------------------------ #

@router.post(
    "",
    status_code=status.HTTP_202_ACCEPTED,
    response_model=models.JobCreatedResponse,
    responses=http_error_responses,
    summary="Create async jobs to upload and process documents",
    description=(
        "Upload documents (PDF or DOCX) for processing. Supports two operation types:\n\n"
        "- **ingestion**: Process and index documents into vector database for semantic search\n"
        "- **digitization**: Convert document to text/markdown/JSON format (single file only)\n\n"
        "The operation runs asynchronously in the background. "
        "Use the returned `job_id` to track progress."
    ),
    response_description="Job ID for tracking the processing status",
)
async def create_job(
    background_tasks: BackgroundTasks,
    files: List[UploadFile] = File(
        ...,
        description="Document files (PDF or DOCX) to process",
    ),
    operation: models.OperationType = Query(
        models.OperationType.INGESTION,
        description="Operation type: 'ingestion' or 'digitization'",
    ),
    output_format: models.OutputFormat = Query(
        models.OutputFormat.JSON,
        description="Output format for digitization: 'json', 'md', or 'txt'",
    ),
    job_name: Optional[str] = Query(None, description="Optional human-readable name for the job"),
):
    try:
        # 1. Block new jobs while an import/export is running.
        if await db_ops.is_import_export_in_progress():
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "Cannot create new jobs while import/export operation is in progress",
            )

        # 2. Guard against empty submissions.
        if not files:
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                "No files provided. Please submit at least one file.",
            )

        if operation == models.OperationType.DIGITIZATION and len(files) > 1:
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                "Only 1 file allowed for digitization.",
            )

        # 3. Cross-process active-job check for ingestion.
        if operation == models.OperationType.INGESTION:
            has_active, active_job_ids = dg_util.has_active_jobs(operation=operation.value)
            if has_active:
                error_msg = "An ingestion job is already running"
                if active_job_ids:
                    error_msg += f" (job_id: {active_job_ids[0]})"
                logger.error(f"Rejected ingestion request: {error_msg}")
                APIError.raise_error(ErrorCode.RATE_LIMIT_EXCEEDED, error_msg)

        # 4. Semaphore availability check.
        op_key = operation.value  # "ingestion" | "digitization"
        if concurrency_manager.is_locked(op_key):
            APIError.raise_error(
                ErrorCode.RATE_LIMIT_EXCEEDED,
                f"Too many concurrent {operation} requests.",
            )

        # 5. Read files and normalize extensions to lowercase before any
        #    further processing so that filenames used as keys (job state,
        #    doc-ID mapping, staging globs) are always consistent.
        job_id = dg_util.generate_uuid()
        file_contents_raw = await asyncio.gather(
            *[f.read() for f in files], return_exceptions=True
        )
        for f in files:
            if f.filename:
                p = Path(f.filename)
                f.filename = p.stem + p.suffix.lower()

        filenames, file_contents = await _validate_files(files, file_contents_raw)

        # 6. Acquire semaphore slot.
        await concurrency_manager.acquire(op_key)

        # 7. Stage files and schedule background task.
        try:
            await dg_util.stage_upload_files(
                job_id,
                filenames,
                str(settings.digitize.staging_dir / job_id),
                file_contents,
            )
            doc_id_dict = dg_util.initialize_job_state(
                job_id, operation, output_format, filenames, job_name
            )
            if operation == models.OperationType.INGESTION:
                background_tasks.add_task(_run_ingest, job_id, filenames, doc_id_dict)
            else:
                background_tasks.add_task(_run_digitize, job_id, doc_id_dict, output_format)
        except Exception as exc:
            concurrency_manager.release(op_key)
            logger.error(
                f"Failed to schedule background task for job {job_id}, "
                f"semaphore released: {exc}"
            )
            APIError.raise_error("INTERNAL_SERVER_ERROR", str(exc))

        return {"job_id": job_id}

    except HTTPException:
        raise
    except Exception as exc:
        logger.error(f"Unexpected error in create_job: {exc}")
        APIError.raise_error("INTERNAL_SERVER_ERROR", str(exc))


@router.get(
    "",
    response_model=models.JobsListResponse,
    responses={500: http_error_responses[500]},
    summary="List all jobs",
    description="Retrieve information about all submitted jobs with pagination and filtering options.",
    response_description="Paginated list of jobs with their current status",
)
async def list_jobs(
    latest: bool = Query(False, description="Return only the latest job"),
    limit: int = Query(20, ge=1, le=100, description="Number of records per page"),
    offset: int = Query(0, ge=0, description="Number of records to skip"),
    status: Optional[models.JobStatus] = Query(None, description="Filter by job status"),
    operation: Optional[models.OperationType] = Query(None, description="Filter by operation type"),
):
    """Retrieve information about all submitted jobs with pagination and filtering."""
    try:
        from digitize.utils.db import get_all_jobs

        jobs_data, total = get_all_jobs(
            status=status,
            operation=operation.value if operation else None,
            limit=limit if not latest else 1,
            offset=offset if not latest else 0,
        )

        if latest and jobs_data:
            jobs_data = [jobs_data[0]]
            total = 1

        return models.JobsListResponse(
            pagination=models.PaginationInfo(total=total, limit=limit, offset=offset),
            data=jobs_data,
        )
    except HTTPException as exc:
        logger.error(f"Server error in list_jobs: {exc.status_code} - {exc.detail}")
        raise
    except Exception as exc:
        logger.error(f"Failed to retrieve jobs: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, "Failed to retrieve jobs")


@router.get(
    "/{job_id}",
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    summary="Get job by ID",
    description="Retrieve detailed status and progress information for a specific job.",
    response_description="Detailed job information including document statuses and statistics",
)
async def get_job(job_id: str):
    """Retrieve detailed status of a specific job by its ID."""
    try:
        from digitize.utils.db import get_job as _get_job

        job_data = _get_job(job_id)

        if job_data is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"No job found with id '{job_id}'",
            )
            return

        return job_data
    except HTTPException as exc:
        logger.error(
            f"HTTP error retrieving job {job_id}: "
            f"status={exc.status_code}, detail={exc.detail}"
        )
        raise
    except Exception as exc:
        logger.error(f"Failed to retrieve job {job_id}: {exc}", exc_info=True)
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            f"Failed to retrieve job information for '{job_id}'",
        )


@router.delete(
    "/{job_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        404: http_error_responses[404],
        409: http_error_responses[409],
        500: http_error_responses[500],
    },
    summary="Delete job",
    description=(
        "Delete a job status record. Only completed or failed jobs can be deleted. "
        "Active jobs (accepted or in_progress) cannot be deleted. "
        "Note: This only deletes the job record, not the associated document data."
    ),
    response_description="No content on successful deletion",
)
async def delete_job(job_id: str):
    """Deletes a job record from database. Does not touch associated document metadata."""
    try:
        from digitize.utils.db import get_job as _get_job
        from digitize.db.manager import db_manager

        job_data = _get_job(job_id)

        if job_data is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"No job found with id '{job_id}'",
            )

        job_status = job_data.get("status", "")
        if job_status in (models.JobStatus.ACCEPTED, models.JobStatus.IN_PROGRESS):
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                f"Job '{job_id}' is still active and cannot be deleted",
            )

        db_manager.delete_job(job_id)
        logger.info(f"Deleted job '{job_id}' from database")
        return

    except HTTPException as exc:
        logger.error(
            f"HTTP error deleting job {job_id}: "
            f"status={exc.status_code}, detail={exc.detail}"
        )
        raise
    except Exception as exc:
        logger.error(f"Failed to delete job {job_id}: {exc}", exc_info=True)
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            f"Failed to delete job '{job_id}'",
        )


