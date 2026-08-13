"""
connectors/sync_tick.py — end-to-end sync-tick logic for one connector.

Phases
------
1.  open_new_sync_log        → INSERT connector_sync_logs row (status='started')
2.  load known/all checksums + scanner.scan()
3.  _classify()              → ingest_list, orphan_checksums
                               cross-connector dups registered inline
3b. update_sync_log()        → single DB write of total/new/removed counts (all known post-classify)
4a. _process_new_files()     → download → create job/doc → add checksum row
4b. _delete_orphans()        → remove checksum rows; delete docs with no owners
5.  close_sync_log()         → finalize tick row with terminal status + reset sync_status

Called by the APScheduler job (_run_tick_wrapped in scheduler.py, PR7) and
directly by POST /v1/connectors/{id}/sync after the caller has already
acquired the sync lock via try_acquire_sync_lock().
"""

from __future__ import annotations

import asyncio
from enum import Enum
from pathlib import Path
from typing import Optional

from common.misc_utils import cleanup_staging_directory, get_logger
from digitize.connectors.scanners.scanner_factory import build_scanner
from digitize.pipeline.ingest import ingest
from digitize.settings import settings
from digitize.connectors.models import ConnectorStatus, SyncLogStatus
from digitize.models import JobStatus, OutputFormat, OperationType
from digitize.utils.db import (
    add_connector_checksum_entry,
    close_sync_log,
    get_active_connector,
    get_connector_sync_status,
    get_sync_log_status,
    get_job,
    list_all_checksums,
    list_connector_checksums,
    lookup_connector_content_by_checksum,
    open_new_sync_log,
    remove_connector_checksum_entry,
    update_sync_log,
)
from digitize.utils.jobs import generate_uuid, initialize_job_state

logger = get_logger("sync_tick")


# ---------------------------------------------------------------------------
# Interrupt handling enum
# ---------------------------------------------------------------------------

class InterruptType(str, Enum):
    """Indicates the type of interrupt signal detected during sync execution."""
    SYNC_CANCEL = "sync_cancel"      # CANCEL_PENDING in ConnectorSyncLog
    DELETE_CONNECTOR = "delete_connector"  # DELETE_PENDING in Connectors table


# ---------------------------------------------------------------------------
# Cancellation helper
# ---------------------------------------------------------------------------

def _check_interrupt_call(connector_id: str, sync_seq: int) -> Optional[InterruptType]:
    """
    Check for interrupt signals from both connectors and connector_sync_logs tables.

    Checks two sources:
    1. connectors.sync_status for DELETE_PENDING (delete entire connector)
    2. connector_sync_logs.status for CANCEL_PENDING on the active seq row
       (cancel only the current sync; written by mark_sync_cancel_pending)

    Returns the appropriate InterruptType if a signal is detected, None otherwise.
    Performs a live DB query — no in-memory state. Called at phase boundaries
    inside run_tick / _process_new_files so that a DELETE or CANCEL request is
    honoured promptly without leaving the tick running to completion.
    """
    # Check connectors table for DELETE_PENDING
    connector_status = get_connector_sync_status(connector_id)
    if connector_status == ConnectorStatus.DELETE_PENDING:
        return InterruptType.DELETE_CONNECTOR

    # Check the specific sync-log row for CANCEL_PENDING (not the connector row)
    sync_log_status = get_sync_log_status(connector_id, sync_seq)
    if sync_log_status == SyncLogStatus.CANCEL_PENDING:
        return InterruptType.SYNC_CANCEL

    return None


# ---------------------------------------------------------------------------
# Public entry-point
# ---------------------------------------------------------------------------

async def run_tick(connector_id: str) -> None:
    """
    Execute one full sync tick for *connector_id*.

    The caller is responsible for acquiring the sync lock (sync_status='syncing')
    before calling this coroutine.  open_new_sync_log() is called here to open
    the sync-log row.
    """
    config = get_active_connector(connector_id)
    if config is None:
        logger.error(f"Connector {connector_id!r} not found; tick aborted")
        return

    sync_seq: int = open_new_sync_log(connector_id)
    scanner = build_scanner(config)

    try:
        # Phase boundary: check for interrupt signals before any remote I/O starts.
        interrupt = _check_interrupt_call(connector_id, sync_seq)
        if interrupt:
            raise asyncio.CancelledError(
                f"Connector {connector_id!r} interrupted (type={interrupt.value})"
            )

        await asyncio.to_thread(scanner.connect)
        scanned_files: list[tuple[str, str]] = await asyncio.to_thread(scanner.scan)

        known_checksums: set[str] = set(list_connector_checksums(connector_id))
        all_checksums: set[str] = set(list_all_checksums())

        ingest_list, orphan_checksums = _classify(
            connector_id, scanned_files, known_checksums, all_checksums
        )

        update_sync_log(
            connector_id,
            sync_seq,
            total_files=len(scanned_files),
            new_files=len(ingest_list),
            removed_files=len(orphan_checksums),
        )

        await _process_new_files(sync_seq, connector_id, scanner, ingest_list)

        await _delete_orphans(connector_id, orphan_checksums)

        _complete_tick(sync_seq, connector_id)

    except asyncio.CancelledError as ce:
        logger.info(f"Tick cancelled for connector {connector_id!r}: {ce}")
        interrupt = _check_interrupt_call(connector_id, sync_seq)
        await _handle_interrupt(sync_seq, connector_id, interrupt)
        raise

    except Exception as exc:
        logger.error(
            f"Tick failed for connector {connector_id!r}: {exc}", exc_info=True
        )
        _fail_tick(sync_seq, connector_id, exc)

    finally:
        await asyncio.to_thread(scanner.close)


# ---------------------------------------------------------------------------
# Classification (pure — no I/O beyond inline DB writes for cross-connector dups)
# ---------------------------------------------------------------------------

def _classify(
    connector_id: str,
    scanned_files: list[tuple[str, str]],
    known_checksums: set[str],
    all_checksums: set[str],
) -> tuple[list[tuple[str, str]], set[str]]:
    """
    Partition *scanned_files* into files to ingest and orphan checksums to remove.

    Cross-connector duplicates (checksum already owned by another connector) are
    registered inline via add_connector_checksum_entry — no deferred list.

    Returns
    -------
    ingest_list:
        (remote_path, checksum) pairs that are brand-new to all connectors.
    orphan_checksums:
        Checksums previously owned by *connector_id* that no longer appear on
        the remote source.
    """
    scanned_checksums: set[str] = set()
    seen_this_tick: set[str] = set()
    ingest_list: list[tuple[str, str]] = []

    for remote_path, checksum in scanned_files:
        scanned_checksums.add(checksum)
        if checksum in known_checksums:
            pass  # already owned by this connector → skip
        elif checksum in all_checksums:
            if checksum not in seen_this_tick:
                seen_this_tick.add(checksum)
                existing_doc_id = lookup_connector_content_by_checksum(checksum)
                if existing_doc_id:
                    add_connector_checksum_entry(connector_id, checksum, existing_doc_id)
        else:
            if checksum not in seen_this_tick:
                seen_this_tick.add(checksum)
                ingest_list.append((remote_path, checksum))

    orphan_checksums = known_checksums - scanned_checksums
    return ingest_list, orphan_checksums


# ---------------------------------------------------------------------------
# Phase 4a — download + ingest new files
# ---------------------------------------------------------------------------

_BATCH_SIZE = 10
_JOB_POLL_INTERVAL = 10  # seconds between job-status polls while waiting for a batch


async def _wait_for_job(job_id: str, connector_id: str, sync_seq: int) -> None:
    """Poll *job_id* until it reaches a terminal state.

    Sleeps *_JOB_POLL_INTERVAL* seconds between polls.  On every wake-up it
    also calls ``_check_interrupt_call`` so that a deletion/cancel request is
    honoured promptly even while the batch is running.

    Raises ``asyncio.CancelledError`` if the connector is marked for deletion
    or a stop-sync request is issued during the wait.
    """
    _TERMINAL = {JobStatus.COMPLETED.value, JobStatus.FAILED.value}
    while True:
        await asyncio.sleep(_JOB_POLL_INTERVAL)
        interrupt = _check_interrupt_call(connector_id, sync_seq)
        if interrupt:
            raise asyncio.CancelledError(
                f"Connector {connector_id!r} interrupted (type={interrupt.value})"
            )
        job_data = get_job(job_id)
        status = (job_data or {}).get("status", "")
        logger.debug(f"Polling job {job_id!r} for connector {connector_id!r}: status={status!r}")
        if status in _TERMINAL:
            break


async def _process_new_files(
    sync_seq: int,
    connector_id: str,
    scanner,
    ingest_list: list[tuple[str, str]],
) -> None:
    """Download and ingest *ingest_list* in batches of up to 10 files.

    Each batch of up to 10 files is downloaded into a single staging directory,
    registered as a single job, and passed to ``ingest()`` together.

    Staging directories are removed after each batch regardless of success.

    Raises ``RuntimeError`` after processing all batches if any batch failed,
    so the caller can mark the connector as OUT_OF_SYNC.
    """
    staging_base = settings.digitize.staging_dir / "connectors"
    batch_failed = False

    for batch_number, batch_offset in enumerate(range(0, len(ingest_list), _BATCH_SIZE), start=1):
        batch = ingest_list[batch_offset : batch_offset + _BATCH_SIZE]

        # Cancellation checkpoint: bail before each batch starts.
        interrupt = _check_interrupt_call(connector_id, sync_seq)
        if interrupt:
            raise asyncio.CancelledError(
                f"Connector {connector_id!r} interrupted (type={interrupt.value})"
            )

        job_id = generate_uuid()
        batch_dir_name = f"{connector_id}-{sync_seq}-{batch_number}"
        batch_dir = staging_base / batch_dir_name
        batch_dir.mkdir(parents=True, exist_ok=True)

        # checksum → filename for post-ingest checksum registration
        checksum_to_filename: dict[str, str] = {}

        try:
            for remote_path, checksum in batch:
                filename = Path(remote_path).name
                local_checksum = await asyncio.to_thread(
                    scanner.download_to, remote_path, batch_dir / filename
                )
                if not scanner.verify_integrity(local_checksum, checksum):
                    logger.warning(
                        f"Integrity check failed for {remote_path!r} in connector "
                        f"{connector_id!r}; skipping file"
                    )
                    continue
                checksum_to_filename[checksum] = filename

            # Cancellation checkpoint: bail after each batch of downloads.
            interrupt = _check_interrupt_call(connector_id, sync_seq)
            if interrupt:
                raise asyncio.CancelledError(
                    f"Connector {connector_id!r} interrupted (type={interrupt.value})"
                )

            filenames = list(checksum_to_filename.values())
            job_name = f"{connector_id} - {sync_seq} - {batch_number}"
            doc_id_dict = initialize_job_state(
                job_id=job_id,
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=filenames,
                job_name=job_name,
            )

            for checksum, filename in checksum_to_filename.items():
                add_connector_checksum_entry(connector_id, checksum, doc_id_dict[filename])

            await asyncio.to_thread(ingest, batch_dir, job_id, doc_id_dict)

            await _wait_for_job(job_id, connector_id, sync_seq)

        except asyncio.CancelledError:
            raise

        except Exception as exc:
            logger.warning(
                f"Failed to ingest batch {batch_number} for connector {connector_id!r}: {exc}",
                exc_info=True,
            )
            batch_failed = True

        finally:
            cleanup_staging_directory(batch_dir_name, staging_base, ignore_errors=True)

    if batch_failed:
        raise RuntimeError(
            f"One or more batches failed to ingest for connector {connector_id!r}; "
            "connector marked as out of sync"
        )


# ---------------------------------------------------------------------------
# Phase 4b — orphan removal
# ---------------------------------------------------------------------------

async def _delete_orphans(connector_id: str, orphan_checksums: set[str]) -> None:
    """Remove orphaned checksum rows and delete documents that lose their last owner."""
    from digitize.api.v1.connectors import _best_effort_delete_document

    for checksum in orphan_checksums:
        try:
            remaining, doc_id = remove_connector_checksum_entry(connector_id, checksum)
            if remaining == 0 and doc_id:
                await asyncio.to_thread(_best_effort_delete_document, doc_id)
        except Exception as exc:
            logger.error(
                f"Error removing orphan checksum {checksum!r} "
                f"for connector {connector_id!r}: {exc}",
                exc_info=True,
            )
            raise


# ---------------------------------------------------------------------------
# Interrupt handler — differentiated handling for cancel vs delete
# ---------------------------------------------------------------------------

async def _handle_interrupt(
    sync_seq: int,
    connector_id: str,
    interrupt_type: Optional[InterruptType],
) -> None:
    """
    Handle cancellation based on interrupt type.

    SYNC_CANCEL: Stop the current sync, set connector to OUT_OF_SYNC, cleanup staging.
    DELETE_CONNECTOR: Stop sync + full teardown (remove checksums, docs, connector row).
    """
    if interrupt_type is None:
        # Default cancel behavior — treat as sync cancel
        _cancel_tick(sync_seq, connector_id)
        return

    if interrupt_type == InterruptType.SYNC_CANCEL:
        logger.info(f"Handling sync cancel for connector {connector_id!r}")
        _cancel_tick(sync_seq, connector_id)
        # Note: close_sync_log() automatically sets connector to OUT_OF_SYNC when
        # sync log status is CANCELLED. Cleanup staging directory for this sync.
        from digitize.api.v1.connectors import _sweep_staging_dir
        from digitize.settings import settings
        _sweep_staging_dir(
            connector_id,
            settings.digitize.staging_dir / "connectors",
            sync_seq=sync_seq,
        )

    elif interrupt_type == InterruptType.DELETE_CONNECTOR:
        logger.info(f"Handling delete connector for {connector_id!r}")
        _cancel_tick(sync_seq, connector_id)
        # Run full teardown: remove checksums, delete orphaned docs, delete connector row
        from digitize.api.v1.connectors import _run_teardown
        await _run_teardown(connector_id)


# ---------------------------------------------------------------------------
# Phase 5 helpers
# ---------------------------------------------------------------------------

def _complete_tick(sync_seq: int, connector_id: str) -> None:
    close_sync_log(
        connector_id=connector_id,
        seq=sync_seq,
        status=SyncLogStatus.COMPLETED,
    )
    logger.info(f"Tick completed for {connector_id!r}")


def _cancel_tick(sync_seq: int, connector_id: str) -> None:
    """Close the sync log with status='cancelled' after a CancelledError."""
    try:
        close_sync_log(
            connector_id=connector_id,
            seq=sync_seq,
            status=SyncLogStatus.CANCELLED,
        )
    except Exception as close_exc:
        logger.error(
            f"Failed to close sync log for {connector_id!r} after cancellation: {close_exc}",
            exc_info=True,
        )


def _fail_tick(sync_seq: int, connector_id: str, exc: Exception) -> None:
    try:
        close_sync_log(
            connector_id=connector_id,
            seq=sync_seq,
            status=SyncLogStatus.FAILED,
            error=str(exc),
        )
    except Exception as close_exc:
        logger.error(
            f"Failed to close sync log for {connector_id!r} after tick failure: {close_exc}",
            exc_info=True,
        )
