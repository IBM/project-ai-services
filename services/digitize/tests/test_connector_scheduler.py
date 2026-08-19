"""
Unit tests for services/digitize/connectors/scheduler.py
and the connector portion of services/digitize/utils/recovery.py.

Coverage
--------
scheduler.register_connector_job
  - calls sched.add_schedule with correct kwargs
  - fire_immediately=True  → trigger.start_time is ~now (fires on first tick)
  - fire_immediately=False → trigger.start_time is ~now+interval (deferred)
  - raises RuntimeError when _scheduler is None

scheduler.remove_connector_job
  - calls sched.remove_schedule with the connector_id
  - silently handles LookupError (schedule not registered)
  - raises RuntimeError when _scheduler is None

recover_connector_sync_state
  - returns 0 and skips close when no syncing connectors
  - calls close_open_sync_log for each affected connector
  - logs warning when no open sync log row found
  - continues past per-connector errors and returns correct count
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from unittest.mock import AsyncMock, patch

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
    async def test_add_schedule_deferred_when_not_fire_immediately(self):
        """fire_immediately=False → trigger.start_time is ~now + interval_seconds."""
        mock_sched = AsyncMock()
        import digitize.connectors.scheduler as sched_mod
        original = sched_mod._scheduler
        sched_mod._scheduler = mock_sched
        try:
            before = datetime.now(timezone.utc)
            from digitize.connectors.scheduler import register_connector_job
            await register_connector_job("conn-A", 600, fire_immediately=False)
            after = datetime.now(timezone.utc)

            mock_sched.add_schedule.assert_awaited_once()
            _, kwargs = mock_sched.add_schedule.await_args
            assert kwargs["id"] == "conn-A"
            trigger = kwargs["trigger"]
            # start_time should be ~now + 600s (deferred by one full interval)
            expected_lo = before + timedelta(seconds=600)
            expected_hi = after + timedelta(seconds=600)
            assert expected_lo <= trigger.start_time <= expected_hi
        finally:
            sched_mod._scheduler = original

    @pytest.mark.asyncio
    async def test_add_schedule_fires_immediately_when_requested(self):
        """fire_immediately=True → trigger.start_time is ~now (fires on first tick)."""
        mock_sched = AsyncMock()
        import digitize.connectors.scheduler as sched_mod
        original = sched_mod._scheduler
        sched_mod._scheduler = mock_sched
        try:
            before = datetime.now(timezone.utc)
            from digitize.connectors.scheduler import register_connector_job
            await register_connector_job("conn-B", 300, fire_immediately=True)
            after = datetime.now(timezone.utc)

            _, kwargs = mock_sched.add_schedule.await_args
            trigger = kwargs["trigger"]
            # start_time should be ~now (within the call window)
            assert before <= trigger.start_time <= after
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
    async def test_calls_remove_schedule(self):
        mock_sched = AsyncMock()
        import digitize.connectors.scheduler as sched_mod
        original = sched_mod._scheduler
        sched_mod._scheduler = mock_sched
        try:
            from digitize.connectors.scheduler import remove_connector_job
            await remove_connector_job("conn-X")
            mock_sched.remove_schedule.assert_awaited_once_with("conn-X")
        finally:
            sched_mod._scheduler = original

    @pytest.mark.asyncio
    async def test_silently_handles_lookup_error(self):
        """remove_connector_job must not raise if the schedule was never registered."""
        mock_sched = AsyncMock()
        mock_sched.remove_schedule.side_effect = LookupError("not found")
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
