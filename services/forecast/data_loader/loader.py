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
    schema: Optional[str] = None
    batches_count: int = 0


class Db2DataLoader:
    """Coordinates data extraction from IBM Db2 directly into Parquet files on local disk."""

    def __init__(self, db2_client: Db2Client) -> None:
        self.db2_client = db2_client

    def extract_to_parquet(
        self,
        table: str,
        columns: Sequence[str],
        output_path: str | Path,
        schema: Optional[str] = None,
        where_clause: Optional[str] = None,
        batch_size: int = DEFAULT_LOADER_CHUNK_ROWS,
        max_training_rows: int = DEFAULT_MAX_TRAINING_ROWS,
        compression: str = DEFAULT_PARQUET_COMPRESSION,
    ) -> ExtractResult:
        """Stream table rows from Db2 in batches and write directly to a Parquet file.

        Args:
            table: Source table name.
            columns: List of columns to extract.
            output_path: Destination path for the .parquet file.
            schema: Optional schema/library name in Db2.
            where_clause: Optional SQL filter condition (e.g., "STATUS = 'ACTIVE'").
            batch_size: Number of rows to fetch per chunk from Db2.
            max_training_rows: Maximum allowed row limit. Extraction aborts if exceeded.
            compression: Parquet compression codec (default 'snappy').

        Returns:
            ExtractResult with extracted row count, duration, and file metadata.

        Raises:
            DBTableNotFoundError: If table does not exist or user lacks access.
            DBColumnNotFoundError: If any requested column is missing.
            RowLimitExceededError: If extracted rows exceed max_training_rows.
            DBConnectionError: If connection is dropped during extraction.
            ParquetWriteError: If Parquet writing fails.
        """
        if not columns:
            raise DBColumnNotFoundError("At least one column must be specified for extraction")

        start_time = time.monotonic()
        target_path = Path(output_path).resolve()
        cols_requested = list(columns)

        logger.info(
            "Starting Db2 data extraction: table=%s schema=%s columns=%s batch_size=%d max_training_rows=%d output=%s",
            table,
            schema,
            cols_requested,
            batch_size,
            max_training_rows,
            target_path,
        )

        # 1. Fetch available column metadata and validate requested columns exist
        available_cols = self.db2_client.list_columns(schema, table)
        available_col_map = {col.name.upper(): col for col in available_cols}

        missing_cols = [c for c in cols_requested if c.upper() not in available_col_map]
        if missing_cols:
            qual = f"{schema}.{table}" if schema else table
            raise DBColumnNotFoundError(
                f"Column(s) {missing_cols} not found in table '{qual}'. Available columns: {list(available_col_map.keys())}"
            )

        # 2. Build Arrow schema with mapped data types
        col_type_map = {
            col.name.upper(): col.type for col in available_cols
        }
        arrow_schema = build_arrow_schema(cols_requested, col_type_map)

        # 3. Build optimized SELECT SQL statement
        query = self.db2_client.build_select_query(
            schema=schema,
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
            schema=schema,
            batches_count=batches_count,
        )


def load_db2_to_parquet(
    connection_config: Dict[str, Any],
    table: str,
    columns: Sequence[str],
    output_path: str | Path,
    schema: Optional[str] = None,
    where_clause: Optional[str] = None,
    batch_size: int = DEFAULT_LOADER_CHUNK_ROWS,
    max_training_rows: int = DEFAULT_MAX_TRAINING_ROWS,
    compression: str = DEFAULT_PARQUET_COMPRESSION,
) -> ExtractResult:
    """Convenience helper to connect to Db2, extract rows in batches, and write to Parquet.

    Args:
        connection_config: Dictionary containing 'host', 'database', 'username', 'password',
                           and optional 'port', 'connect_timeout', 'query_timeout', 'ignore_unauthorized', 'ca_cert'.
        table: Target table name.
        columns: Column names to extract.
        output_path: Destination path for Parquet file.
        schema: Optional schema/library name.
        where_clause: Optional SQL WHERE predicate.
        batch_size: Batch fetch size.
        max_training_rows: Maximum rows allowed.
        compression: Parquet compression.

    Returns:
        ExtractResult object.
    """
    client = Db2Client(
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
    with client:
        loader = Db2DataLoader(client)
        return loader.extract_to_parquet(
            table=table,
            columns=columns,
            output_path=output_path,
            schema=schema,
            where_clause=where_clause,
            batch_size=batch_size,
            max_training_rows=max_training_rows,
            compression=compression,
        )
