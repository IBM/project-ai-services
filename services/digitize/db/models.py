"""
SQLAlchemy ORM models for digitize metadata storage.

These models map to the PostgreSQL schema defined in init_schema.sql.
"""

from datetime import datetime, timezone
from typing import List, Optional

from sqlalchemy import (
    BigInteger,
    Integer,
    String,
    Text,
    DateTime,
    ForeignKey,
    CheckConstraint,
    Index,
    UniqueConstraint,
)
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, relationship


class Base(DeclarativeBase):
    """Base class for all ORM models."""
    pass


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
            "status IN ('accepted', 'in_progress', 'completed', 'failed')",
            name="chk_job_status"
        ),
        CheckConstraint(
            "operation IN ('ingestion', 'digitization')",
            name="chk_job_operation"
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
            " 'chunked', 'completed', 'failed', 'already_exists')",
            name="chk_doc_status"
        ),
        CheckConstraint(
            "type IN ('ingestion', 'digitization')",
            name="chk_doc_type"
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


class ActiveConnector(Base):
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
    sync_status: Mapped[str] = mapped_column(Text, nullable=False, default="up to date")
    last_sync_error: Mapped[Optional[str]] = mapped_column(Text, nullable=True)
    total_files: Mapped[int] = mapped_column(Integer, nullable=False, default=0)

    # Relationships
    sync_history: Mapped[List["ConnectorSyncHistory"]] = relationship(
        "ConnectorSyncHistory",
        back_populates="connector",
        lazy="select",
        cascade="all, delete-orphan",
    )

    __table_args__ = (
        CheckConstraint("type IN ('ssh', 's3')", name="chk_connector_type"),
    )

    def __repr__(self) -> str:
        return f"<ActiveConnector(id='{self.id}', name='{self.name}', type='{self.type}', status='{self.sync_status}')>"


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


class ConnectorSyncHistory(Base):
    """
    Persistent per-tick history backing the sync-history API.

    Maps to the 'connector_sync_history' table in PostgreSQL.
    """
    __tablename__ = "connector_sync_history"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True, autoincrement=True)
    connector_id: Mapped[str] = mapped_column(
        Text,
        ForeignKey("connectors.id", ondelete="CASCADE"),
        nullable=False,
    )
    seq: Mapped[int] = mapped_column(Integer, nullable=False)
    started_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    finished_at: Mapped[Optional[datetime]] = mapped_column(DateTime(timezone=True), nullable=True)
    total_files: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    new_files: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    removed_files: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    failed_files: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    status: Mapped[str] = mapped_column(Text, nullable=False, default="started")
    error: Mapped[str] = mapped_column(Text, nullable=False, default="")

    # Relationships
    connector: Mapped["ActiveConnector"] = relationship(
        "ActiveConnector", back_populates="sync_history"
    )

    __table_args__ = (
        Index("idx_csh_connector_started", "connector_id", "started_at"),
        UniqueConstraint("connector_id", "seq", name="uq_csh_connector_seq"),
    )

    def __repr__(self) -> str:
        return f"<ConnectorSyncHistory(id={self.id}, connector_id='{self.connector_id}', seq={self.seq})>"

# Made with Bob
