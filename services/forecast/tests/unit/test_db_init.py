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


def _setup_cursor(existing_role):
    """Return a cursor whose role lookup yields existing_role: None or (rolsuper,)."""
    cursor = MagicMock()
    cursor.fetchone.return_value = existing_role
    conn = MagicMock()
    conn.cursor.return_value = cursor
    connection = MagicMock()
    connection.__enter__.return_value = conn
    return cursor, conn, connection


def _run_setup(role_exists: bool) -> list[str]:
    cursor, _, connection = _setup_cursor((False,) if role_exists else None)

    with patch.object(init_db, "get_connection", return_value=connection):
        assert init_db.setup_worker_role(
            "h", "5432", "forecast_metadata", "owner", "pw", "forecast_worker", "wpw"
        )
    return [_flatten(call.args[0]) for call in cursor.execute.call_args_list]


def _table_columns(table: str) -> set[str]:
    """Return the column names declared in the CREATE TABLE statement of a table."""
    body = re.search(rf"CREATE TABLE IF NOT EXISTS {table} \((.*?)\n\);", SCHEMA, re.S).group(1)
    columns = set()
    depth = 0  # Open parentheses: a multi-line constraint continues until they close.
    for line in body.splitlines():
        line = line.split("--")[0].strip()
        if not line:
            continue
        if depth == 0 and not line.upper().startswith("CONSTRAINT"):
            columns.add(line.split()[0])
        depth += line.count("(") - line.count(")")
    return columns


class TestSchemaFile:
    # The columns of the Database Tables section of the proposal. The training
    # configuration also keeps validation_metric, which the create request carries.
    EXPECTED_COLUMNS = {
        "connectors": {
            "id", "name", "type", "config", "created_at", "updated_at", "status", "message",
        },
        "training_configs": {
            "connector_id", "source_table", "target_column", "input_columns",
            "validation_metric", "schedule", "created_at",
        },
        "jobs": {
            "id", "seq", "connector_id", "status", "message", "result",
            "created_at", "started_at", "finished_at",
        },
        "forecasters": {"id", "job_id", "version", "name", "status", "metrics", "created_at"},
    }

    @pytest.mark.parametrize("table", sorted(EXPECTED_COLUMNS))
    def test_table_has_the_expected_columns(self, table):
        assert _table_columns(table) == self.EXPECTED_COLUMNS[table]

    def test_jobs_keeps_row_count_in_result_not_a_column(self):
        assert "total_rows" not in _table_columns("jobs")
        for key in ("rows_read", "algorithm", "metric", "validation_score",
                    "training_seconds", "attempts"):
            assert re.search(rf"^--\s+{key}\s", SCHEMA, re.M), key

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

    def test_connector_status_is_limited_to_the_uppercase_values(self):
        assert re.search(
            r"chk_connectors_status CHECK \(status IN "
            r"\('ACCEPTED', 'TRAINING', 'TRAINED', 'FAILED'\)\)",
            SCHEMA,
        )

    def test_schedule_is_limited_to_the_cadences(self):
        assert re.search(
            r"chk_training_configs_schedule CHECK \(\s*schedule IS NULL OR lower\(schedule\) IN "
            r"\('hourly', 'daily', 'weekly', 'monthly', 'yearly'\)\s*\)",
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

    def test_refuses_to_reconfigure_a_superuser_role(self, capsys):
        cursor, conn, connection = _setup_cursor((True,))
        with patch.object(init_db, "get_connection", return_value=connection):
            assert not init_db.setup_worker_role(
                "h", "5432", "forecast_metadata", "owner", "pw", "postgres", "wpw"
            )
        statements = [_flatten(call.args[0]) for call in cursor.execute.call_args_list]
        assert len(statements) == 1 and statements[0].startswith("SELECT rolsuper")
        conn.commit.assert_not_called()
        cursor.close.assert_called_once()
        out = capsys.readouterr().out
        assert "superuser" in out
        assert "wpw" not in out

    def test_updates_password_when_role_exists(self):
        statements = _run_setup(role_exists=True)
        assert any(
            s.startswith('ALTER ROLE "forecast_worker" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD')
            for s in statements
        )
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

    def test_failure_returns_false(self):
        with patch.object(init_db, "get_connection", side_effect=RuntimeError("boom")):
            assert not init_db.setup_worker_role(
                "h", "5432", "d", "u", "p", "forecast_worker", "wpw"
            )

    def test_password_is_never_printed(self, capsys):
        _run_setup(role_exists=False)
        _run_setup(role_exists=True)
        assert "wpw" not in capsys.readouterr().out

    def test_failure_message_does_not_print_the_password(self, capsys):
        with patch.object(init_db, "get_connection", side_effect=RuntimeError("boom")):
            init_db.setup_worker_role("h", "5432", "d", "u", "p", "forecast_worker", "wpw")
        assert "wpw" not in capsys.readouterr().out


class TestMain:
    @pytest.fixture(autouse=True)
    def _environment(self, monkeypatch):
        for name, value in {
            "POSTGRES_HOST": "h",
            "POSTGRES_DB": "forecast_metadata",
            "POSTGRES_USER": "owner",
            "POSTGRES_PASSWORD": "pw",
            "WORKER_POSTGRES_USER": "forecast_worker",
            "WORKER_POSTGRES_PASSWORD": "wpw",
        }.items():
            monkeypatch.setenv(name, value)

    @staticmethod
    def _run(**results):
        """Run main() with each step returning the given result; return the exit code."""
        steps = {
            "wait_for_postgres": True,
            "create_database": True,
            "initialize_schema": True,
            "setup_worker_role": True,
        }
        steps.update(results)
        patches = [patch.object(init_db, name, return_value=value) for name, value in steps.items()]
        for p in patches:
            p.start()
        try:
            with pytest.raises(SystemExit) as exit_info:
                init_db.main()
        finally:
            for p in patches:
                p.stop()
        return exit_info.value.code

    @pytest.mark.parametrize("worker_user", ["owner", ""])
    def test_refuses_owner_or_blank_worker_user(self, monkeypatch, worker_user):
        monkeypatch.setenv("WORKER_POSTGRES_USER", worker_user)

        with patch.object(init_db, "wait_for_postgres") as wait, \
                patch.object(init_db, "setup_worker_role") as setup:
            with pytest.raises(SystemExit) as exit_info:
                init_db.main()

        assert exit_info.value.code == 1
        wait.assert_not_called()
        setup.assert_not_called()

    def test_refuses_an_empty_worker_password(self, monkeypatch):
        monkeypatch.setenv("WORKER_POSTGRES_PASSWORD", "")

        with patch.object(init_db, "wait_for_postgres") as wait, \
                patch.object(init_db, "setup_worker_role") as setup:
            with pytest.raises(SystemExit) as exit_info:
                init_db.main()

        assert exit_info.value.code == 1
        wait.assert_not_called()
        setup.assert_not_called()

    def test_succeeds_when_every_step_succeeds(self):
        assert self._run() == 0

    @pytest.mark.parametrize(
        "failing_step",
        ["wait_for_postgres", "create_database", "initialize_schema", "setup_worker_role"],
    )
    def test_exits_non_zero_when_a_step_fails(self, failing_step):
        assert self._run(**{failing_step: False}) == 1

    def test_stops_at_the_first_failing_step(self):
        with patch.object(init_db, "wait_for_postgres", return_value=False), \
                patch.object(init_db, "create_database") as create, \
                patch.object(init_db, "initialize_schema") as schema, \
                patch.object(init_db, "setup_worker_role") as role:
            with pytest.raises(SystemExit):
                init_db.main()
        create.assert_not_called()
        schema.assert_not_called()
        role.assert_not_called()

    def test_password_is_never_printed(self, capsys):
        self._run()
        assert "wpw" not in capsys.readouterr().out
