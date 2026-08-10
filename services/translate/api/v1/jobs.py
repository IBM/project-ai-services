"""
Async job endpoints.

POST   /v1/translate/jobs                          — create job
GET    /v1/translate/jobs                          — list jobs (paginated, status filter)
GET    /v1/translate/jobs/{job_id}                 — job detail
GET    /v1/translate/jobs/{job_id}/result          — get result JSON
GET    /v1/translate/jobs/{job_id}/result/download — download translated file
"""

from datetime import datetime, timezone
from typing import Optional

from fastapi import APIRouter, BackgroundTasks, File, Form, HTTPException, UploadFile, status
from fastapi.responses import PlainTextResponse

from common.error_utils import APIError, ErrorCode
from common.misc_utils import get_logger
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
    validate_file_extension,
)
from translate.workers.concurrency import concurrency_manager
from translate.workers.translation_worker import run_translation_job

logger = get_logger("jobs_api")

router = APIRouter()

# ---------------------------------------------------------------------------
# Translation-specific HTTP error helpers
# (codes not in the shared ErrorCode enum get their own raise helpers)
# ---------------------------------------------------------------------------

def _raise_invalid_language(detail: str) -> None:
    raise HTTPException(
        status_code=400,
        detail={"error": {"code": "INVALID_LANGUAGE", "message": detail, "status": 400}},
    )

def _raise_same_language(detail: str) -> None:
    raise HTTPException(
        status_code=400,
        detail={"error": {"code": "SAME_LANGUAGE", "message": detail, "status": 400}},
    )

def _raise_unsupported_file_type(detail: str) -> None:
    raise HTTPException(
        status_code=415,
        detail={"error": {"code": "UNSUPPORTED_FILE_TYPE", "message": detail, "status": 415}},
    )

def _raise_file_too_large(detail: str) -> None:
    raise HTTPException(
        status_code=413,
        detail={"error": {"code": "FILE_TOO_LARGE", "message": detail, "status": 413}},
    )

def _raise_job_not_complete(detail: str) -> None:
    raise HTTPException(
        status_code=409,
        detail={"error": {"code": "JOB_NOT_COMPLETE", "message": detail, "status": 409}},
    )

def _raise_job_failed(detail: str) -> None:
    raise HTTPException(
        status_code=410,
        detail={"error": {"code": "JOB_FAILED", "message": detail, "status": 410}},
    )

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _normalise_language(value: Optional[str]) -> str:
    """Lowercase + strip; treat empty/None as 'auto'."""
    if not value or not value.strip():
        return "auto"
    return value.strip().lower()


def _validate_languages(
    source_language: str,
    target_language: str,
) -> tuple[str, str]:
    """
    Validate and normalise both language parameters.

    Returns ``(normalised_source, normalised_target)`` on success.
    Raises ``HTTPException`` on any validation failure.
    """
    supported = settings.translate.supported_languages_list
    supported_display = ", ".join(s.capitalize() for s in sorted(supported))

    norm_source = _normalise_language(source_language)
    norm_target = _normalise_language(target_language)

    # target must not be "auto"
    if norm_target == "auto":
        _raise_invalid_language(
            f"'auto' is not valid for target_language. "
            f"Please specify an explicit target language. Supported: {supported_display}."
        )

    # target must be in allowlist
    if norm_target not in supported:
        _raise_invalid_language(
            f"'{target_language}' is not a supported language. "
            f"Supported: {supported_display}."
        )

    # source (if explicit) must be in allowlist
    if norm_source != "auto" and norm_source not in supported:
        _raise_invalid_language(
            f"'{source_language}' is not a supported language. "
            f"Supported: {supported_display}. "
            f"Use 'auto' for source_language to auto-detect."
        )

    # explicit source must differ from target
    if norm_source != "auto" and norm_source == norm_target:
        _raise_same_language(
            f"source_language and target_language must differ "
            f"(both are '{norm_source}')."
        )

    return norm_source, norm_target


def _format_dt(dt: Optional[datetime]) -> Optional[str]:
    """Return ISO-8601 string for *dt*, or None."""
    if dt is None:
        return None
    return dt.isoformat()


def _job_to_detail(job) -> JobDetailResponse:
    return JobDetailResponse(
        job_id=job.job_id,
        job_name=job.job_name,
        status=job.status,
        source_language=job.source_language,
        target_language=job.target_language,
        input_type=job.input_type,
        document_name=job.document_name,
        submitted_at=_format_dt(job.submitted_at),
        completed_at=_format_dt(job.completed_at),
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
        submitted_at=_format_dt(job.submitted_at),
        completed_at=_format_dt(job.completed_at),
        error=job.error,
    )


# ---------------------------------------------------------------------------
# POST /v1/translate/jobs
# ---------------------------------------------------------------------------

@router.post(
    "",
    status_code=status.HTTP_202_ACCEPTED,
    response_model=JobCreatedResponse,
    summary="Submit a file for async translation",
    description=(
        "Upload a `.txt` or `.md` file and specify a target language. "
        "Returns a ``job_id`` for polling. "
        "Source language is auto-detected when omitted."
    ),
)
async def create_translation_job(
    background_tasks: BackgroundTasks,
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
    filename = file.filename or "upload"

    # 1. Validate file extension.
    try:
        input_type = validate_file_extension(filename)
    except ValueError as exc:
        _raise_unsupported_file_type(str(exc))

    # 2. Validate languages.
    norm_source, norm_target = _validate_languages(source_language, target_language)

    # 3. Check file size before reading content.
    content = await file.read()
    max_bytes = settings.translate.max_upload_size_mb * 1024 * 1024
    if len(content) > max_bytes:
        _raise_file_too_large(
            f"File size {len(content) // 1024} KB exceeds the "
            f"{settings.translate.max_upload_size_mb} MB limit."
        )

    # 4. Check job admission semaphore — non-blocking try.
    if concurrency_manager.job_limiter.locked():
        APIError.raise_error(
            ErrorCode.RATE_LIMIT_EXCEEDED,
            f"Maximum concurrent jobs ({settings.translate.max_concurrent_jobs}) "
            "already running. Please try again later.",
        )

    # 5. Generate job ID; stage the file.
    job_id = generate_uuid()
    staged_path = await stage_uploaded_file(
        job_id=job_id,
        filename=filename,
        content=content,
    )

    # 6. Insert DB row with status=accepted.
    db_manager.create_job(
        job_id=job_id,
        source_language=norm_source,
        target_language=norm_target,
        input_type=input_type,
        job_name=job_name,
        document_name=filename,
        submitted_at=datetime.now(timezone.utc),
    )

    # 7. Launch background worker.
    background_tasks.add_task(
        run_translation_job,
        job_id=job_id,
        staged_file_path=staged_path,
        source_language=norm_source,
        target_language=norm_target.capitalize(),
        document_name=filename,
        input_type=input_type,
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
    summary="List translation jobs",
    description="Paginated list of jobs with optional status filter.",
)
async def list_jobs(
    limit: int = 20,
    offset: int = 0,
    status_filter: Optional[str] = None,
) -> JobsListResponse:
    if limit < 1 or limit > 100:
        APIError.raise_error(
            ErrorCode.INVALID_PARAMETER,
            "limit must be between 1 and 100.",
        )
    if offset < 0:
        APIError.raise_error(
            ErrorCode.INVALID_PARAMETER,
            "offset must be >= 0.",
        )

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
    summary="Get job details",
    description="Return full status and metadata for a specific job.",
)
async def get_job(job_id: str) -> JobDetailResponse:
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
    summary="Get translation result",
    description=(
        "Retrieve the full translation result as JSON. "
        "Returns 409 if the job is still running, 410 if it failed."
    ),
)
async def get_job_result(job_id: str) -> JobResultResponse:
    job = db_manager.get_job_by_id(job_id)
    if job is None:
        APIError.raise_error(
            ErrorCode.RESOURCE_NOT_FOUND,
            f"No job found with id '{job_id}'.",
        )

    if job.status in (JobStatus.ACCEPTED.value, JobStatus.IN_PROGRESS.value):
        _raise_job_not_complete(
            f"Job is still {job.status}. "
            f"Poll GET /v1/translate/jobs/{job_id} for status."
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
    summary="Download translated file",
    description=(
        "Download the translated content as a plain-text file. "
        "Extension and MIME type match the original upload "
        "(.txt → text/plain, .md → text/markdown)."
    ),
)
async def download_job_result(job_id: str):
    job = db_manager.get_job_by_id(job_id)
    if job is None:
        APIError.raise_error(
            ErrorCode.RESOURCE_NOT_FOUND,
            f"No job found with id '{job_id}'.",
        )

    if job.status in (JobStatus.ACCEPTED.value, JobStatus.IN_PROGRESS.value):
        _raise_job_not_complete(
            f"Job is still {job.status}. "
            f"Poll GET /v1/translate/jobs/{job_id} for status."
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

