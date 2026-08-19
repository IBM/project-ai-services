"""
Unit tests for enqueue_conversion_tasks — utils/jobs.py.

Covers:
  - Correct slot allocation: first N tasks → 'queued', remainder → 'pending'
  - Page count used correctly to set is_large
  - One create_conversion_task call per file
"""

import asyncio
from pathlib import Path
from unittest.mock import AsyncMock, Mock, call, patch

import pytest


# The db_manager singleton import path used at runtime inside enqueue_conversion_tasks
_DB_MANAGER_PATH = "digitize.db.manager.db_manager"


@pytest.mark.unit
class TestEnqueueConversionTasks:
    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _run(self, coro):
        return asyncio.run(coro)

    def _make_doc_id_dict(self, filenames):
        return {fn: f"doc-{i}" for i, fn in enumerate(filenames)}

    # ------------------------------------------------------------------
    # Basic smoke test
    # ------------------------------------------------------------------

    def test_single_file_queued_when_slots_available(self, tmp_path):
        from digitize.utils.jobs import enqueue_conversion_tasks
        from digitize.models import OutputFormat

        filenames = ["file.pdf"]
        doc_id_dict = self._make_doc_id_dict(filenames)
        (tmp_path / "file.pdf").write_bytes(b"%PDF-1.4")

        created_tasks = []

        with patch(_DB_MANAGER_PATH) as mock_mgr, \
             patch("digitize.utils.jobs.get_document_page_count", return_value=10), \
             patch("digitize.utils.jobs.generate_uuid", return_value="task-001"):

            mock_mgr.create_conversion_task.side_effect = lambda **kw: created_tasks.append(kw)

            self._run(enqueue_conversion_tasks(
                job_id="j1",
                op_key="digitization",
                filenames=filenames,
                doc_id_dict=doc_id_dict,
                staging_dir=tmp_path,
                output_format=OutputFormat.JSON,
                quota=5,
                queued_for_op=0,
            ))

        assert len(created_tasks) == 1
        assert created_tasks[0]["status"] == "queued"
        assert created_tasks[0]["operation"] == "digitization"
        assert created_tasks[0]["output_format"] == "json"

    def test_excess_files_become_pending(self, tmp_path):
        """
        4 files submitted, 2 free slots → first 2 queued, last 2 pending.
        """
        from digitize.utils.jobs import enqueue_conversion_tasks
        from digitize.models import OutputFormat

        filenames = [f"f{i}.pdf" for i in range(4)]
        doc_id_dict = self._make_doc_id_dict(filenames)
        for fn in filenames:
            (tmp_path / fn).write_bytes(b"%PDF-1.4")

        created_tasks = []

        with patch(_DB_MANAGER_PATH) as mock_mgr, \
             patch("digitize.utils.jobs.get_document_page_count", return_value=5), \
             patch("digitize.utils.jobs.generate_uuid",
                   side_effect=[f"task-{i}" for i in range(4)]):

            mock_mgr.create_conversion_task.side_effect = lambda **kw: created_tasks.append(kw)

            self._run(enqueue_conversion_tasks(
                job_id="j1",
                op_key="ingestion",
                filenames=filenames,
                doc_id_dict=doc_id_dict,
                staging_dir=tmp_path,
                output_format=OutputFormat.JSON,
                quota=10,
                queued_for_op=8,  # only 2 free slots
            ))

        assert len(created_tasks) == 4
        statuses = [t["status"] for t in created_tasks]
        assert statuses[:2] == ["queued", "queued"]
        assert statuses[2:] == ["pending", "pending"]

    def test_all_tasks_pending_when_no_slots_free(self, tmp_path):
        """
        queued_for_op == quota → 0 free slots → all tasks inserted as pending.
        """
        from digitize.utils.jobs import enqueue_conversion_tasks
        from digitize.models import OutputFormat

        filenames = ["a.pdf", "b.pdf"]
        doc_id_dict = self._make_doc_id_dict(filenames)
        for fn in filenames:
            (tmp_path / fn).write_bytes(b"%PDF-1.4")

        created_tasks = []

        with patch(_DB_MANAGER_PATH) as mock_mgr, \
             patch("digitize.utils.jobs.get_document_page_count", return_value=3), \
             patch("digitize.utils.jobs.generate_uuid",
                   side_effect=["t1", "t2"]):

            mock_mgr.create_conversion_task.side_effect = lambda **kw: created_tasks.append(kw)

            self._run(enqueue_conversion_tasks(
                job_id="j1",
                op_key="ingestion",
                filenames=filenames,
                doc_id_dict=doc_id_dict,
                staging_dir=tmp_path,
                output_format=OutputFormat.JSON,
                quota=5,
                queued_for_op=5,  # quota full
            ))

        assert all(t["status"] == "pending" for t in created_tasks)

    def test_large_file_sets_is_large_true(self, tmp_path):
        """Page count >= threshold → is_large=True."""
        from digitize.utils.jobs import enqueue_conversion_tasks
        from digitize.models import OutputFormat
        from digitize.settings import settings

        filenames = ["big.pdf"]
        doc_id_dict = self._make_doc_id_dict(filenames)
        (tmp_path / "big.pdf").write_bytes(b"%PDF-1.4")

        threshold = settings.digitize.heavy_doc_page_threshold  # default 500
        created_tasks = []

        with patch(_DB_MANAGER_PATH) as mock_mgr, \
             patch("digitize.utils.jobs.get_document_page_count",
                   return_value=threshold), \
             patch("digitize.utils.jobs.generate_uuid", return_value="t-large"):

            mock_mgr.create_conversion_task.side_effect = lambda **kw: created_tasks.append(kw)

            self._run(enqueue_conversion_tasks(
                job_id="j1",
                op_key="digitization",
                filenames=filenames,
                doc_id_dict=doc_id_dict,
                staging_dir=tmp_path,
                output_format=OutputFormat.JSON,
                quota=5,
                queued_for_op=0,
            ))

        assert created_tasks[0]["is_large"] is True

    def test_normal_file_sets_is_large_false(self, tmp_path):
        """Page count < threshold → is_large=False."""
        from digitize.utils.jobs import enqueue_conversion_tasks
        from digitize.models import OutputFormat
        from digitize.settings import settings

        filenames = ["small.pdf"]
        doc_id_dict = self._make_doc_id_dict(filenames)
        (tmp_path / "small.pdf").write_bytes(b"%PDF-1.4")

        threshold = settings.digitize.heavy_doc_page_threshold
        created_tasks = []

        with patch(_DB_MANAGER_PATH) as mock_mgr, \
             patch("digitize.utils.jobs.get_document_page_count",
                   return_value=threshold - 1), \
             patch("digitize.utils.jobs.generate_uuid", return_value="t-small"):

            mock_mgr.create_conversion_task.side_effect = lambda **kw: created_tasks.append(kw)

            self._run(enqueue_conversion_tasks(
                job_id="j1",
                op_key="digitization",
                filenames=filenames,
                doc_id_dict=doc_id_dict,
                staging_dir=tmp_path,
                output_format=OutputFormat.JSON,
                quota=5,
                queued_for_op=0,
            ))

        assert created_tasks[0]["is_large"] is False

    def test_each_file_gets_unique_task_id(self, tmp_path):
        """A unique task_id must be generated for each file."""
        from digitize.utils.jobs import enqueue_conversion_tasks
        from digitize.models import OutputFormat

        filenames = ["f1.pdf", "f2.pdf", "f3.pdf"]
        doc_id_dict = self._make_doc_id_dict(filenames)
        for fn in filenames:
            (tmp_path / fn).write_bytes(b"%PDF-1.4")

        created_tasks = []
        uuids = ["t-1", "t-2", "t-3"]

        with patch(_DB_MANAGER_PATH) as mock_mgr, \
             patch("digitize.utils.jobs.get_document_page_count", return_value=5), \
             patch("digitize.utils.jobs.generate_uuid", side_effect=uuids):

            mock_mgr.create_conversion_task.side_effect = lambda **kw: created_tasks.append(kw)

            self._run(enqueue_conversion_tasks(
                job_id="j1",
                op_key="ingestion",
                filenames=filenames,
                doc_id_dict=doc_id_dict,
                staging_dir=tmp_path,
                output_format=OutputFormat.JSON,
                quota=10,
                queued_for_op=0,
            ))

        task_ids = [t["task_id"] for t in created_tasks]
        assert task_ids == uuids
        assert len(set(task_ids)) == 3  # all unique

    def test_cached_file_path_uses_staging_dir(self, tmp_path):
        """cached_file must be the staging path, not just the filename."""
        from digitize.utils.jobs import enqueue_conversion_tasks
        from digitize.models import OutputFormat

        filenames = ["doc.pdf"]
        doc_id_dict = self._make_doc_id_dict(filenames)
        (tmp_path / "doc.pdf").write_bytes(b"%PDF-1.4")

        created_tasks = []

        with patch(_DB_MANAGER_PATH) as mock_mgr, \
             patch("digitize.utils.jobs.get_document_page_count", return_value=0), \
             patch("digitize.utils.jobs.generate_uuid", return_value="t1"):

            mock_mgr.create_conversion_task.side_effect = lambda **kw: created_tasks.append(kw)

            self._run(enqueue_conversion_tasks(
                job_id="j1",
                op_key="digitization",
                filenames=filenames,
                doc_id_dict=doc_id_dict,
                staging_dir=tmp_path,
                output_format=OutputFormat.JSON,
                quota=5,
                queued_for_op=0,
            ))

        expected_path = str(tmp_path / "doc.pdf")
        assert created_tasks[0]["cached_file"] == expected_path
