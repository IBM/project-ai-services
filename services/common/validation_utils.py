from common.llm_utils import tokenize_with_llm
from common.misc_utils import get_logger

logger = get_logger("validation")


def validate_query_length(query: str, emb_endpoint: str, max_token_length: int) -> tuple[bool, str | None]:
    """
    Validate that query length does not exceed maximum allowed tokens.

    Args:
        query: The query string to validate
        emb_endpoint: Endpoint used for tokenization
        max_token_length: Maximum allowed token count (service-specific)

    Returns:
        (is_valid, error_message) tuple
    """
    try:
        tokens = tokenize_with_llm(query, emb_endpoint)
        token_count = len(tokens)

        if token_count > max_token_length:
            error_msg = f"Query length ({token_count} tokens) exceeds maximum allowed length of {max_token_length} tokens"
            logger.warning(error_msg)
            return False, error_msg

        return True, None
    except Exception as e:
        logger.error(f"Error validating query length: {e}")
        # If tokenization fails, allow the request to proceed
        # to avoid blocking legitimate requests due to tokenization issues
        return True, None
