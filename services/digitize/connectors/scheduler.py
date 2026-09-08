"""
connectors/scheduler.py — APScheduler-backed connector sync scheduler.

Manages one recurring IntervalTrigger job per connector.  Each job calls
dispatch_sync() directly — the shared callable used by both the HTTP handler
and the scheduler.

Module-level state
------------------
_scheduler : AsyncIOScheduler | None
    Assigned by app.py lifespan before any connector endpoint is called.
    Must not be referenced at import time.

Usage
-----
    # Inside lifespan (app.py):
    sched = AsyncIOScheduler(jobstores={"default": job_store})
    import digitize.connectors.scheduler as scheduler_module
    scheduler_module._scheduler = sched
    await scheduler_module.register_connector_job(
        connector_id, interval_seconds, fire_immediately=True
    )
    sched.start()
    yield
    sched.shutdown()
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone

from apscheduler.schedulers.asyncio import AsyncIOScheduler
from apscheduler.triggers.interval import IntervalTrigger

from common.misc_utils import get_logger

logger = get_logger("connector_scheduler")

# ---------------------------------------------------------------------------
# Module-level scheduler singleton — set by lifespan in app.py
# ---------------------------------------------------------------------------

_scheduler: AsyncIOScheduler | None = None


def _get_scheduler() -> AsyncIOScheduler:
    if _scheduler is None:
        raise RuntimeError(
            "ConnectorScheduler not initialised — "
            "_scheduler must be set during application lifespan startup"
        )
    return _scheduler


# ---------------------------------------------------------------------------
# Public registration / removal helpers
# ---------------------------------------------------------------------------

async def register_connector_job(
    connector_id: str,
    interval_seconds: int,
    fire_immediately: bool = False,
) -> None:
    """Schedule (or reschedule) a recurring sync job for *connector_id*.

    Parameters
    ----------
    connector_id:
        UUID of the connector to schedule.
    interval_seconds:
        How often the tick should fire.
    fire_immediately:
        When True the first tick fires at ``now()`` instead of waiting
        one full interval.  Use this when attaching a new connector so
        the initial scan starts right away.
    """
    from digitize.api.v1.connectors import dispatch_sync

    now = datetime.now(timezone.utc)
    # APScheduler v3: IntervalTrigger always schedules the first run at
    # start_date + N*interval.
    # Passing next_run_time=now overrides the trigger's first computed fire time
    # so the job executes immediately, then repeats on the normal interval.
    start_date = now if fire_immediately else now + timedelta(seconds=interval_seconds)

    sched = _get_scheduler()
    sched.add_job(
        func=dispatch_sync,
        trigger=IntervalTrigger(seconds=interval_seconds, start_date=start_date),
        args=[connector_id],
        id=connector_id,
        replace_existing=True,
        next_run_time=now if fire_immediately else None,
    )
    logger.info(
        f"Registered scheduler job for connector {connector_id!r} "
        f"(interval={interval_seconds}s, fire_immediately={fire_immediately})"
    )


async def remove_connector_job(connector_id: str) -> None:
    """Remove the scheduled job for *connector_id*, if it exists.

    Silently ignores the case where no job is registered (e.g., the scheduler
    was restarted and the job was not re-registered before deletion).
    """
    sched = _get_scheduler()
    try:
        sched.remove_job(connector_id)
        logger.info(f"Removed scheduler job for connector {connector_id!r}")
    except Exception as exc:
        # JobLookupError or similar if the job doesn't exist — safe to ignore.
        logger.warning(
            f"Could not remove scheduler job for {connector_id!r} "
            f"(may not have been registered): {exc}"
        )
