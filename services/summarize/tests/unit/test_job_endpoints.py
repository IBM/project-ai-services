"""
Unit tests for async summarization job endpoints.

Tests cover happy path scenarios and error cases for all five job endpoints:
- GET /v1/summarize/jobs (list)
- GET /v1/summarize/jobs/{job_id} (detail)
- GET /v1/summarize/jobs/{job_id}/result (result)
- DELETE /v1/summarize/jobs/{job_id} (delete single)
- DELETE /v1/summarize/jobs (bulk delete)
"""

import json
import uuid
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import Mock, patch

import pytest
from fastapi.testclient import TestClient


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


# Made with Bob