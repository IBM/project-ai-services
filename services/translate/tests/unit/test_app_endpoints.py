"""
Unit tests for translate app endpoints (FastAPI routes).

Tests cover:
 - GET /health
 - GET /  (Swagger UI)
 - Middleware: X-Request-ID injection
 - POST /v1/translate    (sync)
 - POST /v1/translate/jobs  (create job)
 - GET  /v1/translate/jobs  (list)
 - GET  /v1/translate/jobs/{id}  (detail)
 - GET  /v1/translate/jobs/{id}/result
 - GET  /v1/translate/jobs/{id}/result/download
"""

from datetime import datetime, timezone
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock, Mock, patch

import pytest
from fastapi.testclient import TestClient


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_db_job(
    job_id: str = "job-123",
    status: str = "completed",
    source_language: str = "german",
    target_language: str = "english",
    input_type: str = "txt",
    document_name: str = "doc.txt",
    job_name: str = None,
    error: str = None,
    job_metadata: dict = None,
) -> Mock:
    job = Mock()
    job.job_id = job_id
    job.job_name = job_name
    job.status = status
    job.source_language = source_language
    job.target_language = target_language
    job.input_type = input_type
    job.document_name = document_name
    job.submitted_at = datetime(2024, 1, 1, 0, 0, 0, tzinfo=timezone.utc)
    job.completed_at = datetime(2024, 1, 1, 1, 0, 0, tzinfo=timezone.utc)
    job.error = error
    job.job_metadata = job_metadata or {}
    job.updated_at = datetime(2024, 1, 1, 1, 0, 0, tzinfo=timezone.utc)
    return job


# ---------------------------------------------------------------------------
# Fixture: TestClient wired with all required mocks
# ---------------------------------------------------------------------------


@pytest.fixture
def translate_test_client(monkeypatch, tmp_path):
    """Build a TestClient for the translate app with all side-effects mocked."""
    import translate.app as translate_app
    import translate.api.v1.translate as sync_router_module
    import translate.api.v1.jobs as jobs_router_module

    staging_dir = tmp_path / "staging"
    results_dir = tmp_path / "results"
    staging_dir.mkdir(parents=True)
    results_dir.mkdir(parents=True)

    # Patch settings used inside the app and routers
    fake_settings = SimpleNamespace(
        common=SimpleNamespace(
            app=SimpleNamespace(log_level="INFO"),
            llm=SimpleNamespace(
                endpoint="http://vllm:8000",
                model="granite",
                api_key=None,
                max_model_len=32768,
                max_batch_size=32,
            ),
            language=SimpleNamespace(language_detection_min_confidence=0.9),
        ),
        translate=SimpleNamespace(
            staging_dir=staging_dir,
            results_dir=results_dir,
            max_upload_size_mb=10,
            max_concurrent_jobs=8,
            chunk_parallelism=4,
            chunk_token_budget=13107,
            chunk_budget_ratio=0.40,
            prompt_overhead_tokens=250,
            min_output_tokens=50,
            translation_temperature=0.0,
            supported_languages="english,german",
            supported_languages_list=["english", "german"],
        ),
    )

    monkeypatch.setattr(translate_app, "settings", fake_settings, raising=False)
    monkeypatch.setattr(sync_router_module, "settings", fake_settings, raising=False)
    monkeypatch.setattr(jobs_router_module, "settings", fake_settings, raising=False)

    # Patch configure_uvicorn_logging to avoid stdout capture issues
    monkeypatch.setattr(translate_app, "configure_uvicorn_logging", Mock())

    # Patch db_manager used in routers
    mock_db = Mock()
    mock_db.create_job = Mock(return_value=_make_db_job())
    mock_db.get_job_by_id = Mock(return_value=None)
    mock_db.get_all_jobs = Mock(return_value=([], 0))
    mock_db.update_job = Mock(return_value=True)
    mock_db.get_active_jobs = Mock(return_value=[])

    monkeypatch.setattr(jobs_router_module, "db_manager", mock_db)

    # Patch concurrency manager (already initialized)
    import asyncio
    from translate.workers.concurrency import ConcurrencyManager
    mock_cm = ConcurrencyManager()
    mock_cm._job_limiter = asyncio.BoundedSemaphore(8)
    mock_cm._chunk_semaphore = asyncio.BoundedSemaphore(4)
    mock_cm._vllm_semaphore = asyncio.BoundedSemaphore(32)
    monkeypatch.setattr(sync_router_module, "concurrency_manager", mock_cm)
    monkeypatch.setattr(jobs_router_module, "concurrency_manager", mock_cm)

    # Patch storage_manager to avoid real FS ops in job endpoints
    mock_storage = Mock()
    mock_storage.stage_upload_file = AsyncMock(return_value=staging_dir / "job-123" / "doc.txt")
    mock_storage.cleanup_staging = Mock()
    mock_storage.write_result = Mock()
    mock_storage.read_result = Mock()
    mock_storage.delete_result = Mock()
    monkeypatch.setattr(jobs_router_module, "storage_manager", mock_storage)

    # Patch run_translation_job so background tasks don't actually run
    monkeypatch.setattr(jobs_router_module, "run_translation_job", AsyncMock())

    # Patch generate_uuid for deterministic job IDs
    monkeypatch.setattr(jobs_router_module, "generate_uuid", Mock(return_value="job-123"))

    # Patch stage_uploaded_file to return a fake Path
    monkeypatch.setattr(
        jobs_router_module, "stage_uploaded_file",
        AsyncMock(return_value=staging_dir / "job-123" / "doc.txt")
    )

    return TestClient(translate_app.app), mock_db, mock_storage


# ---------------------------------------------------------------------------
# Health and Docs
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestHealthAndDocs:
    def test_health_returns_ok(self, translate_test_client):
        client, _, _ = translate_test_client
        response = client.get("/health")
        assert response.status_code == 200
        assert response.json() == {"status": "ok"}

    def test_root_returns_swagger_ui(self, translate_test_client):
        client, _, _ = translate_test_client
        response = client.get("/")
        assert response.status_code == 200
        assert "text/html" in response.headers["content-type"]


# ---------------------------------------------------------------------------
# Middleware — X-Request-ID
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestRequestIdMiddleware:
    def test_existing_request_id_is_echoed(self, translate_test_client):
        client, _, _ = translate_test_client
        response = client.get("/health", headers={"X-Request-ID": "my-id-123"})
        assert response.headers.get("x-request-id") == "my-id-123"

    def test_missing_request_id_is_generated(self, translate_test_client):
        client, _, _ = translate_test_client
        response = client.get("/health")
        assert "x-request-id" in response.headers
        assert len(response.headers["x-request-id"]) > 0


# ---------------------------------------------------------------------------
# POST /v1/translate — sync endpoint
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestSyncTranslateEndpoint:
    def test_successful_translation(self, translate_test_client):
        client, _, _ = translate_test_client

        with patch("translate.api.v1.translate.tokenize_with_llm", return_value=list(range(10))), \
             patch("translate.api.v1.translate.get_llm_max_model_len", return_value=32768), \
             patch("translate.api.v1.translate.resolve_source_language", return_value=("German", "DE")), \
             patch("translate.api.v1.translate.build_messages", return_value=[]), \
             patch("translate.api.v1.translate.query_vllm_translate",
                   new=AsyncMock(return_value=("Hallo Welt", 10, 8))):

            response = client.post(
                "/v1/translate",
                json={
                    "text": "Hello World",
                    "source_language": "german",
                    "target_language": "english",
                },
            )

        assert response.status_code == 200
        data = response.json()
        assert "data" in data
        assert "meta" in data
        assert "usage" in data
        assert data["data"]["translation"] == "Hallo Welt"

    def test_empty_text_returns_400(self, translate_test_client):
        client, _, _ = translate_test_client
        response = client.post(
            "/v1/translate",
            json={"text": "   ", "target_language": "english"},
        )
        assert response.status_code == 400

    def test_unsupported_target_language_returns_400(self, translate_test_client):
        client, _, _ = translate_test_client
        response = client.post(
            "/v1/translate",
            json={"text": "Hello", "target_language": "klingon"},
        )
        assert response.status_code == 400

    def test_same_source_and_target_returns_400(self, translate_test_client):
        client, _, _ = translate_test_client
        response = client.post(
            "/v1/translate",
            json={"text": "Hello", "source_language": "english", "target_language": "english"},
        )
        assert response.status_code == 400

    def test_context_limit_exceeded_returns_413(self, translate_test_client):
        client, _, _ = translate_test_client

        # 5000 input tokens, max_model_len=4096, overhead=250 → available=−1154 < input
        with patch("translate.api.v1.translate.tokenize_with_llm", return_value=list(range(5000))), \
             patch("translate.api.v1.translate.get_llm_max_model_len", return_value=4096), \
             patch("translate.api.v1.translate.resolve_source_language", return_value=("German", "DE")):

            response = client.post(
                "/v1/translate",
                json={"text": "x" * 100, "target_language": "english"},
            )

        assert response.status_code == 413

    def test_vllm_request_error_returns_503(self, translate_test_client):
        import httpx
        client, _, _ = translate_test_client

        with patch("translate.api.v1.translate.tokenize_with_llm", return_value=list(range(10))), \
             patch("translate.api.v1.translate.get_llm_max_model_len", return_value=32768), \
             patch("translate.api.v1.translate.resolve_source_language", return_value=("German", "DE")), \
             patch("translate.api.v1.translate.build_messages", return_value=[]), \
             patch("translate.api.v1.translate.query_vllm_translate",
                   new=AsyncMock(side_effect=httpx.RequestError("connection refused"))):

            response = client.post(
                "/v1/translate",
                json={"text": "Hello", "target_language": "english"},
            )

        assert response.status_code == 503

    def test_vllm_http_status_error_returns_500(self, translate_test_client):
        import httpx
        client, _, _ = translate_test_client

        with patch("translate.api.v1.translate.tokenize_with_llm", return_value=list(range(10))), \
             patch("translate.api.v1.translate.get_llm_max_model_len", return_value=32768), \
             patch("translate.api.v1.translate.resolve_source_language", return_value=("German", "DE")), \
             patch("translate.api.v1.translate.build_messages", return_value=[]), \
             patch("translate.api.v1.translate.query_vllm_translate",
                   new=AsyncMock(side_effect=httpx.HTTPStatusError(
                       "500", request=MagicMock(), response=MagicMock()
                   ))):

            response = client.post(
                "/v1/translate",
                json={"text": "Hello", "target_language": "english"},
            )

        assert response.status_code == 500

    def test_rate_limit_returns_429_when_vllm_semaphore_locked(self, translate_test_client, monkeypatch):
        client, _, _ = translate_test_client
        import asyncio
        import translate.api.v1.translate as sync_mod

        # Lock the single-slot vllm semaphore
        locked_semaphore = asyncio.BoundedSemaphore(1)
        locked_semaphore._value = 0  # simulate locked state

        mock_cm = MagicMock()
        mock_cm.vllm_semaphore = locked_semaphore
        monkeypatch.setattr(sync_mod, "concurrency_manager", mock_cm)

        with patch("translate.api.v1.translate.tokenize_with_llm", return_value=list(range(5))), \
             patch("translate.api.v1.translate.get_llm_max_model_len", return_value=32768), \
             patch("translate.api.v1.translate.validate_languages", return_value=("auto", "english")):

            response = client.post(
                "/v1/translate",
                json={"text": "Hello", "target_language": "english"},
            )

        assert response.status_code == 429


# ---------------------------------------------------------------------------
# POST /v1/translate/jobs — create job
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestCreateJobEndpoint:
    def test_valid_txt_upload_returns_202(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.create_job.return_value = _make_db_job()

        response = client.post(
            "/v1/translate/jobs",
            files={"file": ("test.txt", b"Hello World", "text/plain")},
            data={"target_language": "english"},
        )

        assert response.status_code == 202
        assert response.json()["job_id"] == "job-123"

    def test_valid_md_upload_returns_202(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.create_job.return_value = _make_db_job()

        response = client.post(
            "/v1/translate/jobs",
            files={"file": ("doc.md", b"# Title\nContent here", "text/markdown")},
            data={"target_language": "english"},
        )

        assert response.status_code == 202

    def test_unsupported_extension_returns_415(self, translate_test_client):
        client, _, _ = translate_test_client

        with patch("translate.api.v1.jobs.validate_file_extension",
                   side_effect=ValueError("Unsupported extension")):
            response = client.post(
                "/v1/translate/jobs",
                files={"file": ("doc.pdf", b"%PDF content", "application/pdf")},
                data={"target_language": "english"},
            )

        assert response.status_code == 415

    def test_invalid_target_language_returns_400(self, translate_test_client):
        client, _, _ = translate_test_client

        response = client.post(
            "/v1/translate/jobs",
            files={"file": ("test.txt", b"Hello", "text/plain")},
            data={"target_language": "klingon"},
        )

        assert response.status_code == 400

    def test_same_language_returns_400(self, translate_test_client):
        client, _, _ = translate_test_client

        response = client.post(
            "/v1/translate/jobs",
            files={"file": ("test.txt", b"Hello", "text/plain")},
            data={"source_language": "english", "target_language": "english"},
        )

        assert response.status_code == 400

    def test_file_too_large_returns_413(self, translate_test_client, monkeypatch):
        client, _, _ = translate_test_client
        import translate.api.v1.jobs as jobs_mod

        # Override settings.translate.max_upload_size_mb = 1 byte (effectively)
        monkeypatch.setattr(
            jobs_mod.settings.translate, "max_upload_size_mb", 0, raising=False
        )

        response = client.post(
            "/v1/translate/jobs",
            files={"file": ("test.txt", b"a" * 2000, "text/plain")},
            data={"target_language": "english"},
        )

        assert response.status_code == 413

    def test_db_create_failure_returns_500(self, translate_test_client):
        client, mock_db, mock_storage = translate_test_client
        mock_db.create_job.return_value = None  # DB failure

        response = client.post(
            "/v1/translate/jobs",
            files={"file": ("test.txt", b"Hello World", "text/plain")},
            data={"target_language": "english"},
        )

        assert response.status_code == 500
        # Staging should have been cleaned up
        mock_storage.cleanup_staging.assert_called()

    def test_job_name_optional_param(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.create_job.return_value = _make_db_job()

        response = client.post(
            "/v1/translate/jobs",
            files={"file": ("test.txt", b"Hello", "text/plain")},
            data={"target_language": "english", "job_name": "My Translation"},
        )

        assert response.status_code == 202


# ---------------------------------------------------------------------------
# GET /v1/translate/jobs — list
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestListJobsEndpoint:
    def test_empty_list(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_all_jobs.return_value = ([], 0)

        response = client.get("/v1/translate/jobs")

        assert response.status_code == 200
        data = response.json()
        assert data["pagination"]["total"] == 0
        assert data["data"] == []

    def test_pagination_defaults(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_all_jobs.return_value = ([], 0)

        response = client.get("/v1/translate/jobs")
        data = response.json()
        assert data["pagination"]["limit"] == 20
        assert data["pagination"]["offset"] == 0

    def test_custom_pagination(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_all_jobs.return_value = ([], 0)

        response = client.get("/v1/translate/jobs?limit=5&offset=10")
        data = response.json()
        assert data["pagination"]["limit"] == 5
        assert data["pagination"]["offset"] == 10

    def test_valid_status_filter(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_all_jobs.return_value = ([], 0)

        response = client.get("/v1/translate/jobs?status_filter=completed")
        assert response.status_code == 200

    def test_invalid_status_filter_returns_400(self, translate_test_client):
        client, _, _ = translate_test_client

        response = client.get("/v1/translate/jobs?status_filter=bogus_status")
        assert response.status_code == 400

    def test_returns_jobs_in_response(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_all_jobs.return_value = ([_make_db_job()], 1)

        response = client.get("/v1/translate/jobs")
        data = response.json()
        assert data["pagination"]["total"] == 1
        assert len(data["data"]) == 1
        assert data["data"][0]["job_id"] == "job-123"


# ---------------------------------------------------------------------------
# GET /v1/translate/jobs/{job_id} — detail
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestGetJobEndpoint:
    def test_existing_job_returns_200(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job()

        response = client.get("/v1/translate/jobs/job-123")
        assert response.status_code == 200
        data = response.json()
        assert data["job_id"] == "job-123"
        assert data["status"] == "completed"

    def test_missing_job_returns_404(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = None

        response = client.get("/v1/translate/jobs/nonexistent")
        assert response.status_code == 404

    def test_job_detail_fields(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(
            job_name="My Job",
            input_type="md",
            document_name="doc.md",
        )

        response = client.get("/v1/translate/jobs/job-123")
        data = response.json()
        assert data["job_name"] == "My Job"
        assert data["input_type"] == "md"
        assert data["document_name"] == "doc.md"


# ---------------------------------------------------------------------------
# GET /v1/translate/jobs/{job_id}/result
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestGetJobResultEndpoint:
    def test_completed_job_returns_result(self, translate_test_client):
        client, mock_db, mock_storage = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(status="completed")
        mock_storage.read_result.return_value = {
            "data": {"translation": "Hallo Welt"},
            "meta": {"model": "granite"},
            "usage": {"input_tokens": 10},
        }

        with patch("translate.api.v1.jobs.read_result_file",
                   return_value={
                       "data": {"translation": "Hallo Welt"},
                       "meta": {"model": "granite"},
                       "usage": {"input_tokens": 10},
                   }):
            response = client.get("/v1/translate/jobs/job-123/result")

        assert response.status_code == 200
        assert response.json()["data"]["translation"] == "Hallo Welt"

    def test_in_progress_job_returns_202(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(status="in_progress")

        response = client.get("/v1/translate/jobs/job-123/result")
        assert response.status_code == 202

    def test_accepted_job_returns_202(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(status="accepted")

        response = client.get("/v1/translate/jobs/job-123/result")
        assert response.status_code == 202

    def test_failed_job_returns_410(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(status="failed", error="disk full")

        response = client.get("/v1/translate/jobs/job-123/result")
        assert response.status_code == 410

    def test_missing_job_returns_404(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = None

        response = client.get("/v1/translate/jobs/missing/result")
        assert response.status_code == 404

    def test_result_file_not_found_returns_500(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(status="completed")

        with patch("translate.api.v1.jobs.read_result_file", side_effect=FileNotFoundError()):
            response = client.get("/v1/translate/jobs/job-123/result")

        assert response.status_code == 500


# ---------------------------------------------------------------------------
# GET /v1/translate/jobs/{job_id}/result/download
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestDownloadJobResultEndpoint:
    def test_completed_job_returns_file_content(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(
            status="completed", input_type="txt", document_name="original.txt"
        )

        with patch("translate.api.v1.jobs.read_result_file",
                   return_value={"data": {"translation": "Hallo Welt"}, "meta": {}, "usage": {}}):
            response = client.get("/v1/translate/jobs/job-123/result/download")

        assert response.status_code == 200
        assert response.text == "Hallo Welt"
        assert "original_translated.txt" in response.headers["content-disposition"]

    def test_md_file_has_correct_media_type(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(
            status="completed", input_type="md", document_name="original.md"
        )

        with patch("translate.api.v1.jobs.read_result_file",
                   return_value={"data": {"translation": "# Hallo"}, "meta": {}, "usage": {}}):
            response = client.get("/v1/translate/jobs/job-123/result/download")

        assert response.status_code == 200
        assert "text/markdown" in response.headers["content-type"]
        assert "original_translated.md" in response.headers["content-disposition"]

    def test_in_progress_returns_202(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(status="in_progress")

        response = client.get("/v1/translate/jobs/job-123/result/download")
        assert response.status_code == 202

    def test_failed_job_returns_410(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(status="failed", error="crash")

        response = client.get("/v1/translate/jobs/job-123/result/download")
        assert response.status_code == 410

    def test_missing_job_returns_404(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = None

        response = client.get("/v1/translate/jobs/missing/result/download")
        assert response.status_code == 404

    def test_result_file_not_found_returns_500(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(status="completed")

        with patch("translate.api.v1.jobs.read_result_file", side_effect=FileNotFoundError()):
            response = client.get("/v1/translate/jobs/job-123/result/download")

        assert response.status_code == 500

    def test_document_name_without_extension_uses_job_id(self, translate_test_client):
        client, mock_db, _ = translate_test_client
        mock_db.get_job_by_id.return_value = _make_db_job(
            status="completed", input_type="txt", document_name=None
        )

        with patch("translate.api.v1.jobs.read_result_file",
                   return_value={"data": {"translation": "Hi"}, "meta": {}, "usage": {}}):
            response = client.get("/v1/translate/jobs/job-123/result/download")

        assert response.status_code == 200
        # Filename derived from job_id when document_name is None
        assert "_translated.txt" in response.headers["content-disposition"]

# Made with Bob
