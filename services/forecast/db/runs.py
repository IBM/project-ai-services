"""
The API's database access for a training run, with the API's role.

The training worker has no database access at all. Everything about a run belongs to the API: the
status of the job and of the connector, the timestamps, ``jobs.result`` and ``jobs.message`` (which
the API copies from the files that the worker wrote, see forecast.utils.job_files), the number of
attempts and the ``forecasters`` row.
"""
from datetime import datetime, timezone
from typing import Optional
from uuid import UUID

from sqlalchemy import null, select, update
from sqlalchemy.orm import Session

from forecast.db.models import Connector, Forecaster, Job, TrainingConfig
from forecast.errors import PermanentFailure
from forecast.utils.job_files import TrainingJob


def _now() -> datetime:
    return datetime.now(timezone.utc)


def find_queued_job(session: Session, connector_id: UUID | str) -> Optional[int]:
    """Return the ``seq`` of the oldest QUEUED job of the connector, or None."""
    return session.scalar(
        select(Job.seq).where(Job.connector_id == connector_id, Job.status == "QUEUED").order_by(Job.seq).limit(1)
    )


def has_running_job(session: Session, connector_id: UUID | str) -> bool:
    """Whether the connector already has a RUNNING job."""
    running = select(Job.seq).where(Job.connector_id == connector_id, Job.status == "RUNNING").limit(1)
    return session.scalar(running) is not None


def mark_running(session: Session, job_id: int) -> bool:
    """Set a QUEUED job RUNNING and its connector TRAINING. Return False when the job is not QUEUED.

    Raises:
        sqlalchemy.exc.IntegrityError: another job is RUNNING (idx_jobs_single_running).
    """
    connector_id = session.execute(
        update(Job)
        .where(Job.seq == job_id, Job.status == "QUEUED")
        .values(status="RUNNING", started_at=_now(), message=None)
        .returning(Job.connector_id)
    ).scalar_one_or_none()
    if connector_id is None:
        return False
    session.execute(update(Connector).where(Connector.id == connector_id).values(status="TRAINING", message=None))
    return True


def read_training_job(session: Session, job_id: int) -> TrainingJob:
    """Read what the worker needs to know about the job. The connector's credentials are not read.

    Raises:
        PermanentFailure: the job, its connector or its training configuration does not exist.
    """
    row = session.execute(
        select(Job.connector_id, TrainingConfig)
        .select_from(Job)
        .outerjoin(TrainingConfig, TrainingConfig.connector_id == Job.connector_id)
        .where(Job.seq == job_id)
    ).first()
    if row is None:
        raise PermanentFailure(f"Job {job_id} does not exist")
    connector_id, config = row
    if connector_id is None:  # The connector was deleted after the job was queued.
        raise PermanentFailure(f"The connector of job {job_id} does not exist")
    if config is None:
        raise PermanentFailure(f"Connector {connector_id} has no training configuration")
    return TrainingJob(
        job_id=job_id,
        connector_id=connector_id,
        source_table=config.source_table,
        target_column=config.target_column,
        input_names=[column["name"] for column in config.input_columns],
    )


def start_attempt(session: Session, job_id: int) -> None:
    """Empty jobs.message and jobs.result, so that an attempt does not keep what the one before it left."""
    session.execute(update(Job).where(Job.seq == job_id).values(message=None, result=null()))


def set_result(session: Session, job_id: int, result: dict) -> None:
    """Replace jobs.result with what the worker reported. ``attempts`` is added when the job ends."""
    session.execute(update(Job).where(Job.seq == job_id).values(result=result))


def read_message(session: Session, job_id: int) -> Optional[str]:
    """Return jobs.message, which the loader or the worker wrote when an attempt failed."""
    return session.scalar(select(Job.message).where(Job.seq == job_id))


def set_message(session: Session, job_id: int, message: Optional[str]) -> None:
    """Replace jobs.message."""
    session.execute(update(Job).where(Job.seq == job_id).values(message=message))


def _finish(session: Session, job: Job, status: str, message: Optional[str], attempts: int) -> Optional[Connector]:
    # The API copied the worker's result into jobs.result. Readers tolerate missing keys.
    job.result = {**(job.result or {}), "attempts": attempts}
    job.status = status
    job.message = message
    job.finished_at = _now()
    return session.get(Connector, job.connector_id) if job.connector_id else None


def mark_completed(session: Session, job_id: int, attempts: int) -> None:
    """Record the forecaster that the job trained, then set the job COMPLETED and its connector TRAINED.

    Raises:
        sqlalchemy.exc.IntegrityError: the forecaster is already recorded.
    """
    job = session.get(Job, job_id)
    result = job.result or {}
    # The shape of the API's samples: the validation metric (the worker calls it validation_metric), its score
    # and the training time. A key that the result lacks is left out.
    wanted = {"metric": "validation_metric", "validation_score": "validation_score", "training_seconds": "training_seconds"}
    session.add(
        Forecaster(
            connector_id=job.connector_id,
            job_id=job_id,
            version=job_id,
            metrics={name: result[key] for name, key in wanted.items() if key in result},
        )
    )
    connector = _finish(session, job, "COMPLETED", None, attempts)
    if connector is not None:
        connector.status = "TRAINED"
        connector.message = None
        connector.last_trained_at = job.finished_at


def mark_failed(session: Session, job_id: int, message: str, attempts: int) -> None:
    """Set the job FAILED with ``message``, and its connector FAILED."""
    job = session.get(Job, job_id)
    connector = _finish(session, job, "FAILED", message, attempts)
    if connector is not None:
        connector.status = "FAILED"
        connector.message = message
