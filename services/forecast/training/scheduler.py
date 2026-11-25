"""
Scheduling of connector training jobs.

APScheduler starts every training run, inside the API process. The job store is the service's
PostgreSQL database, in the ``scheduler`` schema that the init container creates, so scheduled jobs
survive a restart. ``POST /v1/connectors`` registers a one-off job that fires at once and runs the
connector's QUEUED job (orchestrator/run.py). The cron job of a connector with a schedule, and the
recovery after a restart, are Story 8.
"""
from datetime import datetime, timezone
from typing import Optional

from apscheduler.jobstores.sqlalchemy import SQLAlchemyJobStore
from apscheduler.schedulers.asyncio import AsyncIOScheduler
from sqlalchemy.engine import Engine

from common.misc_utils import get_logger

logger = get_logger("forecast_scheduler")

SCHEDULER_SCHEMA = "scheduler"
# A run that is late by more than this, for example because the API was down, is skipped, not run late.
MISFIRE_GRACE_S = 60

_scheduler: Optional[AsyncIOScheduler] = None


def start_scheduler(engine: Engine) -> AsyncIOScheduler:
    """Create the scheduler on the database ``engine`` and start it. Call it inside the running event loop."""
    global _scheduler
    store = SQLAlchemyJobStore(engine=engine, tableschema=SCHEDULER_SCHEMA)
    _scheduler = AsyncIOScheduler(jobstores={"default": store})
    _scheduler.start()
    logger.info("Training scheduler started")
    return _scheduler


def stop_scheduler() -> None:
    """Stop the scheduler without waiting for a running training: the API is shutting down."""
    global _scheduler
    scheduler, _scheduler = _scheduler, None
    if scheduler is not None and scheduler.running:
        scheduler.shutdown(wait=False)


def register_connector_job(connector_id: str, schedule: Optional[str]) -> None:
    """Register the first training of a connector, which fires at once.

    ``schedule`` is the connector's cron schedule. Registering it is Story 8, so it is not used yet.

    Raises:
        RuntimeError: the scheduler is not running.
    """
    if _scheduler is None:
        raise RuntimeError("The training scheduler is not running")
    _scheduler.add_job(
        "forecast.orchestrator.run:run_training",
        trigger="date",
        run_date=datetime.now(timezone.utc),
        args=[connector_id],
        id=f"train-first-{connector_id}",
        replace_existing=True,
        misfire_grace_time=MISFIRE_GRACE_S,
        max_instances=1,
    )
