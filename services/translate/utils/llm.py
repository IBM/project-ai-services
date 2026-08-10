"""
Async LLM client for translation calls.

Provides ``query_vllm_translate``, a thin ``httpx.AsyncClient`` wrapper that
posts to ``POST {llm_endpoint}/v1/chat/completions`` and returns
``(translated_text, input_tokens, output_tokens)`` (§9.3).

The function is fully async — it does not block the event loop.
"""

import httpx

from common.misc_utils import get_logger

logger = get_logger("llm")


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

    logger.info(f"vLLM choices: {choices}")
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
