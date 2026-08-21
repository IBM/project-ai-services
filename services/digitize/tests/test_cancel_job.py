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
     - Staging cleanup and semaphore release happen even on cancellation
     - Normal pipeline exception does NOT produce CANCELLED status

  3. ``_run_ingest`` background task (jobs.py)
     - JobCancelledError causes CANCELLED status on non-terminal docs and job
     - clean_files=True → vector-DB chunks are removed for the right doc IDs
     - clean_files=False → vector DB is NOT touched
     - VDB cleanup failure is swallowed (only a warning)
     - Staging cleanup and semaphore release happen even on cancellation

  4. ``process_documents / _run_batch`` (orchestrator.py)
     - Cancellation at conversion stage: pending futures are cancelled, running
       ones are drained; JobCancelledError is raised afterwards
     - Cancellation at processing stage: same pattern
     - Cancellation at chunking stage: same pattern
     - Cancellation at indexing stage: pending index futures cancelled; already
       running index futures are let to complete (no stale VDB entries)
     - Non-cancelled jobs run through all stages without interruption
"""

import asyncio
import sys
import threading
import types
from concurrent.futures import Future
from pathlib import Path
from types import SimpleNamespace
from typing import cast
from unittest.mock import AsyncMock, MagicMock, Mock, call, patch

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
from digitize.workers.concurrency import concurrency_manager


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
    monkeypatch.setattr(concurrency_manager, "is_locked", Mock(return_value=False))
    monkeypatch.setattr(concurrency_manager, "acquire", AsyncMock())
    monkeypatch.setattr(concurrency_manager, "release", Mock())
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
            patch("digitize.api.v1.jobs.concurrency_manager"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_digitize(
                job_id=job_id,
                doc_id_dict={"sample.pdf": "doc-active"},
                output_format=Mock(),
            )

        # Completed doc must NOT be touched
        update_calls = mock_db.update_document.call_args_list
        updated_doc_ids = [c.args[0] for c in update_calls]
        assert "doc-done" not in updated_doc_ids

        # In-progress doc must be marked CANCELLED
        assert "doc-active" in updated_doc_ids
        mock_db.update_job.assert_called_once_with(job_id, status=JobStatus.CANCELLED)

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
            patch("digitize.api.v1.jobs.concurrency_manager"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_digitize(
                job_id=job_id,
                doc_id_dict={},
                output_format=Mock(),
            )

        mock_db.update_document.assert_not_called()

    @pytest.mark.asyncio
    async def test_staging_cleanup_and_semaphore_released_on_cancellation(self, tmp_path):
        """finally block must always run: cleanup + semaphore release."""
        mock_cleanup = Mock()
        mock_concurrency = Mock()

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", Mock(
                get_documents_by_job_id=Mock(return_value=[]),
                update_job=Mock(),
            )),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=Mock()),
            patch("digitize.api.v1.jobs.cleanup_staging_directory", mock_cleanup),
            patch("digitize.api.v1.jobs.concurrency_manager", mock_concurrency),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_digitize("job-x", {}, Mock())

        mock_cleanup.assert_called_once()
        mock_concurrency.release.assert_called_once_with("digitization")

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
            patch("digitize.api.v1.jobs.concurrency_manager"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_digitize("job-fail", {}, Mock())

        mock_db.update_document.assert_not_called()
        mock_status_mgr.update_job_progress.assert_called()


# ===========================================================================
# 3. _run_ingest background task
# ===========================================================================


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

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=Mock()),
            patch("digitize.api.v1.jobs.cleanup_staging_directory"),
            patch("digitize.api.v1.jobs.concurrency_manager"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_ingest(job_id, ["file.pdf"], {"file.pdf": "doc-pending"})

        updated = [c.args[0] for c in mock_db.update_document.call_args_list]
        assert "doc-pending" in updated
        assert "doc-done" not in updated
        mock_db.update_job.assert_called_once_with(job_id, status=JobStatus.CANCELLED)

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
            patch("digitize.api.v1.jobs.concurrency_manager"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
            patch("common.db_utils.get_vector_store", return_value=mock_vector_store),
        ):
            await jobs_module._run_ingest(job_id, list(doc_id_dict.keys()), doc_id_dict)

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
            patch("digitize.api.v1.jobs.concurrency_manager"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
            patch("common.db_utils.get_vector_store", mock_get_vector_store),
        ):
            await jobs_module._run_ingest(job_id, [], {})

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
            patch("digitize.api.v1.jobs.concurrency_manager"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
            patch("common.db_utils.get_vector_store", return_value=exploding_store),
        ):
            # Must not raise
            await jobs_module._run_ingest(job_id, ["x.pdf"], doc_id_dict)

    @pytest.mark.asyncio
    async def test_staging_cleanup_and_semaphore_released_on_cancellation(self, tmp_path):
        mock_cleanup = Mock()
        mock_concurrency = Mock()

        mock_db = Mock()
        mock_db.get_documents_by_job_id = Mock(return_value=[])
        mock_db.update_job = Mock()
        mock_db.get_job_by_id = Mock(return_value=Mock(stats={}))

        with (
            patch("digitize.api.v1.jobs.asyncio.to_thread", new=AsyncMock(side_effect=JobCancelledError("c"))),
            patch("digitize.api.v1.jobs.db_manager", mock_db),
            patch("digitize.api.v1.jobs.get_status_manager", return_value=Mock()),
            patch("digitize.api.v1.jobs.cleanup_staging_directory", mock_cleanup),
            patch("digitize.api.v1.jobs.concurrency_manager", mock_concurrency),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
        ):
            await jobs_module._run_ingest("job-y", [], {})

        mock_cleanup.assert_called_once()
        mock_concurrency.release.assert_called_once_with("ingestion")

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
            patch("digitize.api.v1.jobs.concurrency_manager"),
            patch("digitize.api.v1.jobs.settings", SimpleNamespace(digitize=SimpleNamespace(staging_dir=tmp_path))),
            patch("common.db_utils.get_vector_store", return_value=mock_vs),
        ):
            await jobs_module._run_ingest(job_id, list(doc_id_dict.keys()), doc_id_dict)

        removed = set(mock_vs.remove_docs_from_index.call_args.args[0])
        assert removed == {"doc-mine"}
        assert "doc-foreign" not in removed


# ===========================================================================
# 4. orchestrator._run_batch / process_documents – cancellation checkpoints
# ===========================================================================


def _make_future(result=None, exception=None, *, running=False, done_immediately=True) -> Future:
    """Build a real concurrent.futures.Future in the desired state."""
    f: Future = Future()
    if exception:
        f.set_exception(exception)
    elif done_immediately:
        f.set_result(result)
    # If done_immediately=False and running=False the future stays PENDING.
    return f


def _make_running_future(result=None, delay: float = 0.0) -> Future:
    """Return a Future that is currently RUNNING and will resolve to *result*.

    The poll loops in the orchestrator only exit a future via ``fut.done()``
    or ``fut.cancel()``.  A RUNNING future is neither cancellable nor done
    until its result is set, so we schedule that on a background thread
    (after an optional *delay*) so the poll loop can drain naturally.

    Why this avoids a hang
    ----------------------
    The orchestrator loops:
        while pending:
            if is_cancelled and not fut.running() and not fut.done(): cancel + remove
            elif fut.done():                                           collect + remove
            if pending: time.sleep(0.5)   ← patched to no-op in tests

    With ``time.sleep`` patched to a no-op the loop spins at full speed.
    The background thread sets the result after ``delay`` seconds (default 0),
    which is enough for the scheduler to give the loop at least one iteration
    before the future becomes done, so the ``assert not fut.cancelled()``
    check is meaningful and the loop terminates without hanging.
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
class TestOrchestratorCancellationAtConversionStage:
    """Cancellation observed while conversion futures are still pending/running."""

    def test_pending_conversion_future_is_cancelled_when_job_cancelled(self):
        from digitize.processing.orchestrator import process_documents

        # Pending future (never started)
        pending_fut: Future = Future()

        # is_job_cancelled returns True on first check
        mock_db = Mock()
        mock_db.is_job_cancelled = Mock(return_value=True)

        mock_status_mgr = Mock()

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=mock_status_mgr),
            patch("digitize.processing.orchestrator.ProcessPoolExecutor") as mock_ppe,
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.get_document_page_count", return_value=1),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            # ProcessPoolExecutor.submit → returns our pending future
            mock_conv_ex = MagicMock()
            mock_conv_ex.__enter__ = Mock(return_value=mock_conv_ex)
            mock_conv_ex.__exit__ = Mock(return_value=False)
            mock_conv_ex.submit = Mock(return_value=pending_fut)
            mock_ppe.return_value = mock_conv_ex

            mock_thread_ex = MagicMock()
            mock_thread_ex.__enter__ = Mock(return_value=mock_thread_ex)
            mock_thread_ex.__exit__ = Mock(return_value=False)
            mock_tpe.return_value = mock_thread_ex

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

    def test_running_conversion_future_is_allowed_to_complete(self):
        """A future already running() must NOT have cancel() called on it.

        The RUNNING future resolves itself via a background thread so the
        orchestrator's poll loop exits naturally instead of spinning forever.
        The result is set to (None, 0.0) — a falsy converted_json triggers
        the "conversion returned None" error path, which is fine; we only
        care that cancel() was never called.
        """
        from digitize.processing.orchestrator import process_documents

        # Resolves to (None, 0.0): falsy converted_json → orchestrator logs
        # an error and continues; it does NOT raise, so the test can assert.
        running_fut = _make_running_future(result=(None, 0.0))

        mock_db = Mock()
        mock_db.is_job_cancelled = Mock(return_value=True)

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ProcessPoolExecutor") as mock_ppe,
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.get_document_page_count", return_value=1),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            mock_conv_ex = MagicMock()
            mock_conv_ex.__enter__ = Mock(return_value=mock_conv_ex)
            mock_conv_ex.__exit__ = Mock(return_value=False)
            mock_conv_ex.submit = Mock(return_value=running_fut)
            mock_ppe.return_value = mock_conv_ex

            mock_thread_ex = MagicMock()
            mock_thread_ex.__enter__ = Mock(return_value=mock_thread_ex)
            mock_thread_ex.__exit__ = Mock(return_value=False)
            mock_tpe.return_value = mock_thread_ex

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=["fake.pdf"],
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-conv-running",
                    doc_id_dict={"fake.pdf": "doc-1"},
                )

        # The running future must NOT have been cancelled
        assert not running_fut.cancelled()


@pytest.mark.unit
class TestOrchestratorCancellationAtProcessingStage:
    """Cancellation observed while processing (text/table extraction) futures are pending."""

    def _build_done_conversion_future(self):
        """A completed conversion future returning a dummy (converted_json, time)."""
        f: Future = Future()
        f.set_result(("converted.json", 1.0))
        return f

    def test_pending_processing_future_is_cancelled_when_job_cancelled(self):
        from digitize.processing.orchestrator import process_documents

        conv_fut = self._build_done_conversion_future()
        pending_proc_fut: Future = Future()

        call_count = {"n": 0}

        def is_cancelled(job_id):
            # Return False during conversion stage (calls 1-2), True when processing stage checks (call 3+)
            # Call 1: pre-loop check at line 539
            # Call 2: conversion poll loop iteration — False so pending_proc_fut gets submitted
            # Call 3: processing poll loop — True so pending_proc_fut is cancelled
            call_count["n"] += 1
            return call_count["n"] >= 3

        mock_db = Mock()
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ProcessPoolExecutor") as mock_ppe,
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.get_document_page_count", return_value=1),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            mock_conv_ex = MagicMock()
            mock_conv_ex.__enter__ = Mock(return_value=mock_conv_ex)
            mock_conv_ex.__exit__ = Mock(return_value=False)
            mock_conv_ex.submit = Mock(return_value=conv_fut)
            mock_ppe.return_value = mock_conv_ex

            mock_thread_ex = MagicMock()
            mock_thread_ex.__enter__ = Mock(return_value=mock_thread_ex)
            mock_thread_ex.__exit__ = Mock(return_value=False)
            mock_thread_ex.submit = Mock(return_value=pending_proc_fut)
            mock_tpe.return_value = mock_thread_ex

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

        assert pending_proc_fut.cancelled()


@pytest.mark.unit
class TestOrchestratorCancellationAtChunkingStage:
    """Cancellation observed while chunking futures are still pending."""

    def test_pending_chunking_future_is_cancelled_when_job_cancelled(self):
        from digitize.processing.orchestrator import process_documents

        conv_fut: Future = Future()
        conv_fut.set_result(("conv.json", 1.0))

        proc_fut: Future = Future()
        proc_fut.set_result(("txt.json", "tab.json", 5, 2, {"process_text": 0.1, "process_tables": 0.1}, "en"))

        pending_chunk_fut: Future = Future()

        call_count = {"n": 0}

        def is_cancelled(job_id):
            call_count["n"] += 1
            # Call 1: pre-loop check; Call 2: conversion poll — False so proc future submitted
            # Call 3: processing poll — False so chunk future submitted
            # Call 4: chunking poll — True so pending_chunk_fut is cancelled
            return call_count["n"] >= 4

        mock_db = Mock()
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ProcessPoolExecutor") as mock_ppe,
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.get_document_page_count", return_value=1),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            conv_ex = MagicMock()
            conv_ex.__enter__ = Mock(return_value=conv_ex)
            conv_ex.__exit__ = Mock(return_value=False)
            conv_ex.submit = Mock(return_value=conv_fut)
            mock_ppe.return_value = conv_ex

            submit_calls = {"n": 0}

            def thread_submit(*args, **kwargs):
                submit_calls["n"] += 1
                if submit_calls["n"] == 1:
                    return proc_fut
                return pending_chunk_fut

            thread_ex = MagicMock()
            thread_ex.__enter__ = Mock(return_value=thread_ex)
            thread_ex.__exit__ = Mock(return_value=False)
            thread_ex.submit = Mock(side_effect=thread_submit)
            mock_tpe.return_value = thread_ex

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=["fake.pdf"],
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-cancel-chunk",
                    doc_id_dict={"fake.pdf": "doc-1"},
                )

        assert pending_chunk_fut.cancelled()


@pytest.mark.unit
class TestOrchestratorCancellationAtIndexingStage:
    """Cancellation observed while indexing futures are in-flight."""

    def test_pending_indexing_future_is_cancelled(self):
        """A pending (not yet running) indexing future must be cancelled.

        is_job_cancelled is called once per poll-loop iteration:
          call 1 → conversion stage   (line 550)
          call 2 → processing stage   (line 609)
          call 3 → chunking stage     (line 678)
          call 4 → indexing stage     (line 761)

        Returning False for the first 3 lets the pipeline submit work all the
        way to the indexing executor before we signal cancellation.
        """
        from digitize.processing.orchestrator import process_documents

        conv_fut: Future = Future()
        conv_fut.set_result(("conv.json", 1.0))

        proc_fut: Future = Future()
        proc_fut.set_result(("txt.json", "tab.json", 5, 2, {"process_text": 0.1, "process_tables": 0.1}, "en"))

        chunk_result = ("text_chunks.json", "table_chunks.json", 2.5)
        chunk_fut: Future = Future()
        chunk_fut.set_result(chunk_result)

        pending_index_fut: Future = Future()

        check_count = {"n": 0}

        def is_cancelled(job_id):
            check_count["n"] += 1
            # Call 1: pre-loop check; Call 2: conversion poll — False → proc submitted
            # Call 3: processing poll — False → chunk submitted
            # Call 4: chunking poll — False → index future submitted
            # Call 5: indexing poll — True → pending_index_fut is cancelled
            return check_count["n"] >= 5

        mock_db = Mock()
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ProcessPoolExecutor") as mock_ppe,
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.count_chunks", return_value=5),
            patch("digitize.processing.orchestrator.merge_chunked_documents", return_value=[]),
            patch("digitize.processing.orchestrator.get_document_page_count", return_value=1),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            conv_ex = MagicMock()
            conv_ex.__enter__ = Mock(return_value=conv_ex)
            conv_ex.__exit__ = Mock(return_value=False)
            conv_ex.submit = Mock(return_value=conv_fut)
            mock_ppe.return_value = conv_ex

            submit_calls = {"n": 0}

            def thread_submit(*args, **kwargs):
                n = submit_calls["n"]
                submit_calls["n"] += 1
                if n == 0:
                    return proc_fut
                if n == 1:
                    return chunk_fut
                return pending_index_fut

            thread_ex = MagicMock()
            thread_ex.__enter__ = Mock(return_value=thread_ex)
            thread_ex.__exit__ = Mock(return_value=False)
            thread_ex.submit = Mock(side_effect=thread_submit)
            mock_tpe.return_value = thread_ex

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
        finish so no stale entries are left in the VDB."""
        from digitize.processing.orchestrator import process_documents

        conv_fut: Future = Future()
        conv_fut.set_result(("conv.json", 1.0))

        proc_fut: Future = Future()
        proc_fut.set_result(("txt.json", "tab.json", 5, 2, {"process_text": 0.1, "process_tables": 0.1}, "en"))

        chunk_fut: Future = Future()
        chunk_fut.set_result(("text_chunks.json", "table_chunks.json", 2.5))

        # Resolves to True (success) via a background thread so the poll loop
        # drains naturally instead of spinning forever on a never-done future.
        running_index_fut = _make_running_future(result=True)

        # is_cancelled must be False for the conversion, processing, and
        # chunking poll loops so the pipeline actually reaches the indexing
        # stage.  Only return True starting from the 4th call (indexing loop).
        _cancel_calls = {"n": 0}

        def _is_cancelled(job_id):
            _cancel_calls["n"] += 1
            return _cancel_calls["n"] >= 4

        mock_db = Mock()
        mock_db.is_job_cancelled = Mock(side_effect=_is_cancelled)

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ProcessPoolExecutor") as mock_ppe,
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.count_chunks", return_value=3),
            patch("digitize.processing.orchestrator.merge_chunked_documents", return_value=[]),
            patch("digitize.processing.orchestrator.get_document_page_count", return_value=1),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            conv_ex = MagicMock()
            conv_ex.__enter__ = Mock(return_value=conv_ex)
            conv_ex.__exit__ = Mock(return_value=False)
            conv_ex.submit = Mock(return_value=conv_fut)
            mock_ppe.return_value = conv_ex

            submit_calls = {"n": 0}

            def thread_submit(*args, **kwargs):
                n = submit_calls["n"]
                submit_calls["n"] += 1
                if n == 0:
                    return proc_fut
                if n == 1:
                    return chunk_fut
                return running_index_fut

            thread_ex = MagicMock()
            thread_ex.__enter__ = Mock(return_value=thread_ex)
            thread_ex.__exit__ = Mock(return_value=False)
            thread_ex.submit = Mock(side_effect=thread_submit)
            mock_tpe.return_value = thread_ex

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

        conv_fut: Future = Future()
        conv_fut.set_result(("conv.json", 1.5))

        proc_fut: Future = Future()
        proc_fut.set_result(("txt.json", "tab.json", 10, 3, {"process_text": 0.5, "process_tables": 0.3}, "en"))

        chunk_fut: Future = Future()
        chunk_fut.set_result(("text_chunks.json", "table_chunks.json", 1.2))

        mock_db = Mock()
        mock_db.is_job_cancelled = Mock(return_value=False)

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ProcessPoolExecutor") as mock_ppe,
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.count_chunks", return_value=8),
            patch("digitize.processing.orchestrator.merge_chunked_documents", return_value=[]),
            patch("digitize.processing.orchestrator.get_document_page_count", return_value=1),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            conv_ex = MagicMock()
            conv_ex.__enter__ = Mock(return_value=conv_ex)
            conv_ex.__exit__ = Mock(return_value=False)
            conv_ex.submit = Mock(return_value=conv_fut)
            mock_ppe.return_value = conv_ex

            submit_calls = {"n": 0}

            def thread_submit(*args, **kwargs):
                n = submit_calls["n"]
                submit_calls["n"] += 1
                if n == 0:
                    return proc_fut
                return chunk_fut

            thread_ex = MagicMock()
            thread_ex.__enter__ = Mock(return_value=thread_ex)
            thread_ex.__exit__ = Mock(return_value=False)
            thread_ex.submit = Mock(side_effect=thread_submit)
            mock_tpe.return_value = thread_ex

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


@pytest.mark.unit
class TestOrchestratorCancellationWith10Files:
    """Regression test for the 10-file race where cancellation is observed only
    after some conversion futures have already completed and their downstream
    process_futures have been submitted.

    Scenario
    --------
    10 files are submitted.  The ProcessPoolExecutor has 4 workers, so the
    first 4 conversions complete in loop-iteration-1 (is_cancelled=False) and
    their process_futures are queued.  Loop-iteration-2 observes
    is_cancelled=True: the remaining conversion futures are pending and get
    cancelled, but the 4 already-queued process_futures must *also* be
    cancelled before JobCancelledError is raised — otherwise they run to
    completion inside the executor's shutdown(wait=True).
    """

    def test_queued_process_futures_are_cancelled_when_cancel_arrives_mid_batch(self):
        """process_futures submitted before cancel was observed must be cancelled."""
        from digitize.processing.orchestrator import process_documents

        n_files = 10
        n_early = 4  # conversions that complete *before* cancel is observed

        file_names = [f"file{i}.pdf" for i in range(n_files)]
        doc_id_dict = {fn: f"doc-{i}" for i, fn in enumerate(file_names)}

        # Build conversion futures:
        #   first n_early are already done (simulate completed before cancel)
        #   rest are pending (not started)
        early_conv_futs = []
        for _ in range(n_early):
            f: Future = Future()
            f.set_result(("conv.json", 1.0))
            early_conv_futs.append(f)

        late_conv_futs = []
        for _ in range(n_files - n_early):
            late_conv_futs.append(Future())  # pending, never resolved

        all_conv_futs = early_conv_futs + late_conv_futs

        # process_futures returned when processor_executor.submit() is called
        # (only for the n_early docs whose conversion finished before cancel).
        # These start as pending — they must be cancelled by the fix.
        process_futs = []
        for _ in range(n_early):
            process_futs.append(Future())  # pending

        submit_idx = {"n": 0}

        def conv_submit(*args, **kwargs):
            idx = submit_idx["n"]
            submit_idx["n"] += 1
            return all_conv_futs[idx]

        # is_job_cancelled: False on first check (conversion loop iter 1),
        # True from the second check onward (iter 2 when cancel arrives).
        cancel_call = {"n": 0}

        def is_cancelled(job_id):
            cancel_call["n"] += 1
            # Call 1: pre-loop check — False
            # Call 2: conversion poll iteration 1 — False so early conv futures submit process_futs
            # Call 3: conversion poll iteration 2 — True so late conv futures are cancelled
            #         and process_futures (already submitted) are then cancelled before raising
            return cancel_call["n"] >= 3

        mock_db = Mock()
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)

        proc_submit_idx = {"n": 0}

        def proc_submit(*args, **kwargs):
            idx = proc_submit_idx["n"]
            proc_submit_idx["n"] += 1
            return process_futs[idx]

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ProcessPoolExecutor") as mock_ppe,
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.get_document_page_count", return_value=1),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            conv_ex = MagicMock()
            conv_ex.__enter__ = Mock(return_value=conv_ex)
            conv_ex.__exit__ = Mock(return_value=False)
            conv_ex.submit = Mock(side_effect=conv_submit)
            mock_ppe.return_value = conv_ex

            proc_ex = MagicMock()
            proc_ex.__enter__ = Mock(return_value=proc_ex)
            proc_ex.__exit__ = Mock(return_value=False)
            proc_ex.submit = Mock(side_effect=proc_submit)
            mock_tpe.return_value = proc_ex

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=file_names,
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-10-files",
                    doc_id_dict=doc_id_dict,
                )

        # Every process_future that was submitted before cancel was observed
        # must have been cancelled, not left to run.
        for i, pf in enumerate(process_futs):
            assert pf.cancelled(), (
                f"process_future[{i}] was NOT cancelled — "
                "the fix to cancel queued downstream futures at the "
                "conversion→process boundary is missing or incomplete"
            )

    def test_queued_chunk_futures_are_cancelled_when_cancel_arrives_mid_processing(self):
        """chunk_futures submitted before cancel was observed must be cancelled."""
        from digitize.processing.orchestrator import process_documents

        n_files = 10
        n_early = 4  # files whose processing completes before cancel is observed

        file_names = [f"file{i}.pdf" for i in range(n_files)]
        doc_id_dict = {fn: f"doc-{i}" for i, fn in enumerate(file_names)}

        # All conversions complete immediately (pre-cancel).
        conv_futs = []
        for _ in range(n_files):
            f: Future = Future()
            f.set_result(("conv.json", 1.0))
            conv_futs.append(f)

        # Processing futures:
        #   first n_early are done (completed before cancel is observed in proc loop)
        #   rest are pending (never resolved — will be in pending_process when cancel hits)
        early_proc_futs = []
        for _ in range(n_early):
            f: Future = Future()
            f.set_result(("txt.json", "tab.json", 5, 2, {"process_text": 0.1, "process_tables": 0.1}, "en"))
            early_proc_futs.append(f)

        late_proc_futs = [Future() for _ in range(n_files - n_early)]

        all_proc_futs = early_proc_futs + late_proc_futs

        # chunk_futures submitted for the n_early docs — must be cancelled.
        chunk_futs = [Future() for _ in range(n_early)]

        conv_idx = {"n": 0}
        proc_idx = {"n": 0}
        chunk_idx = {"n": 0}

        def conv_submit(*args, **kwargs):
            idx = conv_idx["n"]
            conv_idx["n"] += 1
            return conv_futs[idx]

        cancel_call = {"n": 0}

        def is_cancelled(job_id):
            cancel_call["n"] += 1
            # Call 1: pre-loop check — False
            # Call 2: conversion poll — False → all 10 proc futures submitted
            # Call 3: processing poll iteration 1 — False → early proc futures submit chunk_futs
            # Call 4: processing poll iteration 2 — True → late proc futures cancelled,
            #         already-queued chunk_futs are cancelled before raising
            return cancel_call["n"] >= 4

        mock_db = Mock()
        mock_db.is_job_cancelled = Mock(side_effect=is_cancelled)

        thread_submit_idx = {"n": 0}

        def thread_submit(*args, **kwargs):
            idx = thread_submit_idx["n"]
            thread_submit_idx["n"] += 1
            if idx < n_files:
                return all_proc_futs[idx]
            # chunk_futs come after all proc_futs
            chunk_idx_val = idx - n_files
            return chunk_futs[chunk_idx_val]

        with (
            patch("digitize.processing.orchestrator.db_manager", mock_db),
            patch("digitize.processing.orchestrator.get_status_manager", return_value=Mock()),
            patch("digitize.processing.orchestrator.ProcessPoolExecutor") as mock_ppe,
            patch("digitize.processing.orchestrator.ContextAwareThreadPoolExecutor") as mock_tpe,
            patch("digitize.processing.orchestrator.get_document_page_count", return_value=1),
            patch("digitize.processing.orchestrator.time.sleep"),
        ):
            conv_ex = MagicMock()
            conv_ex.__enter__ = Mock(return_value=conv_ex)
            conv_ex.__exit__ = Mock(return_value=False)
            conv_ex.submit = Mock(side_effect=conv_submit)
            mock_ppe.return_value = conv_ex

            thread_ex = MagicMock()
            thread_ex.__enter__ = Mock(return_value=thread_ex)
            thread_ex.__exit__ = Mock(return_value=False)
            thread_ex.submit = Mock(side_effect=thread_submit)
            mock_tpe.return_value = thread_ex

            with pytest.raises(JobCancelledError):
                process_documents(
                    input_paths=file_names,
                    out_path="/tmp/out",
                    llm_model="m", llm_endpoint="e",
                    emb_endpoint="emb",
                    max_tokens=512,
                    job_id="job-10-files-proc",
                    doc_id_dict=doc_id_dict,
                )

        for i, cf in enumerate(chunk_futs):
            assert cf.cancelled(), (
                f"chunk_future[{i}] was NOT cancelled — "
                "the fix to cancel queued downstream futures at the "
                "processing→chunk boundary is missing or incomplete"
            )
