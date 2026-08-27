"""
Unit tests for recover_conversion_tasks — utils/recovery.py.

Verifies that:
  - running → failed (with chunk dir cleanup)
  - queued → failed unconditionally (pipeline task is gone after restart)
  - pending → failed unconditionally (same reason)
  - The function returns an accurate count of recovered (status-changed) tasks
"""

from pathlib import Path
from unittest.mock import Mock, patch

import pytest


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_task(task_id, status, cached_file):
    t = Mock()
    t.task_id = task_id
    t.status = status
    t.cached_file = cached_file
    return t


# The import path for db_manager used at runtime inside recover_conversion_tasks
_DB_MANAGER_PATH = "digitize.db.manager.db_manager"


@pytest.mark.unit
class TestRecoverConversionTasks:
    def test_running_task_marked_failed(self, tmp_path):
        from digitize.utils.recovery import recover_conversion_tasks

        cached = str(tmp_path / "file.pdf")
        running = _make_task("t-run", "running", cached)

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.return_value = [running]
            mock_mgr.update_task_status = Mock()

            count = recover_conversion_tasks()

        mock_mgr.update_task_status.assert_any_call(
            "t-run", "failed", error="Service restarted during conversion"
        )
        assert count == 1

    def test_running_task_chunks_dir_cleaned(self, tmp_path):
        from digitize.utils.recovery import recover_conversion_tasks

        cached = str(tmp_path / "file.pdf")
        chunk_dir = tmp_path / "chunks"
        chunk_dir.mkdir()
        (chunk_dir / "chunk0.json").write_text("{}")

        running = _make_task("t-run-chunks", "running", cached)

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.return_value = [running]
            mock_mgr.update_task_status = Mock()

            recover_conversion_tasks()

        assert not chunk_dir.exists()  # shutil.rmtree was called

    def test_queued_task_marked_failed(self, tmp_path):
        """queued → failed unconditionally; the pipeline task is gone after restart."""
        from digitize.utils.recovery import recover_conversion_tasks

        queued = _make_task("t-queued", "queued", str(tmp_path / "file.pdf"))

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.return_value = [queued]
            mock_mgr.update_task_status = Mock()

            count = recover_conversion_tasks()

        mock_mgr.update_task_status.assert_any_call(
            "t-queued", "failed",
            error="Service restarted before conversion could complete",
        )
        assert count == 1

    def test_pending_task_marked_failed(self, tmp_path):
        """pending → failed unconditionally; the pipeline task is gone after restart."""
        from digitize.utils.recovery import recover_conversion_tasks

        pending = _make_task("t-pending", "pending", str(tmp_path / "file.pdf"))

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.return_value = [pending]
            mock_mgr.update_task_status = Mock()

            count = recover_conversion_tasks()

        mock_mgr.update_task_status.assert_any_call(
            "t-pending", "failed",
            error="Service restarted before conversion could complete",
        )
        assert count == 1

    def test_mixed_recovery_counts_correctly(self, tmp_path):
        """1 running + 1 queued + 1 pending → all three failed → count=3."""
        from digitize.utils.recovery import recover_conversion_tasks

        running = _make_task("t-r",  "running", str(tmp_path / "run.pdf"))
        queued  = _make_task("t-q",  "queued",  str(tmp_path / "q.pdf"))
        pending = _make_task("t-p",  "pending", str(tmp_path / "p.pdf"))

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.return_value = [running, queued, pending]
            mock_mgr.update_task_status = Mock()

            count = recover_conversion_tasks()

        assert count == 3

    def test_returns_zero_when_nothing_to_recover(self, tmp_path):
        from digitize.utils.recovery import recover_conversion_tasks

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.return_value = []
            mock_mgr.update_task_status = Mock()

            count = recover_conversion_tasks()

        mock_mgr.update_task_status.assert_not_called()
        assert count == 0

    def test_handles_db_error_gracefully(self):
        """An exception in the DB layer must not propagate; returns 0."""
        from digitize.utils.recovery import recover_conversion_tasks
        from sqlalchemy.exc import SQLAlchemyError

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.side_effect = SQLAlchemyError("db down")

            count = recover_conversion_tasks()

        assert count == 0
