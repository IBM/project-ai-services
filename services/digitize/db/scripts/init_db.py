#!/usr/bin/env python3
"""
Database initialization script using Python and psycopg2.

This script:
1. Waits for PostgreSQL to be ready
2. Creates the database if it doesn't exist
3. Runs the schema initialization SQL

Environment Variables Required:
- POSTGRES_HOST: PostgreSQL host
- POSTGRES_PORT: PostgreSQL port (default: 5432)
- POSTGRES_DB: Database name to create
- POSTGRES_USER: PostgreSQL user
- POSTGRES_PASSWORD: PostgreSQL password
"""

import os
import sys
import time
from pathlib import Path
from contextlib import contextmanager
from typing import Generator

import psycopg2
from psycopg2 import sql
from psycopg2.extensions import ISOLATION_LEVEL_AUTOCOMMIT


# Script directory and schema file path
SCRIPT_DIR = Path(__file__).parent
SCHEMA_FILE = SCRIPT_DIR / 'init_schema.sql'


def get_env_var(name: str, default: str | None = None) -> str:
    """Get environment variable or exit if required and not found."""
    value = os.getenv(name, default)
    if value is None:
        print(f"❌ Error: Required environment variable {name} is not set")
        sys.exit(1)
    return value


@contextmanager
def get_connection(host: str, port: str, database: str, user: str, password: str) -> Generator:
    """Context manager for database connections."""
    conn = None
    try:
        conn = psycopg2.connect(
            host=host,
            port=port,
            database=database,
            user=user,
            password=password,
            connect_timeout=5
        )
        yield conn
    finally:
        if conn:
            conn.close()


def wait_for_postgres(host: str, port: str, user: str, password: str, max_attempts: int = 30) -> bool:
    """Wait for PostgreSQL to be ready."""
    print(f"Waiting for PostgreSQL at {host}:{port} to be ready...")
    
    for attempt in range(1, max_attempts + 1):
        try:
            # Try to connect to the default 'postgres' database
            with get_connection(host, port, 'postgres', user, password):
                print(f"✅ PostgreSQL is ready! (attempt {attempt}/{max_attempts})")
                return True
        except psycopg2.OperationalError:
            if attempt < max_attempts:
                print(f"Attempt {attempt}/{max_attempts}: PostgreSQL not ready yet, waiting...")
                time.sleep(2)
            else:
                print(f"❌ Failed to connect to PostgreSQL after {max_attempts} attempts")
                return False
    
    return False


def database_exists(conn, dbname: str) -> bool:
    """Check if database exists using existing connection."""
    cursor = conn.cursor()
    try:
        cursor.execute(
            "SELECT 1 FROM pg_database WHERE datname = %s",
            (dbname,)
        )
        return cursor.fetchone() is not None
    finally:
        cursor.close()


def create_database(host: str, port: str, user: str, password: str, dbname: str) -> bool:
    """Create database if it doesn't exist."""
    try:
        with get_connection(host, port, 'postgres', user, password) as conn:
            conn.set_isolation_level(ISOLATION_LEVEL_AUTOCOMMIT)

            if database_exists(conn, dbname):
                print(f"✅ Database '{dbname}' already exists")
                return True

            print(f"Creating database '{dbname}'...")
            cursor = conn.cursor()
            try:
                cursor.execute(
                    sql.SQL("CREATE DATABASE {}").format(sql.Identifier(dbname))
                )
                print(f"✅ Database '{dbname}' created successfully")
                return True
            finally:
                cursor.close()
    except Exception as e:
        print(f"❌ Error creating database: {e}")
        return False


def initialize_schema(host: str, port: str, user: str, password: str, dbname: str, sql_file: Path) -> bool:
    """Run schema SQL and verify tables using a single connection."""
    try:
        if not sql_file.exists():
            print(f"❌ Error: Schema file not found: {sql_file}")
            return False

        print(f"Reading schema from {sql_file}...")
        schema_sql = sql_file.read_text()

        # Use a single connection for both schema creation and verification
        with get_connection(host, port, dbname, user, password) as conn:
            cursor = conn.cursor()
            try:
                # Step 1: Execute schema SQL
                print(f"Executing schema SQL on database '{dbname}'...")
                cursor.execute(schema_sql)
                conn.commit()
                print("✅ Schema created successfully")

                # Step 2: Verify tables were created (reuse same connection)
                print("Verifying schema...")
                cursor.execute("""
                    SELECT table_name
                    FROM information_schema.tables
                    WHERE table_schema = 'public'
                    AND table_type = 'BASE TABLE'
                    ORDER BY table_name
                """)
                tables = [row[0] for row in cursor.fetchall()]

                expected_tables = {'jobs', 'documents'}
                found_tables = set(tables)

                if expected_tables.issubset(found_tables):
                    print(f"✅ Schema verification passed. Tables found: {', '.join(sorted(tables))}")
                    return True
                else:
                    missing = expected_tables - found_tables
                    print(f"❌ Schema verification failed. Missing tables: {', '.join(missing)}")
                    return False
            finally:
                cursor.close()

    except Exception as e:
        print(f"❌ Error during schema initialization: {e}")
        return False


def main():
    """Main initialization function."""
    print("=" * 60)
    print("PostgreSQL Database Initialization")
    print("=" * 60)

    # Get configuration from environment
    host = get_env_var('POSTGRES_HOST')
    port = get_env_var('POSTGRES_PORT', '5432')
    dbname = get_env_var('POSTGRES_DB')
    user = get_env_var('POSTGRES_USER')
    password = get_env_var('POSTGRES_PASSWORD')

    print(f"Configuration:")
    print(f"  Host: {host}")
    print(f"  Port: {port}")
    print(f"  Database: {dbname}")
    print(f"  User: {user}")
    print()

    # Step 1: Wait for PostgreSQL to be ready
    if not wait_for_postgres(host, port, user, password):
        print("❌ Failed to connect to PostgreSQL server")
        sys.exit(1)

    # Step 2: Create database if it doesn't exist
    if not create_database(host, port, user, password, dbname):
        print("❌ Failed to create database")
        sys.exit(1)

    # Step 3: Initialize schema and verify (single connection)
    if not initialize_schema(host, port, user, password, dbname, SCHEMA_FILE):
        print("❌ Database schema initialization failed")
        sys.exit(1)

    print()
    print("=" * 60)
    print("✅ Database initialization complete!")
    print("=" * 60)
    sys.exit(0)


if __name__ == '__main__':
    main()

# Made with Bob
