"""
Database package for digitize service.

Provides ORM models, database configuration, and migration utilities.
"""

from digitize.db.models import Base, Job, Document
from digitize.db.database import (
    engine,
    SessionLocal,
    ScopedSession,
    get_db_session,
    init_db,
    check_db_connection,
    close_db_connections,
)

__all__ = [
    # Models
    "Base",
    "Job",
    "Document",
    # Database
    "engine",
    "SessionLocal",
    "ScopedSession",
    "get_db_session",
    "init_db",
    "check_db_connection",
    "close_db_connections",
]

# Made with Bob
