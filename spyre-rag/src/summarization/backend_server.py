import os
import logging
from typing import Optional
from threading import BoundedSemaphore
import requests
import uvicorn
from fastapi import FastAPI, File, Form, UploadFile, HTTPException
from pydantic import BaseModel

from common.settings import get_settings
from common.llm_utils import create_llm_session, query_vllm_completions
from common.misc_utils import get_model_endpoints, set_log_level, get_logger

log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif not "info" in level:
        raise Exception(f"Unknown LOG_LEVEL passed: '{level}'")
set_log_level(log_level)
logger = get_logger("LLM")
app = FastAPI(
    title="Summarization API",
    description="AI-powered text summarization service",
    version="1.0.0"
)

settings = get_settings()
concurrency_limiter = BoundedSemaphore(settings.max_concurrent_requests)

def initialize_models():
    global llm_model_dict
    _, llm_model_dict, _ = get_model_endpoints()
    
    
# Setting 32 to fully utilse the vLLM's Max Batch Size
POOL_SIZE = 32
MAX_WORDS_BEST_PERFORMANCE = 2000
MAX_WORDS_DEGRADED_PERFORMANCE = 21500
MAX_SUMMARY_LENGTH = 1000
MIN_SUMMARY_LENGTH = 10
MAX_FILE_SIZE_MB = settings.max_file_size_mb
MAX_FILE_SIZE_BYTES = MAX_FILE_SIZE_MB * 1024 *1024
create_llm_session(pool_maxsize=POOL_SIZE)

class SummaryResponse(BaseModel):
    summary: str
    original_length: int
    output_length: int
    model: str


def extract_text_from_pdf(file_content):
    """Extract text from PDF file content."""
    # call digitize documents API
    return "PDF text here"


def extract_text_from_txt(file_content):
    """Extract text from TXT file content."""
    try:
        return file_content.decode("utf-8").strip()
    except UnicodeDecodeError:
        try:
            return file_content.decode("latin-1").strip()
        except Exception as e:
            raise HTTPException(
                status_code=400,
                detail=f"Failed to decode text file: {str(e)}"
            )

def count_words(text: str) -> int:
    """Count words in text."""
    return len(text.split())


@app.post('/v1/summarize', response_model=SummaryResponse)
def summarize(text: Optional[str] = Form(None, description="Plain text content to summarize"),
    file: Optional[UploadFile] = File(None, description="File upload (.txt or .pdf)"),
    summary_length: int = Form(..., description="Desired summary length in words (1-1000)", ge=MIN_SUMMARY_LENGTH, le=MAX_SUMMARY_LENGTH)
):
    """
    Summarize text content from plain text or file upload.

    - text: Plain text content to summarize (required if file not provided)
    - file: File upload (.txt or .pdf) (required if text not provided)
    - summary_length: Desired summary length in number of words (1-1000)

    Returns a JSON response with the summary and metadata.
    """
    llm_model = llm_model_dict['llm_model']
    llm_endpoint = llm_model_dict['llm_endpoint']
    # Validate that either text or file is provided
    if text is None and file is None:
        raise HTTPException(
            status_code=400,
            detail="Either 'text' or 'file' parameter is required"
        )

    content_text = ""

    # If both are provided, text takes priority
    if text is not None and text.strip():
        content_text = text.strip()
    elif file is not None:
        # Validate file type
        filename = file.filename
        file_extension = filename.lower().split(".")[-1] if "." in filename else ""

        if file_extension not in ["txt", "pdf"]:
            raise HTTPException(
                status_code=400,
                detail="Unsupported file type. Only .txt and .pdf files are allowed"
            )

        file_content = file.file.read()

        # Validate file size
        if len(file_content) > MAX_FILE_SIZE_BYTES:
            raise HTTPException(
                status_code=413,
                detail=f"File size exceeds maximum limit of {MAX_FILE_SIZE_MB}MB"
            )

        # Extract text based on file type
        if file_extension == "pdf":
            content_text = extract_text_from_pdf(file_content)
        else:
            content_text = extract_text_from_txt(file_content)


    # Validate extracted text is not empty
    if not content_text.strip():
        raise HTTPException(
            status_code=400,
            detail="No text content found to summarize"
        )

    # Validate input text word count
    word_count = count_words(content_text)
    if word_count > MAX_WORDS_DEGRADED_PERFORMANCE:
        raise HTTPException(
            status_code=400,
            detail=f"Input text exceeds maximum word limit of {MAX_WORDS_DEGRADED_PERFORMANCE} words"
        )

    # Log warning if text exceeds the best performance threshold
    if word_count > MAX_WORDS_BEST_PERFORMANCE:
        logger.info(f"Input text exceeds maximum word limit of {MAX_WORDS_BEST_PERFORMANCE} words. Performance may be degraded.")

    prompt = settings.prompts.query_vllm_summarize.format(summary_length=summary_length, text=content_text)
    if not concurrency_limiter.acquire(blocking=False):
        raise HTTPException(
            status_code=429,
            detail="Server busy. Try again shortly."
        )
    try:
        summary = query_vllm_completions(llm_endpoint, prompt, llm_model, settings.llm_max_tokens, settings.summarization_temperature)

    except requests.exceptions.RequestException as e:
        concurrency_limiter.release()
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error calling vLLM API: {error_details}")

        raise HTTPException(
            status_code=500,
            detail=error_details
        )
    except Exception as e:
        concurrency_limiter.release()
        logger.error(f"Error calling vLLM API: {e}")
        raise HTTPException(
            status_code=500,
            detail=str(e)
        )
    concurrency_limiter.release()
    if not summary:
        raise HTTPException(
            status_code=500,
            detail="Something went wrong while generating the summary. Please try again later."
        )
    return SummaryResponse(
        summary=summary,
        original_length=word_count,
        output_length=count_words(summary),
        model=llm_model
    )

@app.get("/health")
def health_check():
    """Health check endpoint."""
    return {"status": "healthy"}


if __name__ == "__main__":
    initialize_models()
    uvicorn.run(app, host="0.0.0.0", port=8000)
