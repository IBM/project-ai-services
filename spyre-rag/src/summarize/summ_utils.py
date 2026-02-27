import logging
import os
import re
from typing import Optional
import pypdfium2 as pdfium
from pydantic import BaseModel, Field

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

def compute_target_and_max_tokens(input_word_count: int, summary_length: Optional[int]):
    if summary_length is not None:
        target_word_count = summary_length
    else:
        target_word_count = max(1, int(input_word_count * settings.summarization_coefficient))

    est_output_tokens = int(target_word_count / settings.token_to_word_ratios.en)
    max_tokens = est_output_tokens + settings.summarization_prompt_token_count
    logger.debug(f"max tokens: {max_tokens}, estimated output tokens: {est_output_tokens}")
    return target_word_count, max_tokens

def extract_text_from_pdf(content: bytes) -> str:
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


def build_messages(text, target_words, summary_length) -> list:
    if summary_length:
        user_prompt = settings.prompts.summarize_user_prompt_with_length.format(target_words=target_words, text=text)
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


class ErrorDetail(BaseModel):
    code: str = Field(..., description="Machine-readable error code.")
    message: str = Field(..., description="Human-readable error message.")
    status: int = Field(..., description="HTTP status code.")


class SummarizeErrorResponseBadRequest(BaseModel):
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "MISSING_INPUT",
                    "message": "Either 'text' or 'file' parameter is required",
                    "status": 400,
                }
            }
        }
    }


class SummarizeErrorResponseContextLimitExceeded(BaseModel):
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "CONTEXT_LIMIT_EXCEEDED",
                    "message": "File size exceeds maximum token limit",
                    "status": 413,
                }
            }
        }
    }


class SummarizeErrorResponseUnsupportedContentType(BaseModel):
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "UNSUPPORTED_CONTENT_TYPE",
                    "message":  "Content-Type must be application/json or multipart/form-data",
                    "status": 415,
                }
            }
        }
    }


class SummarizeErrorResponseInternalServiceError(BaseModel):
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "LLM_ERROR",
                    "message":  "Failed to generate summary. Please try again later",
                    "status": 500,
                }
            }
        }
    }

error_responses = {
    400: {"description": "Bad request (missing input, unsupported file type, invalid params)", "model": SummarizeErrorResponseBadRequest},
    413: {"description": "Input exceeds context window limit", "model": SummarizeErrorResponseContextLimitExceeded},
    415: {"description": "Unsupported Content-Type", "model": SummarizeErrorResponseUnsupportedContentType},
    500: {"description": "LLM service error", "model": SummarizeErrorResponseInternalServiceError},
}

def validate_summary_length(summary_length):
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
