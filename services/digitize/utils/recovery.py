"""
Crash recovery utilities.

Implements a fast single-query recovery strategy: on startup a single
database query locates all zombie jobs (accepted / in_progress at the
time of the previous crash) and marks them as failed.

This mirrors the pattern introduced in digitize-api-sample where
recovery logic is isolated from the general utility bag.
"""

from common.misc_utils import get_logger, cleanup_staging_directory
from digitize.models import JobStatus, DocStatus
from digitize.utils.db import get_all_jobs, get_status_manager

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
    orphan_statuses = [JobStatus.ACCEPTED, JobStatus.IN_PROGRESS]

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

                        # Only documents that are still in-flight need updating.
                        in_flight = {
                            DocStatus.ACCEPTED.value,
                            DocStatus.IN_PROGRESS.value,
                            DocStatus.DIGITIZED.value,
                            DocStatus.PROCESSED.value,
                            DocStatus.CHUNKED.value,
                        }
                        if doc_status in in_flight and doc_id:
                            stale_doc_ids.append(doc_id)

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

                    if stale_doc_ids:
                        error_message += (
                            ". Stale documents may exist. "
                            "Please use DELETE /v1/documents/{id} to remove them "
                            f"and re-submit: {', '.join(stale_doc_ids)}"
                        )

                    # Final job-level update.
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
