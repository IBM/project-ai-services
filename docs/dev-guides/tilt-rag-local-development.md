# RAG on macOS with Tilt

[`Tiltfile.rag`](../../Tiltfile.rag) runs the RAG stack (chatbot, similarity search, chatbot UI, digitize) locally. It is **standalone**: it does not use the catalog's database, backend or UI, and it does not need the root [`Tiltfile`](../../Tiltfile).

> **Local development only.** The OpenSearch password is a publicly documented default. Never use this setup on a shared environment.

## Why it is different from the container deployment

The RAG images are built for `linux/ppc64le`, so they cannot run on Apple Silicon. `Tiltfile.rag` instead runs the repo's services natively from source and replaces the three vLLM containers with a small local model server.

| Resource | What it does |
|----------|--------------|
| `rag-python-env` | Creates `hack/tilt/.venv` and installs [`hack/tilt/requirements.txt`](../../hack/tilt/requirements.txt). The first run is slow (torch). |
| `rag-opensearch` | OpenSearch in Podman (multi-arch image, 1 GB heap). |
| `rag-model-server` | [`hack/tilt/model_server.py`](../../hack/tilt/model_server.py): serves the vLLM-style endpoints the services expect, on one port. |
| `rag-similarity`, `rag-chatbot` | The repo's FastAPI services, run with `uvicorn --reload`. |
| `rag-chatbot-ui` | The chatbot UI via Vite, using [`ui/chatbot/vite.tilt.config.js`](../../ui/chatbot/vite.tilt.config.js). |
| `rag-digitize-db`, `rag-digitize-db-init` | A dedicated PostgreSQL in Podman (port 5433) and the digitize schema. |
| `rag-digitize` | The digitize API (PDF/DOCX ingestion into OpenSearch), run with `uvicorn --reload`. |
| `rag-digitize-ui` | The digitize UI via Vite (`ui/digitize`), proxying `/v1` to the API. |
| `rag-ingest` | Manual trigger. Indexes `.txt`/`.md` files into OpenSearch (quick alternative to digitize). |

### Model server

| Path prefix | Backed by |
|-------------|-----------|
| `/llm` | Chat completions through LiteLLM to the provider selected with `llm_provider`: a local Ollama instance (default, via its OpenAI-compatible `/v1` API) or IBM watsonx.ai. Plus a local `/tokenize` using a Hugging Face tokenizer. |
| `/emb` | `ibm-granite/granite-embedding-278m-multilingual` (sentence-transformers, on MPS/CPU). |
| `/rerank` | `BAAI/bge-reranker-v2-m3` (CrossEncoder), Cohere-compatible `/v2/rerank`. |

The embedding and reranker models are the same ones the production templates use. They download from the Hugging Face Hub on first start (about 3 GB).

## Prerequisites

- [Tilt](https://docs.tilt.dev/install.html), Python 3.12, Node.js/npm
- Podman with a running machine (`podman machine start`). Give it at least 6-8 GB: `podman machine set --memory 8192`
- One LLM provider:
  - **Ollama** (default): [Ollama](https://ollama.com) running on the host with the model pulled: `ollama pull granite4.1:8b`
  - **watsonx**: a watsonx.ai API key, URL and project ID

## Run

With Ollama (default):

```bash
tilt up -f Tiltfile.rag
```

With watsonx.ai:

```bash
export WATSONX_APIKEY=<your key>          # read from the environment only, never a flag
export WATSONX_URL=https://us-south.ml.cloud.ibm.com
export WATSONX_PROJECT_ID=<your project id>

tilt up -f Tiltfile.rag -- --llm_provider=watsonx
```

In the Tilt UI (http://localhost:10350), wait for the resources to turn green, trigger `rag-ingest` to index the sample document in [`hack/tilt/sample-docs`](../../hack/tilt/sample-docs), then open http://localhost:3000. To ingest PDF/DOCX files, open the digitize UI at http://localhost:4001 (API/Swagger at http://localhost:4000); documents indexed there are searchable from the chatbot. The first PDF conversion downloads the Docling layout/table models from Hugging Face.

Stop and clean up with `tilt down -f Tiltfile.rag`. This removes the OpenSearch and digitize PostgreSQL containers, their data volumes and `hack/tilt/.data` (digitize cache), but keeps `hack/tilt/.venv`.

If you run the catalog Tiltfile at the same time, give one of them a different Tilt UI port: `tilt up -f Tiltfile.rag --port 10351`.

## Options

Pass any option after `--`, for example `tilt up -f Tiltfile.rag -- --rag_ui_port=3001`.

| Option | Default |
|--------|---------|
| `llm_provider` | `$LLM_PROVIDER`, else `ollama` (`ollama` or `watsonx`) |
| `ollama_url` | `$OLLAMA_URL`, else `http://127.0.0.1:11434/v1` |
| `ollama_model` | `$OLLAMA_MODEL`, else `granite4.1:8b` |
| `watsonx_url` / `watsonx_project_id` | `$WATSONX_URL` / `$WATSONX_PROJECT_ID` |
| `watsonx_model` | `$WATSONX_MODEL`, else `ibm/granite-4-h-small` |
| `model_server_port` | `8100` |
| `rag_chatbot_port` / `rag_similarity_port` / `rag_ui_port` | `5050` / `7070` / `3000` |
| `opensearch_image` / `opensearch_port` / `opensearch_password` | `opensearchproject/opensearch:3.2.0` / `9200` / `AiServices@12345` |
| `digitize_port` / `digitize_ui_port` | `4000` / `4001` |
| `digitize_db_image` / `digitize_db_port` / `digitize_db_password` | `postgres:16-alpine` / `5433` / `Aiservicesdb@1234` |
| `rag_docs_dir` | `hack/tilt/sample-docs` |
| `python_bin` | `python3` |

Ports 5000 and 7000 are avoided on purpose: the macOS AirPlay Receiver listens on both.

## Limitations

- Does not exercise the Podman or OpenShift deployment path.
- `rag-ingest` handles plain text and Markdown only; use digitize for PDF/DOCX. Digitize runs Docling on CPU/MPS and is slow on large PDFs; its table summarisation calls the selected LLM.
- Japanese sentence splitting needs the `ja_core_news_sm` spaCy model, which is not installed.

## Troubleshooting

| Symptom | Cause / fix |
|---------|-------------|
| `Address already in use` or readiness probe `403` | Another process holds the port. Change it with `--rag_chatbot_port` / `--rag_similarity_port`. |
| `rag-model-server` stays not ready | It is still downloading or loading models. `/health` returns `loading` until done; check its log. |
| Chat returns `LLM_UNAVAILABLE` (503) | With `ollama`: Ollama is not running or the model is not pulled (check `curl http://127.0.0.1:11434/v1/models` and `ollama list`). With `watsonx`: `WATSONX_APIKEY`, `WATSONX_URL` or `WATSONX_PROJECT_ID` is not set in the environment that started Tilt. |
| `rag-opensearch` not ready | Give the Podman machine more memory (`podman machine set --memory 8192`, then restart it). |
| A service crashed on a missing module | Add it to `hack/tilt/requirements.txt`; `rag-python-env` reinstalls on change, then trigger the crashed resource. |
