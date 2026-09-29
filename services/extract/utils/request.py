"""HTTP request helpers for the extract service."""

from fastapi import Request
from common.error_utils import APIError, ErrorCode
from common.misc_utils import get_logger

from extract.settings import settings

logger = get_logger("request_utils")


async def check_request_body_size(request: Request) -> bytes:
    """Read the request body and enforce the configured size limit.

    Checks the ``Content-Length`` header first (fast path), then re-checks
    the actual body length after reading to guard against missing or
    incorrect headers.

    Returns:
        The raw request body bytes.

    Raises:
        HTTPException(413) if either the header or the actual body size
            exceeds ``settings.extract.max_request_body_bytes``.
        HTTPException(400) if the body cannot be read.
    """
    limit = settings.extract.max_request_body_bytes
    content_length = request.headers.get("content-length")
    if content_length is not None and int(content_length) > limit:
        msg = f"Request body exceeds the maximum allowed size of {limit} bytes."
        logger.error(msg)
        APIError.raise_error(ErrorCode.REQUEST_TOO_LARGE, msg, details={"max_request_body_bytes": limit})

    try:
        raw_body = await request.body()
    except Exception as exc:
        msg = "Failed to read request body."
        logger.error(f"{msg}: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INVALID_REQUEST, msg)

    if len(raw_body) > limit:
        msg = f"Request body exceeds the maximum allowed size of {limit} bytes."
        logger.error(msg)
        APIError.raise_error(ErrorCode.REQUEST_TOO_LARGE, msg, details={"max_request_body_bytes": limit})

    return raw_body
