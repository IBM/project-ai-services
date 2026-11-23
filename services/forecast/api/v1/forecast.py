"""
Forecast REST API endpoints.

Mounted at /v1/forecast by app.py.

Endpoints:
  GET    /v1/forecast                      (default connector forecasters)
  GET    /v1/forecast/{connector_id}       (connector forecasters)
  POST   /v1/forecast                      (forecast on default connector)
  POST   /v1/forecast/{connector_id}       (forecast on connector)
"""

from typing import Optional
from uuid import UUID

from fastapi import APIRouter, Query, status
from fastapi.concurrency import run_in_threadpool

from common.error_utils import APIError, ErrorCode, http_error_responses
from common.misc_utils import get_logger
from forecast.db import connectors as connectors_db
from forecast.db.connection import get_db_session
from forecast.models import ForecasterItem, ForecastRequest, ForecastResponse
from forecast.utils.api_utils import handle_database_errors
from forecast.utils.forecast_utils import (
    forecaster_item,
    resolve_connector,
    resolve_default,
    run_forecast,
)

router = APIRouter()
logger = get_logger("forecast_api")


# ---------------------------------------------------------------------------
# GET /v1/forecast
# ---------------------------------------------------------------------------


@router.get(
    "",
    response_model=list[ForecasterItem],
    responses={
        404: http_error_responses[404],
        429: http_error_responses[429],
        500: http_error_responses[500],
    },
    summary="List forecasters of the default connector",
    description="List of forecasters for the default connector. Bounded and ordered newest first.",
    response_description="List of all forecasters for the default connector",
)
def list_default_forecasters() -> list[ForecasterItem]:
    """Return every forecaster of the default connector, newest version first."""
    with handle_database_errors("listing default forecasters"), get_db_session() as session:
        connector = connectors_db.get_default_connector(session)
        if connector is None:
            logger.warning("List default forecasters: no connector found")
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, "No connector found")

        rows = connectors_db.list_forecasters(session, str(connector.id))

    return [forecaster_item(row) for row in rows]


# ---------------------------------------------------------------------------
# GET /v1/forecast/{connector_id}
# ---------------------------------------------------------------------------


@router.get(
    "/{connector_id}",
    response_model=list[ForecasterItem],
    responses={
        404: http_error_responses[404],
        422: http_error_responses[422],
        429: http_error_responses[429],
        500: http_error_responses[500],
    },
    summary="List forecasters of a connector",
    description="List of forecasters for a connector. Bounded and ordered newest first.",
    response_description="List of all forecasters for the connector",
)
def list_connector_forecasters(connector_id: UUID) -> list[ForecasterItem]:
    """Return every forecaster of the connector, newest version first."""
    cid = str(connector_id)
    with handle_database_errors("listing forecasters", cid), get_db_session() as session:
        connector = connectors_db.get_connector(session, cid)
        if connector is None:
            logger.warning(f"List forecasters: connector {cid} not found")
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"Connector {cid!r} not found")

        rows = connectors_db.list_forecasters(session, cid)

    return [forecaster_item(row) for row in rows]


# ---------------------------------------------------------------------------
# POST /v1/forecast
# ---------------------------------------------------------------------------


@router.post(
    "",
    status_code=status.HTTP_200_OK,
    response_model=ForecastResponse,
    responses={
        400: http_error_responses[400],
        404: http_error_responses[404],
        422: http_error_responses[422],
        429: http_error_responses[429],
        500: http_error_responses[500],
        503: http_error_responses[503],
    },
    summary="Run a forecast on the default connector",
    description=(
        "Runs a forecast against the default connector's trained forecaster on the rows in the request. "
        "Selects the newest forecaster by default, or the specified version."
    ),
    response_description="Forecast predictions and confidence per row",
)
async def forecast_default(
    body: ForecastRequest,
    version: Optional[int] = Query(None, description="Specific retained forecaster version"),
) -> ForecastResponse:
    """Run inference against the default connector's forecaster."""
    cid, target_version, input_columns = await run_in_threadpool(resolve_default, version)
    return await run_forecast(cid, target_version, input_columns, body.root)


# ---------------------------------------------------------------------------
# POST /v1/forecast/{connector_id}
# ---------------------------------------------------------------------------


@router.post(
    "/{connector_id}",
    status_code=status.HTTP_200_OK,
    response_model=ForecastResponse,
    responses={
        400: http_error_responses[400],
        404: http_error_responses[404],
        422: http_error_responses[422],
        429: http_error_responses[429],
        500: http_error_responses[500],
        503: http_error_responses[503],
    },
    summary="Run a forecast on a connector",
    description=(
        "Runs a forecast against the connector's trained forecaster on the rows in the request. "
        "Selects the newest forecaster by default, or the specified version."
    ),
    response_description="Forecast predictions and confidence per row",
)
async def forecast_connector(
    connector_id: UUID,
    body: ForecastRequest,
    version: Optional[int] = Query(None, description="Specific retained forecaster version"),
) -> ForecastResponse:
    """Run inference against a specific connector's forecaster."""
    cid = str(connector_id)
    target_version, input_columns = await run_in_threadpool(resolve_connector, cid, version)
    return await run_forecast(cid, target_version, input_columns, body.root)
