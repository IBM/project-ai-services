"""
Unit tests for the database initialization script.

The tests run without a real database. The schema file is checked as text and main()
against patched steps.
"""

import re
from unittest.mock import patch

import pytest

from forecast.db.scripts import init_db

pytestmark = pytest.mark.unit

SCHEMA = init_db.SCHEMA_FILE.read_text()


def _table_columns(table: str) -> set[str]:
    """Return the column names declared in the CREATE TABLE statement of a table."""
    body = re.search(rf"CREATE TABLE IF NOT EXISTS {table} \((.*?)\n\);", SCHEMA, re.S).group(1)
    columns = set()
    depth = 0  # Open parentheses: a multi-line constraint continues until they close.
    for line in body.splitlines():
        line = line.split("--")[0].strip()
        if not line:
            continue
        if depth == 0 and not line.upper().startswith(("CONSTRAINT", "PRIMARY KEY")):
            columns.add(line.split()[0])
        depth += line.count("(") - line.count(")")
    return columns


class TestSchemaFile:
    # The columns of the Database Tables section of the proposal.
    EXPECTED_COLUMNS = {
        "connectors": {
            "id", "name", "type", "connection_details", "attached_at", "updated_at",
            "last_trained_at", "status", "message",
        },
        "training_configs": {
            "connector_id", "source_table", "target_column", "input_columns", "schedule",
        },
        "jobs": {
            "seq", "connector_id", "status", "message", "result",
            "created_at", "started_at", "finished_at",
        },
        "forecasters": {"connector_id", "job_id", "version", "metrics", "created_at"},
    }

    @pytest.mark.parametrize("table", sorted(EXPECTED_COLUMNS))
    def test_table_has_the_expected_columns(self, table):
        assert _table_columns(table) == self.EXPECTED_COLUMNS[table]

    def test_jobs_keeps_row_count_in_result_not_a_column(self):
        assert "total_rows" not in _table_columns("jobs")
        for key in ("rows_read", "algorithm", "training_seconds", "attempts"):
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
        assert re.search(r"seq\s+BIGINT\s+GENERATED ALWAYS AS IDENTITY PRIMARY KEY", SCHEMA)

    def test_single_running_job_index(self):
        assert re.search(
            r"CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_single_running\s+"
            r"ON jobs \(status\) WHERE status = 'RUNNING';",
            SCHEMA,
        )

    def test_active_job_keeps_its_connector(self):
        assert re.search(
            r"CHECK \(status NOT IN \('QUEUED', 'RUNNING'\) OR connector_id IS NOT NULL\)",
            SCHEMA,
        )

    def test_job_status_is_limited_to_the_uppercase_values(self):
        assert re.search(
            r"chk_jobs_status CHECK \(status IN \('QUEUED', 'RUNNING', 'COMPLETED', 'FAILED'\)\)",
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

    def test_forecasters_are_keyed_by_connector_and_version(self):
        assert re.search(r"PRIMARY KEY \(connector_id, version\)", SCHEMA)

    def test_foreign_key_delete_rules(self):
        assert re.search(
            r"connector_id\s+UUID\s+REFERENCES connectors\(id\) ON DELETE SET NULL", SCHEMA
        )
        assert re.search(r"job_id\s+BIGINT\s+REFERENCES jobs\(seq\) ON DELETE SET NULL", SCHEMA)


class TestMain:
    @pytest.fixture(autouse=True)
    def _environment(self, monkeypatch):
        for name, value in {
            "POSTGRES_HOST": "h",
            "POSTGRES_DB": "forecast_metadata",
            "POSTGRES_USER": "owner",
            "POSTGRES_PASSWORD": "pw",
        }.items():
            monkeypatch.setenv(name, value)

    @staticmethod
    def _run(**results):
        """Run main() with each step returning the given result; return the exit code."""
        steps = {
            "wait_for_postgres": True,
            "create_database": True,
            "initialize_schema": True,
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

    def test_succeeds_when_every_step_succeeds(self):
        assert self._run() == 0

    @pytest.mark.parametrize(
        "failing_step",
        ["wait_for_postgres", "create_database", "initialize_schema"],
    )
    def test_exits_non_zero_when_a_step_fails(self, failing_step):
        assert self._run(**{failing_step: False}) == 1

    def test_stops_at_the_first_failing_step(self):
        with patch.object(init_db, "wait_for_postgres", return_value=False), \
                patch.object(init_db, "create_database") as create, \
                patch.object(init_db, "initialize_schema") as schema:
            with pytest.raises(SystemExit):
                init_db.main()
        create.assert_not_called()
        schema.assert_not_called()

    def test_password_is_never_printed(self, monkeypatch, capsys):
        monkeypatch.setenv("POSTGRES_PASSWORD", "s3cret-owner-password")
        self._run()
        assert "s3cret-owner-password" not in capsys.readouterr().out
