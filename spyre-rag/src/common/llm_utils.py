import logging
import requests
from requests.adapters import HTTPAdapter
from concurrent.futures import ThreadPoolExecutor, as_completed
from tqdm import tqdm

from common.misc_utils import get_logger
from common.settings import get_settings

logger = get_logger("LLM")

is_debug = logger.isEnabledFor(logging.DEBUG) 
tqdm_wrapper = None
if is_debug:
    tqdm_wrapper = tqdm
else:
    tqdm_wrapper = lambda x, **kwargs: x
    
settings = get_settings()

SESSION = None

def create_llm_session(pool_maxsize, pool_connections: int = 2, pool_block: bool = True):
    global SESSION

    # SESSION object will be used by instruct and embedding endpoints. Hence keeping pool_connections = 2
    # Need to use SESSION object for following reasons:
    # - To limit the number of concurrent requests getting created to instruct vLLM's API to 32
    # - To fix the ephemeral port exhaustion issue during chunking, since numerous tokenize calls are made to embedding server
    if SESSION is None:
        adapter = HTTPAdapter(
            pool_connections=pool_connections,
            pool_maxsize=pool_maxsize,
            pool_block=pool_block
        )

        session = requests.Session()
        session.mount("http://", adapter)
        session.mount("https://", adapter)

        SESSION = session

def summarize_and_classify_single_table(prompt, gen_model, llm_endpoint):
    payload = {
        "model": gen_model,
        "messages": [{"role": "user", "content": prompt}],
        "temperature": 0,
        "max_tokens": 512,
        "stream": False,
    }

    try:
        response = SESSION.post(f"{llm_endpoint}/v1/chat/completions", json=payload)
        response.raise_for_status()
        data = response.json() or {}
        choices = data.get("choices", [])
        text = ""
        if choices:
            text = (choices[0].get("message", {}).get("content") or "").strip()
        summary = ""
        decision = True
        for line in text.splitlines():
            if line.lower().startswith("summary:"):
                summary = line[len("summary:"):].strip()
            elif line.lower().startswith("decision:"):
                decision = "yes" in line.lower()

        return summary or "No summary.", decision

    except Exception as e:
        logger.error(f"Error summarizing/classifying table: {e}")
        return "No summary.", True

def summarize_and_classify_tables(table_htmls, gen_model, llm_endpoint, pdf_path, max_workers=32):
    prompts = [
        settings.prompts.table_summary_and_classify.format(content=html)
        for html in table_htmls
    ]

    summaries = [None] * len(prompts)
    decisions = [True] * len(prompts)

    with ThreadPoolExecutor(max_workers=min(max_workers, len(prompts))) as executor:
        futures = {
            executor.submit(
                summarize_and_classify_single_table,
                prompt,
                gen_model,
                llm_endpoint
            ): idx
            for idx, prompt in enumerate(prompts)
        }

        for future in tqdm_wrapper(
            as_completed(futures),
            total=len(prompts),
            desc=f"Summarizing & classifying tables of '{pdf_path}'"
        ):
            idx = futures[future]
            summaries[idx], decisions[idx] = future.result()

    return summaries, decisions

def query_vllm_models(llm_endpoint):
    logger.debug('Querying VLLM models')
    try:
        response = SESSION.get(f"{llm_endpoint}/v1/models")
        response.raise_for_status()
        resp_json = response.json()
    except requests.exceptions.RequestException as e:
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error calling vLLM models API: {error_details}")
        return {"error": error_details}, 0.
    except Exception as e:
        logger.error(f"Error calling vLLM models API: {e}")
        return {"error": str(e)}, 0.
    return resp_json

def query_vllm_payload(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature,
                stream):
    context = "\n\n".join([doc.get("page_content") for doc in documents])

    logger.debug(f'Original Context: {context}')

    # dynamic chunk truncation: truncates the context, if doesn't fit in the sequence length
    question_token_count = len(tokenize_with_llm(question, llm_endpoint))
    reamining_tokens = settings.max_input_length - (settings.prompt_template_token_count + question_token_count)
    context = detokenize_with_llm(tokenize_with_llm(context, llm_endpoint)[:reamining_tokens], llm_endpoint)
    logger.debug(f"Truncated Context: {context}")

    prompt = settings.prompts.query_vllm_stream.format(context=context, question=question)
    logger.debug("PROMPT:  ", prompt)
    headers = {
        "accept": "application/json",
        "Content-type": "application/json"
    }
    payload = {
        "messages": [{"role": "user", "content": prompt}],
        "model": llm_model,
        "max_tokens": max_new_tokens,
        "repetition_penalty": 1.1,
        "temperature": temperature,
        "stop": stop_words,
        "stream": stream
    }
    return headers, payload

def query_vllm_non_stream(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature):
    headers, payload = query_vllm_payload(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature, False )
    try:
        # Use requests for synchronous HTTP requests
        response = SESSION.post(f"{llm_endpoint}/v1/chat/completions", json=payload, headers=headers, stream=False)
        response.raise_for_status()
    except requests.exceptions.RequestException as e:
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error calling vLLM API: {error_details}")
        return {"error": error_details}
    except Exception as e:
        logger.error(f"Error calling vLLM API: {e}")
        return {"error": str(e)}
    return response.json()

def query_vllm_stream(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature):
    headers, payload = query_vllm_payload(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature, True )
    try:
        # Use requests for synchronous HTTP requests
        logger.debug("STREAMING RESPONSE")
        with SESSION.post(f"{llm_endpoint}/v1/chat/completions", json=payload, headers=headers, stream=True) as r:
            for raw_line in r.iter_lines(decode_unicode=True):
                if not raw_line:
                    continue

                yield f"{raw_line}\n\n"
    except requests.exceptions.RequestException as e:
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error calling vLLM stream API: {error_details}")
        return {"error": error_details}
    except Exception as e:
        logger.error(f"Error calling vLLM stream API: {e}")
        return {"error": str(e)}

def query_vllm_summarize(
    llm_endpoint: str,
    messages: list,
    model: str,
    max_tokens: int,
    temperature: float,
):
    headers = {
        "accept": "application/json",
        "Content-type": "application/json",
    }
    stop_words = [w for w in settings.summarization_stop_words.split(",") if w]
    payload = {
        "messages": messages,
        "model": model,
        "max_tokens": max_tokens,
        "temperature": temperature,
    }
    if stop_words:
        payload["stop"] = stop_words

    try:
        response = SESSION.post(
            f"{llm_endpoint}/v1/chat/completions",
            json=payload,
            headers=headers,
            stream=False,
        )
        response.raise_for_status()
    except requests.exceptions.RequestException as e:
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error calling vLLM API: {error_details}")
        return error_details, 0, 0
    except Exception as e:
        logger.error(f"Error calling vLLM API: {e}")
        return str(e), 0, 0

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

def tokenize_with_llm(prompt, emb_endpoint):
    payload = {
        "prompt": prompt
    }
    try:
        response = SESSION.post(f"{emb_endpoint}/tokenize", json=payload)
        response.raise_for_status()
        result = response.json()
        tokens = result.get("tokens", [])
        return tokens
    except requests.exceptions.RequestException as e:
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error encoding prompt: {error_details}")
        raise e
    except Exception as e:
        logger.error(f"Error encoding prompt: {e}")
        raise e

def detokenize_with_llm(tokens, emb_endpoint):
    payload = {
        "tokens": tokens
    }
    try:
        response = SESSION.post(f"{emb_endpoint}/detokenize", json=payload)
        response.raise_for_status()
        result = response.json()
        prompt = result.get("prompt", "")
        return prompt
    except requests.exceptions.RequestException as e:
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error decoding tokens: {error_details}")
        raise e
    except Exception as e:
        logger.error(f"Error decoding tokens: {e}")
        raise e
