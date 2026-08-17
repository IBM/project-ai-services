"""
Unit tests for POST /v1/extract — synchronous extraction endpoint.

Covers:
  - ExtractException (class definition + handler serialisation)
  - Helper functions: strip_markdown_fences, render_few_shot_block,
    build_messages, call_vllm, validate_output
  - ExtractionRequest / ExtractionResponse Pydantic models
  - settings.extract.max_request_body_bytes
  - Full endpoint: happy path, all error codes, semaphore, retry logic,
    finish_reason=length fast-fail, body-size guard
"""

import json
from datetime import datetime, timezone
from unittest.mock import AsyncMock, Mock, patch

import pytest
import requests as requests_lib

from extract.utils.exceptions import ExtractException
from extract.utils.vllm import (
    build_messages,
    render_few_shot_block,
    strip_markdown_fences,
    validate_output,
)
from extract.models import ExtractionRequest, ExtractionResponse


# ---------------------------------------------------------------------------
# Shared fixtures
# ---------------------------------------------------------------------------

SIMPLE_SCHEMA = {
    "type": "object",
    "properties": {
        "invoice_number": {"type": "string"},
        "vendor_name": {"type": "string"},
        "total_amount": {"type": "number"},
    },
    "required": ["invoice_number", "vendor_name", "total_amount"],
}

VALID_EXTRACTION = {
    "invoice_number": "INV-001",
    "vendor_name": "Acme",
    "total_amount": 100.0,
}


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
    row.json_schema = json_schema or SIMPLE_SCHEMA
    row.examples = examples
    row.custom_prompt = custom_prompt
    row.schema_tokens = schema_tokens
    row.examples_tokens = examples_tokens
    row.custom_prompt_tokens = custom_prompt_tokens
    row.created_at = datetime(2026, 7, 7, tzinfo=timezone.utc)
    return row


def _vllm_response(content: str, finish_reason: str = "stop", prompt_tokens=500, completion_tokens=80):
    """Build a minimal vLLM /v1/chat/completions response dict."""
    return {
        "choices": [
            {
                "message": {"role": "assistant", "content": content},
                "finish_reason": finish_reason,
            }
        ],
        "usage": {
            "prompt_tokens": prompt_tokens,
            "completion_tokens": completion_tokens,
        },
    }


# ---------------------------------------------------------------------------
# ExtractException — class + handler
# ---------------------------------------------------------------------------

class TestExtractException:
    def test_attributes_set_correctly(self):
        exc = ExtractException(409, "MY_CODE", "something went wrong")
        assert exc.status_code == 409
        assert exc.code == "MY_CODE"
        assert exc.message == "something went wrong"
        assert exc.details == {}

    def test_details_default_empty_dict(self):
        exc = ExtractException(400, "C", "m")
        assert exc.details == {}

    def test_details_populated(self):
        exc = ExtractException(422, "C", "m", details={"x": 1})
        assert exc.details == {"x": 1}

    def test_is_exception(self):
        exc = ExtractException(400, "C", "m")
        assert isinstance(exc, Exception)

    def test_handler_serialises_to_json(self, extract_test_client):
        """The app exception handler returns the canonical error envelope."""
        # GET /v1/extract/jobs/some-id is a real endpoint; mock DB to return None → 404.
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=None):
            resp = extract_test_client.get("/v1/extract/jobs/some-id")
        assert resp.status_code == 404
        body = resp.json()
        assert body["error"]["code"] == "RESOURCE_NOT_FOUND"
        assert body["error"]["status"] == 404

    def test_handler_includes_details_when_present(self, extract_test_client):
        """When ExtractException carries details, the handler must include them."""
        with patch(
            "extract.api.v1.jobs.db_repo.get_schema_by_id",
            side_effect=ExtractException(400, "DEMO", "demo", details={"k": "v"}),
        ):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "hello", "schema_id": "x"},
            )
        assert "details" in resp.json()["error"]
        assert resp.json()["error"]["details"]["k"] == "v"


# ---------------------------------------------------------------------------
# Helper: strip_markdown_fences
# ---------------------------------------------------------------------------

class TestStripMarkdownFences:
    def test_no_fences_unchanged(self):
        assert strip_markdown_fences('{"a": 1}') == '{"a": 1}'

    def test_json_fence_stripped(self):
        text = "```json\n{\"a\": 1}\n```"
        assert strip_markdown_fences(text) == '{"a": 1}'

    def test_plain_fence_stripped(self):
        text = "```\n{\"a\": 1}\n```"
        assert strip_markdown_fences(text) == '{"a": 1}'

    def test_leading_trailing_whitespace_stripped(self):
        assert strip_markdown_fences("   {}   ") == "{}"

    def test_fence_case_insensitive(self):
        text = "```JSON\n{}\n```"
        assert strip_markdown_fences(text) == "{}"

    def test_empty_string(self):
        assert strip_markdown_fences("") == ""

    def test_partial_fence_no_close(self):
        result = strip_markdown_fences("```json\n{}")
        assert result == "{}"


# ---------------------------------------------------------------------------
# Helper: render_few_shot_block
# ---------------------------------------------------------------------------

class TestRenderFewShotBlock:
    def test_none_returns_empty(self):
        assert render_few_shot_block(None) == ""

    def test_empty_list_returns_empty(self):
        assert render_few_shot_block([]) == ""

    def test_single_example_renders_correctly(self):
        examples = [{"text": "Hello world", "output": {"field": "value"}}]
        result = render_few_shot_block(examples)
        assert "Example text:\nHello world" in result
        assert 'Example JSON:\n{"field": "value"}' in result

    def test_multiple_examples_separated_by_blank_line(self):
        examples = [
            {"text": "t1", "output": {"a": 1}},
            {"text": "t2", "output": {"b": 2}},
        ]
        result = render_few_shot_block(examples)
        assert "\n\n" in result
        assert "t1" in result
        assert "t2" in result

    def test_output_is_json_serialised(self):
        examples = [{"text": "t", "output": {"x": True, "y": None}}]
        result = render_few_shot_block(examples)
        assert '"x": true' in result
        assert '"y": null' in result


# ---------------------------------------------------------------------------
# Helper: build_messages
# ---------------------------------------------------------------------------

class TestBuildMessages:
    def test_returns_system_and_user_messages(self):
        msgs = build_messages(SIMPLE_SCHEMA, "", "some text", None)
        roles = [m["role"] for m in msgs]
        assert roles == ["system", "user"]

    def test_system_contains_extraction_instructions(self):
        msgs = build_messages(SIMPLE_SCHEMA, "", "text", None)
        sys_content = msgs[0]["content"]
        assert "extraction assistant" in sys_content.lower()
        assert "JSON" in sys_content

    def test_custom_prompt_appended_to_system(self):
        msgs = build_messages(SIMPLE_SCHEMA, "", "text", "Use ISO 8601 for dates.")
        assert "ISO 8601" in msgs[0]["content"]

    def test_no_custom_prompt_placeholder_empty(self):
        msgs = build_messages(SIMPLE_SCHEMA, "", "text", None)
        assert "{custom_prompt}" not in msgs[0]["content"]

    def test_user_prompt_contains_schema(self):
        msgs = build_messages(SIMPLE_SCHEMA, "", "text", None)
        assert "invoice_number" in msgs[1]["content"]

    def test_user_prompt_contains_input_text(self):
        msgs = build_messages(SIMPLE_SCHEMA, "", "THE INPUT TEXT", None)
        assert "THE INPUT TEXT" in msgs[1]["content"]

    def test_user_prompt_contains_few_shot_block(self):
        few_shot = "Example text:\nhello\nExample JSON:\n{}"
        msgs = build_messages(SIMPLE_SCHEMA, few_shot, "text", None)
        assert few_shot in msgs[1]["content"]

    def test_user_prompt_ends_with_json_cue(self):
        msgs = build_messages(SIMPLE_SCHEMA, "", "text", None)
        assert msgs[1]["content"].rstrip().endswith("JSON:")


# ---------------------------------------------------------------------------
# Helper: validate_output
# ---------------------------------------------------------------------------

class TestValidateOutput:
    def test_valid_json_passes(self):
        raw = json.dumps(VALID_EXTRACTION)
        result = validate_output(raw, SIMPLE_SCHEMA)
        assert result == VALID_EXTRACTION

    def test_markdown_fences_stripped_before_parse(self):
        raw = f"```json\n{json.dumps(VALID_EXTRACTION)}\n```"
        result = validate_output(raw, SIMPLE_SCHEMA)
        assert result == VALID_EXTRACTION

    def test_invalid_json_raises_value_error(self):
        with pytest.raises(ValueError, match="not valid JSON"):
            validate_output("this is not json", SIMPLE_SCHEMA)

    def test_schema_violation_raises_value_error(self):
        bad = {"invoice_number": "INV-1"}  # missing required vendor_name, total_amount
        with pytest.raises(ValueError):
            validate_output(json.dumps(bad), SIMPLE_SCHEMA)

    def test_wrong_type_raises_value_error(self):
        bad = {**VALID_EXTRACTION, "total_amount": "not-a-number"}
        with pytest.raises(ValueError):
            validate_output(json.dumps(bad), SIMPLE_SCHEMA)

    def test_extra_properties_allowed_by_default(self):
        with_extra = {**VALID_EXTRACTION, "unexpected_field": "ok"}
        result = validate_output(json.dumps(with_extra), SIMPLE_SCHEMA)
        assert result["unexpected_field"] == "ok"


# ---------------------------------------------------------------------------
# ExtractionRequest model
# ---------------------------------------------------------------------------

class TestExtractionRequestModel:
    def test_valid_fields_accepted(self):
        req = ExtractionRequest(text="hello", schema_id="abc-123")
        assert req.text == "hello"
        assert req.schema_id == "abc-123"

    def test_empty_text_rejected(self):
        with pytest.raises(Exception):
            ExtractionRequest(text="", schema_id="abc")

    def test_empty_schema_id_rejected(self):
        with pytest.raises(Exception):
            ExtractionRequest(text="hello", schema_id="")

    def test_missing_text_rejected(self):
        with pytest.raises(Exception):
            ExtractionRequest(schema_id="abc")

    def test_missing_schema_id_rejected(self):
        with pytest.raises(Exception):
            ExtractionRequest(text="hello")


# ---------------------------------------------------------------------------
# settings.extract.max_request_body_bytes
# ---------------------------------------------------------------------------

class TestMaxRequestBodyBytesSetting:
    def test_default_is_one_mib(self):
        from extract.settings import settings
        assert settings.extract.max_request_body_bytes == 1_048_576

    def test_can_be_overridden(self, monkeypatch):
        monkeypatch.setenv("MAX_REQUEST_BODY_BYTES", "2097152")
        from extract.settings import ExtractionConfig
        cfg = ExtractionConfig()
        assert cfg.max_request_body_bytes == 2_097_152


# ---------------------------------------------------------------------------
# POST /v1/extract — endpoint integration tests
# ---------------------------------------------------------------------------

class TestExtractSyncEndpoint:

    # ── happy path ────────────────────────────────────────────────────────

    def test_200_happy_path(self, extract_test_client, monkeypatch):
        schema_row = _mock_schema_row()
        good_json = json.dumps(VALID_EXTRACTION)

        _setup_happy_path(monkeypatch, schema_row, input_tokens=200, raw_output=good_json)

        resp = extract_test_client.post(
            "/v1/extract",
            json={"text": "INVOICE INV-001 Acme 100.00", "schema_id": "schema-001"},
        )

        assert resp.status_code == 200
        body = resp.json()
        assert body["data"]["extraction"] == VALID_EXTRACTION
        assert body["data"]["schema_id"] == "schema-001"
        assert body["data"]["source"]["input_type"] == "text"
        assert body["data"]["source"]["input_tokens"] == 200
        assert "model" in body["meta"]
        assert "processing_time_ms" in body["meta"]
        assert body["meta"]["validation_attempts"] == 1
        assert "input_tokens" in body["usage"]
        assert "output_tokens" in body["usage"]
        assert "total_tokens" in body["usage"]

    def test_200_with_examples_and_custom_prompt(self, extract_test_client, monkeypatch):
        examples = [{"text": "sample", "output": VALID_EXTRACTION}]
        schema_row = _mock_schema_row(
            examples=examples,
            custom_prompt="Always use EUR for currency.",
            examples_tokens=40,
            custom_prompt_tokens=10,
        )
        good_json = json.dumps(VALID_EXTRACTION)
        _setup_happy_path(monkeypatch, schema_row, input_tokens=150, raw_output=good_json)

        resp = extract_test_client.post(
            "/v1/extract",
            json={"text": "some invoice text", "schema_id": "schema-001"},
        )
        assert resp.status_code == 200

    # ── 400 Bad Request ───────────────────────────────────────────────────

    def test_400_invalid_json_body(self, extract_test_client):
        resp = extract_test_client.post(
            "/v1/extract",
            content=b"not json at all",
            headers={"Content-Type": "application/json"},
        )
        assert resp.status_code == 422

    def test_400_missing_schema_id(self, extract_test_client):
        resp = extract_test_client.post("/v1/extract", json={"text": "hello"})
        assert resp.status_code == 422
        assert resp.json()["detail"][0]["type"] == "missing"

    def test_400_missing_text(self, extract_test_client):
        resp = extract_test_client.post("/v1/extract", json={"schema_id": "abc"})
        assert resp.status_code == 422
        assert resp.json()["detail"][0]["type"] == "missing"

    def test_400_empty_text(self, extract_test_client):
        resp = extract_test_client.post(
            "/v1/extract",
            json={"text": "", "schema_id": "abc"},
        )
        assert resp.status_code == 422
        assert resp.json()["detail"][0]["type"] == "string_too_short"

    # ── 404 Not Found ─────────────────────────────────────────────────────

    def test_404_unknown_schema(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=None):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "hello", "schema_id": "nonexistent"},
            )
        assert resp.status_code == 404
        assert resp.json()["error"]["code"] == "SCHEMA_NOT_FOUND"

    # ── 413 Request body too large ────────────────────────────────────────

    def test_413_content_length_header_exceeds_limit(self, extract_test_client, monkeypatch):
        monkeypatch.setattr(
            "extract.utils.request.settings.extract.max_request_body_bytes", 100
        )
        resp = extract_test_client.post(
            "/v1/extract",
            json={"text": "x" * 200, "schema_id": "abc"},
            headers={"Content-Length": "10000"},
        )
        assert resp.status_code == 413
        assert resp.json()["error"]["code"] == "REQUEST_TOO_LARGE"

    def test_413_body_exceeds_limit_without_header(self, extract_test_client, monkeypatch):
        monkeypatch.setattr(
            "extract.utils.request.settings.extract.max_request_body_bytes", 100
        )
        resp = extract_test_client.post(
            "/v1/extract",
            json={"text": "x" * 200, "schema_id": "abc"},
        )
        assert resp.status_code == 413
        assert resp.json()["error"]["code"] == "REQUEST_TOO_LARGE"

    def test_413_details_include_limit(self, extract_test_client, monkeypatch):
        monkeypatch.setattr(
            "extract.utils.request.settings.extract.max_request_body_bytes", 100
        )
        resp = extract_test_client.post(
            "/v1/extract",
            json={"text": "x" * 200, "schema_id": "abc"},
        )
        assert resp.json()["error"]["details"]["max_request_body_bytes"] == 100

    # ── 413 Context limit exceeded ────────────────────────────────────────

    def test_413_context_limit_exceeded(self, extract_test_client, monkeypatch):
        schema_row = _mock_schema_row(schema_tokens=500, examples_tokens=300)

        # check_extraction_budget raises ExtractException(413); the endpoint
        # re-raises it — the ExtractException handler fires.
        from extract.utils.exceptions import ExtractException as _ExtractException
        context_err = _ExtractException(
            413,
            "CONTEXT_LIMIT_EXCEEDED",
            "Input does not fit in the model context window.",
            details={
                "max_model_len": 32768,
                "input_tokens": 30000,
                "schema_tokens": 500,
                "examples_tokens": 300,
                "custom_prompt_tokens": 0,
                "prompt_overhead_tokens": 150,
                "reserved_output_tokens": 1000,
                "total_required_tokens": 31950,
                "excess_tokens": 0,
            },
        )

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             patch("extract.api.v1.jobs.asyncio.to_thread", new=AsyncMock(return_value=30000)), \
             patch("extract.api.v1.jobs.check_extraction_budget", side_effect=context_err):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "x" * 1000, "schema_id": "schema-001"},
            )

        assert resp.status_code == 413
        body = resp.json()["error"]
        assert body["code"] == "CONTEXT_LIMIT_EXCEEDED"
        assert "input_tokens" in body["details"]
        assert "schema_tokens" in body["details"]
        assert "examples_tokens" in body["details"]
        assert "custom_prompt_tokens" in body["details"]
        assert "prompt_overhead_tokens" in body["details"]
        assert "reserved_output_tokens" in body["details"]

    # ── 413 Output budget exceeded (finish_reason=length) ─────────────────

    def test_413_output_budget_exceeded_retries_then_fails(self, extract_test_client, monkeypatch):
        """finish_reason=length triggers one retry with boosted budget; 413 if retry also truncates."""
        schema_row = _mock_schema_row()
        length_resp = _vllm_response("partial output...", finish_reason="length")
        vllm_calls = {"n": 0}

        def _fake_call_vllm(*args, **kwargs):
            vllm_calls["n"] += 1
            return length_resp

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.api.v1.jobs.compute_reserved_output", return_value=768), \
             patch("extract.utils.vllm.call_vllm", side_effect=_fake_call_vllm):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "some text", "schema_id": "schema-001"},
            )

        assert resp.status_code == 413
        body = resp.json()["error"]
        assert body["code"] == "OUTPUT_BUDGET_EXCEEDED"
        assert body["details"]["finish_reason"] == "length"
        assert vllm_calls["n"] == 2  # initial + one boosted retry

    def test_413_output_budget_has_boosted_reserved_tokens_in_details(self, extract_test_client, monkeypatch):
        """The reserved_output_tokens in the 413 details reflects the boosted retry budget."""
        schema_row = _mock_schema_row()
        length_resp = _vllm_response("truncated...", finish_reason="length")

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.api.v1.jobs.compute_reserved_output", return_value=768), \
             patch("extract.utils.vllm.call_vllm", return_value=length_resp):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "x", "schema_id": "schema-001"},
            )

        assert resp.json()["error"]["details"]["reserved_output_tokens"] == 768

    def test_200_output_budget_length_then_success(self, extract_test_client, monkeypatch):
        """First call returns finish_reason=length; boosted retry succeeds → 200."""
        schema_row = _mock_schema_row()
        valid_json = json.dumps(VALID_EXTRACTION)
        calls = {"n": 0}

        def _fake_call_vllm(*args, **kwargs):
            calls["n"] += 1
            if calls["n"] == 1:
                return _vllm_response("truncated...", finish_reason="length")
            return _vllm_response(valid_json)

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.api.v1.jobs.compute_reserved_output", return_value=768), \
             patch("extract.utils.vllm.call_vllm", side_effect=_fake_call_vllm):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "some text", "schema_id": "schema-001"},
            )

        assert resp.status_code == 200
        assert resp.json()["data"]["extraction"] == VALID_EXTRACTION
        assert calls["n"] == 2

    # ── 422 Validation failed ─────────────────────────────────────────────

    def test_422_validation_fails_after_retry(self, extract_test_client, monkeypatch):
        """Both initial and retry outputs fail validation → 422."""
        schema_row = _mock_schema_row()
        bad_json = json.dumps({"invoice_number": "X"})  # missing required fields
        vllm_calls = {"n": 0}

        def _fake_call_vllm(*args, **kwargs):
            vllm_calls["n"] += 1
            return _vllm_response(bad_json)

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.utils.vllm.call_vllm", side_effect=_fake_call_vllm):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "some text", "schema_id": "schema-001"},
            )

        assert resp.status_code == 422
        body = resp.json()["error"]
        assert body["code"] == "EXTRACTION_VALIDATION_FAILED"
        assert "validation_errors" in body["details"]
        assert "raw_output" in body["details"]
        assert vllm_calls["n"] == 2  # initial + one retry

    def test_422_retry_output_in_details(self, extract_test_client, monkeypatch):
        schema_row = _mock_schema_row()
        bad_raw = '{"invoice_number": "X"}'

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.utils.vllm.call_vllm", return_value=_vllm_response(bad_raw)):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "x", "schema_id": "schema-001"},
            )
        assert resp.json()["error"]["details"]["raw_output"] == bad_raw

    def test_validation_attempts_2_when_retry_succeeds(self, extract_test_client, monkeypatch):
        """First call returns invalid JSON; retry returns valid JSON → 200, attempts=2."""
        schema_row = _mock_schema_row()
        calls = {"n": 0}

        def _fake_call_vllm(*args, **kwargs):
            calls["n"] += 1
            if calls["n"] == 1:
                return _vllm_response('{"invoice_number": "X"}')
            return _vllm_response(json.dumps(VALID_EXTRACTION))

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.utils.vllm.call_vllm", side_effect=_fake_call_vllm):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "some text", "schema_id": "schema-001"},
            )

        assert resp.status_code == 200
        assert resp.json()["meta"]["validation_attempts"] == 2
        assert resp.json()["data"]["extraction"] == VALID_EXTRACTION

    def test_finish_reason_length_on_retry_returns_413(self, extract_test_client, monkeypatch):
        """First call produces invalid JSON; retry truncates → 413, not 422."""
        schema_row = _mock_schema_row()
        calls = {"n": 0}

        def _fake_call_vllm(*args, **kwargs):
            calls["n"] += 1
            if calls["n"] == 1:
                return _vllm_response('{"invoice_number": "X"}')  # invalid → triggers retry
            return _vllm_response("...", finish_reason="length")   # truncated on retry

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.utils.vllm.call_vllm", side_effect=_fake_call_vllm):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "x", "schema_id": "schema-001"},
            )

        assert resp.status_code == 413
        assert resp.json()["error"]["code"] == "OUTPUT_BUDGET_EXCEEDED"

    # ── 429 Too Many Requests ─────────────────────────────────────────────

    def test_429_when_semaphore_locked(self, extract_test_client, monkeypatch):
        schema_row = _mock_schema_row()
        locked_semaphore = Mock()
        locked_semaphore.locked.return_value = True

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             patch("extract.api.v1.jobs.concurrency_limiter", locked_semaphore):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "some text", "schema_id": "schema-001"},
            )

        assert resp.status_code == 429
        assert resp.json()["error"]["code"] == "RATE_LIMIT_EXCEEDED"

    # ── 503 Service Unavailable ───────────────────────────────────────────

    def test_503_tokenization_error(self, extract_test_client, monkeypatch):
        schema_row = _mock_schema_row()

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             patch(
                 "extract.api.v1.jobs.asyncio.to_thread",
                 new=AsyncMock(side_effect=ConnectionError("tokenize endpoint down")),
             ):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "hello", "schema_id": "schema-001"},
            )

        assert resp.status_code == 503
        assert resp.json()["error"]["code"] == "TOKENIZATION_ERROR"

    def test_503_vllm_connection_error(self, extract_test_client, monkeypatch):
        schema_row = _mock_schema_row()

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch(
                 "extract.utils.vllm.call_vllm",
                 side_effect=requests_lib.exceptions.ConnectionError("unreachable"),
             ):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "hello", "schema_id": "schema-001"},
            )

        assert resp.status_code == 503
        assert resp.json()["error"]["code"] == "LLM_UNAVAILABLE"

    def test_503_vllm_connection_error_on_retry(self, extract_test_client, monkeypatch):
        """vLLM goes down while executing the validation retry."""
        schema_row = _mock_schema_row()
        calls = {"n": 0}

        def _fake_call_vllm(*args, **kwargs):
            calls["n"] += 1
            if calls["n"] == 1:
                return _vllm_response('{"invoice_number": "X"}')
            raise requests_lib.exceptions.ConnectionError("unreachable")

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.utils.vllm.call_vllm", side_effect=_fake_call_vllm):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "x", "schema_id": "schema-001"},
            )

        assert resp.status_code == 503
        assert resp.json()["error"]["code"] == "LLM_UNAVAILABLE"

    # ── 500 Internal Server Error ─────────────────────────────────────────

    def test_500_vllm_http_error(self, extract_test_client, monkeypatch):
        schema_row = _mock_schema_row()
        http_err = requests_lib.exceptions.HTTPError()
        http_err.response = Mock(status_code=500)

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.utils.vllm.call_vllm", side_effect=http_err):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "hello", "schema_id": "schema-001"},
            )

        assert resp.status_code == 500
        assert resp.json()["error"]["code"] == "LLM_ERROR"

    def test_500_empty_choices(self, extract_test_client, monkeypatch):
        schema_row = _mock_schema_row()

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.utils.vllm.call_vllm", return_value={"choices": [], "usage": {}}):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "x", "schema_id": "schema-001"},
            )

        assert resp.status_code == 500
        assert resp.json()["error"]["code"] == "LLM_ERROR"

    # ── Guided decoding toggle ─────────────────────────────────────────────

    def test_guided_json_included_when_enabled(self, extract_test_client, monkeypatch):
        schema_row = _mock_schema_row()
        captured = {}

        def _fake_call_vllm(messages, max_tokens, normalized_schema, llm_endpoint, llm_model):
            captured["schema"] = normalized_schema
            return _vllm_response(json.dumps(VALID_EXTRACTION))

        monkeypatch.setattr("extract.utils.vllm.settings.extract.guided_decoding_enabled", True)

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.utils.vllm.call_vllm", side_effect=_fake_call_vllm):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "some text", "schema_id": "schema-001"},
            )

        assert resp.status_code == 200
        assert captured["schema"] == SIMPLE_SCHEMA

    def test_guided_json_absent_when_disabled(self, extract_test_client, monkeypatch):
        """When GUIDED_DECODING_ENABLED=false, extra_body must not be set."""
        import common.misc_utils as misc_utils

        schema_row = _mock_schema_row()
        captured_payload: dict = {}

        def _mock_post(url, json=None, headers=None, stream=False):
            captured_payload.update(json or {})
            mock_resp = Mock()
            mock_resp.raise_for_status = Mock()
            mock_resp.json.return_value = _vllm_response(json_module_dumps(VALID_EXTRACTION))
            return mock_resp

        monkeypatch.setattr("extract.utils.vllm.settings.extract.guided_decoding_enabled", False)
        fake_session = Mock()
        fake_session.post.side_effect = _mock_post
        monkeypatch.setattr(misc_utils, "SESSION", fake_session)

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "some text", "schema_id": "schema-001"},
            )

        assert resp.status_code == 200
        assert "extra_body" not in captured_payload

    # ── Token usage ────────────────────────────────────────────────────────

    def test_usage_reflects_vllm_token_counts(self, extract_test_client, monkeypatch):
        schema_row = _mock_schema_row()
        vllm_resp = _vllm_response(json.dumps(VALID_EXTRACTION), prompt_tokens=400, completion_tokens=60)

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.utils.vllm.call_vllm", return_value=vllm_resp):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "x", "schema_id": "schema-001"},
            )

        usage = resp.json()["usage"]
        assert usage["input_tokens"] == 400
        assert usage["output_tokens"] == 60
        assert usage["total_tokens"] == 460

    def test_usage_accumulates_across_retry(self, extract_test_client, monkeypatch):
        """Token counts from initial and retry calls must be summed."""
        schema_row = _mock_schema_row()
        calls = {"n": 0}

        def _fake_call_vllm(*args, **kwargs):
            calls["n"] += 1
            if calls["n"] == 1:
                return _vllm_response('{"invoice_number": "X"}', prompt_tokens=300, completion_tokens=50)
            return _vllm_response(json.dumps(VALID_EXTRACTION), prompt_tokens=400, completion_tokens=80)

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=schema_row), \
             _patch_concurrency_free(), \
             _patch_to_thread(), \
             patch("extract.api.v1.jobs.check_extraction_budget", return_value=512), \
             patch("extract.utils.vllm.call_vllm", side_effect=_fake_call_vllm):
            resp = extract_test_client.post(
                "/v1/extract",
                json={"text": "some text", "schema_id": "schema-001"},
            )

        assert resp.status_code == 200
        usage = resp.json()["usage"]
        assert usage["input_tokens"] == 700
        assert usage["output_tokens"] == 130
        assert usage["total_tokens"] == 830


# ---------------------------------------------------------------------------
# Module-level patch helpers
# ---------------------------------------------------------------------------

def _patch_concurrency_free():
    """Patch concurrency_limiter so locked() is False and async-with passes."""
    import asyncio
    return patch("extract.api.v1.jobs.concurrency_limiter", asyncio.BoundedSemaphore(1))


def _patch_to_thread(token_count: int = 200):
    """Return a context-manager patch for asyncio.to_thread that:
    - returns *token_count* for the first call (_tokenize)
    - calls fn(*args) directly on all subsequent calls (_call_vllm and retry)
    """
    _call_count = {"n": 0}

    async def _dispatch(fn, *args, **kwargs):
        _call_count["n"] += 1
        if _call_count["n"] == 1:
            return token_count
        return fn(*args, **kwargs)

    return patch("extract.api.v1.jobs.asyncio.to_thread", new=_dispatch)


def _setup_happy_path(monkeypatch, schema_row, input_tokens: int, raw_output: str):
    import asyncio as _asyncio

    monkeypatch.setattr("extract.api.v1.jobs.db_repo.get_schema_by_id", Mock(return_value=schema_row))
    monkeypatch.setattr("extract.api.v1.jobs.check_extraction_budget", Mock(return_value=512))
    monkeypatch.setattr("extract.utils.vllm.call_vllm", Mock(return_value=_vllm_response(raw_output)))
    monkeypatch.setattr("extract.api.v1.jobs.concurrency_limiter", _asyncio.BoundedSemaphore(1))

    _call_count = {"n": 0}

    async def _to_thread_side_effect(fn, *args, **kwargs):
        _call_count["n"] += 1
        if _call_count["n"] == 1:
            return input_tokens
        return fn(*args, **kwargs)

    monkeypatch.setattr("extract.api.v1.jobs.asyncio.to_thread", _to_thread_side_effect)


# Alias to avoid shadowing the `json` parameter name inside _mock_post
json_module_dumps = json.dumps
