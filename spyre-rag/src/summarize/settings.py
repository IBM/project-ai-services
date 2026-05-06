"""
Configuration settings for Summarization service.
These values can be overridden via environment variables.
"""
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

    max_concurrent_requests: int = Field(
        default=32,
        ge=1,
        description="Maximum number of concurrent requests",
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


class Settings(BaseSettings):
    common: CommonSettings = Field(default_factory=CommonSettings)
    summarize: SummarizationConfig = Field(default_factory=SummarizationConfig)

# Global settings instance
settings = Settings()

# Made with Bob
