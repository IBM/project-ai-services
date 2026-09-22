"""
Shared error handling utilities for all AI services.

Provides standardized error codes, response models, and exception handling
to ensure consistent error responses across digitize, summarize, and chatbot APIs.
"""
import logging
from enum import Enum
from typing import Optional, Dict, Any, NoReturn
from fastapi import HTTPException, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

logger = logging.getLogger(__name__)


class ErrorCode(str, Enum):
    """Standard error codes used across all services."""
    # Client errors (4xx)
    INVALID_REQUEST = "INVALID_REQUEST"
    MISSING_INPUT = "MISSING_INPUT"
    EMPTY_INPUT = "EMPTY_INPUT"
    INVALID_PARAMETER = "INVALID_PARAMETER"
    AUTHENTICATION_FAILED = "AUTHENTICATION_FAILED"
    RESOURCE_NOT_FOUND = "RESOURCE_NOT_FOUND"
    METHOD_NOT_ALLOWED = "METHOD_NOT_ALLOWED"
    RESOURCE_LOCKED = "RESOURCE_LOCKED"
    RESOURCE_CONFLICT = "RESOURCE_CONFLICT"
    UNSUPPORTED_MEDIA_TYPE = "UNSUPPORTED_MEDIA_TYPE"
    UNSUPPORTED_FILE_TYPE = "UNSUPPORTED_FILE_TYPE"
    UNSUPPORTED_CONTENT_TYPE = "UNSUPPORTED_CONTENT_TYPE"
    INVALID_FILE_CONTENT = "INVALID_FILE_CONTENT"
    INVALID_SCHEMA = "INVALID_SCHEMA"
    DUPLICATE_FILE = "DUPLICATE_FILE"
    REQUEST_TOO_LARGE = "REQUEST_TOO_LARGE"
    CONTEXT_LIMIT_EXCEEDED = "CONTEXT_LIMIT_EXCEEDED"
    INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH = "INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH"
    RATE_LIMIT_EXCEEDED = "RATE_LIMIT_EXCEEDED"
    SERVER_BUSY = "SERVER_BUSY"
    FORBIDDEN = "FORBIDDEN"
    JOB_FAILED = "JOB_FAILED"

    # Server errors (5xx)
    INTERNAL_SERVER_ERROR = "INTERNAL_SERVER_ERROR"
    LLM_ERROR = "LLM_ERROR"
    LLM_UNAVAILABLE = "LLM_UNAVAILABLE"
    VECTOR_STORE_NOT_READY = "VECTOR_STORE_NOT_READY"
    INSUFFICIENT_STORAGE = "INSUFFICIENT_STORAGE"
    EXTRACTION_VALIDATION_FAILED = "EXTRACTION_VALIDATION_FAILED"
    FILE_STAGING_ERROR = "FILE_STAGING_ERROR"
    DATABASE_ERROR = "DATABASE_ERROR"


class ErrorDetail(BaseModel):
    """Error detail model for structured error responses."""
    code: str = Field(..., description="Machine-readable error code")
    message: str = Field(..., description="Human-readable error message")
    status: int = Field(..., description="HTTP status code")


class ErrorResponse(BaseModel):
    """Standard error response wrapper."""
    error: ErrorDetail


class BadRequestErrorResponse(BaseModel):
    """400 Bad Request error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "INVALID_REQUEST",
                    "message": "Request validation failed",
                    "status": 400
                }
            }
        }
    }


class UnauthorizedErrorResponse(BaseModel):
    """401 Unauthorized error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "AUTHENTICATION_FAILED",
                    "message": "Authentication failed",
                    "status": 401
                }
            }
        }
    }


class NotFoundErrorResponse(BaseModel):
    """404 Not Found error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "RESOURCE_NOT_FOUND",
                    "message": "The requested resource was not found",
                    "status": 404
                }
            }
        }
    }


class MethodNotAllowedErrorResponse(BaseModel):
    """405 Method Not Allowed error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "METHOD_NOT_ALLOWED",
                    "message": "This operation is not allowed for this resource",
                    "status": 405
                }
            }
        }
    }


class ConflictErrorResponse(BaseModel):
    """409 Conflict error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "RESOURCE_LOCKED",
                    "message": "Resource is locked by an active operation",
                    "status": 409
                }
            }
        }
    }


class PayloadTooLargeErrorResponse(BaseModel):
    """413 Payload Too Large error response.

    The ``code`` field will be one of:

    * ``CONTEXT_LIMIT_EXCEEDED`` – input text is too large for the model context window.
    * ``REQUEST_TOO_LARGE`` – the raw request body or file count exceeds the configured limit.
    """
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "examples": [
                {
                    "summary": "Input exceeds model context window",
                    "value": {
                        "error": {
                            "code": "CONTEXT_LIMIT_EXCEEDED",
                            "message": "Input size exceeds maximum token limit: "
                                       "Input does not fit in the model context window.",
                            "status": 413,
                        }
                    },
                },
                {
                    "summary": "Request body or file count too large",
                    "value": {
                        "error": {
                            "code": "REQUEST_TOO_LARGE",
                            "message": "Request body exceeds the maximum allowed size",
                            "status": 413,
                        }
                    },
                },
            ]
        }
    }


class UnsupportedMediaTypeErrorResponse(BaseModel):
    """415 Unsupported Media Type error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "UNSUPPORTED_MEDIA_TYPE",
                    "message": "File format not supported",
                    "status": 415
                }
            }
        }
    }


class UnprocessableEntityErrorResponse(BaseModel):
    """422 Unprocessable Entity error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "examples": [
                {
                    "summary": "Model output failed schema validation",
                    "value": {
                        "error": {
                            "code": "EXTRACTION_VALIDATION_FAILED",
                            "message": "Model output failed schema validation",
                            "status": 422,
                        }
                    },
                },
                {
                    "summary": "Job completed with a failure status",
                    "value": {
                        "error": {
                            "code": "JOB_FAILED",
                            "message": "The job completed with a failure status",
                            "status": 422,
                        }
                    },
                },
            ]
        }
    }


class RateLimitErrorResponse(BaseModel):
    """429 Too Many Requests error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "examples": [
                {
                    "summary": "Per-client rate limit exceeded",
                    "value": {
                        "error": {
                            "code": "RATE_LIMIT_EXCEEDED",
                            "message": "Too many requests",
                            "status": 429,
                        }
                    },
                },
                {
                    "summary": "Server at maximum concurrency",
                    "value": {
                        "error": {
                            "code": "SERVER_BUSY",
                            "message": "Server is busy. Please try again later",
                            "status": 429,
                        }
                    },
                },
            ]
        }
    }


class InternalServerErrorResponse(BaseModel):
    """500 Internal Server Error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "INTERNAL_SERVER_ERROR",
                    "message": "An unexpected error occurred",
                    "status": 500
                }
            }
        }
    }


class ServiceUnavailableErrorResponse(BaseModel):
    """503 Service Unavailable error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "VECTOR_STORE_NOT_READY",
                    "message": "Vector store not initialized",
                    "status": 503
                }
            }
        }
    }


# HTTP error responses dictionary for use in endpoint decorators
http_error_responses: Dict[int | str, Dict[str, Any]] = {
    400: {"description": "Bad Request - Invalid input or validation error", "model": BadRequestErrorResponse},
    401: {"description": "Unauthorized - Authentication failed", "model": UnauthorizedErrorResponse},
    404: {"description": "Not Found - Resource does not exist", "model": NotFoundErrorResponse},
    405: {"description": "Method Not Allowed - Operation not permitted for this resource", "model": MethodNotAllowedErrorResponse},
    409: {"description": "Conflict - Resource is locked or a duplicate exists", "model": ConflictErrorResponse},
    413: {"description": "Payload Too Large - Input exceeds size limits", "model": PayloadTooLargeErrorResponse},
    415: {"description": "Unsupported Media Type - Invalid file format or content", "model": UnsupportedMediaTypeErrorResponse},
    422: {"description": "Unprocessable Entity - Validation or job failure", "model": UnprocessableEntityErrorResponse},
    429: {"description": "Too Many Requests - Rate limit exceeded or server busy", "model": RateLimitErrorResponse},
    500: {"description": "Internal Server Error - Unexpected error occurred", "model": InternalServerErrorResponse},
    503: {"description": "Service Unavailable - Service not ready", "model": ServiceUnavailableErrorResponse},
}


class APIError:
    """
    Standardized API error definitions and helper methods.

    Usage:
        APIError.raise_error(ErrorCode.INVALID_REQUEST, "No files provided")
    """

    # Error definitions with status codes and default messages
    ERROR_DEFINITIONS = {
        ErrorCode.INVALID_REQUEST: (400, "Request validation failed"),
        ErrorCode.MISSING_INPUT: (400, "Required input is missing"),
        ErrorCode.EMPTY_INPUT: (400, "Input cannot be empty"),
        ErrorCode.INVALID_PARAMETER: (400, "Invalid parameter value"),
        ErrorCode.AUTHENTICATION_FAILED: (401, "Authentication failed"),
        ErrorCode.FORBIDDEN: (403, "Access to this resource is forbidden"),
        ErrorCode.RESOURCE_NOT_FOUND: (404, "The requested resource was not found"),
        ErrorCode.METHOD_NOT_ALLOWED: (405, "This operation is not allowed for this resource"),
        ErrorCode.RESOURCE_LOCKED: (409, "Resource is locked by an active operation"),
        ErrorCode.RESOURCE_CONFLICT: (409, "A resource with that identifier already exists"),
        ErrorCode.UNSUPPORTED_MEDIA_TYPE: (415, "File format not supported"),
        ErrorCode.UNSUPPORTED_FILE_TYPE: (415, "File type not supported"),
        ErrorCode.UNSUPPORTED_CONTENT_TYPE: (415, "Content-Type not supported"),
        ErrorCode.INVALID_FILE_CONTENT: (415, "File content is invalid"),
        ErrorCode.INVALID_SCHEMA: (400, "Schema is invalid"),
        ErrorCode.DUPLICATE_FILE: (400, "Duplicate filename detected"),
        ErrorCode.REQUEST_TOO_LARGE: (413, "Request body exceeds the maximum allowed size"),
        ErrorCode.CONTEXT_LIMIT_EXCEEDED: (413, "Input size exceeds maximum token limit"),
        ErrorCode.INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH: (400, "Input text is smaller than summary length"),
        ErrorCode.RATE_LIMIT_EXCEEDED: (429, "Too many requests"),
        ErrorCode.SERVER_BUSY: (429, "Server is busy. Please try again later"),
        ErrorCode.JOB_FAILED: (422, "The job completed with a failure status"),
        ErrorCode.INTERNAL_SERVER_ERROR: (500, "An unexpected error occurred"),
        ErrorCode.LLM_ERROR: (500, "Failed to generate response. Please try again later"),
        ErrorCode.LLM_UNAVAILABLE: (503, "LLM service is unavailable. Please try again later"),
        ErrorCode.VECTOR_STORE_NOT_READY: (503, "Vector store not initialized"),
        ErrorCode.INSUFFICIENT_STORAGE: (507, "Insufficient storage space"),
        ErrorCode.EXTRACTION_VALIDATION_FAILED: (422, "Model output failed schema validation"),
        ErrorCode.FILE_STAGING_ERROR: (500, "Failed to save uploaded file"),
        ErrorCode.DATABASE_ERROR: (500, "A database error occurred"),
    }

    @staticmethod
    def raise_error(
        error_code: ErrorCode | str,
        detail: Optional[str] = None,
        details: Optional[Any] = None,
        status_code: Optional[int] = None,
    ) -> NoReturn:
        """
        Raise a standardized HTTPException with structured error format.

        Args:
            error_code: ErrorCode enum or string matching an error code
            detail: Optional additional detail to append to the standard message
            details: Optional extra payload to include as ``error.details`` in
                     the response body.  May be a list (e.g. per-file validation
                     failures) or a dict (e.g. ``{"referencing_job_ids": [...]}``)
            status_code: Optional HTTP status code override.  When supplied it
                         replaces the default code derived from ``error_code``.
                         Use this to propagate an upstream status code rather
                         than forcing the canonical one (e.g. a 4xx from an
                         upstream service should not become a 503).

        Raises:
            HTTPException with structured error response
        """
        # Convert string to ErrorCode if needed
        if isinstance(error_code, str):
            try:
                error_code = ErrorCode(error_code)
            except ValueError:
                error_code = ErrorCode.INTERNAL_SERVER_ERROR

        # Get error definition
        default_status_code, default_message = APIError.ERROR_DEFINITIONS.get(
            error_code,
            (500, "An unexpected error occurred")
        )
        # Use the caller-supplied status code if provided, otherwise fall back
        # to the canonical one for this error code.
        if status_code is None:
            status_code = default_status_code

        # Build message
        message = default_message
        if detail:
            message = f"{message}: {detail}"

        # Build structured error body
        error_body: dict = {
            "code": error_code.value,
            "message": message,
            "status": status_code,
        }
        if details is not None:
            error_body["details"] = details

        # Raise HTTPException with structured error
        raise HTTPException(
            status_code=status_code,
            detail={"error": error_body},
        )


def extract_http_error_message(exc) -> str:
    """Extract the human-readable message from a FastAPI HTTPException.

    ``APIError.raise_error`` always stores the user-facing message at
    ``exc.detail["error"]["message"]``.  This helper unwraps that path and
    falls back to ``str(exc.detail)`` for any exception whose detail is not
    structured (e.g. plain-string FastAPI validation errors).

    Args:
        exc: A ``fastapi.HTTPException`` instance.

    Returns:
        The extracted message string.
    """
    if isinstance(exc.detail, dict):
        return exc.detail.get("error", {}).get("message", str(exc.detail))
    return str(exc.detail)


def build_http_error_detail(exc, operation_message: str) -> dict:
    """Build a structured error detail dict for re-raising an HTTPException.

    Combines a human-readable *operation_message* (describing what was being
    attempted and why it failed) with the error code extracted from the
    original exception so the HTTP status code, error code, and message are
    all consistent and displayable to the end user.

    Args:
        exc: The original ``fastapi.HTTPException`` being handled.
        operation_message: The fully-composed user-facing message, e.g.
            ``"Failed to create connector 'x': Connector already exists"``.

    Returns:
        A ``{"error": {"code": ..., "message": ..., "status": ...}}`` dict
        suitable for passing directly as ``HTTPException(detail=...)``.
    """
    code = (
        exc.detail.get("error", {}).get("code", "INTERNAL_SERVER_ERROR")
        if isinstance(exc.detail, dict)
        else "INTERNAL_SERVER_ERROR"
    )
    return {"error": {"code": code, "message": operation_message, "status": exc.status_code}}


async def http_exception_handler(request: Request, exc: Exception) -> JSONResponse:
    """
    Custom exception handler to format HTTPException responses consistently.

    Transforms FastAPI's default {detail: "..."} to structured format:
    {error: {code, message, status}}

    Usage in FastAPI app:
        app.add_exception_handler(HTTPException, http_exception_handler)
    """
    if isinstance(exc, HTTPException):
        # If detail is already structured (dict with "error" key), pass it
        # through as-is so that APIError.raise_error payloads — including any
        # "details" field — are preserved exactly.
        if isinstance(exc.detail, dict) and "error" in exc.detail:
            return JSONResponse(
                status_code=exc.status_code,
                content=exc.detail
            )

        # detail is either a plain string or an unexpected dict shape — stringify it.
        error_message = str(exc.detail)

        # Map status codes to error codes.
        # 500/502/503/507 entries make the intent explicit even though the
        # fallthrough default would also produce INTERNAL_SERVER_ERROR.
        # 409 maps to RESOURCE_LOCKED — the most common 409 reason across all
        # services.  Callers that need RESOURCE_CONFLICT raise it explicitly
        # via APIError.raise_error (which takes the structured pass-through
        # path above), so this fallback only applies to unstructured 409s.
        status_to_code = {
            400: ErrorCode.INVALID_REQUEST,
            401: ErrorCode.AUTHENTICATION_FAILED,
            403: ErrorCode.FORBIDDEN,
            404: ErrorCode.RESOURCE_NOT_FOUND,
            409: ErrorCode.RESOURCE_LOCKED,
            413: ErrorCode.CONTEXT_LIMIT_EXCEEDED,
            415: ErrorCode.UNSUPPORTED_MEDIA_TYPE,
            422: ErrorCode.INVALID_REQUEST,
            429: ErrorCode.RATE_LIMIT_EXCEEDED,
            500: ErrorCode.INTERNAL_SERVER_ERROR,
            502: ErrorCode.INTERNAL_SERVER_ERROR,
            503: ErrorCode.LLM_UNAVAILABLE,
            507: ErrorCode.INSUFFICIENT_STORAGE,
        }

        error_code = status_to_code.get(exc.status_code, ErrorCode.INTERNAL_SERVER_ERROR)

        return JSONResponse(
            status_code=exc.status_code,
            content={
                "error": {
                    "code": error_code.value,
                    "message": error_message,
                    "status": exc.status_code
                }
            }
        )

    # Non-HTTPException: truly unexpected crash — log at ERROR with full traceback.
    logger.error(f"Unhandled exception occurred: {exc}", exc_info=True)
    return JSONResponse(
        status_code=500,
        content={
            "error": {
                "code": ErrorCode.INTERNAL_SERVER_ERROR.value,
                "message": "An unexpected internal server error occurred.",
                "status": 500
            }
        }
    )
