"""
The training worker's access to PostgreSQL, through the restricted forecast_worker role.

The role can read the job's identity and the training configuration, and update only
jobs.result and jobs.message (see db/scripts/init_db.py). It cannot read jobs.result, so
every write replaces the whole value, and it has no grant on connectors, which hold the
source credentials, or on forecasters. Reading the source is the orchestrator's job.
"""
import os
from dataclasses import dataclass
from uuid import UUID

from sqlalchemy import create_engine, select, update
from sqlalchemy.engine import URL, Engine
from sqlalchemy.orm import Session, load_only
from sqlalchemy.pool import NullPool

from forecast.errors import PermanentFailure
from forecast.db.models import Job, TrainingConfig
from forecast.settings import settings


@dataclass(frozen=True)
class TrainingJob:
    """The rows the worker reads for one job, with the fields it uses."""

    job: Job
    config: TrainingConfig

    @property
    def job_id(self) -> int:
        """jobs.seq, also the forecaster version."""
        return self.job.seq

    @property
    def connector_id(self) -> UUID:
        return self.job.connector_id

    @property
    def source_table(self) -> str:
        return self.config.source_table

    @property
    def target_column(self) -> str:
        return self.config.target_column

    @property
    def input_names(self) -> list[str]:
        return [column["name"] for column in self.config.input_columns]

    @property
    def columns(self) -> list[str]:
        """The input columns, then the target."""
        return [*self.input_names, self.target_column]


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
        """Read the job and its training configuration.

        Raises:
            PermanentFailure: the job, its connector or the training configuration
                does not exist. Names no secret.
        """
        query = (
            select(Job, TrainingConfig)
            # The role may read only these columns of jobs.
            .options(load_only(Job.seq, Job.connector_id))
            .select_from(Job)
            .outerjoin(TrainingConfig, TrainingConfig.connector_id == Job.connector_id)
            .where(Job.seq == job_id)
        )
        with Session(self._engine) as session:
            row = session.execute(query).first()
        if row is None:
            raise PermanentFailure(f"Job {job_id} does not exist")
        job, config = row
        if job.connector_id is None:  # The connector was deleted after the job was queued.
            raise PermanentFailure(f"The connector of job {job_id} does not exist")
        if config is None:
            raise PermanentFailure(f"Connector {job.connector_id} has no training configuration")
        return TrainingJob(job, config)

    def write_result(self, job_id: int, result: dict) -> None:
        """Replace jobs.result."""
        self._update(job_id, result=result)

    def write_failure(self, job_id: int, message: str) -> None:
        """Set jobs.message. jobs.result keeps what an earlier write stored."""
        self._update(job_id, message=message)

    def clear_failure(self, job_id: int) -> None:
        """Empty jobs.message, so a retry does not keep the message of the attempt before it."""
        self._update(job_id, message=None)

    def _update(self, job_id: int, **values) -> None:
        with Session(self._engine) as session, session.begin():
            session.execute(
                update(Job).where(Job.seq == job_id).values(**values),
                execution_options={"synchronize_session": False},
            )
