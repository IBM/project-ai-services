"""
Database repository layer for Job and Document operations.

Provides CRUD operations with proper error handling and transaction management.
"""

from datetime import datetime, timezone
from typing import List, Optional, Dict, Any, cast
from sqlalchemy import select, update, delete, func, or_, and_
from sqlalchemy.engine import CursorResult
from sqlalchemy.exc import SQLAlchemyError, IntegrityError

from common.misc_utils import get_logger
from digitize.db.models import Job, Document
from digitize.db.connection import get_db_session
from digitize.models import JobStatus, DocStatus

logger = get_logger("db_repository")


class DatabaseManager:
    """Manager for database operations with error handling and logging."""

    @staticmethod
    def create_job(
        job_id: str,
        operation: str,
        status: JobStatus = JobStatus.ACCEPTED,
        job_name: Optional[str] = None,
        submitted_at: Optional[datetime] = None,
        completed_at: Optional[datetime] = None,
        error: Optional[str] = None,
        stats: Optional[Dict[str, int]] = None
    ) -> Optional[Job]:
        """
        Create a new job in the database.

        Args:
            job_id: Unique identifier for the job
            operation: Type of operation (ingestion/digitization)
            status: Initial job status
            job_name: Optional human-readable name
            submitted_at: Submission timestamp (defaults to now)
            completed_at: Completion timestamp (optional, for import)
            error: Error message (optional, for import)
            stats: Initial statistics dictionary

        Returns:
            Created Job object or None on failure
        """
        try:
            with get_db_session() as session:
                job = Job(
                    job_id=job_id,
                    job_name=job_name,
                    operation=operation,
                    status=status.value,
                    submitted_at=submitted_at or datetime.now(timezone.utc),
                    completed_at=completed_at,
                    error=error,
                    stats=stats or {
                        "total_documents": 0,
                        "completed": 0,
                        "failed": 0,
                        "in_progress": 0
                    }
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
    def get_job_by_id(job_id: str) -> Optional[Job]:
        """
        Retrieve a job by its ID.

        Args:
            job_id: Unique identifier for the job

        Returns:
            Job object or None if not found
        """
        try:
            with get_db_session() as session:
                stmt = select(Job).where(Job.job_id == job_id)
                job = session.scalar(stmt)
                if job:
                    # Eagerly access all attributes to load them before session closes
                    _ = (job.job_id, job.job_name, job.operation, job.status,
                         job.submitted_at, job.completed_at, job.error,
                         job.stats, job.updated_at)
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
        operation: Optional[str] = None,
        limit: int = 20,
        offset: int = 0
    ) -> tuple[List[Job], int]:
        """
        Retrieve all jobs with optional filtering and pagination.

        Args:
            status: Filter by job status
            operation: Filter by operation type
            limit: Maximum number of jobs to return
            offset: Number of jobs to skip

        Returns:
            Tuple of (list of Job objects, total count)
        """
        try:
            with get_db_session() as session:
                # Build query with filters
                stmt = select(Job)
                
                filters = []
                if status:
                    filters.append(Job.status == status.value)
                if operation:
                    filters.append(Job.operation == operation)
                
                if filters:
                    stmt = stmt.where(and_(*filters))
                
                # Get total count
                count_stmt = select(func.count()).select_from(stmt.subquery())
                total = session.scalar(count_stmt) or 0
                
                # Apply ordering and pagination
                stmt = stmt.order_by(Job.submitted_at.desc()).limit(limit).offset(offset)
                
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
        stats: Optional[Dict[str, int]] = None
    ) -> bool:
        """
        Update job fields in the database.

        Args:
            job_id: Unique identifier for the job
            status: New job status
            completed_at: Completion timestamp
            error: Error message
            stats: Updated statistics

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
                if stats is not None:
                    updates["stats"] = stats
                
                if not updates:
                    logger.debug(f"No updates provided for job {job_id}")
                    return True
                
                stmt = update(Job).where(Job.job_id == job_id).values(**updates)
                result = cast(CursorResult, session.execute(stmt))
                
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
                stmt = delete(Job).where(Job.job_id == job_id)
                result = cast(CursorResult, session.execute(stmt))
                
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
    def create_document(
        doc_id: str,
        name: str,
        doc_type: str,
        status: DocStatus,
        output_format: str,
        submitted_at: Optional[datetime] = None,
        completed_at: Optional[datetime] = None,
        error: Optional[str] = None,
        job_id: Optional[str] = None,
        metadata: Optional[Dict[str, Any]] = None
    ) -> Optional[Document]:
        """
        Create a new document in the database.

        Args:
            doc_id: Unique identifier for the document
            name: Document filename
            doc_type: Type of document (ingestion/digitization)
            status: Initial document status
            output_format: Output format (txt/md/json)
            submitted_at: Submission timestamp (defaults to now)
            completed_at: Completion timestamp (optional, for import)
            error: Error message (optional, for import)
            job_id: Associated job ID
            metadata: Additional metadata dictionary

        Returns:
            Created Document object or None on failure
        """
        try:
            with get_db_session() as session:
                document = Document(
                    doc_id=doc_id,
                    job_id=job_id,
                    name=name,
                    type=doc_type,
                    status=status.value,
                    output_format=output_format,
                    submitted_at=submitted_at or datetime.now(timezone.utc),
                    completed_at=completed_at,
                    error=error,
                    doc_metadata=metadata or {}
                )
                session.add(document)
                session.flush()
                logger.info(f"Created document in database: {doc_id}")
                return document
        except IntegrityError as e:
            logger.error(f"Document {doc_id} already exists or invalid job_id: {e}")
            return None
        except SQLAlchemyError as e:
            logger.error(f"Database error creating document {doc_id}: {e}", exc_info=True)
            return None
        except Exception as e:
            logger.error(f"Unexpected error creating document {doc_id}: {e}", exc_info=True)
            return None

    @staticmethod
    def get_document_by_id(doc_id: str) -> Optional[Document]:
        """
        Retrieve a document by its ID.

        Args:
            doc_id: Unique identifier for the document

        Returns:
            Document object or None if not found
        """
        try:
            with get_db_session() as session:
                stmt = select(Document).where(Document.doc_id == doc_id)
                document = session.scalar(stmt)
                if document:
                    # Eagerly access all attributes to load them before session closes
                    _ = (document.doc_id, document.job_id, document.name, document.type,
                         document.status, document.output_format, document.submitted_at,
                         document.completed_at, document.error, document.doc_metadata,
                         document.updated_at)
                    # Expunge the object from session to prevent DetachedInstanceError
                    session.expunge(document)
                    logger.debug(f"Retrieved document from database: {doc_id}")
                else:
                    logger.debug(f"Document not found in database: {doc_id}")
                return document
        except SQLAlchemyError as e:
            logger.error(f"Database error retrieving document {doc_id}: {e}", exc_info=True)
            return None
        except Exception as e:
            logger.error(f"Unexpected error retrieving document {doc_id}: {e}", exc_info=True)
            return None

    @staticmethod
    def get_all_documents(
        status: Optional[str] = None,
        name: Optional[str] = None,
        limit: int = 20,
        offset: int = 0
    ) -> tuple[List[Document], int]:
        """
        Retrieve all documents with optional filtering and pagination.

        Args:
            status: Filter by document status
            name: Filter by document name (partial match)
            limit: Maximum number of documents to return
            offset: Number of documents to skip

        Returns:
            Tuple of (list of Document objects, total count)
        """
        try:
            with get_db_session() as session:
                # Build query with filters
                stmt = select(Document)
                
                filters = []
                if status:
                    filters.append(Document.status == status)
                if name:
                    filters.append(Document.name.ilike(f"%{name}%"))
                
                if filters:
                    stmt = stmt.where(and_(*filters))
                
                # Get total count
                count_stmt = select(func.count()).select_from(stmt.subquery())
                total = session.scalar(count_stmt) or 0
                
                # Apply ordering and pagination
                stmt = stmt.order_by(Document.submitted_at.desc()).limit(limit).offset(offset)
                
                documents = list(session.scalars(stmt).all())
                # Eagerly load all attributes and expunge documents from session
                for doc in documents:
                    # Access all attributes to load them before session closes
                    _ = (doc.doc_id, doc.job_id, doc.name, doc.type, doc.status,
                         doc.output_format, doc.submitted_at, doc.completed_at,
                         doc.error, doc.doc_metadata, doc.updated_at)
                    session.expunge(doc)
                logger.debug(f"Retrieved {len(documents)} documents from database (total: {total})")
                return documents, total
        except SQLAlchemyError as e:
            logger.error(f"Database error retrieving documents: {e}", exc_info=True)
            return [], 0
        except Exception as e:
            logger.error(f"Unexpected error retrieving documents: {e}", exc_info=True)
            return [], 0

    @staticmethod
    def get_documents_by_job_id(job_id: str) -> List[Document]:
        """
        Retrieve all documents associated with a job.

        Args:
            job_id: Unique identifier for the job

        Returns:
            List of Document objects
        """
        try:
            with get_db_session() as session:
                stmt = select(Document).where(Document.job_id == job_id).order_by(Document.submitted_at)
                documents = list(session.scalars(stmt).all())
                # Eagerly load all attributes and expunge documents from session
                for doc in documents:
                    # Access all attributes to load them before session closes
                    _ = (doc.doc_id, doc.job_id, doc.name, doc.type, doc.status,
                         doc.output_format, doc.submitted_at, doc.completed_at,
                         doc.error, doc.doc_metadata, doc.updated_at)
                    session.expunge(doc)
                logger.debug(f"Retrieved {len(documents)} documents for job {job_id}")
                return documents
        except SQLAlchemyError as e:
            logger.error(f"Database error retrieving documents for job {job_id}: {e}", exc_info=True)
            return []
        except Exception as e:
            logger.error(f"Unexpected error retrieving documents for job {job_id}: {e}", exc_info=True)
            return []

    @staticmethod
    def update_document(
        doc_id: str,
        status: Optional[DocStatus] = None,
        completed_at: Optional[datetime] = None,
        error: Optional[str] = None,
        metadata: Optional[Dict[str, Any]] = None
    ) -> bool:
        """
        Update document fields in the database.

        Args:
            doc_id: Unique identifier for the document
            status: New document status
            completed_at: Completion timestamp
            error: Error message
            metadata: Updated metadata dictionary

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
                    updates["doc_metadata"] = metadata
                
                if not updates:
                    logger.debug(f"No updates provided for document {doc_id}")
                    return True
                
                stmt = update(Document).where(Document.doc_id == doc_id).values(**updates)
                result = cast(CursorResult, session.execute(stmt))
                
                if result.rowcount > 0:
                    logger.debug(f"Updated document in database: {doc_id}")
                    return True
                else:
                    logger.warning(f"Document not found for update: {doc_id}")
                    return False
        except SQLAlchemyError as e:
            logger.error(f"Database error updating document {doc_id}: {e}", exc_info=True)
            return False
        except Exception as e:
            logger.error(f"Unexpected error updating document {doc_id}: {e}", exc_info=True)
            return False

    @staticmethod
    def delete_document(doc_id: str) -> bool:
        """
        Delete a document from the database.

        Args:
            doc_id: Unique identifier for the document

        Returns:
            True if deletion successful, False otherwise
        """
        try:
            with get_db_session() as session:
                stmt = delete(Document).where(Document.doc_id == doc_id)
                result = cast(CursorResult, session.execute(stmt))
                
                if result.rowcount > 0:
                    logger.info(f"Deleted document from database: {doc_id}")
                    return True
                else:
                    logger.warning(f"Document not found for deletion: {doc_id}")
                    return False
        except SQLAlchemyError as e:
            logger.error(f"Database error deleting document {doc_id}: {e}", exc_info=True)
            return False
        except Exception as e:
            logger.error(f"Unexpected error deleting document {doc_id}: {e}", exc_info=True)
            return False

    @staticmethod
    def get_active_jobs(operation: Optional[str] = None) -> List[Job]:
        """
        Get all active jobs (accepted or in_progress status).

        Args:
            operation: Optional filter by operation type

        Returns:
            List of active Job objects
        """
        try:
            with get_db_session() as session:
                stmt = select(Job).where(
                    or_(
                        Job.status == JobStatus.ACCEPTED.value,
                        Job.status == JobStatus.IN_PROGRESS.value
                    )
                )
                
                if operation:
                    stmt = stmt.where(Job.operation == operation)
                
                jobs = list(session.scalars(stmt).all())
                logger.debug(f"Retrieved {len(jobs)} active jobs")
                return jobs
        except SQLAlchemyError as e:
            logger.error(f"Database error retrieving active jobs: {e}", exc_info=True)
            return []
        except Exception as e:
            logger.error(f"Unexpected error retrieving active jobs: {e}", exc_info=True)
            return []
    @staticmethod
    def delete_all_documents() -> Dict[str, Any]:
        """
        Delete all documents from the database.
        
        Returns:
            Dictionary with deletion statistics:
            - deleted_count: Number of documents deleted
            - success: Whether operation completed successfully
        """
        try:
            with get_db_session() as session:
                stmt = delete(Document)
                result = cast(CursorResult, session.execute(stmt))
                deleted_count = result.rowcount
                
                logger.info(f"Deleted all documents from database: {deleted_count} documents")
                return {
                    "deleted_count": deleted_count,
                    "success": True
                }
        except SQLAlchemyError as e:
            logger.error(f"Database error deleting all documents: {e}", exc_info=True)
            return {
                "deleted_count": 0,
                "success": False,
                "error": str(e)
            }
        except Exception as e:
            logger.error(f"Unexpected error deleting all documents: {e}", exc_info=True)
            return {
                "deleted_count": 0,
                "success": False,
                "error": str(e)
            }
    
    @staticmethod
    def delete_all_jobs() -> Dict[str, Any]:
        """
        Delete all jobs from the database.
        
        Returns:
            Dictionary with deletion statistics:
            - deleted_count: Number of jobs deleted
            - success: Whether operation completed successfully
        """
        try:
            with get_db_session() as session:
                stmt = delete(Job)
                result = cast(CursorResult, session.execute(stmt))
                deleted_count = result.rowcount
                
                logger.info(f"Deleted all jobs from database: {deleted_count} jobs")
                return {
                    "deleted_count": deleted_count,
                    "success": True
                }
        except SQLAlchemyError as e:
            logger.error(f"Database error deleting all jobs: {e}", exc_info=True)
            return {
                "deleted_count": 0,
                "success": False,
                "error": str(e)
            }
        except Exception as e:
            logger.error(f"Unexpected error deleting all jobs: {e}", exc_info=True)
            return {
                "deleted_count": 0,
                "success": False,
                "error": str(e)
            }



# Singleton instance for easy access
db_manager = DatabaseManager()

# Made with Bob
