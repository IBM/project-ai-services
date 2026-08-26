"""
Unit tests for summarize_and_classify_single_table and summarize_and_classify_tables
in common/llm_utils.py.

Covers:
- Successful summarization and classification
- Retry behaviour on transient HTTP errors
- Cancellation via threading.Event
- Fail-fast: first failure stops remaining tables
- Empty input edge case
"""

import threading
import pytest
from unittest.mock import Mock, patch
import requests


PROMPT_TEMPLATE = "Summarize: {content}"


def _make_response(content: str, status_code: int = 200):
    """Build a minimal mock requests.Response."""
    resp = Mock(spec=requests.Response)
    resp.status_code = status_code
    resp.json.return_value = {
        "choices": [{"message": {"content": content}}]
    }
    resp.raise_for_status = Mock()
    if status_code >= 400:
        resp.raise_for_status.side_effect = requests.exceptions.HTTPError(
            response=resp
        )
    return resp


def _make_http_error(status_code: int = 500):
    resp = Mock()
    resp.status_code = status_code
    resp.text = "Internal Server Error"
    err = requests.exceptions.HTTPError(response=resp)
    return err


@pytest.fixture
def mock_session(monkeypatch):
    """Patch misc_utils.SESSION with a fresh Mock before each test."""
    import common.misc_utils as misc_utils
    session = Mock()
    monkeypatch.setattr(misc_utils, "SESSION", session)
    return session


@pytest.fixture
def mock_llm_settings(monkeypatch):
    """Patch common.llm_utils.settings and get_vllm_headers."""
    monkeypatch.setattr("common.llm_utils.settings", Mock())
    monkeypatch.setattr("common.llm_utils.get_vllm_headers", Mock(return_value={}))
    monkeypatch.setattr("common.retry_utils.time.sleep", Mock())


@pytest.mark.unit
class TestSummarizeAndClassifySingleTable:
    """Tests for the single-table worker function."""

    def test_successful_summarization(self, mock_session, mock_llm_settings):
        """Returns (summary, decision) on a well-formed LLM response."""
        from common.llm_utils import summarize_and_classify_single_table

        mock_session.post.return_value = _make_response(
            "Summary: A data table.\nDecision: Yes"
        )

        summary, decision = summarize_and_classify_single_table(
            "prompt", "model", "http://llm", 512
        )

        assert summary == "A data table."
        assert decision is True

    def test_cancelled_before_call(self, mock_session, mock_llm_settings):
        """Returns (No summary., False) immediately if cancel_event is set before the call."""
        from common.llm_utils import summarize_and_classify_single_table

        cancel = threading.Event()
        cancel.set()

        summary, decision = summarize_and_classify_single_table(
            "prompt", "model", "http://llm", 512, cancel_event=cancel
        )

        assert summary == "No summary."
        assert decision is False
        mock_session.post.assert_not_called()

    def test_raises_on_http_error_after_retries(self, mock_session, mock_llm_settings):
        """Raises after all retries are exhausted on a 500 error."""
        from common.llm_utils import summarize_and_classify_single_table

        mock_session.post.side_effect = _make_http_error(500)

        with pytest.raises(requests.exceptions.HTTPError):
            summarize_and_classify_single_table(
                "prompt", "model", "http://llm", 512
            )

        # max_retries=3, so 3 POST calls should have been attempted
        assert mock_session.post.call_count == 3

    def test_retries_then_succeeds(self, mock_session, mock_llm_settings):
        """Succeeds on second attempt after one transient 500."""
        from common.llm_utils import summarize_and_classify_single_table

        good_resp = _make_response("Summary: Table ok.\nDecision: No")
        mock_session.post.side_effect = [
            _make_http_error(500),
            good_resp,
        ]

        summary, decision = summarize_and_classify_single_table(
            "prompt", "model", "http://llm", 512
        )

        assert summary == "Table ok."
        assert decision is False
        assert mock_session.post.call_count == 2

    def test_no_summary_fallback(self, mock_session, mock_llm_settings):
        """Returns 'No summary.' when LLM response has no Summary: line."""
        from common.llm_utils import summarize_and_classify_single_table

        mock_session.post.return_value = _make_response("Decision: No")

        summary, decision = summarize_and_classify_single_table(
            "prompt", "model", "http://llm", 512
        )

        assert summary == "No summary."
        assert decision is False


@pytest.mark.unit
class TestSummarizeAndClassifyTables:
    """Tests for the multi-table orchestrator."""

    def _patch_single(self, side_effect=None, return_value=None):
        """Patch summarize_and_classify_single_table directly (bypassing the decorator)."""
        target = "common.llm_utils.summarize_and_classify_single_table"
        if side_effect is not None:
            return patch(target, side_effect=side_effect)
        return patch(target, return_value=return_value)

    def test_all_tables_succeed(self):
        """All summaries and decisions are collected when every table succeeds."""
        from common.llm_utils import summarize_and_classify_tables

        with self._patch_single(return_value=("Summary", True)):
            summaries, decisions, failures = summarize_and_classify_tables(
                ["md1", "md2", "md3"],
                "model", "http://llm", "doc.pdf",
                PROMPT_TEMPLATE,
            )

        assert summaries == ["Summary", "Summary", "Summary"]
        assert decisions == [True, True, True]
        assert failures == {}

    def test_empty_input_returns_empty(self):
        """Empty table list returns empty results without error."""
        from common.llm_utils import summarize_and_classify_tables

        summaries, decisions, failures = summarize_and_classify_tables(
            [], "model", "http://llm", "doc.pdf", PROMPT_TEMPLATE
        )

        assert summaries == []
        assert decisions == []
        assert failures == {}

    def test_one_failure_stops_remaining_and_records_error(self):
        """
        When one worker raises, cancel_event is set, remaining tables are
        not processed, and the failure is recorded in the failures dict.
        """
        from common.llm_utils import summarize_and_classify_tables

        call_count = {"n": 0}

        def flaky(prompt, *args, **kwargs):
            call_count["n"] += 1
            cancel_event = kwargs.get("cancel_event")
            if cancel_event is not None and cancel_event.is_set():
                return "No summary.", False
            if call_count["n"] == 1:
                raise requests.exceptions.HTTPError("500 Server Error")
            return "Summary", True

        with patch("common.llm_utils.summarize_and_classify_single_table", side_effect=flaky):
            summaries, decisions, failures = summarize_and_classify_tables(
                ["md1", "md2", "md3", "md4", "md5"],
                "model", "http://llm", "doc.pdf",
                PROMPT_TEMPLATE,
                max_workers=1,  # sequential to make behaviour deterministic
            )

        # At least one failure must be recorded
        assert len(failures) >= 1
        # The failed table's error message should be present
        assert any("500" in msg or "Failed to process" in msg for msg in failures.values())

    def test_failure_recorded_with_table_index(self):
        """The failures dict key matches the index of the failed table."""
        from common.llm_utils import summarize_and_classify_tables

        def always_fail(prompt, *args, **kwargs):
            cancel_event = kwargs.get("cancel_event")
            if cancel_event is not None and cancel_event.is_set():
                return "No summary.", False
            raise ValueError("LLM exploded")

        with patch("common.llm_utils.summarize_and_classify_single_table", side_effect=always_fail):
            summaries, decisions, failures = summarize_and_classify_tables(
                ["md0"],
                "model", "http://llm", "doc.pdf",
                PROMPT_TEMPLATE,
                max_workers=1,
            )

        assert 0 in failures
        assert "LLM exploded" in failures[0]

    def test_cancelled_tables_skipped_gracefully(self):
        """
        When cancel_event is set after the first failure, remaining workers
        return ('No summary.', False) without raising. Output lists still
        have one entry per input table and only the triggering failure is
        recorded in failures.
        """
        from common.llm_utils import summarize_and_classify_tables

        call_count = {"n": 0}

        def one_fail_rest_cancel(prompt, *args, **kwargs):
            call_count["n"] += 1
            cancel_event = kwargs.get("cancel_event")
            if cancel_event is not None and cancel_event.is_set():
                return "No summary.", False
            raise requests.exceptions.ConnectionError("timeout")

        with patch("common.llm_utils.summarize_and_classify_single_table", side_effect=one_fail_rest_cancel):
            summaries, decisions, failures = summarize_and_classify_tables(
                ["md0", "md1", "md2"],
                "model", "http://llm", "doc.pdf",
                PROMPT_TEMPLATE,
                max_workers=1,
            )

        # The first table must be a failure
        assert 0 in failures
        # Lengths must always equal the number of input tables
        assert len(summaries) == 3
        assert len(decisions) == 3

    def test_partial_success_non_failed_tables_preserved(self):
        """Successful results that completed before the failure are preserved."""
        from common.llm_utils import summarize_and_classify_tables

        results_map = {
            0: ("Good summary", True),
        }

        def selective_fail(prompt, *args, **kwargs):
            cancel_event = kwargs.get("cancel_event")
            if cancel_event is not None and cancel_event.is_set():
                return "No summary.", False
            # Identify table by prompt content
            if "md0" in prompt:
                return results_map[0]
            raise ValueError("bad table")

        with patch("common.llm_utils.summarize_and_classify_single_table", side_effect=selective_fail):
            summaries, decisions, failures = summarize_and_classify_tables(
                ["md0", "md1"],
                "model", "http://llm", "doc.pdf",
                PROMPT_TEMPLATE,
                max_workers=1,
            )

        assert len(summaries) == 2
        assert len(decisions) == 2
        # md0 succeeded
        assert "Good summary" in summaries
        # md1 failed
        assert len(failures) >= 1
