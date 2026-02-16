import logging
import os
import time
import re
from asyncio import BoundedSemaphore
from typing import Optional
import uvicorn
import pypdfium2 as pdfium
from fastapi import FastAPI, Request, UploadFile
from pydantic import BaseModel, Field
from fastapi.responses import JSONResponse
from fastapi.openapi.docs import get_swagger_ui_html

from common.llm_utils import create_llm_session, query_vllm_summarize
from common.settings import get_settings
from common.misc_utils import get_model_endpoints, set_log_level, get_logger


log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif not "info" in level:
        raise Exception(f"Unknown LOG_LEVEL passed: '{level}'")
set_log_level(log_level)
logger = get_logger("Summarize")

settings = get_settings()
concurrency_limiter = BoundedSemaphore(settings.max_concurrent_requests)

app = FastAPI(
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

create_llm_session(pool_maxsize=settings.vllm_pool_size)


ALLOWED_FILE_EXTENSIONS = {".txt", ".pdf"}

# Pre-compute max input word count from context length at startup
# input_words/ratio + buf + (input_words/ratio)*coeff < max_model_len
# => input_words * (1 + coeff) / ratio < max_model_len - buf
MAX_INPUT_WORDS = int(
    (settings.context_lengths.granite_3_3_8b_instruct - settings.summarization_token_buffer)
    * settings.token_to_word_ratios.en
    / (1 + settings.summarization_coefficient)
)

def initialize_models():
    global llm_model_dict
    _, llm_model_dict, _ = get_model_endpoints()

def _word_count(text: str) -> int:
    return len(text.split())



def _compute_target_and_max_tokens(input_word_count: int, summary_length: Optional[int]):
    if summary_length is not None:
        target_words = summary_length
    else:
        target_words = max(1, int(input_word_count * settings.summarization_coefficient))

    est_output_tokens = int(target_words / settings.token_to_word_ratios.en)
    max_tokens = est_output_tokens + settings.summarization_token_buffer
    logger.debug(f"max tokens: {max_tokens}, estimated output tokens: {est_output_tokens}")
    return target_words, max_tokens

def _extract_text_from_pdf(content: bytes) -> str:
    pdf = pdfium.PdfDocument(content)
    text_parts = []
    for page_index in range(len(pdf)):
        page = pdf[page_index]
        textpage = page.get_textpage()
        text_parts.append(textpage.get_text_range())
        textpage.close()
        page.close()
    pdf.close()
    return "\n".join(text_parts)

def _trim_to_last_sentence(text: str) -> str:
    """Remove any trailing incomplete sentence."""
    match = re.match(r"(.*[.!?])", text, re.DOTALL)
    return match.group(1).strip() if match else text.strip()

def _build_success_response(
    summary: str,
    original_length: int,
    input_type: str,
    model: str,
    processing_time_ms: int,
    input_tokens: int,
    output_tokens: int,
):
    return {
        "data": {
            "summary": summary,
            "original_length": original_length,
            "summary_length": _word_count(summary),
        },
        "meta": {
            "model": model,
            "processing_time_ms": processing_time_ms,
            "input_type": input_type,
        },
        "usage": {
            "input_tokens": input_tokens,
            "output_tokens": output_tokens,
            "total_tokens": input_tokens + output_tokens,
        },
    }


def _build_error_response(code: str, message: str, status: int):
    return JSONResponse(
        status_code=status,
        content={
            "error": {
                "code": code,
                "message": message,
                "status": status,
            }
        },
    )


def _build_prompt(content_text, target_words, summary_length):
    if summary_length:
        return settings.prompts.summarize_with_length.format(target_words=target_words, text=content_text)
    else:
        return settings.prompts.summarize_without_length.format(text=content_text)

def _build_messages(text, target_words, summary_length) -> list:
    if summary_length:
        user_prompt = settings.prompts.summarize_user_prompt_with_length.format(target_words=target_words, text=text)
    else:
        user_prompt = settings.prompts.summarize_user_prompt_without_length.format(text=text)
    return [
        {
            "role": "system",
            "content": settings.prompts.summarize_system_prompt,
        },
        {
            "role": "user",
            "content": user_prompt,
        },
    ]


async def _handle_summarize(
    llm_model: str,
    llm_endpoint: str,
    content_text: str,
    input_type: str,
    summary_length: Optional[int],
):
    """Core summarization logic shared by both JSON and form-data paths."""
    input_word_count = _word_count(content_text)
    if summary_length and summary_length > input_word_count:
        return _build_error_response(
            "INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH",
            "Input text is smaller than summary length",
            400,
        )

    if input_word_count > MAX_INPUT_WORDS:
        return _build_error_response(
            "CONTEXT_LIMIT_EXCEEDED",
            "File size exceeds maximum token limit",
            413,
        )

    target_words, max_tokens = _compute_target_and_max_tokens(input_word_count, summary_length)

    messages = _build_messages(content_text, target_words, summary_length)

    await concurrency_limiter.acquire()
    try:
        start = time.time()
        result = query_vllm_summarize(
            llm_endpoint=llm_endpoint,
            messages=messages,
            model=llm_model,
            max_tokens=max_tokens,
            temperature=settings.summarization_temperature,
        )
        elapsed_ms = int((time.time() - start) * 1000)
    finally:
        concurrency_limiter.release()

    if isinstance(result, dict) and "error" in result:
        return _build_error_response(
            "LLM_ERROR",
            "Failed to generate summary. Please try again later",
            500,
        )

    summary = _trim_to_last_sentence(result) if isinstance(result, str) else ""
    input_tokens = int(input_word_count / settings.token_to_word_ratios.en)
    output_tokens = int(_word_count(summary) / settings.token_to_word_ratios.en)

    return _build_success_response(
        summary=summary,
        original_length=input_word_count,
        input_type=input_type,
        model=llm_model,
        processing_time_ms=elapsed_ms,
        input_tokens=input_tokens,
        output_tokens=output_tokens,
    )

class SummaryData(BaseModel):
    summary: str = Field(..., description="The generated summary text.")
    original_length: int = Field(..., description="Word count of original text.")
    summary_length: int = Field(..., description="Word count of the generated summary.")


class SummaryMeta(BaseModel):
    model: str = Field(..., description="The AI model used for summarization.")
    processing_time_ms: int = Field(..., description="Request processing time in milliseconds.")
    input_type: str = Field(..., description="The type of input provided. Valid values: text, file.")


class SummaryUsage(BaseModel):
    input_tokens: int = Field(..., description="Number of input tokens consumed.")
    output_tokens: int = Field(..., description="Number of output tokens generated.")
    total_tokens: int = Field(..., description="Total number of tokens used (input + output).")


class SummarizeSuccessResponse(BaseModel):
    data: SummaryData
    meta: SummaryMeta
    usage: SummaryUsage

    model_config = {
        "json_schema_extra": {
            "example": {
                "data": {
                    "summary": "AI has advanced significantly through deep learning and large language models, impacting healthcare, finance, and transportation with both opportunities and ethical challenges.",
                    "original_length": 250,
                    "summary_length": 22,
                },
                "meta": {
                    "model": "ibm-granite/granite-3.3-8b-instruct",
                    "processing_time_ms": 1245,
                    "input_type": "text",
                },
                "usage": {
                    "input_tokens": 385,
                    "output_tokens": 62,
                    "total_tokens": 447,
                },
            }
        }
    }



class ErrorDetail(BaseModel):
    code: str = Field(..., description="Machine-readable error code.")
    message: str = Field(..., description="Human-readable error message.")
    status: int = Field(..., description="HTTP status code.")


class SummarizeErrorResponseBadRequest(BaseModel):
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "MISSING_INPUT",
                    "message": "Either 'text' or 'file' parameter is required",
                    "status": 400,
                }
            }
        }
    }

class SummarizeErrorResponseContextLimitExceeded(BaseModel):
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "CONTEXT_LIMIT_EXCEEDED",
                    "message": "File size exceeds maximum token limit",
                    "status": 413,
                }
            }
        }
    }

class SummarizeErrorResponseUnsupportedContentType(BaseModel):
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "UNSUPPORTED_CONTENT_TYPE",
                    "message":  "Content-Type must be application/json or multipart/form-data",
                    "status": 415,
                }
            }
        }
    }

class SummarizeErrorResponseInternalServiceError(BaseModel):
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "LLM_ERROR",
                    "message":  "Failed to generate summary. Please try again later",
                    "status": 500,
                }
            }
        }
    }

_error_responses = {
    400: {"description": "Bad request (missing input, unsupported file type, invalid params)", "model": SummarizeErrorResponseBadRequest},
    413: {"description": "Input exceeds context window limit", "model": SummarizeErrorResponseContextLimitExceeded},
    415: {"description": "Unsupported Content-Type", "model": SummarizeErrorResponseUnsupportedContentType},
    500: {"description": "LLM service error", "model": SummarizeErrorResponseInternalServiceError},
}


@app.post("/v1/summarize",
          response_model=SummarizeSuccessResponse,
          responses=_error_responses,
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
    llm_model = llm_model_dict['llm_model']
    llm_endpoint = llm_model_dict['llm_endpoint']
    content_type = request.headers.get("content-type", "")

    # ----- JSON path -----
    if "application/json" in content_type:
        try:
            body = await request.json()
        except Exception:
            return _build_error_response(
                "INVALID_JSON",
                "Request body is not valid JSON",
                400,
            )

        text = body.get("text").strip()
        if not text:
            return _build_error_response(
                "MISSING_INPUT",
                "Either 'text' or 'file' parameter is required",
                400,
            )

        summary_length = body.get("length")
        if summary_length is not None:
            try:
                summary_length = int(summary_length)
            except (TypeError, ValueError):
                return _build_error_response(
                    "INVALID_PARAMETER",
                    "length must be an integer",
                    400,
                )

        return await _handle_summarize(llm_model, llm_endpoint, text, "text", summary_length)

    # ----- Multipart / form-data path -----
    elif "multipart/form-data" in content_type:
        form = await request.form()
        file: Optional[UploadFile] = form.get("file")
        length_raw = form.get("length")

        summary_length: Optional[int] = None
        if length_raw is not None:
            try:
                summary_length = int(length_raw)
            except (TypeError, ValueError):
                return _build_error_response(
                    "INVALID_PARAMETER",
                    "length must be an integer",
                    400,
                )

        if file and hasattr(file, "filename"):
            filename = file.filename or ""
            ext = os.path.splitext(filename)[1].lower()
            if ext not in ALLOWED_FILE_EXTENSIONS:
                return _build_error_response(
                    "UNSUPPORTED_FILE_TYPE",
                    "Only .txt and .pdf files are allowed.",
                    400,
                )
            raw = await file.read()
            if ext == ".pdf":
                try:
                    start = time.time()
                    content_text = _extract_text_from_pdf(raw)
                    logger.info(f"PDF extraction took {(time.time() - start) * 1000:.0f}ms")
                except Exception as e:
                    logger.error(f"PDF extraction failed: {e}")
                    return _build_error_response(
                        "PDF_EXTRACTION_ERROR",
                        "Failed to extract text from PDF file.",
                        400,
                    )
            else:
                content_text = raw.decode("utf-8", errors="replace")
            input_type = "file"
        else:
            return _build_error_response(
                "MISSING_INPUT",
                "Either 'text' or 'file' parameter is required",
                400,
            )

        if not content_text or not content_text.strip():
            return _build_error_response(
                "EMPTY_INPUT",
                "The provided input contains no extractable text.",
                400,
            )

        return await _handle_summarize(llm_model, llm_endpoint, content_text.strip(), input_type, summary_length)

    else:
        return _build_error_response(
            "UNSUPPORTED_CONTENT_TYPE",
            "Content-Type must be application/json or multipart/form-data",
            415,
        )


@app.get("/health")
async def health():
    return {"status": "ok"}


if __name__ == "__main__":
    initialize_models()
    port = int(os.getenv("PORT", "8000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
