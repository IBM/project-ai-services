"""
Unit tests for WeightedSemaphore — workers/conversion_semaphore.py.

Verifies capacity management, head-of-line blocking semantics, and
concurrent acquire/release behaviour.
"""

import asyncio
import pytest


# ---------------------------------------------------------------------------
# Import the class under test directly (not the singleton) so tests are
# not coupled to the module-level settings value.
# ---------------------------------------------------------------------------
from digitize.workers.conversion_semaphore import WeightedSemaphore


@pytest.mark.unit
class TestWeightedSemaphoreInit:
    def test_initial_capacity_equals_available(self):
        sem = WeightedSemaphore(capacity=4)
        assert sem.capacity == 4
        assert sem.available == 4

    def test_capacity_one(self):
        sem = WeightedSemaphore(capacity=1)
        assert sem.capacity == 1
        assert sem.available == 1


@pytest.mark.unit
class TestWeightedSemaphoreAcquireRelease:
    def test_acquire_reduces_available(self):
        async def _run():
            sem = WeightedSemaphore(capacity=4)
            await sem.acquire(1)
            assert sem.available == 3

        asyncio.run(_run())

    def test_acquire_weight_2_reduces_by_2(self):
        async def _run():
            sem = WeightedSemaphore(capacity=4)
            await sem.acquire(2)
            assert sem.available == 2

        asyncio.run(_run())

    def test_release_restores_available(self):
        async def _run():
            sem = WeightedSemaphore(capacity=4)
            await sem.acquire(2)
            await sem.release(2)
            assert sem.available == 4

        asyncio.run(_run())

    def test_multiple_acquires_drain_capacity(self):
        async def _run():
            sem = WeightedSemaphore(capacity=4)
            await sem.acquire(1)
            await sem.acquire(1)
            await sem.acquire(2)
            assert sem.available == 0

        asyncio.run(_run())

    def test_acquire_full_capacity_at_once(self):
        async def _run():
            sem = WeightedSemaphore(capacity=4)
            await sem.acquire(4)
            assert sem.available == 0

        asyncio.run(_run())


@pytest.mark.unit
class TestWeightedSemaphoreBlocking:
    def test_acquire_blocks_until_capacity_available(self):
        """
        A weight-2 acquire issued when only 1 unit is free must block
        until a release brings capacity back up to 2.
        """
        results: list[str] = []

        async def _run():
            sem = WeightedSemaphore(capacity=2)
            await sem.acquire(1)  # 1 unit consumed → 1 available

            async def _waiter():
                await sem.acquire(2)  # needs 2; will block
                results.append("waiter_acquired")

            task = asyncio.create_task(_waiter())
            await asyncio.sleep(0)  # let waiter start
            assert results == []   # waiter is blocked

            await sem.release(1)   # free the 1 held → now 2 available
            await asyncio.sleep(0)  # let waiter resume
            await task
            assert results == ["waiter_acquired"]

        asyncio.run(_run())

    def test_release_wakes_multiple_waiters(self):
        """
        Releasing units should wake all waiters whose weight now fits.
        Two weight-1 waiters must both be unblocked by a single weight-2 release.
        """
        acquired: list[int] = []

        async def _run():
            sem = WeightedSemaphore(capacity=2)
            await sem.acquire(2)  # drain all capacity

            async def _waiter(n: int):
                await sem.acquire(1)
                acquired.append(n)

            t1 = asyncio.create_task(_waiter(1))
            t2 = asyncio.create_task(_waiter(2))
            await asyncio.sleep(0)  # both tasks waiting

            await sem.release(2)
            await asyncio.gather(t1, t2)
            assert sorted(acquired) == [1, 2]

        asyncio.run(_run())

    def test_available_reflects_inflight_weight(self):
        """
        WeightedSemaphore.available reflects the total consumed weight so
        the dispatcher can read it synchronously without an acquire attempt.
        """
        async def _run():
            sem = WeightedSemaphore(capacity=4)
            await sem.acquire(2)
            assert sem.available == 2
            await sem.acquire(1)
            assert sem.available == 1
            await sem.release(1)
            assert sem.available == 2
            await sem.release(2)
            assert sem.available == 4

        asyncio.run(_run())


@pytest.mark.unit
class TestWeightedSemaphoreConcurrency:
    def test_concurrent_acquires_respect_capacity(self):
        """
        Ten coroutines each try to acquire weight 1 on a capacity-4 semaphore.
        At most 4 should ever hold the semaphore concurrently.
        """
        max_concurrent = 0
        current_holders = 0

        async def _worker(sem: WeightedSemaphore):
            nonlocal max_concurrent, current_holders
            await sem.acquire(1)
            current_holders += 1
            max_concurrent = max(max_concurrent, current_holders)
            await asyncio.sleep(0.01)
            current_holders -= 1
            await sem.release(1)

        async def _run():
            sem = WeightedSemaphore(capacity=4)
            tasks = [asyncio.create_task(_worker(sem)) for _ in range(10)]
            await asyncio.gather(*tasks)

        asyncio.run(_run())
        assert max_concurrent <= 4

    def test_large_and_normal_tasks_interleave(self):
        """
        Mix weight-2 (large) and weight-1 (normal) acquires on capacity 4.
        Verify no over-acquisition occurs.
        """
        errors: list[str] = []
        current_weight = 0
        CAPACITY = 4

        async def _worker(sem: WeightedSemaphore, weight: int):
            nonlocal current_weight
            await sem.acquire(weight)
            current_weight += weight
            if current_weight > CAPACITY:
                errors.append(f"Over capacity: {current_weight}")
            await asyncio.sleep(0.005)
            current_weight -= weight
            await sem.release(weight)

        async def _run():
            sem = WeightedSemaphore(capacity=CAPACITY)
            tasks = (
                [asyncio.create_task(_worker(sem, 2)) for _ in range(4)]
                + [asyncio.create_task(_worker(sem, 1)) for _ in range(4)]
            )
            await asyncio.gather(*tasks)

        asyncio.run(_run())
        assert errors == [], f"Capacity violated: {errors}"
