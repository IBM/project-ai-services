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
  GET    /v1/connectors/{connector_id}/syncs/{sync_seq}
  POST   /v1/connectors/{connector_id}/syncs
  POST   /v1/connectors/{connector_id}/syncs/{sync_seq}/stop
"""

import asyncio
import uuid
from typing import List, Optional

from fastapi import APIRouter, HTTPException, Query, Response, status
from sqlalchemy.exc import IntegrityError

from common.misc_utils import cleanup_staging_directory, get_logger, get_utc_timestamp
from common.error_utils import APIError, ErrorCode, http_error_responses
from digitize.connectors.scanners.scanner_factory import build_scanner
from digitize.connectors.models import (
    ConnectorCreateRequest,
    ConnectorDetailResponse,
    ConnectorListItem,
    ConnectorUpdateRequest,
    SyncLogDetailResponse,
    SyncLogItem,
    SyncLogResponse,
    ConnectorStatus,
    SyncLogStatus,
    SyncTriggerResponse,
    ConnectorError,
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

_CREDENTIAL_ERROR_MSG = ConnectorError.CREDENTIAL_ERROR_MSG  # canonical value lives in connectors.models


async def _probe_connector_credentials(
    connector_id: str,
    connector_type: str,
    encrypted_details: dict,
    allowed_extensions: list,
    current_error: Optional[str] = None,
) -> None:
    """
    Attempt a real connection using the supplied (encrypted) credentials.

    On failure: the connector's error field is set to a generic auth-failure message.

    On success: if the connector previously had an auth-failure error (i.e. the error
    was set by a prior probe), it is cleared so the connector no longer shows a stale
    credential warning.  Unrelated errors (e.g. sync errors) are left untouched.
    """
    class _Row:
        """Minimal duck-type accepted by build_scanner."""
        def __init__(self, ctype: str, details: dict, extensions: list) -> None:
            self.type = ctype
            self.connection_details = details
            self.allowed_extensions = extensions

    scanner = build_scanner(_Row(connector_type, encrypted_details, allowed_extensions))
    loop = asyncio.get_event_loop()
    try:
        await loop.run_in_executor(None, scanner.connect)
        await loop.run_in_executor(None, scanner.close)
        logger.info(f"Credential probe succeeded for connector {connector_id!r}")
        # Clear the error only if it was previously set by a credential probe —
        # leave any sync-related error intact.
        if current_error == _CREDENTIAL_ERROR_MSG:
            db_ops.set_connector_error(connector_id, None)
    except Exception as exc:
        logger.error(
            f"Credential probe failed for connector {connector_id!r}: {exc}",
            exc_info=True,
        )
        db_ops.set_connector_error(connector_id, _CREDENTIAL_ERROR_MSG)
        try:
            await loop.run_in_executor(None, scanner.close)
        except Exception:
            pass


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
    """Create a new connector and schedule its worker.

    Validates connector settings, encrypts credentials, persists the new connector
    configuration in the database, and schedules the background worker.
    """
    connector_id = body.id or str(uuid.uuid4())
    try:
        encrypted_details = encrypt_secrets(body.type, body.connection_details)

        sync_interval = settings.digitize.connector.sync_interval_seconds

        # Register the connector with the scheduler so it starts ticking
        # immediately (fire_immediately=True for the first-ever sync).
        try:
            import digitize.connectors.scheduler as _sched
            await _sched.register_connector_job(
                connector_id, sync_interval, fire_immediately=True
            )
        except Exception as sched_exc:
            logger.error(
                f"Scheduler registration failed for {connector_id!r}: {sched_exc}",
                exc_info=True,
            )
            raise

        db_ops.insert_connector(
            connector_id=connector_id,
            name=body.name,
            connector_type=body.type,
            connection_details=encrypted_details,
            allowed_extensions=body.allowed_extensions,
            sync_interval_seconds=sync_interval,
        )

        asyncio.create_task(
            _probe_connector_credentials(
                connector_id,
                body.type,
                encrypted_details,
                body.allowed_extensions,
            )
        )

        logger.info(
            f"Connector {connector_id!r} ({body.name!r}) attached "
            f"(type={body.type}, interval={sync_interval}s)"
        )

        return Response(
            content=f'{{"id": "{connector_id}"}}',
            status_code=201,
            media_type="application/json",
        )

    except IntegrityError:
        # id or name already exists
        APIError.raise_error(
            ErrorCode.RESOURCE_LOCKED,
            f"Connector {connector_id!r} or name {body.name!r} already exists",
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
    """Update an existing connector's configurations.

    Encrypts updated secrets if provided and applies modifications. Running workers
    will pick up the updated settings on their next execution interval.
    """
    try:
        # Nothing to update — treat as success
        if body.name is None and body.allowed_extensions is None and body.connection_details is None:
            return Response(status_code=200)

        existing = db_ops.get_active_connector(connector_id)
        if existing is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"Connector {connector_id!r} not found",
            )
        if existing.sync_status == ConnectorStatus.DELETE_PENDING:
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                f"Connector {connector_id!r} is pending deletion and cannot be updated",
            )

        # If connection_details is being updated, we need to merge with existing
        # encrypted details so untouched keys stay encrypted and intact.
        merged_details: Optional[dict] = None
        if body.connection_details is not None:
            merged_details = merge_and_encrypt_partial(
                existing.type,
                existing.connection_details,
                body.connection_details,
            )

        db_ops.upsert_connector(
            connector_id=connector_id,
            name=body.name,
            connection_details=merged_details,
            allowed_extensions=body.allowed_extensions,
        )

        if merged_details is not None:
            # Connection details changed — probe the new credentials.
            probe_extensions = body.allowed_extensions or existing.allowed_extensions or []
            asyncio.create_task(
                _probe_connector_credentials(
                    connector_id,
                    existing.type,
                    merged_details,
                    probe_extensions,
                    current_error=existing.error,
                )
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
            f"Connector name {body.name!r} is already in use",
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
        500: http_error_responses[500],
    },
    summary="Detach and delete a connector",
    description=(
        "Non-blocking detachment. Always returns 204 immediately. "
        "If a sync tick is running the connector is marked delete_pending and "
        "the tick handles teardown; otherwise teardown runs as a background task."
    ),
    response_description="No content — teardown proceeds in the background",
)
async def delete_connector(connector_id: str):
    """Fast, non-blocking DELETE.

    Case A — sync_status == 'syncing':
        Mark DELETE_PENDING. The running tick will hit _check_delete_pending at
        its next checkpoint, cancel itself, and dispatch teardown.
        Return 204 immediately.

    Case B — sync_status != 'syncing':
        Mark DELETE_PENDING, dispatch asyncio.create_task(_run_teardown(...)),
        return 204 immediately.
    """
    try:
        connector = db_ops.get_active_connector(connector_id)
        if connector is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"Connector {connector_id!r} not found",
            )

        db_ops.mark_connector_delete_pending(connector_id)

        if connector.sync_status != ConnectorStatus.SYNCING:
            # No tick running — kick off teardown ourselves.
            asyncio.create_task(_run_teardown(connector_id))

        return Response(status_code=204)

    except HTTPException:
        raise
    except Exception as exc:
        logger.error(f"Unexpected error deleting connector {connector_id}: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


async def _run_teardown(connector_id: str) -> None:
    """
    Teardown for connector deletion.

    Scheduled via asyncio.create_task from the delete endpoint (Case B),
    and awaited directly from _handle_interrupt in sync_tick (Case A).

    Steps:
      1. Remove the scheduled job so no new ticks fire
      2. Snapshot checksums owned by this connector
      3. Remove ownership rows; delete documents when last owner
      4. Sweep residual batch staging directories
      5. Delete the connector row (cascades to connector_sync_logs)
    """
    logger.info(f"Starting teardown for connector {connector_id!r}")
    deletion_failed = False
    try:
        # Step 1: Remove the scheduled job so no new ticks fire after this point.
        try:
            import digitize.connectors.scheduler as _sched
            await _sched.remove_connector_job(connector_id)
        except Exception as sched_exc:
            deletion_failed = True
            logger.warning(
                f"Could not remove scheduler job for {connector_id!r}: {sched_exc}"
            )

        # Steps 2+3: remove checksum ownership; delete orphaned documents
        owned_checksums = db_ops.list_connector_checksums(connector_id)
        for checksum in owned_checksums:
            try:
                remaining, doc_id = db_ops.remove_connector_checksum_entry(connector_id, checksum)
                if remaining == 0 and doc_id:
                    if not _best_effort_delete_document(doc_id):
                        deletion_failed = True
            except Exception as exc:
                deletion_failed = True
                logger.error(
                    f"Error removing checksum {checksum!r} for connector "
                    f"{connector_id!r}: {exc}",
                    exc_info=True,
                )

        # Step 4: sweep any residual batch staging directories
        if not _sweep_staging_dir(connector_id, settings.digitize.staging_dir / "connectors"):
            deletion_failed = True

        if deletion_failed:
            db_ops.set_connector_error(connector_id, "Teardown failed — skipping connector row deletion")
            logger.warning(f"Skipping connector row deletion for {connector_id!r} due to teardown failures")
            return

        # Step 5: delete the connector row (cascades to connector_sync_logs)
        deleted = db_ops.delete_active_connector(connector_id)
        if not deleted:
            logger.warning(
                f"delete_active_connector returned False for {connector_id!r} "
                "— row may have already been removed"
            )

        logger.info(f"Connector {connector_id!r} teardown complete")
    except Exception as exc:
        logger.error(
            f"Unexpected error during teardown for connector {connector_id!r}: {exc}",
            exc_info=True,
        )
        db_ops.set_connector_error(connector_id, "Documents deletion failed")


def _best_effort_delete_document(doc_id: str) -> bool:
    """
    Delete a document via the full teardown path (VDB → files → DB record).

    Calls delete_document_data() so that indexed chunks and output files are
    cleaned up — not just the DB row.
    All failures are logged and swallowed (best-effort semantics).

    Returns True on success, False if an error occurred.
    """
    try:
        from digitize.api.v1.documents import delete_document_data
        delete_document_data(doc_id)
        logger.debug(f"Deleted document {doc_id!r} (connector cleanup)")
        return True
    except Exception as exc:
        logger.error(
            f"Best-effort document deletion failed for {doc_id!r}: {exc}",
            exc_info=True,
        )
        return False


def _sweep_staging_dir(
    connector_id: str,
    staging_connectors_dir,
    sync_seq: int | None = None,
) -> bool:
    """
    Remove any residual batch staging directories for *connector_id*.

    Per-batch dirs are named ``<connector_id>-<job_id>-<batch_number>`` and are
    cleaned up inline after each ingest.  This sweep only has work to do when a
    worker crashed mid-tick and left a directory behind.

    When *sync_seq* is given the sweep is narrowed to dirs matching
    ``<connector_id>-<sync_seq>-*`` (i.e. only the batches of that sync).

    Returns True on success, False if an error occurred.
    """
    from pathlib import Path

    base = Path(staging_connectors_dir)
    if not base.exists():
        return True
    prefix = f"{connector_id}-{sync_seq}-" if sync_seq is not None else f"{connector_id}-"
    try:
        for entry in base.iterdir():
            if entry.is_dir() and entry.name.startswith(prefix):
                cleanup_staging_directory(entry.name, base, ignore_errors=True)
                logger.debug(f"Swept residual staging dir {entry.name!r} for connector {connector_id!r}")
        return True
    except Exception as exc:
        logger.error(
            f"Error sweeping staging dir for connector {connector_id!r}: {exc}",
            exc_info=True,
        )
        return False


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
    """Retrieve a list of all active connectors with their current sync state.

    Strips out any sensitive/secret connection details from the response.
    """
    try:
        connectors = db_ops.list_connectors()
        return [
            ConnectorListItem(
                id=c.id,
                name=c.name,
                type=c.type,
                attached_at=get_utc_timestamp(c.attached_at),
                last_sync_at=get_utc_timestamp(c.last_sync_at),
                sync_status=c.sync_status,
                error=c.error,
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
    """Retrieve detailed information for a single connector by its ID.

    Strips out any sensitive/secret connection details from the response.
    """
    try:
        connector = db_ops.get_active_connector(connector_id)
        if connector is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"Connector {connector_id!r} not found",
            )

        return ConnectorDetailResponse(
            id=connector.id,
            name=connector.name,
            type=connector.type,
            allowed_extensions=list(connector.allowed_extensions or []),
            sync_interval_seconds=connector.sync_interval_seconds,
            attached_at=get_utc_timestamp(connector.attached_at),
            last_sync_at=get_utc_timestamp(connector.last_sync_at),
            sync_status=connector.sync_status,
            error=connector.error,
            connection_details=strip_secrets(connector.type, connector.connection_details or {}),
            total_files=connector.total_files,
        )
    except HTTPException:
        raise
    except Exception as exc:
        logger.error(f"Unexpected error fetching connector {connector_id}: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


# ---------------------------------------------------------------------------
# POST /v1/connectors/{connector_id}/syncs
# ---------------------------------------------------------------------------


class SyncNotFound(Exception):
    """Raised by dispatch_sync when the connector does not exist."""


class SyncLocked(Exception):
    """Raised by dispatch_sync when the connector cannot accept a new sync
    (DELETE_PENDING or a cancellation already in progress)."""


async def dispatch_sync(connector_id: str) -> int:
    """Dispatch a sync tick for *connector_id* and return the active sync_seq.

    This is the shared core used by both the HTTP handler (``trigger_sync``)
    and the APScheduler (registered directly via ``register_connector_job``).
    It contains no FastAPI or HTTP concerns — callers map the exceptions it
    raises to their own error handling:

    Raises
    ------
    SyncNotFound
        The connector does not exist.
    SyncLocked
        The connector is pending deletion, or a cancellation is already in
        progress and a new sync cannot be accepted yet.
    RuntimeError
        The sync lock is held but no active sync-log row exists (should not
        happen under normal operation).

    Returns
    -------
    int
        The ``sync_seq`` of the active sync — either the newly dispatched one
        or the already-running one (idempotent: safe to call when a tick is
        already in progress).

    APScheduler usage
    -----------------
    The scheduler registers this function directly as the job callable::

        from digitize.api.v1.connectors import dispatch_sync
        await sched.add_schedule(func_or_task_id=dispatch_sync, args=[connector_id], ...)
    """
    from digitize.connectors.sync_tick import run_tick

    connector = db_ops.get_active_connector(connector_id)
    if connector is None:
        raise SyncNotFound(f"Connector {connector_id!r} not found")
    if connector.sync_status == ConnectorStatus.DELETE_PENDING:
        raise SyncLocked(
            f"Connector {connector_id!r} is pending deletion and cannot accept new syncs."
        )

    active_seq = db_ops.get_active_sync_seq(connector_id)
    if active_seq is not None:
        sync_log_status = db_ops.get_sync_log_status(connector_id, active_seq)
        if sync_log_status == SyncLogStatus.CANCEL_PENDING:
            raise SyncLocked(
                f"Connector {connector_id!r} has a sync cancellation in progress "
                f"(seq={active_seq}) and cannot accept a new sync until it completes."
            )

    acquired = db_ops.try_acquire_sync_lock(connector_id)
    if acquired:
        # Open the sync-log row synchronously before dispatching the background
        # task so the seq is known immediately — no polling required.
        sync_seq = db_ops.init_sync_log_and_update_connector(connector_id)
        asyncio.create_task(run_tick(connector_id, sync_seq))
        logger.info(f"Sync dispatched for connector {connector_id!r}, seq={sync_seq}")
    else:
        sync_seq = db_ops.get_active_sync_seq(connector_id)
        if sync_seq is None:
            raise RuntimeError(
                f"Sync lock held but no active sync-log row found for connector {connector_id!r}"
            )
        logger.info(f"Sync already in progress for connector {connector_id!r}, seq={sync_seq}")

    return sync_seq


@router.post(
    "/{connector_id}/syncs",
    status_code=status.HTTP_202_ACCEPTED,
    response_model=SyncTriggerResponse,
    responses={
        404: http_error_responses[404],
        500: http_error_responses[500],
    },
    summary="Trigger an immediate manual sync",
    description=(
        "Dispatches a sync tick for the connector immediately. "
        "Safe and idempotent: if a tick is already running the request is "
        "accepted without starting a duplicate (no-op 202). "
        "The tick runs asynchronously; this endpoint returns as soon as the "
        "task has been dispatched. "
        "Always returns the sync_seq of the active sync — either the newly "
        "dispatched one or the already-running one."
    ),
    response_description="Sync dispatched (or already in progress)",
)
async def trigger_sync(connector_id: str):
    """Manually dispatch a synchronization task for a specified connector.

    If a synchronization task is already in progress, returns details for the
    currently active run.
    """
    try:
        sync_seq = await dispatch_sync(connector_id)
        return SyncTriggerResponse(sync_seq=sync_seq)
    except SyncNotFound as exc:
        APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, str(exc))
    except SyncLocked as exc:
        APIError.raise_error(ErrorCode.RESOURCE_LOCKED, str(exc))
    except HTTPException:
        raise
    except Exception as exc:
        logger.error(f"Unexpected error triggering sync for {connector_id}: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


# ---------------------------------------------------------------------------
# POST /v1/connectors/{connector_id}/syncs/{sync_seq}/stop
# ---------------------------------------------------------------------------

@router.post(
    "/{connector_id}/syncs/{sync_seq}/stop",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        404: http_error_responses[404],
        409: http_error_responses[409],
        500: http_error_responses[500],
    },
    summary="Stop a running sync",
    description=(
        "Signals the active sync tick to stop at its next cancellation checkpoint. "
        "The caller must supply the sync_seq of the sync they intend to cancel. "
        "Returns 409 if sync_seq does not match the currently-running sync "
        "(stale seq) or if no sync is running at all. "
        "Returns 204 immediately; the tick exits asynchronously and the sync log "
        "is marked 'cancelled'. The connector remains and resumes its normal "
        "schedule on the next interval."
    ),
    response_description="Stop signal sent",
)
async def cancel_sync(connector_id: str, sync_seq: int):
    """Request cancellation/stopping of a currently running sync task.

    Sends a cancellation signal to the connector worker.
    """
    try:
        connector = db_ops.get_active_connector(connector_id)
        if connector is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"Connector {connector_id!r} not found",
            )
        if connector.sync_status != ConnectorStatus.SYNCING:
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "No sync is currently running for this connector.",
            )

        active_seq = db_ops.get_active_sync_seq(connector_id)
        if active_seq is None or active_seq != sync_seq:
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                f"sync_seq {sync_seq} is not the active sync for this connector.",
            )

        signalled = db_ops.mark_sync_cancel_pending(connector_id)
        if not signalled:
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "No sync is currently running for this connector.",
            )

        logger.info(f"Cancel-sync signal sent for connector {connector_id!r} (seq={sync_seq})")
        return Response(status_code=204)

    except HTTPException:
        raise
    except Exception as exc:
        logger.error(f"Unexpected error cancelling sync for {connector_id}: {exc}", exc_info=True)
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
    """Retrieve a paginated history of sync runs for a specific connector."""
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
                seq=log.seq,
                started_at=get_utc_timestamp(log.started_at) or "",
                finished_at=get_utc_timestamp(log.finished_at),
                total_files=log.total_files,
                new_files=log.new_files,
                removed_files=log.removed_files,
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


# ---------------------------------------------------------------------------
# GET /v1/connectors/{connector_id}/syncs/{sync_seq}
# ---------------------------------------------------------------------------

@router.get(
    "/{connector_id}/syncs/{sync_seq}",
    response_model=SyncLogDetailResponse,
    responses={
        404: http_error_responses[404],
        500: http_error_responses[500],
    },
    summary="Get a specific sync log entry",
    description="Returns one sync log entry identified by its sequence number.",
    response_description="Sync log entry",
)
async def get_sync(connector_id: str, sync_seq: int):
    """Retrieve details of a single sync run by its sequence number."""
    try:
        connector = db_ops.get_active_connector(connector_id)
        if connector is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"Connector {connector_id!r} not found",
            )

        log = db_ops.get_sync_log(connector_id, sync_seq)
        if log is None:
            APIError.raise_error(
                ErrorCode.RESOURCE_NOT_FOUND,
                f"Sync {sync_seq} not found for connector {connector_id!r}",
            )

        return SyncLogDetailResponse(
            seq=log.seq,
            started_at=get_utc_timestamp(log.started_at) or "",
            finished_at=get_utc_timestamp(log.finished_at),
            total_files=log.total_files,
            new_files=log.new_files,
            removed_files=log.removed_files,
            status=log.status,
            error=log.error or "",
        )

    except HTTPException:
        raise
    except Exception as exc:
        logger.error(
            f"Unexpected error fetching sync {sync_seq} for {connector_id}: {exc}",
            exc_info=True,
        )
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))

# Made with Bob
