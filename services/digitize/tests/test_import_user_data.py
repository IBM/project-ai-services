"""
Tests for the user-data import: jobs → documents → document_checksums, in dependency
order, skip-existing.
"""

from unittest.mock import MagicMock, patch

import pytest
from pydantic import ValidationError
from sqlalchemy.dialects import postgresql

import digitize.utils.db as db_ops
from digitize.db.manager import DatabaseManager
from digitize.models import ImportRequest


def _job(job_id):
    return {
        "job_id": job_id, "operation": "ingestion", "status": "completed",
        "submitted_at": "2024-01-01T00:00:00Z", "stats": {},
    }


def _doc(doc_id, job_id="job-1"):
    return {
        "id": doc_id, "job_id": job_id, "name": f"{doc_id}.pdf", "type": "ingestion",
        "status": "completed", "output_format": "json",
        "submitted_at": "2024-01-01T00:00:00Z",
    }


def _checksum(doc_id, checksum=None):
    return {"checksum": checksum or f"sum-{doc_id}", "type": "ingestion", "doc_id": doc_id}


def _request(jobs=(), documents=(), checksums=(), validate_only=False):
    return ImportRequest(
        data={
            "jobs": list(jobs),
            "documents": list(documents),
            "document_checksums": list(checksums),
        },
        validate_only=validate_only,
    )


@pytest.fixture
def mgr():
    """A db_manager mock plus an existing-ids hook; records the call order."""
    manager = MagicMock()
    manager.calls = []
    manager.create_job.side_effect = lambda **kw: manager.calls.append(("job", kw["job_id"])) or object()
    manager.create_document.side_effect = lambda **kw: manager.calls.append(("doc", kw["doc_id"])) or object()

    def insert_checksum(checksum, doc_type, doc_id):
        manager.calls.append(("checksum", doc_id))
        return True

    manager.insert_document_checksum.side_effect = insert_checksum
    manager.existing_jobs = []
    manager.existing_docs = []

    with patch.object(db_ops, "engine", object()), \
            patch.object(db_ops, "db_manager", manager), \
            patch.object(db_ops, "get_all_job_ids", lambda: list(manager.existing_jobs)), \
            patch.object(db_ops, "get_all_document_ids", lambda: list(manager.existing_docs)):
        yield manager


@pytest.mark.unit
class TestImportRequestValidation:
    def test_checksum_only_payload_is_valid(self):
        assert _request(checksums=[_checksum("doc-1")]).data.document_checksums

    def test_empty_payload_rejected(self):
        with pytest.raises(ValidationError, match="At least one"):
            _request()

    def test_old_payload_without_checksums_still_valid(self):
        req = ImportRequest(data={"jobs": [_job("job-1")], "documents": []})
        assert req.data.document_checksums == []


@pytest.mark.unit
class TestImportDependencyOrder:
    def test_imports_jobs_then_documents_then_checksums(self, mgr):
        res = db_ops.import_metadata(
            _request(
                jobs=[_job("job-1")],
                documents=[_doc("doc-1"), _doc("doc-2")],
                checksums=[_checksum("doc-1"), _checksum("doc-2")],
            )
        )

        assert mgr.calls == [
            ("job", "job-1"),
            ("doc", "doc-1"), ("doc", "doc-2"),
            ("checksum", "doc-1"), ("checksum", "doc-2"),
        ]
        assert res.summary.jobs.imported == 1
        assert res.summary.documents.imported == 2
        assert res.summary.document_checksums.total_received == 2
        assert res.summary.document_checksums.imported == 2
        assert res.errors == [] and res.warnings == []

    def test_checksum_for_document_already_in_database(self, mgr):
        mgr.existing_jobs = ["job-1"]
        mgr.existing_docs = ["doc-1"]

        res = db_ops.import_metadata(_request(checksums=[_checksum("doc-1")]))

        assert mgr.calls == [("checksum", "doc-1")]
        assert res.summary.document_checksums.imported == 1

    def test_checksum_of_unknown_document_is_orphan_warning(self, mgr):
        res = db_ops.import_metadata(_request(checksums=[_checksum("ghost")]))

        mgr.insert_document_checksum.assert_not_called()
        assert res.summary.document_checksums.failed == 1
        assert [w.type for w in res.warnings] == ["orphaned_document_checksum"]
        assert res.warnings[0].record_type == "document_checksum"

    def test_checksum_of_document_that_failed_to_import_is_orphan(self, mgr):
        mgr.create_document.side_effect = None
        mgr.create_document.return_value = None  # document insert fails

        res = db_ops.import_metadata(
            _request(jobs=[_job("job-1")], documents=[_doc("doc-1")], checksums=[_checksum("doc-1")])
        )

        assert res.summary.documents.failed == 1
        assert res.summary.document_checksums.failed == 1
        mgr.insert_document_checksum.assert_not_called()

    def test_checksum_of_document_orphaned_by_missing_job_is_orphan(self, mgr):
        res = db_ops.import_metadata(
            _request(documents=[_doc("doc-1", job_id="missing")], checksums=[_checksum("doc-1")])
        )

        assert res.summary.documents.failed == 1
        assert res.summary.document_checksums.failed == 1
        mgr.create_document.assert_not_called()
        mgr.insert_document_checksum.assert_not_called()

    def test_existing_checksum_is_skipped(self, mgr):
        mgr.existing_jobs = ["job-1"]
        mgr.existing_docs = ["doc-1"]
        mgr.insert_document_checksum.side_effect = None
        mgr.insert_document_checksum.return_value = False

        res = db_ops.import_metadata(_request(checksums=[_checksum("doc-1")]))

        assert res.summary.document_checksums.skipped == 1
        assert res.summary.document_checksums.imported == 0

    def test_checksum_insert_error_is_reported_and_import_continues(self, mgr):
        mgr.existing_docs = ["doc-1", "doc-2"]
        results = iter([RuntimeError("boom"), True])

        def flaky(checksum, doc_type, doc_id):
            result = next(results)
            if isinstance(result, Exception):
                raise result
            return result

        mgr.insert_document_checksum.side_effect = flaky

        res = db_ops.import_metadata(_request(checksums=[_checksum("doc-1"), _checksum("doc-2")]))

        assert res.summary.document_checksums.failed == 1
        assert res.summary.document_checksums.imported == 1
        assert [e.type for e in res.errors] == ["database_error"]

    def test_validate_only_writes_nothing_but_checks_dependencies(self, mgr):
        res = db_ops.import_metadata(
            _request(
                jobs=[_job("job-1")],
                documents=[_doc("doc-1")],
                checksums=[_checksum("doc-1"), _checksum("ghost")],
                validate_only=True,
            )
        )

        mgr.create_job.assert_not_called()
        mgr.create_document.assert_not_called()
        mgr.insert_document_checksum.assert_not_called()
        assert res.summary.document_checksums.imported == 1
        assert res.summary.document_checksums.failed == 1


@pytest.mark.unit
class TestInsertDocumentChecksum:
    def _insert(self, rowcount):
        session = MagicMock()
        session.execute.return_value.rowcount = rowcount
        ctx = MagicMock()
        ctx.__enter__.return_value = session
        ctx.__exit__.return_value = None
        with patch("digitize.db.manager.get_db_session", return_value=ctx):
            return DatabaseManager.insert_document_checksum("abc", "ingestion", "doc-1"), session

    def test_inserted(self):
        inserted, _ = self._insert(rowcount=1)
        assert inserted is True

    def test_conflict_leaves_existing_row(self):
        inserted, _ = self._insert(rowcount=0)
        assert inserted is False

    def test_statement_is_insert_on_conflict_do_nothing(self):
        _, session = self._insert(rowcount=1)
        stmt = session.execute.call_args.args[0]
        sql = str(stmt.compile(dialect=postgresql.dialect()))
        assert "INSERT INTO document_checksum" in sql
        assert "ON CONFLICT DO NOTHING" in sql
