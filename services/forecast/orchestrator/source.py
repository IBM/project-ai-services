"""
The API's contact with the data loader.

The loader reads every row of the connector's table with the connector's decrypted
credentials, in batches, and writes them to a Parquet file. The orchestrator runs it before it
starts the worker and hands the file to the worker. This module maps the loader's errors to the
two kinds of failure, so nothing else depends on the loader.

It runs with the API's database role: the worker's role has no grant on ``connectors``.
"""
from pathlib import Path
from typing import Sequence

import pandas as pd

from forecast.errors import PermanentFailure, TransientFailure


def load_training_data(
    *,
    table: str,
    columns: Sequence[str],
    output_path: Path,
    connector_id: str,
    connector_type: str,
    connection_details: dict,
) -> pd.DataFrame:
    """Extract the whole ``table`` to ``output_path`` and return it.

    ``columns`` are the input columns, then the target, as named in the training configuration,
    which is what the forecast endpoint validates rows against. The loader matches them to the
    table's columns regardless of case and writes the table's own spelling, so they are renamed back.
    ``connection_details`` are the connector's, with the secret fields still encrypted.

    Raises:
        PermanentFailure: the loader reports a failure that a retry cannot fix, such as a
            connector that cannot be decrypted, or a table or column that does not exist.
        TransientFailure: any other loader failure, such as a dropped connection.
    """
    from forecast.data_loader import DataLoaderError, Db2DataLoader  # Only the orchestrator needs it.

    try:
        result = Db2DataLoader().extract_to_parquet(
            table=table,
            columns=list(columns),
            output_path=output_path,
            connector_id=connector_id,
            connector_type=connector_type,
            encrypted_connection_details=connection_details,
        )
    except DataLoaderError as exc:
        if exc.is_permanent_failure:
            # The loader's messages name the connector or table and hold no secret.
            raise PermanentFailure(str(exc)) from exc
        raise TransientFailure(str(exc)) from exc
    return pd.read_parquet(output_path).rename(columns=dict(zip(result.columns, columns)))
