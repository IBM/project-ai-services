"""
Concurrency management for the translate service.
Consolidates all semaphore logic in one place

Three semaphores are managed:
- ``job_limiter``       — async job admission (default 8 slots)
- ``chunk_semaphore``   — per-job chunk parallelism cap (default 4 slots)
- ``vllm_semaphore``    — shared vLLM inference gate (default 32 slots)
"""

import asyncio

from translate.settings import settings


class ConcurrencyManager:
    """
    Manages concurrency limits for the translate service.

    Limits are driven from ``TranslationConfig``:
    - ``max_concurrent_jobs``    → job admission semaphore
    - ``chunk_parallelism``      → per-job chunk semaphore
    - ``common.llm.max_batch_size`` → shared vLLM semaphore
    """

    def __init__(self) -> None:
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
        return self._job_limiter

    @property
    def chunk_semaphore(self) -> asyncio.BoundedSemaphore:
        """Per-job chunk parallelism semaphore."""
        return self._chunk_semaphore

    @property
    def vllm_semaphore(self) -> asyncio.BoundedSemaphore:
        """Shared vLLM inference gate semaphore."""
        return self._vllm_semaphore

    def stats(self) -> dict:
        """Return current concurrency stats for monitoring / health checks."""
        return {
            "job_limiter_locked": self._job_limiter.locked(),
            "job_limit": settings.translate.max_concurrent_jobs,
            "chunk_semaphore_locked": self._chunk_semaphore.locked(),
            "chunk_parallelism": settings.translate.chunk_parallelism,
            "vllm_semaphore_locked": self._vllm_semaphore.locked(),
            "vllm_limit": settings.common.llm.max_batch_size,
        }


# Module-level singleton used by app.py and routers.
concurrency_manager = ConcurrencyManager()

# Made with Bob
