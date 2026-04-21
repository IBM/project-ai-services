import logging
import os
import re
from typing import Optional
import pypdfium2 as pdfium
from pydantic import BaseModel, Field
import threading

from common.settings import get_settings
from common.misc_utils import set_log_level, get_logger


log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif not "info" in level:
        logging.warning("Unknown LOG_LEVEL passed: '%s'", level)
set_log_level(log_level)
logger = get_logger("summarize")

settings = get_settings()
_pdf_lock = threading.Lock()

# Pre-compute max input word count from context length at startup
# input_words/ratio + buf + (input_words/ratio)*coeff < max_model_len
# => input_words * (1 + coeff) / ratio < max_model_len - buf
MAX_INPUT_WORDS = int(
    (settings.context_lengths.granite_3_3_8b_instruct - settings.summarization_prompt_token_count)
    * settings.token_to_word_ratios.en
    / (1 + settings.summarization_coefficient)
)

def word_count(text: str) -> int:
    return len(text.split())

def validate_input_word_count_for_level(input_word_count: int, summary_level: str) -> int:
    """
    Validate input word count with hard and soft limits, return available output tokens.
    
    Hard limit: input + prompt must not exceed (context_limit - minimum_summary_words)
    Soft limit: If within hard limit, allow processing even if level's ideal output won't fit
    
    Args:
        input_word_count: Number of words in input text
        summary_level: "brief", "standard", or "detailed"
    
    Returns:
        available_output_tokens: Maximum tokens available for summary generation
    
    Raises:
        SummarizeException: If input exceeds hard limit
    """
    # Convert input words to tokens
    input_tokens = int(input_word_count / settings.token_to_word_ratios.en)
    
    # Calculate minimum output tokens needed
    minimum_output_tokens = int(settings.minimum_summary_words / settings.token_to_word_ratios.en)
    
    # Hard limit: input + prompt + minimum_output must fit in context
    max_allowed_input_tokens = (
        settings.context_lengths.granite_3_3_8b_instruct -
        settings.summarization_prompt_token_count -
        minimum_output_tokens
    )
    
    if input_tokens > max_allowed_input_tokens:
        # Convert to words for user-friendly error message
        max_allowed_input_words = int(max_allowed_input_tokens * settings.token_to_word_ratios.en)
        raise SummarizeException(
            413, "CONTEXT_LIMIT_EXCEEDED",
            f"Input size ({input_word_count} words, ~{input_tokens} tokens) exceeds maximum allowed. "
            f"Maximum input: ~{max_allowed_input_words} words ({max_allowed_input_tokens} tokens) "
            f"to ensure at least {settings.minimum_summary_words} words for summary.",
        )
    
    # Calculate available output tokens
    available_output_tokens = (
        settings.context_lengths.granite_3_3_8b_instruct -
        input_tokens -
        settings.summarization_prompt_token_count
    )
    
    # Soft limit check: log warning if level's ideal output won't fit, but don't fail
    level_config = getattr(settings.summarization_levels, summary_level)
    
    # Calculate ideal output tokens for this level
    # From: input_tokens + output_tokens = context - prompt
    # And: output_tokens = input_tokens × coeff × multiplier
    # Solving: ideal_output = (context - prompt) × (coeff × multiplier) / (1 + coeff × multiplier)
    ideal_output_tokens = int(
        (settings.context_lengths.granite_3_3_8b_instruct - settings.summarization_prompt_token_count) *
        (settings.summarization_coefficient * level_config.multiplier) /
        (1 + settings.summarization_coefficient * level_config.multiplier)
    )
    
    if available_output_tokens < ideal_output_tokens:
        available_output_words = int(available_output_tokens * settings.token_to_word_ratios.en)
        ideal_output_words = int(ideal_output_tokens * settings.token_to_word_ratios.en)
        logger.warning(
            f"Input size ({input_word_count} words) limits output space. "
            f"'{summary_level}' level target is ~{ideal_output_words} words, "
            f"but only ~{available_output_words} words available for summary."
        )
    
    return available_output_tokens

def compute_target_and_max_tokens_from_level(
    input_word_count: int,
    summary_level: str,
    available_output_tokens: int
) -> tuple[int, int, int, int]:
    """
    Compute target words and tokens based on abstraction level and available space.
    
    Args:
        input_word_count: Number of words in input text
        summary_level: "brief", "standard", or "detailed"
        available_output_tokens: Maximum tokens available for output (from validation)
    
    Returns:
        (target_words, min_words, max_words, max_tokens)
    """
    # Get level configuration
    level_config = getattr(settings.summarization_levels, summary_level)
    
    # Calculate ideal target based on input size and level multiplier
    base_target = int(input_word_count * settings.summarization_coefficient)
    ideal_target_words = int(base_target * level_config.multiplier)
    
    # Cap target to available space (convert tokens to words)
    max_possible_words = int(available_output_tokens * settings.token_to_word_ratios.en)
    target_word_count = min(ideal_target_words, max_possible_words)
    
    # Calculate min/max bounds (85% to 115% of target)
    min_words = int(target_word_count * 0.85)
    max_words = int(target_word_count * 1.15)
    
    # Cap max_words to available space
    max_words = min(max_words, max_possible_words)
    
    # Convert to tokens with small buffer
    est_output_tokens = int(target_word_count / settings.token_to_word_ratios.en)
    buffer = max(20, int(est_output_tokens * 0.1))  # 10% buffer
    max_tokens = min(est_output_tokens + buffer, available_output_tokens)
    
    logger.debug(
        f"Level: {summary_level}, Target: {target_word_count} words "
        f"({min_words}-{max_words}), Max tokens: {max_tokens}, Available: {available_output_tokens}"
    )
    
    return target_word_count, min_words, max_words, max_tokens

def compute_target_and_max_tokens(input_word_count: int, summary_length: Optional[int]):
    """Legacy function for backward compatibility with direct length specification."""
    if summary_length is not None:
        target_word_count = summary_length
    else:
        target_word_count = max(1, int(input_word_count * settings.summarization_coefficient))

    # Calculate min/max bounds
    min_words = int(target_word_count * 0.85)
    max_words = int(target_word_count * 1.15)
    
    est_output_tokens = int(target_word_count / settings.token_to_word_ratios.en)
    buffer = max(20, int(est_output_tokens * 0.1))
    max_tokens = est_output_tokens + buffer
    
    logger.debug(f"Target: {target_word_count} words, Max tokens: {max_tokens}")
    return target_word_count, min_words, max_words, max_tokens

def extract_text_from_pdf(content: bytes) -> str:
    with _pdf_lock:
        pdf = pdfium.PdfDocument(content)
        text_parts = []
        for page_index in range(len(pdf)):
            page = pdf[page_index]
            textpage = page.get_textpage()
            text_parts.append(textpage.get_text_range())
            textpage.close()
            page.close()
        pdf.close()
        return "\n".join(text_parts)

def trim_to_last_sentence(text: str) -> str:
    """Remove any trailing incomplete sentence."""
    match = re.match(r"(.*[.!?])", text, re.DOTALL)
    return match.group(1).strip() if match else text.strip()

def build_success_response(
    summary: str,
    original_length: int,
    input_type: str,
    model: str,
    processing_time_ms: int,
    input_tokens: int,
    output_tokens: int,
):
    return {
        "data": {
            "summary": summary,
            "original_length": original_length,
            "summary_length": word_count(summary),
        },
        "meta": {
            "model": model,
            "processing_time_ms": processing_time_ms,
            "input_type": input_type,
        },
        "usage": {
            "input_tokens": input_tokens,
            "output_tokens": output_tokens,
            "total_tokens": input_tokens + output_tokens,
        },
    }

class SummarizeException(Exception):
    def __init__(self, code: int, status: str, message: str):
        self.code = code
        self.message = message
        self.status = status


def build_messages(text: str, target_words: int, min_words: int, max_words: int, has_length_spec: bool) -> list:
    """
    Build messages for summarization with explicit length constraints.
    
    Args:
        text: Text to summarize
        target_words: Target word count
        min_words: Minimum acceptable word count
        max_words: Maximum acceptable word count
        has_length_spec: Whether user specified a length (vs automatic)
    """
    if has_length_spec:
        user_prompt = settings.prompts.summarize_user_prompt_with_length.format(
            target_words=target_words,
            min_words=min_words,
            max_words=max_words,
            text=text
        )
    else:
        user_prompt = settings.prompts.summarize_user_prompt_without_length.format(text=text)
    
    return [
        {
            "role": "system",
            "content": settings.prompts.summarize_system_prompt,
        },
        {
            "role": "user",
            "content": user_prompt,
        },
    ]


class SummaryData(BaseModel):
    summary: str = Field(..., description="The generated summary text.")
    original_length: int = Field(..., description="Word count of original text.")
    summary_length: int = Field(..., description="Word count of the generated summary.")


class SummaryMeta(BaseModel):
    model: str = Field(..., description="The AI model used for summarization.")
    processing_time_ms: int = Field(..., description="Request processing time in milliseconds.")
    input_type: str = Field(..., description="The type of input provided. Valid values: text, file.")


class SummaryUsage(BaseModel):
    input_tokens: int = Field(..., description="Number of input tokens consumed.")
    output_tokens: int = Field(..., description="Number of output tokens generated.")
    total_tokens: int = Field(..., description="Total number of tokens used (input + output).")


class SummarizeSuccessResponse(BaseModel):
    data: SummaryData
    meta: SummaryMeta
    usage: SummaryUsage

    model_config = {
        "json_schema_extra": {
            "example": {
                "data": {
                    "summary": "AI has advanced significantly through deep learning and large language models, impacting healthcare, finance, and transportation with both opportunities and ethical challenges.",
                    "original_length": 250,
                    "summary_length": 22,
                },
                "meta": {
                    "model": "ibm-granite/granite-3.3-8b-instruct",
                    "processing_time_ms": 1245,
                    "input_type": "text",
                },
                "usage": {
                    "input_tokens": 385,
                    "output_tokens": 62,
                    "total_tokens": 447,
                },
            }
        }
    }

def validate_summary_length(summary_length) -> Optional[int]:
    if summary_length:
        try:
            summary_length = int(summary_length)
        except (TypeError, ValueError):
            raise SummarizeException(400, "INVALID_PARAMETER",
                                     "Length must be an integer")
        if summary_length <=0 or summary_length > MAX_INPUT_WORDS:
            raise SummarizeException(400, "INVALID_PARAMETER",
                                     "Length is out of bounds")
        return summary_length
    return None

def validate_summary_level(summary_level: Optional[str]) -> Optional[str]:
    """
    Validate and return summary level.
    
    Args:
        summary_level: User-provided level or None
    
    Returns:
        Valid summary level or None if not provided
    """
    if summary_level is None:
        return None
    
    valid_levels = ["brief", "standard", "detailed"]
    if summary_level not in valid_levels:
        raise SummarizeException(
            400, "INVALID_PARAMETER",
            f"summary_level must be one of: {', '.join(valid_levels)}"
        )
    return summary_level
