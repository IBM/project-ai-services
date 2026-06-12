"""
Unit tests for zombie job recovery scan functionality.

Tests the boot-up recovery scan that identifies and cleans up zombie jobs
(jobs with status 'accepted' or 'in_progress' that were interrupted by restart).
"""

import shutil
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import Mock, patch, MagicMock

import pytest

from summarize.models import JobStatus


@pytest.fixture
def mock_zombie_jobs():
    """Create mock zombie jobs for testing."""
    from summarize.db.models import SummarizeJob
    
    job1 = SummarizeJob(
        job_id="zombie-job-1",
        job_name="Zombie Job 1",
        status="accepted",
        submitted_at=datetime.now(timezone.utc),
        document_name="test1.pdf",
        document_word_count=1000,
        level="standard",
        job_type="direct"
    )
    
    job2 = SummarizeJob(
        job_id="zombie-job-2",
        job_name="Zombie Job 2",
        status="in_progress",
        submitted_at=datetime.now(timezone.utc),
        document_name="test2.pdf",
        document_word_count=2000,
        level="brief",
        job_type="chunked"
    )
    
    return [job1, job2]


@pytest.mark.unit
class TestRecoveryZombieJobs:
    """Tests for recover_zombie_jobs function."""
    
    def test_no_zombie_jobs(self):
        """Test recovery when no zombie jobs exist."""
        from summarize.job_utils import recover_zombie_jobs
        
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active:
            mock_get_active.return_value = []
            
            result = recover_zombie_jobs()
            
            assert result == 0
            mock_get_active.assert_called_once()
    
    def test_recover_single_zombie_job(self, mock_zombie_jobs, tmp_path):
        """Test recovery of a single zombie job."""
        from summarize.job_utils import recover_zombie_jobs
        
        zombie_job = mock_zombie_jobs[0]
        
        # Create a mock staging directory
        staging_dir = tmp_path / "staging" / zombie_job.job_id
        staging_dir.mkdir(parents=True)
        
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active, \
             patch("summarize.db.manager.db_repo.update_job") as mock_update, \
             patch("summarize.job_utils.settings.summarize.cache_dir", tmp_path):
            
            mock_get_active.return_value = [zombie_job]
            mock_update.return_value = True
            
            result = recover_zombie_jobs()
            
            assert result == 1
            mock_get_active.assert_called_once()
            mock_update.assert_called_once()
            
            # Verify update was called with correct parameters
            call_args = mock_update.call_args
            assert call_args.kwargs["job_id"] == zombie_job.job_id
            assert call_args.kwargs["status"] == JobStatus.FAILED
            assert call_args.kwargs["error"] == "System restarted during processing"
            assert call_args.kwargs["completed_at"] is not None
            
            # Verify staging directory was deleted
            assert not staging_dir.exists()
    
    def test_recover_multiple_zombie_jobs(self, mock_zombie_jobs, tmp_path):
        """Test recovery of multiple zombie jobs."""
        from summarize.job_utils import recover_zombie_jobs
        
        # Create mock staging directories for both jobs
        for job in mock_zombie_jobs:
            staging_dir = tmp_path / "staging" / job.job_id
            staging_dir.mkdir(parents=True)
        
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active, \
             patch("summarize.db.manager.db_repo.update_job") as mock_update, \
             patch("summarize.job_utils.settings.summarize.cache_dir", tmp_path):
            
            mock_get_active.return_value = mock_zombie_jobs
            mock_update.return_value = True
            
            result = recover_zombie_jobs()
            
            assert result == 2
            mock_get_active.assert_called_once()
            assert mock_update.call_count == 2
            
            # Verify both staging directories were deleted
            for job in mock_zombie_jobs:
                staging_dir = tmp_path / "staging" / job.job_id
                assert not staging_dir.exists()
    
    def test_recover_zombie_job_no_staging_dir(self, mock_zombie_jobs):
        """Test recovery when staging directory doesn't exist."""
        from summarize.job_utils import recover_zombie_jobs
        
        zombie_job = mock_zombie_jobs[0]
        
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active, \
             patch("summarize.db.manager.db_repo.update_job") as mock_update, \
             patch("summarize.job_utils.settings.summarize.cache_dir", Path("/nonexistent")):
            
            mock_get_active.return_value = [zombie_job]
            mock_update.return_value = True
            
            # Should not raise an error even if staging dir doesn't exist
            result = recover_zombie_jobs()
            
            assert result == 1
            mock_update.assert_called_once()
    
    def test_recover_zombie_job_update_fails(self, mock_zombie_jobs):
        """Test recovery when database update fails."""
        from summarize.job_utils import recover_zombie_jobs
        
        zombie_job = mock_zombie_jobs[0]
        
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active, \
             patch("summarize.db.manager.db_repo.update_job") as mock_update:
            
            mock_get_active.return_value = [zombie_job]
            mock_update.return_value = False  # Simulate update failure
            
            result = recover_zombie_jobs()
            
            # Should still return 0 since update failed
            assert result == 0
            mock_update.assert_called_once()
    
    def test_recover_zombie_job_partial_failure(self, mock_zombie_jobs, tmp_path):
        """Test recovery when one job succeeds and one fails."""
        from summarize.job_utils import recover_zombie_jobs
        
        # Create staging directories
        for job in mock_zombie_jobs:
            staging_dir = tmp_path / "staging" / job.job_id
            staging_dir.mkdir(parents=True)
        
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active, \
             patch("summarize.db.manager.db_repo.update_job") as mock_update, \
             patch("summarize.job_utils.settings.summarize.cache_dir", tmp_path):
            
            mock_get_active.return_value = mock_zombie_jobs
            # First call succeeds, second fails
            mock_update.side_effect = [True, False]
            
            result = recover_zombie_jobs()
            
            assert result == 1  # Only one successful recovery
            assert mock_update.call_count == 2
    
    def test_recover_zombie_job_exception_handling(self, mock_zombie_jobs):
        """Test that exceptions during recovery are handled gracefully."""
        from summarize.job_utils import recover_zombie_jobs
        
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active:
            mock_get_active.side_effect = Exception("Database error")
            
            # Should not raise, should return 0
            result = recover_zombie_jobs()
            
            assert result == 0
    
    def test_recover_zombie_job_staging_cleanup_error(self, mock_zombie_jobs, tmp_path):
        """Test recovery continues even if staging cleanup fails."""
        from summarize.job_utils import recover_zombie_jobs
        
        zombie_job = mock_zombie_jobs[0]
        
        with patch("summarize.db.manager.db_repo.get_active_jobs") as mock_get_active, \
             patch("summarize.db.manager.db_repo.update_job") as mock_update, \
             patch("summarize.job_utils.shutil.rmtree") as mock_rmtree, \
             patch("summarize.job_utils.settings.summarize.cache_dir", tmp_path):
            
            mock_get_active.return_value = [zombie_job]
            mock_update.return_value = True
            mock_rmtree.side_effect = Exception("Cleanup error")
            
            # Should still count as recovered even if cleanup fails
            result = recover_zombie_jobs()
            
            assert result == 1
            mock_update.assert_called_once()


@pytest.mark.unit
@pytest.mark.asyncio
class TestRecoveryScanIntegration:
    """Tests for recovery scan integration in app startup."""
    
    async def test_recovery_scan_called_on_startup(self):
        """Test that recovery scan is called during app startup."""
        from summarize.app import lifespan
        
        with patch("summarize.app.configure_uvicorn_logging"), \
             patch("summarize.app.initialize_models"), \
             patch("summarize.app.create_llm_session"), \
             patch("summarize.app.check_db_connection", return_value=True), \
             patch("summarize.app.ensure_directories"), \
             patch("summarize.job_utils.recover_zombie_jobs", return_value=0) as mock_recover, \
             patch("summarize.app.close_db_connections"), \
             patch("summarize.db.models.Base.metadata.create_all"):
            
            # Create a mock app
            mock_app = Mock()
            
            # Execute lifespan context manager
            async with lifespan(mock_app):
                # Verify recovery scan was called
                mock_recover.assert_called_once()
    
    async def test_recovery_scan_logs_recovered_jobs(self):
        """Test that recovery scan logs when jobs are recovered."""
        from summarize.app import lifespan
        
        with patch("summarize.app.configure_uvicorn_logging"), \
             patch("summarize.app.initialize_models"), \
             patch("summarize.app.create_llm_session"), \
             patch("summarize.app.check_db_connection", return_value=True), \
             patch("summarize.app.ensure_directories"), \
             patch("summarize.job_utils.recover_zombie_jobs", return_value=3) as mock_recover, \
             patch("summarize.app.logger") as mock_logger, \
             patch("summarize.app.close_db_connections"), \
             patch("summarize.db.models.Base.metadata.create_all"):
            
            mock_app = Mock()
            
            async with lifespan(mock_app):
                # Verify recovery scan was called and logged
                mock_recover.assert_called_once()
                
                # Check that warning was logged for recovered jobs
                warning_calls = [call for call in mock_logger.warning.call_args_list
                               if "zombie job" in str(call).lower()]
                assert len(warning_calls) > 0


# Made with Bob