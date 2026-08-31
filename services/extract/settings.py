"""
Configuration settings for the Extract Information service.

All values can be overridden via environment variables.
"""

from pathlib import Path

from pydantic import Field
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


    # Request body size limit (bytes) — enforced before tokenisation to avoid
    # spending a /tokenize round-trip on obviously oversized payloads.
    max_request_body_bytes: int = Field(
        default=1_048_576,  # 1 MiB
        ge=1,
        description=(
            "Maximum allowed size of the POST /v1/extract JSON request body "
            "in bytes. Requests exceeding this limit receive 413 before "
            "tokenisation begins."
        ),
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

    # Prompts (Section 8.1 of proposal)
    extraction_system_prompt: str = Field(
        default=(
            "You are an information extraction assistant. You extract data from the provided\n"
            "text and return it strictly as a single JSON object that conforms to the given\n"
            "JSON schema. Output ONLY the JSON object. Do not add explanations, markdown\n"
            "fences, headings, or any other text.\n\n"
            "Rules:\n"
            "1. Extract values EXACTLY as they appear in the text (preserve numbers, dates,\n"
            "   identifiers verbatim; normalize dates to the schema's declared format).\n"
            "2. If a non-required property is not present in the text, omit it or set it to\n"
            "   null. NEVER invent values.\n"
            "3. Required properties must be populated from the text.\n\n"
            "{custom_prompt}"
        ),
        description=(
            "System prompt template for extraction requests (Section 8.1).  "
            "Contains one optional placeholder: {custom_prompt}, which is replaced "
            "with the schema's custom_prompt value or removed when absent."
        ),
    )

    extraction_user_prompt: str = Field(
        default=(
            "JSON schema:\n"
            "{normalized_json_schema}\n\n"
            "{few_shot_block}\n\n"
            "Text:\n"
            "{input_text}\n\n"
            "JSON:"
        ),
        description=(
            "User prompt template for extraction requests (Section 8.1).  "
            "Placeholders: {normalized_json_schema}, {few_shot_block}, {input_text}."
        ),
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
    max_concurrent_requests: int = Field(
        default=8,
        ge=1,
        description="Maximum number of async + sync extraction requests running in parallel",
    )

    # Batch submission
    max_files_per_job: int = Field(
        default=64,
        ge=1,
        description="Maximum number of files allowed in a single batch job submission",
    )

    # Maximum number of files processed in parallel within a single batch job
    parallel_files_per_job: int = Field(
        default=4,
        ge=1,
        description="Maximum number of files processed concurrently within one batch job",
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
