"""
Database repository layer for schema-registry and extract-job operations.

All public methods are static, matching the summarize service convention.
A module-level singleton ``db_repo`` is provided for convenience.
"""

from datetime import datetime, timezone
from typing import Any, Dict, List, Optional, Tuple

from sqlalchemy import delete, func, or_, select, update
from sqlalchemy.exc import IntegrityError, SQLAlchemyError

from common.misc_utils import get_logger
from extract.db.connection import get_db_session
from extract.db.models import ExtractionSchema, ExtractJob

logger = get_logger("db_manager")

# Active-job statuses — used in RESTRICT checks and zombie recovery.
_ACTIVE_STATUSES = ("accepted", "in_progress")


class DatabaseManager:
    """CRUD repository for extract schemas and jobs."""

    # ------------------------------------------------------------------
    # Schema registry
    # ------------------------------------------------------------------

    @staticmethod
    def create_schema(
        schema_id: str,
        name: str,
        json_schema: dict,
        schema_tokens: int,
        examples_tokens: int = 0,
        custom_prompt_tokens: int = 0,
        description: Optional[str] = None,
        examples: Optional[list] = None,
        custom_prompt: Optional[str] = None,
    ) -> Optional[ExtractionSchema]:
        """Insert a new schema row.  Returns the row or None on failure."""
        try:
            with get_db_session() as session:
                row = ExtractionSchema(
                    schema_id=schema_id,
                    name=name,
                    description=description,
                    json_schema=json_schema,
                    examples=examples,
                    custom_prompt=custom_prompt,
                    schema_tokens=schema_tokens,
                    examples_tokens=examples_tokens,
                    custom_prompt_tokens=custom_prompt_tokens,
                    created_at=datetime.now(timezone.utc),
                )
                session.add(row)
                session.flush()
                session.expunge(row)
                logger.info(f"Created schema in database: {schema_id} ({name!r})")
                return row
        except IntegrityError as exc:
            logger.warning(f"Schema {name!r} already exists: {exc}")
            return None
        except SQLAlchemyError as exc:
            logger.error(f"DB error creating schema {schema_id}: {exc}", exc_info=True)
            return None

    @staticmethod
    def get_schema_by_id(schema_id: str) -> Optional[ExtractionSchema]:
        """Return a schema row or None if not found."""
        try:
            with get_db_session() as session:
                row = session.scalar(
                    select(ExtractionSchema).where(ExtractionSchema.schema_id == schema_id)
                )
                if row:
                    # Eagerly load all columns before session closes.
                    _ = (
                        row.schema_id, row.name, row.description, row.json_schema,
                        row.examples, row.custom_prompt, row.schema_tokens,
                        row.examples_tokens, row.custom_prompt_tokens, row.created_at,
                    )
                    session.expunge(row)
                return row
        except SQLAlchemyError as exc:
            logger.error(f"DB error retrieving schema {schema_id}: {exc}", exc_info=True)
            return None

    @staticmethod
    def schema_name_exists(name: str) -> bool:
        """Return True when a schema with *name* already exists."""
        try:
            with get_db_session() as session:
                count = session.scalar(
                    select(func.count()).where(ExtractionSchema.name == name)
                )
                return (count or 0) > 0
        except SQLAlchemyError as exc:
            logger.error(f"DB error checking schema name {name!r}: {exc}", exc_info=True)
            return False

    @staticmethod
    def list_schemas(
        name_filter: Optional[str] = None,
        limit: int = 20,
        offset: int = 0,
    ) -> Tuple[List[ExtractionSchema], int]:
        """
        Return a paginated list of schemas and the total count.

        *name_filter* is a case-insensitive substring match on ``name``.
        """
        try:
            with get_db_session() as session:
                stmt = select(ExtractionSchema)
                if name_filter:
                    stmt = stmt.where(ExtractionSchema.name.ilike(f"%{name_filter}%"))

                count_stmt = select(func.count()).select_from(stmt.subquery())
                total = session.scalar(count_stmt) or 0

                stmt = stmt.order_by(ExtractionSchema.created_at.desc()).limit(limit).offset(offset)
                rows = list(session.scalars(stmt).all())
                for row in rows:
                    _ = (
                        row.schema_id, row.name, row.description, row.examples,
                        row.schema_tokens, row.examples_tokens,
                        row.custom_prompt_tokens, row.created_at,
                    )
                    session.expunge(row)
                return rows, total
        except SQLAlchemyError as exc:
            logger.error(f"DB error listing schemas: {exc}", exc_info=True)
            return [], 0

    @staticmethod
    def get_referencing_job_ids(schema_id: str, limit: int = 10) -> List[str]:
        """
        Return up to *limit* job_ids that reference *schema_id*.

        Used by DELETE /v1/schemas/{id} to populate the 409 conflict payload.
        """
        try:
            with get_db_session() as session:
                rows = session.scalars(
                    select(ExtractJob.job_id)
                    .where(ExtractJob.schema_id == schema_id)
                    .limit(limit)
                ).all()
                return list(rows)
        except SQLAlchemyError as exc:
            logger.error(
                f"DB error fetching referencing jobs for schema {schema_id}: {exc}", exc_info=True
            )
            return []

    @staticmethod
    def any_schema_has_jobs() -> bool:
        """Return True if any extract_job row exists (used for bulk schema delete guard)."""
        try:
            with get_db_session() as session:
                count = session.scalar(select(func.count()).select_from(ExtractJob))
                return (count or 0) > 0
        except SQLAlchemyError as exc:
            logger.error(f"DB error checking for any jobs: {exc}", exc_info=True)
            return True  # Fail safe: prevent bulk delete on uncertainty.

    @staticmethod
    def delete_schema(schema_id: str) -> bool:
        """
        Delete a schema row.

        Returns True on success, False if the row was not found.
        Propagates IntegrityError if the FK RESTRICT constraint fires (the
        caller is responsible for catching it and returning 409).
        """
        try:
            with get_db_session() as session:
                result = session.execute(
                    delete(ExtractionSchema).where(ExtractionSchema.schema_id == schema_id)
                )
                if result.rowcount > 0:
                    logger.info(f"Deleted schema: {schema_id}")
                    return True
                logger.warning(f"Schema not found for deletion: {schema_id}")
                return False
        except IntegrityError:
            # FK RESTRICT fired — let the caller handle the 409.
            raise
        except SQLAlchemyError as exc:
            logger.error(f"DB error deleting schema {schema_id}: {exc}", exc_info=True)
            return False

    @staticmethod
    def delete_all_schemas() -> bool:
        """
        Delete all schema rows.

        Should only be called after verifying no jobs reference any schema
        (use ``any_schema_has_jobs`` first).

        Returns True on success.
        Propagates IntegrityError if RESTRICT fires unexpectedly.
        """
        try:
            with get_db_session() as session:
                result = session.execute(delete(ExtractionSchema))
                logger.info(f"Deleted {result.rowcount} schema(s)")
                return True
        except IntegrityError:
            raise
        except SQLAlchemyError as exc:
            logger.error(f"DB error bulk-deleting schemas: {exc}", exc_info=True)
            return False

    # ------------------------------------------------------------------
    # Extract jobs
    # ------------------------------------------------------------------

    @staticmethod
    def create_job(
        job_id: str,
        schema_id: str,
        document_name: str,
        source_type: str,
        job_name: Optional[str] = None,
        submitted_at: Optional[datetime] = None,
    ) -> Optional[ExtractJob]:
        """Insert a new extract_jobs row with status='accepted'."""
        try:
            with get_db_session() as session:
                row = ExtractJob(
                    job_id=job_id,
                    job_name=job_name,
                    schema_id=schema_id,
                    status="accepted",
                    document_name=document_name,
                    source_type=source_type,
                    submitted_at=submitted_at or datetime.now(timezone.utc),
                )
                session.add(row)
                session.flush()
                session.expunge(row)
                logger.info(f"Created extract job: {job_id}")
                return row
        except IntegrityError as exc:
            logger.error(f"Job {job_id} already exists: {exc}")
            return None
        except SQLAlchemyError as exc:
            logger.error(f"DB error creating job {job_id}: {exc}", exc_info=True)
            return None

    @staticmethod
    def get_job_by_id(job_id: str) -> Optional[ExtractJob]:
        """Return a job row or None if not found."""
        try:
            with get_db_session() as session:
                row = session.scalar(
                    select(ExtractJob).where(ExtractJob.job_id == job_id)
                )
                if row:
                    _ = (
                        row.job_id, row.job_name, row.schema_id, row.status,
                        row.submitted_at, row.completed_at, row.error,
                        row.document_name, row.source_type, row.document_word_count,
                        row.digitize_job_id, row.digitize_doc_id,
                        row.job_metadata, row.updated_at,
                    )
                    session.expunge(row)
                return row
        except SQLAlchemyError as exc:
            logger.error(f"DB error retrieving job {job_id}: {exc}", exc_info=True)
            return None

    @staticmethod
    def update_job(
        job_id: str,
        status: Optional[str] = None,
        completed_at: Optional[datetime] = None,
        error: Optional[str] = None,
        metadata: Optional[Dict[str, Any]] = None,
        digitize_job_id: Optional[str] = None,
        digitize_doc_id: Optional[str] = None,
        document_word_count: Optional[int] = None,
    ) -> bool:
        """
        Partial update for a job row.

        *metadata* is a **deep-merge** update (not a replacement) to avoid
        clobbering sibling keys written by earlier phases.
        """
        try:
            with get_db_session() as session:
                updates: Dict[str, Any] = {}
                if status is not None:
                    updates["status"] = status
                if completed_at is not None:
                    updates["completed_at"] = completed_at
                if error is not None:
                    updates["error"] = error
                if digitize_job_id is not None:
                    updates["digitize_job_id"] = digitize_job_id
                if digitize_doc_id is not None:
                    updates["digitize_doc_id"] = digitize_doc_id
                if document_word_count is not None:
                    updates["document_word_count"] = document_word_count

                if metadata is not None:
                    # Deep-merge: fetch current value, update in Python, write back.
                    current_row = session.scalar(
                        select(ExtractJob).where(ExtractJob.job_id == job_id)
                    )
                    current_meta = (current_row.job_metadata or {}) if current_row else {}
                    merged = _deep_merge(current_meta, metadata)
                    updates["job_metadata"] = merged

                if not updates:
                    return True

                result = session.execute(
                    update(ExtractJob).where(ExtractJob.job_id == job_id).values(**updates)
                )
                if result.rowcount > 0:
                    logger.debug(f"Updated job {job_id}: {list(updates.keys())}")
                    return True
                logger.warning(f"Job not found for update: {job_id}")
                return False
        except SQLAlchemyError as exc:
            logger.error(f"DB error updating job {job_id}: {exc}", exc_info=True)
            return False

    @staticmethod
    def list_jobs(
        status: Optional[str] = None,
        schema_id: Optional[str] = None,
        limit: int = 20,
        offset: int = 0,
        latest: bool = False,
    ) -> Tuple[List[ExtractJob], int]:
        """Return a paginated list of jobs and the total count."""
        try:
            with get_db_session() as session:
                stmt = select(ExtractJob)
                if status:
                    stmt = stmt.where(ExtractJob.status == status)
                if schema_id:
                    stmt = stmt.where(ExtractJob.schema_id == schema_id)

                count_stmt = select(func.count()).select_from(stmt.subquery())
                total = session.scalar(count_stmt) or 0

                if latest:
                    limit = 1
                    offset = 0

                stmt = (
                    stmt.order_by(ExtractJob.submitted_at.desc())
                    .limit(limit)
                    .offset(offset)
                )
                rows = list(session.scalars(stmt).all())
                for row in rows:
                    _ = (
                        row.job_id, row.job_name, row.schema_id, row.status,
                        row.document_name, row.submitted_at, row.completed_at,
                    )
                    session.expunge(row)
                return rows, total
        except SQLAlchemyError as exc:
            logger.error(f"DB error listing jobs: {exc}", exc_info=True)
            return [], 0

    @staticmethod
    def delete_job(job_id: str) -> bool:
        """Delete a single job row.  Returns True on success."""
        try:
            with get_db_session() as session:
                result = session.execute(
                    delete(ExtractJob).where(ExtractJob.job_id == job_id)
                )
                if result.rowcount > 0:
                    logger.info(f"Deleted job: {job_id}")
                    return True
                logger.warning(f"Job not found for deletion: {job_id}")
                return False
        except SQLAlchemyError as exc:
            logger.error(f"DB error deleting job {job_id}: {exc}", exc_info=True)
            return False

    @staticmethod
    def get_active_jobs() -> List[ExtractJob]:
        """Return all accepted/in_progress jobs (used by zombie recovery scan)."""
        try:
            with get_db_session() as session:
                rows = list(session.scalars(
                    select(ExtractJob).where(
                        or_(
                            ExtractJob.status == "accepted",
                            ExtractJob.status == "in_progress",
                        )
                    )
                ).all())
                for row in rows:
                    _ = (row.job_id, row.status, row.schema_id)
                    session.expunge(row)
                return rows
        except SQLAlchemyError as exc:
            logger.error(f"DB error fetching active jobs: {exc}", exc_info=True)
            return []

    @staticmethod
    def has_active_jobs() -> bool:
        """Return True if any accepted/in_progress job exists."""
        try:
            with get_db_session() as session:
                count = session.scalar(
                    select(func.count()).where(
                        or_(
                            ExtractJob.status == "accepted",
                            ExtractJob.status == "in_progress",
                        )
                    )
                )
                return (count or 0) > 0
        except SQLAlchemyError as exc:
            logger.error(f"DB error checking active jobs: {exc}", exc_info=True)
            return True  # Fail safe.

    @staticmethod
    def delete_all_jobs() -> bool:
        """Delete all job rows.  Returns True on success."""
        try:
            with get_db_session() as session:
                result = session.execute(delete(ExtractJob))
                logger.info(f"Deleted {result.rowcount} job(s)")
                return True
        except SQLAlchemyError as exc:
            logger.error(f"DB error bulk-deleting jobs: {exc}", exc_info=True)
            return False


# ---------------------------------------------------------------------------
# Deep-merge helper (avoids PostgreSQL || shallow-merge clobbering siblings)
# ---------------------------------------------------------------------------

def _deep_merge(base: Dict[str, Any], updates: Dict[str, Any]) -> Dict[str, Any]:
    """
    Recursively merge *updates* into *base*, returning a new dict.

    For keys present in both where both values are dicts, the values are
    merged recursively.  For all other types, *updates* overwrites *base*.
    """
    merged = dict(base)
    for key, value in updates.items():
        if key in merged and isinstance(merged[key], dict) and isinstance(value, dict):
            merged[key] = _deep_merge(merged[key], value)
        else:
            merged[key] = value
    return merged


# Module-level singleton for convenient import.
db_repo = DatabaseManager()

# Made with Bob
