"""
Digitization pipeline entry-point.

Drives the single-document digitization job lifecycle:
poll conversion_tasks → update job/doc status.

The dispatcher (workers/conversion_dispatcher.py) owns only the
conversion_tasks row.  This function is the sole owner of job and
document status transitions for digitization jobs.
"""
import time
from pathlib import Path

from common.misc_utils import cleanup_staging_directory, get_logger, get_utc_timestamp
from digitize.db.manager import db_manager
from digitize.db.models import ConversionTaskStatus
from digitize.models import JobStatus, DocStatus
from digitize.settings import settings
from digitize.utils.db import get_status_manager
from digitize.db.manager import db_manager
from digitize.exceptions import JobCancelledError

logger = get_logger("digitize")


def _poll_until(job_id, terminal_statuses, deadline, timeout_s, phase_label):
    """
    Poll the conversion_tasks row until its status is in ``terminal_statuses``
    or the deadline expires.

    Returns the task object (possibly in a terminal state) or ``None`` if the
    row disappeared.  Raises ``_DeadlineExceeded`` when the deadline is hit so
    the caller can apply a consistent failure path.

    Args:
        job_id:           Job identifier used to look up the task row.
        terminal_statuses: Set of status strings that end polling.
        deadline:         ``time.monotonic()`` value after which to give up.
        timeout_s:        Original timeout in seconds, used only in the error message.
        phase_label:      Human-readable label for the phase ("start" / "complete").
    """
    task = db_manager.get_conversion_task_by_job_id(job_id)
    while task is not None and task.status not in terminal_statuses:
        if time.monotonic() >= deadline:
            doc_name = Path(task.cached_file).name if task.cached_file else "unknown"
            raise _DeadlineExceeded(
                f"Conversion task for job {job_id} ({doc_name}) did not {phase_label} within "
                f"{timeout_s:.0f}s — dispatcher may be stalled"
            )
        time.sleep(settings.digitize.conversion_poll_interval)
        task = db_manager.get_conversion_task_by_job_id(job_id)
        if task is None:
            logger.warning(f"Task for job {job_id} disappeared during polling")
    return task


class _DeadlineExceeded(Exception):
    """Raised by ``_poll_until`` when the conversion deadline is exceeded."""


def digitize(
    job_id: str,
    doc_id_dict: dict,
):
    """
    Poll the conversion_tasks row until the dispatcher marks it terminal
    (completed or failed), then update job and document status accordingly.

    The dispatcher handles conversion, semaphore management, and writes
    result_path / error to the task row.  This function owns all
    JobStatus and DocStatus transitions.

    A single deadline of ``settings.digitize.conversion_timeout_s`` seconds is
    set at the start and shared across both poll loops.  If the deadline expires
    before the dispatcher advances or completes the task (e.g. dispatcher stall,
    semaphore leak, worker crash), the job is failed with a timeout message.

    Args:
        job_id:      Job identifier.
        doc_id_dict: Mapping from filename to document ID.
    """
    status_mgr = get_status_manager(job_id)
    doc_id = next(iter(doc_id_dict.values()), "")

    def _fail(error: str) -> None:
        logger.error(error)
        if doc_id:
            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=error)
        status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.FAILED, error=error)

    task = db_manager.get_conversion_task_by_job_id(job_id)
    if task is None:
        _fail(f"No conversion task found for job {job_id}")
        return

    timeout_s = settings.digitize.conversion_timeout_s
    deadline = time.monotonic() + timeout_s

    # Check cancellation before entering the ProcessPoolExecutor — the only safe abort point
    if job_id and db_manager.is_job_cancelled(job_id):
        raise JobCancelledError(f"Job {job_id} was cancelled before digitization started")

    try:
        # Phase 1: wait until the dispatcher picks up the task (queued → running).
        task = _poll_until(
            job_id,
            {ConversionTaskStatus.RUNNING, ConversionTaskStatus.COMPLETED, ConversionTaskStatus.FAILED},
            deadline, timeout_s, "start",
        )

        # Mark IN_PROGRESS as soon as the dispatcher starts running.
        if task is not None and task.status == ConversionTaskStatus.RUNNING and doc_id:
            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.IN_PROGRESS})
            status_mgr.update_job_progress(doc_id, DocStatus.IN_PROGRESS, JobStatus.IN_PROGRESS)

        # Phase 2: wait until the dispatcher reaches a terminal state.
        task = _poll_until(
            job_id,
            {ConversionTaskStatus.COMPLETED, ConversionTaskStatus.FAILED},
            deadline, timeout_s, "complete",
        )

    except _DeadlineExceeded as exc:
        _fail(str(exc))
        return

    finally:
        # Remove the staging directory once the full digitization pipeline has
        # finished (success, failure, timeout, or cancellation).  _run_digitize
        # also calls cleanup_staging_directory in its own finally — that call is
        # idempotent and will be a no-op once the directory is already gone.
        cleanup_staging_directory(job_id, settings.digitize.staging_dir)

    if task is None or task.status == ConversionTaskStatus.FAILED:
        _fail((task.error or "Conversion failed") if task else "Task row missing")
        return

    # task.status == "completed"
    logger.info(f"Digitization task for job {job_id} completed → {task.result_path}")
    if doc_id:
        status_mgr.update_doc_metadata(doc_id, {
            "status": DocStatus.COMPLETED,
            "pages": task.page_count or 0,
            "completed_at": get_utc_timestamp(),
        })
    status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED, JobStatus.COMPLETED)
