"""
Exceptions for data loading, Db2 connectivity, and Parquet storage.
"""
from typing import Optional


class DataLoaderError(Exception):
    """Base exception for all data loader operations."""

    is_permanent_failure: bool = False


class ConnectorNotFoundError(DataLoaderError):
    """Raised when the specified connector no longer exists."""

    is_permanent_failure: bool = True

    def __init__(self, connector_id: str):
        self.connector_id = connector_id
        super().__init__(f"Connector '{connector_id}' not found")


class ConnectorDecryptionError(DataLoaderError):
    """Raised when connector credentials cannot be decrypted.

    Guarantees that the error message contains only the connector identifier
    and never leaks decrypted secret credentials or raw ciphertext.
    """

    is_permanent_failure: bool = True

    def __init__(self, connector_id: str, reason: Optional[str] = None):
        self.connector_id = connector_id
        detail = f": {reason}" if reason else ""
        super().__init__(f"Connector '{connector_id}' credentials could not be decrypted{detail}")


class DBConnectionError(DataLoaderError):
    """Raised when the Db2 database host is unreachable or connection is lost (transient failure)."""

    is_permanent_failure: bool = False


class DBAuthError(DataLoaderError):
    """Raised when Db2 credentials or authentication fails."""

    is_permanent_failure: bool = True


class DBTableNotFoundError(DataLoaderError):
    """Raised when the specified Db2 table or schema cannot be found."""

    is_permanent_failure: bool = True


class DBColumnNotFoundError(DataLoaderError):
    """Raised when one or more required columns are missing in the Db2 table."""

    is_permanent_failure: bool = True


class RowLimitExceededError(DataLoaderError):
    """Raised when data extraction exceeds the configured maximum row limit.

    This is a permanent failure (will not succeed on retry without config changes).
    """

    is_permanent_failure: bool = True

    def __init__(self, row_count: int, max_training_rows: int, message: Optional[str] = None):
        self.row_count = row_count
        self.max_training_rows = max_training_rows
        msg = message or f"Extracted row count ({row_count}) exceeded maximum limit of {max_training_rows} rows"
        super().__init__(msg)


class ParquetWriteError(DataLoaderError):
    """Raised when an error occurs while writing data to a Parquet file."""

    is_permanent_failure: bool = True
