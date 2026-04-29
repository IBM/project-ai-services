"""
Shared error handling utilities for all AI services.

Provides standardized error codes, response models, and exception handling
to ensure consistent error responses across digitize, summarize, and chatbot APIs.
"""
from enum import Enum
from typing import Optional, Dict, Any, NoReturn
from fastapi import HTTPException, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field


class ErrorCode(str, Enum):
    """Standard error codes used across all services."""
    # Client errors (4xx)
    INVALID_REQUEST = "INVALID_REQUEST"
    MISSING_INPUT = "MISSING_INPUT"
    EMPTY_INPUT = "EMPTY_INPUT"
    INVALID_PARAMETER = "INVALID_PARAMETER"
    RESOURCE_NOT_FOUND = "RESOURCE_NOT_FOUND"
    RESOURCE_LOCKED = "RESOURCE_LOCKED"
    UNSUPPORTED_MEDIA_TYPE = "UNSUPPORTED_MEDIA_TYPE"
    UNSUPPORTED_FILE_TYPE = "UNSUPPORTED_FILE_TYPE"
    UNSUPPORTED_CONTENT_TYPE = "UNSUPPORTED_CONTENT_TYPE"
    CONTEXT_LIMIT_EXCEEDED = "CONTEXT_LIMIT_EXCEEDED"
    INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH = "INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH"
    RATE_LIMIT_EXCEEDED = "RATE_LIMIT_EXCEEDED"
    SERVER_BUSY = "SERVER_BUSY"

    # Server errors (5xx)
    INTERNAL_SERVER_ERROR = "INTERNAL_SERVER_ERROR"
    LLM_ERROR = "LLM_ERROR"
    VECTOR_STORE_NOT_READY = "VECTOR_STORE_NOT_READY"
    INSUFFICIENT_STORAGE = "INSUFFICIENT_STORAGE"


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
    """413 Payload Too Large error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "CONTEXT_LIMIT_EXCEEDED",
                    "message": "Input size exceeds maximum token limit",
                    "status": 413
                }
            }
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


class RateLimitErrorResponse(BaseModel):
    """429 Too Many Requests error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "RATE_LIMIT_EXCEEDED",
                    "message": "Too many requests",
                    "status": 429
                }
            }
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
    404: {"description": "Not Found - Resource does not exist", "model": NotFoundErrorResponse},
    409: {"description": "Conflict - Resource is locked or in use", "model": ConflictErrorResponse},
    413: {"description": "Payload Too Large - Input exceeds size limits", "model": PayloadTooLargeErrorResponse},
    415: {"description": "Unsupported Media Type - Invalid file format", "model": UnsupportedMediaTypeErrorResponse},
    429: {"description": "Too Many Requests - Rate limit exceeded", "model": RateLimitErrorResponse},
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
        ErrorCode.RESOURCE_NOT_FOUND: (404, "The requested resource was not found"),
        ErrorCode.RESOURCE_LOCKED: (409, "Resource is locked by an active operation"),
        ErrorCode.UNSUPPORTED_MEDIA_TYPE: (415, "File format not supported"),
        ErrorCode.UNSUPPORTED_FILE_TYPE: (415, "File type not supported"),
        ErrorCode.UNSUPPORTED_CONTENT_TYPE: (415, "Content-Type not supported"),
        ErrorCode.CONTEXT_LIMIT_EXCEEDED: (413, "Input size exceeds maximum token limit"),
        ErrorCode.INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH: (400, "Input text is smaller than summary length"),
        ErrorCode.RATE_LIMIT_EXCEEDED: (429, "Too many requests"),
        ErrorCode.SERVER_BUSY: (429, "Server is busy. Please try again later"),
        ErrorCode.INTERNAL_SERVER_ERROR: (500, "An unexpected error occurred"),
        ErrorCode.LLM_ERROR: (500, "Failed to generate response. Please try again later"),
        ErrorCode.VECTOR_STORE_NOT_READY: (503, "Vector store not initialized"),
        ErrorCode.INSUFFICIENT_STORAGE: (507, "Insufficient storage space"),
    }

    @staticmethod
    def raise_error(error_code: ErrorCode | str, detail: Optional[str] = None) -> NoReturn:
        """
        Raise a standardized HTTPException with structured error format.

        Args:
            error_code: ErrorCode enum or string matching an error code
            detail: Optional additional detail to append to the standard message

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
        status_code, default_message = APIError.ERROR_DEFINITIONS.get(
            error_code,
            (500, "An unexpected error occurred")
        )

        # Build message
        message = default_message
        if detail:
            message = f"{message}: {detail}"

        # Raise HTTPException with structured error
        raise HTTPException(
            status_code=status_code,
            detail={
                "error": {
                    "code": error_code.value,
                    "message": message,
                    "status": status_code
                }
            }
        )


async def http_exception_handler(request: Request, exc: Exception) -> JSONResponse:
    """
    Custom exception handler to format HTTPException responses consistently.

    Transforms FastAPI's default {detail: "..."} to structured format:
    {error: {code, message, status}}

    Usage in FastAPI app:
        app.add_exception_handler(HTTPException, http_exception_handler)
    """
    if isinstance(exc, HTTPException):
        # If detail is already structured (dict), use it as-is
        if isinstance(exc.detail, dict) and "error" in exc.detail:
            return JSONResponse(
                status_code=exc.status_code,
                content=exc.detail
            )

        # Otherwise, transform to structured format
        error_message = str(exc.detail)

        # Map status codes to error codes
        status_to_code = {
            400: ErrorCode.INVALID_REQUEST,
            404: ErrorCode.RESOURCE_NOT_FOUND,
            409: ErrorCode.RESOURCE_LOCKED,
            413: ErrorCode.CONTEXT_LIMIT_EXCEEDED,
            415: ErrorCode.UNSUPPORTED_MEDIA_TYPE,
            429: ErrorCode.RATE_LIMIT_EXCEEDED,
            500: ErrorCode.INTERNAL_SERVER_ERROR,
            503: ErrorCode.VECTOR_STORE_NOT_READY,
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

    return JSONResponse(
        status_code=500,
        content={
            "error": {
                "code": ErrorCode.INTERNAL_SERVER_ERROR.value,
                "message": str(exc),
                "status": 500
            }
        }
    )
