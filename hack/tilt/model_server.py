"""
Local model server for running the RAG services on a laptop (e.g. macOS) with Tilt.

The RAG services expect vLLM-style endpoints for the LLM, the embedding model and
the reranker. This process provides them from a single port so that no
ppc64le/vLLM containers are needed:

  /llm/...     chat completions forwarded to IBM watsonx.ai (through LiteLLM),
               plus a local /tokenize (Hugging Face tokenizer)
  /emb/...     /v1/embeddings + /tokenize served locally with sentence-transformers
  /rerank/...  Cohere-compatible /v2/rerank served locally with a CrossEncoder

Point the services at it with:
  LLM_ENDPOINT=http://localhost:<port>/llm
  EMB_ENDPOINT=http://localhost:<port>/emb
  RERANKER_ENDPOINT=http://localhost:<port>/rerank

Local development only. Configuration is read from environment variables (see Config).
"""
import asyncio
import json
import logging
import os
import threading
import uuid
from contextlib import asynccontextmanager

import uvicorn
from fastapi import APIRouter, FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse, StreamingResponse

logging.basicConfig(level=os.getenv("LOG_LEVEL", "INFO"))
log = logging.getLogger("model-server")


class Config:
    port = int(os.getenv("MODEL_SERVER_PORT", "8100"))
    device = os.getenv("MODEL_SERVER_DEVICE", "auto")  # auto | mps | cuda | cpu

    embed_model = os.getenv("EMBED_MODEL", "ibm-granite/granite-embedding-278m-multilingual")
    embed_max_len = int(os.getenv("EMBED_MAX_LEN", "512"))
    rerank_model = os.getenv("RERANK_MODEL", "BAAI/bge-reranker-v2-m3")
    rerank_max_len = int(os.getenv("RERANK_MAX_LEN", "1024"))

    # Model name the services ask for (LLM_MODEL) and the watsonx model that serves it.
    llm_model = os.getenv("WATSONX_MODEL", "ibm/granite-4-h-small")
    llm_max_len = int(os.getenv("LLM_MAX_LEN", "26208"))
    # Hugging Face tokenizer used for the LLM's /tokenize (token counting only).
    llm_tokenizer = os.getenv("LLM_TOKENIZER", "ibm-granite/granite-4.1-8b")

    watsonx_url = os.getenv("WATSONX_URL", "")
    watsonx_api_key = os.getenv("WATSONX_APIKEY", "")
    watsonx_project_id = os.getenv("WATSONX_PROJECT_ID", "")


class Models:
    """Lazily-loaded models, loaded once at startup in a worker thread."""

    embedder = None
    reranker = None
    llm_tokenizer = None
    ready = False
    error = None
    # torch on MPS is not safe to call concurrently from many threads.
    lock = threading.Lock()


def _resolve_device() -> str:
    import torch

    if Config.device != "auto":
        return Config.device
    if torch.cuda.is_available():
        return "cuda"
    if torch.backends.mps.is_available():
        return "mps"
    return "cpu"


def _load_models():
    try:
        from sentence_transformers import CrossEncoder, SentenceTransformer
        from transformers import AutoTokenizer

        device = _resolve_device()
        log.info("Loading embedding model %s on %s", Config.embed_model, device)
        Models.embedder = SentenceTransformer(Config.embed_model, device=device)
        Models.embedder.max_seq_length = Config.embed_max_len

        log.info("Loading reranker model %s on %s", Config.rerank_model, device)
        Models.reranker = CrossEncoder(Config.rerank_model, device=device, max_length=Config.rerank_max_len)

        log.info("Loading LLM tokenizer %s", Config.llm_tokenizer)
        try:
            Models.llm_tokenizer = AutoTokenizer.from_pretrained(Config.llm_tokenizer)
        except Exception as e:  # tokenizer is only used to count tokens
            log.warning("LLM tokenizer %s unavailable (%s); falling back to the embedding tokenizer", Config.llm_tokenizer, e)
            Models.llm_tokenizer = Models.embedder.tokenizer

        Models.ready = True
        log.info("All models loaded")
    except Exception as e:  # surfaced through /health
        Models.error = f"{type(e).__name__}: {e}"
        log.exception("Model loading failed")


@asynccontextmanager
async def lifespan(_: FastAPI):
    threading.Thread(target=_load_models, daemon=True).start()
    yield


app = FastAPI(title="Local RAG model server", lifespan=lifespan)


def _require_ready():
    if Models.error:
        raise HTTPException(status_code=503, detail=f"model loading failed: {Models.error}")
    if not Models.ready:
        raise HTTPException(status_code=503, detail="models are still loading")


@app.get("/health")
async def health():
    if Models.error:
        return JSONResponse({"status": "error", "detail": Models.error}, status_code=503)
    if not Models.ready:
        return JSONResponse({"status": "loading"}, status_code=503)
    return {"status": "ok"}


def _models_payload(model_id: str, max_len: int) -> dict:
    return {"object": "list", "data": [{"id": model_id, "object": "model", "max_model_len": max_len}]}


# --------------------------------------------------------------------------- #
# Embeddings: /emb
# --------------------------------------------------------------------------- #
emb = APIRouter(prefix="/emb")


@emb.get("/v1/models")
async def emb_models():
    return _models_payload(Config.embed_model, Config.embed_max_len)


@emb.post("/tokenize")
async def emb_tokenize(body: dict):
    _require_ready()
    ids = Models.embedder.tokenizer.encode(body.get("prompt", ""), add_special_tokens=False)
    return {"tokens": ids, "count": len(ids), "max_model_len": Config.embed_max_len}


@emb.post("/v1/embeddings")
async def embeddings(body: dict):
    _require_ready()
    texts = body.get("input", [])
    if isinstance(texts, str):
        texts = [texts]
    max_tokens = min(Config.embed_max_len, int(body.get("truncate_prompt_tokens") or Config.embed_max_len) + 1)

    def _encode():
        with Models.lock:
            Models.embedder.max_seq_length = max_tokens
            return Models.embedder.encode(texts, convert_to_numpy=True, show_progress_bar=False)

    vectors = await asyncio.to_thread(_encode)
    return {
        "object": "list",
        "model": Config.embed_model,
        "data": [{"object": "embedding", "index": i, "embedding": v.tolist()} for i, v in enumerate(vectors)],
        "usage": {"prompt_tokens": 0, "total_tokens": 0},
    }


# --------------------------------------------------------------------------- #
# Reranker: /rerank  (Cohere v2 compatible)
# --------------------------------------------------------------------------- #
rerank = APIRouter(prefix="/rerank")


@rerank.get("/v1/models")
async def rerank_models():
    return _models_payload(Config.rerank_model, Config.rerank_max_len)


@rerank.post("/v2/rerank")
@rerank.post("/v1/rerank")
async def rerank_documents(body: dict):
    _require_ready()
    query = body.get("query", "")
    documents = [d if isinstance(d, str) else d.get("text", "") for d in body.get("documents", [])]
    top_n = body.get("top_n") or len(documents)

    def _score():
        with Models.lock:
            if not documents:
                return []
            return Models.reranker.predict([(query, d) for d in documents], show_progress_bar=False)

    scores = await asyncio.to_thread(_score)
    ranked = sorted(
        ({"index": i, "relevance_score": float(s)} for i, s in enumerate(scores)),
        key=lambda r: r["relevance_score"],
        reverse=True,
    )[:top_n]
    return {"id": str(uuid.uuid4()), "results": ranked, "meta": {"api_version": {"version": "2"}}}


# --------------------------------------------------------------------------- #
# LLM: /llm  (watsonx.ai through LiteLLM)
# --------------------------------------------------------------------------- #
llm = APIRouter(prefix="/llm")

# Request fields the services send that we forward as-is to LiteLLM.
_LLM_PASSTHROUGH = {
    "messages", "max_tokens", "temperature", "top_p", "stop", "stream",
    "stream_options", "frequency_penalty", "presence_penalty", "seed", "response_format",
}


@llm.get("/v1/models")
async def llm_models():
    return _models_payload(Config.llm_model, Config.llm_max_len)


@llm.post("/tokenize")
async def llm_tokenize(body: dict):
    _require_ready()
    ids = Models.llm_tokenizer.encode(body.get("prompt", ""), add_special_tokens=False)
    return {"tokens": ids, "count": len(ids), "max_model_len": Config.llm_max_len}


@llm.post("/v1/chat/completions")
async def chat_completions(request: Request):
    import litellm

    if not (Config.watsonx_url and Config.watsonx_api_key and Config.watsonx_project_id):
        raise HTTPException(status_code=503, detail="WATSONX_URL, WATSONX_APIKEY and WATSONX_PROJECT_ID must be set")

    body = await request.json()
    kwargs = {k: v for k, v in body.items() if k in _LLM_PASSTHROUGH}
    stream = bool(kwargs.get("stream"))
    if not stream:
        kwargs.pop("stream_options", None)

    common = dict(
        model=f"watsonx/{Config.llm_model}",
        api_base=Config.watsonx_url,
        api_key=Config.watsonx_api_key,
        project_id=Config.watsonx_project_id,
        drop_params=True,
        **kwargs,
    )

    if not stream:
        try:
            resp = await litellm.acompletion(**common)
        except Exception as e:
            log.exception("watsonx chat completion failed")
            raise HTTPException(status_code=getattr(e, "status_code", 502) or 502, detail=str(e))
        return JSONResponse(resp.model_dump())

    async def _sse():
        try:
            stream_resp = await litellm.acompletion(**common)
            async for chunk in stream_resp:
                yield f"data: {chunk.model_dump_json()}\n\n"
        except Exception as e:
            log.exception("watsonx streaming chat completion failed")
            yield f"data: {json.dumps({'error': {'message': str(e)}})}\n\n"
        yield "data: [DONE]\n\n"

    return StreamingResponse(_sse(), media_type="text/event-stream")


app.include_router(emb)
app.include_router(rerank)
app.include_router(llm)


if __name__ == "__main__":
    uvicorn.run(app, host="127.0.0.1", port=Config.port, log_level="info")
