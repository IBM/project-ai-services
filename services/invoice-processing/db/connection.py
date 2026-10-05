"""
Database connection and session factory for invoice processing service.
"""

from common.db.connection import get_connection_manager
from settings import settings

(
    engine,
    SessionLocal,
    ScopedSession,
    get_db_session,
    check_db_connection,
    close_db_connections,
) = get_connection_manager("invoice_processing_database", settings)
