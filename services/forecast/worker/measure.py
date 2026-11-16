"""
Measure the memory and time of one training run, to set WORKER_MEMORY_LIMIT_MB.

Run it inside the target image (ppc64le), where the numbers are the ones that count:

    python -m forecast.worker.measure --rows 100000 1000000 --columns 20

Each size runs ``forecast.worker.main.run`` in its own process on a synthetic table (half
numeric and half text columns, a binary target), with the loader and the database replaced
by in-process fakes. Peak resident memory is the kernel's high-water mark for the process
and its children, so it includes AutoGluon's worker processes. Set WORKER_MEMORY_LIMIT_MB
above the largest peak at the largest table the service must train on, with margin, and
below the container limit.
"""
import argparse
import json
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from uuid import uuid4

import numpy as np
import pandas as pd

from forecast.worker.jobs import TrainingJob
from forecast.worker.main import peak_memory_mb, run


def synthetic_table(rows: int, columns: int) -> tuple[pd.DataFrame, list[dict]]:
    rng = np.random.default_rng(0)
    data = {}
    for i in range(columns):
        data[f"C{i}"] = rng.random(rows) * 100 if i % 2 == 0 else rng.choice(list("ABCDEFGH"), rows)
    df = pd.DataFrame(data)
    df["TARGET"] = np.where(df["C0"] + rng.normal(0, 20, rows) > 50, "Y", "N")
    return df, [{"name": name} for name in data]


class MemoryStore:
    """Keeps the results in memory in place of the jobs table."""

    def __init__(self, job: TrainingJob) -> None:
        self.job, self.results = job, []

    def read(self, job_id):
        return self.job

    def write_result(self, job_id, result):
        self.results.append(result)


def measure_one(rows: int, columns: int) -> dict:
    """Run one job in this process and return its measurements."""
    from forecast.settings import settings

    df, input_columns = synthetic_table(rows, columns)
    job = TrainingJob(1, uuid4(), "db2_for_i", {}, "S.T", "TARGET", input_columns)
    store = MemoryStore(job)

    def load(_job, path):
        df.to_parquet(path)
        return len(df)

    with tempfile.TemporaryDirectory() as volume:
        settings.forecast.model_repo_path = Path(volume)
        settings.forecast.scratch_dir = Path(volume) / "scratch"
        started = time.monotonic()
        run(1, store, load)
        elapsed = time.monotonic() - started
    return {
        "rows": rows,
        "columns": columns,
        "wall_seconds": round(elapsed, 1),
        "peak_mb": peak_memory_mb(),
        "result": store.results[-1],
    }


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="forecast.worker.measure", description=__doc__.split("\n\n")[0])
    parser.add_argument("--rows", type=int, nargs="+", required=True, help="Table sizes to measure")
    parser.add_argument("--columns", type=int, default=20, help="Input columns of the table")
    parser.add_argument("--one", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args(argv)

    if args.one:  # Child: measure one size and print it as JSON.
        print(json.dumps(measure_one(args.rows[0], args.columns)))
        return 0

    for rows in args.rows:  # A process per size, so each peak is that size's alone.
        child = subprocess.run(
            [sys.executable, "-m", "forecast.worker.measure", "--one", "--rows", str(rows),
             "--columns", str(args.columns)],
            capture_output=True, text=True,
        )
        lines = child.stdout.strip().splitlines()
        print(lines[-1] if child.returncode == 0 and lines else f"rows={rows} FAILED\n{child.stderr[-2000:]}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
