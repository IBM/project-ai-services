"""
SQLAlchemy ORM models for digitize metadata storage.

These models map to the PostgreSQL schema defined in init_schema.sql.
"""

from datetime import datetime
from typing import List

from sqlalchemy import (
    Column,
    String,
    Text,
    DateTime,
    ForeignKey,
    CheckConstraint,
    Index,
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
        default=datetime.utcnow,
        onupdate=datetime.utcnow
    )
    
    # Relationships
    documents: Mapped[List["Document"]] = relationship(
        "Document",
        back_populates="job",
        cascade="all, delete-orphan",
        lazy="selectinload"
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
        ForeignKey("jobs.job_id", ondelete="CASCADE"),
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
    metadata: Mapped[dict] = mapped_column(JSONB, nullable=False, default={})
    
    # Auto-updated timestamp
    updated_at: Mapped[datetime] = mapped_column(
        DateTime,
        default=datetime.utcnow,
        onupdate=datetime.utcnow
    )
    
    # Relationships
    job: Mapped["Job"] = relationship("Job", back_populates="documents")
    
    # Constraints
    __table_args__ = (
        CheckConstraint(
            "status IN ('accepted', 'in_progress', 'digitized', 'processed', 'chunked', 'completed', 'failed')",
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

# Made with Bob
