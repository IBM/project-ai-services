"""
Standardized error responses for the digitize API.
"""
from enum import Enum
from typing import Optional, Dict, Any
from fastapi import HTTPException
from pydantic import BaseModel, Field


class ErrorCode(str, Enum):
    """Standard error codes for the API."""
    RESOURCE_NOT_FOUND = "RESOURCE_NOT_FOUND"
    RESOURCE_LOCKED = "RESOURCE_LOCKED"
    INTERNAL_SERVER_ERROR = "INTERNAL_SERVER_ERROR"
    INVALID_REQUEST = "INVALID_REQUEST"
    UNSUPPORTED_MEDIA_TYPE = "UNSUPPORTED_MEDIA_TYPE"
    RATE_LIMIT_EXCEEDED = "RATE_LIMIT_EXCEEDED"


class ErrorDetail(BaseModel):
    """Error detail model for API responses."""
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
                    "message": "Request validation failed: No files provided",
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
                    "message": "The requested resource was not found: No job found with id 'abc123'",
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


class UnsupportedMediaTypeErrorResponse(BaseModel):
    """415 Unsupported Media Type error response."""
    error: ErrorDetail

    model_config = {
        "json_schema_extra": {
            "example": {
                "error": {
                    "code": "UNSUPPORTED_MEDIA_TYPE",
                    "message": "File format not supported: Only PDF files are allowed",
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
                    "message": "Too many requests: Too many concurrent digitization requests",
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


# Common error responses dictionary for use in endpoint decorators
common_error_responses: Dict[int | str, Dict[str, Any]] = {
    400: {"description": "Bad Request - Invalid input or validation error", "model": BadRequestErrorResponse},
    404: {"description": "Not Found - Resource does not exist", "model": NotFoundErrorResponse},
    409: {"description": "Conflict - Resource is locked or in use", "model": ConflictErrorResponse},
    415: {"description": "Unsupported Media Type - Invalid file format", "model": UnsupportedMediaTypeErrorResponse},
    429: {"description": "Too Many Requests - Rate limit exceeded", "model": RateLimitErrorResponse},
    500: {"description": "Internal Server Error - Unexpected error occurred", "model": InternalServerErrorResponse},
}


class APIError:
    """Standardized API error definitions."""

    RESOURCE_NOT_FOUND = {
        "status": 404,
        "message": "The requested resource was not found"
    }

    RESOURCE_LOCKED = {
        "status": 409,
        "message": "Resource is locked by an active operation"
    }

    INTERNAL_SERVER_ERROR = {
        "status": 500,
        "message": "An unexpected error occurred"
    }

    INVALID_REQUEST = {
        "status": 400,
        "message": "Request validation failed"
    }

    UNSUPPORTED_MEDIA_TYPE = {
        "status": 415,
        "message": "File format not supported"
    }

    RATE_LIMIT_EXCEEDED = {
        "status": 429,
        "message": "Too many requests"
    }

    INSUFFICIENT_STORAGE = {
        "status": 507,
        "message": "Insufficient storage space"
    }

    @staticmethod
    def raise_error(error_type: str, detail: Optional[str] = None):
        """
        Raise a standardized HTTPException.

        Args:
            error_type: One of the error types defined in APIError
            detail: Optional additional detail to append to the standard message
        """
        error_def = getattr(APIError, error_type, APIError.INTERNAL_SERVER_ERROR)
        message = error_def["message"]
        if detail:
            message = f"{message}: {detail}"

        raise HTTPException(
            status_code=error_def["status"],
            detail=message
        )
