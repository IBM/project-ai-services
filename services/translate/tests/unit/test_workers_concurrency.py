"""
Unit tests for translate/workers/concurrency.py — ConcurrencyManager.
"""

import asyncio
import pytest
from unittest.mock import patch

from translate.workers.concurrency import ConcurrencyManager


@pytest.mark.unit
class TestConcurrencyManagerInitialize:
    def test_properties_raise_before_initialize(self):
        mgr = ConcurrencyManager()
        with pytest.raises(RuntimeError, match="initialize"):
            _ = mgr.job_limiter

    def test_chunk_semaphore_raises_before_initialize(self):
        mgr = ConcurrencyManager()
        with pytest.raises(RuntimeError, match="initialize"):
            _ = mgr.chunk_semaphore

    def test_vllm_semaphore_raises_before_initialize(self):
        mgr = ConcurrencyManager()
        with pytest.raises(RuntimeError, match="initialize"):
            _ = mgr.vllm_semaphore

    @pytest.mark.asyncio
    async def test_initialize_creates_semaphores(self):
        mgr = ConcurrencyManager()
        with patch("translate.workers.concurrency.settings") as mock_settings:
            mock_settings.translate.max_concurrent_jobs = 8
            mock_settings.translate.chunk_parallelism = 4
            mock_settings.common.llm.max_batch_size = 32
            mgr.initialize()

        assert isinstance(mgr.job_limiter, asyncio.BoundedSemaphore)
        assert isinstance(mgr.chunk_semaphore, asyncio.BoundedSemaphore)
        assert isinstance(mgr.vllm_semaphore, asyncio.BoundedSemaphore)

    @pytest.mark.asyncio
    async def test_semaphore_limits_match_settings(self):
        mgr = ConcurrencyManager()
        with patch("translate.workers.concurrency.settings") as mock_settings:
            mock_settings.translate.max_concurrent_jobs = 3
            mock_settings.translate.chunk_parallelism = 2
            mock_settings.common.llm.max_batch_size = 10
            mgr.initialize()

        # BoundedSemaphore._value holds the initial count
        assert mgr._job_limiter._value == 3
        assert mgr._chunk_semaphore._value == 2
        assert mgr._vllm_semaphore._value == 10


@pytest.mark.unit
class TestConcurrencyManagerStats:
    def test_stats_before_initialize_returns_none_values(self):
        mgr = ConcurrencyManager()
        with patch("translate.workers.concurrency.settings") as mock_settings:
            mock_settings.translate.max_concurrent_jobs = 8
            mock_settings.translate.chunk_parallelism = 4
            mock_settings.common.llm.max_batch_size = 32
            stats = mgr.stats()

        assert stats["job_limiter_locked"] is None
        assert stats["chunk_semaphore_locked"] is None
        assert stats["vllm_semaphore_locked"] is None

    @pytest.mark.asyncio
    async def test_stats_after_initialize_returns_bool_values(self):
        mgr = ConcurrencyManager()
        with patch("translate.workers.concurrency.settings") as mock_settings:
            mock_settings.translate.max_concurrent_jobs = 8
            mock_settings.translate.chunk_parallelism = 4
            mock_settings.common.llm.max_batch_size = 32
            mgr.initialize()
            stats = mgr.stats()

        assert isinstance(stats["job_limiter_locked"], bool)
        assert isinstance(stats["chunk_semaphore_locked"], bool)
        assert isinstance(stats["vllm_semaphore_locked"], bool)
        assert stats["job_limit"] == 8
        assert stats["chunk_parallelism"] == 4
        assert stats["vllm_limit"] == 32

    @pytest.mark.asyncio
    async def test_stats_shows_locked_when_semaphore_full(self):
        mgr = ConcurrencyManager()
        with patch("translate.workers.concurrency.settings") as mock_settings:
            mock_settings.translate.max_concurrent_jobs = 1
            mock_settings.translate.chunk_parallelism = 1
            mock_settings.common.llm.max_batch_size = 1
            mgr.initialize()

        async def _acquire_and_check():
            async with mgr.job_limiter:
                return mgr.job_limiter.locked()

        locked = await _acquire_and_check()
        # After acquiring the single slot the semaphore is locked
        assert locked is True


@pytest.mark.unit
class TestConcurrencyManagerSemaphoreUsage:
    @pytest.mark.asyncio
    async def test_vllm_semaphore_can_be_acquired_and_released(self):
        mgr = ConcurrencyManager()
        with patch("translate.workers.concurrency.settings") as mock_settings:
            mock_settings.translate.max_concurrent_jobs = 8
            mock_settings.translate.chunk_parallelism = 4
            mock_settings.common.llm.max_batch_size = 32
            mgr.initialize()

        assert not mgr.vllm_semaphore.locked()
        async with mgr.vllm_semaphore:
            pass  # acquired and released
        assert not mgr.vllm_semaphore.locked()

# Made with Bob
