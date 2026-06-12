import os
import logging
import asyncio
import uuid
from typing import Optional
from fastapi import FastAPI, Request, HTTPException, Header, Depends
from fastapi.security import HTTPBearer, HTTPAuthorizationCredentials
from fastapi.openapi.docs import get_swagger_ui_html
from fastapi.responses import StreamingResponse, Response
import json
from contextlib import asynccontextmanager
from asyncio import BoundedSemaphore
from functools import wraps
import uvicorn
from starlette.concurrency import iterate_in_threadpool
from lingua import Language

from common.misc_utils import set_log_level
from common.lang_utils import detect_language, LanguageCodes, get_max_tokens_map

from chatbot.settings import settings
from chatbot.conversation_utils import get_conversation_context, truncate_history_by_tokens
from chatbot.query_rephrasing import rephrase_query_with_context

set_log_level(settings.common.app.log_level)

from common.diagnostic_logger import setup_comprehensive_crash_handler
import common.db_utils as db
from common.misc_utils import get_embedding_endpoint, get_llm_endpoint, get_reranker_endpoint, set_request_id, create_llm_session, configure_uvicorn_logging
from common.llm_utils import query_vllm_stream, query_vllm_non_stream, query_vllm_models, tokenize_with_llm
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

# Language-specific messages
NO_DOCUMENTS_FOUND_MESSAGES = {
    "EN": "No documents found in the knowledge base for this query.",
    "DE": "Für diese Anfrage wurden keine Dokumente in der Wissensdatenbank gefunden.",
    "IT": "Nessun documento trovato nella base di conoscenza per questa richiesta.",
    "FR": "Aucun document trouvé dans la base de connaissances pour cette requête.",
}

# Cache for auth requirement check
auth_required_cache = {"checked": False, "required": False}
auth_cache_lock = asyncio.Lock()

concurrency_limiter = BoundedSemaphore(settings.common.llm.max_batch_size)

def initialize_models():
    global emb_model_dict, llm_model_dict, reranker_model_dict
    emb_model_dict = get_embedding_endpoint()
    llm_model_dict = get_llm_endpoint()
    reranker_model_dict = get_reranker_endpoint()

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
    create_llm_session(pool_maxsize=settings.common.llm.max_batch_size)
    initialize_models()
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
    description="""RAG-based chatbot API with document retrieval, reranking, and LLM-powered responses.

**Key Features**:
- **Conversational RAG**: Multi-turn conversations with automatic context management and query rephrasing
- **Semantic Search**: Vector-based document retrieval with reranking for improved relevance
- **Streaming Support**: Real-time token generation for responsive user experience
- **Multi-language**: Automatic language detection (English, German, French, Italian supported)
- **Performance Metrics**: Detailed timing and token usage tracking

**Authentication**: Optional vLLM API key authentication via Bearer token in Authorization header.
""",
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

def get_stop_words_with_special_tokens(request_stop_words):
    """
    Add common special tokens to stop words to prevent them from appearing in responses.

    Args:
        request_stop_words: Stop words from the request (can be None, list, or string)

    Returns:
        List of stop words including special tokens
    """
    stop_words = list(request_stop_words) if request_stop_words else []
    # Add common special tokens that should stop generation
    special_tokens = ["[/assistant]", "</s>", "<|endoftext|>", "<|im_end|>"]
    for token in special_tokens:
        if token not in stop_words:
            stop_words.append(token)
    return stop_words

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
    description="""Generate chat completions grounded in retrieved documents using RAG (Retrieval-Augmented Generation).

**Conversational Mode**: Supports multi-turn conversations by passing message history. The system automatically:
- Extracts the current query from the last message
- Uses previous messages as conversation context
- Rephrases the current query based on conversation history for better retrieval
- Maintains context-aware responses across turns

**Features**:
- Single-turn queries: Pass one message for standalone questions
- Multi-turn conversations: Pass message array with history for context-aware responses
- Streaming: Set `stream=true` for real-time token generation
- Language detection: Automatically detects query language (English, German, French, Italian supported)
- Query rephrasing: Automatically rephrases follow-up questions using conversation context

**Authentication**: Requires API key in Authorization header (Bearer token) if vLLM authentication is enabled.

**Response Headers**:
- `X-Rephrased-Query`: Contains the rephrased query when conversation history is used (only if different from original)
- `X-Request-ID`: Unique request identifier for tracking and metrics
""",
    responses={
        200: {
            "description": "Successful Response",
            "content": {
                "application/json": {
                    "examples": {
                        "single_turn": {
                            "summary": "Single-turn response",
                            "description": "Response to a standalone query without conversation history",
                            "value": {
                                "choices": [
                                    {
                                        "message": {
                                            "content": "Based on the retrieved documents, artificial intelligence (AI) is the simulation of human intelligence processes by machines, especially computer systems. These processes include learning, reasoning, and self-correction."
                                        }
                                    }
                                ]
                            }
                        },
                        "multi_turn": {
                            "summary": "Multi-turn response",
                            "description": "Response to a follow-up question with conversation context",
                            "value": {
                                "choices": [
                                    {
                                        "message": {
                                            "content": "Some common examples of machine learning applications include: 1) Email spam filtering, 2) Image recognition and classification, 3) Recommendation systems (like Netflix or Amazon), 4) Voice assistants (Siri, Alexa), and 5) Autonomous vehicles."
                                        }
                                    }
                                ]
                            }
                        }
                    }
                },
                "text/event-stream": {
                    "schema": {
                        "type": "string",
                        "description": "Server-Sent Events stream. Each event is formatted as: data: {JSON}\\n\\n. Stream ends with data: [DONE]\\n\\n"
                    },
                    "examples": {
                        "streaming": {
                            "summary": "Streaming response",
                            "description": "Real-time token generation for immediate user feedback",
                            "value": 'data: {"choices":[{"delta":{"content":"Based on"}}]}\n\ndata: {"choices":[{"delta":{"content":" the retrieved"}}]}\n\ndata: {"choices":[{"delta":{"content":" documents,"}}]}\n\ndata: {"choices":[{"delta":{"content":" artificial"}}]}\n\ndata: {"choices":[{"delta":{"content":" intelligence..."}}]}\n\ndata: [DONE]\n\n'
                        }
                    }
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
async def chat_completion(req: ChatCompletionRequest, credentials: Optional[HTTPAuthorizationCredentials] = Depends(security)) -> ChatCompletionResponse | StreamingResponse | Response:
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

    current_query, previous_messages = get_conversation_context(req.messages)

    # Validate query is not empty
    if not current_query or not current_query.strip():
        APIError.raise_error(ErrorCode.EMPTY_INPUT, "Query cannot be empty")

    # Detect language from current query (stateless - detect on every message)
    try:
        query_lang = detect_language(current_query)
        
        # Fallback to English if unsupported language detected
        if query_lang not in LanguageCodes.supported_languages():
            logging.debug(
                f"Unsupported language detected ({query_lang}). "
                "Falling back to English."
            )
            query_lang = LanguageCodes.ENGLISH
        
        logging.debug(f"Detected language for current message: {query_lang}")
        
    except Exception as e:
        logging.warning(
            f"Language detection failed: {e}. "
            "Falling back to English."
        )
        query_lang = LanguageCodes.ENGLISH

    # Ensure vectorstore is initialized on first request
    if vectorstore is None:
        await ensure_vectorstore_initialized()

    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_model_len = emb_model_dict['max_model_len']
        llm_model = llm_model_dict['llm_model']
        llm_endpoint = llm_model_dict['llm_endpoint']
        reranker_model = reranker_model_dict['reranker_model']
        reranker_endpoint = reranker_model_dict['reranker_endpoint']


        # Validate query length
        is_valid, error_msg = await asyncio.to_thread(
            validate_query_length, current_query, emb_endpoint
        )
        if not is_valid:
            # Return streaming error response for consistency
            if req.stream:
                async def stream_query_length_error():
                    message = "Your query is too long. Please shorten it and try again."
                    yield f"data: {json.dumps({'choices': [{'delta': {'content': message}}]})}\n\n"
                return StreamingResponse(stream_query_length_error(), media_type="text/event-stream")
            APIError.raise_error(ErrorCode.INVALID_PARAMETER, error_msg)

        max_tokens = req.max_tokens
        # giving priority to max_tokens passed in the request, otherwise according to query language
        if not max_tokens:
            max_tokens = get_max_tokens_map().get(query_lang, settings.llm.english.max_tokens)

        rephrased_query = current_query
        
        # Process conversation history and rephrase query for supported conversational languages
        if previous_messages:
            truncated_history_for_rephrasing = await asyncio.to_thread(
                truncate_history_by_tokens,
                previous_messages,
                settings.query_rephrasing.history_token_budget,
                lambda text: tokenize_with_llm(text, llm_endpoint)
            )

            if truncated_history_for_rephrasing:
                rephrased_query = await rephrase_query_with_context(
                    current_query=current_query,
                    previous_messages=truncated_history_for_rephrasing,
                    llm_endpoint=llm_endpoint,
                    llm_model=llm_model,
                    api_key=api_key,
                    lang=query_lang,
                )

        docs, perf_stat_dict = await asyncio.to_thread(
            search_only,
            rephrased_query,
            emb_model, emb_endpoint, emb_max_model_len,
            reranker_model,
            reranker_endpoint,
            settings.chatbot.num_chunks_post_search,
            settings.chatbot.num_chunks_post_reranker,
            vectorstore=vectorstore
        )

        if not docs:
            message = NO_DOCUMENTS_FOUND_MESSAGES.get(query_lang, NO_DOCUMENTS_FOUND_MESSAGES["EN"])
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
            stop_words = get_stop_words_with_special_tokens(req.stop)

            if req.stream:
                vllm_stream = await asyncio.to_thread(
                    query_vllm_stream,
                    current_query,
                    docs,
                    llm_endpoint,
                    llm_model,
                    stop_words,
                    max_tokens,
                    req.temperature,
                    perf_stat_dict,
                    query_lang,
                    api_key,
                    previous_messages,
                    rephrased_query,
                )
                # For streaming, release is handled in locked_stream's finally block
                response = StreamingResponse(locked_stream(vllm_stream, perf_stat_dict), media_type="text/event-stream")
                # Add rephrased query as a custom header if available
                if rephrased_query and rephrased_query != current_query:
                    response.headers["X-Rephrased-Query"] = rephrased_query
                return response

            vllm_non_stream = await asyncio.to_thread(
                query_vllm_non_stream,
                current_query,
                docs,
                llm_endpoint,
                llm_model,
                stop_words,
                max_tokens,
                req.temperature,
                perf_stat_dict,
                query_lang,
                api_key,
                previous_messages,
                rephrased_query,
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

                response_data = ChatCompletionResponse(
                    choices=choices
                )
                # Add rephrased query as a custom header if available
                if rephrased_query and rephrased_query != current_query:
                    return Response(
                        content=response_data.model_dump_json(),
                        media_type="application/json",
                        headers={"X-Rephrased-Query": rephrased_query}
                    )
                return response_data

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
