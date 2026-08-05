"""
SQLAlchemy ORM model for translate_jobs table.
"""
from datetime import datetime, timezone
from typing import Optional

from sqlalchemy import (
    CheckConstraint,
    DateTime,
    Index,
    String,
    Text,
)
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Base(DeclarativeBase):
    """Base class for all ORM models."""
    pass


class TranslateJob(Base):
    """
    ORM model for the translate_jobs table.

    One row per async translation job.  Sync requests are stateless and never
    produce a row.
    """

    __tablename__ = "translate_jobs"

    # Job identity
    job_id: Mapped[str] = mapped_column(String(255), primary_key=True)
    job_name: Mapped[Optional[str]] = mapped_column(String(500), nullable=True)

    # Translation parameters (stored lowercase)
    source_language: Mapped[str] = mapped_column(String(100), nullable=False)
    target_language: Mapped[str] = mapped_column(String(100), nullable=False)

    # Input document info
    input_type: Mapped[str] = mapped_column(String(20), nullable=False)
    document_name: Mapped[Optional[str]] = mapped_column(String(500), nullable=True)

    # Job lifecycle
    status: Mapped[str] = mapped_column(String(50), nullable=False)
    submitted_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    completed_at: Mapped[Optional[datetime]] = mapped_column(DateTime(timezone=True), nullable=True)
    error: Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    # Phase, token diagnostics, timings (JSONB)
    job_metadata: Mapped[Optional[dict]] = mapped_column(JSONB, nullable=True)

    # Auto-updated timestamp
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=lambda: datetime.now(timezone.utc),
        onupdate=lambda: datetime.now(timezone.utc),
        nullable=False,
    )

    __table_args__ = (
        CheckConstraint(
            "status IN ('accepted','in_progress','completed','failed')",
            name="chk_translate_job_status",
        ),
        CheckConstraint(
            "input_type IN ('text','txt','md')",
            name="chk_translate_input_type",
        ),
        Index("idx_translate_jobs_submitted_at_status", "submitted_at", "status"),
    )

    def __repr__(self) -> str:
        return (
            f"<TranslateJob(job_id='{self.job_id}', status='{self.status}', "
            f"document='{self.document_name}')>"
        )

# Made with Bob
