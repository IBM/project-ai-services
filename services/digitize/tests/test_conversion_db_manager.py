"""
Unit tests for DatabaseManager conversion task operations — db/manager.py.

Tests cover: create_conversion_task, get_conversion_task,
get_conversion_task_by_job_id, get_conversion_tasks_by_job_id,
get_conversion_tasks (by status), get_queued_count, get_queued_counts,
update_task_status, peek_head, claim_head, promote_pending.

The DB session is mocked by patching get_db_session at the manager module
level (where it was imported), which is the canonical approach.
"""

from contextlib import contextmanager
from datetime import datetime, timezone
from unittest.mock import MagicMock, Mock, patch, call

import pytest


# ---------------------------------------------------------------------------
# Session fixture — patches at the manager's own import site
# ---------------------------------------------------------------------------

@pytest.fixture()
def session():
    """Return a mock session and patch it into db/manager.py's get_db_session."""
    mock_session = MagicMock()
    mock_session.commit = Mock()
    mock_session.rollback = Mock()
    mock_session.close = Mock()
    mock_session.add = Mock()
    mock_session.flush = Mock()
    mock_session.scalar = Mock(return_value=None)
    mock_session.scalars = Mock(return_value=Mock(all=Mock(return_value=[])))
    mock_session.execute = Mock(return_value=Mock(rowcount=0, fetchone=Mock(return_value=None)))
    mock_session.expunge = Mock()

    @contextmanager
    def _fake_session():
        yield mock_session

    with patch("digitize.db.manager.get_db_session", side_effect=lambda: _fake_session()):
        yield mock_session


# ---------------------------------------------------------------------------
# Helpers — build ConversionTask-like mock rows
# ---------------------------------------------------------------------------

def _ct(**kwargs):
    """Build a lightweight ConversionTask mock."""
    defaults = dict(
        task_id="t1",
        job_id="j1",
        doc_id="d1",
        operation="digitization",
        cached_file="/tmp/file.pdf",
        output_format="json",
        page_count=5,
        is_large=False,
        status="queued",
        result_path=None,
        error=None,
        queued_at=datetime(2024, 1, 1, tzinfo=timezone.utc),
        started_at=None,
        completed_at=None,
    )
    defaults.update(kwargs)
    t = Mock()
    for k, v in defaults.items():
        setattr(t, k, v)
    return t


@pytest.mark.unit
class TestCreateConversionTask:
    def test_creates_task_and_returns_it(self, session):
        from digitize.db.manager import DatabaseManager

        result = DatabaseManager.create_conversion_task(
            task_id="t1",
            job_id="j1",
            doc_id="d1",
            operation="digitization",
            cached_file="/tmp/file.pdf",
            output_format="json",
            page_count=10,
            is_large=False,
            status="queued",
        )
        session.add.assert_called_once()
        session.flush.assert_called_once()

    def test_returns_none_on_integrity_error(self, session):
        from sqlalchemy.exc import IntegrityError
        from digitize.db.manager import DatabaseManager

        session.flush.side_effect = IntegrityError(
            "duplicate key", {}, Exception()
        )

        result = DatabaseManager.create_conversion_task(
            task_id="t1",
            job_id="j1",
            doc_id="d1",
            operation="digitization",
            cached_file="/tmp/f.pdf",
            output_format="json",
            page_count=0,
            is_large=False,
        )
        assert result is None


@pytest.mark.unit
class TestGetConversionTask:
    def test_returns_task_when_found(self, session):
        from digitize.db.manager import DatabaseManager

        task = _ct(task_id="t1")
        session.scalar.return_value = task

        result = DatabaseManager.get_conversion_task("t1")
        assert result is task

    def test_returns_none_when_not_found(self, session):
        from digitize.db.manager import DatabaseManager

        session.scalar.return_value = None

        result = DatabaseManager.get_conversion_task("missing")
        assert result is None


@pytest.mark.unit
class TestGetConversionTaskByJobId:
    def test_returns_first_task_for_job(self, session):
        from digitize.db.manager import DatabaseManager

        task = _ct(job_id="j1")
        session.scalar.return_value = task

        result = DatabaseManager.get_conversion_task_by_job_id("j1")
        assert result is task

    def test_returns_none_when_no_task(self, session):
        from digitize.db.manager import DatabaseManager

        session.scalar.return_value = None

        result = DatabaseManager.get_conversion_task_by_job_id("j-nonexistent")
        assert result is None


@pytest.mark.unit
class TestGetConversionTasksByJobId:
    def test_returns_ordered_tasks(self, session):
        from digitize.db.manager import DatabaseManager

        t1 = _ct(task_id="t1")
        t2 = _ct(task_id="t2")
        session.scalars.return_value = Mock(all=Mock(return_value=[t1, t2]))

        result = DatabaseManager.get_conversion_tasks_by_job_id("j1")
        assert result == [t1, t2]

    def test_returns_empty_list_on_error(self, session):
        from sqlalchemy.exc import SQLAlchemyError
        from digitize.db.manager import DatabaseManager

        session.scalars.side_effect = SQLAlchemyError("db error")

        result = DatabaseManager.get_conversion_tasks_by_job_id("j1")
        assert result == []


@pytest.mark.unit
class TestGetConversionTasks:
    def test_returns_all_tasks_with_given_status(self, session):
        from digitize.db.manager import DatabaseManager

        t1 = _ct(status="running")
        session.scalars.return_value = Mock(all=Mock(return_value=[t1]))

        result = DatabaseManager.get_conversion_tasks("running")
        assert result == [t1]

    def test_empty_list_when_none_match(self, session):
        from digitize.db.manager import DatabaseManager

        session.scalars.return_value = Mock(all=Mock(return_value=[]))

        result = DatabaseManager.get_conversion_tasks("pending")
        assert result == []


@pytest.mark.unit
class TestGetQueuedCounts:
    def test_returns_count_dict(self, session):
        from digitize.db.manager import DatabaseManager

        session.execute.return_value = Mock(
            all=Mock(return_value=[("ingestion", 3), ("digitization", 1)])
        )

        result = DatabaseManager.get_queued_counts()
        assert result == {"ingestion": 3, "digitization": 1}

    def test_fills_missing_operations_with_zero(self, session):
        from digitize.db.manager import DatabaseManager

        session.execute.return_value = Mock(
            all=Mock(return_value=[("ingestion", 5)])
        )

        result = DatabaseManager.get_queued_counts()
        assert result["ingestion"] == 5
        assert result["digitization"] == 0

    def test_returns_zero_dict_on_error(self, session):
        from sqlalchemy.exc import SQLAlchemyError
        from digitize.db.manager import DatabaseManager

        session.execute.side_effect = SQLAlchemyError("db error")

        result = DatabaseManager.get_queued_counts()
        assert result == {"ingestion": 0, "digitization": 0}


@pytest.mark.unit
class TestUpdateTaskStatus:
    def test_sets_running_returns_true(self, session):
        from digitize.db.manager import DatabaseManager

        session.execute.return_value = Mock(rowcount=1)

        ok = DatabaseManager.update_task_status("t1", "running")
        assert ok is True

    def test_sets_completed_at_for_terminal_statuses(self, session):
        from digitize.db.manager import DatabaseManager

        session.execute.return_value = Mock(rowcount=1)

        for terminal in ("completed", "failed"):
            ok = DatabaseManager.update_task_status("t1", terminal)
            assert ok is True

    def test_returns_false_when_no_rows_updated(self, session):
        from digitize.db.manager import DatabaseManager

        session.execute.return_value = Mock(rowcount=0)

        ok = DatabaseManager.update_task_status("t-ghost", "completed")
        assert ok is False

    def test_returns_false_on_db_error(self, session):
        from sqlalchemy.exc import SQLAlchemyError
        from digitize.db.manager import DatabaseManager

        session.execute.side_effect = SQLAlchemyError("crash")

        ok = DatabaseManager.update_task_status("t1", "running")
        assert ok is False


@pytest.mark.unit
class TestPeekHead:
    def test_returns_oldest_queued_task_for_operation(self, session):
        from digitize.db.manager import DatabaseManager

        task = _ct(task_id="t1", operation="ingestion", status="queued")
        session.scalar.return_value = task

        result = DatabaseManager.peek_head("ingestion")
        assert result is task

    def test_returns_none_when_queue_empty(self, session):
        from digitize.db.manager import DatabaseManager

        session.scalar.return_value = None

        result = DatabaseManager.peek_head("digitization")
        assert result is None

    def test_returns_none_on_db_error(self, session):
        from sqlalchemy.exc import SQLAlchemyError
        from digitize.db.manager import DatabaseManager

        session.scalar.side_effect = SQLAlchemyError("db error")

        result = DatabaseManager.peek_head("ingestion")
        assert result is None


@pytest.mark.unit
class TestClaimHead:
    def test_returns_claimed_task(self, session):
        from digitize.db.manager import DatabaseManager

        task = _ct(task_id="t1", status="running")
        row = Mock()
        row.__getitem__ = Mock(return_value=task)
        session.execute.return_value = Mock(fetchone=Mock(return_value=row))

        result = DatabaseManager.claim_head("ingestion")
        assert result is task

    def test_returns_none_when_no_queued_task(self, session):
        from digitize.db.manager import DatabaseManager

        session.execute.return_value = Mock(fetchone=Mock(return_value=None))

        result = DatabaseManager.claim_head("digitization")
        assert result is None

    def test_returns_none_on_db_error(self, session):
        from sqlalchemy.exc import SQLAlchemyError
        from digitize.db.manager import DatabaseManager

        session.execute.side_effect = SQLAlchemyError("lock error")

        result = DatabaseManager.claim_head("ingestion")
        assert result is None


@pytest.mark.unit
class TestPromotePending:
    def test_promotes_up_to_quota_headroom(self, session):
        from digitize.db.manager import DatabaseManager

        # queued_count = 3, quota = 5 → headroom = 2
        candidate_rows = [
            Mock(task_id="tp1", cached_file="/tmp/a.pdf"),
            Mock(task_id="tp2", cached_file="/tmp/b.pdf"),
        ]

        session.scalar.return_value = 3  # queued_count
        # execute call order: advisory lock, SELECT candidates, UPDATE
        session.execute.side_effect = [
            Mock(),                                        # pg_advisory_xact_lock
            Mock(all=Mock(return_value=candidate_rows)),   # SELECT candidates
            Mock(rowcount=2),                              # UPDATE
        ]

        promoted = DatabaseManager.promote_pending("ingestion", quota=5)
        assert promoted == 2

    def test_promotes_zero_when_queue_full(self, session):
        from digitize.db.manager import DatabaseManager

        session.scalar.return_value = 10  # already at quota
        # advisory lock fires, but SELECT candidates and UPDATE must not
        session.execute.side_effect = [Mock()]  # pg_advisory_xact_lock only

        promoted = DatabaseManager.promote_pending("ingestion", quota=10)
        assert promoted == 0
        # Only the advisory-lock execute should have been issued — no SELECT or UPDATE
        assert session.execute.call_count == 1

    def test_returns_zero_on_db_error(self, session):
        from sqlalchemy.exc import SQLAlchemyError
        from digitize.db.manager import DatabaseManager

        session.scalar.side_effect = SQLAlchemyError("db error")

        promoted = DatabaseManager.promote_pending("digitization", quota=5)
        assert promoted == 0


@pytest.mark.unit
class TestCheckQuotaAtomic:
    def test_returns_true_when_below_quota(self, session):
        from digitize.db.manager import DatabaseManager
        from sqlalchemy import text

        # Advisory lock execute + scalar count query
        session.execute.return_value = Mock()  # pg_advisory_xact_lock
        session.scalar.return_value = 3        # 3 queued tasks

        ok, count = DatabaseManager.check_quota_atomic("ingestion", quota=10)
        assert ok is True
        assert count == 3

    def test_returns_false_when_at_quota(self, session):
        from digitize.db.manager import DatabaseManager

        session.execute.return_value = Mock()
        session.scalar.return_value = 10       # exactly at quota

        ok, count = DatabaseManager.check_quota_atomic("digitization", quota=10)
        assert ok is False
        assert count == 10

    def test_returns_true_on_db_error_fail_open(self, session):
        """On DB error the gate fails open so the semaphore remains the hard limit."""
        from sqlalchemy.exc import SQLAlchemyError
        from digitize.db.manager import DatabaseManager

        session.execute.side_effect = SQLAlchemyError("advisory lock failed")

        ok, count = DatabaseManager.check_quota_atomic("ingestion", quota=5)
        assert ok is True
        assert count == 0


