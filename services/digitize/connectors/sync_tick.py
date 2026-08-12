"""
connectors/sync_tick.py — end-to-end sync-tick logic for one connector.

Phases
------
1.  open_new_sync_log        → INSERT connector_sync_logs row (status='started')
2.  load known/all checksums + scanner.scan()
3.  _classify()              → ingest_list, orphan_checksums
                               cross-connector dups registered inline
4a. _process_new_files()     → download → create job/doc → add checksum row
4b. _delete_orphans()        → remove checksum rows; delete docs with no owners
5.  close_sync_log()         → finalize tick row + reset sync_status

Called by the APScheduler job (_run_tick_wrapped in scheduler.py, PR7) and
directly by POST /v1/connectors/{id}/sync after the caller has already
acquired the sync lock via try_acquire_sync_lock().
"""

from __future__ import annotations

import asyncio
from pathlib import Path

from common.misc_utils import cleanup_staging_directory, get_logger
from digitize.connectors.scanners.scanner_factory import build_scanner
from digitize.pipeline.ingest import ingest
from digitize.settings import settings
from digitize.models import JobStatus, OutputFormat, OperationType
from digitize.utils.db import (
    add_connector_checksum_entry,
    close_sync_log,
    get_active_connector,
    get_connector_sync_status,
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
# Cancellation helper
# ---------------------------------------------------------------------------

def _check_delete_pending(connector_id: str) -> None:
    """
    Raise asyncio.CancelledError if the connector is marked for deletion or
    a stop-sync request has been issued.

    Performs a live DB query — no in-memory state.  Called at the three
    phase boundaries inside run_tick / _process_new_files so that a DELETE
    or DELETE /sync request is honoured promptly without leaving the tick
    running to completion.
    """
    status = get_connector_sync_status(connector_id)
    if status in ("delete pending", "cancel pending"):
        raise asyncio.CancelledError(
            f"Connector {connector_id!r} sync cancelled (status={status!r})"
        )


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

    failed: int = 0
    new: int = 0
    removed: int = 0

    try:
        # Phase boundary: check for DELETE_PENDING before any remote I/O starts.
        _check_delete_pending(connector_id)

        await asyncio.to_thread(scanner.connect)
        scanned_files: list[tuple[str, str]] = await asyncio.to_thread(scanner.scan)

        known_checksums: set[str] = set(list_connector_checksums(connector_id))
        all_checksums: set[str] = set(list_all_checksums())

        ingest_list, orphan_checksums = _classify(
            connector_id, scanned_files, known_checksums, all_checksums
        )

        update_sync_log(sync_seq, total_files=len(scanned_files))

        new, failed = await _process_new_files(
            sync_seq, connector_id, scanner, ingest_list
        )

        removed = await _delete_orphans(connector_id, orphan_checksums)

        _complete_tick(
            sync_seq, connector_id,
            total_files=len(scanned_files),
            new_files=new,
            removed_files=removed,
            failed_files=failed,
        )

    except asyncio.CancelledError:
        logger.info(f"Tick cancelled for connector {connector_id!r} (delete_pending)")
        _cancel_tick(sync_seq, connector_id)
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


async def _wait_for_job(job_id: str, connector_id: str) -> None:
    """Poll *job_id* until it reaches a terminal state.

    Sleeps *_JOB_POLL_INTERVAL* seconds between polls.  On every wake-up it
    also calls ``_check_delete_pending`` so that a deletion/cancel request is
    honoured promptly even while the batch is running.

    Raises ``asyncio.CancelledError`` if the connector is marked for deletion
    or a stop-sync request is issued during the wait.
    """
    _TERMINAL = {JobStatus.COMPLETED.value, JobStatus.FAILED.value}
    while True:
        await asyncio.sleep(_JOB_POLL_INTERVAL)
        _check_delete_pending(connector_id)
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
) -> tuple[int, int]:
    """Download and ingest *ingest_list* in batches of up to 10 files.

    Each batch of up to 10 files is downloaded into a single staging directory,
    registered as a single job, and passed to ``ingest()`` together.

    Returns (new_count, failed_count).  Staging directories are removed after
    each batch regardless of success.
    """
    staging_base = settings.digitize.staging_dir / "connectors"
    new_count = 0
    failed_count = 0

    for batch_number in range(0, len(ingest_list), _BATCH_SIZE):
        batch = ingest_list[batch_number : batch_number + _BATCH_SIZE]

        # Cancellation checkpoint: bail before each batch starts.
        _check_delete_pending(connector_id)

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
                scanner.verify_integrity(local_checksum, checksum)
                checksum_to_filename[checksum] = filename

            # Cancellation checkpoint: bail after each batch of downloads.
            _check_delete_pending(connector_id)

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

            await _wait_for_job(job_id, connector_id)

            new_count += len(batch)
            update_sync_log(sync_seq, new_files=new_count)

        except Exception as exc:
            logger.warning(
                f"Failed to ingest batch {batch_number!r} for connector {connector_id!r}: {exc}",
                exc_info=True,
            )
            failed_count += len(batch)
            update_sync_log(sync_seq, failed_files=failed_count)

        finally:
            cleanup_staging_directory(batch_dir_name, staging_base, ignore_errors=True)

    return new_count, failed_count


# ---------------------------------------------------------------------------
# Phase 4b — orphan removal
# ---------------------------------------------------------------------------

async def _delete_orphans(connector_id: str, orphan_checksums: set[str]) -> int:
    """Remove orphaned checksum rows and delete documents that lose their last owner.

    Returns the number of ownership rows removed.
    """
    from digitize.api.v1.connectors import _best_effort_delete_document

    removed = 0
    for checksum in orphan_checksums:
        try:
            remaining, doc_id = remove_connector_checksum_entry(connector_id, checksum)
            removed += 1
            if remaining == 0 and doc_id:
                await asyncio.to_thread(_best_effort_delete_document, doc_id)
        except Exception as exc:
            logger.error(
                f"Error removing orphan checksum {checksum!r} "
                f"for connector {connector_id!r}: {exc}",
                exc_info=True,
            )
    return removed


# ---------------------------------------------------------------------------
# Phase 5 helpers
# ---------------------------------------------------------------------------

def _complete_tick(
    sync_seq: int,
    connector_id: str,
    total_files: int,
    new_files: int,
    removed_files: int,
    failed_files: int,
) -> None:
    close_sync_log(
        connector_id=connector_id,
        seq=sync_seq,
        status="completed",
        total_files=total_files,
        new_files=new_files,
        removed_files=removed_files,
        failed_files=failed_files,
    )
    logger.info(
        f"Tick completed for {connector_id!r} — "
        f"total={total_files} new={new_files} removed={removed_files} failed={failed_files}"
    )


def _cancel_tick(sync_seq: int, connector_id: str) -> None:
    """Close the sync log with status='cancelled' after a CancelledError."""
    try:
        close_sync_log(
            connector_id=connector_id,
            seq=sync_seq,
            status="cancelled",
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
            status="failed",
            error=str(exc),
        )
    except Exception as close_exc:
        logger.error(
            f"Failed to close sync log for {connector_id!r} after tick failure: {close_exc}",
            exc_info=True,
        )
