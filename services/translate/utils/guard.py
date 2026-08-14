"""
Hard context-window guard for translation.

Two entry points:

``check_sync_guard(input_tokens, settings)``
    Validates the full input of a synchronous translation call against
    ``MAX_MODEL_LEN``.  Returns ``(max_tokens, None)`` on pass or
    ``(0, diagnostics_dict)`` on breach.

``check_chunk_guard(chunk_tokens, settings)``
    Validates a single async chunk before the LLM call.  Returns
    ``(max_tokens, None)`` on pass or ``(0, diagnostics_dict)`` on breach.

Both functions return the calculated ``max_tokens`` value for the LLM call
(with 10% breathing-room buffer applied) — the caller must never re-compute
this independently so that the budget arithmetic stays in one place.
"""

from typing import Optional

from common.misc_utils import get_logger
from translate.settings import Settings

logger = get_logger("guard")


def _compute_max_tokens(available_output: int) -> int:
    """
    Apply a 10% breathing-room buffer to *available_output*.

    The buffer prevents the LLM from being cut off mid-sentence when the
    output happens to track input length closely (as in translation).

    Buffer floor: 20 tokens — ensures a non-zero minimum reserve even on
    very small chunks.
    """
    buffer = max(20, int(available_output * 0.10))
    return available_output - buffer


def _build_diagnostics(
    max_model_len: int,
    input_tokens: int,
    prompt_overhead_tokens: int,
    min_output_tokens: int,
) -> dict:
    """Return the standard token-diagnostics dict (§8.3)."""
    total_required = input_tokens + prompt_overhead_tokens + min_output_tokens
    return {
        "max_model_len": max_model_len,
        "input_tokens": input_tokens,
        "prompt_overhead_tokens": prompt_overhead_tokens,
        "min_output_buffer_tokens": min_output_tokens,
        "total_required_tokens": total_required,
        "excess_tokens": max(0, total_required - max_model_len),
    }


def check_sync_guard(
    input_tokens: int,
    settings: Settings,
) -> tuple[int, Optional[dict]]:
    """
    Sync-path guard. Returns ``(max_tokens, None)`` if the input fits in the
    context window, or ``(0, diagnostics_dict)`` if it exceeds the limit (→ 413).
    """
    max_model_len = settings.common.llm.max_model_len
    prompt_overhead = settings.translate.prompt_overhead_tokens
    min_output = settings.translate.min_output_tokens

    max_allowed_input = max_model_len - prompt_overhead - min_output

    if input_tokens > max_allowed_input:
        diag = _build_diagnostics(max_model_len, input_tokens, prompt_overhead, min_output)
        logger.warning(
            f"Sync guard breach: input_tokens={input_tokens} > "
            f"max_allowed={max_allowed_input} "
            f"(max_model_len={max_model_len}, overhead={prompt_overhead}, "
            f"min_output={min_output})"
        )
        return 0, diag

    available_output = max_model_len - input_tokens - prompt_overhead
    max_tokens = _compute_max_tokens(available_output)

    logger.debug(
        f"Sync guard OK: input_tokens={input_tokens}, "
        f"available_output={available_output}, max_tokens={max_tokens}"
    )
    return max_tokens, None


# Made with Bob
