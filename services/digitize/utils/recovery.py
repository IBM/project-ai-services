"""
Crash recovery utilities.

Implements a fast single-query recovery strategy: on startup a single
database query locates all zombie jobs (accepted / in_progress at the
time of the previous crash) and marks them as failed.

Also sweeps the ``conversion_tasks`` table to recover tasks that were
left in ``running`` state when the service crashed, and verifies that
``queued``/``pending`` tasks still have their cached input files.

This mirrors the pattern introduced in digitize-api-sample where
recovery logic is isolated from the general utility bag.
"""

import shutil
from pathlib import Path

from common.misc_utils import get_logger, cleanup_staging_directory
from digitize.db.models import ConversionTaskStatus
from digitize.models import JobStatus, DocStatus
from digitize.utils.db import (
    close_open_sync_log,
    get_all_jobs,
    get_status_manager,
    reset_syncing_connectors,
)

logger = get_logger("recovery")


def recover_zombie_jobs() -> int:
    """
    Mark all incomplete jobs as failed on startup.

    Scans the database for jobs with ``accepted`` or ``in_progress`` status
    and marks them (and their documents) as ``failed``.  Intermediate files
    are cleaned up for each affected document.

    Returns:
        Number of zombie jobs that were recovered.

    Example::

        zombie_count = recover_zombie_jobs()
        logger.info(f"Recovered {zombie_count} zombie jobs")
    """
    from digitize.processing.orchestrator import clean_intermediate_files
    import digitize.settings as config

    orphan_count = 0
    orphan_statuses = [JobStatus.ACCEPTED, JobStatus.IN_PROGRESS, JobStatus.CANCEL_PENDING]

    try:
        for status in orphan_statuses:
            jobs_data, _ = get_all_jobs(status=status, limit=10_000, offset=0)

            for job_data in jobs_data:
                job_id = job_data.get("job_id")
                if not job_id:
                    logger.warning("Skipping zombie job entry with missing job_id")
                    continue

                try:
                    current_status = job_data.get("status")
                    is_cancelling = current_status == JobStatus.CANCEL_PENDING.value
                    logger.warning(
                        f"Found zombie job: {job_id} with status '{current_status}'"
                    )

                    status_mgr = get_status_manager(job_id)
                    error_message = "System restarted during processing"
                    stale_doc_ids: list[str] = []

                    for doc in job_data.get("documents", []):
                        doc_id = doc.get("id")
                        doc_status = doc.get("status")

                        if doc_id:
                            # Best-effort: clean any intermediate files from the
                            # previous run regardless of the document's status.
                            try:
                                clean_intermediate_files(
                                    doc_id,
                                    config.settings.digitize.digitized_docs_dir,
                                )
                            except Exception as clean_err:
                                logger.warning(
                                    f"Could not clean intermediate files for "
                                    f"{doc_id}: {clean_err}"
                                )

                        # Documents still in-flight need resolving to a terminal state.
                        in_flight = {
                            DocStatus.ACCEPTED.value,
                            DocStatus.IN_PROGRESS.value,
                            DocStatus.DIGITIZED.value,
                            DocStatus.PROCESSED.value,
                            DocStatus.CHUNKED.value,
                        }
                        if doc_status in in_flight and doc_id:
                            stale_doc_ids.append(doc_id)

                            if is_cancelling:
                                # Job was being cancelled when the service crashed —
                                # complete the cancellation rather than marking failed.
                                status_mgr.update_doc_metadata(
                                    doc_id,
                                    {"status": DocStatus.CANCELLED},
                                )
                                status_mgr.update_job_progress(
                                    doc_id=doc_id,
                                    doc_status=DocStatus.CANCELLED,
                                    job_status=JobStatus.IN_PROGRESS,
                                    error="",
                                )
                            else:
                                status_mgr.update_doc_metadata(
                                    doc_id,
                                    {"status": DocStatus.FAILED},
                                    error=(
                                        f"System restarted during processing. "
                                        f"Use DELETE /v1/documents/{doc_id} to remove "
                                        "the stale document and re-submit."
                                    ),
                                )
                                # Keep job in IN_PROGRESS while we update each doc so
                                # that the stats recalculation inside update_job_progress
                                # sees the running totals correctly.
                                status_mgr.update_job_progress(
                                    doc_id=doc_id,
                                    doc_status=DocStatus.FAILED,
                                    job_status=JobStatus.IN_PROGRESS,
                                    error="",
                                )

                    if stale_doc_ids and not is_cancelling:
                        error_message += (
                            ". Stale documents may exist. "
                            "Please use DELETE /v1/documents/{id} to remove them "
                            f"and re-submit: {', '.join(stale_doc_ids)}"
                        )

                    # Final job-level update — honour the original intent.
                    if is_cancelling:
                        status_mgr.update_job_progress(
                            doc_id="",
                            doc_status=DocStatus.CANCELLED,
                            job_status=JobStatus.CANCELLED,
                        )
                        logger.info(f"✅ Marked zombie job {job_id} as cancelled")
                    else:
                        status_mgr.update_job_progress(
                            doc_id="",
                            doc_status=DocStatus.FAILED,
                            job_status=JobStatus.FAILED,
                            error=error_message,
                        )
                        logger.info(f"✅ Marked zombie job {job_id} as failed")
                    orphan_count += 1

                    # Clean up the staging directory that belonged to this job.
                    cleanup_staging_directory(
                        job_id, config.settings.digitize.staging_dir
                    )

                except Exception as exc:
                    logger.error(
                        f"Error recovering zombie job {job_id}: {exc}", exc_info=True
                    )

    except Exception as exc:
        logger.error(f"Error scanning for zombie jobs: {exc}", exc_info=True)

    if orphan_count:
        logger.debug(f"🔄 Recovered {orphan_count} zombie job(s) on startup")
    else:
        logger.debug("✅ No zombie jobs found on startup")

    return orphan_count


def recover_connector_sync_state() -> int:
    """
    Recover connectors that were mid-tick when the service crashed.

    On startup this function:
    1. Bulk-updates every connector whose ``status = 'syncing'`` to
       ``'out of sync'`` — releasing the sync lock so future ticks can run.
    2. For each affected connector, closes the still-open ``connector_sync_logs``
       row (status = 'started' or 'cancel pending') by setting it to
       ``'failed'`` with an explanatory error message.

    Returns:
        Number of connectors that were recovered.
    """
    _CRASH_ERROR = "Service restarted during sync tick"

    affected_ids = reset_syncing_connectors(error=_CRASH_ERROR)

    for connector_id in affected_ids:
        try:
            closed = close_open_sync_log(connector_id, _CRASH_ERROR)
            if closed:
                logger.info(
                    f"Closed stale sync log for connector {connector_id!r} after crash recovery"
                )
            else:
                logger.warning(
                    f"No open sync-log row found for connector {connector_id!r} during crash recovery"
                )
        except Exception as exc:
            logger.error(
                f"Error closing sync log for connector {connector_id!r}: {exc}",
                exc_info=True,
            )

    if affected_ids:
        logger.info(
            f"Connector crash recovery: reset {len(affected_ids)} connector(s): {affected_ids}"
        )
    else:
        logger.debug("No stuck connector syncs found on startup")

    return len(affected_ids)


def recover_conversion_tasks() -> int:
    """
    On startup, sweep the ``conversion_tasks`` table.

    All three active statuses are failed unconditionally:

    - ``running``  → ``failed``  (process died mid-conversion; chunk state unknown)
    - ``queued``   → ``failed``  (pipeline task that polled this row is gone)
    - ``pending``  → ``failed``  (same — no pipeline task will ever promote or consume it)

    The pipeline tasks (``_run_ingest`` / ``_run_digitize``) are fire-and-forget
    asyncio tasks created by the previous process.  They are not restarted on
    startup, so any task they were responsible for polling will never reach the
    job-level status update even if the dispatcher re-runs it.

    Returns:
        Number of tasks that were recovered (status changed).
    """
    from digitize.db.manager import db_manager

    recovered = 0

    try:
        tasks = db_manager.get_conversion_tasks(
                status=[
                    ConversionTaskStatus.RUNNING,
                    ConversionTaskStatus.QUEUED,
                    ConversionTaskStatus.PENDING,
                ]
            )
        for task in tasks:
            if task.status == ConversionTaskStatus.RUNNING:
                # Best-effort: clean chunk directories left over from the crashed run
                try:
                    chunk_dir = Path(task.cached_file).parent / "chunks"
                    if chunk_dir.exists():
                        shutil.rmtree(chunk_dir)
                except Exception as clean_err:
                    logger.warning(
                        f"Could not clean chunk dir for task {task.task_id}: {clean_err}"
                    )
                db_manager.update_task_status(
                    task.task_id, ConversionTaskStatus.FAILED,
                    error="Service restarted during conversion",
                )
                logger.warning(f"Recovery: task {task.task_id} running→failed")
                recovered += 1
            else:
                # queued / pending — the pipeline task that would have polled this
                # row is gone and will not be restarted.  Fail unconditionally so
                # the job surfaces a clean terminal state rather than hanging forever.
                db_manager.update_task_status(
                    task.task_id, ConversionTaskStatus.FAILED,
                    error="Service restarted before conversion could complete",
                )
                logger.warning(
                    f"Recovery: task {task.task_id} {task.status}→failed"
                )
                recovered += 1

    except Exception as exc:
        logger.error(f"Error during conversion task recovery: {exc}", exc_info=True)

    if recovered:
        logger.info(f"🔄 Recovered {recovered} conversion task(s) on startup")
    else:
        logger.debug("✅ No stale conversion tasks found on startup")

    return recovered
