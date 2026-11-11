"""Triton helpers: the TRITON_URL check that runs at startup."""
from typing import Optional

from forecast.settings import settings


def triton_url_check() -> Optional[str]:
    """Return what is wrong with TRITON_URL, or None when it can be used.

    The value is not echoed, because a URL can carry credentials.
    """
    url = settings.forecast.triton_url.strip()
    if not url:
        return "TRITON_URL is not set"
    if not url.lower().startswith(("http://", "https://")):
        return "TRITON_URL must start with http:// or https://"
    return None
