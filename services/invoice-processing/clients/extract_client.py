"""
Client stub for Extract service.
"""

from typing import Any


class ExtractClient:
    """Client for interacting with the Extract service."""

    def __init__(self, base_url: str | None = None):
        self.base_url = base_url

    async def register_schema(self, *args: Any, **kwargs: Any) -> Any:
        raise NotImplementedError("ExtractClient is not yet implemented")

    async def submit_extraction(self, *args: Any, **kwargs: Any) -> Any:
        raise NotImplementedError("ExtractClient is not yet implemented")

    async def poll_job(self, *args: Any, **kwargs: Any) -> Any:
        raise NotImplementedError("ExtractClient is not yet implemented")
