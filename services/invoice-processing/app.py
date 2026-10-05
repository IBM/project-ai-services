"""
Invoice Processing Service — FastAPI application.
"""

from contextlib import asynccontextmanager
import uuid

import requests
import uvicorn
from fastapi import FastAPI, HTTPException, Request
from fastapi.openapi.docs import get_swagger_ui_html

from common.diagnostic_logger import setup_comprehensive_crash_handler
from common.error_utils import http_exception_handler
from common.misc_utils import (
    configure_uvicorn_logging,
    get_logger,
    set_log_level,
    set_request_id,
)

from db.connection import (
    check_db_connection,
    close_db_connections,
    engine,
)
from db.models import Base
from schema import INVOICE_SCHEMA_PAYLOAD
from settings import settings

set_log_level(settings.common.app.log_level)
logger = get_logger("app")

diagnostic_logger, stderr_monitor, signal_handler = setup_comprehensive_crash_handler(logger)


def ensure_directories() -> None:
    """Create staging and results cache directories if not present."""
    for directory in [settings.invoice.staging_dir, settings.invoice.results_dir]:
        directory.mkdir(parents=True, exist_ok=True)
        logger.debug(f"Ensured directory: {directory}")


def _initialize_database() -> None:
    """Connect to database and create all schema tables."""
    try:
        connected = check_db_connection()
    except Exception as exc:
        raise RuntimeError(f"Database connection check failed: {exc}") from exc

    if not connected:
        raise RuntimeError("Database connection required but not available.")

    logger.info("Database connection established")

    try:
        if engine is not None:
            Base.metadata.create_all(bind=engine)
            logger.info("Database schema initialized")
    except Exception as exc:
        logger.error(f"Failed to initialize database schema: {exc}")
        raise RuntimeError(f"Database schema initialization failed: {exc}") from exc


def _register_invoice_schema() -> None:
    """Register invoice schema with extract service at startup.

    If extract service URL is configured, attempts registration:
    - 201: Schema created
    - 409: Schema already exists (idempotent)
    - Any other status or network error: raises RuntimeError
    """
    if not settings.invoice.extract_url:
        logger.warning("EXTRACT_URL not configured — skipping schema registration")
        return

    url = f"{settings.invoice.extract_url.rstrip('/')}/v1/schemas"
    logger.info(f"Registering invoice schema with extract service at {url}...")

    try:
        resp = requests.post(url, json=INVOICE_SCHEMA_PAYLOAD, timeout=10.0)
        if resp.status_code == 201:
            logger.info("Successfully registered invoice extraction schema")
        elif resp.status_code == 409:
            logger.info("Invoice extraction schema already registered (idempotent)")
        else:
            error_msg = f"Failed to register invoice schema with extract service: HTTP {resp.status_code} - {resp.text}"
            logger.error(error_msg)
            raise RuntimeError(error_msg)
    except requests.RequestException as exc:
        error_msg = f"Network error connecting to extract service schema endpoint: {exc}"
        logger.error(error_msg)
        raise RuntimeError(error_msg) from exc


def recover_zombie_jobs() -> int:
    """Boilerplate stub for zombie job recovery."""
    logger.debug("Zombie job recovery scan stub")
    return 0


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application lifespan events."""
    configure_uvicorn_logging(settings.common.app.log_level, ["/health"])
    logger.info("Application starting up...")
    _initialize_database()
    ensure_directories()
    _register_invoice_schema()
    recover_zombie_jobs()

    yield

    logger.info("Application shutting down...")
    try:
        close_db_connections()
        logger.info("Database connections closed")
    except Exception as exc:
        logger.error(f"Error closing DB connections: {exc}", exc_info=True)
    stderr_monitor.stop()


tags_metadata = [
    {"name": "invoices", "description": "Invoice processing and pipeline management"},
    {"name": "health", "description": "Health check"},
]

app = FastAPI(
    lifespan=lifespan,
    title="AI-Services Invoice Processing API",
    description="End-to-end invoice document ingestion, extraction, and ERP staging microservice.",
    version="1.0.0",
    openapi_tags=tags_metadata,
)


@app.middleware("http")
async def add_request_id(request: Request, call_next):
    """Middleware to set and propagate X-Request-ID."""
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4()))
    set_request_id(request_id)
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    return response


@app.get("/", include_in_schema=False)
def swagger_root():
    """Swagger UI redirect."""
    return get_swagger_ui_html(
        openapi_url="/openapi.json",
        title="AI-Services Invoice Processing API — Swagger UI",
    )


@app.get("/health", tags=["health"])
def health():
    """Health check endpoint."""
    return {"status": "ok"}


@app.exception_handler(HTTPException)
async def custom_http_exception_handler(request: Request, exc: HTTPException):
    """Delegate to shared error handler."""
    return await http_exception_handler(request, exc)


from api.v1.invoices import router as invoices_router

app.include_router(invoices_router, prefix="/v1/invoices", tags=["invoices"])

if __name__ == "__main__":
    uvicorn.run(
        "app:app",
        host="0.0.0.0",
        port=settings.common.app.port,
        log_level=settings.common.app.log_level.lower(),
    )
