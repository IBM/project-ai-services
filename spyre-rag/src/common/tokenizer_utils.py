"""Tokenization utilities supporting both local and LLM-based tokenization."""

from transformers import AutoTokenizer, PreTrainedTokenizerBase
from typing import List, Optional
import threading
from common.misc_utils import get_logger
from common.settings import settings

logger = get_logger("tokenizer")

_tokenizer: Optional[PreTrainedTokenizerBase] = None
_tokenizer_lock = threading.Lock()


def initialize_local_tokenizer(model_path: str) -> None:
    """
    Initialize the local tokenizer from a model path for offline tokenization.
    If not initialized, the system will fall back to LLM-based tokenization via API.
    
    Args:
        model_path: Path to the local model directory
        
    Raises:
        Exception: If tokenizer fails to load
    """
    global _tokenizer
    with _tokenizer_lock:
        if _tokenizer is None:
            logger.info(f"Loading tokenizer from {model_path}")
            _tokenizer = AutoTokenizer.from_pretrained(
                model_path,
                local_files_only=True
            )
            logger.info("Tokenizer loaded successfully")


def _tokenize_with_llm(prompt: str) -> List[int]:
    """
    Tokenize text using the LLM embedding endpoint.
    
    Args:
        prompt: Text to tokenize
        
    Returns:
        List of token IDs
        
    Raises:
        RuntimeError: If SESSION is not initialized
        requests.exceptions.RequestException: If request fails
    """
    import common.misc_utils as misc_utils
    from common.retry_utils import retry_on_transient_error
    
    emb_endpoint = settings.model_endpoints.emb_endpoint
    
    @retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
    def _tokenize_request():
        if misc_utils.SESSION is None:
            raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")
        
        payload = {"prompt": prompt}
        response = misc_utils.SESSION.post(f"{emb_endpoint}/tokenize", json=payload)
        response.raise_for_status()
        result = response.json()
        return result.get("tokens", [])
    
    return _tokenize_request()


def _detokenize_with_llm(tokens: List[int]) -> str:
    """
    Detokenize tokens using the LLM embedding endpoint.
    
    Args:
        tokens: List of token IDs to detokenize
        
    Returns:
        Detokenized text string
        
    Raises:
        RuntimeError: If SESSION is not initialized
        requests.exceptions.RequestException: If request fails
    """
    import common.misc_utils as misc_utils
    from common.retry_utils import retry_on_transient_error
    
    emb_endpoint = settings.model_endpoints.emb_endpoint
    
    @retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
    def _detokenize_request():
        if misc_utils.SESSION is None:
            raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")
        
        payload = {"tokens": tokens}
        response = misc_utils.SESSION.post(f"{emb_endpoint}/detokenize", json=payload)
        response.raise_for_status()
        result = response.json()
        return result.get("prompt", "")
    
    return _detokenize_request()


def tokenize(text: str) -> List[int]:
    """
    Tokenize text and return token IDs.
    Uses local tokenizer if TOKENIZER_MODEL_PATH is set, otherwise falls back to LLM-based tokenization.
    
    Args:
        text: Text to tokenize
        
    Returns:
        List of token IDs
        
    Raises:
        RuntimeError: If tokenizer not initialized and settings not available
    """
    # Use local tokenizer if available
    if _tokenizer is not None:
        return _tokenizer.encode(text, add_special_tokens=False)
    
    # Fall back to LLM-based tokenization
    logger.debug("Using LLM-based tokenization")
    return _tokenize_with_llm(text)


def detokenize(tokens: List[int]) -> str:
    """
    Detokenize token IDs and return text.
    Uses local tokenizer if TOKENIZER_MODEL_PATH is set, otherwise falls back to LLM-based tokenization.
    
    Args:
        tokens: List of token IDs to detokenize
        
    Returns:
        Detokenized text string
        
    Raises:
        RuntimeError: If tokenizer not initialized and settings not available
    """
    # Use local tokenizer if available
    if _tokenizer is not None:
        return _tokenizer.decode(tokens, skip_special_tokens=True)
    
    # Fall back to LLM-based tokenization
    logger.debug("Using LLM-based detokenization")
    return _detokenize_with_llm(tokens)

# Made with Bob
