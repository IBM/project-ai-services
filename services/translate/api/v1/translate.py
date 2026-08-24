"""
Synchronous translation endpoint.

POST /v1/translate — inline plain-text translation (stateless).

No DB writes, no staging.  The full pipeline is:
  1. Validate request fields.
  2. Acquire concurrency_limiter (vllm_semaphore); 429 if at capacity.
  3. Tokenize input; run sync context guard; 413 with diagnostics on breach.
  4. Resolve source language; build messages.
  5. Call query_vllm_translate (temperature=0.0).
  6. Return 200 with data / meta / usage.
"""

import asyncio
import time

import httpx
from fastapi import APIRouter, status

from common.error_utils import APIError, ErrorCode, http_error_responses
from common.llm_utils import tokenize_with_llm
from common.misc_utils import get_logger
from translate.models import SyncTranslateRequest, SyncTranslateResponse
from translate.settings import settings
from translate.utils.errors import _raise_context_limit_exceeded
from translate.utils.language import validate_languages
from translate.utils.llm import get_llm_max_model_len, query_vllm_translate
from translate.utils.prompt import build_messages, resolve_source_language
from translate.workers.concurrency import concurrency_manager

logger = get_logger("translate_api")

router = APIRouter()


# ---------------------------------------------------------------------------
# POST /v1/translate
# ---------------------------------------------------------------------------

@router.post(
    "",
    status_code=status.HTTP_200_OK,
    response_model=SyncTranslateResponse,
    response_description="Translated text with source/target language and token usage.",
    summary="Translate plain text (synchronous)",
    description=(
        "Translate inline plain text between supported languages. "
        "Stateless — no file upload, no DB record. "
        "Source language is auto-detected when omitted or set to 'auto'."
    ),
    responses={
        400: http_error_responses[400],   # INVALID_LANGUAGE / SAME_LANGUAGE
        413: http_error_responses[413],   # CONTEXT_LIMIT_EXCEEDED
        429: http_error_responses[429],   # concurrency_limiter at capacity
        500: http_error_responses[500],
        503: http_error_responses[503],   # vLLM unreachable
    },
)
async def sync_translate(body: SyncTranslateRequest) -> SyncTranslateResponse:
    start_time = time.perf_counter()

    # 1. Validate input text.
    if not body.text or not body.text.strip():
        APIError.raise_error(ErrorCode.EMPTY_INPUT, "The 'text' field must not be empty.")

    # 2. Validate languages.
    norm_source, norm_target = validate_languages(body.source_language, body.target_language)

    # 3. Check concurrency_limiter (vllm_semaphore) — non-blocking probe.
    if concurrency_manager.vllm_semaphore.locked():
        APIError.raise_error(
            ErrorCode.RATE_LIMIT_EXCEEDED,
            "Server is at capacity. Please try again shortly.",
        )

    # 4. Tokenize and run sync context guard.
    llm_endpoint = settings.common.llm.endpoint
    input_tokens: int = await asyncio.to_thread(
        lambda: len(tokenize_with_llm(body.text, llm_endpoint))
    )

    max_model_len = get_llm_max_model_len()
    prompt_overhead = settings.translate.prompt_overhead_tokens
    available_for_output = max_model_len - input_tokens - prompt_overhead

    if available_for_output < input_tokens:
        _raise_context_limit_exceeded(
            "Input text exceeds the model context limit for translation "
            "(output window must be at least as large as the input).",
            diagnostics={
                "input_tokens": input_tokens,
                "prompt_overhead_tokens": prompt_overhead,
                "max_model_len": max_model_len,
                "available_for_output": available_for_output,
            },
        )

    # Cap output tokens: 2× input provides generous headroom for most language
    # pairs (en→de expansion is typically ~20–30 %).
    max_tokens = min(available_for_output, input_tokens * 2)

    # 5. Resolve source language and build prompt messages.
    resolved_name, _resolved_code = resolve_source_language(
        text=body.text,
        requested_source=norm_source,
        settings=settings,
        async_path=False,
    )

    messages = build_messages(
        text=body.text,
        resolved_source_language=resolved_name,
        target_language=norm_target.capitalize(),
    )

    # 6. Call vLLM, gated by the shared concurrency semaphore.
    model = settings.common.llm.model
    api_key = settings.common.llm.api_key
    headers = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"

    try:
        async with httpx.AsyncClient(headers=headers, timeout=3600.0) as client:
            async with concurrency_manager.vllm_semaphore:
                translation, in_tok, out_tok = await query_vllm_translate(
                    client=client,
                    llm_endpoint=llm_endpoint,
                    messages=messages,
                    model=model,
                    max_tokens=max_tokens,
                    temperature=settings.translate.translation_temperature,
                )
    except httpx.RequestError as exc:
        logger.error(f"vLLM unreachable during sync translate: {exc}", exc_info=True)
        APIError.raise_error(
            ErrorCode.VECTOR_STORE_NOT_READY,
            "Translation service is temporarily unavailable. Please try again later.",
        )
    except httpx.HTTPStatusError as exc:
        logger.error(
            f"vLLM returned HTTP {exc.response.status_code} during sync translate: {exc}",
            exc_info=True,
        )
        APIError.raise_error(
            ErrorCode.LLM_ERROR,
            "Translation model returned an unexpected error. Please try again.",
        )

    processing_time_ms = round((time.perf_counter() - start_time) * 1000)

    logger.info(
        f"Sync translate completed: {norm_source}→{norm_target}, "
        f"{in_tok} in / {out_tok} out tokens, {processing_time_ms}ms"
    )

    return SyncTranslateResponse(
        data={
            "translation": translation,
            "source_language": resolved_name.lower() if resolved_name else norm_source,
            "target_language": norm_target,
        },
        meta={
            "model": model,
            "processing_time_ms": processing_time_ms,
            "input_type": "text",
        },
        usage={
            "input_tokens": in_tok,
            "output_tokens": out_tok,
            "total_tokens": in_tok + out_tok,
        },
    )

