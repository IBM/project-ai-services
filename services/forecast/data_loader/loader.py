"""
Orchestrates extracting streaming batches from Db2 and persisting to compressed Parquet.

Enforces column validation, streaming batching, and atomic file storage.
"""
from __future__ import annotations

import logging
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Optional, Sequence

from forecast.data_loader.db2_client import ColumnInfo, Db2Client
from forecast.data_loader.exceptions import (
    ConnectorDecryptionError,
    DBColumnNotFoundError,
    UnsupportedColumnTypeError,
)
from forecast.data_loader.parquet_store import (
    ParquetBatchWriter,
    build_arrow_schema,
)
from forecast.settings import settings
from forecast.utils.encryption import DecryptionError, decrypt_secrets

logger = logging.getLogger(__name__)


@dataclass
class ExtractResult:
    """Result summary of a completed Db2 to Parquet extraction."""

    output_path: str
    rows_written: int
    columns: List[str]
    bytes_written: int
    duration_seconds: float
    table: str
    batches_count: int = 0


def _resolve_columns(
    requested: Sequence[str], available: Sequence[ColumnInfo], table: str
) -> List[ColumnInfo]:
    """Map requested column names to catalogue columns.

    An exact match wins; otherwise the match is case-insensitive and must be unique.
    Db2 quoted identifiers are case-sensitive, so queries must use the catalogue spelling.
    """
    by_exact = {col.name: col for col in available}
    by_upper: Dict[str, List[ColumnInfo]] = {}
    for col in available:
        by_upper.setdefault(col.name.upper(), []).append(col)

    resolved: List[ColumnInfo] = []
    missing: List[str] = []
    for name in requested:
        if name in by_exact:
            resolved.append(by_exact[name])
            continue
        candidates = by_upper.get(name.upper(), [])
        if len(candidates) > 1:
            raise DBColumnNotFoundError(
                f"Column '{name}' is ambiguous in table '{table}': matches "
                f"{[c.name for c in candidates]}. Use the exact spelling."
            )
        if candidates:
            resolved.append(candidates[0])
        else:
            missing.append(name)

    if missing:
        raise DBColumnNotFoundError(
            f"Column(s) {missing} not found in table '{table}'. Available columns: {[c.name for c in available]}"
        )

    names = [col.name for col in resolved]
    duplicates = sorted({name for name in names if names.count(name) > 1})
    if duplicates:
        raise DBColumnNotFoundError(f"Column(s) {duplicates} requested more than once from table '{table}'")
    return resolved


# Mapepire returns these as a reference to a separate download ({'blob_url': ..., 'size': ...})
# and not as the value, so they cannot be written to Parquet.
_BINARY_TYPES = frozenset({"BINARY", "VARBINARY", "VARBIN", "BLOB"})


def _reject_binary_columns(columns: Sequence[ColumnInfo], table: str) -> None:
    """Fail before any query runs if a requested column has a binary type."""
    binary = [f"{col.name} ({col.type})" for col in columns if col.type.split("(")[0].strip().upper() in _BINARY_TYPES]
    if binary:
        raise UnsupportedColumnTypeError(
            f"Column(s) {binary} of table '{table}' have a binary type, which cannot be read through "
            "Mapepire. Leave them out of the columns to extract."
        )


def _as_bool(value: Any) -> bool:
    """Parse a bool that may have been stored as a string (decrypted fields are strings)."""
    if isinstance(value, str):
        return value.strip().lower() in ("true", "1", "yes")
    return bool(value)


def _client_from_config(connection_config: Dict[str, Any]) -> Db2Client:
    """Build a Db2Client from a connection config dictionary.

    Decrypted connector fields are strings, so numeric and boolean options are converted here.
    """
    return Db2Client(
        host=connection_config["host"],
        database=connection_config["database"],
        username=connection_config["username"],
        password=connection_config["password"],
        port=int(connection_config.get("port", settings.forecast.mapepire_port)),
        connect_timeout=int(
            connection_config.get("connect_timeout", settings.forecast.connector_connect_timeout_s)
        ),
        query_timeout=int(
            connection_config.get("query_timeout", settings.forecast.connector_query_timeout_s)
        ),
        ignore_unauthorized=_as_bool(connection_config.get("ignore_unauthorized", False)),
        ca_cert=connection_config.get("ca_cert"),
    )


class Db2DataLoader:
    """Coordinates data extraction from IBM Db2 directly into Parquet files on local disk."""

    def __init__(self, db2_client: Optional[Db2Client] = None) -> None:
        self.db2_client = db2_client

    def extract_to_parquet(
        self,
        table: str,
        columns: Sequence[str],
        output_path: str | Path,
        batch_size: int = settings.forecast.loader_chunk_rows,
        compression: str = settings.forecast.parquet_compression,
        connector_id: Optional[str] = None,
        encrypted_connection_details: Optional[Dict[str, Any]] = None,
    ) -> ExtractResult:
        """Stream table rows from Db2 in batches and write directly to a Parquet file.

        By default the loader's own ``db2_client`` is used. If
        ``encrypted_connection_details`` is given, the connector credentials are
        decrypted with DB_ENCRYPTION_KEY and a client is created (and closed) just
        for this extraction, ignoring ``db2_client``.

        Args:
            table: Source table as 'SCHEMA.TABLE' or 'TABLE' (resolved in the current schema).
            columns: List of columns to extract.
            output_path: Destination path for the .parquet file.
            batch_size: Number of rows to fetch per chunk from Db2.
            compression: Parquet compression codec (default from PARQUET_COMPRESSION).
            connector_id: Connector identifier, used in decryption errors.
            encrypted_connection_details: Connector connection details as stored, with the
                secret fields encrypted (see ``forecast.utils.encryption``).

        Returns:
            ExtractResult with extracted row count, duration, and file metadata.

        Raises:
            ConnectorDecryptionError: If connector credentials cannot be decrypted
                (names the connector without leaking credentials).
            ValueError: If neither a client nor connector details are available, or
                connector details are given without connector_id.
            InvalidTableReferenceError: If table is not 'TABLE' or 'SCHEMA.TABLE'.
            DBTableNotFoundError: If table does not exist or user lacks access.
            DBColumnNotFoundError: If any requested column is missing or requested twice.
            UnsupportedColumnTypeError: If any requested column has a binary type.
            DBConnectionError: If connection is dropped during extraction.
            ParquetWriteError: If Parquet writing fails.
        """
        if encrypted_connection_details is not None:
            if not connector_id:
                raise ValueError("connector_id is required with encrypted_connection_details")

            try:
                decrypted_config = decrypt_secrets(encrypted_connection_details)
            except DecryptionError as exc:
                raise ConnectorDecryptionError(connector_id) from exc

            with _client_from_config(decrypted_config) as client:
                return Db2DataLoader(client).extract_to_parquet(
                    table=table,
                    columns=columns,
                    output_path=output_path,
                    batch_size=batch_size,
                    compression=compression,
                )

        if self.db2_client is None:
            raise ValueError("Either a Db2Client or encrypted_connection_details must be provided")

        if not columns:
            raise DBColumnNotFoundError("At least one column must be specified for extraction")

        start_time = time.monotonic()
        target_path = Path(output_path).resolve()
        cols_requested = list(columns)

        logger.info(
            "Starting Db2 data extraction: table=%s columns=%s batch_size=%d output=%s",
            table,
            cols_requested,
            batch_size,
            target_path,
        )

        # 1. Fetch catalogue metadata and resolve the table and requested columns to the
        #    catalogue spelling (matching is case-insensitive, quoted queries are not)
        available_cols = self.db2_client.list_columns(table)
        resolved_cols = _resolve_columns(cols_requested, available_cols, table)
        _reject_binary_columns(resolved_cols, table)
        resolved_names = [col.name for col in resolved_cols]
        resolved_table = available_cols[0].table_ref or table

        # 2. Build Arrow schema with mapped data types
        arrow_schema = build_arrow_schema(resolved_names, {col.name: col.type for col in resolved_cols})

        # 3. Build optimized SELECT SQL statement
        query = self.db2_client.build_select_query(
            table=resolved_table,
            columns=resolved_names,
            batch_size=batch_size,
        )

        # 4. Stream batches and write to Parquet
        total_rows = 0
        batches_count = 0

        with ParquetBatchWriter(output_path=target_path, schema=arrow_schema, compression=compression) as writer:
            for batch in self.db2_client.stream_query_batches(query, batch_size=batch_size):
                batch_len = len(batch)
                writer.write_batch(batch)
                total_rows += batch_len
                batches_count += 1
                logger.debug(
                    "Extracted and wrote batch %d (%d rows, total %d so far)",
                    batches_count,
                    batch_len,
                    total_rows,
                )

        duration = time.monotonic() - start_time
        file_size = target_path.stat().st_size if target_path.exists() else 0

        logger.info(
            "Extraction completed successfully: %d rows written in %d batches (%.2f seconds, %d bytes) -> %s",
            total_rows,
            batches_count,
            duration,
            file_size,
            target_path,
        )

        return ExtractResult(
            output_path=str(target_path),
            rows_written=total_rows,
            columns=resolved_names,
            bytes_written=file_size,
            duration_seconds=duration,
            table=resolved_table,
            batches_count=batches_count,
        )
