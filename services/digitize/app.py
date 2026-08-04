"""
FastAPI application entry point.

Implements the Digitize Documents API.  Following the digitize-api-sample
pattern, all endpoint logic lives in dedicated router modules:

  - api/v1/jobs.py      — job creation, listing, detail, deletion
  - api/v1/admin.py     — import/export metadata operations
  - api/v1/documents.py — document listing, detail, content, deletion

This file is responsible only for:
  - Application lifespan (startup / shutdown)
  - Middleware (request-ID injection)
  - Router registration
"""

import uuid
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, HTTPException, Request, status
from fastapi.openapi.docs import get_swagger_ui_html
from lingua import Language

from common.diagnostic_logger import setup_comprehensive_crash_handler
from common.misc_utils import set_log_level, get_logger
from common.misc_utils import set_request_id, configure_uvicorn_logging
from common.lang_utils import setup_language_detector
from common.error_utils import http_exception_handler
from digitize.settings import settings

set_log_level(settings.common.app.log_level)

from digitize.db.connection import check_db_connection, close_db_connections
import digitize.utils.jobs as dg_util
from digitize.utils.recovery import recover_zombie_jobs

logger = get_logger("digitize_server")
diagnostic_logger, stderr_monitor, signal_handler = setup_comprehensive_crash_handler(logger)


# ------------------------------------------------------------------ #
# Lifespan                                                            #
# ------------------------------------------------------------------ #

@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application lifespan events (startup and shutdown)."""
    filtered_paths = ["/health", "/v1/jobs"]
    configure_uvicorn_logging(settings.common.app.log_level, filtered_paths)
    logger.info("Application starting up...")

    # Language detector for document processing.
    try:
        setup_language_detector(
            [Language.ENGLISH, Language.GERMAN, Language.ITALIAN, Language.FRENCH]
        )
        logger.info("Language detector initialized for EN, DE, IT, FR")
    except Exception as exc:
        logger.error(f"Error initializing language detector: {exc}", exc_info=True)

    # Database connection — required for all operations.
    try:
        if check_db_connection():
            logger.info("✅ Database connection established")

            try:
                from digitize.db.models import Base
                from digitize.db.connection import engine

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
    logger.info("Application shutting down...")
    try:
        close_db_connections()
        logger.info("Database connections closed")
    except Exception as exc:
        logger.error(f"Error closing database connections: {exc}", exc_info=True)

    stderr_monitor.stop()


# ------------------------------------------------------------------ #
# Application factory                                                 #
# ------------------------------------------------------------------ #

tags_metadata = [
    {
        "name": "health",
        "description": "Health check and service status endpoints",
    },
    {
        "name": "jobs",
        "description": (
            "Job tracking and management for document processing "
            "(Ingestion | Digitization) operations"
        ),
    },
    {
        "name": "documents",
        "description": "Document management operations including retrieval and deletion",
    },
]

app = FastAPI(
    title="Digitize Documents Service",
    description=(
        "Document digitization and ingestion API for processing PDF and DOCX files "
        "into searchable content. "
        "Supports both digitization (converting documents to text/markdown/JSON) and "
        "ingestion (processing and indexing documents into a vector database for "
        "semantic search)."
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
        title="Digitize Documents Service — Swagger UI",
    )


@app.get(
    "/health",
    status_code=status.HTTP_200_OK,
    tags=["health"],
    summary="Health check",
    description="Check if the service is running and healthy. Used for liveness probes.",
    response_description="Service health status",
)
async def health_check():
    return {"status": "ok"}


# ------------------------------------------------------------------ #
# Router registration                                                 #
# ------------------------------------------------------------------ #

from digitize.api.v1.jobs import router as jobs_router
from digitize.api.v1.admin import router as admin_router
from digitize.api.v1.documents import router as documents_router

app.include_router(jobs_router, prefix="/v1/jobs", tags=["jobs"])
app.include_router(admin_router, prefix="/v1", tags=["jobs"])
app.include_router(documents_router, prefix="/v1/documents", tags=["documents"])


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=4000)
