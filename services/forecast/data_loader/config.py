"""
Configuration and defaults for the Forecast Data Loader module.

Centralizes all default timeouts, chunk sizes, row limits, ports, and compression options.
"""
from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Optional

# Default configuration constants
DEFAULT_LOADER_CHUNK_ROWS: int = int(os.getenv("LOADER_CHUNK_ROWS", "100000"))
DEFAULT_MAX_TRAINING_ROWS: int = int(os.getenv("MAX_TRAINING_ROWS", "10000000"))
DEFAULT_PARQUET_COMPRESSION: str = os.getenv("PARQUET_COMPRESSION", "snappy")
DEFAULT_MAPEPIRE_PORT: int = int(os.getenv("MAPEPIRE_PORT", "8076"))
DEFAULT_CONNECT_TIMEOUT_S: int = int(os.getenv("CONNECTOR_CONNECT_TIMEOUT_S", "30"))
DEFAULT_QUERY_TIMEOUT_S: int = int(os.getenv("CONNECTOR_QUERY_TIMEOUT_S", "3600"))


@dataclass
class DataLoaderConfig:
    """Configuration container for data loading and extraction pipelines."""

    batch_size: int = DEFAULT_LOADER_CHUNK_ROWS
    max_training_rows: int = DEFAULT_MAX_TRAINING_ROWS
    compression: str = DEFAULT_PARQUET_COMPRESSION
    port: int = DEFAULT_MAPEPIRE_PORT
    connect_timeout: int = DEFAULT_CONNECT_TIMEOUT_S
    query_timeout: int = DEFAULT_QUERY_TIMEOUT_S
    ignore_unauthorized: bool = False
    ca_cert: Optional[str | bytes] = None

    @classmethod
    def from_settings(cls) -> DataLoaderConfig:
        """Create DataLoaderConfig populated from global forecast settings if available."""
        try:
            from forecast.settings import settings

            return cls(
                batch_size=settings.forecast.loader_chunk_rows,
                max_training_rows=settings.forecast.max_training_rows,
            )
        except Exception:
            return cls()
