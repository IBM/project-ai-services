"""
Database configuration and session management for PostgreSQL.

Provides connection pooling, session factory, and database initialization
that can be shared across multiple services.
"""

import os
import re
from contextlib import contextmanager
from typing import Generator, Tuple, Optional, Any
from urllib.parse import quote_plus

from sqlalchemy import create_engine, event, Engine, text
from sqlalchemy.orm import sessionmaker, Session, scoped_session
from sqlalchemy.pool import QueuePool

from common.misc_utils import get_logger


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
    # Type assertion: user and password are guaranteed to be str at this point due to validation above
    encoded_user = quote_plus(str(user))
    encoded_password = quote_plus(str(password))

    return f"postgresql://{encoded_user}:{encoded_password}@{host}:{port}/{database}"


def create_db_engine(
    logger_name: str = "database",
    pool_size: int = 5,
    max_overflow: int = 10,
    pool_timeout: int = 30,
    pool_recycle: int = 3600,
    echo: bool = False
) -> Engine:
    """
    Create SQLAlchemy engine with connection pooling.
    
    Args:
        logger_name: Name for the logger instance
        pool_size: Number of connections to maintain in the pool
        max_overflow: Maximum number of connections to create beyond pool_size
        pool_timeout: Seconds to wait before giving up on getting a connection
        pool_recycle: Seconds after which to recycle connections
        echo: If True, log all SQL statements
        
    Returns:
        SQLAlchemy Engine instance
    """
    logger = get_logger(logger_name)
    database_url = get_database_url()
    
    # Mask password in URL for logging (replace anything between : and @ with ****)
    safe_url = re.sub(r'://([^:]+):([^@]+)@', r'://\1:****@', database_url)
    logger.info(f"Database URL: {safe_url}")
    
    # Create engine with connection pooling
    engine = create_engine(
        database_url,
        poolclass=QueuePool,
        pool_size=pool_size,
        max_overflow=max_overflow,
        pool_timeout=pool_timeout,
        pool_recycle=pool_recycle,
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


def create_session_factory(engine: Engine) -> sessionmaker:
    """
    Create a session factory bound to the given engine.
    
    Args:
        engine: SQLAlchemy Engine instance
        
    Returns:
        Session factory
    """
    return sessionmaker(
        autocommit=False,
        autoflush=False,
        bind=engine,
        future=True
    )


def create_scoped_session_factory(session_factory: sessionmaker) -> scoped_session:
    """
    Create a scoped session factory for thread-safe access.
    
    Args:
        session_factory: Session factory
        
    Returns:
        Scoped session factory
    """
    return scoped_session(session_factory)


def create_session_context_manager(session_factory: sessionmaker):
    """
    Create a context manager function for database sessions.
    
    Args:
        session_factory: Session factory
        
    Returns:
        Context manager function
    """
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
        if not session_factory:
            raise RuntimeError("Database not initialized. Set environment variables and restart.")
        
        session = session_factory()
        try:
            yield session
            session.commit()
        except Exception:
            session.rollback()
            raise
        finally:
            session.close()
    
    return get_db_session


def create_connection_checker(engine: Optional[Engine], logger_name: str = "database"):
    """
    Create a database connection checker function.
    
    Args:
        engine: SQLAlchemy Engine instance
        logger_name: Name for the logger instance
        
    Returns:
        Connection checker function
    """
    logger = get_logger(logger_name)
    
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
    
    return check_db_connection


def create_connection_closer(engine: Optional[Engine], logger_name: str = "database"):
    """
    Create a database connection closer function.
    
    Args:
        engine: SQLAlchemy Engine instance
        logger_name: Name for the logger instance
        
    Returns:
        Connection closer function
    """
    logger = get_logger(logger_name)
    
    def close_db_connections() -> None:
        """
        Close all database connections.
        
        Should be called during application shutdown.
        """
        if engine:
            engine.dispose()
            logger.info("Database connections closed")
    
    return close_db_connections


def get_connection_manager(
    logger_name: str = "database",
    settings: Optional[Any] = None
) -> Tuple[Optional[Engine], Optional[sessionmaker], Optional[scoped_session], Any, Any, Any]:
    """
    Factory function to create all database connection objects.
    
    Args:
        logger_name: Name for the logger instance
        settings: Settings object with database configuration (optional)
        
    Returns:
        Tuple of (engine, SessionLocal, ScopedSession, get_db_session, 
                  check_db_connection, close_db_connections)
    """
    logger = get_logger(logger_name)
    
    try:
        # Get pool configuration from settings if available
        pool_config = {}
        if settings and hasattr(settings, 'database'):
            pool_config = {
                'pool_size': getattr(settings.database, 'pool_size', 5),
                'max_overflow': getattr(settings.database, 'max_overflow', 10),
                'pool_timeout': getattr(settings.database, 'pool_timeout', 30),
                'pool_recycle': getattr(settings.database, 'pool_recycle', 3600),
            }
        
        engine = create_db_engine(logger_name=logger_name, echo=False, **pool_config)
        logger.info("Database engine created successfully")
        
        session_factory = create_session_factory(engine)
        scoped_session_factory = create_scoped_session_factory(session_factory)
        get_db_session = create_session_context_manager(session_factory)
        check_db_connection = create_connection_checker(engine, logger_name)
        close_db_connections = create_connection_closer(engine, logger_name)
        
        return (
            engine,
            session_factory,
            scoped_session_factory,
            get_db_session,
            check_db_connection,
            close_db_connections
        )
    except ValueError as e:
        logger.warning(f"Database engine not initialized: {e}")
        logger.warning("Database operations will fail until environment variables are set")
        return (None, None, None, None, None, None)


# Made with Bob