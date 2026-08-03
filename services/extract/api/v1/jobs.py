"""Job-related API endpoints.

Handles job creation, listing, retrieval, and deletion.

Exposes one router:
- ``router`` → mounted at ``/v1/extract``
"""

import uuid
from datetime import datetime, timezone
from typing import Optional

from fastapi import APIRouter, BackgroundTasks, File, Form, Query, UploadFile
from fastapi.responses import JSONResponse, Response

from common.error_utils import http_error_responses
from common.misc_utils import cleanup_staging_directory, get_logger

from extract.db.manager import db_repo
from extract.models import (
    DocumentInfo,
    JobCreatedResponse,
    JobDetailResponse,
    JobListItem,
    JobResultResponse,
    JobsListResponse,
    PaginationInfo,
)
from extract.settings import settings
from extract.state import job_limiter
from extract.utils.job import (
    delete_all_job_files,
    delete_job_files,
    read_result_file,
    stage_uploaded_file,
    validate_file_extension,
)
from extract.utils.schema import SchemaValidationError

router = APIRouter()
logger = get_logger("jobs_router")


# ---------------------------------------------------------------------------
# Helper
# ---------------------------------------------------------------------------

def _fmt_dt(dt: Optional[datetime]) -> Optional[str]:
    """Return an ISO-8601 string (UTC) or None."""
    if dt is None:
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.isoformat()


# ---------------------------------------------------------------------------
# POST /v1/extract — Synchronous extraction stub
# ---------------------------------------------------------------------------

@router.post("", tags=["extraction"], include_in_schema=True)
async def extract_sync():
    """Synchronous extraction — implementation in follow-up iteration."""
    raise SchemaValidationError("NOT_IMPLEMENTED", "POST /v1/extract not yet implemented.", status=501)


# ---------------------------------------------------------------------------
# POST /v1/extract/jobs — Submit an async extraction job
# ---------------------------------------------------------------------------

@router.post(
    "/jobs",
    status_code=202,
    response_model=JobCreatedResponse,
    responses={
        202: {"description": "Job accepted"},
        400: http_error_responses[400],
        404: http_error_responses[404],
        415: http_error_responses[415],
        429: http_error_responses[429],
        500: http_error_responses[500],
    },
    summary="Create async extraction job",
    description=(
        "Submit a `.txt` or `.pdf` file for asynchronous entity extraction "
        "against a registered schema.  Returns immediately with a `job_id`.\n\n"
        "**Form parameters:**\n"
        "- `file` (required): A single `.txt` or `.pdf` file\n"
        "- `schema_id` (required): ID of a registered extraction schema\n"
        "- `job_name` (optional): Human-readable label for the job\n"
    ),
    tags=["jobs"],
)
async def create_extract_job(
    background_tasks: BackgroundTasks,
    file: UploadFile = File(...),
    schema_id: str = Form(...),
    job_name: Optional[str] = Form(None),
) -> JobCreatedResponse:
    """Validate, stage, record, and enqueue an async extraction job."""
    # --- Job admission check ---
    if job_limiter.locked():
        raise SchemaValidationError(
            "RATE_LIMIT_EXCEEDED",
            "Job concurrency limit reached. Please try again later.",
            status=429,
        )

    # --- File extension validation ---
    filename = file.filename or ""
    is_valid, ext = validate_file_extension(filename)
    if not is_valid:
        raise SchemaValidationError(
            "UNSUPPORTED_FILE_TYPE",
            f"Only .txt and .md files are accepted. Received: {ext or 'unknown'}",
            status=415,
        )

    # --- Schema existence check ---
    schema_row = db_repo.get_schema_by_id(schema_id)
    if schema_row is None:
        raise SchemaValidationError(
            "SCHEMA_NOT_FOUND",
            f"No schema with id {schema_id!r}.",
            status=404,
        )

    # --- Generate job ID ---
    job_id = str(uuid.uuid4())
    source_type = ext.lstrip(".")  # "txt" or "md"

    # --- Stage the uploaded file ---
    try:
        stage_uploaded_file(job_id, file)
    except IOError as exc:
        logger.error(f"Failed to stage file for job {job_id}: {exc}")
        raise SchemaValidationError(
            "FILE_STAGING_ERROR",
            "Failed to save uploaded file.",
            status=500,
        )

    # --- Persist job record ---
    row = db_repo.create_job(
        job_id=job_id,
        schema_id=schema_id,
        document_name=filename,
        source_type=source_type,
        job_name=job_name,
        submitted_at=datetime.now(timezone.utc),
    )
    if row is None:
        cleanup_staging_directory(job_id, settings.extract.staging_dir)
        raise SchemaValidationError(
            "DATABASE_ERROR",
            "Failed to create job record.",
            status=500,
        )

    # --- Enqueue background worker ---
    background_tasks.add_task(_process_extract_job, job_id)

    logger.info(f"Accepted extraction job {job_id} (schema={schema_id}, file={filename!r})")
    return JobCreatedResponse(job_id=job_id)


async def _process_extract_job(job_id: str) -> None:
    """
    Background worker stub.

    A full worker (tokenization, vLLM call,
    schema validation) will be added in a follow-up iteration.
    For now it simply acquires the job_limiter slot so the semaphore
    accounting is correct and immediately releases it.
    """
    async with job_limiter:
        logger.info(f"Background worker invoked for job {job_id} (stub — no processing yet)")


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs — List jobs with pagination and filters
# ---------------------------------------------------------------------------

@router.get(
    "/jobs",
    response_model=JobsListResponse,
    responses={
        200: {"description": "Paginated job list"},
        400: http_error_responses[400],
        500: http_error_responses[500],
    },
    summary="List extraction jobs",
    description=(
        "Return a paginated list of extraction jobs.\n\n"
        "**Query parameters:**\n"
        "- `latest` (bool): Return only the most-recent job. Default: false\n"
        "- `limit` (int): Records per page (1–100). Default: 20\n"
        "- `offset` (int): Records to skip. Default: 0\n"
        "- `status` (string): Filter by `accepted`, `in_progress`, `completed`, or `failed`\n"
        "- `schema_id` (string): Filter jobs by the schema they extract against\n"
    ),
    tags=["jobs"],
)
async def list_extract_jobs(
    latest: Optional[bool] = Query(default=None, description="Return only the most recent job"),
    limit: int = Query(default=20, ge=1, le=100, description="Records per page"),
    offset: int = Query(default=0, ge=0, description="Records to skip"),
    status: Optional[str] = Query(default=None, description="Status filter"),
    schema_id: Optional[str] = Query(default=None, description="Filter by schema_id"),
) -> JobsListResponse:
    _VALID_STATUSES = {"accepted", "in_progress", "completed", "failed"}
    if status is not None and status not in _VALID_STATUSES:
        raise SchemaValidationError(
            "INVALID_PARAMETER",
            f"Invalid status value. Must be one of: {', '.join(sorted(_VALID_STATUSES))}",
            status=400,
        )

    rows, total = db_repo.list_jobs(
        status=status,
        schema_id=schema_id,
        limit=limit,
        offset=offset,
        latest=bool(latest),
    )

    data = [
        JobListItem(
            job_id=row.job_id,
            job_name=row.job_name,
            schema_id=row.schema_id,
            status=row.status,
            document_name=row.document_name,
            submitted_at=_fmt_dt(row.submitted_at),
            completed_at=_fmt_dt(row.completed_at),
        )
        for row in rows
    ]
    effective_limit = 1 if latest else limit
    effective_offset = 0 if latest else offset
    return JobsListResponse(
        pagination=PaginationInfo(total=total, limit=effective_limit, offset=effective_offset),
        data=data,
    )


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id} — Full job status
# ---------------------------------------------------------------------------

@router.get(
    "/jobs/{job_id}",
    response_model=JobDetailResponse,
    responses={
        200: {"description": "Job details"},
        404: http_error_responses[404],
        500: http_error_responses[500],
    },
    summary="Get job details",
    description=(
        "Retrieve the full status of a specific extraction job, including "
        "the document block, current processing phase, error message, and "
        "token diagnostics persisted in the job's `metadata` JSONB column."
    ),
    tags=["jobs"],
)
async def get_extract_job(job_id: str) -> JobDetailResponse:
    row = db_repo.get_job_by_id(job_id)
    if row is None:
        raise SchemaValidationError(
            "RESOURCE_NOT_FOUND",
            f"Job {job_id!r} not found.",
            status=404,
        )

    return JobDetailResponse(
        job_id=row.job_id,
        job_name=row.job_name,
        schema_id=row.schema_id,
        status=row.status,
        document=DocumentInfo(
            name=row.document_name,
            source_type=row.source_type,
            digitize_job_id=row.digitize_job_id,
            digitize_doc_id=row.digitize_doc_id,
        ),
        metadata=row.job_metadata,
        submitted_at=_fmt_dt(row.submitted_at),
        completed_at=_fmt_dt(row.completed_at),
        error=row.error,
    )


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id}/result — Retrieve extraction result
# ---------------------------------------------------------------------------

@router.get(
    "/jobs/{job_id}/result",
    response_model=JobResultResponse,
    responses={
        200: {"description": "Extraction result"},
        202: {"description": "Job still in progress"},
        404: http_error_responses[404],
        409: {"description": "Job failed — result unavailable; inspect the job resource"},
        500: http_error_responses[500],
    },
    summary="Get extraction result",
    description=(
        "Retrieve the extraction result for a completed job.\n\n"
        "- **202** while the job is `accepted` or `in_progress`.\n"
        "- **409** if the job exists but `failed` — the body points at the job "
        "resource so the caller can inspect the error and diagnostics rather "
        "than receiving a generic 404 that conflates 'gone' with 'failed'.\n"
        "- **404** if no job with this ID exists at all.\n"
        "- **200** with the result payload once the job is `completed`."
    ),
    tags=["jobs"],
)
async def get_extract_job_result(job_id: str):
    row = db_repo.get_job_by_id(job_id)
    if row is None:
        raise SchemaValidationError(
            "RESOURCE_NOT_FOUND",
            f"Job {job_id!r} not found.",
            status=404,
        )

    if row.status in ("accepted", "in_progress"):
        return JSONResponse(
            status_code=202,
            content={
                "message": "Job is still in progress.",
                "job_id": job_id,
                "status": row.status,
            },
        )

    if row.status == "failed":
        return JSONResponse(
            status_code=409,
            content={
                "error": {
                    "code": "JOB_FAILED",
                    "message": (
                        f"Job {job_id!r} failed and has no result. "
                        f"Inspect GET /v1/extract/jobs/{job_id} for details."
                    ),
                    "status": 409,
                    "job_id": job_id,
                }
            },
        )

    # status == "completed" — read result file from disk
    result_data = read_result_file(job_id)
    if result_data is None:
        logger.error(f"Result file missing for completed job {job_id}")
        raise SchemaValidationError(
            "INTERNAL_SERVER_ERROR",
            "Result file not found for completed job.",
            status=500,
        )

    return JobResultResponse(
        data=result_data.get("data", {}),
        status=result_data.get("status", "completed"),
        meta=result_data.get("meta", {}),
        usage=result_data.get("usage", {}),
    )


# ---------------------------------------------------------------------------
# DELETE /v1/extract/jobs/{job_id} — Delete a single job
# ---------------------------------------------------------------------------

@router.delete(
    "/jobs/{job_id}",
    status_code=204,
    responses={
        204: {"description": "Job and result deleted"},
        404: http_error_responses[404],
        409: {"description": "Job is still active (accepted or in_progress)"},
        500: http_error_responses[500],
    },
    summary="Delete extraction job",
    description=(
        "Delete a job record and its result file.  "
        "Returns **409 Conflict** if the job is `accepted` or `in_progress`. "
        "Does **not** delete the digitized document in the Digitize service — "
        "that lifecycle is managed independently via the Digitize API."
    ),
    tags=["jobs"],
)
async def delete_extract_job(job_id: str) -> Response:
    row = db_repo.get_job_by_id(job_id)
    if row is None:
        raise SchemaValidationError(
            "RESOURCE_NOT_FOUND",
            f"Job {job_id!r} not found.",
            status=404,
        )

    if row.status in ("accepted", "in_progress"):
        raise SchemaValidationError(
            "RESOURCE_LOCKED",
            f"Cannot delete active job {job_id!r}. Current status: {row.status}.",
            status=409,
        )

    delete_job_files(job_id)

    success = db_repo.delete_job(job_id)
    if not success:
        raise SchemaValidationError(
            "INTERNAL_SERVER_ERROR",
            "Failed to delete job from database.",
            status=500,
        )

    logger.info(f"Deleted job {job_id!r}")
    return Response(status_code=204)


# ---------------------------------------------------------------------------
# DELETE /v1/extract/jobs — Bulk delete (confirm=true required)
# ---------------------------------------------------------------------------

@router.delete(
    "/jobs",
    status_code=204,
    responses={
        204: {"description": "All jobs and results deleted"},
        400: http_error_responses[400],
        409: {"description": "Active jobs exist"},
        500: http_error_responses[500],
    },
    summary="Bulk delete all extraction jobs",
    description=(
        "Delete **all** extraction job records, result files, and any "
        "remaining staging directories.\n\n"
        "Requires `?confirm=true`.\n\n"
        "Returns **409 Conflict** if any job is `accepted` or `in_progress`."
    ),
    tags=["jobs"],
)
async def bulk_delete_extract_jobs(
    confirm: Optional[str] = Query(
        default=None,
        description="Must be 'true' to confirm destructive bulk deletion",
    ),
) -> Response:
    if confirm != "true":
        raise SchemaValidationError(
            "CONFIRMATION_REQUIRED",
            "Bulk delete requires ?confirm=true.",
            status=400,
        )

    if db_repo.has_active_jobs():
        raise SchemaValidationError(
            "RESOURCE_LOCKED",
            "Cannot bulk-delete: one or more active jobs exist. "
            "Wait for them to complete or cancel them individually.",
            status=409,
        )

    delete_all_job_files()

    success = db_repo.delete_all_jobs()
    if not success:
        raise SchemaValidationError(
            "INTERNAL_SERVER_ERROR",
            "Failed to delete jobs from database.",
            status=500,
        )

    logger.info("Bulk deleted all extraction jobs")
    return Response(status_code=204)
