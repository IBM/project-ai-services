# Forecast on macOS with Tilt

[`hack/tilt/services/forecast/Tiltfile`](../../hack/tilt/services/forecast/Tiltfile) runs the **forecast** service locally. It sits next to the [extract / translate / summarize Tiltfile](tilt-services-local-development.md) but is separate, because forecast has no LLM and needs a restricted worker database role and (optionally) Triton. Design: [forecast service proposal](../proposals/forecast_service_proposal.md).

> **Local development only.** The PostgreSQL password, worker password and `DB_ENCRYPTION_KEY` below are publicly documented defaults.

## Usage

```bash
tilt up -f hack/tilt/services/forecast/Tiltfile    # PostgreSQL, Triton and the API
tilt down -f hack/tilt/services/forecast/Tiltfile
```

Swagger UI: http://localhost:8500/docs (the connector routes are hidden from the schema in the current code, but they are served under `/v1/connectors`).

| Resource | What it does |
|----------|--------------|
| `python-env` | Creates `hack/tilt/services/forecast/.venv` from [`requirements.txt`](../../hack/tilt/services/forecast/requirements.txt). Python 3.10+. |
| `postgres` | PostgreSQL in Podman, port `5437`, database `forecast_metadata`. |
| `forecast-db-init` | Runs `services/forecast/db/scripts/init_db.py`: database, schema and the restricted `forecast_worker` role (idempotent). |
| `triton` | Triton in Podman with `--model-control-mode=explicit`, reading `.tilt/services/forecast/models` read-only. |
| `forecast-api` | `uvicorn forecast.app:app --reload` from source (edits to `forecast` or `common` reload it). |

## What is and is not covered

The Tiltfile follows what is merged: API, connectors, database/init, encryption and the `TRITON_URL` check. The training worker, scheduler, data loader, Triton load/unload and the forecast endpoints are still WIP, so:

- Triton is always started, and `forecast-api` waits for it. The API only validates `TRITON_URL` at startup today; it does not call Triton yet. The first `tilt up` pulls a multi-GB image.
- The venv has no AutoGluon/pandas/tritonclient/APScheduler. When the worker lands, add them from `services/forecast/requirements.txt` to the Tiltfile's `requirements.txt`.
- No model server and no LLM: the service does not use one.
- The proposal's Triton image (`quay.io/powercloud/tritonserver`) is ppc64le. Locally the default is `nvcr.io/nvidia/tritonserver:24.08-py3`, which is multi-arch, so Apple Silicon pulls the native arm64 build.

## Options

`tilt up -f hack/tilt/services/forecast/Tiltfile -- --forecast_port=8501`

| Option | Default |
|--------|---------|
| `forecast_port` | `8500` |
| `postgres_port` / `postgres_image` / `postgres_password` | `5437` / `docker.io/library/postgres:18-alpine` / `Aiservicesdb@1234` |
| `worker_postgres_user` / `worker_postgres_password` | `forecast_worker` / `ForecastWorker@1234` |
| `triton_image` / `triton_platform` / `triton_port` | `nvcr.io/nvidia/tritonserver:24.08-py3` / unset (native) / `8000` |
| `python_bin` | first of `python3.12` … `python3` |

`DB_ENCRYPTION_KEY` is read from the environment (exactly 32 bytes, used as is); default `forecast-local-dev-key-012345678`. Changing it makes stored connector secrets unreadable.

Runtime data (model repository, scratch, CSV data dir) is under `.tilt/services/forecast`; `tilt down` removes it, the containers and the volume, and keeps the venv.

## Troubleshooting

| Symptom | Cause / fix |
|---------|-------------|
| `forecast-api` fails with `DB_ENCRYPTION_KEY must be exactly 32 bytes` | The environment variable is not 32 bytes. |
| `postgres` not ready | Podman machine not running (`podman machine start`). |
| `triton` not ready | First start needs the multi-GB image (`podman pull nvcr.io/nvidia/tritonserver:24.08-py3`); check its log. |
| `tilt down` refuses to run | Tilt's kube-context guard; see the note in [tilt-services-local-development.md](tilt-services-local-development.md#troubleshooting). |
