"""
The worker's access to PostgreSQL, through the restricted forecast_worker role.

The role can read the job's identity, the connector and the training configuration,
and update only jobs.result and jobs.message (see db/scripts/init_db.py). It cannot
read jobs.result, so every write replaces the whole value, and it has no grant on
forecasters.
"""
import os
from dataclasses import dataclass
from uuid import UUID

from sqlalchemy import create_engine, select, update
from sqlalchemy.engine import URL, Engine
from sqlalchemy.pool import NullPool

from forecast.db.models import Connector, Job, TrainingConfig
from forecast.settings import settings
from forecast.worker.errors import PermanentFailure


@dataclass(frozen=True)
class TrainingJob:
    """Everything the worker reads from the database for one job."""

    job_id: int  # jobs.seq, also the forecaster version
    connector_id: UUID
    connector_type: str
    connection_details: dict  # Secret fields are still encrypted.
    source_table: str
    target_column: str
    input_columns: list  # [{"name": ..., "type": ...}, ...]


def worker_engine() -> Engine:
    """Engine for the forecast_worker role. The worker is short-lived, so no pool."""
    url = URL.create(
        "postgresql",
        username=settings.forecast.worker_postgres_user,
        password=settings.forecast.worker_postgres_password.get_secret_value(),
        host=os.getenv("POSTGRES_HOST"),
        port=int(os.getenv("POSTGRES_PORT", "5432")),
        database=os.getenv("POSTGRES_DB"),
    )
    return create_engine(url, poolclass=NullPool)


class JobStore:
    """Reads a job and writes its result or failure message."""

    def __init__(self, engine: Engine) -> None:
        self._engine = engine

    def read(self, job_id: int) -> TrainingJob:
        """Read the job, its connector and its training configuration.

        Raises:
            PermanentFailure: the job, the connector or the training configuration
                does not exist. Names no secret.
        """
        query = (
            select(
                Job.seq,
                Job.connector_id,
                Connector.type,
                Connector.connection_details,
                TrainingConfig.source_table,
                TrainingConfig.target_column,
                TrainingConfig.input_columns,
            )
            .select_from(Job)
            .outerjoin(Connector, Connector.id == Job.connector_id)
            .outerjoin(TrainingConfig, TrainingConfig.connector_id == Job.connector_id)
            .where(Job.seq == job_id)
        )
        with self._engine.connect() as conn:
            row = conn.execute(query).first()
        if row is None:
            raise PermanentFailure(f"Job {job_id} does not exist")
        if row.connector_id is None or row.type is None:
            raise PermanentFailure(f"The connector of job {job_id} does not exist")
        if row.source_table is None:
            raise PermanentFailure(f"Connector {row.connector_id} has no training configuration")
        return TrainingJob(*row)

    def write_result(self, job_id: int, result: dict) -> None:
        """Replace jobs.result."""
        self._update(job_id, result=result)

    def write_failure(self, job_id: int, message: str) -> None:
        """Set jobs.message. jobs.result keeps what an earlier write stored."""
        self._update(job_id, message=message)

    def _update(self, job_id: int, **values) -> None:
        with self._engine.begin() as conn:
            conn.execute(update(Job).where(Job.seq == job_id).values(**values))
