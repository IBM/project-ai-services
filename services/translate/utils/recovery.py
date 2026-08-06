"""
Crash recovery utilities.

On startup, scans the database for translate jobs that were left in
``accepted`` or ``in_progress`` state by a previous server crash and
marks them as ``failed``.  Staging directories for those jobs are
cleaned up as well.

Full staging-dir cleanup is wired in PR 2 once job_utils / StorageManager
are in place.  This module provides the minimal recovery loop that
app.py's lifespan requires from PR 1 onward.
"""

from datetime import datetime, timezone

from common.misc_utils import get_logger
from translate.db.manager import db_manager
from translate.models import JobStatus

logger = get_logger("recovery")


def recover_zombie_jobs() -> int:
    """
    Mark all incomplete translate jobs as failed on startup.

    Scans for ``accepted`` / ``in_progress`` rows and updates each to
    ``failed`` with a standard error message.

    Returns:
        Number of zombie jobs recovered.
    """
    import translate.settings as config

    orphan_count = 0

    try:
        zombies = db_manager.get_active_jobs()

        for job in zombies:
            job_id = job.job_id
            logger.warning(f"Found zombie translate job: {job_id} (status='{job.status}')")

            try:
                db_manager.update_job(
                    job_id=job_id,
                    status=JobStatus.FAILED,
                    completed_at=datetime.now(timezone.utc),
                    error="System restarted during processing",
                )

                # Best-effort staging cleanup (full impl added in PR 2)
                staging_path = config.settings.translate.staging_dir / job_id
                if staging_path.exists():
                    import shutil
                    try:
                        shutil.rmtree(staging_path)
                        logger.debug(f"Cleaned staging dir for zombie job: {job_id}")
                    except Exception as clean_err:
                        logger.warning(
                            f"Could not clean staging dir for {job_id}: {clean_err}"
                        )

                logger.info(f"✅ Marked zombie translate job {job_id} as failed")
                orphan_count += 1

            except Exception as exc:
                logger.error(
                    f"Error recovering zombie translate job {job_id}: {exc}",
                    exc_info=True,
                )

    except Exception as exc:
        logger.error(f"Error scanning for zombie translate jobs: {exc}", exc_info=True)

    if orphan_count:
        logger.debug(f"🔄 Recovered {orphan_count} zombie translate job(s) on startup")
    else:
        logger.debug("✅ No zombie translate jobs found on startup")

    return orphan_count

# Made with Bob
