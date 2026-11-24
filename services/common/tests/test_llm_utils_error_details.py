"""
Unit tests for raise_for_status_with_details / extract_error_message in common/llm_utils.py.

Verifies that the descriptive upstream error (e.g. from LiteLLM/watsonx) is surfaced
instead of only the generic "400 Client Error: Bad Request for url: ..." message.
"""

import json

import pytest
import requests

from common.llm_utils import extract_error_message, raise_for_status_with_details


def _make_response(status_code: int, body, url: str = "http://localhost:4100/tokenize") -> requests.Response:
    resp = requests.Response()
    resp.status_code = status_code
    resp.reason = "Bad Request"
    resp.url = url
    resp._content = body if isinstance(body, bytes) else json.dumps(body).encode()
    return resp


WATSONX_INNER = {
    "errors": [
        {
            "code": "json_validation_error",
            "message": "Json document validation error: project_id should be a version 4 uuid ",
            "more_info": "https://cloud.ibm.com/apidocs/watsonx-ai#text-tokenization",
        }
    ],
    "trace": "3bfbed448a26a32ff9e3bd7ebab4b818",
    "status_code": 400,
}
LITELLM_BODY = {"error": {"message": json.dumps(WATSONX_INNER), "type": "None", "param": "None", "code": "400"}}


@pytest.mark.unit
class TestExtractErrorMessage:
    def test_nested_litellm_watsonx_error(self):
        msg = extract_error_message(_make_response(400, LITELLM_BODY))
        assert "project_id should be a version 4 uuid" in msg

    def test_openai_style_error(self):
        msg = extract_error_message(_make_response(400, {"error": {"message": "bad model"}}))
        assert msg == "bad model"

    def test_fastapi_detail(self):
        assert extract_error_message(_make_response(404, {"detail": "not found"})) == "not found"

    def test_non_json_body_falls_back_to_text(self):
        assert extract_error_message(_make_response(502, b"upstream down")) == "upstream down"

    def test_truncates_long_messages(self):
        msg = extract_error_message(_make_response(500, b"x" * 5000))
        assert len(msg) == 500

    def test_empty_body(self):
        assert extract_error_message(_make_response(500, b"")) == ""


@pytest.mark.unit
class TestRaiseForStatusWithDetails:
    def test_success_does_not_raise(self):
        raise_for_status_with_details(_make_response(200, {"ok": True}))

    def test_error_includes_upstream_message_and_keeps_response(self):
        resp = _make_response(400, LITELLM_BODY)
        with pytest.raises(requests.exceptions.HTTPError) as exc_info:
            raise_for_status_with_details(resp)

        err = exc_info.value
        assert "400 Client Error" in str(err)
        assert "project_id should be a version 4 uuid" in str(err)
        assert err.response is resp
        assert err.response.status_code == 400

    def test_empty_body_re_raises_original(self):
        with pytest.raises(requests.exceptions.HTTPError) as exc_info:
            raise_for_status_with_details(_make_response(500, b""))
        assert "Upstream error" not in str(exc_info.value)
