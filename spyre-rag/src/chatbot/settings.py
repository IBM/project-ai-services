"""
Configuration settings for Chatbot/RAG service.
These values can be overridden via environment variables.
"""
from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.misc_utils import get_logger
from common.settings import Settings as CommonSettings

logger = get_logger("settings")


class QueryRephrasingConfig(BaseSettings):
    """Query rephrasing configuration for conversational RAG."""
    
    model_config = SettingsConfigDict(env_prefix="QUERY_REPHRASING_")
    
    timeout_seconds: float = Field(
        default=5.0,
        gt=0,
        description="Timeout for rephrasing LLM call in seconds"
    )
    
    max_response_tokens: int = Field(
        default=100,
        gt=0,
        le=615,
        description="Maximum tokens for rephrased query response (used as minimum baseline)"
    )
    
    max_response_tokens_multiplier: float = Field(
        default=1.2,
        gt=1.0,
        le=2.0,
        description="Multiplier for dynamic max_response_tokens calculation based on input query length"
    )
    
    temperature: float = Field(
        default=0.0,
        ge=0.0,
        le=1.0,
        description="Temperature for rephrasing (0=deterministic)"
    )
    
    history_token_budget: int = Field(
        default=1000,
        gt=0,
        description="Maximum tokens allocated for conversation history during query rephrasing"
    )
    
    rephrase_prompt_template: str = Field(
        default=(
            "Given the conversation history and the current question, create a standalone query for semantic search.\n\n"
            "Instructions:\n"
            "1. If the current question is already standalone and clear, return it EXACTLY as-is (preserve original wording)\n"
            "2. If the question references previous context (uses pronouns like 'it', 'this', 'that', 'they'), replace them with specific nouns from the conversation history\n"
            "3. Only merge context if the current question is clearly a follow-up that requires previous information\n"
            "4. Remove conversational filler words (e.g., 'Can you tell me', 'Also', 'Thanks', 'Please')\n"
            "5. Keep the query concise and focused on the core search intent\n"
            "6. If the conversation history is irrelevant to the current question, ignore it\n"
            "7. Return ONLY the rephrased query, no explanation or additional text\n\n"
            "Conversation History:\n{conversation_history}\n\n"
            "Current Question: {current_query}\n\n"
            "Rephrased Query:"
        ),
        description="Prompt template for query rephrasing with placeholders: {conversation_history}, {current_query}"
    )


class RAGConfig(BaseSettings):
    """RAG retrieval and ranking settings."""

    conversational_mode: bool = Field(
        default=False,
        description="Enable conversational RAG mode with query rephrasing and context management"
    )

    score_threshold: float = Field(
        default=0.4,
        gt=0.0,
        lt=1.0,
        description="Minimum similarity score threshold for retrieval",
    )

    max_concurrent_requests: int = Field(
        default=32,
        gt=0,
        description="Maximum concurrent requests for RAG operations",
    )

    num_chunks_post_search: int = Field(
        default=10,
        gt=5,
        le=15,
        description="Number of chunks to retrieve after initial search",
    )

    num_chunks_post_reranker: int = Field(
        default=3,
        gt=1,
        le=5,
        description="Number of chunks to keep after reranking",
    )

    max_query_token_length: int = Field(
        default=512,
        gt=0,
        description="Maximum token length for user queries",
    )

    prompt_template_token_count: int = Field(
        default=250,
        ge=0,
        description="Estimated token count for query prompt template",
    )

    initial_system_message: str = Field(
        default=(
            "You are a helpful, conversational AI assistant. "
            "Engage naturally with users across multiple turns of conversation. "
            "Provide clear, accurate, and contextually relevant responses. "
            "Reference previous exchanges when appropriate to maintain conversation flow."
        ),
        description="Initial system prompt for conversational behavior",
    )

    rag_system_message: str = Field(
        default=(
            "Retrieved Context:\n{context}\n\n"
            "Rephrased Query: {rephrased_query}\n\n"
            "Instructions: Answer the user's question based on the retrieved context above. "
            "Consider the conversation history to provide contextually relevant responses. "
            "Be conversational and reference previous exchanges when relevant. "
            "If the context doesn't contain enough information, acknowledge this clearly."
        ),
        description="RAG system prompt template with context and rephrased query",
    )

    history_token_budget: int = Field(
        default=2000,
        gt=0,
        description="Maximum tokens allocated for conversation history",
    )

    initial_system_token_overhead: int = Field(
        default=100,
        gt=0,
        description="Estimated tokens for initial system message",
    )

    rag_system_token_overhead: int = Field(
        default=200,
        gt=0,
        description="Estimated tokens for RAG system message (excluding context)",
    )

    # Legacy prompt fields retained for compatibility with language prompt helpers.
    query_vllm_stream_prompt: str = Field(
        default=(
            "You are given:\n1. **A short context text** containing factual information.\n"
            "2. **A user's question** seeking clarification or advice.\n"
            "3. **Return a concise, to-the-point answer grounded strictly in the provided context.**\n\n"
            "The answer should be accurate, easy to follow, based on the context(s), and include clear reasoning or justification.\n"
            "If the context does not provide enough information, answer using your general knowledge.\n\n"
            "Context:\n{context}\n\nQuestion:\n{question}\n\nAnswer:"
        ),
        description="Legacy English prompt template for query streaming",
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

    @field_validator('prompt_template_token_count')
    @classmethod
    def validate_prompt_template_token_count(cls, v):
        """Validate prompt_template_token_count with warning fallback."""
        if not isinstance(v, int):
            logger.warning(f"Setting prompt_template_token_count to default '250' as it is missing in the settings")
            return 250
        return v


class Settings(BaseSettings):
    common: CommonSettings = Field(default_factory=CommonSettings)
    chatbot: RAGConfig = Field(default_factory=RAGConfig)
    query_rephrasing: QueryRephrasingConfig = Field(default_factory=QueryRephrasingConfig)

# Global settings instance
settings = Settings()

# Made with Bob
