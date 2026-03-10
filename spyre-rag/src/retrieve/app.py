import os
import logging
import asyncio
import uuid
from typing import Optional
from fastapi import FastAPI, Request, HTTPException
from fastapi.openapi.docs import get_swagger_ui_html
from fastapi.responses import JSONResponse, StreamingResponse
import json
from contextlib import asynccontextmanager
from asyncio import BoundedSemaphore
from functools import wraps
import common.db_utils as db
from common.llm_utils import create_llm_session, query_vllm_stream, query_vllm_non_stream, query_vllm_models
from common.misc_utils import get_model_endpoints, set_log_level, set_request_id
from common.settings import get_settings
from common.perf_utils import perf_registry
from retrieve.backend_utils import search_only, validate_query_length
from retrieve.response_utils import (
    ReferenceRequest,
    ReferenceResponse,
    ChatCompletionRequest,
    ChatCompletionResponse,
    ChatChoice,
    ChatMessage,
    DBStatusResponse,
    HealthResponse,
    ModelsResponse,
    PerfMetricsResponse,
)
import uvicorn
from starlette.concurrency import iterate_in_threadpool


log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif not "info" in level:
        logging.warning(f"Unknown LOG_LEVEL passed: '{level}', using default INFO level")
set_log_level(log_level)

vectorstore = None

# Globals to be set dynamically
emb_model_dict = {}
llm_model_dict = {}
reranker_model_dict = {}

settings = get_settings()
concurrency_limiter = BoundedSemaphore(settings.max_concurrent_requests)

# Setting 32 to fully utilse the vLLM's Max Batch Size
POOL_SIZE = 32

def initialize_models():
    global emb_model_dict, llm_model_dict, reranker_model_dict
    emb_model_dict, llm_model_dict, reranker_model_dict = get_model_endpoints()

def initialize_vectorstore():
    global vectorstore
    vectorstore = db.get_vector_store()

@asynccontextmanager
async def lifespan(app):
    initialize_models()
    initialize_vectorstore()
    create_llm_session(pool_maxsize=POOL_SIZE)
    yield

app = FastAPI(
    lifespan=lifespan,
    title="AI-Services Chatbot API",
    description="RAG-based chatbot API with document retrieval, reranking, and LLM-powered responses.",
    version="1.0.0"
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
        title="AI-Services Chatbot API - Swagger UI",
    )

def limit_concurrency(f):
    @wraps(f)
    async def wrapper(*args, **kwargs):
        if concurrency_limiter.locked():
            raise HTTPException(status_code=429, detail="Server busy. Try again shortly.")
        await concurrency_limiter.acquire()
        try:
            return await f(*args, **kwargs)
        finally:
            concurrency_limiter.release()
    return wrapper

@app.post(
    "/reference",
    response_model=ReferenceResponse,
    summary="Retrieve reference documents",
    description="Search the vector store using the prompt, rerank results, and return relevant document chunks with performance metrics."
)
async def get_reference_docs(req: ReferenceRequest) -> ReferenceResponse:
    # Validate query is not empty
    if not req.prompt or not req.prompt.strip():
        raise HTTPException(status_code=400, detail="Query cannot be empty")
    
    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        reranker_model = reranker_model_dict['reranker_model']
        reranker_endpoint = reranker_model_dict['reranker_endpoint']
        
        # Validate query length
        is_valid, token_count, error_msg = await asyncio.to_thread(
            validate_query_length, req.prompt, emb_endpoint
        )
        if not is_valid:
            raise HTTPException(
                status_code=400,
                detail=f"{error_msg}. Query has {token_count} tokens, maximum allowed is {settings.max_query_token_length} tokens."
            )

        docs, perf_stat_dict = await asyncio.to_thread(
            search_only,
            req.prompt,
            emb_model, emb_endpoint, emb_max_tokens,
            reranker_model,
            reranker_endpoint,
            settings.num_chunks_post_search,
            settings.num_chunks_post_reranker,
            vectorstore=vectorstore
        )
        # Store metrics in registry for reference endpoint
        perf_registry.add_metric(perf_stat_dict)
        
    except HTTPException:
        # Re-raise HTTPException without modification
        raise
    except db.VectorStoreNotReadyError as e:
        raise HTTPException(status_code=503, detail=str(e))
    except Exception as e:
        raise HTTPException(status_code=500, detail=repr(e))
    
    return ReferenceResponse(documents=docs, perf_metrics=perf_stat_dict)


@app.get(
    "/v1/models",
    response_model=ModelsResponse,
    summary="List LLM models",
    description="List available models from the configured vLLM endpoint."
)
async def list_models():
    logging.debug("List models..")
    try:
        llm_endpoint = llm_model_dict['llm_endpoint']
        return await asyncio.to_thread(query_vllm_models, llm_endpoint)
    except Exception as e:
        raise HTTPException(status_code=500, detail=repr(e))


@app.get(
    "/v1/perf_metrics",
    response_model=PerfMetricsResponse,
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
            raise HTTPException(status_code=404, detail=f"No metric found for request_id: {request_id}")
        return PerfMetricsResponse(metrics=[metric])
    else:
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
    summary="Chat with RAG",
    description="Generate chat completions grounded in retrieved documents. Returns streaming response if stream=true, otherwise returns structured JSON. For detailed request and response structure, see: https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create"
)
async def chat_completion(req: ChatCompletionRequest) -> ChatCompletionResponse | StreamingResponse:
    if not req.messages:
        raise HTTPException(status_code=400, detail="messages can't be empty")

    query = req.messages[0].content
    
    # Validate query is not empty
    if not query or not query.strip():
        raise HTTPException(status_code=400, detail="Query cannot be empty")

    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        llm_model = llm_model_dict['llm_model']
        llm_endpoint = llm_model_dict['llm_endpoint']
        reranker_model = reranker_model_dict['reranker_model']
        reranker_endpoint = reranker_model_dict['reranker_endpoint']
        
        # Validate query length
        is_valid, token_count, error_msg = await asyncio.to_thread(
            validate_query_length, query, emb_endpoint
        )
        if not is_valid:
            raise HTTPException(
                status_code=400,
                detail=f"{error_msg}. Query has {token_count} tokens, maximum allowed is {settings.max_query_token_length} tokens."
            )
        
        docs, perf_stat_dict = await asyncio.to_thread(
            search_only,
            query,
            emb_model, emb_endpoint, emb_max_tokens,
            reranker_model,
            reranker_endpoint,
            settings.num_chunks_post_search,
            settings.num_chunks_post_reranker,
            vectorstore=vectorstore
        )
    except HTTPException:
        # Re-raise HTTPException without modification
        raise
    except db.VectorStoreNotReadyError as e:
        raise HTTPException(status_code=503, detail=str(e))
    except Exception as e:
        raise HTTPException(status_code=500, detail=repr(e))

    if not docs:
        message = "No documents found in the knowledge base for this query."
        if req.stream:
            async def stream_docs_not_found():
                yield f"data: {json.dumps({'choices': [{'delta': {'content': message}}]})}\n\n"
            return StreamingResponse(stream_docs_not_found(), media_type="text/event-stream")
        else:
            return ChatCompletionResponse(
                choices=[ChatChoice(message=ChatMessage(content=message))]
            )

    if concurrency_limiter.locked():
        if req.stream:
            async def stream_server_busy():
                message = "Server busy. Try again shortly."
                yield f"data: {json.dumps({'choices': [{'delta': {'content': message}}]})}\n\n"
            return StreamingResponse(stream_server_busy(), media_type="text/event-stream")
        else:
            raise HTTPException(status_code=429, detail="Server busy. Try again shortly.")
    await concurrency_limiter.acquire()

    try:
        if req.stream:
            vllm_stream = await asyncio.to_thread(
                query_vllm_stream, query, docs, llm_endpoint, llm_model, req.stop, req.max_tokens, req.temperature, perf_stat_dict
            )
            # For streaming, release is handled in locked_stream's finally block
            return StreamingResponse(locked_stream(vllm_stream, perf_stat_dict), media_type="text/event-stream")
        else:
            vllm_non_stream = await asyncio.to_thread(
                query_vllm_non_stream, query, docs, llm_endpoint, llm_model, req.stop, req.max_tokens, req.temperature, perf_stat_dict
            )
            # Store metrics in registry for non-stream
            perf_registry.add_metric(perf_stat_dict)
            
            # Handle error responses
            if isinstance(vllm_non_stream, dict) and "error" in vllm_non_stream:
                raise HTTPException(status_code=500, detail=str(vllm_non_stream["error"]))
            
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
            
            # If response doesn't match expected structure, raise an error
            raise HTTPException(status_code=500, detail="Unexpected response format from LLM")
    except Exception as e:
        raise HTTPException(status_code=500, detail=repr(e))
    finally:
        # Release semaphore for non-streaming requests
        # For streaming requests, release is handled in locked_stream's finally block
        if not req.stream:
            concurrency_limiter.release()

@app.get(
    "/db-status",
    response_model=DBStatusResponse,
    response_model_exclude_none=True,
    summary="Vector DB status",
    description="Check whether the vector store is initialized and populated."
)
async def db_status() -> DBStatusResponse:
    try:
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
    summary="Health check",
    description="Check if the service is running."
)
async def health() -> HealthResponse:
    return HealthResponse(status="ok")

if __name__ == "__main__":
    port = int(os.getenv("PORT", "5000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
