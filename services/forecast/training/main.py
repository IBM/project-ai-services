"""
Entry point of the training worker: ``python -m forecast.training <job_file> <data_path> <output_dir>``.

Trains on the Parquet file that the API's data loader wrote (``data_path``), exports the model to
ONNX and writes the forecaster's Triton directory to ``output_dir`` on local scratch. The worker
does not read the source database, has no access to connector credentials and none to the
database, and never touches the repository volume: the API publishes the directory, loads it into
Triton, records it in ``forecasters`` and owns every database write. It learns what to train from
``job_file`` (``job.json``) and reports back in files next to it, ``result.json`` and
``failure.json`` (forecast.utils.job_files). AutoGluon works in a directory next to them, and the
worker removes nothing: the whole run directory belongs to the API, which deletes it after every
attempt, also when it had to kill the worker.

The failure message is the text of a ``TrainingFailure``, which is written for the user. Any other
exception is reported as a fixed text with its class name, and its details go to the log only,
because the message of a driver or library error can carry hosts, user names or data.

Exit codes (``forecast.errors``): 0 success, 1 failure that a retry may fix,
3 failure that would only repeat.
"""
import argparse
import os
import resource
import sys
from pathlib import Path

import pandas as pd

from forecast.errors import EXIT_FAILURE, EXIT_OK, EXIT_PERMANENT, PermanentFailure, TrainingFailure
from common.misc_utils import get_logger
from forecast.settings import settings
from forecast.training.model_dir import write_model_dir
from forecast.training.onnx_export import export
from forecast.training.training import train
from forecast.utils.job_files import MAX_MESSAGE_CHARS, TrainingJob, read_job, write_failure, write_result
from forecast.utils.names import forecaster_name

logger = get_logger("forecast_worker")

_UNEXPECTED_FAILURE = "Training failed with an unexpected {name}. The details are in the worker log."


def parse_args(argv: list[str]) -> argparse.Namespace:
    """Parse the command line of the worker."""
    parser = argparse.ArgumentParser(prog="forecast.training", description="Run one training job.")
    parser.add_argument("job_file", type=Path, help="The job.json that the API wrote. result.json and failure.json go next to it")
    parser.add_argument("data_path", type=Path, help="Parquet file with the input columns and the target")
    parser.add_argument("output_dir", type=Path, help="Empty or new directory for the Triton model directory")
    return parser.parse_args(argv)


def in_job_spelling(data: pd.DataFrame, job: TrainingJob) -> pd.DataFrame:
    """Rename the columns of ``data`` to the spelling of the training configuration.

    The loader matches the requested columns to the table's regardless of case and writes the
    table's spelling (IBM i upper-cases its names), while the configuration keeps what the user
    typed, and the forecast endpoint validates rows against that. An exact match wins, as in the
    loader; otherwise the match must be unique.
    """
    rename = {}
    for name in job.columns:
        if name in data.columns:
            continue
        found = [column for column in data.columns if column.upper() == name.upper()]
        if len(found) != 1:
            raise PermanentFailure(f"Column {name} is {'ambiguous' if found else 'missing'} in the data of table {job.source_table}")
        rename[found[0]] = name
    return data.rename(columns=rename)


def run(job_file: Path, data_path: Path, output_dir: Path) -> Path:
    """Train on ``data_path``, write the forecaster to ``output_dir`` and return it."""
    job = read_job(job_file)
    report_dir = job_file.parent

    data = in_job_spelling(pd.read_parquet(data_path), job)
    rows_read = len(data)
    write_result(report_dir, {"rows_read": rows_read})
    if rows_read == 0:
        raise PermanentFailure(f"Table {job.source_table} has no rows")

    # AutoGluon writes its models under report_dir/autogluon. A worker that is killed cannot clean
    # up after itself, so this must be a place that the API removes.
    trained = train(data, job, report_dir, settings.forecast.default_time_limit_seconds)
    model = export(trained.predictor, trained.encoding)

    write_model_dir(output_dir, forecaster_name(job.connector_id, job.job_id), model)
    write_result(
        report_dir,
        {
            "rows_read": rows_read,
            "algorithm": trained.algorithm,
            "validation_metric": trained.validation_metric,
            "validation_score": trained.validation_score,
            "test_score": trained.test_score,
            "training_seconds": trained.training_seconds,
        },
    )
    return output_dir


def peak_memory_mb() -> dict[str, float]:
    """Peak resident memory of this process and of its largest child, for sizing WORKER_MEMORY_LIMIT_MB."""
    scale = 1 if sys.platform == "darwin" else 1024  # ru_maxrss is bytes on macOS, KiB on Linux.
    return {
        who: round(resource.getrusage(which).ru_maxrss * scale / 2**20, 1)
        for who, which in (("self", resource.RUSAGE_SELF), ("largest_child", resource.RUSAGE_CHILDREN))
    }


def _record_failure(report_dir: Path, exc: Exception) -> None:
    message = str(exc) if isinstance(exc, TrainingFailure) else _UNEXPECTED_FAILURE.format(name=type(exc).__name__)
    try:
        write_failure(report_dir, message[:MAX_MESSAGE_CHARS])
    except Exception:
        logger.exception("Could not write the failure file in %s", report_dir)


def main(argv: list[str]) -> int:
    """Run the job named on the command line and return the exit code."""
    args = parse_args(argv)
    os.umask(0o022)  # Directories 0755 and files 0644, so Triton's user can read them.
    report_dir = args.job_file.parent
    try:
        published = run(args.job_file, args.data_path, args.output_dir)
    except PermanentFailure as exc:
        logger.error("Job file %s failed permanently: %s", args.job_file, exc, exc_info=exc if exc.__cause__ else False)
        _record_failure(report_dir, exc)
        return EXIT_PERMANENT
    except Exception as exc:
        logger.exception("Job file %s failed", args.job_file)
        _record_failure(report_dir, exc)
        return EXIT_FAILURE
    logger.info("Wrote %s, peak memory MB %s", published, peak_memory_mb())
    return EXIT_OK
