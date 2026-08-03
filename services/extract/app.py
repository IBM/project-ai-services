"""
Extract Information Service — FastAPI application.
"""

import uuid
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, Request
from fastapi.openapi.docs import get_swagger_ui_html
from fastapi.responses import JSONResponse
from common.misc_utils import configure_uvicorn_logging, create_llm_session, get_llm_endpoint, get_logger, set_log_level, set_request_id
from common.diagnostic_logger import setup_comprehensive_crash_handler
from extract.db.connection import check_db_connection, close_db_connections


from extract.utils.schema import (
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


# Module-level model dict populated during lifespan startup.
llm_model_dict: dict = {}


# ---------------------------------------------------------------------------
# Lifespan
# ---------------------------------------------------------------------------

def ensure_directories() -> None:
    """Create cache subdirectories if they do not already exist."""
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

    # Compute prompt scaffold token overhead now that the LLM session is ready.
    from extract.utils.schema import calculate_prompt_overhead_tokens
    llm_endpoint = llm_model_dict.get("llm_endpoint", "")
    calculate_prompt_overhead_tokens(llm_endpoint)

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
    from extract.utils.job import recover_zombie_jobs
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


from extract.api.v1.schema import router as schema_router
from extract.api.v1.jobs import router as jobs_router

app.include_router(schema_router, prefix="/v1/schemas", tags=["schemas"])
app.include_router(jobs_router, prefix="/v1/extract", tags=["jobs"])
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
