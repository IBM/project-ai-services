"""
Configuration settings for Chatbot/RAG service.
These values can be overridden via environment variables.
"""
from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.misc_utils import get_logger

logger = get_logger("settings")


class RAGConfig(BaseSettings):
    """RAG retrieval and ranking settings."""

    model_config = SettingsConfigDict(
        env_prefix="",
        case_sensitive=True,
        extra="ignore",
    )

    score_threshold: float = Field(
        alias="SCORE_THRESHOLD",
        default=0.4,
        gt=0.0,
        lt=1.0,
        description="Minimum similarity score threshold for retrieval",
    )

    max_concurrent_requests: int = Field(
        alias="MAX_CONCURRENT_REQUESTS",
        default=32,
        gt=0,
        description="Maximum concurrent requests for RAG operations",
    )

    num_chunks_post_search: int = Field(
        alias="NUM_CHUNKS_POST_SEARCH",
        default=10,
        gt=5,
        le=15,
        description="Number of chunks to retrieve after initial search",
    )

    num_chunks_post_reranker: int = Field(
        alias="NUM_CHUNKS_POST_RERANKER",
        default=3,
        gt=1,
        le=5,
        description="Number of chunks to keep after reranking",
    )

    max_query_token_length: int = Field(
        alias="MAX_QUERY_TOKEN_LENGTH",
        default=512,
        gt=0,
        description="Maximum token length for user queries",
    )

    # Query streaming prompts
    query_vllm_stream_prompt: str = Field(
        alias="QUERY_VLLM_STREAM_PROMPT",
        default=(
            "You are given:\n1. **A short context text** containing factual information.\n"
            "2. **A user's question** seeking clarification or advice.\n"
            "3. **Return a concise, to-the-point answer grounded strictly in the provided context.**\n\n"
            "The answer should be accurate, easy to follow, based on the context(s), and include clear reasoning or justification.\n"
            "If the context does not provide enough information, answer using your general knowledge.\n\n"
            "Context:\n{context}\n\nQuestion:\n{question}\n\nAnswer:"
        ),
        description="English prompt template for query streaming",
    )

    query_vllm_stream_de_prompt: str = Field(
        alias="QUERY_VLLM_STREAM_DE_PROMPT",
        default=(
            "Sie erhalten: 1. **Einen kurzen Kontexttext** mit sachlichen Informationen.\n"
            "2. **Die Frage eines Nutzers**, der um Klärung oder Rat bittet.\n"
            "3. **Geben Sie eine prägnante und aussagekräftige Antwort, die sich strikt auf den gegebenen Kontext stützt.**\n\n"
            "Die Antwort sollte korrekt, leicht verständlich und kontextbezogen sein sowie eine klare Begründung enthalten.\n"
            "Wenn der Kontext nicht genügend Informationen liefert, antworten Sie mit Ihrem Allgemeinwissen.\n\n"
            "Kontext:{context}\n\nFrage:{question}\n\nAntwort:"
        ),
        description="German prompt template for query streaming",
    )

    @field_validator('score_threshold')
    @classmethod
    def validate_score_threshold(cls, v):
        """Validate score threshold with warning fallback."""
        if not (isinstance(v, float) and 0 < v < 1):
            logger.warning(f"Setting score threshold to default '0.4' as it is missing or malformed in the settings")
            return 0.4
        return v

    @field_validator('max_concurrent_requests')
    @classmethod
    def validate_max_concurrent_requests(cls, v):
        """Validate max concurrent requests with warning fallback."""
        if not (isinstance(v, int) and v > 0):
            logger.warning(f"Setting max_concurrent_requests to default '32' as it is missing or malformed in the settings")
            return 32
        return v

    @field_validator('num_chunks_post_search')
    @classmethod
    def validate_num_chunks_post_search(cls, v):
        """Validate num chunks post search with warning fallback."""
        if not (isinstance(v, int) and 5 < v <= 15):
            logger.warning(f"Setting num_chunks_post_search to default '10' as it is missing or malformed in the settings")
            return 10
        return v

    @field_validator('num_chunks_post_reranker')
    @classmethod
    def validate_num_chunks_post_reranker(cls, v):
        """Validate num chunks post reranker with warning fallback."""
        if not (isinstance(v, int) and 1 < v <= 5):
            logger.warning(f"Setting num_chunks_post_reranker to default '3' as it is missing or malformed in the settings")
            return 3
        return v


# Global settings instance
settings = RAGConfig()

# Backward compatibility: expose settings as module-level constants
SCORE_THRESHOLD = settings.score_threshold
MAX_CONCURRENT_REQUESTS = settings.max_concurrent_requests
NUM_CHUNKS_POST_SEARCH = settings.num_chunks_post_search
NUM_CHUNKS_POST_RERANKER = settings.num_chunks_post_reranker
MAX_QUERY_TOKEN_LENGTH = settings.max_query_token_length
QUERY_VLLM_STREAM_PROMPT = settings.query_vllm_stream_prompt
QUERY_VLLM_STREAM_DE_PROMPT = settings.query_vllm_stream_de_prompt

# Made with Bob
