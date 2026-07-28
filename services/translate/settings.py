"""
Configuration settings for Translation service.
These values can be overridden via environment variables.
"""
from pathlib import Path

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.settings import Settings as CommonSettings


class TranslationConfig(BaseSettings):
    """Translation-specific settings."""

    # Cache / storage
    cache_dir: Path = Field(
        default=Path("/var/cache/translate"),
        description="Base cache directory for staging and results",
    )

    # Context-window budget
    chunk_token_budget: int = Field(
        default=13107,
        ge=1,
        description=(
            "Maximum input tokens per translation chunk (async path). "
            "Default is 13107 (≈ 40% of 32768, the granite-3.3-8b-instruct context window). "
            "Can be overridden via CHUNK_TOKEN_BUDGET env var. "
            "Updated at runtime if a different MAX_MODEL_LEN is detected."
        ),
    )

    prompt_overhead_tokens: int = Field(
        default=150,
        ge=0,
        description="Estimated token overhead for system + user prompt template",
    )

    min_output_tokens: int = Field(
        default=50,
        ge=1,
        description="Minimum output buffer reserved for context guard",
    )

    # Upload limit
    max_upload_size_mb: int = Field(
        default=10,
        ge=1,
        description="Maximum accepted file size for async job uploads (MB)",
    )

    # Translation LLM settings
    translation_temperature: float = Field(
        default=0.0,
        ge=0.0,
        le=2.0,
        description="LLM temperature for all translation calls",
    )

    # Concurrency
    max_concurrent_jobs: int = Field(
        default=8,
        ge=1,
        description="Async job admission semaphore size",
    )

    chunk_parallelism: int = Field(
        default=4,
        ge=1,
        le=32,
        description="Max chunks translated concurrently within a single job",
    )

    # Language allowlist (comma-separated, lowercase)
    supported_languages: str = Field(
        default="english,german",
        description="Comma-separated allowlist of supported language names (lowercase)",
    )

    @property
    def supported_languages_list(self) -> list[str]:
        """Return supported languages as a normalised lowercase list."""
        return [
            lang.strip().lower()
            for lang in self.supported_languages.split(",")
            if lang.strip()
        ]

    @property
    def staging_dir(self) -> Path:
        return self.cache_dir / "staging"

    @property
    def results_dir(self) -> Path:
        return self.cache_dir / "results"


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
    common: CommonSettings = Field(default_factory=CommonSettings)
    translate: TranslationConfig = Field(default_factory=TranslationConfig)
    database: DatabaseConfig = Field(default_factory=DatabaseConfig)


# Global settings instance
settings = Settings()

# Made with Bob
