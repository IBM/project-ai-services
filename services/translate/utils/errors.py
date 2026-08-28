"""
Translation-specific HTTP error helpers.

These error codes are not in the shared ``ErrorCode`` enum so they get their
own raise helpers here rather than being inlined in the router.
"""

from typing import NoReturn

from fastapi import HTTPException


def _raise_invalid_language(detail: str) -> NoReturn:
    raise HTTPException(
        status_code=400,
        detail={"error": {"code": "INVALID_LANGUAGE", "message": detail, "status": 400}},
    )


def _raise_same_language(detail: str) -> NoReturn:
    raise HTTPException(
        status_code=400,
        detail={"error": {"code": "SAME_LANGUAGE", "message": detail, "status": 400}},
    )


def _raise_unsupported_file_type(detail: str) -> NoReturn:
    raise HTTPException(
        status_code=415,
        detail={"error": {"code": "UNSUPPORTED_FILE_TYPE", "message": detail, "status": 415}},
    )


def _raise_file_too_large(detail: str) -> NoReturn:
    raise HTTPException(
        status_code=413,
        detail={"error": {"code": "FILE_TOO_LARGE", "message": detail, "status": 413}},
    )


def _raise_job_failed(detail: str) -> NoReturn:
    raise HTTPException(
        status_code=410,
        detail={"error": {"code": "JOB_FAILED", "message": detail, "status": 410}},
    )


def _raise_context_limit_exceeded(detail: str, diagnostics: dict) -> NoReturn:
    raise HTTPException(
        status_code=413,
        detail={
            "error": {
                "code": "CONTEXT_LIMIT_EXCEEDED",
                "message": detail,
                "status": 413,
                "diagnostics": diagnostics,
            }
        },
    )

# Made with Bob
