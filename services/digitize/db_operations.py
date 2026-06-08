"""
Database operations module for digitize service.

This module contains all functions that directly interact with the database
using db_manager. It provides a clean separation between database operations
and other utility functions.
"""

from datetime import datetime, timezone
from time import perf_counter
from typing import List, Optional, Dict, Any, Mapping

from common.misc_utils import get_logger
from digitize.models import (
    OutputFormat,
    DocumentDetailResponse,
    JobStatus,
    JobState,
    DocStatus,
    JobDocumentSummary,
    JobStats,
    ExportJobRecord,
    ExportDocumentRecord,
    ImportRequest,
    ImportResponse,
    ImportSummary,
    ImportEntitySummary,
    ImportRecordIssue,
    ExportResponse,
    ExportSummary,
    ExportEntitySummary,
    ExportPagination,
    ImportExportData,
)
from digitize.db.manager import db_manager
from digitize.db.connection import engine

logger = get_logger("db_operations")

# ============================================================================
# Import/Export Lock Management
# ============================================================================

# Store active lock connection to keep it alive
_import_export_lock_connection = None

async def is_import_export_in_progress() -> bool:
    """
    Check if an import/export operation is currently in progress.
    Returns True if locked, False if available.
    """
    import asyncio

    def _sync_check():
        from digitize.db.connection import get_db_session
        from sqlalchemy import text

        try:
            with get_db_session() as session:
                # Check if the advisory lock is currently held
                # pg_try_advisory_lock returns true if lock acquired, false if already held
                # We immediately release it if we acquired it (just checking status)
                result = session.execute(text("SELECT pg_try_advisory_lock(123456789)")).scalar()
                if result:
                    # We acquired it, so it wasn't in use - release it immediately
                    session.execute(text("SELECT pg_advisory_unlock(123456789)"))
                    return False
                else:
                    # Lock is held by another process
                    return True
        except Exception as e:
            logger.error(f"Failed to check import/export lock status: {e}")
            # On error, assume it's not in progress to avoid blocking operations
            return False

    return await asyncio.to_thread(_sync_check)

async def acquire_import_export_lock() -> bool:
    """
    Acquire a distributed lock for import/export operations using PostgreSQL advisory locks.
    Returns True if lock was acquired, False if another process holds the lock.
    IMPORTANT: Keeps the database connection open to maintain the lock.
    Runs in thread pool to avoid blocking the async event loop.
    """
    import asyncio

    def _sync_acquire():
        global _import_export_lock_connection
        from digitize.db.connection import engine
        from sqlalchemy import text
        import os

        pid = os.getpid()
        logger.info(f"[PID {pid}] Attempting to acquire import/export lock...")

        if not engine:
            logger.error(f"[PID {pid}] Database engine not initialized")
            return False

        conn = None
        try:
            # Create a new connection that we'll keep open
            conn = engine.connect()
            logger.debug(f"[PID {pid}] Database connection created")

            # Try to acquire PostgreSQL advisory lock (non-blocking)
            # Lock ID: 123456789 (arbitrary unique number for import/export operations)
            result = conn.execute(text("SELECT pg_try_advisory_lock(123456789)")).scalar()
            logger.debug(f"[PID {pid}] Lock acquisition result: {result}")

            if result:
                # Lock acquired - store connection to keep it alive
                _import_export_lock_connection = conn
                logger.warning(f"[PID {pid}] ✅ Import/export lock ACQUIRED successfully")
                return True
            else:
                # Lock not acquired - close connection
                conn.close()
                logger.warning(f"[PID {pid}] ❌ Import/export lock ALREADY HELD by another process")
                return False
        except Exception as e:
            logger.error(f"[PID {pid}] Failed to acquire import/export lock: {e}", exc_info=True)
            if conn:
                try:
                    conn.close()
                except:
                    pass
            return False

    return await asyncio.to_thread(_sync_acquire)

async def release_import_export_lock():
    """Release the distributed import/export lock and close the connection."""
    import asyncio

    def _sync_release():
        global _import_export_lock_connection
        from sqlalchemy import text
        import os

        pid = os.getpid()
        logger.info(f"[PID {pid}] Releasing import/export lock...")

        try:
            if _import_export_lock_connection:
                # Release PostgreSQL advisory lock
                _import_export_lock_connection.execute(text("SELECT pg_advisory_unlock(123456789)"))
                _import_export_lock_connection.close()
                _import_export_lock_connection = None
                logger.warning(f"[PID {pid}] ✅ Import/export lock RELEASED successfully")
            else:
                logger.warning(f"[PID {pid}] No lock connection to release")
        except Exception as e:
            logger.error(f"[PID {pid}] Failed to release import/export lock: {e}", exc_info=True)
            # Ensure connection is closed even on error
            if _import_export_lock_connection:
                try:
                    _import_export_lock_connection.close()
                except:
                    pass
                _import_export_lock_connection = None

    await asyncio.to_thread(_sync_release)


# ============================================================================
# Job Operations
# ============================================================================

def create_job(
    job_id: str,
    operation: str,
    submitted_at: str,
    documents_info: list[str],
    job_name: Optional[str] = None
) -> None:
    """
    Create job in database.

    Args:
        job_id: Unique identifier for the job
        operation: Type of operation (ingestion/digitization)
        submitted_at: ISO timestamp when job was submitted
        documents_info: List of document filenames
        job_name: Optional human-readable name for the job
    """
    if engine is None:
        raise RuntimeError("Database not available. Cannot create job without database connection.")

    try:
        # Parse ISO timestamp to datetime
        submitted_dt = datetime.fromisoformat(submitted_at.replace("Z", "+00:00"))

        # Create job in database
        db_manager.create_job(
            job_id=job_id,
            operation=operation,
            status=JobStatus.ACCEPTED,
            job_name=job_name,
            submitted_at=submitted_dt,
            stats={
                "total_documents": len(documents_info),
                "completed": 0,
                "failed": 0,
                "in_progress": 0
            }
        )
        logger.info(f"Created job {job_id} in database")

    except Exception as e:
        logger.error(f"Failed to create job {job_id} in database: {e}", exc_info=True)
        raise


def get_job(job_id: str) -> Optional[dict]:
    """
    Get job data from database.

    Args:
        job_id: Unique identifier for the job

    Returns:
        Job data dictionary or None if not found
    """
    # Database is the primary and only source
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve job without database connection.")

    try:
        job = db_manager.get_job_by_id(job_id)
        if job:
            # Get documents for this job
            documents = db_manager.get_documents_by_job_id(job_id)
            doc_summaries = [
                JobDocumentSummary(
                    id=doc.doc_id,
                    name=doc.name,
                    status=doc.status
                )
                for doc in documents
            ]

            # Create JobState object
            job_state = JobState(
                job_id=job.job_id,
                job_name=job.job_name,
                operation=job.operation,
                status=JobStatus(job.status),
                submitted_at=job.submitted_at.isoformat().replace("+00:00", "Z"),
                completed_at=job.completed_at.isoformat().replace("+00:00", "Z") if job.completed_at else None,
                documents=doc_summaries,
                stats=JobStats(**job.stats),
                error=job.error
            )

            logger.debug(f"Retrieved job {job_id} from database")
            return job_state.to_dict()
        else:
            logger.debug(f"Job {job_id} not found in database")
            return None
    except Exception as e:
        logger.error(f"Failed to get job {job_id} from database: {e}", exc_info=True)
        raise


def get_all_jobs(
    status: Optional[JobStatus] = None,
    operation: Optional[str] = None,
    limit: int = 20,
    offset: int = 0
) -> tuple[List[dict], int]:
    """
    Get all jobs from database.

    Args:
        status: Filter by job status
        operation: Filter by operation type
        limit: Maximum number of jobs to return
        offset: Number of jobs to skip

    Returns:
        Tuple of (list of job dictionaries, total count)
    """
    # Database is the primary and only source
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve jobs without database connection.")

    try:
        jobs, total = db_manager.get_all_jobs(
            status=status,
            operation=operation,
            limit=limit,
            offset=offset
        )

        # Convert SQLAlchemy models to dictionaries
        job_dicts = []
        for job in jobs:
            # Get documents for this job
            documents = db_manager.get_documents_by_job_id(job.job_id)
            doc_summaries = [
                JobDocumentSummary(
                    id=doc.doc_id,
                    name=doc.name,
                    status=doc.status
                )
                for doc in documents
            ]

            # Create JobState object
            job_state = JobState(
                job_id=job.job_id,
                job_name=job.job_name,
                operation=job.operation,
                status=JobStatus(job.status),
                submitted_at=job.submitted_at.isoformat().replace("+00:00", "Z"),
                completed_at=job.completed_at.isoformat().replace("+00:00", "Z") if job.completed_at else None,
                documents=doc_summaries,
                stats=JobStats(**job.stats),
                error=job.error
            )
            job_dicts.append(job_state.to_dict())

        logger.debug(f"Retrieved {len(job_dicts)} jobs from database (total: {total})")
        return job_dicts, total
    except Exception as e:
        logger.error(f"Failed to get jobs from database: {e}", exc_info=True)
        raise


# ============================================================================
# Document Operations
# ============================================================================

def create_document(
    doc_name: str,
    doc_id: str,
    job_id: str,
    output_format: OutputFormat,
    operation: str,
    submitted_at: str
) -> None:
    """
    Create document metadata in database.

    Args:
        doc_name: Name of the document file
        doc_id: Unique identifier for the document
        job_id: ID of the parent job
        output_format: Output format for the document
        operation: Type of operation (ingestion/digitization)
        submitted_at: ISO timestamp when document was submitted
    """
    if engine is None:
        raise RuntimeError("Database not available. Cannot create document without database connection.")

    try:
        # Parse ISO timestamp to datetime
        submitted_dt = datetime.fromisoformat(submitted_at.replace("Z", "+00:00"))

        # Create document in database
        db_manager.create_document(
            doc_id=doc_id,
            name=doc_name,
            doc_type=operation,
            status=DocStatus.ACCEPTED,
            output_format=output_format.value,
            submitted_at=submitted_dt,
            job_id=job_id,
            metadata={
                "pages": 0,
                "tables": 0,
                "timing_in_secs": {
                    "digitizing": None,
                    "processing": None,
                    "chunking": None,
                    "indexing": None
                }
            }
        )
        logger.info(f"Created document {doc_id} in database")

    except Exception as e:
        logger.error(f"Failed to create document {doc_id} in database: {e}", exc_info=True)
        raise


def get_document(doc_id: str, include_details: bool = True) -> DocumentDetailResponse:
    """
    Get document data from database and return as Pydantic model.

    Args:
        doc_id: Unique identifier for the document
        include_details: If True, includes metadata fields; if False, excludes them

    Returns:
        DocumentDetailResponse model with document information

    Raises:
        FileNotFoundError: If document doesn't exist in database
        RuntimeError: If database is not available
    """
    logger.debug(f"Fetching document {doc_id} with include_details={include_details}")

    # Database is the primary and only source
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve document without database connection.")

    try:
        doc = db_manager.get_document_by_id(doc_id)
        if doc:
            # Convert SQLAlchemy model to dictionary
            doc_dict = {
                "id": doc.doc_id,
                "job_id": doc.job_id,
                "name": doc.name,
                "type": doc.type,
                "status": doc.status,
                "output_format": doc.output_format,
                "submitted_at": doc.submitted_at.isoformat().replace("+00:00", "Z"),
                "completed_at": doc.completed_at.isoformat().replace("+00:00", "Z") if doc.completed_at else None,
                "error": doc.error,
                "metadata": doc.doc_metadata
            }

            # Conditionally exclude metadata if not requested
            if not include_details:
                doc_dict.pop('metadata', None)

            # Let Pydantic validate and convert the data
            response = DocumentDetailResponse(**doc_dict)
            logger.debug(f"Successfully retrieved document {doc_id}")
            return response
        else:
            logger.debug(f"Document {doc_id} not found in database")
            raise FileNotFoundError(f"Document with ID '{doc_id}' not found")
    except FileNotFoundError:
        raise
    except Exception as e:
        logger.error(f"Failed to get document {doc_id} from database: {e}", exc_info=True)
        raise


def get_all_documents_paginated(
    status: Optional[str] = None,
    name: Optional[str] = None,
    limit: int = 20,
    offset: int = 0
) -> tuple[List[dict], int]:
    """
    Get all documents from database.

    Args:
        status: Filter by document status
        name: Filter by document name (partial match)
        limit: Maximum number of documents to return
        offset: Number of documents to skip

    Returns:
        Tuple of (list of document dictionaries, total count)
    """
    # Database is the primary and only source
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve documents without database connection.")

    try:
        documents, total = db_manager.get_all_documents(
            status=status,
            name=name,
            limit=limit,
            offset=offset
        )

        # Convert SQLAlchemy models to dictionaries
        doc_dicts = [
            {
                "id": doc.doc_id,
                "name": doc.name,
                "type": doc.type,
                "status": doc.status,
                "submitted_at": doc.submitted_at.isoformat().replace("+00:00", "Z")
            }
            for doc in documents
        ]
        logger.debug(f"Retrieved {len(doc_dicts)} documents from database (total: {total})")
        return doc_dicts, total
    except Exception as e:
        logger.error(f"Failed to get documents from database: {e}", exc_info=True)
        raise


def get_all_job_ids() -> list[str]:
    """
    Read all job IDs from the database.

    Returns:
        List of job IDs found in database
    """
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve job IDs without database connection.")

    try:
        logger.debug("Reading job IDs from database")
        all_jobs = []
        offset = 0

        while True:
            jobs, total = db_manager.get_all_jobs(limit=IMPORT_EXPORT_DEFAULT_LIMIT, offset=offset)
            if not jobs:
                break
            all_jobs.extend(jobs)
            offset += len(jobs)
            if offset >= total:
                break

        job_ids = [job.job_id for job in all_jobs]
        logger.info(f"Found {len(job_ids)} job IDs in database")
        return job_ids
    except Exception as e:
        logger.error(f"Failed to read job IDs from database: {e}", exc_info=True)
        raise


def get_all_document_ids() -> list[str]:
    """
    Read all document IDs from the database.

    Returns:
        List of document IDs found in database
    """
    if engine is None:
        raise RuntimeError("Database not available. Cannot retrieve document IDs without database connection.")

    try:
        logger.debug("Reading document IDs from database")
        all_documents = []
        offset = 0

        while True:
            documents, total = db_manager.get_all_documents(limit=IMPORT_EXPORT_DEFAULT_LIMIT, offset=offset)
            if not documents:
                break
            all_documents.extend(documents)
            offset += len(documents)
            if offset >= total:
                break

        doc_ids = [doc.doc_id for doc in all_documents]
        logger.info(f"Found {len(doc_ids)} document IDs in database")
        return doc_ids
    except Exception as e:
        logger.error(f"Failed to read document IDs from database: {e}", exc_info=True)
        raise


IMPORT_EXPORT_DEFAULT_LIMIT = 10000
IMPORT_EXPORT_BATCH_SIZE = 100

def _parse_iso_datetime(timestamp: Optional[str]) -> Optional[datetime]:
    """Parse ISO-8601 timestamps with optional Z suffix."""
    if not timestamp:
        return None
    return datetime.fromisoformat(timestamp.replace("Z", "+00:00"))


def _serialize_datetime(timestamp: Optional[datetime]) -> Optional[str]:
    """Serialize datetime to ISO-8601 string with Z suffix."""
    if timestamp is None:
        return None
    return timestamp.isoformat().replace("+00:00", "Z")


def _build_import_summary(total_jobs: int, total_documents: int) -> ImportSummary:
    """Create an initialized import summary object."""
    return ImportSummary(
        jobs=ImportEntitySummary(total_received=total_jobs),
        documents=ImportEntitySummary(total_received=total_documents),
    )


def export_metadata(limit: int = IMPORT_EXPORT_DEFAULT_LIMIT, offset: int = 0) -> ExportResponse:
    """
    Export jobs and documents from PostgreSQL as JSON-serializable metadata.

    Args:
        limit: Maximum combined records to return. Use -1 to return all records.
        offset: Number of combined records to skip.

    Returns:
        ExportResponse with exported jobs/documents and pagination metadata.
    """
    if engine is None:
        raise RuntimeError("Database not available. Cannot export metadata without database connection.")

    if offset < 0:
        raise ValueError("offset cannot be negative")

    if limit == 0 or limit < -1:
        raise ValueError("limit must be -1 or a positive integer")

    started_at = perf_counter()

    if limit == -1:
        # Fetch all records in batches
        jobs, total_jobs = db_manager.get_all_jobs(limit=IMPORT_EXPORT_DEFAULT_LIMIT, offset=0)
        documents, total_documents = db_manager.get_all_documents(limit=IMPORT_EXPORT_DEFAULT_LIMIT, offset=0)

        # Fetch remaining jobs
        remaining_job_offset = len(jobs)
        while remaining_job_offset < total_jobs:
            batch, _ = db_manager.get_all_jobs(limit=IMPORT_EXPORT_DEFAULT_LIMIT, offset=remaining_job_offset)
            if not batch:
                break
            jobs.extend(batch)
            remaining_job_offset += len(batch)

        # Fetch remaining documents
        remaining_doc_offset = len(documents)
        while remaining_doc_offset < total_documents:
            batch, _ = db_manager.get_all_documents(limit=IMPORT_EXPORT_DEFAULT_LIMIT, offset=remaining_doc_offset)
            if not batch:
                break
            documents.extend(batch)
            remaining_doc_offset += len(batch)
    else:
        # Fetch records respecting the combined limit across jobs and documents
        # First, get total counts
        _, total_jobs = db_manager.get_all_jobs(limit=1, offset=0)
        _, total_documents = db_manager.get_all_documents(limit=1, offset=0)

        # Calculate how many records to skip and fetch
        total_records = total_jobs + total_documents

        # Determine which records fall within the requested range
        jobs = []
        documents = []

        if offset < total_jobs:
            # We need some jobs
            jobs_to_fetch = min(limit, total_jobs - offset)
            jobs, _ = db_manager.get_all_jobs(limit=jobs_to_fetch, offset=offset)

            # If we haven't reached the limit, fetch documents
            remaining_limit = limit - len(jobs)
            if remaining_limit > 0:
                documents, _ = db_manager.get_all_documents(limit=remaining_limit, offset=0)
        else:
            # Skip all jobs, fetch only documents
            doc_offset = offset - total_jobs
            documents, _ = db_manager.get_all_documents(limit=limit, offset=doc_offset)

    exported_jobs = [
        ExportJobRecord(
            job_id=job.job_id,
            operation=job.operation,
            status=job.status,
            job_name=job.job_name,
            submitted_at=_serialize_datetime(job.submitted_at) or "",
            completed_at=_serialize_datetime(job.completed_at),
            stats=job.stats or {},
            error=job.error,
        )
        for job in jobs
    ]

    exported_documents = [
        ExportDocumentRecord(
            id=doc.doc_id,
            job_id=doc.job_id,
            name=doc.name,
            type=doc.type,
            status=doc.status,
            output_format=doc.output_format,
            submitted_at=_serialize_datetime(doc.submitted_at) or "",
            completed_at=_serialize_datetime(doc.completed_at),
            error=doc.error,
            metadata=doc.doc_metadata or {},
        )
        for doc in documents
    ]

    returned_records = len(exported_jobs) + len(exported_documents)
    total_records = total_jobs + total_documents
    effective_limit = returned_records if limit == -1 else limit

    return ExportResponse(
        status="completed",
        data=ImportExportData(jobs=exported_jobs, documents=exported_documents),
        summary=ExportSummary(
            jobs=ExportEntitySummary(
                total_exported=len(exported_jobs),
                completed=sum(1 for job in exported_jobs if job.status == JobStatus.COMPLETED.value),
                failed=sum(1 for job in exported_jobs if job.status == JobStatus.FAILED.value),
            ),
            documents=ExportEntitySummary(
                total_exported=len(exported_documents),
                completed=sum(1 for doc in exported_documents if doc.status == DocStatus.COMPLETED.value),
                failed=sum(1 for doc in exported_documents if doc.status == DocStatus.FAILED.value),
            ),
        ),
        export_timestamp=datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        duration_seconds=round(perf_counter() - started_at, 4),
        pagination=ExportPagination(
            limit=effective_limit,
            offset=0 if limit == -1 else offset,
            has_more=False if limit == -1 else (offset + returned_records) < total_records,
            total_records=total_records,
            returned_records=returned_records,
        ),
    )


def import_metadata(payload: ImportRequest) -> ImportResponse:
    """
    Import jobs and documents into PostgreSQL in skip-existing mode.

    Args:
        payload: Import request payload.

    Returns:
        ImportResponse with import summary, warnings, and errors.
    """
    if engine is None:
        raise RuntimeError("Database not available. Cannot import metadata without database connection.")

    started_at = perf_counter()
    summary = _build_import_summary(len(payload.data.jobs), len(payload.data.documents))
    warnings: list[ImportRecordIssue] = []
    errors: list[ImportRecordIssue] = []

    existing_job_ids = set(get_all_job_ids())
    existing_document_ids = set(get_all_document_ids())

    importable_job_ids = set(existing_job_ids)

    for job_record in payload.data.jobs:
        if job_record.job_id in existing_job_ids:
            summary.jobs.skipped += 1
            continue

        # Parse and validate timestamps once, then reuse
        try:
            submitted_at = _parse_iso_datetime(job_record.submitted_at)
            completed_at = _parse_iso_datetime(job_record.completed_at)
        except ValueError as exc:
            summary.jobs.failed += 1
            errors.append(
                ImportRecordIssue(
                    record_type="job",
                    record_id=job_record.job_id,
                    type="validation_error",
                    message=f"Invalid timestamp: {exc}",
                )
            )
            continue

        if payload.validate_only:
            summary.jobs.imported += 1
            importable_job_ids.add(job_record.job_id)
            continue

        created_job = db_manager.create_job(
            job_id=job_record.job_id,
            operation=job_record.operation,
            status=JobStatus(job_record.status),
            job_name=job_record.job_name,
            submitted_at=submitted_at,
            completed_at=completed_at,
            error=job_record.error,
            stats=job_record.stats,
        )

        if created_job is None:
            summary.jobs.failed += 1
            errors.append(
                ImportRecordIssue(
                    record_type="job",
                    record_id=job_record.job_id,
                    type="database_error",
                    message="Failed to create job record",
                )
            )
            continue

        summary.jobs.imported += 1
        importable_job_ids.add(job_record.job_id)

    for document_record in payload.data.documents:
        if document_record.id in existing_document_ids:
            summary.documents.skipped += 1
            continue

        if document_record.job_id and document_record.job_id not in importable_job_ids:
            summary.documents.failed += 1
            warnings.append(
                ImportRecordIssue(
                    record_type="document",
                    record_id=document_record.id,
                    type="orphaned_document",
                    message=f"Document references non-existent job_id: {document_record.job_id}",
                )
            )
            continue

        # Parse and validate timestamps once, then reuse
        try:
            submitted_at = _parse_iso_datetime(document_record.submitted_at)
            completed_at = _parse_iso_datetime(document_record.completed_at)
        except ValueError as exc:
            summary.documents.failed += 1
            errors.append(
                ImportRecordIssue(
                    record_type="document",
                    record_id=document_record.id,
                    type="validation_error",
                    message=f"Invalid timestamp: {exc}",
                )
            )
            continue

        if payload.validate_only:
            summary.documents.imported += 1
            continue

        created_document = db_manager.create_document(
            doc_id=document_record.id,
            name=document_record.name,
            doc_type=document_record.type,
            status=DocStatus(document_record.status),
            output_format=document_record.output_format,
            submitted_at=submitted_at,
            completed_at=completed_at,
            error=document_record.error,
            job_id=document_record.job_id,
            metadata=document_record.metadata,
        )

        if created_document is None:
            summary.documents.failed += 1
            errors.append(
                ImportRecordIssue(
                    record_type="document",
                    record_id=document_record.id,
                    type="database_error",
                    message="Failed to create document record",
                )
            )
            continue

        summary.documents.imported += 1

    return ImportResponse(
        status="completed",
        summary=summary,
        duration_seconds=round(perf_counter() - started_at, 4),
        errors=errors,
        warnings=warnings,
    )


# ============================================================================
# Database Status Manager
# ============================================================================

class DatabaseStatusManager:
    """
    Database-only StatusManager that persists to PostgreSQL database.

    - Storage: PostgreSQL database only (required)
    - Raises error if database unavailable
    """

    def __init__(self, job_id: str):
        """
        Initialize database-first status manager.

        Args:
            job_id: Unique identifier for the job

        Raises:
            RuntimeError: If database is not available
        """
        self.job_id = job_id

        if engine is None:
            raise RuntimeError(f"Database not available for job {job_id}. Cannot proceed without database.")

        self.db_enabled = True

    def update_doc_metadata(
        self,
        doc_id: str,
        details: Mapping[str, Any],
        error: str = ""
    ) -> None:
        """
        Update document metadata in database.

        Args:
            doc_id: Document identifier
            details: Dictionary of fields to update
            error: Optional error message
        """
        try:
            self._update_document(doc_id, details, error)
        except Exception as e:
            logger.error(f"Failed to update document {doc_id} in database: {e}", exc_info=True)
            raise

    def update_job_progress(
        self,
        doc_id: str,
        doc_status: DocStatus,
        job_status: JobStatus,
        error: str = ""
    ) -> None:
        """
        Update job progress in database.

        Args:
            doc_id: Document identifier (empty string for job-level updates)
            doc_status: New document status
            job_status: New job status
            error: Optional error message
        """
        try:
            self._update_job(doc_id, doc_status, job_status, error)
        except Exception as e:
            logger.error(f"Failed to update job {self.job_id} in database: {e}", exc_info=True)
            raise

    def _update_document(
        self,
        doc_id: str,
        details: Mapping[str, Any],
        error: str
    ) -> None:
        """
        Update document in database.

        Args:
            doc_id: Document identifier
            details: Dictionary of fields to update
            error: Optional error message
        """
        # Separate metadata fields from top-level fields
        metadata_fields, top_level_fields = _categorize_fields(details)

        # Prepare update parameters
        update_params: Dict[str, Any] = {}

        # Handle status update
        if "status" in top_level_fields:
            status_value = top_level_fields["status"]
            try:
                update_params["status"] = DocStatus(status_value)
            except (ValueError, TypeError):
                logger.warning(f"Invalid status value: {status_value}")

        # Handle completed_at
        if "completed_at" in top_level_fields:
            completed_at_str = top_level_fields["completed_at"]
            if completed_at_str:
                try:
                    update_params["completed_at"] = datetime.fromisoformat(
                        completed_at_str.replace("Z", "+00:00")
                    )
                except (ValueError, TypeError) as e:
                    logger.warning(f"Invalid completed_at format: {completed_at_str}, {e}")

        # Handle error
        if error:
            update_params["error"] = error

        # Handle metadata updates
        if metadata_fields:
            # Get existing document to merge metadata
            existing_doc = db_manager.get_document_by_id(doc_id)
            if existing_doc:
                merged_metadata = existing_doc.doc_metadata.copy()

                # Merge timing updates
                if "timing_in_secs" in metadata_fields:
                    merged_metadata.setdefault("timing_in_secs", {})
                    merged_metadata["timing_in_secs"].update(metadata_fields["timing_in_secs"])

                # Update other metadata fields
                for key, value in metadata_fields.items():
                    if key != "timing_in_secs" and value is not None:
                        merged_metadata[key] = value

                update_params["metadata"] = merged_metadata

        # Perform database update
        if update_params:
            success = db_manager.update_document(doc_id, **update_params)
            if success:
                logger.debug(f"Updated document {doc_id} in database")
            else:
                logger.warning(f"Document {doc_id} not found in database for update")

    def _update_job(
        self,
        doc_id: str,
        doc_status: DocStatus,
        job_status: JobStatus,
        error: str
    ) -> None:
        """
        Update job and associated document in database.

        Args:
            doc_id: Document identifier (empty for job-level updates)
            doc_status: New document status
            job_status: New job status
            error: Optional error message
        """
        # Update document status if doc_id provided
        if doc_id:
            db_manager.update_document(doc_id, status=doc_status)

        # Get current job to recalculate stats
        job = db_manager.get_job_by_id(self.job_id)
        if not job:
            logger.warning(f"Job {self.job_id} not found in database")
            return

        # Get all documents for this job to recalculate stats
        documents = db_manager.get_documents_by_job_id(self.job_id)

        # Recalculate statistics
        stats = {
            "total_documents": len(documents),
            "completed": sum(1 for d in documents if d.status == DocStatus.COMPLETED.value),
            "failed": sum(1 for d in documents if d.status == DocStatus.FAILED.value),
            "in_progress": sum(
                1 for d in documents if d.status in [
                    DocStatus.IN_PROGRESS.value,
                    DocStatus.DIGITIZED.value,
                    DocStatus.PROCESSED.value,
                    DocStatus.CHUNKED.value
                ]
            )
        }

        # Prepare job update parameters
        update_params: Dict[str, Any] = {
            "status": job_status,
            "stats": stats
        }

        # Set completed_at if job is finished
        if job_status in [JobStatus.COMPLETED, JobStatus.FAILED]:
            total_docs = stats["total_documents"]
            completed_docs = stats["completed"]
            failed_docs = stats["failed"]

            if total_docs > 0 and (completed_docs + failed_docs) == total_docs:
                update_params["completed_at"] = datetime.now(timezone.utc)

        # Set error if provided
        if error and job_status == JobStatus.FAILED:
            update_params["error"] = error

        # Perform database update
        success = db_manager.update_job(self.job_id, **update_params)
        if success:
            logger.debug(f"Updated job {self.job_id} in database")
        else:
            logger.warning(f"Job {self.job_id} not found in database for update")


def get_status_manager(job_id: str) -> DatabaseStatusManager:
    """
    Factory function to get database-first status manager.

    Returns DatabaseStatusManager which requires database to be available.

    Args:
        job_id: Unique identifier for the job

    Returns:
        DatabaseStatusManager instance

    Raises:
        RuntimeError: If database is not available
    """
    return DatabaseStatusManager(job_id)


# ============================================================================
# Helper Functions
# ============================================================================

def _categorize_fields(details: Mapping[str, Any]) -> tuple[dict[str, Any], dict[str, Any]]:
    """
    Separate fields into metadata wrapper and top-level categories.

    Args:
        details: Dictionary of fields to categorize

    Returns:
        Tuple of (metadata_fields, top_level_fields)
    """
    METADATA_KEYS = {"pages", "tables", "chunks", "timing_in_secs"}

    metadata_fields = {
        k: v if k == "timing_in_secs" and isinstance(v, dict) else _extract_value(v)
        for k, v in details.items() if k in METADATA_KEYS
    }

    top_level_fields = {
        k: _extract_value(v)
        for k, v in details.items() if k not in METADATA_KEYS
    }

    return metadata_fields, top_level_fields


def _extract_value(v: Any) -> Any:
    """Extract .value from enums, return raw value otherwise."""
    return v.value if hasattr(v, "value") else v

# Made with Bob
