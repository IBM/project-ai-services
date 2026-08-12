"""
Unit tests for recover_conversion_tasks — utils/recovery.py.

Verifies that:
  - running → failed (with chunk dir cleanup)
  - queued tasks with missing file → failed
  - queued tasks with file present → kept (status unchanged)
  - pending tasks with missing file → failed
  - pending tasks with file present → kept
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
            mock_mgr.get_conversion_tasks.side_effect = lambda status: {
                "running": [running],
                "queued": [],
                "pending": [],
            }[status]
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
            mock_mgr.get_conversion_tasks.side_effect = lambda status: {
                "running": [running],
                "queued": [],
                "pending": [],
            }[status]
            mock_mgr.update_task_status = Mock()

            recover_conversion_tasks()

        assert not chunk_dir.exists()  # shutil.rmtree was called

    def test_queued_task_with_missing_file_marked_failed(self, tmp_path):
        from digitize.utils.recovery import recover_conversion_tasks

        missing = str(tmp_path / "gone.pdf")
        queued = _make_task("t-queued-gone", "queued", missing)

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.side_effect = lambda status: {
                "running": [],
                "queued": [queued],
                "pending": [],
            }[status]
            mock_mgr.update_task_status = Mock()

            count = recover_conversion_tasks()

        mock_mgr.update_task_status.assert_any_call(
            "t-queued-gone", "failed",
            error="Cached input file lost during restart",
        )
        assert count == 1

    def test_queued_task_with_present_file_kept(self, tmp_path):
        from digitize.utils.recovery import recover_conversion_tasks

        present = str(tmp_path / "here.pdf")
        Path(present).write_bytes(b"%PDF-1.4")
        queued = _make_task("t-queued-ok", "queued", present)

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.side_effect = lambda status: {
                "running": [],
                "queued": [queued],
                "pending": [],
            }[status]
            mock_mgr.update_task_status = Mock()

            count = recover_conversion_tasks()

        # update_task_status must NOT be called for this task
        for c in mock_mgr.update_task_status.call_args_list:
            assert c.args[0] != "t-queued-ok"
        assert count == 0

    def test_pending_task_with_missing_file_marked_failed(self, tmp_path):
        from digitize.utils.recovery import recover_conversion_tasks

        missing = str(tmp_path / "lost_pending.pdf")
        pending = _make_task("t-pending-gone", "pending", missing)

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.side_effect = lambda status: {
                "running": [],
                "queued": [],
                "pending": [pending],
            }[status]
            mock_mgr.update_task_status = Mock()

            count = recover_conversion_tasks()

        mock_mgr.update_task_status.assert_any_call(
            "t-pending-gone", "failed",
            error="Cached input file lost during restart",
        )
        assert count == 1

    def test_pending_task_with_present_file_kept(self, tmp_path):
        from digitize.utils.recovery import recover_conversion_tasks

        present = str(tmp_path / "pending_ok.pdf")
        Path(present).write_bytes(b"%PDF-1.4")
        pending = _make_task("t-pending-ok", "pending", present)

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.side_effect = lambda status: {
                "running": [],
                "queued": [],
                "pending": [pending],
            }[status]
            mock_mgr.update_task_status = Mock()

            count = recover_conversion_tasks()

        for c in mock_mgr.update_task_status.call_args_list:
            assert c.args[0] != "t-pending-ok"
        assert count == 0

    def test_mixed_recovery_counts_correctly(self, tmp_path):
        """Combined scenario: 1 running + 1 queued-missing + 1 queued-ok → count=2."""
        from digitize.utils.recovery import recover_conversion_tasks

        present = str(tmp_path / "ok.pdf")
        Path(present).write_bytes(b"%PDF-1.4")
        missing = str(tmp_path / "gone.pdf")

        running = _make_task("t-r", "running", present)
        q_gone = _make_task("t-q-gone", "queued", missing)
        q_ok = _make_task("t-q-ok", "queued", present)

        with patch(_DB_MANAGER_PATH) as mock_mgr:
            mock_mgr.get_conversion_tasks.side_effect = lambda status: {
                "running": [running],
                "queued": [q_gone, q_ok],
                "pending": [],
            }[status]
            mock_mgr.update_task_status = Mock()

            count = recover_conversion_tasks()

        assert count == 2  # running + queued-missing

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
