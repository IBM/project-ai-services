"""
Forecast service — FastAPI application entry point.

This file is responsible only for:
  - Application lifespan (startup / shutdown)
  - Middleware (request-ID injection)
  - Exception handler registration
  - The health check

The connector and forecast routers are added by later stories.
"""

import uuid
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, Request, Response, status

from common.diagnostic_logger import setup_comprehensive_crash_handler
from common.misc_utils import (
    configure_uvicorn_logging,
    get_logger,
    set_log_level,
    set_request_id,
)
from forecast.settings import settings

set_log_level(settings.common.app.log_level)

from forecast.db.connection import check_db_reachable, close_db_connections
from forecast.encryption import EncryptionKeyError, get_cipher
from forecast.errors import register_exception_handlers
from forecast.triton import is_triton_ready, triton_url_problem

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

    # The service starts even when PostgreSQL or Triton is down, because
    # /health reports both. Only a warning is logged here.
    if not check_db_reachable():
        logger.warning("PostgreSQL is not reachable; /health will report 503")
    # A bad TRITON_URL can never work, so it is reported as a configuration error
    # instead of the "not reachable" warning, which would point at the network.
    triton_problem = triton_url_problem()
    if triton_problem:
        logger.error(f"{triton_problem}. Forecast requests will fail until this is fixed.")
    elif not is_triton_ready():
        logger.warning("Triton is not reachable; forecast requests will fail until it is")

    # A missing or invalid key does not stop the service, but creating a connector
    # needs it and fails until it is fixed.
    try:
        get_cipher()
    except EncryptionKeyError as exc:
        logger.error(f"{exc} Creating connectors will fail until this is fixed.")

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


# ------------------------------------------------------------------ #
# Middleware                                                          #
# ------------------------------------------------------------------ #

@app.middleware("http")
async def add_request_id(request: Request, call_next):
    """Extract or generate a request ID, and return it in the X-Request-ID header."""
    request_id = request.headers.get("X-Request-ID") or str(uuid.uuid4())
    set_request_id(request_id)
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    return response


# ------------------------------------------------------------------ #
# Health                                                              #
# ------------------------------------------------------------------ #

@app.get(
    "/health",
    tags=["health"],
    summary="Health check",
    description=(
        "Returns 200 when PostgreSQL is reachable and 503 when it is not. "
        "The response also reports Triton. An unreachable Triton does not make "
        "the service unhealthy, because training and connector management still work."
    ),
)
def health_check(response: Response):
    """Report PostgreSQL and Triton reachability."""
    postgres_ok = check_db_reachable()
    triton_ok = is_triton_ready()

    if not postgres_ok:
        response.status_code = status.HTTP_503_SERVICE_UNAVAILABLE

    return {
        "status": "ok" if postgres_ok else "unavailable",
        "checks": {
            "postgres": "ok" if postgres_ok else "error",
            "triton": "ok" if triton_ok else "unreachable",
        },
    }


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=SERVICE_PORT)
