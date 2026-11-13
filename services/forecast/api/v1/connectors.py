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

from contextlib import contextmanager
from datetime import datetime, timezone
from typing import Any, Optional
from uuid import UUID

from fastapi import APIRouter, Query, Response, status
from pydantic import ValidationError
from sqlalchemy.exc import (
    IntegrityError,
    SQLAlchemyError,
    TimeoutError as PoolTimeoutError,
)

from common.error_utils import APIError, ErrorCode, http_error_responses
from common.misc_utils import get_logger, get_utc_timestamp
from forecast.db import connectors as connectors_db
from forecast.db.connection import get_db_session
from forecast.models import ConnectorCreateRequest, ConnectorUpdateRequest, Db2ForIDetails
from forecast.training.scheduler import register_connector_job
from forecast.settings import settings
from forecast.utils.db2_probe import probe_db2_connector
from forecast.utils.encryption import (
    DecryptionError,
    decrypt_secrets,
    encrypt_secrets,
    safe_connection_details,
)

router = APIRouter()
logger = get_logger("forecast_connectors")

# ---------------------------------------------------------------------------
# Response helpers
# ---------------------------------------------------------------------------


def _timestamp(value: Optional[datetime]) -> Optional[str]:
    """Format a timestamp as UTC ISO 8601, whatever timezone the database session uses."""
    if value is None:
        return None
    if value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    return get_utc_timestamp(value.astimezone(timezone.utc))


def _list_item(row: dict) -> dict[str, Any]:
    """Format a connector database row into the list response item schema."""
    return {
        "id": str(row["id"]),
        "name": row["name"],
        "type": row["type"],
        "created_at": _timestamp(row["created_at"]),
        "last_trained_at": _timestamp(row["last_trained_at"]),
        "status": row["status"],
        "table": row["source_table"],
        "target_column": row["target_column"],
        "message": row["message"] or "",
    }


def _job_item(row: dict) -> dict[str, Any]:
    """Format a job database row into the metrics job history item schema."""
    return {
        "seq": row["seq"],
        "status": row["status"],
        "created_at": _timestamp(row["created_at"]),
        "started_at": _timestamp(row["started_at"]),
        "finished_at": _timestamp(row["finished_at"]),
        "message": row["message"] or "",
        "result": row["result"],
    }


def _training_summary(jobs: list[dict]) -> dict[str, Any]:
    """Summarize jobs ordered newest first."""
    completed = [job for job in jobs if job["status"] == "COMPLETED"]
    finished = [job for job in jobs if job["status"] in ("COMPLETED", "FAILED")]
    last_finished = finished[0] if finished else None
    last_completed = completed[0] if completed else None
    return {
        "jobs_completed": len(completed),
        "jobs_failed": sum(job["status"] == "FAILED" for job in jobs),
        "last_job_status": jobs[0]["status"] if jobs else None,
        "last_duration_seconds": (
            (last_finished["result"] or {}).get("training_seconds") if last_finished else None
        ),
        "last_completed_at": (
            _timestamp(last_completed["finished_at"]) if last_completed else None
        ),
    }


# ---------------------------------------------------------------------------
# POST /v1/connectors
# ---------------------------------------------------------------------------


_LOCK_NOT_AVAILABLE = "55P03"


@contextmanager
def _database_errors(operation: str, connector_id: Optional[str] = None):
    """Turn a database error into DATABASE_ERROR without its message.

    The message of a SQLAlchemy error includes the bound parameters, which for a
    connector write are the encrypted config. Only the exception type and safe
    SQLSTATE code are logged.
    """
    try:
        yield
    except PoolTimeoutError:
        target = f" connector {connector_id}" if connector_id else ""
        logger.warning(f"Database connection pool exhausted while {operation}{target}")
        APIError.raise_error(
            ErrorCode.SERVER_BUSY,
            "Database connection pool is temporarily busy. Please retry shortly.",
        )
    except SQLAlchemyError as exc:
        target = f" connector {connector_id}" if connector_id else ""
        orig = getattr(exc, "orig", None)
        sqlstate = getattr(orig, "pgcode", None) or getattr(orig, "sqlstate", None)
        state_str = f" (SQLSTATE {sqlstate})" if sqlstate else ""
        logger.error(f"Database error while {operation}{target}: {type(exc).__name__}{state_str}")
        if sqlstate == _LOCK_NOT_AVAILABLE:
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "The connector is currently locked by a concurrent operation. Please retry shortly.",
            )
        APIError.raise_error(ErrorCode.DATABASE_ERROR)


@router.post(
    "",
    status_code=status.HTTP_201_CREATED,
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
def create_connector(body: ConnectorCreateRequest):
    """Create the single connector and queue its first training job."""
    connector_id = str(body.id)

    # Fast-check if a connector already exists before running the external probe
    with _database_errors("checking existing connector", connector_id), get_db_session() as session:
        if connectors_db.connector_exists(session):
            logger.warning(f"Create rejected for connector {connector_id}: a connector already exists")
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "A connector already exists. Only one connector is supported.",
            )

    # Probe Db2 for i connectivity, credentials, and schema before acquiring locks/inserting
    probe_db2_connector(
        host=body.connection_details.host,
        port=body.connection_details.port,
        database=body.connection_details.database,
        username=body.connection_details.username,
        password=body.connection_details.password,
        table=body.training.table,
        columns=[col.name for col in body.training.input_columns] + [body.training.target_column],
    )

    encrypted = encrypt_secrets(body.connection_details.model_dump(exclude_none=True))

    with _database_errors("creating", connector_id), get_db_session() as session:
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
        except IntegrityError:
            logger.warning(
                f"Create rejected for connector {connector_id}: duplicate connector name or ID"
            )
            APIError.raise_error(
                ErrorCode.RESOURCE_CONFLICT,
                f"A connector named {body.name!r} already exists",
            )

    # Scheduled only after the transaction has committed, so a failed insert
    # never leaves a scheduler job for a connector that does not exist.
    register_connector_job(connector_id, body.schedule)
    logger.info(f"Connector {connector_id} ({body.name!r}) created, first training queued")
    return {"id": connector_id}


# ---------------------------------------------------------------------------
# GET /v1/connectors
# ---------------------------------------------------------------------------


@router.get(
    "",
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
):
    """Return one page of connectors, optionally filtered by name."""
    with _database_errors("listing connectors"), get_db_session() as session:
        rows, total = connectors_db.list_connectors(
            session, limit=limit, offset=offset, name=name
        )
    return {
        "pagination": {"total": total, "limit": limit, "offset": offset},
        "data": [_list_item(row) for row in rows],
    }


# ---------------------------------------------------------------------------
# GET /v1/connectors/{connector_id}
# ---------------------------------------------------------------------------


@router.get(
    "/{connector_id}",
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    summary="Get a connector",
    description="One connector. Secrets, such as the password, are never returned.",
    response_description="Connector detail",
)
def get_connector(connector_id: UUID):
    """Return one connector with its safe connection details."""
    cid = str(connector_id)
    with _database_errors("getting", cid), get_db_session() as session:
        row = connectors_db.get_connector(session, cid)
    if row is None:
        logger.warning(f"Get: connector {cid} not found")
        APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"Connector {cid!r} not found")

    return {
        "id": str(row["id"]),
        "name": row["name"],
        "type": row["type"],
        "table": row["source_table"],
        "target_column": row["target_column"],
        "schedule": row["schedule"],
        "connection_details": safe_connection_details(row["config"]),
        "status": row["status"],
        "last_trained_at": _timestamp(row["last_trained_at"]),
        "message": row["message"] or "",
        "created_at": _timestamp(row["created_at"]),
        "updated_at": _timestamp(row["updated_at"]),
    }


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

    with _database_errors("updating", cid), get_db_session() as session:
        row = connectors_db.get_connector(session, cid, for_update=False)
    if row is None:
        logger.warning(f"Update: connector {cid} not found")
        APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"Connector {cid!r} not found")

    config = dict(row["config"])
    supplied = (
        body.connection_details.model_dump(exclude_unset=True)
        if body.connection_details is not None
        else {}
    )
    if supplied:
        try:
            # Only the fields that stay are decrypted, so an unreadable stored
            # secret does not block a PUT that replaces it.
            kept = decrypt_secrets(
                {
                    k: v
                    for k, v in config.items()
                    if k not in supplied and k in Db2ForIDetails.model_fields
                }
            )
            merged = Db2ForIDetails.model_validate({**kept, **supplied})
        except DecryptionError:
            logger.warning(
                f"Update of connector {cid} rejected: stored secret could not be decrypted"
            )
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                "Stored secret could not be decrypted. Please provide all required secrets in the request.",
            )
        except ValidationError as exc:
            problems = "; ".join(
                f"{'.'.join(str(part) for part in error['loc'])}: {error['msg']}"
                for error in exc.errors(include_input=False)
            )
            logger.warning(
                f"Update of connector {cid} rejected: merged connection_details are invalid: {problems}"
            )
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                f"Invalid connection_details: {problems}",
            )

        # Security check: never replay a stored password to a changed host or username
        endpoint_changed = (
            merged.host != config.get("host")
            or merged.username != config.get("username")
        )
        if endpoint_changed and "password" not in supplied:
            logger.warning(
                f"Update of connector {cid} rejected: password required when host or username changes"
            )
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                "Password must be supplied when host or username changes.",
            )

        # Probe updated Db2 credentials outside the PostgreSQL transaction
        probe_db2_connector(
            host=merged.host,
            port=merged.port,
            database=merged.database,
            username=merged.username,
            password=merged.password,
            table=row.get("source_table"),
            columns=None,
        )

        # Store the validated values, so "8471" is kept as the integer 8471.
        encrypted_supplied = encrypt_secrets(merged.model_dump(include=set(supplied)))
    else:
        encrypted_supplied = None

    if body.name is not None or body.connection_details is not None:
        with _database_errors("updating", cid), get_db_session() as session:
            current = connectors_db.get_connector(session, cid, for_update=True)
            if current is None:
                logger.warning(f"Update: connector {cid} not found")
                APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"Connector {cid!r} not found")
            
            final_config = dict(current["config"])
            if encrypted_supplied is not None:
                final_config.update(encrypted_supplied)

            try:
                connectors_db.update_connector(session, cid, name=body.name, config=final_config)
            except IntegrityError as exc:
                orig = getattr(exc, "orig", None)
                sqlstate = getattr(orig, "pgcode", None) or getattr(orig, "sqlstate", None)
                if sqlstate and sqlstate != "23505":
                    raise
                logger.warning(f"Update of connector {cid} rejected: the name {body.name!r} is taken")
                APIError.raise_error(
                    ErrorCode.RESOURCE_CONFLICT,
                    f"A connector named {body.name!r} already exists",
                )

    logger.info(f"Connector {cid} updated")
    return Response(status_code=status.HTTP_200_OK)


# ---------------------------------------------------------------------------
# GET /v1/connectors/{connector_id}/metrics
# ---------------------------------------------------------------------------


@router.get(
    "/{connector_id}/metrics",
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    summary="Connector metrics (developer use only)",
    description=(
        "Training and availability metrics of one connector, for debugging and capacity "
        "checks. The fields can change without notice and are not part of the stable API."
    ),
    response_description="Connector metrics",
)
def get_connector_metrics(connector_id: UUID):
    """Return the ready versions, training summary and job history of a connector."""
    cid = str(connector_id)
    with _database_errors("getting metrics for", cid), get_db_session() as session:
        if connectors_db.get_connector(session, cid) is None:
            logger.warning(f"Metrics: connector {cid} not found")
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"Connector {cid!r} not found")
        versions = connectors_db.list_ready_versions(session, cid)
        jobs = connectors_db.list_jobs(session, cid)

    return {
        "connector_id": cid,
        "versions": {"ready": versions},
        "training": _training_summary(jobs),
        "jobs": [_job_item(job) for job in jobs],
    }
