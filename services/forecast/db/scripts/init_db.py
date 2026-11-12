#!/usr/bin/env python3
"""
Database initialization script for forecast service.

Uses the common database initialization helpers to create the database and apply
init_schema.sql, then creates the restricted role the training worker connects
with and gives it column-level grants.

Environment Variables Required (in addition to the common POSTGRES_* ones):
- WORKER_POSTGRES_USER: name of the restricted worker role
- WORKER_POSTGRES_PASSWORD: password of the restricted worker role
"""

import sys
from pathlib import Path

from psycopg2 import sql

from common.db.scripts.init_db import (
    create_database,
    get_connection,
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


def setup_worker_role(
    host: str,
    port: str,
    dbname: str,
    user: str,
    password: str,
    worker_user: str,
    worker_password: str,
) -> bool:
    """Create or update the restricted worker role and apply its grants.

    The worker reads the connector (including its encrypted config, which it
    decrypts itself) and the training configuration. It reads the job's identity
    and writes only the job's result and message. It has no grant on
    forecasters, and the API owns every status change.
    """
    role = sql.Identifier(worker_user)
    database = sql.Identifier(dbname)

    # Revoking first makes the grants below the complete list on every run,
    # whatever an earlier run granted.
    grants = [
        sql.SQL("REVOKE ALL ON ALL TABLES IN SCHEMA public FROM {}").format(role),
        sql.SQL("GRANT CONNECT ON DATABASE {} TO {}").format(database, role),
        sql.SQL("GRANT USAGE ON SCHEMA public TO {}").format(role),
        sql.SQL("GRANT SELECT ON connectors TO {}").format(role),
        sql.SQL("GRANT SELECT ON training_configs TO {}").format(role),
        sql.SQL("GRANT SELECT (id, seq, connector_id, status) ON jobs TO {}").format(role),
        sql.SQL("GRANT UPDATE (result, message) ON jobs TO {}").format(role),
    ]

    try:
        with get_connection(host, port, dbname, user, password) as conn:
            cursor = conn.cursor()
            try:
                cursor.execute("SELECT rolsuper FROM pg_roles WHERE rolname = %s", (worker_user,))
                existing = cursor.fetchone()
                if existing is not None and existing[0]:
                    # Resetting a superuser's password would hand control of the
                    # cluster to whoever sets WORKER_POSTGRES_USER.
                    print(f"❌ Role '{worker_user}' is a superuser, refusing to reconfigure it")
                    return False
                if existing is None:
                    print(f"Creating role '{worker_user}'...")
                    cursor.execute(
                        sql.SQL("CREATE ROLE {} LOGIN PASSWORD {}").format(
                            role, sql.Literal(worker_password)
                        )
                    )
                else:
                    # Keeps the password in step with the environment on every run.
                    # The attributes are explicit, so a role that was created by hand
                    # with more rights is brought back to the restricted set.
                    print(f"Role '{worker_user}' already exists, updating its password")
                    cursor.execute(
                        sql.SQL(
                            "ALTER ROLE {} LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD {}"
                        ).format(
                            role, sql.Literal(worker_password)
                        )
                    )

                for grant in grants:
                    cursor.execute(grant)

                conn.commit()
                print(f"✅ Role '{worker_user}' configured")
                return True
            finally:
                cursor.close()
    except Exception as e:
        print(f"❌ Error configuring worker role: {e}")
        return False


def main() -> None:
    """Create the database and schema, then configure the worker role."""
    print("=" * 60)
    print("Forecast PostgreSQL Database Initialization")
    print("=" * 60)

    host = get_env_var('POSTGRES_HOST')
    port = get_env_var('POSTGRES_PORT', '5432')
    dbname = get_env_var('POSTGRES_DB')
    user = get_env_var('POSTGRES_USER')
    password = get_env_var('POSTGRES_PASSWORD')
    worker_user = get_env_var('WORKER_POSTGRES_USER')
    worker_password = get_env_var('WORKER_POSTGRES_PASSWORD')

    # setup_worker_role() resets the role's password and revokes its table rights.
    # Pointed at the owner, it would change the owner's password and leave the
    # "restricted" worker with full access. An empty password would create a role
    # that nobody can log in to, while the run still reports success.
    if not worker_user or worker_user == user:
        print("❌ WORKER_POSTGRES_USER must be set and different from POSTGRES_USER")
        sys.exit(1)
    if not worker_password:
        print("❌ WORKER_POSTGRES_PASSWORD must not be empty")
        sys.exit(1)

    print("Configuration:")
    print(f"  Host: {host}")
    print(f"  Port: {port}")
    print(f"  Database: {dbname}")
    print(f"  User: {user}")
    print(f"  Worker role: {worker_user}")
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

    if not setup_worker_role(
        host, port, dbname, user, password, worker_user, worker_password
    ):
        print("❌ Worker role setup failed")
        sys.exit(1)

    print()
    print("=" * 60)
    print("✅ Database initialization complete!")
    print("=" * 60)
    sys.exit(0)


if __name__ == '__main__':
    main()
