"""
Language detection and prompt-building utilities for translation.

``resolve_source_language(text, requested_source, settings, async_path)``
    Resolves the declared ``source_language`` to a concrete language name
    (e.g. ``"German"``) via lingua detection, or signals that the LLM should
    auto-detect when lingua confidence is too low.

``build_messages(text, resolved_source_language, target_language)``
    Constructs the two-element ``[system, user]`` message list for the vLLM
    chat/completions call.  Selects the explicit-source template when a
    resolved language is provided, or the LLM auto-detect template otherwise.
"""

from typing import Optional

from common.lang_utils import detect_language, LanguageCodes
from common.misc_utils import get_logger
from translate.settings import Settings

logger = get_logger("prompt")

# ---------------------------------------------------------------------------
# Prompt templates (§10.1)
# ---------------------------------------------------------------------------

_SYSTEM_PROMPT = """\
You are a professional translator. Your task is to translate the provided text accurately and faithfully.

Rules:
- Translate ALL content including headings, paragraphs, bullet points, and table cell content
- Preserve ALL markdown formatting: headings (#, ##), bold (**), italic (*), bullet lists (-, *), numbered lists
- Preserve ALL markdown tables: keep the | pipe | structure and separator lines (|---|---|) exactly as-is; translate only the cell and header text
- Do NOT add any explanation, commentary, preamble, or notes
- Do NOT omit any part of the input
- Do NOT translate code blocks, URLs, proper nouns that are universally known, or technical identifiers
- Output ONLY the translated text, nothing else"""

_USER_PROMPT_EXPLICIT = """\
Translate the following text from {source_language} to {target_language}.

Text:
{text}

Translation:"""

_USER_PROMPT_AUTO_DETECT = """\
Detect the language of the following text and translate it to {target_language}.

Text:
{text}

Translation:"""

# ---------------------------------------------------------------------------
# ISO code → display name mapping
# ---------------------------------------------------------------------------

_ISO_TO_NAME: dict[str, str] = {
    LanguageCodes.ENGLISH: "English",
    LanguageCodes.GERMAN: "German",
    LanguageCodes.FRENCH: "French",
    LanguageCodes.ITALIAN: "Italian",
}


def iso_code_to_language_name(iso_code: str) -> Optional[str]:
    """Convert a lingua ISO-639-1 code (uppercase) to a display name, or None."""
    return _ISO_TO_NAME.get(iso_code.upper())


# ---------------------------------------------------------------------------
# Language resolution (§10.2)
# ---------------------------------------------------------------------------

def resolve_source_language(
    text: str,
    requested_source: str,
    settings: Settings,
    *,
    async_path: bool = False,
) -> tuple[Optional[str], Optional[str]]:
    """
    Resolve the caller-supplied ``source_language`` to a concrete language name
    (suitable for the explicit-source prompt template) or ``None`` (which triggers
    the LLM auto-detect prompt template).

    Detection strategy:
    - **Async path:** sample 500 chars at three evenly-spaced positions (start,
      middle, end) and take the majority vote.  This prevents an English-language
      header from overriding the body language.
    - **Sync path:** sample the first 500 chars directly.

    Args:
        text:             Full document text (async) or inline text (sync).
        requested_source: Value of ``source_language`` from the request
                          (e.g. ``"German"``, ``"auto"``, or ``""`).
        settings:         Service settings (provides min_confidence threshold).
        async_path:       Use three-position sampling when ``True``.

    Returns:
        ``(resolved_language_name, resolved_iso_code)``
        Both are ``None`` when lingua confidence is below threshold (LLM
        auto-detects).  When the caller passed an explicit supported language,
        both values are returned without running lingua at all.
    """
    min_confidence = settings.common.language.language_detection_min_confidence
    normalised = requested_source.strip().lower()

    # Explicit language supplied — validate and return immediately, no detection.
    if normalised not in ("", "auto"):
        # Map display name (case-insensitive) to ISO code for storage / logging.
        for iso, name in _ISO_TO_NAME.items():
            if name.lower() == normalised:
                logger.debug(f"Source language explicitly provided: {name} ({iso})")
                return name, iso
        # Unknown language — caller is responsible for validating allowlist before
        # this function is called; return None to fall back to LLM auto-detect.
        logger.warning(
            f"Explicit source_language '{requested_source}' not in ISO mapping; "
            "falling back to LLM auto-detect"
        )
        return None, None

    # Auto-detect path.
    if async_path:
        detected_code = _detect_majority_vote(text, min_confidence)
    else:
        sample = text[:500]
        detected_code = detect_language(sample, min_confidence=min_confidence)

    if detected_code:
        language_name = iso_code_to_language_name(detected_code)
        if language_name:
            logger.debug(
                f"Lingua resolved source language: {language_name} ({detected_code})"
            )
            return language_name, detected_code

    # Below-confidence fallback — let the LLM auto-detect.
    logger.debug("Lingua confidence below threshold; falling back to LLM auto-detect")
    return None, None


def _detect_majority_vote(text: str, min_confidence: float) -> Optional[str]:
    """
    Sample 500 characters at three evenly-spaced positions and return the
    language code that appears in the majority of samples (≥ 2 out of 3).

    Returns ``None`` if no majority or if all samples fall below *min_confidence*.
    """
    text_len = len(text)
    if text_len == 0:
        return None

    sample_size = 500
    positions = [0, max(0, text_len // 2 - sample_size // 2), max(0, text_len - sample_size)]

    votes: dict[str, int] = {}
    for pos in positions:
        sample = text[pos : pos + sample_size]
        code = detect_language(sample, min_confidence=min_confidence)
        if code:
            votes[code] = votes.get(code, 0) + 1

    if not votes:
        return None

    best_code = max(votes, key=lambda c: votes[c])
    if votes[best_code] >= 2:
        return best_code
    return None  # no majority — fall back to LLM auto-detect


# ---------------------------------------------------------------------------
# Message builder (§10.1)
# ---------------------------------------------------------------------------

def build_messages(
    text: str,
    resolved_source_language: Optional[str],
    target_language: str,
) -> list[dict]:
    """
    Build the two-element ``[system, user]`` message list for the vLLM call.

    Args:
        text:                     Chunk text (async) or full text (sync) to
                                  translate.
        resolved_source_language: Display-name of the source language (e.g.
                                  ``"German"``) if known, or ``None`` to use
                                  the LLM auto-detect template.
        target_language:          Display-name of the target language (e.g.
                                  ``"English"``).

    Returns:
        ``[{"role": "system", "content": …}, {"role": "user", "content": …}]``
    """
    if resolved_source_language:
        user_content = _USER_PROMPT_EXPLICIT.format(
            source_language=resolved_source_language,
            target_language=target_language,
            text=text,
        )
    else:
        user_content = _USER_PROMPT_AUTO_DETECT.format(
            target_language=target_language,
            text=text,
        )

    return [
        {"role": "system", "content": _SYSTEM_PROMPT},
        {"role": "user", "content": user_content},
    ]

# Made with Bob
