"""
Database repository layer for TranslateJob operations.

Provides CRUD operations with proper error handling and transaction management.
"""
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional, Tuple

from sqlalchemy import func, or_, select, update
from sqlalchemy.exc import IntegrityError, SQLAlchemyError

from common.misc_utils import get_logger
from translate.db.connection import get_db_session
from translate.db.models import TranslateJob
from translate.models import JobStatus

logger = get_logger("db_manager")


class TranslateDatabaseManager:
    """Repository for translate_jobs CRUD operations."""

    @staticmethod
    def create_job(
        job_id: str,
        source_language: str,
        target_language: str,
        input_type: str,
        status: JobStatus = JobStatus.ACCEPTED,
        job_name: Optional[str] = None,
        document_name: Optional[str] = None,
        submitted_at: Optional[datetime] = None,
    ) -> Optional[TranslateJob]:
        """
        Insert a new job row with status 'accepted'.

        Args:
            job_id: UUID string for the job.
            source_language: Normalised lowercase source language (e.g. 'german' or 'auto').
            target_language: Normalised lowercase target language (e.g. 'english').
            input_type: One of 'text', 'txt', 'md'.
            status: Initial status (defaults to ACCEPTED).
            job_name: Optional human-readable label.
            document_name: Original filename (None for sync text jobs).
            submitted_at: Submission timestamp; defaults to now(UTC).

        Returns:
            The created TranslateJob instance, or None on failure.
        """
        try:
            with get_db_session() as session:
                job = TranslateJob(
                    job_id=job_id,
                    job_name=job_name,
                    source_language=source_language,
                    target_language=target_language,
                    input_type=input_type,
                    document_name=document_name,
                    status=status.value,
                    submitted_at=submitted_at or datetime.now(timezone.utc),
                )
                session.add(job)
                session.flush()
                logger.info(f"Created translate job in database: {job_id}")
                return job
        except IntegrityError as e:
            logger.error(f"Translate job {job_id} already exists: {e}")
            return None
        except SQLAlchemyError as e:
            logger.error(f"Database error creating translate job {job_id}: {e}", exc_info=True)
            return None
        except Exception as e:
            logger.error(f"Unexpected error creating translate job {job_id}: {e}", exc_info=True)
            return None

    @staticmethod
    def get_job_by_id(job_id: str) -> Optional[TranslateJob]:
        """
        Retrieve a job by its ID.

        Returns the detached ORM instance, or None if not found.
        """
        try:
            with get_db_session() as session:
                stmt = select(TranslateJob).where(TranslateJob.job_id == job_id)
                job = session.scalar(stmt)
                if job:
                    # Eagerly load all attributes before the session closes
                    _ = (
                        job.job_id, job.job_name, job.source_language, job.target_language,
                        job.input_type, job.document_name, job.document_word_count,
                        job.status, job.submitted_at, job.completed_at, job.error,
                        job.job_metadata, job.updated_at,
                    )
                    session.expunge(job)
                    logger.debug(f"Retrieved translate job: {job_id}")
                else:
                    logger.debug(f"Translate job not found: {job_id}")
                return job
        except SQLAlchemyError as e:
            logger.error(f"Database error retrieving translate job {job_id}: {e}", exc_info=True)
            return None
        except Exception as e:
            logger.error(f"Unexpected error retrieving translate job {job_id}: {e}", exc_info=True)
            return None

    @staticmethod
    def get_all_jobs(
        status: Optional[JobStatus] = None,
        limit: int = 20,
        offset: int = 0,
    ) -> Tuple[List[TranslateJob], int]:
        """
        List jobs with optional status filter and pagination.

        Returns:
            Tuple of (page of TranslateJob objects, total matching count).
        """
        try:
            with get_db_session() as session:
                stmt = select(TranslateJob)

                if status is not None:
                    stmt = stmt.where(TranslateJob.status == status.value)

                # Total count before pagination
                count_stmt = select(func.count()).select_from(stmt.subquery())
                total = session.scalar(count_stmt) or 0

                stmt = (
                    stmt.order_by(TranslateJob.submitted_at.desc())
                    .limit(limit)
                    .offset(offset)
                )
                jobs = list(session.scalars(stmt).all())
                for job in jobs:
                    session.expunge(job)
                logger.debug(f"Retrieved {len(jobs)} translate jobs (total: {total})")
                return jobs, total
        except SQLAlchemyError as e:
            logger.error(f"Database error listing translate jobs: {e}", exc_info=True)
            return [], 0
        except Exception as e:
            logger.error(f"Unexpected error listing translate jobs: {e}", exc_info=True)
            return [], 0

    @staticmethod
    def update_job(
        job_id: str,
        status: Optional[JobStatus] = None,
        completed_at: Optional[datetime] = None,
        error: Optional[str] = None,
        metadata: Optional[Dict[str, Any]] = None,
        document_word_count: Optional[int] = None,
        source_language: Optional[str] = None,
    ) -> bool:
        """
        Partial update of a job row.

        Only supplied (non-None) kwargs are written.

        Returns:
            True if the row was found and updated, False otherwise.
        """
        try:
            with get_db_session() as session:
                updates: Dict[str, Any] = {}
                if status is not None:
                    updates["status"] = status.value
                if completed_at is not None:
                    updates["completed_at"] = completed_at
                if error is not None:
                    updates["error"] = error
                if metadata is not None:
                    updates["job_metadata"] = metadata
                if document_word_count is not None:
                    updates["document_word_count"] = document_word_count
                if source_language is not None:
                    updates["source_language"] = source_language

                if not updates:
                    logger.debug(f"No updates provided for translate job {job_id}")
                    return True

                stmt = (
                    update(TranslateJob)
                    .where(TranslateJob.job_id == job_id)
                    .values(**updates)
                )
                result = session.execute(stmt)

                if result.rowcount > 0:
                    logger.debug(f"Updated translate job: {job_id}")
                    return True
                else:
                    logger.warning(f"Translate job not found for update: {job_id}")
                    return False
        except SQLAlchemyError as e:
            logger.error(f"Database error updating translate job {job_id}: {e}", exc_info=True)
            return False
        except Exception as e:
            logger.error(f"Unexpected error updating translate job {job_id}: {e}", exc_info=True)
            return False

    @staticmethod
    def get_active_jobs() -> List[TranslateJob]:
        """
        Return all jobs with status 'accepted' or 'in_progress'.

        Used by the boot-time zombie-recovery scan.
        """
        try:
            with get_db_session() as session:
                stmt = select(TranslateJob).where(
                    or_(
                        TranslateJob.status == JobStatus.ACCEPTED.value,
                        TranslateJob.status == JobStatus.IN_PROGRESS.value,
                    )
                )
                jobs = list(session.scalars(stmt).all())
                for job in jobs:
                    session.expunge(job)
                logger.debug(f"Retrieved {len(jobs)} active translate jobs")
                return jobs
        except SQLAlchemyError as e:
            logger.error(f"Database error retrieving active translate jobs: {e}", exc_info=True)
            return []
        except Exception as e:
            logger.error(f"Unexpected error retrieving active translate jobs: {e}", exc_info=True)
            return []


# Singleton instance
db_manager = TranslateDatabaseManager()

# Made with Bob
