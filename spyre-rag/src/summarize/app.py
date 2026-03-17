import asyncio
import time
import logging
import os
import uuid
from contextlib import asynccontextmanager
from typing import Optional

import uvicorn
from fastapi import FastAPI, Request, UploadFile
from fastapi.openapi.docs import get_swagger_ui_html
from fastapi.responses import JSONResponse, StreamingResponse
from starlette.concurrency import iterate_in_threadpool

from common.llm_utils import create_llm_session, query_vllm_summarize, query_vllm_summarize_stream
from common.misc_utils import get_model_endpoints
from common.settings import get_settings
from common.misc_utils import set_log_level, get_logger, set_request_id
from summarize.summ_utils import (
    SummarizeException,
    word_count,
    build_success_response,
    build_messages,
    trim_to_last_sentence,
    MAX_INPUT_WORDS,
    compute_target_and_max_tokens,
    SummarizeSuccessResponse,
    error_responses,
    validate_summary_length,
    extract_text_from_pdf
)

log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif not "info" in level:
        logging.warning(f"Unknown LOG_LEVEL passed: '{level}'")
set_log_level(log_level)
logger = get_logger("app")

settings = get_settings()
concurrency_limiter = asyncio.BoundedSemaphore(settings.max_concurrent_requests)

@asynccontextmanager
async def lifespan(app):
    initialize_models()
    create_llm_session(pool_maxsize=settings.max_concurrent_requests)
    yield

# OpenAPI tags metadata for endpoint organization
tags_metadata = [
    {
        "name": "summarization",
        "description": "Text and document summarization operations"
    },
    {
        "name": "health",
        "description": "Health check and service status"
    }
]

app = FastAPI(
    lifespan=lifespan,
    title="AI-Services Summarization API",
    description="Accepts text or files (.txt / .pdf) and returns AI-generated summaries.",
    version="1.0.0",
    openapi_tags=tags_metadata
)

@app.middleware("http")
async def add_request_id(request: Request, call_next):
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4()))
    set_request_id(request_id)
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    return response

@app.get("/", include_in_schema=False)
def swagger_root():
    """Expose Swagger UI at the root path (/)"""
    return get_swagger_ui_html(
        openapi_url="/openapi.json",
        title="AI-Services Summarization API - Swagger UI",
    )

ALLOWED_FILE_EXTENSIONS = {".txt", ".pdf"}

@app.exception_handler(SummarizeException)
async def summarize_exception_handler(request: Request, exc: SummarizeException):
    return JSONResponse(
        status_code=exc.code,
        content={
            "error": {
                "code": exc.code,
                "message": exc.message,
                "status": exc.status,
            }
        },
    )

def initialize_models():
    global llm_model_dict
    _, llm_model_dict,_  = get_model_endpoints()

async def locked_stream(stream_g):
    """Wrap a vLLM SSE generator, releasing the concurrency semaphore when the stream ends."""
    try:
        async for chunk in iterate_in_threadpool(stream_g):
            yield chunk
    finally:
        concurrency_limiter.release()


def _validate_input_word_count(input_word_count: int, summary_length: Optional[int]) -> None:
    """Raise SummarizeException if word-count constraints are violated."""
    if summary_length and summary_length > input_word_count:
        raise SummarizeException(
            400, "INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH",
            "Input text is smaller than summary length",
        )
    if input_word_count > MAX_INPUT_WORDS:
        raise SummarizeException(
            413, "CONTEXT_LIMIT_EXCEEDED",
            "Input size exceeds maximum token limit",
        )


async def handle_summarize(
    content_text: str,
    input_type: str,
    summary_length: Optional[int],
    stream: bool = False,
):
    """Core summarization logic shared by both JSON and form-data paths."""
    input_word_count = word_count(content_text)
    _validate_input_word_count(input_word_count, summary_length)

    target_words, max_tokens = compute_target_and_max_tokens(input_word_count, summary_length)
    messages = build_messages(content_text, target_words, summary_length)

    logger.info(
        f"Received {input_type} request with input size:{input_word_count} words"
        f"{f', target summary length: {summary_length} words' if summary_length is not None else ''}"
    )

    llm_endpoint = llm_model_dict['llm_endpoint']
    llm_model = llm_model_dict['llm_model']

    if stream:
        await concurrency_limiter.acquire()
        try:
            vllm_stream = query_vllm_summarize_stream(
                llm_endpoint=llm_endpoint,
                messages=messages,
                model=llm_model,
                max_tokens=max_tokens,
                temperature=settings.summarization_temperature,
            )
        except Exception as e:
            logger.error(f"LLM call failed with error: {e}")
            concurrency_limiter.release()
            raise SummarizeException(500, "LLM_ERROR",
                                     f"Failed to generate summary, error: {e} Please try again later")
        return StreamingResponse(
            locked_stream(vllm_stream),
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
                "Access-Control-Allow-Headers": "Content-Type",
            },
        )

    async with concurrency_limiter:
        start = time.time()
        result, in_tokens, out_tokens = query_vllm_summarize(
            llm_endpoint=llm_endpoint,
            messages=messages,
            model=llm_model,
            max_tokens=max_tokens,
            temperature=settings.summarization_temperature,
        )
        logger.info(f"Input tokens: {in_tokens}, output tokens: {out_tokens}")
        elapsed_ms = int((time.time() - start) * 1000)

    if isinstance(result, dict) and "error" in result:
        raise SummarizeException(500, "LLM_ERROR",
                                 "Failed to generate summary. Please try again later")

    summary = trim_to_last_sentence(result) if isinstance(result, str) else ""

    return build_success_response(
        summary=summary,
        original_length=input_word_count,
        input_type=input_type,
        model=llm_model,
        processing_time_ms=elapsed_ms,
        input_tokens=in_tokens,
        output_tokens=out_tokens,
    )

@app.post("/v1/summarize",
response_model=SummarizeSuccessResponse,
responses=error_responses,
summary="Summarize text or file",
description=(
      "Accepts **either** `application/json` or `multipart/form-data` based on "
      "the `Content-Type` header.\n\n"
      "---\n\n"
      "### Option 1: JSON body (`Content-Type: application/json`)\n\n"
      "| Field | Type | Required | Description |\n"
      "|-------|------|----------|-------------|\n"
      "| `text` | string | Yes | Plain text content to summarize |\n"
      "| `length` | integer | No | Desired summary length in words  |\n"
      "| `stream` | boolean | No | Stream the summary as it is generated, default False |\n\n"
      "**Example:**\n"
      "```bash\n"
      'curl -X POST /v1/summarize -H "Content-Type: application/json" -d '
      '{\n'
      '  "text": "Artificial intelligence has made significant progress...",\n'
      '  "length": 25\n'
      '}\n'
      "```\n\n"
      "---\n\n"
      "### Option 2: Form data (`Content-Type: multipart/form-data`)\n\n"
      "| Field | Type | Required | Description |\n"
      "|-------|------|----------|-------------|\n"
      "| `file` | file | Conditional | `.txt` or `.pdf` file to summarize |\n"
      "| `length` | integer | No | Desired summary length in words |\n"
      "| `stream` | boolean | No | Stream the summary as it is generated, default False |\n\n"
      "**Example (curl):**\n"
      "```bash\n"
      'curl -X POST /v1/summarize -F "file=@report.pdf" -F "length=100"\n'
      "```\n\n"
      "---\n\n"
      "**Note:** Swagger UI cannot render interactive input fields for this endpoint "
      "because it accepts two different content types. Use curl or Postman to test."
),
response_description="Summarization result with metadata and token usage.",
tags=["Summarization"],
)
async def summarize(request: Request):
    """Accept plain text via JSON or text/file via multipart/form-data."""
    try:
        if concurrency_limiter.locked():
            raise SummarizeException(429, "SERVER_BUSY",
                                     "Server is busy. Please try again later.")
        content_type = request.headers.get("content-type", "")

        # ----- JSON path -----
        if "application/json" in content_type:
            try:
                body = await request.json()
            except Exception as e:
                logger.error(f"error: {e}")
                raise SummarizeException(400, "INVALID_JSON",
                                         "Request body is not valid JSON")

            text = body.get("text", "").strip()
            if not text:
                raise SummarizeException(400, "MISSING_INPUT",
                                         "Either 'text' or 'file' parameter is required")
            summary_length = validate_summary_length(body.get("length"))
            stream = bool(body.get("stream", False))

            return await handle_summarize(text, "text", summary_length, stream)

        # ----- Multipart / form-data path -----
        elif "multipart/form-data" in content_type:
            form = await request.form()
            file: Optional[UploadFile] = form.get("file")  # type: ignore[assignment]

            summary_length = validate_summary_length(form.get("length"))
            stream = str(form.get("stream", "false")).lower() == "true"

            if file and hasattr(file, "filename"):
                filename = file.filename or ""
                ext = os.path.splitext(filename)[1].lower()
                if ext not in ALLOWED_FILE_EXTENSIONS:
                    raise SummarizeException(400, "UNSUPPORTED_FILE_TYPE",
                                             "Only .txt and .pdf files are allowed.")
                raw = await file.read()
                if ext == ".pdf":
                    try:
                        start = time.time()
                        content_text = await asyncio.to_thread(extract_text_from_pdf, raw)
                        logger.debug(f"PDF extraction took {(time.time() - start) * 1000:.0f}ms")
                    except Exception as e:
                        logger.error(f"PDF extraction failed: {e}")
                        raise SummarizeException(415, "UNSUPPORTED_CONTENT_TYPE",
                                                 "File is not a valid txt/pdf file.")
                else:
                    try:
                        content_text = raw.decode("utf-8", errors="strict")
                    except UnicodeDecodeError as e:
                        logger.error(f"Failed to decode text file as UTF-8: {e}")
                        raise SummarizeException(415, "UNSUPPORTED_CONTENT_TYPE",
                                                 "File is not a valid txt/pdf file.")
            else:
                raise SummarizeException(400, "MISSING_INPUT",
                                         "Either 'text' or 'file' parameter is required")

            if not content_text or not content_text.strip():
                raise SummarizeException(400, "EMPTY_INPUT",
                                         "The provided input contains no extractable text.")
            return await handle_summarize(content_text.strip(), "file", summary_length, stream)

        else:
            raise SummarizeException(415, "UNSUPPORTED_CONTENT_TYPE",
                                     "Content-Type must be application/json or multipart/form-data")

    except SummarizeException as se:
        raise se
    except Exception as e:
        logger.error(f"Got exception while generating summary: {e}")
        raise SummarizeException(500, "INTERNAL_SERVER_ERROR",
                                 f"Failed to generate summary, error: {e} Please try again later")

@app.get(
    "/health",
    tags=["health"],
    summary="Health check",
    description="Check if the service is running and healthy.",
    response_description="Service health status"
)
async def health():
    return {"status": "ok"}


if __name__ == "__main__":
    port = int(os.getenv("PORT", "6000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
