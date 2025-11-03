from flask import Flask, request, jsonify, Response, stream_with_context
import json
import logging
import os
import time

from common.db_utils import MilvusVectorStore
from common.llm_utils import query_vllm_stream
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

@app.route("/generate", methods=["POST"])
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

@app.route("/stream", methods=["POST"])
def stream():
    data = request.get_json()
    prompt = data.get("prompt", "")
    num_chunks_post_rrf = data.get("num_chunks_post_rrf", 10)
    num_docs_reranker = data.get("num_docs_reranker", 3)
    use_reranker = data.get("use_reranker", True)
    max_tokens = data.get("max_tokens", 512)
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
    except Exception as e:
        return jsonify({"error": repr(e)})

    stop_words = ""
    if stop_words:
        stop_words = stop_words.strip(' ').split(',')
        stop_words = [w.strip() for w in stop_words]
        stop_words = list \
            (set(stop_words) + set(['Context:', 'Question:', '\nContext:', '\nAnswer:', '\nQuestion:', 'Answer:']))
    else:
        stop_words = ['Context:', 'Question:', '\nContext:', '\nAnswer:', '\nQuestion:', 'Answer:']
    return Response(stream_with_context(query_vllm_stream(prompt, docs, llm_endpoint, llm_model, stop_words,max_tokens, True, dynamic_chunk_truncation=TRUNCATION)),
                    content_type='text/plain',
                    mimetype='text/event-stream', headers={
            'Cache-Control': 'no-cache',
            'Connection': 'keep-alive',
            'Access-Control-Allow-Origin': '*',
            'Access-Control-Allow-Headers': 'Content-Type'
        })


@app.route("/reference", methods=["POST"])
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
    except Exception as e:
        return jsonify({"error": repr(e)})
    return Response(
        json.dumps({"documents": docs}, default=str),
        mimetype="application/json"
    )

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
