"""
Query rephrasing utilities for conversational RAG.

This module provides functionality to rephrase conversational queries into
self-contained, search-optimized queries using LLM-based transformation.

Note: Query rephrasing is only active when conversational_mode is enabled
in the RAGConfig settings.
"""
import time
from typing import List, Dict, Optional

from common.misc_utils import get_logger
from common.retry_utils import retry_on_transient_error
from common.llm_utils import tokenize_with_llm, get_vllm_headers
import common.misc_utils as misc_utils

logger = get_logger("query_rephrasing")


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


def format_messages_for_rephrasing(messages: List[Dict[str, str]]) -> str:
    """
    Format conversation messages into a readable string for rephrasing context.
    
    Args:
        messages: List of message dicts with 'role' and 'content' keys
                 (OpenAI message format)
    
    Returns:
        Formatted conversation history string
    
    Example:
        >>> messages = [
        ...     {"role": "user", "content": "What is Spyre?"},
        ...     {"role": "assistant", "content": "Spyre is an AI accelerator..."}
        ... ]
        >>> format_messages_for_rephrasing(messages)
        'User: What is Spyre?\\nAssistant: Spyre is an AI accelerator...\\n'

    Will be updated when integrating with UI
    """
    if not messages:
        return ""
    
    formatted_lines = []
    for msg in messages:
        role = msg.get("role", "unknown")
        content = msg.get("content", "")
        
        # Capitalize role for readability
        role_display = role.capitalize()
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
    api_key: str | None = None
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
    
    Returns:
        Rephrased query string
    
    Raises:
        RuntimeError: If LLM session not initialized
        requests.exceptions.RequestException: On HTTP errors
    """
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")
    
    payload = {
        "model": llm_model,
        "messages": [
            {"role": "user", "content": prompt}
        ],
        "temperature": temperature,
        "max_tokens": max_tokens,
        "stop": ["\n\n", "Question:", "Current Question:"],
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


def is_conversational_mode_enabled() -> bool:
    """
    Check if conversational mode is enabled in settings.
    
    This is the master switch that controls whether conversational RAG features
    (including query rephrasing) are active.
    
    Returns:
        True if conversational mode is enabled, False otherwise
    """
    try:
        from chatbot.settings import settings
        return settings.chatbot.conversational_mode
    except Exception as e:
        logger.warning(f"Could not check conversational_mode setting: {e}")
        return False


async def rephrase_query_with_context(
    current_query: str,
    previous_messages: List[Dict[str, str]],
    llm_endpoint: str,
    llm_model: str,
    config: Optional[Dict] = None,
    api_key: str | None = None
) -> str:
    """
    Rephrase a conversational query to be self-contained using conversation context.
    
    This function transforms queries with pronouns and contextual references into
    standalone queries suitable for semantic search. It uses an LLM to understand
    the conversation context and reformulate the query.
    
    Note: This function respects the conversational_mode flag in RAGConfig. If
    conversational_mode is False, rephrasing is automatically disabled regardless
    of other configuration.
    
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
        ...     "model-name"
        ... )
        'Is Spyre supported on Power 11?'
    """
    # Check if conversational mode is enabled (master switch)
    if not is_conversational_mode_enabled():
        logger.debug("Query rephrasing skipped: conversational_mode is disabled")
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
        conversation_history = format_messages_for_rephrasing(previous_messages)
        
        if not conversation_history:
            logger.debug("Skipping query rephrasing: empty conversation history")
            return current_query
        
        # Get prompt template from settings (already imported above)
        prompt_template = settings.query_rephrasing.rephrase_prompt_template
        
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
            api_key=api_key
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
