"""
Configuration settings for Similarity Search service.
These values can be overridden via environment variables.
"""
from pydantic import Field, field_validator
from pydantic_settings import BaseSettings

from common.misc_utils import get_logger
from common.settings import Settings as CommonSettings

logger = get_logger("settings")


class SimilarityConfig(BaseSettings):
    """Similarity search settings."""

    num_chunks_post_search: int = Field(
        default=10,
        gt=0,
        description="Number of results to return when top_k is not specified by the caller",
    )

    @field_validator('num_chunks_post_search')
    @classmethod
    def validate_num_chunks_post_search(cls, v):
        """Validate num_chunks_post_search with warning fallback."""
        if not (isinstance(v, int) and v > 0):
            logger.warning(f"Setting num_chunks_post_search to default '10' as it is missing or malformed in the settings")
            return 10
        return v


class Settings(BaseSettings):
    common: CommonSettings = Field(default_factory=CommonSettings)
    similarity: SimilarityConfig = Field(default_factory=SimilarityConfig)

# Global settings instance
settings = Settings()

# Made with Bob