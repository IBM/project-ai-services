"""
Data loader package for the Forecast service.

Handles database extraction (Db2 for i / Db2 LUW) in streaming chunks,
type conversion, and disk persistence to compressed Parquet files.
"""

from forecast.data_loader.config import (
    DEFAULT_CONNECT_TIMEOUT_S,
    DEFAULT_LOADER_CHUNK_ROWS,
    DEFAULT_MAPEPIRE_PORT,
    DEFAULT_PARQUET_COMPRESSION,
    DEFAULT_QUERY_TIMEOUT_S,
    DataLoaderConfig,
)
from forecast.data_loader.db2_client import ColumnInfo, Db2Client
from forecast.data_loader.exceptions import (
    ConnectorDecryptionError,
    ConnectorNotFoundError,
    DataLoaderError,
    DBAuthError,
    DBColumnNotFoundError,
    DBConnectionError,
    DBTableNotFoundError,
    InvalidTableReferenceError,
    ParquetWriteError,
)
from forecast.data_loader.loader import (
    Db2DataLoader,
    ExtractResult,
)
from forecast.data_loader.parquet_store import (
    ParquetBatchWriter,
    build_arrow_schema,
    map_db_type_to_arrow,
)

__all__ = [
    "ColumnInfo",
    "DEFAULT_CONNECT_TIMEOUT_S",
    "DEFAULT_LOADER_CHUNK_ROWS",
    "ConnectorDecryptionError",
    "ConnectorNotFoundError",
    "DEFAULT_MAPEPIRE_PORT",
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
    "InvalidTableReferenceError",
    "ParquetBatchWriter",
    "ParquetWriteError",
    "build_arrow_schema",
    "map_db_type_to_arrow",
]
