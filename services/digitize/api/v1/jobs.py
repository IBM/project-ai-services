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

from fastapi import APIRouter, File, HTTPException, Query, UploadFile, status

from common.misc_utils import get_logger, validate_document_file, cleanup_staging_directory, generate_file_checksum
from common.error_utils import APIError, ErrorCode, http_error_responses, extract_http_error_message, build_http_error_detail
import digitize.utils.jobs as dg_util
import digitize.models as models
from digitize.utils.db import get_status_manager
import digitize.utils.db as db_ops
from digitize.settings import settings
from digitize.db.manager import db_manager

router = APIRouter()
logger = get_logger("jobs_router")

# ------------------------------------------------------------------ #
# Background pipeline helpers                                         #
# ------------------------------------------------------------------ #

async def _run_digitize(
    job_id: str,
    doc_id_dict: dict,
) -> None:
    """
    Poll the conversion_tasks row for this digitization job until the
    dispatcher marks it terminal, then surface the result.

    Runs the blocking pipeline call in a thread so the event loop stays free.
    """
    try:
        logger.info(f"🚀 Digitization started for job: {job_id}")
        from digitize.pipeline.digitize import digitize
        await asyncio.to_thread(digitize, job_id, doc_id_dict)
        logger.info(f"Digitization for job {job_id} completed successfully")
    except Exception as exc:
        logger.error(f"Error in digitization job {job_id}: {exc}", exc_info=True)
        status_mgr = get_status_manager(job_id)
        status_mgr.update_job_progress(
            "",
            models.DocStatus.FAILED,
            models.JobStatus.FAILED,
            error=f"Error occurred while processing digitization pipeline: {exc}",
        )
    finally:
        cleanup_staging_directory(job_id, settings.digitize.staging_dir)



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
    responses={
        **http_error_responses,
        409: {
            "description": "All submitted files have already been processed.",
        },
    },
    summary="Create async jobs to upload and process documents",
    description=(
        "Upload documents (PDF or DOCX) for processing. Supports two operation types:\n\n"
        "- **ingestion**: Process and index documents into vector database for semantic search\n"
        "- **digitization**: Convert document to text/markdown/JSON format (single file only)\n\n"
        "The operation runs asynchronously in the background. "
        "Use the returned `job_id` to track progress.\n\n"
        "Files are deduplicated using hash against completed documents. "
        "If only some files are duplicates, the job proceeds with the novel files; "
        "skipped files appear in the document list with `status=already_exists`."
    ),
    response_description="Job accepted. `job_id` can be used to poll status.",
    tags=["MCP"],
    operation_id="create_digitize_job",
)
async def create_job(
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
    """Create a new background digitization or ingestion job for one or more files.

    Validates files, generates unique job identifiers, and enqueues the tasks.
    """
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

        # 3. Per-operation queue quota gate — atomic advisory-lock check.
        #    The gate requires at least one free slot — it does NOT require all N
        #    new tasks to fit.
        op_key = operation.value  # "ingestion" | "digitization"
        if op_key == "ingestion":
            quota = settings.digitize.ingestion_queue_quota
        else:
            quota = settings.digitize.digitization_queue_quota

        quota_ok, queued_for_op = db_manager.check_quota_atomic(op_key, quota)
        if not quota_ok:
            logger.warning(
                "Queue quota exceeded for %s: %d/%d slots used — returning 429",
                operation.value, queued_for_op, quota,
            )
            APIError.raise_error(
                ErrorCode.RATE_LIMIT_EXCEEDED,
                f"The {operation.value} conversion queue is full "
                f"({queued_for_op}/{quota} slots used). Please retry later.",
            )

        # 4. Read files and normalise extensions to lowercase.
        job_id = dg_util.generate_uuid()
        file_contents_raw = await asyncio.gather(
            *[f.read() for f in files], return_exceptions=True
        )
        for f in files:
            if f.filename:
                p = Path(f.filename)
                f.filename = p.stem + p.suffix.lower()

        filenames, file_contents = await _validate_files(files, file_contents_raw)

        # 5. File-hash already-exists detection.
        #
        # Each file is hashed and checked against completed documents of the same
        # operation type. Files that already exist are removed from the batch; novel
        # files continue. If ALL files already exist, return 409 immediately (no job
        # created). If SOME files already exist, only novel files are processed;
        # skipped files are recorded in the job's document list with status
        # `already_exists` (visible via GET /v1/jobs/{job_id}).
        #
        # A file already exists only if a document with:
        #   - type   = operation ('ingestion' or 'digitization')
        #   - status = 'completed'
        #   - metadata->>'file_hash' = computed_hash
        # is found. Failed / in-progress records are NOT considered to already exist.

        already_exists_files: list[models.AlreadyExistsFile] = []
        file_checksum_dict: dict[str, str] = {}  # filename -> md5 hex (novel files only)

        novel_filenames: list[str] = []
        novel_contents:  list[bytes] = []

        for filename, content in zip(filenames, file_contents):
            file_hash = await asyncio.to_thread(generate_file_checksum, content)
            existing = db_manager.find_completed_document_by_hash(file_hash, operation.value)

            # A digitization request is also blocked by a completed ingestion of the
            # same file — no point re-digitizing content that is already indexed.
            if not existing and operation == models.OperationType.DIGITIZATION:
                existing = db_manager.find_completed_document_by_hash(
                    file_hash, models.OperationType.INGESTION.value
                )

            if existing:
                logger.info(
                    f"File '{filename}' already exists — matches completed "
                    f"doc_id={existing.doc_id} (hash={file_hash[:20]}...)"
                )
                already_exists_files.append(
                    models.AlreadyExistsFile(
                        filename=filename,
                        existing_doc_id=existing.doc_id,
                        existing_doc_name=existing.name,
                        file_hash=file_hash,
                    )
                )
            else:
                novel_filenames.append(filename)
                novel_contents.append(content)
                file_checksum_dict[filename] = file_hash

        # All files already exist → 409 Conflict, no job created.
        if not novel_filenames:
            skipped_summary = ", ".join(
                f"'{s.filename}' (existing doc: {s.existing_doc_id})"
                for s in already_exists_files
            )
            logger.warning(
                f"All {len(already_exists_files)} submitted file(s) already exist. "
                f"No job created. Files: {skipped_summary}"
            )
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                f"All submitted files have already been processed: {skipped_summary}. "
                f"Delete the existing documents first if you want to re-process.",
            )

        # Replace filenames/file_contents with the novel subset only.
        filenames     = novel_filenames
        file_contents = novel_contents

        # 6. Stage files to disk.
        await dg_util.stage_upload_files(
            job_id,
            filenames,
            str(settings.digitize.staging_dir / job_id),
            file_contents,
        )

        # 7, 8 & 9. Create DB rows, enqueue conversion tasks, launch pipeline.
        doc_id_dict = await dg_util.initialize_and_launch(
            job_id=job_id,
            operation=operation,
            output_format=output_format,
            filenames=filenames,
            staging_dir=settings.digitize.staging_dir / job_id,
            quota=quota,
            queued_for_op=queued_for_op,
            job_name=job_name,
            already_exists_files=already_exists_files,
            file_checksum_dict=file_checksum_dict,
        )

        # Digitization jobs need a separate pipeline task (not launch_ingest_pipeline).
        if operation == models.OperationType.DIGITIZATION:
            asyncio.create_task(_run_digitize(job_id, doc_id_dict))

        logger.info(
            f"Job {job_id} accepted — {len(filenames)} file(s), "
            f"op={op_key}, format={output_format.value}"
        )
        return {"job_id": job_id}

    except HTTPException as exc:
        message = f"Failed to create job: {extract_http_error_message(exc)}"
        logger.error(message)
        raise HTTPException(status_code=exc.status_code, detail=build_http_error_detail(exc, message))
    except Exception as exc:
        logger.error(f"Unexpected error in create_job: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Unexpected error creating job: {exc}")


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
        message = f"Failed to list jobs: {extract_http_error_message(exc)}"
        logger.error(message)
        raise HTTPException(status_code=exc.status_code, detail=build_http_error_detail(exc, message))
    except Exception as exc:
        logger.error(f"Failed to retrieve jobs: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Failed to retrieve jobs: {exc}")


@router.get(
    "/{job_id}",
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    summary="Get job by ID",
    description="Retrieve detailed status and progress information for a specific job.",
    response_description="Detailed job information including document statuses and statistics",
    tags=["MCP"],
    operation_id="get_digitize_job",
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
        message = f"Failed to get job '{job_id}': {extract_http_error_message(exc)}"
        logger.error(message)
        raise HTTPException(status_code=exc.status_code, detail=build_http_error_detail(exc, message))
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
        message = f"Failed to delete job '{job_id}': {extract_http_error_message(exc)}"
        logger.error(message)
        raise HTTPException(status_code=exc.status_code, detail=build_http_error_detail(exc, message))
    except Exception as exc:
        logger.error(f"Failed to delete job {job_id}: {exc}", exc_info=True)
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            f"Failed to delete job '{job_id}'",
        )
