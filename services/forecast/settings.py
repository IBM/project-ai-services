"""
Configuration settings for the Forecast service.

Every variable in the Configuration section of the proposal is read here,
except POSTGRES_HOST/PORT/DB/USER/PASSWORD, which common.db.connection reads.
Values can be overridden via environment variables of the same name.
"""
from pathlib import Path
from typing import Optional

from pydantic import Field, SecretStr, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.settings import Settings as CommonSettings


class ForecastConfig(BaseSettings):
    """Forecast-specific settings."""

    # Storage
    model_repo_path: Path = Field(
        default=Path("/models"),
        description="Root of the shared repository volume (Triton model repository)",
    )

    scratch_dir: Path = Field(
        default=Path("/var/cache/forecast/scratch"),
        description="Local training scratch, not shared",
    )

    data_dir: Path = Field(
        default=Path("/data"),
        description="Read-only root of CSV training files; CSV paths in jobs are relative to it",
    )

    # Secrets
    db_encryption_key: SecretStr = Field(
        default=SecretStr(""),
        repr=False,
        description="32-byte AES-256-GCM key for connector secrets",
    )

    # Triton
    triton_url: str = Field(
        default="",
        description="Triton endpoint used for model control and inference",
    )

    triton_startup_wait_s: int = Field(
        default=120,
        ge=0,
        description="How long the API waits for Triton to report ready at boot",
    )

    triton_load_timeout_s: int = Field(
        default=120,
        ge=1,
        description="Timeout for one load or unload call and for the wait on the model state",
    )

    triton_infer_timeout_s: int = Field(
        default=30,
        ge=1,
        description="Timeout for one inference request forwarded to Triton",
    )

    # Retention and training
    forecaster_retention_count: int = Field(
        default=5,
        ge=1,
        description="Forecasters kept per connector, and finished runs kept per connector",
    )

    default_time_limit_seconds: int = Field(
        default=1800,
        ge=1,
        description="Time limit applied to each training job",
    )

    training_max_attempts: int = Field(
        default=2,
        ge=1,
        description="Attempts of the worker step per job, the first one included",
    )

    training_retry_delay_seconds: int = Field(
        default=60,
        ge=0,
        description="Wait between two attempts of the same job",
    )

    max_queued_jobs: int = Field(
        default=10,
        ge=1,
        description="Queued jobs accepted before POST /v1/connectors returns 429",
    )

    max_training_rows: int = Field(
        default=10_000_000,
        ge=1,
        description="Largest row count a job may train on",
    )

    max_inference_rows: int = Field(
        default=5000,
        ge=1,
        description="Largest row count a forecast may use for inference",
    )

    loader_chunk_rows: int = Field(
        default=100_000,
        ge=1,
        description="Rows per chunk when streaming the source to scratch",
    )

    worker_memory_limit_mb: Optional[int] = Field(
        default=None,
        ge=1,
        description="Hard memory cap applied to the worker subprocess; unset until benchmarked",
    )

    # Restricted database role used by the worker subprocess
    worker_postgres_user: str = Field(
        default="",
        description="User of the restricted forecast_worker role",
    )

    worker_postgres_password: SecretStr = Field(
        default=SecretStr(""),
        repr=False,
        description="Password of the restricted forecast_worker role",
    )

    @field_validator("worker_memory_limit_mb", mode="before")
    @classmethod
    def blank_means_unset(cls, value):
        """Treat an empty or blank environment value as unset."""
        if isinstance(value, str) and not value.strip():
            return None
        return value


class DatabaseConfig(BaseSettings):
    """Database connection pool configuration."""

    pool_size: int = Field(
        default=5,
        ge=1,
        description="Number of connections to keep in the pool",
    )

    max_overflow: int = Field(
        default=5,
        ge=0,
        description="Maximum number of connections that can be created beyond pool_size",
    )

    pool_timeout: int = Field(
        default=30,
        ge=1,
        description="Timeout in seconds for getting a connection from the pool",
    )

    pool_recycle: int = Field(
        default=3600,
        ge=1,
        description="Time in seconds after which connections are recycled (1 hour default)",
    )

    model_config = SettingsConfigDict(env_prefix="DB_")


class Settings(BaseSettings):
    """Settings of the Forecast service."""

    common: CommonSettings = Field(default_factory=CommonSettings)
    forecast: ForecastConfig = Field(default_factory=ForecastConfig)
    database: DatabaseConfig = Field(default_factory=DatabaseConfig)


# Global settings instance
settings = Settings()
