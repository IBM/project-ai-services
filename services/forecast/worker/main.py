"""
Entry point of the training worker: ``python -m forecast.worker <job_id>``.

Reads the job, loads the data, trains with AutoGluon, exports the model to ONNX and
publishes the forecaster's directory. The worker never deletes from the repository
volume and never writes to ``forecasters``: the API loads the forecaster into Triton,
records it and owns every status change. Its only database writes are jobs.result and
jobs.message. It removes only its own local scratch directory.

Exit codes (``forecast.worker.errors``): 0 success, 1 failure that a retry may fix,
3 failure that would only repeat.
"""
import argparse
import os
import resource
import sys
import tempfile
from pathlib import Path
from typing import Callable

import pandas as pd

from common.misc_utils import get_logger
from forecast.settings import settings
from forecast.worker.errors import EXIT_FAILURE, EXIT_OK, EXIT_PERMANENT, PermanentFailure
from forecast.worker.jobs import JobStore, TrainingJob, worker_engine
from forecast.worker.onnx_export import export
from forecast.worker.publish import forecaster_name, publish
from forecast.worker.source import load_training_data
from forecast.worker.training import train

logger = get_logger("forecast_worker")

_MAX_MESSAGE_CHARS = 1000

Loader = Callable[[TrainingJob, Path], pd.DataFrame]


def parse_args(argv: list[str]) -> argparse.Namespace:
    """Parse the command line of the worker."""
    parser = argparse.ArgumentParser(prog="forecast.worker", description="Run one training job.")
    parser.add_argument("job_id", type=int, help="The jobs.seq of the training job to run")
    return parser.parse_args(argv)


def run(job_id: int, store: JobStore, loader: Loader = load_training_data) -> Path:
    """Run the job and return the directory of the published forecaster."""
    config = settings.forecast
    job = store.read(job_id)
    config.scratch_dir.mkdir(mode=0o755, parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory(dir=config.scratch_dir, prefix=f"job-{job_id}-") as scratch:
        data = loader(job, Path(scratch) / "data.parquet")
        rows_read = len(data)
        store.write_result(job_id, {"rows_read": rows_read})
        if rows_read == 0:
            raise PermanentFailure(f"Table {job.source_table} has no rows")

        trained = train(data, job, Path(scratch), config.default_time_limit_seconds)
        model = export(trained.predictor, trained.encoding)

    published = publish(
        config.model_repo_path, job_id, forecaster_name(job.connector_id, job.job_id), model
    )
    store.write_result(
        job_id,
        {
            "rows_read": rows_read,
            "algorithm": trained.algorithm,
            "validation_metric": trained.validation_metric,
            "validation_score": trained.validation_score,
            "training_seconds": trained.training_seconds,
        },
    )
    return published


def peak_memory_mb() -> dict[str, float]:
    """Peak resident memory of this process and of its largest child, for sizing WORKER_MEMORY_LIMIT_MB."""
    scale = 1 if sys.platform == "darwin" else 1024  # ru_maxrss is bytes on macOS, KiB on Linux.
    return {
        who: round(resource.getrusage(which).ru_maxrss * scale / 2**20, 1)
        for who, which in (("self", resource.RUSAGE_SELF), ("largest_child", resource.RUSAGE_CHILDREN))
    }


def _record_failure(store: JobStore, job_id: int, exc: Exception) -> None:
    message = str(exc) if isinstance(exc, PermanentFailure) else f"{type(exc).__name__}: {exc}"
    try:
        store.write_failure(job_id, message[:_MAX_MESSAGE_CHARS])
    except Exception:
        logger.exception("Could not store the failure of job %s", job_id)


def main(argv: list[str]) -> int:
    """Run the job named on the command line and return the exit code."""
    args = parse_args(argv)
    os.umask(0o022)  # Directories 0755 and files 0644, so Triton's user can read them.
    store = JobStore(worker_engine())
    try:
        published = run(args.job_id, store)
    except PermanentFailure as exc:
        logger.error("Job %s failed permanently: %s", args.job_id, exc)
        _record_failure(store, args.job_id, exc)
        return EXIT_PERMANENT
    except Exception as exc:
        logger.exception("Job %s failed", args.job_id)
        _record_failure(store, args.job_id, exc)
        return EXIT_FAILURE
    logger.info("Job %s published %s, peak memory MB %s", args.job_id, published.name, peak_memory_mb())
    return EXIT_OK
