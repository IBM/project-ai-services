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
from digitize.models import OutputFormat, OperationType
from digitize.pipeline.ingest import ingest
from digitize.settings import settings
from digitize.utils.db import (
    add_connector_checksum_entry,
    close_sync_log,
    get_active_connector,
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

async def _process_new_files(
    sync_seq: int,
    connector_id: str,
    scanner,
    ingest_list: list[tuple[str, str]],
) -> tuple[int, int]:
    """Download and ingest each file in *ingest_list*.

    Returns (new_count, failed_count).  Staging directories are removed after
    each file regardless of success.
    """
    staging_base = settings.digitize.staging_dir / "connectors"
    new_count = 0
    failed_count = 0

    for batch_number, (remote_path, checksum) in enumerate(ingest_list):
        job_id = generate_uuid()
        batch_dir_name = f"{connector_id}-{job_id}-{batch_number}"
        batch_dir = staging_base / batch_dir_name
        batch_dir.mkdir(parents=True, exist_ok=True)

        try:
            local_checksum = await asyncio.to_thread(
                scanner.download_to, remote_path, batch_dir / Path(remote_path).name
            )
            scanner.verify_integrity(local_checksum, checksum)

            filename = Path(remote_path).name
            job_name = f"{connector_id} - {sync_seq} - {batch_number}"
            doc_id_dict = initialize_job_state(
                job_id=job_id,
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=[filename],
                job_name=job_name,
            )
            doc_id = doc_id_dict[filename]
            add_connector_checksum_entry(connector_id, checksum, doc_id)

            await asyncio.to_thread(ingest, batch_dir, job_id, doc_id_dict)

            new_count += 1
            update_sync_log(sync_seq, new_files=new_count)

        except Exception as exc:
            logger.warning(
                f"Failed to ingest {remote_path!r} for connector {connector_id!r}: {exc}",
                exc_info=True,
            )
            failed_count += 1
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
