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
    
    class EnglishConfig(BaseSettings):
        """English-specific query rephrasing settings."""
        
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
            description="English prompt template for query rephrasing with placeholders: {conversation_history}, {current_query}"
        )
        
        role_labels: dict[str, str] = Field(
            default={
                "user": "User",
                "assistant": "Assistant",
                "system": "System",
                "unknown": "Unknown",
            },
            description="English role labels for conversation message formatting"
        )
        
        stop_sequences: list[str] = Field(
            default=["\n\n", "Question:", "Current Question:"],
            description="English stop sequences for LLM query rephrasing"
        )
    
    class GermanConfig(BaseSettings):
        """German-specific query rephrasing settings."""
        
        rephrase_prompt_template: str = Field(
            default=(
                "Erstelle anhand des Gesprächsverlaufs und der aktuellen Frage eine eigenständige Suchanfrage für die semantische Suche.\n\n"
                "Anweisungen:\n"
                "1. Wenn die aktuelle Frage bereits eigenständig und klar ist, gib sie GENAU unverändert zurück\n"
                "2. Wenn die Frage auf vorherigen Kontext verweist (z. B. mit Pronomen wie 'es', 'dies', 'diese', 'sie'), ersetze diese durch konkrete Begriffe aus dem Gesprächsverlauf\n"
                "3. Füge Kontext nur dann zusammen, wenn die aktuelle Frage eindeutig eine Anschlussfrage ist, die frühere Informationen benötigt\n"
                "4. Entferne überflüssige Gesprächsfloskeln (z. B. 'Kannst du mir sagen', 'Außerdem', 'Danke', 'Bitte')\n"
                "5. Halte die Suchanfrage kurz und auf die eigentliche Suchabsicht fokussiert\n"
                "6. Wenn der Gesprächsverlauf für die aktuelle Frage irrelevant ist, ignoriere ihn\n"
                "7. Gib NUR die umformulierte Suchanfrage zurück, ohne Erklärung oder zusätzlichen Text\n\n"
                "Gesprächsverlauf:\n{conversation_history}\n\n"
                "Aktuelle Frage: {current_query}\n\n"
                "Umformulierte Suchanfrage:"
            ),
            description="German prompt template for query rephrasing with placeholders: {conversation_history}, {current_query}"
        )
        
        role_labels: dict[str, str] = Field(
            default={
                "user": "Benutzer",
                "assistant": "Assistent",
                "system": "System",
                "unknown": "Unbekannt",
            },
            description="German role labels for conversation message formatting"
        )
        
        stop_sequences: list[str] = Field(
            default=["\n\n", "Frage:", "Aktuelle Frage:"],
            description="German stop sequences for LLM query rephrasing"
        )
        
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
    
    # Language-specific configurations
    english: EnglishConfig = Field(default_factory=EnglishConfig)
    german: GermanConfig = Field(default_factory=GermanConfig)

class LLMConfig(BaseSettings):
    """Chatbot-specific LLM generation settings."""
    
    class EnglishConfig(BaseSettings):
        """English-specific LLM settings."""
        
        max_tokens: int = Field(
            default=512,
            gt=0,
            description="Maximum tokens for LLM generation (English)",
        )
        
        @field_validator('max_tokens')
        @classmethod
        def validate_max_tokens(cls, v):
            """Validate max_tokens with warning fallback."""
            if not (isinstance(v, int) and v > 0):
                logger.warning("Setting max_tokens to default '512' as it is missing or malformed in the settings")
                return 512
            return v
    
    class GermanConfig(BaseSettings):
        """German-specific LLM settings."""
        
        max_tokens: int = Field(
            default=700,
            gt=0,
            description="Maximum tokens for LLM generation (German)",
        )
        
        @field_validator('max_tokens')
        @classmethod
        def validate_max_tokens(cls, v):
            """Validate max_tokens with warning fallback."""
            if not (isinstance(v, int) and v > 0):
                logger.warning("Setting max_tokens_de to default '700' as it is missing or malformed in the settings")
                return 700
            return v

    temperature: float = Field(
        default=0.0,
        ge=0.0,
        lt=1.0,
        description="Temperature for LLM generation",
    )

    # Language-specific configurations
    english: EnglishConfig = Field(default_factory=EnglishConfig)
    german: GermanConfig = Field(default_factory=GermanConfig)

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
    
    class EnglishConfig(BaseSettings):
        """English-specific RAG settings."""
        
        DEFAULT_SYSTEM_PROMPT: ClassVar[str] = (
            "You are a helpful, conversational AI assistant. "
            "The conversation language is fixed to be English"
            "Engage naturally with users across multiple turns of conversation. "
            "Provide clear, accurate, and contextually relevant responses. "
            "Reference previous exchanges when appropriate to maintain conversation flow."
            "Answer only the specific question asked. Do not add conversational filler, offer additional assistance, suggest follow-up steps, or ask follow-up questions at the end of your response. End your response immediately once the question has been answered."
        )
        
        system_prompt: str = Field(
            default=DEFAULT_SYSTEM_PROMPT,
            description="English conversational system prompt for session-based behavior",
        )
        
        query_system_prompt: str = Field(
            default=(
                "Session language: English\n\n"
                "Retrieved Context:\n{context}\n\n"
                "Rephrased Query: {rephrased_query}\n\n"
                "Instructions: Answer the user's question based on the retrieved context above. "
                "Consider the conversation history to provide contextually relevant responses. "
                "Be conversational and reference previous exchanges when relevant. "
                "If the context doesn't contain enough information, acknowledge this clearly."
            ),
            description="RAG system prompt template with context and rephrased query",
        )
    
    class GermanConfig(BaseSettings):
        """German-specific RAG settings."""
        
        DEFAULT_SYSTEM_PROMPT: ClassVar[str] = (
            "Sie sind ein hilfreicher, dialogorientierter KI-Assistent. "
            "Die Gesprächssprache ist für die gesamte Sitzung anhand der ersten Nachricht des Nutzers festgelegt. "
            "Antworten Sie immer nur in dieser Sitzungssprache, auch wenn spätere Nachrichten Sprachen mischen. "
            "Geben Sie klare, präzise und kontextbezogene Antworten. "
            "Beziehen Sie sich bei Bedarf auf frühere Nachrichten, um den Gesprächsfluss aufrechtzuerhalten. "
            "Beantworten Sie nur die konkret gestellte Frage. Fügen Sie keine Gesprächsfloskeln hinzu, "
            "bieten Sie keine zusätzliche Hilfe an, schlagen Sie keine nächsten Schritte vor und stellen Sie am Ende keine Rückfragen. "
            "Beenden Sie Ihre Antwort sofort, sobald die Frage beantwortet ist."
        )
        
        system_prompt: str = Field(
            default=DEFAULT_SYSTEM_PROMPT,
            description="German conversational system prompt for session-based behavior",
        )
        
        query_system_prompt: str = Field(
            default=(
                "Sitzungssprache: Deutsch\n\n"
                "Abgerufener Kontext:\n{context}\n\n"
                "Suchanfrage:\n{rephrased_query}\n\n"
                "Anweisungen: Beantworten Sie die aktuelle Frage des Nutzers anhand des oben abgerufenen Kontexts. "
                "Halten Sie einen natürlichen Gesprächsfluss aufrecht und beziehen Sie frühere Gesprächsbeiträge ein, wenn sie relevant sind. "
                "Antworten Sie ausschließlich auf Deutsch, weil die Sitzungssprache anhand der ersten Nachricht des Nutzers festgelegt wurde. "
                "Wenn der abgerufene Kontext nicht genügend Informationen enthält, sagen Sie das klar."
            ),
            description="German conversational RAG system prompt template with context and search query",
        )
    
    similarity_service_url: str = Field(
        default="http://similarity:8080",
        description="URL of the similarity search service"
    )

    rerank: bool = Field(
        default=True,
        description="Enable reranking of search results"
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

    llm_validate_custom_system_prompt: bool = Field(
        default=True,
        description="Enable/disable LLM-based validation for custom system prompts"
    )
    
    # Language-specific configurations
    english: EnglishConfig = Field(default_factory=EnglishConfig)
    german: GermanConfig = Field(default_factory=GermanConfig)
    
    # Single env vars that get applied based on language detection
    system_prompt: str = Field(
        default="",
        description="Custom system prompt (language auto-detected and applied to appropriate config)"
    )
    
    def model_post_init(self, __context):
        """
        Post-initialization to handle custom prompts with language detection and validation.
        
        Flow:
        1. Detect language of custom prompt
        2. Validate the prompt (if validation enabled)
        3. Override the appropriate language config only if validation passes
        4. Fallback to English on any failure
        """
        # Handle custom system_prompt
        if self.system_prompt:
            try:
                from common.lang_utils import detect_language, language_codes
                
                detected_lang = detect_language(self.system_prompt, min_confidence=0.7)
                
                # Fallback to English if unsupported language
                if detected_lang not in language_codes.values():
                    logger.warning(
                        f"Custom system_prompt detected as unsupported language ({detected_lang}). "
                        "Falling back to English."
                    )
                    detected_lang = language_codes["English"]
                
                logger.info(f"Custom system_prompt detected as: {detected_lang}")
                
                if self.llm_validate_custom_system_prompt:
                    try:
                        from chatbot.prompt_validator import validate_prompt_with_llm
                        if misc_utils.SESSION is None:
                            create_llm_session(pool_maxsize=1)
                        
                        validation_result = validate_prompt_with_llm(
                            self.system_prompt,
                            prompt_type="initial_system",
                            enable_semantic_check=True,
                            enable_injection_check=True,
                            language=detected_lang
                        )
                        
                        if not validation_result.is_valid():
                            logger.warning(
                                f"LLM validation failed for custom system_prompt: "
                                f"{validation_result.reason}. "
                                f"Falling back to default system prompt."
                            )
                            return
                        
                        logger.info(
                            f"LLM validation passed for custom system_prompt: "
                            f"All validation checks passed"
                        )
                    except Exception as e:
                        logger.warning(f"Error during LLM validation: {e}. Proceeding with basic validation only.")

                if detected_lang == language_codes["German"]:
                    self.german.system_prompt = self.system_prompt
                    logger.info("Applied custom system_prompt to German config")
                else:
                    self.english.system_prompt = self.system_prompt
                    logger.info("Applied custom system_prompt to English config")
                    
            except Exception as e:
                logger.warning(
                    f"Error processing custom system_prompt: {e}. "
                    "Falling back to English config."
                )
                self.english.system_prompt = self.system_prompt

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

    @field_validator('english', mode='after')
    @classmethod
    def validate_english_system_prompt(cls, v, info):
        """Validate English system_prompt with language detection, warning fallback and LLM validation."""
        default_prompt = cls.EnglishConfig.DEFAULT_SYSTEM_PROMPT
        system_prompt = v.system_prompt
        
        if system_prompt == default_prompt:
            return v
        
        # Basic validation: check if prompt is not empty and has reasonable length
        v_stripped = system_prompt.strip()
        if len(v_stripped) == 0:
            v.system_prompt = default_prompt
            return v
        
        if len(v_stripped) < 10:
            logger.warning(
                f"English system_prompt too short ({len(v_stripped)} chars). "
                "Falling back to default system prompt."
            )
            v.system_prompt = default_prompt
            return v
        
        if len(v_stripped) > 5000:
            logger.warning(
                f"English system_prompt too long ({len(v_stripped)} chars). "
                "Truncating to 5000 characters."
            )
            v_stripped = v_stripped[:5000]
        
        logger.info("Using custom English system_prompt")
        v.system_prompt = v_stripped
        return v

    @field_validator('german', mode='after')
    @classmethod
    def validate_german_system_prompt(cls, v, info):
        """Validate German system_prompt with language detection, warning fallback and LLM validation."""
        default_prompt = cls.GermanConfig.DEFAULT_SYSTEM_PROMPT
        system_prompt = v.system_prompt
        
        if system_prompt == default_prompt:
            return v
        
        # Basic validation: check if prompt is not empty and has reasonable length
        v_stripped = system_prompt.strip()
        if len(v_stripped) == 0:
            v.system_prompt = default_prompt
            return v
        
        if len(v_stripped) < 10:
            logger.warning(
                f"German system_prompt too short ({len(v_stripped)} chars). "
                "Falling back to default system prompt."
            )
            v.system_prompt = default_prompt
            return v
        
        if len(v_stripped) > 5000:
            logger.warning(
                f"German system_prompt too long ({len(v_stripped)} chars). "
                "Truncating to 5000 characters."
            )
            v_stripped = v_stripped[:5000]
        
        logger.info("Using custom German system_prompt")
        v.system_prompt = v_stripped
        return v

class Settings(BaseSettings):
    common: CommonSettings = Field(default_factory=CommonSettings)
    chatbot: RAGConfig = Field(default_factory=RAGConfig)
    llm: LLMConfig = Field(default_factory=LLMConfig)
    query_rephrasing: QueryRephrasingConfig = Field(default_factory=QueryRephrasingConfig)

# Global settings instance
settings = Settings()

# Made with Bob
