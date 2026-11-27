"""
Helpers shared by the forecast API endpoints.

Formatting of database rows into response models, translation of database
errors, and validation of connection_details updates.
"""

from collections.abc import Iterator
from contextlib import contextmanager
from datetime import datetime, timezone
from typing import NoReturn, Optional

from pydantic import ValidationError
from sqlalchemy.exc import SQLAlchemyError, TimeoutError as PoolTimeoutError

from common.error_utils import APIError, ErrorCode
from common.misc_utils import get_logger, get_utc_timestamp
from forecast.db.connectors import ConnectorListRow, JobRow
from forecast.models import (
    ConnectorListItem,
    Db2ForIDetails,
    JobItem,
    JobResult,
    TrainingSummary,
)
from forecast.utils.encryption import DecryptionError, EncryptionKeyError, decrypt_secrets, encrypt_secrets

logger = get_logger("forecast_api")

LOCK_NOT_AVAILABLE = "55P03"
UNIQUE_VIOLATION = "23505"

# The stored password is only ever used with these values; changing one needs a new password.
# The certificate is not listed: a supplied certificate always needs a new password (see below).
PASSWORD_BOUND_FIELDS = ("host", "port", "database", "username", "ignore_secure_connection")


# ---------------------------------------------------------------------------
# Response formatting
# ---------------------------------------------------------------------------


def format_timestamp(value: Optional[datetime]) -> Optional[str]:
    """Format a timestamp as UTC ISO 8601, whatever timezone the database session uses."""
    if value is None:
        return None
    if value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    return get_utc_timestamp(value.astimezone(timezone.utc))


def list_item(row: ConnectorListRow) -> ConnectorListItem:
    """Format a connector database row into the list response item."""
    return ConnectorListItem(
        id=str(row.id),
        name=row.name,
        type=row.type,
        created_at=format_timestamp(row.created_at),
        last_trained_at=format_timestamp(row.last_trained_at),
        status=row.status,
        table=row.source_table,
        target_column=row.target_column,
        message=row.message or "",
    )


def job_item(row: JobRow) -> JobItem:
    """Format a job database row into the metrics job history item."""
    return JobItem(
        seq=row.seq,
        status=row.status,
        created_at=format_timestamp(row.created_at),
        started_at=format_timestamp(row.started_at),
        finished_at=format_timestamp(row.finished_at),
        message=row.message or "",
        result=JobResult.model_validate(row.result) if row.result is not None else None,
    )


def training_summary(jobs: list[JobRow]) -> TrainingSummary:
    """Summarize jobs ordered newest first."""
    completed = [job for job in jobs if job.status == "COMPLETED"]
    finished = [job for job in jobs if job.status in ("COMPLETED", "FAILED")]
    last_finished = finished[0] if finished else None
    last_completed = completed[0] if completed else None
    return TrainingSummary(
        jobs_completed=len(completed),
        jobs_failed=sum(job.status == "FAILED" for job in jobs),
        last_job_status=jobs[0].status if jobs else None,
        last_duration_seconds=(
            (last_finished.result or {}).get("training_seconds") if last_finished else None
        ),
        last_completed_at=(
            format_timestamp(last_completed.finished_at) if last_completed else None
        ),
    )


# ---------------------------------------------------------------------------
# Database errors
# ---------------------------------------------------------------------------


def sqlstate(exc: Exception) -> Optional[str]:
    """Return the SQLSTATE of a database error (psycopg2 pgcode or psycopg3 sqlstate)."""
    orig = getattr(exc, "orig", None)
    return getattr(orig, "pgcode", None) or getattr(orig, "sqlstate", None)


@contextmanager
def handle_database_errors(operation: str, connector_id: Optional[str] = None) -> Iterator[None]:
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
        state = sqlstate(exc)
        state_str = f" (SQLSTATE {state})" if state else ""
        logger.error(f"Database error while {operation}{target}: {type(exc).__name__}{state_str}")
        if state == LOCK_NOT_AVAILABLE:
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "The connector is currently locked by a concurrent operation. Please retry shortly.",
            )
        APIError.raise_error(ErrorCode.DATABASE_ERROR)


# ---------------------------------------------------------------------------
# Connection details update
# ---------------------------------------------------------------------------


def _raise_unusable_key(cid: str) -> NoReturn:
    """Answer 500: the service cannot encrypt or decrypt secrets, which is not the caller's fault."""
    logger.error(f"Update of connector {cid} failed: DB_ENCRYPTION_KEY is missing or invalid")
    APIError.raise_error(
        ErrorCode.INTERNAL_SERVER_ERROR,
        "The connector encryption key of the service is missing or invalid.",
    )


def merge_connection_details(cid: str, config: dict, supplied: dict) -> dict:
    """Validate the stored details merged with the supplied ones; return the supplied fields encrypted.

    Raises INVALID_REQUEST when a kept secret cannot be decrypted, the merged details
    are invalid, or the host, port, database, username or ignore_secure_connection
    changes, or a certificate is supplied, without a new password.
    Raises INTERNAL_SERVER_ERROR when DB_ENCRYPTION_KEY is missing or invalid, because the
    service starts without it and secrets can then be neither read nor written.
    """
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
    except EncryptionKeyError:
        _raise_unusable_key(cid)
    except DecryptionError:
        logger.warning(f"Update of connector {cid} rejected: stored secret could not be decrypted")
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

    # Security check: never replay a stored password to a changed target, identity or TLS trust.
    # A stored connector that predates ignore_secure_connection has it unset, which means false.
    changed = [
        f
        for f in PASSWORD_BOUND_FIELDS
        if getattr(merged, f) != config.get(f, False if f == "ignore_secure_connection" else None)
    ]
    if "certificate" in supplied:
        changed.append("certificate")
    if changed and "password" not in supplied:
        logger.warning(
            f"Update of connector {cid} rejected: password required when "
            f"{', '.join(changed)} changes"
        )
        APIError.raise_error(
            ErrorCode.INVALID_REQUEST,
            "Password must be supplied when host, port, database, username, "
            "ignore_secure_connection or certificate changes.",
        )

    # Store the validated values, so "8471" is kept as the integer 8471.
    try:
        return encrypt_secrets(merged.model_dump(include=set(supplied)))
    except EncryptionKeyError:
        _raise_unusable_key(cid)
