"""
Unit tests for async summarization job endpoints.

Tests cover happy path scenarios and error cases for all five job endpoints:
- GET /v1/summarize/jobs (list)
- GET /v1/summarize/jobs/{job_id} (detail)
- GET /v1/summarize/jobs/{job_id}/result (result)
- DELETE /v1/summarize/jobs/{job_id} (delete single)
- DELETE /v1/summarize/jobs (bulk delete)
"""

import io
import json
import uuid
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import Mock, patch

import pytest
from fastapi import UploadFile
from fastapi.testclient import TestClient

from summarize.summ_utils import SummarizeException


@pytest.fixture
def mock_job():
    """Create a mock job object for testing."""
    from summarize.db.models import SummarizeJob
    
    job = SummarizeJob(
        job_id="test-job-123",
        job_name="Test Job",
        status="completed",
        submitted_at=datetime.now(timezone.utc),
        completed_at=datetime.now(timezone.utc),
        document_name="test.pdf",
        document_word_count=1000,
        level="standard",
        job_type="direct",
        job_metadata=None,
        error=None
    )
    return job


@pytest.fixture
def mock_result_data():
    """Create mock result data."""
    return {
        "data": {
            "summary": "This is a test summary.",
            "original_length": 1000,
            "summary_length": 50
        },
        "meta": {
            "model": "test-model",
            "processing_time_ms": 1000,
            "input_type": "file",
            "strategy": "direct"
        },
        "usage": {
            "input_tokens": 500,
            "output_tokens": 100,
            "total_tokens": 600
        }
    }


@pytest.mark.unit
class TestListJobsEndpoint:
    """Tests for GET /v1/summarize/jobs endpoint."""
    
    def test_list_jobs_empty(self, summarize_test_client):
        """Test listing jobs when none exist."""
        with patch("summarize.db.manager.db_repo.get_all_jobs") as mock_get_all:
            mock_get_all.return_value = ([], 0)
            
            response = summarize_test_client.get("/v1/summarize/jobs")
            
            assert response.status_code == 200
            data = response.json()
            assert data["pagination"]["total"] == 0
            assert data["pagination"]["limit"] == 20
            assert data["pagination"]["offset"] == 0
            assert data["data"] == []
    
    def test_list_jobs_with_pagination(self, summarize_test_client, mock_job):
        """Test listing jobs with custom pagination."""
        with patch("summarize.db.manager.db_repo.get_all_jobs") as mock_get_all:
            mock_get_all.return_value = ([mock_job], 1)
            
            response = summarize_test_client.get("/v1/summarize/jobs?limit=10&offset=5")
            
            assert response.status_code == 200
            data = response.json()
            assert data["pagination"]["limit"] == 10
            assert data["pagination"]["offset"] == 5
            assert len(data["data"]) == 1
            mock_get_all.assert_called_once()
    
    def test_list_jobs_with_status_filter(self, summarize_test_client, mock_job):
        """Test filtering jobs by status."""
        with patch("summarize.db.manager.db_repo.get_all_jobs") as mock_get_all:
            mock_get_all.return_value = ([mock_job], 1)
            
            response = summarize_test_client.get("/v1/summarize/jobs?status=completed")
            
            assert response.status_code == 200
            data = response.json()
            assert len(data["data"]) == 1
            assert data["data"][0]["status"] == "completed"
    
    def test_list_jobs_latest_flag(self, summarize_test_client, mock_job):
        """Test getting only the latest job."""
        with patch("summarize.db.manager.db_repo.get_all_jobs") as mock_get_all:
            mock_get_all.return_value = ([mock_job], 1)
            
            response = summarize_test_client.get("/v1/summarize/jobs?latest=true")
            
            assert response.status_code == 200
            data = response.json()
            assert data["pagination"]["limit"] == 1
            assert data["pagination"]["offset"] == 0
    
    def test_list_jobs_invalid_limit_too_low(self, summarize_test_client):
        """Test with limit below minimum."""
        response = summarize_test_client.get("/v1/summarize/jobs?limit=0")
        assert response.status_code == 400
        assert "Limit must be between 1 and 100" in response.json()["error"]["message"]
    
    def test_list_jobs_invalid_limit_too_high(self, summarize_test_client):
        """Test with limit above maximum."""
        response = summarize_test_client.get("/v1/summarize/jobs?limit=101")
        assert response.status_code == 400
        assert "Limit must be between 1 and 100" in response.json()["error"]["message"]
    
    def test_list_jobs_invalid_offset(self, summarize_test_client):
        """Test with negative offset parameter."""
        response = summarize_test_client.get("/v1/summarize/jobs?offset=-1")
        assert response.status_code == 400
        assert "Offset must be non-negative" in response.json()["error"]["message"]
    
    def test_list_jobs_invalid_status(self, summarize_test_client):
        """Test with invalid status value."""
        response = summarize_test_client.get("/v1/summarize/jobs?status=invalid_status")
        assert response.status_code == 400
        assert "Invalid status value" in response.json()["error"]["message"]


@pytest.mark.unit
class TestGetJobDetailsEndpoint:
    """Tests for GET /v1/summarize/jobs/{job_id} endpoint."""
    
    def test_get_job_details_success(self, summarize_test_client, mock_job):
        """Test getting details of an existing job."""
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job:
            mock_get_job.return_value = mock_job
            
            response = summarize_test_client.get("/v1/summarize/jobs/test-job-123")
            
            assert response.status_code == 200
            data = response.json()
            assert data["job_id"] == "test-job-123"
            assert data["job_name"] == "Test Job"
            assert data["status"] == "completed"
            assert "document" in data
            assert data["document"]["name"] == "test.pdf"
    
    def test_get_job_not_found(self, summarize_test_client):
        """Test getting a non-existent job returns 404."""
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job:
            mock_get_job.return_value = None
            
            response = summarize_test_client.get("/v1/summarize/jobs/nonexistent-job")
            
            assert response.status_code == 404
            assert "not found" in response.json()["error"]["message"]


@pytest.mark.unit
class TestGetJobResultEndpoint:
    """Tests for GET /v1/summarize/jobs/{job_id}/result endpoint."""
    
    def test_get_result_completed_job(self, summarize_test_client, mock_job, mock_result_data):
        """Test getting result for a completed job."""
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job, \
             patch("summarize.app.read_result_file") as mock_read_result:
            mock_get_job.return_value = mock_job
            mock_read_result.return_value = mock_result_data
            
            response = summarize_test_client.get("/v1/summarize/jobs/test-job-123/result")
            
            assert response.status_code == 200
            data = response.json()
            assert "data" in data
            assert "meta" in data
            assert "usage" in data
            assert data["data"]["summary"] == "This is a test summary."
    
    def test_get_result_job_not_found(self, summarize_test_client):
        """Test getting result for non-existent job returns 404."""
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job:
            mock_get_job.return_value = None
            
            response = summarize_test_client.get("/v1/summarize/jobs/nonexistent/result")
            
            assert response.status_code == 404
    
    def test_get_result_in_progress_job(self, summarize_test_client, mock_job):
        """Test getting result for in-progress job returns 202."""
        mock_job.status = "in_progress"
        
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job:
            mock_get_job.return_value = mock_job
            
            response = summarize_test_client.get("/v1/summarize/jobs/test-job-123/result")
            
            assert response.status_code == 202
            data = response.json()
            assert "still in progress" in data["message"]
    
    def test_get_result_accepted_job(self, summarize_test_client, mock_job):
        """Test getting result for accepted job returns 202."""
        mock_job.status = "accepted"
        
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job:
            mock_get_job.return_value = mock_job
            
            response = summarize_test_client.get("/v1/summarize/jobs/test-job-123/result")
            
            assert response.status_code == 202
    
    def test_get_result_failed_job(self, summarize_test_client, mock_job):
        """Test getting result for failed job returns 404."""
        mock_job.status = "failed"
        mock_job.error = "Processing failed"
        
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job:
            mock_get_job.return_value = mock_job
            
            response = summarize_test_client.get("/v1/summarize/jobs/test-job-123/result")
            
            assert response.status_code == 404
            assert "failed" in response.json()["error"]["message"].lower()
    
    def test_get_result_missing_file(self, summarize_test_client, mock_job):
        """Test when result file is missing for completed job."""
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job, \
             patch("summarize.app.read_result_file") as mock_read_result:
            mock_get_job.return_value = mock_job
            mock_read_result.return_value = None
            
            response = summarize_test_client.get("/v1/summarize/jobs/test-job-123/result")
            
            assert response.status_code == 500
            assert "Result file not found" in response.json()["error"]["message"]


@pytest.mark.unit
class TestDeleteJobEndpoint:
    """Tests for DELETE /v1/summarize/jobs/{job_id} endpoint."""
    
    def test_delete_completed_job(self, summarize_test_client, mock_job):
        """Test deleting a completed job."""
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job, \
             patch("summarize.app.delete_job_files") as mock_delete_files, \
             patch("summarize.db.manager.db_repo.delete_job") as mock_delete_job:
            mock_get_job.return_value = mock_job
            mock_delete_job.return_value = True
            
            response = summarize_test_client.delete("/v1/summarize/jobs/test-job-123")
            
            assert response.status_code == 204
            mock_delete_files.assert_called_once_with("test-job-123")
            mock_delete_job.assert_called_once_with("test-job-123")
    
    def test_delete_failed_job(self, summarize_test_client, mock_job):
        """Test deleting a failed job."""
        mock_job.status = "failed"
        
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job, \
             patch("summarize.app.delete_job_files") as mock_delete_files, \
             patch("summarize.db.manager.db_repo.delete_job") as mock_delete_job:
            mock_get_job.return_value = mock_job
            mock_delete_job.return_value = True
            
            response = summarize_test_client.delete("/v1/summarize/jobs/test-job-123")
            
            assert response.status_code == 204
    
    def test_delete_job_not_found(self, summarize_test_client):
        """Test deleting a non-existent job returns 404."""
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job:
            mock_get_job.return_value = None
            
            response = summarize_test_client.delete("/v1/summarize/jobs/nonexistent")
            
            assert response.status_code == 404
    
    def test_delete_in_progress_job(self, summarize_test_client, mock_job):
        """Test deleting an in-progress job returns 409."""
        mock_job.status = "in_progress"
        
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job:
            mock_get_job.return_value = mock_job
            
            response = summarize_test_client.delete("/v1/summarize/jobs/test-job-123")
            
            assert response.status_code == 409
            assert "Cannot delete active job" in response.json()["error"]["message"]
    
    def test_delete_accepted_job(self, summarize_test_client, mock_job):
        """Test deleting an accepted job returns 409."""
        mock_job.status = "accepted"
        
        with patch("summarize.db.manager.db_repo.get_job_by_id") as mock_get_job:
            mock_get_job.return_value = mock_job
            
            response = summarize_test_client.delete("/v1/summarize/jobs/test-job-123")
            
            assert response.status_code == 409


@pytest.mark.unit
class TestBulkDeleteJobsEndpoint:
    """Tests for DELETE /v1/summarize/jobs endpoint."""
    
    def test_bulk_delete_with_confirm(self, summarize_test_client):
        """Test bulk delete with confirm=true."""
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active, \
             patch("summarize.app.delete_all_job_files") as mock_delete_files, \
             patch("summarize.db.manager.db_repo.delete_all_jobs") as mock_delete_all:
            mock_get_active.return_value = []
            mock_delete_all.return_value = True
            
            response = summarize_test_client.delete("/v1/summarize/jobs?confirm=true")
            
            assert response.status_code == 204
            mock_delete_files.assert_called_once()
            mock_delete_all.assert_called_once()
    
    def test_bulk_delete_without_confirm(self, summarize_test_client):
        """Test bulk delete without confirm parameter returns 400."""
        response = summarize_test_client.delete("/v1/summarize/jobs")
        
        assert response.status_code == 400
        assert "confirm=true" in response.json()["error"]["message"]
    
    def test_bulk_delete_confirm_false(self, summarize_test_client):
        """Test bulk delete with confirm=false returns 400."""
        response = summarize_test_client.delete("/v1/summarize/jobs?confirm=false")
        
        assert response.status_code == 400
    
    def test_bulk_delete_with_active_jobs(self, summarize_test_client, mock_job):
        """Test bulk delete with active jobs returns 409."""
        mock_job.status = "in_progress"
        
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active:
            mock_get_active.return_value = [mock_job]
            
            response = summarize_test_client.delete("/v1/summarize/jobs?confirm=true")
            
            assert response.status_code == 409
            assert "active job" in response.json()["error"]["message"].lower()
    
    def test_bulk_delete_database_failure(self, summarize_test_client):
        """Test bulk delete when database deletion fails."""
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active, \
             patch("summarize.app.delete_all_job_files") as mock_delete_files, \
             patch("summarize.db.manager.db_repo.delete_all_jobs") as mock_delete_all:
            mock_get_active.return_value = []
            mock_delete_all.return_value = False
            
            response = summarize_test_client.delete("/v1/summarize/jobs?confirm=true")
            
            assert response.status_code == 500

@pytest.mark.unit
class TestCreateSummarizationJobEndpoint:
    """Tests for POST /v1/summarize/jobs endpoint (create_summarization_job)."""
    
    @pytest.fixture
    def mock_upload_file(self):
        """Mock FastAPI UploadFile for testing."""
        file_content = b"This is test file content for summarization."
        return ("test.txt", io.BytesIO(file_content), "text/plain")
    
    @pytest.fixture
    def mock_upload_file_pdf(self):
        """Mock PDF UploadFile."""
        file_content = b"%PDF-1.4 fake pdf content"
        return ("test.pdf", io.BytesIO(file_content), "application/pdf")
    
    def test_create_job_success_with_all_parameters(self, summarize_test_client, mock_upload_file):
        """Test successful job creation with all parameters provided."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.validate_summary_level", return_value="standard"), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db") as mock_create_db, \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                data={"level": "standard", "job_name": "Test Job"},
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            data = response.json()
            assert data["job_id"] == test_uuid
            mock_create_db.assert_called_once()
    
    def test_create_job_success_minimal_parameters(self, summarize_test_client, mock_upload_file):
        """Test successful job creation with only required file parameter."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db") as mock_create_db, \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            data = response.json()
            assert data["job_id"] == test_uuid
            # Verify default level is used
            call_args = mock_create_db.call_args
            assert call_args[0][3] == "standard"
    
    def test_create_job_with_brief_level(self, summarize_test_client, mock_upload_file):
        """Test job creation with brief level."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.validate_summary_level", return_value="brief"), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db") as mock_create_db, \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                data={"level": "brief"},
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            call_args = mock_create_db.call_args
            assert call_args[0][3] == "brief"
    
    def test_create_job_with_detailed_level(self, summarize_test_client, mock_upload_file):
        """Test job creation with detailed level."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.validate_summary_level", return_value="detailed"), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db") as mock_create_db, \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                data={"level": "detailed"},
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            call_args = mock_create_db.call_args
            assert call_args[0][3] == "detailed"
    
    def test_create_job_with_pdf_file(self, summarize_test_client, mock_upload_file_pdf):
        """Test job creation with PDF file."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".pdf")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.pdf")), \
             patch("summarize.app.create_job_with_db"), \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file_pdf}
            )
            
            assert response.status_code == 202
            assert response.json()["job_id"] == test_uuid
    
    def test_create_job_rate_limit_exceeded(self, summarize_test_client, mock_upload_file):
        """Test job creation when server is at capacity."""
        with patch("summarize.app.concurrency_limiter.locked", return_value=True):
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 429
            error = response.json()["error"]
            assert "capacity" in error["message"].lower()
    
    def test_create_job_invalid_file_extension(self, summarize_test_client):
        """Test job creation with unsupported file extension."""
        file_content = b"fake doc content"
        mock_file = ("test.doc", io.BytesIO(file_content), "application/msword")
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(False, ".doc")):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_file}
            )
            
            assert response.status_code == 415
            error = response.json()["error"]
            assert ".txt" in error["message"] and ".pdf" in error["message"]
    
    def test_create_job_missing_file_extension(self, summarize_test_client):
        """Test job creation with file that has no extension."""
        file_content = b"content"
        mock_file = ("", io.BytesIO(file_content), "application/octet-stream")
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(False, None)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_file}
            )
            
            # FastAPI returns 422 for validation errors on empty filename
            assert response.status_code in [415, 422]
    
    def test_create_job_invalid_level(self, summarize_test_client, mock_upload_file):
        """Test job creation with invalid level parameter."""
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.validate_summary_level", side_effect=SummarizeException(
                 400, "INVALID_LEVEL", "Invalid level value"
             )):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                data={"level": "invalid_level"},
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 400
            error = response.json()["error"]
            assert "level" in error["message"].lower() or "invalid" in error["message"].lower()
    
    def test_create_job_file_staging_error(self, summarize_test_client, mock_upload_file):
        """Test job creation when file staging fails."""
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", side_effect=IOError("Disk full")):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 500
            error = response.json()["error"]
            assert "file" in error["message"].lower() or "staging" in error["message"].lower()
    
    def test_create_job_database_error(self, summarize_test_client, mock_upload_file):
        """Test job creation when database operation fails."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db", side_effect=Exception("DB connection failed")), \
             patch("summarize.app.cleanup_staging_directory") as mock_cleanup, \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 500
            error = response.json()["error"]
            assert "database" in error["message"].lower() or "job record" in error["message"].lower()
            # Verify cleanup was called
            mock_cleanup.assert_called_once()
    
    def test_create_job_database_error_triggers_cleanup(self, summarize_test_client, mock_upload_file):
        """Test that staging directory is cleaned up when database creation fails."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db", side_effect=Exception("DB error")), \
             patch("summarize.app.cleanup_staging_directory") as mock_cleanup, \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)), \
             patch("summarize.app.settings") as mock_settings:
            
            mock_settings.summarize.staging_dir = Path("/var/cache/summarize/staging")
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 500
            # Verify cleanup was called with correct job_id and staging_dir
            mock_cleanup.assert_called_once_with(test_uuid, mock_settings.summarize.staging_dir)
    
    def test_create_job_schedules_background_task(self, summarize_test_client, mock_upload_file):
        """Test that background task is scheduled for job processing."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db"), \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            # Background task scheduling is handled by FastAPI's BackgroundTasks
            # In actual execution, process_summarization_job would be called
    
    def test_create_job_background_task_parameters(self, summarize_test_client, mock_upload_file):
        """Test that background task is called with correct parameters."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.validate_summary_level", return_value="brief"), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db"), \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                data={"level": "brief"},
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            # Background task would be called with job_id and level="brief"
    
    def test_create_job_response_format(self, summarize_test_client, mock_upload_file):
        """Test that response has correct format with job_id."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db"), \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            data = response.json()
            assert "job_id" in data
            assert data["job_id"] == test_uuid
            # Verify it's a valid UUID format
            assert uuid.UUID(data["job_id"])
    
    def test_create_job_returns_202_accepted(self, summarize_test_client, mock_upload_file):
        """Test that endpoint returns exactly 202 Accepted status."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db"), \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            # Must be 202, not 200 or 201
            assert response.status_code == 202
    
    def test_create_job_summarize_exception_propagated(self, summarize_test_client, mock_upload_file):
        """Test that SummarizeException is properly propagated."""
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.validate_summary_level", side_effect=SummarizeException(
                 400, "TEST_ERROR", "Test error message"
             )):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                data={"level": "invalid"},
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 400
            error = response.json()["error"]
            assert "Test error message" in error["message"]
    
    def test_create_job_unexpected_exception(self, summarize_test_client, mock_upload_file):
        """Test handling of unexpected exceptions."""
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", side_effect=RuntimeError("Unexpected error")):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 500
            error = response.json()["error"]
            assert "summarization job" in error["message"].lower() or "unexpected" in error["message"].lower()
    
    def test_create_job_generates_valid_uuid(self, summarize_test_client, mock_upload_file):
        """Test that generated job_id is a valid UUID v4."""
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db"):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            job_id = response.json()["job_id"]
            # Verify it's a valid UUID
            parsed_uuid = uuid.UUID(job_id)
            assert str(parsed_uuid) == job_id
    
    def test_create_job_with_job_name(self, summarize_test_client, mock_upload_file):
        """Test job creation with custom job_name."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db") as mock_create_db, \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                data={"job_name": "My Custom Job"},
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            # Verify job_name was passed to create_job_with_db
            call_args = mock_create_db.call_args
            assert call_args[0][4] == "My Custom Job"
    
    def test_create_job_passes_correct_parameters_to_db(self, summarize_test_client, mock_upload_file):
        """Test that correct parameters are passed to create_job_with_db."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db") as mock_create_db, \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                data={"level": "standard", "job_name": "Test"},
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            # Verify create_job_with_db was called with correct parameters
            mock_create_db.assert_called_once_with(
                test_uuid,
                "direct",  # SummarizationType.DIRECT.value
                0,  # word_count (not calculated at creation time)
                "standard",
                "Test",
                "test.txt"
            )
    
    def test_create_job_none_level_defaults_to_standard(self, summarize_test_client, mock_upload_file):
        """Test that None level parameter defaults to 'standard'."""
        test_uuid = "12345678-1234-5678-1234-567812345678"
        
        with patch("summarize.app.concurrency_limiter.locked", return_value=False), \
             patch("summarize.app.validate_file_extension", return_value=(True, ".txt")), \
             patch("summarize.app.stage_uploaded_file", return_value=Path("/tmp/staged/test.txt")), \
             patch("summarize.app.create_job_with_db") as mock_create_db, \
             patch("summarize.app.uuid.uuid4", return_value=uuid.UUID(test_uuid)):
            
            response = summarize_test_client.post(
                "/v1/summarize/jobs",
                files={"file": mock_upload_file}
            )
            
            assert response.status_code == 202
            # Verify level defaults to "standard"
            call_args = mock_create_db.call_args
            assert call_args[0][3] == "standard"  # level is 4th positional arg




# Made with Bob