"""
Database repository layer for Job and Document operations.

Provides CRUD operations with proper error handling and transaction management.
"""

from datetime import datetime, timezone
from typing import List, Optional, Dict, Any
from sqlalchemy import select, update, delete, func, or_, and_
from sqlalchemy.exc import SQLAlchemyError, IntegrityError

from common.misc_utils import get_logger
from summarize.db.models import SummarizeJob
from summarize.db.connection import get_db_session
from summarize.models import JobStatus, SummarizationLevel, SummarizationType

logger = get_logger("db_repository")


class DatabaseManager:
    """Repository for database operations with error handling and logging."""

    @staticmethod
    def create_job(
            job_id: str,
            status: JobStatus = JobStatus.ACCEPTED,
            job_name: Optional[str] = None,
            doc_name: Optional[str]= None,
            word_count: int= 0,
            job_level: SummarizationLevel = SummarizationLevel.STANDARD,
            job_type: SummarizationType = SummarizationType.DIRECT,
            submitted_at: Optional[datetime] = None
    ) -> Optional[SummarizeJob]:
        """
        Create a new job in the database.

        Args:
            job_id: Unique identifier for the job
            doc_name: name of the document
            status: Initial job status
            job_name: Optional human-readable name
            submitted_at: Submission timestamp (defaults to now)
            job_level: level specified for summarization
            job_type: summarization type - direct or chunked
            word_count: no of words in the document


        Returns:
            Created Job object or None on failure
        """
        try:
            with get_db_session() as session:
                job = SummarizeJob(
                    job_id=job_id,
                    job_name=job_name,
                    document_name= doc_name,
                    document_word_count=word_count,
                    status=status.value,
                    level=job_level,
                    job_type=job_type,
                    submitted_at=submitted_at or datetime.now(timezone.utc),
                )
                session.add(job)
                session.flush()  # Ensure job is persisted before returning
                logger.info(f"Created job in database: {job_id}")
                return job
        except IntegrityError as e:
            logger.error(f"Job {job_id} already exists in database: {e}")
            return None
        except SQLAlchemyError as e:
            logger.error(f"Database error creating job {job_id}: {e}", exc_info=True)
            return None
        except Exception as e:
            logger.error(f"Unexpected error creating job {job_id}: {e}", exc_info=True)
            return None

    @staticmethod
    def get_job_by_id(job_id: str) -> Optional[SummarizeJob]:
        """
        Retrieve a job by its ID.

        Args:
            job_id: Unique identifier for the job

        Returns:
            Job object or None if not found
        """
        try:
            with get_db_session() as session:
                stmt = select(SummarizeJob).where(SummarizeJob.job_id == job_id)
                job = session.scalar(stmt)
                if job:
                    # Eagerly access all attributes to load them before session closes
                    _ = (job.job_id, job.job_name, job.level, job.status,
                         job.submitted_at, job.completed_at, job.error,
                         job.metadata, job.updated_at)
                    # Expunge the object from session to prevent DetachedInstanceError
                    session.expunge(job)
                    logger.debug(f"Retrieved job from database: {job_id}")
                else:
                    logger.debug(f"Job not found in database: {job_id}")
                return job
        except SQLAlchemyError as e:
            logger.error(f"Database error retrieving job {job_id}: {e}", exc_info=True)
            return None
        except Exception as e:
            logger.error(f"Unexpected error retrieving job {job_id}: {e}", exc_info=True)
            return None

    @staticmethod
    def get_all_jobs(
            status: Optional[JobStatus] = None,
            job_type: Optional[SummarizationType] = None,
            limit: int = 20,
            offset: int = 0
    ) -> tuple[List[SummarizeJob], int]:
        """
        Retrieve all jobs with optional filtering and pagination.

        Args:
            status: Filter by job status
            job_type: Filter by job type
            limit: Maximum number of jobs to return
            offset: Number of jobs to skip

        Returns:
            Tuple of (list of Job objects, total count)
        """
        try:
            with get_db_session() as session:
                # Build query with filters
                stmt = select(SummarizeJob)

                filters = []
                if status:
                    filters.append(SummarizeJob.status == status.value)
                if job_type:
                    filters.append(SummarizeJob.job_type == job_type)

                if filters:
                    stmt = stmt.where(and_(*filters))

                # Get total count
                count_stmt = select(func.count()).select_from(stmt.subquery())
                total = session.scalar(count_stmt) or 0

                # Apply ordering and pagination
                stmt = stmt.order_by(SummarizeJob.submitted_at.desc()).limit(limit).offset(offset)

                jobs = list(session.scalars(stmt).all())
                # Expunge all jobs from session to prevent DetachedInstanceError
                for job in jobs:
                    session.expunge(job)
                logger.debug(f"Retrieved {len(jobs)} jobs from database (total: {total})")
                return jobs, total
        except SQLAlchemyError as e:
            logger.error(f"Database error retrieving jobs: {e}", exc_info=True)
            return [], 0
        except Exception as e:
            logger.error(f"Unexpected error retrieving jobs: {e}", exc_info=True)
            return [], 0

    @staticmethod
    def update_job(
            job_id: str,
            status: Optional[JobStatus] = None,
            completed_at: Optional[datetime] = None,
            error: Optional[str] = None,
            metadata : Optional[Dict[str, Any]] = None
    ) -> bool:
        """
        Update job fields in the database.

        Args:
            job_id: Unique identifier for the job
            status: New job status
            completed_at: Completion timestamp
            error: Error message
            metadata: Updated metadata

        Returns:
            True if update successful, False otherwise
        """
        try:
            with get_db_session() as session:
                updates = {}
                if status is not None:
                    updates["status"] = status.value
                if completed_at is not None:
                    updates["completed_at"] = completed_at
                if error is not None:
                    updates["error"] = error
                if metadata is not None:
                    updates["job_metadata"] = metadata

                if not updates:
                    logger.debug(f"No updates provided for job {job_id}")
                    return True

                stmt = update(SummarizeJob).where(SummarizeJob.job_id == job_id).values(**updates)
                result = session.execute(stmt)

                if result.rowcount > 0:
                    logger.debug(f"Updated job in database: {job_id}")
                    return True
                else:
                    logger.warning(f"Job not found for update: {job_id}")
                    return False
        except SQLAlchemyError as e:
            logger.error(f"Database error updating job {job_id}: {e}", exc_info=True)
            return False
        except Exception as e:
            logger.error(f"Unexpected error updating job {job_id}: {e}", exc_info=True)
            return False

    @staticmethod
    def delete_job(job_id: str) -> bool:
        """
        Delete a job from the database.

        Args:
            job_id: Unique identifier for the job

        Returns:
            True if deletion successful, False otherwise
        """
        try:
            with get_db_session() as session:
                stmt = delete(SummarizeJob).where(SummarizeJob.job_id == job_id)
                result = session.execute(stmt)

                if result.rowcount > 0:
                    logger.info(f"Deleted job from database: {job_id}")
                    return True
                else:
                    logger.warning(f"Job not found for deletion: {job_id}")
                    return False
        except SQLAlchemyError as e:
            logger.error(f"Database error deleting job {job_id}: {e}", exc_info=True)
            return False
        except Exception as e:
            logger.error(f"Unexpected error deleting job {job_id}: {e}", exc_info=True)
            return False


    @staticmethod
    def get_active_jobs(job_type: Optional[str] = None) -> List[SummarizeJob]:
        """
        Get all active jobs (accepted or in_progress status).

        Args:
            job_type: Optional filter by operation type

        Returns:
            List of active Job objects
        """
        try:
            with get_db_session() as session:
                stmt = select(SummarizeJob).where(
                    or_(
                        SummarizeJob.status == JobStatus.ACCEPTED.value,
                        SummarizeJob.status == JobStatus.IN_PROGRESS.value
                    )
                )

                if job_type:
                    stmt = stmt.where(SummarizeJob.job_type == job_type)

                jobs = list(session.scalars(stmt).all())
                # Expunge all jobs from session to prevent DetachedInstanceError
                for job in jobs:
                    session.expunge(job)
                logger.debug(f"Retrieved {len(jobs)} active jobs")
                return jobs
        except SQLAlchemyError as e:
            logger.error(f"Database error retrieving active jobs: {e}", exc_info=True)
            return []
        except Exception as e:
            logger.error(f"Unexpected error retrieving active jobs: {e}", exc_info=True)
            return []

    @staticmethod
    def delete_all_jobs() -> bool:
        """
        Delete all jobs from the database.
        Used for bulk cleanup operations.
        
        Returns:
            True if deletion successful, False otherwise
        """
        try:
            with get_db_session() as session:
                stmt = delete(SummarizeJob)
                result = session.execute(stmt)
                logger.info(f"Deleted {result.rowcount} jobs from database")
                return True
        except SQLAlchemyError as e:
            logger.error(f"Database error deleting all jobs: {e}", exc_info=True)
            return False
        except Exception as e:
            logger.error(f"Unexpected error deleting all jobs: {e}", exc_info=True)
            return False


# Singleton instance for easy access
db_repo = DatabaseManager()
