"""
Unit tests for the _process_extract_job background worker.

Every external boundary is mocked so no real DB, filesystem, or vLLM is
required.  Tests drive the function directly with asyncio.run (via pytest-asyncio
or the synchronous shim below) so the full coroutine executes.

Coverage matrix
───────────────
Step 1  – DB update to in_progress + job_row / schema_row resolution failures
Step 2  – Staged-file read (missing dir, UTF-8 decode error)
Step 3  – Tokenization failure, context-window budget breach
Step 4  – vLLM call failure, empty choices list
Step 5  – finish_reason=length retry (success + still length → OUTPUT_BUDGET_EXCEEDED)
Step 6  – validate_with_retry failure → EXTRACTION_VALIDATION_FAILED
Step 7  – Result-file write failure → RESULT_WRITE_ERROR
Step 8  – Happy path: status=completed, result file contents
Step 9  – Staging directory cleanup called in every terminal path
"""

import asyncio
import json
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, Mock, call, patch

import pytest

# ---------------------------------------------------------------------------
# Test helpers
# ---------------------------------------------------------------------------

def _run(coro):
    """Run a coroutine synchronously."""
    return asyncio.run(coro)


def _mock_job_row(
    job_id="job-001",
    schema_id="schema-001",
    document_name="invoice.txt",
    source_type="txt",
    status="accepted",
):
    row = Mock()
    row.job_id = job_id
    row.schema_id = schema_id
    row.document_name = document_name
    row.source_type = source_type
    row.status = status
    return row


def _mock_schema_row(
    schema_id="schema-001",
    json_schema=None,
    examples=None,
    custom_prompt=None,
    schema_tokens=80,
    examples_tokens=0,
    custom_prompt_tokens=0,
):
    row = Mock()
    row.schema_id = schema_id
    row.json_schema = json_schema or {
        "type": "object",
        "properties": {"invoice_number": {"type": "string"}},
        "required": ["invoice_number"],
    }
    row.examples = examples
    row.custom_prompt = custom_prompt
    row.schema_tokens = schema_tokens
    row.examples_tokens = examples_tokens
    row.custom_prompt_tokens = custom_prompt_tokens
    return row


def _vllm_response(
    content: str,
    finish_reason: str = "stop",
    prompt_tokens: int = 500,
    completion_tokens: int = 80,
) -> dict:
    return {
        "choices": [
            {"message": {"role": "assistant", "content": content}, "finish_reason": finish_reason}
        ],
        "usage": {"prompt_tokens": prompt_tokens, "completion_tokens": completion_tokens},
    }


VALID_EXTRACTION = {"invoice_number": "INV-001"}
VALID_EXTRACTION_JSON = json.dumps(VALID_EXTRACTION)

# Default LLM dict used across tests
LLM_DICT = {"llm_endpoint": "http://vllm:8000", "llm_model": "granite-3.3", "max_model_len": 32768}


def _make_async_cm_mock():
    """Return a MagicMock usable as ``async with mock:``.

    Using a plain asyncio.BoundedSemaphore here would bind it to whatever
    event loop existed at creation time (module-import), which is a different
    loop from the one asyncio.run() creates per test.  A MagicMock with
    AsyncMock __aenter__/__aexit__ is loop-agnostic and avoids that error.
    """
    cm = MagicMock()
    cm.__aenter__ = AsyncMock(return_value=None)
    cm.__aexit__ = AsyncMock(return_value=False)
    return cm


def _standard_patches(
    *,
    job_row=None,
    schema_row=None,
    file_text: str = "INVOICE #INV-001 Vendor: Acme TOTAL: EUR 100",
    input_tokens: int = 50,
    reserved_output: int = 512,
    vllm_response=None,
    validate_return=None,
    update_job_return: bool = True,
):
    """
    Return a dict of patch targets and their configured mocks for the happy path.
    Callers override individual entries as needed.
    """
    if job_row is None:
        job_row = _mock_job_row()
    if schema_row is None:
        schema_row = _mock_schema_row()
    if vllm_response is None:
        vllm_response = _vllm_response(VALID_EXTRACTION_JSON)
    if validate_return is None:
        validate_return = (VALID_EXTRACTION, 1, 0, 0)

    patches = {
        # Semaphores: replaced with loop-agnostic async context-manager mocks.
        "extract.state.job_limiter": _make_async_cm_mock(),
        "extract.api.v1.jobs.concurrency_limiter": _make_async_cm_mock(),
        "extract.api.v1.jobs.get_llm_endpoint": Mock(return_value=LLM_DICT),
        "extract.api.v1.jobs.db_repo.get_job_by_id": Mock(return_value=job_row),
        "extract.api.v1.jobs.db_repo.update_job": Mock(return_value=update_job_return),
        "extract.api.v1.jobs._resolve_schema": Mock(return_value=schema_row),
        "extract.api.v1.jobs._tokenize": Mock(return_value=input_tokens),
        "extract.api.v1.jobs.check_extraction_budget": Mock(return_value=reserved_output),
        "extract.api.v1.jobs.render_few_shot_block": Mock(return_value=""),
        "extract.api.v1.jobs.build_messages": Mock(return_value=[{"role": "user", "content": "..."}]),
        "extract.api.v1.jobs.call_vllm_safe": AsyncMock(return_value=vllm_response),
        "extract.api.v1.jobs.validate_with_retry": AsyncMock(return_value=validate_return),
        "extract.api.v1.jobs.cleanup_staging_directory": Mock(),
    }
    return patches


def _apply_patches(patches: dict):
    """Context-manager stack for all patch targets."""
    from contextlib import ExitStack
    stack = ExitStack()
    mocks = {}
    for target, mock in patches.items():
        mocks[target] = stack.enter_context(patch(target, mock))
    return stack, mocks


# ---------------------------------------------------------------------------
# Helpers for staged-file simulation (replaces filesystem reads)
# ---------------------------------------------------------------------------

class _FakePath:
    """Minimal Path-like object for staged_path.read_bytes()."""

    def __init__(self, content: bytes):
        self._content = content

    def read_bytes(self) -> bytes:
        return self._content

    def __str__(self):
        return "<fake_path>"


def _patch_staging_dir(job_id: str, staged_file_content: bytes | None):
    """
    Patch ``settings.extract.staging_dir`` so ``job_dir.iterdir()`` returns
    a single fake path (or nothing if *staged_file_content* is None).
    """
    fake_job_dir = Mock()
    if staged_file_content is None:
        fake_job_dir.exists.return_value = False
    else:
        fake_job_dir.exists.return_value = True
        fake_path = _FakePath(staged_file_content)
        fake_job_dir.iterdir.return_value = [fake_path]

    fake_staging_root = Mock()
    fake_staging_root.__truediv__ = Mock(return_value=fake_job_dir)

    return patch("extract.api.v1.jobs.settings") , fake_staging_root, fake_job_dir


def _patch_settings_with_staging(
    staged_file_content: bytes | None,
    results_dir: Path | None = None,
):
    """
    Return a mock settings object wired up with a staging dir that returns
    *staged_file_content* and a results_dir that accepts writes.
    """
    if results_dir is None:
        # Use a MagicMock so Path / operator and write_text can be called
        results_dir_mock = MagicMock()
        results_dir_mock.__truediv__ = lambda self, name: results_dir_mock
        results_dir_mock.parent = results_dir_mock
        results_dir_mock.mkdir = Mock()
        results_dir_mock.write_text = Mock()
    else:
        results_dir_mock = results_dir

    fake_job_dir = Mock()
    if staged_file_content is None:
        fake_job_dir.exists.return_value = False
    else:
        fake_job_dir.exists.return_value = True
        fake_path = _FakePath(staged_file_content)
        fake_job_dir.iterdir.return_value = [fake_path]

    mock_settings = Mock()
    mock_settings.extract.staging_dir.__truediv__ = Mock(return_value=fake_job_dir)
    mock_settings.extract.results_dir.__truediv__ = Mock(return_value=results_dir_mock)
    mock_settings.extract.output_token_factor = 2.0

    return mock_settings, results_dir_mock, fake_job_dir


# ---------------------------------------------------------------------------
# Import target under test
# ---------------------------------------------------------------------------

from extract.api.v1.jobs import _process_extract_job  # noqa: E402


# ===========================================================================
# Step 1 — in_progress + row/schema resolution failures
# ===========================================================================

class TestStep1InProgress:
    def test_update_job_called_with_in_progress(self):
        """Worker immediately marks the job in_progress."""
        text = b"INVOICE #INV-001"
        p = _standard_patches()
        mock_settings, results_mock, _ = _patch_settings_with_staging(text)
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        first_call = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list[0]
        assert first_call == call(job_id="job-001", status="in_progress")

    def test_job_row_not_found_aborts_silently(self):
        """If the DB row is gone at worker start the worker exits without exception."""
        p = _standard_patches()
        p["extract.api.v1.jobs.db_repo.get_job_by_id"] = Mock(return_value=None)
        mock_settings, _, _ = _patch_settings_with_staging(b"text")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))  # must not raise

        # update_job should have been called once (in_progress) but not again
        assert p["extract.api.v1.jobs.db_repo.update_job"].call_count == 1

    def test_schema_not_found_marks_failed(self):
        from extract.utils.exceptions import ExtractException
        p = _standard_patches()
        p["extract.api.v1.jobs._resolve_schema"] = Mock(
            side_effect=ExtractException(404, "SCHEMA_NOT_FOUND", "No schema")
        )
        mock_settings, _, _ = _patch_settings_with_staging(b"text")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "SCHEMA_NOT_FOUND"
        p["extract.api.v1.jobs.cleanup_staging_directory"].assert_called_once()


# ===========================================================================
# Step 2 — Staged file reading
# ===========================================================================

class TestStep2ReadFile:
    def test_missing_staged_dir_marks_failed(self):
        p = _standard_patches()
        mock_settings, _, _ = _patch_settings_with_staging(None)  # dir does not exist
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "STAGED_FILE_MISSING"

    def test_utf8_decode_error_marks_failed(self):
        # \xff\xfe is invalid UTF-8
        p = _standard_patches()
        mock_settings, _, _ = _patch_settings_with_staging(b"\xff\xfe binary garbage")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "FILE_DECODE_ERROR"
        p["extract.api.v1.jobs.cleanup_staging_directory"].assert_called_once()


# ===========================================================================
# Step 3 — Tokenization + context-window guard
# ===========================================================================

class TestStep3TokenGuard:
    def test_tokenization_failure_marks_failed(self):
        p = _standard_patches()
        p["extract.api.v1.jobs._tokenize"] = Mock(side_effect=RuntimeError("vllm down"))
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "TOKENIZATION_ERROR"
        p["extract.api.v1.jobs.cleanup_staging_directory"].assert_called_once()

    def test_context_limit_exceeded_marks_failed(self):
        from extract.utils.exceptions import ExtractException
        p = _standard_patches()
        p["extract.api.v1.jobs.check_extraction_budget"] = Mock(
            side_effect=ExtractException(413, "CONTEXT_LIMIT_EXCEEDED", "Too large",
                                         details={"excess_tokens": 500})
        )
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "CONTEXT_LIMIT_EXCEEDED"
        assert "error_details" in failed_call.kwargs.get("metadata", {})
        p["extract.api.v1.jobs.cleanup_staging_directory"].assert_called_once()

    def test_context_guard_diagnostics_persisted_in_metadata(self):
        from extract.utils.exceptions import ExtractException
        details = {"excess_tokens": 200, "total_required_tokens": 33000}
        p = _standard_patches()
        p["extract.api.v1.jobs.check_extraction_budget"] = Mock(
            side_effect=ExtractException(413, "CONTEXT_LIMIT_EXCEEDED", "x", details=details)
        )
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["metadata"]["error_details"] == details


# ===========================================================================
# Step 4 — vLLM call failure / empty choices
# ===========================================================================

class TestStep4VllmCall:
    def test_vllm_connection_error_marks_failed(self):
        from extract.utils.exceptions import ExtractException
        p = _standard_patches()
        p["extract.api.v1.jobs.call_vllm_safe"] = AsyncMock(
            side_effect=ExtractException(503, "LLM_UNAVAILABLE", "unreachable")
        )
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "LLM_UNAVAILABLE"
        p["extract.api.v1.jobs.cleanup_staging_directory"].assert_called_once()

    def test_empty_choices_marks_failed(self):
        p = _standard_patches()
        p["extract.api.v1.jobs.call_vllm_safe"] = AsyncMock(
            return_value={"choices": [], "usage": {}}
        )
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "LLM_ERROR"

    def test_phase_extracting_set_before_vllm_call(self):
        p = _standard_patches()
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        update_calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        phase_calls = [c for c in update_calls if c.kwargs.get("metadata", {}).get("phase") == "extracting"]
        assert len(phase_calls) >= 1


# ===========================================================================
# Step 5 — finish_reason=length retry
# ===========================================================================

class TestStep5LengthRetry:
    def test_length_retry_succeeds_with_boosted_budget(self):
        """First call returns length; second call returns a valid extraction."""
        first_resp = _vllm_response("", finish_reason="length")
        second_resp = _vllm_response(VALID_EXTRACTION_JSON, finish_reason="stop")

        p = _standard_patches()
        p["extract.api.v1.jobs.call_vllm_safe"] = AsyncMock(
            side_effect=[first_resp, second_resp]
        )
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        statuses = [c.kwargs.get("status") for c in calls if "status" in c.kwargs]
        assert "completed" in statuses

    def test_length_on_retry_marks_output_budget_exceeded(self):
        """Both first and retry calls return finish_reason=length → OUTPUT_BUDGET_EXCEEDED."""
        length_resp = _vllm_response("", finish_reason="length")

        p = _standard_patches()
        p["extract.api.v1.jobs.call_vllm_safe"] = AsyncMock(
            side_effect=[length_resp, length_resp]
        )
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "OUTPUT_BUDGET_EXCEEDED"
        p["extract.api.v1.jobs.cleanup_staging_directory"].assert_called_once()

    def test_vllm_error_on_length_retry_marks_failed(self):
        from extract.utils.exceptions import ExtractException
        length_resp = _vllm_response("", finish_reason="length")

        p = _standard_patches()
        p["extract.api.v1.jobs.call_vllm_safe"] = AsyncMock(
            side_effect=[length_resp, ExtractException(500, "LLM_ERROR", "crash on retry")]
        )
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "LLM_ERROR"

    def test_length_retry_uses_boosted_max_tokens(self):
        """compute_reserved_output is called once with a higher factor after length."""
        length_resp = _vllm_response("", finish_reason="length")
        good_resp = _vllm_response(VALID_EXTRACTION_JSON)

        p = _standard_patches()
        p["extract.api.v1.jobs.call_vllm_safe"] = AsyncMock(
            side_effect=[length_resp, good_resp]
        )
        compute_mock = Mock(return_value=768)
        p["extract.api.v1.jobs.compute_reserved_output"] = compute_mock
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        # compute_reserved_output called with factor > the base factor
        compute_mock.assert_called_once()
        _, kwargs = compute_mock.call_args
        assert kwargs.get("output_token_factor", 0) > mock_settings.extract.output_token_factor


# ===========================================================================
# Step 6 — Validation failure
# ===========================================================================

class TestStep6Validation:
    def test_validation_failure_marks_extraction_validation_failed(self):
        from extract.utils.exceptions import ExtractException
        p = _standard_patches()
        p["extract.api.v1.jobs.validate_with_retry"] = AsyncMock(
            side_effect=ExtractException(
                422, "EXTRACTION_VALIDATION_FAILED", "schema mismatch",
                details={"validation_errors": "missing field", "raw_output": "{}"}
            )
        )
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "EXTRACTION_VALIDATION_FAILED"
        assert "error_details" in failed_call.kwargs.get("metadata", {})
        p["extract.api.v1.jobs.cleanup_staging_directory"].assert_called_once()

    def test_phase_validating_set_before_validate_call(self):
        p = _standard_patches()
        mock_settings, _, _ = _patch_settings_with_staging(b"text content")
        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        update_calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        phase_calls = [c for c in update_calls if c.kwargs.get("metadata", {}).get("phase") == "validating"]
        assert len(phase_calls) >= 1


# ===========================================================================
# Step 7 — Result file write failure
# ===========================================================================

class TestStep7ResultWrite:
    def test_result_write_error_marks_failed(self):
        p = _standard_patches()

        # Build a settings mock whose results_dir / job_id raises on write_text
        fake_result_path = Mock()
        fake_result_path.parent = Mock()
        fake_result_path.parent.mkdir = Mock()
        fake_result_path.write_text = Mock(side_effect=OSError("disk full"))

        fake_results_dir = Mock()
        fake_results_dir.__truediv__ = Mock(return_value=fake_result_path)

        fake_job_dir = Mock()
        fake_job_dir.exists.return_value = True
        fake_job_dir.iterdir.return_value = [_FakePath(b"text content")]

        mock_settings = Mock()
        mock_settings.extract.staging_dir.__truediv__ = Mock(return_value=fake_job_dir)
        mock_settings.extract.results_dir.__truediv__ = Mock(return_value=fake_result_path)
        mock_settings.extract.output_token_factor = 2.0

        p["extract.api.v1.jobs.settings"] = mock_settings

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "RESULT_WRITE_ERROR"
        p["extract.api.v1.jobs.cleanup_staging_directory"].assert_called_once()


# ===========================================================================
# Steps 8–9 — Happy path: completed status + result file + staging cleanup
# ===========================================================================

class TestHappyPath:
    def _run_happy_path(self, extra_patches=None):
        """Execute the worker in the fully-happy scenario, return mocks dict."""
        p = _standard_patches()
        written: list[str] = []

        fake_result_path = Mock()
        fake_result_path.parent = Mock()
        fake_result_path.parent.mkdir = Mock()
        fake_result_path.write_text = Mock(side_effect=lambda text, encoding=None: written.append(text))

        fake_job_dir = Mock()
        fake_job_dir.exists.return_value = True
        fake_job_dir.iterdir.return_value = [_FakePath(b"INVOICE #INV-001 Vendor: Acme")]

        mock_settings = Mock()
        mock_settings.extract.staging_dir.__truediv__ = Mock(return_value=fake_job_dir)
        mock_settings.extract.results_dir.__truediv__ = Mock(return_value=fake_result_path)
        mock_settings.extract.output_token_factor = 2.0

        p["extract.api.v1.jobs.settings"] = mock_settings
        if extra_patches:
            p.update(extra_patches)

        with _apply_patches(p)[0]:
            _run(_process_extract_job("job-001"))

        return p, written, fake_result_path

    def test_status_set_to_completed(self):
        p, _, _ = self._run_happy_path()
        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        statuses = [c.kwargs.get("status") for c in calls if "status" in c.kwargs]
        assert "completed" in statuses

    def test_completed_at_set(self):
        p, _, _ = self._run_happy_path()
        calls = p["extract.api.v1.jobs.db_repo.update_job"].call_args_list
        completed_call = next(c for c in calls if c.kwargs.get("status") == "completed")
        assert completed_call.kwargs.get("completed_at") is not None

    def test_result_file_written(self):
        _, written, _ = self._run_happy_path()
        assert len(written) == 1
        payload = json.loads(written[0])
        assert payload["status"] == "completed"

    def test_result_payload_structure(self):
        _, written, _ = self._run_happy_path()
        payload = json.loads(written[0])
        assert "data" in payload
        assert "extraction" in payload["data"]
        assert "source" in payload["data"]
        assert payload["data"]["source"]["input_type"] == "file"
        assert "meta" in payload
        assert "usage" in payload

    def test_result_meta_contains_timing(self):
        _, written, _ = self._run_happy_path()
        payload = json.loads(written[0])
        assert "timing_in_secs" in payload["meta"]
        assert "extracting" in payload["meta"]["timing_in_secs"]
        assert "validating" in payload["meta"]["timing_in_secs"]

    def test_result_meta_validation_attempts(self):
        _, written, _ = self._run_happy_path()
        payload = json.loads(written[0])
        assert payload["meta"]["validation_attempts"] == 1

    def test_staging_dir_cleaned_up(self):
        p, _, _ = self._run_happy_path()
        p["extract.api.v1.jobs.cleanup_staging_directory"].assert_called_once()

    def test_validation_attempts_2_when_retry_needed(self):
        extra = {
            "extract.api.v1.jobs.validate_with_retry": AsyncMock(
                return_value=(VALID_EXTRACTION, 2, 50, 30)
            )
        }
        _, written, _ = self._run_happy_path(extra_patches=extra)
        payload = json.loads(written[0])
        assert payload["meta"]["validation_attempts"] == 2

    def test_usage_totals_include_retry_tokens(self):
        extra = {
            "extract.api.v1.jobs.validate_with_retry": AsyncMock(
                return_value=(VALID_EXTRACTION, 2, 50, 30)
            ),
            "extract.api.v1.jobs.call_vllm_safe": AsyncMock(
                return_value=_vllm_response(VALID_EXTRACTION_JSON, prompt_tokens=400, completion_tokens=80)
            ),
        }
        _, written, _ = self._run_happy_path(extra_patches=extra)
        payload = json.loads(written[0])
        # 400 prompt + 50 retry_prompt = 450; 80 completion + 30 retry_completion = 110
        assert payload["usage"]["input_tokens"] == 450
        assert payload["usage"]["output_tokens"] == 110
        assert payload["usage"]["total_tokens"] == 560

    def test_word_count_included_in_source(self):
        """input_words reflects the word count of the staged file content."""
        _, written, _ = self._run_happy_path()
        payload = json.loads(written[0])
        # "INVOICE #INV-001 Vendor: Acme" → 4 words (split)
        assert payload["data"]["source"]["input_words"] == 4

    def test_document_name_in_source(self):
        _, written, _ = self._run_happy_path()
        payload = json.loads(written[0])
        assert payload["data"]["source"]["document_name"] == "invoice.txt"

    def test_schema_id_in_data(self):
        _, written, _ = self._run_happy_path()
        payload = json.loads(written[0])
        assert payload["data"]["schema_id"] == "schema-001"
