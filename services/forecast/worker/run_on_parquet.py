"""
Run the worker's own logic (train -> ONNX export -> atomic publish) on an existing Parquet
file, with the database and the data loader faked. The file stands in for what the loader
wrote, so this tests the worker alone.

    cd services
    PYTHONPATH=. python -m forecast.worker.run_on_parquet \
        --parquet /path/to/extract.parquet --target TARGET_COLUMN [--inputs A B C]

The forecaster is published under MODEL_REPO_PATH and the scratch files go under
SCRATCH_DIR. Set both to a directory you can delete.
"""
import argparse
import json
import stat
import sys
from pathlib import Path
from uuid import uuid4

import pandas as pd
import pyarrow.parquet as pq

from forecast.settings import settings
from forecast.worker.jobs import TrainingJob
from forecast.worker.main import peak_memory_mb, run
from forecast.worker.measure import MemoryStore


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="forecast.worker.run_on_parquet", description=__doc__.split("\n\n")[0])
    parser.add_argument("--parquet", required=True, type=Path)
    parser.add_argument("--target", required=True, help="Target column of the file")
    parser.add_argument("--inputs", nargs="*", help="Input columns. Default: every other column")
    args = parser.parse_args(argv)

    columns = pq.read_schema(args.parquet).names
    inputs = args.inputs or [c for c in columns if c != args.target]
    job = TrainingJob(
        1, uuid4(), "db2_for_i", {}, "SCHEMA.TABLE", args.target, [{"name": c} for c in inputs]
    )
    store = MemoryStore(job)
    settings.forecast.model_repo_path.mkdir(parents=True, exist_ok=True)  # A mounted volume in production.

    def load(_job, _output_path):
        return pd.read_parquet(args.parquet)

    published = run(job.job_id, store, load)

    print("published    :", published)
    for path in [published, *sorted(published.rglob("*"))]:
        print(f"  {stat.filemode(path.stat().st_mode)}  {path.relative_to(published.parent)}")
    print("jobs.result  :", json.dumps(store.results[-1], indent=2))
    print("peak memory  :", peak_memory_mb())
    print("staging left :", list((settings.forecast.model_repo_path / ".staging").iterdir()))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
