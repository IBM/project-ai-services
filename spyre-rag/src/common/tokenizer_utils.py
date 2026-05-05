"""Local tokenization utilities using HuggingFace transformers."""

from transformers import AutoTokenizer, PreTrainedTokenizerBase
from typing import List, Optional
import threading
from common.misc_utils import get_logger

logger = get_logger("tokenizer")

_tokenizer: Optional[PreTrainedTokenizerBase] = None
_tokenizer_lock = threading.Lock()


def initialize_tokenizer(model_path: str) -> None:
    """
    Initialize the tokenizer from a local model path.
    
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


def tokenize(text: str) -> List[int]:
    """
    Tokenize text and return token IDs.
    
    Args:
        text: Text to tokenize
        
    Returns:
        List of token IDs
        
    Raises:
        RuntimeError: If tokenizer not initialized
    """
    if _tokenizer is None:
        raise RuntimeError(
            "Tokenizer not initialized. Call initialize_tokenizer() first."
        )
    return _tokenizer.encode(text, add_special_tokens=False)


def detokenize(tokens: List[int]) -> str:
    """
    Detokenize token IDs and return text.
    
    Args:
        tokens: List of token IDs to detokenize
        
    Returns:
        Detokenized text string
        
    Raises:
        RuntimeError: If tokenizer not initialized
    """
    if _tokenizer is None:
        raise RuntimeError(
            "Tokenizer not initialized. Call initialize_tokenizer() first."
        )
    return _tokenizer.decode(tokens, skip_special_tokens=True)

# Made with Bob
