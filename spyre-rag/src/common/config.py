"""
Configuration settings for RAG system.
These values can be overridden via environment variables.
"""
from pydantic import Field, field_validator, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.misc_utils import get_logger

logger = get_logger("settings")


class LLMConfig(BaseSettings):
    """LLM model and generation settings."""

    model_config = SettingsConfigDict(
        env_prefix="",
        case_sensitive=True,
        extra="ignore",
    )

    # Context lengths
    granite_3_3_8b_instruct_context_length: int = Field(
        alias="GRANITE_3_3_8B_INSTRUCT_CONTEXT_LENGTH",
        default=32768,
        ge=1,
        description="Context length for Granite 3.3 8B Instruct model",
    )

    # Token to word ratios
    token_to_word_ratio_en: float = Field(
        alias="TOKEN_TO_WORD_RATIO_EN",
        default=0.75,
        gt=0.0,
        le=2.0,
        description="Token to word ratio for English text",
    )

    # Generation parameters
    llm_max_tokens: int = Field(
        alias="LLM_MAX_TOKENS",
        default=512,
        gt=0,
        description="Maximum tokens for LLM generation (English)",
    )

    llm_max_tokens_de: int = Field(
        alias="LLM_MAX_TOKENS_DE",
        default=700,
        gt=0,
        description="Maximum tokens for LLM generation (German)",
    )

    temperature: float = Field(
        alias="TEMPERATURE",
        default=0.0,
        ge=0.0,
        lt=1.0,
        description="Temperature for LLM generation",
    )

    max_input_length: int = Field(
        alias="MAX_INPUT_LENGTH",
        default=6000,
        ge=3000,
        le=32000,
        description="Maximum input length in characters",
    )

    prompt_template_token_count: int = Field(
        alias="PROMPT_TEMPLATE_TOKEN_COUNT",
        default=250,
        ge=0,
        description="Estimated token count for prompt template",
    )

    # LLM connection pool
    llm_pool_size: int = Field(
        alias="LLM_POOL_SIZE",
        default=32,
        ge=1,
        description="Connection pool size for LLM service",
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

    @field_validator('prompt_template_token_count')
    @classmethod
    def validate_prompt_template_token_count(cls, v):
        """Validate prompt_template_token_count with warning fallback."""
        if not isinstance(v, int):
            logger.warning(f"Setting prompt_template_token_count to default '250' as it is missing in the settings")
            return 250
        return v



class LanguageConfig(BaseSettings):
    """Language detection settings."""

    model_config = SettingsConfigDict(
        env_prefix="",
        case_sensitive=True,
        extra="ignore",
    )

    language_detection_min_confidence: float = Field(
        alias="LANGUAGE_DETECTION_MIN_CONFIDENCE",
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

    model_config = SettingsConfigDict(
        env_prefix="",
        case_sensitive=True,
        extra="ignore",
    )

    log_level: str = Field(
        alias="LOG_LEVEL",
        default="INFO",
        description="Logging level (DEBUG, INFO, WARNING, ERROR)",
    )

    port: int = Field(
        alias="PORT",
        default=5000,
        ge=1,
        le=65535,
        description="Application port number",
    )


class ModelEndpointsConfig(BaseSettings):
    """Model endpoint configuration."""

    model_config = SettingsConfigDict(
        env_prefix="",
        case_sensitive=True,
        extra="ignore",
    )

    # Embedding model
    emb_endpoint: str = Field(
        alias="EMB_ENDPOINT",
        default="",
        description="Embedding model endpoint URL",
    )

    emb_model: str = Field(
        alias="EMB_MODEL",
        default="",
        description="Embedding model name",
    )

    emb_max_tokens: int = Field(
        alias="EMB_MAX_TOKENS",
        default=512,
        ge=1,
        description="Maximum tokens for embedding model",
    )

    # LLM model
    llm_endpoint: str = Field(
        alias="LLM_ENDPOINT",
        default="",
        description="LLM endpoint URL",
    )

    llm_model: str = Field(
        alias="LLM_MODEL",
        default="",
        description="LLM model name",
    )

    # Reranker model
    reranker_endpoint: str = Field(
        alias="RERANKER_ENDPOINT",
        default="",
        description="Reranker endpoint URL",
    )

    reranker_model: str = Field(
        alias="RERANKER_MODEL",
        default="",
        description="Reranker model name",
    )


class VectorStoreConfig(BaseSettings):
    """Vector store configuration."""

    model_config = SettingsConfigDict(
        env_prefix="",
        case_sensitive=True,
        extra="ignore",
    )

    vector_store_type: str = Field(
        alias="VECTOR_STORE_TYPE",
        default="OPENSEARCH",
        description="Type of vector store (OPENSEARCH, etc.)",
    )

    # OpenSearch specific
    opensearch_host: str = Field(
        alias="OPENSEARCH_HOST",
        default="",
        description="OpenSearch host",
    )

    opensearch_port: str = Field(
        alias="OPENSEARCH_PORT",
        default="9200",
        description="OpenSearch port",
    )

    opensearch_username: str = Field(
        alias="OPENSEARCH_USERNAME",
        default="",
        description="OpenSearch username",
    )

    opensearch_password: str = Field(
        alias="OPENSEARCH_PASSWORD",
        default="",
        description="OpenSearch password",
    )

    opensearch_db_prefix: str = Field(
        alias="OPENSEARCH_DB_PREFIX",
        default="rag",
        description="OpenSearch database prefix",
    )

    opensearch_index_name: str = Field(
        alias="OPENSEARCH_INDEX_NAME",
        default="default",
        description="OpenSearch index name",
    )

    local_cache_dir: str = Field(
        alias="LOCAL_CACHE_DIR",
        default="/var/cache",
        description="Local cache directory for vector store operations",
    )


class Settings(BaseSettings):
    """Main settings class combining all common configuration sections."""

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=True,
        extra="ignore",
    )

    app: AppConfig = Field(default_factory=AppConfig)
    llm: LLMConfig = Field(default_factory=LLMConfig)
    language: LanguageConfig = Field(default_factory=LanguageConfig)
    model_endpoints: ModelEndpointsConfig = Field(default_factory=ModelEndpointsConfig)
    vector_store: VectorStoreConfig = Field(default_factory=VectorStoreConfig)


# Global settings instance
settings = Settings()

# Backward compatibility: expose settings as module-level constants
# App settings
LOG_LEVEL = settings.app.log_level
PORT = settings.app.port

# Context lengths
GRANITE_3_3_8B_INSTRUCT_CONTEXT_LENGTH = settings.llm.granite_3_3_8b_instruct_context_length

# Token to word ratios
TOKEN_TO_WORD_RATIO_EN = settings.llm.token_to_word_ratio_en

# LLM settings
LLM_MAX_TOKENS = settings.llm.llm_max_tokens
LLM_MAX_TOKENS_DE = settings.llm.llm_max_tokens_de
TEMPERATURE = settings.llm.temperature
MAX_INPUT_LENGTH = settings.llm.max_input_length
PROMPT_TEMPLATE_TOKEN_COUNT = settings.llm.prompt_template_token_count
LLM_POOL_SIZE = settings.llm.llm_pool_size

# Language detection
LANGUAGE_DETECTION_MIN_CONFIDENCE = settings.language.language_detection_min_confidence

# Model endpoints
EMB_ENDPOINT = settings.model_endpoints.emb_endpoint
EMB_MODEL = settings.model_endpoints.emb_model
EMB_MAX_TOKENS = settings.model_endpoints.emb_max_tokens
LLM_ENDPOINT = settings.model_endpoints.llm_endpoint
LLM_MODEL = settings.model_endpoints.llm_model
RERANKER_ENDPOINT = settings.model_endpoints.reranker_endpoint
RERANKER_MODEL = settings.model_endpoints.reranker_model

# Vector store
VECTOR_STORE_TYPE = settings.vector_store.vector_store_type
OPENSEARCH_HOST = settings.vector_store.opensearch_host
OPENSEARCH_PORT = settings.vector_store.opensearch_port
OPENSEARCH_USERNAME = settings.vector_store.opensearch_username
OPENSEARCH_PASSWORD = settings.vector_store.opensearch_password
OPENSEARCH_DB_PREFIX = settings.vector_store.opensearch_db_prefix
OPENSEARCH_INDEX_NAME = settings.vector_store.opensearch_index_name
LOCAL_CACHE_DIR = settings.vector_store.local_cache_dir

# Service-specific configs are now in their respective modules:
# - digitize.config for digitize service settings
# - chatbot.config for RAG/chatbot settings
# - summarize.config for summarization settings

# Made with Bob
