from re import I
from lingua import Language, LanguageDetectorBuilder

from common.misc_utils import get_logger
from common.settings import settings

logger = get_logger("LANG")

_language_detector = None

# Language codes class
class LanguageCodes:
    """Language codes as class attributes for easy access without dictionary keys.
    
    Provides both uppercase ISO codes (for LLM APIs) and lowercase codes (for sentence splitter).
    """
    ENGLISH = "EN"
    GERMAN = "DE"
    ITALIAN = "IT"
    FRENCH = "FR"
    
    # Mapping from uppercase ISO codes to SentenceSplitter language codes
    # Using class variables to avoid duplication
    _TO_SENTENCE_SPLITTER = {
        ENGLISH: "en",
        GERMAN: "de",
        ITALIAN: "it",
        FRENCH: "fr"
    }
    
    @classmethod
    def supported_languages(cls) -> list[str]:
        """Get list of supported language codes.
        
        Returns:
            List of supported language codes (e.g., ['EN', 'DE', 'IT', 'FR'])
        """
        return [cls.ENGLISH, cls.GERMAN, cls.ITALIAN, cls.FRENCH]
    
def to_sentence_splitter_lang(lingua_code: str) -> str:
    """
    Convert lingua ISO code to SentenceSplitter language code.
    
    Args:
        lingua_code: Lingua ISO code (e.g., 'EN', 'DE', 'IT', 'FR')
        
    Returns:
        SentenceSplitter language code (e.g., 'en', 'de', 'it', 'fr')
    """
    return LanguageCodes._TO_SENTENCE_SPLITTER.get(lingua_code, 'en')

def get_prompt_for_language(lang: str, prompts: dict[str, str]) -> str:
    """
    Get the appropriate prompt template based on language code.
    Used for non-English languages only (English uses conversational mode).

    Args:
        lang: Language code (DE, etc.)
        prompts: Dictionary mapping language codes to prompt templates

    Returns:
        The appropriate prompt template for the language, defaults to EN if not found
    """
    # Use the prompts dictionary passed as parameter
    return prompts.get(lang, prompts.get(LanguageCodes.ENGLISH, ""))

def get_max_tokens_map() -> dict[str, int]:
    """
    Get the max tokens map for different languages.
    Lazily imports from chatbot settings to avoid circular dependencies.
    
    Returns:
        Dictionary mapping language codes to max tokens
    """
    from chatbot.settings import settings as chatbot_settings
    return {
        LanguageCodes.ENGLISH: chatbot_settings.llm.english.max_tokens,
        LanguageCodes.GERMAN: chatbot_settings.llm.german.max_tokens,
        LanguageCodes.ITALIAN: chatbot_settings.llm.italian.max_tokens,
        LanguageCodes.FRENCH: chatbot_settings.llm.french.max_tokens,
    }

def setup_language_detector(languages: list[Language]):
    """Call once at app startup, before serving requests."""
    global _language_detector
    if _language_detector is not None:
        return
    _language_detector = (
        LanguageDetectorBuilder
        .from_languages(*languages)
        .with_preloaded_language_models()
        .build()
    )

def detect_language(text: str, min_confidence: float = settings.language.language_detection_min_confidence) -> str:
    """
    Detect the language of a text string.

    Returns a language code (EN, DE) if confidence >= min_confidence, else EN by default.
    Thread-safe — can be called from any endpoint or background task.
    """

    if not _language_detector:
        logger.warning("Lingua detector not initialized. Call setup_language_detector() at startup.")
        return LanguageCodes.ENGLISH

    confidences = _language_detector.compute_language_confidence_values(text)
    if confidences and confidences[0].value >= min_confidence:
        top = confidences[0]
        return top.language.iso_code_639_1.name
    return LanguageCodes.ENGLISH
