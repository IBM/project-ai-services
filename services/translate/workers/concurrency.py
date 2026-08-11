"""
Concurrency management for the translate service.
Consolidates all semaphore logic in one place.

Three semaphores are managed:
- ``job_limiter``       — async job admission (default 8 slots)
- ``chunk_semaphore``   — per-job chunk parallelism cap (default 4 slots)
- ``vllm_semaphore``    — shared vLLM inference gate (default 32 slots)
"""

import asyncio
from typing import Optional

from translate.settings import settings


class ConcurrencyManager:
    """
    Manages concurrency limits for the translate service.

    Limits are driven from ``TranslationConfig``:
    - ``max_concurrent_jobs``       → job admission semaphore
    - ``chunk_parallelism``         → per-job chunk semaphore
    - ``common.llm.max_batch_size`` → shared vLLM semaphore

    Call ``initialize()`` once from the FastAPI lifespan before any
    endpoint handler runs.
    """

    def __init__(self) -> None:
        # Semaphores are None until initialize() is called inside the lifespan.
        self._job_limiter: Optional[asyncio.BoundedSemaphore] = None
        self._chunk_semaphore: Optional[asyncio.BoundedSemaphore] = None
        self._vllm_semaphore: Optional[asyncio.BoundedSemaphore] = None

    def initialize(self) -> None:
        """
        Create all semaphores inside the running event loop.

        Must be called from within an async context (e.g. the FastAPI
        lifespan) so that the semaphores bind to the correct uvloop loop.
        """
        self._job_limiter = asyncio.BoundedSemaphore(
            settings.translate.max_concurrent_jobs
        )
        self._chunk_semaphore = asyncio.BoundedSemaphore(
            settings.translate.chunk_parallelism
        )
        self._vllm_semaphore = asyncio.BoundedSemaphore(
            settings.common.llm.max_batch_size
        )

    @property
    def job_limiter(self) -> asyncio.BoundedSemaphore:
        """Async job admission semaphore."""
        if self._job_limiter is None:
            raise RuntimeError(
                "ConcurrencyManager.initialize() has not been called. "
                "Ensure it is invoked from the FastAPI lifespan."
            )
        return self._job_limiter

    @property
    def chunk_semaphore(self) -> asyncio.BoundedSemaphore:
        """Per-job chunk parallelism semaphore."""
        if self._chunk_semaphore is None:
            raise RuntimeError(
                "ConcurrencyManager.initialize() has not been called. "
                "Ensure it is invoked from the FastAPI lifespan."
            )
        return self._chunk_semaphore

    @property
    def vllm_semaphore(self) -> asyncio.BoundedSemaphore:
        """Shared vLLM inference gate semaphore."""
        if self._vllm_semaphore is None:
            raise RuntimeError(
                "ConcurrencyManager.initialize() has not been called. "
                "Ensure it is invoked from the FastAPI lifespan."
            )
        return self._vllm_semaphore

    def stats(self) -> dict:
        """Return current concurrency stats for monitoring / health checks."""
        return {
            "job_limiter_locked": self._job_limiter.locked() if self._job_limiter else None,
            "job_limit": settings.translate.max_concurrent_jobs,
            "chunk_semaphore_locked": self._chunk_semaphore.locked() if self._chunk_semaphore else None,
            "chunk_parallelism": settings.translate.chunk_parallelism,
            "vllm_semaphore_locked": self._vllm_semaphore.locked() if self._vllm_semaphore else None,
            "vllm_limit": settings.common.llm.max_batch_size,
        }


# Module-level singleton used by app.py and routers.
# Semaphores are uninitialised until lifespan calls concurrency_manager.initialize().
concurrency_manager = ConcurrencyManager()

# Made with Bob
