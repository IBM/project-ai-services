"""
Database configuration and session management for PostgreSQL.

Provides connection pooling and a session factory for the Forecast service.
The tables are created by the init container, not by the application.
"""

import threading
from concurrent.futures import Future
from concurrent.futures import TimeoutError as FutureTimeoutError
from typing import Optional

from common.db.connection import get_connection_manager
from common.misc_utils import get_logger
from forecast.settings import settings

logger = get_logger("forecast_db")

# /health must answer even when the database host does not respond. The engine
# has no connect timeout (the pool timeout is 30 seconds), so the probe is capped
# here instead of in common/.
DB_PROBE_TIMEOUT_S = 3.0

# Get configured connection objects from common database utilities. When the
# POSTGRES_* variables are missing, every entry is None and the service reports
# the database as unreachable instead of failing to start.
(
    engine,
    SessionLocal,
    ScopedSession,
    get_db_session,
    _check_db_connection,
    _close_db_connections,
) = get_connection_manager("database", settings)


# At most one probe runs at a time. A probe stuck on an unreachable host stays in
# flight, and later calls wait on that same probe instead of starting another thread.
_probe_lock = threading.Lock()
_probe_in_flight: Optional[Future] = None


def _run_probe(result: Future) -> None:
    """Run the database check and store its outcome in the given future."""
    try:
        result.set_result(bool(_check_db_connection()))
    except Exception as exc:
        # Only the exception class is logged: the message can contain the host or credentials.
        logger.warning(f"Database probe failed: {type(exc).__name__}")
        result.set_result(False)


def check_db_reachable(timeout: float = DB_PROBE_TIMEOUT_S) -> bool:
    """Return True when PostgreSQL answers a trivial query within the timeout."""
    global _probe_in_flight
    if not _check_db_connection:
        return False

    with _probe_lock:
        if _probe_in_flight is None or _probe_in_flight.done():
            _probe_in_flight = Future()
            # A daemon thread, so a probe stuck on a dead host cannot delay shutdown.
            threading.Thread(
                target=_run_probe, args=(_probe_in_flight,), name="db-probe", daemon=True
            ).start()
        probe = _probe_in_flight

    try:
        return probe.result(timeout=timeout)
    except FutureTimeoutError:
        logger.warning(f"Database probe did not answer within {timeout}s")
        return False


def close_db_connections() -> None:
    """Close every pooled database connection."""
    if _close_db_connections:
        _close_db_connections()
