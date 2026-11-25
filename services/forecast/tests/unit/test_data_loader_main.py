"""
Unit tests of the data loader's entry point (data_loader/main.py).

The database is replaced by a fake session and the loader by a stand-in, so the tests check what the
entry point reads, what it hands to the loader, and which exit code and message each failure gets.
"""
import runpy
import sys
from types import SimpleNamespace
from uuid import UUID

import pytest
from sqlalchemy.sql.dml import Update

from forecast.data_loader import main as loader_main
from forecast.errors import EXIT_FAILURE, EXIT_OK, EXIT_PERMANENT, PermanentFailure, TransientFailure
from forecast.db.models import Connector, TrainingConfig

pytestmark = pytest.mark.unit

CONNECTOR_ID = UUID("c9e66790-7425-40de-944b-e07fc1f90ae7")
CONNECTION = {"host": "h", "password": "encrypted"}
CONFIG = TrainingConfig(
    connector_id=CONNECTOR_ID,
    source_table="SALES.ORDERS",
    target_column="LATE",
    input_columns=[{"name": "REGION"}, {"name": "QUANTITY"}],
)
CONNECTOR = Connector(id=CONNECTOR_ID, connection_details=CONNECTION)
FOUND = (CONNECTOR_ID, CONFIG, CONNECTOR)


class FakeSession:
    """Stands in for a Session: returns one row for the select, records the messages it is told to store."""

    def __init__(self, row=FOUND, fail_update=False):
        self.row = row
        self.fail_update = fail_update
        self.messages: list[str] = []
        self.closed = False

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False

    def begin(self):
        return self

    def close(self):
        self.closed = True

    def execute(self, statement, **kwargs):
        if isinstance(statement, Update):
            if self.fail_update:
                raise RuntimeError("database is down")
            self.messages.append(statement.compile().params["message"])
            return None
        return SimpleNamespace(first=lambda: self.row)


def stand_in_loader(monkeypatch, outcome=None):
    """Replace the loader and record its calls. ``outcome`` is an exception to raise, or None."""
    calls = []

    def load_training_data(**kwargs):
        calls.append(kwargs)
        if outcome:
            raise outcome
        return SimpleNamespace(rows_written=123)

    monkeypatch.setattr(loader_main, "load_training_data", load_training_data)
    return calls


def test_reads_the_job_and_hands_the_loader_what_it_needs(monkeypatch, tmp_path):
    calls = stand_in_loader(monkeypatch)
    session = FakeSession()
    output = tmp_path / "scratch" / "attempt" / "data.parquet"

    assert loader_main.main(["7", str(output)], lambda: session) == EXIT_OK

    assert calls == [
        {
            "table": "SALES.ORDERS",
            "columns": ["REGION", "QUANTITY", "LATE"],  # The input columns, then the target.
            "output_path": output,
            "connector_id": str(CONNECTOR_ID),
            "connection_details": CONNECTION,  # Still encrypted; only the loader decrypts it.
        }
    ]
    assert output.parent.is_dir()
    assert session.messages == []


def test_the_database_session_is_released_before_the_source_is_read(monkeypatch, tmp_path):
    session = FakeSession()
    seen = []
    monkeypatch.setattr(
        loader_main, "load_training_data", lambda **kwargs: seen.append(session.closed) or SimpleNamespace(rows_written=1)
    )

    loader_main.main(["7", str(tmp_path / "data.parquet")], lambda: session)

    assert seen == [True]


@pytest.mark.parametrize(
    "row, message",
    [
        (None, "Job 7 does not exist"),
        ((None, None, None), "The connector of job 7 does not exist"),  # ON DELETE SET NULL.
        ((CONNECTOR_ID, CONFIG, None), "The connector of job 7 does not exist"),
        ((CONNECTOR_ID, None, CONNECTOR), f"Connector {CONNECTOR_ID} has no training configuration"),
    ],
)
def test_a_job_that_cannot_be_read_fails_permanently(monkeypatch, tmp_path, row, message):
    calls = stand_in_loader(monkeypatch)
    session = FakeSession(row=row)

    assert loader_main.main(["7", str(tmp_path / "data.parquet")], lambda: session) == EXIT_PERMANENT

    assert calls == []
    assert session.messages == [message]


def test_a_permanent_failure_of_the_loader_exits_3_and_stores_its_message(monkeypatch, tmp_path):
    stand_in_loader(monkeypatch, PermanentFailure("Table SALES.ORDERS does not exist"))
    session = FakeSession()

    assert loader_main.main(["7", str(tmp_path / "data.parquet")], lambda: session) == EXIT_PERMANENT

    assert session.messages == ["Table SALES.ORDERS does not exist"]


def test_a_permanent_failure_is_logged_with_its_cause_and_stored_without_it(monkeypatch, tmp_path):
    try:
        raise RuntimeError("driver text for host 10.1.2.3")
    except RuntimeError as cause:
        failure = PermanentFailure("Authentication to the Db2 source failed.")
        failure.__cause__ = cause
    stand_in_loader(monkeypatch, failure)
    logged = []
    monkeypatch.setattr(loader_main.logger, "error", lambda *args, **kwargs: logged.append(kwargs))
    session = FakeSession()

    assert loader_main.main(["7", str(tmp_path / "data.parquet")], lambda: session) == EXIT_PERMANENT

    assert session.messages == ["Authentication to the Db2 source failed."]
    assert logged == [{"exc_info": failure}]  # The traceback, with the cause and its text, is in the log.


def test_a_transient_failure_of_the_loader_exits_1_and_stores_its_message(monkeypatch, tmp_path):
    stand_in_loader(monkeypatch, TransientFailure("Connection to the source was lost"))
    session = FakeSession()

    assert loader_main.main(["7", str(tmp_path / "data.parquet")], lambda: session) == EXIT_FAILURE

    assert session.messages == ["Connection to the source was lost"]


def test_an_unexpected_error_is_stored_as_a_fixed_text_without_its_details(monkeypatch, tmp_path):
    stand_in_loader(monkeypatch, OSError("connect to 10.1.2.3 as AIS failed"))
    session = FakeSession()

    assert loader_main.main(["7", str(tmp_path / "data.parquet")], lambda: session) == EXIT_FAILURE

    assert session.messages == ["Reading the source failed with an unexpected OSError. The details are in the loader log."]


def test_a_long_message_is_cut(monkeypatch, tmp_path):
    stand_in_loader(monkeypatch, PermanentFailure("x" * 5000))
    session = FakeSession()

    loader_main.main(["7", str(tmp_path / "data.parquet")], lambda: session)

    assert len(session.messages[0]) == 1000


def test_an_unreachable_database_does_not_change_the_exit_code(monkeypatch, tmp_path):
    stand_in_loader(monkeypatch, PermanentFailure("Table SALES.ORDERS does not exist"))
    session = FakeSession(fail_update=True)

    assert loader_main.main(["7", str(tmp_path / "data.parquet")], lambda: session) == EXIT_PERMANENT


def test_the_job_id_must_be_a_number(tmp_path):
    with pytest.raises(SystemExit) as raised:
        loader_main.parse_args(["seven", str(tmp_path)])

    assert raised.value.code == 2  # argparse's code, which the exit codes leave free.


def test_python_dash_m_runs_main(monkeypatch):
    monkeypatch.setattr(sys, "argv", ["forecast.data_loader", "--help"])

    with pytest.raises(SystemExit) as raised:
        runpy.run_module("forecast.data_loader", run_name="__main__")

    assert raised.value.code == 0


def test_without_a_session_factory_the_loader_uses_the_api_role_and_needs_a_configured_database(monkeypatch, tmp_path):
    from forecast.db import connection

    session = FakeSession()
    stand_in_loader(monkeypatch)
    checked = []
    monkeypatch.setattr(connection, "SessionLocal", lambda: session)  # The API's role, not the worker's.
    monkeypatch.setattr(connection, "check_db_configured", lambda: checked.append(True))

    assert loader_main.main(["7", str(tmp_path / "data.parquet")]) == EXIT_OK

    assert checked == [True]


def test_without_a_configured_database_the_loader_stops_before_it_reads_anything(monkeypatch, tmp_path):
    from forecast.db import connection

    calls = stand_in_loader(monkeypatch)

    def unconfigured():
        raise RuntimeError("Database connection is not configured.")

    monkeypatch.setattr(connection, "check_db_configured", unconfigured)

    with pytest.raises(RuntimeError, match="not configured"):
        loader_main.main(["7", str(tmp_path / "data.parquet")])

    assert calls == []
