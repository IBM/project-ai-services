"""
connectors/sync_tick.py — end-to-end sync-tick logic for one connector.

Phases
------
1.  [caller] try_acquire_sync_lock()              → atomically set connector.status=SYNCING
2.  [caller] init_sync_log_and_update_connector() → INSERT connector_sync_logs row (status='started'),
             returns sync_seq which is passed directly into run_tick()
3.  load known/all checksums + scanner.scan()
4.  _classify()              → ingest_list, orphan_checksums
                               cross-connector dups registered inline
4b. update_sync_log()        → single DB write of total/new/removed counts (all known post-classify)
5a. _process_new_files()     → download → create job/doc → add checksum row
5b. _delete_orphans()        → remove checksum rows; delete docs with no owners
6.  finalize_sync_log_and_update_connector()      → finalize tick row with terminal status + reset status

Caller contract
---------------
Any caller — the HTTP handler (POST /v1/connectors/{id}/syncs) or the
APScheduler (dispatch_sync in connectors.py) — must follow the same
three-step preamble before calling run_tick():

    acquired = db_ops.try_acquire_sync_lock(connector_id)
    if not acquired:
        return  # another tick is already running
    sync_seq = db_ops.init_sync_log_and_update_connector(connector_id)
    asyncio.create_task(run_tick(connector_id, sync_seq))

This keeps seq generation synchronous and in the caller's control, eliminating
any need for polling after task dispatch.
"""

from __future__ import annotations

import asyncio
from enum import Enum
from pathlib import Path
from typing import Optional

from pydantic import ValidationError

from common.misc_utils import cleanup_staging_directory, get_logger, validate_document_file
from digitize.connectors.scanners.scanner_factory import build_scanner
from digitize.settings import settings
from digitize.connectors.models import ConnectorError, ConnectorStatus, SyncLogStatus
from digitize.models import JobStatus, OutputFormat, OperationType
from digitize.utils.db import (
    add_connector_checksum_entry,
    finalize_sync_log_and_update_connector,
    get_connector_by_id,
    get_connector_sync_status,
    get_sync_log_status,
    get_job,
    increment_completed_files,
    list_all_checksums,
    list_connector_checksums,
    lookup_connector_content_by_checksum,
    update_connector_total_files_and_message,
    update_sync_log,
)
from digitize.db.models import JobSource
from digitize.utils.jobs import (
    enqueue_conversion_tasks,
    generate_uuid,
    get_job_document_stats,
    initialize_job_state,
)

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
    1. connectors.status for DELETE_PENDING (delete entire connector)
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

async def run_tick(connector_id: str, sync_seq: int) -> None:
    """
    Execute one full sync tick for *connector_id*.

    Parameters
    ----------
    connector_id:
        The connector to sync.
    sync_seq:
        The seq of the sync-log row that the caller already opened via
        ``init_sync_log_and_update_connector()``.  The caller is also
        responsible for acquiring the sync lock beforehand via
        ``try_acquire_sync_lock()``.
    """
    config = get_connector_by_id(connector_id)
    if config is None:
        logger.error(f"Connector {connector_id!r} not found; tick aborted")
        _fail_tick(sync_seq, connector_id, RuntimeError(f"Connector {connector_id!r} not found"))
        return

    scanner = None
    try:
        scanner = build_scanner(config)

        # Phase boundary: check for interrupt signals before any remote I/O starts.
        interrupt = _check_interrupt_call(connector_id, sync_seq)
        if interrupt:
            raise asyncio.CancelledError(
                f"Connector {connector_id!r} interrupted (type={interrupt.value})"
            )

        try:
            await asyncio.to_thread(scanner.connect)
        except ConnectionError as conn_exc:
            logger.error(
                f"Connector {connector_id!r} failed to connect — treating as credential error: {conn_exc}"
            )
            _fail_tick(sync_seq, connector_id, conn_exc, error_msg=ConnectorError.CREDENTIAL_ERROR_MSG.value)
            return

        scanned_files: list[tuple[str, str]] = await asyncio.to_thread(scanner.scan)

        known_checksums: set[str] = set(list_connector_checksums(connector_id))
        all_checksums: set[str] = set(list_all_checksums())

        ingest_list, orphan_checksums = _classify(
            connector_id, scanned_files, known_checksums, all_checksums
        )

        total_files = len(scanned_files)
        new_files = len(ingest_list)

        update_sync_log(
            connector_id,
            sync_seq,
            total_files=total_files,
            new_files=new_files,
            removed_files=len(orphan_checksums),
        )

        update_connector_total_files_and_message(connector_id, total_files, f"{new_files} new files found")

        invalid_files = await _process_new_files(sync_seq, connector_id, config.name, scanner, ingest_list)

        await _delete_orphans(connector_id, orphan_checksums)

        if invalid_files:
            files_str = ", ".join(invalid_files)
            validation_error_msg = f"Invalid file(s) detected from source - {files_str}"
            logger.error(validation_error_msg)
            _fail_tick(sync_seq, connector_id, RuntimeError(validation_error_msg), error_msg=validation_error_msg)
        else:
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
        if isinstance(exc, ValidationError):
            _fail_tick(sync_seq, connector_id, exc, error_msg=ConnectorError.CREDENTIAL_ERROR_MSG.value)
        else:
            _fail_tick(sync_seq, connector_id, exc)

    finally:
        if scanner is not None:
            try:
                await asyncio.to_thread(scanner.close)
            except Exception as close_exc:
                logger.warning(
                    f"scanner.close() failed for connector {connector_id!r}: {close_exc}"
                )


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


async def _wait_for_job(
    job_id: str,
    connector_id: str,
    sync_seq: int,
) -> None:
    """Poll *job_id* until it reaches a terminal state.

    Sleeps *_JOB_POLL_INTERVAL* seconds between polls.  On every wake-up it
    also calls ``_check_interrupt_call`` so that a deletion/cancel request is
    honoured promptly even while the batch is running.

    On each poll that returns job data, any documents that have newly moved into
    a completed state are counted and ``increment_completed_files`` is called
    immediately so the sync log reflects real-time progress rather than a single
    bulk update at the end.

    Raises ``asyncio.CancelledError`` if the connector is marked for deletion
    or a stop-sync request is issued during the wait.
    """
    _TERMINAL = {JobStatus.COMPLETED.value, JobStatus.COMPLETED_WITH_ERRORS.value, JobStatus.FAILED.value}
    prev_completed_count = 0
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

        # Count any docs that newly reached 'completed' since the last poll.
        job_stats = get_job_document_stats(job_id)
        completed_count = len(job_stats["completed_docs"])
        newly_completed = completed_count - prev_completed_count
        if newly_completed > 0:
            prev_completed_count = completed_count
            increment_completed_files(connector_id, sync_seq, count=newly_completed)

        if status in _TERMINAL:
            break


async def _process_new_files(
    sync_seq: int,
    connector_id: str,
    connector_name: str,
    scanner,
    ingest_list: list[tuple[str, str]],
) -> list[str]:
    """Download and ingest *ingest_list* in batches of up to 10 files.

    Each batch of up to 10 files is downloaded into a single staging directory,
    registered as a single job, and passed to ``ingest()`` together.

    Staging directories are removed after each batch regardless of success.

    Raises ``RuntimeError`` after processing all batches if any batch failed,
    so the caller can mark the connector as OUT_OF_SYNC.

    Returns
    -------
    list[str]
        Remote paths of files rejected by ``validate_document_file`` across all
        batches.  If non-empty the caller closes the sync log as FAILED with
        a message listing the invalid files.
    """
    staging_base = settings.digitize.staging_dir / "connectors"
    batch_failed = False
    invalid_files: list[str] = []

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

        # filename → checksum, built during download phase
        filename_to_checksum: dict[str, str] = {}

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
                try:
                    file_bytes = (batch_dir / filename).read_bytes()
                    validate_document_file(filename, file_bytes)
                except ValueError as val_exc:
                    logger.warning(
                        f"Invalid file {remote_path!r} in connector "
                        f"{connector_id!r}; skipping file: {val_exc}"
                    )
                    (batch_dir / filename).unlink(missing_ok=True)
                    invalid_files.append(remote_path)
                    continue
                filename_to_checksum[filename] = checksum

            # Cancellation checkpoint: bail after each batch of downloads.
            interrupt = _check_interrupt_call(connector_id, sync_seq)
            if interrupt:
                raise asyncio.CancelledError(
                    f"Connector {connector_id!r} interrupted (type={interrupt.value})"
                )

            if not filename_to_checksum:
                continue

            filenames = list(filename_to_checksum.keys())
            job_name = f"Connector-{connector_name}-{sync_seq}-{batch_number}"
            doc_id_dict = initialize_job_state(
                job_id=job_id,
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=filenames,
                job_name=job_name,
                source=JobSource.CONNECTOR,
            )

            # doc_id → checksum: built from doc_id_dict (filename→doc_id) + filename_to_checksum
            doc_id_to_checksum: dict[str, str] = {
                doc_id: filename_to_checksum[filename]
                for filename, doc_id in doc_id_dict.items()
                if filename in filename_to_checksum
            }

            # Enqueue all files into conversion_tasks as 'queued' (connector path —
            # no pending phase, no quota). The dispatcher's C-ING turn picks them up.
            await enqueue_conversion_tasks(
                job_id=job_id,
                op_key=OperationType.INGESTION,
                filenames=filenames,
                doc_id_dict=doc_id_dict,
                staging_dir=batch_dir,
                output_format=OutputFormat.JSON,
                quota=0,          # unused for connector tasks
                queued_for_op=0,  # unused for connector tasks
                connector_id=connector_id,
            )

            await _wait_for_job(job_id, connector_id, sync_seq)

            job_stats = get_job_document_stats(job_id)
            for doc in job_stats["completed_docs"] + job_stats["completed_with_errors_docs"]:
                checksum = doc_id_to_checksum.get(doc["id"])
                if checksum is not None:
                    add_connector_checksum_entry(connector_id, checksum, doc["id"])

            if job_stats["failed_count"] > 0:
                logger.warning(
                    f"Batch {batch_number} for connector {connector_id!r}: "
                    f"{job_stats['failed_count']} of {job_stats['total_docs']} "
                    f"document(s) failed ingestion"
                )
                batch_failed = True

        except asyncio.CancelledError:
            raise

        except Exception as exc:
            logger.error(
                f"Failed to ingest batch {batch_number} for connector {connector_id!r}: {exc}",
                exc_info=True,
            )
            batch_failed = True

        finally:
            cleanup_staging_directory(batch_dir_name, staging_base, ignore_errors=True)

    if batch_failed:
        raise RuntimeError(
            f"One or more documents failed to sync. See more details in digitize jobs "
            f"Connector-{connector_name}-{sync_seq}-*"
        )

    return invalid_files


# ---------------------------------------------------------------------------
# Phase 4b — orphan removal
# ---------------------------------------------------------------------------

async def _delete_orphans(connector_id: str, orphan_checksums: set[str]) -> None:
    """Remove orphaned checksum rows and delete documents that lose their last owner."""
    from digitize.api.v1.connectors import _remove_checksums

    checksum_removal_failures, doc_deletion_failures = await asyncio.to_thread(
        _remove_checksums, connector_id, orphan_checksums
    )
    if checksum_removal_failures:
        raise RuntimeError(
            f"checksum removal failed for {len(checksum_removal_failures)} checksum(s)"
        )
    if doc_deletion_failures:
        raise RuntimeError(
            f"document deletion failed for {len(doc_deletion_failures)} checksum(s)"
        )


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
        # Note: finalize_sync_log_and_update_connector() automatically sets connector to OUT_OF_SYNC when
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
    finalize_sync_log_and_update_connector(
        connector_id=connector_id,
        seq=sync_seq,
        status=SyncLogStatus.COMPLETED,
    )
    logger.info(f"Tick completed for {connector_id!r}")


def _cancel_tick(sync_seq: int, connector_id: str) -> None:
    """Close the sync log with status='cancelled' after a CancelledError."""
    try:
        finalize_sync_log_and_update_connector(
            connector_id=connector_id,
            seq=sync_seq,
            status=SyncLogStatus.CANCELLED,
        )
    except Exception as close_exc:
        logger.error(
            f"Failed to close sync log for {connector_id!r} after cancellation: {close_exc}",
            exc_info=True,
        )


def _fail_tick(sync_seq: int, connector_id: str, exc: Exception, error_msg: Optional[str] = None) -> None:
    try:
        finalize_sync_log_and_update_connector(
            connector_id=connector_id,
            seq=sync_seq,
            status=SyncLogStatus.FAILED,
            error=error_msg if error_msg is not None else str(exc),
        )
    except Exception as close_exc:
        logger.error(
            f"Failed to close sync log for {connector_id!r} after tick failure: {close_exc}",
            exc_info=True,
        )
