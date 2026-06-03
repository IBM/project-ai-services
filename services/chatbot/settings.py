"""
Configuration settings for Chatbot/RAG service.
These values can be overridden via environment variables.
"""
from typing import ClassVar
from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from common.misc_utils import get_logger, create_llm_session
import common.misc_utils as misc_utils
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


class LLMConfig(BaseSettings):
    """Chatbot-specific LLM generation settings."""

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

    temperature: float = Field(
        default=0.0,
        ge=0.0,
        lt=1.0,
        description="Temperature for LLM generation",
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

    @field_validator('temperature')
    @classmethod
    def validate_temperature(cls, v):
        """Validate temperature with warning fallback."""
        if not (isinstance(v, float) and 0 <= v < 1):
            logger.warning("Setting temperature to default '0.0' as it is missing or malformed in the settings")
            return 0.0
        return v


class RAGConfig(BaseSettings):
    """RAG retrieval and ranking settings."""
    
    model_config = SettingsConfigDict(env_prefix="CHATBOT_")
    DEFAULT_SYSTEM_PROMPT: ClassVar[str] = (
        "You are a helpful, conversational AI assistant. "
        "Engage naturally with users across multiple turns of conversation. "
        "Provide clear, accurate, and contextually relevant responses. "
        "Reference previous exchanges when appropriate to maintain conversation flow."
        "Answer only the specific question asked. Do not add conversational filler, offer additional assistance, suggest follow-up steps, or ask follow-up questions at the end of your response. End your response immediately once the question has been answered."
    )

    search_mode: str = Field(
        default="hybrid",
        description="Search mode for document retrieval (e.g., 'hybrid', 'dense', 'sparse')"
    )

    score_threshold: float = Field(
        default=0.4,
        gt=0.0,
        lt=1.0,
        description="Minimum similarity score threshold for retrieval",
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

    system_prompt: str = Field(
        default=DEFAULT_SYSTEM_PROMPT,
        description="Initial system prompt for conversational behavior",
    )

    query_system_prompt: str = Field(
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
    legacy_query_vllm_stream_en_prompt: str = Field(
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

    # Chatbot prompt (non-conversational) for German language.
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

    llm_validate_custom_system_prompt: bool = Field(
        default=True,
        description="Enable/disable LLM-based validation for custom system prompts"
    )

    @field_validator('score_threshold')
    @classmethod
    def validate_score_threshold(cls, v):
        """Validate score threshold with warning fallback."""
        if not (isinstance(v, float) and 0 < v < 1):
            logger.warning(f"Setting score threshold to default '0.4' as it is missing or malformed in the settings")
            return 0.4
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
    @field_validator('system_prompt', mode='after')
    @classmethod
    def validate_system_prompt(cls, v, info):
        """Validate system_prompt with language detection, warning fallback and LLM validation."""
        default_prompt = cls.DEFAULT_SYSTEM_PROMPT
        
        # Basic validation: check if prompt is not empty and has reasonable length
        v_stripped = v.strip()
        if len(v_stripped) == 0:
            return default_prompt
        
        if len(v_stripped) < 10:
            logger.warning(
                f"system_prompt too short ({len(v_stripped)} chars). "
                "Falling back to default system prompt."
            )
            return default_prompt
        
        if len(v_stripped) > 5000:
            logger.warning(
                f"system_prompt too long ({len(v_stripped)} chars). "
                "Truncating to 5000 characters."
            )
            v_stripped = v_stripped[:5000]
        
        # Language detection: Only allow English custom system prompts
        try:
            from common.lang_utils import detect_language, lang_en
            detected_lang = detect_language(v_stripped, min_confidence=0.7)
            if detected_lang != lang_en:
                logger.warning(
                    f"Custom system prompt detected as non-English language ({detected_lang}). "
                    "Custom system prompts are only supported in English. "
                    "Falling back to default system prompt."
                )
                return default_prompt
            logger.info("Custom system prompt language validation passed (English detected)")
        except Exception as e:
            logger.warning(f"Error during language detection for custom system prompt: {e}. Proceeding with validation.")
        
        # LLM-based validation (if enabled)
        llm_validation_enabled = info.data.get('llm_validate_custom_system_prompt', True)
        if llm_validation_enabled:
            try:
                from chatbot.prompt_validator import validate_prompt_with_llm
                if misc_utils.SESSION is None:
                    create_llm_session(pool_maxsize=1)
                
                validation_result = validate_prompt_with_llm(
                    v_stripped,
                    prompt_type="initial_system",
                    enable_semantic_check=True,
                    enable_injection_check=True
                )
                
                if not validation_result.is_valid():
                    logger.warning(
                        f"LLM validation failed for system_prompt: "
                        f"{validation_result.reason}. "
                        f"Falling back to default system prompt."
                    )
                    return default_prompt
                
                logger.info(
                    f"LLM validation passed for system_prompt: "
                    f"{validation_result.reason}"
                )
            except Exception as e:
                logger.warning(f"Error during LLM validation: {e}. Proceeding with basic validation only.")
        
        logger.info("Using custom system_prompt from environment")
        return v_stripped

class Settings(BaseSettings):
    common: CommonSettings = Field(default_factory=CommonSettings)
    chatbot: RAGConfig = Field(default_factory=RAGConfig)
    llm: LLMConfig = Field(default_factory=LLMConfig)
    query_rephrasing: QueryRephrasingConfig = Field(default_factory=QueryRephrasingConfig)

# Global settings instance
settings = Settings()

# Made with Bob
