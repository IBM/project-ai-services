"""
Data loader package for the Forecast service.

Handles database extraction (Db2 for i / Db2 LUW) in streaming chunks,
type conversion, and disk persistence to compressed Parquet files.
"""

from forecast.data_loader.config import (
    DEFAULT_CONNECT_TIMEOUT_S,
    DEFAULT_LOADER_CHUNK_ROWS,
    DEFAULT_MAPEPIRE_PORT,
    DEFAULT_MAX_TRAINING_ROWS,
    DEFAULT_PARQUET_COMPRESSION,
    DEFAULT_QUERY_TIMEOUT_S,
    DataLoaderConfig,
)
from forecast.data_loader.db2_client import ColumnInfo, Db2Client
from forecast.data_loader.exceptions import (
    DataLoaderError,
    DBAuthError,
    DBColumnNotFoundError,
    DBConnectionError,
    DBTableNotFoundError,
    ParquetWriteError,
    RowLimitExceededError,
)
from forecast.data_loader.loader import Db2DataLoader, ExtractResult, load_db2_to_parquet
from forecast.data_loader.parquet_store import (
    ParquetBatchWriter,
    build_arrow_schema,
    map_db_type_to_arrow,
)

__all__ = [
    "ColumnInfo",
    "DEFAULT_CONNECT_TIMEOUT_S",
    "DEFAULT_LOADER_CHUNK_ROWS",
    "DEFAULT_MAPEPIRE_PORT",
    "DEFAULT_MAX_TRAINING_ROWS",
    "DEFAULT_PARQUET_COMPRESSION",
    "DEFAULT_QUERY_TIMEOUT_S",
    "DataLoaderConfig",
    "DataLoaderError",
    "DBAuthError",
    "DBColumnNotFoundError",
    "DBConnectionError",
    "DBTableNotFoundError",
    "Db2Client",
    "Db2DataLoader",
    "ExtractResult",
    "ParquetBatchWriter",
    "ParquetWriteError",
    "RowLimitExceededError",
    "build_arrow_schema",
    "load_db2_to_parquet",
    "map_db_type_to_arrow",
]
