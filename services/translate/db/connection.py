"""
Database configuration and session management for PostgreSQL.

Provides connection pooling, session factory, and database initialization
for the translate service.
"""

from common.db.connection import get_connection_manager
from translate.settings import settings

# Get configured connection objects from common database utilities
(
    engine,
    SessionLocal,
    ScopedSession,
    get_db_session,
    check_db_connection,
    close_db_connections,
) = get_connection_manager("database", settings)

# Made with Bob
