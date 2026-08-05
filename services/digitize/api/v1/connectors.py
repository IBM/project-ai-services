"""
Connector REST API endpoints.

Mounted at /v1/connectors by app.py.

Endpoints:
  POST   /v1/connectors
  PUT    /v1/connectors/{connector_id}
  DELETE /v1/connectors/{connector_id}
  GET    /v1/connectors
  GET    /v1/connectors/{connector_id}
  GET    /v1/connectors/{connector_id}/syncs
"""

from typing import List, Optional

from fastapi import APIRouter, HTTPException, Query, Response, status
from sqlalchemy.exc import IntegrityError

from common.misc_utils import cleanup_staging_directory, get_logger, get_utc_timestamp
from common.error_utils import APIError, ErrorCode, http_error_responses
from digitize.connectors.models import (
    ConnectorCreateRequest,
    ConnectorDetailResponse,
    ConnectorListItem,
    ConnectorUpdateRequest,
    SyncLogItem,
    SyncLogResponse,
)
from digitize.connectors.encryption import (
    encrypt_secrets,
    merge_and_encrypt_partial,
    strip_secrets,
)
import digitize.utils.db as db_ops
from digitize.settings import settings

router = APIRouter()
logger = get_logger("connectors_router")

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_KEY_PATH = None  # resolved lazily via _get_key_path()


def _get_key_path() -> str:
    """Return the encryption key path from settings."""
    return settings.digitize.connector.encryption_key_path


# ---------------------------------------------------------------------------
# POST /v1/connectors
# ---------------------------------------------------------------------------

@router.post(
    "",
    status_code=status.HTTP_202_ACCEPTED,
    responses={
        409: http_error_responses[409],
        500: http_error_responses[500],
    },
    summary="Attach a data-source connector",
    description=(
        "Creates a connector, stores encrypted credentials, and schedules the "
        "worker to start asynchronously. The worker runs its first tick immediately "
        "after the thread starts. sync_interval_seconds is taken from the "
        "CONNECTOR_SYNC_INTERVAL_SECONDS env variable and cannot be set per-request."
    ),
    response_description="Connector created; worker start scheduled",
)
async def create_connector(body: ConnectorCreateRequest):
    try:
        key_path = _get_key_path()
        encrypted_details = encrypt_secrets(body.type, body.connection_details, key_path)

        sync_interval = settings.digitize.connector.sync_interval_seconds

        db_ops.insert_connector(
            connector_id=body.connector_id,
            name=body.connector_name,
            connector_type=body.type,
            connection_details=encrypted_details,
            allowed_extensions=body.allowed_extensions,
            sync_interval_seconds=sync_interval,
        )

        logger.info(
            f"Connector {body.connector_id!r} ({body.connector_name!r}) attached "
            f"(type={body.type}, interval={sync_interval}s)"
        )
        # Worker start is a no-op stub in PR3 — the worker manager (PR7) will hook
        # in here once implemented.
        return Response(status_code=200)

    except IntegrityError:
        # id or name already exists
        APIError.raise_error(
            ErrorCode.RESOURCE_LOCKED,
            f"Connector {body.connector_id!r} or name {body.connector_name!r} already exists",
        )
    except RuntimeError as exc:
        # encryption key not found
        logger.error(f"Encryption key error: {exc}")
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))
    except HTTPException:
        raise
    except Exception as exc:
        logger.error(f"Unexpected error creating connector: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


# ---------------------------------------------------------------------------
# PUT /v1/connectors/{connector_id}
# ---------------------------------------------------------------------------

@router.put(
    "/{connector_id}",
    status_code=status.HTTP_200_OK,
    responses={
        404: http_error_responses[404],
        409: http_error_responses[409],
        500: http_error_responses[500],
    },
    summary="Update a connector's configuration",
    description=(
        "Partial update — only supplied fields are written. "
        "connection_details is merged at the key level (untouched keys survive). "
        "If credentials are included they are re-encrypted before storage. "
        "type and sync_interval_seconds cannot be changed."
    ),
    response_description="Connector updated; running worker picks up changes on next tick",
)
async def update_connector(connector_id: str, body: ConnectorUpdateRequest):
    try:
        # Nothing to update — treat as success
        if body.connector_name is None and body.allowed_extensions is None and body.connection_details is None:
            return Response(status_code=200)

        key_path = _get_key_path()

        # If connection_details is being updated, we need to merge with existing
        # encrypted details so untouched keys stay encrypted and intact.
        merged_details: Optional[dict] = None
        if body.connection_details is not None:
            existing = db_ops.get_active_connector(connector_id)
            if existing is None:
                APIError.raise_error(
                    ErrorCode.RESOURCE_NOT_FOUND,
                    f"Connector {connector_id!r} not found",
                )
            merged_details = merge_and_encrypt_partial(
                existing.type,
                existing.connection_details,
                body.connection_details,
                key_path,
            )

        db_ops.upsert_connector(
            connector_id=connector_id,
            name=body.connector_name,
            connection_details=merged_details,
            allowed_extensions=body.allowed_extensions,
        )

        logger.info(f"Connector {connector_id!r} updated")
        return Response(status_code=200)

    except FileNotFoundError:
        APIError.raise_error(
            ErrorCode.RESOURCE_NOT_FOUND,
            f"Connector {connector_id!r} not found",
        )
    except IntegrityError:
        APIError.raise_error(
            ErrorCode.RESOURCE_LOCKED,
            f"Connector name {body.connector_name!r} is already in use",
        )
    except RuntimeError as exc:
        logger.error(f"Encryption key error: {exc}")
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))
    except HTTPException:
        raise
    except Exception as exc:
        logger.error(f"Unexpected error updating connector {connector_id}: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


# ---------------------------------------------------------------------------
# DELETE /v1/connectors/{connector_id}
# ---------------------------------------------------------------------------

@router.delete(
    "/{connector_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        404: http_error_responses[404],
        409: http_error_responses[409],
        500: http_error_responses[500],
    },
    summary="Detach and delete a connector",
    description=(
        "Removes a connector and all its associated state. "
        "Rejected with 409 if a sync tick is currently in progress. "
        "Documents owned exclusively by this connector are deleted. "
        "Staging directories are cleaned up best-effort."
    ),
    response_description="No content on successful deletion",
)
async def delete_connector(connector_id: str):
    """
    DELETE stub for PR3 (no worker yet):
    1. Check connector exists → 404 if not
    2. Guard: check if sync is in progress (sync_status == 'syncing') → 409
    3. Snapshot checksums owned by this connector
    4. Remove ownership rows and delete documents when last owner
    5. Delete the connector row
    6. Best-effort staging dir cleanup
    """
    try:
        connector = db_ops.get_active_connector(connector_id)
        if connector is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"Connector {connector_id!r} not found",
            )

        # Guard: reject if a tick is currently running
        if connector.sync_status == "syncing":
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "A sync tick is currently running for this connector. "
                "Retry after the tick completes.",
            )

        # Snapshot checksums owned by this connector
        owned_checksums = db_ops.list_connector_checksums(connector_id)

        # Remove ownership rows; delete documents when last owner
        for checksum in owned_checksums:
            try:
                remaining, doc_id = db_ops.remove_connector_checksum_entry(connector_id, checksum)
                if remaining == 0 and doc_id:
                    _best_effort_delete_document(doc_id)
            except Exception as exc:
                logger.error(
                    f"Error removing checksum {checksum!r} for connector "
                    f"{connector_id!r}: {exc}",
                    exc_info=True,
                )

        # Delete the connector row (cascades to connector_sync_logs)
        deleted = db_ops.delete_active_connector(connector_id)
        if not deleted:
            logger.warning(
                f"delete_active_connector returned False for {connector_id!r} "
                "— row may have already been removed"
            )

        # Best-effort sweep of any residual batch staging directories.
        # Normal teardown: per-batch dirs are already gone (cleaned up in
        # _process_new_files finally blocks). This glob catches anything left
        # behind by a mid-tick crash: staging/connectors/<connector_id>-*
        _sweep_connector_staging(connector_id, settings.digitize.staging_dir / "connectors")

        logger.info(f"Connector {connector_id!r} detached and deleted")
        return None

    except HTTPException:
        raise
    except Exception as exc:
        logger.error(f"Unexpected error deleting connector {connector_id}: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


def _best_effort_delete_document(doc_id: str) -> None:
    """
    Delete a document via the db_manager.  200 / 204 / 404 are all treated
    as success — 5xx and unexpected exceptions are logged and swallowed.
    """
    try:
        from digitize.db.manager import db_manager
        db_manager.delete_document(doc_id)
        logger.debug(f"Deleted document {doc_id!r} (connector cleanup)")
    except Exception as exc:
        logger.error(
            f"Best-effort document deletion failed for {doc_id!r}: {exc}",
            exc_info=True,
        )


def _sweep_connector_staging(connector_id: str, staging_connectors_dir) -> None:
    """
    Remove any residual batch staging directories for *connector_id*.

    Per-batch dirs are named ``<connector_id>-<job_id>-<batch_number>`` and are
    cleaned up inline after each ingest.  This sweep only has work to do when a
    worker crashed mid-tick and left a directory behind.
    """
    from pathlib import Path

    base = Path(staging_connectors_dir)
    if not base.exists():
        return
    prefix = f"{connector_id}-"
    for entry in base.iterdir():
        if entry.is_dir() and entry.name.startswith(prefix):
            cleanup_staging_directory(entry.name, base, ignore_errors=True)
            logger.debug(f"Swept residual staging dir {entry.name!r} for connector {connector_id!r}")


# ---------------------------------------------------------------------------
# GET /v1/connectors
# ---------------------------------------------------------------------------

@router.get(
    "",
    response_model=List[ConnectorListItem],
    responses={500: http_error_responses[500]},
    summary="List all connectors",
    description=(
        "Returns all attached connectors with their sync state. "
        "Secret connection fields (private_key, secret_access_key) are never included."
    ),
    response_description="List of connectors",
)
async def list_connectors():
    try:
        connectors = db_ops.list_connectors()
        return [
            ConnectorListItem(
                connector_id=c.id,
                connector_name=c.name,
                type=c.type,
                attached_at=get_utc_timestamp(c.attached_at),
                last_sync_at=get_utc_timestamp(c.last_sync_at),
                sync_status=c.sync_status,
                last_sync_error=c.last_sync_error,
                total_files=c.total_files,
            )
            for c in connectors
        ]
    except HTTPException:
        raise
    except Exception as exc:
        logger.error(f"Unexpected error listing connectors: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


# ---------------------------------------------------------------------------
# GET /v1/connectors/{connector_id}
# ---------------------------------------------------------------------------

@router.get(
    "/{connector_id}",
    response_model=ConnectorDetailResponse,
    responses={
        404: http_error_responses[404],
        500: http_error_responses[500],
    },
    summary="Get a single connector",
    description=(
        "Returns one connector with its latest file-processing counters. "
        "Secret connection fields are stripped from the response."
    ),
    response_description="Connector detail",
)
async def get_connector(connector_id: str):
    try:
        connector = db_ops.get_active_connector(connector_id)
        if connector is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"Connector {connector_id!r} not found",
            )

        # Fetch latest sync stats from the most recent log row (if any)
        logs, _ = db_ops.list_sync_logs(connector_id, limit=1, offset=0)
        latest_log = logs[0] if logs else None

        return ConnectorDetailResponse(
            connector_id=connector.id,
            connector_name=connector.name,
            type=connector.type,
            allowed_extensions=list(connector.allowed_extensions or []),
            sync_interval_seconds=connector.sync_interval_seconds,
            attached_at=get_utc_timestamp(connector.attached_at),
            last_sync_at=get_utc_timestamp(connector.last_sync_at),
            sync_status=connector.sync_status,
            last_sync_error=connector.last_sync_error,
            connection_details=strip_secrets(connector.type, connector.connection_details or {}),
            total_files=connector.total_files,
            new_files=latest_log.new_files if latest_log else 0,
            removed_files=latest_log.removed_files if latest_log else 0,
            failed_files=latest_log.failed_files if latest_log else 0,
        )
    except HTTPException:
        raise
    except Exception as exc:
        logger.error(f"Unexpected error fetching connector {connector_id}: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


# ---------------------------------------------------------------------------
# GET /v1/connectors/{connector_id}/syncs
# ---------------------------------------------------------------------------

@router.get(
    "/{connector_id}/syncs",
    response_model=SyncLogResponse,
    responses={
        404: http_error_responses[404],
        500: http_error_responses[500],
    },
    summary="Get sync log for a connector",
    description="Returns paginated sync log for a connector, newest first.",
    response_description="Paginated sync log",
)
async def get_sync_history(
    connector_id: str,
    limit: int = Query(50, ge=1, le=200, description="Max records to return (capped at 200)"),
    offset: int = Query(0, ge=0, description="Zero-based offset"),
):
    try:
        connector = db_ops.get_active_connector(connector_id)
        if connector is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"Connector {connector_id!r} not found",
            )

        logs, total = db_ops.list_sync_logs(connector_id, limit=limit, offset=offset)

        items = [
            SyncLogItem(
                id=log.id,
                started_at=get_utc_timestamp(log.started_at) or "",
                finished_at=get_utc_timestamp(log.finished_at),
                total_files=log.total_files,
                new_files=log.new_files,
                removed_files=log.removed_files,
                failed_files=log.failed_files,
                status=log.status,
                error=log.error or "",
            )
            for log in logs
        ]

        return SyncLogResponse(total=total, limit=limit, offset=offset, items=items)

    except HTTPException:
        raise
    except Exception as exc:
        logger.error(
            f"Unexpected error fetching sync log for {connector_id}: {exc}",
            exc_info=True,
        )
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))

# Made with Bob
