"""
Helpers of the forecast endpoints (api/v1/forecast.py).

Formatting of forecaster rows, validation and selection of the forecaster version, the
database lookups of a forecast request, and the call to Triton for a forecast.
"""

from typing import Any, Optional

from sqlalchemy.orm import Session

from common.error_utils import APIError, ErrorCode
from common.misc_utils import get_logger
from forecast.db import connectors as connectors_db
from forecast.db.connection import get_db_session
from forecast.db.connectors import ForecasterRow
from forecast.models import ForecasterItem, ForecastResponse, Prediction
from forecast.settings import settings
from forecast.triton_client import triton
from forecast.utils.api_utils import format_timestamp, handle_database_errors
from forecast.utils.inference_validation import validate_inference_instances

logger = get_logger("forecast_api")


def forecaster_item(row: ForecasterRow) -> ForecasterItem:
    """Format a forecaster row into the list response item."""
    return ForecasterItem(
        version=row.version,
        metrics=row.metrics,
        created_at=format_timestamp(row.created_at),
    )


def validate_version(session: Session, cid: str, version: Optional[int]) -> int:
    """Return the version to forecast with: the requested one, or the newest ready version.

    Raises 400 when ``version`` is given and is not a positive integer, and 404 when the
    requested version, or any ready version, does not exist.
    """
    if version is not None:
        if version < 1:
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                "version query parameter must be a positive integer.",
            )
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


def resolve_default(version: Optional[int]) -> tuple[str, int, list[str]]:
    """Return the id, the version to use and the input columns of the default connector.

    Blocking (database), so the endpoints call it through ``run_in_threadpool``.
    """
    with handle_database_errors("getting default forecaster"), get_db_session() as session:
        connector = connectors_db.get_default_connector(session)
        if connector is None:
            logger.warning("Forecast default: no connector found")
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, "No connector found")

        cid = str(connector.id)
        return cid, validate_version(session, cid, version), connector.input_columns or []


def resolve_connector(cid: str, version: Optional[int]) -> tuple[int, list[str]]:
    """Return the version to use and the input columns of a connector.

    Blocking (database), so the endpoints call it through ``run_in_threadpool``.
    """
    with handle_database_errors("getting forecaster", cid), get_db_session() as session:
        connector = connectors_db.get_connector(session, cid)
        if connector is None:
            logger.warning(f"Forecast: connector {cid} not found")
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"Connector {cid!r} not found")

        return validate_version(session, cid, version), connector.input_columns or []


async def run_forecast(
    cid: str, version: int, input_columns: list[str], instances: list[dict[str, Any]]
) -> ForecastResponse:
    """Validate the rows, forward them to Triton and return the predictions of the forecaster."""
    validate_inference_instances(
        instances, input_columns, max_rows=settings.forecast.max_inference_rows
    )
    model_name = f"fc_{cid}_{version}"
    predictions = await triton.run_inference(
        model_name=model_name, instances=instances, input_columns=input_columns
    )
    return ForecastResponse(
        version=version,
        predictions=[Prediction(**prediction) for prediction in predictions],
    )
