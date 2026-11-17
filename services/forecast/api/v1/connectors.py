"""
Connector REST API endpoints.

Mounted at /v1/connectors by app.py. Only one connector is supported in this
release, so POST returns 409 while a connector exists.

Endpoints:
  POST   /v1/connectors
  GET    /v1/connectors
  GET    /v1/connectors/{connector_id}
  PUT    /v1/connectors/{connector_id}
  GET    /v1/connectors/{connector_id}/metrics   (developer use only)
"""

from typing import Optional
from uuid import UUID

from fastapi import APIRouter, Query, Response, status
from sqlalchemy.exc import IntegrityError

from common.error_utils import APIError, ErrorCode, http_error_responses
from common.misc_utils import get_logger
from forecast.db import connectors as connectors_db
from forecast.db.connection import get_db_session
from forecast.models import (
    ConnectorCreateRequest,
    ConnectorCreateResponse,
    ConnectorListResponse,
    ConnectorMetricsResponse,
    ConnectorResponse,
    ConnectorUpdateRequest,
    ConnectorVersions,
    Pagination,
)
from forecast.settings import settings
from forecast.training.scheduler import register_connector_job
from forecast.utils.api_utils import (
    UNIQUE_VIOLATION,
    handle_database_errors,
    format_timestamp,
    job_item,
    list_item,
    merge_connection_details,
    sqlstate,
    training_summary,
)
from forecast.utils.encryption import encrypt_secrets, safe_connection_details

router = APIRouter()
logger = get_logger("forecast_connectors")


# ---------------------------------------------------------------------------
# POST /v1/connectors
# ---------------------------------------------------------------------------


@router.post(
    "",
    status_code=status.HTTP_201_CREATED,
    response_model=ConnectorCreateResponse,
    responses={
        409: http_error_responses[409],
        429: http_error_responses[429],
        500: http_error_responses[500],
    },
    summary="Create the connector and queue its first training",
    description=(
        "Stores the connector with its encrypted secrets and the training details, and "
        "queues the first training job. Only one connector is supported in this release."
    ),
    response_description="Connector created",
)
def create_connector(body: ConnectorCreateRequest) -> ConnectorCreateResponse:
    """Create the single connector and queue its first training job."""
    connector_id = str(body.id)

    # Cheap pre-check, so a request that will be rejected never takes the table lock below.
    with handle_database_errors("checking existing connector", connector_id), get_db_session() as session:
        if connectors_db.connector_exists(session):
            logger.warning(f"Create rejected for connector {connector_id}: a connector already exists")
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "A connector already exists. Only one connector is supported.",
            )

    encrypted = encrypt_secrets(body.connection_details.model_dump(exclude_none=True))

    with handle_database_errors("creating", connector_id), get_db_session() as session:
        connectors_db.lock_connectors(session)
        if connectors_db.connector_exists(session):
            logger.warning(f"Create rejected for connector {connector_id}: a connector already exists")
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "A connector already exists. Only one connector is supported.",
            )
        if connectors_db.count_queued_jobs(session) >= settings.forecast.max_queued_jobs:
            logger.warning(f"Create rejected for connector {connector_id}: training queue is full")
            APIError.raise_error(ErrorCode.SERVER_BUSY, "Too many training jobs are queued.")

        try:
            connectors_db.insert_connector(
                session,
                connector_id=connector_id,
                name=body.name,
                connector_type=body.type,
                config=encrypted,
                source_table=body.training.table,
                target_column=body.training.target_column,
                input_columns=[
                    column.model_dump(exclude_none=True) for column in body.training.input_columns
                ],
                schedule=body.schedule,
            )
        except IntegrityError as exc:
            # Only a duplicate id or name is a conflict. Any other integrity error
            # is a server fault and goes to database_errors.
            if sqlstate(exc) not in (None, UNIQUE_VIOLATION):
                raise
            logger.warning(
                f"Create rejected for connector {connector_id}: duplicate connector name or ID"
            )
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                f"A connector with this id or the name {body.name!r} already exists",
            )

    # Scheduled only after the transaction has committed, so a failed insert
    # never leaves a scheduler job for a connector that does not exist. The
    # connector and its queued job are stored by now: a failed registration must
    # not fail the call, or the retry would be rejected as "a connector already
    # exists". Startup registers the jobs of every connector that has none.
    try:
        register_connector_job(connector_id, body.schedule)
    except Exception:
        logger.error(
            f"Connector {connector_id} is stored, but its training could not be scheduled",
            exc_info=True,
        )
    logger.info(f"Connector {connector_id} ({body.name!r}) created, first training queued")
    return ConnectorCreateResponse(id=connector_id)


# ---------------------------------------------------------------------------
# GET /v1/connectors
# ---------------------------------------------------------------------------


@router.get(
    "",
    response_model=ConnectorListResponse,
    responses={500: http_error_responses[500]},
    summary="List connectors",
    description="Paginated list of connectors. The name filter is a partial, case-insensitive match.",
    response_description="Paginated list of connectors",
)
def list_connectors(
    limit: int = Query(50, ge=1, le=200, description="Records per page (1-200)."),
    offset: int = Query(0, ge=0, description="Records to skip."),
    name: Optional[str] = Query(
        None, max_length=255, description="Partial, case-insensitive match on the name."
    ),
) -> ConnectorListResponse:
    """Return one page of connectors, optionally filtered by name."""
    with handle_database_errors("listing connectors"), get_db_session() as session:
        rows, total = connectors_db.list_connectors(
            session, limit=limit, offset=offset, name=name
        )
    return ConnectorListResponse(
        pagination=Pagination(total=total, limit=limit, offset=offset),
        data=[list_item(row) for row in rows],
    )


# ---------------------------------------------------------------------------
# GET /v1/connectors/{connector_id}
# ---------------------------------------------------------------------------


@router.get(
    "/{connector_id}",
    response_model=ConnectorResponse,
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    summary="Get a connector",
    description="One connector. Secrets, such as the password, are never returned.",
    response_description="Connector detail",
)
def get_connector(connector_id: UUID) -> ConnectorResponse:
    """Return one connector with its safe connection details."""
    cid = str(connector_id)
    with handle_database_errors("getting", cid), get_db_session() as session:
        row = connectors_db.get_connector(session, cid)
    if row is None:
        logger.warning(f"Get: connector {cid} not found")
        APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"Connector {cid!r} not found")

    return ConnectorResponse(
        id=str(row.id),
        name=row.name,
        type=row.type,
        table=row.source_table,
        target_column=row.target_column,
        schedule=row.schedule,
        connection_details=safe_connection_details(row.config),
        status=row.status,
        last_trained_at=format_timestamp(row.last_trained_at),
        message=row.message or "",
        created_at=format_timestamp(row.created_at),
        updated_at=format_timestamp(row.updated_at),
    )


# ---------------------------------------------------------------------------
# PUT /v1/connectors/{connector_id}
# ---------------------------------------------------------------------------


@router.put(
    "/{connector_id}",
    status_code=status.HTTP_200_OK,
    responses={
        400: http_error_responses[400],
        404: http_error_responses[404],
        409: http_error_responses[409],
        500: http_error_responses[500],
    },
    summary="Update a connector",
    description=(
        "Partial update. connection_details is merged key by key, and supplied secrets are "
        "encrypted again. The merged details are validated before anything is written. "
        "type and training cannot be changed."
    ),
    response_description="Connector updated",
)
def update_connector(connector_id: UUID, body: Optional[ConnectorUpdateRequest] = None):
    """Apply a partial update to a connector and validate the merged connection details."""
    body = body or ConnectorUpdateRequest()
    cid = str(connector_id)
    supplied = (
        body.connection_details.model_dump(exclude_unset=True)
        if body.connection_details is not None
        else {}
    )
    writes = body.name is not None or body.connection_details is not None

    # One transaction: the row is locked on the first read, so the merge, the
    # validation and the host/username check run against the config that is
    # written. A concurrent PUT cannot change it in between.
    with handle_database_errors("updating", cid), get_db_session() as session:
        row = connectors_db.get_connector(session, cid, for_update=writes)
        if row is None:
            logger.warning(f"Update: connector {cid} not found")
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"Connector {cid!r} not found")

        if writes:
            config = dict(row.config)
            if supplied:
                config.update(merge_connection_details(cid, config, supplied))

            try:
                connectors_db.update_connector(session, cid, name=body.name, config=config)
            except IntegrityError as exc:
                if sqlstate(exc) not in (None, UNIQUE_VIOLATION):
                    raise
                logger.warning(f"Update of connector {cid} rejected: the name {body.name!r} is taken")
                APIError.raise_error(
                    ErrorCode.RESOURCE_LOCKED,
                    f"A connector named {body.name!r} already exists",
                )

    logger.info(f"Connector {cid} updated")
    return Response(status_code=status.HTTP_200_OK)


# ---------------------------------------------------------------------------
# GET /v1/connectors/{connector_id}/metrics
# ---------------------------------------------------------------------------


@router.get(
    "/{connector_id}/metrics",
    response_model=ConnectorMetricsResponse,
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    summary="Connector metrics (developer use only)",
    description=(
        "Training and availability metrics of one connector, for debugging and capacity "
        "checks. The fields can change without notice and are not part of the stable API."
    ),
    response_description="Connector metrics",
)
def get_connector_metrics(connector_id: UUID) -> ConnectorMetricsResponse:
    """Return the ready versions, training summary and job history of a connector."""
    cid = str(connector_id)
    with handle_database_errors("getting metrics for", cid), get_db_session() as session:
        if connectors_db.get_connector(session, cid) is None:
            logger.warning(f"Metrics: connector {cid} not found")
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"Connector {cid!r} not found")
        versions = connectors_db.list_ready_versions(session, cid)
        jobs = connectors_db.list_jobs(session, cid)

    return ConnectorMetricsResponse(
        connector_id=cid,
        versions=ConnectorVersions(ready=versions),
        training=training_summary(jobs),
        jobs=[job_item(job) for job in jobs],
    )
