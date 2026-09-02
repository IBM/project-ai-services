"""
Unit tests for services/digitize/connectors/scheduler.py
and the connector portion of services/digitize/utils/recovery.py.

Coverage
--------
scheduler.register_connector_job
  - calls sched.add_job with correct kwargs
  - fire_immediately=True  → trigger.start_date is ~now (fires on first tick)
  - fire_immediately=False → trigger.start_date is ~now+interval (deferred)
  - raises RuntimeError when _scheduler is None

scheduler.remove_connector_job
  - calls sched.remove_job with the connector_id
  - silently handles JobLookupError (job not registered)
  - raises RuntimeError when _scheduler is None

recover_connector_sync_state
  - returns 0 and skips close when no syncing connectors
  - calls close_open_sync_log for each affected connector
  - logs warning when no open sync log row found
  - continues past per-connector errors and returns correct count
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from unittest.mock import MagicMock, AsyncMock, patch

import pytest

# ---------------------------------------------------------------------------
# Scheduler module path shortcuts
# ---------------------------------------------------------------------------
SCHED_MODULE = "digitize.connectors.scheduler"
RECOVERY_MODULE = "digitize.utils.recovery"


# ===========================================================================
# scheduler.register_connector_job
# ===========================================================================

class TestRegisterConnectorJob:
    """Tests for register_connector_job."""

    @pytest.mark.asyncio
    async def test_raises_when_scheduler_none(self):
        import digitize.connectors.scheduler as sched_mod
        original = sched_mod._scheduler
        sched_mod._scheduler = None
        try:
            with pytest.raises(RuntimeError, match="not initialised"):
                from digitize.connectors.scheduler import register_connector_job
                await register_connector_job("c1", 300)
        finally:
            sched_mod._scheduler = original

    @pytest.mark.asyncio
    async def test_add_job_deferred_when_not_fire_immediately(self):
        """fire_immediately=False → trigger.start_date is ~now + interval_seconds."""
        mock_sched = MagicMock()
        import digitize.connectors.scheduler as sched_mod
        original = sched_mod._scheduler
        sched_mod._scheduler = mock_sched
        try:
            before = datetime.now(timezone.utc)
            from digitize.connectors.scheduler import register_connector_job
            await register_connector_job("conn-A", 600, fire_immediately=False)
            after = datetime.now(timezone.utc)

            mock_sched.add_job.assert_called_once()
            _, kwargs = mock_sched.add_job.call_args
            assert kwargs["id"] == "conn-A"
            trigger = kwargs["trigger"]
            # start_date should be ~now + 600s (deferred by one full interval)
            expected_lo = before + timedelta(seconds=600)
            expected_hi = after + timedelta(seconds=600)
            assert expected_lo <= trigger.start_date <= expected_hi
        finally:
            sched_mod._scheduler = original

    @pytest.mark.asyncio
    async def test_add_job_fires_immediately_when_requested(self):
        """fire_immediately=True → job fires at ~now via next_run_time override.

        APScheduler v3 IntervalTrigger always schedules the first run at
        start_date + N*interval, so passing start_date=now would wait a full
        interval.  The correct approach is next_run_time=now, which overrides
        the trigger's first computed fire time.
        """
        mock_sched = MagicMock()
        import digitize.connectors.scheduler as sched_mod
        original = sched_mod._scheduler
        sched_mod._scheduler = mock_sched
        try:
            before = datetime.now(timezone.utc)
            from digitize.connectors.scheduler import register_connector_job
            await register_connector_job("conn-B", 300, fire_immediately=True)
            after = datetime.now(timezone.utc)

            _, kwargs = mock_sched.add_job.call_args
            # next_run_time must be set to ~now to trigger immediate execution
            assert kwargs["next_run_time"] is not None
            assert before <= kwargs["next_run_time"] <= after
            # start_date is also ~now so the subsequent interval is anchored correctly
            assert before <= kwargs["trigger"].start_date <= after
        finally:
            sched_mod._scheduler = original

    @pytest.mark.asyncio
    async def test_uses_replace_existing(self):
        """add_job must always pass replace_existing=True to handle duplicates."""
        mock_sched = MagicMock()
        import digitize.connectors.scheduler as sched_mod
        original = sched_mod._scheduler
        sched_mod._scheduler = mock_sched
        try:
            from digitize.connectors.scheduler import register_connector_job
            await register_connector_job("conn-C", 300, fire_immediately=False)

            _, kwargs = mock_sched.add_job.call_args
            assert kwargs["replace_existing"] is True
        finally:
            sched_mod._scheduler = original


# ===========================================================================
# scheduler.remove_connector_job
# ===========================================================================

class TestRemoveConnectorJob:
    """Tests for remove_connector_job."""

    @pytest.mark.asyncio
    async def test_raises_when_scheduler_none(self):
        import digitize.connectors.scheduler as sched_mod
        original = sched_mod._scheduler
        sched_mod._scheduler = None
        try:
            with pytest.raises(RuntimeError, match="not initialised"):
                from digitize.connectors.scheduler import remove_connector_job
                await remove_connector_job("c1")
        finally:
            sched_mod._scheduler = original

    @pytest.mark.asyncio
    async def test_calls_remove_job(self):
        mock_sched = MagicMock()
        import digitize.connectors.scheduler as sched_mod
        original = sched_mod._scheduler
        sched_mod._scheduler = mock_sched
        try:
            from digitize.connectors.scheduler import remove_connector_job
            await remove_connector_job("conn-X")
            mock_sched.remove_job.assert_called_once_with("conn-X")
        finally:
            sched_mod._scheduler = original

    @pytest.mark.asyncio
    async def test_silently_handles_job_lookup_error(self):
        """remove_connector_job must not raise if the job was never registered."""
        from apscheduler.jobstores.base import JobLookupError

        mock_sched = MagicMock()
        mock_sched.remove_job.side_effect = JobLookupError("conn-missing")
        import digitize.connectors.scheduler as sched_mod
        original = sched_mod._scheduler
        sched_mod._scheduler = mock_sched
        try:
            from digitize.connectors.scheduler import remove_connector_job
            # Must not raise
            await remove_connector_job("conn-missing")
        finally:
            sched_mod._scheduler = original


# ===========================================================================
# recover_connector_sync_state
# ===========================================================================

class TestRecoverConnectorSyncState:
    """Tests for utils/recovery.recover_connector_sync_state."""

    def test_returns_zero_when_no_stuck_connectors(self):
        with patch(f"{RECOVERY_MODULE}.reset_syncing_connectors", return_value=[]) as mock_reset, \
             patch(f"{RECOVERY_MODULE}.close_open_sync_log") as mock_close:
            from digitize.utils.recovery import recover_connector_sync_state
            result = recover_connector_sync_state()
            assert result == 0
            mock_reset.assert_called_once()
            mock_close.assert_not_called()

    def test_closes_sync_log_for_each_affected_connector(self):
        affected = ["conn-1", "conn-2"]
        with patch(f"{RECOVERY_MODULE}.reset_syncing_connectors", return_value=affected), \
             patch(f"{RECOVERY_MODULE}.close_open_sync_log", return_value=True) as mock_close:
            from digitize.utils.recovery import recover_connector_sync_state
            result = recover_connector_sync_state()
            assert result == 2
            assert mock_close.call_count == 2
            mock_close.assert_any_call("conn-1", "Service restarted during sync tick")
            mock_close.assert_any_call("conn-2", "Service restarted during sync tick")

    def test_logs_warning_when_no_sync_log_found(self):
        """close_open_sync_log returning False triggers a warning (no exception)."""
        with patch(f"{RECOVERY_MODULE}.reset_syncing_connectors", return_value=["conn-3"]), \
             patch(f"{RECOVERY_MODULE}.close_open_sync_log", return_value=False):
            from digitize.utils.recovery import recover_connector_sync_state
            # Must not raise even when no open log row exists
            result = recover_connector_sync_state()
            assert result == 1

    def test_continues_past_per_connector_error(self):
        """Errors for individual connectors must be caught; all connectors are attempted."""
        affected = ["conn-ok", "conn-err"]

        def _close_side_effect(connector_id, error):
            if connector_id == "conn-err":
                raise RuntimeError("DB exploded")
            return True

        with patch(f"{RECOVERY_MODULE}.reset_syncing_connectors", return_value=affected), \
             patch(f"{RECOVERY_MODULE}.close_open_sync_log",
                   side_effect=_close_side_effect):
            from digitize.utils.recovery import recover_connector_sync_state
            result = recover_connector_sync_state()
            # Both connectors were in the affected list; count reflects that
            assert result == 2


# ===========================================================================
# _connector_scheduler_lifespan with delete_pending recovery
# ===========================================================================

class TestLifespanRecoveryDeletePending:
    """Tests for lifespan startup recovery of delete_pending connectors."""

    @pytest.mark.asyncio
    async def test_lifespan_recovery_delete_pending(self):
        import sys
        import types
        import asyncio

        # Stub out the apscheduler jobstore submodule so patch() can resolve it
        # without importing the real package's optional SQLAlchemy dependency.
        if "apscheduler.jobstores.sqlalchemy" not in sys.modules:
            _stub = types.ModuleType("apscheduler.jobstores.sqlalchemy")
            _stub.SQLAlchemyJobStore = None  # placeholder so patch() finds the attribute
            sys.modules["apscheduler.jobstores.sqlalchemy"] = _stub

        from digitize.app import _connector_scheduler_lifespan
        from digitize.connectors.models import ConnectorStatus

        # Mock connectors list
        mock_conn_active = MagicMock()
        mock_conn_active.id = "conn-active"
        mock_conn_active.sync_status = ConnectorStatus.UP_TO_DATE
        mock_conn_active.sync_interval_seconds = 300

        mock_conn_delete = MagicMock()
        mock_conn_delete.id = "conn-delete"
        mock_conn_delete.sync_status = ConnectorStatus.DELETE_PENDING
        mock_conn_delete.sync_interval_seconds = 300

        mock_connectors = [mock_conn_active, mock_conn_delete]

        # Mocks for functions/classes used in lifespan
        mock_recover = MagicMock(return_value=0)
        mock_list = MagicMock(return_value=mock_connectors)
        mock_register = AsyncMock()
        mock_teardown = AsyncMock()

        # Mock scheduler instance (v3 API: synchronous start/shutdown)
        mock_sched_instance = MagicMock()
        mock_sched_instance.start = MagicMock()
        mock_sched_instance.shutdown = MagicMock()

        mock_sched_class = MagicMock(return_value=mock_sched_instance)

        # Mock job store and engine
        mock_job_store = MagicMock()

        with patch("digitize.app.recover_connector_sync_state", mock_recover), \
             patch("digitize.utils.db.list_connectors", mock_list), \
             patch("digitize.connectors.scheduler.register_connector_job", mock_register), \
             patch("digitize.api.v1.connectors._run_teardown", mock_teardown), \
             patch("apscheduler.schedulers.asyncio.AsyncIOScheduler", mock_sched_class), \
             patch("apscheduler.jobstores.sqlalchemy.SQLAlchemyJobStore", return_value=mock_job_store), \
             patch("digitize.db.connection.engine", MagicMock()):

            # Run the context manager
            async with _connector_scheduler_lifespan():
                # Let any background tasks (like the created task for teardown) yield control to execute
                await asyncio.sleep(0.05)

        # Assertions
        mock_recover.assert_called_once()
        mock_list.assert_called_once()

        # "conn-active" should be registered
        mock_register.assert_awaited_once_with(
            "conn-active",
            300,
            fire_immediately=False,
        )

        # "conn-delete" should not be registered but teardown should be triggered
        mock_teardown.assert_awaited_once_with("conn-delete")

        # scheduler lifecycle
        mock_sched_instance.start.assert_called_once()
        mock_sched_instance.shutdown.assert_called_once()
