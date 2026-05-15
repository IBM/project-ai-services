"""
Database configuration and session management for PostgreSQL.

Provides connection pooling, session factory, and database initialization.
"""

import os
import re
from contextlib import contextmanager
from typing import Generator
from urllib.parse import quote_plus

from sqlalchemy import create_engine, event, Engine, text
from sqlalchemy.orm import sessionmaker, Session, scoped_session
from sqlalchemy.pool import QueuePool

from common.misc_utils import get_logger
from digitize.settings import settings

logger = get_logger("database")


# Database connection configuration from environment variables
def get_database_url() -> str:
    """
    Construct database URL from environment variables.

    Properly URL-encodes credentials to handle special characters like @, :, /, etc.

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

    # URL-encode credentials to handle special characters (@, :, /, etc.)
    encoded_user = quote_plus(user)
    encoded_password = quote_plus(password)

    return f"postgresql://{encoded_user}:{encoded_password}@{host}:{port}/{database}"


def create_db_engine(echo: bool = False) -> Engine:
    """
    Create SQLAlchemy engine with connection pooling.
    
    Args:
        echo: If True, log all SQL statements
        
    Returns:
        SQLAlchemy Engine instance
    """
    database_url = get_database_url()
    # Mask password in URL for logging (replace anything between : and @ with ****)
    safe_url = re.sub(r'://([^:]+):([^@]+)@', r'://\1:****@', database_url)
    logger.info(f"db url = %s", safe_url)
    # Create engine with connection pooling
    engine = create_engine(
        database_url,
        poolclass=QueuePool,
        pool_size=settings.database.pool_size,
        max_overflow=settings.database.max_overflow,
        pool_timeout=settings.database.pool_timeout,
        pool_recycle=settings.database.pool_recycle,
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
            conn.execute(text("SELECT 1"))
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


# Made with Bob
