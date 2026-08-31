"""
SQLAlchemy ORM models for digitize metadata storage.

These models map to the PostgreSQL schema defined in init_schema.sql.
"""

import enum
from datetime import datetime, timezone
from typing import List, Optional

from sqlalchemy import (
    Boolean,
    Integer,
    String,
    Text,
    DateTime,
    ForeignKey,
    CheckConstraint,
    Index,
    UniqueConstraint,
    desc,
)
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, relationship

from digitize.connectors.models import ConnectorStatus, SyncLogStatus


class Base(DeclarativeBase):
    """Base class for all ORM models."""
    pass


class JobSource(str, enum.Enum):
    """Origin of a job: submitted by a human user or created by a connector sync."""
    USER      = "user"
    CONNECTOR = "connector"


class DocumentSource(str, enum.Enum):
    """Origin of a document: submitted by a human user or created by a connector sync.

    Denormalised copy of the parent job's source so that document-only queries
    (list, delete, existence checks) can filter without joining to the jobs table.
    """
    USER      = "user"
    CONNECTOR = "connector"


class Job(Base):
    """
    Job model representing a processing job.

    Maps to the 'jobs' table in PostgreSQL.
    """
    __tablename__ = "jobs"

    # Primary key
    job_id: Mapped[str] = mapped_column(String(255), primary_key=True)

    # Job metadata
    job_name: Mapped[str | None] = mapped_column(String(500), nullable=True)
    operation: Mapped[str] = mapped_column(String(50), nullable=False)
    status: Mapped[str] = mapped_column(String(50), nullable=False)
    source: Mapped[str] = mapped_column(
        String(20), nullable=False, default=JobSource.USER.value
    )

    # Timestamps
    submitted_at: Mapped[datetime] = mapped_column(DateTime, nullable=False)
    completed_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)

    # Error tracking
    error: Mapped[str | None] = mapped_column(Text, nullable=True)

    # Statistics (stored as JSONB)
    stats: Mapped[dict] = mapped_column(
        JSONB,
        nullable=False,
        default={"total_documents": 0, "completed": 0, "failed": 0, "in_progress": 0}
    )

    # Auto-updated timestamp
    updated_at: Mapped[datetime] = mapped_column(
        DateTime,
        default=lambda: datetime.now(timezone.utc),
        onupdate=lambda: datetime.now(timezone.utc)
    )

    # Relationships
    documents: Mapped[List["Document"]] = relationship(
        "Document",
        back_populates="job",
        lazy="select"
    )

    # Constraints
    __table_args__ = (
        CheckConstraint(
            "status IN ('accepted', 'in_progress', 'completed', 'completed_with_errors', 'failed', 'cancel_pending', 'cancelled')",
            name="chk_job_status"
        ),
        CheckConstraint(
            "operation IN ('ingestion', 'digitization')",
            name="chk_job_operation"
        ),
        CheckConstraint(
            "source IN ('user', 'connector')",
            name="chk_job_source"
        ),
        Index("idx_jobs_submitted_at_status", "submitted_at", "status"),
    )

    def __repr__(self) -> str:
        return f"<Job(job_id='{self.job_id}', status='{self.status}')>"


class Document(Base):
    """
    Document model representing a document being processed.

    Maps to the 'documents' table in PostgreSQL.
    """
    __tablename__ = "documents"

    # Primary key
    doc_id: Mapped[str] = mapped_column(String(255), primary_key=True)

    # Foreign key to job
    job_id: Mapped[str | None] = mapped_column(
        String(255),
        ForeignKey("jobs.job_id", ondelete="SET NULL"),
        nullable=True
    )

    # Document metadata
    name: Mapped[str] = mapped_column(String(500), nullable=False)
    type: Mapped[str] = mapped_column(String(50), nullable=False)
    status: Mapped[str] = mapped_column(String(50), nullable=False)
    source: Mapped[str] = mapped_column(
        String(20), nullable=False, default=DocumentSource.USER.value
    )
    output_format: Mapped[str] = mapped_column(String(10), nullable=False)

    # Timestamps
    submitted_at: Mapped[datetime] = mapped_column(DateTime, nullable=False)
    completed_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)

    # Error tracking
    error: Mapped[str | None] = mapped_column(Text, nullable=True)

    # Additional metadata (stored as JSONB)
    doc_metadata: Mapped[dict] = mapped_column("metadata", JSONB, nullable=False, default={}, key="doc_metadata")

    # Auto-updated timestamp
    updated_at: Mapped[datetime] = mapped_column(
        DateTime,
        default=lambda: datetime.now(timezone.utc),
        onupdate=lambda: datetime.now(timezone.utc)
    )

    # Relationships
    job: Mapped["Job"] = relationship("Job", back_populates="documents")

    # Constraints
    __table_args__ = (
        CheckConstraint(
            "status IN ('accepted', 'in_progress', 'digitized', 'processed',"
            " 'chunked', 'completed', 'completed_with_errors', 'failed', 'already_exists', 'cancelled')",
            name="chk_doc_status"
        ),
        CheckConstraint(
            "type IN ('ingestion', 'digitization')",
            name="chk_doc_type"
        ),
        CheckConstraint(
            "source IN ('user', 'connector')",
            name="chk_doc_source"
        ),
        CheckConstraint(
            "output_format IN ('txt', 'md', 'json')",
            name="chk_output_format"
        ),
        Index("idx_documents_job_id", "job_id"),
        Index("idx_documents_submitted_at_status", "submitted_at", "status"),
    )

    def __repr__(self) -> str:
        return f"<Document(doc_id='{self.doc_id}', name='{self.name}', status='{self.status}')>"


class DocumentChecksum(Base):
    """
    Registry table that maps a checksum to the authoritative completed
    Document row for that content.

    Maps to the 'document_checksum' table in PostgreSQL.

    The FK to documents(doc_id) ON DELETE CASCADE means registry entries are
    automatically removed when the referenced document is deleted, preventing
    orphaned hashes from blocking future re-ingestion of the same file.
    """
    __tablename__ = "document_checksum"

    checksum: Mapped[str] = mapped_column(Text, primary_key=True)
    doc_id: Mapped[str] = mapped_column(
        Text,
        ForeignKey("documents.doc_id", ondelete="CASCADE"),
        nullable=False,
        unique=True,
    )

    def __repr__(self) -> str:
        return f"<DocumentChecksum(checksum='{self.checksum[:20]}...', doc_id='{self.doc_id}')>"


class Connector(Base):
    """
    Stores connector config, encrypted credential blobs, and top-level sync state.

    Maps to the 'connectors' table in PostgreSQL.
    """
    __tablename__ = "connectors"

    id: Mapped[str] = mapped_column(Text, primary_key=True)
    name: Mapped[str] = mapped_column(Text, nullable=False, unique=True)
    type: Mapped[str] = mapped_column(Text, nullable=False)
    connection_details: Mapped[dict] = mapped_column(JSONB, nullable=False, default={})
    allowed_extensions: Mapped[list] = mapped_column(JSONB, nullable=False, default=[])
    sync_interval_seconds: Mapped[int] = mapped_column(Integer, nullable=False, default=300)
    attached_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        nullable=False,
        default=lambda: datetime.now(timezone.utc),
    )
    last_sync_at: Mapped[Optional[datetime]] = mapped_column(DateTime(timezone=True), nullable=True)
    status: Mapped[str] = mapped_column(Text, nullable=False, default=ConnectorStatus.UP_TO_DATE)
    total_files: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    message: Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    # Relationships
    sync_logs: Mapped[List["ConnectorSyncLog"]] = relationship(
        "ConnectorSyncLog",
        back_populates="connector",
        lazy="select",
        cascade="all, delete-orphan",
    )

    __table_args__ = (
        CheckConstraint("type IN ('file_system', 'object_storage')", name="chk_connector_type"),
    )

    def __repr__(self) -> str:
        return f"<Connector(id='{self.id}', name='{self.name}', type='{self.type}', status='{self.status}')>"


class ConnectorDocumentChecksum(Base):
    """
    Connector-sourced document dedup and reference-counting table.

    Maps to the 'connector_document_checksum' table in PostgreSQL.

    One row per (checksum, connector_id) pair. A checksum may appear in multiple
    rows (shared across connectors). doc_id is stored on every row so that
    deletion can proceed without a join. No FK constraints and no ON DELETE CASCADE
    — deletion is an intentional, reference-counted operation managed in application code.
    """
    __tablename__ = "connector_document_checksum"

    checksum: Mapped[str] = mapped_column(Text, nullable=False, primary_key=True)
    connector_id: Mapped[str] = mapped_column(Text, nullable=False, primary_key=True)
    doc_id: Mapped[str] = mapped_column(Text, nullable=False)

    __table_args__ = (
        Index("idx_cdc_connector_id", "connector_id"),
    )

    def __repr__(self) -> str:
        return f"<ConnectorDocumentChecksum(checksum='{self.checksum[:20]}...', connector_id='{self.connector_id}')>"


class ConnectorSyncLog(Base):
    """
    Persistent per-tick history backing the sync-logs API.

    Maps to the 'connector_sync_logs' table in PostgreSQL.
    """
    __tablename__ = "connector_sync_logs"

    connector_id: Mapped[str] = mapped_column(
        Text,
        ForeignKey("connectors.id", ondelete="CASCADE"),
        nullable=False,
        primary_key=True,
    )
    seq: Mapped[int] = mapped_column(Integer, nullable=False, primary_key=True)
    started_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    finished_at: Mapped[Optional[datetime]] = mapped_column(DateTime(timezone=True), nullable=True)
    total_files: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    new_files: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    completed_files: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    removed_files: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    status: Mapped[str] = mapped_column(Text, nullable=False, default=SyncLogStatus.STARTED)
    error: Mapped[str] = mapped_column(Text, nullable=False, default="")

    # Relationships
    connector: Mapped["Connector"] = relationship(
        "Connector", back_populates="sync_logs"
    )

    __table_args__ = (
        Index("idx_csl_connector_started", "connector_id", desc("started_at")),
    )

    def __repr__(self) -> str:
        return f"<ConnectorSyncLog(connector_id='{self.connector_id}', seq={self.seq})>"


class ConversionTaskStatus(str, enum.Enum):
    """Lifecycle statuses for a ConversionTask row."""
    PENDING        = "pending"         # over-quota; waiting for queue headroom
    QUEUED         = "queued"          # admitted to queue; waiting for semaphore slot
    RUNNING        = "running"         # semaphore slot acquired
    COMPLETED      = "completed"       # conversion finished successfully
    FAILED         = "failed"          # conversion failed or timed out
    CANCEL_PENDING = "cancel_pending"  # cancellation requested; dispatcher will stop before or after current chunk
    CANCELLED      = "cancelled"       # dispatcher acknowledged cancellation and halted


class ConversionTask(Base):
    """
    Conversion task model representing a single Docling conversion work item.

    Maps to the 'conversion_tasks' table in PostgreSQL.
    Each task is linked to a job and drives the dispatcher-based conversion queue.
    """
    __tablename__ = "conversion_tasks"

    # Primary key
    task_id: Mapped[str] = mapped_column(String(255), primary_key=True)

    # Link back to the digitize job that owns this task
    job_id: Mapped[str | None] = mapped_column(
        String(255),
        ForeignKey("jobs.job_id", ondelete="SET NULL"),
        nullable=True,
    )

    # Digitize document id; informational only
    doc_id: Mapped[str | None] = mapped_column(String(255), nullable=True)

    # Operation type — 'ingestion' | 'digitization'
    operation: Mapped[str] = mapped_column(String(50), nullable=False)

    # Input — absolute path written at enqueue time
    cached_file: Mapped[str] = mapped_column(Text, nullable=False)

    # Output format
    output_format: Mapped[str] = mapped_column(String(10), nullable=False)

    # Page count; may be 0 for DOCX
    page_count: Mapped[int | None] = mapped_column(Integer, nullable=True)

    # True when page_count >= heavy_doc_page_threshold
    is_large: Mapped[bool] = mapped_column(Boolean, nullable=False, default=False)

    # Lifecycle status — use ConversionTaskStatus values; stored as VARCHAR in DB
    status: Mapped[str] = mapped_column(String(50), nullable=False)

    # Written on completion
    result_path: Mapped[str | None] = mapped_column(Text, nullable=True)

    # Error message on failure
    error: Mapped[str | None] = mapped_column(Text, nullable=True)

    # Timestamps
    queued_at: Mapped[datetime] = mapped_column(
        DateTime,
        nullable=False,
        default=lambda: datetime.now(timezone.utc),
    )
    started_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)
    completed_at: Mapped[datetime | None] = mapped_column(DateTime, nullable=True)

    # Constraints and indexes
    __table_args__ = (
        CheckConstraint(
            "operation IN ('ingestion', 'digitization')",
            name="chk_ct_operation",
        ),
        CheckConstraint(
            "output_format IN ('json', 'md', 'txt')",
            name="chk_ct_output_format",
        ),
        CheckConstraint(
            "status IN ('pending', 'queued', 'running', 'completed', 'failed', 'cancel_pending', 'cancelled')",
            name="chk_ct_status",
        ),
        # Supports dispatcher's pick query (ORDER BY queued_at per status+operation)
        Index("idx_ct_status_op_queued", "status", "operation", "queued_at"),
        # Supports get_conversion_task_by_job_id — called by pipeline pollers
        Index("idx_ct_job_id", "job_id"),
    )

    def __repr__(self) -> str:
        return (
            f"<ConversionTask(task_id='{self.task_id}', "
            f"status='{self.status}', operation='{self.operation}')>"
        )

# Made with Bob
