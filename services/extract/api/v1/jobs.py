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
from typing import List, Optional

from fastapi import APIRouter, File, Form, Query, Request, UploadFile
from fastapi.responses import JSONResponse, Response
from common.error_utils import http_error_responses
from common.misc_utils import cleanup_staging_directory, get_llm_endpoint, get_logger

from extract.db.manager import db_repo
from extract.models import (
    BatchDocumentItem,
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
    read_doc_result_file,
    stage_multiple_files,
    stage_uploaded_file,
    validate_file_extension,
)
from extract.utils.schema import (
    _tokenize,
    check_extraction_budget,
    compute_reserved_output,
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
async def extract_sync(request: Request, body: ExtractionRequest) -> JSONResponse:
    """Synchronous entity extraction — blocking call with schema-validated JSON output."""
    t_start = time.monotonic()

    # ------------------------------------------------------------------
    # 0. Request-body size guard — before any parsing or tokenisation
    # ------------------------------------------------------------------
    await check_request_body_size(request)

    # ------------------------------------------------------------------
    # 1. Basic field validation
    # ------------------------------------------------------------------
    if not body.text.strip():
        raise ExtractException(400, "INVALID_REQUEST", "text field is empty")
    schema_row = _resolve_schema(body.schema_id)

    # ------------------------------------------------------------------
    # 2. Semaphore check (non-blocking — reject immediately if saturated)
    # ------------------------------------------------------------------
    if concurrency_limiter.locked():
        raise ExtractException(
            429, "RATE_LIMIT_EXCEEDED",
            "Server is at maximum vLLM concurrency. Please retry later.",
        )

    llm_model_dict = get_llm_endpoint()
    llm_endpoint: str = llm_model_dict.get("llm_endpoint", "")
    llm_model: str = llm_model_dict.get("llm_model", "")
    max_model_len: int = llm_model_dict.get('max_model_len', "")

    # ------------------------------------------------------------------
    # 3–8. Core extraction
    #       One semaphore slot held across BOTH the initial call and the
    #       validation retry so a second attempt cannot be starved.
    # ------------------------------------------------------------------


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
    #       check_extraction_budget raises ExtractException.
    try:
        reserved_output = check_extraction_budget(
            input_tokens=input_tokens,
            schema_tokens=schema_row.schema_tokens,
            examples_tokens=schema_row.examples_tokens,
            custom_prompt_tokens=schema_row.custom_prompt_tokens,
            max_model_len=max_model_len,
        )
    except ExtractException as ext_exc:
        raise ext_exc
    except Exception as e:
        logger.error(e)
        raise ExtractException(500,
            "INTERNAL_SERVER_ERROR",
            "Something went wrong. Please try again later."
        )

    # ── 5. Prompt assembly ────────────────────────────────────────────
    few_shot_block = render_few_shot_block(schema_row.examples)
    messages = build_messages(
        normalized_schema=schema_row.json_schema,
        few_shot_block=few_shot_block,
        input_text=body.text,
        custom_prompt=schema_row.custom_prompt,
    )


    async with concurrency_limiter:
        # ── 6. First vLLM call ────────────────────────────────────────────
        vllm_resp = await call_vllm_safe(
            messages, reserved_output, schema_row.json_schema, llm_endpoint, llm_model
        )

        choices = vllm_resp.get("choices", [])
        if not choices:
            raise ExtractException(500, "LLM_ERROR", "vLLM returned an empty choices list.")

        choice = choices[0]
        finish_reason: str = choice.get("finish_reason", "")

        # ── 7. Output-budget exceeded — retry once with 1.5× output_token_factor ─
        if finish_reason == "length":
            boosted_reserved_output = compute_reserved_output(
                schema_row.schema_tokens,
                output_token_factor=1.5 * settings.extract.output_token_factor,
            )
            logger.warning(
                "finish_reason=length on first call; retrying with boosted "
                "reserved_output=%d (was %d)",
                boosted_reserved_output,
                reserved_output,
            )
            vllm_resp = await call_vllm_safe(
                messages, boosted_reserved_output, schema_row.json_schema, llm_endpoint, llm_model
            )
            choices = vllm_resp.get("choices", [])
            if not choices:
                raise ExtractException(500, "LLM_ERROR", "vLLM returned an empty choices list.")
            choice = choices[0]
            finish_reason = choice.get("finish_reason", "")
            if finish_reason == "length":
                raise ExtractException(
                    413, "OUTPUT_BUDGET_EXCEEDED",
                    "The model output was truncated because it reached the reserved "
                    "output token limit.",
                    details={
                        "reserved_output_tokens": boosted_reserved_output,
                        "finish_reason": "length",
                    },
                )
            reserved_output = boosted_reserved_output

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
    """Raise 429 if the concurrency slot is exhausted."""
    from extract import state
    if state.extract_limiter.locked():
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
        raise ExtractException(
            404,"SCHEMA_NOT_FOUND",
              f"No schema with id {schema_id!r}.")
    return row



# ---------------------------------------------------------------------------
# Probe size for binary-detection heuristics. 8 KB is large enough to catch
# null bytes, invalid UTF-8, or control-character runs in virtually any
# misnamed binary file, while aligning with the OS page size and Python's
# default IO buffer. The full UTF-8 decode in the worker catches anything
# deeper; this probe just moves obvious rejections to submission time.
# Probe size for binary-detection heuristics.
MAX_PROBE_BYTES = 8192


async def _validate_file_content(file: UploadFile) -> None:
    """Validate that an uploaded file is a genuine text file.

    Reads only the first 8 KB, then resets the file pointer.
    Raises ExtractException on any content validation failure.
    """
    probe = await file.read(MAX_PROBE_BYTES)
    await file.seek(0)

    if not probe or not probe.strip():
        raise ExtractException(400, "BAD_REQUEST", "File is empty.")

    try:
        decoded = probe.decode("utf-8")
    except UnicodeDecodeError:
        raise ExtractException(400, "BAD_REQUEST", "File content is not valid UTF-8 text.")
    # Gate 2: no null bytes
    if b"\x00" in probe:
        raise ExtractException(
            415, "BAD_REQUEST", "File contains null bytes and appears to be binary."
        )
    # Gate 3: low control character ratio
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
    # Gate 4: reject text files that are actually PDFs
    if probe[:4] == b"%PDF":
        ext = os.path.splitext(file.filename or "")[1].lower()
        raise ExtractException(
            415, "BAD_REQUEST", f"File has {ext} extension but contains PDF content."
        )


# ---------------------------------------------------------------------------
# POST /v1/extract/jobs — Submit an async extraction job (single or batch)
# ---------------------------------------------------------------------------

@router.post(
    "/jobs",
    status_code=202,
    response_model=JobCreatedResponse,
    responses={
        202: {"description": "Job accepted"},
        400: http_error_responses[400],
        404: http_error_responses[404],
        413: http_error_responses[413],
        415: http_error_responses[415],
        429: http_error_responses[429],
        500: http_error_responses[500],
    },
    summary="Create async extraction job",
    description=(
        "Submit one or more `.txt` or `.md` files for asynchronous entity extraction "
        "against a registered schema.  Returns immediately with a `job_id`.\n\n"
        "**Form parameters:**\n"
        "- `files` (required): One or more `.txt` or `.md` files (no duplicates)\n"
        "- `schema_id` (required): ID of a registered extraction schema\n"
        "- `job_name` (optional): Human-readable label for the job\n"
    ),
    tags=["jobs"],
)
async def create_extract_job(
    files: List[UploadFile] = File(...),
    schema_id: str = Form(...),
    job_name: Optional[str] = Form(None),
) -> JobCreatedResponse:
    """Validate, stage, record, and enqueue an async extraction job (single or batch)."""
    _check_job_admission()

    # ------------------------------------------------------------------
    # 1. File count validation
    # ------------------------------------------------------------------
    if not files:
        raise ExtractException(400, "INVALID_REQUEST", "At least one file is required.")

    if len(files) > settings.extract.max_files_per_job:
        raise ExtractException(
            413, "TOO_MANY_FILES",
            f"Too many files: {len(files)} submitted, maximum is "
            f"{settings.extract.max_files_per_job}.",
            details={"submitted": len(files), "limit": settings.extract.max_files_per_job},
        )

    # ------------------------------------------------------------------
    # 2. Per-file extension + content validation (all-or-nothing)
    # ------------------------------------------------------------------
    validated: list[tuple[str, str]] = []  # (filename, source_type)
    content_errors: list[dict] = []

    for idx, file in enumerate(files):
        filename = (file.filename or "").lower()
        is_valid, ext = validate_file_extension(filename)
        if not is_valid:
            raw_ext = os.path.splitext(filename)[1] or "unknown"
            raise ExtractException(
                415, "UNSUPPORTED_FILE_TYPE",
                f"Only .txt and .md files are accepted. "
                f"File at index {idx} ({filename!r}) has extension: {raw_ext}",
            )
        source_type = (ext or "").lstrip(".")
        validated.append((filename, source_type))

    # Duplicate filename check
    filenames_seen: set[str] = set()
    for idx, (filename, _) in enumerate(validated):
        if filename in filenames_seen:
            raise ExtractException(
                400, "DUPLICATE_FILE",
                f"Duplicate filename detected at index {idx}: {filename!r}. "
                "All file names must be unique within a batch.",
            )
        filenames_seen.add(filename)

    # Content validation — collect all failures before rejecting
    for idx, file in enumerate(files):
        try:
            await _validate_file_content(file)
        except ExtractException as exc:
            content_errors.append({
                "index": idx,
                "filename": validated[idx][0],
                "reason": exc.message,
            })

    if content_errors:
        raise ExtractException(
            415, "INVALID_FILE_CONTENT",
            "One or more files failed content validation.",
            details=content_errors,
        )

    # ------------------------------------------------------------------
    # 3. Schema lookup
    # ------------------------------------------------------------------
    _resolve_schema(schema_id)

    # ------------------------------------------------------------------
    # 4. Stage all files, create job + document rows
    # ------------------------------------------------------------------
    job_id = str(uuid.uuid4())
    try:
        stage_multiple_files(job_id, files)
    except IOError as exc:
        logger.error(f"Failed to stage files for job {job_id}: {exc}")
        raise ExtractException(500, "FILE_STAGING_ERROR", "Failed to save uploaded files.")

    _success = False
    try:
        try:
            row = db_repo.create_job(
                job_id=job_id,
                schema_id=schema_id,
                job_name=job_name,
                submitted_at=datetime.now(timezone.utc),
                file_count=len(files),
            )
        except Exception as exc:
            logger.error(f"Unexpected DB error creating job {job_id}: {exc}")
            raise ExtractException(500, "DATABASE_ERROR", "Failed to create job record.")

        if row is None:
            raise ExtractException(500, "DATABASE_ERROR", "Failed to create job record.")

        # Insert one document row per file
        doc_entries = [
            {
                "doc_id": str(uuid.uuid4()),
                "filename": filename,
                "source_type": source_type,
            }
            for filename, source_type in validated
        ]
        ok = db_repo.create_documents(job_id, doc_entries)
        if not ok:
            db_repo.delete_job(job_id)
            raise ExtractException(500, "DATABASE_ERROR", "Failed to create document records.")

        _success = True
        asyncio.create_task(_process_batch_job(job_id))
        logger.info(
            f"Accepted extraction job {job_id} (schema={schema_id}, "
            f"files={len(files)}, job_name={job_name!r})"
        )
        return JobCreatedResponse(job_id=job_id, file_count=len(files))
    finally:
        if not _success:
            cleanup_staging_directory(job_id, settings.extract.staging_dir)


# ---------------------------------------------------------------------------
# _process_file — per-file extraction pipeline (called from batch worker)
# ---------------------------------------------------------------------------

async def _process_file(
    job_id: str,
    doc_id: str,
    staged_path,
    schema_row,
    llm_endpoint: str,
    llm_model: str,
    max_model_len: int,
) -> None:
    """Run the full extraction pipeline for one file.

    Updates the document row in the DB at each stage transition.  Never
    raises — failures are recorded on the document row so the batch can
    continue with subsequent files.
    """
    from extract import state

    t_doc_start = time.monotonic()

    async with state.parallel_file_limiter:
        db_repo.update_document(
            doc_id=doc_id,
            status="in_progress",
            started_at=datetime.now(timezone.utc),
        )

        try:
            # ── reading ───────────────────────────────────────────────
            try:
                text = staged_path.read_bytes().decode("utf-8")
            except UnicodeDecodeError as exc:
                logger.error(f"UTF-8 decode failed for doc {doc_id}: {exc}")
                db_repo.update_document(
                    doc_id=doc_id,
                    status="failed",
                    error="File could not be decoded as UTF-8.",
                    completed_at=datetime.now(timezone.utc),
                )
                return
            except Exception as exc:
                logger.error(f"Read failed for doc {doc_id}: {exc}")
                db_repo.update_document(
                    doc_id=doc_id,
                    status="failed",
                    error=f"Failed to read staged file: {exc}",
                    completed_at=datetime.now(timezone.utc),
                )
                return

            input_word_count = len(text.split())

            # ── tokenizing ────────────────────────────────────────────
            try:
                input_tokens: int = await asyncio.to_thread(
                    _tokenize, text, llm_endpoint
                )
            except Exception as exc:
                logger.error(f"Tokenization failed for doc {doc_id}: {exc}", exc_info=True)
                db_repo.update_document(
                    doc_id=doc_id,
                    status="failed",
                    error="Failed to tokenise the input text.",
                    completed_at=datetime.now(timezone.utc),
                )
                return

            db_repo.update_document(doc_id=doc_id, input_tokens=input_tokens, word_count=input_word_count)

            try:
                reserved_output = check_extraction_budget(
                    input_tokens=input_tokens,
                    schema_tokens=schema_row.schema_tokens,
                    examples_tokens=schema_row.examples_tokens,
                    custom_prompt_tokens=schema_row.custom_prompt_tokens,
                    max_model_len=max_model_len,
                )
            except ExtractException as exc:
                db_repo.update_document(
                    doc_id=doc_id,
                    status="failed",
                    error=exc.message,
                    completed_at=datetime.now(timezone.utc),
                    metadata={"token_diagnostics": exc.details} if exc.details else None,
                )
                return

            # ── extracting ────────────────────────────────────────────
            few_shot_block = render_few_shot_block(schema_row.examples)
            messages = build_messages(
                normalized_schema=schema_row.json_schema,
                few_shot_block=few_shot_block,
                input_text=text,
                custom_prompt=schema_row.custom_prompt,
            )

            t_extract_start = time.monotonic()
            try:
                async with concurrency_limiter:
                    vllm_resp = await call_vllm_safe(
                        messages, reserved_output, schema_row.json_schema,
                        llm_endpoint, llm_model,
                    )
            except ExtractException as exc:
                db_repo.update_document(
                    doc_id=doc_id,
                    status="failed",
                    error=exc.message,
                    completed_at=datetime.now(timezone.utc),
                )
                return

            choices = vllm_resp.get("choices", [])
            if not choices:
                db_repo.update_document(
                    doc_id=doc_id,
                    status="failed",
                    error="vLLM returned an empty choices list.",
                    completed_at=datetime.now(timezone.utc),
                )
                return

            choice = choices[0]
            finish_reason: str = choice.get("finish_reason", "")
            max_tokens_adjusted = False

            # finish_reason=length → retry once with boosted budget
            if finish_reason == "length":
                boosted_reserved_output = compute_reserved_output(
                    schema_row.schema_tokens,
                    output_token_factor=1.5 * settings.extract.output_token_factor,
                )
                logger.warning(
                    "finish_reason=length for doc %s; retrying with boosted "
                    "reserved_output=%d (was %d)",
                    doc_id, boosted_reserved_output, reserved_output,
                )
                try:
                    async with concurrency_limiter:
                        vllm_resp = await call_vllm_safe(
                            messages, boosted_reserved_output, schema_row.json_schema,
                            llm_endpoint, llm_model,
                        )
                except ExtractException as exc:
                    db_repo.update_document(
                        doc_id=doc_id,
                        status="failed",
                        error=exc.message,
                        completed_at=datetime.now(timezone.utc),
                    )
                    return

                choices = vllm_resp.get("choices", [])
                if not choices:
                    db_repo.update_document(
                        doc_id=doc_id,
                        status="failed",
                        error="vLLM returned an empty choices list.",
                        completed_at=datetime.now(timezone.utc),
                    )
                    return

                choice = choices[0]
                finish_reason = choice.get("finish_reason", "")
                if finish_reason == "length":
                    db_repo.update_document(
                        doc_id=doc_id,
                        status="failed",
                        error="The model output was truncated even after retrying with increased budget.",
                        completed_at=datetime.now(timezone.utc),
                        metadata={
                            "token_diagnostics": {
                                "reserved_output_tokens": boosted_reserved_output,
                                "max_tokens_adjusted": True,
                                "finish_reason": "length",
                            }
                        },
                    )
                    return

                reserved_output = boosted_reserved_output
                max_tokens_adjusted = True

            t_extract_secs = time.monotonic() - t_extract_start

            raw_output: str = choice.get("message", {}).get("content", "") or ""
            usage = vllm_resp.get("usage", {})
            total_prompt_tokens: int = usage.get("prompt_tokens", 0)
            total_completion_tokens: int = usage.get("completion_tokens", 0)

            # ── validating ────────────────────────────────────────────
            t_validate_start = time.monotonic()
            try:
                parsed_output, validation_attempts, extra_pt, extra_ct = (
                    await validate_with_retry(
                        raw_output, messages, reserved_output,
                        schema_row.json_schema, llm_endpoint, llm_model,
                    )
                )
            except ExtractException as exc:
                db_repo.update_document(
                    doc_id=doc_id,
                    status="failed",
                    error=exc.message,
                    completed_at=datetime.now(timezone.utc),
                    metadata={"validation": {"last_errors": exc.details}} if exc.details else None,
                )
                return

            t_validate_secs = time.monotonic() - t_validate_start
            total_prompt_tokens += extra_pt
            total_completion_tokens += extra_ct

            # ── writing ───────────────────────────────────────────────
            processing_time_ms = int((time.monotonic() - t_doc_start) * 1000)
            result_payload = {
                "data": {
                    "extraction": parsed_output,
                    "schema_id": schema_row.schema_id,
                    "source": {
                        "input_type": "file",
                        "document_name": staged_path.name,
                        "input_words": input_word_count,
                        "input_tokens": input_tokens,
                    },
                },
                "status": "completed",
                "meta": {
                    "model": llm_model,
                    "processing_time_ms": processing_time_ms,
                    "validation_attempts": validation_attempts,
                    "timing_in_secs": {
                        "extracting": round(t_extract_secs, 3),
                        "validating": round(t_validate_secs, 3),
                    },
                },
                "usage": {
                    "input_tokens": total_prompt_tokens,
                    "output_tokens": total_completion_tokens,
                    "total_tokens": total_prompt_tokens + total_completion_tokens,
                },
            }

            result_dir = settings.extract.results_dir / job_id
            result_path = result_dir / f"{doc_id}_result.json"
            try:
                result_dir.mkdir(parents=True, exist_ok=True)
                result_path.write_text(json.dumps(result_payload), encoding="utf-8")
            except Exception as exc:
                logger.error(
                    f"Failed to write result for doc {doc_id} in job {job_id}: {exc}",
                    exc_info=True,
                )
                db_repo.update_document(
                    doc_id=doc_id,
                    status="failed",
                    error="Failed to write result file to disk.",
                    completed_at=datetime.now(timezone.utc),
                )
                return

            db_repo.update_document(
                doc_id=doc_id,
                status="completed",
                completed_at=datetime.now(timezone.utc),
                metadata={
                    "token_diagnostics": {
                        "input_tokens": input_tokens,
                        "schema_tokens": schema_row.schema_tokens,
                        "reserved_output_tokens": reserved_output,
                        "max_tokens_adjusted": max_tokens_adjusted,
                    },
                    "timing_in_secs": {
                        "extracting": round(t_extract_secs, 3),
                        "validating": round(t_validate_secs, 3),
                    },
                    "validation": {"attempts": validation_attempts},
                },
            )
            logger.info(f"Doc {doc_id} completed in {processing_time_ms} ms")

        except Exception as exc:
            logger.error(
                f"Unexpected error processing doc {doc_id} in job {job_id}: {exc}",
                exc_info=True,
            )
            db_repo.update_document(
                doc_id=doc_id,
                status="failed",
                error=f"Unexpected error: {exc}",
                completed_at=datetime.now(timezone.utc),
            )


# ---------------------------------------------------------------------------
# _process_batch_job — batch background worker
# ---------------------------------------------------------------------------

async def _process_batch_job(job_id: str) -> None:
    """Background worker: process each document in the batch sequentially."""
    from extract import state

    async with state.extract_limiter:
        t_start = time.monotonic()
        logger.info(f"Batch worker started for job {job_id}")

        db_repo.update_job(job_id=job_id, status="in_progress")

        llm_model_dict = get_llm_endpoint()
        llm_endpoint: str = llm_model_dict.get("llm_endpoint", "")
        llm_model: str = llm_model_dict.get("llm_model", "")
        max_model_len: int = llm_model_dict.get("max_model_len", "")

        job_row = db_repo.get_job_by_id(job_id)
        if job_row is None:
            logger.error(f"Job {job_id} not found in DB at worker start; aborting.")
            return

        try:
            try:
                schema_row = _resolve_schema(job_row.schema_id)
            except ExtractException as exc:
                db_repo.update_job(
                    job_id=job_id,
                    status="failed",
                    error=exc.code,
                    completed_at=datetime.now(timezone.utc),
                )
                return

            doc_rows = db_repo.get_documents_by_job(job_id)
            if not doc_rows:
                logger.error(f"No document rows found for job {job_id}")
                db_repo.update_job(
                    job_id=job_id,
                    status="failed",
                    error="NO_DOCUMENTS",
                    completed_at=datetime.now(timezone.utc),
                )
                return

            job_dir = settings.extract.staging_dir / job_id

            # Process each document sequentially (files are short-lived tasks, the
            # parallel_file_limiter inside _process_file limits true concurrency).
            tasks = []
            for doc_row in doc_rows:
                staged_path = job_dir / doc_row.filename
                tasks.append(
                    _process_file(
                        job_id=job_id,
                        doc_id=doc_row.doc_id,
                        staged_path=staged_path,
                        schema_row=schema_row,
                        llm_endpoint=llm_endpoint,
                        llm_model=llm_model,
                        max_model_len=max_model_len,
                    )
                )

            # Run tasks with limited parallelism: gather in chunks of parallel_files_per_job
            chunk_size = settings.extract.parallel_files_per_job
            for i in range(0, len(tasks), chunk_size):
                await asyncio.gather(*tasks[i:i + chunk_size])

            # Determine final job status from document outcomes
            final_docs = db_repo.get_documents_by_job(job_id)
            n_completed = sum(1 for d in final_docs if d.status == "completed")
            n_failed = sum(1 for d in final_docs if d.status == "failed")
            total = len(final_docs)

            if n_failed == 0:
                final_status = "completed"
                job_error = None
            elif n_completed == 0:
                final_status = "failed"
                job_error = f"All {total} file(s) failed extraction."
            else:
                final_status = "completed_with_errors"
                job_error = f"{n_failed} of {total} files failed extraction."

            processing_time_ms = int((time.monotonic() - t_start) * 1000)
            db_repo.update_job(
                job_id=job_id,
                status=final_status,
                error=job_error,
                completed_at=datetime.now(timezone.utc),
            )
            logger.info(
                f"Batch job {job_id} {final_status} in {processing_time_ms} ms "
                f"({n_completed}/{total} completed)"
            )

        finally:
            cleanup_staging_directory(job_id, settings.extract.staging_dir)



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
        "- `status` (string): Filter by `accepted`, `in_progress`, `completed`, "
        "`completed_with_errors`, or `failed`\n"
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
    """Retrieve a list of extraction jobs with pagination and optional status/schema filtering."""
    _VALID_STATUSES = {"accepted", "in_progress", "completed", "completed_with_errors", "failed"}
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
            file_count=row.file_count,
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
        "Retrieve the full status of a specific extraction job.\n\n"
    ),
    tags=["jobs"],
)
async def get_extract_job(job_id: str) -> JobDetailResponse:
    """Retrieve the full status and detail metadata of a specific extraction job."""
    row = db_repo.get_job_by_id(job_id)
    if row is None:
        raise ExtractException(404, "RESOURCE_NOT_FOUND", f"Job {job_id!r} not found.")

    doc_rows = db_repo.get_documents_by_job(job_id)

    if doc_rows:
        # Batch job — include per-document summary and progress counters
        documents = [
            BatchDocumentItem(
                doc_id=d.doc_id,
                filename=d.filename,
                status=d.status,
                error=d.error or "",
            )
            for d in doc_rows
        ]
        n_completed = sum(1 for d in doc_rows if d.status == "completed")
        n_failed = sum(1 for d in doc_rows if d.status == "failed")
        n_pending = sum(1 for d in doc_rows if d.status in ("pending", "in_progress"))

        return JobDetailResponse(
            job_id=row.job_id,
            job_name=row.job_name,
            schema_id=row.schema_id,
            status=row.status,
            documents=documents,
            file_count=row.file_count,
            files_completed=n_completed,
            files_failed=n_failed,
            files_pending=n_pending,
            metadata=row.job_metadata,
            submitted_at=fmt_dt(row.submitted_at) or "",
            completed_at=fmt_dt(row.completed_at) or "",
            error=row.error,
        )
    else:
        raise ExtractException(404, "RESOURCE_NOT_FOUND", f"No documents found for job {job_id!r}")


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id}/results/{doc_id} — Per-document result
# ---------------------------------------------------------------------------

@router.get(
    "/jobs/{job_id}/results/{doc_id}",
    response_model=JobResultResponse,
    responses={
        200: {"description": "Extraction result for this document"},
        202: {"description": "Document still processing"},
        404: http_error_responses[404],
        410: {"description": "Document failed extraction"},
        500: http_error_responses[500],
    },
    summary="Get per-document extraction result",
    description=(
        "Retrieve the extraction result for a single document in a batch job.\n\n"
        "- **202** while the document is `pending` or `in_progress`.\n"
        "- **410** if the document failed — inspect the job resource for error details.\n"
        "- **404** if the job or document does not exist.\n"
        "- **200** with the result payload once the document is `completed`."
    ),
    tags=["jobs"],
)
async def get_document_result(job_id: str, doc_id: str):
    """Retrieve the extraction result for one document in a batch job."""
    # Verify parent job exists
    job_row = db_repo.get_job_by_id(job_id)
    if job_row is None:
        raise ExtractException(404, "RESOURCE_NOT_FOUND", f"Job {job_id!r} not found.")

    doc_row = db_repo.get_document_by_id(doc_id)
    if doc_row is None or doc_row.job_id != job_id:
        raise ExtractException(
            404, "RESOURCE_NOT_FOUND",
            f"Document {doc_id!r} not found in job {job_id!r}.",
        )

    if doc_row.status in ("pending", "in_progress"):
        return JSONResponse(
            status_code=202,
            content={
                "message": "Document is still processing.",
                "job_id": job_id,
                "doc_id": doc_id,
                "status": doc_row.status,
            },
        )

    if doc_row.status == "failed":
        return JSONResponse(
            status_code=410,
            content={
                "error": {
                    "code": "DOCUMENT_FAILED",
                    "message": (
                        f"Document {doc_id!r} failed extraction. "
                        f"Inspect GET /v1/extract/jobs/{job_id} for details."
                    ),
                    "status": 410,
                    "job_id": job_id,
                    "doc_id": doc_id,
                }
            },
        )

    # status == "completed"
    result_data = read_doc_result_file(job_id, doc_id)
    if result_data is None:
        logger.error(f"Result file missing for completed doc {doc_id} in job {job_id}")
        raise ExtractException(
            500, "INTERNAL_SERVER_ERROR",
            "Result file not found for completed document.",
        )

    return JobResultResponse(
        data=result_data.get("data", {}),
        status=result_data.get("status", "completed"),
        meta=result_data.get("meta", {}),
        usage=result_data.get("usage", {}),
    )


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id}/results/{doc_id}/download — Download result
# ---------------------------------------------------------------------------

@router.get(
    "/jobs/{job_id}/results/{doc_id}/download",
    responses={
        200: {"description": "Result JSON file download"},
        404: http_error_responses[404],
        410: {"description": "Document failed extraction"},
        500: http_error_responses[500],
    },
    summary="Download per-document extraction result",
    description=(
        "Download the extraction result for a single document as a `.json` file.\n\n"
        "- **410** if the document failed extraction.\n"
        "- **404** if the job, document, or result file does not exist."
    ),
    tags=["jobs"],
)
async def download_document_result(job_id: str, doc_id: str):
    """Download the extraction result JSON for one document in a batch job."""
    job_row = db_repo.get_job_by_id(job_id)
    if job_row is None:
        raise ExtractException(404, "RESOURCE_NOT_FOUND", f"Job {job_id!r} not found.")

    doc_row = db_repo.get_document_by_id(doc_id)
    if doc_row is None or doc_row.job_id != job_id:
        raise ExtractException(
            404, "RESOURCE_NOT_FOUND",
            f"Document {doc_id!r} not found in job {job_id!r}.",
        )

    if doc_row.status == "failed":
        return JSONResponse(
            status_code=410,
            content={
                "error": {
                    "code": "DOCUMENT_FAILED",
                    "message": (
                        f"Document {doc_id!r} failed extraction. "
                        f"Inspect GET /v1/extract/jobs/{job_id} for details."
                    ),
                    "status": 410,
                }
            },
        )

    if doc_row.status != "completed":
        raise ExtractException(
            404, "RESOURCE_NOT_FOUND",
            f"No result available for document {doc_id!r} (status={doc_row.status!r}).",
        )

    result_data = read_doc_result_file(job_id, doc_id)
    if result_data is None:
        raise ExtractException(
            404, "RESOURCE_NOT_FOUND",
            f"Result file not found for document {doc_id!r}.",
        )

    filename_stem = os.path.splitext(doc_row.filename)[0]
    download_filename = f"{filename_stem}_result.json"

    return Response(
        content=json.dumps(result_data, indent=2),
        media_type="application/json",
        headers={
            "Content-Disposition": f'attachment; filename="{download_filename}"',
        },
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
        "Delete a job record and its result file(s).  "
        "Returns **409 Conflict** if the job is `accepted` or `in_progress`."
    ),
    tags=["jobs"],
)
async def delete_extract_job(job_id: str) -> Response:
    """Delete a specific completed or failed extraction job record and its associated result files."""
    row = db_repo.get_job_by_id(job_id)
    if row is None:
        raise ExtractException(404, "RESOURCE_NOT_FOUND", f"Job {job_id!r} not found.")

    if row.status not in ("completed", "completed_with_errors", "failed"):
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
    """Delete all extraction jobs and their result files after receiving explicit confirmation."""
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
