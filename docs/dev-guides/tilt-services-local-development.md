# Extract, Translate, Summarize and Forecast on macOS with Tilt

The Tiltfile in [`hack/tilt/services`](../../hack/tilt/services) runs the **extract**, **translate**, **summarize** and **forecast** services locally, each one on its own with *only* its dependencies. They are standalone: they do not use the catalog's Tiltfile and do not need the RAG stack.

> **Local development only.** The PostgreSQL password is a publicly documented default. Never use this setup on a shared environment.

## Usage

One Tiltfile, [`hack/tilt/services/Tiltfile`](../../hack/tilt/services/Tiltfile). Pick the service with the `--service` option (`extract`, `translate`, `summarize` or `forecast`):

```bash
tilt up -f hack/tilt/services/Tiltfile -- --service=extract
tilt up -f hack/tilt/services/Tiltfile -- --service=translate
tilt up -f hack/tilt/services/Tiltfile -- --service=summarize
tilt up -f hack/tilt/services/Tiltfile -- --service=forecast
```

Extract, translate and summarize start PostgreSQL, the model server and `<service>-api` only. Forecast has no LLM, so it starts PostgreSQL, Triton and `forecast-api` instead (see [Forecast](#forecast)). A missing or unknown service fails with a message listing the valid names.

For the other stacks see [Catalog](tilt-local-development.md) (`Tiltfile`) and [RAG](tilt-rag-local-development.md) (`Tiltfile.rag`: chatbot, similarity, digitize).

## Why these services are lighter than RAG

The three services only call an LLM (chat completions plus `/tokenize`) and store job metadata in PostgreSQL. They do **not** use OpenSearch, the embedding model, the reranker or a UI. So, compared with [`Tiltfile.rag`](../../Tiltfile.rag), there is no OpenSearch container, no torch/sentence-transformers/Docling install and no multi-GB model download.

| Resource | What it does |
|----------|--------------|
| `python-env` | Creates `hack/tilt/services/.venv` from [`requirements.txt`](../../hack/tilt/services/requirements.txt), shared by every service. Needs Python 3.10 or newer; created once. |
| `postgres` | PostgreSQL in Podman, one database per service (`extract_metadata`, `translate_metadata`, `summarize_metadata`, `forecast_metadata`). |
| `model-server` | Extract / translate / summarize only. [`hack/tilt/model_server.py`](../../hack/tilt/model_server.py) with `MODEL_SERVER_MODELS=llm`: serves `/llm` only (chat completions through Ollama or watsonx.ai, plus a local `/tokenize`). |
| `<service>-db-init` | Creates the service database and schema (idempotent). For forecast it also creates the restricted `forecast_worker` role. |
| `<service>-api` | The service's FastAPI app from [`services/`](../../services), run with `uvicorn --reload` (edits to the service or `services/common` reload it). |
| `triton` | Forecast only. See [Forecast](#forecast). |
| `quick-links` | URLs of the APIs. |

Environment variables passed to the services match the `rag-dev` pod templates (`LLM_ENDPOINT`, `LLM_MODEL`, `LLM_MAX_MODEL_LEN`, `POSTGRES_*`, `DB_POOL_SIZE`, `PORT`).

### Layout

```
hack/tilt/
├── model_server.py          # shared with Tiltfile.rag (LLM-only mode added)
└── services/
    ├── Tiltfile             # --service=extract|translate|summarize|forecast
    └── requirements.txt     # one lean venv for all services (no torch / docling / OpenSearch client)
```

## Prerequisites

- [Tilt](https://docs.tilt.dev/install.html) and Python 3.10+ (3.12 recommended; the macOS system `python3` 3.9 is too old)
- Podman with a running machine (`podman machine start`)
- Extract / translate / summarize: one LLM provider:
  - **Ollama** (default): running on the host with the model pulled: `ollama pull granite4.1:8b`
  - **watsonx**: a watsonx.ai API key, URL and project ID

Forecast needs no LLM provider. No Node.js is needed.

## Run

With Ollama (default):

```bash
tilt up -f hack/tilt/services/Tiltfile -- --service=extract
```

With watsonx.ai:

```bash
export WATSONX_APIKEY=<your key>          # read from the environment only, never a flag
export WATSONX_URL=https://us-south.ml.cloud.ibm.com
export WATSONX_PROJECT_ID=<your project id>

tilt up -f hack/tilt/services/Tiltfile -- --service=translate --llm_provider=watsonx
```

In the Tilt UI (http://localhost:10350) wait for the resources to turn green, then open the Swagger UI of the service (served at `/`):

| Service | Default URL |
|---------|-------------|
| Extract | http://localhost:9500 |
| Translate | http://localhost:9000 |
| Summarize | http://localhost:6100 |
| Forecast | http://localhost:8500/docs |

Stop and clean up with `tilt down -f hack/tilt/services/Tiltfile -- --service=<name>`. This removes the PostgreSQL container, its data volume and the service caches (`.tilt/services/<name>`), but keeps `hack/tilt/services/.venv`.

### Running several at once

Every service has its own default ports, PostgreSQL container/volume and cache directory, so they can run side by side. Give each `tilt up` its own Tilt UI port:

```bash
tilt up -f hack/tilt/services/Tiltfile --port 10351 -- --service=extract
tilt up -f hack/tilt/services/Tiltfile --port 10352 -- --service=translate
```

Each of these starts its own PostgreSQL and model server; the model server loads only a tokenizer, so this is cheap.

## Options

Pass any option after `--`, for example `tilt up -f hack/tilt/services/Tiltfile -- --service=extract --extract_port=9501`.

| Option | Default |
|--------|---------|
| `service` | required (`extract`, `translate`, `summarize` or `forecast`) |
| `llm_provider` | `$LLM_PROVIDER`, else `ollama` (`ollama` or `watsonx`) |
| `ollama_url` | `$OLLAMA_URL`, else `http://127.0.0.1:11434/v1` |
| `ollama_model` | `$OLLAMA_MODEL`, else `granite4.1:8b` |
| `watsonx_url` / `watsonx_project_id` | `$WATSONX_URL` / `$WATSONX_PROJECT_ID` |
| `watsonx_model` | `$WATSONX_MODEL`, else `ibm/granite-4-h-small` |
| `llm_max_model_len` | `26208` |
| `model_server_port` | `8200` (extract), `8201` (translate), `8202` (summarize); not used by forecast |
| `extract_port` / `translate_port` / `summarize_port` / `forecast_port` | `9500` / `9000` / `6100` / `8500` (only the option of the selected service exists) |
| `postgres_image` | `docker.io/library/postgres:16-alpine` |
| `postgres_port` | `5434` (extract), `5435` (translate), `5436` (summarize), `5437` (forecast) |
| `postgres_password` | `Aiservicesdb@1234` |
| `python_bin` | first of `python3.12`, `python3.13`, `python3.11`, `python3.10`, `python3` |

Default ports avoid 5432 (catalog Tiltfile), 5433/8100/9200/3000/4000/4001/5050/7070 (`Tiltfile.rag`) and 5000/7000 (macOS AirPlay Receiver), so every Tiltfile in the repo can run together.

## Forecast

`--service=forecast` runs the forecast service (design: [forecast service proposal](../proposals/forecast_service_proposal.md)). It has no LLM and no model server; it needs a restricted worker database role and Triton:

| Resource | What it does |
|----------|--------------|
| `triton` | Triton in Podman with `--model-control-mode=explicit`, reading `.tilt/services/forecast/models` read-only. `forecast-api` waits for it. |
| `forecast-db-init` | Runs `services/forecast/db/scripts/init_db.py`: database, schema and the restricted `forecast_worker` role. |

Swagger UI is at http://localhost:8500/docs (the connector routes are hidden from the schema in the current code, but are served under `/v1/connectors`).

- **Triton image.** The proposal's `quay.io/powercloud/tritonserver` is ppc64le only. Locally the default is `nvcr.io/nvidia/tritonserver:24.08-py3`, which is multi-arch, so Apple Silicon pulls the native arm64 build (about 13 GB on first `tilt up`).
- **Covered.** API, connectors, database/init, encryption and the `TRITON_URL` check. The API does not call Triton yet; the training worker, scheduler, data loader, Triton load/unload and the forecast endpoints are WIP.
- **Venv.** The shared `requirements.txt` has only forecast's `cryptography` so far, no AutoGluon, pandas, tritonclient or APScheduler yet. Add them from `services/forecast/requirements.txt` when the worker lands.
- **Encryption key.** `DB_ENCRYPTION_KEY` is read from the environment (exactly 32 bytes, used as is); default `forecast-local-dev-key-012345678`. Changing it makes stored connector secrets unreadable.
- **Data.** Model repository, scratch and CSV data dir live under `.tilt/services/forecast`; `tilt down` removes them and both containers, and keeps the venv.

Forecast-only options:

| Option | Default |
|--------|---------|
| `worker_postgres_user` / `worker_postgres_password` | `forecast_worker` / `ForecastWorker@1234` |
| `triton_image` / `triton_platform` / `triton_port` | `nvcr.io/nvidia/tritonserver:24.08-py3` / unset (native) / `8000` |

## Limitations

- Does not exercise the Podman or OpenShift deployment path or the `ppc64le` images.
- The LLM is Ollama or watsonx.ai, not vLLM. The extract service sends `guided_json` (vLLM guided decoding) in `extra_body`; the model server forwards only standard OpenAI fields, so locally the model is not constrained to the schema and relies on the service's validate-and-retry. Extraction quality depends on the model.
- Japanese sentence splitting needs the `ja_core_news_sm` spaCy model, which is not installed.

## Troubleshooting

| Symptom | Cause / fix |
|---------|-------------|
| `python-env`: `Python >= 3.10 is required` | Pass `--python_bin=python3.12` (or install it: `brew install python@3.12`). |
| `Address already in use` or readiness probe `403` | Another process holds the port. Change it with `--<service>_port`, `--postgres_port` or `--model_server_port`. |
| `<service>-api` exits at startup, `The AI service is unreachable` / connection refused to the LLM | `model-server` is not ready or Ollama is not running (`curl http://127.0.0.1:11434/v1/models`, `ollama list`). |
| `model-server` stays not ready | It is downloading the LLM tokenizer (`ibm-granite/granite-4.1-8b`) from Hugging Face on first start; check its log. |
| LLM calls return 503 with `watsonx` | `WATSONX_APIKEY`, `WATSONX_URL` or `WATSONX_PROJECT_ID` is not set in the environment that started Tilt. |
| `postgres` not ready | Check that the Podman machine is running (`podman machine list`). |
| `tilt down` fails with `Refusing to run 'local' because ... might be a production kube context` | Tilt's safety guard looks at your current kube context even though these Tiltfiles use no Kubernetes. Switch to a local context (`kubectl config use-context <local>`), or remove the leftovers by hand: `podman rm -f ai-svc-<name>-postgres (forecast also ai-svc-forecast-triton); podman volume rm ai-svc-<name>-postgres-data; rm -rf .tilt/services/<name>`. The root `Tiltfile` and `Tiltfile.rag` behave the same way. |
| `forecast-api` fails with `DB_ENCRYPTION_KEY must be exactly 32 bytes` | The `DB_ENCRYPTION_KEY` environment variable is not 32 bytes. |
| `triton` not ready | First start pulls a multi-GB image (`podman pull nvcr.io/nvidia/tritonserver:24.08-py3`); check its log. |
| A service crashed on a missing module | Add it to `hack/tilt/services/requirements.txt`; `python-env` reinstalls on change, then trigger the crashed resource. |
