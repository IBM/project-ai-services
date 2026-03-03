import hashlib
import logging
import os
from pathlib import Path

LOG_LEVEL = logging.INFO

LOCAL_CACHE_DIR = "/var/cache"
chunk_suffix = "_clean_chunk.json"
text_suffix = "_clean_text.json"
table_suffix = "_tables.json"

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


def get_txt_tab_filenames(file_paths, out_path):
    original_filenames = [fp.split('/')[-1] for fp in file_paths]
    input_txt_files, input_tab_files = [], []
    for fn in original_filenames:
        f, _ = os.path.splitext(fn)
        input_txt_files.append(f'{out_path}/{f}{text_suffix}')
        input_tab_files.append(f'{out_path}/{f}{table_suffix}')
    return original_filenames, input_txt_files, input_tab_files


def get_model_endpoints():
    emb_model_dict = {
        'emb_endpoint': os.getenv("EMB_ENDPOINT"),
        'emb_model':    os.getenv("EMB_MODEL"),
        'max_tokens':   int(os.getenv("EMB_MAX_TOKENS", "512")),
    }

    llm_model_dict = {
        'llm_endpoint': os.getenv("LLM_ENDPOINT", ""),
        'llm_model':    os.getenv("LLM_MODEL", ""),
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

def generate_file_checksum(file):
    sha256 = hashlib.sha256()
    with open(file, 'rb') as f:
        for chunk in iter(lambda: f.read(128 * sha256.block_size), b''):
            sha256.update(chunk)
    return sha256.hexdigest()

def verify_checksum(file, checksum_file):
    file_sha256 = generate_file_checksum(file)
    f = open(checksum_file, "r")
    data = f.read()
    csum = data.split(' ')[0]
    if csum == file_sha256:
        return True
    return False


def has_allowed_extension(path, allowed_file_types):
    return path.lower().split('.')[-1] in allowed_file_types

def is_supported_file(path,allowed_file_types):
    try:
        with open(path, "rb") as f:
            header = f.read(8)
        for signature in allowed_file_types.values():
            if header.startswith(signature):
                return True
        return False
    except Exception as e:
        return False

def get_unprocessed_files(original_files, processed_pdfs):
    return set(original_files).difference(set(processed_pdfs))
