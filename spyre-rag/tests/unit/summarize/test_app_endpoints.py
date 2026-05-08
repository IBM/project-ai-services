import asyncio
from unittest.mock import AsyncMock, Mock, patch

import pytest

from summarize.summ_utils import SummarizeException


@pytest.mark.unit
class TestHealthAndDocs:
    def test_health_returns_ok(self, summarize_test_client):
        response = summarize_test_client.get("/health")

        assert response.status_code == 200
        assert response.json() == {"status": "ok"}

    def test_root_returns_swagger_ui(self, summarize_test_client):
        response = summarize_test_client.get("/")

        assert response.status_code == 200
        assert "Swagger UI" in response.text


@pytest.mark.unit
class TestRequestIdMiddleware:
    def test_existing_request_id_is_echoed(self, summarize_test_client):
        with patch("summarize.app.set_request_id") as mock_set_request_id:
            response = summarize_test_client.get("/health", headers={"X-Request-ID": "req-123"})

        assert response.status_code == 200
        assert response.headers["X-Request-ID"] == "req-123"
        mock_set_request_id.assert_called_once_with("req-123")

    def test_missing_request_id_is_generated(self, summarize_test_client):
        with patch("summarize.app.set_request_id") as mock_set_request_id:
            response = summarize_test_client.get("/health")

        assert response.status_code == 200
        assert "X-Request-ID" in response.headers
        assert response.headers["X-Request-ID"]
        mock_set_request_id.assert_called_once()


@pytest.mark.unit
class TestSummarizeJsonRoute:
    def test_json_text_only_calls_handle_summarize(self, summarize_test_client):
        mocked_response = {
            "data": {"summary": "summary", "original_length": 10, "summary_length": 1},
            "meta": {"model": "test-llm-model", "processing_time_ms": 10, "input_type": "text"},
            "usage": {"input_tokens": 5, "output_tokens": 2, "total_tokens": 7},
        }

        with patch("summarize.app.handle_summarize", new=AsyncMock(return_value=mocked_response)) as mock_handle:
            response = summarize_test_client.post("/v1/summarize", json={"text": "some content"})

        assert response.status_code == 200
        assert response.json() == mocked_response
        mock_handle.assert_called_once_with("some content", "text", None, None, False)

    def test_json_with_level_calls_handle_summarize(self, summarize_test_client):
        mocked_response = {
            "data": {"summary": "summary", "original_length": 10, "summary_length": 1},
            "meta": {"model": "test-llm-model", "processing_time_ms": 10, "input_type": "text"},
            "usage": {"input_tokens": 5, "output_tokens": 2, "total_tokens": 7},
        }

        with patch("summarize.app.handle_summarize", new=AsyncMock(return_value=mocked_response)) as mock_handle:
            response = summarize_test_client.post(
                "/v1/summarize",
                json={"text": "some content", "level": "brief"},
            )

        assert response.status_code == 200
        mock_handle.assert_called_once_with("some content", "text", None, "brief", False)

    def test_json_with_length_calls_handle_summarize(self, summarize_test_client):
        mocked_response = {
            "data": {"summary": "summary", "original_length": 10, "summary_length": 1},
            "meta": {"model": "test-llm-model", "processing_time_ms": 10, "input_type": "text"},
            "usage": {"input_tokens": 5, "output_tokens": 2, "total_tokens": 7},
        }

        with patch("summarize.app.handle_summarize", new=AsyncMock(return_value=mocked_response)) as mock_handle:
            response = summarize_test_client.post(
                "/v1/summarize",
                json={"text": "some content", "length": 25},
            )

        assert response.status_code == 200
        mock_handle.assert_called_once_with("some content", "text", 25, None, False)

    def test_invalid_json_returns_400(self, summarize_test_client):
        response = summarize_test_client.post(
            "/v1/summarize",
            data="{not-valid-json",
            headers={"Content-Type": "application/json"},
        )

        assert response.status_code == 400
        body = response.json()
        assert body["error"]["code"] == 400
        assert body["error"]["status"] == "INVALID_JSON"

    def test_blank_text_returns_missing_input(self, summarize_test_client):
        response = summarize_test_client.post("/v1/summarize", json={"text": "   "})

        assert response.status_code == 400
        assert response.json()["error"]["status"] == "MISSING_INPUT"

    def test_invalid_level_returns_400(self, summarize_test_client):
        response = summarize_test_client.post(
            "/v1/summarize",
            json={"text": "some content", "level": "bad-level"},
        )

        assert response.status_code == 400
        assert response.json()["error"]["status"] == "INVALID_PARAMETER"

    def test_invalid_length_returns_400(self, summarize_test_client):
        response = summarize_test_client.post(
            "/v1/summarize",
            json={"text": "some content", "length": "bad"},
        )

        assert response.status_code == 400
        assert response.json()["error"]["status"] == "INVALID_PARAMETER"

    def test_unsupported_content_type_returns_415(self, summarize_test_client):
        response = summarize_test_client.post(
            "/v1/summarize",
            data="plain text body",
            headers={"Content-Type": "text/plain"},
        )

        assert response.status_code == 415
        assert response.json()["error"]["status"] == "UNSUPPORTED_CONTENT_TYPE"


@pytest.mark.unit
class TestSummarizeMultipartRoute:
    def test_valid_txt_file_calls_handle_summarize(self, summarize_test_client):
        mocked_response = {
            "data": {"summary": "summary", "original_length": 10, "summary_length": 1},
            "meta": {"model": "test-llm-model", "processing_time_ms": 10, "input_type": "file"},
            "usage": {"input_tokens": 5, "output_tokens": 2, "total_tokens": 7},
        }

        with patch("summarize.app.handle_summarize", new=AsyncMock(return_value=mocked_response)) as mock_handle:
            response = summarize_test_client.post(
                "/v1/summarize",
                files={"file": ("sample.txt", b"hello world from text file", "text/plain")},
            )

        assert response.status_code == 200
        mock_handle.assert_called_once_with("hello world from text file", "file", None, None, False)

    def test_valid_pdf_file_uses_pdf_extractor(self, summarize_test_client):
        mocked_response = {
            "data": {"summary": "summary", "original_length": 10, "summary_length": 1},
            "meta": {"model": "test-llm-model", "processing_time_ms": 10, "input_type": "file"},
            "usage": {"input_tokens": 5, "output_tokens": 2, "total_tokens": 7},
        }

        with patch("summarize.app.extract_text_from_pdf", return_value="pdf extracted text") as mock_extract, \
             patch("summarize.app.handle_summarize", new=AsyncMock(return_value=mocked_response)) as mock_handle:
            response = summarize_test_client.post(
                "/v1/summarize",
                files={"file": ("sample.pdf", b"%PDF-1.4 fake bytes", "application/pdf")},
            )

        assert response.status_code == 200
        mock_extract.assert_called_once()
        mock_handle.assert_called_once_with("pdf extracted text", "file", None, None, False)

    def test_missing_file_returns_400(self, summarize_test_client):
        response = summarize_test_client.post(
            "/v1/summarize",
            data={},
            headers={"Content-Type": "multipart/form-data; boundary=test-boundary"},
        )

        assert response.status_code == 400
        assert response.json()["error"]["status"] == "MISSING_INPUT"

    def test_unsupported_extension_returns_400(self, summarize_test_client):
        response = summarize_test_client.post(
            "/v1/summarize",
            files={"file": ("sample.docx", b"fake", "application/octet-stream")},
        )

        assert response.status_code == 400
        assert response.json()["error"]["status"] == "UNSUPPORTED_FILE_TYPE"

    def test_invalid_utf8_text_file_returns_415(self, summarize_test_client):
        response = summarize_test_client.post(
            "/v1/summarize",
            files={"file": ("sample.txt", b"\xff\xfe\xfa", "text/plain")},
        )

        assert response.status_code == 415
        assert response.json()["error"]["status"] == "UNSUPPORTED_CONTENT_TYPE"

    def test_pdf_extraction_failure_returns_415(self, summarize_test_client):
        with patch("summarize.app.extract_text_from_pdf", side_effect=Exception("bad pdf")):
            response = summarize_test_client.post(
                "/v1/summarize",
                files={"file": ("sample.pdf", b"%PDF-1.4 fake bytes", "application/pdf")},
            )

        assert response.status_code == 415
        assert response.json()["error"]["status"] == "UNSUPPORTED_CONTENT_TYPE"

    def test_empty_extracted_text_returns_400(self, summarize_test_client):
        with patch("summarize.app.handle_summarize", new=AsyncMock()) as mock_handle:
            response = summarize_test_client.post(
                "/v1/summarize",
                files={"file": ("sample.txt", b"   ", "text/plain")},
            )

        assert response.status_code == 400
        assert response.json()["error"]["status"] == "EMPTY_INPUT"
        mock_handle.assert_not_called()


@pytest.mark.unit
class TestServerBusyAndExceptionHandling:
    def test_locked_concurrency_returns_429(self, summarize_test_client):
        busy_limiter = Mock()
        busy_limiter.locked.return_value = True

        with patch("summarize.app.concurrency_limiter", busy_limiter):
            response = summarize_test_client.post("/v1/summarize", json={"text": "some content"})

        assert response.status_code == 429
        assert response.json()["error"]["status"] == "SERVER_BUSY"

    def test_summarize_exception_handler_returns_expected_json(self, summarize_test_client):
        with patch(
            "summarize.app.handle_summarize",
            new=AsyncMock(side_effect=SummarizeException(400, "INVALID_PARAMETER", "bad input")),
        ):
            response = summarize_test_client.post("/v1/summarize", json={"text": "some content"})

        assert response.status_code == 400
        assert response.json() == {
            "error": {
                "code": 400,
                "message": "bad input",
                "status": "INVALID_PARAMETER",
            }
        }


@pytest.mark.unit
class TestHandleSummarize:
    @pytest.mark.asyncio
    async def test_handle_summarize_returns_success_payload(self, summarize_sample_text):
        from summarize.app import handle_summarize

        with patch(
                 "summarize.app.llm_model_dict",
                 {"llm_endpoint": "http://localhost:8002", "llm_model": "test-llm-model"},
                 create=True,
             ), \
             patch("summarize.app.tokenize_with_llm", return_value=list(range(40))), \
             patch("summarize.app.validate_input_and_get_available_tokens", return_value=200), \
             patch("summarize.app.compute_target_and_max_tokens", return_value=(60, 51, 69, 80)), \
             patch("summarize.app.build_messages", return_value=[{"role": "user", "content": "prompt"}]), \
             patch("summarize.app.query_vllm_summarize", return_value=("Sentence one. Sentence two incomplete", 40, 20)):
            result = await handle_summarize(
                content_text=summarize_sample_text,
                input_type="text",
                summary_length=60,
                summary_level=None,
                stream=False,
            )

        assert result["data"]["summary"] == "Sentence one."
        assert result["meta"]["model"] == "test-llm-model"
        assert result["meta"]["input_type"] == "text"
        assert result["usage"]["input_tokens"] == 40
        assert result["usage"]["output_tokens"] == 20

    @pytest.mark.asyncio
    async def test_handle_summarize_with_both_level_and_length_raises_error(self, summarize_sample_text):
        from summarize.app import handle_summarize

        with patch(
                 "summarize.app.llm_model_dict",
                 {"llm_endpoint": "http://localhost:8002", "llm_model": "test-llm-model"},
                 create=True,
             ), \
             patch("summarize.app.tokenize_with_llm", return_value=list(range(10))):
            with pytest.raises(SummarizeException) as exc:
                await handle_summarize(
                    content_text=summarize_sample_text,
                    input_type="text",
                    summary_length=20,
                    summary_level="brief",
                    stream=False,
                )

        assert exc.value.code == 400
        assert exc.value.status == "INVALID_PARAMETER"

    @pytest.mark.asyncio
    async def test_handle_summarize_dict_error_raises_llm_error(self, summarize_sample_text):
        from summarize.app import handle_summarize

        with patch(
                 "summarize.app.llm_model_dict",
                 {"llm_endpoint": "http://localhost:8002", "llm_model": "test-llm-model"},
                 create=True,
             ), \
             patch("summarize.app.tokenize_with_llm", return_value=list(range(40))), \
             patch("summarize.app.validate_input_and_get_available_tokens", return_value=200), \
             patch("summarize.app.compute_target_and_max_tokens", return_value=(60, 51, 69, 80)), \
             patch("summarize.app.build_messages", return_value=[{"role": "user", "content": "prompt"}]), \
             patch("summarize.app.query_vllm_summarize", return_value=({"error": "failure"}, 40, 0)):
            with pytest.raises(SummarizeException) as exc:
                await handle_summarize(
                    content_text=summarize_sample_text,
                    input_type="text",
                    summary_length=60,
                    summary_level=None,
                    stream=False,
                )

        assert exc.value.code == 500
        assert exc.value.status == "LLM_ERROR"

    @pytest.mark.asyncio
    async def test_handle_summarize_stream_failure_releases_limiter(self, summarize_sample_text):
        from summarize.app import handle_summarize

        limiter = Mock()
        limiter.acquire = Mock()
        limiter.release = Mock()

        async def fake_acquire():
            return None

        limiter.acquire = fake_acquire

        with patch("summarize.app.concurrency_limiter", limiter), \
             patch(
                 "summarize.app.llm_model_dict",
                 {"llm_endpoint": "http://localhost:8002", "llm_model": "test-llm-model"},
                 create=True,
             ), \
             patch("summarize.app.tokenize_with_llm", return_value=list(range(40))), \
             patch("summarize.app.validate_input_and_get_available_tokens", return_value=200), \
             patch("summarize.app.compute_target_and_max_tokens", return_value=(60, 51, 69, 80)), \
             patch("summarize.app.build_messages", return_value=[{"role": "user", "content": "prompt"}]), \
             patch("summarize.app.query_vllm_summarize_stream", side_effect=Exception("llm down")):
            with pytest.raises(SummarizeException) as exc:
                await handle_summarize(
                    content_text=summarize_sample_text,
                    input_type="text",
                    summary_length=60,
                    summary_level=None,
                    stream=True,
                )

        assert exc.value.code == 500
        assert exc.value.status == "LLM_ERROR"
        limiter.release.assert_called_once()

    @pytest.mark.asyncio
    async def test_handle_summarize_stream_returns_streaming_response(self, summarize_sample_text):
        from summarize.app import handle_summarize

        class DummyLimiter:
            async def acquire(self):
                return None

            def release(self):
                return None

        def fake_stream():
            yield "data: hello\n\n"

        with patch("summarize.app.concurrency_limiter", DummyLimiter()), \
             patch(
                 "summarize.app.llm_model_dict",
                 {"llm_endpoint": "http://localhost:8002", "llm_model": "test-llm-model"},
                 create=True,
             ), \
             patch("summarize.app.tokenize_with_llm", return_value=list(range(40))), \
             patch("summarize.app.validate_input_and_get_available_tokens", return_value=200), \
             patch("summarize.app.compute_target_and_max_tokens", return_value=(60, 51, 69, 80)), \
             patch("summarize.app.build_messages", return_value=[{"role": "user", "content": "prompt"}]), \
             patch("summarize.app.query_vllm_summarize_stream", return_value=fake_stream()):
            response = await handle_summarize(
                content_text=summarize_sample_text,
                input_type="text",
                summary_length=60,
                summary_level=None,
                stream=True,
            )

        assert response.media_type == "text/event-stream"
        assert response.headers["Cache-Control"] == "no-cache"
        assert response.headers["Connection"] == "keep-alive"

# Made with Bob
