"""
Unit tests for enqueue_conversion_tasks — utils/jobs.py.

Covers:
  - Correct slot allocation: first N tasks → 'queued', remainder → 'pending'
  - Page count used correctly to set is_large
  - All tasks are passed to create_conversion_tasks_batch in a single call
"""

import asyncio
from pathlib import Path
from unittest.mock import AsyncMock, Mock, patch

import pytest


# The db_manager singleton import path used at runtime inside enqueue_conversion_tasks
_DB_MANAGER_PATH = "digitize.db.manager.db_manager"


def _run_enqueue(tmp_path, filenames, *, page_count=10, queued_for_op=0,
                 quota=10, op_key="ingestion", uuids=None):
    """Helper: run enqueue_conversion_tasks, return the list passed to batch."""
    from digitize.utils.jobs import enqueue_conversion_tasks
    from digitize.models import OutputFormat

    doc_id_dict = {fn: f"doc-{i}" for i, fn in enumerate(filenames)}
    for fn in filenames:
        (tmp_path / fn).write_bytes(b"%PDF-1.4")

    batch_calls = []

    uuid_side = uuids if uuids else [f"task-{i}" for i in range(len(filenames))]
    with patch(_DB_MANAGER_PATH) as mock_mgr, \
         patch("digitize.utils.jobs.get_document_page_count", return_value=page_count), \
         patch("digitize.utils.jobs.generate_uuid", side_effect=uuid_side):

        mock_mgr.create_conversion_tasks_batch.side_effect = \
            lambda tasks: batch_calls.extend(tasks)

        asyncio.run(enqueue_conversion_tasks(
            job_id="j1",
            op_key=op_key,
            filenames=filenames,
            doc_id_dict=doc_id_dict,
            staging_dir=tmp_path,
            output_format=OutputFormat.JSON,
            quota=quota,
            queued_for_op=queued_for_op,
        ))

    return batch_calls


@pytest.mark.unit
class TestEnqueueConversionTasks:

    # ------------------------------------------------------------------
    # Basic smoke test
    # ------------------------------------------------------------------

    def test_single_file_queued_when_slots_available(self, tmp_path):
        tasks = _run_enqueue(tmp_path, ["file.pdf"], op_key="digitization")

        assert len(tasks) == 1
        assert tasks[0]["status"] == "queued"
        assert tasks[0]["operation"] == "digitization"
        assert tasks[0]["output_format"] == "json"

    def test_single_batch_call_for_multiple_files(self, tmp_path):
        """All rows must be delivered to create_conversion_tasks_batch in one call."""
        from digitize.utils.jobs import enqueue_conversion_tasks
        from digitize.models import OutputFormat

        filenames = [f"f{i}.pdf" for i in range(5)]
        doc_id_dict = {fn: f"doc-{i}" for i, fn in enumerate(filenames)}
        for fn in filenames:
            (tmp_path / fn).write_bytes(b"%PDF-1.4")

        with patch(_DB_MANAGER_PATH) as mock_mgr, \
             patch("digitize.utils.jobs.get_document_page_count", return_value=5), \
             patch("digitize.utils.jobs.generate_uuid",
                   side_effect=[f"t{i}" for i in range(5)]):

            asyncio.run(enqueue_conversion_tasks(
                job_id="j1",
                op_key="ingestion",
                filenames=filenames,
                doc_id_dict=doc_id_dict,
                staging_dir=tmp_path,
                output_format=OutputFormat.JSON,
                quota=10,
                queued_for_op=0,
            ))

        # batch method called exactly once; single-file method never called
        mock_mgr.create_conversion_tasks_batch.assert_called_once()
        mock_mgr.create_conversion_task.assert_not_called()
        # the single call received all 5 task dicts
        passed = mock_mgr.create_conversion_tasks_batch.call_args[0][0]
        assert len(passed) == 5

    # ------------------------------------------------------------------
    # Slot allocation
    # ------------------------------------------------------------------

    def test_excess_files_become_pending(self, tmp_path):
        """4 files submitted, 2 free slots → first 2 queued, last 2 pending."""
        tasks = _run_enqueue(
            tmp_path,
            [f"f{i}.pdf" for i in range(4)],
            queued_for_op=8, quota=10,  # 2 free slots
        )
        assert len(tasks) == 4
        assert [t["status"] for t in tasks] == ["queued", "queued", "pending", "pending"]

    def test_all_tasks_pending_when_no_slots_free(self, tmp_path):
        """queued_for_op == quota → 0 free slots → all tasks inserted as pending."""
        tasks = _run_enqueue(
            tmp_path,
            ["a.pdf", "b.pdf"],
            queued_for_op=5, quota=5,
        )
        assert all(t["status"] == "pending" for t in tasks)

    # ------------------------------------------------------------------
    # is_large classification
    # ------------------------------------------------------------------

    def test_large_file_sets_is_large_true(self, tmp_path):
        """Page count >= threshold → is_large=True."""
        from digitize.settings import settings
        threshold = settings.digitize.heavy_doc_page_threshold
        tasks = _run_enqueue(tmp_path, ["big.pdf"], page_count=threshold,
                             op_key="digitization")
        assert tasks[0]["is_large"] is True

    def test_normal_file_sets_is_large_false(self, tmp_path):
        """Page count < threshold → is_large=False."""
        from digitize.settings import settings
        threshold = settings.digitize.heavy_doc_page_threshold
        tasks = _run_enqueue(tmp_path, ["small.pdf"], page_count=threshold - 1,
                             op_key="digitization")
        assert tasks[0]["is_large"] is False

    # ------------------------------------------------------------------
    # Field correctness
    # ------------------------------------------------------------------

    def test_each_file_gets_unique_task_id(self, tmp_path):
        """A unique task_id must be generated for each file."""
        uuids = ["t-1", "t-2", "t-3"]
        tasks = _run_enqueue(tmp_path, ["f1.pdf", "f2.pdf", "f3.pdf"], uuids=uuids)
        assert [t["task_id"] for t in tasks] == uuids
        assert len({t["task_id"] for t in tasks}) == 3

    def test_cached_file_path_uses_staging_dir(self, tmp_path):
        """cached_file must be the full staging path, not just the filename."""
        tasks = _run_enqueue(tmp_path, ["doc.pdf"], page_count=0,
                             op_key="digitization")
        assert tasks[0]["cached_file"] == str(tmp_path / "doc.pdf")
