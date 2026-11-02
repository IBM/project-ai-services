"""
Unit tests for the database initialization script.

The tests run without a real database. The schema file is checked as text and the
worker role setup is checked against a mocked connection.
"""

import re
from unittest.mock import MagicMock, patch

import pytest

from forecast.db.scripts import init_db

pytestmark = pytest.mark.unit

SCHEMA = init_db.SCHEMA_FILE.read_text()


def _flatten(query) -> str:
    """Render a psycopg2 SQL object as text without a live connection."""
    if isinstance(query, str):
        return query
    if isinstance(query, init_db.sql.Composed):
        return "".join(_flatten(part) for part in query)
    if isinstance(query, init_db.sql.Identifier):
        return '"' + '"."'.join(query.strings) + '"'
    if isinstance(query, init_db.sql.Literal):
        return repr(query.wrapped)
    return query.string


def _run_setup(role_exists: bool) -> list[str]:
    cursor = MagicMock()
    cursor.fetchone.return_value = (1,) if role_exists else None
    conn = MagicMock()
    conn.cursor.return_value = cursor
    connection = MagicMock()
    connection.__enter__.return_value = conn

    with patch.object(init_db, "get_connection", return_value=connection):
        assert init_db.setup_worker_role(
            "h", "5432", "forecast_metadata", "owner", "pw", "forecast_worker", "wpw"
        )
    return [_flatten(call.args[0]) for call in cursor.execute.call_args_list]


class TestSchemaFile:
    def test_expected_tables(self):
        assert init_db.EXPECTED_TABLES == {
            "connectors", "training_configs", "jobs", "forecasters",
        }

    @pytest.mark.parametrize("table", sorted(init_db.EXPECTED_TABLES))
    def test_every_table_is_created_idempotently(self, table):
        assert f"CREATE TABLE IF NOT EXISTS {table} (" in SCHEMA

    def test_every_statement_is_idempotent(self):
        lines = re.findall(r"^CREATE (?!OR REPLACE).*", SCHEMA, re.M)
        assert lines, "no CREATE statements found"
        for line in lines:
            assert "IF NOT EXISTS" in line, line

    def test_scheduler_schema_is_created(self):
        assert "CREATE SCHEMA IF NOT EXISTS scheduler;" in SCHEMA

    def test_seq_is_an_identity_column(self):
        assert re.search(r"seq\s+BIGINT\s+GENERATED ALWAYS AS IDENTITY", SCHEMA)

    def test_single_running_job_index(self):
        assert re.search(
            r"CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_single_running\s+"
            r"ON jobs \(status\) WHERE status = 'running';",
            SCHEMA,
        )

    def test_active_job_keeps_its_connector(self):
        assert re.search(
            r"CHECK \(status NOT IN \('queued', 'running'\) OR connector_id IS NOT NULL\)",
            SCHEMA,
        )

    def test_forecaster_name_is_unique(self):
        assert re.search(r"name\s+TEXT\s+NOT NULL UNIQUE", SCHEMA)

    def test_foreign_key_delete_rules(self):
        assert re.search(
            r"connector_id\s+UUID\s+REFERENCES connectors\(id\) ON DELETE SET NULL", SCHEMA
        )
        assert re.search(r"job_id\s+UUID\s+REFERENCES jobs\(id\) ON DELETE SET NULL", SCHEMA)


class TestWorkerRole:
    def test_creates_role_when_missing(self):
        statements = _run_setup(role_exists=False)
        assert any(s.startswith('CREATE ROLE "forecast_worker" LOGIN PASSWORD') for s in statements)
        assert not any(s.startswith("ALTER ROLE") for s in statements)

    def test_updates_password_when_role_exists(self):
        statements = _run_setup(role_exists=True)
        assert any(s.startswith('ALTER ROLE "forecast_worker" LOGIN PASSWORD') for s in statements)
        assert not any(s.startswith("CREATE ROLE") for s in statements)

    def test_grants_are_the_complete_list(self):
        grants = [s for s in _run_setup(role_exists=True) if s.startswith(("GRANT", "REVOKE"))]
        assert grants == [
            'REVOKE ALL ON ALL TABLES IN SCHEMA public FROM "forecast_worker"',
            'GRANT CONNECT ON DATABASE "forecast_metadata" TO "forecast_worker"',
            'GRANT USAGE ON SCHEMA public TO "forecast_worker"',
            'GRANT SELECT ON connectors TO "forecast_worker"',
            'GRANT SELECT ON training_configs TO "forecast_worker"',
            'GRANT SELECT (id, seq, connector_id, status) ON jobs TO "forecast_worker"',
            'GRANT UPDATE (result, message) ON jobs TO "forecast_worker"',
        ]

    def test_no_grant_on_forecasters(self):
        assert not any("forecasters" in s for s in _run_setup(role_exists=False))

    @pytest.mark.parametrize("worker_user", ["owner", ""])
    def test_main_refuses_owner_or_blank_worker_user(self, monkeypatch, worker_user):
        for name, value in {
            "POSTGRES_HOST": "h",
            "POSTGRES_DB": "forecast_metadata",
            "POSTGRES_USER": "owner",
            "POSTGRES_PASSWORD": "pw",
            "WORKER_POSTGRES_USER": worker_user,
            "WORKER_POSTGRES_PASSWORD": "wpw",
        }.items():
            monkeypatch.setenv(name, value)

        with patch.object(init_db, "wait_for_postgres") as wait, \
                patch.object(init_db, "setup_worker_role") as setup:
            with pytest.raises(SystemExit) as exit_info:
                init_db.main()

        assert exit_info.value.code == 1
        wait.assert_not_called()
        setup.assert_not_called()

    def test_failure_returns_false(self):
        with patch.object(init_db, "get_connection", side_effect=RuntimeError("boom")):
            assert not init_db.setup_worker_role(
                "h", "5432", "d", "u", "p", "forecast_worker", "wpw"
            )
