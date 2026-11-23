"""The shared HTTP client of the service, used for the requests to Triton."""

from typing import Optional

import httpx

# One client for the whole service, so that requests reuse the connections to Triton. It is created
# on first use, inside the running event loop, and closed at shutdown.
_client: Optional[httpx.AsyncClient] = None


def get_client() -> httpx.AsyncClient:
    """Return the shared client, creating it on first use."""
    global _client
    if _client is None:
        _client = httpx.AsyncClient()
    return _client


async def close_client() -> None:
    """Close the shared client. The next call of ``get_client`` creates a new one."""
    global _client
    client, _client = _client, None
    if client is not None:
        await client.aclose()
