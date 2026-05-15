"""
Configuration settings for RAG system.
These values can be overridden via environment variables.
"""
from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.misc_utils import get_logger

logger = get_logger("settings")


class LLMConfig(BaseSettings):
    """LLM model configuration."""
    
    model_config = SettingsConfigDict(env_prefix='LLM_')

    endpoint: str = Field(
        default="",
        description="LLM endpoint URL",
    )

    model: str = Field(
        default="",
        description="LLM model name",
    )

    max_model_len: int = Field(
        default=32768,
        ge=1,
        description="Fallback maximum context length for the configured LLM model",
    )

    token_to_word_ratio_en: float = Field(
        default=0.75,
        gt=0.0,
        le=2.0,
        description="Token to word ratio for English text",
    )

    max_batch_size: int = Field(
        default=32,
        ge=1,
        description="Maximum batch size for LLM service (used for connection pool size)",
    )

    api_key: str = Field(
        default="",
        description="API key for vLLM authentication (optional, read from LLM_API_KEY env var)",
    )

    max_tokens: int = Field(
        default=512,
        gt=0,
        description="Maximum tokens for LLM generation (English)",
    )

    max_tokens_de: int = Field(
        default=700,
        gt=0,
        description="Maximum tokens for LLM generation (German)",
    )

    @field_validator('max_tokens')
    @classmethod
    def validate_max_tokens(cls, v):
        """Validate max_tokens with warning fallback."""
        if not (isinstance(v, int) and v > 0):
            logger.warning("Setting max_tokens to default '512' as it is missing or malformed in the settings")
            return 512
        return v

    @field_validator('max_tokens_de')
    @classmethod
    def validate_max_tokens_de(cls, v):
        """Validate max_tokens_de with warning fallback."""
        if not (isinstance(v, int) and v > 0):
            logger.warning("Setting max_tokens_de to default '700' as it is missing or malformed in the settings")
            return 700
        return v


class EmbeddingConfig(BaseSettings):
    """Embedding model configuration."""
    
    model_config = SettingsConfigDict(env_prefix='EMB_')

    endpoint: str = Field(
        default="",
        description="Embedding model endpoint URL",
    )

    model: str = Field(
        default="",
        description="Embedding model name",
    )

    max_model_len: int = Field(
        default=512,
        ge=1,
        description="Fallback maximum context length for the configured embedding model",
    )


class RerankerConfig(BaseSettings):
    """Reranker model configuration."""
    
    model_config = SettingsConfigDict(env_prefix='RERANKER_')

    endpoint: str = Field(
        default="",
        description="Reranker endpoint URL",
    )

    model: str = Field(
        default="",
        description="Reranker model name",
    )


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


class PromptsConfig(BaseSettings):
    """RAG prompt templates shared across services."""

    query_vllm_stream_prompt: str = Field(
        default=(
            "You are given:\n1. **A short context text** containing factual information.\n"
            "2. **A user's question** seeking clarification or advice.\n"
            "3. **Return a concise, to-the-point answer grounded strictly in the provided context.**\n\n"
            "The answer should be accurate, easy to follow, based on the context(s), and include clear reasoning or justification.\n"
            "If the context does not provide enough information, answer using your general knowledge.\n\n"
            "Context:\n{context}\n\nQuestion:\n{question}\n\nAnswer:"
        ),
        description="English prompt template for RAG query streaming",
    )

    query_vllm_stream_de_prompt: str = Field(
        default=(
            "Sie erhalten: 1. **Einen kurzen Kontexttext** mit sachlichen Informationen.\n"
            "2. **Die Frage eines Nutzers**, der um Klärung oder Rat bittet.\n"
            "3. **Geben Sie eine prägnante und aussagekräftige Antwort, die sich strikt auf den gegebenen Kontext stützt.**\n\n"
            "Die Antwort sollte korrekt, leicht verständlich und kontextbezogen sein sowie eine klare Begründung enthalten.\n"
            "Wenn der Kontext nicht genügend Informationen liefert, antworten Sie mit Ihrem Allgemeinwissen.\n\n"
            "Kontext:{context}\n\nFrage:{question}\n\nAntwort:"
        ),
        description="German prompt template for RAG query streaming",
    )

    prompt_template_token_count: int = Field(
        default=250,
        ge=0,
        description="Estimated token count for the RAG prompt template (used for context window math)",
    )


class Settings(BaseSettings):
    """Main settings class combining all common configuration sections."""

    app: AppConfig = Field(default_factory=AppConfig)
    llm: LLMConfig = Field(default_factory=LLMConfig)
    embedding: EmbeddingConfig = Field(default_factory=EmbeddingConfig)
    reranker: RerankerConfig = Field(default_factory=RerankerConfig)
    language: LanguageConfig = Field(default_factory=LanguageConfig)
    prompts: PromptsConfig = Field(default_factory=PromptsConfig)
    vector_store: VectorStoreConfig = Field(default_factory=VectorStoreConfig)


# Global settings instance
settings = Settings()

# Made with Bob
