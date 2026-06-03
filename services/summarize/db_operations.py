from typing import Optional

from common.misc_utils import get_utc_timestamp
from summarize.models import JobStatus
from summarize.db.manager import db_repo
from summarize.db.connection import engine
from common.misc_utils import set_log_level, get_logger
from summarize.settings import settings

set_log_level(settings.common.app.log_level)
logger = get_logger("db_operations")


def create_job_with_db(
        job_id: str,
        job_type: str,
        word_count: int,
        level: str,
        job_name: Optional[str] = None,
        doc_name: Optional[str] = None,
) -> None:
    """
    Create job in database.

    Args:
        job_id: Unique identifier for the job
        job_type
        word_count
        level
        submitted_at: ISO timestamp when job was submitted
        job_name: Optional human-readable name for the job
        doc_name
    """
    if engine is None:
        raise RuntimeError("Database not available. Cannot create job without database connection.")

    try:

        submitted_at = get_utc_timestamp()

        # Create job in database
        db_repo.create_job(
            job_id=job_id,
            status=JobStatus.ACCEPTED,
            job_name=job_name,
            doc_name=doc_name,
            word_count=word_count,
            job_level=level,
            job_type=job_type,
            submitted_at=submitted_at,
        )
        logger.info(f"Created job {job_id} in database")

    except Exception as e:
        logger.error(f"Failed to create job {job_id} in database: {e}", exc_info=True)
        raise
