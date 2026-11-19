"""
SQLAlchemy ORM models for the forecast metadata tables.

The tables are created by db/scripts/init_schema.sql, not by Base.metadata.create_all,
so these models must match that file. tests/unit/test_db_models.py checks this.

The restricted forecast_worker role has column-level grants on jobs, so the worker
must update only jobs.result and jobs.message and must not flush other columns.
"""

import uuid
from datetime import datetime
from typing import Optional

from sqlalchemy import (
    BigInteger,
    CheckConstraint,
    DateTime,
    FetchedValue,
    ForeignKey,
    Identity,
    Index,
    PrimaryKeyConstraint,
    String,
    Text,
    func,
    text,
)
from sqlalchemy.dialects.postgresql import JSONB, UUID
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Base(DeclarativeBase):
    """Base class for all ORM models."""


class Connector(Base):
    """Connection details of one source database. Maps to 'connectors'."""

    __tablename__ = "connectors"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    name: Mapped[str] = mapped_column(String(255), nullable=False, unique=True)
    type: Mapped[str] = mapped_column(Text, nullable=False)
    # Safe fields in plaintext, secret fields encrypted.
    connection_details: Mapped[dict] = mapped_column(
        JSONB, nullable=False, server_default=text("'{}'")
    )
    attached_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.current_timestamp()
    )
    # Kept current by the update_connectors_updated_at database trigger.
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        nullable=False,
        server_default=func.current_timestamp(),
        server_onupdate=FetchedValue(),
    )
    last_trained_at: Mapped[Optional[datetime]] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    status: Mapped[str] = mapped_column(Text, nullable=False)
    message: Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    __table_args__ = (
        CheckConstraint(
            "status IN ('ACCEPTED', 'TRAINING', 'TRAINED', 'FAILED')",
            name="chk_connectors_status",
        ),
    )

    def __repr__(self) -> str:
        return f"<Connector(id='{self.id}', name='{self.name}', status='{self.status}')>"


class TrainingConfig(Base):
    """Training details given when the connector was created. Maps to 'training_configs'."""

    __tablename__ = "training_configs"

    connector_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), ForeignKey("connectors.id", ondelete="CASCADE"), primary_key=True
    )
    source_table: Mapped[str] = mapped_column(Text, nullable=False)
    target_column: Mapped[str] = mapped_column(Text, nullable=False)
    # [{"name": ..., "type": ...}, ...]
    input_columns: Mapped[list] = mapped_column(JSONB, nullable=False)
    # hourly, daily, weekly, monthly or yearly. NULL: only the first training runs.
    schedule: Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    __table_args__ = (
        CheckConstraint(
            "schedule IS NULL OR lower(schedule) IN ('hourly', 'daily', 'weekly', 'monthly', 'yearly')",
            name="chk_training_configs_schedule",
        ),
    )

    def __repr__(self) -> str:
        return f"<TrainingConfig(connector_id='{self.connector_id}', source_table='{self.source_table}')>"


class Job(Base):
    """
    Training job and run history. Maps to 'jobs'.

    seq is the primary key and the forecaster version. The database assigns it, so
    it is never set from Python.
    """

    __tablename__ = "jobs"

    seq: Mapped[int] = mapped_column(BigInteger, Identity(always=True), primary_key=True)
    connector_id: Mapped[Optional[uuid.UUID]] = mapped_column(
        UUID(as_uuid=True), ForeignKey("connectors.id", ondelete="SET NULL"), nullable=True
    )
    status: Mapped[str] = mapped_column(Text, nullable=False)
    message: Mapped[Optional[str]] = mapped_column(Text, nullable=True)
    # rows_read, algorithm, training_seconds, attempts. Keys can be missing.
    result: Mapped[Optional[dict]] = mapped_column(JSONB, nullable=True)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.current_timestamp()
    )
    started_at: Mapped[Optional[datetime]] = mapped_column(DateTime(timezone=True), nullable=True)
    finished_at: Mapped[Optional[datetime]] = mapped_column(DateTime(timezone=True), nullable=True)

    __table_args__ = (
        CheckConstraint(
            "status IN ('QUEUED', 'RUNNING', 'COMPLETED', 'FAILED')",
            name="chk_jobs_status",
        ),
        CheckConstraint(
            "status NOT IN ('QUEUED', 'RUNNING') OR connector_id IS NOT NULL",
            name="chk_jobs_active_has_connector",
        ),
        # At most one running job.
        Index(
            "idx_jobs_single_running",
            "status",
            unique=True,
            postgresql_where=text("status = 'RUNNING'"),
        ),
        Index("idx_jobs_connector_created", "connector_id", text("created_at DESC")),
    )

    def __repr__(self) -> str:
        return f"<Job(seq={self.seq}, connector_id='{self.connector_id}', status='{self.status}')>"


class Forecaster(Base):
    """One forecaster loaded into Triton. Maps to 'forecasters'."""

    __tablename__ = "forecasters"

    connector_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), ForeignKey("connectors.id", ondelete="CASCADE"), nullable=False
    )
    job_id: Mapped[Optional[int]] = mapped_column(
        BigInteger, ForeignKey("jobs.seq", ondelete="SET NULL"), nullable=True
    )
    version: Mapped[int] = mapped_column(BigInteger, nullable=False)
    # algorithm and training_seconds, copied from the job's result.
    metrics: Mapped[Optional[dict]] = mapped_column(JSONB, nullable=True)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.current_timestamp()
    )

    __table_args__ = (PrimaryKeyConstraint("connector_id", "version"),)

    def __repr__(self) -> str:
        return f"<Forecaster(connector_id='{self.connector_id}', version={self.version})>"
