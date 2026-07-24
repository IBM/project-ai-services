"""
Database configuration and session management for the extract service.

Thin wrapper around the common connection manager so that the extract
service has its own named connection pool and logger while reusing all
pooling, health-check, and session-context logic from common.
"""

from common.db.connection import get_connection_manager
from extract.settings import settings

(
    engine,
    SessionLocal,
    ScopedSession,
    get_db_session,
    check_db_connection,
    close_db_connections,
) = get_connection_manager("extract_database", settings)

# Made with Bob
