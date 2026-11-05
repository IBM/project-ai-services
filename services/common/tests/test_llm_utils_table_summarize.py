"""
Unit tests for summarize_and_classify_single_table and summarize_and_classify_tables
in common/llm_utils.py.

Covers:
- Successful summarization and classification
- Retry behaviour on transient HTTP errors
- Non-fatal failures: failed tables use fallback values, all other tables continue
- Empty input edge case
"""

import json

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
    """Tests for the single-table worker function (guided-JSON response contract)."""

    def test_successful_summarization(self, mock_session, mock_llm_settings):
        """Returns (summary, decision) from a JSON response."""
        from common.llm_utils import summarize_and_classify_single_table

        mock_session.post.return_value = _make_response(
            json.dumps({"summary": "A data table.", "decision": "yes"})
        )

        summary, decision = summarize_and_classify_single_table(
            "prompt", "model", "http://llm", 512
        )

        assert summary == "A data table."
        assert decision is True

    def test_decision_no_returns_false(self, mock_session, mock_llm_settings):
        from common.llm_utils import summarize_and_classify_single_table

        mock_session.post.return_value = _make_response(
            json.dumps({"summary": "Prepared by John Smith.", "decision": "no"})
        )

        summary, decision = summarize_and_classify_single_table(
            "prompt", "model", "http://llm", 512
        )

        assert summary == "Prepared by John Smith."
        assert decision is False

    def test_request_is_constrained_to_json_schema(self, mock_session, mock_llm_settings):
        """The payload must carry the schema so the server enforces the output format."""
        from common.llm_utils import TABLE_SUMMARY_SCHEMA, summarize_and_classify_single_table

        mock_session.post.return_value = _make_response(
            json.dumps({"summary": "s", "decision": "yes"})
        )

        summarize_and_classify_single_table("prompt", "model", "http://llm", 512)

        payload = mock_session.post.call_args.kwargs["json"]
        assert payload["response_format"]["type"] == "json_schema"
        assert payload["response_format"]["json_schema"]["schema"] == TABLE_SUMMARY_SCHEMA
        assert payload["max_tokens"] == 512
        assert TABLE_SUMMARY_SCHEMA["required"] == ["summary", "decision"]
        assert TABLE_SUMMARY_SCHEMA["properties"]["decision"]["enum"] == ["yes", "no"]

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

        good_resp = _make_response(json.dumps({"summary": "Table ok.", "decision": "no"}))
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

    def test_empty_summary_falls_back_to_no_summary(self, mock_session, mock_llm_settings):
        from common.llm_utils import summarize_and_classify_single_table

        mock_session.post.return_value = _make_response(
            json.dumps({"summary": "  ", "decision": "no"})
        )

        summary, decision = summarize_and_classify_single_table(
            "prompt", "model", "http://llm", 512
        )

        assert summary == "No summary."
        assert decision is False

    @pytest.mark.parametrize(
        "response_text",
        [
            '{"summary": "Row one and row',  # truncated at max_tokens
            "Summary: A data table.\nDecision: yes",  # old line format is no longer accepted
            '{"summary": "x"}',  # missing decision
            '{"decision": "yes"}',  # missing summary
            "",  # empty content
        ],
    )
    def test_invalid_response_raises_and_logs_error(
        self, mock_session, mock_llm_settings, response_text
    ):
        """A response that is not the expected JSON fails loudly instead of being guessed at."""
        from common.llm_utils import summarize_and_classify_single_table

        mock_session.post.return_value = _make_response(response_text)

        with patch("common.llm_utils.logger") as mock_logger:
            with pytest.raises(ValueError, match="Invalid table summary response"):
                summarize_and_classify_single_table("prompt", "model", "http://llm", 512)

        errors = [c.args[0] for c in mock_logger.error.call_args_list]
        assert any("not the expected JSON" in e for e in errors)

    def test_parse_logs_decision_and_summary(self, mock_session, mock_llm_settings):
        """An info log records the decision, keep flag and summary size."""
        from common.llm_utils import summarize_and_classify_single_table

        mock_session.post.return_value = _make_response(
            json.dumps({"summary": "Ridership rose.", "decision": "yes"})
        )

        with patch("common.llm_utils.logger") as mock_logger:
            summarize_and_classify_single_table("prompt", "model", "http://llm", 512)

        info_msgs = [c.args[0] for c in mock_logger.info.call_args_list]
        parsed = [m for m in info_msgs if "Parsed table LLM response" in m]
        assert len(parsed) == 1
        assert "decision='yes'" in parsed[0]
        assert "keep=True" in parsed[0]
        assert "summary_chars=15" in parsed[0]
        mock_logger.error.assert_not_called()


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

    def test_one_failure_all_others_still_processed(self):
        """
        When one worker raises, the failure is recorded but remaining tables
        continue to be processed — failures are non-fatal.
        """
        from common.llm_utils import summarize_and_classify_tables

        call_count = {"n": 0}

        def flaky(prompt, *args, **kwargs):
            call_count["n"] += 1
            if call_count["n"] == 1:
                raise requests.exceptions.HTTPError("500 Server Error")
            return "Summary", True

        with patch("common.llm_utils.summarize_and_classify_single_table", side_effect=flaky):
            summaries, decisions, failures = summarize_and_classify_tables(
                ["md1", "md2", "md3", "md4", "md5"],
                "model", "http://llm", "doc.pdf",
                PROMPT_TEMPLATE,
                max_workers=1,
            )

        # All 5 tables must be represented in output
        assert len(summaries) == 5
        assert len(decisions) == 5
        # Exactly one failure recorded
        assert len(failures) == 1
        assert any("500" in msg or "Failed to process" in msg for msg in failures.values())

    def test_failure_recorded_with_table_index(self):
        """The failures dict key matches the index of the failed table."""
        from common.llm_utils import summarize_and_classify_tables

        def always_fail(prompt, *args, **kwargs):
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

    def test_failed_table_uses_fallback_values(self):
        """
        A failed table gets ('No summary.', False) as its fallback values
        and processing continues for all remaining tables.
        """
        from common.llm_utils import summarize_and_classify_tables

        def fail_first(prompt, *args, **kwargs):
            if "md0" in prompt:
                raise ValueError("bad table")
            return "Good summary", True

        with patch("common.llm_utils.summarize_and_classify_single_table", side_effect=fail_first):
            summaries, decisions, failures = summarize_and_classify_tables(
                ["md0", "md1", "md2"],
                "model", "http://llm", "doc.pdf",
                PROMPT_TEMPLATE,
                max_workers=1,
            )

        assert len(summaries) == 3
        assert len(decisions) == 3
        # Failed table gets fallback
        assert summaries[0] == "No summary."
        assert decisions[0] is False
        # Other tables processed normally
        assert summaries[1] == "Good summary"
        assert summaries[2] == "Good summary"
        assert len(failures) == 1

    def test_partial_success_non_failed_tables_preserved(self):
        """Successful results are preserved alongside failures."""
        from common.llm_utils import summarize_and_classify_tables

        def selective_fail(prompt, *args, **kwargs):
            if "md0" in prompt:
                return "Good summary", True
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
        assert "Good summary" in summaries
        assert len(failures) == 1
