"""
Configuration settings for the Embedding service.
These values can be overridden via environment variables.
"""
from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.settings import Settings as CommonSettings


class EmbeddingServiceConfig(BaseSettings):
    """Embedding service settings."""

    model_config = SettingsConfigDict(env_prefix='EMBEDDING_')

    model_name: str = Field(
        default="openai/clip-vit-base-patch32",
        description=(
            "Model to load at startup. Must be a DMG-approved model "
            "compatible with the vLLM runtime "
            "(e.g. openai/clip-vit-base-patch32, "
            "google/siglip-base-patch16-224)."
        ),
    )


class Settings(BaseSettings):
    common: CommonSettings = Field(default_factory=CommonSettings)
    embedding_service: EmbeddingServiceConfig = Field(
        default_factory=EmbeddingServiceConfig
    )


# Global settings instance
settings = Settings()
