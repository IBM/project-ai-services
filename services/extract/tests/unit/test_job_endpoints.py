"""
Unit tests for job CRUD endpoints:
  POST   /v1/extract/jobs
  GET    /v1/extract/jobs
  GET    /v1/extract/jobs/{job_id}
  GET    /v1/extract/jobs/{job_id}/result
  GET    /v1/extract/jobs/{job_id}/results/{doc_id}
  GET    /v1/extract/jobs/{job_id}/results/{doc_id}/download
  DELETE /v1/extract/jobs/{job_id}
  DELETE /v1/extract/jobs

All external boundaries (DB, file system, semaphores) are mocked so tests
run without PostgreSQL or a real filesystem.
"""

import json
from datetime import datetime, timezone
from unittest.mock import AsyncMock, MagicMock, Mock, patch, mock_open

import pytest

from extract.utils.exceptions import ExtractException


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

def _mock_job_row(
    job_id="job-001",
    job_name="Q3 contract",
    schema_id="schema-001",
    status="completed",
    submitted_at=None,
    completed_at=None,
    error=None,
    job_metadata=None,
    file_count=1,
):
    row = Mock()
    row.job_id = job_id
    row.job_name = job_name
    row.schema_id = schema_id
    row.status = status
    row.submitted_at = submitted_at or datetime(2026, 7, 7, 10, 0, 0, tzinfo=timezone.utc)
    row.completed_at = completed_at or datetime(2026, 7, 7, 10, 5, 0, tzinfo=timezone.utc)
    row.error = error
    row.job_metadata = job_metadata
    row.file_count = file_count
    return row


def _mock_doc_row(
    doc_id="doc-001",
    job_id="job-001",
    filename="invoice_001.txt",
    source_type="txt",
    status="completed",
    error=None,
    usage_input_tokens=None,
    usage_output_tokens=None,
):
    row = Mock()
    row.doc_id = doc_id
    row.job_id = job_id
    row.filename = filename
    row.source_type = source_type
    row.status = status
    row.error = error
    row.word_count = None
    row.input_tokens = None
    row.usage_input_tokens = usage_input_tokens
    row.usage_output_tokens = usage_output_tokens
    row.doc_metadata = None
    row.started_at = None
    row.completed_at = datetime(2026, 7, 7, 10, 5, 0, tzinfo=timezone.utc)
    row.created_at = datetime(2026, 7, 7, 10, 0, 0, tzinfo=timezone.utc)
    return row


def _mock_schema_row(schema_id="schema-001"):
    row = Mock()
    row.schema_id = schema_id
    return row


# ---------------------------------------------------------------------------
# POST /v1/extract/jobs
# ---------------------------------------------------------------------------

class TestCreateExtractJob:
    def _post_single(self, client, filename="doc.txt", schema_id="schema-001", job_name=None):
        """Post a single-file job (backward-compat usage)."""
        data = {"schema_id": schema_id}
        if job_name:
            data["job_name"] = job_name
        files = [("files", (filename, b"invoice text content", "text/plain"))]
        return client.post("/v1/extract/jobs", data=data, files=files)

    def _post_batch(self, client, filenames, schema_id="schema-001", job_name=None):
        """Post a multi-file batch job."""
        data = {"schema_id": schema_id}
        if job_name:
            data["job_name"] = job_name
        files = [
            ("files", (fn, f"content of {fn}".encode(), "text/plain"))
            for fn in filenames
        ]
        return client.post("/v1/extract/jobs", data=data, files=files)

    def test_202_single_file(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=_mock_schema_row()), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.stage_multiple_files"), \
             patch("extract.api.v1.jobs.db_repo.create_job", return_value=_mock_job_row(status="accepted")), \
             patch("extract.api.v1.jobs.db_repo.create_documents", return_value=True), \
             patch("extract.api.v1.jobs.validate_file_content", new=AsyncMock()), \
             patch("extract.api.v1.jobs.process_batch_job", new=AsyncMock()), \
             _patch_extract_limiter_free():
            resp = self._post_single(extract_test_client)

        assert resp.status_code == 202
        body = resp.json()
        assert "job_id" in body
        assert body["file_count"] == 1

    def test_202_batch_three_files(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=_mock_schema_row()), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.stage_multiple_files"), \
             patch("extract.api.v1.jobs.db_repo.create_job",
                   return_value=_mock_job_row(status="accepted", file_count=3)), \
             patch("extract.api.v1.jobs.db_repo.create_documents", return_value=True), \
             patch("extract.api.v1.jobs.validate_file_content", new=AsyncMock()), \
             patch("extract.api.v1.jobs.process_batch_job", new=AsyncMock()), \
             _patch_extract_limiter_free():
            resp = self._post_batch(
                extract_test_client,
                ["invoice_001.txt", "invoice_002.txt", "invoice_003.txt"],
            )

        assert resp.status_code == 202
        body = resp.json()
        assert "job_id" in body
        assert body["file_count"] == 3

    def test_400_no_files(self, extract_test_client):
        """Submitting with no files at all is rejected before DB calls."""
        with _patch_extract_limiter_free():
            resp = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
            )
        # FastAPI 422 because files field is required, OR 400 from our guard
        assert resp.status_code in (400, 422)

    def test_400_duplicate_filenames(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=_mock_schema_row()), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.validate_file_content", new=AsyncMock()), \
             _patch_extract_limiter_free():
            files = [
                ("files", ("same.txt", b"content a", "text/plain")),
                ("files", ("same.txt", b"content b", "text/plain")),
            ]
            resp = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
                files=files,
            )
        assert resp.status_code == 400
        assert resp.json()["error"]["code"] == "DUPLICATE_FILE"

    def test_413_too_many_files(self, extract_test_client):
        from extract.settings import settings
        limit = settings.extract.max_files_per_job
        with patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.validate_file_content", new=AsyncMock()), \
             _patch_extract_limiter_free():
            files = [
                ("files", (f"file_{i}.txt", b"content", "text/plain"))
                for i in range(limit + 1)
            ]
            resp = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
                files=files,
            )
        assert resp.status_code == 413
        assert resp.json()["error"]["code"] == "TOO_MANY_FILES"

    def test_404_unknown_schema(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=None), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.validate_file_content", new=AsyncMock()), \
             _patch_extract_limiter_free():
            resp = self._post_single(extract_test_client, schema_id="nonexistent")

        assert resp.status_code == 404
        assert resp.json()["error"]["code"] == "SCHEMA_NOT_FOUND"

    def test_415_invalid_extension(self, extract_test_client):
        with patch("extract.api.v1.jobs.validate_file_extension", return_value=(False, "")), \
             _patch_extract_limiter_free():
            resp = self._post_single(extract_test_client, filename="report.pdf")

        assert resp.status_code == 415
        assert resp.json()["error"]["code"] == "UNSUPPORTED_FILE_TYPE"

    def test_415_invalid_content_one_file_in_batch(self, extract_test_client):
        """One bad file in a batch should result in 415 with details."""
        call_count = [0]

        async def _side_effect(file):
            call_count[0] += 1
            if file.filename == "bad.txt":
                raise ExtractException(415, "BAD_REQUEST", "File contains null bytes.")

        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=_mock_schema_row()), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.validate_file_content", side_effect=_side_effect), \
             _patch_extract_limiter_free():
            files = [
                ("files", ("ok.txt", b"good content", "text/plain")),
                ("files", ("bad.txt", b"\x00binary", "text/plain")),
            ]
            resp = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
                files=files,
            )
        assert resp.status_code == 415
        body = resp.json()
        assert body["error"]["code"] == "INVALID_FILE_CONTENT"
        assert len(body["error"]["details"]) == 1
        assert body["error"]["details"][0]["filename"] == "bad.txt"

    def test_429_extract_limiter_full(self, extract_test_client):
        locked = Mock()
        locked.locked.return_value = True
        with patch("extract.state.extract_limiter", locked):
            resp = self._post_single(extract_test_client)

        assert resp.status_code == 429
        assert resp.json()["error"]["code"] == "RATE_LIMIT_EXCEEDED"

    def test_500_staging_failure(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=_mock_schema_row()), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.validate_file_content", new=AsyncMock()), \
             patch("extract.api.v1.jobs.stage_multiple_files", side_effect=IOError("disk full")), \
             _patch_extract_limiter_free():
            resp = self._post_single(extract_test_client)

        assert resp.status_code == 500
        assert resp.json()["error"]["code"] == "FILE_STAGING_ERROR"

    def test_500_db_create_returns_none(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=_mock_schema_row()), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.validate_file_content", new=AsyncMock()), \
             patch("extract.api.v1.jobs.stage_multiple_files"), \
             patch("extract.api.v1.jobs.db_repo.create_job", return_value=None), \
             patch("extract.api.v1.jobs.cleanup_staging_directory"), \
             _patch_extract_limiter_free():
            resp = self._post_single(extract_test_client)

        assert resp.status_code == 500
        assert resp.json()["error"]["code"] == "DATABASE_ERROR"

    def test_500_documents_create_fails(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=_mock_schema_row()), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.validate_file_content", new=AsyncMock()), \
             patch("extract.api.v1.jobs.stage_multiple_files"), \
             patch("extract.api.v1.jobs.db_repo.create_job", return_value=_mock_job_row(status="accepted")), \
             patch("extract.api.v1.jobs.db_repo.create_documents", return_value=False), \
             patch("extract.api.v1.jobs.db_repo.delete_job"), \
             patch("extract.api.v1.jobs.cleanup_staging_directory"), \
             _patch_extract_limiter_free():
            resp = self._post_single(extract_test_client)

        assert resp.status_code == 500
        assert resp.json()["error"]["code"] == "DATABASE_ERROR"


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs
# ---------------------------------------------------------------------------

class TestListExtractJobs:
    def test_200_empty_list(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([], 0)):
            resp = extract_test_client.get("/v1/extract/jobs")

        assert resp.status_code == 200
        body = resp.json()
        assert body["pagination"]["total"] == 0
        assert body["data"] == []

    def test_200_with_results(self, extract_test_client):
        rows = [_mock_job_row(job_id=f"job-{i}", job_name=f"job {i}") for i in range(3)]
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=(rows, 3)):
            resp = extract_test_client.get("/v1/extract/jobs")

        assert resp.status_code == 200
        assert resp.json()["pagination"]["total"] == 3
        assert len(resp.json()["data"]) == 3

    def test_file_count_in_list_item(self, extract_test_client):
        row = _mock_job_row(file_count=5)
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([row], 1)):
            resp = extract_test_client.get("/v1/extract/jobs")

        item = resp.json()["data"][0]
        assert item["file_count"] == 5

    def test_status_filter_passed_to_db(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([], 0)) as mock_list:
            extract_test_client.get("/v1/extract/jobs?status=completed")
        mock_list.assert_called_once_with(
            status="completed", schema_id=None, limit=20, offset=0, latest=False
        )

    def test_schema_id_filter_passed_to_db(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([], 0)) as mock_list:
            extract_test_client.get("/v1/extract/jobs?schema_id=schema-001")
        mock_list.assert_called_once_with(
            status=None, schema_id="schema-001", limit=20, offset=0, latest=False
        )

    def test_pagination_params_passed_to_db(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([], 0)) as mock_list:
            extract_test_client.get("/v1/extract/jobs?limit=5&offset=10")
        mock_list.assert_called_once_with(
            status=None, schema_id=None, limit=5, offset=10, latest=False
        )

    def test_400_invalid_status_value(self, extract_test_client):
        resp = extract_test_client.get("/v1/extract/jobs?status=invalid_status")
        assert resp.status_code == 400
        assert resp.json()["error"]["code"] == "INVALID_PARAMETER"

    def test_completed_with_errors_is_valid_status(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([], 0)) as mock_list:
            resp = extract_test_client.get("/v1/extract/jobs?status=completed_with_errors")
        assert resp.status_code == 200

    def test_latest_flag_sets_limit_1(self, extract_test_client):
        row = _mock_job_row()
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([row], 1)):
            resp = extract_test_client.get("/v1/extract/jobs?latest=true")

        assert resp.status_code == 200
        assert resp.json()["pagination"]["limit"] == 1
        assert resp.json()["pagination"]["offset"] == 0

    def test_limit_out_of_range_returns_422(self, extract_test_client):
        resp = extract_test_client.get("/v1/extract/jobs?limit=0")
        assert resp.status_code == 422

    def test_response_fields_present(self, extract_test_client):
        row = _mock_job_row()
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([row], 1)):
            resp = extract_test_client.get("/v1/extract/jobs")

        item = resp.json()["data"][0]
        assert item["job_id"] == "job-001"
        assert item["schema_id"] == "schema-001"
        assert item["status"] == "completed"
        assert "submitted_at" in item


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id} — single-file jobs
# ---------------------------------------------------------------------------

class TestGetExtractJobSingleFile:
    """Single-file jobs are stored as batch jobs with one document row."""

    def test_200_completed_job(self, extract_test_client):
        row = _mock_job_row()
        doc = _mock_doc_row(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_documents_by_job", return_value=[doc]):
            resp = extract_test_client.get("/v1/extract/jobs/job-001")

        assert resp.status_code == 200
        body = resp.json()
        assert body["job_id"] == "job-001"
        assert body["status"] == "completed"
        assert body["documents"] is not None
        assert len(body["documents"]) == 1

    def test_200_in_progress_job(self, extract_test_client):
        row = _mock_job_row(status="in_progress", completed_at=None, job_metadata={"phase": "extracting"})
        doc = _mock_doc_row(status="in_progress")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_documents_by_job", return_value=[doc]):
            resp = extract_test_client.get("/v1/extract/jobs/job-001")

        assert resp.status_code == 200
        assert resp.json()["status"] == "in_progress"
        assert resp.json()["metadata"]["phase"] == "extracting"

    def test_404_unknown_job(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=None):
            resp = extract_test_client.get("/v1/extract/jobs/nonexistent")

        assert resp.status_code == 404
        assert resp.json()["error"]["code"] == "RESOURCE_NOT_FOUND"

    def test_error_field_present_on_failed_job(self, extract_test_client):
        row = _mock_job_row(status="failed", error="CONTEXT_LIMIT_EXCEEDED")
        doc = _mock_doc_row(status="failed", error="CONTEXT_LIMIT_EXCEEDED")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_documents_by_job", return_value=[doc]):
            resp = extract_test_client.get("/v1/extract/jobs/job-001")

        assert resp.status_code == 200
        assert resp.json()["error"] == "CONTEXT_LIMIT_EXCEEDED"


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id} — batch jobs
# ---------------------------------------------------------------------------

class TestGetExtractJobBatch:
    def _make_batch_job(self, status="in_progress", file_count=3):
        return _mock_job_row(
            job_id="batch-001",
            status=status,
            file_count=file_count,
            error=None if status != "completed_with_errors" else "1 of 3 files failed extraction.",
        )

    def _make_docs(self, statuses):
        return [
            _mock_doc_row(
                doc_id=f"doc-{i:03d}",
                job_id="batch-001",
                filename=f"invoice_{i:03d}.txt",
                status=s,
                error="CONTEXT_LIMIT_EXCEEDED" if s == "failed" else None,
            )
            for i, s in enumerate(statuses, 1)
        ]

    def test_200_in_progress_has_documents_array(self, extract_test_client):
        row = self._make_batch_job(status="in_progress")
        docs = self._make_docs(["completed", "failed", "in_progress"])
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_documents_by_job", return_value=docs):
            resp = extract_test_client.get("/v1/extract/jobs/batch-001")

        assert resp.status_code == 200
        body = resp.json()
        assert body["documents"] is not None
        assert len(body["documents"]) == 3
        assert body["document"] is None
        assert body["files_completed"] == 1
        assert body["files_failed"] == 1
        assert body["files_pending"] == 1  # in_progress counts as pending
        assert body["file_count"] == 3

    def test_200_completed_with_errors(self, extract_test_client):
        row = self._make_batch_job(status="completed_with_errors")
        docs = self._make_docs(["completed", "failed", "completed"])
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_documents_by_job", return_value=docs):
            resp = extract_test_client.get("/v1/extract/jobs/batch-001")

        body = resp.json()
        assert body["status"] == "completed_with_errors"
        assert body["files_completed"] == 2
        assert body["files_failed"] == 1
        assert body["error"] == "1 of 3 files failed extraction."

    def test_document_error_in_batch_response(self, extract_test_client):
        row = self._make_batch_job(status="completed_with_errors")
        docs = self._make_docs(["completed", "failed"])
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_documents_by_job", return_value=docs):
            resp = extract_test_client.get("/v1/extract/jobs/batch-001")

        failed_doc = next(d for d in resp.json()["documents"] if d["status"] == "failed")
        assert failed_doc["error"] == "CONTEXT_LIMIT_EXCEEDED"


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id}/results/{doc_id} — single-file result via
# the per-document endpoint (single-file jobs are batch jobs with one doc)
# ---------------------------------------------------------------------------

_RECONSTRUCTED_PAYLOAD = {
    "data": {"extraction": {"invoice_number": "INV-001"}, "schema_id": "schema-001", "source": {}},
    "status": "completed",
    "meta": {"model": "granite", "processing_time_ms": 1200, "validation_attempts": 1},
    "usage": {"input_tokens": 400, "output_tokens": 60, "total_tokens": 460},
}
_FILE_DATA = {"extraction": {"invoice_number": "INV-001"}}


class TestGetExtractJobResult:
    def test_200_completed_returns_result(self, extract_test_client):
        row = _mock_job_row(status="completed")
        doc = _mock_doc_row(doc_id="doc-001", status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc), \
             patch("extract.api.v1.jobs.read_doc_result_file", return_value=_FILE_DATA), \
             patch("extract.api.v1.jobs.build_result_payload", return_value=_RECONSTRUCTED_PAYLOAD):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        assert resp.status_code == 200
        body = resp.json()
        assert "data" in body
        assert "meta" in body
        assert "usage" in body
        assert body["status"] == "completed"

    def test_202_while_in_progress(self, extract_test_client):
        row = _mock_job_row(status="in_progress")
        doc = _mock_doc_row(doc_id="doc-001", status="in_progress")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        assert resp.status_code == 202
        assert resp.json()["status"] == "in_progress"

    def test_202_while_accepted(self, extract_test_client):
        row = _mock_job_row(status="accepted")
        doc = _mock_doc_row(doc_id="doc-001", status="pending")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        assert resp.status_code == 202

    def test_404_unknown_job(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=None):
            resp = extract_test_client.get("/v1/extract/jobs/nonexistent/results/doc-001")

        assert resp.status_code == 404
        assert resp.json()["error"]["code"] == "RESOURCE_NOT_FOUND"

    def test_410_failed_doc(self, extract_test_client):
        row = _mock_job_row(status="completed_with_errors")
        doc = _mock_doc_row(doc_id="doc-001", status="failed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        assert resp.status_code == 410

    def test_500_missing_result_file(self, extract_test_client):
        row = _mock_job_row(status="completed")
        doc = _mock_doc_row(doc_id="doc-001", status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc), \
             patch("extract.api.v1.jobs.read_doc_result_file", return_value=None):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        assert resp.status_code == 500
        assert resp.json()["error"]["code"] == "INTERNAL_SERVER_ERROR"


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id}/results/{doc_id}
# ---------------------------------------------------------------------------

class TestGetDocumentResult:
    def test_200_completed_doc(self, extract_test_client):
        job_row = _mock_job_row(status="completed", file_count=3)
        doc_row = _mock_doc_row(doc_id="doc-001", job_id="job-001", status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job_row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc_row), \
             patch("extract.api.v1.jobs.read_doc_result_file", return_value=_FILE_DATA), \
             patch("extract.api.v1.jobs.build_result_payload", return_value=_RECONSTRUCTED_PAYLOAD):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        assert resp.status_code == 200
        body = resp.json()
        assert body["status"] == "completed"
        assert "data" in body

    def test_payload_reconstructed_from_db(self, extract_test_client):
        """build_result_payload is called with the job row, doc row, and file data."""
        job_row = _mock_job_row(status="completed", file_count=1)
        doc_row = _mock_doc_row(
            doc_id="doc-001", job_id="job-001", status="completed",
            usage_input_tokens=400, usage_output_tokens=60,
        )
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job_row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc_row), \
             patch("extract.api.v1.jobs.read_doc_result_file", return_value=_FILE_DATA), \
             patch("extract.api.v1.jobs.build_result_payload", return_value=_RECONSTRUCTED_PAYLOAD) as mock_build:
            extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        mock_build.assert_called_once_with(job_row, doc_row, _FILE_DATA)

    def test_202_pending_doc(self, extract_test_client):
        job_row = _mock_job_row(status="in_progress", file_count=3)
        doc_row = _mock_doc_row(doc_id="doc-001", status="pending")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job_row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc_row):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        assert resp.status_code == 202
        assert resp.json()["status"] == "pending"

    def test_202_in_progress_doc(self, extract_test_client):
        job_row = _mock_job_row(status="in_progress", file_count=3)
        doc_row = _mock_doc_row(doc_id="doc-001", status="in_progress")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job_row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc_row):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        assert resp.status_code == 202

    def test_410_failed_doc(self, extract_test_client):
        job_row = _mock_job_row(status="completed_with_errors", file_count=3)
        doc_row = _mock_doc_row(doc_id="doc-002", status="failed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job_row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc_row):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-002")

        assert resp.status_code == 410
        assert resp.json()["error"]["code"] == "DOCUMENT_FAILED"

    def test_404_unknown_job(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=None):
            resp = extract_test_client.get("/v1/extract/jobs/bad-job/results/doc-001")

        assert resp.status_code == 404

    def test_404_unknown_doc(self, extract_test_client):
        job_row = _mock_job_row(status="completed", file_count=2)
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job_row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=None):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/bad-doc")

        assert resp.status_code == 404

    def test_404_doc_belongs_to_different_job(self, extract_test_client):
        job_row = _mock_job_row(status="completed", file_count=2)
        doc_row = _mock_doc_row(doc_id="doc-001", job_id="other-job", status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job_row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc_row):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        assert resp.status_code == 404

    def test_500_missing_result_file(self, extract_test_client):
        job_row = _mock_job_row(status="completed", file_count=2)
        doc_row = _mock_doc_row(doc_id="doc-001", status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job_row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc_row), \
             patch("extract.api.v1.jobs.read_doc_result_file", return_value=None):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001")

        assert resp.status_code == 500


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id}/results/{doc_id}/download
# ---------------------------------------------------------------------------

class TestDownloadDocumentResult:
    def test_200_download_completed(self, extract_test_client):
        job_row = _mock_job_row(status="completed", file_count=2)
        doc_row = _mock_doc_row(doc_id="doc-001", filename="invoice_001.txt", status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job_row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc_row), \
             patch("extract.api.v1.jobs.read_doc_result_file", return_value=_FILE_DATA), \
             patch("extract.api.v1.jobs.build_result_payload", return_value=_RECONSTRUCTED_PAYLOAD):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001/download")

        assert resp.status_code == 200
        assert "Content-Disposition" in resp.headers
        assert "invoice_001_result.json" in resp.headers["Content-Disposition"]
        assert resp.headers["content-type"].startswith("application/json")

    def test_410_failed_doc(self, extract_test_client):
        job_row = _mock_job_row(status="completed_with_errors", file_count=2)
        doc_row = _mock_doc_row(doc_id="doc-001", status="failed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job_row), \
             patch("extract.api.v1.jobs.db_repo.get_document_by_id", return_value=doc_row):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/results/doc-001/download")

        assert resp.status_code == 410

    def test_404_unknown_job(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=None):
            resp = extract_test_client.get("/v1/extract/jobs/bad/results/doc-001/download")

        assert resp.status_code == 404


# ---------------------------------------------------------------------------
# DELETE /v1/extract/jobs/{job_id}
# ---------------------------------------------------------------------------

class TestDeleteExtractJob:
    def test_204_completed_job_deleted(self, extract_test_client):
        row = _mock_job_row(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.delete_job_files"), \
             patch("extract.api.v1.jobs.db_repo.delete_job", return_value=True):
            resp = extract_test_client.delete("/v1/extract/jobs/job-001")

        assert resp.status_code == 204

    def test_204_failed_job_deleted(self, extract_test_client):
        row = _mock_job_row(status="failed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.delete_job_files"), \
             patch("extract.api.v1.jobs.db_repo.delete_job", return_value=True):
            resp = extract_test_client.delete("/v1/extract/jobs/job-001")

        assert resp.status_code == 204

    def test_204_completed_with_errors_deleted(self, extract_test_client):
        row = _mock_job_row(status="completed_with_errors", file_count=3)
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.delete_job_files"), \
             patch("extract.api.v1.jobs.db_repo.delete_job", return_value=True):
            resp = extract_test_client.delete("/v1/extract/jobs/job-001")

        assert resp.status_code == 204

    def test_404_unknown_job(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=None):
            resp = extract_test_client.delete("/v1/extract/jobs/nonexistent")

        assert resp.status_code == 404
        assert resp.json()["error"]["code"] == "RESOURCE_NOT_FOUND"

    def test_409_active_job_locked(self, extract_test_client):
        for active_status in ("accepted", "in_progress"):
            row = _mock_job_row(status=active_status)
            with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row):
                resp = extract_test_client.delete("/v1/extract/jobs/job-001")

            assert resp.status_code == 409, f"Expected 409 for status={active_status}"
            assert resp.json()["error"]["code"] == "RESOURCE_LOCKED"

    def test_500_db_delete_fails(self, extract_test_client):
        row = _mock_job_row(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.delete_job_files"), \
             patch("extract.api.v1.jobs.db_repo.delete_job", return_value=False):
            resp = extract_test_client.delete("/v1/extract/jobs/job-001")

        assert resp.status_code == 500
        assert resp.json()["error"]["code"] == "INTERNAL_SERVER_ERROR"

    def test_result_files_deleted(self, extract_test_client):
        """delete_job_files must be called before the DB row is removed."""
        row = _mock_job_row(status="completed")
        call_order = []

        def _mock_delete_files(job_id):
            call_order.append("files")

        def _mock_delete_db(job_id):
            call_order.append("db")
            return True

        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.delete_job_files", side_effect=_mock_delete_files), \
             patch("extract.api.v1.jobs.db_repo.delete_job", side_effect=_mock_delete_db):
            extract_test_client.delete("/v1/extract/jobs/job-001")

        assert call_order == ["files", "db"]


# ---------------------------------------------------------------------------
# DELETE /v1/extract/jobs (bulk)
# ---------------------------------------------------------------------------

class TestBulkDeleteExtractJobs:
    def test_204_no_active_jobs(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.has_active_jobs", return_value=False), \
             patch("extract.api.v1.jobs.delete_all_job_files"), \
             patch("extract.api.v1.jobs.db_repo.delete_all_jobs", return_value=True):
            resp = extract_test_client.delete("/v1/extract/jobs?confirm=true")

        assert resp.status_code == 204

    def test_400_missing_confirm(self, extract_test_client):
        resp = extract_test_client.delete("/v1/extract/jobs")
        assert resp.status_code == 400
        assert resp.json()["error"]["code"] == "CONFIRMATION_REQUIRED"

    def test_400_confirm_not_true(self, extract_test_client):
        for val in ("false", "yes", "1"):
            resp = extract_test_client.delete(f"/v1/extract/jobs?confirm={val}")
            assert resp.status_code == 400, f"Expected 400 for confirm={val}"
            assert resp.json()["error"]["code"] == "CONFIRMATION_REQUIRED"

    def test_409_active_jobs_exist(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.has_active_jobs", return_value=True):
            resp = extract_test_client.delete("/v1/extract/jobs?confirm=true")

        assert resp.status_code == 409
        assert resp.json()["error"]["code"] == "RESOURCE_LOCKED"

    def test_500_db_delete_fails(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.has_active_jobs", return_value=False), \
             patch("extract.api.v1.jobs.delete_all_job_files"), \
             patch("extract.api.v1.jobs.db_repo.delete_all_jobs", return_value=False):
            resp = extract_test_client.delete("/v1/extract/jobs?confirm=true")

        assert resp.status_code == 500
        assert resp.json()["error"]["code"] == "INTERNAL_SERVER_ERROR"

    def test_all_job_files_deleted(self, extract_test_client):
        """delete_all_job_files must be called before the DB rows are removed."""
        call_order = []
        with patch("extract.api.v1.jobs.db_repo.has_active_jobs", return_value=False), \
             patch("extract.api.v1.jobs.delete_all_job_files", side_effect=lambda: call_order.append("files")), \
             patch("extract.api.v1.jobs.db_repo.delete_all_jobs", side_effect=lambda: call_order.append("db") or True):
            extract_test_client.delete("/v1/extract/jobs?confirm=true")

        assert call_order == ["files", "db"]


# ---------------------------------------------------------------------------
# Patch helpers
# ---------------------------------------------------------------------------

def _patch_extract_limiter_free():
    """Patch extract_limiter so locked() is False and async-with passes."""
    import asyncio
    return patch("extract.state.extract_limiter", asyncio.BoundedSemaphore(1))
