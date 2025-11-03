import os
import json
import logging

LOG_LEVEL = logging.INFO

LOCAL_CACHE_DIR = "/var/cache"

def set_log_level(level):
    global LOG_LEVEL
    LOG_LEVEL = level

def get_logger(name):
    logger = logging.getLogger(name)
    logger.setLevel(LOG_LEVEL)
    logger.propagate = False

    console_handler = logging.StreamHandler()
    console_handler.setLevel(LOG_LEVEL)
    formatter = logging.Formatter(
        '%(asctime)s - %(name)-18s - %(levelname)-8s - %(message)s',
        datefmt='%Y-%m-%d %H:%M:%S')
    console_handler.setFormatter(formatter)

    logger.addHandler(console_handler)

    return logger

def get_prompts():
       
    env_path = os.getenv("PROMPT_PATH")
    if env_path and os.path.exists(env_path):
        prompt_path = env_path
    else:
        base_dir = os.path.dirname(os.path.abspath(__file__))
        prompt_path = os.path.join(base_dir, "..", "prompts.json")
        prompt_path = os.path.normpath(prompt_path)

    try:
        with open(prompt_path, "r", encoding="utf-8") as file:
            data = json.load(file)

            llm_classify = data.get("llm_classify")
            table_summary = data.get("table_summary")
            query_vllm = data.get("query_vllm")
            query_vllm_stream = data.get("query_vllm_stream")

            if any(prompt in (None, "") for prompt in (
                    llm_classify,
                    table_summary,
                    query_vllm,
                    query_vllm_stream,
            )):
                raise ValueError(f"One or more prompt variables are missing or empty in '{prompt_path}' file.")

            return llm_classify, table_summary, query_vllm, query_vllm_stream
    except FileNotFoundError:
        raise FileNotFoundError(f"JSON file not found at: {prompt_path}")
    except json.JSONDecodeError as e:
        raise ValueError(f"Error parsing JSON at {prompt_path}: {e}")


def get_txt_tab_filenames(file_paths, out_path):
    original_filenames = [fp.split('/')[-1] for fp in file_paths]
    input_txt_files, input_tab_files = [], []
    for fn in original_filenames:
        f, _ = os.path.splitext(fn)
        input_txt_files.append(f'{out_path}/{f}_clean_text.json')
        input_tab_files.append(f'{out_path}/{f}_tables.json')
    return original_filenames, input_txt_files, input_tab_files


def get_model_endpoints():
    emb_model_dict = {
        'emb_endpoint': os.getenv("EMB_ENDPOINT"),
        'emb_model':    os.getenv("EMB_MODEL"),
        'max_tokens':   int(os.getenv("EMB_MAX_TOKENS", "512")),
    }

    llm_model_dict = {
        'llm_endpoint': os.getenv("LLM_ENDPOINT"),
        'llm_model':    os.getenv("LLM_MODEL"),
    }

    reranker_model_dict = {
        'reranker_endpoint': os.getenv("RERANKER_ENDPOINT"),
        'reranker_model':    os.getenv("RERANKER_MODEL"),
    }

    return emb_model_dict, llm_model_dict, reranker_model_dict

def setup_cache_dir(dir):
    cache_dir = os.path.join(LOCAL_CACHE_DIR, f'{dir}_cache')
    os.makedirs(cache_dir, exist_ok=True)
    return cache_dir
