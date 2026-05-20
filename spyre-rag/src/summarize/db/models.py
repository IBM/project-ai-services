"""
SQLAlchemy ORM models for summarize metadata storage.

These models map to the PostgreSQL schema for async summarization jobs.
"""

from datetime import datetime, timezone
from typing import Optional

from sqlalchemy import (
    String,
    Text,
    Integer,
    DateTime,
    CheckConstraint,
    Index,
)
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Base(DeclarativeBase):
    """Base class for all ORM models."""
    pass


class SummarizeJob(Base):
    """
    SummarizeJob model representing an async summarization job.
    
    Maps to the 'summarize_jobs' table in PostgreSQL.
    Each job processes exactly one document (file).
    """
    __tablename__ = "summarize_jobs"
    
    # Job identity
    job_id: Mapped[str] = mapped_column(String(255), primary_key=True)
    job_name: Mapped[Optional[str]] = mapped_column(String(500), nullable=True)
    
    # Job status
    status: Mapped[str] = mapped_column(String(50), nullable=False)
    submitted_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    completed_at: Mapped[Optional[datetime]] = mapped_column(DateTime(timezone=True), nullable=True)
    error: Mapped[Optional[str]] = mapped_column(Text, nullable=True)
    
    # Document info (inlined — one job = one document)
    document_name: Mapped[str] = mapped_column(String(500), nullable=False)
    document_word_count: Mapped[Optional[int]] = mapped_column(Integer, nullable=True)
    
    # Summarization parameters
    level: Mapped[str] = mapped_column(String(20), nullable=False, default='standard')
    job_type: Mapped[str] = mapped_column(String(20), nullable=False, default='direct')
    
    # Chunking progress and other job metadata (JSONB)
    # Example: {"total_chunks": 12, "completed_chunks": 7, "phase": "summarizing"}
    job_metadata: Mapped[Optional[dict]] = mapped_column(JSONB, nullable=True)
    
    # Auto-updated timestamp
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=lambda: datetime.now(timezone.utc),
        onupdate=lambda: datetime.now(timezone.utc),
        nullable=False
    )
    
    # Constraints
    __table_args__ = (
        CheckConstraint(
            "status IN ('accepted', 'in_progress', 'completed', 'failed')",
            name='chk_summarize_job_status'
        ),
        CheckConstraint(
            "level IN ('brief', 'standard', 'detailed')",
            name='chk_summarize_job_level'
        ),
        CheckConstraint(
            "job_type IN ('direct', 'chunked')",
            name='chk_summarize_job_type'
        ),
        Index('idx_summarize_jobs_submitted_at_status', 'submitted_at', 'status'),
    )
    
    def __repr__(self) -> str:
        return f"<SummarizeJob(job_id='{self.job_id}', status='{self.status}', document='{self.document_name}')>"

