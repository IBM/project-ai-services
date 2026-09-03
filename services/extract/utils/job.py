"""
Utility functions for async extract-job management.

Includes directory setup, file staging, result file I/O, job admission
checks, per-file extraction pipeline, batch orchestration, and the
zombie-job recovery scan run at service startup.
"""

import asyncio
import json
import os
import shutil
import time
import unicodedata
from datetime import datetime, timezone
from enum import Enum
from pathlib import Path
from typing import Any, Dict, List, Optional

from fastapi import UploadFile

from common.misc_utils import cleanup_staging_directory, get_llm_endpoint, get_logger
from extract.db.manager import db_repo
from extract.settings import settings
from extract.state import concurrency_limiter
from extract.utils.exceptions import ExtractException
from extract.utils.schema import (
    _tokenize,
    check_extraction_budget,
    compute_reserved_output,
)
from extract.utils.vllm import (
    build_messages,
    call_vllm_safe,
    render_few_shot_block,
    validate_with_retry,
)

logger = get_logger("job_utils")

ALLOWED_EXTENSIONS = {".txt", ".md"}


class JobStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    COMPLETED_WITH_ERRORS = "completed_with_errors"
    FAILED = "failed"


class DocumentStatus(str, Enum):
    PENDING = "pending"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"


# ---------------------------------------------------------------------------
# Directory management
# ---------------------------------------------------------------------------

def ensure_directories() -> None:
    """Create staging and results directories if they do not exist."""
    for d in [settings.extract.staging_dir, settings.extract.results_dir]:
        d.mkdir(parents=True, exist_ok=True)


# ---------------------------------------------------------------------------
# File helpers
# ---------------------------------------------------------------------------

def validate_file_extension(filename: str) -> tuple[bool, Optional[str]]:
    """Return (is_valid, extension_or_None)."""
    if not filename:
        return False, None
    import os
    ext = os.path.splitext(filename)[1].lower()
    return (ext in ALLOWED_EXTENSIONS), (ext if ext in ALLOWED_EXTENSIONS else None)


def stage_uploaded_file(job_id: str, file: UploadFile) -> Path:
    """
    Write the uploaded file to the staging directory for *job_id*.

    Returns the path to the staged file.
    Raises IOError on failure (caller should clean up and return 500).
    """
    job_dir = settings.extract.staging_dir / job_id
    job_dir.mkdir(parents=True, exist_ok=True)

    filename = file.filename or "uploaded_file"
    staged_path = job_dir / filename

    try:
        with open(staged_path, "wb") as fh:
            shutil.copyfileobj(file.file, fh)
        logger.info(f"Staged file for job {job_id}: {staged_path}")
        return staged_path
    except Exception as exc:
        shutil.rmtree(job_dir, ignore_errors=True)
        raise IOError(f"Failed to stage file for job {job_id}: {exc}") from exc


def stage_multiple_files(job_id: str, files: List[UploadFile]) -> List[Path]:
    """
    Write all uploaded files to the staging directory for *job_id*.

    Returns the list of staged paths in the same order as *files*.
    Raises IOError on any failure; staged files already written are removed.
    """
    job_dir = settings.extract.staging_dir / job_id
    job_dir.mkdir(parents=True, exist_ok=True)

    staged: List[Path] = []
    try:
        for file in files:
            filename = file.filename or "uploaded_file"
            staged_path = job_dir / filename
            with open(staged_path, "wb") as fh:
                shutil.copyfileobj(file.file, fh)
            staged.append(staged_path)
            logger.info(f"Staged file for job {job_id}: {staged_path}")
        return staged
    except Exception as exc:
        shutil.rmtree(job_dir, ignore_errors=True)
        raise IOError(f"Failed to stage files for job {job_id}: {exc}") from exc


def read_doc_result_file(job_id: str, doc_id: str) -> Optional[Dict[str, Any]]:
    """Read and parse the per-document result JSON for a batch job.

    Stored at ``{results_dir}/{job_id}/{doc_id}_result.json``.
    Returns None if absent.
    """
    path = settings.extract.results_dir / job_id / f"{doc_id}_result.json"
    if not path.exists():
        return None
    try:
        with open(path, "r", encoding="utf-8") as fh:
            return json.load(fh)
    except Exception as exc:
        logger.error(f"Failed to read doc result file for job {job_id} doc {doc_id}: {exc}")
        return None


def delete_job_files(job_id: str) -> None:
    """Delete result file(s) and staging directory for *job_id*.

    Handles both single-file (flat result JSON) and batch (per-job results dir) layouts.
    """

    # Batch per-job results directory
    job_results_dir = settings.extract.results_dir / job_id
    if job_results_dir.exists():
        try:
            shutil.rmtree(job_results_dir, ignore_errors=True)
        except Exception as exc:
            logger.error(f"Failed to delete results dir for job {job_id}: {exc}")

    staging_path = settings.extract.staging_dir / job_id
    if staging_path.exists():
        try:
            shutil.rmtree(staging_path, ignore_errors=True)
        except Exception as exc:
            logger.error(f"Failed to delete staging dir for job {job_id}: {exc}")


def delete_all_job_files() -> None:
    """Delete all result files and all staging directories."""
    results_dir = settings.extract.results_dir
    if results_dir.exists():
        # Per-job batch results directories
        for d in results_dir.iterdir():
            if d.is_dir():
                try:
                    shutil.rmtree(d, ignore_errors=True)
                except Exception as exc:
                    logger.error(f"Failed to delete job results dir {d.name}: {exc}")

    staging_dir = settings.extract.staging_dir
    if staging_dir.exists():
        for d in staging_dir.iterdir():
            if d.is_dir():
                try:
                    shutil.rmtree(d, ignore_errors=True)
                except Exception as exc:
                    logger.error(f"Failed to delete staging dir {d.name}: {exc}")


# ---------------------------------------------------------------------------
# Zombie-job recovery
# ---------------------------------------------------------------------------

def recover_zombie_jobs() -> int:
    """
    Mark any ``accepted`` or ``in_progress`` job as ``failed`` after a restart.

    For batch jobs, also marks any ``pending`` or ``in_progress`` documents as
    ``failed``.  Documents that already ``completed`` are preserved.

    Called once during FastAPI startup.  Returns the number of recovered jobs.
    """
    logger.info("Starting zombie job recovery scan...")
    try:
        zombies = db_repo.get_active_jobs()
        if not zombies:
            logger.info("No zombie jobs found")
            return 0

        count = 0
        for job in zombies:
            job_id = job.job_id
            logger.warning(f"Zombie job found: {job_id} (status={job.status})")

            # Mark any pending/in_progress document rows as failed.
            db_repo.fail_zombie_documents(job_id)

            db_repo.update_job(
                job_id=job_id,
                status=JobStatus.FAILED,
                error="System restarted during processing",
                completed_at=datetime.now(timezone.utc),
            )
            staging_path = settings.extract.staging_dir / job_id
            if staging_path.exists():
                shutil.rmtree(staging_path, ignore_errors=True)
            count += 1

        logger.info(f"Zombie recovery complete: {count} job(s) recovered")
        return count
    except Exception as exc:
        logger.error(f"Error during zombie recovery scan: {exc}", exc_info=True)
        return 0

# ---------------------------------------------------------------------------
# Job admission check
# ---------------------------------------------------------------------------

def check_job_admission() -> None:
    """Raise 429 if the concurrency slot is exhausted."""
    from extract import state
    if state.extract_limiter.locked():
        raise ExtractException(
            429, "RATE_LIMIT_EXCEEDED",
            "Job concurrency limit reached. Please try again later.",
        )


# ---------------------------------------------------------------------------
# File validation helpers
# ---------------------------------------------------------------------------

def validate_and_resolve_file(file: UploadFile) -> tuple[str, str]:
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


def resolve_schema(schema_id: str):
    """Return the schema row for *schema_id*.

    Raises:
        ExtractException(404) if the schema does not exist.
    """
    row = db_repo.get_schema_by_id(schema_id)
    if row is None:
        raise ExtractException(
            404, "SCHEMA_NOT_FOUND",
            f"No schema with id {schema_id!r}.")
    return row


# ---------------------------------------------------------------------------
# Probe size for binary-detection heuristics. 8 KB is large enough to catch
# null bytes, invalid UTF-8, or control-character runs in virtually any
# misnamed binary file, while aligning with the OS page size and Python's
# default IO buffer. The full UTF-8 decode in the worker catches anything
# deeper; this probe just moves obvious rejections to submission time.
# ---------------------------------------------------------------------------
MAX_PROBE_BYTES = 8192


async def validate_file_content(file: UploadFile) -> None:
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
# process_file — per-file extraction pipeline (called from batch worker)
# ---------------------------------------------------------------------------

async def process_file(
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
            status=DocumentStatus.IN_PROGRESS,
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
                    status=DocumentStatus.FAILED,
                    error="File could not be decoded as UTF-8.",
                    completed_at=datetime.now(timezone.utc),
                )
                return
            except Exception as exc:
                logger.error(f"Read failed for doc {doc_id}: {exc}")
                db_repo.update_document(
                    doc_id=doc_id,
                    status=DocumentStatus.FAILED,
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
                    status=DocumentStatus.FAILED,
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
                    status=DocumentStatus.FAILED,
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
                    status=DocumentStatus.FAILED,
                    error=exc.message,
                    completed_at=datetime.now(timezone.utc),
                )
                return

            choices = vllm_resp.get("choices", [])
            if not choices:
                db_repo.update_document(
                    doc_id=doc_id,
                    status=DocumentStatus.FAILED,
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
                        status=DocumentStatus.FAILED,
                        error=exc.message,
                        completed_at=datetime.now(timezone.utc),
                    )
                    return

                choices = vllm_resp.get("choices", [])
                if not choices:
                    db_repo.update_document(
                        doc_id=doc_id,
                        status=DocumentStatus.FAILED,
                        error="vLLM returned an empty choices list.",
                        completed_at=datetime.now(timezone.utc),
                    )
                    return

                choice = choices[0]
                finish_reason = choice.get("finish_reason", "")
                if finish_reason == "length":
                    db_repo.update_document(
                        doc_id=doc_id,
                        status=DocumentStatus.FAILED,
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
                    status=DocumentStatus.FAILED,
                    error=exc.message,
                    completed_at=datetime.now(timezone.utc),
                    metadata={"validation": {"last_errors": exc.details}} if exc.details else None,
                )
                return

            t_validate_secs = time.monotonic() - t_validate_start
            total_prompt_tokens += extra_pt
            total_completion_tokens += extra_ct

            # ── writing ───────────────────────────────────────────────
            result_dir = settings.extract.results_dir / job_id
            result_path = result_dir / f"{doc_id}_result.json"
            try:
                result_dir.mkdir(parents=True, exist_ok=True)
                result_path.write_text(json.dumps( parsed_output), encoding="utf-8")
            except Exception as exc:
                logger.error(
                    f"Failed to write result for doc {doc_id} in job {job_id}: {exc}",
                    exc_info=True,
                )
                db_repo.update_document(
                    doc_id=doc_id,
                    status=DocumentStatus.FAILED,
                    error="Failed to write result file to disk.",
                    completed_at=datetime.now(timezone.utc),
                )
                return

            db_repo.update_document(
                doc_id=doc_id,
                status=DocumentStatus.COMPLETED,
                completed_at=datetime.now(timezone.utc),
                usage_input_tokens=total_prompt_tokens,
                usage_output_tokens=total_completion_tokens,
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
            logger.info(f"Doc {doc_id} completed in {int((time.monotonic() - t_doc_start) * 1000)} ms")

        except Exception as exc:
            logger.error(
                f"Unexpected error processing doc {doc_id} in job {job_id}: {exc}",
                exc_info=True,
            )
            db_repo.update_document(
                doc_id=doc_id,
                status=DocumentStatus.FAILED,
                error=f"Unexpected error: {exc}",
                completed_at=datetime.now(timezone.utc),
            )


# ---------------------------------------------------------------------------
# process_batch_job — batch background worker
# ---------------------------------------------------------------------------

async def process_batch_job(job_id: str) -> None:
    """Background worker: process each document in the batch sequentially."""
    from extract import state

    async with state.extract_limiter:
        t_start = time.monotonic()
        logger.info(f"Batch worker started for job {job_id}")

        db_repo.update_job(job_id=job_id, status=JobStatus.IN_PROGRESS)

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
                schema_row = resolve_schema(job_row.schema_id)
            except ExtractException as exc:
                db_repo.update_job(
                    job_id=job_id,
                    status=JobStatus.FAILED,
                    error=exc.code,
                    completed_at=datetime.now(timezone.utc),
                )
                return

            doc_rows = db_repo.get_documents_by_job(job_id)
            if not doc_rows:
                logger.error(f"No document rows found for job {job_id}")
                db_repo.update_job(
                    job_id=job_id,
                    status=JobStatus.FAILED,
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
                    process_file(
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
            n_completed = sum(1 for d in final_docs if d.status == DocumentStatus.COMPLETED)
            n_failed = sum(1 for d in final_docs if d.status == DocumentStatus.FAILED)
            total = len(final_docs)

            if n_failed == 0:
                final_status = JobStatus.COMPLETED
                job_error = None
            elif n_completed == 0:
                final_status = JobStatus.FAILED
                job_error = f"All {total} file(s) failed extraction."
            else:
                final_status = JobStatus.COMPLETED_WITH_ERRORS
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
# build_result_payload — reconstruct full result payload from DB rows
# ---------------------------------------------------------------------------

def build_result_payload(job_row, doc_row, result_file_data: dict) -> dict:
    """Reconstruct the full result payload from DB rows + the stored extraction file.

    The result file now only contains ``{"extraction": <parsed_output>}``.
    All envelope fields are rebuilt from the job and document DB rows so that
    the file stays as small as possible.
    """
    meta = doc_row.doc_metadata or {}
    timing = meta.get("timing_in_secs", {})
    validation_attempts = meta.get("validation", {}).get("attempts", 1)

    usage_in = doc_row.usage_input_tokens or 0
    usage_out = doc_row.usage_output_tokens or 0

    processing_time_ms = None
    if doc_row.started_at and doc_row.completed_at:
        delta = doc_row.completed_at - doc_row.started_at
        processing_time_ms = int(delta.total_seconds() * 1000)

    llm_model = get_llm_endpoint().get("llm_model", "")

    return {
        "data": {
            "extraction": result_file_data,
            "schema_id": job_row.schema_id,
            "source": {
                "input_type": "file",
                "document_name": doc_row.filename,
                "input_words": doc_row.word_count,
                "input_tokens": doc_row.input_tokens,
            },
        },
        "status": DocumentStatus.COMPLETED,
        "meta": {
            "model": llm_model,
            "processing_time_ms": processing_time_ms,
            "validation_attempts": validation_attempts,
            "timing_in_secs": {
                "extracting": timing.get("extracting"),
                "validating": timing.get("validating"),
            },
        },
        "usage": {
            "input_tokens": usage_in,
            "output_tokens": usage_out,
            "total_tokens": usage_in + usage_out,
        },
    }


# Made with Bob
