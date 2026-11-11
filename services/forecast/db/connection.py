"""
Database configuration and session management for PostgreSQL.

Provides connection pooling and a session factory for the Forecast service.
The tables are created by the init container, not by the application.
"""

from common.db.connection import get_connection_manager
from forecast.settings import settings

# Get configured connection objects from common database utilities. When the
# POSTGRES_* variables are missing, every entry is None.
(
    engine,
    SessionLocal,
    ScopedSession,
    get_db_session,
    _check_db_connection,
    _close_db_connections,
) = get_connection_manager("database", settings)


def close_db_connections() -> None:
    """Close every pooled database connection."""
    if _close_db_connections:
        _close_db_connections()
