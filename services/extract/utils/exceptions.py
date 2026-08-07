"""
Extract service exception.

Provides a single exception class for all API-level errors in the extract
service.

Usage::

    raise ExtractException(404, "RESOURCE_NOT_FOUND", "Job 'x' not found.")

The companion exception handler in ``extract.app`` converts this to the
standard ``{"error": {"code": ..., "message": ..., "status": ...}}`` shape.
"""


class ExtractException(Exception):
    """Raised to signal an API-level error in the extract service.

    Args:
        status_code: HTTP status code to return (e.g. 404, 429).
        code:        Machine-readable string error code (e.g. ``"RESOURCE_NOT_FOUND"``).
        message:     Human-readable description of the error.
    """

    def __init__(self, status_code: int, code: str, message: str) -> None:
        self.status_code = status_code
        self.code = code
        self.message = message
        super().__init__(message)
