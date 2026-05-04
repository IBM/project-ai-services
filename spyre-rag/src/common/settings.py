"""
Configuration settings for RAG system.
These values can be overridden via environment variables.
"""
from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.misc_utils import get_logger

logger = get_logger("settings")


class LLMConfig(BaseSettings):
    """LLM model and generation settings."""

    # Context lengths
    granite_3_3_8b_instruct_context_length: int = Field(
        default=32768,
        ge=1,
        description="Context length for Granite 3.3 8B Instruct model",
    )

    # Token to word ratios
    token_to_word_ratio_en: float = Field(
        default=0.75,
        gt=0.0,
        le=2.0,
        description="Token to word ratio for English text",
    )

    # Generation parameters
    llm_max_tokens: int = Field(
        default=512,
        gt=0,
        description="Maximum tokens for LLM generation (English)",
    )

    llm_max_tokens_de: int = Field(
        default=700,
        gt=0,
        description="Maximum tokens for LLM generation (German)",
    )

    temperature: float = Field(
        default=0.0,
        ge=0.0,
        lt=1.0,
        description="Temperature for LLM generation",
    )

    max_input_length: int = Field(
        default=6000,
        ge=3000,
        le=32000,
        description="Maximum input length in characters",
    )

    # LLM connection pool / max batch size
    llm_max_batch_size: int = Field(
        default=32,
        ge=1,
        description="Maximum batch size for LLM service (used for connection pool size)",
    )

    @field_validator('llm_max_tokens')
    @classmethod
    def validate_llm_max_tokens(cls, v):
        """Validate llm_max_tokens with warning fallback."""
        if not (isinstance(v, int) and v > 0):
            logger.warning(f"Setting llm_max_tokens to default '512' as it is missing or malformed in the settings")
            return 512
        return v

    @field_validator('llm_max_tokens_de')
    @classmethod
    def validate_llm_max_tokens_de(cls, v):
        """Validate llm_max_tokens_de with warning fallback."""
        if not (isinstance(v, int) and v > 0):
            logger.warning(f"Setting llm_max_tokens_de to default '700' as it is missing or malformed in the settings")
            return 700
        return v

    @field_validator('temperature')
    @classmethod
    def validate_temperature(cls, v):
        """Validate temperature with warning fallback."""
        if not (isinstance(v, float) and 0 <= v < 1):
            logger.warning(f"Setting temperature to default '0.0' as it is missing or malformed in the settings")
            return 0.0
        return v

    @field_validator('max_input_length')
    @classmethod
    def validate_max_input_length(cls, v):
        """Validate max_input_length with warning fallback."""
        if not (isinstance(v, int) and 3000 <= v <= 32000):
            logger.warning(f"Setting max_input_length to default '6000' as it is missing or malformed in the settings")
            return 6000
        return v


class LanguageConfig(BaseSettings):
    """Language detection settings."""

    language_detection_min_confidence: float = Field(
        default=0.5,
        ge=0.0,
        le=1.0,
        description="Minimum confidence threshold for language detection",
    )

    @field_validator('language_detection_min_confidence')
    @classmethod
    def validate_language_detection_min_confidence(cls, v):
        """Validate language_detection_min_confidence with warning fallback."""
        if not isinstance(v, float):
            logger.warning(f"Setting language_detection_min_confidence to default '0.5' as it is missing in the settings")
            return 0.5
        return v


class AppConfig(BaseSettings):
    """Application-level configuration."""

    log_level: str = Field(
        default="INFO",
        description="Logging level (DEBUG, INFO, WARNING, ERROR)",
    )

    port: int = Field(
        default=5000,
        ge=1,
        le=65535,
        description="Application port number",
    )

    @field_validator('log_level')
    @classmethod
    def validate_log_level(cls, v):
        """Validate and normalize log level to uppercase."""
        if isinstance(v, str):
            return v.upper()
        return v


class ModelEndpointsConfig(BaseSettings):
    """Model endpoint configuration."""

    # Embedding model
    emb_endpoint: str = Field(
        default="",
        description="Embedding model endpoint URL",
    )

    emb_model: str = Field(
        default="",
        description="Embedding model name",
    )

    emb_max_tokens: int = Field(
        default=512,
        ge=1,
        description="Maximum tokens for embedding model",
    )

    # LLM model
    llm_endpoint: str = Field(
        default="",
        description="LLM endpoint URL",
    )

    llm_model: str = Field(
        default="",
        description="LLM model name",
    )

    # vLLM API Key for authentication (used by digitize and summarize services)
    vllm_api_key: str = Field(
        default="",
        description="API key for vLLM authentication (optional, read from VLLM_API_KEY env var)",
    )

    # Reranker model
    reranker_endpoint: str = Field(
        default="",
        description="Reranker endpoint URL",
    )

    reranker_model: str = Field(
        default="",
        description="Reranker model name",
    )


class VectorStoreConfig(BaseSettings):
    """Vector store configuration."""

    vector_store_type: str = Field(
        default="OPENSEARCH",
        description="Type of vector store (OPENSEARCH, etc.)",
    )

    # OpenSearch specific
    opensearch_host: str = Field(
        default="",
        description="OpenSearch host",
    )

    opensearch_port: str = Field(
        default="9200",
        description="OpenSearch port",
    )

    opensearch_username: str = Field(
        default="",
        description="OpenSearch username",
    )

    opensearch_password: str = Field(
        default="",
        description="OpenSearch password",
    )

    opensearch_db_prefix: str = Field(
        default="rag",
        description="OpenSearch database prefix",
    )

    opensearch_index_name: str = Field(
        default="default",
        description="OpenSearch index name",
    )

    opensearch_num_shards: int = Field(
        default=1,
        description="OpenSearch number of shards",
    )

    local_cache_dir: str = Field(
        default="/var/cache",
        description="Local cache directory for vector store operations",
    )


class Settings(BaseSettings):
    """Main settings class combining all common configuration sections."""

    app: AppConfig = Field(default_factory=AppConfig)
    llm: LLMConfig = Field(default_factory=LLMConfig)
    language: LanguageConfig = Field(default_factory=LanguageConfig)
    model_endpoints: ModelEndpointsConfig = Field(default_factory=ModelEndpointsConfig)
    vector_store: VectorStoreConfig = Field(default_factory=VectorStoreConfig)


# Global settings instance
settings = Settings()

# Made with Bob
