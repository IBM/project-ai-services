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

from common.misc_utils import get_logger, get_utc_timestamp
from digitize.db.manager import db_manager
from digitize.models import JobStatus, DocStatus
from digitize.settings import settings
from digitize.utils.db import get_status_manager

logger = get_logger("digitize")


def digitize(
    directory_path: Path,
    job_id: str,
    doc_id_dict: dict,
    output_format,
    file_checksum_dict: dict | None = None,  # filename -> checksum pre-computed at upload
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
        directory_path:     Staging directory (kept for call-site compatibility).
        job_id:             Job identifier.
        doc_id_dict:        Mapping from filename to document ID.
        output_format:      Output format (unused — task row already carries it).
        file_checksum_dict: Pre-computed checksums keyed by filename (unused here;
                            kept for API symmetry with the ingestion pipeline).
    """
    status_mgr = get_status_manager(job_id)
    doc_id = next(iter(doc_id_dict.values()), "")

    task = db_manager.get_conversion_task_by_job_id(job_id)
    if task is None:
        error = f"No conversion task found for job {job_id}"
        logger.error(error)
        if doc_id:
            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=error)
        status_mgr.update_job_progress("", DocStatus.FAILED, JobStatus.FAILED, error=error)
        return

    timeout_s = settings.digitize.conversion_timeout_s
    deadline = time.monotonic() + timeout_s

    # Poll until the dispatcher advances the task past queued state.
    while task.status not in ("running", "completed", "failed"):
        if time.monotonic() >= deadline:
            error = (
                f"Conversion task for job {job_id} did not start within "
                f"{timeout_s:.0f}s — dispatcher may be stalled"
            )
            logger.error(error)
            if doc_id:
                status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=error)
            status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.FAILED, error=error)
            return
        time.sleep(settings.digitize.conversion_poll_interval)
        task = db_manager.get_conversion_task_by_job_id(job_id)
        if task is None:
            logger.warning(f"Task for job {job_id} disappeared during polling")
            break

    # Mark job/doc IN_PROGRESS as soon as the dispatcher starts running the task.
    if task is not None and task.status == "running" and doc_id:
        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.IN_PROGRESS})
        status_mgr.update_job_progress(doc_id, DocStatus.IN_PROGRESS, JobStatus.IN_PROGRESS)

    # Continue polling until terminal.
    while task is not None and task.status not in ("completed", "failed"):
        if time.monotonic() >= deadline:
            error = (
                f"Conversion task for job {job_id} did not complete within "
                f"{timeout_s:.0f}s — dispatcher may be stalled"
            )
            logger.error(error)
            if doc_id:
                status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=error)
            status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.FAILED, error=error)
            return
        time.sleep(settings.digitize.conversion_poll_interval)
        task = db_manager.get_conversion_task_by_job_id(job_id)
        if task is None:
            logger.warning(f"Task for job {job_id} disappeared during polling")
            break

    if task is None or task.status == "failed":
        error = (task.error or "Conversion failed") if task else "Task row missing"
        logger.error(f"Digitization task for job {job_id} failed: {error}")
        if doc_id:
            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=error)
        status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.FAILED, error=error)
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
