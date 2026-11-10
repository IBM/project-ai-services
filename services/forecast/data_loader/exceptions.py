"""
Exceptions for data loading, Db2 connectivity, and Parquet storage.
"""
from typing import Optional


class DataLoaderError(Exception):
    """Base exception for all data loader operations."""


class DBConnectionError(DataLoaderError):
    """Raised when the Db2 database host is unreachable or connection is lost."""


class DBAuthError(DataLoaderError):
    """Raised when Db2 credentials or authentication fails."""


class DBTableNotFoundError(DataLoaderError):
    """Raised when the specified Db2 table or schema cannot be found."""


class DBColumnNotFoundError(DataLoaderError):
    """Raised when one or more required columns are missing in the Db2 table."""


class RowLimitExceededError(DataLoaderError):
    """Raised when data extraction exceeds the configured maximum row limit."""

    def __init__(self, row_count: int, max_training_rows: int, message: Optional[str] = None):
        self.row_count = row_count
        self.max_training_rows = max_training_rows
        msg = message or f"Extracted row count ({row_count}) exceeded maximum limit of {max_training_rows} rows"
        super().__init__(msg)


class ParquetWriteError(DataLoaderError):
    """Raised when an error occurs while writing data to a Parquet file."""
