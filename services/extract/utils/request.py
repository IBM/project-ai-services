"""HTTP request helpers for the extract service."""

from fastapi import Request

from extract.settings import settings
from extract.utils.exceptions import ExtractException


async def check_request_body_size(request: Request) -> bytes:
    """Read the request body and enforce the configured size limit.

    Checks the ``Content-Length`` header first (fast path), then re-checks
    the actual body length after reading to guard against missing or
    incorrect headers.

    Returns:
        The raw request body bytes.

    Raises:
        ExtractException(413) if either the header or the actual body size
            exceeds ``settings.extract.max_request_body_bytes``.
        ExtractException(400) if the body cannot be read.
    """
    limit = settings.extract.max_request_body_bytes
    content_length = request.headers.get("content-length")
    if content_length is not None and int(content_length) > limit:
        raise ExtractException(
            413, "REQUEST_TOO_LARGE",
            f"Request body exceeds the maximum allowed size of {limit} bytes.",
            details={"max_request_body_bytes": limit},
        )

    try:
        raw_body = await request.body()
    except Exception:
        raise ExtractException(400, "INVALID_REQUEST", "Failed to read request body.")

    if len(raw_body) > limit:
        raise ExtractException(
            413, "REQUEST_TOO_LARGE",
            f"Request body exceeds the maximum allowed size of {limit} bytes.",
            details={"max_request_body_bytes": limit},
        )

    return raw_body
