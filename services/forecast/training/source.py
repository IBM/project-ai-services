"""
The worker's only contact with the data loader.

The loader reads every row of the connector's table with the connector's decrypted
credentials, in batches, and writes them to a Parquet file. This module reads that file
back and maps the loader's errors to the worker's two kinds of failure, so nothing else in
the worker depends on the loader.
"""
from pathlib import Path

import pandas as pd

from forecast.errors import PermanentFailure, TransientFailure
from forecast.db.jobs import TrainingJob


def load_training_data(job: TrainingJob, output_path: Path) -> pd.DataFrame:
    """Extract the job's whole table to ``output_path`` and return it.

    The columns of the frame are named as in the training configuration, which is what the
    forecast endpoint validates rows against. The loader matches them to the table's columns
    regardless of case and writes the table's own spelling, so they are renamed back.

    Raises:
        PermanentFailure: the loader reports a failure that a retry cannot fix, such as a
            connector that cannot be decrypted, or a table or column that does not exist.
        TransientFailure: any other loader failure, such as a dropped connection.
    """
    from forecast.data_loader import DataLoaderError, Db2DataLoader  # Only the worker needs it here.

    try:
        result = Db2DataLoader().extract_to_parquet(
            table=job.source_table,
            columns=job.columns,
            output_path=output_path,
            connector_id=str(job.connector_id),
            connector_type=job.connector_type,
            encrypted_connection_details=job.connection_details,
        )
    except DataLoaderError as exc:
        if exc.is_permanent_failure:
            # The loader's messages name the connector or table and hold no secret.
            raise PermanentFailure(str(exc)) from exc
        raise TransientFailure(str(exc)) from exc
    return pd.read_parquet(output_path).rename(columns=dict(zip(result.columns, job.columns)))
