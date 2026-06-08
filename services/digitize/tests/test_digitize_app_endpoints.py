from types import SimpleNamespace
from typing import cast
from unittest.mock import AsyncMock, Mock, patch
import asyncio

import pytest
from fastapi.testclient import TestClient

import digitize.app as digitize_app
import digitize.db_operations as db_ops
import digitize.db.connection as db_conn
from digitize.models import JobStatus, OperationType, OutputFormat, ImportRequest


@pytest.fixture
def digitize_test_client(monkeypatch, tmp_path, mock_db_operations):
    digitized_dir = tmp_path / "digitized"
    staging_dir = tmp_path / "staging"

    for path in (digitized_dir, staging_dir):
        path.mkdir(parents=True, exist_ok=True)

    fake_settings = SimpleNamespace(
        common=SimpleNamespace(app=SimpleNamespace(log_level="INFO")),
        digitize=SimpleNamespace(
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
        stage_upload_files_mock = cast(AsyncMock, digitize_app.dg_util.stage_upload_files)
        initialize_job_state_mock = cast(Mock, digitize_app.dg_util.initialize_job_state)

        response = digitize_test_client.post(
            "/v1/jobs?operation=digitization&output_format=json",
            files=[("files", ("sample.pdf", b"%PDF-1.4 test", "application/pdf"))],
        )

        assert response.status_code == 202
        assert response.json() == {"job_id": "job-123"}
        stage_upload_files_mock.assert_awaited_once()
        initialize_job_state_mock.assert_called_once_with(
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
        initialize_job_state_mock = cast(Mock, digitize_app.dg_util.initialize_job_state)

        response = digitize_test_client.post(
            "/v1/jobs?operation=digitization&output_format=md&job_name=My+Job",
            files=[("files", ("sample.pdf", b"%PDF-1.4 test", "application/pdf"))],
        )

        assert response.status_code == 202
        initialize_job_state_mock.assert_called_with(
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
        monkeypatch.setattr(db_ops, "get_all_jobs", Mock(return_value=([job.to_dict() for job in jobs], 2)))

        response = digitize_test_client.get("/v1/jobs?latest=true&operation=digitization")

        assert response.status_code == 200
        body = response.json()
        assert body["pagination"]["total"] == 1
        assert body["data"][0]["job_id"] == "job-2"

    def test_get_job_by_id(self, digitize_test_client, monkeypatch, tmp_path):
        monkeypatch.setattr(
            db_ops,
            "get_job",
            Mock(return_value={"job_id": "job-123"}),
        )

        response = digitize_test_client.get("/v1/jobs/job-123")

        assert response.status_code == 200
        assert response.json() == {"job_id": "job-123"}

    def test_get_missing_job_returns_404(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(
            db_ops,
            "get_job",
            Mock(return_value=None),
        )

        response = digitize_test_client.get("/v1/jobs/job-404")

        assert response.status_code == 404

    def test_delete_completed_job_succeeds(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(
            db_ops,
            "get_job",
            Mock(return_value={"job_id": "job-123", "status": JobStatus.COMPLETED.value}),
        )
        mock_delete = Mock()
        monkeypatch.setattr("digitize.db.manager.db_manager.delete_job", mock_delete)

        response = digitize_test_client.delete("/v1/jobs/job-123")

        assert response.status_code == 204
        mock_delete.assert_called_once_with("job-123")

    def test_delete_active_job_returns_409(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(
            db_ops,
            "get_job",
            Mock(return_value={"job_id": "job-123", "status": JobStatus.IN_PROGRESS.value}),
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
        monkeypatch.setattr(db_ops, "get_all_documents_paginated", Mock(return_value=(docs, 1)))

        response = digitize_test_client.get("/v1/documents?status=completed&name=alp")

        assert response.status_code == 200
        assert response.json()["data"][0]["id"] == "doc-1"

    def test_list_documents_invalid_status_returns_400(self, digitize_test_client):
        response = digitize_test_client.get("/v1/documents?status=bad-status")

        assert response.status_code == 400

    def test_get_document_metadata_without_and_with_details(self, digitize_test_client, monkeypatch):
        from digitize.models import DocumentDetailResponse
        mock_doc = DocumentDetailResponse(
            id="doc-1",
            job_id="job-1",
            name="sample.pdf",
            type="digitization",
            status="completed",
            output_format="json"
        )
        get_document_mock = Mock(return_value=mock_doc)
        monkeypatch.setattr(
            db_ops,
            "get_document",
            get_document_mock,
        )

        response = digitize_test_client.get("/v1/documents/doc-1")
        detailed = digitize_test_client.get("/v1/documents/doc-1?details=true")

        assert response.status_code == 200
        assert detailed.status_code == 200
        assert get_document_mock.call_args_list[0][1]["include_details"] is False
        assert get_document_mock.call_args_list[1][1]["include_details"] is True

    def test_get_missing_document_returns_404(self, digitize_test_client, monkeypatch):
        # Mock get_document to raise FileNotFoundError which should be caught and converted to 404
        def mock_get_document(doc_id, include_details=False):
            raise FileNotFoundError(f"Document with ID '{doc_id}' not found")

        monkeypatch.setattr(db_ops, "get_document", mock_get_document)

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
        from digitize.models import DocumentDetailResponse
        delete_document_files_mock = cast(Mock, digitize_app.dg_util.delete_document_files)
        mock_doc = DocumentDetailResponse(
            id="doc-1",
            job_id="job-1",
            name="test.pdf",
            type="digitization",
            status="completed",
            output_format="json"
        )
        # The delete endpoint uses dg_util.get_document, not db_ops.get_document
        monkeypatch.setattr(
            digitize_app.dg_util,
            "get_document",
            Mock(return_value=mock_doc),
        )

        fake_vector_store = Mock()
        fake_vector_store.delete_document_by_id.return_value = 5

        with patch("common.db_utils.get_vector_store", return_value=fake_vector_store):
            response = digitize_test_client.delete("/v1/documents/doc-1")

        assert response.status_code == 204
        fake_vector_store.delete_document_by_id.assert_called_once_with("doc-1")
        delete_document_files_mock.assert_called_once_with("doc-1", output_format="json")

    def test_delete_active_document_returns_409(self, digitize_test_client, monkeypatch):
        from digitize.models import DocumentDetailResponse
        mock_doc = DocumentDetailResponse(
            id="doc-1",
            job_id="job-1",
            name="test.pdf",
            type="digitization",
            status="in_progress",
            output_format="json"
        )
        # The delete endpoint uses dg_util.get_document, not db_ops.get_document
        monkeypatch.setattr(
            digitize_app.dg_util,
            "get_document",
            Mock(return_value=mock_doc),
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
        reset_db_mock = cast(Mock, digitize_app.reset_db)

        response = digitize_test_client.delete("/v1/documents?confirm=true")

        assert response.status_code == 204
        reset_db_mock.assert_called_once()

@pytest.mark.unit
class TestImportExportEndpoints:
    def test_import_metadata_success(self, digitize_test_client, monkeypatch):
        payload = {
            "data": {
                "jobs": [
                    {
                        "job_id": "job-1",
                        "operation": "ingestion",
                        "status": "completed",
                        "job_name": "Import Job",
                        "submitted_at": "2024-01-01T00:00:00Z",
                        "completed_at": "2024-01-01T01:00:00Z",
                        "stats": {"total_documents": 1, "completed": 1, "failed": 0, "in_progress": 0},
                        "error": None,
                    }
                ],
                "documents": [
                    {
                        "id": "doc-1",
                        "job_id": "job-1",
                        "name": "sample.pdf",
                        "type": "ingestion",
                        "status": "completed",
                        "output_format": "json",
                        "submitted_at": "2024-01-01T00:00:00Z",
                        "completed_at": "2024-01-01T00:30:00Z",
                        "error": None,
                        "metadata": {"pages": 2},
                    }
                ],
            },
            "validate_only": False,
        }

        monkeypatch.setattr(digitize_app.dg_util, "has_active_jobs", Mock(return_value=(False, [])))
        monkeypatch.setattr(
            db_ops,
            "import_metadata",
            Mock(
                return_value={
                    "status": "completed",
                    "summary": {
                        "jobs": {"total_received": 1, "imported": 1, "skipped": 0, "failed": 0},
                        "documents": {"total_received": 1, "imported": 1, "skipped": 0, "failed": 0},
                    },
                    "duration_seconds": 0.1,
                    "errors": [],
                    "warnings": [],
                }
            ),
        )

        response = digitize_test_client.post("/v1/import", json=payload)

        assert response.status_code == 200
        assert response.json()["summary"]["jobs"]["imported"] == 1

    def test_import_metadata_rejects_when_active_jobs_exist(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(digitize_app.dg_util, "has_active_jobs", Mock(return_value=(True, ["job-active"])))

        response = digitize_test_client.post(
            "/v1/import",
            json={
                "data": {
                    "jobs": [{"job_id": "job-1", "operation": "ingestion", "status": "completed", "submitted_at": "2024-01-01T00:00:00Z", "stats": {}}],
                    "documents": [],
                }
            },
        )

        assert response.status_code == 409
        assert "job-active" in response.text

    def test_import_metadata_rejects_too_many_records(self, digitize_test_client, monkeypatch):
        monkeypatch.setattr(digitize_app.dg_util, "has_active_jobs", Mock(return_value=(False, [])))
        monkeypatch.setattr(digitize_app, "MAX_IMPORT_RECORDS", 1)

        response = digitize_test_client.post(
            "/v1/import",
            json={
                "data": {
                    "jobs": [
                        {"job_id": "job-1", "operation": "ingestion", "status": "completed", "submitted_at": "2024-01-01T00:00:00Z", "stats": {}},
                        {"job_id": "job-2", "operation": "ingestion", "status": "completed", "submitted_at": "2024-01-01T00:00:00Z", "stats": {}},
                    ],
                    "documents": [],
                }
            },
        )

        assert response.status_code == 413

    def test_export_metadata_default_limit(self, digitize_test_client, monkeypatch):
        export_metadata_mock = Mock(
            return_value={
                "status": "completed",
                "data": {"jobs": [], "documents": []},
                "summary": {
                    "jobs": {"total_exported": 0, "completed": 0, "failed": 0},
                    "documents": {"total_exported": 0, "completed": 0, "failed": 0},
                },
                "export_timestamp": "2024-01-01T00:00:00Z",
                "duration_seconds": 0.1,
                "pagination": {
                    "limit": db_ops.IMPORT_EXPORT_DEFAULT_LIMIT,
                    "offset": 0,
                    "has_more": False,
                    "total_records": 0,
                    "returned_records": 0,
                },
            }
        )
        monkeypatch.setattr(db_ops, "export_metadata", export_metadata_mock)

        response = digitize_test_client.get("/v1/export")

        assert response.status_code == 200
        export_metadata_mock.assert_called_once_with(limit=db_ops.IMPORT_EXPORT_DEFAULT_LIMIT, offset=0)

    def test_export_metadata_limit_minus_one(self, digitize_test_client, monkeypatch):
        export_metadata_mock = Mock(
            return_value={
                "status": "completed",
                "data": {"jobs": [{"job_id": "job-1", "operation": "ingestion", "status": "completed", "submitted_at": "2024-01-01T00:00:00Z", "stats": {}}], "documents": []},
                "summary": {
                    "jobs": {"total_exported": 1, "completed": 1, "failed": 0},
                    "documents": {"total_exported": 0, "completed": 0, "failed": 0},
                },
                "export_timestamp": "2024-01-01T00:00:00Z",
                "duration_seconds": 0.1,
                "pagination": {
                    "limit": 1,
                    "offset": 0,
                    "has_more": False,
                    "total_records": 1,
                    "returned_records": 1,
                },
            }
        )
        monkeypatch.setattr(db_ops, "export_metadata", export_metadata_mock)

        response = digitize_test_client.get("/v1/export?limit=-1")

        assert response.status_code == 200
        export_metadata_mock.assert_called_once_with(limit=-1, offset=0)

    def test_export_metadata_invalid_limit_returns_400(self, digitize_test_client):
        response = digitize_test_client.get("/v1/export?limit=0")

        assert response.status_code == 400


# Made with Bob
