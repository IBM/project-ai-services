"""
Unit tests for job CRUD endpoints:
  POST   /v1/extract/jobs
  GET    /v1/extract/jobs
  GET    /v1/extract/jobs/{job_id}
  GET    /v1/extract/jobs/{job_id}/result
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
    document_name="contract.txt",
    source_type="txt",
    submitted_at=None,
    completed_at=None,
    error=None,
    job_metadata=None,
):
    row = Mock()
    row.job_id = job_id
    row.job_name = job_name
    row.schema_id = schema_id
    row.status = status
    row.document_name = document_name
    row.source_type = source_type
    row.submitted_at = submitted_at or datetime(2026, 7, 7, 10, 0, 0, tzinfo=timezone.utc)
    row.completed_at = completed_at or datetime(2026, 7, 7, 10, 5, 0, tzinfo=timezone.utc)
    row.error = error
    row.job_metadata = job_metadata
    return row


def _mock_schema_row(schema_id="schema-001"):
    row = Mock()
    row.schema_id = schema_id
    return row


# ---------------------------------------------------------------------------
# POST /v1/extract/jobs
# ---------------------------------------------------------------------------

class TestCreateExtractJob:
    def _post_job(self, client, filename="doc.txt", schema_id="schema-001", job_name=None):
        data = {"schema_id": schema_id}
        if job_name:
            data["job_name"] = job_name
        files = {"file": (filename, b"invoice text content", "text/plain")}
        return client.post("/v1/extract/jobs", data=data, files=files)

    def test_202_accepted(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=_mock_schema_row()), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.stage_uploaded_file"), \
             patch("extract.api.v1.jobs.db_repo.create_job", return_value=_mock_job_row(status="accepted")), \
             patch("extract.api.v1.jobs._validate_file_content", new=AsyncMock()), \
             _patch_job_limiter_free():
            resp = self._post_job(extract_test_client)

        assert resp.status_code == 202
        assert "job_id" in resp.json()

    def test_404_unknown_schema(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=None), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs._validate_file_content", new=AsyncMock()), \
             _patch_job_limiter_free():
            resp = self._post_job(extract_test_client, schema_id="nonexistent")

        assert resp.status_code == 404
        assert resp.json()["error"]["code"] == "SCHEMA_NOT_FOUND"

    def test_415_invalid_extension(self, extract_test_client):
        with patch("extract.api.v1.jobs.validate_file_extension", return_value=(False, "")), \
             _patch_job_limiter_free():
            resp = self._post_job(extract_test_client, filename="report.pdf")

        assert resp.status_code == 415
        assert resp.json()["error"]["code"] == "UNSUPPORTED_FILE_TYPE"

    def test_429_job_limiter_full(self, extract_test_client):
        locked = Mock()
        locked.locked.return_value = True
        with patch("extract.state.job_limiter", locked):
            resp = self._post_job(extract_test_client)

        assert resp.status_code == 429
        assert resp.json()["error"]["code"] == "RATE_LIMIT_EXCEEDED"

    def test_500_staging_failure(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=_mock_schema_row()), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs._validate_file_content", new=AsyncMock()), \
             patch("extract.api.v1.jobs.stage_uploaded_file", side_effect=IOError("disk full")), \
             _patch_job_limiter_free():
            resp = self._post_job(extract_test_client)

        assert resp.status_code == 500
        assert resp.json()["error"]["code"] == "FILE_STAGING_ERROR"

    def test_500_db_create_returns_none(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=_mock_schema_row()), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs._validate_file_content", new=AsyncMock()), \
             patch("extract.api.v1.jobs.stage_uploaded_file"), \
             patch("extract.api.v1.jobs.db_repo.create_job", return_value=None), \
             patch("extract.api.v1.jobs.cleanup_staging_directory"), \
             _patch_job_limiter_free():
            resp = self._post_job(extract_test_client)

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
        assert item["document_name"] == "contract.txt"
        assert "submitted_at" in item


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id}
# ---------------------------------------------------------------------------

class TestGetExtractJob:
    def test_200_completed_job(self, extract_test_client):
        row = _mock_job_row()
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row):
            resp = extract_test_client.get("/v1/extract/jobs/job-001")

        assert resp.status_code == 200
        body = resp.json()
        assert body["job_id"] == "job-001"
        assert body["status"] == "completed"
        assert body["document"]["name"] == "contract.txt"
        assert body["document"]["source_type"] == "txt"

    def test_200_in_progress_job(self, extract_test_client):
        row = _mock_job_row(status="in_progress", completed_at=None, job_metadata={"phase": "extracting"})
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row):
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
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row):
            resp = extract_test_client.get("/v1/extract/jobs/job-001")

        assert resp.status_code == 200
        assert resp.json()["error"] == "CONTEXT_LIMIT_EXCEEDED"


# ---------------------------------------------------------------------------
# GET /v1/extract/jobs/{job_id}/result
# ---------------------------------------------------------------------------

class TestGetExtractJobResult:
    _RESULT_PAYLOAD = {
        "data": {"extraction": {"invoice_number": "INV-001"}, "schema_id": "schema-001", "source": {}},
        "status": "completed",
        "meta": {"model": "granite", "processing_time_ms": 1200, "validation_attempts": 1},
        "usage": {"input_tokens": 400, "output_tokens": 60, "total_tokens": 460},
    }

    def test_200_completed_returns_result(self, extract_test_client):
        row = _mock_job_row(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.read_result_file", return_value=self._RESULT_PAYLOAD):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/result")

        assert resp.status_code == 200
        body = resp.json()
        assert "data" in body
        assert "meta" in body
        assert "usage" in body
        assert body["status"] == "completed"

    def test_202_while_in_progress(self, extract_test_client):
        row = _mock_job_row(status="in_progress")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/result")

        assert resp.status_code == 202
        assert resp.json()["status"] == "in_progress"

    def test_202_while_accepted(self, extract_test_client):
        row = _mock_job_row(status="accepted")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/result")

        assert resp.status_code == 202

    def test_404_unknown_job(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=None):
            resp = extract_test_client.get("/v1/extract/jobs/nonexistent/result")

        assert resp.status_code == 404
        assert resp.json()["error"]["code"] == "RESOURCE_NOT_FOUND"

    def test_409_failed_job(self, extract_test_client):
        row = _mock_job_row(status="failed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/result")

        assert resp.status_code == 409
        assert resp.json()["error"]["code"] == "JOB_FAILED"

    def test_500_missing_result_file(self, extract_test_client):
        row = _mock_job_row(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=row), \
             patch("extract.api.v1.jobs.read_result_file", return_value=None):
            resp = extract_test_client.get("/v1/extract/jobs/job-001/result")

        assert resp.status_code == 500
        assert resp.json()["error"]["code"] == "INTERNAL_SERVER_ERROR"


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

def _patch_job_limiter_free():
    """Patch job_limiter so locked() is False and async-with passes."""
    import asyncio
    return patch("extract.state.job_limiter", asyncio.BoundedSemaphore(1))
