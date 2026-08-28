"""
Async LLM client and runtime model-length resolution for translation calls.

Provides:
- ``get_llm_max_model_len()``  — resolves the model's real context window at runtime
                                  (cached after the first call).
- ``get_chunk_token_budget()`` — derives chunk budget as 40% of the resolved max len,
                                  falling back to ``settings.translate.chunk_token_budget``
                                  if resolution fails.
- ``query_vllm_translate()``   — async vLLM chat/completions wrapper.
"""

import httpx

from common.misc_utils import get_logger, resolve_model_max_len
from translate.settings import settings

logger = get_logger("llm")


def get_llm_max_model_len() -> int:
    """
    Resolve the configured model's real context window length.

    Tries ``/model/info`` (LiteLLM) then ``/v1/models`` (vLLM) in order,
    falling back to ``settings.common.llm.max_model_len`` if neither responds.
    Result is cached in-process — no repeated network calls.
    """
    return resolve_model_max_len(
        settings.common.llm.endpoint,
        settings.common.llm.model,
        settings.common.llm.max_model_len,
        settings.common.llm.api_key or None,
    )


def get_chunk_token_budget() -> int:
    """
    Return the effective chunk token budget for the current model.

    Resolves the real ``max_model_len`` at runtime and returns
    ``CHUNK_BUDGET_RATIO × max_model_len``. Falls back to
    ``settings.translate.chunk_token_budget`` only if resolution fails
    and the fallback is returned by ``get_llm_max_model_len``.
    """
    return int(get_llm_max_model_len() * settings.translate.chunk_budget_ratio)


async def query_vllm_translate(
    client: httpx.AsyncClient,
    llm_endpoint: str,
    messages: list[dict],
    model: str,
    max_tokens: int,
    temperature: float = 0.0,
) -> tuple[str, int, int]:
    """
    Send one translation call to the vLLM endpoint.

    Args:
        client:       Shared ``httpx.AsyncClient`` (caller manages lifecycle).
        llm_endpoint: Base URL of the vLLM OpenAI-compatible endpoint
                      (e.g. ``http://vllm:8000``).  The path
                      ``/v1/chat/completions`` is appended here.
        messages:     Two-element list: ``[{role: system, …}, {role: user, …}]``.
        model:        Model name string (e.g. ``"ibm-granite/granite-3.3-8b-instruct"``).
        max_tokens:   Hard cap on output tokens; computed per-call by the context guard.
        temperature:  Sampling temperature.  Defaults to ``0.0`` for deterministic
                      translation — do not change without a deliberate reason.

    Returns:
        ``(translated_text, input_tokens, output_tokens)``

    Raises:
        httpx.HTTPStatusError: Non-2xx response from vLLM.
        httpx.RequestError:    Network-level failure (timeout, connection refused …).
    """
    payload: dict = {
        "messages": messages,
        "model": model,
        "max_tokens": max_tokens,
        "temperature": temperature,
    }

    logger.debug(
        f"Calling vLLM translate: model={model}, max_tokens={max_tokens}, "
        f"temperature={temperature}, messages_count={len(messages)}"
    )

    response = await client.post(
        f"{llm_endpoint}/v1/chat/completions",
        json=payload,
        headers={"Content-Type": "application/json"},
    )
    response.raise_for_status()
    data = response.json()

    choices = data.get("choices", [])
    if not choices:
        raise ValueError("vLLM returned an empty 'choices' array")

    content: str = (choices[0].get("message", {}).get("content") or "").strip()
    usage = data.get("usage", {})
    input_tokens: int = usage.get("prompt_tokens", 0)
    output_tokens: int = usage.get("completion_tokens", 0)

    logger.debug(
        f"vLLM translate response: input_tokens={input_tokens}, "
        f"output_tokens={output_tokens}, content_len={len(content)}"
    )

    return content, input_tokens, output_tokens

# Made with Bob
