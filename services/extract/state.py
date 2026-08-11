"""
Shared async semaphores for the Extract service.

Defining these here (rather than in app.py) lets both app.py and the
api/v1/jobs.py router import them without creating a circular dependency.
"""

import asyncio

from extract.settings import settings

# Global vLLM concurrency limiter (shared by sync + async extraction paths).
concurrency_limiter = asyncio.BoundedSemaphore(settings.common.llm.max_batch_size)

# Async job admission semaphore (caps background workers).
job_limiter = asyncio.BoundedSemaphore(settings.extract.max_concurrent_jobs)
