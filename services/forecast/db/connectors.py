"""
Database access for connectors, their training details, jobs and forecasters.

Uses SQLAlchemy ORM models from ``forecast.db.models``. Every function takes
the session of the caller, which commits or rolls back.
"""

import uuid
from dataclasses import dataclass
from datetime import datetime
from typing import Any, Optional

from sqlalchemy import Select, exists, func, select, text, update
from sqlalchemy.orm import Session
from sqlalchemy.sql.elements import ColumnElement

from forecast.db.models import Connector, ConnectorStatus, Forecaster, Job, TrainingConfig


@dataclass(frozen=True)
class ConnectorListRow:
    """A connector as listed: its identity, status and the training table."""

    id: uuid.UUID
    name: str
    type: str
    status: str
    message: Optional[str]
    created_at: datetime
    last_trained_at: Optional[datetime]
    # None when the connector has no training_configs row.
    source_table: Optional[str]
    target_column: Optional[str]


@dataclass(frozen=True)
class ConnectorRow(ConnectorListRow):
    """A connector with its stored config and schedule."""

    # connection_details: safe fields in plaintext, secret fields encrypted.
    config: dict[str, Any]
    updated_at: datetime
    schedule: Optional[str]
    # Columns the forecast API validates rows against; None without a training_configs row.
    input_columns: Optional[list[str]] = None


@dataclass(frozen=True)
class ForecasterRow:
    """A trained forecaster version."""

    version: int
    metrics: Optional[dict[str, Any]]
    created_at: datetime


@dataclass(frozen=True)
class JobRow:
    """A training job as stored."""

    seq: int
    status: str
    message: Optional[str]
    result: Optional[dict[str, Any]]
    created_at: datetime
    started_at: Optional[datetime]
    finished_at: Optional[datetime]


# A lock that a stuck transaction holds fails after this time instead of blocking forever.
LOCK_TIMEOUT_SQL = "SET LOCAL lock_timeout = '5s'"


def _as_uuid(val: Any) -> uuid.UUID:
    """Convert string or UUID to uuid.UUID object."""
    return uuid.UUID(val) if isinstance(val, str) else val


def lock_connectors(session: Session) -> None:
    """Serialize connector creation: two transactions cannot both see an empty table."""
    session.execute(text(LOCK_TIMEOUT_SQL))
    session.execute(text("LOCK TABLE connectors IN SHARE ROW EXCLUSIVE MODE"))


def connector_exists(session: Session) -> bool:
    """Return True when a connector exists, whatever its status: a FAILED one blocks a new one too."""
    return bool(session.scalar(select(exists().select_from(Connector))))


def connector_id_exists(session: Session, connector_id: str) -> bool:
    """Return True when a connector row with this id exists, whatever its status."""
    return bool(
        session.scalar(select(exists().where(Connector.id == _as_uuid(connector_id))))
    )


def insert_failed_connector(
    session: Session,
    *,
    connector_id: str,
    name: str,
    connector_type: str,
    config: dict,
    message: str,
) -> None:
    """Store a connector that was not accepted: status FAILED, no training details, no job."""
    session.add(
        Connector(
            id=_as_uuid(connector_id),
            name=name,
            type=connector_type,
            connection_details=config,
            status=ConnectorStatus.FAILED,
            message=message,
        )
    )
    session.flush()


def insert_connector(
    session: Session,
    *,
    connector_id: str,
    name: str,
    connector_type: str,
    config: dict,
    source_table: str,
    target_column: str,
    input_columns: list[str],
    schedule: Optional[str],
) -> None:
    """Store the connector, its training details and the first training job."""
    cid_uuid = _as_uuid(connector_id)
    connector = Connector(
        id=cid_uuid,
        name=name,
        type=connector_type,
        connection_details=config,
        status=ConnectorStatus.ACCEPTED,
    )
    training_config = TrainingConfig(
        connector_id=cid_uuid,
        source_table=source_table,
        target_column=target_column,
        input_columns=input_columns,
        schedule=schedule,
    )
    job = Job(
        connector_id=cid_uuid,
        status="QUEUED",
    )
    session.add_all([connector, training_config, job])
    session.flush()


def _name_filter(name: Optional[str]) -> Optional[ColumnElement[bool]]:
    """Partial, case-insensitive match filter on Connector.name."""
    if name is None:
        return None
    escaped = name.replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")
    return Connector.name.ilike(f"%{escaped}%", escape="\\")


def list_connectors(
    session: Session, *, limit: int, offset: int, name: Optional[str]
) -> tuple[list[ConnectorListRow], int]:
    """Return one page of connectors and the total count that matches the filter."""
    stmt = (
        select(
            Connector.id,
            Connector.name,
            Connector.type,
            Connector.status,
            Connector.message,
            Connector.attached_at.label("created_at"),
            Connector.last_trained_at,
            TrainingConfig.source_table,
            TrainingConfig.target_column,
        )
        .outerjoin(TrainingConfig, TrainingConfig.connector_id == Connector.id)
    )
    count_stmt = select(func.count(Connector.id))

    name_cond = _name_filter(name)
    if name_cond is not None:
        stmt = stmt.where(name_cond)
        count_stmt = count_stmt.where(name_cond)

    total = session.scalar(count_stmt) or 0
    stmt = stmt.order_by(Connector.attached_at.desc(), Connector.id).limit(limit).offset(offset)
    rows = session.execute(stmt).mappings().all()
    return [ConnectorListRow(**row) for row in rows], total


def _connector_select() -> Select:
    """Select a connector with its stored config and training details."""
    return select(
        Connector.id,
        Connector.name,
        Connector.type,
        Connector.connection_details.label("config"),
        Connector.status,
        Connector.message,
        Connector.attached_at.label("created_at"),
        Connector.updated_at,
        Connector.last_trained_at,
        TrainingConfig.source_table,
        TrainingConfig.target_column,
        TrainingConfig.input_columns,
        TrainingConfig.schedule,
    ).outerjoin(TrainingConfig, TrainingConfig.connector_id == Connector.id)


def get_connector(
    session: Session, connector_id: str, *, for_update: bool = False
) -> Optional[ConnectorRow]:
    """Return one connector with its training details, or None when it does not exist.

    With ``for_update``, the connector row is locked until the transaction ends, so a
    read-modify-write of its config cannot lose a concurrent update.
    """
    cid_uuid = _as_uuid(connector_id)
    if for_update:
        session.execute(text(LOCK_TIMEOUT_SQL))

    stmt = _connector_select().where(Connector.id == cid_uuid)
    if for_update:
        stmt = stmt.with_for_update(of=Connector)

    row = session.execute(stmt).mappings().first()
    return ConnectorRow(**row) if row is not None else None


def update_connector(
    session: Session, connector_id: str, *, name: Optional[str], config: dict
) -> None:
    """Set the name when given, and store the config (already encrypted where needed)."""
    cid_uuid = _as_uuid(connector_id)
    values = {"connection_details": config}
    if name is not None:
        values["name"] = name
    stmt = update(Connector).where(Connector.id == cid_uuid).values(**values)
    session.execute(stmt)


def list_ready_versions(session: Session, connector_id: str) -> list[int]:
    """Return the versions of the connector forecasters that are loaded into Triton, newest first."""
    cid_uuid = _as_uuid(connector_id)
    stmt = (
        select(Forecaster.version)
        .where(Forecaster.connector_id == cid_uuid)
        .order_by(Forecaster.version.desc())
    )
    return list(session.scalars(stmt).all())


def get_default_connector(session: Session) -> Optional[ConnectorRow]:
    """Return the default (first) connector with training details, or None."""
    stmt = (
        _connector_select()
        .order_by(Connector.attached_at.asc(), Connector.id.asc())
        .limit(1)
    )
    row = session.execute(stmt).mappings().first()
    return ConnectorRow(**row) if row is not None else None


def _forecaster_select() -> Select:
    """Select the version, metrics and creation time of forecasters."""
    return select(Forecaster.version, Forecaster.metrics, Forecaster.created_at)


def list_forecasters(session: Session, connector_id: str) -> list[ForecasterRow]:
    """Return every forecaster of the connector that is loaded into Triton, newest version first."""
    cid_uuid = _as_uuid(connector_id)
    stmt = (
        _forecaster_select()
        .where(Forecaster.connector_id == cid_uuid)
        .order_by(Forecaster.version.desc())
    )
    rows = session.execute(stmt).mappings().all()
    return [ForecasterRow(**row) for row in rows]


def get_forecaster(session: Session, connector_id: str, version: int) -> Optional[ForecasterRow]:
    """Return one forecaster of a connector by version."""
    cid_uuid = _as_uuid(connector_id)
    stmt = _forecaster_select().where(
        Forecaster.connector_id == cid_uuid, Forecaster.version == version
    )
    row = session.execute(stmt).mappings().first()
    return ForecasterRow(**row) if row is not None else None


def get_newest_forecaster(session: Session, connector_id: str) -> Optional[ForecasterRow]:
    """Return the newest forecaster for the connector."""
    cid_uuid = _as_uuid(connector_id)
    stmt = (
        _forecaster_select()
        .where(Forecaster.connector_id == cid_uuid)
        .order_by(Forecaster.version.desc())
        .limit(1)
    )
    row = session.execute(stmt).mappings().first()
    return ForecasterRow(**row) if row is not None else None


def list_jobs(session: Session, connector_id: str) -> list[JobRow]:
    """Every job of the connector, newest first."""
    cid_uuid = _as_uuid(connector_id)
    stmt = (
        select(
            Job.seq,
            Job.status,
            Job.message,
            Job.result,
            Job.created_at,
            Job.started_at,
            Job.finished_at,
        )
        .where(Job.connector_id == cid_uuid)
        .order_by(Job.seq.desc())
    )
    rows = session.execute(stmt).mappings().all()
    return [JobRow(**row) for row in rows]
