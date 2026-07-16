"""
SQLAlchemy ORM models for extract service metadata storage.

Two tables:
  schemas      — immutable schema registry; no UPDATE grant in production.
  extract_jobs — mutable async-job tracking with FK to schemas (RESTRICT).
"""

from datetime import datetime, timezone
from typing import Optional

from sqlalchemy import (
    CheckConstraint,
    DateTime,
    ForeignKey,
    Index,
    Integer,
    String,
    Text,
)
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Base(DeclarativeBase):
    """Shared declarative base for extract ORM models."""
    pass


class ExtractionSchema(Base):
    """
    Immutable schema registry row.

    Rows are written once (INSERT) and never changed.  The application
    layer exposes no update endpoint, and the DB role is denied UPDATE
    on this table as a defense-in-depth measure (see init_schema.sql).
    """

    __tablename__ = "schemas"

    schema_id: Mapped[str] = mapped_column(String(255), primary_key=True)
    name: Mapped[str] = mapped_column(String(200), nullable=False, unique=True)
    description: Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    # Normalized draft 2020-12 JSON Schema stored as JSONB
    json_schema: Mapped[dict] = mapped_column(JSONB, nullable=False)
    examples: Mapped[Optional[list]] = mapped_column(JSONB, nullable=True)
    custom_prompt: Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    # Token counts cached at registration time (schemas are immutable so these
    # never go stale and save one /tokenize call per extraction request).
    schema_tokens: Mapped[int] = mapped_column(Integer, nullable=False)
    examples_tokens: Mapped[int] = mapped_column(Integer, nullable=False, default=0)
    custom_prompt_tokens: Mapped[int] = mapped_column(Integer, nullable=False, default=0)

    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=lambda: datetime.now(timezone.utc),
        nullable=False,
    )

    __table_args__ = (
        Index("idx_schemas_name", "name"),
    )

    def __repr__(self) -> str:  # pragma: no cover
        return f"<ExtractionSchema(schema_id='{self.schema_id}', name='{self.name}')>"


class ExtractJob(Base):
    """
    Async extraction job row.

    References ExtractionSchema via ON DELETE RESTRICT so that a schema
    row cannot be deleted while any job (active or historical) references it.
    """

    __tablename__ = "extract_jobs"

    job_id: Mapped[str] = mapped_column(String(255), primary_key=True)
    job_name: Mapped[Optional[str]] = mapped_column(String(500), nullable=True)

    schema_id: Mapped[str] = mapped_column(
        String(255),
        ForeignKey("schemas.schema_id", ondelete="RESTRICT"),
        nullable=False,
    )

    status: Mapped[str] = mapped_column(String(50), nullable=False)
    submitted_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    completed_at: Mapped[Optional[datetime]] = mapped_column(DateTime(timezone=True), nullable=True)
    error: Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    # Document info (one job = one document)
    document_name: Mapped[str] = mapped_column(String(500), nullable=False)
    source_type: Mapped[str] = mapped_column(String(10), nullable=False)  # 'txt' | 'pdf'
    document_word_count: Mapped[Optional[int]] = mapped_column(Integer, nullable=True)

    # Digitize service pointers (PDF path only)
    digitize_job_id: Mapped[Optional[str]] = mapped_column(String(255), nullable=True)
    digitize_doc_id: Mapped[Optional[str]] = mapped_column(String(255), nullable=True)

    # Phase, token diagnostics, timings, validation debug info (JSONB)
    job_metadata: Mapped[Optional[dict]] = mapped_column("metadata", JSONB, nullable=True)

    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=lambda: datetime.now(timezone.utc),
        onupdate=lambda: datetime.now(timezone.utc),
        nullable=False,
    )

    __table_args__ = (
        CheckConstraint(
            "status IN ('accepted', 'in_progress', 'completed', 'failed')",
            name="chk_extract_job_status",
        ),
        CheckConstraint(
            "source_type IN ('txt', 'pdf')",
            name="chk_extract_source_type",
        ),
        Index("idx_extract_jobs_submitted_at_status", "submitted_at", "status"),
        Index("idx_extract_jobs_schema_id", "schema_id"),
    )

    def __repr__(self) -> str:  # pragma: no cover
        return (
            f"<ExtractJob(job_id='{self.job_id}', status='{self.status}', "
            f"schema_id='{self.schema_id}')>"
        )

# Made with Bob
