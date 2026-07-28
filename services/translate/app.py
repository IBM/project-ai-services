"""
Translation service — FastAPI application entry point.

All the endpoint logic lives in dedicated router modules:

  - api/v1/translate.py — POST /v1/translate (sync, stateless)
  - api/v1/jobs.py      — async job creation, listing, detail, result, download

This file is responsible only for:
  - Application lifespan (startup / shutdown)
  - Middleware (request-ID injection)
  - Exception handler registration
  - Router registration
"""

import uuid
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, HTTPException, Request, status
from fastapi.openapi.docs import get_swagger_ui_html

from common.diagnostic_logger import setup_comprehensive_crash_handler
from common.error_utils import http_exception_handler
from common.misc_utils import set_log_level, get_logger, set_request_id, configure_uvicorn_logging
from translate.settings import settings

set_log_level(settings.common.app.log_level)

from translate.db.connection import check_db_connection, close_db_connections
from translate.utils.recovery import recover_zombie_jobs

logger = get_logger("translate_server")
diagnostic_logger, stderr_monitor, signal_handler = setup_comprehensive_crash_handler(logger)


# ------------------------------------------------------------------ #
# Lifespan                                                            #
# ------------------------------------------------------------------ #

@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application lifespan events (startup and shutdown)."""
    filtered_paths = ["/health"]
    configure_uvicorn_logging(settings.common.app.log_level, filtered_paths)
    logger.info("Translation service starting up...")

    # Database connection — required for all operations.
    try:
        if check_db_connection():
            logger.info("✅ Database connection established")

            try:
                from translate.db.models import Base
                from translate.db.connection import engine

                if engine is None:
                    raise RuntimeError("Database engine is not initialized")
                Base.metadata.create_all(bind=engine)
                logger.info("✅ Database schema initialized")
            except Exception as schema_err:
                logger.error(
                    f"❌ Failed to initialize database schema: {schema_err}",
                    exc_info=True,
                )
                raise RuntimeError(
                    f"Database schema initialization failed: {schema_err}"
                )
        else:
            logger.error(
                "❌ Database connection failed — service requires database to operate"
            )
            raise RuntimeError(
                "Database connection required but not available. "
                "Please check database configuration."
            )
    except RuntimeError as exc:
        logger.error(f"❌ Startup aborted: {exc}", exc_info=True)
        raise
    except Exception as exc:
        logger.error(f"❌ Database check failed: {exc}", exc_info=True)
        raise RuntimeError(f"Database connection required but failed: {exc}")

    # Ensure cache directories exist.
    _ensure_cache_directories()

    # Orphan / zombie job recovery on startup.
    try:
        zombie_count = recover_zombie_jobs()
        if zombie_count > 0:
            logger.info(
                f"Found {zombie_count} zombie job(s) from previous app server run"
            )
    except Exception as exc:
        logger.error(f"Error during zombie job recovery: {exc}", exc_info=True)

    yield

    # Shutdown.
    logger.info("Translation service shutting down...")
    try:
        close_db_connections()
        logger.info("Database connections closed")
    except Exception as exc:
        logger.error(f"Error closing database connections: {exc}", exc_info=True)

    stderr_monitor.stop()


# ------------------------------------------------------------------ #
# Helpers                                                             #
# ------------------------------------------------------------------ #

def _ensure_cache_directories() -> None:
    """Create staging and results directories under the cache root if absent."""
    for directory in (
        settings.translate.staging_dir,
        settings.translate.results_dir,
    ):
        directory.mkdir(parents=True, exist_ok=True)
        logger.debug(f"Cache directory ready: {directory}")


# ------------------------------------------------------------------ #
# Application factory                                                 #
# ------------------------------------------------------------------ #

tags_metadata = [
    {
        "name": "health",
        "description": "Health check and service status",
    },
    {
        "name": "translation",
        "description": "Synchronous inline-text translation (POST /v1/translate)",
    },
    {
        "name": "jobs",
        "description": "Async translation job management (file uploads)",
    },
]

app = FastAPI(
    title="AI-Services Translation API",
    description=(
        "Translates plain text (sync) or .txt / .md files (async) between languages "
        "using an OpenAI-compatible vLLM endpoint."
    ),
    version="1.0.0",
    lifespan=lifespan,
    openapi_tags=tags_metadata,
)


# ------------------------------------------------------------------ #
# Exception handler                                                   #
# ------------------------------------------------------------------ #

@app.exception_handler(HTTPException)
async def custom_http_exception_handler(request: Request, exc: HTTPException):
    """Delegate to the shared handler from common.error_utils."""
    return await http_exception_handler(request, exc)


# ------------------------------------------------------------------ #
# Middleware                                                          #
# ------------------------------------------------------------------ #

@app.middleware("http")
async def add_request_id(request: Request, call_next):
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4()))
    set_request_id(request_id)
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    return response


# ------------------------------------------------------------------ #
# Built-in routes                                                     #
# ------------------------------------------------------------------ #

@app.get("/", include_in_schema=False)
def swagger_root():
    """Expose Swagger UI at the root path (/)."""
    return get_swagger_ui_html(
        openapi_url="/openapi.json",
        title="AI-Services Translation API — Swagger UI",
    )


@app.get(
    "/health",
    status_code=status.HTTP_200_OK,
    tags=["health"],
    summary="Health check",
    description="Check if the service is running and healthy.",
)
async def health_check():
    return {"status": "ok"}


# ------------------------------------------------------------------ #
# Router registration                                                 #
# ------------------------------------------------------------------ #

from translate.api.v1.translate import router as translate_router
from translate.api.v1.jobs import router as jobs_router

app.include_router(translate_router, prefix="/v1/translate", tags=["translation"])
app.include_router(jobs_router, prefix="/v1/translate/jobs", tags=["jobs"])


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=9000)

# Made with Bob
