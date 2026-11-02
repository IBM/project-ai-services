"""Triton helpers that the skeleton needs: a readiness probe for /health."""
from typing import Optional

import requests

from forecast.settings import settings

# The probe must never hold up /health, so it has its own short timeout.
HEALTH_TIMEOUT_S = 2


def triton_url_problem() -> Optional[str]:
    """Return what is wrong with TRITON_URL, or None when it can be used.

    The value is not echoed, because a URL can carry credentials.
    """
    url = settings.forecast.triton_url.strip()
    if not url:
        return "TRITON_URL is not set"
    if not url.lower().startswith(("http://", "https://")):
        return "TRITON_URL must start with http:// or https://"
    return None


def is_triton_ready() -> bool:
    """Return True when Triton answers GET /v2/health/ready with 200."""
    url = settings.forecast.triton_url.rstrip("/")
    if not url:
        return False
    try:
        response = requests.get(f"{url}/v2/health/ready", timeout=HEALTH_TIMEOUT_S)
    except requests.RequestException:
        return False
    return response.status_code == 200
