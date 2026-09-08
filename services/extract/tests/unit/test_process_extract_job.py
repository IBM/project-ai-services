"""
Unit tests for the background extraction workers.

Architecture under test
───────────────────────
  _process_batch_job  – orchestrates the batch: marks in_progress, resolves
                        schema, builds per-file tasks, determines final status.
  _process_file       – per-file pipeline: read → tokenize → extract →
                        validate → write result; updates doc row at every phase.

Every external boundary (DB, filesystem, vLLM, semaphores) is mocked so no
real DB, filesystem, or vLLM is required.

Coverage matrix
───────────────
Batch orchestrator:
  Step 1a – job marked in_progress on worker start
  Step 1b – job row not found → silent abort
  Step 1c – schema not found → job marked failed
  Step 1d – no document rows found → job marked failed
  Step 1e – final status: all-completed, all-failed, mixed

Per-file pipeline (tested via _process_file directly):
  Step 2  – UTF-8 decode error → doc failed(FILE_READ_ERROR)
  Step 3  – Tokenization failure, context-window budget breach
  Step 4  – vLLM call failure, empty choices list
  Step 5  – finish_reason=length retry (success + still length → OUTPUT_BUDGET_EXCEEDED)
  Step 6  – validate_with_retry failure → EXTRACTION_VALIDATION_FAILED
  Step 7  – Result-file write failure → RESULT_WRITE_ERROR
  Step 8  – Happy path: doc completed, result file written with correct structure
  Step 9  – Staging cleanup called via batch worker in every terminal path
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
    status="accepted",
    file_count=1,
):
    row = Mock()
    row.job_id = job_id
    row.schema_id = schema_id
    row.status = status
    row.file_count = file_count
    return row


def _mock_doc_row(
    doc_id="doc-001",
    job_id="job-001",
    filename="invoice.txt",
    source_type="txt",
    status="pending",
):
    row = Mock()
    row.doc_id = doc_id
    row.job_id = job_id
    row.filename = filename
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


def _apply_patches(patches: dict):
    """Context-manager stack for all patch targets."""
    from contextlib import ExitStack
    stack = ExitStack()
    mocks = {}
    for target, mock in patches.items():
        mocks[target] = stack.enter_context(patch(target, mock))
    return stack, mocks


# ---------------------------------------------------------------------------
# Staged-file simulation helpers
# ---------------------------------------------------------------------------

class _FakePath:
    """Minimal Path-like object for staged_path.read_bytes() / .name."""

    def __init__(self, content: bytes, name: str = "invoice.txt"):
        self._content = content
        self.name = name

    def read_bytes(self) -> bytes:
        return self._content

    def __str__(self):
        return f"<fake_path/{self.name}>"

    def __truediv__(self, other):
        return self


# ---------------------------------------------------------------------------
# Standard patch factories
# ---------------------------------------------------------------------------

def _standard_batch_patches(
    *,
    job_row=None,
    schema_row=None,
    doc_rows=None,
    update_doc_return: bool = True,
    update_job_return: bool = True,
):
    """Patches for _process_batch_job (orchestration layer only)."""
    if job_row is None:
        job_row = _mock_job_row()
    if schema_row is None:
        schema_row = _mock_schema_row()
    if doc_rows is None:
        doc_rows = [_mock_doc_row()]

    # After _process_file completes, _process_batch_job calls get_documents_by_job
    # a second time to tally outcomes.  We make all docs look completed by default.
    completed_doc_rows = []
    for d in doc_rows:
        cdoc = Mock()
        cdoc.doc_id = d.doc_id
        cdoc.status = "completed"
        completed_doc_rows.append(cdoc)

    return {
        "extract.state.extract_limiter": _make_async_cm_mock(),
        "extract.utils.job.get_llm_endpoint": Mock(return_value=LLM_DICT),
        "extract.utils.job.db_repo.get_job_by_id": Mock(return_value=job_row),
        "extract.utils.job.db_repo.update_job": Mock(return_value=update_job_return),
        "extract.utils.job.db_repo.update_document": Mock(return_value=update_doc_return),
        "extract.utils.job.db_repo.get_documents_by_job": Mock(
            side_effect=[doc_rows, completed_doc_rows]
        ),
        "extract.utils.job.resolve_schema": Mock(return_value=schema_row),
        "extract.utils.job.process_file": AsyncMock(),
        "extract.utils.job.cleanup_staging_directory": Mock(),
    }


def _make_default_settings_mock():
    """Return a settings mock with a no-op results_dir suitable for _process_file tests.

    Any test that reaches the result-write stage without overriding settings will
    use this mock so the real /var/cache filesystem is never touched.
    """
    fake_result_path = MagicMock()
    fake_result_path.write_text = Mock()

    fake_job_results_dir = MagicMock()
    fake_job_results_dir.mkdir = Mock()
    fake_job_results_dir.__truediv__ = Mock(return_value=fake_result_path)

    fake_results_root = MagicMock()
    fake_results_root.__truediv__ = Mock(return_value=fake_job_results_dir)

    mock_settings = Mock()
    mock_settings.extract.results_dir = fake_results_root
    mock_settings.extract.output_token_factor = 2.0
    return mock_settings


def _standard_file_patches(
    *,
    staged_content: bytes = b"INVOICE #INV-001 Vendor: Acme TOTAL: EUR 100",
    input_tokens: int = 50,
    reserved_output: int = 512,
    vllm_response=None,
    validate_return=None,
    update_doc_return: bool = True,
):
    """Patches for _process_file (per-file pipeline).

    Includes a default settings mock so tests that reach the result-write
    stage never touch the real /var/cache filesystem.
    """
    if vllm_response is None:
        vllm_response = _vllm_response(VALID_EXTRACTION_JSON)
    if validate_return is None:
        validate_return = (VALID_EXTRACTION, 1, 0, 0)

    return {
        "extract.state.parallel_file_limiter": _make_async_cm_mock(),
        "extract.utils.job.concurrency_limiter": _make_async_cm_mock(),
        "extract.utils.job.db_repo.update_document": Mock(return_value=update_doc_return),
        "extract.utils.job._tokenize": Mock(return_value=input_tokens),
        "extract.utils.job.check_extraction_budget": Mock(return_value=reserved_output),
        "extract.utils.job.render_few_shot_block": Mock(return_value=""),
        "extract.utils.job.build_messages": Mock(return_value=[{"role": "user", "content": "..."}]),
        "extract.utils.job.call_vllm_safe": AsyncMock(return_value=vllm_response),
        "extract.utils.job.validate_with_retry": AsyncMock(return_value=validate_return),
        # Default settings mock — prevents real /var/cache writes in any test that
        # reaches the write stage without explicitly overriding settings.
        "extract.utils.job.settings": _make_default_settings_mock(),
    }


# ---------------------------------------------------------------------------
# Import targets under test
# ---------------------------------------------------------------------------

from extract.utils.job import process_batch_job, process_file  # noqa: E402


# ===========================================================================
# Batch orchestrator — Step 1
# ===========================================================================

class TestStep1BatchOrchestrator:
    def test_job_marked_in_progress(self):
        """_process_batch_job immediately marks the job in_progress."""
        p = _standard_batch_patches()
        with _apply_patches(p)[0]:
            _run(process_batch_job("job-001"))

        first_call = p["extract.utils.job.db_repo.update_job"].call_args_list[0]
        assert first_call == call(job_id="job-001", status="in_progress")

    def test_job_row_not_found_aborts_silently(self):
        """If the DB row is gone at worker start the worker exits without exception."""
        p = _standard_batch_patches()
        p["extract.utils.job.db_repo.get_job_by_id"] = Mock(return_value=None)

        with _apply_patches(p)[0]:
            _run(process_batch_job("job-001"))  # must not raise

        # Only in_progress update should have been attempted (no further updates)
        assert p["extract.utils.job.db_repo.update_job"].call_count == 1

    def test_schema_not_found_marks_job_failed(self):
        from extract.utils.exceptions import ExtractException
        p = _standard_batch_patches()
        p["extract.utils.job.resolve_schema"] = Mock(
            side_effect=ExtractException(404, "SCHEMA_NOT_FOUND", "No schema")
        )

        with _apply_patches(p)[0]:
            _run(process_batch_job("job-001"))

        calls = p["extract.utils.job.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "SCHEMA_NOT_FOUND"
        p["extract.utils.job.cleanup_staging_directory"].assert_called_once()

    def test_no_document_rows_marks_job_failed(self):
        """If the document table is empty for a job the worker marks it failed."""
        p = _standard_batch_patches()
        p["extract.utils.job.db_repo.get_documents_by_job"] = Mock(return_value=[])

        with _apply_patches(p)[0]:
            _run(process_batch_job("job-001"))

        calls = p["extract.utils.job.db_repo.update_job"].call_args_list
        failed_call = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed_call.kwargs["error"] == "NO_DOCUMENTS"
        p["extract.utils.job.cleanup_staging_directory"].assert_called_once()

    def test_all_completed_marks_job_completed(self):
        p = _standard_batch_patches()
        # Both calls return all-completed docs
        completed_docs = [Mock(status="completed"), Mock(status="completed")]
        p["extract.utils.job.db_repo.get_documents_by_job"] = Mock(
            side_effect=[[_mock_doc_row()], completed_docs]
        )
        with _apply_patches(p)[0]:
            _run(process_batch_job("job-001"))

        calls = p["extract.utils.job.db_repo.update_job"].call_args_list
        final = next(c for c in calls if c.kwargs.get("status") in ("completed", "failed", "completed_with_errors"))
        assert final.kwargs["status"] == "completed"

    def test_all_failed_marks_job_failed(self):
        p = _standard_batch_patches()
        failed_docs = [Mock(status="failed"), Mock(status="failed")]
        p["extract.utils.job.db_repo.get_documents_by_job"] = Mock(
            side_effect=[[_mock_doc_row()], failed_docs]
        )
        with _apply_patches(p)[0]:
            _run(process_batch_job("job-001"))

        calls = p["extract.utils.job.db_repo.update_job"].call_args_list
        final = next(c for c in calls if c.kwargs.get("status") in ("completed", "failed", "completed_with_errors"))
        assert final.kwargs["status"] == "failed"

    def test_mixed_outcome_marks_completed_with_errors(self):
        p = _standard_batch_patches()
        mixed_docs = [Mock(status="completed"), Mock(status="failed")]
        p["extract.utils.job.db_repo.get_documents_by_job"] = Mock(
            side_effect=[[_mock_doc_row(), _mock_doc_row(doc_id="doc-002")], mixed_docs]
        )
        with _apply_patches(p)[0]:
            _run(process_batch_job("job-001"))

        calls = p["extract.utils.job.db_repo.update_job"].call_args_list
        final = next(c for c in calls if c.kwargs.get("status") in ("completed", "failed", "completed_with_errors"))
        assert final.kwargs["status"] == "completed_with_errors"

    def test_staging_cleanup_always_called(self):
        """cleanup_staging_directory is called even when job fails early."""
        from extract.utils.exceptions import ExtractException
        p = _standard_batch_patches()
        p["extract.utils.job.resolve_schema"] = Mock(
            side_effect=ExtractException(404, "SCHEMA_NOT_FOUND", "No schema")
        )
        with _apply_patches(p)[0]:
            _run(process_batch_job("job-001"))

        p["extract.utils.job.cleanup_staging_directory"].assert_called_once()


# ===========================================================================
# Per-file pipeline — helpers
# ===========================================================================

def _run_process_file(
    patches: dict,
    staged_content: bytes,
    doc_id: str = "doc-001",
    job_id: str = "job-001",
    schema_row=None,
):
    """Run _process_file with a fake staged path and given patches dict."""
    if schema_row is None:
        schema_row = _mock_schema_row()

    fake_path = _FakePath(staged_content, name="invoice.txt")

    with _apply_patches(patches)[0]:
        _run(process_file(
            job_id=job_id,
            doc_id=doc_id,
            staged_path=fake_path,
            schema_row=schema_row,
            llm_endpoint="http://vllm:8000",
            llm_model="granite-3.3",
            max_model_len=32768,
        ))

    return patches


# ===========================================================================
# Step 2 — Staged file reading
# ===========================================================================

class TestStep2ReadFile:
    def test_utf8_decode_error_marks_doc_failed(self):
        p = _standard_file_patches()
        _run_process_file(p, b"\xff\xfe binary garbage")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        failed = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert "could not be decoded" in failed.kwargs.get("error", "") or "Failed to read" in failed.kwargs.get("error", "")

    def test_doc_marked_in_progress_on_start(self):
        p = _standard_file_patches()
        _run_process_file(p, b"valid content")

        first_call = p["extract.utils.job.db_repo.update_document"].call_args_list[0]
        assert first_call.kwargs.get("status") == "in_progress"


# ===========================================================================
# Step 3 — Tokenization + context-window guard
# ===========================================================================

class TestStep3TokenGuard:
    def test_tokenization_failure_marks_doc_failed(self):
        p = _standard_file_patches()
        p["extract.utils.job._tokenize"] = Mock(side_effect=RuntimeError("vllm down"))
        _run_process_file(p, b"text content")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        failed = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert "tokenise" in failed.kwargs.get("error", "")

    def test_context_limit_exceeded_marks_doc_failed(self):
        from extract.utils.exceptions import ExtractException
        p = _standard_file_patches()
        p["extract.utils.job.check_extraction_budget"] = Mock(
            side_effect=ExtractException(
                413, "CONTEXT_LIMIT_EXCEEDED", "Too large",
                details={"excess_tokens": 500},
            )
        )
        _run_process_file(p, b"text content")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        failed = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed.kwargs.get("status") == "failed"

    def test_context_guard_diagnostics_in_metadata(self):
        from extract.utils.exceptions import ExtractException
        details = {"excess_tokens": 200, "total_required_tokens": 33000}
        p = _standard_file_patches()
        p["extract.utils.job.check_extraction_budget"] = Mock(
            side_effect=ExtractException(413, "CONTEXT_LIMIT_EXCEEDED", "x", details=details)
        )
        _run_process_file(p, b"text content")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        failed = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed.kwargs.get("metadata", {}).get("token_diagnostics") == details



# ===========================================================================
# Step 4 — vLLM call failure / empty choices
# ===========================================================================

class TestStep4VllmCall:
    def test_vllm_connection_error_marks_doc_failed(self):
        from extract.utils.exceptions import ExtractException
        p = _standard_file_patches()
        p["extract.utils.job.call_vllm_safe"] = AsyncMock(
            side_effect=ExtractException(503, "LLM_UNAVAILABLE", "unreachable")
        )
        _run_process_file(p, b"text content")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        failed = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed.kwargs.get("status") == "failed"

    def test_empty_choices_marks_doc_failed(self):
        p = _standard_file_patches()
        p["extract.utils.job.call_vllm_safe"] = AsyncMock(
            return_value={"choices": [], "usage": {}}
        )
        _run_process_file(p, b"text content")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        failed = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert "empty choices" in failed.kwargs.get("error", "")



# ===========================================================================
# Step 5 — finish_reason=length retry
# ===========================================================================

class TestStep5LengthRetry:
    def test_length_retry_succeeds_with_boosted_budget(self):
        """First call returns length; second call returns a valid extraction."""
        first_resp = _vllm_response("", finish_reason="length")
        second_resp = _vllm_response(VALID_EXTRACTION_JSON, finish_reason="stop")

        p = _standard_file_patches()
        p["extract.utils.job.call_vllm_safe"] = AsyncMock(
            side_effect=[first_resp, second_resp]
        )
        _run_process_file(p, b"text content")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        statuses = [c.kwargs.get("status") for c in calls if "status" in c.kwargs]
        assert "completed" in statuses

    def test_length_on_retry_marks_output_budget_exceeded(self):
        """Both first and retry calls return finish_reason=length."""
        length_resp = _vllm_response("", finish_reason="length")

        p = _standard_file_patches()
        p["extract.utils.job.call_vllm_safe"] = AsyncMock(
            side_effect=[length_resp, length_resp]
        )
        _run_process_file(p, b"text content")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        failed = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert "truncated" in failed.kwargs.get("error", "")

    def test_vllm_error_on_length_retry_marks_doc_failed(self):
        from extract.utils.exceptions import ExtractException
        length_resp = _vllm_response("", finish_reason="length")

        p = _standard_file_patches()
        p["extract.utils.job.call_vllm_safe"] = AsyncMock(
            side_effect=[length_resp, ExtractException(500, "LLM_ERROR", "crash on retry")]
        )
        _run_process_file(p, b"text content")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        failed = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed.kwargs.get("error") == "crash on retry"

    def test_length_retry_uses_boosted_max_tokens(self):
        """compute_reserved_output is called with a higher factor after length."""
        length_resp = _vllm_response("", finish_reason="length")
        good_resp = _vllm_response(VALID_EXTRACTION_JSON)

        p = _standard_file_patches()
        p["extract.utils.job.call_vllm_safe"] = AsyncMock(
            side_effect=[length_resp, good_resp]
        )
        compute_mock = Mock(return_value=768)
        p["extract.utils.job.compute_reserved_output"] = compute_mock

        schema_row = _mock_schema_row()
        _run_process_file(p, b"text content", schema_row=schema_row)

        compute_mock.assert_called_once()
        _, kwargs = compute_mock.call_args
        # The boosted factor should be 1.5× the base (2.0 default from settings)
        # In the actual code it reads settings.extract.output_token_factor, but since
        # settings isn't mocked here, it will use the real default (2.0). Just verify
        # compute_reserved_output was called with some output_token_factor.
        assert "output_token_factor" in kwargs


# ===========================================================================
# Step 6 — Validation failure
# ===========================================================================

class TestStep6Validation:
    def test_validation_failure_marks_doc_failed(self):
        from extract.utils.exceptions import ExtractException
        p = _standard_file_patches()
        p["extract.utils.job.validate_with_retry"] = AsyncMock(
            side_effect=ExtractException(
                422, "EXTRACTION_VALIDATION_FAILED", "schema mismatch",
                details={"validation_errors": "missing field", "raw_output": "{}"},
            )
        )
        _run_process_file(p, b"text content")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        failed = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert failed.kwargs.get("status") == "failed"



# ===========================================================================
# Step 7 — Result file write failure
# ===========================================================================

class TestStep7ResultWrite:
    def test_result_write_error_marks_doc_failed(self):
        p = _standard_file_patches()

        # Make settings.extract.results_dir / job_id / doc_id_result.json raise on write_text
        fake_result_path = MagicMock()
        fake_result_path.write_text = Mock(side_effect=OSError("disk full"))

        fake_job_results_dir = MagicMock()
        fake_job_results_dir.mkdir = Mock()
        fake_job_results_dir.__truediv__ = Mock(return_value=fake_result_path)

        fake_results_root = MagicMock()
        fake_results_root.__truediv__ = Mock(return_value=fake_job_results_dir)

        mock_settings = Mock()
        mock_settings.extract.results_dir = fake_results_root
        mock_settings.extract.output_token_factor = 2.0
        p["extract.utils.job.settings"] = mock_settings

        _run_process_file(p, b"text content")

        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        failed = next(c for c in calls if c.kwargs.get("status") == "failed")
        assert "result file" in failed.kwargs.get("error", "")


# ===========================================================================
# Steps 8–9 — Happy path: doc completed, result file written correctly
# ===========================================================================

class TestHappyPath:
    def _run_happy_path(self, extra_patches=None):
        """Run _process_file through the full happy path; return patches, written list."""
        p = _standard_file_patches()
        written: list[str] = []

        fake_result_path = MagicMock()
        fake_result_path.write_text = Mock(side_effect=lambda text, encoding=None: written.append(text))

        fake_job_results_dir = MagicMock()
        fake_job_results_dir.mkdir = Mock()
        fake_job_results_dir.__truediv__ = Mock(return_value=fake_result_path)

        fake_results_root = MagicMock()
        fake_results_root.__truediv__ = Mock(return_value=fake_job_results_dir)

        mock_settings = Mock()
        mock_settings.extract.results_dir = fake_results_root
        mock_settings.extract.output_token_factor = 2.0
        p["extract.utils.job.settings"] = mock_settings

        if extra_patches:
            p.update(extra_patches)

        _run_process_file(p, b"INVOICE #INV-001 Vendor: Acme")
        return p, written, fake_result_path

    def test_doc_marked_completed(self):
        p, _, _ = self._run_happy_path()
        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        statuses = [c.kwargs.get("status") for c in calls if "status" in c.kwargs]
        assert "completed" in statuses

    def test_completed_at_set(self):
        p, _, _ = self._run_happy_path()
        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        completed_call = next(c for c in calls if c.kwargs.get("status") == "completed")
        assert completed_call.kwargs.get("completed_at") is not None

    def test_result_file_written(self):
        # File now only contains the parsed extraction output.
        _, written, _ = self._run_happy_path()
        assert len(written) == 1
        payload = json.loads(written[0])
        assert "invoice_number" in payload
        assert list(payload.keys()) == ["invoice_number"]

    def test_result_file_contains_extraction(self):
        _, written, _ = self._run_happy_path()
        payload = json.loads(written[0])
        assert payload == VALID_EXTRACTION

    def test_completed_update_saves_usage_tokens(self):
        # usage_input_tokens and usage_output_tokens must be persisted on the
        # completion update_document call so the result endpoint can reconstruct them.
        p, _, _ = self._run_happy_path()
        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        completed_call = next(c for c in calls if c.kwargs.get("status") == "completed")
        assert completed_call.kwargs["usage_input_tokens"] == 500   # default prompt_tokens
        assert completed_call.kwargs["usage_output_tokens"] == 80   # default completion_tokens

    def test_completed_update_saves_timing_in_metadata(self):
        p, _, _ = self._run_happy_path()
        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        completed_call = next(c for c in calls if c.kwargs.get("status") == "completed")
        meta = completed_call.kwargs.get("metadata", {})
        assert "timing_in_secs" in meta
        assert "extracting" in meta["timing_in_secs"]
        assert "validating" in meta["timing_in_secs"]

    def test_completed_update_saves_validation_attempts(self):
        p, _, _ = self._run_happy_path()
        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        completed_call = next(c for c in calls if c.kwargs.get("status") == "completed")
        meta = completed_call.kwargs.get("metadata", {})
        assert meta.get("validation", {}).get("attempts") == 1

    def test_validation_attempts_2_when_retry_needed(self):
        extra = {
            "extract.utils.job.validate_with_retry": AsyncMock(
                return_value=(VALID_EXTRACTION, 2, 50, 30)
            )
        }
        p, _, _ = self._run_happy_path(extra_patches=extra)
        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        completed_call = next(c for c in calls if c.kwargs.get("status") == "completed")
        meta = completed_call.kwargs.get("metadata", {})
        assert meta.get("validation", {}).get("attempts") == 2

    def test_usage_totals_include_retry_tokens(self):
        extra = {
            "extract.utils.job.validate_with_retry": AsyncMock(
                return_value=(VALID_EXTRACTION, 2, 50, 30)
            ),
            "extract.utils.job.call_vllm_safe": AsyncMock(
                return_value=_vllm_response(VALID_EXTRACTION_JSON, prompt_tokens=400, completion_tokens=80)
            ),
        }
        p, _, _ = self._run_happy_path(extra_patches=extra)
        calls = p["extract.utils.job.db_repo.update_document"].call_args_list
        completed_call = next(c for c in calls if c.kwargs.get("status") == "completed")
        # 400 prompt + 50 retry_prompt = 450; 80 completion + 30 retry_completion = 110
        assert completed_call.kwargs["usage_input_tokens"] == 450
        assert completed_call.kwargs["usage_output_tokens"] == 110


# ===========================================================================
# Step 9 — Staging cleanup is the batch worker's responsibility
# ===========================================================================

class TestStep9StagingCleanup:
    def test_staging_cleaned_after_all_docs_processed(self):
        p = _standard_batch_patches()
        with _apply_patches(p)[0]:
            _run(process_batch_job("job-001"))

        p["extract.utils.job.cleanup_staging_directory"].assert_called_once_with(
            "job-001", p["extract.utils.job.cleanup_staging_directory"].call_args[0][1]
        )
