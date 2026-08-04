"""
Concurrency management using semaphores.

Provides rate limiting for ingestion and digitization operations,
consolidating all semaphore logic that previously lived as module-level
variables in app.py.
"""

import asyncio

from digitize.settings import settings


class ConcurrencyManager:
    """
    Manages concurrency limits for digitization and ingestion operations.

    Limits are driven directly from ``DigitizeConfig``:
    - Ingestion: ``settings.digitize.ingestion_concurrency_limit`` (default 1)
    - Digitization: ``settings.digitize.digitization_concurrency_limit`` (default 2)
    """

    def __init__(self) -> None:
        self._ingestion = asyncio.BoundedSemaphore(
            settings.digitize.ingestion_concurrency_limit
        )
        self._digitization = asyncio.BoundedSemaphore(
            settings.digitize.digitization_concurrency_limit
        )

    def get(self, operation: str) -> asyncio.BoundedSemaphore:
        """
        Return the semaphore for the given operation type.

        Args:
            operation: ``"ingestion"`` or ``"digitization"``

        Returns:
            The corresponding :class:`asyncio.BoundedSemaphore`.
        """
        if operation == "ingestion":
            return self._ingestion
        return self._digitization

    def is_locked(self, operation: str) -> bool:
        """
        Check whether the semaphore for *operation* is fully occupied.

        Args:
            operation: ``"ingestion"`` or ``"digitization"``

        Returns:
            ``True`` if no slots are available, ``False`` otherwise.
        """
        return self.get(operation).locked()

    async def acquire(self, operation: str) -> None:
        """
        Acquire a slot for the given operation.

        Args:
            operation: ``"ingestion"`` or ``"digitization"``
        """
        await self.get(operation).acquire()

    def release(self, operation: str) -> None:
        """
        Release a previously acquired slot.

        Args:
            operation: ``"ingestion"`` or ``"digitization"``
        """
        self.get(operation).release()

    def stats(self) -> dict:
        """
        Return current concurrency statistics for monitoring / health checks.

        Returns:
            Dictionary with ``ingestion_locked`` and ``digitization_locked`` booleans.
        """
        return {
            "ingestion_locked": self._ingestion.locked(),
            "digitization_locked": self._digitization.locked(),
            "ingestion_limit": settings.digitize.ingestion_concurrency_limit,
            "digitization_limit": settings.digitize.digitization_concurrency_limit,
        }


# Module-level singleton used by app.py and the job router.
concurrency_manager = ConcurrencyManager()
