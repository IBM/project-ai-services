"""
Entry point of the data loader: ``python -m forecast.data_loader <job_id> <output_path>``.

The API runs it as a subprocess before the training worker. It reads the job, the training
configuration and the connector with the API's database role, extracts the whole source table to
a Parquet file at ``output_path`` and exits. The worker reads that file. Running it apart from the
worker keeps the connector's credentials out of the training process, and a slow or hung source
cannot use the training time limit or block the API's event loop.

A failure message for the user goes to jobs.message. It names the connector, table or columns and
never a secret. The loader's errors are written that way: the text of the driver or of the library
that failed is kept off the message and stays in the exception's cause. Any other exception is stored
as a fixed text with its class name. The details, including every cause, go to the log only, because
the text of a driver error can carry hosts, user names or rows.

Exit codes (``forecast.errors``), the same as the worker's: 0 success, 1 failure that a retry
may fix, 3 failure that would only repeat. The output file is written whole or not at all.
"""
import argparse
from pathlib import Path
from typing import Callable

from sqlalchemy import select, update
from sqlalchemy.orm import Session

from common.misc_utils import get_logger
from forecast.db.models import Connector, Job, TrainingConfig
from forecast.errors import EXIT_FAILURE, EXIT_OK, EXIT_PERMANENT, PermanentFailure, TrainingFailure
from forecast.orchestrator.source import load_training_data

logger = get_logger("forecast_loader")

_MAX_MESSAGE_CHARS = 1000
_UNEXPECTED_FAILURE = "Reading the source failed with an unexpected {name}. The details are in the loader log."


def parse_args(argv: list[str]) -> argparse.Namespace:
    """Parse the command line of the loader."""
    parser = argparse.ArgumentParser(prog="forecast.data_loader", description="Read the source table of one job.")
    parser.add_argument("job_id", type=int, help="The jobs.seq of the training job to read the data for")
    parser.add_argument("output_path", type=Path, help="Parquet file to write")
    return parser.parse_args(argv)


def read_job(session: Session, job_id: int) -> tuple[TrainingConfig, Connector]:
    """Read the training configuration and the connector of the job.

    Raises:
        PermanentFailure: the job, its connector or its training configuration does not exist.
    """
    row = session.execute(
        select(Job.connector_id, TrainingConfig, Connector)
        .select_from(Job)
        .outerjoin(TrainingConfig, TrainingConfig.connector_id == Job.connector_id)
        .outerjoin(Connector, Connector.id == Job.connector_id)
        .where(Job.seq == job_id)
    ).first()
    if row is None:
        raise PermanentFailure(f"Job {job_id} does not exist")
    connector_id, config, connector = row
    if connector_id is None or connector is None:  # The connector was deleted after the job was queued.
        raise PermanentFailure(f"The connector of job {job_id} does not exist")
    if config is None:
        raise PermanentFailure(f"Connector {connector_id} has no training configuration")
    return config, connector


def run(job_id: int, session: Session, output_path: Path) -> int:
    """Write the table of the job to ``output_path`` and return the number of rows read."""
    config, connector = read_job(session, job_id)
    columns = [*(column["name"] for column in config.input_columns), config.target_column]
    # The decrypted credentials are held by the loader only. The session is not needed while it reads.
    connection_details = dict(connector.connection_details)
    connector_id = str(connector.id)
    session.close()

    output_path.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
    result = load_training_data(
        table=config.source_table,
        columns=columns,
        output_path=output_path,
        connector_id=connector_id,
        connection_details=connection_details,
    )
    return result.rows_written


def _record_failure(session_factory: Callable[[], Session], job_id: int, exc: Exception) -> None:
    message = str(exc) if isinstance(exc, TrainingFailure) else _UNEXPECTED_FAILURE.format(name=type(exc).__name__)
    try:
        with session_factory() as session, session.begin():
            session.execute(
                update(Job).where(Job.seq == job_id).values(message=message[:_MAX_MESSAGE_CHARS]),
                execution_options={"synchronize_session": False},
            )
    except Exception:
        logger.exception("Could not store the failure of job %s", job_id)


def main(argv: list[str], session_factory: Callable[[], Session] | None = None) -> int:
    """Read the source of the job named on the command line and return the exit code."""
    args = parse_args(argv)
    if session_factory is None:
        from forecast.db.connection import SessionLocal, check_db_configured

        check_db_configured()
        session_factory = SessionLocal
    try:
        with session_factory() as session:
            rows = run(args.job_id, session, args.output_path)
    except PermanentFailure as exc:
        logger.error("Job %s: the source cannot be read: %s", args.job_id, exc, exc_info=exc)
        _record_failure(session_factory, args.job_id, exc)
        return EXIT_PERMANENT
    except Exception as exc:
        logger.exception("Job %s: reading the source failed", args.job_id)
        _record_failure(session_factory, args.job_id, exc)
        return EXIT_FAILURE
    logger.info("Job %s: wrote %s rows to %s", args.job_id, rows, args.output_path)
    return EXIT_OK

