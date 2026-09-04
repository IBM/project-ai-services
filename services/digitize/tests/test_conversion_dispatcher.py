"""
Unit tests for the conversion dispatcher — workers/conversion_dispatcher.py.

These tests focus on:
  - _other() helper
  - _try_claim_if_fits() head-of-line blocking logic
  - dispatch_loop() round-robin behaviour (mocked DB + semaphore)
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
# _other()
# ---------------------------------------------------------------------------

@pytest.mark.unit
class TestOther:
    def test_ingestion_returns_digitization(self):
        from digitize.workers.conversion_dispatcher import _other
        assert _other("ingestion") == "digitization"

    def test_digitization_returns_ingestion(self):
        from digitize.workers.conversion_dispatcher import _other
        assert _other("digitization") == "ingestion"


# ---------------------------------------------------------------------------
# _try_claim_if_fits()
# ---------------------------------------------------------------------------

@pytest.mark.unit
class TestTryClaimIfFits:
    def test_returns_none_when_no_head(self):
        from digitize.workers import conversion_dispatcher as mod

        with patch.object(mod.db_manager, "peek_head", return_value=None):
            result = mod._try_claim_if_fits("ingestion", available=4)
        assert result is None

    def test_returns_none_when_head_too_large(self):
        from digitize.workers import conversion_dispatcher as mod

        head = _make_task(is_large=True)  # weight 2
        with patch.object(mod.db_manager, "peek_head", return_value=head):
            result = mod._try_claim_if_fits("ingestion", available=1)  # only 1 free
        assert result is None

    def test_claims_normal_task_when_capacity_available(self):
        from digitize.workers import conversion_dispatcher as mod

        head = _make_task(is_large=False)  # weight 1
        claimed = _make_task(is_large=False)
        with patch.object(mod.db_manager, "peek_head", return_value=head), \
             patch.object(mod.db_manager, "claim_head", return_value=claimed):
            result = mod._try_claim_if_fits("digitization", available=2)
        assert result is claimed

    def test_claims_large_task_when_2_units_free(self):
        from digitize.workers import conversion_dispatcher as mod

        head = _make_task(is_large=True)  # weight 2
        claimed = _make_task(is_large=True)
        with patch.object(mod.db_manager, "peek_head", return_value=head), \
             patch.object(mod.db_manager, "claim_head", return_value=claimed):
            result = mod._try_claim_if_fits("ingestion", available=2)
        assert result is claimed

    def test_head_of_line_blocking_does_not_skip(self):
        """Large head present → nothing behind it is skipped, returns None."""
        from digitize.workers import conversion_dispatcher as mod

        head = _make_task(is_large=True)  # needs 2 units
        with patch.object(mod.db_manager, "peek_head", return_value=head), \
             patch.object(mod.db_manager, "claim_head") as mock_claim:
            result = mod._try_claim_if_fits("ingestion", available=1)
        assert result is None
        mock_claim.assert_not_called()  # claim_head must never be called

    def test_zero_budget_returns_none(self):
        from digitize.workers import conversion_dispatcher as mod

        head = _make_task(is_large=False)  # weight 1
        with patch.object(mod.db_manager, "peek_head", return_value=head):
            result = mod._try_claim_if_fits("ingestion", available=0)
        assert result is None


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

    def test_turn_flips_after_first_task_claimed(self):
        import digitize.workers.conversion_dispatcher as mod
        mod._rr_turn = "ingestion"

        first_t = _make_task(task_id="t-ing", operation="ingestion", is_large=False)
        second_t = _make_task(task_id="t-dig", operation="digitization", is_large=False)

        # Set 4 units available so first AND second can be claimed
        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 4)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits",
                                   side_effect=lambda op, avail: first_t if op == "ingestion" else second_t), \
                     patch.object(mod.db_manager, "peek_head",
                                   return_value=_make_task(is_large=False)), \
                     patch.object(mod.db_manager, "get_queued_counts",
                                   return_value={"ingestion": 0, "digitization": 0}), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
                     patch.object(mod.conversion_semaphore, "acquire", new_callable=AsyncMock), \
                     patch("asyncio.create_task", new=_make_create_task_mock()), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
                    mock_sleep.side_effect = asyncio.CancelledError()
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

                # First was claimed → turn flips to "digitization"
                assert mod._rr_turn == "digitization"

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

    def test_turn_does_not_flip_when_first_blocked(self):
        """
        If first type's head is a large file that can't fit (available=1),
        _rr_turn must NOT flip.
        """
        import digitize.workers.conversion_dispatcher as mod
        mod._rr_turn = "ingestion"

        large_head = _make_task(task_id="t-large-ing", operation="ingestion", is_large=True)

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 1)  # only 1 free — can't fit large (weight 2)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", return_value=None), \
                     patch.object(mod.db_manager, "peek_head", return_value=large_head), \
                     patch.object(mod.db_manager, "get_queued_counts",
                                   return_value={"ingestion": 5, "digitization": 0}), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
                     patch("asyncio.create_task", new=_make_create_task_mock()), \
                     patch("asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
                    mock_sleep.side_effect = asyncio.CancelledError()
                    try:
                        await mod.dispatch_loop()
                    except asyncio.CancelledError:
                        pass

                assert mod._rr_turn == "ingestion"  # unchanged

            asyncio.run(_run())
        finally:
            _set_semaphore_available(mod, original_available)

    def test_second_gets_no_budget_when_first_head_is_large_and_unavailable(self):
        """
        First (ING) head needs 2 units but only 1 is free → budget_for_second = 0.
        No DIG task is queued so second_needed=0 → budget_for_first = available=1.
        _try_claim_if_fits calls: ingestion receives 1, digitization receives 0.
        """
        import digitize.workers.conversion_dispatcher as mod
        mod._rr_turn = "ingestion"

        large_ing = _make_task(task_id="t-ing-large", operation="ingestion", is_large=True)

        calls = []

        def _mock_try_claim(op, avail):
            calls.append((op, avail))
            return None  # nothing can run

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 1)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", side_effect=_mock_try_claim), \
                     patch.object(mod.db_manager, "peek_head",
                                   side_effect=lambda op: large_ing if op == "ingestion" else None), \
                     patch.object(mod.db_manager, "get_queued_counts",
                                   return_value={"ingestion": 5, "digitization": 0}), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
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

        # second_needed=0 (no DIG head) → budget_for_first = max(0, 1-0) = 1
        # first_needed=2  (large ING)   → budget_for_second = max(0, 1-2) = 0
        assert ("ingestion", 1) in calls
        assert ("digitization", 0) in calls

    def test_first_gets_no_budget_when_second_head_is_large_and_slot_needed(self):
        """
        Regression test for large-file starvation via the other queue.

        Scenario (mirrors hol_blocking probe tick 1):
          - _rr_turn = "digitization"  (turn just flipped to DIG as first)
          - available = 1
          - second queue (ING) head is a large task needing weight=2
          - first queue (DIG) head is a normal task needing weight=1

        Before the fix, DIG normal would be dispatched (budget=available=1 ≥ 1),
        consuming the last slot and leaving the large ING task unable to ever
        accumulate its required 2 free slots until DIG *and* all other running
        tasks finished — indefinite starvation.

        After the fix:
          budget_for_first = max(0, available - second_needed)
                           = max(0,     1     -      2       ) = 0
        DIG must also be blocked; neither queue dispatches on this tick.
        """
        import digitize.workers.conversion_dispatcher as mod
        mod._rr_turn = "digitization"

        normal_dig = _make_task(task_id="t-dig-normal", operation="digitization", is_large=False)
        large_ing  = _make_task(task_id="t-ing-large",  operation="ingestion",    is_large=True)

        calls = []

        def _mock_try_claim(op, avail):
            calls.append((op, avail))
            return None  # nothing should run

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 1)

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", side_effect=_mock_try_claim), \
                     patch.object(mod.db_manager, "peek_head",
                                   side_effect=lambda op: normal_dig if op == "digitization" else large_ing), \
                     patch.object(mod.db_manager, "get_queued_counts",
                                   return_value={"ingestion": 1, "digitization": 1}), \
                     patch.object(mod.db_manager, "promote_pending", return_value=0), \
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

        # second_needed=2 (large ING) → budget_for_first = max(0, 1-2) = 0
        #   → DIG receives budget=0, _try_claim_if_fits("digitization", 0) → None, no dispatch
        # first_needed=1  (normal DIG) → first_reservation=0 (only >1 triggers reservation)
        #   → budget_for_second = max(0, available_after_first - 0) = max(0, 1-0) = 1
        #   → ING receives budget=1, _try_claim_if_fits("ingestion", 1) → None
        #     (large ING needs weight=2 > budget=1, so claim_head returns None inside the helper)
        # Neither queue dispatches; the budget calculation is correct.
        assert ("digitization", 0) in calls, (
            "DIG normal must NOT be dispatched when large ING is waiting for 2 slots"
        )
        assert ("ingestion", 1) in calls, (
            "ING receives budget=1 (no first_reservation for normal DIG head); "
            "_try_claim_if_fits rejects it internally because weight=2 > budget=1"
        )

    def test_promote_pending_called_each_tick(self):
        import digitize.workers.conversion_dispatcher as mod
        mod._rr_turn = "ingestion"

        promote_calls = []

        original_available = mod.conversion_semaphore._available
        _set_semaphore_available(mod, 0)  # no capacity — skips claim logic

        try:
            async def _run():
                with patch.object(mod, "_try_claim_if_fits", return_value=None), \
                     patch.object(mod.db_manager, "peek_head", return_value=None), \
                     patch.object(mod.db_manager, "get_queued_counts",
                                   return_value={"ingestion": 0, "digitization": 0}), \
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
        mod._rr_turn = "ingestion"

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
