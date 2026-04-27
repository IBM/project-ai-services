from lingua import Language, LanguageDetectorBuilder

from common.misc_utils import get_logger
from chatbot.settings import settings

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
        lang_de: settings.chatbot.query_vllm_stream_de_prompt,
        lang_en: settings.chatbot.query_vllm_stream_prompt
    }
    return prompt_map.get(lang, settings.chatbot.query_vllm_stream_prompt)

max_tokens_map = {
                lang_en: settings.common.llm.llm_max_tokens,
                lang_de: settings.common.llm.llm_max_tokens_de
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

def detect_language(text: str, min_confidence: float = settings.common.language.language_detection_min_confidence) -> str:
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
