"""Job-related API endpoints.

Handles extraction (sync) and job CRUD.

Exposes one router:
- ``router`` → mounted at ``/v1/extract``
"""

import asyncio
import json
import os
import time
import unicodedata
import uuid
from datetime import datetime, timezone
from typing import Optional

from fastapi import APIRouter, BackgroundTasks, File, Form, Query, Request, UploadFile
from fastapi.responses import JSONResponse, Response
from common.error_utils import http_error_responses
from common.misc_utils import cleanup_staging_directory, get_llm_endpoint, get_logger

from extract.db.manager import db_repo
from extract.models import (
    DocumentInfo,
    ExtractionRequest,
    ExtractionResponse,
    JobCreatedResponse,
    JobDetailResponse,
    JobListItem,
    JobResultResponse,
    JobsListResponse,
    PaginationInfo,
)
from extract.state import concurrency_limiter
from extract.settings import settings
from extract.utils.exceptions import ExtractException
from extract.utils.request import check_request_body_size
from extract.utils.vllm import (
    build_messages,
    call_vllm_safe,
    render_few_shot_block,
    validate_with_retry,
)
from extract.utils.job import (
    delete_all_job_files,
    delete_job_files,
    read_result_file,
    stage_uploaded_file,
    validate_file_extension,
)
from extract.utils.schema import (
    SchemaValidationError,
    _tokenize,
    check_extraction_budget,
    fmt_dt,
)

router = APIRouter()
logger = get_logger("jobs_router")


# ---------------------------------------------------------------------------
# POST /v1/extract — Synchronous extraction
# ---------------------------------------------------------------------------

@router.post(
    "",
    response_model=ExtractionResponse,
    status_code=200,
    tags=["extraction"],
    summary="Synchronous extraction",
    description=(
        "Extract structured data from plain text against a registered schema in a "
        "single blocking call.  Returns validated, schema-conformant JSON.\n\n"
        "**Processing steps:**\n"
        "1. Enforce request-body size limit (413 `REQUEST_TOO_LARGE`) before tokenization.\n"
        "2. Validate `schema_id` (404) and `text` non-empty (400).\n"
        "3. Acquire the global vLLM concurrency slot (429 if saturated).\n"
        "4. Tokenise `text` via vLLM `/tokenize` (exact count).\n"
        "5. Hard context-window guard — 413 `CONTEXT_LIMIT_EXCEEDED` with full token "
        "diagnostics (`input_tokens`, `schema_tokens`, `examples_tokens`, "
        "`custom_prompt_tokens`, `prompt_overhead_tokens`, `reserved_output_tokens`) "
        "on breach.\n"
        "6. Build prompt: system prompt + `{custom_prompt}` + schema + few-shot block + text.\n"
        "7. Call vLLM with `guided_json` (when `GUIDED_DECODING_ENABLED=true`) and "
        "`temperature=0.0`. `finish_reason=length` → 413 `OUTPUT_BUDGET_EXCEEDED` "
        "(no validation retry burned).\n"
        "8. Server-side `jsonschema` validation; one bounded retry on failure (validation "
        "errors appended to prompt). 422 `EXTRACTION_VALIDATION_FAILED` if retry also fails.\n"
        "9. Return extraction + token usage + timing.\n\n"
        "The semaphore slot is held across both the initial call and the retry."
    ),
    responses={
        400: http_error_responses[400],
        404: http_error_responses[404],
        413: http_error_responses[413],
        422: {"description": "Extraction output failed schema validation after retry"},
        429: http_error_responses[429],
        500: http_error_responses[500],
        503: http_error_responses[503],
    },
    include_in_schema=True,
)
async def extract_sync(request: Request) -> JSONResponse:
    """Synchronous entity extraction — blocking call with schema-validated JSON output."""
    t_start = time.monotonic()

    # ------------------------------------------------------------------
    # 0. Request-body size guard — before any parsing or tokenisation
    # ------------------------------------------------------------------
    raw_body = await check_request_body_size(request)

    # ------------------------------------------------------------------
    # 1. Parse & basic field validation
    # ------------------------------------------------------------------
    try:
        body_dict = json.loads(raw_body)
    except Exception:
        raise ExtractException(400, "INVALID_JSON", "Request body is not valid JSON.")

    try:
        body = ExtractionRequest(**body_dict)
    except Exception as exc:
        raise ExtractException(400, "INVALID_REQUEST", f"Invalid request body: {exc}")

    schema_row = _resolve_schema(body.schema_id)

    # ------------------------------------------------------------------
    # 2. Semaphore check (non-blocking — reject immediately if saturated)
    # ------------------------------------------------------------------
    if concurrency_limiter.locked():
        raise ExtractException(
            429, "RATE_LIMIT_EXCEEDED",
            "Server is at maximum vLLM concurrency. Please retry later.",
        )

    # ------------------------------------------------------------------
    # Resolve LLM config once — cached after the first call, but kept
    # outside the semaphore to avoid holding the slot during any cold-start
    # network probes.
    # ------------------------------------------------------------------
    llm_model_dict = get_llm_endpoint()
    llm_endpoint: str = llm_model_dict.get("llm_endpoint", "")
    llm_model: str = llm_model_dict.get("llm_model", "")
    max_model_len: int = settings.common.llm.max_model_len

    # ------------------------------------------------------------------
    # 3–8. Core extraction
    #       One semaphore slot held across BOTH the initial call and the
    #       validation retry so a second attempt cannot be starved.
    # ------------------------------------------------------------------
    async with concurrency_limiter:

        # ── 3. Exact input token count via /tokenize ─────────────────────
        try:
            input_tokens: int = await asyncio.to_thread(
                _tokenize, body.text, llm_endpoint
            )
        except Exception as exc:
            logger.error(f"Tokenization failed: {exc}", exc_info=True)
            raise ExtractException(
                503, "TOKENIZATION_ERROR",
                "Failed to tokenise the input text. "
                "Ensure the vLLM /tokenize endpoint is reachable.",
            )

        # ── 4. Hard context-window guard ─────────────────────────────────
        #       check_extraction_budget raises SchemaValidationError(413)
        #       with full diagnostics — catch and re-raise as ExtractException.
        try:
            reserved_output = check_extraction_budget(
                input_tokens=input_tokens,
                schema_tokens=schema_row.schema_tokens,
                examples_tokens=schema_row.examples_tokens,
                custom_prompt_tokens=schema_row.custom_prompt_tokens,
                max_model_len=max_model_len,
            )
        except SchemaValidationError as budget_exc:
            raise ExtractException(
                budget_exc.status,
                budget_exc.code,
                budget_exc.message,
                details=budget_exc.details,
            ) from budget_exc

        # ── 5. Prompt assembly ────────────────────────────────────────────
        few_shot_block = render_few_shot_block(schema_row.examples)
        messages = build_messages(
            normalized_schema=schema_row.json_schema,
            few_shot_block=few_shot_block,
            input_text=body.text,
            custom_prompt=schema_row.custom_prompt,
        )

        # ── 6. First vLLM call ────────────────────────────────────────────
        vllm_resp = await call_vllm_safe(
            messages, reserved_output, schema_row.json_schema, llm_endpoint, llm_model
        )

        choices = vllm_resp.get("choices", [])
        if not choices:
            raise ExtractException(500, "LLM_ERROR", "vLLM returned an empty choices list.")

        choice = choices[0]
        finish_reason: str = choice.get("finish_reason", "")

        # ── 7. Output-budget exceeded — fail fast, do NOT burn the retry ─
        if finish_reason == "length":
            raise ExtractException(
                413, "OUTPUT_BUDGET_EXCEEDED",
                "The model output was truncated because it reached the reserved "
                "output token limit. Adjust OUTPUT_TOKEN_FACTOR, MIN_OUTPUT_TOKENS, "
                "or MAX_OUTPUT_TOKENS via environment configuration.",
                details={
                    "reserved_output_tokens": reserved_output,
                    "finish_reason": "length",
                },
            )

        raw_output: str = choice.get("message", {}).get("content", "") or ""
        usage = vllm_resp.get("usage", {})
        total_prompt_tokens: int = usage.get("prompt_tokens", 0)
        total_completion_tokens: int = usage.get("completion_tokens", 0)

        # ── 8. Server-side validation + one bounded retry ─────────────────
        parsed_output, validation_attempts, extra_pt, extra_ct = await validate_with_retry(
            raw_output, messages, reserved_output,
            schema_row.json_schema, llm_endpoint, llm_model,
        )
        total_prompt_tokens += extra_pt
        total_completion_tokens += extra_ct

    # ------------------------------------------------------------------
    # 9. Return response
    # ------------------------------------------------------------------
    processing_time_ms = int((time.monotonic() - t_start) * 1000)

    return JSONResponse(
        status_code=200,
        content={
            "data": {
                "extraction": parsed_output,
                "schema_id": body.schema_id,
                "source": {
                    "input_type": "text",
                    "input_tokens": input_tokens,
                },
            },
            "meta": {
                "model": llm_model,
                "processing_time_ms": processing_time_ms,
                "validation_attempts": validation_attempts,
            },
            "usage": {
                "input_tokens": total_prompt_tokens,
                "output_tokens": total_completion_tokens,
                "total_tokens": total_prompt_tokens + total_completion_tokens,
            },
        },
    )


# ---------------------------------------------------------------------------
# create_extract_job — private helpers
# ---------------------------------------------------------------------------

def _check_job_admission() -> None:
    """Raise 429 if the job concurrency slot is exhausted."""
    from extract.state import job_limiter  # deferred to avoid circular import
    if job_limiter.locked():
        raise ExtractException(
            429, "RATE_LIMIT_EXCEEDED",
            "Job concurrency limit reached. Please try again later.",
        )


def _validate_and_resolve_file(file: UploadFile) -> tuple[str, str]:
    """Normalise the filename and validate its extension.

    Returns:
        (normalised_filename, source_type)  e.g. ("report.txt", "txt")

    Raises:
        ExtractException(415) on an unsupported or missing extension.
    """
    filename = (file.filename or "").lower()
    is_valid, ext = validate_file_extension(filename)
    if not is_valid:
        raw_ext = os.path.splitext(filename)[1] or "unknown"
        raise ExtractException(
            415, "UNSUPPORTED_FILE_TYPE",
            f"Only .txt and .md files are accepted. Received: {raw_ext}",
        )
    return filename, (ext or "").lstrip(".")


def _resolve_schema(schema_id: str):
    """Return the schema row for *schema_id*.

    Raises:
        ExtractException(404) if the schema does not exist.
    """
    row = db_repo.get_schema_by_id(schema_id)
    if row is None:
        raise ExtractException(404, "SCHEMA_NOT_FOUND", f"No schema with id {schema_id!r}.")
    return row


def _stage_and_persist(
    job_id: str,
    file: UploadFile,
    schema_id: str,
    filename: str,
    source_type: str,
    job_name: Optional[str],
) -> None:
    """Stage the uploaded file then write the DB record.

    Cleans up the staging directory on any failure after staging so no
    orphaned files are left on disk.

    Raises:
        ExtractException(500) on staging or database failure.
    """
    try:
        stage_uploaded_file(job_id, file)
    except IOError as exc:
        logger.error(f"Failed to stage file for job {job_id}: {exc}")
        raise ExtractException(500, "FILE_STAGING_ERROR", "Failed to save uploaded file.")

    try:
        row = db_repo.create_job(
            job_id=job_id,
            schema_id=schema_id,
            document_name=filename,
            source_type=source_type,
            job_name=job_name,
            submitted_at=datetime.now(timezone.utc),
        )
    except Exception as exc:
        logger.error(f"Unexpected DB error creating job {job_id}: {exc}")
        cleanup_staging_directory(job_id, settings.extract.staging_dir)
        raise ExtractException(500, "DATABASE_ERROR", "Failed to create job record.")

    if row is None:
        cleanup_staging_directory(job_id, settings.extract.staging_dir)
        raise ExtractException(500, "DATABASE_ERROR", "Failed to create job record.")


# ---------------------------------------------------------------------------

# Probe size for binary-detection heuristics.
MAX_PROBE_BYTES = 8192


async def _validate_file_content(file: UploadFile) -> None:
    """Validate that an uploaded file is a genuine text file.

    Reads only the first 8 KB, then resets the file pointer.
    Raises ExtractException on any content validation failure.
    """
    probe = await file.read(MAX_PROBE_BYTES)
    await file.seek(0)

    if not probe:
        raise ExtractException(400, "BAD_REQUEST", "File is empty.")

    try:
        decoded = probe.decode("utf-8")
    except UnicodeDecodeError:
        raise ExtractException(400, "BAD_REQUEST", "File content is not valid UTF-8 text.")

    if b"\x00" in probe:
        raise ExtractException(
            415, "BAD_REQUEST", "File contains null bytes and appears to be binary."
        )

    control_count = sum(
        1 for ch in decoded
        if unicodedata.category(ch).startswith("Cc")
        and ch not in ("\n", "\r", "\t", "\f")
    )
    if len(decoded) > 0 and (control_count / len(decoded)) > 0.05:
        raise ExtractException(
            415, "BAD_REQUEST",
            "File contains excessive control characters and appears to be binary.",
        )

    if probe[:4] == b"%PDF":
        ext = os.path.splitext(file.filename or "")[1].lower()
        raise ExtractException(
            415, "BAD_REQUEST", f"File has {ext} extension but contains PDF content."
        )


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
        "Submit a `.txt` or `.md` file for asynchronous entity extraction "
        "against a registered schema.  Returns immediately with a `job_id`.\n\n"
        "**Form parameters:**\n"
        "- `file` (required): A single `.txt` or `.md` file\n"
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
    _check_job_admission()
    filename, source_type = _validate_and_resolve_file(file)
    await _validate_file_content(file)
    _resolve_schema(schema_id)

    job_id = str(uuid.uuid4())
    _stage_and_persist(job_id, file, schema_id, filename, source_type, job_name)

    background_tasks.add_task(_process_extract_job, job_id)
    logger.info(f"Accepted extraction job {job_id} (schema={schema_id}, file={filename!r})")
    return JobCreatedResponse(job_id=job_id)


async def _process_extract_job(job_id: str) -> None:
    """
    Background worker stub.

    A full worker (tokenization, vLLM call, schema validation) will be added
    in a follow-up iteration.  For now it acquires the job_limiter slot so
    semaphore accounting is correct and immediately releases it.
    """
    from extract.state import job_limiter  # deferred to avoid circular import
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
        raise ExtractException(
            400, "INVALID_PARAMETER",
            f"Invalid status value. Must be one of: {', '.join(sorted(_VALID_STATUSES))}",
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
            submitted_at=fmt_dt(row.submitted_at) or "",
            completed_at=fmt_dt(row.completed_at) or "",
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
        raise ExtractException(404, "RESOURCE_NOT_FOUND", f"Job {job_id!r} not found.")

    return JobDetailResponse(
        job_id=row.job_id,
        job_name=row.job_name,
        schema_id=row.schema_id,
        status=row.status,
        document=DocumentInfo(
            name=row.document_name,
            source_type=row.source_type,
        ),
        metadata=row.job_metadata,
        submitted_at=fmt_dt(row.submitted_at) or "",
        completed_at=fmt_dt(row.completed_at) or "",
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
        raise ExtractException(404, "RESOURCE_NOT_FOUND", f"Job {job_id!r} not found.")

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
        raise ExtractException(
            500, "INTERNAL_SERVER_ERROR", "Result file not found for completed job."
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
        "Returns **409 Conflict** if the job is `accepted` or `in_progress`."
    ),
    tags=["jobs"],
)
async def delete_extract_job(job_id: str) -> Response:
    row = db_repo.get_job_by_id(job_id)
    if row is None:
        raise ExtractException(404, "RESOURCE_NOT_FOUND", f"Job {job_id!r} not found.")

    if row.status not in ("completed", "failed"):
        raise ExtractException(
            409, "RESOURCE_LOCKED",
            f"Cannot delete active job {job_id!r}. Current status: {row.status}.",
        )

    delete_job_files(job_id)

    success = db_repo.delete_job(job_id)
    if not success:
        raise ExtractException(
            500, "INTERNAL_SERVER_ERROR", "Failed to delete job from database."
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
        raise ExtractException(400, "CONFIRMATION_REQUIRED", "Bulk delete requires ?confirm=true.")

    if db_repo.has_active_jobs():
        raise ExtractException(
            409, "RESOURCE_LOCKED",
            "Cannot bulk-delete: one or more active jobs exist. "
            "Wait for them to complete or cancel them individually.",
        )

    delete_all_job_files()

    success = db_repo.delete_all_jobs()
    if not success:
        raise ExtractException(
            500, "INTERNAL_SERVER_ERROR", "Failed to delete jobs from database."
        )

    logger.info("Bulk deleted all extraction jobs")
    return Response(status_code=204)
