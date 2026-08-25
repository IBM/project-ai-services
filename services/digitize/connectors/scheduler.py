"""
connectors/scheduler.py — APScheduler-backed connector sync scheduler.

Manages one recurring IntervalTrigger job per connector.  Each job calls
dispatch_sync() directly — the shared callable used by both the HTTP handler
and the scheduler.

Module-level state
------------------
_scheduler : AsyncScheduler | None
    Assigned by app.py lifespan before any connector endpoint is called.
    Must not be referenced at import time.

Usage
-----
    # Inside lifespan (app.py):
    async with AsyncScheduler(data_store=data_store) as sched:
        import digitize.connectors.scheduler as scheduler_module
        scheduler_module._scheduler = sched
        await scheduler_module.register_connector_job(
            connector_id, interval_seconds, fire_immediately=True
        )
        yield

    # After lifespan exit:
    #   The ``async with`` block runs __aexit__, which stops the scheduler.
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone

from apscheduler import AsyncScheduler
from apscheduler.triggers.interval import IntervalTrigger

from common.misc_utils import get_logger

logger = get_logger("connector_scheduler")

# ---------------------------------------------------------------------------
# Module-level scheduler singleton — set by lifespan in app.py
# ---------------------------------------------------------------------------

_scheduler: AsyncScheduler | None = None


def _get_scheduler() -> AsyncScheduler:
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
    start_time = now if fire_immediately else now + timedelta(seconds=interval_seconds)

    sched = _get_scheduler()
    await sched.add_schedule(
        func_or_task_id=dispatch_sync,
        trigger=IntervalTrigger(seconds=interval_seconds, start_time=start_time),
        args=[connector_id],
        id=connector_id,
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
        await sched.remove_schedule(connector_id)
        logger.info(f"Removed scheduler job for connector {connector_id!r}")
    except Exception as exc:
        # LookupError or similar if the schedule doesn't exist — safe to ignore.
        logger.warning(
            f"Could not remove scheduler job for {connector_id!r} "
            f"(may not have been registered): {exc}"
        )
