import os
import logging
from common.misc_utils import set_log_level

log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif not "info" in level:
        raise Exception(f"Unknown LOG_LEVEL passed: '{level}'")
set_log_level(log_level)


from flask import Flask, request, jsonify, Response, stream_with_context
import json
from threading import BoundedSemaphore
from functools import wraps

import common.db_utils as db
from common.llm_utils import create_llm_session, query_vllm_stream, query_vllm_non_stream, query_vllm_models
from common.misc_utils import get_model_endpoints, set_log_level
from common.settings import get_settings
from common.perf_utils import perf_registry
from retrieve.backend_utils import search_only


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

app = Flask(__name__)

# Setting 32 to fully utilse the vLLM's Max Batch Size
POOL_SIZE = 32

create_llm_session(pool_maxsize=POOL_SIZE)

def limit_concurrency(f):
    @wraps(f)
    def wrapper(*args, **kwargs):
        if not concurrency_limiter.acquire(blocking=False):
            return jsonify({"error": "Server busy. Try again shortly."}), 429
        try:
            return f(*args, **kwargs)
        finally:
            concurrency_limiter.release()
    return wrapper


@app.post("/reference")
def get_reference_docs():
    data = request.get_json()
    query = data.get("prompt", "")
    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
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
        # Store metrics in registry for reference endpoint
        perf_registry.add_metric(perf_stat_dict)
        
    except db.get_vector_store_not_ready() as e:
        return jsonify({"error": str(e)}), 503   # Service unavailable
    except Exception as e:
        return jsonify({"error": repr(e)})
    
    response_data = {"documents": docs, "perf_metrics": perf_stat_dict}
    return Response(
        json.dumps(response_data, default=str),
        mimetype="application/json"
    )


@app.get("/v1/models")
def list_models():
    logging.debug("List models..")
    try:
        llm_endpoint = llm_model_dict['llm_endpoint']
        return query_vllm_models(llm_endpoint)
    except Exception as e:
        return jsonify({"error": repr(e)})


@app.get("/v1/perf_metrics")
def get_perf_metrics():
    return jsonify(perf_registry.get_metrics())

def locked_stream(stream_g, perf_stat_dict):
    try:
        for chunk in stream_g:
            yield chunk
    finally:
        perf_registry.add_metric(perf_stat_dict)
        concurrency_limiter.release()


@app.post("/v1/chat/completions")
def chat_completion():
    data = request.get_json()
    if data and len(data.get("messages", [])) == 0:
        return jsonify({"error": "messages can't be empty"})
    msgs = data.get("messages")[0]
    query = msgs.get("content")
    max_tokens = data.get("max_tokens", settings.llm_max_tokens)
    temperature = data.get("temperature", settings.temperature)
    stop_words = data.get("stop")
    stream = data.get("stream", False)
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
        return jsonify({"error": str(e)}), 503   # Service unavailable
    except Exception as e:
        return jsonify({"error": repr(e)})

    resp_text = None

    if docs:
        if not concurrency_limiter.acquire(blocking=False):
            return jsonify({"error": "Server busy. Try again shortly."}), 429

        try:
            if stream:
                vllm_stream = query_vllm_stream(query, docs, llm_endpoint, llm_model, stop_words, max_tokens, temperature, perf_stat_dict )
                resp_text = stream_with_context(locked_stream(vllm_stream, perf_stat_dict))           
            else:
                vllm_non_stream = query_vllm_non_stream(query, docs, llm_endpoint, llm_model, stop_words, max_tokens, temperature, perf_stat_dict )
                
                # Add performance metrics to the response dictionary
                if isinstance(vllm_non_stream, dict):
                    vllm_non_stream["perf_metrics"] = perf_stat_dict
                
                resp_text = json.dumps(vllm_non_stream, indent=None, separators=(',', ':'))
                # Store metrics in registry for non-stream
                perf_registry.add_metric(perf_stat_dict)
                # release semaphore lock because its non-stream request
                concurrency_limiter.release()
        except Exception as e:
            concurrency_limiter.release()
            return jsonify({"error": repr(e)}), 500

    else:
        resp_text = stream_with_context(stream_docs_not_found())

    if stream:
        return Response(resp_text,
                content_type='text/event-stream',
                mimetype='text/event-stream', headers={
                'Cache-Control': 'no-cache',
                'Connection': 'keep-alive',
                'Access-Control-Allow-Headers': 'Content-Type'
            })
    return Response(resp_text,
                content_type='application/json',
                mimetype='application/json', headers={
                'Cache-Control': 'no-cache',
                'Connection': 'keep-alive',
                'Access-Control-Allow-Headers': 'Content-Type'
                })

@app.get("/db-status")
def db_status():
    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        status = vectorstore.check_db_populated(emb_model, emb_endpoint, emb_max_tokens)
        if status==True:
            return jsonify({"ready": True}), 200
        else:
            return jsonify({"ready": False, "message": "No data ingested"}), 200
        
    except Exception as e:
        return jsonify({"ready": False, "message": str(e)}), 500


def stream_docs_not_found():
    message = "No documents found in the knowledge base for this query."
    yield f"data: {json.dumps({'choices': [{'delta': {'content': message}}]})}\n\n"


@app.get("/health")
def health():
    return jsonify({"status": "ok"}), 200


if __name__ == "__main__":
    initialize_models()
    initialize_vectorstore()
    port = int(os.getenv("PORT", "5000"))
    app.run(host="0.0.0.0", port=port)
