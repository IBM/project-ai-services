import os
import logging
import asyncio
import uuid
from typing import Optional
from fastapi import FastAPI, Request, HTTPException, Header, Depends
from fastapi.security import HTTPBearer, HTTPAuthorizationCredentials
from fastapi.openapi.docs import get_swagger_ui_html
from fastapi.responses import StreamingResponse
import json
from contextlib import asynccontextmanager
from asyncio import BoundedSemaphore
from functools import wraps
import uvicorn
from starlette.concurrency import iterate_in_threadpool
from lingua import Language

from common.misc_utils import set_log_level
from chatbot.settings import settings

set_log_level(settings.common.app.log_level)

from common.diagnostic_logger import setup_comprehensive_crash_handler
import common.db_utils as db
from common.lang_utils import setup_language_detector, detect_language, lang_de, max_tokens_map
from common.misc_utils import get_model_endpoints, set_request_id, create_llm_session, configure_uvicorn_logging
from common.llm_utils import query_vllm_stream, query_vllm_non_stream, query_vllm_models
from common.perf_utils import perf_registry
from common.error_utils import APIError, ErrorCode, http_error_responses, http_exception_handler
from chatbot.backend_utils import search_only, validate_query_length
from chatbot.response_utils import (
    ChatCompletionRequest,
    ChatCompletionResponse,
    ChatChoice,
    ChatMessage,
    DBStatusResponse,
    HealthResponse,
    ModelsResponse,
    PerfMetricsResponse,
)

vectorstore = None
vectorstore_lock = asyncio.Lock()

# Globals to be set dynamically
emb_model_dict = {}
llm_model_dict = {}
reranker_model_dict = {}

# Cache for auth requirement check
auth_required_cache = {"checked": False, "required": False}
auth_cache_lock = asyncio.Lock()

concurrency_limiter = BoundedSemaphore(settings.chatbot.max_concurrent_requests)

def initialize_models():
    global emb_model_dict, llm_model_dict, reranker_model_dict
    emb_model_dict, llm_model_dict, reranker_model_dict = get_model_endpoints()

def initialize_vectorstore():
    global vectorstore
    vectorstore = db.get_vector_store()

async def ensure_vectorstore_initialized():
    """Lazy initialization of vectorstore on first request.

    Note: This lazy initialization approach is used to facilitate OpenShift deployments,
    allowing the application to start successfully even when the vector store is not
    immediately available.
    """
    global vectorstore
    if vectorstore is None:
        async with vectorstore_lock:
            # Double-check pattern to avoid race conditions
            if vectorstore is None:
                logging.info("Initializing vectorstore on first request...")
                initialize_vectorstore()
                logging.info("Vectorstore initialized successfully")

diagnostic_logger, stderr_monitor, signal_handler = setup_comprehensive_crash_handler(logging.getLogger("chatbot"))

@asynccontextmanager
async def lifespan(app):
    filtered_paths = ['/health']
    configure_uvicorn_logging(settings.common.app.log_level, filtered_paths)
    initialize_models()
    setup_language_detector([Language.ENGLISH, Language.GERMAN])
    create_llm_session(pool_maxsize=settings.common.llm.llm_max_batch_size)
    yield
    stderr_monitor.stop()

# OpenAPI tags metadata for endpoint organization
tags_metadata = [
    {
        "name": "retrieval",
        "description": "Document retrieval and search operations using semantic search"
    },
    {
        "name": "chat",
        "description": "Chat completion with RAG (Retrieval-Augmented Generation)"
    },
    {
        "name": "models",
        "description": "LLM model information and management"
    },
    {
        "name": "monitoring",
        "description": "Performance metrics, health checks, and database status"
    }
]

app = FastAPI(
    lifespan=lifespan,
    title="AI-Services Chatbot API",
    description="RAG-based chatbot API with document retrieval, reranking, and LLM-powered responses.",
    version="1.0.0",
    openapi_tags=tags_metadata
)

# Simple Bearer token security scheme for Swagger UI
security = HTTPBearer(auto_error=False, description="Enter your vLLM API key")
app.add_exception_handler(HTTPException, http_exception_handler)

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
        title="AI-Services Chatbot API - Swagger UI",
    )

def limit_concurrency(f):
    @wraps(f)
    async def wrapper(*args, **kwargs):
        if concurrency_limiter.locked():
            APIError.raise_error(ErrorCode.SERVER_BUSY, "Try again shortly.")
        await concurrency_limiter.acquire()
        try:
            return await f(*args, **kwargs)
        finally:
            concurrency_limiter.release()
    return wrapper

async def is_auth_required() -> bool:
    """
    Check if vLLM authentication is required and cache the result.
    Returns True if auth is required, False otherwise.
    """
    global auth_required_cache
    
    # Check cache first
    if auth_required_cache["checked"]:
        return auth_required_cache["required"]
    
    async with auth_cache_lock:
        # Double-check after acquiring lock
        if auth_required_cache["checked"]:
            return auth_required_cache["required"]
        
        try:
            llm_endpoint = llm_model_dict['llm_endpoint']
            # Try to access without API key
            await asyncio.to_thread(query_vllm_models, llm_endpoint, None)
            # If successful, auth is not required
            auth_required_cache["checked"] = True
            auth_required_cache["required"] = False
            logging.debug("vLLM authentication is NOT required")
            return False
        except Exception as e:
            # Check if it's an authentication error
            error_str = str(e).lower()
            if "401" in error_str or "unauthorized" in error_str or "forbidden" in error_str:
                # Auth is required
                auth_required_cache["checked"] = True
                auth_required_cache["required"] = True
                logging.debug("vLLM authentication IS required")
                return True
            # For other errors, allow subsequent calls
            logging.debug(f"Error checking auth requirement: {e}, assuming auth is required")
            auth_required_cache["checked"] = True
            auth_required_cache["required"] = True
            return False


@app.get(
    "/v1/models",
    response_model=ModelsResponse,
    responses={401: http_error_responses[401], 500: http_error_responses[500]},
    tags=["models"],
    summary="List LLM models",
    description="List available models from the configured vLLM endpoint. **Requires API key in Authorization header** (Bearer token) if vLLM authentication is enabled."
)
async def list_models(credentials: Optional[HTTPAuthorizationCredentials] = Depends(security)):
    """List available LLM models. Requires Authorization header with Bearer token if authentication is enabled."""
    logging.debug("List models..")
    
    # Extract API key from credentials
    api_key = credentials.credentials if credentials else None
    
    # Check if auth is required and enforce it
    if await is_auth_required():
        if not api_key:
            APIError.raise_error(ErrorCode.AUTHENTICATION_FAILED, "API key is required when vLLM authentication is enabled")
    
    try:
        llm_endpoint = llm_model_dict['llm_endpoint']
        return await asyncio.to_thread(query_vllm_models, llm_endpoint, api_key)
    except Exception as e:
        # Check if it's an authentication error
        error_str = str(e).lower()
        if "401" in error_str or "unauthorized" in error_str or "forbidden" in error_str:
            APIError.raise_error(ErrorCode.AUTHENTICATION_FAILED, "Invalid or missing API key")
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, repr(e))


@app.get(
    "/v1/perf_metrics",
    response_model=PerfMetricsResponse,
    responses={404: http_error_responses[404]},
    tags=["monitoring"],
    summary="Get performance metrics",
    description="Return collected performance metrics for recent chat completion and retrieval calls. If request ID is provided, returns only that metric"
)
def get_perf_metrics(request_id: Optional[str] = None) -> PerfMetricsResponse:
    """
    Retrieve performance metrics for API requests.

    Query Parameters:
        request_id: Optional request ID to filter metrics. If provided, returns only that metric.
                   If omitted, returns all recent metrics (up to 1000 most recent).

    Returns:
        PerfMetricsResponse containing a list of performance metrics.

    Raises:
        HTTPException: 404 if request_id is specified but not found.
    """
    if request_id:
        metric = perf_registry.get_metric_by_request_id(request_id)
        if metric is None:
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"No metric found for request_id: {request_id}")
        return PerfMetricsResponse(metrics=[metric])
    metrics = perf_registry.get_metrics()
    return PerfMetricsResponse(metrics=metrics)

async def locked_stream(stream_g, perf_stat_dict):
    try:
        async for chunk in iterate_in_threadpool(stream_g):
            yield chunk
    finally:
        perf_registry.add_metric(perf_stat_dict)
        concurrency_limiter.release()


@app.post(
    "/v1/chat/completions",
    response_model=ChatCompletionResponse,
    tags=["chat"],
    summary="Chat with RAG",
    description="Generate chat completions grounded in retrieved documents. Returns streaming response if stream=true, otherwise returns structured JSON. **Requires API key in Authorization header** (Bearer token) if vLLM authentication is enabled.",
    responses={
        200: {
            "description": "Successful Response",
            "content": {
                "application/json": {
                    "example": {
                        "choices": [
                            {
                                "message": {
                                    "content": "Based on the retrieved documents, artificial intelligence..."
                                }
                            }
                        ]
                    }
                },
                "text/event-stream": {
                    "schema": {
                        "type": "string",
                        "description": "Server-Sent Events stream. Each event is formatted as: data: {JSON}\\n\\n"
                    },
                    "example": 'data: {"choices":[{"delta":{"content":"Based on"}}]}\n\ndata: {"choices":[{"delta":{"content":" the retrieved"}}]}\n\ndata: {"choices":[{"delta":{"content":" documents..."}}]}\n\n'
                }
            }
        },
        400: http_error_responses[400],
        401: http_error_responses[401],
        429: http_error_responses[429],
        500: http_error_responses[500],
        503: http_error_responses[503]
    }
)
async def chat_completion(req: ChatCompletionRequest, credentials: Optional[HTTPAuthorizationCredentials] = Depends(security)) -> ChatCompletionResponse | StreamingResponse:
    # Extract API key from credentials
    api_key = credentials.credentials if credentials else None
    
    # Check if auth is required and enforce it
    if await is_auth_required():
        if not api_key:
            message = "API key is required when vLLM authentication is enabled"
            if req.stream:
                async def stream_auth_error():
                    yield f"data: {json.dumps({'choices': [{'delta': {'content': message}}]})}\n\n"
                return StreamingResponse(stream_auth_error(), media_type="text/event-stream", status_code=401)
            APIError.raise_error(ErrorCode.AUTHENTICATION_FAILED, message)
    
    if not req.messages:
        APIError.raise_error(ErrorCode.EMPTY_INPUT, "messages can't be empty")

    query = req.messages[0].content

    # Validate query is not empty
    if not query or not query.strip():
        APIError.raise_error(ErrorCode.EMPTY_INPUT, "Query cannot be empty")

    # Ensure vectorstore is initialized on first request
    if vectorstore is None:
        await ensure_vectorstore_initialized()

    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        llm_model = llm_model_dict['llm_model']
        llm_endpoint = llm_model_dict['llm_endpoint']
        reranker_model = reranker_model_dict['reranker_model']
        reranker_endpoint = reranker_model_dict['reranker_endpoint']


        # Validate query length
        is_valid, error_msg = await asyncio.to_thread(
            validate_query_length, query, emb_endpoint
        )
        if not is_valid:
            # Return streaming error response for consistency
            if req.stream:
                async def stream_query_length_error():
                    message = "Your query is too long. Please shorten it and try again."
                    yield f"data: {json.dumps({'choices': [{'delta': {'content': message}}]})}\n\n"
                return StreamingResponse(stream_query_length_error(), media_type="text/event-stream")
            APIError.raise_error(ErrorCode.INVALID_PARAMETER, error_msg)

        lang = detect_language(query)

        max_tokens = req.max_tokens
        # giving priority to max_tokens passed in the request, otherwise according to detected language of query
        if not max_tokens:
            max_tokens = max_tokens_map.get(lang, settings.common.llm.llm_max_tokens)

        docs, perf_stat_dict = await asyncio.to_thread(
            search_only,
            query,
            emb_model, emb_endpoint, emb_max_tokens,
            reranker_model,
            reranker_endpoint,
            settings.chatbot.num_chunks_post_search,
            settings.chatbot.num_chunks_post_reranker,
            vectorstore=vectorstore
        )

        if not docs:
            message = "No documents found in the knowledge base for this query."
            if lang == lang_de:
                message = "Für diese Anfrage wurden keine Dokumente in der Wissensdatenbank gefunden."
            if req.stream:
                async def stream_docs_not_found():
                    yield f"data: {json.dumps({'choices': [{'delta': {'content': message}}]})}\n\n"
                return StreamingResponse(stream_docs_not_found(), media_type="text/event-stream")
            return ChatCompletionResponse(
                choices=[ChatChoice(message=ChatMessage(content=message))]
            )

        if concurrency_limiter.locked():
            if req.stream:
                async def stream_server_busy():
                    message = "Server busy. Try again shortly."
                    yield f"data: {json.dumps({'choices': [{'delta': {'content': message}}]})}\n\n"
                return StreamingResponse(stream_server_busy(), media_type="text/event-stream")
            APIError.raise_error(ErrorCode.SERVER_BUSY, "Try again shortly.")
        await concurrency_limiter.acquire()

        try:
            if req.stream:
                vllm_stream = await asyncio.to_thread(
                    query_vllm_stream, query, docs, llm_endpoint, llm_model, req.stop, max_tokens, req.temperature, perf_stat_dict, lang, api_key
                )
                # For streaming, release is handled in locked_stream's finally block
                return StreamingResponse(locked_stream(vllm_stream, perf_stat_dict), media_type="text/event-stream")

            vllm_non_stream = await asyncio.to_thread(
                query_vllm_non_stream, query, docs, llm_endpoint, llm_model, req.stop, max_tokens, req.temperature, perf_stat_dict, lang, api_key
            )
            # Store metrics in registry for non-stream
            perf_registry.add_metric(perf_stat_dict)

            # Handle error responses
            if isinstance(vllm_non_stream, dict) and "error" in vllm_non_stream:
                APIError.raise_error(ErrorCode.LLM_ERROR, str(vllm_non_stream["error"]))

            # Convert vLLM response to ChatCompletionResponse
            if isinstance(vllm_non_stream, dict) and "choices" in vllm_non_stream:
                choices = []
                for choice in vllm_non_stream.get("choices", []):
                    if isinstance(choice, dict):
                        message_dict = choice.get("message", {})
                        if isinstance(message_dict, dict):
                            message_content = message_dict.get("content", "")
                            choices.append(ChatChoice(message=ChatMessage(content=message_content)))
                return ChatCompletionResponse(choices=choices)

            APIError.raise_error(ErrorCode.LLM_ERROR, "Unexpected response format from LLM")
        finally:
            # Release semaphore for non-streaming requests
            # For streaming requests, release is handled in locked_stream's finally block
            if not req.stream:
                concurrency_limiter.release()

    except db.VectorStoreNotReadyError as e:
        APIError.raise_error(ErrorCode.VECTOR_STORE_NOT_READY, str(e))
    except HTTPException:
        raise
    except Exception as e:
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, repr(e))

@app.get(
    "/db-status",
    response_model=DBStatusResponse,
    response_model_exclude_none=True,
    responses={500: http_error_responses[500]},
    tags=["monitoring"],
    summary="Vector DB status",
    description="Check whether the vector store is initialized and populated."
)
async def db_status() -> DBStatusResponse:
    try:
        # Ensure vectorstore is initialized on first request
        if vectorstore is None:
            await ensure_vectorstore_initialized()

        if vectorstore is None:
            return DBStatusResponse(ready=False, message="Vector store not initialized")

        status = await asyncio.to_thread(
            vectorstore.check_db_populated
        )
        if status:
            return DBStatusResponse(ready=True)
        else:
            return DBStatusResponse(ready=False, message="No data ingested")

    except Exception as e:
        return DBStatusResponse(ready=False, message=str(e))

@app.get(
    "/health",
    response_model=HealthResponse,
    status_code=200,
    tags=["monitoring"],
    summary="Health check",
    description="Check if the service is running."
)
async def health() -> HealthResponse:
    return HealthResponse(status="ok")

if __name__ == "__main__":
    port = int(os.getenv("PORT", "5000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
