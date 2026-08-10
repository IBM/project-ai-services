"""
Background translation worker.

``run_translation_job`` is the single entry-point launched by FastAPI's
``BackgroundTasks``.  It owns the full async document-translation pipeline:

  1  Acquire ``job_limiter``; DB → ``in_progress``, ``phase=chunking``
  2  Read staged file as UTF-8
  3  Resolve ``source_language`` via lingua detection (three-position sampling)
  4  Chunk the document via ``build_translation_chunks``; run per-chunk guard
  5  DB → ``phase=translating``; dispatch all chunks via ``asyncio.gather``
     (each chunk acquires ``chunk_semaphore`` then ``vllm_semaphore``)
  6  ``join_after``-aware assembly → ``translated_markdown``
  7  Write ``/var/cache/translate/results/{job_id}_result.json``
  8  DB → ``completed`` / ``failed``; delete staging dir; release ``job_limiter``
"""

import asyncio
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import httpx

from common.misc_utils import get_logger
from translate.db.manager import db_manager
from translate.models import JobStatus, TranslationChunk
from translate.settings import settings
from translate.utils.chunking import build_translation_chunks
from translate.utils.guard import check_chunk_guard
from translate.utils.llm import query_vllm_translate
from translate.utils.prompt import build_messages, resolve_source_language
from translate.utils.storage import storage_manager
from translate.workers.concurrency import concurrency_manager

logger = get_logger("worker")


# ---------------------------------------------------------------------------
# Per-chunk translation task
# ---------------------------------------------------------------------------

async def _translate_chunk(
    chunk: TranslationChunk,
    resolved_source_language: Optional[str],
    target_language: str,
    http_client: httpx.AsyncClient,
) -> tuple[TranslationChunk, str, int, int]:
    """
    Translate a single chunk, respecting both the per-job and global semaphores.

    Returns:
        ``(chunk, translated_text, input_tokens, output_tokens)``

    Raises:
        httpx.HTTPStatusError: Non-2xx response from vLLM (propagates to gather).
        RuntimeError:          Per-chunk context guard breach.
    """
    max_tokens, diag = check_chunk_guard(chunk.token_count, settings)
    if diag:
        raise RuntimeError(
            f"Chunk {chunk.index} failed context guard: {diag}"
        )

    messages = build_messages(chunk.text, resolved_source_language, target_language)
    llm_endpoint = settings.common.llm.endpoint
    model = settings.common.llm.model

    async with concurrency_manager.chunk_semaphore:
        async with concurrency_manager.vllm_semaphore:
            translation, in_tok, out_tok = await query_vllm_translate(
                client=http_client,
                llm_endpoint=llm_endpoint,
                messages=messages,
                model=model,
                max_tokens=max_tokens,
                temperature=settings.translate.translation_temperature,
            )

    return chunk, translation, in_tok, out_tok


# ---------------------------------------------------------------------------
# Result assembly
# ---------------------------------------------------------------------------

def _assemble_translation(
    results: list[tuple[TranslationChunk, str, int, int]],
) -> tuple[str, int, int]:
    """
    Join translated chunks using ``join_after``-aware logic (§2.6).

    ``"paragraph"`` boundaries use ``"\\n\\n"``.
    ``"sentence"`` boundaries (sentence-fallback sub-chunks) use ``" "``.

    Args:
        results: List of ``(chunk, translation, in_tok, out_tok)`` in chunk
                 index order (as returned by ``asyncio.gather``).

    Returns:
        ``(translated_markdown, total_input_tokens, total_output_tokens)``
    """
    parts: list[str] = []
    total_in = total_out = 0

    for i, (chunk, translation, in_tok, out_tok) in enumerate(results):
        parts.append(translation)
        total_in += in_tok
        total_out += out_tok
        if i < len(results) - 1:
            parts.append(" " if chunk.join_after == "sentence" else "\n\n")

    return "".join(parts), total_in, total_out


# ---------------------------------------------------------------------------
# Main worker entry-point
# ---------------------------------------------------------------------------

async def run_translation_job(
    job_id: str,
    staged_file_path: Path,
    source_language: str,
    target_language: str,
    document_name: str,
    input_type: str,
) -> None:
    """
    Full async translation pipeline for a single job.

    This function is intended to be launched as a FastAPI ``BackgroundTask``
    immediately after the ``POST /v1/translate/jobs`` response is sent.

    Args:
        job_id:           UUID of the job row already in the DB.
        staged_file_path: Absolute path to the uploaded file in staging.
        source_language:  Value from the request (``"auto"`` or a language name).
        target_language:  Display name of the target language (e.g. ``"English"``).
        document_name:    Original filename (for result metadata).
        input_type:       ``"txt"`` or ``"md"`` (used in result metadata).
    """
    start_total = time.perf_counter()
    llm_endpoint = settings.common.llm.endpoint
    model = settings.common.llm.model

    async with concurrency_manager.job_limiter:
        try:
            # ------------------------------------------------------------------
            # Step 1 — Mark in_progress, phase=chunking
            # ------------------------------------------------------------------
            db_manager.update_job(
                job_id=job_id,
                status=JobStatus.IN_PROGRESS,
                metadata={"phase": "chunking"},
            )
            logger.info(f"[{job_id}] Worker started — chunking")

            # ------------------------------------------------------------------
            # Step 2 — Read staged file as UTF-8
            # ------------------------------------------------------------------
            try:
                text = staged_file_path.read_text(encoding="utf-8")
            except Exception as exc:
                raise RuntimeError(f"Failed to read staged file: {exc}") from exc

            # ------------------------------------------------------------------
            # Step 3 — Resolve source language via lingua (three-position sampling)
            # ------------------------------------------------------------------
            start_chunking = time.perf_counter()
            resolved_name, resolved_code = resolve_source_language(
                text=text,
                requested_source=source_language,
                settings=settings,
                async_path=True,
            )

            # Update DB with resolved source language so it's visible in detail view.
            if resolved_name:
                db_manager.update_job(
                    job_id=job_id,
                    source_language=resolved_name.lower(),
                )
                logger.debug(
                    f"[{job_id}] Resolved source language: {resolved_name} ({resolved_code})"
                )
            else:
                logger.debug(f"[{job_id}] Source language: LLM auto-detect")

            # ------------------------------------------------------------------
            # Step 4 — Chunk the document; run per-chunk guard
            # ------------------------------------------------------------------
            chunks = await build_translation_chunks(
                text=text,
                llm_endpoint=llm_endpoint,
                source_language_code=resolved_code,
            )

            chunking_secs = round(time.perf_counter() - start_chunking, 3)
            logger.info(
                f"[{job_id}] Chunked into {len(chunks)} chunk(s) in {chunking_secs}s"
            )

            # Guard check: run eagerly before dispatching any LLM calls.
            # A breach here means CHUNK_TOKEN_BUDGET is misconfigured.
            for chunk in chunks:
                max_tokens, diag = check_chunk_guard(chunk.token_count, settings)
                if diag:
                    raise RuntimeError(
                        f"CONTEXT_LIMIT_EXCEEDED: chunk {chunk.index} "
                        f"({chunk.token_count} tokens) exceeds context window. "
                        f"Diagnostics: {diag}"
                    )

            # ------------------------------------------------------------------
            # Step 5 — Transition to translating; dispatch chunks concurrently
            # ------------------------------------------------------------------
            db_manager.update_job(
                job_id=job_id,
                metadata={
                    "phase": "translating",
                    "chunking": {
                        "chunk_count": len(chunks),
                        "chunk_token_budget": settings.translate.chunk_token_budget,
                    },
                    "timing_in_secs": {"chunking": chunking_secs},
                },
            )

            start_translating = time.perf_counter()

            # Build one httpx.AsyncClient for all chunk calls in this job.
            api_key = settings.common.llm.api_key
            headers = {"Content-Type": "application/json"}
            if api_key:
                headers["Authorization"] = f"Bearer {api_key}"

            async with httpx.AsyncClient(headers=headers, timeout=3600.0) as http_client:
                tasks = [
                    asyncio.create_task(
                        _translate_chunk(
                            chunk=chunk,
                            resolved_source_language=resolved_name,
                            target_language=target_language,
                            http_client=http_client,
                        )
                    )
                    for chunk in chunks
                ]
                try:
                    results: list[tuple[TranslationChunk, str, int, int]] = (
                        await asyncio.gather(*tasks)
                    )
                except Exception:
                    # Cancel all sibling tasks on any failure.
                    for t in tasks:
                        if not t.done():
                            t.cancel()
                    await asyncio.gather(*tasks, return_exceptions=True)
                    raise

            translating_secs = round(time.perf_counter() - start_translating, 3)
            logger.info(
                f"[{job_id}] Translated {len(chunks)} chunk(s) in {translating_secs}s"
            )

            # ------------------------------------------------------------------
            # Step 6 — join_after-aware assembly
            # ------------------------------------------------------------------
            translated_markdown, total_in_tok, total_out_tok = _assemble_translation(
                results
            )

            # ------------------------------------------------------------------
            # Step 7 — Write result file
            # ------------------------------------------------------------------
            total_ms = round((time.perf_counter() - start_total) * 1000)

            result_payload = {
                "job_id": job_id,
                "data": {
                    "translation": translated_markdown,
                    "source_language": resolved_name.lower() if resolved_name else source_language.lower(),
                    "target_language": target_language.lower(),
                    "input_type": input_type,
                    "document_name": document_name,
                },
                "meta": {
                    "model": model,
                    "processing_time_ms": total_ms,
                    "timing_in_secs": {
                        "chunking": chunking_secs,
                        "translating": translating_secs,
                    },
                },
                "usage": {
                    "input_tokens": total_in_tok,
                    "output_tokens": total_out_tok,
                    "total_tokens": total_in_tok + total_out_tok,
                },
            }

            storage_manager.write_result(job_id, result_payload)

            # ------------------------------------------------------------------
            # Step 8 — DB → completed
            # ------------------------------------------------------------------
            completed_metadata = {
                "phase": "completed",
                "input_tokens": total_in_tok,
                "output_tokens": total_out_tok,
                "model": model,
                "processing_time_ms": total_ms,
                "timing_in_secs": {
                    "chunking": chunking_secs,
                    "translating": translating_secs,
                },
                "chunking": {
                    "chunk_count": len(chunks),
                    "chunk_token_budget": settings.translate.chunk_token_budget,
                },
            }

            db_manager.update_job(
                job_id=job_id,
                status=JobStatus.COMPLETED,
                completed_at=datetime.now(timezone.utc),
                metadata=completed_metadata,
                source_language=(resolved_name.lower() if resolved_name else None),
            )
            logger.info(
                f"[{job_id}] ✅ Completed in {total_ms}ms "
                f"({total_in_tok} in / {total_out_tok} out tokens)"
            )

        except Exception as exc:
            error_msg = str(exc)
            logger.error(f"[{job_id}] ❌ Job failed: {error_msg}", exc_info=True)
            db_manager.update_job(
                job_id=job_id,
                status=JobStatus.FAILED,
                completed_at=datetime.now(timezone.utc),
                error=error_msg,
                metadata={"phase": "failed"},
            )

        finally:
            # Always clean up staging — even on failure.
            storage_manager.cleanup_staging(job_id)
            logger.debug(f"[{job_id}] Staging directory cleaned up")

