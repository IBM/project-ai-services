"""
The worker's only contact with the data loader.

The loader reads the connector's table with the connector's decrypted credentials and
streams it to a Parquet file. This module maps its errors to the worker's two kinds of
failure, so nothing else in the worker depends on the loader.
"""
from pathlib import Path

from forecast.settings import settings
from forecast.worker.errors import PermanentFailure
from forecast.worker.jobs import TrainingJob


def load_training_data(job: TrainingJob, output_path: Path) -> int:
    """Stream the job's table to ``output_path`` and return the number of rows read.

    Raises:
        PermanentFailure: the connector is missing or cannot be decrypted, the table
            or a column does not exist, or the table has more than MAX_TRAINING_ROWS rows.
    """
    from forecast import data_loader as loader  # Imported here: it needs the Db2 client.

    config = settings.forecast
    columns = [column["name"] for column in job.input_columns] + [job.target_column]
    try:
        result = loader.Db2DataLoader().extract_to_parquet(
            table=job.source_table,
            columns=columns,
            output_path=output_path,
            batch_size=config.loader_chunk_rows,
            max_training_rows=config.max_training_rows,
            connector_id=str(job.connector_id),
            connector_type=job.connector_type,
            encrypted_connection_details=job.connection_details,
        )
    except (
        loader.ConnectorNotFoundError,
        loader.ConnectorDecryptionError,
        loader.DBTableNotFoundError,
        loader.DBColumnNotFoundError,
        loader.InvalidTableReferenceError,
        loader.RowLimitExceededError,
    ) as exc:
        # The loader's messages name the connector or table and hold no secret.
        raise PermanentFailure(str(exc)) from exc
    return result.rows_written
