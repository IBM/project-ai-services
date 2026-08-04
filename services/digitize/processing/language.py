"""
Language detection and token utilities.

Responsibilities:
- Document language detection (sampling-based)
- Token counting via the embedding endpoint
- Header font-size collection and header-level derivation
- Sentence splitting helpers
"""

import random
from collections import Counter

from common.lang_utils import LanguageCodes, detect_language
from common.llm_utils import tokenize_with_llm
from common.misc_utils import get_logger

logger = get_logger("processing.language")


def count_tokens(text, emb_endpoint):
    token_len = len(tokenize_with_llm(text, emb_endpoint))
    return token_len


def detect_document_language(data) -> str:
    """
    Detect the language of a document by sampling random blocks.

    Args:
        data: List of document blocks, where each block is a dict with a 'text' field

    Returns:
        Lingua ISO 639-1 language code in uppercase format ('EN', 'DE', 'IT', 'FR').
        Falls back to 'EN' if detection fails or language is not supported.
    """
    default_lang = LanguageCodes.ENGLISH
    if not isinstance(data, list):
        logger.warning(f"Invalid input: expected list, got {type(data).__name__}, falling back to '{default_lang}'")
        return default_lang

    if not data:
        logger.warning(f"Empty data list provided for language detection, falling back to '{default_lang}'")
        return default_lang

    if not all(isinstance(block, dict) for block in data):
        logger.warning(f"Invalid input: data list contains non-dict elements, falling back to '{default_lang}'")
        return default_lang

    try:
        # Sample 3 random blocks from the data
        detected_languages = []
        # Generate random indices and collect valid text blocks without looping through all data
        sampled_blocks = []
        attempted_indices = set()
        max_attempts = min(len(data), 50)  # Limit attempts to avoid infinite loop

        # Keep trying random indices until we get 3 valid blocks or exhaust attempts
        while len(sampled_blocks) < 3 and len(attempted_indices) < max_attempts:
            # Generate a random index
            idx = random.randint(0, len(data) - 1)

            # Skip if already tried this index
            if idx in attempted_indices:
                continue

            attempted_indices.add(idx)

            # Check if this block has valid text
            block = data[idx]
            if isinstance(block.get("text"), str) and block.get("text", "").strip():
                sampled_blocks.append(block.get("text", ""))

        if not sampled_blocks:
            logger.warning(f"No text blocks found for language detection, falling back to '{default_lang}'")
            return default_lang

        for block_text in sampled_blocks:
            # Truncate to 500 characters
            chunk = block_text[:500]

            # Detect language for this chunk
            if chunk.strip():
                detected_lang = detect_language(chunk)
                detected_languages.append(detected_lang)

        if not detected_languages:
            logger.warning(f"No languages detected from samples, falling back to '{default_lang}'")
            return default_lang

        # Get the most common detected language (returns lingua ISO code like 'EN', 'DE')
        most_common_lang = Counter(detected_languages).most_common(1)[0][0]
        logger.debug(f"Detected languages: {detected_languages}, using: {most_common_lang}")

        # Check if the detected language is supported
        # Use the keys from _TO_SENTENCE_SPLITTER mapping which contains all supported languages
        supported_langs = list(LanguageCodes._TO_SENTENCE_SPLITTER.keys())
        if most_common_lang not in supported_langs:
            logger.warning(f"Detected language '{most_common_lang}' is not supported, falling back to '{default_lang}'")
            return default_lang

        return most_common_lang

    except Exception as e:
        logger.warning(f"Language detection failed: {e}, falling back to '{default_lang}'")
        return default_lang


def collect_header_font_sizes(elements):
    """
    elements: list of dicts with at least keys: 'label', 'font_size'
    Returns a sorted list of unique section_header font sizes, descending.
    """
    sizes = {
        el['font_size']
        for el in elements
        if el.get('label') == 'section_header' and el.get('font_size') is not None
    }
    return sorted(sizes, reverse=True)


def get_header_level(text, font_size, sorted_font_sizes):
    """
    Determine header level based on markdown syntax or font size hierarchy.
    """
    text = text.strip()

    # Priority 1: Markdown syntax
    if text.startswith('#'):
        level = len(text.strip()) - len(text.strip().lstrip('#'))
        return level, text.strip().lstrip('#').strip()

    # Priority 2: Font size ranking
    try:
        level = sorted_font_sizes.index(font_size) + 1
    except ValueError:
        level = len(sorted_font_sizes)

    return level, text
