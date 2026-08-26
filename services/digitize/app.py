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

import os
import uuid
from contextlib import asynccontextmanager

os.environ.setdefault("TZ", "UTC")

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

import asyncio

from digitize.db.connection import check_db_connection, close_db_connections
import digitize.utils.jobs as dg_util
from digitize.utils.recovery import recover_zombie_jobs, recover_connector_sync_state, recover_conversion_tasks

logger = get_logger("digitize_server")
diagnostic_logger, stderr_monitor, signal_handler = setup_comprehensive_crash_handler(logger)


# ------------------------------------------------------------------ #
# Startup / shutdown helpers                                          #
# ------------------------------------------------------------------ #

def _init_language_detector():
    """Initialize the language detector used for document processing."""
    try:
        setup_language_detector(
            [Language.ENGLISH, Language.GERMAN, Language.ITALIAN, Language.FRENCH]
        )
        logger.info("Language detector initialized for EN, DE, IT, FR")
    except Exception as exc:
        logger.error(f"Error initializing language detector: {exc}", exc_info=True)


def _init_database():
    """Verify the database connection and initialize the schema.

    Raises RuntimeError if the database is unavailable or schema init fails,
    since the service requires a database to operate.
    """
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


def _shutdown(dispatcher_task: asyncio.Task) -> None:
    """Cancel the dispatcher and release sync resources on application shutdown.

    Cancels ``dispatcher_task`` (caller must await it to absorb CancelledError),
    closes DB connections, and stops the stderr monitor.
    """
    dispatcher_task.cancel()
    logger.info("Application shutting down...")
    try:
        close_db_connections()
        logger.info("Database connections closed")
    except Exception as exc:
        logger.error(f"Error closing database connections: {exc}", exc_info=True)
    stderr_monitor.stop()


def _recover_zombie_jobs():
    """Recover orphan / zombie jobs left over from a previous app server run."""
    try:
        zombie_count = recover_zombie_jobs()
        if zombie_count > 0:
            logger.info(
                f"Found {zombie_count} zombie job(s) from previous app server run"
            )
    except Exception as exc:
        logger.error(f"Error during zombie job recovery: {exc}", exc_info=True)

def _recover_conversion_tasks():
    """Recover stale conversion tasks left over from a previous app server run."""
    try:
        ct_count = recover_conversion_tasks()
        if ct_count > 0:
            logger.info(
                f"Recovered {ct_count} stale conversion task(s) from previous run"
            )
    except Exception as exc:
        logger.error(f"Error during conversion task recovery: {exc}", exc_info=True)


# ------------------------------------------------------------------ #
# Connector scheduler                                                 #
# ------------------------------------------------------------------ #

@asynccontextmanager
async def _connector_scheduler_lifespan():
    """Start the connector scheduler and keep it running for the app's lifetime.

    Wraps `yield` in an `async with AsyncScheduler(...)` block so the scheduler
    stays open until the application shuts down.
    """
    import digitize.connectors.scheduler as scheduler_module
    from digitize.utils.db import list_connectors
    from apscheduler import AsyncScheduler
    from apscheduler.datastores.sqlalchemy import SQLAlchemyDataStore
    from digitize.db.connection import engine as db_engine

    try:
        data_store = SQLAlchemyDataStore(db_engine, schema="scheduler")
        async with AsyncScheduler(data_store=data_store) as sched:
            scheduler_module._scheduler = sched

            # Connector crash recovery — unlock connectors stuck in 'syncing'.
            try:
                recovered = recover_connector_sync_state()
                if recovered:
                    logger.info(
                        f"Connector crash recovery: reset {recovered} stuck connector(s)"
                    )
            except Exception as exc:
                logger.error(
                    f"Error during connector sync state recovery: {exc}", exc_info=True
                )

            # Re-register all existing connectors (fire_immediately=False so we
            # don't trigger a duplicate tick for connectors that are already
            # up-to-date after crash recovery).
            # If a connector is in status 'delete pending', trigger the delete
            # procedure again and do not register a job.
            try:
                import asyncio
                from digitize.connectors.models import ConnectorStatus
                from digitize.api.v1.connectors import _run_teardown

                connectors = list_connectors()
                registered_count = 0
                for connector in connectors:
                    if connector.sync_status == ConnectorStatus.DELETE_PENDING:
                        logger.info(
                            f"Connector crash recovery: found connector {connector.id!r} "
                            "in 'delete pending' status. Re-triggering delete procedure."
                        )
                        asyncio.create_task(_run_teardown(connector.id))
                        continue

                    await scheduler_module.register_connector_job(
                        connector.id,
                        connector.sync_interval_seconds,
                        fire_immediately=False,
                    )
                    registered_count += 1

                if registered_count:
                    logger.info(
                        f"Re-registered {registered_count} connector job(s) with scheduler"
                    )
            except Exception as exc:
                logger.error(
                    f"Error recovering/re-registering connector jobs: {exc}", exc_info=True
                )

            await sched.start_in_background()
            logger.info("✅ Connector scheduler started")

            yield

    except Exception as exc:
        logger.error(
            f"❌ Failed to start connector scheduler: {exc}", exc_info=True
        )
        # Yield anyway so the service stays up even if the scheduler fails.
        yield


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
    _init_language_detector()

    # Database connection.
    _init_database()

    # Orphan / zombie job recovery on startup.
    _recover_zombie_jobs()

    # Stale conversion task recovery on startup.
    _recover_conversion_tasks()

    # Start conversion dispatcher.
    from digitize.workers.conversion_dispatcher import dispatch_loop
    dispatcher_task = asyncio.create_task(dispatch_loop())
    logger.info("✅ Conversion dispatcher started")

    # Connector scheduler.
    async with _connector_scheduler_lifespan():
        yield

    # Shutdown — cancel dispatcher, close DB connections, stop monitor.
    _shutdown(dispatcher_task)
    try:
        await dispatcher_task
    except asyncio.CancelledError:
        pass



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
    {
        "name": "connectors",
        "description": "Data-source connector lifecycle management (file_system, object_storage)",
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
    """Middleware to extract or generate a unique Request ID for tracing.

    Sets the request ID in thread-local or task-local context and appends it to
    the outgoing response headers.
    """
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
    """Perform a basic health check.

    Returns:
        dict: A dictionary indicating the service is running and healthy.
    """
    return {"status": "ok"}


# ------------------------------------------------------------------ #
# Router registration                                                 #
# ------------------------------------------------------------------ #

from digitize.api.v1.jobs import router as jobs_router
from digitize.api.v1.admin import router as admin_router
from digitize.api.v1.documents import router as documents_router
from digitize.api.v1.connectors import router as connectors_router

app.include_router(jobs_router, prefix="/v1/jobs", tags=["jobs"])
app.include_router(admin_router, prefix="/v1", tags=["jobs"])
app.include_router(documents_router, prefix="/v1/documents", tags=["documents"])
app.include_router(connectors_router, prefix="/v1/connectors", tags=["connectors"])


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=4000)
