"""
One training run, from the APScheduler job to the forecaster in Triton.

``run_training`` is the function that APScheduler fires for a connector. Under an in-process lock,
so that only one training runs at a time, it takes the connector's QUEUED job, sets it RUNNING and
runs an *attempt*: the data loader subprocess, then the training worker subprocess, each in its own
process group with a time limit (the worker also with a memory limit). A failed attempt is run again
after TRAINING_RETRY_DELAY_SECONDS, up to TRAINING_MAX_ATTEMPTS in total, unless the process exited
with the code for a failure that would only repeat. Each attempt starts from an empty scratch
directory, so a retry reads the source again.

Only after a successful attempt does the API publish the model directory to the repository volume,
load it into Triton and record it in ``forecasters``. These steps run once and are not retried. The
job ends COMPLETED or FAILED, and an unexpected error ends it FAILED too, so a RUNNING row never
blocks later trainings.

The subprocesses write their own failure message to jobs.message. A process that was killed, or that
died before it could write, leaves none, so the API writes one. The processes inherit the API's
environment, except the secrets that they must not hold: the worker gets no database credentials of
the API and no encryption key, only the restricted role (WORKER_POSTGRES_USER/PASSWORD).
"""
import asyncio
import os
import shutil
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Optional

from sqlalchemy.exc import IntegrityError

from common.misc_utils import get_logger
from forecast.db import runs
from forecast.db.connection import get_db_session
from forecast.errors import EXIT_PERMANENT
from forecast.orchestrator.process import ProcessResult, run_process
from forecast.orchestrator.publish import publish
from forecast.settings import settings
from forecast.triton_client.triton import TritonLoadError, load_model, unload_model
from forecast.utils.names import forecaster_name

logger = get_logger("forecast_orchestrator")

# The loader's own client timeouts (connect and query) fire first and give a message. The time limit of
# its process is only the backstop behind them.
LOADER_GRACE_S = 60
# DEFAULT_TIME_LIMIT_SECONDS bounds AutoGluon's fit. Reading the data, the split and the ONNX export
# come on top of it.
WORKER_GRACE_S = 300

# The worker must not hold what the loader needs: the API's database login and the encryption key.
_NOT_FOR_THE_WORKER = ("POSTGRES_USER", "POSTGRES_PASSWORD", "DB_ENCRYPTION_KEY")

_MAX_MESSAGE_CHARS = 1000
_UNEXPECTED_FAILURE = "The training run failed with an unexpected {name}. The details are in the log."

# Named here so that a test can replace the wait between attempts without touching asyncio.
_sleep = asyncio.sleep

# One training at a time. The unique index on RUNNING jobs enforces it in the database as well.
_lock = asyncio.Lock()


@dataclass(frozen=True)
class AttemptFailure:
    """Why an attempt failed. ``message`` is written for the user."""

    message: str
    permanent: bool


async def _db(function: Callable[..., Any], *args) -> Any:
    """Run ``function(session, *args)`` in a thread, in one transaction, so the event loop is not blocked."""

    def call():
        with get_db_session() as session:
            return function(session, *args)

    return await asyncio.to_thread(call)


def _claim(session, connector_id: str) -> Optional[int]:
    """Set the connector's QUEUED job RUNNING and return it, or return None and log why not."""
    if runs.has_running_job(session, connector_id):
        logger.warning("Training of connector %s skipped: it already has a RUNNING job", connector_id)
        return None
    job_id = runs.find_queued_job(session, connector_id)
    if job_id is None:
        logger.warning("Training of connector %s skipped: it has no QUEUED job", connector_id)
        return None
    if not runs.mark_running(session, job_id):
        logger.warning("Training of connector %s skipped: job %s is no longer QUEUED", connector_id, job_id)
        return None
    return job_id


def _worker_env() -> dict[str, str]:
    return {key: value for key, value in os.environ.items() if key not in _NOT_FOR_THE_WORKER}


def _scratch(job_id: int) -> Path:
    return settings.forecast.scratch_dir / f"run-{job_id}"


def _describe(who: str, result: ProcessResult, limit_s: int, memory_limit_mb: Optional[int]) -> Optional[str]:
    """The message for a process that was stopped, or None when it exited by itself."""
    if result.timed_out:
        return f"The {who} was stopped after the time limit of {limit_s} s."
    if result.memory_exceeded:
        return f"The {who} was stopped after it used more than {memory_limit_mb} MB of memory."
    if result.exit_code < 0:
        return f"The {who} was killed by signal {-result.exit_code}."
    return None


async def _failed(
    job_id: int, who: str, result: ProcessResult, limit_s: int, memory_limit_mb: Optional[int] = None
) -> AttemptFailure:
    """Turn the end of a failed process into an AttemptFailure, and store a message if it left none."""
    stopped = _describe(who, result, limit_s, memory_limit_mb)
    permanent = result.exit_code == EXIT_PERMANENT and stopped is None
    # A process that was stopped could not have written the message that is stored.
    stored = None if stopped else await _db(runs.read_message, job_id)
    message = stopped or stored or f"The {who} exited with code {result.exit_code}."
    if not stored:
        await _db(runs.set_message, job_id, message)
    return AttemptFailure(message, permanent)


async def _attempt(job_id: int) -> Optional[AttemptFailure]:
    """Run the loader, then the worker, on an empty scratch directory. Return None on success."""
    config = settings.forecast
    scratch = _scratch(job_id)
    shutil.rmtree(scratch, ignore_errors=True)
    scratch.mkdir(mode=0o755, parents=True)
    await _db(runs.set_message, job_id, None)  # The message of the attempt before must not outlive it.

    data, model = scratch / "data.parquet", scratch / "model"
    loader_limit = config.connector_connect_timeout_s + config.connector_query_timeout_s + LOADER_GRACE_S
    loader = await run_process(
        [sys.executable, "-m", "forecast.data_loader", str(job_id), str(data)],
        name="data loader",
        timeout_s=loader_limit,
    )
    if not loader.ok:
        return await _failed(job_id, "data loader", loader, loader_limit)

    worker_limit = config.default_time_limit_seconds + WORKER_GRACE_S
    worker = await run_process(
        [sys.executable, "-m", "forecast.training", str(job_id), str(data), str(model)],
        name="training worker",
        timeout_s=worker_limit,
        memory_limit_mb=config.worker_memory_limit_mb,
        env=_worker_env(),
    )
    if not worker.ok:
        return await _failed(job_id, "training worker", worker, worker_limit, config.worker_memory_limit_mb)
    return None


async def _attempts(job_id: int) -> tuple[int, Optional[AttemptFailure]]:
    """Run attempts until one succeeds, one fails for good or TRAINING_MAX_ATTEMPTS is reached."""
    config = settings.forecast
    attempt = 0
    while True:
        attempt += 1
        failure = await _attempt(job_id)
        if failure is None or failure.permanent or attempt >= config.training_max_attempts:
            return attempt, failure
        logger.warning(
            "Job %s attempt %s of %s failed: %s Retrying in %s s",
            job_id, attempt, config.training_max_attempts, failure.message, config.training_retry_delay_seconds,
        )
        await _sleep(config.training_retry_delay_seconds)


async def _publish_and_load(connector_id: str, job_id: int, attempts: int) -> None:
    """Publish the worker's model directory, load it into Triton and record it. Runs once.

    A published directory that Triton does not load, or that cannot be recorded, is removed again, after
    a best-effort unload so that Triton does not keep serving a forecaster that has no record. A load
    that failed or timed out on the client may still have completed in Triton, hence the unload there too.

    Raises:
        TritonLoadError: Triton could not load the forecaster.
    """
    name = forecaster_name(connector_id, job_id)
    published = await asyncio.to_thread(
        publish, settings.forecast.model_repo_path, job_id, name, _scratch(job_id) / "model"
    )
    try:
        await load_model(name)
        # A cancel here cannot stop the thread that writes the row: it may still commit it after the
        # handler below has taken the forecaster away. Boot reconciliation (Story 10) removes such a row.
        await _db(runs.mark_completed, job_id, attempts)
    except BaseException:
        try:
            await unload_model(name)
        finally:  # Also when this wait is cancelled in turn, as at shutdown: the directory must go.
            shutil.rmtree(published, ignore_errors=True)
        raise
    # Retention (Story 10) is applied here, after the forecaster is loaded and recorded.


async def _fail(job_id: int, message: str, attempts: int) -> None:
    try:
        await _db(runs.mark_failed, job_id, message[:_MAX_MESSAGE_CHARS], attempts)
    except Exception:
        # The database is down. The job stays RUNNING until the boot recovery fails it.
        logger.exception("Could not mark job %s FAILED", job_id)


async def run_training(connector_id: str) -> None:
    """Run the QUEUED training job of the connector. This is the function that APScheduler fires."""
    async with _lock:
        try:
            job_id = await _db(_claim, connector_id)
        except IntegrityError:
            logger.warning("Training of connector %s skipped: another job is RUNNING", connector_id)
            return
        except Exception:
            # The job is still QUEUED, so nothing was started. Boot recovery (Story 8) requeues it.
            logger.exception("Training of connector %s skipped: its job could not be claimed", connector_id)
            return
        if job_id is None:
            return

        attempts = 0
        try:
            attempts, failure = await _attempts(job_id)
            if failure is not None:
                logger.error("Job %s failed after %s attempt(s): %s", job_id, attempts, failure.message)
                await _fail(job_id, failure.message, attempts)
                return
            await _publish_and_load(connector_id, job_id, attempts)
            logger.info("Job %s completed after %s attempt(s)", job_id, attempts)
        except TritonLoadError as exc:
            logger.error("Job %s failed: %s", job_id, exc)
            await _fail(job_id, str(exc), attempts)
        except Exception as exc:
            logger.exception("Job %s failed", job_id)
            await _fail(job_id, _UNEXPECTED_FAILURE.format(name=type(exc).__name__), attempts)
        finally:
            shutil.rmtree(_scratch(job_id), ignore_errors=True)
