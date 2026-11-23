"""
Error handling for the Forecast service.

Every error response uses the envelope shared by the other services:
``{"error": {"code": ..., "message": ..., "status": ...}}``.
"""
from typing import Dict, NoReturn, Optional

from fastapi import FastAPI, HTTPException, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.exceptions import HTTPException as StarletteHTTPException

from common.error_utils import ErrorCode, http_exception_handler

# The shared ErrorCode enum has no code for Triton or for a generic 503, so the
# codes live here.
TRITON_UNAVAILABLE = "TRITON_UNAVAILABLE"
SERVICE_UNAVAILABLE = "SERVICE_UNAVAILABLE"


# Exit codes of the training worker (``python -m forecast.training <job_id> <data_path> <output_dir>``),
# which the loader subprocess is meant to follow too. The API retries the attempt after EXIT_FAILURE,
# a kill or a timeout, and does not retry EXIT_PERMANENT. 2 is left out because argparse uses it for
# a bad command line.
EXIT_OK = 0
EXIT_FAILURE = 1
EXIT_PERMANENT = 3


class TrainingFailure(Exception):
    """A training failure whose message is written for the user and stored in ``jobs.message``.

    The message must name no secret and no data value. Any other exception of the worker is
    stored as a fixed text, because its message can carry hosts, user names or rows.
    """


class PermanentFailure(TrainingFailure):
    """A failure that a retry would only repeat, such as a missing table or a table too small."""


class TransientFailure(TrainingFailure):
    """A failure that a retry may fix, such as a dropped connection to the source database."""


def raise_triton_unavailable(detail: Optional[str] = None) -> NoReturn:
    """Raise the 503 error used when Triton cannot be reached."""
    message = "Triton cannot be reached"
    if detail:
        message = f"{message}: {detail}"
    raise HTTPException(
        status_code=503,
        detail={"error": {"code": TRITON_UNAVAILABLE, "message": message, "status": 503}},
    )


# Code for an HTTPException whose detail is a plain string. The shared handler maps
# 503 to LLM_UNAVAILABLE, which does not fit this service, so the table lives here.
# 409 and 429 use the codes of the connector API (RESOURCE_LOCKED, SERVER_BUSY).
# Routes that need a specific code pass a structured detail.
_STATUS_TO_CODE = {
    400: ErrorCode.INVALID_REQUEST.value,
    401: ErrorCode.AUTHENTICATION_FAILED.value,
    403: ErrorCode.FORBIDDEN.value,
    404: ErrorCode.RESOURCE_NOT_FOUND.value,
    405: ErrorCode.METHOD_NOT_ALLOWED.value,
    409: ErrorCode.RESOURCE_LOCKED.value,
    413: ErrorCode.REQUEST_TOO_LARGE.value,
    415: ErrorCode.UNSUPPORTED_MEDIA_TYPE.value,
    422: ErrorCode.INVALID_REQUEST.value,
    429: ErrorCode.SERVER_BUSY.value,
    503: SERVICE_UNAVAILABLE,
    507: ErrorCode.INSUFFICIENT_STORAGE.value,
}


def _envelope(
    code: str, message: str, status_code: int, headers: Optional[Dict[str, str]] = None
) -> JSONResponse:
    """Build a response in the shared error envelope."""
    return JSONResponse(
        status_code=status_code,
        content={"error": {"code": code, "message": message, "status": status_code}},
        headers=headers,
    )


async def http_error_handler(request: Request, exc: StarletteHTTPException):
    """Put every HTTP error, raised by Starlette or by a route, in the shared envelope.

    Starlette's HTTPException is the base class of FastAPI's, so one handler
    covers both. The headers of the exception, such as Allow on a 405, are kept.
    """
    if isinstance(exc.detail, dict) and "error" in exc.detail:
        return JSONResponse(status_code=exc.status_code, content=exc.detail, headers=exc.headers)
    code = _STATUS_TO_CODE.get(exc.status_code, ErrorCode.INTERNAL_SERVER_ERROR.value)
    return _envelope(code, str(exc.detail), exc.status_code, exc.headers)


async def validation_exception_handler(request: Request, exc: RequestValidationError):
    """Return a 422 in the shared envelope with code INVALID_REQUEST."""
    problems = "; ".join(
        f"{'.'.join(str(part) for part in error['loc'])}: {error['msg']}"
        for error in exc.errors()
    )
    return _envelope(
        ErrorCode.INVALID_REQUEST.value, f"Request validation failed: {problems}", 422
    )


def register_exception_handlers(app: FastAPI) -> None:
    """Register the handlers that put every error response in the shared envelope."""
    app.add_exception_handler(StarletteHTTPException, http_error_handler)
    app.add_exception_handler(RequestValidationError, validation_exception_handler)
    app.add_exception_handler(Exception, http_exception_handler)
