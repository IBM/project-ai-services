"""
Database configuration and session management for PostgreSQL.

Provides connection pooling, session factory, and database initialization
for the summarize service.
"""

from common.db.connection import get_connection_manager
from summarize.settings import settings

# Get configured connection objects from common database utilities
(
    engine,
    SessionLocal,
    ScopedSession,
    get_db_session,
    check_db_connection,
    close_db_connections
) = get_connection_manager("summarize_database", settings)


# Made with Bob
