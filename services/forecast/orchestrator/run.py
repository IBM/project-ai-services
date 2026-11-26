"""
One training run, from the APScheduler job to the forecaster in Triton.

``run_training`` is the function that APScheduler fires for a connector, for the first training and for
the scheduled ones. Under an in-process lock, so that only one training runs at a time (a run that fires
meanwhile is skipped), it inserts the connector's job as RUNNING and
runs an *attempt*: the data loader subprocess, then the training worker subprocess, each in its own
process group with a time limit (the worker also with a memory limit). A failed attempt is run again
after TRAINING_RETRY_DELAY_SECONDS, up to TRAINING_MAX_ATTEMPTS in total, unless the process exited
with the code for a failure that would only repeat. Each attempt starts from an empty scratch
directory, so a retry reads the source again.

Only after a successful attempt does the API publish the model directory to the repository volume,
load it into Triton and record it in ``forecasters``. These steps run once and are not retried. The
job ends COMPLETED or FAILED, and an unexpected error ends it FAILED too, so a RUNNING row never
blocks later trainings.

The data loader writes its own failure message to jobs.message. The training worker has no database
access: the API tells it what to train in ``job.json`` and, after it ends, copies what it reported in
``result.json`` and ``failure.json`` to jobs.result and jobs.message (forecast.utils.job_files). A
process that was killed, or that died before it could report, leaves no message, so the API writes
one. The loader inherits the API's environment. The worker inherits it without the database settings
and the encryption key.
"""
import asyncio
import os
import shutil
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Awaitable, Callable, Optional

from sqlalchemy.exc import IntegrityError

from common.misc_utils import get_logger
from forecast.db import runs
from forecast.db.connection import get_db_session
from forecast.errors import EXIT_PERMANENT, PermanentFailure
from forecast.orchestrator.process import ProcessResult, run_process
from forecast.orchestrator.publish import publish
from forecast.settings import settings
from forecast.triton_client.triton import TritonLoadError, load_model, unload_model
from forecast.utils import job_files
from forecast.utils.names import forecaster_name

logger = get_logger("forecast_orchestrator")

# The loader's own client timeouts (connect and query) fire first and give a message. The time limit of
# its process is only the backstop behind them.
LOADER_GRACE_S = 60
# DEFAULT_TIME_LIMIT_SECONDS bounds AutoGluon's fit. Reading the data, the split and the ONNX export
# come on top of it.
WORKER_GRACE_S = 300

# The worker has no use for the database and must not hold what the loader needs: the settings of the API's
# database (POSTGRES_*, DB_*) and the encryption key (DB_ENCRYPTION_KEY).
_NOT_FOR_THE_WORKER = ("POSTGRES_", "DB_")

_MAX_MESSAGE_CHARS = job_files.MAX_MESSAGE_CHARS
_UNEXPECTED_FAILURE = "The training run failed with an unexpected {name}. The details are in the log."

# Named here so that a test can replace the wait between attempts without touching asyncio.
_sleep = asyncio.sleep

# One training at a time. The unique index on RUNNING jobs enforces it in the database as well.
_lock = asyncio.Lock()
_SINGLE_RUNNING_INDEX = "idx_jobs_single_running"


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


def _worker_env() -> dict[str, str]:
    return {key: value for key, value in os.environ.items() if not key.startswith(_NOT_FOR_THE_WORKER)}


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
    job_id: int,
    who: str,
    result: ProcessResult,
    limit_s: int,
    reported: Callable[[], Awaitable[Optional[str]]],
    memory_limit_mb: Optional[int] = None,
) -> AttemptFailure:
    """Turn the end of a failed process into an AttemptFailure, and store its message in jobs.message.

    ``reported`` returns the message that the process left, or None: the loader leaves it in jobs.message,
    the worker in failure.json. A process that was stopped could not have left one that is trustworthy,
    and one that left none gets a message from the API.
    """
    stopped = _describe(who, result, limit_s, memory_limit_mb)
    permanent = result.exit_code == EXIT_PERMANENT and stopped is None
    left = None if stopped else await reported()
    message = stopped or left or f"The {who} exited with code {result.exit_code}."
    await _db(runs.set_message, job_id, message)
    return AttemptFailure(message, permanent)


async def _attempt(job_id: int) -> Optional[AttemptFailure]:
    """Run the loader, then the worker, on an empty scratch directory. Return None on success."""
    config = settings.forecast
    scratch = _scratch(job_id)
    shutil.rmtree(scratch, ignore_errors=True)
    scratch.mkdir(mode=0o755, parents=True)
    await _db(runs.start_attempt, job_id)  # The message and the result of the attempt before must not outlive it.

    data, model = scratch / "data.parquet", scratch / "model"
    loader_limit = config.connector_connect_timeout_s + config.connector_query_timeout_s + LOADER_GRACE_S
    loader = await run_process(
        [sys.executable, "-m", "forecast.data_loader", str(job_id), str(data)],
        name="data loader",
        timeout_s=loader_limit,
    )
    if not loader.ok:
        return await _failed(job_id, "data loader", loader, loader_limit, lambda: _db(runs.read_message, job_id))

    try:
        job = await _db(runs.read_training_job, job_id)  # The job or its connector may be gone by now.
    except PermanentFailure as exc:
        await _db(runs.set_message, job_id, str(exc))
        return AttemptFailure(str(exc), permanent=True)
    job_file = await asyncio.to_thread(job_files.write_job, scratch, job)

    worker_limit = config.default_time_limit_seconds + WORKER_GRACE_S
    worker = await run_process(
        [sys.executable, "-m", "forecast.training", str(job_file), str(data), str(model)],
        name="training worker",
        timeout_s=worker_limit,
        memory_limit_mb=config.worker_memory_limit_mb,
        env=_worker_env(),
    )
    # What the worker reported is kept whether it succeeded or not: a failed job shows the rows it read.
    # The files are read once the process is gone, so nothing is written to them any more.
    result = await asyncio.to_thread(job_files.read_result, scratch)
    if result:
        await _db(runs.set_result, job_id, result)
    elif worker.ok:
        # The job still completes, but without metrics: say so, because nothing else shows it.
        logger.warning("Job %s: the training worker exited 0 but left no usable %s", job_id, job_files.RESULT_FILE)
    if not worker.ok:
        return await _failed(
            job_id, "training worker", worker, worker_limit,
            lambda: asyncio.to_thread(job_files.read_failure, scratch), config.worker_memory_limit_mb,
        )
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
    """Train the connector: insert a RUNNING job for it and run it. This is the function that APScheduler fires.

    A run that fires while another training is running is skipped, not held back.
    """
    # No await between this check and the acquire, so no other run can take the lock in between.
    if _lock.locked():
        logger.warning("Training of connector %s skipped: a training is already running", connector_id)
        return
    async with _lock:
        try:
            job_id = await _db(runs.start_job, connector_id)
        except IntegrityError as exc:
            if _SINGLE_RUNNING_INDEX in str(exc.orig):
                logger.warning("Training of connector %s skipped: another job is RUNNING", connector_id)
            else:  # For example, the connector no longer exists.
                logger.exception("Training of connector %s skipped: its job could not be started", connector_id)
            return
        except Exception:
            # No job was inserted, so nothing was started. The connector keeps its status. Boot recovery
            # (Story 8) starts the first training again for a connector that is still ACCEPTED.
            logger.exception("Training of connector %s skipped: its job could not be started", connector_id)
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
