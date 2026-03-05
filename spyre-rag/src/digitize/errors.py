"""
Standardized error responses for the digitize API.
"""
from enum import Enum
from fastapi import HTTPException


class ErrorCode(str, Enum):
    """Standard error codes for the API."""
    RESOURCE_NOT_FOUND = "RESOURCE_NOT_FOUND"
    RESOURCE_LOCKED = "RESOURCE_LOCKED"
    INTERNAL_SERVER_ERROR = "INTERNAL_SERVER_ERROR"
    INVALID_REQUEST = "INVALID_REQUEST"
    UNSUPPORTED_MEDIA_TYPE = "UNSUPPORTED_MEDIA_TYPE"
    RATE_LIMIT_EXCEEDED = "RATE_LIMIT_EXCEEDED"


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
    def raise_error(error_type: str, detail: str = None):
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
