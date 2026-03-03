"""
Periodic cleanup scheduler for resource management.
"""
import asyncio
import logging
from datetime import datetime
from typing import Optional

from common.misc_utils import get_logger
from digitize.resource_utils import (
    cleanup_staging_directory,
    cleanup_old_failed_jobs,
    log_resource_usage
)

logger = get_logger("cleanup_scheduler")

# Cleanup intervals in seconds
STAGING_CLEANUP_INTERVAL = 3600  # 1 hour
FAILED_JOBS_CLEANUP_INTERVAL = 86400  # 24 hours
RESOURCE_LOG_INTERVAL = 1800  # 30 minutes


class CleanupScheduler:
    """Manages periodic cleanup tasks."""
    
    def __init__(self, cache_dir: str = "/var/cache"):
        self.cache_dir = cache_dir
        self.staging_dir = f"{cache_dir}/staging"
        self._running = False
        self._tasks = []
    
    async def _periodic_staging_cleanup(self):
        """Periodically clean up old staging directories."""
        while self._running:
            try:
                logger.debug("Running periodic staging cleanup...")
                cleanup_staging_directory(self.staging_dir, force=False)
                await asyncio.sleep(STAGING_CLEANUP_INTERVAL)
            except asyncio.CancelledError:
                logger.info("Staging cleanup task cancelled")
                break
            except Exception as e:
                logger.error(f"Error in periodic staging cleanup: {e}")
                await asyncio.sleep(STAGING_CLEANUP_INTERVAL)
    
    async def _periodic_failed_jobs_cleanup(self):
        """Periodically clean up old failed jobs."""
        while self._running:
            try:
                logger.debug("Running periodic failed jobs cleanup...")
                count = cleanup_old_failed_jobs(self.cache_dir)
                if count > 0:
                    logger.info(f"Cleaned up {count} old failed jobs")
                await asyncio.sleep(FAILED_JOBS_CLEANUP_INTERVAL)
            except asyncio.CancelledError:
                logger.info("Failed jobs cleanup task cancelled")
                break
            except Exception as e:
                logger.error(f"Error in periodic failed jobs cleanup: {e}")
                await asyncio.sleep(FAILED_JOBS_CLEANUP_INTERVAL)
    
    async def _periodic_resource_logging(self):
        """Periodically log resource usage."""
        while self._running:
            try:
                log_resource_usage(self.cache_dir)
                await asyncio.sleep(RESOURCE_LOG_INTERVAL)
            except asyncio.CancelledError:
                logger.info("Resource logging task cancelled")
                break
            except Exception as e:
                logger.error(f"Error in periodic resource logging: {e}")
                await asyncio.sleep(RESOURCE_LOG_INTERVAL)
    
    async def start(self):
        """Start all periodic cleanup tasks."""
        if self._running:
            logger.warning("Cleanup scheduler is already running")
            return
        
        self._running = True
        logger.info("Starting cleanup scheduler...")
        
        # Create and start all periodic tasks
        self._tasks = [
            asyncio.create_task(self._periodic_staging_cleanup()),
            asyncio.create_task(self._periodic_failed_jobs_cleanup()),
            asyncio.create_task(self._periodic_resource_logging())
        ]
        
        logger.info("Cleanup scheduler started")
    
    async def stop(self):
        """Stop all periodic cleanup tasks."""
        if not self._running:
            return
        
        self._running = False
        logger.info("Stopping cleanup scheduler...")
        
        # Cancel all tasks
        for task in self._tasks:
            task.cancel()
        
        # Wait for all tasks to complete
        await asyncio.gather(*self._tasks, return_exceptions=True)
        
        self._tasks.clear()
        logger.info("Cleanup scheduler stopped")


# Global scheduler instance
_scheduler: Optional[CleanupScheduler] = None


def get_scheduler(cache_dir: str = "/var/cache") -> CleanupScheduler:
    """Get or create the global cleanup scheduler instance."""
    global _scheduler
    if _scheduler is None:
        _scheduler = CleanupScheduler(cache_dir)
    return _scheduler


async def start_cleanup_scheduler(cache_dir: str = "/var/cache"):
    """Start the global cleanup scheduler."""
    scheduler = get_scheduler(cache_dir)
    await scheduler.start()


async def stop_cleanup_scheduler():
    """Stop the global cleanup scheduler."""
    global _scheduler
    if _scheduler is not None:
        await _scheduler.stop()
