"""
Generic retry utility module for handling transient failures in HTTP requests and local operations.

This module provides decorators and functions to retry operations that may fail
due to temporary issues like:
- 5xx server errors (especially 500 Internal Server Error)
- Connection timeouts
- Connection aborted/reset errors
- Connection pool exhaustion
- Transient resource issues (memory, file locks, etc.)
- Document conversion errors (Docling)

The retry logic uses exponential backoff to avoid overwhelming the server.
"""

import time
import functools
import requests
from typing import Callable, TypeVar, Any, Optional, Tuple, Type
from opensearchpy import OpenSearchException, ConnectionError as OSConnectionError, TransportError
from common.misc_utils import get_logger, DoclingConversionError

logger = get_logger("retry_utils")

T = TypeVar('T')


def is_retryable_error(exception: Exception, allow_local_retries: bool = False) -> bool:
    """
    Determine if an exception is retryable.
    
    Args:
        exception: The exception to check
        allow_local_retries: If True, allows retrying certain local operation errors
                           (e.g., MemoryError, OSError with specific codes).
                           Default is False for safety.
        
    Returns:
        True if the error is retryable, False otherwise
    """
    # Network-related errors (HTTP and connection issues)
    if isinstance(exception, (requests.exceptions.HTTPError, requests.exceptions.RequestException)):
        # Check for HTTP 5xx errors
        if isinstance(exception, requests.exceptions.HTTPError) and exception.response is not None:
            status_code = exception.response.status_code
            if 500 <= status_code < 600:
                return True
        
        # Check for connection-related errors
        error_str = str(exception)
        if any(pattern in error_str for pattern in [
            "Already borrowed",
            "Connection aborted",
            "RemoteDisconnected",
            "Connection reset",
            "Connection refused",
            "Connection timeout",
            "Read timed out",
            "Timeout",
            "ConnectionError",
            "Internal Server Error",
            "Service Unavailable",
            "Gateway Timeout",
            "Bad Gateway"
        ]):
            return True
    
    # OpenSearch-related errors
    if isinstance(exception, (OpenSearchException, OSConnectionError, TransportError)):
        # Connection and transport errors are always retryable
        if isinstance(exception, (OSConnectionError, TransportError)):
            return True
        
        # For generic OpenSearchException, check if it contains transient error indicators
        # (OpenSearch wraps HTTP errors in exceptions with error details in the message)
        error_str = str(exception)
        if any(pattern in error_str for pattern in [
            "Connection",
            "Timeout",
            "503",
            "500",
            "502",
            "504",
            "Service Unavailable",
            "Internal Server Error",
            "Bad Gateway",
            "Gateway Timeout"
        ]):
            return True
    
    # Docling conversion errors (always retryable when they occur)
    if isinstance(exception, DoclingConversionError):
        return True
    
    # Local operation errors (only if explicitly allowed)
    # These are typically for PDF processing or other local operations
    if allow_local_retries:
        # Memory errors might be transient if system is under load
        if isinstance(exception, MemoryError):
            return True
        
        # Certain OSError codes that might be transient
        if isinstance(exception, OSError):
            # errno 11: Resource temporarily unavailable
            # errno 12: Cannot allocate memory
            # errno 24: Too many open files
            # errno 28: No space left on device (might be transient if cleanup happens)
            if hasattr(exception, 'errno') and exception.errno in [11, 12, 24]:
                return True
        
        # For generic exceptions when allow_local_retries is True,
        # check for transient error patterns in the message
        error_str = str(exception).lower()
        if any(pattern in error_str for pattern in [
            "temporarily unavailable",
            "resource temporarily unavailable",
            "too many open files",
            "cannot allocate memory",
            "memory error"
        ]):
            return True
    
    return False


def retry_on_transient_error(
    max_retries: int = 3,
    initial_delay: float = 1.0,
    backoff_multiplier: float = 2.0,
    max_delay: float = 10.0,
    retryable_exceptions: Optional[Tuple[Type[Exception], ...]] = None,
    allow_local_retries: bool = False
) -> Callable[[Callable[..., T]], Callable[..., T]]:
    """
    Decorator to retry a function on transient errors with exponential backoff.
    
    Args:
        max_retries: Maximum number of retry attempts (default: 3)
        initial_delay: Initial delay between retries in seconds (default: 1.0)
        backoff_multiplier: Multiplier for delay after each retry (default: 2.0)
        max_delay: Maximum delay between retries in seconds (default: 10.0)
        retryable_exceptions: Tuple of exception types to retry on. If None, uses
                            requests.exceptions.RequestException for HTTP operations
        allow_local_retries: If True, allows retrying certain local operation errors
                           (e.g., MemoryError, resource temporarily unavailable).
                           Use this for local operations like PDF processing. (default: False)
    
    Returns:
        Decorated function with retry logic
        
    Example:
        # For HTTP operations (default):
        @retry_on_transient_error(max_retries=3, initial_delay=1.0)
        def call_api(url):
            response = requests.get(url)
            response.raise_for_status()
            return response.json()
        
        # For local operations (PDF processing, etc.):
        @retry_on_transient_error(max_retries=3, retryable_exceptions=(Exception,), allow_local_retries=True)
        def process_pdf(path):
            return convert_document(path)
    """
    if retryable_exceptions is None:
        retryable_exceptions = (
            requests.exceptions.RequestException,
            OpenSearchException,
            OSConnectionError,
            TransportError
        )
    
    def decorator(func: Callable[..., T]) -> Callable[..., T]:
        @functools.wraps(func)
        def wrapper(*args: Any, **kwargs: Any) -> T:
            last_exception: Optional[Exception] = None
            
            for attempt in range(max_retries):
                try:
                    result = func(*args, **kwargs)
                    
                    # Log successful retry if it wasn't the first attempt
                    if attempt > 0:
                        logger.debug(
                            f"{func.__name__} succeeded on attempt {attempt + 1}/{max_retries}"
                        )
                    
                    return result
                    
                except retryable_exceptions as e:
                    last_exception = e
                    
                    # Check if this is a retryable error
                    if not is_retryable_error(e, allow_local_retries=allow_local_retries):
                        # Not retryable, raise immediately
                        logger.debug(
                            f"{func.__name__} failed with non-retryable error: {e}"
                        )
                        raise
                    
                    # Last attempt, don't retry
                    if attempt == max_retries - 1:
                        logger.debug(
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
                    
                    logger.debug(
                        f"{func.__name__} failed (attempt {attempt + 1}/{max_retries}). "
                        f"Retrying in {backoff_time:.2f}s... Error: {error_details}"
                    )
                    
                    time.sleep(backoff_time)
                    
                except Exception as e:
                    # Unexpected error, log and raise
                    logger.debug(
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


