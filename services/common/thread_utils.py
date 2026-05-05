"""
Context-aware thread pool utilities for managing thread execution with proper context propagation.
"""

import contextvars
import functools
from concurrent.futures import ThreadPoolExecutor
from typing import Callable, Any


class ContextAwareThreadPoolExecutor(ThreadPoolExecutor):
    """
    A ThreadPoolExecutor that propagates contextvars to worker threads.
    
    This executor ensures that context variables (like request context, logging context, etc.)
    are properly propagated from the submitting thread to the worker threads.
    
    Usage:
        with ContextAwareThreadPoolExecutor(max_workers=4) as executor:
            future = executor.submit(some_function, arg1, arg2)
            result = future.result()
    """
    
    def submit(self, fn: Callable, *args: Any, **kwargs: Any):
        """
        Submit a callable to be executed with the current context.
        
        Args:
            fn: The callable to execute
            *args: Positional arguments for the callable
            **kwargs: Keyword arguments for the callable
            
        Returns:
            A Future representing the execution of the callable
        """
        # Capture the current context
        context = contextvars.copy_context()
        
        # Wrap the function to run in the captured context
        @functools.wraps(fn)
        def wrapper():
            return context.run(fn, *args, **kwargs)
        
        # Submit the wrapped function to the parent ThreadPoolExecutor
        return super().submit(wrapper)

# Made with Bob
