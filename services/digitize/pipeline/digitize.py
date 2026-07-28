"""
Digitization pipeline entry-point.

Drives the single-document digitization job lifecycle:
poll conversion_tasks → mark status.

The conversion itself is now owned entirely by the dispatcher
(workers/conversion_dispatcher.py).  This function just waits for the
dispatcher to mark the task as completed or failed, then surfaces the
result through the standard job/doc status updates.
"""
import time
from pathlib import Path

from common.misc_utils import get_logger
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
    Poll the conversion_tasks row inserted by ``POST /v1/jobs`` until the
    dispatcher marks it terminal (completed or failed).

    The dispatcher handles conversion, semaphore management, and writes
    the result_path when it succeeds.  This function's only job is to
    wait and propagate the final status.

    Args:
        directory_path:    Staging directory (kept for call-site compatibility).
        job_id:            Job identifier.
        doc_id_dict:       Mapping from filename to document ID.
        output_format:     Output format (unused — task row already carries it).
        file_checksum_dict: Pre-computed checksums keyed by filename.
                            Passed through for API symmetry; currently unused
                            by this function since the dispatcher writes all
                            status directly.
    """
    status_mgr = get_status_manager(job_id)

    task = db_manager.get_conversion_task_by_job_id(job_id)
    if task is None:
        error = f"No conversion task found for job {job_id}"
        logger.error(error)
        doc_id = next(iter(doc_id_dict.values()), "")
        if doc_id:
            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=error)
        status_mgr.update_job_progress("", DocStatus.FAILED, JobStatus.FAILED, error=error)
        return

    # Poll until the dispatcher marks the task terminal.
    while task.status not in ("completed", "failed"):
        time.sleep(settings.digitize.conversion_poll_interval)
        task = db_manager.get_conversion_task_by_job_id(job_id)
        if task is None:
            logger.warning(f"Task for job {job_id} disappeared during polling")
            break

    if task is None or task.status == "failed":
        error = (task.error or "Conversion failed") if task else "Task row missing"
        logger.error(f"Digitization task for job {job_id} failed: {error}")
        # The dispatcher already updated job/doc status on failure; nothing more to do.
        return

    # task.status == "completed"
    # The dispatcher already set job/doc to COMPLETED for digitization tasks.
    logger.info(f"Digitization task for job {job_id} completed → {task.result_path}")
