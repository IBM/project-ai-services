"""
Configuration settings for the Extract Information service.

All values can be overridden via environment variables.
"""

from pathlib import Path

from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.misc_utils import get_logger
from common.settings import Settings as CommonSettings

logger = get_logger("settings")


class ExtractionConfig(BaseSettings):
    """Extraction-specific settings."""

    # File storage
    cache_dir: Path = Field(
        default=Path("/var/cache/extract"),
        description="Base cache directory for staging and results",
    )


    max_examples: int = Field(
        default=5,
        ge=1,
        description="Maximum number of few-shot examples per schema",
    )

    max_custom_prompt_chars: int = Field(
        default=2000,
        ge=1,
        description="Maximum character length for the custom_prompt field",
    )

    # Context-window budget at registration time (Section 5.1.2 of proposal)
    context_schema_share: float = Field(
        default=0.7,
        gt=0.0,
        lt=1.0,
        description=(
            "Maximum share of MAX_MODEL_LEN that schema fixed overhead "
            "(schema + examples + custom_prompt + PROMPT_OVERHEAD_TOKENS) "
            "may consume.  Registration fails if this fraction is exceeded."
        ),
    )

    # Context-window budget at extraction time
    output_token_factor: float = Field(
        default=2.0,
        gt=0.0,
        description="reserved_output = clamp(schema_tokens * factor, MIN, MAX)",
    )

    min_output_tokens: int = Field(
        default=512,
        ge=1,
        description="Minimum reserved output tokens (floor for small schemas)",
    )

    max_output_tokens: int = Field(
        default=4096,
        ge=1,
        description="Maximum reserved output tokens (ceiling for large schemas)",
    )

    prompt_overhead_tokens: int = Field(
        default=150,
        ge=0,
        description="Estimated token budget for the fixed system-prompt scaffold",
    )

    # vLLM
    guided_decoding_enabled: bool = Field(
        default=True,
        description="Send guided_json to vLLM for constrained generation",
    )

    extraction_temperature: float = Field(
        default=0.0,
        ge=0.0,
        le=2.0,
        description="Temperature for extraction generation (deterministic by default)",
    )

    # Concurrency
    max_concurrent_jobs: int = Field(
        default=4,
        ge=1,
        description="Maximum number of async extraction jobs running in parallel",
    )

    # Digitize service integration
    digitize_base_url: str = Field(
        default="http://digitize:4000",
        description="Base URL of the Digitize Documents service",
    )

    digitize_poll_interval_secs: int = Field(
        default=10,
        ge=1,
        description="Polling interval (seconds) for digitize job status",
    )

    digitize_job_timeout_secs: int = Field(
        default=3600,
        ge=1,
        description="Maximum wait time (seconds) for a digitize job to complete",
    )

    digitize_submit_timeout_secs: int = Field(
        default=900,
        ge=1,
        description="Maximum backoff budget (seconds) when digitize returns 429",
    )

    @property
    def staging_dir(self) -> Path:
        """Job-specific staging directory root."""
        return self.cache_dir / "staging"

    @property
    def results_dir(self) -> Path:
        """Extraction result file directory."""
        return self.cache_dir / "results"


class DatabaseConfig(BaseSettings):
    """Database connection pool configuration."""

    pool_size: int = Field(default=5, ge=1, description="Pool connection count")
    max_overflow: int = Field(default=5, ge=0, description="Extra connections beyond pool_size")
    pool_timeout: int = Field(default=30, ge=1, description="Seconds to wait for a connection")
    pool_recycle: int = Field(default=3600, ge=1, description="Seconds before recycling connections")

    model_config = SettingsConfigDict(env_prefix="DB_")


class Settings(BaseSettings):
    common: CommonSettings = Field(default_factory=CommonSettings)
    extract: ExtractionConfig = Field(default_factory=ExtractionConfig)
    database: DatabaseConfig = Field(default_factory=DatabaseConfig)


# Global settings instance
settings = Settings()

# Made with Bob
