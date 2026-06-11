"""
Query rephrasing utilities for conversational RAG.

This module provides functionality to rephrase conversational queries into
self-contained, search-optimized queries using LLM-based transformation.
"""
import time
from typing import List, Dict, Optional

from common.misc_utils import get_logger
from common.retry_utils import retry_on_transient_error
from common.llm_utils import tokenize_with_llm, get_vllm_headers
from common.lang_utils import detect_language, LanguageCodes
import common.misc_utils as misc_utils

logger = get_logger("query_rephrasing")


def _get_query_rephrasing_language_config(lang: str, settings):
    """Return query rephrasing language config with English fallback."""
    language_config_map = {
        LanguageCodes.ENGLISH: settings.query_rephrasing.english,
        LanguageCodes.GERMAN: settings.query_rephrasing.german,
        LanguageCodes.ITALIAN: settings.query_rephrasing.italian,
        LanguageCodes.FRENCH: settings.query_rephrasing.french,
    }
    return language_config_map.get(lang, settings.query_rephrasing.english)


def calculate_dynamic_max_response_tokens(
    query: str,
    llm_endpoint: str,
    base_max_response_tokens: int,
    multiplier: float,
    system_max_query_length: int
) -> int:
    """
    Calculate dynamic max_response_tokens for query rephrasing based on input query length.
    
    Args:
        query: The input query to be rephrased
        llm_endpoint: LLM endpoint for tokenization
        base_max_response_tokens: Minimum baseline max_response_tokens (from config)
        multiplier: Multiplier for expansion (e.g., 1.2 = 20% expansion)
        system_max_query_length: System-wide max query token length
    
    Returns:
        Calculated max_response_tokens value
    """
    try:
        input_tokens = tokenize_with_llm(query, llm_endpoint)
        input_token_count = len(input_tokens)
        
        dynamic_max = int(input_token_count * multiplier)
        
        calculated_max_response_tokens = max(
            base_max_response_tokens,
            min(dynamic_max, system_max_query_length)
        )
        
        logger.debug(
            f"Dynamic max_response_tokens calculation: input={input_token_count} tokens, "
            f"dynamic={dynamic_max}, final={calculated_max_response_tokens} "
            f"(base={base_max_response_tokens}, multiplier={multiplier}, system_max={system_max_query_length})"
        )
        
        return calculated_max_response_tokens
        
    except Exception as e:
        logger.warning(
            f"Failed to calculate dynamic max_response_tokens: {e}. "
            f"Falling back to base_max_response_tokens={base_max_response_tokens}"
        )
        return base_max_response_tokens


def format_messages_for_rephrasing(messages: List[Dict[str, str]], lang: str = LanguageCodes.ENGLISH) -> str:
    """
    Format conversation messages into a readable string for rephrasing context.
    
    Converts a list of conversation messages into a formatted string with localized
    role labels based on the specified language. Supports English, German, and Italian.
    
    Args:
        messages: List of message dicts with 'role' and 'content' keys
                 (OpenAI message format). Each message should have:
                 - 'role': One of 'user', 'assistant', 'system', or 'unknown'
                 - 'content': The message content string
        lang: Language code for role labels (default: English).
              Supported values: LanguageCodes.ENGLISH, LanguageCodes.GERMAN, LanguageCodes.ITALIAN, LanguageCodes.FRENCH
    
    Returns:
        Formatted conversation history string with localized role labels.
        Returns empty string if messages list is empty.
    
    Example:
        >>> messages = [
        ...     {"role": "user", "content": "What is Spyre?"},
        ...     {"role": "assistant", "content": "Spyre is an AI accelerator..."}
        ... ]
        >>> format_messages_for_rephrasing(messages)
        'User: What is Spyre?\\nAssistant: Spyre is an AI accelerator...'
        >>> format_messages_for_rephrasing(messages, lang=LanguageCodes.GERMAN)
        'Benutzer: What is Spyre?\\nAssistent: Spyre is an AI accelerator...'
    """
    if not messages:
        return ""

    # Get role labels from settings
    from chatbot.settings import settings
    
    labels = _get_query_rephrasing_language_config(lang, settings).role_labels

    formatted_lines = []
    for msg in messages:
        role = (msg.get("role", "unknown") or "unknown").lower()
        content = msg.get("content", "")
        role_display = labels.get(role, role.capitalize())
        formatted_lines.append(f"{role_display}: {content}")

    return "\n".join(formatted_lines)


@retry_on_transient_error(max_retries=2, initial_delay=0.5, backoff_multiplier=2.0)
def call_llm_for_rephrasing(
    prompt: str,
    llm_endpoint: str,
    llm_model: str,
    max_tokens: int = 100,
    temperature: float = 0.0,
    timeout: float = 5.0,
    api_key: str | None = None,
    lang: str = LanguageCodes.ENGLISH
) -> str:
    """
    Call LLM to rephrase a query.
    
    Args:
        prompt: The complete rephrasing prompt
        llm_endpoint: LLM endpoint URL
        llm_model: LLM model name
        max_tokens: Maximum tokens for response
        temperature: Temperature for generation (0.0 = deterministic)
        timeout: Request timeout in seconds
        api_key: Optional API key for vLLM authentication
        lang: Language code for stop sequences (default: English)
    
    Returns:
        Rephrased query string
    
    Raises:
        RuntimeError: If LLM session not initialized
        requests.exceptions.RequestException: On HTTP errors
    """
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")
    
    # Get stop sequences from settings based on language
    from chatbot.settings import settings
    
    stop_sequences = _get_query_rephrasing_language_config(lang, settings).stop_sequences
    
    payload = {
        "model": llm_model,
        "messages": [
            {"role": "user", "content": prompt}
        ],
        "temperature": temperature,
        "max_tokens": max_tokens,
        "stop": stop_sequences,
        "stream": False,
    }
    
    logger.debug(f"Calling LLM for query rephrasing with timeout={timeout}s")
    
    headers = get_vllm_headers(api_key)
    
    response = misc_utils.SESSION.post(
        f"{llm_endpoint}/v1/chat/completions",
        json=payload,
        headers=headers,
        timeout=timeout
    )
    response.raise_for_status()
    
    data = response.json() or {}
    choices = data.get("choices", [])
    
    if not choices:
        logger.warning("LLM returned empty choices for query rephrasing")
        return ""
    
    # For chat completions API, the response is in message.content, not text
    message = choices[0].get("message", {})
    rephrased_text = message.get("content", "").strip()
    logger.debug(f"LLM rephrasing result: '{rephrased_text}'")
    
    return rephrased_text


async def rephrase_query_with_context(
    current_query: str,
    previous_messages: List[Dict[str, str]],
    llm_endpoint: str,
    llm_model: str,
    config: Optional[Dict] = None,
    api_key: str | None = None,
    lang: Optional[str] = None
) -> str:
    """
    Rephrase a conversational query to be self-contained using conversation context.
    
    This function transforms queries with pronouns and contextual references into
    standalone queries suitable for semantic search. It uses an LLM to understand
    the conversation context and reformulate the query.
    
    Args:
        current_query: The user's current query (may contain pronouns/references)
        previous_messages: List of previous conversation messages in OpenAI format
                          [{"role": "user", "content": "..."}, ...]
        llm_endpoint: LLM endpoint URL
        llm_model: LLM model name
        config: Optional configuration dict with keys:
               - timeout_seconds (float): Timeout for LLM call (default: 5.0)
               - max_tokens (int): Max tokens for rephrased query (default: 100)
               - temperature (float): Temperature for generation (default: 0.0)
        api_key: Optional API key for vLLM authentication
        lang: Optional language code (e.g., "EN", "DE"). If not provided, will detect automatically.
    
    Returns:
        Rephrased query string (or original query if rephrasing is skipped/fails)
    
    Example:
        >>> previous = [
        ...     {"role": "user", "content": "What is Spyre?"},
        ...     {"role": "assistant", "content": "Spyre is an AI accelerator..."}
        ... ]
        >>> await rephrase_query_with_context(
        ...     "Is it supported on Power 11?",
        ...     previous,
        ...     "http://llm:8000",
        ...     "model-name",
        ...     lang="EN"
        ... )
        'Is Spyre supported on Power 11?'
    """
    # Use provided lang or detect if not provided
    detected_lang = lang if lang is not None else detect_language(current_query)
    
    if detected_lang not in LanguageCodes.supported_languages():
        logger.debug("Query rephrasing skipped: unsupported language detected")
        return current_query

    # Get configuration from settings
    from chatbot.settings import settings
    
    # Always skip rephrasing if no conversation history
    if not previous_messages or len(previous_messages) == 0:
        logger.debug("Skipping query rephrasing: no conversation history")
        return current_query
    
    start_time = time.time()
    
    try:
        # Format conversation history
        conversation_history = format_messages_for_rephrasing(previous_messages, detected_lang)
        
        if not conversation_history:
            logger.debug("Skipping query rephrasing: empty conversation history")
            return current_query
        
        # Get language-specific prompt template from settings
        prompt_template = _get_query_rephrasing_language_config(detected_lang, settings).rephrase_prompt_template
        
        # Build rephrasing prompt
        prompt = prompt_template.format(
            conversation_history=conversation_history,
            current_query=current_query
        )
        
        logger.debug(f"Rephrasing query: '{current_query}'")
        
        # Calculate dynamic max_response_tokens based on input query length
        dynamic_max_response_tokens = calculate_dynamic_max_response_tokens(
            query=current_query,
            llm_endpoint=llm_endpoint,
            base_max_response_tokens=settings.query_rephrasing.max_response_tokens,
            multiplier=settings.query_rephrasing.max_response_tokens_multiplier,
            system_max_query_length=settings.chatbot.max_query_token_length
        )
        
        # Call LLM for rephrasing with dynamic max_response_tokens
        rephrased_query = call_llm_for_rephrasing(
            prompt=prompt,
            llm_endpoint=llm_endpoint,
            llm_model=llm_model,
            max_tokens=dynamic_max_response_tokens,
            temperature=settings.query_rephrasing.temperature,
            timeout=settings.query_rephrasing.timeout_seconds,
            api_key=api_key,
            lang=detected_lang
        )
        
        # Calculate latency
        latency_ms = (time.time() - start_time) * 1000
        
        # Handle empty response - always fallback to original
        if not rephrased_query or rephrased_query.strip() == "":
            logger.warning(f"LLM returned empty rephrased query (latency: {latency_ms:.0f}ms)")
            logger.info("Falling back to original query")
            return current_query
        
        logger.info(
            f"Query rephrased successfully (latency: {latency_ms:.0f}ms): "
            f"'{current_query}' -> '{rephrased_query}'"
        )
        
        return rephrased_query
    
    except Exception as e:
        latency_ms = (time.time() - start_time) * 1000
        logger.error(
            f"Error during query rephrasing (latency: {latency_ms:.0f}ms): {str(e)}",
            exc_info=True
        )
        
        # Always fallback to original query on error
        logger.info("Falling back to original query due to error")
        return current_query
