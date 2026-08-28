"""
Async job endpoints.

POST   /v1/translate/jobs                          — create job
GET    /v1/translate/jobs                          — list jobs (paginated, status filter)
GET    /v1/translate/jobs/{job_id}                 — job detail
GET    /v1/translate/jobs/{job_id}/result          — get result JSON
GET    /v1/translate/jobs/{job_id}/result/download — download translated file
"""

import asyncio
from datetime import datetime, timezone
from typing import Optional, Union

from fastapi import APIRouter, File, Form, Query, UploadFile, status
from fastapi.responses import JSONResponse, PlainTextResponse

from common.error_utils import APIError, ErrorCode, http_error_responses
from common.misc_utils import get_logger, get_utc_timestamp
from common.validation_utils import validate_file_content, validate_file_extension
from translate.utils.errors import (
    _raise_file_too_large,
    _raise_job_failed,
    _raise_unsupported_file_type,
)
from translate.utils.language import validate_languages
from translate.db.manager import db_manager
from translate.models import (
    JobCreatedResponse,
    JobDetailResponse,
    JobResultResponse,
    JobState,
    JobStatus,
    JobsListResponse,
    PaginationInfo,
)
from translate.settings import settings
from translate.utils.jobs import (
    generate_uuid,
    read_result_file,
    stage_uploaded_file,
)
from translate.utils.storage import storage_manager
from translate.workers.concurrency import concurrency_manager
from translate.workers.translation_worker import run_translation_job

logger = get_logger("jobs_api")

router = APIRouter()

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _job_to_detail(job) -> JobDetailResponse:
    return JobDetailResponse(
        job_id=job.job_id,
        job_name=job.job_name,
        status=job.status,
        source_language=job.source_language,
        target_language=job.target_language,
        input_type=job.input_type,
        document_name=job.document_name,
        submitted_at=get_utc_timestamp(job.submitted_at),
        completed_at=get_utc_timestamp(job.completed_at),
        error=job.error,
        job_metadata=job.job_metadata,
    )


def _job_to_state(job) -> JobState:
    return JobState(
        job_id=job.job_id,
        job_name=job.job_name,
        status=job.status,
        source_language=job.source_language,
        target_language=job.target_language,
        input_type=job.input_type,
        document_name=job.document_name,
        submitted_at=get_utc_timestamp(job.submitted_at),
        completed_at=get_utc_timestamp(job.completed_at),
        error=job.error,
    )


# ---------------------------------------------------------------------------
# POST /v1/translate/jobs
# ---------------------------------------------------------------------------

@router.post(
    "",
    status_code=status.HTTP_202_ACCEPTED,
    response_model=JobCreatedResponse,
    response_description="Job accepted. Poll the returned job_id for status.",
    summary="Submit a file for async translation",
    description=(
        "Upload a `.txt` or `.md` file and specify a target language. "
        "Returns a ``job_id`` for polling. "
        "Source language is auto-detected when omitted."
    ),
    responses={
        400: http_error_responses[400],   # INVALID_LANGUAGE, SAME_LANGUAGE
        413: http_error_responses[413],   # FILE_TOO_LARGE
        415: http_error_responses[415],   # UNSUPPORTED_FILE_TYPE
        429: http_error_responses[429],   # job_limiter at capacity
        500: http_error_responses[500],
    },
)
async def create_translation_job(
    file: UploadFile = File(..., description="The .txt or .md file to translate (UTF-8)"),
    target_language: str = Form(..., description="Target language (e.g. 'English')"),
    source_language: str = Form(
        default="auto",
        description="Source language name, or 'auto' for automatic detection",
    ),
    job_name: Optional[str] = Form(
        default=None,
        description="Optional human-readable label for the job",
    ),
) -> JobCreatedResponse:
    """Create a translation job for an uploaded text or Markdown file."""
    filename = file.filename or "upload"

    # 1. Validate file extension.
    try:
        input_type = validate_file_extension(filename)
    except ValueError as exc:
        _raise_unsupported_file_type(str(exc))

    # 2. Validate file content (UTF-8, not binary, not PDF-disguised-as-text).
    try:
        await validate_file_content(file)
    except ValueError as exc:
        _raise_unsupported_file_type(str(exc))

    # 3. Validate languages.
    norm_source, norm_target = validate_languages(source_language, target_language)

    # 4. Check file size (txt/md only — revisit when PDF/Word support is added via digitize integration).
    max_bytes = settings.translate.max_upload_size_mb * 1024 * 1024
    content = await file.read(max_bytes + 1)
    if len(content) > max_bytes:
        _raise_file_too_large(
            f"File exceeds the {settings.translate.max_upload_size_mb} MB limit."
        )

    # 5. Check job admission semaphore — non-blocking try.
    if concurrency_manager.job_limiter.locked():
        APIError.raise_error(
            ErrorCode.RATE_LIMIT_EXCEEDED,
            f"Maximum concurrent jobs ({settings.translate.max_concurrent_jobs}) "
            "already running. Please try again later.",
        )

    # 6. Generate job ID; stage the file.
    job_id = generate_uuid()
    staged_path = await stage_uploaded_file(
        job_id=job_id,
        filename=filename,
        content=content,
    )

    # 7. Insert DB row with status=accepted.
    created = db_manager.create_job(
        job_id=job_id,
        source_language=norm_source,
        target_language=norm_target,
        input_type=input_type,
        job_name=job_name,
        document_name=filename,
        submitted_at=datetime.now(timezone.utc),
    )
    if created is None:
        # DB insert failed (duplicate job_id or DB error); clean up the staged
        # file so we don't leave an orphan, then surface a 500 to the caller.
        storage_manager.cleanup_staging(job_id)
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            "Failed to create translation job record. Please try again.",
        )

    # 8. Launch background worker.
    asyncio.create_task(
        run_translation_job(
            job_id=job_id,
            staged_file_path=staged_path,
            source_language=norm_source,
            target_language=norm_target.capitalize(),
            document_name=filename,
            input_type=input_type,
        )
    )

    logger.info(
        f"Accepted translation job {job_id}: "
        f"{filename} ({input_type}), {norm_source}→{norm_target}"
    )

    return JobCreatedResponse(job_id=job_id)


# ---------------------------------------------------------------------------
# GET /v1/translate/jobs
# ---------------------------------------------------------------------------

@router.get(
    "",
    response_model=JobsListResponse,
    response_description="Paginated list of jobs matching the query.",
    summary="List translation jobs",
    description="Paginated list of jobs with optional status filter.",
    responses={
        400: http_error_responses[400],   # invalid query params
        500: http_error_responses[500],
    },
)
async def list_jobs(
    limit: int = Query(default=20, ge=1, le=100, description="Records per page"),
    offset: int = Query(default=0, ge=0, description="Records to skip"),
    status_filter: Optional[str] = Query(default=None, description="Filter by job status"),
) -> JobsListResponse:
    """List translation jobs with pagination and an optional status filter."""
    job_status: Optional[JobStatus] = None
    if status_filter is not None:
        try:
            job_status = JobStatus(status_filter.lower())
        except ValueError:
            valid = ", ".join(s.value for s in JobStatus)
            APIError.raise_error(
                ErrorCode.INVALID_PARAMETER,
                f"Invalid status '{status_filter}'. Valid values: {valid}.",
            )

    jobs, total = db_manager.get_all_jobs(status=job_status, limit=limit, offset=offset)

    return JobsListResponse(
        pagination=PaginationInfo(total=total, limit=limit, offset=offset),
        data=[_job_to_state(j) for j in jobs],
    )


# ---------------------------------------------------------------------------
# GET /v1/translate/jobs/{job_id}
# ---------------------------------------------------------------------------

@router.get(
    "/{job_id}",
    response_model=JobDetailResponse,
    response_description="Full job status, phase metadata, and timestamps.",
    summary="Get job details",
    description="Return full status and metadata for a specific job.",
    responses={
        404: http_error_responses[404],   # job not found
        500: http_error_responses[500],
    },
)
async def get_job(job_id: str) -> JobDetailResponse:
    """Return detailed status and metadata for a translation job."""
    job = db_manager.get_job_by_id(job_id)
    if job is None:
        APIError.raise_error(
            ErrorCode.RESOURCE_NOT_FOUND,
            f"No job found with id '{job_id}'.",
        )
    return _job_to_detail(job)


# ---------------------------------------------------------------------------
# GET /v1/translate/jobs/{job_id}/result
# ---------------------------------------------------------------------------

@router.get(
    "/{job_id}/result",
    response_model=JobResultResponse,
    response_description="Translation result with metadata and token usage.",
    summary="Get translation result",
    description=(
        "Retrieve the full translation result as JSON. "
        "Returns 202 with current status if the job is still running, 410 if it failed."
    ),
    responses={
        202: {"description": "Accepted — job is still in progress. Retry after a short delay."},
        404: http_error_responses[404],   # job not found
        410: {"description": "Gone — job failed. Error details on the job resource."},
        500: http_error_responses[500],
    },
)
async def get_job_result(job_id: str) -> Union[JobResultResponse, JSONResponse]:
    """Return the completed translation result for a job."""
    job = db_manager.get_job_by_id(job_id)
    if job is None:
        APIError.raise_error(
            ErrorCode.RESOURCE_NOT_FOUND,
            f"No job found with id '{job_id}'.",
        )

    if job.status in (JobStatus.ACCEPTED.value, JobStatus.IN_PROGRESS.value):
        return JSONResponse(
            status_code=status.HTTP_202_ACCEPTED,
            content={
                "job_id": job_id,
                "status": job.status,
                "message": f"Job is still {job.status}. Poll GET /v1/translate/jobs/{job_id} for status.",
            },
        )

    if job.status == JobStatus.FAILED.value:
        _raise_job_failed(f"Job failed: {job.error or 'unknown error'}")

    try:
        result = read_result_file(job_id)
    except FileNotFoundError:
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            f"Result file for job '{job_id}' not found on disk.",
        )
    except Exception as exc:
        logger.error(f"Error reading result for job {job_id}: {exc}", exc_info=True)
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            "Unexpected error reading translation result.",
        )

    return JobResultResponse(
        data=result.get("data", {}),
        meta=result.get("meta", {}),
        usage=result.get("usage", {}),
    )


# ---------------------------------------------------------------------------
# GET /v1/translate/jobs/{job_id}/result/download
# ---------------------------------------------------------------------------

@router.get(
    "/{job_id}/result/download",
    response_description="Translated file stream with Content-Disposition attachment header.",
    summary="Download translated file",
    description=(
        "Download the translated content as a plain-text file. "
        "Extension and MIME type match the original upload "
        "(.txt → text/plain, .md → text/markdown)."
    ),
    responses={
        202: {"description": "Accepted — job is still in progress. Retry after a short delay."},
        404: http_error_responses[404],   # job not found
        410: {"description": "Gone — job failed. Error details on the job resource."},
        500: http_error_responses[500],
    },
)
async def download_job_result(job_id: str) -> Union[PlainTextResponse, JSONResponse]:
    """Download the translated output file for a completed job."""
    job = db_manager.get_job_by_id(job_id)
    if job is None:
        APIError.raise_error(
            ErrorCode.RESOURCE_NOT_FOUND,
            f"No job found with id '{job_id}'.",
        )

    if job.status in (JobStatus.ACCEPTED.value, JobStatus.IN_PROGRESS.value):
        return JSONResponse(
            status_code=status.HTTP_202_ACCEPTED,
            content={
                "job_id": job_id,
                "status": job.status,
                "message": f"Job is still {job.status}. Poll GET /v1/translate/jobs/{job_id} for status.",
            },
        )

    if job.status == JobStatus.FAILED.value:
        _raise_job_failed(f"Job failed: {job.error or 'unknown error'}")

    try:
        result = read_result_file(job_id)
    except FileNotFoundError:
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            f"Result file for job '{job_id}' not found on disk.",
        )
    except Exception as exc:
        logger.error(
            f"Error reading result for download (job {job_id}): {exc}", exc_info=True
        )
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            "Unexpected error reading translation result.",
        )

    translation_text: str = result.get("data", {}).get("translation", "")
    input_type: str = job.input_type  # "txt" or "md"
    document_name: str = job.document_name or f"{job_id}.{input_type}"

    # Derive download filename: strip extension then append "_translated.<ext>".
    base_name = document_name.rsplit(".", 1)[0] if "." in document_name else document_name
    download_filename = f"{base_name}_translated.{input_type}"

    media_type = "text/markdown" if input_type == "md" else "text/plain"

    return PlainTextResponse(
        content=translation_text,
        media_type=f"{media_type}; charset=utf-8",
        headers={
            "Content-Disposition": f'attachment; filename="{download_filename}"',
        },
    )
