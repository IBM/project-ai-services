# AI Services mock server

A self-contained, in-memory mock of every REST API in this repository: the Go catalog API server and the Python services (digitize, chatbot, summarize, extract, similarity, forecast, translate). Use it to develop and test UIs, CLIs and integrations without GPUs, Spyre cards, vLLM, OpenSearch or PostgreSQL.

Request and response shapes, status codes and error bodies follow the service code in [`services/`](../services) and [`ai-services/internal/pkg/catalog/apiserver`](../ai-services/internal/pkg/catalog/apiserver). The public API reference is the [IBM AI Services developer guide](https://www.ibm.com/docs/en/aiservices/2026.09.0?topic=guide-services).

## Build

Requires JDK 8 or later and Maven 3.6 or later.

```sh
cd mock
mvn package
```

The build produces a single runnable jar: `target/ai-services-mock-jar-with-dependencies.jar`. Its only dependency is Gson, which is bundled into the jar. Bytecode targets Java 8. On JDK 9 and later the build compiles with `--release 8`, so APIs newer than Java 8 are rejected at compile time.

## Run

```sh
java -jar target/ai-services-mock-jar-with-dependencies.jar
```

Each service listens on its own port, as in a real deployment:

| Service      | Default port | Mocks                                        |
|--------------|--------------|----------------------------------------------|
| `catalog`    | 8080         | Catalog API server (`/api/v1/...`)           |
| `digitize`   | 4000         | Digitize documents                           |
| `chatbot`    | 5000         | Question and answer (RAG chat completions)   |
| `summarize`  | 6000         | Summarize                                    |
| `extract`    | **6001**     | Extract information                          |
| `similarity` | 7000         | Find similar items                           |
| `forecast`   | 8500         | Forecast                                     |
| `translate`  | 9000         | Translate                                    |

The real extract service also defaults to port 6000, which collides with summarize. The mock therefore puts extract on 6001. Use `--extract-port 6000 --services extract` if you need the real port.

### Options

| Option                      | Default   | Description                                                         |
|-----------------------------|-----------|---------------------------------------------------------------------|
| `--host <address>`          | `0.0.0.0` | Bind address for all services                                       |
| `--services <a,b,...>`      | all       | Start only the listed services                                      |
| `--<service>-port <port>`   |           | Override one service's port, e.g. `--catalog-port 18080`            |
| `--job-duration-ms <ms>`    | `6000`    | How long a simulated job, deployment or sync takes; `0` is instant  |
| `--stream-delay-ms <ms>`    | `40`      | Delay between streamed SSE chunks                                   |
| `--catalog-user <name>`     | `admin`   | User name accepted by `POST /api/v1/auth/login`                     |
| `--catalog-password <pass>` | `admin`   | Password accepted by `POST /api/v1/auth/login`                      |
| `--catalog-runtime <rt>`    | `podman`  | `podman` or `openshift`; changes runtime-specific catalog behaviour |

## Behaviour

* **State is in memory.** Everything you create is kept until the process exits.
* **Asynchronous work is simulated from elapsed time.** Jobs report their initial status (for example `accepted` or `Downloading`) for the first 15% of `--job-duration-ms`, then an in-progress status, then their final status. Result endpoints return `202` until a job finishes, as the real services do.
* **Failures on demand.** Any job name, uploaded file name, application name, model name or datasource name containing `fail` (case-insensitive) produces a failed outcome, so clients can exercise error paths. For example, uploading `report-fail.pdf` to digitize gives a document with status `failed`.
* **Generated content.** Summaries, translations, chat answers and extractions are derived from the input deterministically; no model is called. Text is read from `.txt`, `.md` and `.html` uploads. Binary formats such as PDF and DOCX are not parsed; placeholder text scaled to the file size is used instead.
* **Errors match each service's format.** The Python services use the `{"error": {"code", "message", "status"}}` envelope. Their request validation errors use FastAPI's `{"detail": [...]}` body. The catalog server uses `{"error": "..."}`, and unknown routes return gin's plain-text `404 page not found`.
* **Every response** carries an `X-Request-ID` header. An incoming `X-Request-ID` is echoed back; otherwise one is generated. Permissive CORS headers are included so browser UIs on other origins can call the mock.
* **API docs.** Each Python service serves Swagger UI at `/` (except forecast) and `/docs`, and a generated OpenAPI document at `/openapi.json`. The catalog server serves Swagger UI under `/swagger/` and its document at `/swagger/doc.json`. These generated documents list the mocked operations and their path parameters. They do not include schemas.

### Catalog API server authentication

```sh
curl -s -X POST localhost:8080/api/v1/auth/login -d '{"username":"admin","password":"admin"}'
# {"access_token":"eyJ...","refresh_token":"eyJ...","token_type":"Bearer"}
curl -s localhost:8080/api/v1/architectures -H "Authorization: Bearer <access_token>"
```

The tokens are HS256 JWTs signed with a key generated at startup. Access tokens expire after 15 minutes and refresh tokens after 24 hours. Refresh rotates the refresh token, and logout revokes tokens, as on the real server. Authenticated responses carry `X-Token-Exp`. `POST /api/v1/auth/token` accepts any non-empty bearer token in place of a ManageIQ token.

Running applications report `endpoints` that point at the mock services. For example, a deployed `digitize` service reports `http://localhost:4000`.

## Mocked endpoints

### Catalog API server (`catalog`, port 8080)

Everything under `/api/v1` except `auth/login`, `auth/token` and `auth/refresh` requires a bearer token.

* `GET /health`, `GET /healthz`, `GET /swagger/*`
* Auth: `POST /api/v1/auth/login`, `POST /api/v1/auth/token`, `POST /api/v1/auth/refresh`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/me`
* Catalog:
  * `GET /api/v1/resources`
  * `GET /api/v1/architectures`, `GET /api/v1/architectures/{id}`, plus `/deploy-options`, `/images` and `/models` under it
  * `GET /api/v1/services`, `GET /api/v1/services/{id}`, plus `/deploy-options`, `/params`, `/images`, `/models` and `/steps` under it
  * `GET /api/v1/catalog/models`, `GET /api/v1/catalog/models/{id}`, `GET /api/v1/catalog/models/{id}/deploy-options`
  * `GET /api/v1/components/{component_type}/providers/{provider_id}/params`
  * `GET /api/v1/connectors`, `GET /api/v1/connectors/{connector_type}/providers/{provider_id}/params`
* Bundles: `POST /api/v1/catalog/bundles`, `POST /api/v1/catalog/bundles/validate`, `GET /api/v1/catalog/bundles`, `GET`, `PUT` and `DELETE /api/v1/catalog/bundles/{id}`. Uploads are `.tar.gz` archives with a top-level `metadata.yaml` that defines `id`, `version` and `type`.
* Applications:
  * `GET` and `POST /api/v1/applications`, with or without a trailing slash
  * `GET`, `PUT` and `DELETE /api/v1/applications/{id}`
  * `GET /api/v1/applications/{id}/resources`, `GET /api/v1/applications/{id}/ps`
  * `GET` and `PUT /api/v1/applications/{id}/datasources`
  * `GET` and `DELETE /api/v1/applications/{id}/datasources/{datasource_id}`
* Workers: `POST` and `GET /api/v1/workers`, `GET` and `DELETE /api/v1/workers/{id}`
* Datasources: `POST` and `GET /api/v1/datasources`, `GET`, `PUT` and `DELETE /api/v1/datasources/{id}`, `GET /api/v1/datasources/{id}/applications`
* Models: `POST` and `GET /api/v1/models`, `GET` and `DELETE /api/v1/models/{id}`, `POST /api/v1/models/{id}/keys`

### Digitize (`digitize`, port 4000)

* `GET /health`
* Jobs:
  * `POST /v1/jobs`: multipart `files`; query `operation`, `output_format` and `job_name`
  * `GET /v1/jobs`
  * `GET` and `DELETE /v1/jobs/{job_id}`
  * `POST /v1/jobs/{job_id}/cancel`
* Documents:
  * `GET /v1/documents`
  * `DELETE /v1/documents?confirm=true`
  * `GET` and `DELETE /v1/documents/{doc_id}`
  * `GET /v1/documents/{doc_id}/content`
* Admin: `POST /v1/import`, `GET /v1/export`, `GET /v1/conversion-tasks`
* Connectors (hidden from the real service's OpenAPI schema):
  * `POST` and `GET /v1/connectors`
  * `GET`, `PUT` and `DELETE /v1/connectors/{connector_id}`
  * `POST` and `GET /v1/connectors/{connector_id}/syncs`
  * `GET /v1/connectors/{connector_id}/syncs/{sync_seq}`
  * `POST /v1/connectors/{connector_id}/syncs/{sync_seq}/stop`

Re-uploading content that is already ingested is reported as a duplicate (`already_exists` or `skipped`), as in the real service.

### Question and answer (`chatbot`, port 5000)

* `GET /health`, `GET /db-status`
* `GET /v1/models`
* `GET /v1/perf_metrics`
* `POST /v1/chat/completions`: query `include_docs`; set `"stream": true` for OpenAI-style `chat.completion.chunk` server-sent events

### Summarize (`summarize`, port 6000)

* `GET /health`
* `POST /v1/summarize`: JSON `{"text": ...}` or multipart `file`; query `level`, `length` and `stream`
* `POST`, `GET` and `DELETE /v1/summarize/jobs`
* `GET` and `DELETE /v1/summarize/jobs/{job_id}`
* `GET /v1/summarize/jobs/{job_id}/result`

### Extract information (`extract`, port 6001)

* `GET /health`
* Schemas: `POST`, `GET` and `DELETE /v1/schemas`, `GET` and `DELETE /v1/schemas/{schema_id}`
* `POST /v1/extract`
* Jobs:
  * `POST`, `GET` and `DELETE /v1/extract/jobs`
  * `GET` and `DELETE /v1/extract/jobs/{job_id}`
  * `GET /v1/extract/jobs/{job_id}/results/{doc_id}`
  * `GET /v1/extract/jobs/{job_id}/results/{doc_id}/download`

### Find similar items (`similarity`, port 7000)

* `GET /health`
* `POST /v1/similarity-search`: searches a small fixed corpus

### Forecast (`forecast`, port 8500)

* `GET /health`. This is the only route the real service exposes so far.

### Translate (`translate`, port 9000)

Supported languages are `english` and `german`.

* `GET /health`
* `POST /v1/translate`
* `POST` and `GET /v1/translate/jobs`
* `GET /v1/translate/jobs/{job_id}`
* `GET /v1/translate/jobs/{job_id}/result`
* `GET /v1/translate/jobs/{job_id}/result/download`

## Code layout

* `com.ibm.aiservices.mock`: entry point (`MockServer`) and configuration.
* `com.ibm.aiservices.mock.http`: a small router on the JDK's built-in `com.sun.net.httpserver`. It handles path parameters, multipart parsing, SSE streaming and pluggable error formats.
* `com.ibm.aiservices.mock.service.common`: FastAPI conventions shared by the Python services.
* `com.ibm.aiservices.mock.service.<name>`: one package per mocked service.

The code follows the [Mapepire Java coding conventions](https://raw.githubusercontent.com/Mapepire-IBMi/mapepire-server/refs/heads/main/CODING_CONVENTIONS_JAVA.md).
