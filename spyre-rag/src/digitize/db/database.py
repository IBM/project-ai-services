"""
Database configuration and session management for PostgreSQL.

Provides connection pooling, session factory, and database initialization.
"""

import os
from contextlib import contextmanager
from typing import Generator

from sqlalchemy import create_engine, event, Engine
from sqlalchemy.orm import sessionmaker, Session, scoped_session
from sqlalchemy.pool import NullPool, QueuePool

from common.misc_utils import get_logger

logger = get_logger("database")


# Database connection configuration from environment variables
def get_database_url() -> str:
    """
    Construct database URL from environment variables.
    
    Returns:
        PostgreSQL connection URL
        
    Raises:
        ValueError: If required environment variables are not set
    """
    host = os.getenv("POSTGRES_HOST")
    port = os.getenv("POSTGRES_PORT", "5432")
    database = os.getenv("POSTGRES_DB")
    user = os.getenv("POSTGRES_USER")
    password = os.getenv("POSTGRES_PASSWORD")
    
    if not all([host, database, user, password]):
        missing = []
        if not host:
            missing.append("POSTGRES_HOST")
        if not database:
            missing.append("POSTGRES_DB")
        if not user:
            missing.append("POSTGRES_USER")
        if not password:
            missing.append("POSTGRES_PASSWORD")
        raise ValueError(f"Missing required environment variables: {', '.join(missing)}")
    
    return f"postgresql://{user}:{password}@{host}:{port}/{database}"


# Connection pool configuration
DB_POOL_SIZE = int(os.getenv("DB_POOL_SIZE", "5"))
DB_MAX_OVERFLOW = int(os.getenv("DB_MAX_OVERFLOW", "5"))
DB_POOL_TIMEOUT = int(os.getenv("DB_POOL_TIMEOUT", "30"))
DB_POOL_RECYCLE = int(os.getenv("DB_POOL_RECYCLE", "3600"))  # 1 hour


def create_db_engine(echo: bool = False) -> Engine:
    """
    Create SQLAlchemy engine with connection pooling.
    
    Args:
        echo: If True, log all SQL statements
        
    Returns:
        SQLAlchemy Engine instance
    """
    database_url = get_database_url()
    
    # Create engine with connection pooling
    engine = create_engine(
        database_url,
        poolclass=QueuePool,
        pool_size=DB_POOL_SIZE,
        max_overflow=DB_MAX_OVERFLOW,
        pool_timeout=DB_POOL_TIMEOUT,
        pool_recycle=DB_POOL_RECYCLE,
        pool_pre_ping=True,  # Verify connections before using
        echo=echo,
        future=True  # Use SQLAlchemy 2.0 style
    )
    
    # Add connection event listeners
    @event.listens_for(engine, "connect")
    def receive_connect(dbapi_conn, connection_record):
        """Log new database connections."""
        logger.debug("New database connection established")
    
    @event.listens_for(engine, "close")
    def receive_close(dbapi_conn, connection_record):
        """Log closed database connections."""
        logger.debug("Database connection closed")
    
    return engine


# Create global engine instance
try:
    engine = create_db_engine(echo=False)
    logger.info("Database engine created successfully")
except ValueError as e:
    logger.warning(f"Database engine not initialized: {e}")
    logger.warning("Database operations will fail until environment variables are set")
    engine = None


# Session factory
SessionLocal = sessionmaker(
    autocommit=False,
    autoflush=False,
    bind=engine,
    future=True
) if engine else None


# Scoped session for thread-safe access
ScopedSession = scoped_session(SessionLocal) if SessionLocal else None


@contextmanager
def get_db_session() -> Generator[Session, None, None]:
    """
    Context manager for database sessions.
    
    Automatically handles session lifecycle:
    - Creates session
    - Commits on success
    - Rolls back on error
    - Closes session
    
    Usage:
        with get_db_session() as session:
            session.add(obj)
            # Automatic commit on exit
    
    Yields:
        SQLAlchemy Session
    """
    if not SessionLocal:
        raise RuntimeError("Database not initialized. Set environment variables and restart.")
    
    session = SessionLocal()
    try:
        yield session
        session.commit()
    except Exception:
        session.rollback()
        raise
    finally:
        session.close()


def init_db() -> None:
    """
    Initialize database schema.
    
    Creates all tables defined in models if they don't exist.
    This is typically called during application startup or migration.
    
    Note: For production, use Alembic migrations instead.
    """
    if not engine:
        raise RuntimeError("Database engine not initialized")
    
    from digitize.db.models import Base
    
    logger.info("Initializing database schema...")
    Base.metadata.create_all(bind=engine)
    logger.info("Database schema initialized successfully")


def check_db_connection() -> bool:
    """
    Check if database connection is working.
    
    Returns:
        True if connection successful, False otherwise
    """
    if not engine:
        logger.error("Database engine not initialized")
        return False
    
    try:
        with engine.connect() as conn:
            conn.execute("SELECT 1")
        logger.info("Database connection check: OK")
        return True
    except Exception as e:
        logger.error(f"Database connection check failed: {e}")
        return False


def close_db_connections() -> None:
    """
    Close all database connections.
    
    Should be called during application shutdown.
    """
    if engine:
        engine.dispose()
        logger.info("Database connections closed")


# Export commonly used components
__all__ = [
    "engine",
    "SessionLocal",
    "ScopedSession",
    "get_db_session",
    "init_db",
    "check_db_connection",
    "close_db_connections",
]

# Made with Bob
