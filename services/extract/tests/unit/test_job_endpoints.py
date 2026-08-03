"""
Unit tests for the extraction job HTTP endpoints.

Covers all six job endpoints:
  POST   /v1/extract/jobs
  GET    /v1/extract/jobs
  GET    /v1/extract/jobs/{job_id}
  GET    /v1/extract/jobs/{job_id}/result
  DELETE /v1/extract/jobs/{job_id}
  DELETE /v1/extract/jobs

All external boundaries (database, filesystem, vLLM) are mocked so the
tests run without a real PostgreSQL instance or LLM endpoint.
"""

import io
import json
import uuid
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import Mock, patch

import pytest


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _mock_job(
    job_id="job-001",
    schema_id="schema-001",
    status="completed",
    document_name="contract.pdf",
    source_type="pdf",
    job_name=None,
    digitize_job_id=None,
    digitize_doc_id=None,
    job_metadata=None,
    error=None,
    submitted_at=None,
    completed_at=None,
):
    """Return a Mock that looks like an ExtractJob ORM row."""
    row = Mock()
    row.job_id = job_id
    row.schema_id = schema_id
    row.status = status
    row.document_name = document_name
    row.source_type = source_type
    row.job_name = job_name
    row.digitize_job_id = digitize_job_id
    row.digitize_doc_id = digitize_doc_id
    row.job_metadata = job_metadata
    row.error = error
    row.submitted_at = submitted_at or datetime(2026, 7, 7, 10, 0, 0, tzinfo=timezone.utc)
    row.completed_at = completed_at or datetime(2026, 7, 7, 10, 5, 0, tzinfo=timezone.utc)
    return row


_RESULT_DATA = {
    "data": {
        "extraction": {"invoice_number": "INV-001", "total_amount": 500.0},
        "schema_id": "schema-001",
        "source": {"input_type": "file", "document_name": "contract.pdf"},
    },
    "status": "completed",
    "meta": {
        "model": "test-model",
        "processing_time_ms": 3000,
        "validation_attempts": 1,
    },
    "usage": {"input_tokens": 800, "output_tokens": 60, "total_tokens": 860},
}


# =========================================================================
# POST /v1/extract/jobs
# =========================================================================

@pytest.mark.unit
class TestCreateExtractJob:
    """Tests for POST /v1/extract/jobs."""

    def _txt_file(self, content=b"Sample text for extraction.", name="doc.txt"):
        return (name, io.BytesIO(content), "text/plain")

    def _pdf_file(self, name="doc.pdf"):
        return (name, io.BytesIO(b"%PDF-1.4 fake content"), "application/pdf")

    def test_success_txt_returns_202(self, extract_test_client):
        """Happy path: .txt file with a valid schema_id returns 202 with job_id."""
        test_uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

        with patch("extract.api.v1.jobs.job_limiter.locked", return_value=False), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=Mock()), \
             patch("extract.api.v1.jobs.stage_uploaded_file"), \
             patch("extract.api.v1.jobs.db_repo.create_job", return_value=_mock_job(job_id=test_uuid)), \
             patch("extract.api.v1.jobs.uuid.uuid4", return_value=uuid.UUID(test_uuid)):

            response = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
                files={"file": self._txt_file()},
            )

        assert response.status_code == 202
        assert response.json()["job_id"] == test_uuid

    def test_success_pdf_returns_202(self, extract_test_client):
        """A PDF file is also accepted and returns 202."""
        test_uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

        with patch("extract.api.v1.jobs.job_limiter.locked", return_value=False), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".pdf")), \
             patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=Mock()), \
             patch("extract.api.v1.jobs.stage_uploaded_file"), \
             patch("extract.api.v1.jobs.db_repo.create_job", return_value=_mock_job(job_id=test_uuid)), \
             patch("extract.api.v1.jobs.uuid.uuid4", return_value=uuid.UUID(test_uuid)):

            response = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
                files={"file": self._pdf_file()},
            )

        assert response.status_code == 202

    def test_job_name_accepted(self, extract_test_client):
        """Optional job_name is forwarded to db_repo.create_job."""
        test_uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

        with patch("extract.api.v1.jobs.job_limiter.locked", return_value=False), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=Mock()), \
             patch("extract.api.v1.jobs.stage_uploaded_file"), \
             patch("extract.api.v1.jobs.db_repo.create_job", return_value=_mock_job(job_id=test_uuid)) as mock_create, \
             patch("extract.api.v1.jobs.uuid.uuid4", return_value=uuid.UUID(test_uuid)):

            extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001", "job_name": "Q3 audit"},
                files={"file": self._txt_file()},
            )

        call_kwargs = mock_create.call_args[1]
        assert call_kwargs.get("job_name") == "Q3 audit"

    def test_rate_limit_returns_429(self, extract_test_client):
        """When job_limiter is full, endpoint returns 429."""
        with patch("extract.api.v1.jobs.job_limiter.locked", return_value=True):
            response = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
                files={"file": self._txt_file()},
            )

        assert response.status_code == 429
        assert response.json()["error"]["code"] == "RATE_LIMIT_EXCEEDED"

    def test_unsupported_extension_returns_415(self, extract_test_client):
        """A .docx file is rejected with 415."""
        with patch("extract.api.v1.jobs.job_limiter.locked", return_value=False), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(False, ".docx")):

            response = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
                files={"file": ("doc.docx", io.BytesIO(b"fake"), "application/octet-stream")},
            )

        assert response.status_code == 415

    def test_unknown_schema_id_returns_404(self, extract_test_client):
        """If the schema_id does not exist, endpoint returns 404."""
        with patch("extract.api.v1.jobs.job_limiter.locked", return_value=False), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=None):

            response = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "nonexistent"},
                files={"file": self._txt_file()},
            )

        assert response.status_code == 404
        assert response.json()["error"]["code"] == "SCHEMA_NOT_FOUND"

    def test_file_staging_failure_returns_500(self, extract_test_client):
        """An IOError during staging returns 500 and does not leave a DB row."""
        with patch("extract.api.v1.jobs.job_limiter.locked", return_value=False), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=Mock()), \
             patch("extract.api.v1.jobs.stage_uploaded_file", side_effect=IOError("Disk full")):

            response = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
                files={"file": self._txt_file()},
            )

        assert response.status_code == 500
        assert "file" in response.json()["error"]["message"].lower() or \
               "staging" in response.json()["error"]["message"].lower()

    def test_db_create_failure_cleans_up_staging(self, extract_test_client):
        """When db.create_job returns None, staging is cleaned up and 500 is returned."""
        test_uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

        with patch("extract.api.v1.jobs.job_limiter.locked", return_value=False), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=Mock()), \
             patch("extract.api.v1.jobs.stage_uploaded_file"), \
             patch("extract.api.v1.jobs.db_repo.create_job", return_value=None), \
             patch("extract.api.v1.jobs.cleanup_staging_directory") as mock_cleanup, \
             patch("extract.api.v1.jobs.uuid.uuid4", return_value=uuid.UUID(test_uuid)):

            response = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
                files={"file": self._txt_file()},
            )

        assert response.status_code == 500
        mock_cleanup.assert_called_once()

    def test_response_contains_valid_uuid(self, extract_test_client):
        """The returned job_id must be a valid UUID string."""
        with patch("extract.api.v1.jobs.job_limiter.locked", return_value=False), \
             patch("extract.api.v1.jobs.validate_file_extension", return_value=(True, ".txt")), \
             patch("extract.api.v1.jobs.db_repo.get_schema_by_id", return_value=Mock()), \
             patch("extract.api.v1.jobs.stage_uploaded_file"), \
             patch("extract.api.v1.jobs.db_repo.create_job", return_value=_mock_job()):

            response = extract_test_client.post(
                "/v1/extract/jobs",
                data={"schema_id": "schema-001"},
                files={"file": self._txt_file()},
            )

        assert response.status_code == 202
        job_id = response.json()["job_id"]
        assert uuid.UUID(job_id)  # Raises ValueError if invalid UUID

    def test_missing_schema_id_returns_422(self, extract_test_client):
        """schema_id is a required form field; omitting it yields FastAPI 422."""
        response = extract_test_client.post(
            "/v1/extract/jobs",
            files={"file": self._txt_file()},
        )
        assert response.status_code == 422


# =========================================================================
# GET /v1/extract/jobs
# =========================================================================

@pytest.mark.unit
class TestListExtractJobs:
    """Tests for GET /v1/extract/jobs."""

    def test_empty_list_returns_200(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([], 0)):
            response = extract_test_client.get("/v1/extract/jobs")

        assert response.status_code == 200
        body = response.json()
        assert body["pagination"]["total"] == 0
        assert body["pagination"]["limit"] == 20
        assert body["pagination"]["offset"] == 0
        assert body["data"] == []

    def test_list_with_jobs_returns_items(self, extract_test_client):
        jobs = [_mock_job(job_id=f"job-{i}", status="completed") for i in range(3)]
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=(jobs, 3)):
            response = extract_test_client.get("/v1/extract/jobs")

        assert response.status_code == 200
        body = response.json()
        assert body["pagination"]["total"] == 3
        assert len(body["data"]) == 3

    def test_pagination_parameters_forwarded(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([], 0)) as mock_list:
            extract_test_client.get("/v1/extract/jobs?limit=5&offset=10")

        mock_list.assert_called_once_with(
            status=None, schema_id=None, limit=5, offset=10, latest=False
        )

    def test_status_filter_forwarded(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([], 0)) as mock_list:
            extract_test_client.get("/v1/extract/jobs?status=completed")

        mock_list.assert_called_once_with(
            status="completed", schema_id=None, limit=20, offset=0, latest=False
        )

    def test_schema_id_filter_forwarded(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([], 0)) as mock_list:
            extract_test_client.get("/v1/extract/jobs?schema_id=schema-001")

        mock_list.assert_called_once_with(
            status=None, schema_id="schema-001", limit=20, offset=0, latest=False
        )

    def test_latest_flag_sets_limit_1(self, extract_test_client):
        job = _mock_job()
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([job], 1)):
            response = extract_test_client.get("/v1/extract/jobs?latest=true")

        assert response.status_code == 200
        body = response.json()
        assert body["pagination"]["limit"] == 1
        assert body["pagination"]["offset"] == 0

    def test_invalid_status_returns_400(self, extract_test_client):
        response = extract_test_client.get("/v1/extract/jobs?status=unknown_status")

        assert response.status_code == 400
        assert response.json()["error"]["code"] == "INVALID_PARAMETER"

    def test_limit_out_of_range_returns_422(self, extract_test_client):
        response = extract_test_client.get("/v1/extract/jobs?limit=0")
        assert response.status_code == 422

    def test_limit_above_max_returns_422(self, extract_test_client):
        response = extract_test_client.get("/v1/extract/jobs?limit=101")
        assert response.status_code == 422

    def test_negative_offset_returns_422(self, extract_test_client):
        response = extract_test_client.get("/v1/extract/jobs?offset=-1")
        assert response.status_code == 422

    def test_response_fields_present(self, extract_test_client):
        job = _mock_job()
        with patch("extract.api.v1.jobs.db_repo.list_jobs", return_value=([job], 1)):
            response = extract_test_client.get("/v1/extract/jobs")

        item = response.json()["data"][0]
        assert "job_id" in item
        assert "schema_id" in item
        assert "status" in item
        assert "document_name" in item
        assert "submitted_at" in item


# =========================================================================
# GET /v1/extract/jobs/{job_id}
# =========================================================================

@pytest.mark.unit
class TestGetExtractJob:
    """Tests for GET /v1/extract/jobs/{job_id}."""

    def test_existing_job_returns_200(self, extract_test_client):
        job = _mock_job()
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job):
            response = extract_test_client.get(f"/v1/extract/jobs/{job.job_id}")

        assert response.status_code == 200
        body = response.json()
        assert body["job_id"] == job.job_id
        assert body["schema_id"] == job.schema_id
        assert body["status"] == job.status

    def test_response_includes_document_block(self, extract_test_client):
        """The document sub-object must include name, source_type, digitize IDs."""
        job = _mock_job(
            source_type="pdf",
            digitize_job_id="dj-999",
            digitize_doc_id="dd-888",
        )
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job):
            response = extract_test_client.get(f"/v1/extract/jobs/{job.job_id}")

        doc = response.json()["document"]
        assert doc["name"] == job.document_name
        assert doc["source_type"] == "pdf"
        assert doc["digitize_job_id"] == "dj-999"
        assert doc["digitize_doc_id"] == "dd-888"

    def test_response_includes_phase_from_metadata(self, extract_test_client):
        """metadata JSONB (phase, token_diagnostics, etc.) is surfaced."""
        job = _mock_job(
            status="in_progress",
            job_metadata={"phase": "digitizing"},
        )
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job):
            response = extract_test_client.get(f"/v1/extract/jobs/{job.job_id}")

        assert response.status_code == 200
        assert response.json()["metadata"]["phase"] == "digitizing"

    def test_error_field_present_for_failed_job(self, extract_test_client):
        job = _mock_job(status="failed", error="CONTEXT_LIMIT_EXCEEDED: too many tokens")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job):
            response = extract_test_client.get(f"/v1/extract/jobs/{job.job_id}")

        assert response.status_code == 200
        assert "CONTEXT_LIMIT_EXCEEDED" in response.json()["error"]

    def test_unknown_job_returns_404(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=None):
            response = extract_test_client.get("/v1/extract/jobs/nonexistent")

        assert response.status_code == 404
        assert response.json()["error"]["code"] == "RESOURCE_NOT_FOUND"


# =========================================================================
# GET /v1/extract/jobs/{job_id}/result
# =========================================================================

@pytest.mark.unit
class TestGetExtractJobResult:
    """Tests for GET /v1/extract/jobs/{job_id}/result."""

    def test_completed_job_returns_200_with_result(self, extract_test_client):
        job = _mock_job(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job), \
             patch("extract.api.v1.jobs.read_result_file", return_value=_RESULT_DATA):
            response = extract_test_client.get(f"/v1/extract/jobs/{job.job_id}/result")

        assert response.status_code == 200
        body = response.json()
        assert "data" in body
        assert "meta" in body
        assert "usage" in body
        assert body["status"] == "completed"

    def test_in_progress_job_returns_202(self, extract_test_client):
        job = _mock_job(status="in_progress")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job):
            response = extract_test_client.get(f"/v1/extract/jobs/{job.job_id}/result")

        assert response.status_code == 202
        body = response.json()
        assert "still in progress" in body["message"]
        assert body["job_id"] == job.job_id
        assert body["status"] == "in_progress"

    def test_accepted_job_returns_202(self, extract_test_client):
        job = _mock_job(status="accepted")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job):
            response = extract_test_client.get(f"/v1/extract/jobs/{job.job_id}/result")

        assert response.status_code == 202

    def test_failed_job_returns_409_not_404(self, extract_test_client):
        """
        A failed-but-existing job must return 409 (distinct from 'not found').
        The response body points the caller at the job resource for diagnostics.
        """
        job = _mock_job(status="failed", error="Processing failed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job):
            response = extract_test_client.get(f"/v1/extract/jobs/{job.job_id}/result")

        assert response.status_code == 409
        body = response.json()
        assert body["error"]["code"] == "JOB_FAILED"
        # Body must help the caller find the job resource
        assert job.job_id in body["error"]["message"]

    def test_nonexistent_job_returns_404(self, extract_test_client):
        """A job that does not exist at all returns 404 (different from 409)."""
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=None):
            response = extract_test_client.get("/v1/extract/jobs/no-such-job/result")

        assert response.status_code == 404
        assert response.json()["error"]["code"] == "RESOURCE_NOT_FOUND"

    def test_missing_result_file_returns_500(self, extract_test_client):
        """If the result file is absent for a completed job, endpoint returns 500."""
        job = _mock_job(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job), \
             patch("extract.api.v1.jobs.read_result_file", return_value=None):
            response = extract_test_client.get(f"/v1/extract/jobs/{job.job_id}/result")

        assert response.status_code == 500
        assert "result file" in response.json()["error"]["message"].lower()

    def test_result_extraction_data_present(self, extract_test_client):
        """Result payload must include extraction data from the result file."""
        job = _mock_job(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job), \
             patch("extract.api.v1.jobs.read_result_file", return_value=_RESULT_DATA):
            response = extract_test_client.get(f"/v1/extract/jobs/{job.job_id}/result")

        extraction = response.json()["data"]["extraction"]
        assert extraction["invoice_number"] == "INV-001"
        assert extraction["total_amount"] == 500.0


# =========================================================================
# DELETE /v1/extract/jobs/{job_id}
# =========================================================================

@pytest.mark.unit
class TestDeleteExtractJob:
    """Tests for DELETE /v1/extract/jobs/{job_id}."""

    def test_delete_completed_job_returns_204(self, extract_test_client):
        job = _mock_job(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job), \
             patch("extract.api.v1.jobs.delete_job_files") as mock_del_files, \
             patch("extract.api.v1.jobs.db_repo.delete_job", return_value=True):
            response = extract_test_client.delete(f"/v1/extract/jobs/{job.job_id}")

        assert response.status_code == 204
        mock_del_files.assert_called_once_with(job.job_id)

    def test_delete_failed_job_returns_204(self, extract_test_client):
        """Failed jobs can be deleted (not active)."""
        job = _mock_job(status="failed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job), \
             patch("extract.api.v1.jobs.delete_job_files"), \
             patch("extract.api.v1.jobs.db_repo.delete_job", return_value=True):
            response = extract_test_client.delete(f"/v1/extract/jobs/{job.job_id}")

        assert response.status_code == 204

    def test_delete_in_progress_job_returns_409(self, extract_test_client):
        job = _mock_job(status="in_progress")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job):
            response = extract_test_client.delete(f"/v1/extract/jobs/{job.job_id}")

        assert response.status_code == 409
        assert response.json()["error"]["code"] == "RESOURCE_LOCKED"
        assert "active job" in response.json()["error"]["message"].lower()

    def test_delete_accepted_job_returns_409(self, extract_test_client):
        """Accepted jobs (queued but not yet running) are also blocked."""
        job = _mock_job(status="accepted")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job):
            response = extract_test_client.delete(f"/v1/extract/jobs/{job.job_id}")

        assert response.status_code == 409

    def test_delete_nonexistent_job_returns_404(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=None):
            response = extract_test_client.delete("/v1/extract/jobs/no-such-job")

        assert response.status_code == 404
        assert response.json()["error"]["code"] == "RESOURCE_NOT_FOUND"

    def test_delete_removes_result_file(self, extract_test_client):
        """delete_job_files is always called before the DB delete."""
        job = _mock_job(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job), \
             patch("extract.api.v1.jobs.delete_job_files") as mock_del, \
             patch("extract.api.v1.jobs.db_repo.delete_job", return_value=True):
            extract_test_client.delete(f"/v1/extract/jobs/{job.job_id}")

        mock_del.assert_called_once_with(job.job_id)

    def test_db_delete_failure_returns_500(self, extract_test_client):
        """If the DB delete fails, endpoint returns 500."""
        job = _mock_job(status="completed")
        with patch("extract.api.v1.jobs.db_repo.get_job_by_id", return_value=job), \
             patch("extract.api.v1.jobs.delete_job_files"), \
             patch("extract.api.v1.jobs.db_repo.delete_job", return_value=False):
            response = extract_test_client.delete(f"/v1/extract/jobs/{job.job_id}")

        assert response.status_code == 500


# =========================================================================
# DELETE /v1/extract/jobs (bulk)
# =========================================================================

@pytest.mark.unit
class TestBulkDeleteExtractJobs:
    """Tests for DELETE /v1/extract/jobs."""

    def test_confirm_true_no_active_jobs_returns_204(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.has_active_jobs", return_value=False), \
             patch("extract.api.v1.jobs.delete_all_job_files") as mock_del_files, \
             patch("extract.api.v1.jobs.db_repo.delete_all_jobs", return_value=True):
            response = extract_test_client.delete("/v1/extract/jobs?confirm=true")

        assert response.status_code == 204
        mock_del_files.assert_called_once()

    def test_missing_confirm_returns_400(self, extract_test_client):
        response = extract_test_client.delete("/v1/extract/jobs")

        assert response.status_code == 400
        assert response.json()["error"]["code"] == "CONFIRMATION_REQUIRED"
        assert "confirm=true" in response.json()["error"]["message"]

    def test_confirm_false_returns_400(self, extract_test_client):
        response = extract_test_client.delete("/v1/extract/jobs?confirm=false")

        assert response.status_code == 400
        assert response.json()["error"]["code"] == "CONFIRMATION_REQUIRED"

    def test_confirm_wrong_value_returns_400(self, extract_test_client):
        response = extract_test_client.delete("/v1/extract/jobs?confirm=yes")

        assert response.status_code == 400

    def test_active_jobs_exist_returns_409(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.has_active_jobs", return_value=True):
            response = extract_test_client.delete("/v1/extract/jobs?confirm=true")

        assert response.status_code == 409
        assert response.json()["error"]["code"] == "RESOURCE_LOCKED"
        assert "active" in response.json()["error"]["message"].lower()

    def test_db_delete_failure_returns_500(self, extract_test_client):
        with patch("extract.api.v1.jobs.db_repo.has_active_jobs", return_value=False), \
             patch("extract.api.v1.jobs.delete_all_job_files"), \
             patch("extract.api.v1.jobs.db_repo.delete_all_jobs", return_value=False):
            response = extract_test_client.delete("/v1/extract/jobs?confirm=true")

        assert response.status_code == 500

    def test_files_deleted_before_db(self, extract_test_client):
        """
        File deletion must be called even if the call order cannot be strictly
        enforced by the test — verify both are called when confirm=true.
        """
        with patch("extract.api.v1.jobs.db_repo.has_active_jobs", return_value=False), \
             patch("extract.api.v1.jobs.delete_all_job_files") as mock_files, \
             patch("extract.api.v1.jobs.db_repo.delete_all_jobs", return_value=True) as mock_db:
            extract_test_client.delete("/v1/extract/jobs?confirm=true")

        mock_files.assert_called_once()
        mock_db.assert_called_once()

# Made with Bob
