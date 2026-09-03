"""
Unit tests for translate/utils/errors.py — HTTP error helper functions.
"""

import pytest
from fastapi import HTTPException

from translate.utils.errors import (
    _raise_context_limit_exceeded,
    _raise_file_too_large,
    _raise_invalid_language,
    _raise_job_failed,
    _raise_same_language,
    _raise_unsupported_file_type,
)


@pytest.mark.unit
class TestRaiseInvalidLanguage:
    def test_raises_http_400(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_invalid_language("bad language")
        assert exc_info.value.status_code == 400

    def test_error_code_is_invalid_language(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_invalid_language("bad language")
        assert exc_info.value.detail["error"]["code"] == "INVALID_LANGUAGE"

    def test_message_is_propagated(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_invalid_language("unsupported: Klingon")
        assert "Klingon" in exc_info.value.detail["error"]["message"]


@pytest.mark.unit
class TestRaiseSameLanguage:
    def test_raises_http_400(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_same_language("both are 'english'")
        assert exc_info.value.status_code == 400

    def test_error_code_is_same_language(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_same_language("both are 'english'")
        assert exc_info.value.detail["error"]["code"] == "SAME_LANGUAGE"


@pytest.mark.unit
class TestRaiseUnsupportedFileType:
    def test_raises_http_415(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_unsupported_file_type("only .txt and .md")
        assert exc_info.value.status_code == 415

    def test_error_code(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_unsupported_file_type("only .txt and .md")
        assert exc_info.value.detail["error"]["code"] == "UNSUPPORTED_FILE_TYPE"


@pytest.mark.unit
class TestRaiseFileTooLarge:
    def test_raises_http_413(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_file_too_large("file exceeds 10 MB")
        assert exc_info.value.status_code == 413

    def test_error_code(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_file_too_large("file exceeds 10 MB")
        assert exc_info.value.detail["error"]["code"] == "FILE_TOO_LARGE"


@pytest.mark.unit
class TestRaiseJobFailed:
    def test_raises_http_410(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_job_failed("job failed: timeout")
        assert exc_info.value.status_code == 410

    def test_error_code(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_job_failed("job failed: timeout")
        assert exc_info.value.detail["error"]["code"] == "JOB_FAILED"


@pytest.mark.unit
class TestRaiseContextLimitExceeded:
    def test_raises_http_413(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_context_limit_exceeded(
                "Input too large",
                diagnostics={"input_tokens": 5000, "max_model_len": 4096},
            )
        assert exc_info.value.status_code == 413

    def test_error_code(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_context_limit_exceeded(
                "Input too large",
                diagnostics={"input_tokens": 5000},
            )
        assert exc_info.value.detail["error"]["code"] == "CONTEXT_LIMIT_EXCEEDED"

    def test_diagnostics_attached(self):
        diag = {"input_tokens": 5000, "max_model_len": 4096, "available_for_output": -904}
        with pytest.raises(HTTPException) as exc_info:
            _raise_context_limit_exceeded("Input too large", diagnostics=diag)
        assert exc_info.value.detail["error"]["diagnostics"] == diag

    def test_message_is_propagated(self):
        with pytest.raises(HTTPException) as exc_info:
            _raise_context_limit_exceeded("Custom error message", diagnostics={})
        assert exc_info.value.detail["error"]["message"] == "Custom error message"

# Made with Bob
