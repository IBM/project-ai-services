"""
Scheduling of connector training jobs.

The APScheduler jobs (the first training at once, and the cron job for the
schedule) are registered in Story 7. Until then this is a no-op, so the create
endpoint already makes the call.
"""
from typing import Optional


def register_connector_job(connector_id: str, schedule: Optional[str]) -> None:
    """Register the training schedule of a connector. Not implemented yet (Story 7)."""
    return None
