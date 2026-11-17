"""
Forecast REST API endpoints.

Mounted at /v1/forecast by app.py.

Endpoints:
  GET    /v1/forecast                      (default connector forecasters)
  GET    /v1/forecast/{connector_id}       (connector forecasters)
  POST   /v1/forecast                      (forecast on default connector)
  POST   /v1/forecast/{connector_id}       (forecast on connector)
"""

from typing import Any, Optional
from uuid import UUID

from fastapi import APIRouter, Query, status
from sqlalchemy.orm import Session

from common.error_utils import APIError, ErrorCode, http_error_responses
from common.misc_utils import get_logger
from forecast.db import connectors as connectors_db
from forecast.db.connection import get_db_session
from forecast.db.connectors import ForecasterRow
from forecast.models import ForecasterItem, ForecastRequest, ForecastResponse, Prediction
from forecast.settings import settings
from forecast.triton_client import triton
from forecast.utils.api_utils import format_timestamp, handle_database_errors
from forecast.utils.inference_validation import validate_inference_instances

router = APIRouter()
logger = get_logger("forecast_api")


def _forecaster_item(row: ForecasterRow) -> ForecasterItem:
    """Format a forecaster row into the list response item."""
    return ForecasterItem(
        version=row.version,
        metrics=row.metrics,
        created_at=format_timestamp(row.created_at),
    )


def _validate_version(version: Optional[int]) -> None:
    """Ensure version is a positive integer when provided."""
    if version is not None and version < 1:
        APIError.raise_error(
            ErrorCode.INVALID_REQUEST,
            "version query parameter must be a positive integer.",
        )


def _select_version(session: Session, cid: str, version: Optional[int]) -> int:
    """Return the requested forecaster version or the newest ready version."""
    if version is not None:
        forecaster = connectors_db.get_forecaster(session, cid, version)
        if forecaster is None:
            logger.warning(f"Forecast: version {version} not found for connector {cid}")
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"Forecaster version {version} not found for connector {cid!r}",
            )
        return version

    forecaster = connectors_db.get_newest_forecaster(session, cid)
    if forecaster is None:
        logger.warning(f"Forecast: no ready forecaster for connector {cid}")
        APIError.raise_error(
            ErrorCode.RESOURCE_NOT_FOUND,
            f"No ready forecaster found for connector {cid!r}",
        )
    return forecaster.version


def _run_forecast(
    cid: str, version: int, input_columns: list[dict], instances: list[dict[str, Any]]
) -> ForecastResponse:
    """Validate the rows, forward them to Triton and return the predictions of the forecaster."""
    validate_inference_instances(
        instances, input_columns, max_rows=settings.forecast.max_inference_rows
    )
    model_name = f"fc_{cid}_{version}"
    predictions = triton.run_inference(
        model_name=model_name, instances=instances, input_columns=input_columns
    )
    return ForecastResponse(
        version=version,
        predictions=[Prediction(**prediction) for prediction in predictions],
    )


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

    return [_forecaster_item(row) for row in rows]


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

    return [_forecaster_item(row) for row in rows]


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
def forecast_default(
    body: ForecastRequest,
    version: Optional[int] = Query(None, description="Specific retained forecaster version"),
) -> ForecastResponse:
    """Run inference against the default connector's forecaster."""
    _validate_version(version)

    with handle_database_errors("getting default forecaster"), get_db_session() as session:
        connector = connectors_db.get_default_connector(session)
        if connector is None:
            logger.warning("Forecast default: no connector found")
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, "No connector found")

        cid = str(connector.id)
        target_version = _select_version(session, cid, version)

    input_columns = connector.input_columns or []
    return _run_forecast(cid, target_version, input_columns, body.root)


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
def forecast_connector(
    connector_id: UUID,
    body: ForecastRequest,
    version: Optional[int] = Query(None, description="Specific retained forecaster version"),
) -> ForecastResponse:
    """Run inference against a specific connector's forecaster."""
    _validate_version(version)
    cid = str(connector_id)

    with handle_database_errors("getting forecaster", cid), get_db_session() as session:
        connector = connectors_db.get_connector(session, cid)
        if connector is None:
            logger.warning(f"Forecast: connector {cid} not found")
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"Connector {cid!r} not found")

        target_version = _select_version(session, cid, version)

    input_columns = connector.input_columns or []
    return _run_forecast(cid, target_version, input_columns, body.root)
