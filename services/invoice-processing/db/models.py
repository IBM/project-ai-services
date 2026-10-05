"""
SQLAlchemy ORM models for Invoice Processing service metadata storage.
"""

from datetime import datetime, timezone
import enum
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
    """Shared declarative base for invoice processing ORM models."""
    pass


class InvoiceJobStatus(str, enum.Enum):
    ACCEPTED = "accepted"
    ROUTING = "routing"
    DIGITIZING = "digitizing"   # waiting for digitize service (PDF path)
    EXTRACTING = "extracting"   # waiting for extract service
    STAGING = "staging"        # assembler running
    REVIEW = "review"          # awaiting human approve/reject
    LOADING = "loading"        # DB load in progress
    COMPLETED = "completed"
    REJECTED = "rejected"      # human rejected
    FAILED = "failed"


class InvoiceJob(Base):
    """
    Async invoice processing job row.
    """

    __tablename__ = "invoice_jobs"

    job_id: Mapped[str] = mapped_column(String(255), primary_key=True)
    filename: Mapped[str] = mapped_column(String(500), nullable=False)
    input_type: Mapped[str] = mapped_column(String(50), nullable=False)  # 'pdf' | 'image'
    pipeline_path: Mapped[Optional[str]] = mapped_column(String(50), nullable=True)  # 'pdf_path' | 'oneshot'
    status: Mapped[str] = mapped_column(String(50), nullable=False, default=InvoiceJobStatus.ACCEPTED.value)
    submitted_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=lambda: datetime.now(timezone.utc),
        nullable=False,
    )
    completed_at: Mapped[Optional[datetime]] = mapped_column(DateTime(timezone=True), nullable=True)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=lambda: datetime.now(timezone.utc),
        onupdate=lambda: datetime.now(timezone.utc),
        nullable=False,
    )
    error: Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    # External job tracking
    digitize_job_id: Mapped[Optional[str]] = mapped_column(String(255), nullable=True)
    extract_job_id: Mapped[Optional[str]] = mapped_column(String(255), nullable=True)

    # Result storage
    staged_header: Mapped[Optional[dict]] = mapped_column(JSONB, nullable=True)
    staged_lines: Mapped[Optional[list]] = mapped_column(JSONB, nullable=True)
    interface_ref: Mapped[Optional[str]] = mapped_column(String(255), nullable=True)

    # Pipeline metadata (latency, timings, diagnostics)
    job_metadata: Mapped[Optional[dict]] = mapped_column("metadata", JSONB, nullable=True)

    __table_args__ = (
        CheckConstraint(
            "status IN ('accepted', 'routing', 'digitizing', 'extracting', 'staging', 'review', 'loading', 'completed', 'rejected', 'failed')",
            name="chk_invoice_job_status",
        ),
        Index("idx_invoice_jobs_submitted_at_status", "submitted_at", "status"),
        Index("idx_invoice_jobs_status", "status"),
    )

    def __repr__(self) -> str:  # pragma: no cover
        return f"<InvoiceJob(job_id='{self.job_id}', status='{self.status}', filename='{self.filename}')>"
