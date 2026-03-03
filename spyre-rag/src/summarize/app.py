import asyncio
import time
import logging
import os
from contextlib import asynccontextmanager
from typing import Optional

import uvicorn
from fastapi import FastAPI, Request, UploadFile
from fastapi.openapi.docs import get_swagger_ui_html
from fastapi.responses import JSONResponse
from common.llm_utils import create_llm_session, query_vllm_summarize
from common.misc_utils import get_model_endpoints
from common.settings import get_settings
from common.misc_utils import set_log_level, get_logger
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


app = FastAPI(lifespan=lifespan,
    title="AI-Services Summarization API",
    description="Accepts text or files (.txt / .pdf) and returns AI-generated summaries.",
    version="1.0.0"
)

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

async def handle_summarize(
    content_text: str,
    input_type: str,
    summary_length: Optional[int],
):
    """Core summarization logic shared by both JSON and form-data paths."""
    input_word_count = word_count(content_text)
    if summary_length and summary_length > input_word_count:
        raise SummarizeException(400, "INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH",
            "Input text is smaller than summary length")

    if input_word_count > MAX_INPUT_WORDS:
        raise SummarizeException(413, "CONTEXT_LIMIT_EXCEEDED",
                                 "Input size exceeds maximum token limit")

    target_words, max_tokens = compute_target_and_max_tokens(input_word_count, summary_length)

    messages = build_messages(content_text, target_words, summary_length)

    await concurrency_limiter.acquire()
    try:
        start = time.time()
        logger.info(f"Received {input_type} request with input size:{input_word_count} "
                    f"words{f', target summary length: {summary_length} words' if summary_length is not None else ''}")
        result, in_tokens, out_tokens = query_vllm_summarize(
            llm_endpoint=llm_model_dict['llm_endpoint'],
            messages=messages,
            model=llm_model_dict['llm_model'],
            max_tokens=max_tokens,
            temperature=settings.summarization_temperature,
        )
        logger.info(f"Input tokens: {in_tokens}, output tokens: {out_tokens}")
        elapsed_ms = int((time.time() - start) * 1000)
    finally:
        concurrency_limiter.release()

    if isinstance(result, dict) and "error" in result:
        raise SummarizeException(500, "LLM_ERROR",
                                 "Failed to generate summary. Please try again later")

    summary = trim_to_last_sentence(result) if isinstance(result, str) else ""

    return build_success_response(
        summary=summary,
        original_length=input_word_count,
        input_type=input_type,
        model=llm_model_dict['llm_model'],
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
      "| `length` | integer | No | Desired summary length in words  |\n\n"
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
      "| `length` | integer | No | Desired summary length in words |\n\n"
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
            except Exception:
                raise SummarizeException(400, "INVALID_JSON",
                                         "Request body is not valid JSON")

            text = body.get("text", "").strip()
            if not text:
                raise SummarizeException(400, "MISSING_INPUT",
                                         "Either 'text' or 'file' parameter is required")
            summary_length = validate_summary_length(body.get("length"))

            return await handle_summarize(text, "text", summary_length)

        # ----- Multipart / form-data path -----
        elif "multipart/form-data" in content_type:
            form = await request.form()
            file: Optional[UploadFile] = form.get("file")

            summary_length = validate_summary_length(form.get("length"))

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
                        raise SummarizeException(400, "PDF_EXTRACTION_ERROR",
                                                 "Failed to extract text from PDF file.")
                else:
                    content_text = raw.decode("utf-8", errors="replace")
            else:
                raise SummarizeException(400, "MISSING_INPUT",
                                         "Either 'text' or 'file' parameter is required")

            if not content_text or not content_text.strip():
                raise SummarizeException(400, "EMPTY_INPUT",
                                         "he provided input contains no extractable text.")
            return await handle_summarize(content_text.strip(), "file", summary_length)

        else:
            raise SummarizeException(415, "UNSUPPORTED_CONTENT_TYPE",
                                     "Content-Type must be application/json or multipart/form-data")

    except SummarizeException as se:
        raise se
    except Exception as e:
        logger.error(f"Got exception while generating summary: {e}")
        raise SummarizeException(500, "INTERNAL_SERVER_ERROR",
                                 "Failed to generate summary. Please try again later")

@app.get("/health")
async def health():
    return {"status": "ok"}


if __name__ == "__main__":
    port = int(os.getenv("PORT", "8000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
