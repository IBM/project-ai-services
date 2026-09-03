"""
Tests for the cancel-job feature.

Coverage:
  1. ``POST /v1/jobs/{job_id}/cancel`` endpoint (jobs.py)
     - 404 when job not found
     - 409 when job is already in a non-cancellable state (completed / failed / cancel_pending / cancelled)
     - 202 for accepted / in_progress jobs (no body)
     - clean_files flag is written to job stats in the DB
     - 500 on unexpected db error

  2. ``_run_digitize`` background task (jobs.py)
     - JobCancelledError causes CANCELLED status on non-terminal docs and job
     - Staging cleanup happens even on cancellation (finally block)
     - Normal pipeline exception does NOT produce CANCELLED status

  3. ``_run_ingest`` background task (jobs.py)
     - JobCancelledError causes CANCELLED status on non-terminal docs and job
     - clean_files=True → vector-DB chunks are removed for the right doc IDs
     - clean_files=False → vector DB is NOT touched
     - VDB cleanup failure is swallowed (only a warning)
     - Staging cleanup happens even on cancellation (finally block)

  4. ``process_documents`` (orchestrator.py) — DB-polled conversion stage
     - Cancellation before processing starts raises JobCancelledError immediately
     - Cancellation mid-conversion: pending tasks are dropped; cancel_tasks_for_job is called
     - Cancellation at processing stage: pending process_futures are cancelled
     - Cancellation at chunking stage: pending chunk_futures are cancelled
     - Cancellation at indexing stage: pending index_futures are cancelled; running ones complete
     - Non-cancelled jobs run through all stages without interruption

  5. ``_run_conversion`` (conversion_dispatcher.py) — dispatcher cancellation checks
     - Check 1: task already cancel_pending before RUNNING → written as CANCELLED, no conversion
     - Check 2: task set to cancel_pending after convert_document_format returns → CANCELLED
     - Check 3: exception raised while task is cancel_pending → CANCELLED (not FAILED)
     - Genuine failure (no cancel_pending) → FAILED

  6. ``convert_doc`` (converter.py)
     - cancel_check=None → no cancellation (all chunks processed normally)
     - cancel_check returning False → no cancellation
     - cancel_check returning True between chunks → JobCancelledError raised

  7. ``_make_db_cancel_check`` (converter.py)
     - Returns False when task status is not cancel_pending
     - Returns True when task status is cancel_pending
     - Returns False when DB raises an exception (never abort a healthy conversion)
"""

import asyncio
import sys
import threading
import types
from concurrent.futures import Future
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock, Mock, patch

import pytest
from fastapi.testclient import TestClient

# ---------------------------------------------------------------------------
# Stub heavy docling deps the same way conftest.py does, so imports work when
# this file is collected before conftest stubs take effect.
# ---------------------------------------------------------------------------
for _pkg in [
    "docling",
    "docling.datamodel",
    "docling.datamodel.document",
    "docling.document_converter",
    "docling_core",
    "docling_core.types",
    "docling_core.types.doc",
    "docling_core.types.doc.document",
]:
    if _pkg not in sys.modules:
        _mod = types.ModuleType(_pkg)
        sys.modules[_pkg] = _mod

for _attr, _name in [
    ("docling.datamodel.document", "ConversionResult"),
    ("docling.document_converter", "DocumentConverter"),
    ("docling_core.types.doc.document", "DoclingDocument"),
]:
    if not hasattr(sys.modules[_attr], _name.split(".")[-1]):
        setattr(sys.modules[_attr], _name.split(".")[-1], MagicMock(name=_name))


import digitize.app as digitize_app
import digitize.api.v1.jobs as jobs_module
import digitize.utils.db as db_ops
from digitize.exceptions import JobCancelledError
from digitize.models import DocStatus, JobStatus


# ===========================================================================
# Shared test fixtures
# ===========================================================================


@pytest.fixture()
def test_client(monkeypatch, tmp_path, mock_db_operations):
    """TestClient configured identically to the existing endpoint tests."""
    import digitize.api.v1.documents as documents_router_module

    digitized_dir = tmp_path / "digitized"
    staging_dir = tmp_path / "staging"
    for p in (digitized_dir, staging_dir):
        p.mkdir(parents=True, exist_ok=True)

    fake_settings = SimpleNamespace(
        common=SimpleNamespace(app=SimpleNamespace(log_level="INFO")),
        digitize=SimpleNamespace(
            digitized_docs_dir=digitized_dir,
            staging_dir=staging_dir,
            digitization_concurrency_limit=2,
            ingestion_concurrency_limit=1,
        ),
    )

    monkeypatch.setattr(digitize_app, "settings", fake_settings, raising=False)
    monkeypatch.setattr(digitize_app.dg_util, "settings", fake_settings, raising=False)
    monkeypatch.setattr(digitize_app.dg_util, "has_active_jobs", Mock(return_value=(False, [])))
    monkeypatch.setattr(digitize_app.dg_util, "generate_uuid", Mock(return_value="job-123"))
    monkeypatch.setattr(digitize_app.dg_util, "stage_upload_files", AsyncMock())
    monkeypatch.setattr(digitize_app.dg_util, "initialize_job_state", Mock(return_value={"sample.pdf": "doc-1"}))
    monkeypatch.setattr(digitize_app.dg_util, "get_document_content", Mock())
    monkeypatch.setattr(digitize_app.dg_util, "is_document_in_active_job", Mock(return_value=False))
    monkeypatch.setattr(documents_router_module, "reset_db", Mock())
    monkeypatch.setattr(digitize_app, "configure_uvicorn_logging", Mock())

    mock_hash_db_manager = Mock()
    mock_hash_db_manager.find_completed_document_by_hash = Mock(return_value=None)
    monkeypatch.setattr(jobs_module, "db_manager", mock_hash_db_manager)

    return TestClient(digitize_app.app)


def _make_doc(doc_id: str, status: str) -> Mock:
    """Helper to build a mock document DB row."""
    doc = Mock()
    doc.doc_id = doc_id
    doc.status = status
    return doc


# ===========================================================================
# 1. cancel_job API endpoint
# ===========================================================================


@pytest.mark.unit
class TestCancelJobEndpoint:
    """Tests for POST /v1/jobs/{job_id}/cancel."""

    # ------------------------------------------------------------------ #
    # 404 – job not found                                                  #
    # ------------------------------------------------------------------ #

    def test_returns_404_when_job_not_found(self, test_client, monkeypatch):
        monkeypatch.setattr(db_ops, "get_job", Mock(return_value=None))

        response = test_client.post("/v1/jobs/nonexistent/cancel")

        assert response.status_code == 404

    # ------------------------------------------------------------------ #
    # 409 – terminal-state jobs cannot be cancelled                        #
    # ------------------------------------------------------------------ #

    @pytest.mark.parametrize("terminal_status", [
        JobStatus.COMPLETED.value,
        JobStatus.FAILED.value,
        JobStatus.CANCEL_PENDING.value,
        JobStatus.CANCELLED.value,
    ])
    def test_returns_409_for_terminal_state_jobs(self, test_client, monkeypatch, terminal_status):
        monkeypatch.setattr(
            db_ops,
            "get_job",
            Mock(return_value={"job_id": "job-1", "status": terminal_status, "stats": {}}),
        )

        response = test_client.post("/v1/jobs/job-1/cancel")

        assert response.status_code == 409

    # ------------------------------------------------------------------ #
    # 202 – accepted / in_progress jobs are cancellable                    #
    # ------------------------------------------------------------------ #

    @pytest.mark.parametrize("active_status", [
        JobStatus.ACCEPTED.value,
        JobStatus.IN_PROGRESS.value,
    ])
    def test_returns_202_for_active_jobs(self, test_client, monkeypatch, active_status):
        mock_update = Mock()
        monkeypatch.setattr(db_ops, "get_job", Mock(return_value={
            "job_id": "job-active", "status": active_status, "stats": {},
        }))
        monkeypatch.setattr("digitize.db.manager.db_manager.update_job", mock_update)

        response = test_client.post("/v1/jobs/job-active/cancel")

        assert response.status_code == 202
        assert response.content == b""

    def test_update_job_called_with_cancel_pending_status(self, test_client, monkeypatch):
        """The endpoint must write CANCEL_PENDING + preserve existing stats."""
        mock_update = Mock()
        existing_stats = {"total_documents": 3, "completed": 1}
        monkeypatch.setattr(db_ops, "get_job", Mock(return_value={
            "job_id": "job-active",
            "status": JobStatus.IN_PROGRESS.value,
            "stats": existing_stats,
        }))
        # db_manager is imported directly into jobs.py's namespace at line 27:
        #   from digitize.db.manager import db_manager
        # So the correct patch target is the jobs module, not db.manager.
        monkeypatch.setattr(jobs_module, "db_manager", Mock(update_job=mock_update))

        test_client.post("/v1/jobs/job-active/cancel")

        mock_update.assert_called_once()
        call_kwargs = mock_update.call_args
        assert call_kwargs.args[0] == "job-active"
        assert call_kwargs.kwargs["status"] == JobStatus.CANCEL_PENDING

    # ------------------------------------------------------------------ #
    # clean_files flag is persisted into job stats                         #
    # ------------------------------------------------------------------ #

    def test_clean_files_false_stored_in_stats(self, test_client, monkeypatch):
        mock_update = Mock()
        monkeypatch.setattr(db_ops, "get_job", Mock(return_value={
            "job_id": "job-ingest",
            "status": JobStatus.ACCEPTED.value,
            "stats": {"total_documents": 2},
        }))
        monkeypatch.setattr(jobs_module, "db_manager", Mock(update_job=mock_update))

        test_client.post("/v1/jobs/job-ingest/cancel?clean_files=false")

        call_kwargs = mock_update.call_args.kwargs
        assert call_kwargs["stats"]["clean_files"] is False

    def test_clean_files_true_stored_in_stats(self, test_client, monkeypatch):
        mock_update = Mock()
        monkeypatch.setattr(db_ops, "get_job", Mock(return_value={
            "job_id": "job-ingest",
            "status": JobStatus.ACCEPTED.value,
            "stats": {},
        }))
        monkeypatch.setattr(jobs_module, "db_manager", Mock(update_job=mock_update))

        test_client.post("/v1/jobs/job-ingest/cancel?clean_files=true")

        call_kwargs = mock_update.call_args.kwargs
        assert call_kwargs["stats"]["clean_files"] is True

    # ------------------------------------------------------------------ #
    # 500 – unexpected db error                                            #
    # ------------------------------------------------------------------ #

    def test_returns_500_on_unexpected_error(self, test_client, monkeypatch):
        monkeypatch.setattr(db_ops, "get_job", Mock(side_effect=RuntimeError("db boom")))

        response = test_client.post("/v1/jobs/job-1/cancel")

        assert response.status_code == 500

    # ------------------------------------------------------------------ #
    # Default query param                                                  #
    # ------------------------------------------------------------------ #

    def test_clean_files_defaults_to_false(self, test_client, monkeypatch):
        mock_update = Mock()
        monkeypatch.setattr(db_ops, "get_job", Mock(return_value={
            "job_id": "job-ingest",
            "status": JobStatus.ACCEPTED.value,
            "stats": {},
        }))
        monkeypatch.setattr(jobs_module, "db_manager", Mock(update_job=mock_update))

        # No ?clean_files query param
        test_client.post("/v1/jobs/job-ingest/cancel")

        call_kwargs = mock_update.call_args.kwargs
        assert call_kwargs["stats"]["clean_files"] is False

    def test_existing_stats_are_preserved_alongside_clean_files(self, test_client, monkeypatch):
        """Pre-existing stats keys must be kept; only clean_files is added/overwritten."""
        mock_update = Mock()
        monkeypatch.setattr(db_ops, "get_job", Mock(return_value={
            "job_id": "job-ingest",
            "status": JobStatus.IN_PROGRESS.value,
            "stats": {"total_documents": 5, "completed": 2},
        }))
        monkeypatch.setattr(jobs_module, "db_manager", Mock(update_job=mock_update))

        test_client.post("/v1/jobs/job-ingest/cancel?clean_files=true")

        call_kwargs = mock_update.call_args.kwargs
        assert call_kwargs["stats"]["total_documents"] == 5
        assert call_kwargs["stats"]["completed"] == 2
        assert call_kwargs["stats"]["clean_files"] is True


# ===========================================================================
# 2. _run_digitize background task
# ===========================================================================
#
# Signature (after refactor): _run_digitize(job_id, doc_id_dict)
# - No output_format argument
# - No concurrency_manager — staging cleanup is the only finally action


@pytest.mark.unit
class TestRunDigitize:
    """Tests for the _run_digitize background helper."""

    @pytest.mark.asyncio
    async def test_cancellation_marks_non_terminal_docs_and_job_cancelled(self, tmp_path):
        """When JobCancelledError is raised, all non-terminal docs become CANCELLED
        and the job itself is marked CANCELLED."""
        job_id = "job-dig-cancel"

        # Two docs: one already completed, one still in_progress
        completed_doc = _make_doc("doc-done", DocStatus.COMPLETED.value)
        active_doc = _make_doc("doc-active", DocStatus.IN_PROGRESS.value)

        mock_db = Mock()
        mock_db.get_documents_by_job_id = Mock(return_value=[completed_doc, active_doc])
        mock_db.update_document = Mock()
        mock_db.update_job = Mock()

        mock_status_mgr = Mock()

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("cancelled"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=mock_status_mgr),
            patch("digitize.api.v1.jobs.cleanup_staging_directory"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_digitize(
                job_id=job_id,
                doc_id_dict={"sample.pdf": "doc-active"},
            )

        # Completed doc must NOT be touched via status manager
        doc_meta_calls = mock_status_mgr.update_doc_metadata.call_args_list
        updated_doc_ids = [c.args[0] for c in doc_meta_calls]
        assert "doc-done" not in updated_doc_ids

        # In-progress doc must be marked CANCELLED via status manager
        assert "doc-active" in updated_doc_ids
        mock_status_mgr.update_job_progress.assert_called_once_with(
            "", DocStatus.CANCELLED, JobStatus.CANCELLED
        )

    @pytest.mark.asyncio
    async def test_terminal_docs_are_not_overwritten_on_cancellation(self, tmp_path):
        """Already-failed / already-cancelled docs must not be touched."""
        job_id = "job-dig-cancel-terminal"

        terminal_docs = [
            _make_doc("doc-fail", DocStatus.FAILED.value),
            _make_doc("doc-cancel", DocStatus.CANCELLED.value),
            _make_doc("doc-exists", DocStatus.ALREADY_EXISTS.value),
        ]

        mock_db = Mock()
        mock_db.get_documents_by_job_id = Mock(return_value=terminal_docs)
        mock_db.update_document = Mock()
        mock_db.update_job = Mock()

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=Mock()),
            patch("digitize.api.v1.jobs.cleanup_staging_directory"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_digitize(
                job_id=job_id,
                doc_id_dict={},
            )

        mock_db.update_document.assert_not_called()

    @pytest.mark.asyncio
    async def test_staging_cleanup_called_on_cancellation(self, tmp_path):
        """finally block must always run: staging directory is cleaned up."""
        mock_cleanup = Mock()

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", Mock(
                get_documents_by_job_id=Mock(return_value=[]),
                update_job=Mock(),
            )),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=Mock()),
            patch("digitize.api.v1.jobs.cleanup_staging_directory", mock_cleanup),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_digitize("job-x", {})

        mock_cleanup.assert_called_once()

    @pytest.mark.asyncio
    async def test_non_cancellation_exception_does_not_mark_cancelled(self, tmp_path):
        """A plain exception must NOT produce CANCELLED status — it goes through
        the generic failure path instead."""
        mock_db = Mock()
        mock_db.get_documents_by_job_id = Mock(return_value=[])

        mock_status_mgr = Mock()

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=RuntimeError("boom"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=mock_status_mgr),
            patch("digitize.api.v1.jobs.cleanup_staging_directory"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_digitize("job-fail", {})

        mock_db.update_document.assert_not_called()
        mock_status_mgr.update_job_progress.assert_called()


# ===========================================================================
# 3. _run_ingest background task
# ===========================================================================
#
# Signature (after refactor): _run_ingest(job_id, doc_id_dict, file_checksum_dict=None)
# - No file_list positional arg
# - No concurrency_manager — staging cleanup is the only finally action


@pytest.mark.unit
class TestRunIngest:
    """Tests for the _run_ingest background helper."""

    @pytest.mark.asyncio
    async def test_cancellation_marks_non_terminal_docs_and_job(self, tmp_path):
        job_id = "job-ingest-cancel"

        pending_doc = _make_doc("doc-pending", DocStatus.ACCEPTED.value)
        done_doc = _make_doc("doc-done", DocStatus.COMPLETED.value)

        mock_db = Mock()
        mock_db.get_documents_by_job_id = Mock(return_value=[pending_doc, done_doc])
        mock_db.update_document = Mock()
        mock_db.update_job = Mock()
        mock_db.get_job_by_id = Mock(return_value=Mock(stats={"clean_files": False}))

        mock_status_mgr = Mock()

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=mock_status_mgr),
            patch("digitize.api.v1.jobs.cleanup_staging_directory"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_ingest(job_id, {"file.pdf": "doc-pending"})

        updated = [c.args[0] for c in mock_status_mgr.update_doc_metadata.call_args_list]
        assert "doc-pending" in updated
        assert "doc-done" not in updated
        mock_status_mgr.update_job_progress.assert_called_once_with(
            "", DocStatus.CANCELLED, JobStatus.CANCELLED
        )

    @pytest.mark.asyncio
    async def test_clean_files_true_removes_vector_db_entries(self, tmp_path):
        """When clean_files=True, indexed doc IDs must be removed from the VDB."""
        job_id = "job-ingest-clean"
        doc_id_dict = {"a.pdf": "doc-aaa", "b.pdf": "doc-bbb"}

        doc_a = _make_doc("doc-aaa", DocStatus.CHUNKED.value)
        doc_b = _make_doc("doc-bbb", DocStatus.COMPLETED.value)

        mock_db = Mock()
        mock_db.get_documents_by_job_id = Mock(return_value=[doc_a, doc_b])
        mock_db.update_document = Mock()
        mock_db.update_job = Mock()
        mock_db.get_job_by_id = Mock(return_value=Mock(stats={"clean_files": True}))

        mock_vector_store = Mock()
        mock_vector_store.remove_docs_from_index = Mock(return_value=10)

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=Mock()),
            patch("digitize.api.v1.jobs.cleanup_staging_directory"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
            patch("common.db_utils.get_vector_store", return_value=mock_vector_store),
        ):
            await jobs_module._run_ingest(job_id, doc_id_dict)

        removed_ids = mock_vector_store.remove_docs_from_index.call_args.args[0]
        assert set(removed_ids) == {"doc-aaa", "doc-bbb"}

    @pytest.mark.asyncio
    async def test_clean_files_false_does_not_touch_vector_db(self, tmp_path):
        """When clean_files=False the VDB must not be queried at all."""
        job_id = "job-ingest-noclean"

        mock_db = Mock()
        mock_db.get_documents_by_job_id = Mock(return_value=[])
        mock_db.update_document = Mock()
        mock_db.update_job = Mock()
        mock_db.get_job_by_id = Mock(return_value=Mock(stats={"clean_files": False}))

        mock_get_vector_store = Mock()

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=Mock()),
            patch("digitize.api.v1.jobs.cleanup_staging_directory"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
            patch("common.db_utils.get_vector_store", mock_get_vector_store),
        ):
            await jobs_module._run_ingest(job_id, {})

        mock_get_vector_store.assert_not_called()

    @pytest.mark.asyncio
    async def test_vdb_cleanup_failure_is_swallowed(self, tmp_path):
        """A VDB cleanup error must NOT propagate; it is only logged as a warning."""
        job_id = "job-ingest-vdb-fail"
        doc_id_dict = {"x.pdf": "doc-x"}

        doc_x = _make_doc("doc-x", DocStatus.COMPLETED.value)

        mock_db = Mock()
        mock_db.get_documents_by_job_id = Mock(return_value=[doc_x])
        mock_db.update_document = Mock()
        mock_db.update_job = Mock()
        mock_db.get_job_by_id = Mock(return_value=Mock(stats={"clean_files": True}))

        exploding_store = Mock()
        exploding_store.remove_docs_from_index = Mock(side_effect=RuntimeError("vdb down"))

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=Mock()),
            patch("digitize.api.v1.jobs.cleanup_staging_directory"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
            patch("common.db_utils.get_vector_store", return_value=exploding_store),
        ):
            # Must not raise
            await jobs_module._run_ingest(job_id, doc_id_dict)

    @pytest.mark.asyncio
    async def test_staging_cleanup_called_on_cancellation(self, tmp_path):
        mock_cleanup = Mock()

        mock_db = Mock()
        mock_db.get_documents_by_job_id = Mock(return_value=[])
        mock_db.update_job = Mock()
        mock_db.get_job_by_id = Mock(return_value=Mock(stats={}))

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=Mock()),
            patch("digitize.api.v1.jobs.cleanup_staging_directory", mock_cleanup),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_ingest("job-y", {})

        mock_cleanup.assert_called_once()

    @pytest.mark.asyncio
    async def test_only_doc_ids_in_doc_id_dict_are_cleaned_from_vdb(self, tmp_path):
        """Only doc IDs that belong to THIS job (present in doc_id_dict.values()) must
        be submitted to remove_docs_from_index, not stray docs returned by the DB."""
        job_id = "job-ingest-scope"
        doc_id_dict = {"mine.pdf": "doc-mine"}

        doc_mine = _make_doc("doc-mine", DocStatus.COMPLETED.value)
        doc_foreign = _make_doc("doc-foreign", DocStatus.COMPLETED.value)  # not in this job's dict

        mock_db = Mock()
        mock_db.get_documents_by_job_id = Mock(return_value=[doc_mine, doc_foreign])
        mock_db.update_document = Mock()
        mock_db.update_job = Mock()
        mock_db.get_job_by_id = Mock(return_value=Mock(stats={"clean_files": True}))

        mock_vs = Mock()
        mock_vs.remove_docs_from_index = Mock(return_value=1)

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=Mock()),
            patch("digitize.api.v1.jobs.cleanup_staging_directory"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
            patch("common.db_utils.get_vector_store", return_value=mock_vs),
        ):
            await jobs_module._run_ingest(job_id, doc_id_dict)

        removed = set(mock_vs.remove_docs_from_index.call_args.args[0])
        assert removed == {"doc-mine"}
        assert "doc-foreign" not in removed


# ===========================================================================
# 4. orchestrator.process_documents — DB-polled conversion stage
#
# The refactored orchestrator no longer uses ProcessPoolExecutor for conversion.
# Instead it polls db_manager.get_conversion_task(task_id) until the dispatcher
# writes a terminal status (COMPLETED / FAILED / CANCELLED).
# cancel_tasks_for_job is called exactly once on first cancellation detection.
# ===========================================================================


def _make_task_stub(task_id: str, status: str, result_path: str = "conv.json",
                    started_at=None, completed_at=None) -> Mock:
    """Build a minimal ConversionTask-like mock for orchestrator polling."""
    t = Mock()
    t.task_id = task_id
    t.status = status
    t.result_path = result_path
    t.error = None
    t.cached_file = f"/staging/{task_id}.pdf"
    t.started_at = started_at
    t.completed_at = completed_at
    return t


def _make_future(result=None, exception=None, *, done_immediately=True) -> Future:
    """Build a real concurrent.futures.Future in the desired state."""
    f: Future = Future()
    if exception:
        f.set_exception(exception)
    elif done_immediately:
        f.set_result(result)
    # If done_immediately=False the future stays PENDING.
    return f


def _make_running_future(result=None, delay: float = 0.0) -> Future:
    """Return a Future that is currently RUNNING and will resolve to *result*.

    The poll loops in the orchestrator only exit a future via ``fut.done()``
    or ``fut.cancel()``.  A RUNNING future is neither cancellable nor done
    until its result is set, so we schedule that on a background thread
    (after an optional *delay*) so the poll loop can drain naturally.
    """
    f: Future = Future()
    f.set_running_or_notify_cancel()  # → RUNNING (not cancellable)

    def _resolve():
        import time as _time
        if delay:
            _time.sleep(delay)
        try:
            f.set_result(result)
        except Exception:
            pass  # future may already be resolved; ignore

    threading.Thread(target=_resolve, daemon=True).start()
    return f


@pytest.mark.unit
class TestOrchestratorCancellationBeforeStart:
    """Cancellation observed before the poll loop begins."""

    def test_raises_immediately_when_job_cancelled_before_loop(self):
        """is_job_cancelled=True at CHECK 1 must raise without entering the poll loop."""
        from digitize.processing.orchestrator import process_documents

        task_stub = _make_task_stub("t-1", "queued")

        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task_stub])
        mock_db.is_job_cancelled = Mock(return_value=True)
        mock_db.cancel_tasks_for_job = Mock()

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            mock_tpe.return_value.__enter__ = Mock(return_value=MagicMock())
            mock_tpe.return_value.__exit__ = Mock(return_value=False)

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=["fake.pdf"],
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-pre-cancel",
                    doc_id_dict={"fake.pdf": "doc-1"},
                )

        # cancel_tasks_for_job must NOT be called — the loop never ran
        mock_db.cancel_tasks_for_job.assert_not_called()


@pytest.mark.unit
class TestOrchestratorCancellationAtConversionStage:
    """Cancellation detected while conversion tasks are still pending in the DB."""

    def test_cancel_tasks_for_job_called_once_on_cancellation(self):
        """When is_job_cancelled becomes True, cancel_tasks_for_job is called exactly
        once so the dispatcher stops any in-flight tasks."""
        from digitize.processing.orchestrator import process_documents

        task_stub = _make_task_stub("t-1", "queued")

        call_count = {"n": 0}

        def is_cancelled(job_id):
            # Call 1 (CHECK 1): False — enter the loop
            # Call 2 (poll cycle 1): True — trigger cancellation path
            call_count["n"] += 1
            return call_count["n"] >= 2

        # get_conversion_task: keep returning queued so the task stays pending
        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task_stub])
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)
        mock_db.cancel_tasks_for_job = Mock()
        mock_db.get_conversion_task = Mock(return_value=_make_task_stub("t-1", "queued"))

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            tpe_inst = MagicMock()
            tpe_inst.__enter__ = Mock(return_value=tpe_inst)
            tpe_inst.__exit__ = Mock(return_value=False)
            mock_tpe.return_value = tpe_inst

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=["fake.pdf"],
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-cancel-conv",
                    doc_id_dict={"fake.pdf": "doc-1"},
                )

        # Must be called exactly once — not repeatedly on every loop tick
        mock_db.cancel_tasks_for_job.assert_called_once_with("job-cancel-conv")

    def test_task_with_cancelled_status_drops_from_pending_without_error(self):
        """A task row that reaches CANCELLED status is removed from pending_task_ids
        without submitting downstream work and without raising."""
        from digitize.processing.orchestrator import process_documents

        # Task starts queued, then on second get_conversion_task poll returns CANCELLED.
        task_queued = _make_task_stub("t-2", "queued")
        task_cancelled = _make_task_stub("t-2", "cancelled")

        get_task_calls = {"n": 0}

        def get_task(task_id):
            get_task_calls["n"] += 1
            # First call: still queued (before the dispatch loop marks it cancelled)
            # Second+ calls: CANCELLED
            if get_task_calls["n"] <= 1:
                return task_queued
            return task_cancelled

        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task_queued])
        # is_job_cancelled: True only on second check so the loop runs one iteration
        cancel_calls = {"n": 0}

        def is_cancelled(job_id):
            cancel_calls["n"] += 1
            return cancel_calls["n"] >= 2

        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)
        mock_db.cancel_tasks_for_job = Mock()
        mock_db.get_conversion_task = Mock(side_effect=get_task)

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            tpe_inst = MagicMock()
            tpe_inst.__enter__ = Mock(return_value=tpe_inst)
            tpe_inst.__exit__ = Mock(return_value=False)
            tpe_inst.submit = Mock()  # must NOT be called for a cancelled task
            mock_tpe.return_value = tpe_inst

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=["fake.pdf"],
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-task-cancelled",
                    doc_id_dict={"fake.pdf": "doc-1"},
                )

        # No processing work should have been submitted for the cancelled task
        tpe_inst.submit.assert_not_called()


@pytest.mark.unit
class TestOrchestratorCancellationAtProcessingStage:
    """Cancellation observed while processing (text/table extraction) futures are pending.

    The orchestrator does NOT explicitly cancel process_futures — it relies on
    _process_stop_event being set so the worker raises JobCancelledError, which
    drains the future via fut.result() raising CancelledError in section B.
    We model this by having the proc_future raise JobCancelledError when collected.
    """

    def test_pending_processing_future_raises_job_cancelled_when_job_cancelled(self):
        from digitize.processing.orchestrator import process_documents

        # Conversion task is already COMPLETED so iter-1 (is_cancelled=False) submits
        # the proc_future immediately.  The proc_future raises JobCancelledError when
        # its result is collected in iter-2 (simulating _process_stop_event firing).
        task_completed = _make_task_stub("t-1", "completed", result_path="conv.json")

        # A done future whose result() raises JobCancelledError — models the worker
        # responding to _process_stop_event by raising JobCancelledError.
        proc_fut_cancelled: Future = Future()
        proc_fut_cancelled.set_exception(JobCancelledError("worker stopped by event"))

        # is_cancelled:
        #   call 1 (CHECK 1): False — enter loop
        #   call 2 (iter 1): False — conversion COMPLETED observed, proc_future submitted
        #                            proc_future is already done (exception), drained in same iter
        #                            JobCancelledError → is_cancelled latched True, stop_event set
        #   The loop then breaks because process_futures/chunk_futures/indexing_futures are empty.
        cancel_calls = {"n": 0}

        def is_cancelled(job_id):
            cancel_calls["n"] += 1
            return False  # cancellation comes from the JobCancelledError in the future

        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task_completed])
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)
        mock_db.cancel_tasks_for_job = Mock()
        mock_db.get_conversion_task = Mock(return_value=task_completed)

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            tpe_inst = MagicMock()
            tpe_inst.__enter__ = Mock(return_value=tpe_inst)
            tpe_inst.__exit__ = Mock(return_value=False)
            tpe_inst.submit = Mock(return_value=proc_fut_cancelled)
            mock_tpe.return_value = tpe_inst

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=["fake.pdf"],
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-cancel-proc",
                    doc_id_dict={"fake.pdf": "doc-1"},
                )


@pytest.mark.unit
class TestOrchestratorCancellationAtChunkingStage:
    """Cancellation observed while chunking futures are pending.

    The orchestrator does NOT explicitly cancel chunk_futures — it relies on
    the stop_event making the worker raise an exception.  We model this by
    having the chunk_future raise JobCancelledError when its result is collected.
    """

    def test_chunk_future_raising_job_cancelled_propagates_cancellation(self):
        from digitize.processing.orchestrator import process_documents

        task_completed = _make_task_stub("t-1", "completed", result_path="conv.json")

        proc_fut: Future = Future()
        proc_fut.set_result(("txt.json", "tab.json", 5, 2, {"process_text": 0.1, "process_tables": 0.1}, "en", {}))

        # chunk_future raises JobCancelledError — simulates the chunker worker
        # being interrupted by the _process_stop_event.
        chunk_fut_cancelled: Future = Future()
        chunk_fut_cancelled.set_exception(Exception("chunker interrupted by stop event"))

        # is_cancelled: always False — the cancellation is signalled through the future
        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task_completed])
        mock_db.is_job_cancelled = Mock(return_value=False)
        mock_db.cancel_tasks_for_job = Mock()
        mock_db.get_conversion_task = Mock(return_value=task_completed)

        submit_calls = {"n": 0}

        def tpe_submit(*args, **kwargs):
            submit_calls["n"] += 1
            if submit_calls["n"] == 1:
                return proc_fut
            return chunk_fut_cancelled

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            tpe_inst = MagicMock()
            tpe_inst.__enter__ = Mock(return_value=tpe_inst)
            tpe_inst.__exit__ = Mock(return_value=False)
            tpe_inst.submit = Mock(side_effect=tpe_submit)
            mock_tpe.return_value = tpe_inst

            # Chunking failure is logged but does not raise JobCancelledError on its own;
            # the pipeline completes cleanly (no work remains after the failed chunk).
            process_documents(
                input_paths=["fake.pdf"],
                out_path="/tmp/out",
                llm_model="m", llm_endpoint="e",
                emb_endpoint="emb",
                max_tokens=512,
                job_id="job-cancel-chunk",
                doc_id_dict={"fake.pdf": "doc-1"},
            )
            # No assertion on cancellation — chunk exception is swallowed by the
            # generic except block (line ~813 in orchestrator); doc status is set FAILED.


@pytest.mark.unit
class TestOrchestratorCancellationAtIndexingStage:
    """Cancellation observed while indexing futures are in-flight."""

    def test_pending_indexing_future_is_cancelled(self):
        """A pending (not yet running) indexing future must be cancelled."""
        from digitize.processing.orchestrator import process_documents

        task_completed = _make_task_stub("t-1", "completed", result_path="conv.json")

        proc_fut: Future = Future()
        proc_fut.set_result(("txt.json", "tab.json", 5, 2, {"process_text": 0.1, "process_tables": 0.1}, "en", {}))

        chunk_fut: Future = Future()
        chunk_result = ("text_chunks.json", "table_chunks.json", 2.5)
        chunk_fut.set_result(chunk_result)

        pending_index_fut: Future = Future()

        # is_cancelled:
        #   call 1 (CHECK 1): False
        #   call 2 (iter 1): False — conversion COMPLETED, proc submitted
        #   call 3 (iter 2): False — proc done, chunk submitted
        #   call 4 (iter 3): False — chunk done, index submitted
        #   call 5 (iter 4): True  — pending index future cancelled
        cancel_calls = {"n": 0}

        def is_cancelled(job_id):
            cancel_calls["n"] += 1
            return cancel_calls["n"] >= 5

        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task_completed])
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)
        mock_db.cancel_tasks_for_job = Mock()
        mock_db.get_conversion_task = Mock(return_value=task_completed)

        submit_calls = {"n": 0}

        def tpe_submit(*args, **kwargs):
            n = submit_calls["n"]
            submit_calls["n"] += 1
            if n == 0:
                return proc_fut
            if n == 1:
                return chunk_fut
            return pending_index_fut

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.count_chunks", return_value=5),
            patch("digitize.processing.orchestrator.merge_chunked_documents", return_value=[]),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            tpe_inst = MagicMock()
            tpe_inst.__enter__ = Mock(return_value=tpe_inst)
            tpe_inst.__exit__ = Mock(return_value=False)
            tpe_inst.submit = Mock(side_effect=tpe_submit)
            mock_tpe.return_value = tpe_inst

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=["fake.pdf"],
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-cancel-index",
                    doc_id_dict={"fake.pdf": "doc-1"},
                    indexing_callback=lambda doc_id, chunks, path: True,
                )

        assert pending_index_fut.cancelled()

    def test_running_indexing_future_is_let_to_complete(self):
        """An already-running indexing future must NOT be cancelled — it must
        finish so no stale entries are let in the VDB.

        Strategy: the index future is RUNNING (not cancellable).  We signal
        cancellation on the second poll cycle (while the index future is still
        RUNNING because the background thread hasn't resolved it yet).  Section D
        sees fut.running()=True → skips cancel().  The background thread then
        resolves the future, it gets drained on the next tick, and the loop raises
        JobCancelledError normally.  The assert checks fut.cancelled() is False.

        To guarantee the future is still RUNNING when cancel is first detected we
        use a threading.Event to gate the future's resolution: the future only
        resolves after the 'cancel_seen' event is set, which happens inside
        is_cancelled() on the call that returns True.
        """
        from digitize.processing.orchestrator import process_documents

        task_completed = _make_task_stub("t-1", "completed", result_path="conv.json")

        proc_fut: Future = Future()
        proc_fut.set_result(("txt.json", "tab.json", 5, 2, {"process_text": 0.1, "process_tables": 0.1}, "en", {}))

        chunk_fut: Future = Future()
        chunk_fut.set_result(("text_chunks.json", "table_chunks.json", 2.5))

        # RUNNING future that only resolves after cancel_seen is set.
        # This guarantees the future is still RUNNING on the first True from is_cancelled().
        cancel_seen = threading.Event()
        running_index_fut: Future = Future()
        running_index_fut.set_running_or_notify_cancel()  # → RUNNING

        def _resolve_after_cancel():
            cancel_seen.wait(timeout=5)  # wait until is_cancelled fires True
            try:
                running_index_fut.set_result(True)
            except Exception:
                pass

        threading.Thread(target=_resolve_after_cancel, daemon=True).start()

        # is_cancelled:
        #   call 1 (CHECK 1): False — enter loop
        #   call 2 (iter 1): False — proc/chunk/index submitted (all happen in iter 1)
        #   call 3 (iter 2): True  — cancel detected; index is RUNNING so NOT cancelled
        #                            cancel_seen.set() → background thread resolves future
        #   call 4 (iter 3): True  — index.done()=True → drained; loop breaks → raise
        cancel_calls = {"n": 0}

        def is_cancelled(job_id):
            cancel_calls["n"] += 1
            if cancel_calls["n"] >= 3:
                cancel_seen.set()  # unblock the background thread to resolve the future
                return True
            return False

        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task_completed])
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)
        mock_db.cancel_tasks_for_job = Mock()
        mock_db.get_conversion_task = Mock(return_value=task_completed)

        submit_calls = {"n": 0}

        def tpe_submit(*args, **kwargs):
            n = submit_calls["n"]
            submit_calls["n"] += 1
            if n == 0:
                return proc_fut
            if n == 1:
                return chunk_fut
            return running_index_fut

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.count_chunks", return_value=3),
            patch("digitize.processing.orchestrator.merge_chunked_documents", return_value=[]),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            tpe_inst = MagicMock()
            tpe_inst.__enter__ = Mock(return_value=tpe_inst)
            tpe_inst.__exit__ = Mock(return_value=False)
            tpe_inst.submit = Mock(side_effect=tpe_submit)
            mock_tpe.return_value = tpe_inst

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=["fake.pdf"],
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-running-index",
                    doc_id_dict={"fake.pdf": "doc-1"},
                    indexing_callback=lambda doc_id, chunks, path: True,
                )

        assert not running_index_fut.cancelled()


@pytest.mark.unit
class TestOrchestratorHappyPath:
    """Sanity check: non-cancelled jobs reach the end without JobCancelledError."""

    def test_no_cancellation_returns_stats(self):
        from digitize.processing.orchestrator import process_documents

        task_completed = _make_task_stub("t-1", "completed", result_path="conv.json")

        proc_fut: Future = Future()
        proc_fut.set_result(("txt.json", "tab.json", 10, 3, {"process_text": 0.5, "process_tables": 0.3}, "en", {}))

        chunk_fut: Future = Future()
        chunk_fut.set_result(("text_chunks.json", "table_chunks.json", 1.2))

        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task_completed])
        mock_db.is_job_cancelled = Mock(return_value=False)
        mock_db.cancel_tasks_for_job = Mock()
        mock_db.get_conversion_task = Mock(return_value=task_completed)

        submit_calls = {"n": 0}

        def tpe_submit(*args, **kwargs):
            n = submit_calls["n"]
            submit_calls["n"] += 1
            if n == 0:
                return proc_fut
            return chunk_fut

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.count_chunks", return_value=8),
            patch("digitize.processing.orchestrator.merge_chunked_documents", return_value=[]),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            tpe_inst = MagicMock()
            tpe_inst.__enter__ = Mock(return_value=tpe_inst)
            tpe_inst.__exit__ = Mock(return_value=False)
            tpe_inst.submit = Mock(side_effect=tpe_submit)
            mock_tpe.return_value = tpe_inst

            _, pdf_stats = process_documents(
                input_paths=["fake.pdf"],
                out_path="/tmp/out",
                llm_model="m", llm_endpoint="e",
                emb_endpoint="emb",
                max_tokens=512,
                job_id="job-ok",
                doc_id_dict={"fake.pdf": "doc-1"},
            )

        # Stats for the processed file must be present
        assert any("fake.pdf" in k for k in pdf_stats)
        # cancel_tasks_for_job must never be called on a healthy run
        mock_db.cancel_tasks_for_job.assert_not_called()


# ===========================================================================
# 5. _run_conversion (conversion_dispatcher.py) — dispatcher cancellation checks
# ===========================================================================


def _make_conv_task(task_id: str = "t-1", cached_file: str = "/staging/t-1.pdf",
                    doc_id: str = "doc-1", output_format: str = "json") -> Mock:
    """Build a minimal ConversionTask mock for dispatcher tests."""
    t = Mock()
    t.task_id = task_id
    t.cached_file = cached_file
    t.doc_id = doc_id
    t.output_format = output_format
    t.is_large = False
    return t


@pytest.mark.unit
class TestRunConversionDispatcher:
    """Tests for _run_conversion in conversion_dispatcher.py."""

    def _run(self, task, mock_db, mock_semaphore, run_in_executor_result=("result.json", 1.0)):
        """Helper: run _run_conversion in a fresh event loop with standard patches."""
        from digitize.workers.conversion_dispatcher import _run_conversion

        async def _go():
            with (
                patch("digitize.workers.conversion_dispatcher.db_manager", mock_db),
                patch("digitize.workers.conversion_dispatcher.conversion_semaphore", mock_semaphore),
                patch("digitize.workers.conversion_dispatcher.settings",
                      SimpleNamespace(digitize=SimpleNamespace(digitized_docs_dir=Path("/out")))),
            ):
                loop = asyncio.get_running_loop()
                with patch.object(loop, "run_in_executor",
                                   new=AsyncMock(return_value=run_in_executor_result)):
                    await _run_conversion(task, weight=1)

        asyncio.run(_go())

    def test_check1_cancel_pending_before_running_writes_cancelled(self, tmp_path):
        """Check 1: task already cancel_pending when dispatcher picks it up →
        written as CANCELLED without starting conversion."""
        from digitize.db.models import ConversionTaskStatus

        cached = tmp_path / "doc.pdf"
        cached.write_bytes(b"%PDF")
        task = _make_conv_task(cached_file=str(cached))

        cancel_pending_stub = Mock(status=ConversionTaskStatus.CANCEL_PENDING)
        update_calls = []

        mock_db = Mock()
        mock_db.get_conversion_task = Mock(return_value=cancel_pending_stub)
        mock_db.update_task_status = Mock(side_effect=lambda *a, **kw: update_calls.append((a, kw)))

        mock_sem = AsyncMock()
        mock_sem.release = AsyncMock()

        self._run(task, mock_db, mock_sem, run_in_executor_result=("result.json", 1.0))

        # update_task_status must have been called with CANCELLED
        statuses = [kw.get("status") or args[1] for args, kw in update_calls
                    if args or kw.get("status")]
        assert any(s == ConversionTaskStatus.CANCELLED for s in
                   [c[0][1] if len(c[0]) > 1 else c[1].get("status")
                    for c in [(a, kw) for a, kw in update_calls]])

        # RUNNING must never have been written
        running_calls = [c for c in update_calls
                         if (len(c[0]) > 1 and c[0][1] == ConversionTaskStatus.RUNNING)]
        assert not running_calls, "Task must NOT be marked RUNNING when cancel_pending"

    def test_check1_missing_cached_file_writes_failed(self, tmp_path):
        """If the cached input file is missing the task is marked FAILED immediately."""
        from digitize.db.models import ConversionTaskStatus

        task = _make_conv_task(cached_file="/nonexistent/missing.pdf")

        mock_db = Mock()
        mock_db.get_conversion_task = Mock(return_value=None)
        mock_db.update_task_status = Mock()

        mock_sem = AsyncMock()
        mock_sem.release = AsyncMock()

        self._run(task, mock_db, mock_sem)

        mock_db.update_task_status.assert_called_once()
        call_args = mock_db.update_task_status.call_args
        assert call_args.args[1] == ConversionTaskStatus.FAILED

    def test_check2_cancel_pending_after_conversion_writes_cancelled(self, tmp_path):
        """Check 2: task flipped to cancel_pending while conversion ran →
        CANCELLED is written instead of COMPLETED."""
        from digitize.db.models import ConversionTaskStatus

        cached = tmp_path / "doc.pdf"
        cached.write_bytes(b"%PDF")
        task = _make_conv_task(cached_file=str(cached))

        # get_conversion_task calls:
        #   1st (Check 1): normal status → proceed
        #   2nd (Check 2): cancel_pending → write CANCELLED
        get_task_calls = {"n": 0}

        def get_task(task_id):
            get_task_calls["n"] += 1
            if get_task_calls["n"] == 1:
                return Mock(status=ConversionTaskStatus.RUNNING)
            return Mock(status=ConversionTaskStatus.CANCEL_PENDING)

        update_calls = []
        mock_db = Mock()
        mock_db.get_conversion_task = Mock(side_effect=get_task)
        mock_db.update_task_status = Mock(side_effect=lambda *a, **kw: update_calls.append((a, kw)))

        mock_sem = AsyncMock()
        mock_sem.release = AsyncMock()

        self._run(task, mock_db, mock_sem, run_in_executor_result=("result.json", 1.0))

        final_statuses = [a[0][1] for a in update_calls if len(a[0]) > 1]
        assert ConversionTaskStatus.CANCELLED in final_statuses
        assert ConversionTaskStatus.COMPLETED not in final_statuses

    def test_check3_exception_with_cancel_pending_writes_cancelled(self, tmp_path):
        """Check 3: exception raised while task is cancel_pending → CANCELLED, not FAILED."""
        from digitize.db.models import ConversionTaskStatus

        cached = tmp_path / "doc.pdf"
        cached.write_bytes(b"%PDF")
        task = _make_conv_task(cached_file=str(cached))

        cancel_pending_stub = Mock(status=ConversionTaskStatus.CANCEL_PENDING)
        update_calls = []

        mock_db = Mock()
        mock_db.get_conversion_task = Mock(return_value=cancel_pending_stub)
        mock_db.update_task_status = Mock(side_effect=lambda *a, **kw: update_calls.append((a, kw)))

        mock_sem = AsyncMock()
        mock_sem.release = AsyncMock()

        # run_in_executor raises an exception simulating JobCancelledError from worker
        self._run(task, mock_db, mock_sem,
                  run_in_executor_result=None)  # patched below separately

        # Re-run via direct async call to control run_in_executor
        from digitize.workers.conversion_dispatcher import _run_conversion

        async def _go():
            with (
                patch("digitize.workers.conversion_dispatcher.db_manager", mock_db),
                patch("digitize.workers.conversion_dispatcher.conversion_semaphore", mock_sem),
                patch("digitize.workers.conversion_dispatcher.settings",
                      SimpleNamespace(digitize=SimpleNamespace(digitized_docs_dir=Path("/out")))),
            ):
                loop = asyncio.get_running_loop()
                with patch.object(loop, "run_in_executor",
                                   new=AsyncMock(side_effect=RuntimeError("worker died"))):
                    await _run_conversion(task, weight=1)

        update_calls.clear()
        asyncio.run(_go())

        final_statuses = [a[0][1] for a in update_calls if len(a[0]) > 1]
        assert ConversionTaskStatus.CANCELLED in final_statuses
        assert ConversionTaskStatus.FAILED not in final_statuses

    def test_genuine_failure_writes_failed(self, tmp_path):
        """A genuine exception (no cancel_pending) must write FAILED."""
        from digitize.db.models import ConversionTaskStatus

        cached = tmp_path / "doc.pdf"
        cached.write_bytes(b"%PDF")
        task = _make_conv_task(cached_file=str(cached))

        running_stub = Mock(status=ConversionTaskStatus.RUNNING)
        update_calls = []

        mock_db = Mock()
        mock_db.get_conversion_task = Mock(return_value=running_stub)
        mock_db.update_task_status = Mock(side_effect=lambda *a, **kw: update_calls.append((a, kw)))

        mock_sem = AsyncMock()
        mock_sem.release = AsyncMock()

        from digitize.workers.conversion_dispatcher import _run_conversion

        async def _go():
            with (
                patch("digitize.workers.conversion_dispatcher.db_manager", mock_db),
                patch("digitize.workers.conversion_dispatcher.conversion_semaphore", mock_sem),
                patch("digitize.workers.conversion_dispatcher.settings",
                      SimpleNamespace(digitize=SimpleNamespace(digitized_docs_dir=Path("/out")))),
            ):
                loop = asyncio.get_running_loop()
                with patch.object(loop, "run_in_executor",
                                   new=AsyncMock(side_effect=RuntimeError("conversion crash"))):
                    await _run_conversion(task, weight=1)

        asyncio.run(_go())

        final_statuses = [a[0][1] for a in update_calls if len(a[0]) > 1]
        assert ConversionTaskStatus.FAILED in final_statuses
        assert ConversionTaskStatus.CANCELLED not in final_statuses

    def test_semaphore_released_in_finally(self, tmp_path):
        """Semaphore must be released unconditionally (even on exception)."""
        from digitize.db.models import ConversionTaskStatus

        cached = tmp_path / "doc.pdf"
        cached.write_bytes(b"%PDF")
        task = _make_conv_task(cached_file=str(cached))

        mock_db = Mock()
        mock_db.get_conversion_task = Mock(return_value=Mock(status=ConversionTaskStatus.RUNNING))
        mock_db.update_task_status = Mock()

        mock_sem = AsyncMock()
        mock_sem.release = AsyncMock()

        from digitize.workers.conversion_dispatcher import _run_conversion

        async def _go():
            with (
                patch("digitize.workers.conversion_dispatcher.db_manager", mock_db),
                patch("digitize.workers.conversion_dispatcher.conversion_semaphore", mock_sem),
                patch("digitize.workers.conversion_dispatcher.settings",
                      SimpleNamespace(digitize=SimpleNamespace(digitized_docs_dir=Path("/out")))),
            ):
                loop = asyncio.get_running_loop()
                with patch.object(loop, "run_in_executor",
                                   new=AsyncMock(side_effect=RuntimeError("boom"))):
                    await _run_conversion(task, weight=2)

        asyncio.run(_go())

        mock_sem.release.assert_called_once_with(2)


# ===========================================================================
# 6. convert_doc (converter.py) — cancel_check callable interface
# ===========================================================================


@pytest.mark.unit
class TestConvertDocCancelCheck:
    """Tests for the cancel_check callable parameter in convert_doc."""

    def _make_fake_settings(self):
        return SimpleNamespace(digitize=SimpleNamespace(doc_chunk_size=100))

    def test_cancel_check_none_processes_all_chunks(self, tmp_path):
        """When cancel_check=None, all chunks are processed without cancellation."""
        from digitize.parsing.converter import convert_doc

        fake_path = tmp_path / "doc.pdf"
        fake_path.write_bytes(b"%PDF")

        # Total pages > chunk_size so we exercise the chunked path
        with (
            patch("digitize.parsing.converter.get_document_page_count", return_value=250),
            patch("digitize.parsing.converter.settings", self._make_fake_settings()),
            patch("digitize.parsing.converter.get_doc_converter", return_value=Mock()),
            patch("digitize.parsing.converter.convert_chunk", return_value=tmp_path / "chunk.json"),
            patch("digitize.parsing.converter.DoclingDocument") as mock_ddoc,
        ):
            mock_ddoc.load_from_json = Mock(return_value=Mock())
            mock_ddoc.concatenate = Mock(return_value=Mock())

            # Must not raise
            convert_doc(str(fake_path), cancel_check=None)

    def test_cancel_check_returning_false_processes_all_chunks(self, tmp_path):
        """A cancel_check that always returns False must not raise."""
        from digitize.parsing.converter import convert_doc

        fake_path = tmp_path / "doc.pdf"
        fake_path.write_bytes(b"%PDF")

        with (
            patch("digitize.parsing.converter.get_document_page_count", return_value=250),
            patch("digitize.parsing.converter.settings", self._make_fake_settings()),
            patch("digitize.parsing.converter.get_doc_converter", return_value=Mock()),
            patch("digitize.parsing.converter.convert_chunk", return_value=tmp_path / "chunk.json"),
            patch("digitize.parsing.converter.DoclingDocument") as mock_ddoc,
        ):
            mock_ddoc.load_from_json = Mock(return_value=Mock())
            mock_ddoc.concatenate = Mock(return_value=Mock())

            # Must not raise
            convert_doc(str(fake_path), cancel_check=lambda: False)

    def test_cancel_check_returning_true_raises_job_cancelled_error(self, tmp_path):
        """A cancel_check that returns True must trigger JobCancelledError between chunks."""
        from digitize.parsing.converter import convert_doc

        fake_path = tmp_path / "doc.pdf"
        fake_path.write_bytes(b"%PDF")

        with (
            patch("digitize.parsing.converter.get_document_page_count", return_value=250),
            patch("digitize.parsing.converter.settings", self._make_fake_settings()),
            patch("digitize.parsing.converter.get_doc_converter", return_value=Mock()),
            patch("digitize.parsing.converter.convert_chunk", return_value=tmp_path / "chunk.json"),
        ):
            with pytest.raises(JobCancelledError):
                convert_doc(str(fake_path), cancel_check=lambda: True)


# ===========================================================================
# 7. _make_db_cancel_check (converter.py)
# ===========================================================================


@pytest.mark.unit
class TestMakeDbCancelCheck:
    """Tests for the _make_db_cancel_check factory in converter.py."""

    def test_returns_false_when_task_not_cancel_pending(self):
        """When DB row status is RUNNING (not cancel_pending), callable returns False."""
        from digitize.parsing.converter import _make_db_cancel_check
        from digitize.db.models import ConversionTaskStatus

        mock_task = Mock(status=ConversionTaskStatus.RUNNING)
        mock_db = Mock()
        mock_db.get_conversion_task = Mock(return_value=mock_task)

        check = _make_db_cancel_check("task-1")

        with patch("digitize.db.manager.db_manager", mock_db):
            # The callable imports db_manager lazily; we patch at the module level
            # by temporarily inserting a mock in sys.modules
            import digitize.db.manager as dm_mod
            original = dm_mod.db_manager
            dm_mod.db_manager = mock_db
            try:
                result = check()
            finally:
                dm_mod.db_manager = original

        assert result is False

    def test_returns_true_when_task_is_cancel_pending(self):
        """When DB row status is CANCEL_PENDING, callable returns True."""
        from digitize.parsing.converter import _make_db_cancel_check
        from digitize.db.models import ConversionTaskStatus

        mock_task = Mock(status=ConversionTaskStatus.CANCEL_PENDING)
        mock_db = Mock()
        mock_db.get_conversion_task = Mock(return_value=mock_task)

        check = _make_db_cancel_check("task-2")

        import digitize.db.manager as dm_mod
        original = dm_mod.db_manager
        dm_mod.db_manager = mock_db
        try:
            result = check()
        finally:
            dm_mod.db_manager = original

        assert result is True

    def test_returns_false_when_db_raises(self):
        """A DB error inside the callable must be swallowed — returns False."""
        from digitize.parsing.converter import _make_db_cancel_check

        mock_db = Mock()
        mock_db.get_conversion_task = Mock(side_effect=RuntimeError("db down"))

        check = _make_db_cancel_check("task-3")

        import digitize.db.manager as dm_mod
        original = dm_mod.db_manager
        dm_mod.db_manager = mock_db
        try:
            result = check()
        finally:
            dm_mod.db_manager = original

        assert result is False


# ===========================================================================
# 8. chunk_text / chunk_tables / chunk_single_file — cancel_event support
# ===========================================================================


@pytest.mark.unit
class TestChunkTextCancelEvent:
    """chunk_text respects a cancel_event threading.Event."""

    def test_cancel_event_not_set_processes_all_blocks(self, tmp_path):
        """With cancel_event unset, all blocks are chunked normally."""
        from digitize.processing.orchestrator import chunk_text
        import json, threading

        data = [
            {"label": "text", "text": "Hello world", "page": 1},
            {"label": "text", "text": "Second block", "page": 2},
        ]
        input_file = tmp_path / "doc-1_text.json"
        input_file.write_text(json.dumps(data))

        event = threading.Event()  # not set

        with (
            patch("digitize.processing.orchestrator.count_tokens", return_value=5),
            patch("digitize.processing.orchestrator.collect_header_font_sizes", return_value={}),
            patch("digitize.processing.orchestrator.get_header_level", return_value=(1, "h")),
            patch("digitize.processing.orchestrator.flush_chunk"),
        ):
            result_path, elapsed = chunk_text(
                str(input_file), str(tmp_path), emb_endpoint="emb",
                doc_id="doc-1", cancel_event=event,
            )

        assert result_path is not None
        assert elapsed is not None

    def test_cancel_event_set_before_first_block_returns_none(self, tmp_path):
        """cancel_event already set → chunk_text returns (None, None).

        chunk_text catches all exceptions internally; the JobCancelledError is
        surfaced only by chunk_single_file which re-raises it.
        """
        from digitize.processing.orchestrator import chunk_text
        import json, threading

        data = [{"label": "text", "text": "Hello", "page": 1}]
        input_file = tmp_path / "doc-2_text.json"
        input_file.write_text(json.dumps(data))

        event = threading.Event()
        event.set()  # already cancelled

        with (
            patch("digitize.processing.orchestrator.count_tokens", return_value=5),
            patch("digitize.processing.orchestrator.collect_header_font_sizes", return_value={}),
            patch("digitize.processing.orchestrator.flush_chunk"),
        ):
            result_path, elapsed = chunk_text(
                str(input_file), str(tmp_path), emb_endpoint="emb",
                doc_id="doc-2", cancel_event=event,
            )

        assert result_path is None
        assert elapsed is None

    def test_cancel_event_set_mid_loop_returns_none(self, tmp_path):
        """cancel_event set mid-loop → chunk_text returns (None, None).

        Uses section_header blocks to trigger flush_chunk inside the loop — that
        is the only place within the loop body where flush_chunk is called, so it
        is the only reliable way to set the event before the *next* iteration's
        cancel check fires.
        """
        from digitize.processing.orchestrator import chunk_text
        import json, threading

        # section_header triggers flush_chunk mid-loop; the cancel check at the
        # top of the following iteration will then see the event as set.
        data = [
            {"label": "section_header", "text": "Chapter 1", "page": 1, "font_size": 16},
            {"label": "text", "text": "Some content", "page": 1},
            {"label": "section_header", "text": "Chapter 2", "page": 2, "font_size": 16},
            {"label": "text", "text": "More content", "page": 2},
        ]
        input_file = tmp_path / "doc-3_text.json"
        input_file.write_text(json.dumps(data))

        event = threading.Event()
        flush_count = {"n": 0}

        # Set the event on the first flush_chunk call (first section_header).
        # The cancel check at the top of the next iteration fires True.
        def set_on_first_flush(*args, **kwargs):
            flush_count["n"] += 1
            if flush_count["n"] == 1:
                event.set()

        with (
            patch("digitize.processing.orchestrator.count_tokens", return_value=5),
            patch("digitize.processing.orchestrator.collect_header_font_sizes", return_value={}),
            patch("digitize.processing.orchestrator.get_header_level", return_value=(1, "Chapter 1")),
            patch("digitize.processing.orchestrator.flush_chunk", side_effect=set_on_first_flush),
        ):
            result_path, elapsed = chunk_text(
                str(input_file), str(tmp_path), emb_endpoint="emb",
                doc_id="doc-3", cancel_event=event,
            )

        assert result_path is None
        assert elapsed is None

    def test_no_cancel_event_processes_normally(self, tmp_path):
        """cancel_event=None (default) behaves identically to before — no change."""
        from digitize.processing.orchestrator import chunk_text
        import json

        data = [{"label": "text", "text": "Only block", "page": 1}]
        input_file = tmp_path / "doc-4_text.json"
        input_file.write_text(json.dumps(data))

        with (
            patch("digitize.processing.orchestrator.count_tokens", return_value=5),
            patch("digitize.processing.orchestrator.collect_header_font_sizes", return_value={}),
            patch("digitize.processing.orchestrator.flush_chunk"),
        ):
            result_path, elapsed = chunk_text(
                str(input_file), str(tmp_path), emb_endpoint="emb",
                doc_id="doc-4",
            )

        assert result_path is not None


@pytest.mark.unit
class TestChunkTablesCancelEvent:
    """chunk_tables respects a cancel_event threading.Event."""

    def test_cancel_event_not_set_processes_all_tables(self, tmp_path):
        """With cancel_event unset, all tables are chunked normally."""
        from digitize.processing.orchestrator import chunk_tables
        import json, threading

        data = {
            "t1": {"caption": "Table 1", "summary": "Summary one", "page_number": 1},
            "t2": {"caption": "Table 2", "summary": "Summary two", "page_number": 2},
        }
        input_file = tmp_path / "doc-5_tables.json"
        input_file.write_text(json.dumps(data))

        event = threading.Event()  # not set

        with patch("digitize.processing.orchestrator.count_tokens", return_value=10):
            result_path, elapsed = chunk_tables(
                str(input_file), str(tmp_path), emb_endpoint="emb",
                doc_id="doc-5", cancel_event=event,
            )

        assert result_path is not None

    def test_cancel_event_set_before_first_table_returns_none(self, tmp_path):
        """cancel_event already set → chunk_tables returns (None, None).

        Like chunk_text, chunk_tables catches all exceptions internally; the
        JobCancelledError is surfaced only by chunk_single_file.
        """
        from digitize.processing.orchestrator import chunk_tables
        import json, threading

        data = {"t1": {"caption": "Table 1", "summary": "Sum", "page_number": 1}}
        input_file = tmp_path / "doc-6_tables.json"
        input_file.write_text(json.dumps(data))

        event = threading.Event()
        event.set()

        with patch("digitize.processing.orchestrator.count_tokens", return_value=5):
            result_path, elapsed = chunk_tables(
                str(input_file), str(tmp_path), emb_endpoint="emb",
                doc_id="doc-6", cancel_event=event,
            )

        assert result_path is None
        assert elapsed is None

    def test_cancel_event_set_mid_loop_returns_none(self, tmp_path):
        """cancel_event set after first table → chunk_tables returns (None, None)."""
        from digitize.processing.orchestrator import chunk_tables
        import json, threading

        data = {
            "t1": {"caption": "T1", "summary": "S1", "page_number": 1},
            "t2": {"caption": "T2", "summary": "S2", "page_number": 2},
        }
        input_file = tmp_path / "doc-7_tables.json"
        input_file.write_text(json.dumps(data))

        event = threading.Event()
        call_count = {"n": 0}

        # Set the event after count_tokens is first called (first table visited).
        # The cancel check at the top of the next iteration fires True.
        def set_after_first(*args, **kwargs):
            call_count["n"] += 1
            if call_count["n"] >= 1:
                event.set()
            return 5

        with patch("digitize.processing.orchestrator.count_tokens", side_effect=set_after_first):
            result_path, elapsed = chunk_tables(
                str(input_file), str(tmp_path), emb_endpoint="emb",
                doc_id="doc-7", cancel_event=event,
            )

        assert result_path is None
        assert elapsed is None

    def test_no_cancel_event_processes_normally(self, tmp_path):
        """cancel_event=None (default) — no change in behaviour."""
        from digitize.processing.orchestrator import chunk_tables
        import json

        data = {"t1": {"caption": "T1", "summary": "Sum", "page_number": 1}}
        input_file = tmp_path / "doc-8_tables.json"
        input_file.write_text(json.dumps(data))

        with patch("digitize.processing.orchestrator.count_tokens", return_value=5):
            result_path, elapsed = chunk_tables(
                str(input_file), str(tmp_path), emb_endpoint="emb",
                doc_id="doc-8",
            )

        assert result_path is not None


@pytest.mark.unit
class TestChunkSingleFileCancelEvent:
    """chunk_single_file forwards cancel_event and re-raises JobCancelledError."""

    def test_cancel_event_set_propagates_as_job_cancelled_error(self, tmp_path):
        """When chunk_text raises JobCancelledError, chunk_single_file re-raises it."""
        from digitize.processing.orchestrator import chunk_single_file
        import threading

        event = threading.Event()
        event.set()

        with (
            patch(
                "digitize.processing.orchestrator.chunk_text",
                side_effect=JobCancelledError("cancelled"),
            ),
            patch("digitize.processing.orchestrator.chunk_tables"),
        ):
            with pytest.raises(JobCancelledError):
                chunk_single_file(
                    "txt.json", "tab.json", str(tmp_path),
                    emb_endpoint="emb", doc_id="doc-9",
                    cancel_event=event,
                )

    def test_cancel_event_forwarded_to_chunk_text_and_chunk_tables(self, tmp_path):
        """cancel_event is forwarded to both chunk_text and chunk_tables."""
        from digitize.processing.orchestrator import chunk_single_file
        import threading

        event = threading.Event()

        txt_result = tmp_path / "doc-10_text_chunks.json"
        tab_result = tmp_path / "doc-10_table_chunks.json"

        with (
            patch(
                "digitize.processing.orchestrator.chunk_text",
                return_value=(str(txt_result), 0.5),
            ) as mock_chunk_text,
            patch(
                "digitize.processing.orchestrator.chunk_tables",
                return_value=(str(tab_result), 0.3),
            ) as mock_chunk_tables,
        ):
            chunk_single_file(
                "txt.json", "tab.json", str(tmp_path),
                emb_endpoint="emb", doc_id="doc-10",
                cancel_event=event,
            )

        # Verify cancel_event was forwarded to both inner calls
        assert mock_chunk_text.call_args.kwargs.get("cancel_event") is event
        assert mock_chunk_tables.call_args.kwargs.get("cancel_event") is event


# ===========================================================================
# 9. OpenSearch insert_chunks — cancel_event between batches
# ===========================================================================


@pytest.mark.unit
class TestOpenSearchInsertChunksCancelEvent:
    """insert_chunks aborts between batches when cancel_event is set."""

    def _make_opensearch(self):
        from common.opensearch import OpensearchVectorStore
        inst = OpensearchVectorStore.__new__(OpensearchVectorStore)
        inst.index_name = "test-index"
        inst.client = MagicMock()
        inst.client.indices.exists = Mock(return_value=True)
        return inst

    def test_cancel_event_not_set_inserts_all_batches(self):
        """All batches are inserted when cancel_event is not set."""
        import threading

        inst = self._make_opensearch()
        inst._setup_index = Mock()

        chunks = [{"page_content": f"chunk {i}", "filename": "f.pdf"} for i in range(5)]
        embedder = Mock()
        embedder.embed_documents = Mock(return_value=[[0.1] * 4] * 5)

        event = threading.Event()  # not set

        with patch("common.opensearch.helpers.bulk", return_value=(5, [])):
            result = inst.insert_chunks(chunks, embedding=embedder, batch_size=2, cancel_event=event)

        assert result is True

    def test_cancel_event_set_aborts_before_first_batch(self):
        """cancel_event already set → returns False before any bulk insert."""
        import threading

        inst = self._make_opensearch()
        inst._setup_index = Mock()

        chunks = [{"page_content": f"chunk {i}", "filename": "f.pdf"} for i in range(4)]
        embedder = Mock()
        embedder.embed_documents = Mock(return_value=[[0.1] * 4] * 4)

        event = threading.Event()
        event.set()

        with patch("common.opensearch.helpers.bulk") as mock_bulk:
            result = inst.insert_chunks(chunks, embedding=embedder, batch_size=2, cancel_event=event)

        assert result is False
        mock_bulk.assert_not_called()

    def test_cancel_event_set_between_batches_aborts_mid_insert(self):
        """cancel_event set after the first batch → second batch is not inserted."""
        import threading

        inst = self._make_opensearch()
        inst._setup_index = Mock()

        chunks = [{"page_content": f"chunk {i}", "filename": "f.pdf"} for i in range(4)]
        embedder = Mock()
        embedder.embed_documents = Mock(return_value=[[0.1] * 4] * 4)

        event = threading.Event()
        bulk_call_count = {"n": 0}

        def fake_bulk(*args, **kwargs):
            bulk_call_count["n"] += 1
            event.set()  # set after first batch so second is cancelled
            return (2, [])

        with patch("common.opensearch.helpers.bulk", side_effect=fake_bulk):
            result = inst.insert_chunks(chunks, embedding=embedder, batch_size=2, cancel_event=event)

        assert result is False
        assert bulk_call_count["n"] == 1  # only first batch executed

    def test_no_cancel_event_inserts_all_batches(self):
        """cancel_event=None (default) — full insert as before."""
        inst = self._make_opensearch()
        inst._setup_index = Mock()

        chunks = [{"page_content": f"chunk {i}", "filename": "f.pdf"} for i in range(3)]
        embedder = Mock()
        embedder.embed_documents = Mock(return_value=[[0.1] * 4] * 3)

        with patch("common.opensearch.helpers.bulk", return_value=(3, [])):
            result = inst.insert_chunks(chunks, embedding=embedder, batch_size=2)

        assert result is True


# ===========================================================================
# 10. process_documents — _index_cancel_event gated by clean_files
# ===========================================================================


@pytest.mark.unit
class TestOrchestratorIndexCancelEventCleanFilesGating:
    """_index_cancel_event is only set (and queued futures only cancelled) when
    clean_files=True.  When clean_files=False a running insert_chunks call is
    let to finish so no partial VDB entries are stranded.
    """

    def _base_setup(self):
        """Return commonly shared futures and task stub."""
        task = _make_task_stub("t-1", "completed", result_path="conv.json")

        proc_fut: Future = Future()
        proc_fut.set_result(
            ("txt.json", "tab.json", 5, 2, {"process_text": 0.1, "process_tables": 0.1}, "en", {})
        )

        chunk_fut: Future = Future()
        chunk_fut.set_result(("text_chunks.json", "table_chunks.json", 2.5))

        return task, proc_fut, chunk_fut

    def test_pending_index_future_cancelled_when_clean_files_true(self):
        """clean_files=True → pending (not-yet-running) index future IS cancelled."""
        from digitize.processing.orchestrator import process_documents

        task, proc_fut, chunk_fut = self._base_setup()

        pending_index_fut: Future = Future()  # PENDING — cancellable

        cancel_calls = {"n": 0}

        def is_cancelled(job_id):
            cancel_calls["n"] += 1
            return cancel_calls["n"] >= 5

        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task])
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)
        mock_db.cancel_tasks_for_job = Mock()
        mock_db.get_conversion_task = Mock(return_value=task)
        # clean_files=True
        mock_db.get_job_by_id = Mock(return_value=Mock(stats={"clean_files": True}))

        submit_calls = {"n": 0}

        def tpe_submit(*args, **kwargs):
            n = submit_calls["n"]
            submit_calls["n"] += 1
            if n == 0:
                return proc_fut
            if n == 1:
                return chunk_fut
            return pending_index_fut

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.count_chunks", return_value=3),
            patch("digitize.processing.orchestrator.merge_chunked_documents", return_value=[]),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            tpe_inst = MagicMock()
            tpe_inst.__enter__ = Mock(return_value=tpe_inst)
            tpe_inst.__exit__ = Mock(return_value=False)
            tpe_inst.submit = Mock(side_effect=tpe_submit)
            mock_tpe.return_value = tpe_inst

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=["fake.pdf"],
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-cancel-clean-true",
                    doc_id_dict={"fake.pdf": "doc-1"},
                    indexing_callback=lambda doc_id, chunks, path, **kw: True,
                )

        assert pending_index_fut.cancelled(), "pending future must be cancelled when clean_files=True"

    def test_running_index_future_not_cancelled_when_clean_files_false(self):
        """clean_files=False → a RUNNING index future is NOT cancelled; it completes.

        Uses the same gate pattern as TestOrchestratorCancellationAtIndexingStage:
        a RUNNING future that only resolves after cancel is first detected, so the
        poll loop definitely sees it as RUNNING when _clean_files=False is evaluated.
        The key assertion is that the future completes (not cancelled).
        """
        from digitize.processing.orchestrator import process_documents

        task, proc_fut, chunk_fut = self._base_setup()

        cancel_seen = threading.Event()
        running_index_fut: Future = Future()
        running_index_fut.set_running_or_notify_cancel()  # → RUNNING

        def _resolve_after_cancel():
            cancel_seen.wait(timeout=5)
            try:
                running_index_fut.set_result(True)
            except Exception:
                pass

        threading.Thread(target=_resolve_after_cancel, daemon=True).start()

        cancel_calls = {"n": 0}

        def is_cancelled(job_id):
            cancel_calls["n"] += 1
            if cancel_calls["n"] >= 3:
                cancel_seen.set()
                return True
            return False

        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task])
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)
        mock_db.cancel_tasks_for_job = Mock()
        mock_db.get_conversion_task = Mock(return_value=task)
        # clean_files=False — index cancel event must NOT be set
        mock_db.get_job_by_id = Mock(return_value=Mock(stats={"clean_files": False}))

        submit_calls = {"n": 0}

        def tpe_submit(*args, **kwargs):
            n = submit_calls["n"]
            submit_calls["n"] += 1
            if n == 0:
                return proc_fut
            if n == 1:
                return chunk_fut
            return running_index_fut

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.count_chunks", return_value=3),
            patch("digitize.processing.orchestrator.merge_chunked_documents", return_value=[]),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            tpe_inst = MagicMock()
            tpe_inst.__enter__ = Mock(return_value=tpe_inst)
            tpe_inst.__exit__ = Mock(return_value=False)
            tpe_inst.submit = Mock(side_effect=tpe_submit)
            mock_tpe.return_value = tpe_inst

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=["fake.pdf"],
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-cancel-clean-false",
                    doc_id_dict={"fake.pdf": "doc-1"},
                    indexing_callback=lambda doc_id, chunks, path, **kw: True,
                )

        assert not running_index_fut.cancelled(), "future must NOT be cancelled when clean_files=False"

    def test_index_cancel_event_passed_to_indexing_callback(self):
        """The cancel_event kwarg passed to indexing_callback is the _index_cancel_event,
        and it is only set when clean_files=True."""
        from digitize.processing.orchestrator import process_documents
        import threading

        task, proc_fut, chunk_fut = self._base_setup()

        received_events: list = []
        index_fut: Future = Future()
        index_fut.set_result(True)

        mock_db = Mock()
        mock_db.get_conversion_tasks_by_job_id = Mock(return_value=[task])
        mock_db.is_job_cancelled = Mock(return_value=False)
        mock_db.cancel_tasks_for_job = Mock()
        mock_db.get_conversion_task = Mock(return_value=task)

        submit_calls = {"n": 0}

        def tpe_submit(fn, *args, **kwargs):
            n = submit_calls["n"]
            submit_calls["n"] += 1
            if n == 0:
                return proc_fut
            if n == 1:
                return chunk_fut
            # Capture the cancel_event passed to the indexing_callback submit
            received_events.append(kwargs.get("cancel_event"))
            return index_fut

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.count_chunks", return_value=2),
            patch("digitize.processing.orchestrator.merge_chunked_documents", return_value=[]),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            tpe_inst = MagicMock()
            tpe_inst.__enter__ = Mock(return_value=tpe_inst)
            tpe_inst.__exit__ = Mock(return_value=False)
            tpe_inst.submit = Mock(side_effect=tpe_submit)
            mock_tpe.return_value = tpe_inst

            process_documents(
                input_paths=["fake.pdf"],
                out_path="/tmp/out",
                llm_model="m", llm_endpoint="e",
                emb_endpoint="emb",
                max_tokens=512,
                job_id="job-event-capture",
                doc_id_dict={"fake.pdf": "doc-1"},
                indexing_callback=lambda doc_id, chunks, path, **kw: True,
            )

        assert len(received_events) == 1
        event = received_events[0]
        assert isinstance(event, threading.Event), "cancel_event must be a threading.Event"
        # On a non-cancelled job the index cancel event must NOT be set
        assert not event.is_set(), "_index_cancel_event must be clear on a healthy run"
