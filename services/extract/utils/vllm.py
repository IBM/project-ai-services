"""vLLM helper utilities for synchronous extraction."""

import asyncio
import json
import re
from typing import Any, Dict, List, Optional, Tuple

import requests
import common.misc_utils as misc_utils
from common.llm_utils import get_vllm_headers
from jsonschema import Draft202012Validator

from common.misc_utils import get_logger
from extract.settings import settings
from extract.utils.exceptions import ExtractException

logger = get_logger("vllm_utils")


def strip_markdown_fences(text: str) -> str:
    """Remove accidental ```json … ``` or ``` … ``` fences from model output."""
    text = text.strip()
    text = re.sub(r"^```(?:json)?\s*", "", text, flags=re.IGNORECASE)
    text = re.sub(r"\s*```$", "", text)
    return text.strip()


def render_few_shot_block(examples: Optional[List[Dict[str, Any]]]) -> str:
    """Render the few-shot block from stored example rows."""
    if not examples:
        return ""
    parts: List[str] = []
    for ex in examples:
        parts.append(
            f"Example text:\n{ex['text']}\n"
            f"Example JSON:\n{json.dumps(ex['output'], ensure_ascii=False)}"
        )
    return "\n\n".join(parts)


def build_messages(
    normalized_schema: Dict[str, Any],
    few_shot_block: str,
    input_text: str,
    custom_prompt: Optional[str],
) -> List[Dict[str, str]]:
    """Assemble the chat messages list for the extraction prompt."""
    system_content = settings.extract.extraction_system_prompt.format(
        custom_prompt=custom_prompt if custom_prompt else ""
    )
    user_content = settings.extract.extraction_user_prompt.format(
        normalized_json_schema=json.dumps(normalized_schema, ensure_ascii=False),
        few_shot_block=few_shot_block,
        input_text=input_text,
    )
    return [
        {"role": "system", "content": system_content},
        {"role": "user", "content": user_content},
    ]


def call_vllm(
    messages: List[Dict[str, str]],
    max_tokens: int,
    normalized_schema: Dict[str, Any],
    llm_endpoint: str,
    llm_model: str,
) -> Dict[str, Any]:
    """
    Execute a single blocking /v1/chat/completions call.

    Adds ``guided_json`` when ``GUIDED_DECODING_ENABLED`` is true.
    Returns the raw response JSON dict.
    Raises ``requests.exceptions.ConnectionError`` / ``HTTPError`` on failure.
    """
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized.")

    payload: Dict[str, Any] = {
        "model": llm_model,
        "messages": messages,
        "max_tokens": max_tokens,
        "temperature": settings.extract.extraction_temperature,
        "stream": False,
    }

    if settings.extract.guided_decoding_enabled:
        payload["extra_body"] = {"guided_json": normalized_schema}

    headers = get_vllm_headers(settings.common.llm.api_key)
    response = misc_utils.SESSION.post(
        f"{llm_endpoint}/v1/chat/completions",
        json=payload,
        headers=headers,
        stream=False,
    )
    response.raise_for_status()
    return response.json()


def validate_output(raw_text: str, normalized_schema: Dict[str, Any]) -> Dict[str, Any]:
    """
    Parse and validate the model output against *normalized_schema*.

    Returns the parsed dict on success.
    Raises ``ValueError`` whose message lists all schema errors (used as
    the retry-append payload).
    """
    cleaned = strip_markdown_fences(raw_text)
    try:
        parsed = json.loads(cleaned)
    except json.JSONDecodeError as exc:
        raise ValueError(f"Output is not valid JSON: {exc}") from exc

    validator = Draft202012Validator(normalized_schema)
    errors = list(validator.iter_errors(parsed))
    if errors:
        raise ValueError("; ".join(e.message for e in errors))

    return parsed


async def call_vllm_safe(
    messages: List[Dict[str, str]],
    reserved_output: int,
    json_schema: Dict[str, Any],
    llm_endpoint: str,
    llm_model: str,
    *,
    is_retry: bool = False,
) -> Dict[str, Any]:
    """Wrap :func:`call_vllm` in ``asyncio.to_thread`` with uniform error handling.

    Args:
        messages: Chat messages list for the completion request.
        reserved_output: Maximum tokens to reserve for the model output.
        json_schema: The normalised JSON schema (used for guided decoding).
        llm_endpoint: Base URL of the vLLM endpoint.
        llm_model: Model name to pass in the payload.
        is_retry: When ``True`` log messages include the ``"on retry"`` suffix.

    Returns:
        The raw vLLM response dict.

    Raises:
        ExtractException(503) on connection failure.
        ExtractException(500) on HTTP error or any other unexpected failure.
    """
    label = " on retry" if is_retry else ""
    try:
        return await asyncio.to_thread(
            call_vllm, messages, reserved_output, json_schema, llm_endpoint, llm_model
        )
    except requests.exceptions.ConnectionError as exc:
        logger.error(f"vLLM unreachable{label}: {exc}")
        raise ExtractException(503, "LLM_UNAVAILABLE", "The AI service is unreachable.")
    except requests.exceptions.HTTPError as exc:
        logger.error(f"vLLM HTTP error{label}: {exc}")
        raise ExtractException(
            500, "LLM_ERROR",
            f"The AI service returned an error{label}: {exc.response.status_code}",
        )
    except Exception as exc:
        logger.error(f"Unexpected error calling vLLM{label}: {exc}", exc_info=True)
        raise ExtractException(500, "LLM_ERROR", f"Unexpected error during LLM call{label}.")


async def validate_with_retry(
    raw_output: str,
    messages: List[Dict[str, str]],
    reserved_output: int,
    json_schema: Dict[str, Any],
    llm_endpoint: str,
    llm_model: str,
) -> Tuple[Dict[str, Any], int, int, int]:
    """Validate model output against *json_schema*; issue one correction retry on failure.

    On the first successful validation, the extra token counts returned are both zero
    (no retry was performed).  When a retry is required the extra token counts reflect
    the usage from that second call.

    Args:
        raw_output: Raw text content from the first vLLM call.
        messages: The original messages list used in the first call.
        reserved_output: Token budget forwarded to the retry call.
        json_schema: Normalised JSON schema used for validation and guided decoding.
        llm_endpoint: Base URL of the vLLM endpoint.
        llm_model: Model name passed in the retry payload.

    Returns:
        A 4-tuple of:
        - ``parsed_output``: The validated, schema-conformant dict.
        - ``validation_attempts``: ``1`` if no retry was needed, ``2`` otherwise.
        - ``extra_prompt_tokens``: Prompt tokens consumed by the retry call (``0`` if none).
        - ``extra_completion_tokens``: Completion tokens consumed by the retry call (``0`` if none).

    Raises:
        ExtractException(413) if the retry output is truncated (``finish_reason == "length"``).
        ExtractException(422) if the retry output also fails schema validation.
        ExtractException(503/500) propagated from :func:`call_vllm_safe` on LLM failure.
    """
    first_val_err: Optional[ValueError] = None
    try:
        parsed_output = validate_output(raw_output, json_schema)
        return parsed_output, 1, 0, 0
    except ValueError as exc:
        first_val_err = exc  # capture before Python unbinds the name on except-block exit

    retry_messages = messages + [
        {"role": "assistant", "content": raw_output},
        {
            "role": "user",
            "content": (
                "Your previous output failed validation with these errors:\n"
                f"{first_val_err}\n\n"
                f"Previous output:\n{raw_output}\n\n"
                "Return a corrected JSON object that fixes ALL listed errors. "
                "Output ONLY the JSON."
            ),
        },
    ]

    retry_resp = await call_vllm_safe(
        retry_messages, reserved_output, json_schema, llm_endpoint, llm_model, is_retry=True
    )

    retry_choices = retry_resp.get("choices", [])
    if not retry_choices:
        raise ExtractException(500, "LLM_ERROR", "vLLM returned an empty choices list on retry.")

    retry_choice = retry_choices[0]
    if retry_choice.get("finish_reason") == "length":
        raise ExtractException(
            413, "OUTPUT_BUDGET_EXCEEDED",
            "The model output was truncated on the validation retry.",
            details={"reserved_output_tokens": reserved_output, "finish_reason": "length"},
        )

    raw_retry_output: str = retry_choice.get("message", {}).get("content", "") or ""
    retry_usage = retry_resp.get("usage", {})
    extra_prompt_tokens: int = retry_usage.get("prompt_tokens", 0)
    extra_completion_tokens: int = retry_usage.get("completion_tokens", 0)

    try:
        parsed_output = validate_output(raw_retry_output, json_schema)
    except ValueError as retry_err:
        raise ExtractException(
            422, "EXTRACTION_VALIDATION_FAILED",
            "Model output failed schema validation after one retry.",
            details={"validation_errors": str(retry_err), "raw_output": raw_retry_output},
        )
    except Exception as e:
        logger.error(e)
        raise ExtractException(500,
            "INTERNAL_SERVER_ERROR",
            "Something went wrong. Please try again later."
        )

    return parsed_output, 2, extra_prompt_tokens, extra_completion_tokens
