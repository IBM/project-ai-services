from flask import Flask, request, jsonify, Response, stream_with_context
import json
import logging
import os
import time

from common.db_utils import MilvusVectorStore, MilvusNotReadyError
from common.llm_utils import query_vllm_stream, query_vllm_models
from common.misc_utils import get_model_endpoints, set_log_level
from retrieve.backend_utils import search_and_answer_backend, search_only

vectorstore = None
TRUNCATION  = True

# Globals to be set dynamically
emb_model_dict = {}
llm_model_dict = {}
reranker_model_dict = {}

def initialize_models():
    global emb_model_dict, llm_model_dict, reranker_model_dict
    emb_model_dict, llm_model_dict, reranker_model_dict = get_model_endpoints()

def initialize_vectorstore():
    global vectorstore
    vectorstore = MilvusVectorStore()

app = Flask(__name__)

@app.post("/generate")
def generate():
    data = request.get_json()
    prompt = data.get("prompt", "")
    num_chunks_post_rrf = data.get("num_chunks_post_rrf", 10)
    num_docs_reranker = data.get("num_docs_reranker", 3)
    use_reranker = data.get("use_reranker", True)
    max_tokens = data.get("max_tokens", 512)
    start_time = time.time()
    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        llm_model = llm_model_dict['llm_model']
        llm_endpoint = llm_model_dict['llm_endpoint']
        reranker_model = reranker_model_dict['reranker_model']
        reranker_endpoint = reranker_model_dict['reranker_endpoint']

        stop_words = ""

        (rag_ans, docs) = search_and_answer_backend(
            prompt,
            llm_endpoint,
            llm_model,
            emb_model, emb_endpoint, emb_max_tokens,
            reranker_model,
            reranker_endpoint,
            num_chunks_post_rrf,
            num_docs_reranker,
            use_reranker,
            max_tokens,
            stop_words=stop_words,
            language="en",
            vectorstore=vectorstore,
            stream=False,
            truncation=TRUNCATION
        )
    except Exception as e:
        return jsonify({"error": repr(e)}), 500
    end_time = time.time()
    request_time = end_time - start_time
    return Response(
        json.dumps({"response": rag_ans, "documents": docs, "request time": request_time}, default=str),
        mimetype="application/json"
    )

@app.post("/reference")
def get_reference_docs():
    data = request.get_json()
    prompt = data.get("prompt", "")
    num_chunks_post_rrf = data.get("num_chunks_post_rrf", 10)
    num_docs_reranker = data.get("num_docs_reranker", 3)
    use_reranker = data.get("use_reranker", True)
    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        reranker_model = reranker_model_dict['reranker_model']
        reranker_endpoint = reranker_model_dict['reranker_endpoint']

        docs = search_only(
            prompt,
            emb_model, emb_endpoint, emb_max_tokens,
            reranker_model,
            reranker_endpoint,
            num_chunks_post_rrf,
            num_docs_reranker,
            use_reranker,
            vectorstore=vectorstore
        )
    except MilvusNotReadyError as e:
        return jsonify({"error": str(e)}), 503   # Service unavailable
    except Exception as e:
        return jsonify({"error": repr(e)})
    return Response(
        json.dumps({"documents": docs}, default=str),
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

@app.post("/v1/chat/completions")
def chat_completion():
    data = request.get_json()
    if data and len(data.get("messages", [])) == 0:
        return jsonify({"error": "messages can't be empty"})
    msgs = data.get("messages")[0]
    prompt = msgs.get("content")
    num_chunks_post_rrf = data.get("num_chunks_post_rrf", 10)
    num_docs_reranker = data.get("num_docs_reranker", 3)
    use_reranker = data.get("use_reranker", True)
    max_tokens = data.get("max_tokens", 512)
    temperature = data.get("temperature", 0.0)
    stop_words = data.get("stop")
    stream = data.get("stream")
    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        llm_model = llm_model_dict['llm_model']
        llm_endpoint = llm_model_dict['llm_endpoint']
        reranker_model = reranker_model_dict['reranker_model']
        reranker_endpoint = reranker_model_dict['reranker_endpoint']
        docs = search_only(
            prompt,
            emb_model, emb_endpoint, emb_max_tokens,
            reranker_model,
            reranker_endpoint,
            num_chunks_post_rrf,
            num_docs_reranker,
            use_reranker,
            vectorstore=vectorstore
        )
    except MilvusNotReadyError as e:
        return jsonify({"error": str(e)}), 503   # Service unavailable
    except Exception as e:
        return jsonify({"error": repr(e)})

    resp_text = None
    if docs:
        resp_text = stream_with_context(query_vllm_stream(prompt, docs, llm_endpoint, llm_model, stop_words, max_tokens, temperature, stream, dynamic_chunk_truncation=TRUNCATION))
    else:
        resp_text = stream_with_context(stream_docs_not_found())

    return Response(resp_text,
                    content_type='text/event-stream',
                    mimetype='text/event-stream', headers={
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
async def health():
    return jsonify({"status": "ok"}), 200


if __name__ == "__main__":
    initialize_models()
    initialize_vectorstore()

    port = int(os.getenv("PORT", "5000"))

    log_level = logging.INFO
    level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
    if level != "":
        if "debug" in level:
            log_level == logging.DEBUG
        elif not "info" in level:
            raise Exception(f"Unknown LOG_LEVEL passed: '{level}'")
    set_log_level(log_level)

    app.run(host="0.0.0.0", port=port)
