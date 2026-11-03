"""Connector teardown helpers — utils/connectors.py.

Pure utility functions used by both the connectors API router and
sync_tick to clean up a connector's documents and staging directories.
Keeping them here avoids circular imports between connectors.py and
sync_tick.py and makes them independently testable.
"""

import asyncio
from typing import Optional

from common.misc_utils import cleanup_staging_directory, get_logger
from digitize.utils.jobs import (
    TEARDOWN_JOB_WAIT_TIMEOUT_S,
    TEARDOWN_JOB_POLL_INTERVAL_S,
)
import digitize.utils.db as db_ops

logger = get_logger("connectors_utils")


async def wait_for_connector_jobs(connector_id: str) -> Optional[str]:
    """
    Wait until every non-terminal job owned by *connector_id* has reached a
    terminal state.

    Cancellation is NOT requested here — the sync tick requests it when it
    catches the interrupt (see ``_wait_for_job`` in sync_tick).

    Returns ``None`` once no active job remains, or an error message if the
    jobs could not be verified as terminal within the timeout (stuck/crashed
    cancellation) so the caller can refuse to finish the teardown.
    """
    from digitize.db.manager import db_manager

    job_ids = db_manager.get_active_job_ids_for_connector(connector_id)
    if job_ids is None:
        return "could not determine active jobs"

    loop = asyncio.get_running_loop()
    deadline = loop.time() + TEARDOWN_JOB_WAIT_TIMEOUT_S
    while True:
        job_ids = db_manager.get_active_job_ids_for_connector(connector_id)
        if job_ids is not None and not job_ids:
            return None
        if loop.time() >= deadline:
            return (
                "job cancellation did not complete in "
                f"{TEARDOWN_JOB_WAIT_TIMEOUT_S}s"
                + (f" for job(s): {', '.join(job_ids)}" if job_ids else "")
            )
        await asyncio.sleep(TEARDOWN_JOB_POLL_INTERVAL_S)


def cleanup_connector_documents(
    connector_id: str,
    checksums: list[str] | set[str],
) -> tuple[list[str], list[str], list[str]]:
    """
    Clean up documents ingested by *connector_id* for the given *checksums*,
    plus any unregistered documents created for the connector.

    For each checksum:
      1. Removes the `(checksum, connector_id)` ownership mapping in the database.
      2. Checks the remaining owner count for that checksum across other connectors.
      3. If no other connector owns the checksum (`remaining == 0`), the
         underlying ingested document is deleted via `best_effort_delete_document`.
         If other connectors still reference the checksum (`remaining > 0`),
         the document is preserved.

    Afterwards, documents created for this connector that no connector owns
    (see `get_unregistered_connector_docs`, e.g. mid-flight or cancelled docs
    that never reached ``add_connector_checksum_entry``) are also deleted.

    Iterates all *checksums*, accumulating failures rather than stopping on the
    first error so that as many documents/checksums as possible are cleaned up in one pass.

    Returns
    -------
    checksum_removal_failures:
        Checksums for which ``remove_connector_checksum_entry`` raised.
    doc_deletion_failures:
        Checksums whose associated document could not be deleted (best-effort).
    unregistered_doc_failures:
        doc_ids of unregistered documents that could not be deleted.
    """
    checksum_removal_failures: list[str] = []
    doc_deletion_failures: list[str] = []
    unregistered_doc_failures: list[str] = []
    for checksum in checksums:
        try:
            remaining, doc_id = db_ops.remove_connector_checksum_entry(connector_id, checksum)
            if remaining == 0 and doc_id:
                if not best_effort_delete_document(doc_id):
                    doc_deletion_failures.append(checksum)
        except Exception as exc:
            checksum_removal_failures.append(checksum)
            logger.error(
                f"Error removing checksum {checksum!r} for connector "
                f"{connector_id!r}: {exc}",
                exc_info=True,
            )
    unregistered_doc_ids = get_unregistered_connector_docs(connector_id)
    if unregistered_doc_ids:
        logger.info(
            f"Connector {connector_id!r}: cleaning up {len(unregistered_doc_ids)} "
            f"unregistered document(s)"
        )
    for doc_id in unregistered_doc_ids:
        if not best_effort_delete_document(doc_id):
            unregistered_doc_failures.append(doc_id)

    return checksum_removal_failures, doc_deletion_failures, unregistered_doc_failures


def get_unregistered_connector_docs(connector_id: str) -> list[str]:
    """
    Return doc_ids created for *connector_id* that were never registered in
    ``connector_document_checksum``.

    These are documents from in-flight batches that were cancelled before
    ``add_connector_checksum_entry`` ran. Delegates to the DB manager query
    so the JOIN logic stays in one place.
    """
    from digitize.db.manager import db_manager
    return db_manager.get_unregistered_connector_doc_ids(connector_id)


def best_effort_delete_document(doc_id: str) -> bool:
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


def sweep_staging_dir(
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
