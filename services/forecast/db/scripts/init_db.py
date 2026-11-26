#!/usr/bin/env python3
"""
Database initialization script for forecast service.

Uses the common database initialization helpers to create the database and apply
init_schema.sql. The training worker has no database role: the API writes everything
the worker reports.
"""

import sys
from pathlib import Path

from common.db.scripts.init_db import (
    create_database,
    get_env_var,
    initialize_schema,
    wait_for_postgres,
)

# Script directory and schema file path
SCRIPT_DIR = Path(__file__).parent
SCHEMA_FILE = SCRIPT_DIR / 'init_schema.sql'

# Expected tables for forecast service
EXPECTED_TABLES = {
    'connectors',
    'training_configs',
    'jobs',
    'forecasters',
}


def main() -> None:
    """Create the database and apply the schema."""
    print("=" * 60)
    print("Forecast PostgreSQL Database Initialization")
    print("=" * 60)

    host = get_env_var('POSTGRES_HOST')
    port = get_env_var('POSTGRES_PORT', '5432')
    dbname = get_env_var('POSTGRES_DB')
    user = get_env_var('POSTGRES_USER')
    password = get_env_var('POSTGRES_PASSWORD')

    print("Configuration:")
    print(f"  Host: {host}")
    print(f"  Port: {port}")
    print(f"  Database: {dbname}")
    print(f"  User: {user}")
    print(f"  Schema file: {SCHEMA_FILE}")
    print(f"  Expected tables: {', '.join(sorted(EXPECTED_TABLES))}")
    print()

    if not wait_for_postgres(host, port, user, password):
        print("❌ Failed to connect to PostgreSQL server")
        sys.exit(1)

    if not create_database(host, port, user, password, dbname):
        print("❌ Failed to create database")
        sys.exit(1)

    if not initialize_schema(host, port, user, password, dbname, SCHEMA_FILE, EXPECTED_TABLES):
        print("❌ Database schema initialization failed")
        sys.exit(1)

    print()
    print("=" * 60)
    print("✅ Database initialization complete!")
    print("=" * 60)
    sys.exit(0)


if __name__ == '__main__':
    main()
