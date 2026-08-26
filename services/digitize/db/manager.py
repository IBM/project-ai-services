"""
Database repository layer for Job, Document, Connector, and ConversionTask operations.

Provides CRUD operations with proper error handling and transaction management.
"""

from datetime import datetime, timezone
from pathlib import Path
from typing import List, Optional, Dict, Any, cast
from sqlalchemy import select, update, delete, func, or_, and_
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.engine import CursorResult
from sqlalchemy.exc import SQLAlchemyError, IntegrityError

from common.misc_utils import get_logger
from digitize.db.models import (
    Job, Document, DocumentChecksum,
    Connector, ConnectorDocumentChecksum, ConnectorSyncLog,
    ConversionTask,
)
from digitize.db.connection import get_db_session
from digitize.models import JobStatus, DocStatus
from digitize.connectors.models import ConnectorStatus, SyncLogStatus

logger = get_logger("db_repository")

# Sentinel used by update_connector to distinguish "not supplied" from explicit None.
_UNSET: object = object()


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
                return document
        except IntegrityError as e:
            logger.error(f"Document {doc_id} already exists or invalid job_id: {e}")
            raise
        except SQLAlchemyError as e:
            logger.error(f"Database error creating document {doc_id}: {e}", exc_info=True)
            raise
        except Exception as e:
            logger.error(f"Unexpected error creating document {doc_id}: {e}", exc_info=True)
            raise

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
    def upsert_file_checksum(checksum: str, doc_id: str) -> None:
        """
        Insert or ignore a (checksum, doc_id) pair into document_checksum.

        Called once a document reaches COMPLETED status so that subsequent
        uploads of the same content can be detected via find_completed_document_by_hash.

        Args:
            checksum: MD5 hex digest string.
            doc_id: The completed document's primary key.
        """
        try:
            with get_db_session() as session:
                stmt = (
                    pg_insert(DocumentChecksum)
                    .values(checksum=checksum, doc_id=doc_id)
                    .on_conflict_do_update(
                        index_elements=["checksum"],
                        set_={"doc_id": doc_id},
                    )
                )
                session.execute(stmt)
                logger.debug(f"Upserted checksum registry: checksum={checksum[:20]}... doc_id={doc_id}")
        except SQLAlchemyError as e:
            logger.error(f"DB error upserting checksum for {checksum[:20]}...: {e}", exc_info=True)

    @staticmethod
    def find_completed_document_by_hash(
        file_hash: str,
        operation: str = "ingestion",
    ) -> Optional[Document]:
        """
        Find the completed document of the given operation type with a matching
        file hash, using the document_checksum lookup table.

        Only documents with status='completed' and the specified type are considered.
        Failed and in-progress documents are deliberately excluded so that a previous
        failed attempt does not prevent re-processing of the same file.

        Args:
            file_hash: MD5 hex digest string.
            operation: Document type to match — 'ingestion' or 'digitization'.

        Returns:
            The matching Document ORM object (attributes eagerly loaded and
            expunged from session), or None if no completed duplicate exists.
        """
        try:
            with get_db_session() as session:
                stmt = (
                    select(Document)
                    .join(
                        DocumentChecksum,
                        DocumentChecksum.doc_id == Document.doc_id,
                    )
                    .where(
                        DocumentChecksum.checksum == file_hash,
                        Document.type == operation,
                        Document.status == DocStatus.COMPLETED.value,
                    )
                    .limit(1)
                )
                doc = session.scalar(stmt)
                if doc:
                    # Eagerly load all attributes before session closes to prevent
                    # DetachedInstanceError in the caller.
                    _ = (
                        doc.doc_id, doc.job_id, doc.name, doc.type,
                        doc.status, doc.output_format, doc.submitted_at,
                        doc.completed_at, doc.error, doc.doc_metadata,
                    )
                    session.expunge(doc)
                    logger.debug(
                        f"Duplicate detected: file_hash={file_hash[:20]}... "
                        f"matches doc_id={doc.doc_id}"
                    )
                return doc
        except SQLAlchemyError as e:
            logger.error(f"DB error in hash lookup for {file_hash[:20]}...: {e}", exc_info=True)
            # Do NOT raise — a lookup failure must not block ingestion.
            # If the DB is unavailable the caller treats the file as novel.
            return None


    @staticmethod
    def get_all_documents(
        status: Optional[str] = None,
        name: Optional[str] = None,
        limit: int = 20,
        offset: int = 0,
        exclude_connector_sourced: bool = False,
    ) -> tuple[List[Document], int]:
        """
        Retrieve all documents with optional filtering and pagination.

        Args:
            status: Filter by document status
            name: Filter by document name (partial match)
            limit: Maximum number of documents to return
            offset: Number of documents to skip
            exclude_connector_sourced: When True, omit docs that appear in
                connector_document_checksum (connector-sourced documents).

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
                else:
                    # Exclude already_exists docs from the global listing — they are
                    # job-scoped audit records, not real ingested documents.
                    filters.append(Document.status != DocStatus.ALREADY_EXISTS.value)
                if name:
                    filters.append(Document.name.ilike(f"%{name}%"))
                if exclude_connector_sourced:
                    # Exclude connector-sourced documents via NOT EXISTS subquery.
                    filters.append(
                        ~select(ConnectorDocumentChecksum.doc_id)
                        .where(ConnectorDocumentChecksum.doc_id == Document.doc_id)
                        .exists()
                    )

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

        Also removes:
        - The checksum registry entry (so the hash can be re-registered on
          re-ingestion of the same file).
        - Any 'already_exists' shadow documents whose metadata points to this
          doc_id as their existing_doc_id, since those placeholder rows have
          no meaning once the original document is gone.

        Args:
            doc_id: Unique identifier for the document

        Returns:
            True if deletion successful, False otherwise
        """
        try:
            with get_db_session() as session:
                # Remove checksum registry entry first so the hash can be re-registered
                # if the same file is ingested again after deletion.
                session.execute(
                    delete(DocumentChecksum).where(DocumentChecksum.doc_id == doc_id)
                )

                # Remove any already_exists shadow docs that reference this document.
                # These placeholder rows are created when a duplicate file is submitted;
                # their metadata stores the original doc_id under 'existing_doc_id'.
                # Once the original is deleted they become stale, so clean them up too.
                session.execute(
                    delete(Document).where(
                        Document.doc_metadata["existing_doc_id"].as_string() == doc_id
                    )
                )

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
                # Wipe the checksum registry so previously-seen hashes are no longer
                # blocked after a full reset.
                session.execute(delete(DocumentChecksum))
                stmt = delete(Document)
                result = cast(CursorResult, session.execute(stmt))
                deleted_count = result.rowcount
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



    # ========================================================================
    # Connector CRUD
    # ========================================================================

    @staticmethod
    def insert_connector(
        connector_id: str,
        name: str,
        connector_type: str,
        connection_details: dict,
        allowed_extensions: list,
        sync_interval_seconds: int,
    ) -> None:
        """
        Insert a new connector row.

        Uses ON CONFLICT (id) DO NOTHING and re-raises IntegrityError when the
        id already exists so the caller can map that to a 409 response.
        """
        try:
            with get_db_session() as session:
                stmt = (
                    pg_insert(Connector)
                    .values(
                        id=connector_id,
                        name=name,
                        type=connector_type,
                        connection_details=connection_details,
                        allowed_extensions=allowed_extensions,
                        sync_interval_seconds=sync_interval_seconds,
                        attached_at=datetime.now(timezone.utc),
                        sync_status=ConnectorStatus.UP_TO_DATE,
                        total_files=0,
                    )
                    .on_conflict_do_nothing(index_elements=["id"])
                )
                result = session.execute(stmt)
                if result.rowcount == 0:
                    raise IntegrityError(
                        statement=None,
                        params=None,
                        orig=Exception(f"Connector id={connector_id!r} already exists"),
                    )
                logger.info(f"Inserted connector {connector_id!r} ({name!r})")
        except IntegrityError:
            raise
        except SQLAlchemyError as e:
            logger.error(f"DB error inserting connector {connector_id}: {e}", exc_info=True)
            raise

    @staticmethod
    def update_connector(
        connector_id: str,
        name: Optional[str] = None,
        connection_details: Optional[dict] = None,
        allowed_extensions: Optional[list] = None,
        total_files: Optional[int] = None,
        error: "Optional[str]" = _UNSET,  # type: ignore[assignment]
    ) -> None:
        """
        Partial update of an existing connector.

        Only non-``_UNSET`` kwargs are written; connection_details is merged at
        the key level using the PostgreSQL ``||`` JSONB concatenation operator.
        Pass ``error=None`` explicitly to clear a previously set error to NULL.

        Raises FileNotFoundError if no connector with the given id exists.
        """
        try:
            with get_db_session() as session:
                values: Dict[str, Any] = {}
                if name is not None:
                    values["name"] = name
                if allowed_extensions is not None:
                    values["allowed_extensions"] = allowed_extensions
                if total_files is not None:
                    values["total_files"] = total_files
                if error is not _UNSET:
                    values["error"] = error
                if connection_details is not None:
                    stmt = (
                        update(Connector)
                        .where(Connector.id == connector_id)
                        .values(
                            connection_details=Connector.connection_details.op("||")(
                                func.cast(connection_details, Connector.connection_details.type)
                            ),
                            **values,
                        )
                    )
                else:
                    stmt = (
                        update(Connector)
                        .where(Connector.id == connector_id)
                        .values(**values)
                    )
                result = session.execute(stmt)
                if result.rowcount == 0:
                    raise FileNotFoundError(f"Connector id={connector_id!r} not found")
                logger.info(f"Updated connector {connector_id!r}")
        except FileNotFoundError:
            raise
        except SQLAlchemyError as e:
            logger.error(f"DB error updating connector {connector_id}: {e}", exc_info=True)
            raise

    @staticmethod
    def get_connector_by_id(connector_id: str) -> Optional[Connector]:
        """
        Fetch a single connector by id, attributes eagerly loaded and expunged.

        Returns None if not found.
        """
        try:
            with get_db_session() as session:
                connector = session.get(Connector, connector_id)
                if connector is None:
                    logger.debug(f"Connector {connector_id!r} not found")
                    return None
                _ = (
                    connector.id, connector.name, connector.type,
                    connector.connection_details, connector.allowed_extensions,
                    connector.sync_interval_seconds, connector.attached_at,
                    connector.last_sync_at, connector.sync_status,
                    connector.error, connector.total_files,
                )
                session.expunge(connector)
                return connector
        except SQLAlchemyError as e:
            logger.error(f"DB error fetching connector {connector_id}: {e}", exc_info=True)
            return None

    @staticmethod
    def get_connector_sync_status(connector_id: str) -> Optional[str]:
        """
        Return the current sync_status string for a connector.

        Does a minimal SELECT sync_status query — does not load the full row.
        Returns None if the connector does not exist.
        """
        try:
            with get_db_session() as session:
                stmt = (
                    select(Connector.sync_status)
                    .where(Connector.id == connector_id)
                )
                row = session.execute(stmt).one_or_none()
                return row[0] if row else None
        except SQLAlchemyError as e:
            logger.error(
                f"DB error in get_connector_sync_status({connector_id!r}): {e}",
                exc_info=True,
            )
            return None

    @staticmethod
    def get_all_connectors() -> List[Connector]:
        """
        Return all connectors ordered by attached_at descending.

        Each object is eagerly loaded and expunged from the session.
        """
        try:
            with get_db_session() as session:
                stmt = select(Connector).order_by(Connector.attached_at.desc())
                connectors = list(session.scalars(stmt).all())
                for c in connectors:
                    _ = (
                        c.id, c.name, c.type, c.connection_details,
                        c.allowed_extensions, c.sync_interval_seconds,
                        c.attached_at, c.last_sync_at, c.sync_status,
                        c.error, c.total_files,
                    )
                    session.expunge(c)
                logger.debug(f"Listed {len(connectors)} connector(s)")
                return connectors
        except SQLAlchemyError as e:
            logger.error(f"DB error listing connectors: {e}", exc_info=True)
            return []

    @staticmethod
    def delete_connector(connector_id: str) -> bool:
        """
        Delete a connector row by id (cascades to connector_sync_logs).

        Returns True if a row was deleted, False if not found.
        """
        try:
            with get_db_session() as session:
                stmt = delete(Connector).where(Connector.id == connector_id)
                result = session.execute(stmt)
                deleted = result.rowcount > 0
                if deleted:
                    logger.info(f"Deleted connector {connector_id!r}")
                else:
                    logger.debug(f"Connector {connector_id!r} not found for deletion")
                return deleted
        except SQLAlchemyError as e:
            logger.error(f"DB error deleting connector {connector_id}: {e}", exc_info=True)
            return False

    # ========================================================================
    # Connector checksum helpers
    # ========================================================================

    @staticmethod
    def find_connector_doc_by_checksum(checksum: str) -> Optional[str]:
        """Return the doc_id if any connector has registered this checksum, else None."""
        try:
            with get_db_session() as session:
                stmt = (
                    select(ConnectorDocumentChecksum.doc_id)
                    .where(ConnectorDocumentChecksum.checksum == checksum)
                    .limit(1)
                )
                row = session.execute(stmt).one_or_none()
                return row[0] if row else None
        except SQLAlchemyError as e:
            logger.error(f"DB error in find_connector_doc_by_checksum: {e}", exc_info=True)
            return None

    @staticmethod
    def get_connector_checksums(connector_id: str) -> List[str]:
        """Return all checksums owned by the given connector."""
        try:
            with get_db_session() as session:
                stmt = select(ConnectorDocumentChecksum.checksum).where(
                    ConnectorDocumentChecksum.connector_id == connector_id
                )
                rows = session.execute(stmt).all()
                return [r[0] for r in rows]
        except SQLAlchemyError as e:
            logger.error(f"DB error in get_connector_checksums({connector_id}): {e}", exc_info=True)
            return []

    @staticmethod
    def get_all_connector_checksums() -> List[str]:
        """Return all distinct checksums across all connectors."""
        try:
            with get_db_session() as session:
                stmt = select(ConnectorDocumentChecksum.checksum).distinct()
                rows = session.execute(stmt).all()
                return [r[0] for r in rows]
        except SQLAlchemyError as e:
            logger.error(f"DB error in get_all_connector_checksums: {e}", exc_info=True)
            return []

    @staticmethod
    def insert_connector_checksum(connector_id: str, checksum: str, doc_id: str) -> None:
        """
        Insert a (checksum, connector_id, doc_id) row.

        No-op if the row already exists (ON CONFLICT DO NOTHING).
        """
        try:
            with get_db_session() as session:
                stmt = (
                    pg_insert(ConnectorDocumentChecksum)
                    .values(checksum=checksum, connector_id=connector_id, doc_id=doc_id)
                    .on_conflict_do_nothing(index_elements=["checksum", "connector_id"])
                )
                session.execute(stmt)
                logger.debug(
                    f"insert_connector_checksum: connector={connector_id!r} "
                    f"checksum={checksum[:20]}... doc_id={doc_id!r}"
                )
        except SQLAlchemyError as e:
            logger.error(
                f"DB error in insert_connector_checksum({connector_id}, {checksum[:20]}...): {e}",
                exc_info=True,
            )
            raise

    @staticmethod
    def delete_connector_checksum(
        connector_id: str, checksum: str
    ) -> Optional[str]:
        """
        Delete the (checksum, connector_id) row.

        Returns the doc_id of the deleted row, or None if the row did not exist.
        """
        try:
            with get_db_session() as session:
                del_stmt = (
                    delete(ConnectorDocumentChecksum)
                    .where(
                        ConnectorDocumentChecksum.checksum == checksum,
                        ConnectorDocumentChecksum.connector_id == connector_id,
                    )
                    .returning(ConnectorDocumentChecksum.doc_id)
                )
                deleted_row = session.execute(del_stmt).one_or_none()
                if deleted_row is None:
                    return None
                doc_id: str = deleted_row[0]
                logger.debug(
                    f"delete_connector_checksum: connector={connector_id!r} "
                    f"checksum={checksum[:20]}... doc_id={doc_id!r}"
                )
                return doc_id
        except SQLAlchemyError as e:
            logger.error(
                f"DB error in delete_connector_checksum({connector_id}, {checksum[:20]}...): {e}",
                exc_info=True,
            )
            raise

    @staticmethod
    def count_checksum_owners(checksum: str) -> int:
        """
        Return the number of connector rows that still reference *checksum*.
        """
        try:
            with get_db_session() as session:
                count_stmt = select(func.count()).where(
                    ConnectorDocumentChecksum.checksum == checksum
                )
                return session.execute(count_stmt).scalar() or 0
        except SQLAlchemyError as e:
            logger.error(f"DB error in count_checksum_owners({checksum[:20]}...): {e}", exc_info=True)
            raise

    # ========================================================================
    # Sync log helpers
    # ========================================================================

    @staticmethod
    def get_active_sync_seq(connector_id: str) -> Optional[int]:
        """
        Return the seq of the currently-running sync-log row for connector_id.

        A row is considered active when its status is 'started' or 'cancel pending'.
        Returns None if no active row exists.
        """
        try:
            with get_db_session() as session:
                row = session.execute(
                    select(ConnectorSyncLog.seq)
                    .where(
                        ConnectorSyncLog.connector_id == connector_id,
                        ConnectorSyncLog.status.in_(
                            [SyncLogStatus.STARTED, SyncLogStatus.CANCEL_PENDING]
                        ),
                    )
                    .order_by(ConnectorSyncLog.seq.desc())
                    .limit(1)
                ).one_or_none()
                return row[0] if row else None
        except SQLAlchemyError as e:
            logger.error(f"DB error in get_active_sync_seq({connector_id!r}): {e}", exc_info=True)
            raise

    @staticmethod
    def try_acquire_sync_lock(connector_id: str) -> bool:
        """
        Atomically set sync_status='syncing' if it is not already 'syncing'.

        Returns True if the lock was acquired, False if another tick already
        holds it (or the connector row does not exist).
        """
        try:
            with get_db_session() as session:
                result = session.execute(
                    update(Connector)
                    .where(
                        Connector.id == connector_id,
                        Connector.sync_status != ConnectorStatus.SYNCING,
                    )
                    .values(sync_status=ConnectorStatus.SYNCING)
                    .returning(Connector.id)
                ).one_or_none()
                return result is not None
        except SQLAlchemyError as e:
            logger.error(f"DB error in try_acquire_sync_lock({connector_id}): {e}", exc_info=True)
            raise

    @staticmethod
    def mark_sync_cancel_pending(connector_id: str) -> bool:
        """
        Signal a running tick to cancel by setting connector_sync_logs.status='cancel pending'.

        Returns True if the signal was written, False if no started sync-log row exists.
        """
        try:
            with get_db_session() as session:
                result = session.execute(
                    update(ConnectorSyncLog)
                    .where(
                        ConnectorSyncLog.connector_id == connector_id,
                        ConnectorSyncLog.status == SyncLogStatus.STARTED,
                    )
                    .values(status=SyncLogStatus.CANCEL_PENDING)
                    .returning(ConnectorSyncLog.seq)
                ).one_or_none()
                return result is not None
        except SQLAlchemyError as e:
            logger.error(f"DB error in mark_sync_cancel_pending({connector_id!r}): {e}", exc_info=True)
            raise

    @staticmethod
    def mark_connector_delete_pending(connector_id: str) -> bool:
        """
        Set sync_status='delete pending' for the given connector regardless of
        current status.

        Returns True if the connector was found and updated, False if it does
        not exist.
        """
        try:
            with get_db_session() as session:
                result = session.execute(
                    update(Connector)
                    .where(
                        Connector.id == connector_id
                    )
                    .values(sync_status=ConnectorStatus.DELETE_PENDING)
                    .returning(Connector.id)
                ).one_or_none()
                return result is not None
        except SQLAlchemyError as e:
            logger.error(f"DB error in mark_connector_delete_pending({connector_id!r}): {e}", exc_info=True)
            raise

    @staticmethod
    def insert_sync_log(
        connector_id: str,
        started_at: Optional[datetime] = None,
    ) -> int:
        """
        Insert a new sync-log row with status=STARTED and an auto-incremented seq
        (COALESCE(MAX(seq), 0) + 1) scoped to this connector.

        Returns the generated seq value.
        """
        try:
            with get_db_session() as session:
                seq_subquery = (
                    select(func.coalesce(func.max(ConnectorSyncLog.seq), 0) + 1)
                    .where(ConnectorSyncLog.connector_id == connector_id)
                    .scalar_subquery()
                )
                stmt = (
                    pg_insert(ConnectorSyncLog)
                    .values(
                        connector_id=connector_id,
                        seq=seq_subquery,
                        started_at=started_at or datetime.now(timezone.utc),
                        status=SyncLogStatus.STARTED,
                        error="",
                    )
                    .returning(ConnectorSyncLog.seq)
                )
                seq: int = session.execute(stmt).scalar_one()
                logger.debug(f"insert_sync_log: connector={connector_id!r} seq={seq}")
                return seq
        except SQLAlchemyError as e:
            logger.error(f"DB error in insert_sync_log({connector_id}): {e}", exc_info=True)
            raise

    @staticmethod
    def set_connector_sync_status_syncing(connector_id: str) -> None:
        """Set sync_status=SYNCING on the connector row."""
        try:
            with get_db_session() as session:
                session.execute(
                    update(Connector)
                    .where(Connector.id == connector_id)
                    .values(sync_status=ConnectorStatus.SYNCING)
                )
        except SQLAlchemyError as e:
            logger.error(f"DB error in set_connector_sync_status_syncing({connector_id}): {e}", exc_info=True)
            raise

    @staticmethod
    def finalize_sync_log(
        connector_id: str,
        seq: int,
        status: str,
        finished_at: Optional[datetime] = None,
        total_files: Optional[int] = None,
        new_files: Optional[int] = None,
        removed_files: Optional[int] = None,
        error: Optional[str] = None,
    ) -> bool:
        """
        Update the sync-log row identified by (connector_id, seq) with the terminal
        status, finished_at, and optional file counts / error message.

        Returns True on success, False if the row was not found.
        """
        try:
            with get_db_session() as session:
                now = finished_at or datetime.now(timezone.utc)
                log_values: Dict[str, Any] = {
                    "status": status,
                    "finished_at": now,
                }
                if total_files is not None:
                    log_values["total_files"] = total_files
                if new_files is not None:
                    log_values["new_files"] = new_files
                if removed_files is not None:
                    log_values["removed_files"] = removed_files
                if error is not None:
                    log_values["error"] = error
                log_stmt = (
                    update(ConnectorSyncLog)
                    .where(
                        ConnectorSyncLog.connector_id == connector_id,
                        ConnectorSyncLog.seq == seq,
                    )
                    .values(**log_values)
                )
                result = session.execute(log_stmt)
                if result.rowcount == 0:
                    logger.warning(
                        f"Sync log connector={connector_id!r} seq={seq} not found for update"
                    )
                    return False
                return True
        except SQLAlchemyError as e:
            logger.error(
                f"DB error in finalize_sync_log(connector={connector_id!r}, seq={seq}): {e}",
                exc_info=True,
            )
            return False

    @staticmethod
    def update_connector_after_sync(
        connector_id: str,
        status: str,
        last_sync_at: Optional[datetime] = None,
        error: Optional[str] = None,
    ) -> None:
        """
        Update last_sync_at, sync_status, and error on the connector row after a sync run.

        CANCELLED/FAILED both map to OUT_OF_SYNC so the scheduler can retry;
        any other status (e.g. COMPLETED) is written through verbatim.

        ``error`` is written when provided (failure/cancel paths); it is cleared
        to NULL on a successful completion so a past error does not persist.
        """
        try:
            with get_db_session() as session:
                connector_sync_status = (
                    ConnectorStatus.OUT_OF_SYNC
                    if status in (SyncLogStatus.CANCELLED, SyncLogStatus.FAILED)
                    else status
                )
                values: Dict[str, Any] = {
                    "last_sync_at": last_sync_at or datetime.now(timezone.utc),
                    "sync_status": connector_sync_status,
                }
                if status == SyncLogStatus.COMPLETED:
                    values["error"] = None
                elif error is not None:
                    values["error"] = error
                session.execute(
                    update(Connector)
                    .where(Connector.id == connector_id)
                    .values(**values)
                )
        except SQLAlchemyError as e:
            logger.error(
                f"DB error in update_connector_after_sync({connector_id!r}): {e}",
                exc_info=True,
            )
            raise

    @staticmethod
    def update_sync_log_progress(
        connector_id: str,
        seq: int,
        total_files: Optional[int] = None,
        new_files: Optional[int] = None,
        removed_files: Optional[int] = None,
    ) -> bool:
        """
        Write live progress counters into an in-progress sync-log row.

        Only non-None counters are updated. Returns True on success, False if
        the row was not found.
        """
        try:
            with get_db_session() as session:
                values: Dict[str, Any] = {}
                if total_files is not None:
                    values["total_files"] = total_files
                if new_files is not None:
                    values["new_files"] = new_files
                if removed_files is not None:
                    values["removed_files"] = removed_files
                if not values:
                    return True
                stmt = (
                    update(ConnectorSyncLog)
                    .where(
                        ConnectorSyncLog.connector_id == connector_id,
                        ConnectorSyncLog.seq == seq,
                    )
                    .values(**values)
                )
                result = session.execute(stmt)
                updated = result.rowcount > 0
                if not updated:
                    logger.warning(
                        f"Sync log connector={connector_id!r} seq={seq} not found for progress update"
                    )
                return updated
        except SQLAlchemyError as e:
            logger.error(
                f"DB error in update_sync_log_progress(connector={connector_id!r}, seq={seq}): {e}",
                exc_info=True,
            )
            return False

    @staticmethod
    def get_sync_log_status(connector_id: str, seq: int) -> Optional[str]:
        """
        Return the status of a specific sync-log row identified by (connector_id, seq).

        Returns None if the row does not exist.
        """
        try:
            with get_db_session() as session:
                stmt = (
                    select(ConnectorSyncLog.status)
                    .where(
                        ConnectorSyncLog.connector_id == connector_id,
                        ConnectorSyncLog.seq == seq,
                    )
                )
                row = session.execute(stmt).one_or_none()
                return row[0] if row else None
        except SQLAlchemyError as e:
            logger.error(
                f"DB error in get_sync_log_status(connector={connector_id!r}, seq={seq}): {e}",
                exc_info=True,
            )
            return None

    @staticmethod
    def get_sync_log(connector_id: str, seq: int) -> Optional[ConnectorSyncLog]:
        """Return a specific sync-log row identified by (connector_id, seq)."""
        try:
            with get_db_session() as session:
                stmt = select(ConnectorSyncLog).where(
                    ConnectorSyncLog.connector_id == connector_id,
                    ConnectorSyncLog.seq == seq,
                )
                row = session.scalars(stmt).one_or_none()
                if row is None:
                    return None
                _ = (
                    row.connector_id,
                    row.seq,
                    row.started_at,
                    row.finished_at,
                    row.total_files,
                    row.new_files,
                    row.removed_files,
                    row.status,
                    row.error,
                )
                session.expunge(row)
                return row
        except SQLAlchemyError as e:
            logger.error(
                f"DB error in get_sync_log(connector={connector_id!r}, seq={seq}): {e}",
                exc_info=True,
            )
            return None

    @staticmethod
    def count_sync_logs(connector_id: str) -> int:
        """Return the total number of sync-log rows for the given connector."""
        try:
            with get_db_session() as session:
                base = select(ConnectorSyncLog).where(
                    ConnectorSyncLog.connector_id == connector_id
                )
                return session.execute(
                    select(func.count()).select_from(base.subquery())
                ).scalar() or 0
        except SQLAlchemyError as e:
            logger.error(f"DB error in count_sync_logs({connector_id}): {e}", exc_info=True)
            return 0

    @staticmethod
    def get_sync_logs(
        connector_id: str,
        limit: int = 50,
        offset: int = 0,
    ) -> List[ConnectorSyncLog]:
        """
        Return paginated sync-log rows for a connector, newest first.
        """
        try:
            with get_db_session() as session:
                stmt = (
                    select(ConnectorSyncLog)
                    .where(ConnectorSyncLog.connector_id == connector_id)
                    .order_by(ConnectorSyncLog.started_at.desc())
                    .limit(limit)
                    .offset(offset)
                )
                rows = list(session.scalars(stmt).all())
                for row in rows:
                    _ = (
                        row.connector_id, row.seq,
                        row.started_at, row.finished_at,
                        row.total_files, row.new_files, row.removed_files,
                        row.status, row.error,
                    )
                    session.expunge(row)
                logger.debug(
                    f"get_sync_logs: connector={connector_id!r} returned {len(rows)}"
                )
                return rows
        except SQLAlchemyError as e:
            logger.error(f"DB error in get_sync_logs({connector_id}): {e}", exc_info=True)
            return []

    # ========================================================================
    # Document metadata helper
    # ========================================================================

    @staticmethod
    def merge_document_metadata(doc_id: str, metadata: dict) -> bool:
        """
        Merge *metadata* into documents.doc_metadata using the PostgreSQL
        ``||`` JSONB concatenation operator (atomic key-level merge).

        Returns True on success, False if the document row was not found.
        """
        try:
            with get_db_session() as session:
                stmt = (
                    update(Document)
                    .where(Document.doc_id == doc_id)
                    .values(
                        doc_metadata=Document.doc_metadata.op("||")(
                            func.cast(metadata, Document.doc_metadata.type)
                        )
                    )
                )
                result = session.execute(stmt)
                updated = result.rowcount > 0
                if not updated:
                    logger.warning(f"merge_document_metadata: doc_id={doc_id!r} not found")
                return updated
        except SQLAlchemyError as e:
            logger.error(f"DB error in merge_document_metadata({doc_id}): {e}", exc_info=True)
            return False

    # ------------------------------------------------------------------
    # ConversionTask operations
    # ------------------------------------------------------------------

    @staticmethod
    def create_conversion_task(
        task_id: str,
        job_id: str,
        doc_id: str,
        operation: str,
        cached_file: str,
        output_format: str,
        page_count: int,
        is_large: bool,
        status: str = "queued",
    ) -> Optional[ConversionTask]:
        """
        Insert a single conversion_tasks row.

        Args:
            task_id:       Unique task identifier.
            job_id:        Parent job identifier.
            doc_id:        Document identifier (informational).
            operation:     'ingestion' | 'digitization'.
            cached_file:   Absolute path to the staged input file.
            output_format: 'json' | 'md' | 'txt'.
            page_count:    Document page count (0 for DOCX).
            is_large:      True when page_count >= heavy_doc_page_threshold.
            status:        Initial status — 'queued' or 'pending'.

        Returns:
            The created ConversionTask or None on failure.
        """
        try:
            with get_db_session() as session:
                task = ConversionTask(
                    task_id=task_id,
                    job_id=job_id,
                    doc_id=doc_id,
                    operation=operation,
                    cached_file=cached_file,
                    output_format=output_format,
                    page_count=page_count,
                    is_large=is_large,
                    status=status,
                    queued_at=datetime.now(timezone.utc),
                )
                session.add(task)
                session.flush()
                logger.debug(f"Created conversion task {task_id} (status={status})")
                return task
        except IntegrityError as e:
            logger.error(f"ConversionTask {task_id} already exists: {e}")
            return None
        except SQLAlchemyError as e:
            logger.error(f"DB error creating conversion task {task_id}: {e}", exc_info=True)
            return None
        except Exception as e:
            logger.error(f"Unexpected error creating conversion task {task_id}: {e}", exc_info=True)
            return None

    @staticmethod
    def create_conversion_tasks_batch(tasks: list[dict]) -> None:
        """
        Insert multiple conversion_tasks rows in a single transaction.

        All rows are inserted atomically — if any row fails the entire
        batch is rolled back and no tasks are created.  Use this instead
        of repeated ``create_conversion_task`` calls when enqueueing a
        multi-file job to reduce DB round-trips to one.

        Args:
            tasks: Sequence of dicts, each with the same keys accepted by
                   ``create_conversion_task`` (task_id, job_id, doc_id,
                   operation, cached_file, output_format, page_count,
                   is_large, status).

        Raises:
            SQLAlchemyError: propagated on failure so the caller can treat
                the entire enqueue as failed (no partial inserts).
        """
        if not tasks:
            return
        now = datetime.now(timezone.utc)
        with get_db_session() as session:
            session.add_all([
                ConversionTask(
                    task_id=t["task_id"],
                    job_id=t["job_id"],
                    doc_id=t["doc_id"],
                    operation=t["operation"],
                    cached_file=t["cached_file"],
                    output_format=t["output_format"],
                    page_count=t["page_count"],
                    is_large=t["is_large"],
                    status=t["status"],
                    queued_at=now,
                )
                for t in tasks
            ])
            session.flush()
            logger.debug(
                f"Batch-inserted {len(tasks)} conversion task(s) "
                f"(job_id={tasks[0]['job_id']!r})"
            )

    @staticmethod
    def _load_and_expunge_task(task: ConversionTask, session) -> None:
        """
        Eagerly load all columns of ``task`` then detach it from ``session``.

        SQLAlchemy lazy-loads attributes after a session closes, which raises
        ``DetachedInstanceError``.  Accessing every column as a tuple forces
        the ORM to populate them while the session is still open; ``expunge``
        then removes the object from the identity map so callers can use it
        freely after the ``with get_db_session()`` block exits.
        """
        _ = (task.task_id, task.job_id, task.doc_id, task.operation,
             task.cached_file, task.output_format, task.page_count,
             task.is_large, task.status, task.result_path, task.error,
             task.queued_at, task.started_at, task.completed_at)
        session.expunge(task)

    @staticmethod
    def get_conversion_task(task_id: str) -> Optional[ConversionTask]:
        """Return the ConversionTask with the given task_id, or None."""
        try:
            with get_db_session() as session:
                stmt = select(ConversionTask).where(ConversionTask.task_id == task_id)
                task = session.scalar(stmt)
                if task:
                    DatabaseManager._load_and_expunge_task(task, session)
                return task
        except SQLAlchemyError as e:
            logger.error(f"DB error retrieving task {task_id}: {e}", exc_info=True)
            return None

    @staticmethod
    def get_conversion_task_by_job_id(job_id: str) -> Optional[ConversionTask]:
        """
        Return the single ConversionTask for a digitization job.

        Used by the digitize pipeline to poll task status.
        """
        try:
            with get_db_session() as session:
                stmt = (
                    select(ConversionTask)
                    .where(ConversionTask.job_id == job_id)
                    .order_by(ConversionTask.queued_at)
                    .limit(1)
                )
                task = session.scalar(stmt)
                if task:
                    DatabaseManager._load_and_expunge_task(task, session)
                return task
        except SQLAlchemyError as e:
            logger.error(f"DB error retrieving task for job {job_id}: {e}", exc_info=True)
            return None

    @staticmethod
    def get_conversion_tasks_by_job_id(job_id: str) -> List[ConversionTask]:
        """Return all ConversionTask rows for a job, ordered by queued_at."""
        try:
            with get_db_session() as session:
                stmt = (
                    select(ConversionTask)
                    .where(ConversionTask.job_id == job_id)
                    .order_by(ConversionTask.queued_at)
                )
                tasks = list(session.scalars(stmt).all())
                for t in tasks:
                    DatabaseManager._load_and_expunge_task(t, session)
                return tasks
        except SQLAlchemyError as e:
            logger.error(f"DB error retrieving tasks for job {job_id}: {e}", exc_info=True)
            return []

    @staticmethod
    def get_conversion_tasks(status: str) -> List[ConversionTask]:
        """Return all ConversionTask rows with the given status."""
        try:
            with get_db_session() as session:
                stmt = (
                    select(ConversionTask)
                    .where(ConversionTask.status == status)
                    .order_by(ConversionTask.queued_at)
                )
                tasks = list(session.scalars(stmt).all())
                for t in tasks:
                    DatabaseManager._load_and_expunge_task(t, session)
                return tasks
        except SQLAlchemyError as e:
            logger.error(f"DB error retrieving tasks (status={status}): {e}", exc_info=True)
            return []

    @staticmethod
    def get_queued_count(operation: str) -> int:
        """Return the number of tasks currently in 'queued' status for ``operation``."""
        try:
            with get_db_session() as session:
                stmt = select(func.count()).where(
                    ConversionTask.status == "queued",
                    ConversionTask.operation == operation,
                )
                return session.scalar(stmt) or 0
        except SQLAlchemyError as e:
            logger.error(f"DB error counting queued tasks for {operation}: {e}", exc_info=True)
            return 0

    @staticmethod
    def get_queued_counts() -> Dict[str, int]:
        """
        Return the number of 'queued' tasks for both operation types in a single query.

        Non-locking read — used for logging/metrics only.
        For admission decisions use check_quota_atomic() instead.
        """
        try:
            with get_db_session() as session:
                stmt = (
                    select(ConversionTask.operation, func.count())
                    .where(ConversionTask.status == "queued")
                    .group_by(ConversionTask.operation)
                )
                rows = session.execute(stmt).all()
                counts: Dict[str, int] = {"ingestion": 0, "digitization": 0}
                for op, cnt in rows:
                    counts[op] = cnt
                return counts
        except SQLAlchemyError as e:
            logger.error(f"DB error counting queued tasks: {e}", exc_info=True)
            return {"ingestion": 0, "digitization": 0}

    @staticmethod
    def check_quota_atomic(operation: str, quota: int) -> tuple[bool, int]:
        """
        Atomically check whether at least one free queue slot exists for
        ``operation`` by counting tasks with ``status='queued'`` under a
        session-level advisory lock to prevent concurrent over-admission races.

        The quota guards the **queue** (``queued`` status only).  Running tasks
        have already left the queue and freed their slot — a new submission may
        legitimately take that freed slot.  The semaphore is the hard
        execution-time resource limit managed exclusively by the dispatcher;
        it is never consulted at admission time.

        Because PostgreSQL does not support SELECT FOR UPDATE with aggregate
        functions, we serialise concurrent POST /v1/jobs calls via a
        session-level advisory lock keyed on the operation string hash so that
        two concurrent requests cannot both read ``queued=9 < 10`` and both
        proceed.

        Args:
            operation: ``"ingestion"`` or ``"digitization"``.
            quota:     Maximum allowed queued tasks for this operation.

        Returns:
            Tuple of (quota_ok: bool, current_queued_count: int).
            quota_ok is True when ``current_queued_count < quota``.
        """
        from sqlalchemy import text

        # Numeric key for the advisory lock: hash operation string to an int32
        lock_key = hash(operation) & 0x7FFFFFFF

        try:
            with get_db_session() as session:
                # Acquire a session-level advisory lock so concurrent requests
                # for the same operation type are serialised through this block.
                session.execute(text(f"SELECT pg_advisory_xact_lock({lock_key})"))

                queued = session.scalar(
                    select(func.count()).where(
                        ConversionTask.status == "queued",
                        ConversionTask.operation == operation,
                    )
                ) or 0

                return queued < quota, queued
        except SQLAlchemyError as e:
            logger.error(
                f"DB error in check_quota_atomic({operation}, {quota}): {e}",
                exc_info=True,
            )
            # Fail-open: return 0 so the gate passes if we can't count.
            # The dispatcher's semaphore is the hard execution-time limit.
            return True, 0

    @staticmethod
    def update_task_status(
        task_id: str,
        status: str,
        result_path: Optional[str] = None,
        error: Optional[str] = None,
    ) -> bool:
        """
        Update the status (and optional result/error) of a ConversionTask.

        Args:
            task_id:     Task identifier.
            status:      New status string.
            result_path: Path to the output file (set on completion).
            error:       Error message (set on failure).

        Returns:
            True on success, False otherwise.
        """
        try:
            with get_db_session() as session:
                now = datetime.now(timezone.utc)
                updates: Dict[str, Any] = {
                    "status": status,
                }
                if status == "running":
                    updates["started_at"] = now
                if status in ("completed", "failed"):
                    updates["completed_at"] = now
                if result_path is not None:
                    updates["result_path"] = result_path
                if error is not None:
                    updates["error"] = error

                stmt = (
                    update(ConversionTask)
                    .where(ConversionTask.task_id == task_id)
                    .values(**updates)
                )
                result = cast(CursorResult, session.execute(stmt))
                return result.rowcount > 0
        except SQLAlchemyError as e:
            logger.error(f"DB error updating task {task_id}: {e}", exc_info=True)
            return False

    @staticmethod
    def peek_head(operation: str) -> Optional[ConversionTask]:
        """
        Return the oldest 'queued' task for ``operation`` without locking.

        Used by the dispatcher to inspect the head weight before attempting
        an atomic claim.
        """
        try:
            with get_db_session() as session:
                stmt = (
                    select(ConversionTask)
                    .where(
                        ConversionTask.status == "queued",
                        ConversionTask.operation == operation,
                    )
                    .order_by(ConversionTask.queued_at)
                    .limit(1)
                )
                task = session.scalar(stmt)
                if task:
                    DatabaseManager._load_and_expunge_task(task, session)
                return task
        except SQLAlchemyError as e:
            logger.error(f"DB error peeking head for {operation}: {e}", exc_info=True)
            return None

    @staticmethod
    def claim_head(operation: str) -> Optional[ConversionTask]:
        """
        Atomically promote the oldest 'queued' task for ``operation`` to 'running'.

        Uses SELECT … FOR UPDATE SKIP LOCKED so concurrent callers never
        claim the same task.

        Returns:
            The claimed ConversionTask (status already set to 'running'), or None
            if no task was available.
        """
        try:
            with get_db_session() as session:
                # Subquery: find the head task_id under a row-level lock
                subq = (
                    select(ConversionTask.task_id)
                    .where(
                        ConversionTask.status == "queued",
                        ConversionTask.operation == operation,
                    )
                    .order_by(ConversionTask.queued_at)
                    .limit(1)
                    .with_for_update(skip_locked=True)
                    .scalar_subquery()
                )
                now = datetime.now(timezone.utc)
                stmt = (
                    update(ConversionTask)
                    .where(ConversionTask.task_id == subq)
                    .values(
                        status="running",
                        started_at=now,
                    )
                    .returning(ConversionTask)
                )
                result = session.execute(stmt)
                row = result.fetchone()
                if row is None:
                    return None
                # row[0] is the ORM object returned by RETURNING *
                task = row[0]
                DatabaseManager._load_and_expunge_task(task, session)
                return task
        except SQLAlchemyError as e:
            logger.error(f"DB error claiming head for {operation}: {e}", exc_info=True)
            return None

    @staticmethod
    def promote_pending(operation: str, quota: int) -> int:
        """
        Promote as many 'pending' tasks as will fit under ``quota`` for ``operation``.

        This keeps the 'queued' count ≤ quota while draining the pending backlog
        in first-submitted-first-promoted order.

        All three statements (count queued, select candidates, update status) run
        inside a single transaction under the same session-level advisory lock used
        by ``check_quota_atomic``.  This prevents two concurrent callers — e.g. the
        dispatcher's promote tick and a concurrent admission check — from both
        reading a stale queued_count and over-promoting beyond the quota.

        The lock key is identical to ``check_quota_atomic`` (``hash(operation) &
        0x7FFFFFFF``) so that admission and promotion on the same operation type are
        mutually serialised even across different connections.

        Returns:
            Number of tasks promoted.
        """
        from sqlalchemy import text

        # Same key as check_quota_atomic — serialises admission and promotion
        # for the same operation type across all connections.
        lock_key = hash(operation) & 0x7FFFFFFF

        try:
            with get_db_session() as session:
                # Hold the advisory lock for the entire count → select → update
                # sequence so no concurrent caller can interleave.
                session.execute(text(f"SELECT pg_advisory_xact_lock({lock_key})"))

                # Count currently queued tasks for this operation.
                queued_count = session.scalar(
                    select(func.count()).where(
                        ConversionTask.status == "queued",
                        ConversionTask.operation == operation,
                    )
                ) or 0

                headroom = max(0, quota - queued_count)
                if headroom == 0:
                    return 0

                # Fetch the oldest pending tasks that fit under the quota.
                candidates = session.execute(
                    select(ConversionTask.task_id, ConversionTask.cached_file)
                    .where(
                        ConversionTask.status == "pending",
                        ConversionTask.operation == operation,
                    )
                    .order_by(ConversionTask.queued_at)
                    .limit(headroom)
                ).all()

                if not candidates:
                    return 0

                candidate_ids = [row.task_id for row in candidates]
                now = datetime.now(timezone.utc)
                stmt = (
                    update(ConversionTask)
                    .where(ConversionTask.task_id.in_(candidate_ids))
                    .values(status="queued", queued_at=now)
                )
                result = cast(CursorResult, session.execute(stmt))
                promoted = result.rowcount
                if promoted:
                    logger.debug(f"Promoted {promoted} pending → queued for {operation}")
                    for row in candidates:
                        logger.debug(
                            f"  [{operation}] queued: {Path(row.cached_file).name} (task_id={row.task_id})"
                        )
                return promoted
        except SQLAlchemyError as e:
            logger.error(f"DB error promoting pending tasks for {operation}: {e}", exc_info=True)
            return 0


    @staticmethod
    def reset_syncing_connectors(
        error: str = "Service restarted during sync tick",
    ) -> List[str]:
        """
        Bulk-reset all connectors stuck in ``'syncing'`` to ``'out of sync'``.

        Called on startup to unlock connectors that were mid-tick when the
        service crashed.  Returns the list of connector IDs that were reset.

        Also stamps ``error`` on every affected row so that callers can see the
        crash reason without joining to sync-log rows.
        """
        try:
            with get_db_session() as session:
                result = session.execute(
                    update(Connector)
                    .where(Connector.sync_status == ConnectorStatus.SYNCING)
                    .values(
                        sync_status=ConnectorStatus.OUT_OF_SYNC,
                        error=error,
                    )
                    .returning(Connector.id)
                )
                affected = [row[0] for row in result.fetchall()]
                if affected:
                    logger.info(
                        f"reset_syncing_connectors: reset {len(affected)} connector(s) "
                        f"from syncing to out-of-sync: {affected}"
                    )
                return affected
        except SQLAlchemyError as e:
            logger.error(f"DB error in reset_syncing_connectors: {e}", exc_info=True)
            return []

    @staticmethod
    def close_open_sync_log(connector_id: str, error: str) -> bool:
        """
        Close the open (status=STARTED or CANCEL_PENDING) sync-log row for
        *connector_id* by setting its status to FAILED with *error* and
        stamping ``finished_at = now()``.

        Returns True if a row was updated, False if none was found.
        """
        try:
            with get_db_session() as session:
                result = session.execute(
                    update(ConnectorSyncLog)
                    .where(
                        ConnectorSyncLog.connector_id == connector_id,
                        ConnectorSyncLog.status.in_(
                            [SyncLogStatus.STARTED, SyncLogStatus.CANCEL_PENDING]
                        ),
                    )
                    .values(
                        status=SyncLogStatus.FAILED,
                        finished_at=datetime.now(timezone.utc),
                        error=error,
                    )
                    .returning(ConnectorSyncLog.seq)
                ).one_or_none()
                return result is not None
        except SQLAlchemyError as e:
            logger.error(
                f"DB error in close_open_sync_log({connector_id!r}): {e}",
                exc_info=True,
            )
            return False


# Singleton instance for easy access
db_manager = DatabaseManager()

# Made with Bob
