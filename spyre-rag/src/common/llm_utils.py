import logging
import os
import requests
import time
import json
from concurrent.futures import ThreadPoolExecutor, as_completed
from tqdm import tqdm

from common.lang_utils import get_prompt_for_language
from common.misc_utils import get_logger
from common.settings import settings
from common.retry_utils import retry_on_transient_error
from chatbot.settings import settings as chatbot_settings
from chatbot.conversation_utils import truncate_history_by_tokens
from summarize.settings import settings as summarize_settings
from digitize.settings import settings as digitize_settings
import common.misc_utils as misc_utils

logger = get_logger("LLM")

is_debug = logger.isEnabledFor(logging.DEBUG)

def tqdm_wrapper(iterable, **kwargs):
    """Wrapper for tqdm that only shows progress bar in debug mode."""
    if is_debug:
        return tqdm(iterable, **kwargs)
    else:
        return iterable

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def summarize_and_classify_single_table(prompt, gen_model, llm_endpoint):
    """
    Combined function to summarize and classify a table in a single LLM call.
    Returns tuple: (summary, decision)
    """
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    payload = {
        "model": gen_model,
        "messages": [{"role": "user", "content": prompt}],
        "temperature": 0,
        "max_tokens": summarize_settings.summarize.table_summary_max_tokens,
        "stream": False,
    }

    try:
        response = misc_utils.SESSION.post(f"{llm_endpoint}/v1/chat/completions", json=payload, headers=get_vllm_headers(settings.model_endpoints.vllm_api_key))
        response.raise_for_status()
        data = response.json() or {}
        choices = data.get("choices", [])
        text = ""
        if choices:
            text = (choices[0].get("message", {}).get("content") or "").strip()

        # Parse response - handle multi-line summaries
        summary = ""
        decision = False
        lines = text.splitlines()

        # Find Summary: and Decision: lines
        summary_start = -1
        decision_line = -1

        for i, line in enumerate(lines):
            line_lower = line.strip().lower()
            if line_lower.startswith("summary:"):
                summary_start = i
            elif line_lower.startswith("decision:"):
                decision_line = i
                decision = "yes" in line_lower

        # Extract summary (everything between "Summary:" and "Decision:")
        if summary_start >= 0:
            if decision_line > summary_start:
                # Summary is between Summary: and Decision:
                summary_lines = lines[summary_start:decision_line]
                # Remove "Summary:" prefix from first line
                summary_lines[0] = summary_lines[0][summary_lines[0].lower().find("summary:") + len("summary:"):].strip()
                summary = "\n".join(line.strip() for line in summary_lines if line.strip())
            else:
                # No decision found, take everything after Summary:
                summary_lines = lines[summary_start:]
                summary_lines[0] = summary_lines[0][summary_lines[0].lower().find("summary:") + len("summary:"):].strip()
                summary = "\n".join(line.strip() for line in summary_lines if line.strip())

        return summary or "No summary.", decision

    except Exception as e:
        logger.error(f"Error summarizing/classifying table: {e}")
        return "No summary.", False

def summarize_and_classify_tables(table_mds, gen_model, llm_endpoint, pdf_path, max_workers=32):
    """
    Combined function to summarize and classify tables using a single prompt.
    Returns tuple: (summaries, decisions)
    """
    all_prompts = [digitize_settings.digitize.table_summary_and_classify.format(content=md) for md in table_mds]

    results: list[tuple[str, bool] | None] = [None] * len(all_prompts)

    with ThreadPoolExecutor(max_workers=min(max_workers, len(all_prompts))) as executor:
        futures = {
            executor.submit(summarize_and_classify_single_table, prompt, gen_model, llm_endpoint): idx
            for idx, prompt in enumerate(all_prompts)
        }
        for future in tqdm_wrapper(as_completed(futures), total=len(all_prompts),
                                   desc=f"Summarizing and classifying tables of '{pdf_path}'"):
            idx = futures[future]
            results[idx] = future.result()

    # Separate summaries and decisions with proper None handling
    summaries: list[str] = []
    decisions: list[bool] = []

    for result in results:
        if result is not None:
            summary, decision = result
            summaries.append(summary)
            decisions.append(decision)
        else:
            # Default values for failed futures
            summaries.append("No summary.")
            decisions.append(False)

    return summaries, decisions

def get_vllm_headers(api_key: str | None = None):
    """Get headers for vLLM API calls, including auth if provided.
    
    Args:
        api_key: Optional API key to include in Authorization header.
                 If not provided, no auth header is added.
    """
    headers = {
        "accept": "application/json",
        "Content-type": "application/json",
    }

    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
        logger.debug("Using vLLM API key for authentication")

    return headers


@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def query_vllm_models(llm_endpoint, api_key: str | None = None):
    """Used both for listing models and as an auth/availability preflight check.
    
    Args:
        api_key: Optional API key for vLLM authentication
    """
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    logger.debug('Querying VLLM models')
    response = misc_utils.SESSION.get(
        f"{llm_endpoint}/v1/models",
        headers=get_vllm_headers(api_key),
    )
    response.raise_for_status()
    resp_json = response.json()
    return resp_json

def query_vllm_payload(
    question,
    documents,
    llm_endpoint,
    llm_model,
    stop_words,
    max_new_tokens,
    temperature,
    stream,
    lang,
    api_key: str | None = None,
    previous_messages: list | None = None,
    rephrased_query: str | None = None,
):
    context = "\n\n".join([doc.get("page_content") for doc in documents])

    logger.debug(f"Original Context: {context}")

    # Use conversational mode if enabled AND language is English, otherwise use legacy prompts
    if chatbot_settings.chatbot.conversational_mode and lang == "EN":
        # Conversational RAG mode with message history
        question_token_count = len(tokenize_with_llm(question, llm_endpoint))
        context_tokens = tokenize_with_llm(context, llm_endpoint)
        context_token_count = len(context_tokens)

        # Calculate budget for context first (prioritize context over history)
        budget_for_context = settings.llm.granite_3_3_8b_instruct_context_length - (
            chatbot_settings.chatbot.initial_system_token_overhead +
            chatbot_settings.chatbot.rag_system_token_overhead +
            question_token_count +
            max_new_tokens  # Reserve space for model's response
        )
        budget_for_context = max(0, budget_for_context)

        # Check if context fits within budget
        if context_token_count <= budget_for_context:
            # Context fits completely, use remaining budget for history
            remaining_budget_for_history = budget_for_context - context_token_count
            # Cap history budget at configured limit or remaining budget, whichever is smaller
            history_budget = min(chatbot_settings.chatbot.history_token_budget, remaining_budget_for_history)
            logger.debug(f"Context fits completely ({context_token_count} tokens). History budget: {history_budget} tokens")
        else:
            # Context exceeds budget, truncate context and no history
            context = detokenize_with_llm(context_tokens[:budget_for_context], llm_endpoint)
            history_budget = 0
            previous_messages = None
            logger.debug(f"Context truncated from {context_token_count} to {budget_for_context} tokens. No history included.")

        logger.debug(f"Truncated Context: {context}")

        message_array = [
            {
                "role": "system",
                "content": chatbot_settings.chatbot.initial_system_message,
            }
        ]

        if previous_messages and history_budget > 0:
            # Truncate previous messages to fit within history budget using shared utility
            truncated_messages = truncate_history_by_tokens(
                previous_messages,
                history_budget,
                lambda text: tokenize_with_llm(text, llm_endpoint)
            )
            
            if truncated_messages:
                message_array.extend(truncated_messages)

        final_system_content = chatbot_settings.chatbot.rag_system_message.format(
            context=context,
            rephrased_query=rephrased_query or question,
        )
        message_array.append({
            "role": "system",
            "content": final_system_content,
        })
        message_array.append({
            "role": "user",
            "content": question,
        })

        logger.debug(f"Message array length: {len(message_array)}")
        logger.debug(f"History messages: {len(previous_messages) if previous_messages else 0}")
    else:
        # Legacy mode: use simple prompt template without conversation history
        # Dynamic chunk truncation: truncates the context if it doesn't fit in the sequence length
        question_token_count = len(tokenize_with_llm(question, llm_endpoint))
        remaining_tokens = settings.llm.granite_3_3_8b_instruct_context_length - (
            chatbot_settings.chatbot.prompt_template_token_count +
            question_token_count +
            max_new_tokens  # Reserve space for model's response
        )
        remaining_tokens = max(0, remaining_tokens)
        
        context = detokenize_with_llm(
            tokenize_with_llm(context, llm_endpoint)[:remaining_tokens],
            llm_endpoint
        )
        logger.debug(f"Truncated Context: {context}")

        # Get the appropriate prompt template based on language and format it
        prompt_template = get_prompt_for_language(lang)
        prompt = prompt_template.format(context=context, question=question)
        
        message_array = [
            {
                "role": "user",
                "content": prompt,
            }
        ]

        logger.debug(f"Using legacy prompt mode (conversational_mode=False)")

    headers = get_vllm_headers(api_key)
    payload = {
        "messages": message_array,
        "model": llm_model,
        "max_tokens": max_new_tokens,
        "frequency_penalty": 1.1,
        "temperature": temperature,
        "stop": stop_words,
        "stream": stream
    }
    if stream:
        payload["stream_options"] = {"include_usage": True}
    return headers, payload

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def query_vllm_non_stream(
    question,
    documents,
    llm_endpoint,
    llm_model,
    stop_words,
    max_new_tokens,
    temperature,
    perf_stat_dict,
    lang,
    api_key: str | None = None,
    previous_messages: list | None = None,
    rephrased_query: str | None = None,
):
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    headers, payload = query_vllm_payload(
        question,
        documents,
        llm_endpoint,
        llm_model,
        stop_words,
        max_new_tokens,
        temperature,
        False,
        lang,
        api_key,
        previous_messages,
        rephrased_query,
    )

    # Use requests for synchronous HTTP requests
    start_time = time.time()
    response = misc_utils.SESSION.post(f"{llm_endpoint}/v1/chat/completions", json=payload, headers=headers, stream=False)
    request_time = time.time() - start_time
    perf_stat_dict["inference_time"] = request_time
    response.raise_for_status()
    response_json = response.json()
    if 'usage' in response_json:
        perf_stat_dict["completion_tokens"] = response_json['usage'].get('completion_tokens', 0)
        perf_stat_dict["prompt_tokens"] = response_json['usage'].get('prompt_tokens', 0)

    return response_json

def query_vllm_stream(
    question,
    documents,
    llm_endpoint,
    llm_model,
    stop_words,
    max_new_tokens,
    temperature,
    perf_stat_dict,
    lang,
    api_key: str | None = None,
    previous_messages: list | None = None,
    rephrased_query: str | None = None,
):
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    headers, payload = query_vllm_payload(
        question,
        documents,
        llm_endpoint,
        llm_model,
        stop_words,
        max_new_tokens,
        temperature,
        True,
        lang,
        api_key,
        previous_messages,
        rephrased_query,
    )
    try:
        # Use requests for synchronous HTTP requests
        logger.debug("STREAMING RESPONSE")
        token_latencies = []
        start_time = time.time()
        last_token_time = start_time

        with misc_utils.SESSION.post(f"{llm_endpoint}/v1/chat/completions", json=payload, headers=headers, stream=True) as r:
            for raw_line in r.iter_lines(decode_unicode=True):
                if not raw_line:
                    continue

                if not raw_line.startswith("data: "):
                    continue

                data_str = raw_line[len("data: "):]
                if data_str == "[DONE]":
                    break

                try:
                    chunk = json.loads(data_str)

                    # If this is a usage chunk (common in final chunk of OpenAI streams)
                    if 'usage' in chunk and chunk['usage'] is not None:
                        perf_stat_dict["completion_tokens"] = chunk['usage'].get('completion_tokens', 0)
                        perf_stat_dict["prompt_tokens"] = chunk['usage'].get('prompt_tokens', 0)

                    # Only record latency for actual token chunks (choices)
                    if 'choices' in chunk and len(chunk['choices']) > 0:
                        now = time.time()
                        token_latencies.append(now - last_token_time)
                        last_token_time = now
                        yield f"{raw_line}\n\n"

                except json.JSONDecodeError:
                    continue

        request_time = time.time() - start_time
        perf_stat_dict["token_latencies"] = token_latencies
        perf_stat_dict["inference_time"] = request_time

    except requests.exceptions.RequestException as e:
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error calling vLLM stream API: {error_details}")
        yield f"data: {json.dumps({'error': error_details})}\n\n"
        yield "data: [DONE]\n\n"
    except Exception as e:
        logger.error(f"Error calling vLLM stream API: {e}")
        yield f"data: {json.dumps({'error': str(e)})}\n\n"
        yield "data: [DONE]\n\n"

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def query_vllm_summarize(
    llm_endpoint: str,
    messages: list,
    model: str,
    max_tokens: int,
    temperature: float,
):
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    headers = get_vllm_headers(settings.model_endpoints.vllm_api_key)
    stop_words = [w for w in summarize_settings.summarize.summarization_stop_words.split(",") if w]
    payload = {
        "messages": messages,
        "model": model,
        "max_tokens": max_tokens,
        "temperature": temperature,
    }
    if stop_words:
        payload["stop"] = stop_words

    response = misc_utils.SESSION.post(
        f"{llm_endpoint}/v1/chat/completions",
        json=payload,
        headers=headers,
        stream=False,
    )
    response.raise_for_status()

    result = response.json()
    logger.debug(f"vLLM response: {result}")
    content = ""
    input_tokens = 0
    output_tokens = 0
    if "choices" in result and len(result["choices"]) > 0:
        content = result["choices"][0].get("message", {}).get("content", "") or ""
        input_tokens = result.get("usage", {}).get("prompt_tokens", 0)
        output_tokens = result.get("usage", {}).get("completion_tokens", 0)
    return content.strip(), input_tokens, output_tokens

def query_vllm_summarize_stream(
    llm_endpoint: str,
    messages: list,
    model: str,
    max_tokens: int,
    temperature: float,
):
    """Stream a summarization request to vLLM, yielding raw SSE lines."""
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    headers = get_vllm_headers(settings.model_endpoints.vllm_api_key)
    stop_words = [w for w in summarize_settings.summarize.summarization_stop_words.split(",") if w]
    payload = {
        "messages": messages,
        "model": model,
        "max_tokens": max_tokens,
        "temperature": temperature,
        "stream": True,
    }
    if stop_words:
        payload["stop"] = stop_words

    try:
        logger.debug("STREAMING SUMMARIZE RESPONSE")
        with misc_utils.SESSION.post(
            f"{llm_endpoint}/v1/chat/completions",
            json=payload,
            headers=headers,
            stream=True,
        ) as r:
            r.raise_for_status()
            for raw_line in r.iter_lines(decode_unicode=True):
                if not raw_line:
                    continue
                yield f"{raw_line}\n\n"
    except requests.exceptions.RequestException as e:
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error calling vLLM stream API: {error_details}")
        yield f"data: {json.dumps({'error': error_details})}\n\n"
        yield "data: [DONE]\n\n"
    except Exception as e:
        logger.error(f"Error calling vLLM stream API: {e}")
        yield f"data: {json.dumps({'error': str(e)})}\n\n"
        yield "data: [DONE]\n\n"

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def tokenize_with_llm(prompt, emb_endpoint, max_retries=3):
    """
    Tokenize text using the LLM embedding endpoint with retry logic.

    Args:
        prompt: Text to tokenize
        emb_endpoint: Embedding endpoint URL
        max_retries: Maximum number of retry attempts (default: 3)

    Returns:
        List of tokens

    Raises:
        RuntimeError: If SESSION is not initialized
        requests.exceptions.RequestException: If all retries fail
    """
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    payload = {
        "prompt": prompt
    }

    response = misc_utils.SESSION.post(f"{emb_endpoint}/tokenize", json=payload)
    response.raise_for_status()
    result = response.json()
    tokens = result.get("tokens", [])

    return tokens

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def detokenize_with_llm(tokens, emb_endpoint, max_retries=3):
    """
    Detokenize tokens using the LLM embedding endpoint with retry logic.

    Args:
        tokens: List of tokens to detokenize
        emb_endpoint: Embedding endpoint URL
        max_retries: Maximum number of retry attempts (default: 3)

    Returns:
        Detokenized text string

    Raises:
        RuntimeError: If SESSION is not initialized
        requests.exceptions.RequestException: If all retries fail
    """
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    payload = {
        "tokens": tokens
    }

    response = misc_utils.SESSION.post(f"{emb_endpoint}/detokenize", json=payload)
    response.raise_for_status()
    result = response.json()
    prompt = result.get("prompt", "")

    return prompt
