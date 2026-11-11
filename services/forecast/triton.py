"""Triton helpers that the skeleton needs: a readiness probe for /health."""
from typing import Optional

import requests

from common.misc_utils import get_logger
from forecast.settings import settings

logger = get_logger("forecast_triton")

# The probe must never hold up /health, so it has its own short timeout.
HEALTH_TIMEOUT_S = 2


def triton_url_check() -> Optional[str]:
    """Return what is wrong with TRITON_URL, or None when it can be used.

    The value is not echoed, because a URL can carry credentials.
    """
    url = settings.forecast.triton_url.strip()
    if not url:
        return "TRITON_URL is not set"
    if not url.lower().startswith(("http://", "https://")):
        return "TRITON_URL must start with http:// or https://"
    try:
        response = requests.get(f"{url}/v2/health/ready", timeout=HEALTH_TIMEOUT_S)
    except requests.RequestException as exc:
        # Only the exception class is logged: the message can contain the URL.
        return f"Triton readiness probe failed: {type(exc).__name__}"
    return None
