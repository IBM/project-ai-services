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

def create_llm_session(pool_maxsize, pool_connections: int = 1, pool_block: bool = True):
    global SESSION

    # SESSION object will be used by instruct LLM's endpoint only. Hence keeping pool_connections = 1
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

def classify_text_with_llm(text_blocks, gen_model, llm_endpoint, pdf_path, batch_size=32):
    all_prompts = [settings.prompts.llm_classify.format(text=item.strip()) for item in text_blocks]
    
    decisions = []
    for i in tqdm_wrapper(range(0, len(all_prompts), batch_size), desc=f"Classifying table summaries of '{pdf_path}'"):
        batch_prompts = all_prompts[i:i + batch_size]

        payload = {
            "model": gen_model,
            "prompt": batch_prompts,
            "temperature": 0,
            "max_tokens": 3,
        }
        try:
            response = SESSION.post(f"{llm_endpoint}/v1/completions", json=payload)
            response.raise_for_status()
            result = response.json()
            choices = result.get("choices", [])
            for choice in choices:
                reply = choice.get("text", "").strip().lower()
                decisions.append("yes" in reply)
        except requests.exceptions.RequestException as e:
            error_details = str(e)
            if e.response is not None:
                error_details += f", Response Text: {e.response.text}"
            logger.error(f"Error while classifying text with vLLM: {error_details}")
            decisions.append(True)
        except Exception as e:
            logger.error(f"Error while classifying text with vLLM: {e}")
            decisions.append(True)
    return decisions


def summarize_single_table(prompt, gen_model, llm_endpoint):
    payload = {
        "model": gen_model,
        "prompt": prompt,
        "temperature": 0,
        "repetition_penalty": 1.1,
        "max_tokens": 512,
        "stream": False,
    }
    try:
        response = SESSION.post(f"{llm_endpoint}/v1/completions", json=payload)
        response.raise_for_status()
        result = response.json()
        reply = result.get("choices", [{}])[0].get("text", "").strip()
        return reply
    except requests.exceptions.RequestException as e:
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error summarizing table: {error_details}")
        return "No summary."
    except Exception as e:
        logger.error(f"Error summarizing table: {e}")
        return "No summary."


def summarize_table(table_html, gen_model, llm_endpoint, pdf_path, max_workers=32):
    all_prompts = [settings.prompts.table_summary.format(content=html) for html in table_html]

    summaries = [None] * len(all_prompts)

    with ThreadPoolExecutor(max_workers=max(1, min(max_workers, len(all_prompts)))) as executor:
        futures = {
            executor.submit(summarize_single_table, prompt, gen_model, llm_endpoint): idx
            for idx, prompt in enumerate(all_prompts)
        }

        for future in tqdm_wrapper(as_completed(futures), total=len(all_prompts), desc=f"Summarizing tables of '{pdf_path}'"):
            idx = futures[future]
            try:
                summaries[idx] = future.result()
            except Exception as e:
                logger.error(f"Thread failed at index {idx}: {e}")
                summaries[idx] = "No summary."

    return summaries

def query_vllm_models(llm_endpoint):
    logger.debug('Querying VLLM models')
    try:
        response = SESSION.get(f"{llm_endpoint}/v1/models")
        response.raise_for_status()
        resp_json = response.json()
    except requests.exceptions.RequestException as e:
        logger.error(f"Error calling vLLM models API: {e}, {e.response.text}")
        return {"error": str(e) + "\n" + e.response.text}, 0.
    except Exception as e:
        logger.error(f"Error calling vLLM models API: {e}")
        return {"error": str(e)}, 0.
    return resp_json


def query_vllm_stream(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature, stream=False,
                max_input_length=6000, dynamic_chunk_truncation=True):
    template_token_count = 250
    context = "\n\n".join([doc.get("page_content") for doc in documents])

    logger.debug(f'Original Context: {context}')
    if dynamic_chunk_truncation:
        question_token_count = len(tokenize_with_llm(question, llm_endpoint))
        reamining_tokens = max_input_length - (template_token_count + question_token_count)
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

    try:
        # Use requests for synchronous HTTP requests
        logger.debug("STREAMING RESPONSE")
        with SESSION.post(f"{llm_endpoint}/v1/chat/completions", json=payload, headers=headers, stream=stream) as r:
            for raw_line in r.iter_lines(decode_unicode=True):
                if not raw_line:
                    continue

                yield f"{raw_line}\n\n"
    except requests.exceptions.RequestException as e:
        error_details = str(e)
        if e.response is not None:
            error_details += f", Response Text: {e.response.text}"
        logger.error(f"Error calling vLLM stream API: {error_details}")
        return {"error": error_details}, 0.
    except Exception as e:
        logger.error(f"Error calling vLLM stream API: {e}")
        return {"error": str(e)}, 0.

def tokenize_with_llm(prompt, emb_endpoint):
    payload = {
        "prompt": prompt
    }
    try:
        response = requests.post(f"{emb_endpoint}/tokenize", json=payload)
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
        response = requests.post(f"{emb_endpoint}/detokenize", json=payload)
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
