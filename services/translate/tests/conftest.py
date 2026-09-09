"""
Pytest configuration and fixtures for translate service tests.

Provides comprehensive mocking to ensure tests run without a real database,
vLLM endpoint, or file-system dependency.
"""

import sys
import types
from datetime import datetime, timezone
from unittest.mock import MagicMock, Mock, patch

import pytest

# ---------------------------------------------------------------------------
# Patch the crash-handler BEFORE any app module is imported so that
# StderrMonitor.start() never replaces pytest's captured stderr fd.
# ---------------------------------------------------------------------------
import common.diagnostic_logger as _diag_logger  # noqa: E402

_diag_logger.setup_comprehensive_crash_handler = lambda logger: (
    MagicMock(),
    MagicMock(),
    MagicMock(),
)


# ---------------------------------------------------------------------------
# Auto-use fixtures: DB engine + session mocked for every test
# ---------------------------------------------------------------------------


@pytest.fixture(autouse=True)
def mock_database_engine():
    """Mock the SQLAlchemy engine to prevent real DB connections."""
    mock_engine = Mock()
    mock_engine.dispose = Mock()

    with patch("translate.db.connection.engine", mock_engine):
        yield mock_engine


@pytest.fixture(autouse=True)
def mock_db_session():
    """Mock the DB session context-manager used by the DB manager."""
    mock_session = MagicMock()
    mock_session.commit = Mock()
    mock_session.rollback = Mock()
    mock_session.close = Mock()
    mock_session.add = Mock()
    mock_session.flush = Mock()
    mock_session.scalar = Mock(return_value=None)
    mock_session.scalars = Mock(return_value=Mock(all=Mock(return_value=[])))
    mock_session.execute = Mock(return_value=Mock(rowcount=0))
    mock_session.expunge = Mock()

    with patch("translate.db.connection.get_db_session") as mock_get_session:
        mock_get_session.return_value.__enter__ = Mock(return_value=mock_session)
        mock_get_session.return_value.__exit__ = Mock(return_value=None)
        yield mock_session


# ---------------------------------------------------------------------------
# DB Manager mock
# ---------------------------------------------------------------------------


@pytest.fixture
def mock_db_manager():
    """Mock TranslateDatabaseManager singleton."""
    mock_manager = Mock()

    mock_manager.create_job = Mock(return_value=_make_db_job())
    mock_manager.get_job_by_id = Mock(return_value=None)
    mock_manager.get_all_jobs = Mock(return_value=([], 0))
    mock_manager.update_job = Mock(return_value=True)
    mock_manager.get_active_jobs = Mock(return_value=[])

    with patch("translate.db.manager.db_manager", mock_manager), patch(
        "translate.api.v1.jobs.db_manager", mock_manager
    ), patch("translate.utils.recovery.db_manager", mock_manager), patch(
        "translate.workers.translation_worker.db_manager", mock_manager
    ):
        yield mock_manager


# ---------------------------------------------------------------------------
# Concurrency manager mock
# ---------------------------------------------------------------------------


@pytest.fixture
def initialized_concurrency_manager():
    """Return a ConcurrencyManager with semaphores initialized."""
    import asyncio
    from translate.workers.concurrency import ConcurrencyManager

    mgr = ConcurrencyManager()
    # Semaphores must be created inside a running loop.
    # For sync tests we create them directly.
    mgr._job_limiter = asyncio.BoundedSemaphore(8)
    mgr._chunk_semaphore = asyncio.BoundedSemaphore(4)
    mgr._vllm_semaphore = asyncio.BoundedSemaphore(32)
    return mgr


# ---------------------------------------------------------------------------
# Sample data helpers
# ---------------------------------------------------------------------------


def _make_db_job(
    job_id: str = "test-job-123",
    status: str = "accepted",
    source_language: str = "german",
    target_language: str = "english",
    input_type: str = "txt",
    document_name: str = "test.txt",
    job_name: str = None,
    error: str = None,
    job_metadata: dict = None,
) -> Mock:
    """Build a mock TranslateJob ORM object."""
    job = Mock()
    job.job_id = job_id
    job.job_name = job_name
    job.status = status
    job.source_language = source_language
    job.target_language = target_language
    job.input_type = input_type
    job.document_name = document_name
    job.submitted_at = datetime(2024, 1, 1, 0, 0, 0, tzinfo=timezone.utc)
    job.completed_at = None
    job.error = error
    job.job_metadata = job_metadata or {}
    job.updated_at = datetime(2024, 1, 1, 0, 0, 0, tzinfo=timezone.utc)
    return job


@pytest.fixture
def sample_db_job():
    return _make_db_job()


@pytest.fixture
def completed_db_job():
    return _make_db_job(
        status="completed",
        job_metadata={"phase": "completed"},
    )


@pytest.fixture
def failed_db_job():
    return _make_db_job(
        status="failed",
        error="Something went wrong",
        job_metadata={"phase": "failed"},
    )

# Made with Bob
