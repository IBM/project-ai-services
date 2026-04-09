"""
Configuration settings for Summarization service.
These values can be overridden via environment variables.
"""
from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.misc_utils import get_logger

logger = get_logger("settings")


class SummarizationConfig(BaseSettings):
    """Summarization settings."""

    model_config = SettingsConfigDict(
        env_prefix="",
        case_sensitive=True,
        extra="ignore",
    )

    summarization_coefficient: float = Field(
        alias="SUMMARIZATION_COEFFICIENT",
        default=0.2,
        gt=0.0,
        le=1.0,
        description="Coefficient for calculating summary length",
    )

    summarization_prompt_token_count: int = Field(
        alias="SUMMARIZATION_PROMPT_TOKEN_COUNT",
        default=100,
        ge=0,
        description="Estimated token count for summarization prompt",
    )

    summarization_temperature: float = Field(
        alias="SUMMARIZATION_TEMPERATURE",
        default=0.2,
        ge=0.0,
        le=2.0,
        description="Temperature for summarization generation",
    )

    summarization_stop_words: str = Field(
        alias="SUMMARIZATION_STOP_WORDS",
        default="Keywords, Note, ***",
        description="Stop words for summarization (comma-separated)",
    )

    summarize_system_prompt: str = Field(
        alias="SUMMARIZE_SYSTEM_PROMPT",
        default=(
            "You are a summarization assistant. Output ONLY the summary. \n\n"
            "Do not add questions, explanations, headings, code, or any other text."
        ),
        description="System prompt for summarization",
    )

    summarize_user_prompt_with_length: str = Field(
        alias="SUMMARIZE_USER_PROMPT_WITH_LENGTH",
        default=(
            "Summarize the following text in {target_words} words, with an allowed variance of ±50 words."
            "Avoid being overly concise.\nExpand explanations where necessary to meet the word requirement.\n\n"
            "You must strictly meet this word-range requirement. Do not exceed or fall short of the range.\n\n\n"
            "Text:\n{text}\n\nSummary:"
        ),
        description="User prompt for summarization with target length",
    )

    summarize_user_prompt_without_length: str = Field(
        alias="SUMMARIZE_USER_PROMPT_WITHOUT_LENGTH",
        default="Summarize the following text.\n\nText:\n{text}\n\nSummary:",
        description="User prompt for summarization without target length",
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


# Global settings instance
settings = SummarizationConfig()

# Backward compatibility: expose settings as module-level constants
SUMMARIZATION_COEFFICIENT = settings.summarization_coefficient
SUMMARIZATION_PROMPT_TOKEN_COUNT = settings.summarization_prompt_token_count
SUMMARIZATION_TEMPERATURE = settings.summarization_temperature
SUMMARIZATION_STOP_WORDS = settings.summarization_stop_words
SUMMARIZE_SYSTEM_PROMPT = settings.summarize_system_prompt
SUMMARIZE_USER_PROMPT_WITH_LENGTH = settings.summarize_user_prompt_with_length
SUMMARIZE_USER_PROMPT_WITHOUT_LENGTH = settings.summarize_user_prompt_without_length

# Made with Bob
