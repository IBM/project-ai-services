"""
Weighted semaphore for Docling conversion capacity management.

Each conversion task acquires a number of *weight units* from the semaphore
before starting and releases them when done.  The total capacity mirrors
``doc_worker_size`` (default 4) so the semaphore and the underlying
``ProcessPoolExecutor`` agree on the available parallelism budget.

Weight rule:
  - Normal file (page_count < heavy_doc_page_threshold):   weight = 1
  - Large file  (page_count >= heavy_doc_page_threshold):  weight = 2
"""

import asyncio

from digitize.settings import settings


class WeightedSemaphore:
    """Capacity-based semaphore; each acquire consumes ``weight`` units."""

    def __init__(self, capacity: int) -> None:
        self._capacity = capacity
        self._available = capacity
        self._cond = asyncio.Condition()

    @property
    def capacity(self) -> int:
        return self._capacity

    @property
    def available(self) -> int:
        return self._available

    async def acquire(self, weight: int) -> None:
        """Block until ``weight`` units are free, then consume them."""
        async with self._cond:
            await self._cond.wait_for(lambda: self._available >= weight)
            self._available -= weight

    async def release(self, weight: int) -> None:
        """Return ``weight`` units to the pool and wake any waiters."""
        async with self._cond:
            self._available += weight
            self._cond.notify_all()


# Module-level singleton — capacity mirrors doc_worker_size so existing
# ProcessPoolExecutor pools and this semaphore agree on the budget.
conversion_semaphore = WeightedSemaphore(
    capacity=settings.digitize.doc_worker_size  # default 4
)
