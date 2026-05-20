"""
Database initialization script for summarize service.

Executes the SQL schema to create tables, indexes, and triggers.
"""

import os
from pathlib import Path

from sqlalchemy import text

from common.misc_utils import get_logger
from summarize.db.database import engine, check_db_connection

logger = get_logger("init_db")


def init_database() -> bool:
    """
    Initialize the database schema for summarize service.
    
    Reads and executes the init_schema.sql file to create:
    - summarize_jobs table
    - Constraints and indexes
    - Triggers for auto-updating timestamps
    
    Returns:
        True if initialization successful, False otherwise
    """
    if not engine:
        logger.error("Database engine not initialized. Set environment variables.")
        return False
    
    # Check connection first
    if not check_db_connection():
        logger.error("Database connection check failed")
        return False
    
    # Get path to SQL schema file
    script_dir = Path(__file__).parent
    schema_file = script_dir / "init_schema.sql"
    
    if not schema_file.exists():
        logger.error(f"Schema file not found: {schema_file}")
        return False
    
    try:
        # Read SQL schema
        with open(schema_file, 'r') as f:
            schema_sql = f.read()
        
        logger.info("Executing database schema initialization...")
        
        # Execute schema (idempotent - safe to run multiple times)
        with engine.connect() as conn:
            # Execute the entire SQL script
            conn.execute(text(schema_sql))
            conn.commit()
        
        logger.info("Database schema initialized successfully")
        return True
        
    except Exception as e:
        logger.error(f"Failed to initialize database schema: {e}", exc_info=True)
        return False


if __name__ == "__main__":
    """Run database initialization when executed directly."""
    success = init_database()
    exit(0 if success else 1)
