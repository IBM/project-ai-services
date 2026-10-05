"""
Configuration settings for the Invoice Processing service.

All values can be overridden via environment variables.
"""

from pathlib import Path

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.misc_utils import get_logger
from common.settings import Settings as CommonSettings

logger = get_logger("settings")


class InvoiceProcessingConfig(BaseSettings):
    """Invoice processing specific settings."""

    cache_dir: Path = Field(
        default=Path("/var/cache/invoice-processing"),
        description="Base cache directory for staging and results",
    )

    # Downstream service URLs
    digitize_url: str = Field(
        default="",
        description="Base URL of digitize service e.g. http://digitize:4000",
    )
    extract_url: str = Field(
        default="",
        description="Base URL of extract service e.g. http://extract:6000",
    )

    # Schema registration
    extract_schema_name: str = Field(
        default="invoice-extraction-v1",
        description="Name of the schema registered in the extract service",
    )

    # Async job polling
    job_timeout_seconds: int = Field(
        default=300,
        ge=1,
        description="Timeout in seconds when waiting for downstream async jobs",
    )
    job_poll_interval_seconds: float = Field(
        default=3.0,
        ge=0.1,
        description="Interval in seconds between downstream status polls",
    )

    # DB load
    db_load_enabled: bool = Field(
        default=True,
        description="Whether to load staged invoices into target DB",
    )

    @property
    def staging_dir(self) -> Path:
        """Job staging directory root."""
        return self.cache_dir / "staging"

    @property
    def results_dir(self) -> Path:
        """Invoice result file directory root."""
        return self.cache_dir / "results"


class DatabaseConfig(BaseSettings):
    """Invoice processing PostgreSQL connection pool configuration."""

    pool_size: int = Field(default=5, ge=1, description="Pool connection count")
    max_overflow: int = Field(default=5, ge=0, description="Extra connections beyond pool_size")
    pool_timeout: int = Field(default=30, ge=1, description="Seconds to wait for a connection")
    pool_recycle: int = Field(default=3600, ge=1, description="Seconds before recycling connections")

    model_config = SettingsConfigDict(env_prefix="DB_")


class Settings(BaseSettings):
    """Composite settings root."""

    common: CommonSettings = Field(default_factory=CommonSettings)
    invoice: InvoiceProcessingConfig = Field(default_factory=InvoiceProcessingConfig)
    database: DatabaseConfig = Field(default_factory=DatabaseConfig)


# Global settings instance
settings = Settings()
