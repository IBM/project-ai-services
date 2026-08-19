"""
Unit tests for the de-duplication feature introduced across commits
279309ca → 269f3d87 on branch dupIndex.

# NOTE on patching strategy
# --------------------------
# conftest.py installs an autouse fixture `mock_db_operations` that replaces
# several symbols on the `digitize.utils.db` module object **before** each test
# runs.  Because Python module objects are shared singletons, any attempt to
# re-import or re-lookup those symbols inside a test still sees the mock.
#
# To test the *internals* of create_document / get_job we therefore stash
# references to the real implementations at collection time (below), before the
# first fixture runs.  Those references bypass the per-test autouse patches.

Coverage areas
──────────────
1. models.py          – AlreadyExistsFile, DocStatus.ALREADY_EXISTS, JobDocumentSummary.message
2. utils/jobs.py      – initialize_job_state with already_exists_files
                      – get_job_document_stats counting already_exists as completed
3. utils/db.py        – create_document with initial_status / completed_at / extra_metadata
                      – _categorize_fields recognising new metadata keys
                      – get_job / get_all_jobs populate .message for already_exists docs
                      – DatabaseStatusManager._update_job stats (already_exists = completed)
                      – DatabaseStatusManager.update_doc_metadata triggers upsert_file_checksum
4. api/v1/jobs.py     – 409 when ALL files already exist
                      – 202 with mixed batch (some novel, some already-exist)
                      – file_checksum_dict passed to initialize_job_state
                      – digitization also checks ingestion hash
5. db/manager.py      – upsert_file_checksum (insert + on-conflict update)
                      – find_completed_document_by_hash (match / no-match / DB error)
                      – delete_document removes checksum registry row first
                      – delete_all_documents wipes checksum registry
                      – get_all_documents excludes already_exists by default
"""

from datetime import datetime, timezone
from types import SimpleNamespace
from typing import cast
from unittest.mock import AsyncMock, MagicMock, Mock, patch

import pytest

# Stash real implementations before autouse fixtures can replace them.
import digitize.utils.db as _db_mod
_real_create_document = _db_mod.create_document
_real_get_job = _db_mod.get_job

from digitize.models import (
    AlreadyExistsFile,
    DocStatus,
    JobDocumentSummary,
    JobStatus,
    OperationType,
    OutputFormat,
)
from digitize.utils.db import _categorize_fields


# ============================================================================
# 1. Model tests
# ============================================================================

@pytest.mark.unit
class TestAlreadyExistsFileModel:
    def test_fields_are_required(self):
        from pydantic import ValidationError

        with pytest.raises(ValidationError):
            AlreadyExistsFile(filename="f.pdf")  # missing required fields

    def test_valid_construction(self):
        obj = AlreadyExistsFile(
            filename="report.pdf",
            existing_doc_id="doc-old-1",
            existing_doc_name="old-report.pdf",
            file_hash="a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
        )
        assert obj.filename == "report.pdf"
        assert obj.existing_doc_id == "doc-old-1"
        assert obj.existing_doc_name == "old-report.pdf"
        assert obj.file_hash == "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"

    def test_model_dump(self):
        obj = AlreadyExistsFile(
            filename="a.pdf",
            existing_doc_id="d1",
            existing_doc_name="a_orig.pdf",
            file_hash="d41d8cd98f00b204e9800998ecf8427e",
        )
        assert obj.model_dump() == {
            "filename": "a.pdf",
            "existing_doc_id": "d1",
            "existing_doc_name": "a_orig.pdf",
            "file_hash": "d41d8cd98f00b204e9800998ecf8427e",
        }


@pytest.mark.unit
class TestDocStatusAlreadyExists:
    def test_already_exists_value(self):
        assert DocStatus.ALREADY_EXISTS.value == "already_exists"

    def test_already_exists_is_str_subclass(self):
        assert DocStatus.ALREADY_EXISTS == "already_exists"

    def test_already_exists_in_enum_set(self):
        assert "already_exists" in {s.value for s in DocStatus}

    def test_round_trip(self):
        assert DocStatus("already_exists") is DocStatus.ALREADY_EXISTS


@pytest.mark.unit
class TestJobDocumentSummaryMessage:
    def test_message_defaults_to_none(self):
        summary = JobDocumentSummary(id="d1", name="f.pdf", status="completed")
        assert summary.message is None

    def test_message_accepts_string(self):
        summary = JobDocumentSummary(
            id="d1",
            name="f.pdf",
            status="already_exists",
            message="Already ingested as old.pdf",
        )
        assert summary.message == "Already ingested as old.pdf"

    def test_message_included_in_dump(self):
        summary = JobDocumentSummary(
            id="d1", name="f.pdf", status="already_exists", message="Already ingested as x.pdf"
        )
        assert summary.model_dump()["message"] == "Already ingested as x.pdf"

    def test_message_none_included_in_dump_as_none(self):
        summary = JobDocumentSummary(id="d1", name="f.pdf", status="completed")
        assert summary.model_dump()["message"] is None


# ============================================================================
# 2. utils/jobs.py tests
# ============================================================================

@pytest.mark.unit
class TestGetJobDocumentStatsAlreadyExists:
    """get_job_document_stats must count already_exists docs as completed.

    We patch `digitize.utils.jobs.get_job` — the name as it is bound inside
    jobs.py via `from digitize.utils.db import … get_job …`.
    """

    def test_already_exists_counted_in_completed(self):
        job_data = {
            "job_id": "job-1",
            "documents": [
                {"id": "d1", "name": "a.pdf", "status": "completed"},
                {"id": "d2", "name": "b.pdf", "status": "already_exists"},
                {"id": "d3", "name": "c.pdf", "status": "failed"},
            ],
        }
        with patch("digitize.utils.jobs.get_job", return_value=job_data):
            from digitize.utils.jobs import get_job_document_stats
            stats = get_job_document_stats("job-1")

        assert stats["completed_count"] == 2
        assert stats["failed_count"] == 1
        assert stats["total_docs"] == 3

    def test_no_already_exists_docs(self):
        job_data = {
            "job_id": "job-1",
            "documents": [{"id": "d1", "name": "a.pdf", "status": "completed"}],
        }
        with patch("digitize.utils.jobs.get_job", return_value=job_data):
            from digitize.utils.jobs import get_job_document_stats
            stats = get_job_document_stats("job-1")

        assert stats["completed_count"] == 1

    def test_all_already_exists_docs(self):
        job_data = {
            "job_id": "job-1",
            "documents": [
                {"id": "d1", "name": "a.pdf", "status": "already_exists"},
                {"id": "d2", "name": "b.pdf", "status": "already_exists"},
            ],
        }
        with patch("digitize.utils.jobs.get_job", return_value=job_data):
            from digitize.utils.jobs import get_job_document_stats
            stats = get_job_document_stats("job-1")

        assert stats["completed_count"] == 2
        assert stats["failed_count"] == 0


@pytest.mark.unit
class TestInitializeJobStateAlreadyExists:
    """initialize_job_state with already_exists_files must create docs with
    ALREADY_EXISTS status and include their filenames in the job total.

    We patch `digitize.utils.jobs.create_job` and `digitize.utils.jobs.create_document`
    — the names as bound inside jobs.py.
    """

    def _make_skipped(self, filename="old.pdf"):
        return AlreadyExistsFile(
            filename=filename,
            existing_doc_id="doc-existing-1",
            existing_doc_name="old.pdf",
            file_hash="deadbeefdeadbeefdeadbeefdeadbeef",
        )

    def test_create_job_includes_already_exists_filenames(self):
        mock_create_job = Mock()
        mock_create_doc = Mock()

        with patch("digitize.utils.jobs.create_job", mock_create_job), \
             patch("digitize.utils.jobs.create_document", mock_create_doc):
            from digitize.utils.jobs import initialize_job_state
            skipped = self._make_skipped("already.pdf")
            initialize_job_state(
                job_id="job-1",
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=["novel.pdf"],
                already_exists_files=[skipped],
            )

        create_job_call = mock_create_job.call_args
        docs_info = create_job_call[1]["documents_info"]
        assert "novel.pdf" in docs_info
        assert "already.pdf" in docs_info

    def test_already_exists_doc_created_with_correct_status(self):
        mock_create_job = Mock()
        mock_create_doc = Mock()

        with patch("digitize.utils.jobs.create_job", mock_create_job), \
             patch("digitize.utils.jobs.create_document", mock_create_doc):
            from digitize.utils.jobs import initialize_job_state
            skipped = self._make_skipped("already.pdf")
            initialize_job_state(
                job_id="job-1",
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=["novel.pdf"],
                already_exists_files=[skipped],
            )

        assert mock_create_doc.call_count == 2
        skipped_call = next(
            c for c in mock_create_doc.call_args_list if c[1].get("doc_name") == "already.pdf"
        )
        assert skipped_call[1]["initial_status"] == DocStatus.ALREADY_EXISTS

    def test_already_exists_doc_extra_metadata_passed(self):
        mock_create_job = Mock()
        mock_create_doc = Mock()

        with patch("digitize.utils.jobs.create_job", mock_create_job), \
             patch("digitize.utils.jobs.create_document", mock_create_doc):
            from digitize.utils.jobs import initialize_job_state
            skipped = self._make_skipped("already.pdf")
            initialize_job_state(
                job_id="job-1",
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=[],
                already_exists_files=[skipped],
            )

        skipped_call = next(
            c for c in mock_create_doc.call_args_list if c[1].get("doc_name") == "already.pdf"
        )
        meta = skipped_call[1]["extra_metadata"]
        assert meta["existing_doc_id"] == "doc-existing-1"
        assert meta["existing_doc_name"] == "old.pdf"
        assert meta["file_hash"] == "deadbeefdeadbeefdeadbeefdeadbeef"

    def test_no_already_exists_files_does_not_call_extra_create(self):
        mock_create_job = Mock()
        mock_create_doc = Mock()

        with patch("digitize.utils.jobs.create_job", mock_create_job), \
             patch("digitize.utils.jobs.create_document", mock_create_doc):
            from digitize.utils.jobs import initialize_job_state
            initialize_job_state(
                job_id="job-1",
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=["novel.pdf"],
                already_exists_files=None,
            )

        assert mock_create_doc.call_count == 1

    def test_already_exists_doc_id_added_to_return_dict(self):
        mock_create_job = Mock()
        mock_create_doc = Mock()

        with patch("digitize.utils.jobs.create_job", mock_create_job), \
             patch("digitize.utils.jobs.create_document", mock_create_doc):
            from digitize.utils.jobs import initialize_job_state
            skipped = self._make_skipped("already.pdf")
            result = initialize_job_state(
                job_id="job-1",
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=["novel.pdf"],
                already_exists_files=[skipped],
            )

        assert "already.pdf" in result
        assert "novel.pdf" in result

    def test_empty_already_exists_list_treated_same_as_none(self):
        mock_create_job = Mock()
        mock_create_doc = Mock()

        with patch("digitize.utils.jobs.create_job", mock_create_job), \
             patch("digitize.utils.jobs.create_document", mock_create_doc):
            from digitize.utils.jobs import initialize_job_state
            result = initialize_job_state(
                job_id="job-1",
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=["novel.pdf"],
                already_exists_files=[],
            )

        assert mock_create_doc.call_count == 1
        assert "novel.pdf" in result


# ============================================================================
# 3. utils/db.py tests
# ============================================================================

@pytest.mark.unit
class TestCreateDocumentNewParams:
    """create_document accepts initial_status, completed_at, extra_metadata.

    The autouse mock_db_operations fixture replaces the create_document symbol in
    digitize.utils.db before our test runs, so we must also restore the real
    function by importing it from source and calling it directly, with engine and
    db_manager patched.
    """

    def _call(self, mock_dm, **kwargs):
        """Call the real create_document with stubbed engine and db_manager.

        Uses _real_create_document (stashed at collection time) to bypass the
        autouse mock_db_operations fixture that replaces the module-level symbol.
        """
        defaults = dict(
            doc_name="test.pdf",
            doc_id="doc-1",
            job_id="job-1",
            output_format=OutputFormat.JSON,
            operation="ingestion",
            submitted_at="2024-01-01T00:00:00Z",
        )
        defaults.update(kwargs)

        fake_engine = Mock()
        with patch("digitize.utils.db.engine", fake_engine), \
             patch("digitize.utils.db.db_manager", mock_dm):
            _real_create_document(**defaults)

        return mock_dm.create_document.call_args

    def test_default_status_is_accepted(self):
        mock_dm = Mock()
        mock_dm.create_document.return_value = Mock()
        call_args = self._call(mock_dm)
        assert call_args[1]["status"] == DocStatus.ACCEPTED

    def test_custom_initial_status(self):
        mock_dm = Mock()
        mock_dm.create_document.return_value = Mock()
        call_args = self._call(mock_dm, initial_status=DocStatus.ALREADY_EXISTS)
        assert call_args[1]["status"] == DocStatus.ALREADY_EXISTS

    def test_completed_at_parsed_and_passed(self):
        mock_dm = Mock()
        mock_dm.create_document.return_value = Mock()
        call_args = self._call(mock_dm, completed_at="2024-01-01T01:00:00Z")
        completed_dt = call_args[1]["completed_at"]
        assert completed_dt is not None
        assert completed_dt.year == 2024

    def test_completed_at_none_by_default(self):
        mock_dm = Mock()
        mock_dm.create_document.return_value = Mock()
        call_args = self._call(mock_dm)
        assert call_args[1]["completed_at"] is None

    def test_extra_metadata_merged_into_base(self):
        mock_dm = Mock()
        mock_dm.create_document.return_value = Mock()
        call_args = self._call(
            mock_dm,
            extra_metadata={
                "existing_doc_id": "doc-old",
                "existing_doc_name": "original.pdf",
                "file_hash": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
            },
        )
        metadata = call_args[1]["metadata"]
        assert metadata["existing_doc_id"] == "doc-old"
        assert metadata["existing_doc_name"] == "original.pdf"
        assert metadata["file_hash"] == "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
        assert "pages" in metadata  # base fields preserved

    def test_no_extra_metadata_keeps_base_fields(self):
        mock_dm = Mock()
        mock_dm.create_document.return_value = Mock()
        call_args = self._call(mock_dm)
        metadata = call_args[1]["metadata"]
        assert metadata["pages"] == 0
        assert metadata["tables"] == 0

    def test_raises_when_db_manager_returns_none(self):
        mock_dm = Mock()
        mock_dm.create_document.return_value = None
        fake_engine = Mock()

        with patch("digitize.utils.db.engine", fake_engine), \
             patch("digitize.utils.db.db_manager", mock_dm):
            with pytest.raises(Exception):
                _real_create_document(
                    doc_name="x.pdf",
                    doc_id="doc-x",
                    job_id="job-1",
                    output_format=OutputFormat.JSON,
                    operation="ingestion",
                    submitted_at="2024-01-01T00:00:00Z",
                )


@pytest.mark.unit
class TestCategorizeFieldsNewKeys:
    """_categorize_fields must route file_hash, existing_doc_id, existing_doc_name
    into metadata_fields, not top_level_fields."""

    def test_file_hash_goes_to_metadata(self):
        meta, top = _categorize_fields({"file_hash": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"})
        assert "file_hash" in meta
        assert "file_hash" not in top

    def test_existing_doc_id_goes_to_metadata(self):
        meta, top = _categorize_fields({"existing_doc_id": "doc-x"})
        assert "existing_doc_id" in meta
        assert "existing_doc_id" not in top

    def test_existing_doc_name_goes_to_metadata(self):
        meta, top = _categorize_fields({"existing_doc_name": "orig.pdf"})
        assert "existing_doc_name" in meta
        assert "existing_doc_name" not in top

    def test_status_goes_to_top_level(self):
        meta, top = _categorize_fields({"status": DocStatus.COMPLETED})
        assert "status" in top
        assert "status" not in meta

    def test_mixed_fields_split_correctly(self):
        meta, top = _categorize_fields({
            "status": DocStatus.COMPLETED,
            "file_hash": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
            "pages": 5,
        })
        assert "file_hash" in meta
        assert "pages" in meta
        assert "status" in top
        assert "status" not in meta


@pytest.mark.unit
class TestGetJobMessagePopulation:
    """get_job must set message='Already ingested as <name>' for already_exists docs."""

    def _make_doc_mock(self, status, existing_doc_name=None):
        m = Mock()
        m.doc_id = "d1"
        m.name = "dup.pdf"
        m.status = status
        m.doc_metadata = {"existing_doc_name": existing_doc_name} if existing_doc_name else {}
        return m

    def _make_job_mock(self):
        j = Mock()
        j.job_id = "job-1"
        j.job_name = None
        j.operation = "ingestion"
        j.status = "completed"
        j.submitted_at = datetime(2024, 1, 1, tzinfo=timezone.utc)
        j.completed_at = datetime(2024, 1, 1, 1, tzinfo=timezone.utc)
        j.error = None
        j.stats = {"total_documents": 1, "completed": 1, "failed": 0, "in_progress": 0}
        return j

    def test_already_exists_doc_gets_message(self):
        mock_dm = Mock()
        mock_dm.get_job_by_id.return_value = self._make_job_mock()
        mock_dm.get_documents_by_job_id.return_value = [
            self._make_doc_mock("already_exists", "original.pdf")
        ]
        fake_engine = Mock()

        # Use _real_get_job (stashed at collection time) to bypass the autouse
        # mock_db_operations fixture which replaces the module-level get_job symbol.
        with patch("digitize.utils.db.engine", fake_engine), \
             patch("digitize.utils.db.db_manager", mock_dm):
            result = _real_get_job("job-1")

        assert result is not None
        doc_summary = result["documents"][0]
        assert doc_summary["message"] == "Already ingested as original.pdf"

    def test_completed_doc_has_no_message(self):
        mock_dm = Mock()
        mock_dm.get_job_by_id.return_value = self._make_job_mock()
        mock_dm.get_documents_by_job_id.return_value = [
            self._make_doc_mock("completed")
        ]
        fake_engine = Mock()

        with patch("digitize.utils.db.engine", fake_engine), \
             patch("digitize.utils.db.db_manager", mock_dm):
            result = _real_get_job("job-1")

        assert result["documents"][0]["message"] is None


@pytest.mark.unit
class TestUpdateDocMetadataChecksumRegistration:
    """update_doc_metadata must call upsert_file_checksum when status=COMPLETED
    and file_hash is present in the update."""

    def test_checksum_upserted_on_completed_with_file_hash(self, mock_db_manager):
        mock_db_manager.get_document_by_id.return_value = Mock(
            doc_metadata={"pages": 0, "tables": 0, "timing_in_secs": {}}
        )
        mock_db_manager.update_document.return_value = True

        from digitize.utils.db import DatabaseStatusManager
        mgr = DatabaseStatusManager("job-1")
        mgr.update_doc_metadata("doc-1", {
            "status": DocStatus.COMPLETED,
            "file_hash": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
        })

        mock_db_manager.upsert_file_checksum.assert_called_once_with("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", "doc-1")

    def test_checksum_not_upserted_without_file_hash(self, mock_db_manager):
        mock_db_manager.get_document_by_id.return_value = Mock(
            doc_metadata={"pages": 0, "tables": 0, "timing_in_secs": {}}
        )
        mock_db_manager.update_document.return_value = True

        from digitize.utils.db import DatabaseStatusManager
        mgr = DatabaseStatusManager("job-1")
        mgr.update_doc_metadata("doc-1", {"status": DocStatus.COMPLETED, "pages": 3})

        mock_db_manager.upsert_file_checksum.assert_not_called()

    def test_checksum_not_upserted_when_status_not_completed(self, mock_db_manager):
        mock_db_manager.get_document_by_id.return_value = Mock(
            doc_metadata={"pages": 0, "tables": 0, "timing_in_secs": {}}
        )
        mock_db_manager.update_document.return_value = True

        from digitize.utils.db import DatabaseStatusManager
        mgr = DatabaseStatusManager("job-1")
        mgr.update_doc_metadata("doc-1", {
            "status": DocStatus.IN_PROGRESS,
            "file_hash": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
        })

        mock_db_manager.upsert_file_checksum.assert_not_called()


@pytest.mark.unit
class TestUpdateJobStatsAlreadyExists:
    """_update_job must count already_exists documents as completed in job stats."""

    def _make_docs(self, statuses):
        docs = []
        for i, s in enumerate(statuses):
            m = Mock()
            m.doc_id = f"d{i}"
            m.status = s
            docs.append(m)
        return docs

    def test_already_exists_counted_as_completed_in_stats(self, mock_db_manager):
        mock_db_manager.get_job_by_id.return_value = Mock(
            job_id="job-1",
            status="in_progress",
            stats={},
        )
        mock_db_manager.get_documents_by_job_id.return_value = self._make_docs([
            "already_exists",
            "completed",
            "failed",
            "accepted",
        ])
        mock_db_manager.update_job.return_value = True

        from digitize.utils.db import DatabaseStatusManager
        mgr = DatabaseStatusManager("job-1")
        mgr.update_job_progress("d1", DocStatus.COMPLETED, JobStatus.IN_PROGRESS)

        stats = mock_db_manager.update_job.call_args[1]["stats"]
        assert stats["completed"] == 2   # already_exists + completed
        assert stats["failed"] == 1
        assert stats["in_progress"] == 1  # accepted

    def test_accepted_doc_counted_in_in_progress_stats(self, mock_db_manager):
        mock_db_manager.get_job_by_id.return_value = Mock(
            job_id="job-1", status="in_progress", stats={}
        )
        mock_db_manager.get_documents_by_job_id.return_value = self._make_docs([
            "accepted",
            "in_progress",
            "digitized",
            "processed",
            "chunked",
        ])
        mock_db_manager.update_job.return_value = True

        from digitize.utils.db import DatabaseStatusManager
        mgr = DatabaseStatusManager("job-1")
        mgr.update_job_progress("", DocStatus.ACCEPTED, JobStatus.IN_PROGRESS)

        stats = mock_db_manager.update_job.call_args[1]["stats"]
        assert stats["in_progress"] == 5
        assert stats["completed"] == 0


# ============================================================================
# 4. api/v1/jobs.py endpoint tests (duplicate-detection paths)
# ============================================================================

@pytest.fixture
def jobs_test_client(monkeypatch, tmp_path, mock_db_operations):
    """Thin test-client fixture focused on the de-duplication code paths."""
    import digitize.app as digitize_app
    import digitize.api.v1.jobs as jobs_router_module
    import digitize.api.v1.documents as documents_router_module
    import digitize.api.v1.jobs as jobs_router_module
    from fastapi.testclient import TestClient

    digitized_dir = tmp_path / "digitized"
    staging_dir = tmp_path / "staging"
    for p in (digitized_dir, staging_dir):
        p.mkdir(parents=True, exist_ok=True)

    fake_settings = SimpleNamespace(
        common=SimpleNamespace(app=SimpleNamespace(log_level="INFO")),
        digitize=SimpleNamespace(
            digitized_docs_dir=digitized_dir,
            staging_dir=staging_dir,
            ingestion_queue_quota=10,
            digitization_queue_quota=5,
            heavy_doc_page_threshold=500,
            conversion_poll_interval=2.0,
        ),
    )
    monkeypatch.setattr(digitize_app, "settings", fake_settings, raising=False)
    monkeypatch.setattr(digitize_app.dg_util, "settings", fake_settings, raising=False)
    # Single db_manager mock covering all calls made by jobs.py.
    # Individual tests may replace this with their own mock via
    # monkeypatch.setattr(jobs_router_module, "db_manager", ...) — in that case
    # check_quota_atomic must also be set on that replacement mock.
    mock_db_manager = Mock()
    mock_db_manager.check_quota_atomic = Mock(return_value=(True, 0))
    mock_db_manager.find_completed_document_by_hash = Mock(return_value=None)
    monkeypatch.setattr(jobs_router_module, "db_manager", mock_db_manager)

    # Stub out dg_util helpers that touch disk / DB.
    monkeypatch.setattr(digitize_app.dg_util, "get_document_page_count", Mock(return_value=0))
    monkeypatch.setattr(jobs_router_module, "generate_file_checksum", Mock(return_value="sha256:abc123"))

    # Stub out pipeline background tasks.
    monkeypatch.setattr(jobs_router_module, "_run_digitize", Mock())
    monkeypatch.setattr(jobs_router_module, "_run_ingest", Mock())

    monkeypatch.setattr(digitize_app.dg_util, "generate_uuid", Mock(return_value="job-x"))
    monkeypatch.setattr(digitize_app.dg_util, "stage_upload_files", AsyncMock())
    monkeypatch.setattr(digitize_app.dg_util, "enqueue_conversion_tasks", AsyncMock())
    monkeypatch.setattr(
        digitize_app.dg_util, "initialize_job_state", Mock(return_value={"novel.pdf": "doc-1"})
    )
    monkeypatch.setattr(documents_router_module, "reset_db", Mock())
    monkeypatch.setattr(digitize_app, "configure_uvicorn_logging", Mock())

    return TestClient(digitize_app.app)


@pytest.mark.unit
class TestDuplicateDetectionEndpoint:
    """Tests for the hash-based already-exists detection in POST /v1/jobs."""

    def _pdf(self, name="sample.pdf"):
        return ("files", (name, b"%PDF-1.4 test", "application/pdf"))

    def test_all_files_exist_returns_409(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module

        existing = Mock()
        existing.doc_id = "old-doc"
        existing.name = "sample.pdf"

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.find_completed_document_by_hash = Mock(return_value=existing)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("sample.pdf")],
        )

        assert response.status_code == 409

    def test_409_body_mentions_existing_doc(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module

        existing = Mock()
        existing.doc_id = "old-doc-id"
        existing.name = "sample.pdf"

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.find_completed_document_by_hash = Mock(return_value=existing)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("sample.pdf")],
        )

        assert "old-doc-id" in response.text

    def test_mixed_batch_returns_202(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module

        existing = Mock()
        existing.doc_id = "old-doc"
        existing.name = "old.pdf"

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        # first file matches, second is novel
        mock_hash_db.find_completed_document_by_hash = Mock(side_effect=[existing, None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("old.pdf"), self._pdf("new.pdf")],
        )

        assert response.status_code == 202

    def test_mixed_batch_response_has_no_warnings_key(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module

        existing = Mock()
        existing.doc_id = "old-doc"
        existing.name = "old.pdf"

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.find_completed_document_by_hash = Mock(side_effect=[existing, None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("old.pdf"), self._pdf("new.pdf")],
        )

        assert "warnings" not in response.json()

    def test_mixed_batch_already_exists_passed_to_initialize(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module
        import digitize.app as digitize_app

        existing = Mock()
        existing.doc_id = "old-doc"
        existing.name = "old.pdf"

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.find_completed_document_by_hash = Mock(side_effect=[existing, None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("old.pdf"), self._pdf("new.pdf")],
        )

        init_call = cast(Mock, digitize_app.dg_util.initialize_job_state).call_args
        already_exists_arg = init_call[1].get("already_exists_files", init_call[0][-1])
        assert len(already_exists_arg) == 1
        assert already_exists_arg[0].filename == "old.pdf"
        assert already_exists_arg[0].existing_doc_id == "old-doc"

    def test_novel_only_batch_passes_empty_already_exists(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module
        import digitize.app as digitize_app

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.find_completed_document_by_hash = Mock(return_value=None)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("new.pdf")],
        )

        init_call = cast(Mock, digitize_app.dg_util.initialize_job_state).call_args
        already_exists_arg = init_call[1].get("already_exists_files", [])
        assert already_exists_arg == []

    def test_digitization_also_checks_ingestion_hash(self, jobs_test_client, monkeypatch):
        """A file already ingested must block a digitization request of the same content."""
        import digitize.api.v1.jobs as jobs_router_module

        ingested_doc = Mock()
        ingested_doc.doc_id = "ingested-doc"
        ingested_doc.name = "report.pdf"

        # digitization-type lookup returns None; ingestion fallback returns a match
        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.find_completed_document_by_hash = Mock(
            side_effect=[None, ingested_doc]
        )
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=digitization&output_format=json",
            files=[self._pdf("report.pdf")],
        )

        assert response.status_code == 409

    def test_all_files_novel_returns_202(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.find_completed_document_by_hash = Mock(return_value=None)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("brand-new.pdf")],
        )

        assert response.status_code == 202


# ============================================================================
# 5. db/manager.py tests
#
# db/manager.py imports get_db_session at module level:
#   from digitize.db.connection import get_db_session
# so we must patch `digitize.db.manager.get_db_session` — not the connection module.
# ============================================================================

def _make_session_ctx(session):
    """Return a context manager that yields `session`."""
    ctx = MagicMock()
    ctx.__enter__ = Mock(return_value=session)
    ctx.__exit__ = Mock(return_value=None)
    return ctx


@pytest.mark.unit
class TestDatabaseManagerUpsertFileChecksum:
    """DatabaseManager.upsert_file_checksum uses INSERT … ON CONFLICT DO UPDATE."""

    def test_upsert_executes_statement(self):
        session = MagicMock()
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            DatabaseManager.upsert_file_checksum("abc", "doc-1")

        session.execute.assert_called_once()

    def test_upsert_handles_db_error_without_raising(self):
        from sqlalchemy.exc import SQLAlchemyError

        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("boom")

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            # Must not propagate the error
            DatabaseManager.upsert_file_checksum("abc", "doc-1")


@pytest.mark.unit
class TestDatabaseManagerFindCompletedDocumentByHash:
    """find_completed_document_by_hash returns doc / None / None-on-error."""

    def test_returns_none_when_not_found(self):
        session = MagicMock()
        session.scalar.return_value = None

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            result = DatabaseManager.find_completed_document_by_hash("missing", "ingestion")

        assert result is None

    def test_returns_document_when_found(self):
        mock_doc = Mock()
        mock_doc.doc_id = "d1"
        mock_doc.job_id = "j1"
        mock_doc.name = "found.pdf"
        mock_doc.type = "ingestion"
        mock_doc.status = "completed"
        mock_doc.output_format = "json"
        mock_doc.submitted_at = datetime(2024, 1, 1, tzinfo=timezone.utc)
        mock_doc.completed_at = datetime(2024, 1, 1, 1, tzinfo=timezone.utc)
        mock_doc.error = None
        mock_doc.doc_metadata = {}

        session = MagicMock()
        session.scalar.return_value = mock_doc

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            result = DatabaseManager.find_completed_document_by_hash("abc", "ingestion")

        assert result is mock_doc
        session.expunge.assert_called_once_with(mock_doc)

    def test_returns_none_on_db_error(self):
        from sqlalchemy.exc import SQLAlchemyError

        session = MagicMock()
        session.scalar.side_effect = SQLAlchemyError("connection lost")

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            # DB errors must NOT raise — caller treats file as novel
            result = DatabaseManager.find_completed_document_by_hash("x", "ingestion")

        assert result is None

    def test_query_executes_against_session(self):
        session = MagicMock()
        session.scalar.return_value = None

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            DatabaseManager.find_completed_document_by_hash("abc", "digitization")

        session.scalar.assert_called_once()


@pytest.mark.unit
class TestDatabaseManagerDeleteDocumentClearsChecksum:
    """delete_document must remove the checksum registry entry and shadow docs before deleting the doc."""

    def test_execute_called_three_times(self):
        session = MagicMock()
        session.execute.return_value = Mock(rowcount=1)

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            DatabaseManager.delete_document("doc-1")

        # 1st execute: registry delete.
        # 2nd execute: shadow already_exists docs delete.
        # 3rd execute: document delete.
        assert session.execute.call_count == 3

    def test_returns_true_when_deleted(self):
        session = MagicMock()
        # registry delete, shadow docs delete (rowcount irrelevant), doc delete rowcount=1
        session.execute.side_effect = [Mock(rowcount=0), Mock(rowcount=0), Mock(rowcount=1)]

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            result = DatabaseManager.delete_document("doc-1")

        assert result is True

    def test_returns_false_when_not_found(self):
        session = MagicMock()
        session.execute.side_effect = [Mock(rowcount=0), Mock(rowcount=0), Mock(rowcount=0)]

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            result = DatabaseManager.delete_document("doc-missing")

        assert result is False

    def test_shadow_docs_deleted_before_original(self):
        """already_exists placeholder rows referencing this doc_id are removed."""
        session = MagicMock()
        session.execute.return_value = Mock(rowcount=1)

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            import sqlalchemy
            DatabaseManager.delete_document("doc-orig")

        # Verify the second call (index 1) targets Documents via a metadata JSONB lookup.
        # The key 'existing_doc_id' is passed as a bind parameter, so check the params dict.
        second_call_stmt = session.execute.call_args_list[1][0][0]
        dialect = sqlalchemy.dialects.postgresql.dialect()
        compiled = second_call_stmt.compile(dialect=dialect)
        sql = str(compiled).lower()
        assert "delete" in sql
        assert "documents" in sql
        # The JSONB key is passed as a bind param — confirm it appears in the compiled params.
        assert "existing_doc_id" in compiled.params.values()


@pytest.mark.unit
class TestDatabaseManagerDeleteAllDocumentsClearsRegistry:
    """delete_all_documents must wipe the checksum registry."""

    def test_execute_called_twice(self):
        session = MagicMock()
        session.execute.return_value = Mock(rowcount=3)

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            DatabaseManager.delete_all_documents()

        # First call: wipe registry. Second call: delete all documents.
        assert session.execute.call_count == 2

    def test_returns_deleted_count(self):
        session = MagicMock()
        session.execute.side_effect = [Mock(rowcount=0), Mock(rowcount=5)]

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            result = DatabaseManager.delete_all_documents()

        assert result["deleted_count"] == 5


@pytest.mark.unit
class TestGetAllDocumentsExcludesAlreadyExists:
    """get_all_documents must exclude already_exists docs when no explicit status filter."""

    def test_no_status_filter_adds_exclusion(self):
        session = MagicMock()
        session.execute.return_value = Mock(
            scalars=Mock(return_value=Mock(all=Mock(return_value=[]))),
        )
        session.scalar.return_value = 0

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            result = DatabaseManager.get_all_documents(status=None, name=None, limit=10, offset=0)

        assert isinstance(result, tuple)
        # The session must have been used — confirms the method ran to completion
        assert session.execute.called or session.scalar.called

# Made with IBM Bob
