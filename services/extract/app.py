"""
Extract Information Service — FastAPI application.
"""

import asyncio
import uuid
from contextlib import asynccontextmanager
from typing import Optional

import uvicorn
from fastapi import FastAPI, Query, Request
from fastapi.openapi.docs import get_swagger_ui_html
from fastapi.responses import JSONResponse, Response
from sqlalchemy.exc import IntegrityError

from common.misc_utils import configure_uvicorn_logging, create_llm_session, get_llm_endpoint, get_logger, set_log_level, set_request_id
from common.diagnostic_logger import setup_comprehensive_crash_handler
from common.error_utils import http_error_responses

from extract.db.connection import check_db_connection, close_db_connections
from extract.db.manager import db_repo
from extract.models import (
    PaginationInfo,
    SchemaCreatedResponse,
    SchemaDetailResponse,
    SchemaListItem,
    SchemaListResponse,
    SchemaRegisterRequest,
)
from extract.schema_utils import (
    SchemaValidationError,
    check_schema_share_in_context,
    compute_token_counts,
    normalize_schema,
    validate_examples,
    validate_json_schema_structure
)
from extract.settings import settings

set_log_level(settings.common.app.log_level)

logger = get_logger("app")

diagnostic_logger, stderr_monitor, signal_handler = setup_comprehensive_crash_handler(logger)

# Global vLLM concurrency limiter (shared by sync + async extraction paths).
concurrency_limiter = asyncio.BoundedSemaphore(settings.common.llm.max_batch_size)

# Async job admission semaphore (caps background workers).
job_limiter = asyncio.BoundedSemaphore(settings.extract.max_concurrent_jobs)

# Module-level model dict populated during lifespan startup.
llm_model_dict: dict = {}


# ---------------------------------------------------------------------------
# Lifespan
# ---------------------------------------------------------------------------

def ensure_directories() -> None:
    """Create cache sub-directories if they do not already exist."""
    for d in [settings.extract.staging_dir, settings.extract.results_dir]:
        d.mkdir(parents=True, exist_ok=True)
        logger.debug(f"Ensured directory: {d}")


def initialize_models() -> None:
    global llm_model_dict
    llm_model_dict = get_llm_endpoint()


@asynccontextmanager
async def lifespan(app: FastAPI):
    filtered_paths = ["/health"]
    configure_uvicorn_logging(settings.common.app.log_level, filtered_paths)
    create_llm_session(pool_maxsize=settings.common.llm.max_batch_size)
    initialize_models()

    # Database check (required for operation — fail fast if DB is unavailable).
    try:
        if check_db_connection():
            logger.info("✅ Database connection established")
            try:
                from extract.db.models import Base
                from extract.db.connection import engine
                Base.metadata.create_all(bind=engine)
                logger.info("✅ Database schema initialized")
            except Exception as schema_error:
                logger.error(f"❌ Failed to initialize database schema: {schema_error}")
                raise RuntimeError(f"Database schema initialization failed: {schema_error}")
        else:
            raise RuntimeError("Database connection required but not available.")
    except RuntimeError:
        raise
    except Exception as exc:
        raise RuntimeError(f"Database connection required but failed: {exc}")

    ensure_directories()

    # Zombie-job recovery scan on startup.
    logger.info("Running zombie job recovery scan...")
    from extract.job_utils import recover_zombie_jobs
    recovered = recover_zombie_jobs()
    if recovered > 0:
        logger.warning(f"Recovered {recovered} zombie job(s) from previous session")

    yield

    # Shutdown
    logger.info("Application shutting down...")
    try:
        close_db_connections()
        logger.info("Database connections closed")
    except Exception as exc:
        logger.error(f"Error closing DB connections: {exc}", exc_info=True)
    stderr_monitor.stop()


# ---------------------------------------------------------------------------
# App
# ---------------------------------------------------------------------------

tags_metadata = [
    {"name": "schemas", "description": "Immutable extraction schema registry"},
    {"name": "extraction", "description": "Synchronous and asynchronous text extraction"},
    {"name": "jobs", "description": "Async extraction job management"},
    {"name": "health", "description": "Health check"},
]

app = FastAPI(
    lifespan=lifespan,
    title="AI-Services Extract Information API",
    description=(
        "Entity extraction microservice. Register immutable JSON schemas, then "
        "run synchronous or asynchronous extraction against them."
    ),
    version="1.0.0",
    openapi_tags=tags_metadata,
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
    return get_swagger_ui_html(
        openapi_url="/openapi.json",
        title="AI-Services Extract Information API — Swagger UI",
    )


@app.get("/health", tags=["health"])
def health():
    return {"status": "ok"}


# ---------------------------------------------------------------------------
# Error handler for SchemaValidationError
# ---------------------------------------------------------------------------

@app.exception_handler(SchemaValidationError)
async def schema_validation_error_handler(request: Request, exc: SchemaValidationError):
    body: dict = {"error": {"code": exc.code, "message": exc.message, "status": exc.status}}
    if exc.details:
        body["error"]["details"] = exc.details
    return JSONResponse(status_code=exc.status, content=body)


# ---------------------------------------------------------------------------
# Helper: format datetime for responses
# ---------------------------------------------------------------------------

def _fmt_dt(dt) -> Optional[str]:
    if dt is None:
        return None
    return dt.isoformat().replace("+00:00", "Z")


# ---------------------------------------------------------------------------
# POST /v1/schemas — Register a new immutable extraction schema
# ---------------------------------------------------------------------------

@app.post(
    "/v1/schemas",
    status_code=201,
    response_model=SchemaCreatedResponse,
    responses={
        400: http_error_responses[400],
        409: {"description": "Schema name already exists"},
        500: http_error_responses[500],
    },
    summary="Register extraction schema",
    description=(
        "Register a new immutable extraction schema.\n\n"
        "**Validation performed:**\n"
        "1. `json_schema` is valid draft 2020-12 with root `type: object`\n"
        "2. Per-property `\"required\": true` tags are normalized into a standard "
        "`required` array (nested sub-schemas are handled recursively)\n"
        "3. Every `examples[i].output` validates against the normalized schema\n"
        "4. Token-count budget check: fixed overhead ≤ CONTEXT_SCHEMA_SHARE × MAX_MODEL_LEN\n\n"
        "The stored schema is always the **normalized** form."
    ),
    tags=["schemas"],
)
async def register_schema(body: SchemaRegisterRequest) -> SchemaCreatedResponse:
    # --- Normalize per-property "required": true convention FIRST ---
    normalized = normalize_schema(body.json_schema)

    # --- JSON Schema structural validation (against the normalized form) ---
    validate_json_schema_structure(normalized)

    # --- Validate example outputs against normalized schema ---
    examples_raw = [ex.model_dump() for ex in body.examples] if body.examples else None
    validate_examples(examples_raw, normalized)

    # --- Conflict check (name uniqueness) ---
    if db_repo.schema_name_exists(body.name):
        raise SchemaValidationError(
            "CONFLICT",
            f"A schema with name {body.name!r} already exists.",
            status=409,
        )

    # --- Token-count caching ---
    llm_endpoint = llm_model_dict.get("llm_endpoint", "")
    try:
        schema_tokens, examples_tokens, custom_prompt_tokens = await asyncio.to_thread(
            compute_token_counts,
            normalized,
            examples_raw,
            body.custom_prompt,
            llm_endpoint,
        )
    except Exception as exc:
        logger.error(f"Token counting failed: {exc}", exc_info=True)
        raise SchemaValidationError(
            "TOKENIZATION_ERROR",
            "Failed to compute token counts for the schema. "
            "Ensure the LLM tokenize endpoint is reachable.",
            status=500,
        )

    # --- Registration budget check ---
    max_model_len = settings.common.llm.max_model_len
    check_schema_share_in_context(schema_tokens, examples_tokens, custom_prompt_tokens, max_model_len)

    # ---  Persist ---
    schema_id = str(uuid.uuid4())
    row = db_repo.create_schema(
        schema_id=schema_id,
        name=body.name,
        json_schema=normalized,
        schema_tokens=schema_tokens,
        examples_tokens=examples_tokens,
        custom_prompt_tokens=custom_prompt_tokens,
        description=body.description,
        examples=examples_raw,
        custom_prompt=body.custom_prompt,
    )
    if row is None:
        # A second concurrent request might have inserted the same name.
        if db_repo.schema_name_exists(body.name):
            raise SchemaValidationError(
                "CONFLICT",
                f"A schema with name {body.name!r} already exists.",
                status=409,
            )
        raise SchemaValidationError(
            "DATABASE_ERROR",
            "Failed to persist the schema. Please try again.",
            status=500,
        )

    logger.info(f"Registered schema {schema_id!r} ({body.name!r})")
    return SchemaCreatedResponse(
        schema_id=row.schema_id,
        name=row.name,
        description=row.description,
        created_at=_fmt_dt(row.created_at),
    )


# ---------------------------------------------------------------------------
# GET /v1/schemas — List schemas (paginated, name filter, metadata only)
# ---------------------------------------------------------------------------

@app.get(
    "/v1/schemas",
    response_model=SchemaListResponse,
    responses={
        400: http_error_responses[400],
        500: http_error_responses[500],
    },
    summary="List extraction schemas",
    description=(
        "Return a paginated list of registered schemas.  "
        "Schema bodies are **excluded** from this endpoint; use "
        "`GET /v1/schemas/{schema_id}` to retrieve the full definition."
    ),
    tags=["schemas"],
)
async def list_schemas(
    limit: int = Query(default=20, ge=1, le=100, description="Records per page"),
    offset: int = Query(default=0, ge=0, description="Records to skip"),
    name: Optional[str] = Query(default=None, description="Case-insensitive name substring filter"),
) -> SchemaListResponse:
    rows, total = db_repo.list_schemas(name_filter=name, limit=limit, offset=offset)
    data = [
        SchemaListItem(
            schema_id=row.schema_id,
            name=row.name,
            description=row.description,
            example_count=len(row.examples) if row.examples else 0,
            schema_tokens=row.schema_tokens,
            examples_tokens=row.examples_tokens,
            custom_prompt_tokens=row.custom_prompt_tokens,
            created_at=_fmt_dt(row.created_at),
        )
        for row in rows
    ]
    return SchemaListResponse(
        pagination=PaginationInfo(total=total, limit=limit, offset=offset),
        data=data,
    )


# ---------------------------------------------------------------------------
# GET /v1/schemas/{schema_id} — Retrieve full schema definition
# ---------------------------------------------------------------------------

@app.get(
    "/v1/schemas/{schema_id}",
    response_model=SchemaDetailResponse,
    responses={
        404: http_error_responses[404],
        500: http_error_responses[500],
    },
    summary="Get schema by ID",
    description=(
        "Retrieve the full schema record, including the **normalized** "
        "`json_schema`, `examples`, and `custom_prompt`."
    ),
    tags=["schemas"],
)
async def get_schema(schema_id: str) -> SchemaDetailResponse:
    row = db_repo.get_schema_by_id(schema_id)
    if row is None:
        raise SchemaValidationError(
            "SCHEMA_NOT_FOUND",
            f"No schema with id {schema_id!r}.",
            status=404,
        )
    return SchemaDetailResponse(
        schema_id=row.schema_id,
        name=row.name,
        description=row.description,
        json_schema=row.json_schema,
        examples=row.examples,
        custom_prompt=row.custom_prompt,
        schema_tokens=row.schema_tokens,
        examples_tokens=row.examples_tokens,
        custom_prompt_tokens=row.custom_prompt_tokens,
        created_at=_fmt_dt(row.created_at),
    )


# ---------------------------------------------------------------------------
# DELETE /v1/schemas/{schema_id} — Delete a single schema (RESTRICT)
# ---------------------------------------------------------------------------

@app.delete(
    "/v1/schemas/{schema_id}",
    status_code=204,
    responses={
        204: {"description": "Schema deleted"},
        404: http_error_responses[404],
        409: {"description": "Schema is referenced by one or more jobs"},
        500: http_error_responses[500],
    },
    summary="Delete schema",
    description=(
        "Delete a schema.  Rejected if **any** extract job (active or "
        "historical) references this schema.  Delete referencing jobs first."
    ),
    tags=["schemas"],
)
async def delete_schema(schema_id: str) -> Response:
    # Check existence first for a clear 404.
    row = db_repo.get_schema_by_id(schema_id)
    if row is None:
        raise SchemaValidationError(
            "SCHEMA_NOT_FOUND",
            f"No schema with id {schema_id!r}.",
            status=404,
        )

    # Check for referencing jobs before attempting delete (avoids ambiguous DB errors).
    referencing = db_repo.get_referencing_job_ids(schema_id, limit=10)
    if referencing:
        raise SchemaValidationError(
            "SCHEMA_IN_USE",
            f"Schema {schema_id!r} is referenced by {len(referencing)} job(s). "
            "Delete the referencing jobs first.",
            status=409,
            details={"referencing_job_ids": referencing},
        )

    try:
        deleted = db_repo.delete_schema(schema_id)
    except IntegrityError:
        # FK RESTRICT fired — another job was created concurrently.
        referencing = db_repo.get_referencing_job_ids(schema_id, limit=10)
        raise SchemaValidationError(
            "SCHEMA_IN_USE",
            f"Schema {schema_id!r} is referenced by job(s) and cannot be deleted.",
            status=409,
            details={"referencing_job_ids": referencing},
        )

    if not deleted:
        raise SchemaValidationError(
            "SCHEMA_NOT_FOUND",
            f"No schema with id {schema_id!r}.",
            status=404,
        )

    logger.info(f"Deleted schema {schema_id!r}")
    return Response(status_code=204)


# ---------------------------------------------------------------------------
# DELETE /v1/schemas — Bulk delete (confirm=true required)
# ---------------------------------------------------------------------------

@app.delete(
    "/v1/schemas",
    status_code=204,
    responses={
        204: {"description": "All schemas deleted"},
        400: http_error_responses[400],
        409: {"description": "One or more schemas are referenced by jobs"},
        500: http_error_responses[500],
    },
    summary="Bulk delete all schemas",
    description=(
        "Delete **all** registered schemas.  Requires `?confirm=true`.\n\n"
        "Rejected (409) if any extract job exists, because jobs reference "
        "schemas via a FK.  Delete all jobs first."
    ),
    tags=["schemas"],
)
async def bulk_delete_schemas(
    confirm: Optional[str] = Query(
        default=None,
        description="Must be 'true' to confirm destructive bulk deletion",
    ),
) -> Response:
    if confirm != "true":
        raise SchemaValidationError(
            "CONFIRMATION_REQUIRED",
            "Bulk delete requires ?confirm=true.",
            status=400,
        )

    if db_repo.any_schema_has_jobs():
        raise SchemaValidationError(
            "SCHEMAS_IN_USE",
            "One or more schemas are referenced by extract jobs. "
            "Delete all jobs (DELETE /v1/extract/jobs?confirm=true) before bulk-deleting schemas.",
            status=409,
        )

    try:
        db_repo.delete_all_schemas()
    except IntegrityError:
        raise SchemaValidationError(
            "SCHEMAS_IN_USE",
            "One or more schemas are referenced by extract jobs.",
            status=409,
        )

    logger.info("Bulk deleted all schemas")
    return Response(status_code=204)


# ---------------------------------------------------------------------------
# Extraction stubs (POST /v1/extract, jobs CRUD) — implemented separately
# ---------------------------------------------------------------------------

@app.post("/v1/extract", tags=["extraction"], include_in_schema=True)
async def extract_sync():
    """Synchronous extraction — implementation in follow-up iteration."""
    raise SchemaValidationError("NOT_IMPLEMENTED", "POST /v1/extract not yet implemented.", status=501)


@app.post("/v1/extract/jobs", status_code=202, tags=["jobs"], include_in_schema=True)
async def create_extract_job():
    """Async extraction job — implementation in follow-up iteration."""
    raise SchemaValidationError("NOT_IMPLEMENTED", "POST /v1/extract/jobs not yet implemented.", status=501)


@app.get("/v1/extract/jobs", tags=["jobs"], include_in_schema=True)
async def list_extract_jobs():
    raise SchemaValidationError("NOT_IMPLEMENTED", "GET /v1/extract/jobs not yet implemented.", status=501)


@app.get("/v1/extract/jobs/{job_id}", tags=["jobs"], include_in_schema=True)
async def get_extract_job(job_id: str):
    raise SchemaValidationError("NOT_IMPLEMENTED", "GET /v1/extract/jobs/{job_id} not yet implemented.", status=501)


@app.get("/v1/extract/jobs/{job_id}/result", tags=["jobs"], include_in_schema=True)
async def get_extract_job_result(job_id: str):
    raise SchemaValidationError("NOT_IMPLEMENTED", "GET /v1/extract/jobs/{job_id}/result not yet implemented.", status=501)


@app.delete("/v1/extract/jobs/{job_id}", status_code=204, tags=["jobs"], include_in_schema=True)
async def delete_extract_job(job_id: str):
    raise SchemaValidationError("NOT_IMPLEMENTED", "DELETE /v1/extract/jobs/{job_id} not yet implemented.", status=501)


@app.delete("/v1/extract/jobs", status_code=204, tags=["jobs"], include_in_schema=True)
async def bulk_delete_extract_jobs():
    raise SchemaValidationError("NOT_IMPLEMENTED", "DELETE /v1/extract/jobs not yet implemented.", status=501)


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

if __name__ == "__main__":  # pragma: no cover
    uvicorn.run(
        "extract.app:app",
        host="0.0.0.0",
        port=settings.common.app.port,
        log_level=settings.common.app.log_level.lower(),
    )

# Made with Bob
