"""
Forecast service — FastAPI application entry point.

This file is responsible only for:
  - Application lifespan (startup / shutdown)
  - Middleware (request-ID injection)
  - Exception handler registration
  - The health check
  - Connector and forecasting API routes
"""

import uuid
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, Request, status

from common.diagnostic_logger import setup_comprehensive_crash_handler
from common.misc_utils import (
    configure_uvicorn_logging,
    get_logger,
    set_log_level,
    set_request_id,
)
from forecast.settings import settings

set_log_level(settings.common.app.log_level)

from forecast.api.v1 import connectors
from forecast.db.connection import check_db_configured, close_db_connections
from forecast.errors import register_exception_handlers
from forecast.triton_client.triton import triton_url_check
from forecast.utils.encryption import validate_cipher

logger = get_logger("forecast_server")
diagnostic_logger, stderr_monitor, signal_handler = setup_comprehensive_crash_handler(logger)

SERVICE_PORT = 8500


# ------------------------------------------------------------------ #
# Lifespan                                                            #
# ------------------------------------------------------------------ #

@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application lifespan events (startup and shutdown)."""
    configure_uvicorn_logging(settings.common.app.log_level, ["/health"])
    logger.info("Forecast service starting up...")

    # Fail fast: without a valid key the service cannot store connector secrets, so
    # the error stops startup instead of surfacing on the first connector request.
    validate_cipher()
    check_db_configured()

    # Cap thread limiter to DB pool max capacity so pool exhaustion queues in AnyIO
    try:
        import anyio.to_thread

        anyio.to_thread.current_default_thread_limiter().total_tokens = (
            settings.database.pool_size + settings.database.max_overflow
        )
    except Exception as exc:
        logger.warning(f"Could not set thread limiter tokens: {exc}")

    triton_url_check()

    yield

    logger.info("Forecast service shutting down...")
    try:
        close_db_connections()
    except Exception as exc:
        logger.error(f"Error closing database connections: {exc}", exc_info=True)

    stderr_monitor.stop()


# ------------------------------------------------------------------ #
# Application                                                         #
# ------------------------------------------------------------------ #

tags_metadata = [
    {
        "name": "health",
        "description": "Health check and service status",
    },
    {
        "name": "connectors",
        "description": "Source database connectors and training configuration",
    },
]

app = FastAPI(
    title="AI-Services Forecast API",
    description=(
        "Trains tabular forecasters on data from Db2 for i, serves them on Triton "
        "and returns a prediction with a confidence per row."
    ),
    version="1.0.0",
    lifespan=lifespan,
    openapi_tags=tags_metadata,
)

register_exception_handlers(app)
app.include_router(connectors.router, prefix="/v1/connectors", tags=["connectors"], include_in_schema=False)


# ------------------------------------------------------------------ #
# Middleware                                                          #
# ------------------------------------------------------------------ #

@app.middleware("http")
async def add_request_id(request: Request, call_next):
    """Extract or generate a request ID, and return it in the X-Request-ID header."""
    request_id = request.headers.get("X-Request-ID")
    if not request_id or len(request_id) > 128:
        request_id = str(uuid.uuid4())
    set_request_id(request_id)
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    return response


# ------------------------------------------------------------------ #
# Health                                                              #
# ------------------------------------------------------------------ #

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


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=SERVICE_PORT)
