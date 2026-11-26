"""
The files that the API and the training worker exchange in the scratch directory of a run.

The worker has no database access. The API tells it what to train in ``job.json`` and reads what it
reports from ``result.json`` and ``failure.json``, then writes ``jobs.result`` and ``jobs.message``
itself. The worker is the less trusted side: it runs AutoGluon and loads model code on table data. The API
therefore reads the two files it gets back with a size limit, takes only the keys that it knows
and checks their types, and caps the length of the message. Anything else in them is ignored.

Every file is written whole or not at all (a temporary file in the same directory, then a rename),
so a worker that is killed in the middle of a write leaves the previous content or none.
"""
import json
import math
import os
import stat
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Optional
from uuid import UUID

from forecast.errors import PermanentFailure

JOB_FILE = "job.json"
RESULT_FILE = "result.json"
FAILURE_FILE = "failure.json"

# The job file lists up to 1000 column names of up to 128 characters. The files that the worker writes
# hold a few numbers or one message.
MAX_JOB_FILE_BYTES = 1024 * 1024
MAX_REPORT_FILE_BYTES = 64 * 1024

MAX_MESSAGE_CHARS = 1000
_MAX_TEXT_CHARS = 100

_NUMBER = (int, float)
# The keys of jobs.result that the worker may set, and the types that they may have.
RESULT_KEYS: dict[str, tuple[type, ...]] = {
    "rows_read": (int,),
    "algorithm": (str,),
    "validation_metric": (str,),
    "validation_score": _NUMBER,
    "test_score": _NUMBER,
    "training_seconds": _NUMBER,
}


@dataclass(frozen=True)
class TrainingJob:
    """What the worker needs to know about one job."""

    job_id: int  # jobs.seq, also the forecaster version
    connector_id: UUID
    source_table: str
    target_column: str
    input_names: list[str]

    @property
    def columns(self) -> list[str]:
        """The input columns, then the target."""
        return [*self.input_names, self.target_column]


def write_json(path: Path, content: dict) -> None:
    """Write ``content`` to ``path`` whole or not at all."""
    handle, temporary = tempfile.mkstemp(dir=path.parent, prefix=f".{path.name}.")
    try:
        with os.fdopen(handle, "w") as file:
            json.dump(content, file)
        os.replace(temporary, path)
    except BaseException:
        Path(temporary).unlink(missing_ok=True)
        raise


def read_json(path: Path, max_bytes: int) -> Optional[dict]:
    """Return the JSON object in ``path``, or None when the file is missing, not a regular file, too large or invalid.

    A symbolic link is not followed, and a named pipe is not opened for reading, so a file that the worker
    replaced cannot make the API read something else or wait for it.
    """
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except OSError:
        return None
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            return None
        with os.fdopen(descriptor, "rb", closefd=False) as file:
            raw = file.read(max_bytes + 1)
    finally:
        os.close(descriptor)
    if len(raw) > max_bytes:
        return None
    try:
        content = json.loads(raw)
    except ValueError:
        return None
    return content if isinstance(content, dict) else None


# ---------------------------------------------------------------------------
# job.json: from the API to the worker
# ---------------------------------------------------------------------------


def write_job(directory: Path, job: TrainingJob) -> Path:
    """Write the job file into ``directory`` and return its path."""
    path = directory / JOB_FILE
    write_json(
        path,
        {
            "job_id": job.job_id,
            "connector_id": str(job.connector_id),
            "source_table": job.source_table,
            "target_column": job.target_column,
            "input_names": job.input_names,
        },
    )
    return path


def read_job(path: Path) -> TrainingJob:
    """Read the job file.

    Raises:
        PermanentFailure: the file is missing or does not describe a job. The API writes it, so a
            retry would find the same file.
    """
    content = read_json(path, MAX_JOB_FILE_BYTES)
    try:
        names = content["input_names"]
        if not (isinstance(names, list) and all(isinstance(name, str) for name in names)):
            raise TypeError
        job_id = content["job_id"]
        if type(job_id) is not int:
            raise TypeError
        return TrainingJob(
            job_id=job_id,
            connector_id=UUID(content["connector_id"]),
            source_table=_text(content["source_table"]),
            target_column=_text(content["target_column"]),
            input_names=names,
        )
    except (TypeError, KeyError, ValueError):
        raise PermanentFailure("The job file of the training worker is missing or invalid") from None


def _text(value: Any) -> str:
    if not isinstance(value, str):
        raise TypeError
    return value


# ---------------------------------------------------------------------------
# result.json and failure.json: from the worker to the API
# ---------------------------------------------------------------------------


def write_result(directory: Path, result: dict) -> None:
    """Replace the result file of the run in ``directory``."""
    write_json(directory / RESULT_FILE, result)


def read_result(directory: Path) -> dict:
    """Return the result that the worker wrote: the known keys with a value of the right type, else {}."""
    content = read_json(directory / RESULT_FILE, MAX_REPORT_FILE_BYTES) or {}
    result = {}
    for key, types in RESULT_KEYS.items():
        value = content.get(key)
        if isinstance(value, bool) or not isinstance(value, types):
            continue
        if isinstance(value, float) and not math.isfinite(value):
            continue
        result[key] = value[:_MAX_TEXT_CHARS] if isinstance(value, str) else value
    return result


def write_failure(directory: Path, message: str) -> None:
    """Write the failure message of the run in ``directory``."""
    write_json(directory / FAILURE_FILE, {"message": message})


def read_failure(directory: Path) -> Optional[str]:
    """Return the failure message that the worker wrote, cut to ``MAX_MESSAGE_CHARS``, or None."""
    content = read_json(directory / FAILURE_FILE, MAX_REPORT_FILE_BYTES) or {}
    message = content.get("message")
    if not isinstance(message, str) or not message.strip():
        return None
    return message[:MAX_MESSAGE_CHARS]
