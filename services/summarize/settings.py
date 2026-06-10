"""
Configuration settings for Summarization service.
These values can be overridden via environment variables.
"""
from pathlib import Path
from pydantic_settings.main import SettingsConfigDict


from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.misc_utils import get_logger
from common.settings import Settings as CommonSettings

logger = get_logger("settings")


class SummarizationLevel(BaseSettings):
    """Configuration for a single summarization level."""
    
    multiplier: float = Field(
        ...,
        gt=0.0,
        description="Multiplier for the base summarization coefficient",
    )
    
    description: str = Field(
        ...,
        description="Human-readable description of this level",
    )


class SummarizationLevelsConfig(BaseSettings):
    """Configuration for different summarization abstraction levels."""
    
    brief: SummarizationLevel = Field(
        default=SummarizationLevel(multiplier=0.5, description="Quick overview"),
        description="Brief summarization level",
    )
    
    standard: SummarizationLevel = Field(
        default=SummarizationLevel(multiplier=1.0, description="Balanced summary"),
        description="Standard summarization level",
    )
    
    detailed: SummarizationLevel = Field(
        default=SummarizationLevel(multiplier=1.5, description="Comprehensive summary"),
        description="Detailed summarization level",
    )


class SummarizationConfig(BaseSettings):
    """Summarization settings."""

    # Directory paths
    cache_dir: Path = Field(
        default=Path("/var/cache"),
        description="Base cache directory for all operations",
    )

    summarization_coefficient: float = Field(
        default=0.3,
        gt=0.0,
        le=1.0,
        description="Base coefficient for calculating summary length",
    )

    summarization_prompt_token_count: int = Field(
        default=200,
        ge=0,
        description="Estimated token count for summarization prompt",
    )

    summarization_temperature: float = Field(
        default=0.3,
        ge=0.0,
        le=2.0,
        description="Temperature for summarization generation",
    )

    summarization_stop_words: str = Field(
        default="",
        description="Stop words for summarization (comma-separated)",
    )
    
    minimum_summary_words: int = Field(
        default=200,
        gt=0,
        description="Minimum number of words for a valid summary",
    )

    summarize_system_prompt: str = Field(
        default=(
            "You are an expert summarization assistant. Your summaries must be comprehensive and use the full available space. "
            "Preserve numerical data and maintain factual accuracy. Output ONLY the summary."
        ),
        description="System prompt for summarization",
    )

    summarize_user_prompt_with_length: str = Field(
        default=(
            "Create a comprehensive summary of the following text.\n\n"
            "TARGET LENGTH: {target_words} words\n\n"
            "CRITICAL INSTRUCTIONS:\n"
            "1. Your summary MUST approach {target_words} words - do NOT stop early\n"
            "2. Use the FULL available space by including:\n"
            "   - All key findings and main points\n"
            "   - Supporting details and context\n"
            "   - Relevant data and statistics\n"
            "   - Implications and significance\n"
            "3. Preserve ALL numerical data EXACTLY\n"
            "4. A summary under {min_words} words is considered incomplete\n\n"
            "Text:\n{text}\n\n"
            "Comprehensive Summary ({target_words} words):"
        ),
        description="User prompt for summarization with target length",
    )

    summarize_user_prompt_without_length: str = Field(
        default=(
            "Create a thorough and comprehensive summary of the following text.\n\n"
            "REQUIREMENTS:\n"
            "- Be detailed and comprehensive\n"
            "- Preserve all numerical data and statistics\n"
            "- Include key findings, supporting details, and implications\n"
            "- Maintain factual accuracy\n\n"
            "Text:\n{text}\n\n"
            "Comprehensive Summary:"
        ),
        description="User prompt for summarization without target length",
    )

    table_summary_max_tokens: int = Field(
        default=1024,
        ge=0,
        description="Maximum tokens for table summarization",
    )
    
    summarization_levels: SummarizationLevelsConfig = Field(
        default_factory=SummarizationLevelsConfig,
        description="Configuration for different summarization abstraction levels",
    )
    
    # Chunking configuration for large documents
    chunk_parallelism: int = Field(
        default=4,
        ge=1,
        le=32,
        description="Maximum number of chunks to process in parallel per job",
    )
    
    chunk_overlap_sentences: int = Field(
        default=1,
        ge=0,
        le=5,
        description="Number of sentences to overlap between consecutive chunks",
    )
    
    # Merge prompts for chunked summarization
    merge_system_prompt: str = Field(
        default=(
            "You are a summarization assistant. You are given a series of summaries, each covering a consecutive section of the same document. "
            "Your task is to combine them into a single, unified summary. Output ONLY the summary.\n\n"
            "Do not add questions, explanations, headings, code, or any other text."
        ),
        description="System prompt for merging chunk summaries",
    )
    
    merge_user_prompt: str = Field(
        default=(
            "The following are summaries of consecutive sections from a single document.\n"
            "Combine them into one unified summary in {target_words} words, with an allowed variance of ±50 words.\n"
            "Preserve the key points from all sections. Remove redundancy and ensure the summary reads as a single coherent text, not as a list of section summaries.\n\n"
            "CRITICAL INSTRUCTIONS:\n"
            "1. Your summary MUST approach {target_words} words - do NOT stop early\n"
            "2. Use the FULL available space by including:\n"
            "   - All key findings and main points\n"
            "   - Supporting details and context\n"
            "   - Relevant data and statistics\n"
            "   - Implications and significance\n"
            "3. Preserve ALL numerical data EXACTLY\n"
            "4. A summary under {min_words} words is considered incomplete\n\n"
            "Section summaries:\n{merged_chunk_summaries}\n\n"
            "Unified summary:"
        ),
        description="User prompt for merging chunk summaries with target length",
    )

    @field_validator('summarization_coefficient')
    @classmethod
    def validate_summarization_coefficient(cls, v):
        """Validate summarization_coefficient with warning fallback."""
        if not isinstance(v, float):
            logger.warning(f"Setting summarization_coefficient to default '0.2' as it is missing in the settings")
            return 0.2
        return v

    @field_validator('summarization_prompt_token_count')
    @classmethod
    def validate_summarization_prompt_token_count(cls, v):
        """Validate summarization_prompt_token_count with warning fallback."""
        if not isinstance(v, int):
            logger.warning(f"Setting summarization_prompt_token_count to default '100' as it is missing in the settings")
            return 100
        return v

    @field_validator('summarization_temperature')
    @classmethod
    def validate_summarization_temperature(cls, v):
        """Validate summarization_temperature with warning fallback."""
        if not isinstance(v, float):
            logger.warning(f"Setting summarization_temperature to default '0.2' as it is missing in the settings")
            return 0.2
        return v

    @field_validator('summarization_stop_words')
    @classmethod
    def validate_summarization_stop_words(cls, v):
        """Validate summarization_stop_words with warning fallback."""
        if not isinstance(v, str):
            logger.warning(f"Setting summarization_stop_words to default 'Keywords, Note, ***' as it is missing in the settings")
            return "Keywords, Note, ***"
        return v

    @property
    def staging_dir(self) -> Path:
        """Directory for staging uploaded files during processing."""
        return self.cache_dir  / "staging"

    @property
    def results_dir(self) -> Path:
        """Directory for storing completed summarization results."""
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
    summarize: SummarizationConfig = Field(default_factory=SummarizationConfig)
    database: DatabaseConfig = Field(default_factory=DatabaseConfig)

# Global settings instance
settings = Settings()

# Made with Bob
