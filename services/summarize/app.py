import asyncio
import time
import os
import uuid
from contextlib import asynccontextmanager
from typing import Optional

import uvicorn
from fastapi import FastAPI, Request, UploadFile, File, Form, BackgroundTasks
from fastapi.openapi.docs import get_swagger_ui_html
from fastapi.responses import JSONResponse, StreamingResponse, Response
from starlette.concurrency import iterate_in_threadpool

from common.misc_utils import set_log_level, get_logger
from summarize.db_operations import create_job_with_db
from summarize.settings import settings
from summarize.models import (
    SummarizationType,
    JobsListResponse,
    JobCreatedResponse,
    JobDetailResponse,
    JobResultResponse,
    PaginationInfo,
    DocumentInfo,
    JobState,
    JobStatus
)

set_log_level(settings.common.app.log_level)

from common.llm_utils import query_vllm_summarize, query_vllm_summarize_stream, tokenize_with_llm
from common.misc_utils import get_llm_endpoint, set_request_id, configure_uvicorn_logging, create_llm_session
from common.diagnostic_logger import setup_comprehensive_crash_handler

from common.error_utils import http_error_responses
from summarize.summ_utils import (
    SummarizeException,
    word_count,
    build_success_response,
    build_messages,
    trim_to_last_sentence,
    compute_target_and_max_tokens,
    SummarizeSuccessResponse,
    validate_summary_length,
    validate_summary_level,
    validate_input_and_get_available_tokens,
    extract_text_from_pdf,
    get_llm_max_model_len,
    MAX_INPUT_WORDS
)
from summarize.job_utils import (
    ensure_directories,
    validate_file_extension,
    stage_uploaded_file,
    read_result_file,
    delete_job_files,
    delete_all_job_files,
)
from summarize.db.connection import check_db_connection, close_db_connections
from common.misc_utils import cleanup_staging_directory

logger = get_logger("app")

diagnostic_logger, stderr_monitor, signal_handler = setup_comprehensive_crash_handler(logger)

concurrency_limiter = asyncio.BoundedSemaphore(settings.common.llm.max_batch_size)

@asynccontextmanager
async def lifespan(app):
    filtered_paths = ['/health']
    configure_uvicorn_logging(settings.common.app.log_level, filtered_paths)
    create_llm_session(pool_maxsize=settings.common.llm.max_batch_size)
    initialize_models()
    
    # Check database connection and initialize schema (required for operation)
    try:
        if check_db_connection():
            logger.info("✅ Database connection established")
            
            # Initialize database schema (create tables if they don't exist)
            try:
                from summarize.db.models import Base
                from summarize.db.connection import engine
                Base.metadata.create_all(bind=engine)
                logger.info("✅ Database schema initialized")
            except Exception as schema_error:
                logger.error(f"❌ Failed to initialize database schema: {schema_error}")
                raise RuntimeError(f"Database schema initialization failed: {schema_error}")
        else:
            logger.error("❌ Database connection failed - service requires database to operate")
            raise RuntimeError("Database connection required but not available. Please check database configuration.")
    except RuntimeError:
        raise
    except Exception as e:
        logger.error(f"❌ Database check failed: {e}")
        raise RuntimeError(f"Database connection required but failed: {e}")

    logger.info("Ensuring cache directories exist...")
    ensure_directories()

    # Scan for zombie jobs and mark them as failed
    logger.info("Running zombie job recovery scan...")
    from summarize.job_utils import recover_zombie_jobs
    recovered = recover_zombie_jobs()
    if recovered > 0:
        logger.warning(f"Recovered {recovered} zombie job(s) from previous session")
    
    yield

    # Shutdown
    logger.info("Application shutting down...")
    
    # Close database connections
    try:
        close_db_connections()
        logger.info("Database connections closed")
    except Exception as e:
        logger.error(f"Error closing database connections: {e}", exc_info=True)
    stderr_monitor.stop()

# OpenAPI tags metadata for endpoint organization
tags_metadata = [
    {
        "name": "summarization",
        "description": "Text and document summarization operations"
    },
    {
        "name": "jobs",
        "description": "Async summarization job management"
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
    llm_model_dict = get_llm_endpoint()

async def locked_stream(stream_g):
    """Wrap a vLLM SSE generator, releasing the concurrency semaphore when the stream ends."""
    try:
        async for chunk in iterate_in_threadpool(stream_g):
            yield chunk
    finally:
        concurrency_limiter.release()


async def handle_summarize(
    content_text: str,
    input_type: str,
    summary_length: Optional[int] = None,
    summary_level: Optional[str] = None,
    stream: bool = False,
):
    """Core summarization logic shared by both JSON and form-data paths."""
    input_word_count = word_count(content_text)
    
    # Get LLM endpoint for tokenization
    llm_endpoint = llm_model_dict['llm_endpoint']
    llm_model = llm_model_dict['llm_model']
    
    # Get actual token count from the input text
    input_tokens = await asyncio.to_thread(
        lambda: len(tokenize_with_llm(content_text, llm_endpoint))
    )
    
    # Validate that both parameters are not provided simultaneously
    if summary_level is not None and summary_length is not None:
        raise SummarizeException(
            400, "INVALID_PARAMETER",
            "Cannot specify both 'level' and 'length'. Please use only one."
        )
    
    # Unified validation and computation
    available_output_tokens = validate_input_and_get_available_tokens(
        input_tokens, input_word_count, summary_level, summary_length
    )
    
    target_words, min_words, max_words, max_tokens = compute_target_and_max_tokens(
        input_tokens, available_output_tokens, summary_level, summary_length
    )
    
    # Log appropriate message based on which parameter was provided
    if summary_level is not None:
        logger.info(
            f"Received {input_type} request with input size: {input_word_count} words ({input_tokens} tokens), "
            f"level: {summary_level}, target: {target_words} words ({min_words}-{max_words})"
        )
    elif summary_length is not None:
        logger.info(
            f"Received {input_type} request with input size: {input_word_count} words ({input_tokens} tokens), "
            f"target summary length: {summary_length} words"
        )
    else:
        logger.info(
            f"Received {input_type} request with input size: {input_word_count} words ({input_tokens} tokens), "
            f"automatic length"
        )

    messages = build_messages(content_text, target_words, min_words, max_words,
                            (summary_length is not None or summary_level is not None))

    if stream:
        await concurrency_limiter.acquire()
        try:
            vllm_stream = await asyncio.to_thread(
                query_vllm_summarize_stream,
                llm_endpoint=llm_endpoint,
                messages=messages,
                model=llm_model,
                max_tokens=max_tokens,
                temperature=settings.summarize.summarization_temperature,
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
        # Running the call in a async thread pool to avoid blocking the event loop
        result, in_tokens, out_tokens = await asyncio.to_thread(
            query_vllm_summarize,
            llm_endpoint=llm_endpoint,
            messages=messages,
            model=llm_model,
            max_tokens=max_tokens,
            temperature=settings.summarize.summarization_temperature,
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
responses={
    400: http_error_responses[400],
    413: http_error_responses[413],
    415: http_error_responses[415],
    429: http_error_responses[429],
    500: http_error_responses[500],
},
summary="Summarize text or file",
description=(
      "Accepts **either** `application/json` or `multipart/form-data` based on "
      "the `Content-Type` header.\n\n"
      "---\n\n"
      "### Option 1: JSON body (`Content-Type: application/json`)\n\n"
      "| Field | Type | Required | Description |\n"
      "|-------|------|----------|-------------|\n"
      "| `text` | string | Yes | Plain text content to summarize |\n"
      "| `level` | string | No | Abstraction level: 'brief', 'standard' (default), or 'detailed' |\n"
      "| `length` | integer | No | (Legacy) Desired summary length in words |\n"
      "| `stream` | boolean | No | Stream the summary as it is generated, default False |\n\n"
      "**Note:** Use either `level` (recommended) or `length` (legacy), not both.\n\n"
      "**Example with level:**\n"
      "```bash\n"
      'curl -X POST /v1/summarize -H "Content-Type: application/json" -d '
      '{\n'
      '  "text": "Artificial intelligence has made significant progress...",\n'
      '  "level": "brief"\n'
      '}\n'
      "```\n\n"
      "**Example with length (legacy):**\n"
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
      "| `level` | string | No | Abstraction level: 'brief', 'standard' (default), or 'detailed' |\n"
      "| `length` | integer | No | (Legacy) Desired summary length in words |\n"
      "| `stream` | boolean | No | Stream the summary as it is generated, default False |\n\n"
      "**Note:** Use either `level` (recommended) or `length` (legacy), not both.\n\n"
      "**Example with level:**\n"
      "```bash\n"
      'curl -X POST /v1/summarize -F "file=@report.pdf" -F "level=detailed"\n'
      "```\n\n"
      "**Example with length (legacy):**\n"
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
            
            # Support both level (new) and length (legacy)
            summary_level = validate_summary_level(body.get("level"))
            summary_length = validate_summary_length(body.get("length"))
            stream = bool(body.get("stream", False))

            return await handle_summarize(text, "text", summary_length, summary_level, stream)

        # ----- Multipart / form-data path -----
        elif "multipart/form-data" in content_type:
            form = await request.form()
            file: Optional[UploadFile] = form.get("file")  # type: ignore[assignment]

            # Support both level (new) and length (legacy)
            summary_level = validate_summary_level(form.get("level"))
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
            return await handle_summarize(content_text.strip(), "file", summary_length, summary_level, stream)

        else:
            raise SummarizeException(415, "UNSUPPORTED_CONTENT_TYPE",
                                     "Content-Type must be application/json or multipart/form-data")

    except SummarizeException as se:
        raise se
    except Exception as e:
        logger.error(f"Got exception while generating summary: {e}")

# Background task for async job processing
async def process_summarization_job(job_id: str, level):
    """
    Background task to process a summarization job.
    
    Implements both direct and chunked summarization strategies:
    - Direct: For documents within context window
    - Chunked: For large documents exceeding context window
    
    Args:
        job_id: UUID of the job to process
    """
    import json
    from datetime import datetime, timezone
    from summarize.db.manager import db_repo
    from summarize.models import JobStatus
    from summarize.chunk_utils import (
        split_text_into_chunks,
        build_merge_messages
    )
    
    logger.info(f"Background processing started for job {job_id}")
    
    start_time = time.time()
    staging_dir = settings.summarize.staging_dir / job_id
    result_path = settings.summarize.results_dir / f"{job_id}_result.json"
    
    try:
        # Step 1: Read staged file
        if not staging_dir.exists():
            raise Exception(f"Staging directory not found: {staging_dir}")
        
        staged_files = list(staging_dir.glob("*"))
        if not staged_files:
            raise Exception(f"No files found in staging directory: {staging_dir}")
        
        staged_file = staged_files[0]
        filename = staged_file.name
        ext = staged_file.suffix.lower()
        
        logger.info(f"Processing file: {filename} (type: {ext})")
        
        # Step 2: Extract text
        if ext == ".pdf":
            with open(staged_file, 'rb') as f:
                raw_content = f.read()
            content_text = await asyncio.to_thread(extract_text_from_pdf, raw_content)
        elif ext == ".txt":
            with open(staged_file, 'r', encoding='utf-8') as f:
                content_text = f.read()
        else:
            raise Exception(f"Unsupported file type: {ext}")
        
        if not content_text or not content_text.strip():
            raise Exception("Extracted text is empty")
        
        content_text = content_text.strip()
        
        # Step 3: Compute metrics
        input_word_count = word_count(content_text)
        logger.info(f"Document word count: {input_word_count}")
        
        # Get LLM endpoint for tokenization
        llm_endpoint = llm_model_dict['llm_endpoint']
        llm_model = llm_model_dict['llm_model']
        
        # Tokenize input
        input_tokens = await asyncio.to_thread(
            lambda: len(tokenize_with_llm(content_text, llm_endpoint))
        )
        logger.info(f"Document token count: {input_tokens}")
        
        # Update job with word count and set to in_progress
        db_repo.update_job(
            job_id,
            status=JobStatus.IN_PROGRESS,
            metadata={"document_word_count": input_word_count}
        )

        
        # Step 4: Determine strategy - check if input alone fits in context window
        max_model_len = get_llm_max_model_len()
        prompt_tokens = settings.summarize.summarization_prompt_token_count
        
        # Calculate available space for output
        available_output_tokens = max_model_len - input_tokens - prompt_tokens
        
        strategy = "direct"  # Initialize strategy variable
        num_chunks = 0  # Initialize for type checking
        
        # If input + prompt already exceeds context window, must use chunked strategy
        if available_output_tokens <= 0:
            logger.info(
                f"Input too large for direct strategy: input_tokens={input_tokens}, "
                f"prompt_tokens={prompt_tokens}, max_model_len={max_model_len}, "
                f"available_output={available_output_tokens} (NEGATIVE - using CHUNKED)"
            )
            strategy = "chunked"
            # Skip computing file-level tokens for chunked strategy - will be calculated later for merge step
            target_words, min_words, max_words, max_tokens = None, None, None, 0
        else:
            # Compute target/max tokens for direct strategy
            target_words, min_words, max_words, max_tokens = compute_target_and_max_tokens(
                input_tokens, available_output_tokens, level, None
            )
            
            total_required_tokens = input_tokens + prompt_tokens + max_tokens
            
            logger.info(
                f"Strategy decision: input_tokens={input_tokens}, "
                f"prompt_tokens={prompt_tokens}, "
                f"max_tokens={max_tokens}, total_required={total_required_tokens}, "
                f"max_model_len={max_model_len}"
            )
            
            if total_required_tokens <= max_model_len:
                # Direct summarization strategy
                logger.info(f"Using DIRECT strategy (fits in context window)")
                strategy = "direct"
            else:
                logger.info(f"Using CHUNKED strategy (exceeds context window)")
                strategy = "chunked"
                # Reset file-level tokens - will be calculated later for merge step
                target_words, min_words, max_words, max_tokens = None, None, None, 0
        
        if strategy == "direct":
            
            # Build messages
            messages = build_messages(content_text, target_words, min_words, max_words, True)
            
            # Call LLM with semaphore
            async with concurrency_limiter:
                result, in_tokens, out_tokens = await asyncio.to_thread(
                    query_vllm_summarize,
                    llm_endpoint=llm_endpoint,
                    messages=messages,
                    model=llm_model,
                    max_tokens=max_tokens,
                    temperature=settings.summarize.summarization_temperature,
                )
            
            if isinstance(result, dict) and "error" in result:
                raise Exception(f"LLM error: {result.get('error', 'Unknown error')}")
            
            summary = trim_to_last_sentence(result) if isinstance(result, str) else ""
            
            # Token usage
            total_input_tokens = in_tokens
            total_output_tokens = out_tokens
            
        else:
            # Chunked summarization strategy
            logger.info(f"Using CHUNKED strategy (exceeds context window)")
            strategy = "chunked"
            
            # Split into chunks
            chunks = await asyncio.to_thread(
                split_text_into_chunks,
                content_text,
                MAX_INPUT_WORDS,
                settings.summarize.chunk_overlap_sentences
            )
            
            num_chunks = len(chunks)
            logger.info(f"Split into {num_chunks} chunks")
            
            # Calculate token values for each chunk individually
            chunk_token_info = []
            total_estimated_summary_tokens = 0
            
            for i, chunk in enumerate(chunks):
                chunk_tokens = await asyncio.to_thread(
                    tokenize_with_llm,
                    chunk,
                    llm_endpoint
                )
                chunk_input_tokens = len(chunk_tokens)
                chunk_available_output_tokens = (
                    get_llm_max_model_len()
                    - chunk_input_tokens
                    - settings.summarize.summarization_prompt_token_count
                )
                
                # Compute chunk-level target and max tokens
                chunk_target_words, chunk_min_words, chunk_max_words, chunk_max_tokens = compute_target_and_max_tokens(
                    chunk_input_tokens,
                    chunk_available_output_tokens,
                    level,
                    None
                )
                
                chunk_token_info.append({
                    'input_tokens': chunk_input_tokens,
                    'available_output_tokens': chunk_available_output_tokens,
                    'target_words': chunk_target_words,
                    'min_words': chunk_min_words,
                    'max_words': chunk_max_words,
                    'max_tokens': chunk_max_tokens
                })
                
                total_estimated_summary_tokens += chunk_max_tokens
                
                logger.debug(
                    f"Chunk {i+1}/{num_chunks} tokens: input={chunk_input_tokens}, "
                    f"available_output={chunk_available_output_tokens}, "
                    f"max_tokens={chunk_max_tokens}"
                )
            
            logger.debug(
                f"Total estimated summary tokens from all chunks: {total_estimated_summary_tokens}"
            )
            logger.debug(f"Chunk token info: {chunk_token_info}")
            # Pre-check: Estimate if combined summaries will fit in merge step
            # Calculate merge max_tokens based on estimated combined summary size
            merge_input_estimate = total_estimated_summary_tokens
            merge_available_estimate = (
                get_llm_max_model_len()
                - merge_input_estimate
                - settings.summarize.summarization_prompt_token_count
            )
            
            # Use compute_target_and_max_tokens to calculate merge max_tokens consistently
            _, _, _, merge_max_tokens_estimate = compute_target_and_max_tokens(
                merge_input_estimate,
                merge_available_estimate,
                level,
                None
            )
            
            merge_required_tokens = merge_input_estimate + settings.summarize.summarization_prompt_token_count + merge_max_tokens_estimate
            
            logger.debug(
                f"Merge pre-check: estimated_input={merge_input_estimate}, "
                f"estimated_max_tokens={merge_max_tokens_estimate}, "
                f"total_required={merge_required_tokens}, "
                f"context_window={get_llm_max_model_len()}"
            )
            
            if merge_required_tokens > get_llm_max_model_len():
                raise Exception(
                    f"FILE_SIZE_OVER_LIMIT: Document too large. "
                    f"Estimated {num_chunks} chunk summaries would require {merge_required_tokens} tokens, "
                    f"exceeding context window of {get_llm_max_model_len()} tokens."
                )
            
            # Update metadata with chunking info
            db_repo.update_job(
                job_id,
                metadata={
                    "total_chunks": num_chunks,
                    "completed_chunks": 0,
                    "phase": "summarizing"
                }
            )
            
            # Process chunks in parallel with semaphore
            chunk_semaphore = asyncio.BoundedSemaphore(settings.summarize.chunk_parallelism)
            metadata_lock = asyncio.Lock()
            
            chunk_summaries = []
            total_input_tokens = 0
            total_output_tokens = 0
            
            async def process_chunk(chunk_text: str, chunk_index: int):
                nonlocal total_input_tokens, total_output_tokens
                
                # Get token info for this specific chunk
                chunk_info = chunk_token_info[chunk_index]
                
                async with chunk_semaphore:  # Per-job parallelism limit
                    # Build messages for this chunk using its specific token values
                    chunk_messages = build_messages(
                        chunk_text,
                        chunk_info['target_words'],
                        chunk_info['min_words'],
                        chunk_info['max_words'],
                        True
                    )
                    
                    async with concurrency_limiter:  # Global vLLM limit
                        chunk_result, chunk_in_tokens, chunk_out_tokens = await asyncio.to_thread(
                            query_vllm_summarize,
                            llm_endpoint=llm_endpoint,
                            messages=chunk_messages,
                            model=llm_model,
                            max_tokens=chunk_info['max_tokens'],
                            temperature=settings.summarize.summarization_temperature,
                        )
                    
                    if isinstance(chunk_result, dict) and "error" in chunk_result:
                        raise Exception(f"LLM error on chunk {chunk_index}: {chunk_result.get('error')}")
                    
                    chunk_summary = trim_to_last_sentence(chunk_result) if isinstance(chunk_result, str) else ""
                    
                    # Update progress with lock
                    async with metadata_lock:
                        total_input_tokens += chunk_in_tokens
                        total_output_tokens += chunk_out_tokens
                        
                        # Update database progress
                        job_record = db_repo.get_job_by_id(job_id)
                        if job_record:
                            current_metadata = job_record.job_metadata or {}
                            current_metadata["completed_chunks"] = current_metadata.get("completed_chunks", 0) + 1
                            db_repo.update_job(job_id, metadata=current_metadata)
                    
                    logger.debug(f"Completed chunk {chunk_index + 1}/{num_chunks}")
                    logger.debug(f"Chunk summary: {chunk_summary}")
                    return chunk_summary
            
            # Process all chunks in parallel
            tasks = [process_chunk(chunk, i) for i, chunk in enumerate(chunks)]
            chunk_summaries = await asyncio.gather(*tasks)
            
            # Merge step
            logger.info(f"Merging {len(chunk_summaries)} chunk summaries")
            db_repo.update_job(
                job_id,
                metadata={
                    "total_chunks": num_chunks,
                    "completed_chunks": num_chunks,
                    "phase": "merging"
                }
            )
            
            # Concatenate chunk summaries
            merged_text = "\n\n".join(chunk_summaries)
            
            # Calculate merge-specific token values based on the concatenated chunk summaries
            merge_tokens = await asyncio.to_thread(
                tokenize_with_llm,
                merged_text,
                llm_endpoint
            )
            merge_input_tokens = len(merge_tokens)
            merge_available_output_tokens = (
                get_llm_max_model_len()
                - merge_input_tokens
                - settings.summarize.summarization_prompt_token_count
            )
            
            # Compute merge-specific target and max tokens
            merge_target_words, merge_min_words, merge_max_words, merge_max_tokens = compute_target_and_max_tokens(
                merge_input_tokens,
                merge_available_output_tokens,
                level,
                None
            )
            
            logger.debug(
                f"Merge-level tokens: input={merge_input_tokens}, "
                f"available_output={merge_available_output_tokens}, "
                f"max_tokens={merge_max_tokens},"
                f"target_words={merge_target_words}, "
                f"min_words={merge_min_words}, "
                f"max_words={merge_max_words}"
            )
            
            # Build merge messages
            merge_messages = build_merge_messages(merged_text, merge_target_words, merge_min_words, merge_max_words)
            logger.info(f"Merge messages: {merge_messages}")
            # Final merge call
            async with concurrency_limiter:
                merge_result, merge_in_tokens, merge_out_tokens = await asyncio.to_thread(
                    query_vllm_summarize,
                    llm_endpoint=llm_endpoint,
                    messages=merge_messages,
                    model=llm_model,
                    max_tokens=merge_max_tokens,
                    temperature=settings.summarize.summarization_temperature,
                )
            
            if isinstance(merge_result, dict) and "error" in merge_result:
                raise Exception(f"LLM error during merge: {merge_result.get('error')}")
            
            summary = trim_to_last_sentence(merge_result) if isinstance(merge_result, str) else ""
            
            # Add merge tokens to totals
            total_input_tokens += merge_in_tokens
            total_output_tokens += merge_out_tokens
        
        # Step 5: Write result file
        elapsed_ms = int((time.time() - start_time) * 1000)
        
        result_data = {
            "data": {
                "summary": summary,
                "original_length": input_word_count,
                "summary_length": word_count(summary),
            },
            "meta": {
                "model": llm_model,
                "processing_time_ms": elapsed_ms,
                "input_type": "file",
                "strategy": strategy,
            },
            "usage": {
                "input_tokens": total_input_tokens,
                "output_tokens": total_output_tokens,
                "total_tokens": total_input_tokens + total_output_tokens,
            },
        }
        
        if strategy == "chunked":
            result_data["meta"]["chunks_processed"] = num_chunks
        
        # Write result to disk
        with open(result_path, 'w', encoding='utf-8') as f:
            json.dump(result_data, f, indent=2)
        
        logger.info(f"Result written to {result_path}")
        
        # Step 6: Update job status to completed
        db_repo.update_job(
            job_id,
            status=JobStatus.COMPLETED,
            completed_at=datetime.now(timezone.utc)
        )
        
        logger.info(f"Job {job_id} completed successfully ({strategy} strategy)")
        
    except Exception as e:
        logger.error(f"Job {job_id} failed: {e}", exc_info=True)
        
        # Update job status to failed
        db_repo.update_job(
            job_id,
            status=JobStatus.FAILED,
            error=str(e),
            completed_at=datetime.now(timezone.utc)
        )
    
    finally:
        # Step 7: Clean up staging directory
        try:
            cleanup_staging_directory(job_id, settings.summarize.staging_dir)
            logger.debug(f"Cleaned up staging for job {job_id}")
        except Exception as cleanup_error:
            logger.warning(f"Failed to cleanup staging for job {job_id}: {cleanup_error}")


@app.post(
    "/v1/summarize/jobs",
    status_code=202,
    response_model=JobCreatedResponse,
    responses={
        202: {"description": "Job created successfully"},
        400: http_error_responses[400],
        415: http_error_responses[415],
        500: http_error_responses[500],
    },
    summary="Create async summarization job",
    description=(
        "Submit a file (.txt or .pdf) for asynchronous summarization. "
        "Returns immediately with a job_id that can be used to track progress and retrieve results.\n\n"
        "**Form parameters:**\n"
        "- `file` (required): A single .txt or .pdf file to summarize\n"
        "- `level` (optional): Abstraction level - 'brief', 'standard' (default), or 'detailed'\n"
        "- `job_name` (optional): Human-readable label for the job\n\n"
        "**Note:** Unlike the synchronous endpoint, there is no file size limit. "
        "Large documents will be processed using chunked summarization."
    ),
    response_description="Job created with job_id",
    tags=["jobs"],
)
async def create_summarization_job(
    background_tasks: BackgroundTasks,
    file: UploadFile = File(...),
    level: Optional[str] = Form(None),
    job_name: Optional[str] = Form(None),
):
    """
    Create an async summarization job.
    
    Validates the file, stages it, creates a database record, and launches background processing.
    """
    try:
        # Check semaphore availability before processing
        if concurrency_limiter.locked():
            raise SummarizeException(
                429,
                "RATE_LIMIT_EXCEEDED",
                "Server is at capacity processing summarization jobs. Please try again later."
            )
        
        # Validate file extension
        filename = file.filename or ""
        is_valid, ext = validate_file_extension(filename)
        
        if not is_valid:
            raise SummarizeException(
                415,
                "UNSUPPORTED_FILE_TYPE",
                f"Only .txt and .pdf files are allowed. Received: {ext or 'unknown'}"
            )
        
        # Validate level parameter
        if level is not None:
            level = validate_summary_level(level)
        else:
            level = 'standard'  # Default level
        
        # Generate job ID
        job_id = str(uuid.uuid4())
        
        logger.info(f"Creating summarization job {job_id} for file: {filename}")
        
        # Stage the uploaded file
        try:
            staged_path = stage_uploaded_file(job_id, file)
            logger.debug(f"File staged at: {staged_path}")
        except IOError as e:
            logger.error(f"Failed to stage file for job {job_id}: {e}")
            raise SummarizeException(
                500,
                "FILE_STAGING_ERROR",
                "Failed to save uploaded file"
            )
        
        # Create job record in database
        try:
            create_job_with_db(job_id,
                             SummarizationType.DIRECT.value,
                             0,
                             level,
                             job_name,
                             filename)

            logger.info(f"Job record created: {job_id}")
        except Exception as e:
            logger.error(f"Failed to create job record for {job_id}: {e}")
            # Clean up staged file
            cleanup_staging_directory(job_id, settings.summarize.staging_dir)
            raise SummarizeException(
                500,
                "DATABASE_ERROR",
                "Failed to create job record"
            )
        
        # Launch background processing (stub for now)
        background_tasks.add_task(process_summarization_job, job_id, level)
        
        # Return 202 Accepted with job_id using JobCreatedResponse
        return JobCreatedResponse(job_id=job_id)
        
    except SummarizeException as se:
        raise se
    except Exception as e:
        logger.error(f"Unexpected error creating summarization job: {e}", exc_info=True)
        raise SummarizeException(
            500,
            "INTERNAL_SERVER_ERROR",
            f"Failed to create summarization job: {str(e)}"
        )


@app.get(
    "/v1/summarize/jobs",
    response_model=JobsListResponse,
    responses={
        200: {"description": "Paginated job list"},
        400: http_error_responses[400],
        500: http_error_responses[500],
    },
    summary="List all summarization jobs",
    description=(
        "Retrieve all summarization jobs with optional filtering and pagination.\n\n"
        "**Query parameters:**\n"
        "- `latest` (bool): Return only the most recent job. Default: false\n"
        "- `limit` (int): Records per page (1-100). Default: 20\n"
        "- `offset` (int): Records to skip. Default: 0\n"
        "- `status` (string): Filter by status: 'accepted', 'in_progress', 'completed', 'failed'\n"
    ),
    response_description="Paginated list of jobs with metadata",
    tags=["jobs"],
)
async def list_jobs(
    latest: Optional[bool] = None,
    limit: int = 20,
    offset: int = 0,
    status: Optional[str] = None,
):
    """List all summarization jobs with pagination and filters."""
    try:
        # Validate limit parameter
        if limit < 1 or limit > 100:
            raise SummarizeException(
                400,
                "INVALID_PARAMETER",
                "Limit must be between 1 and 100"
            )
        
        # Validate offset parameter
        if offset < 0:
            raise SummarizeException(
                400,
                "INVALID_PARAMETER",
                "Offset must be non-negative"
            )
        
        # Validate and parse status parameter
        status_filter = None
        if status:
            try:
                status_filter = JobStatus(status.lower())
            except ValueError:
                raise SummarizeException(
                    400,
                    "INVALID_PARAMETER",
                    f"Invalid status value. Must be one of: accepted, in_progress, completed, failed"
                )
        
        # Handle latest flag
        if latest:
            limit = 1
            offset = 0
        
        # Get jobs from database
        from summarize.db.manager import db_repo
        jobs, total = db_repo.get_all_jobs(
            status=status_filter,
            limit=limit,
            offset=offset
        )
        
        # Format response using JobsListResponse with JobState objects
        job_list = []
        for job in jobs:
            # Convert status string to JobStatus enum if needed
            job_status = job.status if isinstance(job.status, JobStatus) else JobStatus(job.status)
            
            # Create JobState object
            job_state = JobState(
                job_id=job.job_id,
                job_name=job.job_name,
                status=job_status,
                submitted_at=job.submitted_at.isoformat() if job.submitted_at else "",
                completed_at=job.completed_at.isoformat() if job.completed_at else None,
                updated_at=job.updated_at.isoformat() if hasattr(job, 'updated_at') and job.updated_at else None,
                document_name=job.document_name if hasattr(job, 'document_name') else None,
                document_word_count=job.document_word_count if hasattr(job, 'document_word_count') else 0,
                level=job.level if hasattr(job, 'level') else None,
                job_type=job.job_type if hasattr(job, 'job_type') else None,
                error=job.error if hasattr(job, 'error') else None
            )
            job_list.append(job_state)
        
        return JobsListResponse(
            pagination=PaginationInfo(total=total, limit=limit, offset=offset),
            data=job_list
        )
        
    except SummarizeException as se:
        raise se
    except Exception as e:
        logger.error(f"Error listing jobs: {e}", exc_info=True)
        raise SummarizeException(
            500,
            "INTERNAL_SERVER_ERROR",
            "Failed to retrieve jobs"
        )


@app.get(
    "/v1/summarize/jobs/{job_id}",
    response_model=JobDetailResponse,
    responses={
        200: {"description": "Job details"},
        404: http_error_responses[404],
        500: http_error_responses[500],
    },
    summary="Get job details",
    description="Retrieve detailed status and metadata of a specific summarization job.",
    response_description="Full job details including document info and metadata",
    tags=["jobs"],
)
async def get_job_details(job_id: str):
    """Get detailed information about a specific job."""
    try:
        # Get job from database
        from summarize.db.manager import db_repo
        job = db_repo.get_job_by_id(job_id)
        
        if not job:
            raise SummarizeException(
                404,
                "RESOURCE_NOT_FOUND",
                f"Job {job_id} not found"
            )
        
        # Format response using JobDetailResponse
        document = DocumentInfo(
            name=job.document_name or "",
            status=job.status
        )
        
        # Convert status string to JobStatus enum if needed
        job_status = job.status if isinstance(job.status, JobStatus) else JobStatus(job.status)
        
        return JobDetailResponse(
            job_id=job.job_id,
            job_name=job.job_name,
            status=job_status,
            submitted_at=job.submitted_at.isoformat() if job.submitted_at else "",
            completed_at=job.completed_at.isoformat() if job.completed_at else None,
            document=document,
            error=job.error,
            metadata=job.job_metadata if job.job_metadata else None
        )
        
    except SummarizeException as se:
        raise se
    except Exception as e:
        logger.error(f"Error retrieving job {job_id}: {e}", exc_info=True)
        raise SummarizeException(
            500,
            "INTERNAL_SERVER_ERROR",
            f"Failed to retrieve job details"
        )


@app.get(
    "/v1/summarize/jobs/{job_id}/result",
    response_model=JobResultResponse,
    responses={
        200: {"description": "Summary result"},
        202: {"description": "Job still in progress"},
        404: http_error_responses[404],
        500: http_error_responses[500],
    },
    summary="Get summarization result",
    description=(
        "Retrieve the completed summary and result metadata for a job.\n\n"
        "Returns 202 Accepted if the job is still in progress.\n"
        "Returns 404 if the job doesn't exist or result is not available."
    ),
    response_description="Summarization result with usage statistics",
    tags=["jobs"],
)
async def get_job_result(job_id: str):
    """Get the summarization result for a completed job."""
    try:
        # Get job from database
        from summarize.db.manager import db_repo
        job = db_repo.get_job_by_id(job_id)
        
        if not job:
            raise SummarizeException(
                404,
                "RESOURCE_NOT_FOUND",
                f"Job {job_id} not found"
            )
        
        # Check job status
        if job.status in ['accepted', 'in_progress']:
            return JSONResponse(
                status_code=202,
                content={
                    "message": "Job is still in progress",
                    "job_id": job_id,
                    "status": job.status
                }
            )
        
        if job.status == 'failed':
            raise SummarizeException(
                404,
                "RESOURCE_NOT_FOUND",
                f"Job failed: {job.error or 'Unknown error'}"
            )
        
        # Read result file
        result_data = read_result_file(job_id)
        
        if not result_data:
            logger.error(f"Result file missing for completed job {job_id}")
            raise SummarizeException(
                500,
                "INTERNAL_SERVER_ERROR",
                "Result file not found"
            )
        
        # Use JobResultResponse to structure the response
        return JobResultResponse(
            data=result_data.get("data", {}),
            meta=result_data.get("meta", {}),
            usage=result_data.get("usage", {})
        )
        
    except SummarizeException as se:
        raise se
    except Exception as e:
        logger.error(f"Error retrieving result for job {job_id}: {e}", exc_info=True)
        raise SummarizeException(
            500,
            "INTERNAL_SERVER_ERROR",
            "Failed to retrieve job result"
        )


@app.delete(
    "/v1/summarize/jobs/{job_id}",
    status_code=204,
    responses={
        204: {"description": "Job and associated data deleted"},
        404: http_error_responses[404],
        409: {"description": "Job is still active (accepted or in_progress)"},
        500: http_error_responses[500],
    },
    summary="Delete a job",
    description=(
        "Delete a specific job record and its associated result file.\n\n"
        "Only completed or failed jobs can be deleted. "
        "Attempting to delete an active job (accepted or in_progress) returns 409 Conflict."
    ),
    response_description="Job deleted successfully",
    tags=["jobs"],
)
async def delete_job(job_id: str):
    """Delete a specific job and its associated files."""
    try:
        # Get job from database
        from summarize.db.manager import db_repo
        job = db_repo.get_job_by_id(job_id)
        
        if not job:
            raise SummarizeException(
                404,
                "RESOURCE_NOT_FOUND",
                f"Job {job_id} not found"
            )
        
        # Check if job is active
        if job.status in ['accepted', 'in_progress']:
            raise SummarizeException(
                409,
                "RESOURCE_LOCKED",
                f"Cannot delete active job. Current status: {job.status}"
            )
        
        # Delete files (result and staging)
        delete_job_files(job_id)
        
        # Delete from database
        success = db_repo.delete_job(job_id)
        
        if not success:
            raise SummarizeException(
                500,
                "INTERNAL_SERVER_ERROR",
                "Failed to delete job from database"
            )
        
        logger.info(f"Deleted job {job_id}")
        return Response(status_code=204)
        
    except SummarizeException as se:
        raise se
    except Exception as e:
        logger.error(f"Error deleting job {job_id}: {e}", exc_info=True)
        raise SummarizeException(
            500,
            "INTERNAL_SERVER_ERROR",
            "Failed to delete job"
        )


@app.delete(
    "/v1/summarize/jobs",
    status_code=204,
    responses={
        204: {"description": "All jobs and data deleted"},
        400: http_error_responses[400],
        409: {"description": "Active jobs exist"},
        500: http_error_responses[500],
    },
    summary="Bulk delete all jobs",
    description=(
        "Delete all job records, result files, and staging data.\n\n"
        "**Query parameters:**\n"
        "- `confirm` (bool, required): Must be 'true' to proceed with bulk deletion\n\n"
        "Returns 409 Conflict if any active jobs (accepted or in_progress) exist."
    ),
    response_description="All jobs deleted successfully",
    tags=["jobs"],
)
async def bulk_delete_jobs(confirm: Optional[bool] = None):
    """Delete all jobs and associated files."""
    try:
        # Validate confirm parameter
        if confirm is not True:
            raise SummarizeException(
                400,
                "INVALID_REQUEST",
                "Bulk delete requires confirm=true query parameter"
            )
        
        # Check for active jobs
        from summarize.db.manager import db_repo
        active_jobs = db_repo.get_active_jobs()
        
        if active_jobs:
            raise SummarizeException(
                409,
                "RESOURCE_LOCKED",
                f"Cannot delete: {len(active_jobs)} active job(s) exist"
            )
        
        # Delete all files
        delete_all_job_files()
        
        # Delete all jobs from database
        success = db_repo.delete_all_jobs()
        
        if not success:
            raise SummarizeException(
                500,
                "INTERNAL_SERVER_ERROR",
                "Failed to delete jobs from database"
            )
        
        logger.info("Bulk deleted all jobs")
        return Response(status_code=204)
        
    except SummarizeException as se:
        raise se
    except Exception as e:
        logger.error(f"Error in bulk delete: {e}", exc_info=True)
        raise SummarizeException(
            500,
            "INTERNAL_SERVER_ERROR",
            "Failed to delete all jobs"
        )


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
