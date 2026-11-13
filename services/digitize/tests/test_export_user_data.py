"""
Tests for the user-data export: user jobs, user documents and document_checksum,
paginated as one combined sequence in dependency order.
"""

from datetime import datetime, timezone
from types import SimpleNamespace
from unittest.mock import patch

import pytest
from sqlalchemy.dialects import postgresql

import digitize.utils.db as db_ops
from digitize.db.manager import DatabaseManager


def _sql(stmt) -> str:
    return str(stmt.compile(dialect=postgresql.dialect(), compile_kwargs={"literal_binds": True}))


_TS = datetime(2024, 1, 1, tzinfo=timezone.utc)


def _job(n):
    return SimpleNamespace(
        job_id=f"job-{n}", operation="ingestion", status="completed", source="user",
        job_name=None, submitted_at=_TS, completed_at=_TS, stats={}, error=None,
    )


def _doc(n):
    return SimpleNamespace(
        doc_id=f"doc-{n}", job_id="job-1", name=f"{n}.pdf", type="ingestion",
        status="completed", source="user", output_format="json",
        submitted_at=_TS, completed_at=_TS, error=None, doc_metadata={},
    )


def _checksum(n):
    return SimpleNamespace(checksum=f"sum-{n}", type="ingestion", doc_id=f"doc-{n}")


class _FakeStore:
    """Stands in for db_manager: 2 jobs, 3 documents, 2 checksums."""

    def __init__(self):
        self.rows = {
            "jobs": [_job(1), _job(2)],
            "documents": [_doc(1), _doc(2), _doc(3)],
            "document_checksums": [_checksum(1), _checksum(2)],
        }

    def count_user_export_rows(self, entity):
        return len(self.rows[entity])

    def get_user_export_rows(self, entity, limit=None, offset=0):
        rows = self.rows[entity][offset:]
        return rows if limit is None else rows[:limit]


@pytest.fixture
def fake_store():
    store = _FakeStore()
    with patch.object(db_ops, "engine", object()), patch.object(db_ops, "db_manager", store):
        yield store


def _ids(response):
    d = response.data
    return (
        [j.job_id for j in d.jobs],
        [x.id for x in d.documents],
        [c.doc_id for c in d.document_checksums],
    )


@pytest.mark.unit
class TestUserExportQueries:
    def test_jobs_are_user_only_with_stable_order(self):
        sql = _sql(DatabaseManager._user_export_stmt("jobs"))
        assert "jobs.source = 'user'" in sql
        assert "ORDER BY jobs.submitted_at DESC, jobs.job_id" in sql

    def test_documents_are_user_only_without_audit_records(self):
        sql = _sql(DatabaseManager._user_export_stmt("documents"))
        assert "documents.source = 'user'" in sql
        assert "NOT IN ('already_exists', 'skipped')" in sql
        assert "ORDER BY documents.submitted_at DESC, documents.doc_id" in sql

    def test_document_checksums_ordered_by_primary_key(self):
        sql = _sql(DatabaseManager._user_export_stmt("document_checksums"))
        assert "FROM document_checksum" in sql
        assert "ORDER BY document_checksum.checksum, document_checksum.type" in sql

    def test_unknown_entity_rejected(self):
        with pytest.raises(ValueError):
            DatabaseManager._user_export_stmt("connectors")


@pytest.mark.unit
class TestExportPagination:
    def test_limit_minus_one_returns_everything_in_dependency_order(self, fake_store):
        res = db_ops.export_metadata(limit=-1)

        assert _ids(res) == (
            ["job-1", "job-2"],
            ["doc-1", "doc-2", "doc-3"],
            ["doc-1", "doc-2"],
        )
        assert res.pagination.total_records == 7
        assert res.pagination.returned_records == 7
        assert res.pagination.has_more is False
        assert res.summary.document_checksums.total_exported == 2

    def test_page_spanning_entities_does_not_overlap_or_skip(self, fake_store):
        pages = []
        offset = 0
        while True:
            res = db_ops.export_metadata(limit=3, offset=offset)
            pages.append(_ids(res))
            assert res.pagination.total_records == 7
            if not res.pagination.has_more:
                break
            offset += res.pagination.returned_records

        # 3 + 3 + 1 records
        assert [sum(len(part) for part in page) for page in pages] == [3, 3, 1]
        assert pages[0] == (["job-1", "job-2"], ["doc-1"], [])
        assert pages[1] == ([], ["doc-2", "doc-3"], ["doc-1"])
        assert pages[2] == ([], [], ["doc-2"])

        all_jobs = [j for p in pages for j in p[0]]
        all_docs = [d for p in pages for d in p[1]]
        all_sums = [c for p in pages for c in p[2]]
        assert all_jobs == ["job-1", "job-2"]
        assert all_docs == ["doc-1", "doc-2", "doc-3"]
        assert all_sums == ["doc-1", "doc-2"]

    def test_offset_inside_documents(self, fake_store):
        res = db_ops.export_metadata(limit=2, offset=3)

        assert _ids(res) == ([], ["doc-2", "doc-3"], [])
        assert res.pagination.has_more is True

    def test_offset_past_everything_returns_empty_page(self, fake_store):
        res = db_ops.export_metadata(limit=5, offset=7)

        assert _ids(res) == ([], [], [])
        assert res.pagination.returned_records == 0
        assert res.pagination.has_more is False

    def test_exact_fit_last_page_has_no_more(self, fake_store):
        res = db_ops.export_metadata(limit=2, offset=5)

        assert _ids(res) == ([], [], ["doc-1", "doc-2"])
        assert res.pagination.has_more is False

    @pytest.mark.parametrize("limit", [0, -2])
    def test_invalid_limit(self, fake_store, limit):
        with pytest.raises(ValueError):
            db_ops.export_metadata(limit=limit)

    def test_negative_offset(self, fake_store):
        with pytest.raises(ValueError):
            db_ops.export_metadata(limit=1, offset=-1)
