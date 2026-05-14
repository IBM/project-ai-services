"""
Conversation utilities for conversational RAG message history handling.
"""
from typing import Any, Sequence, Callable

from common.misc_utils import get_logger

logger = get_logger("conversation_utils")


def _message_to_dict(message: Any) -> dict[str, str]:
    """Normalize request message objects to OpenAI-style dicts."""
    if isinstance(message, dict):
        content = message.get("content", "")
        role = message.get("role", "user")
        return {"role": role, "content": content}

    content = getattr(message, "content", "") or ""
    role = getattr(message, "role", "user") or "user"
    return {"role": role, "content": content}


def get_conversation_context(messages: Sequence[Any]) -> tuple[str, list[dict[str, str]]]:
    """
    Extract current query and conversation history from a messages array.

    Accepts either dict-based messages or Pydantic message objects.
    """
    if not messages:
        return "", []

    normalized_messages = [_message_to_dict(message) for message in messages]
    current_query = normalized_messages[-1].get("content", "")
    previous_messages = normalized_messages[:-1]

    return current_query, previous_messages


def truncate_history_by_tokens(
    messages: Sequence[dict[str, str]],
    token_budget: int,
    tokenize_fn: Callable[[str], list]
) -> list[dict[str, str]]:
    """
    Truncate history using a token-based sliding window.

    Keeps the most recent messages that fit within the token budget.
    If the newest single message alone exceeds the budget, it is still kept.
    
    Args:
        messages: List of message dicts with 'content' and 'role' keys
        token_budget: Maximum number of tokens allowed
        tokenize_fn: Function that takes a string and returns a list of tokens
    
    Returns:
        list: truncated_messages
    """
    if not messages:
        return []

    try:
        truncated: list[dict[str, str]] = []
        current_tokens = 0

        for message in reversed(messages):
            content = message.get("content", "")
            message_tokens = len(tokenize_fn(content))

            if not truncated and message_tokens > token_budget:
                logger.info(
                    "Single history message exceeds budget; keeping newest message "
                    f"({message_tokens} tokens > budget {token_budget})"
                )
                return [message]

            if current_tokens + message_tokens <= token_budget:
                truncated.insert(0, message)
                current_tokens += message_tokens
            else:
                break

        dropped_count = len(messages) - len(truncated)
        if dropped_count > 0:
            logger.info(
                f"History truncated: kept {len(truncated)} messages "
                f"({current_tokens} tokens), dropped {dropped_count} older messages"
            )
        else:
            logger.debug(
                f"History within budget: kept all {len(truncated)} messages "
                f"({current_tokens} tokens)"
            )

        return truncated
    except Exception as exc:
        logger.error(f"Failed to truncate history by tokens: {exc}", exc_info=True)
        logger.warning("Falling back to untruncated history")
        return list(messages)
