import re
from typing import Optional
import pypdfium2 as pdfium
from pydantic import BaseModel, Field
import threading

from summarize.settings import settings
from common.misc_utils import set_log_level, get_logger

set_log_level(settings.common.app.log_level)
logger = get_logger("summarize")

_pdf_lock = threading.Lock()

# Pre-compute constants at module load time to avoid recalculating on every request
# Calculate minimum output tokens needed for a valid summary
MINIMUM_OUTPUT_TOKENS = int(
    settings.summarize.minimum_summary_words / settings.common.llm.token_to_word_ratio_en
)

# Hard limit: maximum allowed input tokens (input + prompt + minimum_output must fit in context)
MAX_ALLOWED_INPUT_TOKENS = (
    settings.common.llm.granite_3_3_8b_instruct_context_length -
    settings.summarize.summarization_prompt_token_count -
    MINIMUM_OUTPUT_TOKENS
)

# Pre-compute max input word count from context length at startup
# input_words/ratio + buf + (input_words/ratio)*coeff < max_model_len
# => input_words * (1 + coeff) / ratio < max_model_len - buf
MAX_INPUT_WORDS = int(
    (
        settings.common.llm.granite_3_3_8b_instruct_context_length
        - settings.summarize.summarization_prompt_token_count
    )
    * settings.common.llm.token_to_word_ratio_en
    / (1 + settings.summarize.summarization_coefficient)
)

def word_count(text: str) -> int:
    return len(text.split())


def validate_input_and_get_available_tokens(
    input_tokens: int,
    input_word_count: int,
    summary_level: Optional[str] = None,
    summary_length: Optional[int] = None
) -> int:
    """
    Unified validation function for both summary_level and summary_length approaches.
    Validates input using actual token count with hard and soft limits, returns available output tokens.
    
    Hard limit: input + prompt must not exceed (context_limit - minimum_summary_words)
    Soft limit: For level-based, log warning if level's ideal output won't fit (but don't fail)
    
    Args:
        input_tokens: Actual token count of input text
        input_word_count: Number of words in input text (for logging)
        summary_level: Optional abstraction level ("brief", "standard", or "detailed")
        summary_length: Optional direct word count specification
    
    Returns:
        available_output_tokens: Maximum tokens available for summary generation
    
    Raises:
        SummarizeException: If input exceeds hard limit or summary_length > input_word_count
    """
    
    # Validate summary_length if provided
    if summary_length is not None and summary_length > input_word_count:
        raise SummarizeException(
            400, "INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH",
            "Input text is smaller than summary length",
        )
    
    # Hard limit check
    if input_tokens > MAX_ALLOWED_INPUT_TOKENS:
        # Convert to words for user-friendly error message
        max_allowed_input_words = int(MAX_ALLOWED_INPUT_TOKENS * settings.common.llm.token_to_word_ratio_en)
        raise SummarizeException(
            413, "CONTEXT_LIMIT_EXCEEDED",
            f"Input size ({input_word_count} words, {input_tokens} tokens) exceeds maximum allowed. "
            f"Maximum input: ~{max_allowed_input_words} words ({MAX_ALLOWED_INPUT_TOKENS} tokens) "
            f"to ensure at least {settings.summarize.minimum_summary_words} words for summary.",
        )
    
    # Calculate available output tokens
    available_output_tokens = (
        settings.common.llm.granite_3_3_8b_instruct_context_length -
        input_tokens -
        settings.summarize.summarization_prompt_token_count
    )
    
    # Soft limit check for level-based approach: log warning if level's ideal output won't fit
    if summary_level is not None:
        level_config = getattr(settings.summarize.summarization_levels, summary_level)
        
        # Calculate ideal output tokens for this level based on actual input
        # ideal_output = input * coefficient * level_multiplier
        base_target_tokens = int(input_tokens * settings.summarize.summarization_coefficient)
        ideal_output_tokens = int(base_target_tokens * level_config.multiplier)
        
        if available_output_tokens < ideal_output_tokens:
            available_output_words = int(available_output_tokens * settings.common.llm.token_to_word_ratio_en)
            ideal_output_words = int(ideal_output_tokens * settings.common.llm.token_to_word_ratio_en)
            logger.warning(
                f"Input size ({input_word_count} words, {input_tokens} tokens) limits output space. "
                f"'{summary_level}' level target is ~{ideal_output_words} words, "
                f"but only ~{available_output_words} words available for summary."
            )
    
    return available_output_tokens


def compute_target_and_max_tokens(
    input_tokens: int,
    available_output_tokens: int,
    summary_level: Optional[str] = None,
    summary_length: Optional[int] = None
) -> tuple[Optional[int], Optional[int], Optional[int], int]:
    """
    Unified function to compute target words and tokens for both level-based and length-based approaches.
    
    Args:
        input_tokens: Actual token count of input text
        available_output_tokens: Maximum tokens available for output (from validation)
        summary_level: Optional abstraction level ("brief", "standard", or "detailed")
        summary_length: Optional direct word count specification
    
    Returns:
        (target_words, min_words, max_words, max_tokens)
        Note: In automatic mode, target_words, min_words, and max_words are None
    """
    
    if summary_level is not None:
        # Level-based approach: calculate target based on input and level multiplier
        level_config = getattr(settings.summarize.summarization_levels, summary_level)
        
        # Calculate ideal target based on input tokens and level multiplier
        base_target_tokens = int(input_tokens * settings.summarize.summarization_coefficient)
        ideal_target_tokens = int(base_target_tokens * level_config.multiplier)
        
        # Cap target to available space
        target_tokens = min(ideal_target_tokens, available_output_tokens)
        
        # Convert to words for display
        target_word_count = int(target_tokens * settings.common.llm.token_to_word_ratio_en)
        
        # Calculate min/max bounds (85% to 115% of target)
        min_words = int(target_word_count * 0.85)
        max_words = int(target_word_count * 1.15)
        
        # Cap max_words to available space
        max_possible_words = int(available_output_tokens * settings.common.llm.token_to_word_ratio_en)
        max_words = min(max_words, max_possible_words)
        
        """
        Add buffer to max_tokens to allow the model flexibility to complete thoughts.
        The 10% buffer accounts for token estimation variance and ensures the model
        can reach the target length without being cut off mid-sentence.
        """
        buffer = max(20, int(target_tokens * 0.1))
        max_tokens = min(target_tokens + buffer, available_output_tokens)
        
        logger.debug(
            f"Level: {summary_level}, Input: {input_tokens} tokens, Target: {target_word_count} words "
            f"({min_words}-{max_words}), Max tokens: {max_tokens}, Available: {available_output_tokens}"
        )
        
    elif summary_length is not None:
        # Length-based approach: use specified word count
        target_word_count = summary_length
        
        # Calculate min/max bounds
        min_words = int(target_word_count * 0.85)
        max_words = int(target_word_count * 1.15)
        
        # Cap target_word_count and max_words to available space
        max_possible_words = int(available_output_tokens * settings.common.llm.token_to_word_ratio_en)
        target_word_count = min(target_word_count, max_possible_words)
        max_words = min(max_words, max_possible_words)
        
        # Estimate output tokens from target word count
        est_output_tokens = int(target_word_count / settings.common.llm.token_to_word_ratio_en)
        
        """
        Add buffer to max_tokens to allow the model flexibility to complete thoughts.
        The 10% buffer accounts for token estimation variance and ensures the model
        can reach the target length without being cut off mid-sentence.
        """
        buffer = max(20, int(est_output_tokens * 0.1))
        max_tokens = min(est_output_tokens + buffer, available_output_tokens)
        
        logger.debug(
            f"Length: {summary_length} words, Input: {input_tokens} tokens, Target: {target_word_count} words "
            f"({min_words}-{max_words}), Max tokens: {max_tokens}, Available: {available_output_tokens}"
        )
        
    else:
        # Automatic approach: use default coefficient
        # Word counts are not used in automatic mode (not included in prompt)
        target_word_count = None
        min_words = None
        max_words = None
        
        target_tokens = max(1, int(input_tokens * settings.summarize.summarization_coefficient))
        
        """
        Add buffer to max_tokens to allow the model flexibility to complete thoughts.
        The 10% buffer accounts for token estimation variance and ensures the model
        can reach the target length without being cut off mid-sentence.
        """
        buffer = max(20, int(target_tokens * 0.1))
        max_tokens = min(target_tokens + buffer, available_output_tokens)
        
        logger.debug(
            f"Automatic mode, Input: {input_tokens} tokens, "
            f"Max tokens: {max_tokens}, Available: {available_output_tokens}"
        )
    
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


def build_messages(text: str, target_words: Optional[int], min_words: Optional[int], max_words: Optional[int], has_length_spec: bool) -> list:
    """
    Build messages for summarization with explicit length constraints.
    
    Args:
        text: Text to summarize
        target_words: Target word count (None in automatic mode)
        min_words: Minimum acceptable word count (None in automatic mode)
        max_words: Maximum acceptable word count (None in automatic mode)
        has_length_spec: Whether user specified a length (vs automatic)
    """
    if has_length_spec:
        user_prompt = settings.summarize.summarize_user_prompt_with_length.format(
            target_words=target_words,
            min_words=min_words,
            max_words=max_words,
            text=text
        )
    else:
        user_prompt = settings.summarize.summarize_user_prompt_without_length.format(text=text)
    
    return [
        {
            "role": "system",
            "content": settings.summarize.summarize_system_prompt,
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
            f"level must be one of: {', '.join(valid_levels)}"
        )
    return summary_level
