from lingua import Language, LanguageDetectorBuilder
from common.config import LANGUAGE_DETECTION_MIN_CONFIDENCE, LLM_MAX_TOKENS, LLM_MAX_TOKENS_DE

from common.misc_utils import get_logger
from chatbot.config import QUERY_VLLM_STREAM_DE_PROMPT, QUERY_VLLM_STREAM_PROMPT

logger = get_logger("LANG")

_language_detector = None
lang_en = "EN"
lang_de = "DE"

def get_prompt_for_language(lang: str) -> str:
    """
    Get the appropriate prompt template based on language code.
    This is extensible to more languages easily.
    
    Args:
        lang: Language code (EN, DE, etc.)
    
    Returns:
        The appropriate prompt template for the language
    """
    prompt_map = {
        lang_de: QUERY_VLLM_STREAM_DE_PROMPT,
        lang_en: QUERY_VLLM_STREAM_PROMPT
    }
    return prompt_map.get(lang, QUERY_VLLM_STREAM_PROMPT)  # Default to English if language not found

max_tokens_map = {
                lang_en: LLM_MAX_TOKENS,
                lang_de: LLM_MAX_TOKENS_DE
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

def detect_language(text: str, min_confidence: float = LANGUAGE_DETECTION_MIN_CONFIDENCE) -> str:
    """
    Detect the language of a text string.

    Returns a language code (EN, DE) if confidence >= min_confidence, else EN by default.
    Thread-safe — can be called from any endpoint or background task.
    """
    if not _language_detector:
        logger.warning("Lingua detector not initialized. Call setup_language_detector() at startup.")
        return lang_en

    confidences = _language_detector.compute_language_confidence_values(text)
    if confidences and confidences[0].value >= min_confidence:
        top = confidences[0]
        return top.language.iso_code_639_1.name
    return lang_en
