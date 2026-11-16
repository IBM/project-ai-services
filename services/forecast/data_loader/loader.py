"""
Orchestrates extracting streaming batches from Db2 and persisting to compressed Parquet.

Enforces column validation, streaming batching, hard row limits, and atomic file storage.
"""
from __future__ import annotations

import logging
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Optional, Sequence

from forecast.data_loader.config import (
    DEFAULT_LOADER_CHUNK_ROWS,
    DEFAULT_MAPEPIRE_PORT,
    DEFAULT_MAX_TRAINING_ROWS,
    DEFAULT_PARQUET_COMPRESSION,
    DataLoaderConfig,
)
from forecast.data_loader.db2_client import Db2Client
from forecast.data_loader.exceptions import (
    ConnectorDecryptionError,
    DBColumnNotFoundError,
    RowLimitExceededError,
)
from forecast.data_loader.parquet_store import (
    ParquetBatchWriter,
    build_arrow_schema,
)

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


def _client_from_config(connection_config: Dict[str, Any]) -> Db2Client:
    """Build a Db2Client from a connection config dictionary."""
    return Db2Client(
        host=connection_config["host"],
        database=connection_config["database"],
        username=connection_config["username"],
        password=connection_config["password"],
        port=connection_config.get("port", DEFAULT_MAPEPIRE_PORT),
        connect_timeout=connection_config.get("connect_timeout", 30),
        query_timeout=connection_config.get("query_timeout", 3600),
        ignore_unauthorized=connection_config.get("ignore_unauthorized", False),
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
        where_clause: Optional[str] = None,
        batch_size: int = DEFAULT_LOADER_CHUNK_ROWS,
        max_training_rows: int = DEFAULT_MAX_TRAINING_ROWS,
        compression: str = DEFAULT_PARQUET_COMPRESSION,
        connector_id: Optional[str] = None,
        connector_type: Optional[str] = None,
        encrypted_connection_details: Optional[Dict[str, Any]] = None,
    ) -> ExtractResult:
        """Stream table rows from Db2 in batches and write directly to a Parquet file.

        By default the loader's own ``db2_client`` is used. If
        ``encrypted_connection_details`` is given, the connector credentials are
        decrypted with DB_ENCRYPTION_KEY and a client is created (and closed) just
        for this extraction, ignoring ``db2_client``.

        Args:
            table: Source table in 'SCHEMA.TABLE' form.
            columns: List of columns to extract.
            output_path: Destination path for the .parquet file.
            where_clause: Optional SQL filter condition (e.g., "STATUS = 'ACTIVE'").
            batch_size: Number of rows to fetch per chunk from Db2.
            max_training_rows: Maximum allowed row limit. Extraction aborts if exceeded.
            compression: Parquet compression codec (default 'snappy').
            connector_id: Connector identifier, used in decryption errors.
            connector_type: Connector type used to select the secret fields to decrypt.
            encrypted_connection_details: Encrypted connector configuration.

        Returns:
            ExtractResult with extracted row count, duration, and file metadata.

        Raises:
            ConnectorDecryptionError: If connector credentials cannot be decrypted
                (names the connector without leaking credentials).
            ValueError: If neither a client nor connector details are available, or
                connector details are given without connector_id and connector_type.
            InvalidTableReferenceError: If table is not in 'SCHEMA.TABLE' form.
            DBTableNotFoundError: If table does not exist or user lacks access.
            DBColumnNotFoundError: If any requested column is missing.
            RowLimitExceededError: If extracted rows exceed max_training_rows.
            DBConnectionError: If connection is dropped during extraction.
            ParquetWriteError: If Parquet writing fails.
        """
        if encrypted_connection_details is not None:
            if not connector_id or not connector_type:
                raise ValueError("connector_id and connector_type are required with encrypted_connection_details")

            from forecast.encryption import decrypt_secrets

            try:
                decrypted_config = decrypt_secrets(connector_type, encrypted_connection_details)
            except Exception as exc:
                raise ConnectorDecryptionError(connector_id) from exc

            with _client_from_config(decrypted_config) as client:
                return Db2DataLoader(client).extract_to_parquet(
                    table=table,
                    columns=columns,
                    output_path=output_path,
                    where_clause=where_clause,
                    batch_size=batch_size,
                    max_training_rows=max_training_rows,
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
            "Starting Db2 data extraction: table=%s columns=%s batch_size=%d max_training_rows=%d output=%s",
            table,
            cols_requested,
            batch_size,
            max_training_rows,
            target_path,
        )

        # 1. Fetch available column metadata and validate requested columns exist
        available_cols = self.db2_client.list_columns(table)
        available_col_map = {col.name.upper(): col for col in available_cols}

        missing_cols = [c for c in cols_requested if c.upper() not in available_col_map]
        if missing_cols:
            raise DBColumnNotFoundError(
                f"Column(s) {missing_cols} not found in table '{table}'. Available columns: {list(available_col_map.keys())}"
            )

        # 2. Build Arrow schema with mapped data types
        col_type_map = {
            col.name.upper(): col.type for col in available_cols
        }
        arrow_schema = build_arrow_schema(cols_requested, col_type_map)

        # 3. Build optimized SELECT SQL statement
        query = self.db2_client.build_select_query(
            table=table,
            columns=cols_requested,
            where_clause=where_clause,
            batch_size=batch_size,
        )

        # 4. Stream batches and write to Parquet
        total_rows = 0
        batches_count = 0

        with ParquetBatchWriter(output_path=target_path, schema=arrow_schema, compression=compression) as writer:
            for batch in self.db2_client.stream_query_batches(query, batch_size=batch_size):
                batch_len = len(batch)
                if total_rows + batch_len > max_training_rows:
                    writer.abort()
                    raise RowLimitExceededError(
                        row_count=total_rows + batch_len,
                        max_training_rows=max_training_rows,
                        message=(
                            f"Extraction exceeded max_training_rows limit of {max_training_rows} rows "
                            f"(attempted to read {total_rows + batch_len} rows from {table})"
                        ),
                    )

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
            columns=cols_requested,
            bytes_written=file_size,
            duration_seconds=duration,
            table=table,
            batches_count=batches_count,
        )
