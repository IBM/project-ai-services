"""
Base interface for database load backends.
"""

from abc import ABC, abstractmethod
from typing import Any


class InvoiceDBLoader(ABC):
    """Abstract interface for loading invoice data into a target database."""

    @abstractmethod
    def load(
        self,
        job_id: str,
        header: dict[str, Any],
        lines: list[dict[str, Any]],
    ) -> str:
        """
        Insert one invoice header + N lines into the target system.
        Returns a string reference ID assigned by the target system.
        Raises InvoiceDBLoadError on any failure.
        """
        pass

    def is_configured(self) -> bool:
        """Return True if this backend has sufficient config to attempt a load."""
        return True
