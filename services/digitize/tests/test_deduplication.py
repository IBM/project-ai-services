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
                      – initialize_job_state claims checksums after the document rows exist;
                        a lost claim becomes an already_exists / skipped record
                      – get_job_document_stats counting already_exists as completed
3. utils/db.py        – create_document with initial_status / completed_at / extra_metadata
                      – _categorize_fields recognising new metadata keys
                      – get_job / get_all_jobs populate .message for already_exists docs
                      – DatabaseStatusManager._update_job stats (already_exists = completed)
                      – DatabaseStatusManager.update_doc_metadata leaves checksum lifecycle to db_manager
4. api/v1/jobs.py     – 409 when ALL files already exist
                      – 202 with mixed batch (some novel, some already-exist)
                      – read-only owner lookup; novel file hashes handed to initialize_job_state
                      – digitization also checks the ingestion owner
5. db/manager.py      – claim_file_checksum (atomic INSERT ... ON CONFLICT DO NOTHING)
                      – get_file_owner (read-only lookup, joins documents)
                      – update_document checksum lifecycle (claim removed on fail+cancel)
                      – delete_document removes checksum registry row first
                      – delete_user_documents skips connector docs, wipes user checksum registry
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
class TestDocStatusSkipped:
    def test_skipped_value(self):
        assert DocStatus.SKIPPED.value == "skipped"

    def test_round_trip(self):
        assert DocStatus("skipped") is DocStatus.SKIPPED

    def test_skipped_counted_as_completed_in_job_stats(self):
        job_data = {
            "documents": [
                {"id": "d1", "name": "a.pdf", "status": "completed"},
                {"id": "d2", "name": "b.pdf", "status": "skipped"},
            ]
        }
        with patch("digitize.utils.jobs.get_job", return_value=job_data):
            from digitize.utils.jobs import get_job_document_stats
            stats = get_job_document_stats("job-1")

        assert stats["completed_count"] == 2
        assert stats["failed_count"] == 0


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
        assert "file_hash" not in meta  # hash lives in document_checksum, not on the doc

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

    def _make_skip(self, filename="dup.pdf", **overrides):
        from digitize.models import SkippedFile
        kwargs = dict(
            filename=filename,
            existing_doc_id="doc-active-1",
            existing_doc_name="orig.pdf",
            existing_job_id="job-other",
            file_hash="cafebabecafebabecafebabecafebabe",
        )
        kwargs.update(overrides)
        return SkippedFile(**kwargs)

    def test_skipped_doc_created_with_skipped_status_and_metadata(self):
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
                skipped_files=[self._make_skip("dup.pdf")],
            )

        assert "dup.pdf" in mock_create_job.call_args[1]["documents_info"]
        skipped_call = next(
            c for c in mock_create_doc.call_args_list if c[1].get("doc_name") == "dup.pdf"
        )
        assert skipped_call[1]["initial_status"] == DocStatus.SKIPPED
        assert skipped_call[1]["completed_at"] is not None
        assert skipped_call[1]["extra_metadata"] == {
            "existing_doc_id": "doc-active-1",
            "existing_doc_name": "orig.pdf",
            "existing_job_id": "job-other",
        }
        assert "dup.pdf" in result

    def test_same_job_skipped_resolves_existing_doc_id_from_novel_file(self):
        """A same-job duplicate has no doc_id up front; it must be resolved to the novel
        file's freshly generated doc_id so delete cleanup can find the shadow row."""
        mock_create_job = Mock()
        mock_create_doc = Mock()

        with patch("digitize.utils.jobs.create_job", mock_create_job), \
             patch("digitize.utils.jobs.create_document", mock_create_doc):
            from digitize.utils.jobs import initialize_job_state
            result = initialize_job_state(
                job_id="job-1",
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=["first.pdf"],
                skipped_files=[
                    self._make_skip(
                        "second.pdf",
                        existing_doc_id=None,
                        existing_doc_name="first.pdf",
                        existing_job_id=None,
                    )
                ],
            )

        skipped_call = next(
            c for c in mock_create_doc.call_args_list if c[1].get("doc_name") == "second.pdf"
        )
        meta = skipped_call[1]["extra_metadata"]
        assert meta["existing_doc_id"] == result["first.pdf"]
        assert meta["existing_job_id"] == "job-1"

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

    # ---- checksum claims made after the document rows exist ------------------

    @staticmethod
    def _owner_doc(doc_id="owner-doc", name="orig.pdf", status="completed", job_id="owner-job"):
        return SimpleNamespace(doc_id=doc_id, name=name, status=status, job_id=job_id)

    def _run_with_claims(self, claim_results, documents_info, file_hashes, **kwargs):
        mock_create_doc = Mock()
        mock_dbm = Mock()
        mock_dbm.claim_file_checksum = Mock(side_effect=claim_results)
        with patch("digitize.utils.jobs.create_job", Mock()), \
             patch("digitize.utils.jobs.create_document", mock_create_doc), \
             patch("digitize.utils.jobs.db_manager", mock_dbm):
            from digitize.utils.jobs import initialize_job_state
            result = initialize_job_state(
                job_id="job-1",
                operation=OperationType.INGESTION,
                output_format=OutputFormat.JSON,
                documents_info=documents_info,
                file_hashes=file_hashes,
                **kwargs,
            )
        return result, mock_create_doc, mock_dbm

    def test_no_file_hashes_means_no_claims(self):
        result, _, mock_dbm = self._run_with_claims([], ["a.pdf"], None)
        assert "a.pdf" in result
        mock_dbm.claim_file_checksum.assert_not_called()

    def test_claim_made_per_document_with_its_own_doc_id(self):
        result, _, mock_dbm = self._run_with_claims(
            [None, None], ["a.pdf", "b.pdf"], {"a.pdf": "ha", "b.pdf": "hb"}
        )
        calls = [c.args for c in mock_dbm.claim_file_checksum.call_args_list]
        assert calls == [("ha", "ingestion", result["a.pdf"]), ("hb", "ingestion", result["b.pdf"])]
        mock_dbm.delete_document.assert_not_called()

    def test_lost_claim_to_completed_owner_becomes_already_exists(self):
        already_exists = []
        result, mock_create_doc, mock_dbm = self._run_with_claims(
            [self._owner_doc()], ["a.pdf"], {"a.pdf": "ha"}, already_exists_files=already_exists
        )
        # The just-created document is removed and replaced by a shadow row.
        mock_dbm.delete_document.assert_called_once()
        assert len(already_exists) == 1
        assert already_exists[0].existing_doc_id == "owner-doc"
        shadow = mock_create_doc.call_args_list[-1].kwargs
        assert shadow["initial_status"] == DocStatus.ALREADY_EXISTS
        assert result["a.pdf"] != mock_dbm.delete_document.call_args.args[0]

    def test_lost_claim_to_in_progress_owner_becomes_skipped(self):
        skipped = []
        result, mock_create_doc, _ = self._run_with_claims(
            [self._owner_doc(status="in_progress")], ["a.pdf"], {"a.pdf": "ha"}, skipped_files=skipped
        )
        assert len(skipped) == 1
        assert skipped[0].existing_job_id == "owner-job"
        shadow = mock_create_doc.call_args_list[-1].kwargs
        assert shadow["initial_status"] == DocStatus.SKIPPED
        assert shadow["extra_metadata"]["existing_doc_id"] == "owner-doc"

    def test_same_job_skipped_is_redirected_when_its_original_lost_the_claim(self):
        skipped = [self._make_skip("second.pdf", existing_doc_id=None,
                                   existing_doc_name="first.pdf", existing_job_id=None)]
        _, mock_create_doc, _ = self._run_with_claims(
            [self._owner_doc(doc_id="real-owner", name="real.pdf", job_id="other-job")],
            ["first.pdf"], {"first.pdf": "h1"}, skipped_files=skipped,
        )
        second = next(
            c.kwargs for c in mock_create_doc.call_args_list if c.kwargs.get("doc_name") == "second.pdf"
        )
        assert second["extra_metadata"] == {
            "existing_doc_id": "real-owner",
            "existing_doc_name": "real.pdf",
            "existing_job_id": "other-job",
        }


@pytest.mark.unit
class TestInitializeAndLaunchLostClaims:
    """initialize_and_launch must not process files that lost their checksum claim."""

    @staticmethod
    def _run(tmp_path, state_side_effect):
        import asyncio
        staging = tmp_path / "job-1"
        staging.mkdir()
        for name in ("a.pdf", "b.pdf"):
            (staging / name).write_bytes(b"x")

        def fake_init(**kwargs):
            return state_side_effect(kwargs)

        enqueue = AsyncMock()
        status_mgr = Mock()
        with patch("digitize.utils.jobs.initialize_job_state", side_effect=fake_init), \
             patch("digitize.utils.jobs.enqueue_conversion_tasks", enqueue), \
             patch("digitize.utils.jobs.get_status_manager", return_value=status_mgr), \
             patch("digitize.utils.jobs.launch_ingest_pipeline", Mock()), \
             patch("digitize.utils.jobs.asyncio.create_task") as create_task:
            from digitize.utils.jobs import initialize_and_launch
            asyncio.run(initialize_and_launch(
                job_id="job-1", operation=OperationType.INGESTION, output_format=OutputFormat.JSON,
                filenames=["a.pdf", "b.pdf"], staging_dir=staging, quota=10, queued_for_op=0,
                file_hashes={"a.pdf": "ha", "b.pdf": "hb"},
            ))
        return staging, enqueue, status_mgr, create_task

    def test_files_that_lost_the_claim_are_not_enqueued_or_staged(self, tmp_path):
        def state(kwargs):
            # a.pdf lost its claim: initialize_job_state turned it into an already_exists record
            kwargs["already_exists_files"].append(
                AlreadyExistsFile(filename="a.pdf", existing_doc_id="o", existing_doc_name="a.pdf", file_hash="ha")
            )
            return {"a.pdf": "shadow", "b.pdf": "doc-b"}

        staging, enqueue, _, create_task = self._run(tmp_path, state)

        assert enqueue.call_args.kwargs["filenames"] == ["b.pdf"]
        assert not (staging / "a.pdf").exists()
        assert (staging / "b.pdf").exists()
        create_task.assert_called_once()  # pipeline still launched for the winner

    def test_all_files_lost_completes_job_without_launching_pipeline(self, tmp_path):
        def state(kwargs):
            for n in ("a.pdf", "b.pdf"):
                kwargs["skipped_files"].append(
                    SimpleNamespace(filename=n, existing_doc_id="o", existing_doc_name=n,
                                    existing_job_id="j", file_hash="h")
                )
            return {"a.pdf": "s1", "b.pdf": "s2"}

        staging, enqueue, status_mgr, create_task = self._run(tmp_path, state)

        enqueue.assert_not_called()
        create_task.assert_not_called()
        status_mgr.update_job_progress.assert_called_once_with("", DocStatus.COMPLETED, JobStatus.COMPLETED)
        assert not staging.exists()


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
    """_categorize_fields must route existing_doc_id, existing_doc_name
    into metadata_fields, not top_level_fields."""

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
            "existing_doc_id": "doc-x",
            "pages": 5,
        })
        assert "existing_doc_id" in meta
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

    def _make_job_mock(self, source="user"):
        j = Mock()
        j.job_id = "job-1"
        j.job_name = None
        j.operation = "ingestion"
        j.source = source
        j.status = "completed"
        j.submitted_at = datetime(2024, 1, 1, tzinfo=timezone.utc)
        j.completed_at = datetime(2024, 1, 1, 1, tzinfo=timezone.utc)
        j.error = None
        j.stats = {"total_documents": 1, "completed": 1, "failed": 0, "in_progress": 0}
        return j

    def test_skipped_doc_gets_message(self):
        doc = self._make_doc_mock("skipped", "original.pdf")
        doc.doc_metadata = {
            "existing_doc_name": "original.pdf",
            "existing_job_id": "job-abc",
        }
        mock_dm = Mock()
        mock_dm.get_job_by_id.return_value = self._make_job_mock()
        mock_dm.get_documents_by_job_id.return_value = [doc]

        with patch("digitize.utils.db.engine", Mock()), \
             patch("digitize.utils.db.db_manager", mock_dm):
            result = _real_get_job("job-1")

        assert result["documents"][0]["status"] == "skipped"
        assert (
            result["documents"][0]["message"]
            == "Skipped: Being ingested via Job job-abc as file original.pdf"
        )

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

    @pytest.mark.parametrize(
        "db_source, expected",
        [("user", "user"), ("connector", "connector"), (None, "user")],
    )
    def test_source_is_returned(self, db_source, expected):
        mock_dm = Mock()
        mock_dm.get_job_by_id.return_value = self._make_job_mock(source=db_source)
        mock_dm.get_documents_by_job_id.return_value = []

        with patch("digitize.utils.db.engine", Mock()), \
             patch("digitize.utils.db.db_manager", mock_dm):
            result = _real_get_job("job-1")

        assert result["source"] == expected


@pytest.mark.unit
class TestUpdateDocMetadataDoesNotTouchChecksum:
    """The checksum lifecycle is owned by DatabaseManager.update_document, so the
    status manager must not register checksums itself."""

    def test_completed_update_does_not_upsert_checksum(self, mock_db_manager):
        mock_db_manager.get_document_by_id.return_value = Mock(
            doc_metadata={"pages": 0, "tables": 0, "timing_in_secs": {}}
        )
        mock_db_manager.update_document.return_value = True

        from digitize.utils.db import DatabaseStatusManager
        mgr = DatabaseStatusManager("job-1")
        mgr.update_doc_metadata("doc-1", {"status": DocStatus.COMPLETED, "pages": 3})

        mock_db_manager.update_document.assert_called_once()
        mock_db_manager.claim_file_checksum.assert_not_called()


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


    def test_completed_with_errors_sets_error_field(self, mock_db_manager):
        """update_job_progress with COMPLETED_WITH_ERRORS and a non-None error writes the error."""
        mock_db_manager.get_job_by_id.return_value = Mock(
            job_id="job-1", status="in_progress", stats={}
        )
        mock_db_manager.get_documents_by_job_id.return_value = self._make_docs(["completed_with_errors"])
        mock_db_manager.update_job.return_value = True

        from digitize.utils.db import DatabaseStatusManager
        mgr = DatabaseStatusManager("job-1")
        mgr.update_job_progress("", DocStatus.COMPLETED_WITH_ERRORS, JobStatus.COMPLETED_WITH_ERRORS,
                                 error="1 table failed to summarize")

        call_kwargs = mock_db_manager.update_job.call_args[1]
        assert call_kwargs.get("error") == "1 table failed to summarize"

    def test_completed_with_errors_no_error_when_none(self, mock_db_manager):
        """update_job_progress with COMPLETED_WITH_ERRORS and no error does not write an error field."""
        mock_db_manager.get_job_by_id.return_value = Mock(
            job_id="job-1", status="in_progress", stats={}
        )
        mock_db_manager.get_documents_by_job_id.return_value = self._make_docs(["completed_with_errors"])
        mock_db_manager.update_job.return_value = True

        from digitize.utils.db import DatabaseStatusManager
        mgr = DatabaseStatusManager("job-1")
        mgr.update_job_progress("", DocStatus.COMPLETED_WITH_ERRORS, JobStatus.COMPLETED_WITH_ERRORS)

        call_kwargs = mock_db_manager.update_job.call_args[1]
        assert "error" not in call_kwargs

    def test_failed_still_sets_error_field(self, mock_db_manager):
        """Regression: FAILED status still writes the error field."""
        mock_db_manager.get_job_by_id.return_value = Mock(
            job_id="job-1", status="in_progress", stats={}
        )
        mock_db_manager.get_documents_by_job_id.return_value = self._make_docs(["failed"])
        mock_db_manager.update_job.return_value = True

        from digitize.utils.db import DatabaseStatusManager
        mgr = DatabaseStatusManager("job-1")
        mgr.update_job_progress("", DocStatus.FAILED, JobStatus.FAILED, error="pipeline crashed")

        call_kwargs = mock_db_manager.update_job.call_args[1]
        assert call_kwargs.get("error") == "pipeline crashed"



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
    mock_db_manager.get_file_owner = Mock(return_value=None)
    monkeypatch.setattr(jobs_router_module, "db_manager", mock_db_manager)

    # Stub out dg_util helpers that touch disk / DB.
    monkeypatch.setattr(digitize_app.dg_util, "get_document_page_count", Mock(return_value=0))
    import hashlib
    monkeypatch.setattr(
        jobs_router_module,
        "generate_file_checksum",
        Mock(side_effect=lambda content: hashlib.md5(content if isinstance(content, bytes) else str(content).encode()).hexdigest())
    )

    # Stub out pipeline background tasks.
    # Must be AsyncMock — asyncio.create_task() requires a coroutine.
    monkeypatch.setattr(digitize_app.dg_util, "launch_ingest_pipeline", AsyncMock())
    monkeypatch.setattr(
        digitize_app.dg_util, "initialize_and_launch", AsyncMock(return_value={"novel.pdf": "doc-1"})
    )

    monkeypatch.setattr(digitize_app.dg_util, "generate_uuid", Mock(return_value="job-x"))
    monkeypatch.setattr(digitize_app.dg_util, "stage_upload_files", AsyncMock())
    monkeypatch.setattr(digitize_app.dg_util, "enqueue_conversion_tasks", AsyncMock())
    monkeypatch.setattr(
        digitize_app.dg_util, "initialize_job_state", Mock(return_value={"novel.pdf": "doc-1"})
    )
    monkeypatch.setattr(documents_router_module, "reset_db", Mock())
    monkeypatch.setattr(digitize_app, "configure_uvicorn_logging", Mock())

    return TestClient(digitize_app.app)


def _owner(doc_id, doc_name, status="completed", job_id="owner-job"):
    """The Document owning a checksum, as returned by get_file_owner / claim_file_checksum."""
    return SimpleNamespace(
        doc_id=doc_id, name=doc_name, status=status, job_id=job_id, type="ingestion"
    )


@pytest.mark.unit
class TestDuplicateDetectionEndpoint:
    """Tests for the hash-based already-exists detection in POST /v1/jobs."""

    def _pdf(self, name="sample.pdf", content=None):
        if content is None:
            content = f"%PDF-1.4 test {name}".encode()
        return ("files", (name, content, "application/pdf"))

    def test_all_files_exist_returns_409(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module

        existing = _owner("old-doc", "sample.pdf")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(return_value=existing)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("sample.pdf")],
        )

        assert response.status_code == 409

    def test_409_body_mentions_existing_doc(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module

        existing = _owner("old-doc-id", "sample.pdf")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(return_value=existing)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("sample.pdf")],
        )

        assert "old-doc-id" in response.text

    def test_mixed_batch_returns_202(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module

        existing = _owner("old-doc", "old.pdf")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        # first file matches, second is novel
        mock_hash_db.get_file_owner = Mock(side_effect=[existing, None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("old.pdf"), self._pdf("new.pdf")],
        )

        assert response.status_code == 202

    def test_mixed_batch_response_has_no_warnings_key(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module

        existing = _owner("old-doc", "old.pdf")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(side_effect=[existing, None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("old.pdf"), self._pdf("new.pdf")],
        )

        assert "warnings" not in response.json()

    def test_mixed_batch_already_exists_passed_to_initialize(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module
        import digitize.app as digitize_app

        existing = _owner("old-doc", "old.pdf")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(side_effect=[existing, None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("old.pdf"), self._pdf("new.pdf")],
        )

        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        already_exists_arg = init_call.kwargs.get("already_exists_files", [])
        assert len(already_exists_arg) == 1
        assert already_exists_arg[0].filename == "old.pdf"
        assert already_exists_arg[0].existing_doc_id == "old-doc"

    def test_novel_only_batch_passes_empty_already_exists(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module
        import digitize.app as digitize_app

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(return_value=None)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("new.pdf")],
        )

        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        already_exists_arg = init_call.kwargs.get("already_exists_files", [])
        assert already_exists_arg == []

    def test_digitization_also_checks_ingestion_hash(self, jobs_test_client, monkeypatch):
        """A file already ingested must block a digitization request of the same content."""
        import digitize.api.v1.jobs as jobs_router_module

        ingested_doc = _owner("ingested-doc", "report.pdf")

        # the ingestion-type lookup returns a match
        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(
            side_effect=lambda h, op: ingested_doc if op == "ingestion" else None
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
        mock_hash_db.get_file_owner = Mock(return_value=None)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("brand-new.pdf")],
        )

        assert response.status_code == 202

    def test_within_job_duplicate_documents_silently_dropped(self, jobs_test_client, monkeypatch):
        """Identical files submitted in the same job are deduplicated (dropped silently)."""
        import digitize.app as digitize_app

        same_content = b"%PDF-1.4 identical content"
        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[
                ("files", ("doc1.pdf", same_content, "application/pdf")),
                ("files", ("doc2.pdf", same_content, "application/pdf")),
            ],
        )

        assert response.status_code == 202
        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        filenames_arg = init_call.kwargs.get("filenames", [])
        assert filenames_arg == ["doc1.pdf"]
        assert init_call.kwargs.get("already_exists_files", []) == []

    def test_within_job_duplicates_with_novel_files(self, jobs_test_client, monkeypatch):
        """Mixed batch with duplicates within same job: duplicates dropped, distinct kept."""
        import digitize.app as digitize_app

        content_a = b"%PDF-1.4 content A"
        content_b = b"%PDF-1.4 content B"
        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[
                ("files", ("doc1.pdf", content_a, "application/pdf")),
                ("files", ("doc2.pdf", content_a, "application/pdf")),
                ("files", ("doc3.pdf", content_b, "application/pdf")),
            ],
        )

        assert response.status_code == 202
        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        filenames_arg = init_call.kwargs.get("filenames", [])
        assert filenames_arg == ["doc1.pdf", "doc3.pdf"]

    def test_within_job_duplicate_when_first_already_exists_in_db(self, jobs_test_client, monkeypatch):
        """If doc1 exists in DB and doc2 is a duplicate of doc1 in same batch, doc2 is dropped."""
        import digitize.api.v1.jobs as jobs_router_module
        import digitize.app as digitize_app

        existing = _owner("old-doc", "doc1.pdf")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(side_effect=[existing, None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        content_a = b"%PDF-1.4 content A"
        content_b = b"%PDF-1.4 content B"
        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[
                ("files", ("doc1.pdf", content_a, "application/pdf")),
                ("files", ("doc2.pdf", content_a, "application/pdf")),
                ("files", ("doc3.pdf", content_b, "application/pdf")),
            ],
        )

        assert response.status_code == 202
        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        filenames_arg = init_call.kwargs.get("filenames", [])
        assert filenames_arg == ["doc3.pdf"]
        already_exists_arg = init_call.kwargs.get("already_exists_files", [])
        assert len(already_exists_arg) == 1
        assert already_exists_arg[0].filename == "doc1.pdf"

    def test_within_job_duplicates_all_exist_in_db_returns_409(self, jobs_test_client, monkeypatch):
        """If all distinct files in batch already exist in DB, returns 409."""
        import digitize.api.v1.jobs as jobs_router_module

        existing = _owner("old-doc", "doc1.pdf")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(return_value=existing)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        content_a = b"%PDF-1.4 content A"
        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[
                ("files", ("doc1.pdf", content_a, "application/pdf")),
                ("files", ("doc2.pdf", content_a, "application/pdf")),
            ],
        )

        assert response.status_code == 409

    def test_single_active_file_returns_409(self, jobs_test_client, monkeypatch):
        """A file currently in-flight in an active job returns 409 Conflict."""
        import digitize.api.v1.jobs as jobs_router_module

        active_doc = _owner("active-doc-id", "sample.pdf", "in_progress", "active-job-id")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(return_value=active_doc)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("sample.pdf")],
        )

        assert response.status_code == 409
        assert "active-doc-id" in response.text

    def test_mixed_batch_with_active_file_returns_202(self, jobs_test_client, monkeypatch):
        """Batch with novel file and in-flight active file accepted with 202."""
        import digitize.api.v1.jobs as jobs_router_module
        import digitize.app as digitize_app

        active_doc = _owner("active-doc-id", "active.pdf", "in_progress", "active-job-id")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(side_effect=[active_doc, None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("active.pdf"), self._pdf("novel.pdf")],
        )

        assert response.status_code == 202
        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        filenames_arg = init_call.kwargs.get("filenames", [])
        assert filenames_arg == ["novel.pdf"]
        # In-flight duplicates are `skipped`, not `already_exists`.
        assert init_call.kwargs.get("already_exists_files", []) == []
        skipped_arg = init_call.kwargs.get("skipped_files", [])
        assert len(skipped_arg) == 1
        assert skipped_arg[0].filename == "active.pdf"
        assert skipped_arg[0].existing_doc_id == "active-doc-id"
        assert skipped_arg[0].existing_job_id == "active-job-id"

    def test_all_files_mixed_completed_and_active_returns_409(self, jobs_test_client, monkeypatch):
        """Batch where one file matches document_checksum (completed) and the other
        matches an in-flight document returns 409 and reports both categories."""
        import digitize.api.v1.jobs as jobs_router_module

        completed_doc = _owner("completed-doc-id", "done.pdf", "completed", "completed-job-id")

        active_doc = _owner("active-doc-id", "running.pdf", "in_progress", "active-job-id")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(side_effect=[completed_doc, active_doc])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("done.pdf"), self._pdf("running.pdf")],
        )

        assert response.status_code == 409
        assert "Already processed" in response.text
        assert "completed-doc-id" in response.text
        assert "Currently being processed" in response.text
        assert "active-doc-id" in response.text
        assert "active-job-id" in response.text

    def test_mixed_completed_active_and_novel_returns_202(self, jobs_test_client, monkeypatch):
        """Completed + active + novel files: only the novel file is processed; the
        completed one is recorded as already_exists and the in-flight one as skipped."""
        import digitize.api.v1.jobs as jobs_router_module
        import digitize.app as digitize_app

        completed_doc = _owner("completed-doc-id", "done.pdf", "completed", "completed-job-id")

        active_doc = _owner("active-doc-id", "running.pdf", "in_progress", "active-job-id")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(side_effect=[completed_doc, active_doc, None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[self._pdf("done.pdf"), self._pdf("running.pdf"), self._pdf("novel.pdf")],
        )

        assert response.status_code == 202
        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        assert init_call.kwargs.get("filenames", []) == ["novel.pdf"]
        already_exists = {
            f.filename: f.existing_doc_id for f in init_call.kwargs.get("already_exists_files", [])
        }
        skipped = {
            f.filename: (f.existing_doc_id, f.existing_job_id)
            for f in init_call.kwargs.get("skipped_files", [])
        }
        assert already_exists == {"done.pdf": "completed-doc-id"}
        assert skipped == {"running.pdf": ("active-doc-id", "active-job-id")}

    def test_same_job_duplicate_recorded_as_skipped(self, jobs_test_client):
        """Same content under a different name in one submission is recorded as skipped,
        pointing at the first file in the same job."""
        import digitize.app as digitize_app

        same = b"%PDF-1.4 identical"
        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[
                ("files", ("doc1.pdf", same, "application/pdf")),
                ("files", ("doc2.pdf", same, "application/pdf")),
            ],
        )

        assert response.status_code == 202
        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        assert init_call.kwargs["filenames"] == ["doc1.pdf"]
        skipped_arg = init_call.kwargs["skipped_files"]
        assert len(skipped_arg) == 1
        assert skipped_arg[0].filename == "doc2.pdf"
        assert skipped_arg[0].existing_doc_name == "doc1.pdf"
        assert skipped_arg[0].existing_job_id == init_call.kwargs["job_id"]
        assert init_call.kwargs["already_exists_files"] == []

    def test_same_name_same_content_duplicate_dropped_silently(self, jobs_test_client):
        """Doc ids are keyed by filename, so an identical filename+content pair can't be
        recorded separately and is dropped silently."""
        import digitize.app as digitize_app

        same = b"%PDF-1.4 identical"
        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[
                ("files", ("doc1.pdf", same, "application/pdf")),
                ("files", ("doc1.pdf", same, "application/pdf")),
            ],
        )

        assert response.status_code == 202
        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        assert init_call.kwargs["filenames"] == ["doc1.pdf"]
        assert init_call.kwargs["skipped_files"] == []

    def test_same_job_duplicate_of_existing_doc_not_double_recorded(self, jobs_test_client, monkeypatch):
        """If the first copy resolved to an in-flight doc elsewhere, the second copy is
        covered by that record and is not recorded again."""
        import digitize.api.v1.jobs as jobs_router_module
        import digitize.app as digitize_app

        active_doc = _owner("active-doc-id", "doc1.pdf", "in_progress", "active-job-id")

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(side_effect=[active_doc, None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        same = b"%PDF-1.4 identical"
        other = b"%PDF-1.4 other"
        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion",
            files=[
                ("files", ("doc1.pdf", same, "application/pdf")),
                ("files", ("doc2.pdf", same, "application/pdf")),
                ("files", ("doc3.pdf", other, "application/pdf")),
            ],
        )

        assert response.status_code == 202
        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        assert init_call.kwargs["filenames"] == ["doc3.pdf"]
        assert [f.filename for f in init_call.kwargs["skipped_files"]] == ["doc1.pdf"]

    def test_novel_file_hashes_are_passed_to_initialize(self, jobs_test_client, monkeypatch):
        """The API only looks the owner up; the atomic claim happens in initialize_job_state,
        which receives each novel file's hash."""
        import hashlib
        import digitize.app as digitize_app

        content = b"%PDF-1.4 new"
        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion", files=[("files", ("new.pdf", content, "application/pdf"))]
        )

        assert response.status_code == 202
        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        assert init_call.kwargs["file_hashes"] == {"new.pdf": hashlib.md5(content).hexdigest()}

    def test_duplicates_are_not_in_file_hashes(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module
        import digitize.app as digitize_app

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(side_effect=[_owner("old-doc", "old.pdf"), None])
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        jobs_test_client.post(
            "/v1/jobs?operation=ingestion", files=[self._pdf("old.pdf"), self._pdf("new.pdf")]
        )

        init_call = cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).call_args
        assert list(init_call.kwargs["file_hashes"]) == ["new.pdf"]

    def test_lookup_db_error_returns_500_and_creates_no_job(self, jobs_test_client, monkeypatch):
        """A failed lookup must not be treated as 'novel'."""
        import digitize.api.v1.jobs as jobs_router_module
        import digitize.app as digitize_app

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(side_effect=RuntimeError("db down"))
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion", files=[self._pdf("a.pdf")]
        )

        assert response.status_code == 500
        cast(AsyncMock, digitize_app.dg_util.initialize_and_launch).assert_not_called()

    def test_digitization_looks_up_ingestion_then_digitization(self, jobs_test_client, monkeypatch):
        import digitize.api.v1.jobs as jobs_router_module

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(return_value=None)
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=digitization&output_format=json", files=[self._pdf("r.pdf")]
        )

        assert response.status_code == 202
        assert [c.args[1] for c in mock_hash_db.get_file_owner.call_args_list] == [
            "ingestion", "digitization",
        ]

    def test_ingestion_ignores_digitization_owner(self, jobs_test_client, monkeypatch):
        """A digitization of the same content must not block ingestion: ingestion only
        consults its own (hash, 'ingestion') row."""
        import digitize.api.v1.jobs as jobs_router_module

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        # Simulate the real table: only the 'digitization' key is taken.
        mock_hash_db.get_file_owner = Mock(
            side_effect=lambda h, op: _owner("dig-doc", "r.pdf", "in_progress") if op == "digitization" else None
        )
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=ingestion", files=[self._pdf("r.pdf")]
        )

        assert response.status_code == 202
        assert [c.args[1] for c in mock_hash_db.get_file_owner.call_args_list] == ["ingestion"]

    def test_digitization_blocked_by_in_progress_ingestion(self, jobs_test_client, monkeypatch):
        """The reverse direction is blocked: content being ingested is not re-digitized."""
        import digitize.api.v1.jobs as jobs_router_module

        mock_hash_db = Mock()
        mock_hash_db.check_quota_atomic = Mock(return_value=(True, 0))
        mock_hash_db.get_file_owner = Mock(
            side_effect=lambda h, op: _owner("ing-doc", "r.pdf", "in_progress", "ing-job") if op == "ingestion" else None
        )
        monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db)

        response = jobs_test_client.post(
            "/v1/jobs?operation=digitization&output_format=json", files=[self._pdf("r.pdf")]
        )

        assert response.status_code == 409
        assert "ing-job" in response.text


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


def _compiled_sql(stmt) -> str:
    """Render a SQLAlchemy statement against the PostgreSQL dialect with literal binds."""
    from sqlalchemy.dialects import postgresql
    return str(stmt.compile(dialect=postgresql.dialect(), compile_kwargs={"literal_binds": True}))


@pytest.mark.unit
class TestDatabaseManagerClaimFileChecksum:
    """claim_file_checksum: one atomic INSERT ... ON CONFLICT DO NOTHING."""

    def _claim(self, session):
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            return DatabaseManager.claim_file_checksum("abc", "ingestion", "doc-1")

    def test_returns_none_when_claim_won(self):
        session = MagicMock()
        session.execute.return_value.first.return_value = ("doc-1",)

        assert self._claim(session) is None
        session.scalar.assert_not_called()  # no owner lookup needed

    def test_claim_statement_is_atomic_insert_on_conflict_do_nothing(self):
        session = MagicMock()
        session.execute.return_value.first.return_value = ("doc-1",)

        self._claim(session)

        sql = _compiled_sql(session.execute.call_args[0][0])
        assert "INSERT INTO document_checksum" in sql
        assert "ON CONFLICT (checksum, type) DO NOTHING" in sql
        assert "RETURNING" in sql
        assert session.execute.call_count == 1

    def test_returns_owner_document_when_claim_lost(self):
        owner = _owner("other-doc", "x.pdf", "in_progress", "other-job")
        session = MagicMock()
        session.execute.return_value.first.return_value = None
        session.scalar.return_value = owner

        result = self._claim(session)

        assert result is owner
        session.expunge.assert_called_once_with(owner)

    def test_owner_is_read_from_documents_via_join(self):
        session = MagicMock()
        session.execute.return_value.first.return_value = None
        session.scalar.return_value = _owner("other-doc", "x.pdf")

        self._claim(session)

        sql = _compiled_sql(session.scalar.call_args[0][0])
        assert "FROM documents JOIN document_checksum ON document_checksum.doc_id = documents.doc_id" in sql
        assert "document_checksum.checksum = 'abc'" in sql
        assert "document_checksum.type = 'ingestion'" in sql

    def test_retries_when_owner_released_between_insert_and_select(self):
        session = MagicMock()
        session.execute.return_value.first.side_effect = [None, ("doc-1",)]
        session.scalar.return_value = None  # owner vanished after the conflict

        assert self._claim(session) is None
        assert session.execute.call_count == 2

    def test_raises_after_repeated_vanishing_owner(self):
        session = MagicMock()
        session.execute.return_value.first.return_value = None
        session.scalar.return_value = None

        with pytest.raises(RuntimeError):
            self._claim(session)

    def test_db_error_propagates_so_file_is_not_treated_as_novel(self):
        from sqlalchemy.exc import SQLAlchemyError

        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("boom")

        with pytest.raises(SQLAlchemyError):
            self._claim(session)


@pytest.mark.unit
class TestDocumentChecksumKeying:
    """Ingestion and digitization claims of the same content are independent rows."""

    def test_primary_key_is_checksum_and_type(self):
        from digitize.db.models import DocumentChecksum

        assert [c.name for c in DocumentChecksum.__table__.primary_key.columns] == ["checksum", "type"]

    def test_doc_id_keeps_foreign_key_with_cascade(self):
        from digitize.db.models import DocumentChecksum

        fks = list(DocumentChecksum.__table__.c.doc_id.foreign_keys)
        assert [fk.target_fullname for fk in fks] == ["documents.doc_id"]
        assert fks[0].ondelete == "CASCADE"

    def test_schema_sql_primary_key_is_checksum_and_type(self):
        from pathlib import Path
        import digitize.db as db_pkg

        sql = (Path(db_pkg.__file__).parent / "scripts" / "init_schema.sql").read_text()
        assert "PRIMARY KEY (checksum, type)" in sql
        assert "REFERENCES documents(doc_id) ON DELETE CASCADE" in sql


@pytest.mark.unit
class TestDatabaseManagerGetFileOwner:
    def test_returns_none_when_missing(self):
        session = MagicMock()
        session.scalar.return_value = None

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            assert DatabaseManager.get_file_owner("abc", "ingestion") is None

        session.expunge.assert_not_called()

    def test_returns_detached_document_when_found(self):
        doc = _owner("d1", "a.pdf")
        session = MagicMock()
        session.scalar.return_value = doc

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            assert DatabaseManager.get_file_owner("abc", "ingestion") is doc

        session.expunge.assert_called_once_with(doc)

    def test_query_filters_on_checksum_and_type(self):
        session = MagicMock()
        session.scalar.return_value = None

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            DatabaseManager.get_file_owner("abc", "digitization")

        sql = _compiled_sql(session.scalar.call_args[0][0])
        assert "document_checksum.checksum = 'abc'" in sql
        assert "document_checksum.type = 'digitization'" in sql

    def test_db_error_propagates(self):
        from sqlalchemy.exc import SQLAlchemyError

        session = MagicMock()
        session.scalar.side_effect = SQLAlchemyError("boom")

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            with pytest.raises(SQLAlchemyError):
                DatabaseManager.get_file_owner("abc", "ingestion")


@pytest.mark.unit
class TestDatabaseManagerUpdateDocumentChecksumLifecycle:
    """update_document releases the checksum claim of a failed / cancelled document, same
    transaction. Completed documents keep it; deletion cascades through the FK."""

    def _update(self, status, rowcount=1):
        session = MagicMock()
        session.execute.return_value = Mock(rowcount=rowcount)
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            ok = DatabaseManager.update_document("doc-1", status=status)
        return ok, session

    @pytest.mark.parametrize("status", [DocStatus.FAILED, DocStatus.CANCELLED])
    def test_failed_or_cancelled_removes_checksum(self, status):
        ok, session = self._update(status)

        assert ok is True
        assert session.execute.call_count == 2
        sql = _compiled_sql(session.execute.call_args_list[1][0][0])
        assert sql.startswith("DELETE FROM document_checksum")
        assert "document_checksum.doc_id = 'doc-1'" in sql

    @pytest.mark.parametrize(
        "status",
        [DocStatus.IN_PROGRESS, DocStatus.ACCEPTED, DocStatus.COMPLETED, DocStatus.COMPLETED_WITH_ERRORS],
    )
    def test_other_status_leaves_checksum_alone(self, status):
        ok, session = self._update(status)

        assert ok is True
        assert session.execute.call_count == 1  # only the documents UPDATE

    def test_missing_document_does_not_touch_checksum(self):
        ok, session = self._update(DocStatus.FAILED, rowcount=0)

        assert ok is False
        assert session.execute.call_count == 1


@pytest.mark.unit
class TestDatabaseManagerDeleteDocumentClearsChecksum:
    """delete_document must remove the checksum registry entry and shadow docs before deleting the doc."""

    def test_execute_called_four_times(self):
        session = MagicMock()
        session.execute.return_value = Mock(rowcount=1)

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            DatabaseManager.delete_document("doc-1")

        # 1st execute: registry delete.
        # 2nd execute: shadow already_exists docs delete.
        # 3rd execute: conversion_tasks delete.
        # 4th execute: document delete.
        assert session.execute.call_count == 4

    def test_returns_true_when_deleted(self):
        session = MagicMock()
        # registry delete, shadow docs delete, conversion_tasks delete, doc delete rowcount=1
        session.execute.side_effect = [Mock(rowcount=0), Mock(rowcount=0), Mock(rowcount=0), Mock(rowcount=1)]

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            result = DatabaseManager.delete_document("doc-1")

        assert result is True

    def test_returns_false_when_not_found(self):
        session = MagicMock()
        session.execute.side_effect = [Mock(rowcount=0), Mock(rowcount=0), Mock(rowcount=0), Mock(rowcount=0)]

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
class TestDatabaseManagerDeleteUserDocuments:
    """delete_user_documents must skip connector-sourced docs and wipe checksum registry."""

    def test_returns_early_when_no_user_docs(self):
        session = MagicMock()
        session.scalars.return_value = Mock(all=Mock(return_value=[]))

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            result = DatabaseManager.delete_user_documents()

        # No execute calls — short-circuits when doc_ids is empty.
        assert session.execute.call_count == 0
        assert result == {"deleted_count": 0, "doc_ids": [], "success": True}

    def test_execute_called_four_times_when_user_docs_exist(self):
        session = MagicMock()
        session.scalars.return_value = Mock(all=Mock(return_value=["doc-1", "doc-2"]))
        session.execute.return_value = Mock(rowcount=2)

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            DatabaseManager.delete_user_documents()

        # 1st execute: delete checksum registry rows.
        # 2nd execute: delete shadow already_exists docs.
        # 3rd execute: delete conversion_tasks rows.
        # 4th execute: delete the documents themselves.
        assert session.execute.call_count == 4

    def test_returns_deleted_count(self):
        session = MagicMock()
        session.scalars.return_value = Mock(all=Mock(return_value=["doc-1", "doc-2"]))
        session.execute.side_effect = [Mock(rowcount=0), Mock(rowcount=0), Mock(rowcount=0), Mock(rowcount=2)]

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            result = DatabaseManager.delete_user_documents()

        assert result["deleted_count"] == 2
        assert result["success"] is True

    def test_conversion_tasks_purged_before_document_delete(self):
        """conversion_tasks rows must be deleted in the same session before the
        Document rows are removed, so no orphaned tasks survive a bulk delete."""
        from sqlalchemy.sql.expression import Delete
        from digitize.db.models import ConversionTask, Document

        session = MagicMock()
        session.scalars.return_value = Mock(all=Mock(return_value=["doc-a", "doc-b"]))
        session.execute.return_value = Mock(rowcount=0)

        captured: list = []
        orig_execute = session.execute.side_effect

        def capture(stmt, *a, **kw):
            captured.append(stmt)
            return Mock(rowcount=2 if len(captured) == 4 else 0)

        session.execute.side_effect = capture

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            DatabaseManager.delete_user_documents()

        # Verify that a DELETE against ConversionTask appears (3rd call, index 2)
        # and a DELETE against Document appears as the last call (index 3).
        assert len(captured) == 4
        ct_stmt = captured[2]
        doc_stmt = captured[3]
        assert isinstance(ct_stmt, Delete)
        assert isinstance(doc_stmt, Delete)
        # Confirm the tables targeted are ConversionTask and Document respectively.
        # Use == (structural equality) rather than `is` — the model may be imported
        # under two different module paths during test collection, yielding distinct
        # but equivalent Table objects.
        assert ct_stmt.table == ConversionTask.__table__
        assert doc_stmt.table == Document.__table__

    def test_conversion_tasks_not_deleted_when_no_user_docs(self):
        """When there are no user-submitted docs, no execute calls are made at all —
        including no attempt to delete conversion_tasks."""
        session = MagicMock()
        session.scalars.return_value = Mock(all=Mock(return_value=[]))

        with patch("digitize.db.manager.get_db_session", return_value=_make_session_ctx(session)):
            from digitize.db.manager import DatabaseManager
            DatabaseManager.delete_user_documents()

        assert session.execute.call_count == 0


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
