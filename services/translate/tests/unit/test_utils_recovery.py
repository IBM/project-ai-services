"""
Unit tests for translate/utils/recovery.py — zombie job recovery.
"""

import pytest
from datetime import datetime
from unittest.mock import MagicMock, Mock, patch

from translate.models import JobStatus
from translate.utils.recovery import recover_zombie_jobs


def _make_zombie_job(job_id: str, status: str = "in_progress") -> Mock:
    job = Mock()
    job.job_id = job_id
    job.status = status
    return job


# ---------------------------------------------------------------------------
# recover_zombie_jobs uses `import translate.settings as config` locally,
# so the staging_dir is reached via `translate.settings.settings.translate.staging_dir`.
# We patch that singleton attribute directly.
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestRecoverZombieJobs:
    def test_no_zombies_returns_zero(self, tmp_path):
        mock_db = MagicMock()
        mock_db.get_active_jobs.return_value = []

        with patch("translate.utils.recovery.db_manager", mock_db), \
             patch("translate.settings.settings") as mock_settings:
            mock_settings.translate.staging_dir = tmp_path / "staging"
            result = recover_zombie_jobs()

        assert result == 0
        mock_db.get_active_jobs.assert_called_once()
        mock_db.update_job.assert_not_called()

    def test_single_zombie_is_marked_failed(self, tmp_path):
        zombie = _make_zombie_job("zombie-1", "in_progress")
        mock_db = MagicMock()
        mock_db.get_active_jobs.return_value = [zombie]
        mock_db.update_job.return_value = True

        # No staging directory exists for this job — cleanup is a no-op
        with patch("translate.utils.recovery.db_manager", mock_db), \
             patch("translate.settings.settings") as mock_settings:
            mock_settings.translate.staging_dir = tmp_path / "staging"
            result = recover_zombie_jobs()

        assert result == 1
        call_kwargs = mock_db.update_job.call_args.kwargs
        assert call_kwargs["job_id"] == "zombie-1"
        assert call_kwargs["status"] == JobStatus.FAILED
        assert call_kwargs["error"] == "System restarted during processing"
        assert isinstance(call_kwargs["completed_at"], datetime)

    def test_multiple_zombies_all_marked_failed(self, tmp_path):
        zombies = [_make_zombie_job(f"zombie-{i}") for i in range(3)]
        mock_db = MagicMock()
        mock_db.get_active_jobs.return_value = zombies
        mock_db.update_job.return_value = True

        with patch("translate.utils.recovery.db_manager", mock_db), \
             patch("translate.settings.settings") as mock_settings:
            mock_settings.translate.staging_dir = tmp_path / "staging"
            result = recover_zombie_jobs()

        assert result == 3
        assert mock_db.update_job.call_count == 3

    def test_staging_directory_cleaned_up_when_exists(self, tmp_path):
        zombie = _make_zombie_job("zombie-cleanup", "accepted")
        mock_db = MagicMock()
        mock_db.get_active_jobs.return_value = [zombie]
        mock_db.update_job.return_value = True

        staging_root = tmp_path / "staging"
        job_staging = staging_root / "zombie-cleanup"
        job_staging.mkdir(parents=True)
        (job_staging / "file.txt").write_text("content")

        with patch("translate.utils.recovery.db_manager", mock_db), \
             patch("translate.settings.settings") as mock_settings:
            mock_settings.translate.staging_dir = staging_root
            result = recover_zombie_jobs()

        assert result == 1
        assert not job_staging.exists()

    def test_get_active_jobs_exception_returns_zero(self, tmp_path):
        mock_db = MagicMock()
        mock_db.get_active_jobs.side_effect = RuntimeError("db down")

        with patch("translate.utils.recovery.db_manager", mock_db), \
             patch("translate.settings.settings") as mock_settings:
            mock_settings.translate.staging_dir = tmp_path / "staging"
            result = recover_zombie_jobs()

        assert result == 0

    def test_individual_job_update_failure_continues_loop(self, tmp_path):
        """One update failure does not stop recovery of remaining zombies."""
        zombies = [
            _make_zombie_job("z-fail", "in_progress"),
            _make_zombie_job("z-ok", "accepted"),
        ]
        mock_db = MagicMock()
        mock_db.get_active_jobs.return_value = zombies
        mock_db.update_job.side_effect = [RuntimeError("update failed"), True]

        with patch("translate.utils.recovery.db_manager", mock_db), \
             patch("translate.settings.settings") as mock_settings:
            mock_settings.translate.staging_dir = tmp_path / "staging"
            result = recover_zombie_jobs()

        # z-fail fails during update → not counted; z-ok succeeds → counted
        assert result == 1

    def test_staging_cleanup_error_does_not_abort_recovery(self, tmp_path):
        """Staging cleanup failures are tolerated."""
        zombie = _make_zombie_job("zombie-stg-err")
        mock_db = MagicMock()
        mock_db.get_active_jobs.return_value = [zombie]
        mock_db.update_job.return_value = True

        staging_root = tmp_path / "staging"
        job_dir = staging_root / "zombie-stg-err"
        job_dir.mkdir(parents=True)

        with patch("translate.utils.recovery.db_manager", mock_db), \
             patch("translate.settings.settings") as mock_settings, \
             patch("shutil.rmtree", side_effect=PermissionError("denied")):
            mock_settings.translate.staging_dir = staging_root
            result = recover_zombie_jobs()

        assert result == 1

# Made with Bob
