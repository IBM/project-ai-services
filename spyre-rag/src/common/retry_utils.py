"""
Generic retry utility module for handling transient failures in HTTP requests.

This module provides decorators and functions to retry operations that may fail
due to temporary issues like:
- 5xx server errors (especially 500 Internal Server Error)
- Connection timeouts
- Connection aborted/reset errors
- Connection pool exhaustion

The retry logic uses exponential backoff to avoid overwhelming the server.
"""

import time
import functools
import requests
from typing import Callable, TypeVar, Any, Optional, Tuple, Type
from common.misc_utils import get_logger

logger = get_logger("retry_utils")

T = TypeVar('T')


def is_retryable_error(exception: Exception) -> bool:
    """
    Determine if an exception is retryable.
    
    Args:
        exception: The exception to check
        
    Returns:
        True if the error is retryable, False otherwise
    """
    # Check for HTTP errors
    if isinstance(exception, requests.exceptions.HTTPError):
        if exception.response is not None:
            status_code = exception.response.status_code
            # Retry on 5xx errors
            if 500 <= status_code < 600:
                response_text = exception.response.text
                # Check for specific error patterns
                if any(pattern in response_text for pattern in [
                    "Already borrowed",
                    "Internal Server Error",
                    "Service Unavailable",
                    "Gateway Timeout",
                    "Bad Gateway"
                ]):
                    return True
                # Retry all 5xx errors by default
                return True
        return False
    
    # Check for connection errors
    if isinstance(exception, requests.exceptions.RequestException):
        error_str = str(exception)
        # Check for connection-related errors
        if any(pattern in error_str for pattern in [
            "Connection aborted",
            "RemoteDisconnected",
            "Connection reset",
            "Connection refused",
            "Connection timeout",
            "Read timed out",
            "Timeout",
            "ConnectionError"
        ]):
            return True
    
    return False


def retry_on_transient_error(
    max_retries: int = 3,
    initial_delay: float = 0.1,
    backoff_multiplier: float = 2.0,
    max_delay: float = 10.0,
    retryable_exceptions: Optional[Tuple[Type[Exception], ...]] = None
) -> Callable[[Callable[..., T]], Callable[..., T]]:
    """
    Decorator to retry a function on transient errors with exponential backoff.
    
    Args:
        max_retries: Maximum number of retry attempts (default: 3)
        initial_delay: Initial delay between retries in seconds (default: 0.1)
        backoff_multiplier: Multiplier for delay after each retry (default: 2.0)
        max_delay: Maximum delay between retries in seconds (default: 10.0)
        retryable_exceptions: Tuple of exception types to retry on. If None, uses
                            requests.exceptions.RequestException
    
    Returns:
        Decorated function with retry logic
        
    Example:
        @retry_on_transient_error(max_retries=3, initial_delay=0.1)
        def call_api(url):
            response = requests.get(url)
            response.raise_for_status()
            return response.json()
    """
    if retryable_exceptions is None:
        retryable_exceptions = (requests.exceptions.RequestException,)
    
    def decorator(func: Callable[..., T]) -> Callable[..., T]:
        @functools.wraps(func)
        def wrapper(*args: Any, **kwargs: Any) -> T:
            last_exception: Optional[Exception] = None
            
            for attempt in range(max_retries):
                try:
                    result = func(*args, **kwargs)
                    
                    # Log successful retry if it wasn't the first attempt
                    if attempt > 0:
                        logger.info(
                            f"{func.__name__} succeeded on attempt {attempt + 1}/{max_retries}"
                        )
                    
                    return result
                    
                except retryable_exceptions as e:
                    last_exception = e
                    
                    # Check if this is a retryable error
                    if not is_retryable_error(e):
                        # Not retryable, raise immediately
                        logger.error(
                            f"{func.__name__} failed with non-retryable error: {e}"
                        )
                        raise
                    
                    # Last attempt, don't retry
                    if attempt == max_retries - 1:
                        logger.error(
                            f"{func.__name__} failed after {max_retries} attempts: {e}"
                        )
                        raise
                    
                    # Calculate backoff delay with exponential increase
                    backoff_time = min(
                        initial_delay * (backoff_multiplier ** attempt),
                        max_delay
                    )
                    
                    # Log the retry attempt
                    error_details = str(e)
                    if isinstance(e, requests.exceptions.HTTPError) and e.response is not None:
                        error_details = f"HTTP {e.response.status_code}: {e.response.text[:100]}"
                    
                    logger.warning(
                        f"{func.__name__} failed (attempt {attempt + 1}/{max_retries}). "
                        f"Retrying in {backoff_time:.2f}s... Error: {error_details}"
                    )
                    
                    time.sleep(backoff_time)
                    
                except Exception as e:
                    # Unexpected error, log and raise
                    logger.error(
                        f"{func.__name__} failed with unexpected error: {e}",
                        exc_info=True
                    )
                    raise
            
            # This should not be reached, but just in case
            if last_exception:
                raise last_exception
            raise RuntimeError(f"{func.__name__} failed after all retries")
        
        return wrapper
    return decorator


