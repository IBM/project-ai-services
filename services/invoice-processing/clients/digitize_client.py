"""
Client stub for Digitize service.
"""

from typing import Any


class DigitizeClient:
    """Client for interacting with the Digitize service."""

    def __init__(self, base_url: str | None = None):
        self.base_url = base_url

    async def submit_document(self, *args: Any, **kwargs: Any) -> Any:
        raise NotImplementedError("DigitizeClient is not yet implemented")

    async def poll_job(self, *args: Any, **kwargs: Any) -> Any:
        raise NotImplementedError("DigitizeClient is not yet implemented")
