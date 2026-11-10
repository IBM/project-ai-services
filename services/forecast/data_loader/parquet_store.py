"""
Parquet storage layer for streaming database rows into compressed columnar files.

Handles typed PyArrow schema building, batch appending via ParquetWriter, and
atomic file finalization.
"""
from __future__ import annotations

import logging
import os
from pathlib import Path
from typing import Any, Dict, List, Optional, Sequence

import pyarrow as pa
import pyarrow.parquet as pq

from forecast.data_loader.exceptions import ParquetWriteError

logger = logging.getLogger(__name__)

# Mapping from standard SQL / Db2 data types to PyArrow types
_SQL_TO_ARROW_TYPE: dict[str, pa.DataType] = {
    # Integers
    "SMALLINT": pa.int16(),
    "INTEGER": pa.int32(),
    "INT": pa.int32(),
    "BIGINT": pa.int64(),
    "TINYINT": pa.int8(),
    # Floating point & Decimals
    "REAL": pa.float32(),
    "FLOAT": pa.float64(),
    "DOUBLE": pa.float64(),
    "DOUBLE PRECISION": pa.float64(),
    "DECIMAL": pa.float64(),
    "NUMERIC": pa.float64(),
    "DECFLOAT": pa.float64(),
    # Strings & Characters
    "CHAR": pa.string(),
    "CHARACTER": pa.string(),
    "VARCHAR": pa.string(),
    "VARCHAR2": pa.string(),
    "CLOB": pa.string(),
    "DBCLOB": pa.string(),
    "NCHAR": pa.string(),
    "NVARCHAR": pa.string(),
    "GRAPHIC": pa.string(),
    "VARGRAPHIC": pa.string(),
    # Date & Time
    "DATE": pa.date32(),
    "TIME": pa.string(),
    "TIMESTAMP": pa.timestamp("us"),
    "TIMESTAMPTZ": pa.timestamp("us", tz="UTC"),
    # Boolean & Binary
    "BOOLEAN": pa.bool_(),
    "BOOL": pa.bool_(),
    "BINARY": pa.binary(),
    "VARBINARY": pa.binary(),
    "BLOB": pa.binary(),
}


def map_db_type_to_arrow(db_type: str) -> pa.DataType:
    """Map a SQL/Db2 type string to a pyarrow DataType.

    Strips parameters such as length, precision, or scale (e.g., 'VARCHAR(255)' -> 'VARCHAR',
    'DECIMAL(10,2)' -> 'DECIMAL'). Falls back to pa.string() for unrecognized types.
    """
    if not db_type:
        return pa.string()
    base_type = db_type.split("(")[0].strip().upper()
    return _SQL_TO_ARROW_TYPE.get(base_type, pa.string())


def build_arrow_schema(
    columns: Sequence[str],
    column_types: Optional[Dict[str, str]] = None,
) -> pa.Schema:
    """Construct a PyArrow schema from column names and optional DB type mappings.

    Args:
        columns: Ordered list of column names.
        column_types: Optional mapping of column name (case-insensitive) to SQL type string.

    Returns:
        pyarrow.Schema with properly mapped types.
    """
    type_lookup = {k.upper(): v for k, v in (column_types or {}).items()}
    fields = []
    for col in columns:
        db_type = type_lookup.get(col.upper(), "VARCHAR")
        arrow_type = map_db_type_to_arrow(db_type)
        fields.append(pa.field(col, arrow_type, nullable=True))
    return pa.schema(fields)


class ParquetBatchWriter:
    """Streams data batches directly to a Parquet file on disk with low memory usage.

    Uses an atomic write strategy: writes to a temporary file (`<target>.tmp.<pid>`)
    and renames it to the destination file upon successful completion (`close()`).
    If an error occurs, the temporary file is deleted.
    """

    def __init__(
        self,
        output_path: str | Path,
        schema: pa.Schema,
        compression: str = "snappy",
    ) -> None:
        self.output_path = Path(output_path).resolve()
        self.schema = schema
        self.compression = compression
        self.temp_path = self.output_path.with_name(f"{self.output_path.name}.tmp.{os.getpid()}")

        self._writer: Optional[pq.ParquetWriter] = None
        self._rows_written = 0
        self._batches_written = 0
        self._closed = False
        self._is_aborted = False

        # Ensure parent directory exists
        self.output_path.parent.mkdir(parents=True, exist_ok=True)

    @property
    def rows_written(self) -> int:
        return self._rows_written

    @property
    def batches_written(self) -> int:
        return self._batches_written

    def _ensure_writer(self) -> pq.ParquetWriter:
        if self._writer is None:
            try:
                self._writer = pq.ParquetWriter(
                    str(self.temp_path),
                    self.schema,
                    compression=self.compression,
                )
            except Exception as exc:
                self._is_aborted = True
                raise ParquetWriteError(f"Failed to open Parquet writer at {self.temp_path}: {exc}") from exc
        return self._writer

    def write_batch(self, rows: Sequence[Sequence[Any]]) -> int:
        """Write a batch of row tuples to the Parquet file.

        Args:
            rows: Sequence of row tuples/lists matching the schema column order.

        Returns:
            Number of rows written in this batch.
        """
        if self._closed:
            raise ParquetWriteError("Cannot write to a closed ParquetBatchWriter")
        if not rows:
            return 0

        writer = self._ensure_writer()
        column_names = self.schema.names

        try:
            expected_cols = len(column_names)
            for row in rows:
                if len(row) != expected_cols:
                    raise ValueError(
                        f"Row length ({len(row)}) does not match expected schema column count ({expected_cols})"
                    )

            # Transpose row tuples into column arrays for Arrow RecordBatch
            col_data = {
                col_name: [row[i] for row in rows]
                for i, col_name in enumerate(column_names)
            }
            batch = pa.RecordBatch.from_pydict(col_data, schema=self.schema)
            writer.write_batch(batch)
            batch_count = len(rows)
            self._rows_written += batch_count
            self._batches_written += 1
            return batch_count
        except Exception as exc:
            self._is_aborted = True
            self.abort()
            raise ParquetWriteError(f"Failed to write batch to Parquet: {exc}") from exc

    def close(self) -> None:
        """Finalize the Parquet file (writes footer metadata) and atomically rename to destination."""
        if self._closed:
            return
        self._closed = True

        if not self._is_aborted:
            # Ensure writer exists so even empty datasets produce a valid Parquet file
            self._ensure_writer()
            try:
                self._writer.close()
            except Exception as exc:
                self.abort()
                raise ParquetWriteError(f"Failed to finalize Parquet file: {exc}") from exc
            finally:
                self._writer = None

        if not self._is_aborted and self.temp_path.exists():
            try:
                # Atomic replace on POSIX filesystems
                self.temp_path.replace(self.output_path)
            except Exception as exc:
                self.abort()
                raise ParquetWriteError(
                    f"Failed to atomically rename {self.temp_path} to {self.output_path}: {exc}"
                ) from exc

    def abort(self) -> None:
        """Abort writing, close writer if open, and delete the temporary file."""
        self._is_aborted = True
        if self._writer is not None:
            try:
                self._writer.close()
            except Exception:
                pass
            self._writer = None

        if self.temp_path.exists():
            try:
                self.temp_path.unlink()
            except OSError as exc:
                logger.warning("Could not delete temporary parquet file %s: %s", self.temp_path, exc)

    def __enter__(self) -> ParquetBatchWriter:
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        if exc_type is not None:
            self.abort()
        else:
            self.close()
