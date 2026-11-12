"""
Database access for connectors, their training details, jobs and forecasters.

Uses SQLAlchemy ORM models from ``forecast.db.models``. Every function takes
the session of the caller, which commits or rolls back.
"""
import uuid
from typing import Any, Optional

from sqlalchemy import func, select, text, update
from sqlalchemy.orm import Session

from forecast.db.models import Connector, Forecaster, Job, TrainingConfig

# A lock that a stuck transaction holds fails after this time instead of blocking forever.
LOCK_TIMEOUT_SQL = "SET LOCAL lock_timeout = '5s'"


def lock_connectors(session: Session) -> None:
    """Serialize connector creation: two transactions cannot both see an empty table."""
    session.execute(text(LOCK_TIMEOUT_SQL))
    session.execute(text("LOCK TABLE connectors IN SHARE ROW EXCLUSIVE MODE"))


def connector_exists(session: Session) -> bool:
    """Return True when a connector row exists."""
    return bool(session.scalar(select(func.count(Connector.id))) > 0)


def count_queued_jobs(session: Session) -> int:
    """Return the number of jobs waiting to run."""
    return session.scalar(select(func.count(Job.seq)).where(Job.status == "QUEUED")) or 0


def insert_connector(
    session: Session,
    *,
    connector_id: str,
    name: str,
    connector_type: str,
    config: dict,
    source_table: str,
    target_column: str,
    input_columns: list[dict],
    schedule: Optional[str],
) -> None:
    """Store the connector, its training details and the first training job."""
    cid_uuid = uuid.UUID(connector_id) if isinstance(connector_id, str) else connector_id
    connector = Connector(
        id=cid_uuid,
        name=name,
        type=connector_type,
        connection_details=config,
        status="ACCEPTED",
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


def _name_filter(name: Optional[str]):
    """Partial, case-insensitive match filter on Connector.name."""
    if name is None:
        return None
    escaped = name.replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")
    return Connector.name.ilike(f"%{escaped}%", escape="\\")


def list_connectors(
    session: Session, *, limit: int, offset: int, name: Optional[str]
) -> tuple[list[dict], int]:
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
    return [dict(row) for row in rows], total


def get_connector(
    session: Session, connector_id: str, *, for_update: bool = False
) -> Optional[dict]:
    """Return one connector with its training details, or None when it does not exist.

    With ``for_update``, the connector row is locked until the transaction ends, so a
    read-modify-write of its config cannot lose a concurrent update.
    """
    cid_uuid = uuid.UUID(connector_id) if isinstance(connector_id, str) else connector_id
    if for_update:
        session.execute(text(LOCK_TIMEOUT_SQL))

    stmt = (
        select(
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
            TrainingConfig.schedule,
        )
        .outerjoin(TrainingConfig, TrainingConfig.connector_id == Connector.id)
        .where(Connector.id == cid_uuid)
    )
    if for_update:
        stmt = stmt.with_for_update(of=Connector)

    row = session.execute(stmt).mappings().first()
    return dict(row) if row is not None else None


def update_connector(
    session: Session, connector_id: str, *, name: Optional[str], config: dict
) -> None:
    """Set the name when given, and store the config (already encrypted where needed)."""
    cid_uuid = uuid.UUID(connector_id) if isinstance(connector_id, str) else connector_id
    values = {"connection_details": config}
    if name is not None:
        values["name"] = name
    stmt = update(Connector).where(Connector.id == cid_uuid).values(**values)
    session.execute(stmt)


def list_ready_versions(session: Session, connector_id: str) -> list[int]:
    """Return the versions of the connector forecasters that are loaded into Triton, newest first."""
    cid_uuid = uuid.UUID(connector_id) if isinstance(connector_id, str) else connector_id
    stmt = (
        select(Forecaster.version)
        .where(Forecaster.connector_id == cid_uuid)
        .order_by(Forecaster.version.desc())
    )
    return list(session.scalars(stmt).all())


def list_jobs(session: Session, connector_id: str) -> list[dict]:
    """Every job of the connector, newest first."""
    cid_uuid = uuid.UUID(connector_id) if isinstance(connector_id, str) else connector_id
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
    return [dict(row) for row in rows]
