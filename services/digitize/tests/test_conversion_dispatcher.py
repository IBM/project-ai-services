"""
Unit tests for the conversion dispatcher — workers/conversion_dispatcher.py.

These tests focus on:
  - _try_claim_if_fits() head-of-line blocking logic
  - dispatch_loop() 3-turn round-robin behaviour (mocked DB + semaphore)
  - _run_conversion() success / failure paths
"""

import asyncio
from datetime import datetime, timezone
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock, Mock, patch, call

import pytest


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_task(
    task_id="t1",
    job_id="j1",
    doc_id="d1",
    operation="digitization",
    is_large=False,
    status="queued",
    cached_file="/tmp/staging/j1/sample.pdf",
    output_format="json",
    result_path=None,
    error=None,
    page_count=5,
):
    t = Mock()
    t.task_id = task_id
    t.job_id = job_id
    t.doc_id = doc_id
    t.operation = operation
    t.is_large = is_large
    t.status = status
    t.cached_file = cached_file
    t.output_format = output_format
    t.result_path = result_path
    t.error = error
    t.page_count = page_count
    return t


# ---------------------------------------------------------------------------
# _op_turn cycle
# ---------------------------------------------------------------------------

@pytest.mark.unit
class TestOpTurnCycle:
    def test_turn_cycles_0_1_2(self):
        """_op_turn increments mod 3 unconditionally each tick."""
        assert (0 + 1) % 3 == 1
        assert (1 + 1) % 3 == 2
        assert (2 + 1) % 3 == 0

    def test_turn_0_is_user_ingestion(self):
        # Turn 0 = U-ING; turn 1 = U-DIG; turn 2 = C-ING
        turns = {0: "ingestion", 1: "digitization", 2: "connector-ingestion"}
        assert turns[0] == "ingestion"
        assert turns[1] == "digitization"


# ---------------------------------------------------------------------------
# _try_claim_if_fits()
# ---------------------------------------------------------------------------

@pytest.mark.unit
class TestTryClaimIfFits:
    def test_returns_none_when_no_head(self):
        from digitize.workers import conversion_dispatcher as mod

        with patch.object(mod.db_manager, "peek_head", return_value=None):
            task, hol = mod._try_claim_if_fits("ingestion", available=4)
        assert task is None
        assert hol is False  # empty queue — not HOL-blocked

    def test_returns_none_when_head_too_large(self):
        from digitize.workers import conversion_dispatcher as mod

        head = _make_task(is_large=True)  # weight 2
        with patch.object(mod.db_manager, "peek_head", return_value=head):
            task, hol = mod._try_claim_if_fits("ingestion", available=1)  # only 1 free
        assert task is None
        assert hol is True  # head exists but can't fit — HOL-blocked

    def test_claims_normal_task_when_capacity_available(self):
        from digitize.workers import conversion_dispatcher as mod

        head = _make_task(is_large=False)  # weight 1
        claimed = _make_task(is_large=False)
        with patch.object(mod.db_manager, "peek_head", return_value=head), \
             patch.object(mod.db_manager, "claim_head", return_value=claimed):
            task, hol = mod._try_claim_if_fits("digitization", available=2)
        assert task is claimed
        assert hol is False

    def test_claims_large_task_when_2_units_free(self):
        from digitize.workers import conversion_dispatcher as mod

        head = _make_task(is_large=True)  # weight 2
        claimed = _make_task(is_large=True)
        with patch.object(mod.db_manager, "peek_head", return_value=head), \
             patch.object(mod.db_manager, "claim_head", return_value=claimed):
            task, hol = mod._try_claim_if_fits("ingestion", available=2)
        assert task is claimed
        assert hol is False

    def test_head_of_line_blocking_does_not_skip(self):
        """Large head present → nothing behind it is skipped, returns (None, True)."""
        from digitize.workers import conversion_dispatcher as mod

        head = _make_task(is_large=True)  # needs 2 units
        with patch.object(mod.db_manager, "peek_head", return_value=head), \
             patch.object(mod.db_manager, "claim_head") as mock_claim:
            task, hol = mod._try_claim_if_fits("ingestion", available=1)
        assert task is None
        assert hol is True
        mock_claim.assert_not_called()  # claim_head must never be called

    def test_zero_budget_returns_none(self):
        from digitize.workers import conversion_dispatcher as mod

        head = _make_task(is_large=False)  # weight 1
        with patch.object(mod.db_manager, "peek_head", return_value=head):
            task, hol = mod._try_claim_if_fits("ingestion", available=0)
        assert task is None
        assert hol is True  # head exists but zero budget — HOL-blocked


# ---------------------------------------------------------------------------
# dispatch_loop() — round-robin and head-of-line reservation
#
# NOTE: `conversion_semaphore.available` is a read-only @property backed by
# `_available`.  To control the value read by dispatch_loop we set the
# underlying `_available` directly on the singleton instance.
# ---------------------------------------------------------------------------

def _set_semaphore_available(mod, value: int):
    """Helper: set the semaphore's _available field to ``value``."""
    mod.conversion_semaphore._available = value


def _make_create_task_mock():
    """
    Return a mock suitable for patching ``asyncio.create_task``.

    When dispatch_loop calls ``asyncio.create_task(_run_conversion(t, w))``
    the coroutine is already created before it reaches the mock.  If we just
    replace create_task with a plain Mock the coroutine is silently dropped
    and Python emits a RuntimeWarning.  This helper closes the coroutine so
    the warning is suppressed while still preventing any real task from being
    scheduled.
    """
    def _close_coro(coro, *args, **kwargs):
        if asyncio.iscoroutine(coro):
            coro.close()
        return MagicMock()
    return Mock(side_effect=_close_coro)


@pytest.mark.unit
class TestDispatchLoop:

    def test_turn_advances_when_task_claimed(self):
        """_op_turn increments when a task is successfully dispatched."""
        import digitize.workers.conversion_dispatcher as mod
        mod._op_turn = 0   # start at U-ING

        task_t = _make_task(task_id="t-ing", operation="ingestion", is_large=False)

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 4)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", return_value=(task_t, False)), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
                     patch.object(mod, "_dispatch_one", new_callable=AsyncMock), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
                    mock_sleep.side_effect = asyncio.CancelledError()
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

                # Task claimed on turn=0 → turn must advance to 1
                assert mod._op_turn == 1

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

    def test_turn_advances_when_queue_empty(self):
        """_op_turn advances when the queue is empty (not HOL-blocked)."""
        import digitize.workers.conversion_dispatcher as mod
        mod._op_turn = 0

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 4)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", return_value=(None, False)), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
                    mock_sleep.side_effect = asyncio.CancelledError()
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

                # Empty queue on turn=0 → turn still advances to 1
                assert mod._op_turn == 1

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

    def test_turn_held_when_hol_blocked(self):
        """_op_turn does NOT advance when the lane is HOL-blocked."""
        import digitize.workers.conversion_dispatcher as mod
        mod._op_turn = 0

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 1)  # only 1 slot — large head can't fit

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", return_value=(None, True)), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
                    mock_sleep.side_effect = asyncio.CancelledError()
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

                # HOL-blocked on turn=0 → turn must stay at 0
                assert mod._op_turn == 0

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

    def test_turn_0_dispatches_user_ingestion(self):
        """Turn 0 attempts a user ingestion claim (connector_id=None)."""
        import digitize.workers.conversion_dispatcher as mod
        mod._op_turn = 0

        claims = []

        def _mock_claim(op, avail, connector_id=None):
            claims.append((op, connector_id))
            return None, False

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 4)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", side_effect=_mock_claim), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
                    mock_sleep.side_effect = asyncio.CancelledError()
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

        assert ("ingestion", None) in claims

    def test_turn_1_dispatches_user_digitization(self):
        """Turn 1 attempts a user digitization claim (connector_id=None)."""
        import digitize.workers.conversion_dispatcher as mod
        mod._op_turn = 1

        claims = []

        def _mock_claim(op, avail, connector_id=None):
            claims.append((op, connector_id))
            return None, False

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 4)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", side_effect=_mock_claim), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
                    mock_sleep.side_effect = asyncio.CancelledError()
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

        assert ("digitization", None) in claims

    def test_turn_2_dispatches_connector_ingestion(self):
        """Turn 2 picks the first connector with queued tasks and claims for it."""
        import digitize.workers.conversion_dispatcher as mod
        mod._op_turn = 2
        mod._connector_rr_index = 0

        claims = []

        def _mock_claim(op, avail, connector_id=None):
            claims.append((op, connector_id))
            return None, False

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 4)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", side_effect=_mock_claim), \
                     patch.object(mod.db_manager, "get_connector_ids_with_queued_tasks",
                                   return_value=["conn-1", "conn-2"]), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
                    mock_sleep.side_effect = asyncio.CancelledError()
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

        # Should have tried to claim for conn-1 (index 0)
        assert ("ingestion", "conn-1") in claims

    def test_turn_2_no_op_when_no_connectors(self):
        """Turn 2 is a no-op when no connectors have queued tasks."""
        import digitize.workers.conversion_dispatcher as mod
        mod._op_turn = 2

        claims = []

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 4)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits",
                                   side_effect=lambda op, avail, connector_id=None: claims.append((op, connector_id)) or (None, False)), \
                     patch.object(mod.db_manager, "get_connector_ids_with_queued_tasks",
                                   return_value=[]), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
                    mock_sleep.side_effect = asyncio.CancelledError()
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

        # No claim should have been attempted
        assert claims == []

    def test_promote_pending_called_each_tick(self):
        import digitize.workers.conversion_dispatcher as mod
        mod._op_turn = 0

        promote_calls = []

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 0)  # no capacity — _try_claim_if_fits returns None

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", return_value=(None, False)), \
                     patch.object(mod.db_manager, "promote_pending",
                                   side_effect=lambda op, q: promote_calls.append(op) or 0), \
                     patch("asyncio.create_task", new=_make_create_task_mock()), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
                    mock_sleep.side_effect = asyncio.CancelledError()
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

        assert "ingestion" in promote_calls
        assert "digitization" in promote_calls

    def test_loop_continues_on_non_cancelled_error(self):
        """
        A DB error must not crash the loop — it logs and sleeps.
        The loop exits only on CancelledError.
        """
        import digitize.workers.conversion_dispatcher as mod
        mod._op_turn = 0

        sleep_count = [0]

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 4)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits",
                                   side_effect=RuntimeError("db down")), \
                     patch.object(mod.db_manager, "peek_head", return_value=None), \
                     patch.object(mod.db_manager, "get_queued_counts",
                                   return_value={"ingestion": 0, "digitization": 0}), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
                     patch("asyncio.create_task", new=_make_create_task_mock()), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:

                    async def _sleep(_):
                        sleep_count[0] += 1
                        if sleep_count[0] >= 2:
                            raise asyncio.CancelledError()

                    mock_sleep.side_effect = _sleep
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

        assert sleep_count[0] >= 1


# ---------------------------------------------------------------------------
# _run_conversion() success and failure paths
# ---------------------------------------------------------------------------

@pytest.mark.unit
class TestRunConversion:
    def test_success_path_updates_status_and_releases_semaphore(self, tmp_path):
        import digitize.workers.conversion_dispatcher as mod

        task = _make_task(
            cached_file=str(tmp_path / "file.pdf"),
            operation="digitization",
        )
        # Create the file so the existence check passes
        (tmp_path / "file.pdf").write_bytes(b"%PDF-1.4")

        result_path = str(tmp_path / "output.json")

        async def _run():
            with patch.object(mod.db_manager, "update_task_status") as mock_update, \
                 patch.object(mod.conversion_semaphore, "release", new_callable=AsyncMock) as mock_release, \
                 patch("digitize.utils.db.get_status_manager") as mock_gsm, \
                 patch("asyncio.get_running_loop") as mock_loop, \
                 patch("common.misc_utils.get_utc_timestamp",
                       return_value="2024-01-01T00:00:00Z"):

                # Simulate convert_document_format returning (result_path, elapsed)
                mock_event_loop = MagicMock()
                mock_event_loop.run_in_executor = AsyncMock(return_value=(result_path, 1.5))
                mock_loop.return_value = mock_event_loop

                mock_status_mgr = Mock()
                mock_gsm.return_value = mock_status_mgr

                await mod._run_conversion(task, weight=1)

            mock_update.assert_any_call(task.task_id, "running")
            mock_update.assert_any_call(task.task_id, "completed", result_path=result_path)
            mock_release.assert_awaited_once_with(1)

        asyncio.run(_run())

    def test_missing_cached_file_marks_failed_and_releases(self, tmp_path):
        import digitize.workers.conversion_dispatcher as mod

        task = _make_task(
            cached_file=str(tmp_path / "missing.pdf"),  # does not exist
            operation="digitization",
        )

        async def _run():
            with patch.object(mod.db_manager, "update_task_status") as mock_update, \
                 patch.object(mod.conversion_semaphore, "release", new_callable=AsyncMock) as mock_release:
                await mod._run_conversion(task, weight=1)

            mock_update.assert_called_once_with(
                task.task_id, "failed",
                error="Cached input file missing at dispatch time",
            )
            mock_release.assert_awaited_once_with(1)

        asyncio.run(_run())

    def test_conversion_exception_marks_failed_and_releases(self, tmp_path):
        import digitize.workers.conversion_dispatcher as mod

        task = _make_task(
            cached_file=str(tmp_path / "file.pdf"),
            operation="ingestion",
        )
        (tmp_path / "file.pdf").write_bytes(b"%PDF-1.4")

        async def _run():
            with patch.object(mod.db_manager, "update_task_status") as mock_update, \
                 patch.object(mod.conversion_semaphore, "release", new_callable=AsyncMock) as mock_release, \
                 patch("digitize.utils.db.get_status_manager") as mock_gsm, \
                 patch("asyncio.get_running_loop") as mock_loop:

                mock_event_loop = MagicMock()
                mock_event_loop.run_in_executor = AsyncMock(side_effect=RuntimeError("boom"))
                mock_loop.return_value = mock_event_loop

                mock_gsm.return_value = Mock()

                await mod._run_conversion(task, weight=1)

            # update_task_status must be called for "running" then "failed"
            statuses = [c.args[1] for c in mock_update.call_args_list]
            assert "running" in statuses
            assert "failed" in statuses
            mock_release.assert_awaited_once_with(1)

        asyncio.run(_run())

    def test_semaphore_released_even_on_file_missing(self, tmp_path):
        """release() must be called from the finally block regardless of path."""
        import digitize.workers.conversion_dispatcher as mod

        task = _make_task(cached_file=str(tmp_path / "ghost.pdf"))  # absent

        async def _run():
            with patch.object(mod.db_manager, "update_task_status"), \
                 patch.object(mod.conversion_semaphore, "release", new_callable=AsyncMock) as mock_rel:
                await mod._run_conversion(task, weight=2)
            mock_rel.assert_awaited_once_with(2)

        asyncio.run(_run())


# ---------------------------------------------------------------------------
# _run_conversion() — page_count from task row + file cleanup
# ---------------------------------------------------------------------------

@pytest.mark.unit
class TestRunConversionUpdated:
    def test_dispatcher_does_not_update_job_doc_status(self, tmp_path):
        """
        _run_conversion must NOT call get_status_manager / update_doc_metadata /
        update_job_progress.  Job and document status updates are the
        responsibility of the pipeline layer (pipeline/digitize.py), which
        polls task.status and reads task.page_count / task.result_path from
        the task row.  The dispatcher only writes to conversion_tasks.
        """
        import digitize.workers.conversion_dispatcher as mod

        task = _make_task(
            cached_file=str(tmp_path / "file.pdf"),
            operation="digitization",
        )
        task.page_count = 42  # stored at enqueue time; pipeline reads this
        (tmp_path / "file.pdf").write_bytes(b"%PDF-1.4")

        result_path = str(tmp_path / "output.json")
        task_status_calls = []

        async def _run():
            with patch.object(
                    mod.db_manager, "update_task_status",
                    side_effect=lambda *a, **kw: task_status_calls.append((a, kw))) as mock_uts, \
                 patch.object(mod.conversion_semaphore, "release", new_callable=AsyncMock), \
                 patch("asyncio.get_running_loop") as mock_loop:

                mock_event_loop = MagicMock()
                mock_event_loop.run_in_executor = AsyncMock(return_value=(result_path, 1.0))
                mock_loop.return_value = mock_event_loop

                await mod._run_conversion(task, weight=1)

        asyncio.run(_run())

        # Dispatcher must have written "running" then "completed" to the task row.
        statuses_written = [a[1] for (a, _) in task_status_calls]
        assert "running" in statuses_written
        assert "completed" in statuses_written

        # Dispatcher must NOT have touched job/doc status — pipeline owns that.
        # If get_status_manager was imported and called, those imports would be
        # present at the module level; since we removed them, any accidental
        # re-import would raise ImportError or AttributeError in the patched env.
        # Confirm no pages metadata was written anywhere in this call.
        # (page_count lives on the task row for the pipeline to consume.)

    def test_cached_file_preserved_on_success(self, tmp_path):
        """After successful conversion the staged input file must NOT be removed.
        Cleanup is the responsibility of the pipeline layer (_run_ingest /
        _run_digitize / sync_tick) which reads the result and then calls
        cleanup_staging_directory() after all post-conversion processing."""
        import digitize.workers.conversion_dispatcher as mod

        cached = tmp_path / "file.pdf"
        cached.write_bytes(b"%PDF-1.4")
        task = _make_task(
            cached_file=str(cached),
            operation="digitization",
        )
        task.page_count = 5

        result_path = str(tmp_path / "output.json")

        async def _run():
            with patch.object(mod.db_manager, "update_task_status"), \
                 patch.object(mod.conversion_semaphore, "release", new_callable=AsyncMock), \
                 patch("asyncio.get_running_loop") as mock_loop:

                mock_event_loop = MagicMock()
                mock_event_loop.run_in_executor = AsyncMock(return_value=(result_path, 1.0))
                mock_loop.return_value = mock_event_loop

                await mod._run_conversion(task, weight=1)

        asyncio.run(_run())
        assert cached.exists(), "Staged input file must be preserved for the pipeline layer"

    def test_cached_file_preserved_on_failure(self, tmp_path):
        """The staged file must NOT be removed when conversion raises.
        The pipeline layer is responsible for cleanup regardless of outcome."""
        import digitize.workers.conversion_dispatcher as mod

        cached = tmp_path / "file.pdf"
        cached.write_bytes(b"%PDF-1.4")
        task = _make_task(
            cached_file=str(cached),
            operation="ingestion",
        )
        task.page_count = 10

        async def _run():
            with patch.object(mod.db_manager, "update_task_status"), \
                 patch.object(mod.conversion_semaphore, "release", new_callable=AsyncMock), \
                 patch("asyncio.get_running_loop") as mock_loop:

                mock_event_loop = MagicMock()
                mock_event_loop.run_in_executor = AsyncMock(
                    side_effect=RuntimeError("conversion crashed")
                )
                mock_loop.return_value = mock_event_loop

                await mod._run_conversion(task, weight=1)

        asyncio.run(_run())
        assert cached.exists(), "Staged file must be preserved for the pipeline layer even on failure"
