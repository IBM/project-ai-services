import os
import logging
from fastapi import FastAPI, Request, HTTPException
from fastapi.responses import JSONResponse, StreamingResponse
from pydantic import BaseModel
import json
from contextlib import asynccontextmanager
from asyncio import BoundedSemaphore
from functools import wraps
import common.db_utils as db
from common.llm_utils import create_llm_session, query_vllm_stream, query_vllm_non_stream, query_vllm_models
from common.misc_utils import get_model_endpoints, set_log_level
from common.settings import get_settings
from common.perf_utils import perf_registry
from retrieve.backend_utils import search_only
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

app = FastAPI(lifespan=lifespan)

# Setting 32 to fully utilse the vLLM's Max Batch Size
POOL_SIZE = 32

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

class ReferenceRequest(BaseModel):
    prompt: str

class Message(BaseModel):
    content: str

class ChatCompletionRequest(BaseModel):
    messages: list[Message]
    max_tokens: int = get_settings().llm_max_tokens
    temperature: float = get_settings().temperature
    stop: list[str] | None = None
    stream: bool = False


@app.post(
    "/reference",
    summary="Retrieve reference documents",
    description="Search the vector store using the prompt, rerank results, and return relevant document chunks with performance metrics."
)
async def get_reference_docs(req: ReferenceRequest):
    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        reranker_model = reranker_model_dict['reranker_model']
        reranker_endpoint = reranker_model_dict['reranker_endpoint']

        docs, perf_stat_dict = search_only(
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
        
    except db.get_vector_store_not_ready() as e:
        raise HTTPException(status_code=503, detail=str(e))
    except Exception as e:
        raise HTTPException(status_code=500, detail=repr(e))
    
    return {"documents": docs, "perf_metrics": perf_stat_dict}


@app.get(
    "/v1/models",
    summary="List LLM models",
    description="List available models from the configured vLLM endpoint."
)
async def list_models():
    logging.debug("List models..")
    try:
        llm_endpoint = llm_model_dict['llm_endpoint']
        return query_vllm_models(llm_endpoint)
    except Exception as e:
        raise HTTPException(status_code=500, detail=repr(e))


@app.get(
    "/v1/perf_metrics",
    summary="Get performance metrics",
    description="Return collected performance metrics for recent chat completion calls."
)
def get_perf_metrics():
    return perf_registry.get_metrics()

async def locked_stream(stream_g, perf_stat_dict):
    try:
        async for chunk in iterate_in_threadpool(stream_g):
            yield chunk
    finally:
        perf_registry.add_metric(perf_stat_dict)
        concurrency_limiter.release()


@app.post(
    "/v1/chat/completions",
    summary="Chat with RAG",
    description="Generate chat completions grounded in retrieved documents."
)
async def chat_completion(req: ChatCompletionRequest):
    if not req.messages:
        raise HTTPException(status_code=400, detail="messages can't be empty")

    query = req.messages[0].content

    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        llm_model = llm_model_dict['llm_model']
        llm_endpoint = llm_model_dict['llm_endpoint']
        reranker_model = reranker_model_dict['reranker_model']
        reranker_endpoint = reranker_model_dict['reranker_endpoint']
        
        docs, perf_stat_dict = search_only(
            query,
            emb_model, emb_endpoint, emb_max_tokens,
            reranker_model,
            reranker_endpoint,
            settings.num_chunks_post_search,
            settings.num_chunks_post_reranker,
            vectorstore=vectorstore
        )
    except db.get_vector_store_not_ready() as e:
        raise HTTPException(status_code=503, detail=str(e))
    except Exception as e:
        raise HTTPException(status_code=500, detail=repr(e))

    if not docs:
        async def stream_docs_not_found():
            message = "No documents found in the knowledge base for this query."
            yield f"data: {json.dumps({'choices': [{'delta': {'content': message}}]})}\n\n"
        return StreamingResponse(stream_docs_not_found(), media_type="text/event-stream")

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
        release_required = True
        if req.stream:
            vllm_stream = query_vllm_stream(query, docs, llm_endpoint, llm_model, req.stop, req.max_tokens, req.temperature, perf_stat_dict)
            release_required = False
            return StreamingResponse(locked_stream(vllm_stream, perf_stat_dict), media_type="text/event-stream")
        else:
            vllm_non_stream = query_vllm_non_stream(query, docs, llm_endpoint, llm_model, req.stop, req.max_tokens, req.temperature, perf_stat_dict)
            # Store metrics in registry for non-stream
            perf_registry.add_metric(perf_stat_dict)
            # release semaphore lock because its non-stream request
            concurrency_limiter.release()
            release_required = False
            return vllm_non_stream
    except Exception as e:
        if release_required:
            concurrency_limiter.release()
        raise HTTPException(status_code=500, detail=repr(e))

@app.get(
    "/db-status",
    summary="Vector DB status",
    description="Check whether the vector store is initialized and populated."
)
async def db_status():
    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        status = vectorstore.check_db_populated(emb_model, emb_endpoint, emb_max_tokens)
        if status==True:
            return {"ready": True}
        else:
            return JSONResponse(content={"ready": False, "message": "No data ingested"}, status_code=200)
        
    except Exception as e:
        return JSONResponse(content={"ready": False, "message": str(e)}, status_code=500)

@app.get("/health", status_code=200)
async def health():
    return {"status": "ok"}

if __name__ == "__main__":
    port = int(os.getenv("PORT", "5000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
