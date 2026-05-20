from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock, patch
import asyncio

import pytest
from fastapi.testclient import TestClient

import digitize.app as digitize_app
from digitize.types import JobStatus, OperationType, OutputFormat


@pytest.fixture
def digitize_test_client(monkeypatch, tmp_path):
    docs_dir = tmp_path / "docs"
    jobs_dir = tmp_path / "jobs"
    digitized_dir = tmp_path / "digitized"
    staging_dir = tmp_path / "staging"

    for path in (docs_dir, jobs_dir, digitized_dir, staging_dir):
        path.mkdir(parents=True, exist_ok=True)

    fake_settings = SimpleNamespace(
        common=SimpleNamespace(app=SimpleNamespace(log_level="INFO")),
        digitize=SimpleNamespace(
            docs_dir=docs_dir,
            jobs_dir=jobs_dir,
            digitized_docs_dir=digitized_dir,
            staging_dir=staging_dir,
            digitization_concurrency_limit=2,
            ingestion_concurrency_limit=1,
        ),
    )

    monkeypatch.setattr(digitize_app, "settings", fake_settings, raising=False)
    monkeypatch.setattr(digitize_app.dg_util, "settings", fake_settings, raising=False)
    monkeypatch.setattr(digitize_app, "digitization_semaphore", asyncio.BoundedSemaphore(2))
    monkeypatch.setattr(digitize_app, "ingestion_semaphore", asyncio.BoundedSemaphore(1))
    monkeypatch.setattr(digitize_app.dg_util, "has_active_jobs", Mock(return_value=(False, [])))
    monkeypatch.setattr(digitize_app.dg_util, "generate_uuid", Mock(return_value="job-123"))
    monkeypatch.setattr(digitize_app.dg_util, "stage_upload_files", AsyncMock())
    monkeypatch.setattr(digitize_app.dg_util, "initialize_job_state", Mock(return_value={"sample.pdf": "doc-1"}))
    monkeypatch.setattr(digitize_app.dg_util, "read_all_job_files", Mock(return_value=[]))
    monkeypatch.setattr(digitize_app.dg_util, "read_job_file", Mock())
    monkeypatch.setattr(digitize_app.dg_util, "get_all_documents", Mock(return_value=[]))
    monkeypatch.setattr(digitize_app.dg_util, "get_document_by_id", Mock())
    monkeypatch.setattr(digitize_app.dg_util, "get_document_content", Mock())
    monkeypatch.setattr(digitize_app.dg_util, "is_document_in_active_job", Mock(return_value=False))
    monkeypatch.setattr(digitize_app.dg_util, "delete_document_files", Mock())
    monkeypatch.setattr(digitize_app, "reset_db", Mock())
    monkeypatch.setattr(digitize_app, "configure_uvicorn_logging", Mock())

    return TestClient(digitize_app.app)


@pytest.mark.unit
class TestHealthAndDocs:
    def test_health_returns_ok(self, digitize_test_client):
        response = digitize_test_client.get("/health")

        assert response.status_code == 200
        assert response.json() == {"status": "ok"}

    def test_root_returns_swagger_ui(self, digitize_test_client):
        response = digitize_test_client.get("/")

        assert response.status_code == 200
        assert "Swagger UI" in response.text


@pytest.mark.unit
class TestRequestIdMiddleware:
    def test_existing_request_id_is_echoed(self, digitize_test_client):
        with patch("digitize.app.set_request_id") as mock_set_request_id:
            response = digitize_test_client.get("/health", headers={"X-Request-ID": "req-123"})

        assert response.status_code == 200
        assert response.headers["X-Request-ID"] == "req-123"
        mock_set_request_id.assert_called_once_with("req-123")

    def test_missing_request_id_is_generated(self, digitize_test_client):
        with patch("digitize.app.set_request_id") as mock_set_request_id:
            response = digitize_test_client.get("/health")

        assert response.status_code == 200
        assert response.headers["X-Request-ID"]
        mock_set_request_id.assert_called_once()


@pytest.mark.unit
class TestCreateJobs:
    def test_successful_digitization_job_creation(self, digitize_test_client):
        response = digitize_test_client.post(
            "/v1/jobs?operation=digitization&output_format=json",
            files=[("files", ("sample.pdf", b"%PDF-1.4 test", "application/pdf"))],
        )

        assert response.status_code == 202
        assert response.json() == {"job_id": "job-123"}
        digitize_app.dg_util.stage_upload_files.assert_awaited_once()
        digitize_app.dg_util.initialize_job_state.assert_called_once_with(
            "job-123",
            OperationType.DIGITIZATION,
            OutputFormat.JSON,
            ["sample.pdf"],
            None,
        )

    def test_successful_ingestion_job_creation(self, digitize_test_client):
        response = digitize_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[("files", ("sample.pdf", b"%PDF-1.4 test", "application/pdf"))],
        )

        assert response.status_code == 202
        assert response.json()["job_id"] == "job-123"

    def test_rejects_multiple_files_for_digitization(self, digitize_test_client):
        response = digitize_test_client.post(
            "/v1/jobs?operation=digitization",
            files=[
                ("files", ("a.pdf", b"%PDF-1.4 test", "application/pdf")),
                ("files", ("b.pdf", b"%PDF-1.4 test", "application/pdf")),
            ],
        )

        assert response.status_code == 400

    def test_rejects_when_ingestion_job_already_active(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(digitize_app.dg_util, "has_active_jobs", Mock(return_value=(True, ["job-active"])))

        response = digitize_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[("files", ("sample.pdf", b"%PDF-1.4 test", "application/pdf"))],
        )

        assert response.status_code == 429
        assert "job-active" in response.text

    def test_rejects_invalid_pdf_file(self, digitize_test_client):
        response = digitize_test_client.post(
            "/v1/jobs?operation=digitization",
            files=[("files", ("sample.pdf", b"not-a-pdf", "application/pdf"))],
        )

        assert response.status_code == 415

    def test_output_format_and_job_name_parameters(self, digitize_test_client):
        response = digitize_test_client.post(
            "/v1/jobs?operation=digitization&output_format=md&job_name=My+Job",
            files=[("files", ("sample.pdf", b"%PDF-1.4 test", "application/pdf"))],
        )

        assert response.status_code == 202
        digitize_app.dg_util.initialize_job_state.assert_called_with(
            "job-123",
            OperationType.DIGITIZATION,
            OutputFormat.MD,
            ["sample.pdf"],
            "My Job",
        )


@pytest.mark.unit
class TestJobsEndpoints:
    def test_list_jobs_with_filters_and_latest(self, digitize_test_client, monkeypatch):
        jobs = [
            SimpleNamespace(
                status=JobStatus.COMPLETED,
                operation="digitization",
                submitted_at="2024-01-02T00:00:00Z",
                to_dict=lambda: {"job_id": "job-2", "status": "completed"},
            ),
            SimpleNamespace(
                status=JobStatus.ACCEPTED,
                operation="digitization",
                submitted_at="2024-01-01T00:00:00Z",
                to_dict=lambda: {"job_id": "job-1", "status": "accepted"},
            ),
        ]
        monkeypatch.setattr(digitize_app.dg_util, "read_all_job_files", Mock(return_value=jobs))

        response = digitize_test_client.get("/v1/jobs?latest=true&operation=digitization")

        assert response.status_code == 200
        body = response.json()
        assert body["pagination"]["total"] == 1
        assert body["data"][0]["job_id"] == "job-2"

    def test_get_job_by_id(self, digitize_test_client, monkeypatch, tmp_path):
        job_file = tmp_path / "jobs" / "job-123_status.json"
        job_file.parent.mkdir(parents=True, exist_ok=True)
        job_file.write_text("{}")
        digitize_app.settings.digitize.jobs_dir = job_file.parent
        monkeypatch.setattr(
            digitize_app.dg_util,
            "read_job_file",
            Mock(return_value=SimpleNamespace(to_dict=lambda: {"job_id": "job-123"})),
        )

        response = digitize_test_client.get("/v1/jobs/job-123")

        assert response.status_code == 200
        assert response.json() == {"job_id": "job-123"}

    def test_get_missing_job_returns_404(self, digitize_test_client, tmp_path):
        digitize_app.settings.digitize.jobs_dir = tmp_path / "jobs"
        digitize_app.settings.digitize.jobs_dir.mkdir(parents=True, exist_ok=True)

        response = digitize_test_client.get("/v1/jobs/job-404")

        assert response.status_code == 404

    def test_delete_completed_job_succeeds(self, digitize_test_client, monkeypatch, tmp_path):
        job_file = tmp_path / "jobs" / "job-123_status.json"
        job_file.parent.mkdir(parents=True, exist_ok=True)
        job_file.write_text("{}")
        digitize_app.settings.digitize.jobs_dir = job_file.parent
        monkeypatch.setattr(
            digitize_app.dg_util,
            "read_job_file",
            Mock(return_value=SimpleNamespace(status=JobStatus.COMPLETED)),
        )

        response = digitize_test_client.delete("/v1/jobs/job-123")

        assert response.status_code == 204
        assert not job_file.exists()

    def test_delete_active_job_returns_409(self, digitize_test_client, monkeypatch, tmp_path):
        job_file = tmp_path / "jobs" / "job-123_status.json"
        job_file.parent.mkdir(parents=True, exist_ok=True)
        job_file.write_text("{}")
        digitize_app.settings.digitize.jobs_dir = job_file.parent
        monkeypatch.setattr(
            digitize_app.dg_util,
            "read_job_file",
            Mock(return_value=SimpleNamespace(status=JobStatus.IN_PROGRESS)),
        )

        response = digitize_test_client.delete("/v1/jobs/job-123")

        assert response.status_code == 409


@pytest.mark.unit
class TestDocumentEndpoints:
    def test_list_documents_with_status_and_name(self, digitize_test_client, monkeypatch):
        docs = [
            {
                "id": "doc-1",
                "name": "alpha.pdf",
                "type": "digitization",
                "status": "completed",
                "submitted_at": "2024-01-01T00:00:00Z",
            }
        ]
        monkeypatch.setattr(digitize_app.dg_util, "get_all_documents", Mock(return_value=docs))

        response = digitize_test_client.get("/v1/documents?status=completed&name=alp")

        assert response.status_code == 200
        assert response.json()["data"][0]["id"] == "doc-1"

    def test_list_documents_invalid_status_returns_400(self, digitize_test_client):
        response = digitize_test_client.get("/v1/documents?status=bad-status")

        assert response.status_code == 400

    def test_get_document_metadata_without_and_with_details(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(
            digitize_app.dg_util,
            "get_document_by_id",
            Mock(return_value={"id": "doc-1", "name": "sample.pdf", "type": "digitization", "status": "completed", "output_format": "json"}),
        )

        response = digitize_test_client.get("/v1/documents/doc-1")
        detailed = digitize_test_client.get("/v1/documents/doc-1?details=true")

        assert response.status_code == 200
        assert detailed.status_code == 200
        assert digitize_app.dg_util.get_document_by_id.call_args_list[0].kwargs["include_details"] is False
        assert digitize_app.dg_util.get_document_by_id.call_args_list[1].kwargs["include_details"] is True

    def test_get_missing_document_returns_404(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(digitize_app.dg_util, "get_document_by_id", Mock(side_effect=FileNotFoundError("missing")))

        response = digitize_test_client.get("/v1/documents/doc-404")

        assert response.status_code == 404

    def test_get_document_content(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(
            digitize_app.dg_util,
            "get_document_content",
            Mock(return_value={"result": {"text": "hello"}, "output_format": "json"}),
        )

        response = digitize_test_client.get("/v1/documents/doc-1/content")

        assert response.status_code == 200
        assert response.json()["output_format"] == "json"

    def test_delete_document_success(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(
            digitize_app.dg_util,
            "get_document_by_id",
            Mock(return_value=SimpleNamespace(job_id="job-1", output_format="json")),
        )

        fake_vector_store = Mock()
        fake_vector_store.delete_document_by_id.return_value = 5
        fake_db = SimpleNamespace(get_vector_store=Mock(return_value=fake_vector_store))

        with patch("common.db_utils.get_vector_store", return_value=fake_vector_store):
            response = digitize_test_client.delete("/v1/documents/doc-1")

        assert response.status_code == 204
        fake_vector_store.delete_document_by_id.assert_called_once_with("doc-1")
        digitize_app.dg_util.delete_document_files.assert_called_once_with("doc-1", output_format="json")

    def test_delete_active_document_returns_409(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(
            digitize_app.dg_util,
            "get_document_by_id",
            Mock(return_value=SimpleNamespace(job_id="job-1", output_format="json")),
        )
        monkeypatch.setattr(digitize_app.dg_util, "is_document_in_active_job", Mock(return_value=True))

        response = digitize_test_client.delete("/v1/documents/doc-1")

        assert response.status_code == 409

    def test_bulk_delete_requires_confirmation(self, digitize_test_client):
        response = digitize_test_client.delete("/v1/documents?confirm=false")

        assert response.status_code == 400

    def test_bulk_delete_with_active_jobs_returns_409(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(digitize_app.dg_util, "has_active_jobs", Mock(return_value=(True, ["job-1"])))

        response = digitize_test_client.delete("/v1/documents?confirm=true")

        assert response.status_code == 409

    def test_bulk_delete_success(self, digitize_test_client):
        response = digitize_test_client.delete("/v1/documents?confirm=true")

        assert response.status_code == 204
        digitize_app.reset_db.assert_called_once()

# Made with Bob
