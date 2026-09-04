from __future__ import annotations

import threading
from typing import Dict

import spacy
from lingua import Language, LanguageDetectorBuilder
from sentence_splitter import SentenceSplitter
from spacy.language import Language as SpacyLanguage

from common.misc_utils import get_logger
from common.settings import settings

logger = get_logger("LANG")

_language_detector = None

# ---------------------------------------------------------------------------
# Language codes
# ---------------------------------------------------------------------------

class LanguageCodes:
    """Language codes as class attributes for easy access without dictionary keys.

    Provides both uppercase ISO codes (for LLM APIs) and lowercase codes (for
    sentence splitting).
    """
    ENGLISH = "EN"
    GERMAN = "DE"
    ITALIAN = "IT"
    FRENCH = "FR"

    # Mapping from uppercase ISO codes to ISO-639-1 lowercase splitter codes
    _TO_SENTENCE_SPLITTER = {
        ENGLISH: "en",
        GERMAN: "de",
        ITALIAN: "it",
        FRENCH: "fr",
    }

    _SUPPORTED: frozenset = frozenset({"EN", "DE", "IT", "FR"})

    @classmethod
    def supported_languages(cls) -> frozenset:
        """Get set of supported language codes.

        Returns:
            frozenset of supported language codes (e.g., {'EN', 'DE', 'IT', 'FR'})
        """
        return cls._SUPPORTED


def to_sentence_splitter_lang(lingua_code: str) -> str:
    """Convert a lingua ISO code to a lowercase sentence-splitter language code.

    Args:
        lingua_code: Lingua ISO code (e.g., 'EN', 'DE', 'IT', 'FR')

    Returns:
        Lowercase ISO-639-1 language code (e.g., 'en', 'de', 'it', 'fr').
        Falls back to 'en' for unrecognised codes.
    """
    return LanguageCodes._TO_SENTENCE_SPLITTER.get(lingua_code, "en")


# ---------------------------------------------------------------------------
# Sentence splitting
#
# Routing:
#   "ja" → spaCy ja_core_news_sm  (requires sudachipy + sudachidict-core)
#   all others → sentence-splitter (rule-based, falls back to "en")
# ---------------------------------------------------------------------------

_JA_MODEL = "ja_core_news_sm"
_JA_LANG = "ja"

_spacy_cache: Dict[str, SpacyLanguage] = {}
_spacy_cache_lock = threading.Lock()


def _load_ja_model() -> SpacyLanguage:
    """Load and cache the Japanese spaCy model."""
    with _spacy_cache_lock:
        if _JA_LANG not in _spacy_cache:
            logger.debug(f"Loading spaCy model '{_JA_MODEL}' for language 'ja'")
            nlp = spacy.load(_JA_MODEL, exclude=["ner", "lemmatizer", "morphologizer"])
            if "sentencizer" not in nlp.pipe_names and "senter" not in nlp.pipe_names and "parser" not in nlp.pipe_names:
                nlp.add_pipe("sentencizer")
            _spacy_cache[_JA_LANG] = nlp
        return _spacy_cache[_JA_LANG]


_SENTENCE_SPLITTER_LANGS = {"en", "de", "it", "fr"}
_DEFAULT_SPLIT_LANG = "en"

_splitter_cache: Dict[str, SentenceSplitter] = {}
_splitter_cache_lock = threading.Lock()


def _get_splitter(lang: str) -> SentenceSplitter:
    """Return a cached SentenceSplitter instance for *lang*."""
    resolved = lang if lang in _SENTENCE_SPLITTER_LANGS else _DEFAULT_SPLIT_LANG
    with _splitter_cache_lock:
        if resolved not in _splitter_cache:
            logger.debug(f"Creating SentenceSplitter for language '{resolved}'")
            _splitter_cache[resolved] = SentenceSplitter(language=resolved)
        return _splitter_cache[resolved]


def split_sentences(text: str, lang: str = _DEFAULT_SPLIT_LANG) -> list[str]:
    """Split *text* into a list of sentence strings.

    Uses spaCy for Japanese (``"ja"``); uses ``sentence-splitter`` for all
    other languages, falling back to ``"en"`` for unrecognised codes.

    Args:
        text: Input text to split.
        lang: Lowercase ISO-639-1 language code (e.g. ``"en"``, ``"ja"``).
              Defaults to ``"en"``.

    Returns:
        List of non-empty sentence strings in document order.
    """
    if not text or not text.strip():
        return []

    if lang == _JA_LANG:
        nlp = _load_ja_model()
        doc = nlp(text)
        return [sent.text.strip() for sent in doc.sents if sent.text.strip()]

    splitter = _get_splitter(lang)
    return [s.strip() for s in splitter.split(text) if s.strip()]


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
