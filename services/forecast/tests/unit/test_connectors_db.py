"""
Unit tests for the connector database helpers in forecast.db.connectors.

Tests cover:
 - _name_filter: LIKE wildcards and escape characters are properly formatted
 - lock_connectors: timeout and table lock are executed
 - connector_exists & connector_id_exists: scalar queries
 - insert_failed_connector: a FAILED connector without training details or job
 - insert_connector: ORM models are instantiated with correct fields and added to session
 - get_connector: with and without for_update row locking
 - list_connectors: pagination, count, and name filtering
 - update_connector: updates name and connection_details
 - list_ready_versions & list_jobs: ORM queries and sorting
"""
import uuid
from datetime import datetime, timezone
from unittest.mock import MagicMock

import pytest

from forecast.db import connectors as connectors_db
from forecast.db.models import Connector, Job, TrainingConfig

NOW = datetime(2026, 10, 26, 8, 55, tzinfo=timezone.utc)


@pytest.mark.unit
def test_lock_connectors_sets_timeout_and_locks_table():
    session = MagicMock(name="session")

    connectors_db.lock_connectors(session)

    assert session.execute.call_count == 2
    executed_statements = [str(call.args[0]) for call in session.execute.call_args_list]
    assert connectors_db.LOCK_TIMEOUT_SQL in executed_statements[0]
    assert "LOCK TABLE connectors IN SHARE ROW EXCLUSIVE MODE" in executed_statements[1]


@pytest.mark.unit
def test_connector_exists_returns_boolean():
    session = MagicMock(name="session")
    session.scalar.return_value = 1
    assert connectors_db.connector_exists(session) is True

    session.scalar.return_value = 0
    assert connectors_db.connector_exists(session) is False


@pytest.mark.unit
def test_connector_exists_counts_a_failed_connector_too():
    session = MagicMock(name="session")
    session.scalar.return_value = True

    assert connectors_db.connector_exists(session) is True

    stmt_str = str(session.scalar.call_args[0][0].compile(compile_kwargs={"literal_binds": True}))
    assert "status" not in stmt_str
    assert "WHERE" not in stmt_str


@pytest.mark.unit
def test_connector_id_exists_filters_on_the_id_whatever_the_status():
    session = MagicMock(name="session")
    session.scalar.return_value = True

    assert connectors_db.connector_id_exists(session, CONN_ID) is True

    statement = session.scalar.call_args[0][0]
    stmt_str = str(statement.compile(compile_kwargs={"literal_binds": True}))
    assert CONN_ID.replace("-", "") in stmt_str.replace("-", "")
    assert "status" not in stmt_str

    session.scalar.return_value = False
    assert connectors_db.connector_id_exists(session, CONN_ID) is False


@pytest.mark.unit
def test_insert_failed_connector_stores_only_the_connector():
    session = MagicMock(name="session")

    connectors_db.insert_failed_connector(
        session,
        connector_id=CONN_ID,
        name="test-conn",
        connector_type="db2_for_i",
        config={"host": "localhost", "password": "enc"},
        message="A connector already exists. Only one connector is supported.",
    )

    session.add.assert_called_once()
    session.add_all.assert_not_called()
    connector = session.add.call_args[0][0]
    assert isinstance(connector, Connector)
    assert connector.id == uuid.UUID(CONN_ID)
    assert connector.name == "test-conn"
    assert connector.connection_details == {"host": "localhost", "password": "enc"}
    assert connector.status == "FAILED"
    assert connector.message == "A connector already exists. Only one connector is supported."
    session.flush.assert_called_once()


@pytest.mark.unit
def test_insert_connector_adds_orm_models_to_session():
    session = MagicMock(name="session")
    conn_id = "11111111-1111-1111-1111-111111111111"
    conn_uuid = uuid.UUID(conn_id)

    connectors_db.insert_connector(
        session,
        connector_id=conn_id,
        name="test-conn",
        connector_type="db2_for_i",
        config={"host": "localhost", "password": "enc"},
        source_table="ORDERS",
        target_column="LATE",
        input_columns=["COL1"],
        schedule="daily",
    )

    session.add_all.assert_called_once()
    added_objects = session.add_all.call_args[0][0]
    assert len(added_objects) == 3

    connector, training_config, job = added_objects
    assert isinstance(connector, Connector)
    assert connector.id == conn_uuid
    assert connector.name == "test-conn"
    assert connector.type == "db2_for_i"
    assert connector.connection_details == {"host": "localhost", "password": "enc"}
    assert connector.status == "ACCEPTED"

    assert isinstance(training_config, TrainingConfig)
    assert training_config.connector_id == conn_uuid
    assert training_config.source_table == "ORDERS"
    assert training_config.target_column == "LATE"
    assert training_config.input_columns == ["COL1"]
    assert training_config.schedule == "daily"

    assert isinstance(job, Job)
    assert job.connector_id == conn_uuid
    assert job.status == "QUEUED"

    session.flush.assert_called_once()


@pytest.mark.unit
def test_name_filter_generates_ilike_clause():
    cond = connectors_db._name_filter("test%_")
    assert cond is not None
    # Verify the pattern inside the BinaryExpression
    assert "test\\%\\_" in str(cond.right.value)

    assert connectors_db._name_filter(None) is None


@pytest.mark.unit
def test_list_connectors_executes_query_and_returns_data_and_count():
    session = MagicMock(name="session")
    session.scalar.return_value = 1
    session.execute.return_value.mappings.return_value.all.return_value = [
        {
            "id": uuid.uuid4(), "name": "prod", "type": "db2_for_i", "status": "ACCEPTED",
            "message": None, "created_at": NOW, "last_trained_at": None,
            "source_table": "ORDERS", "target_column": "LATE",
        }
    ]

    rows, total = connectors_db.list_connectors(session, limit=10, offset=0, name="prod")

    assert len(rows) == 1
    assert isinstance(rows[0], connectors_db.ConnectorListRow)
    assert rows[0].source_table == "ORDERS"
    assert total == 1
    assert session.scalar.call_count == 1
    assert session.execute.call_count == 1


@pytest.mark.unit
def test_get_connector_without_for_update():
    session = MagicMock(name="session")
    conn_id = "11111111-1111-1111-1111-111111111111"
    session.execute.return_value.mappings.return_value.first.return_value = {
        "id": uuid.UUID(conn_id), "name": "prod", "type": "db2_for_i",
        "config": {"host": "h"}, "status": "ACCEPTED", "message": None,
        "created_at": NOW, "updated_at": NOW, "last_trained_at": None,
        "source_table": "ORDERS", "target_column": "LATE", "schedule": "daily",
    }

    result = connectors_db.get_connector(session, conn_id, for_update=False)

    assert isinstance(result, connectors_db.ConnectorRow)
    assert result.id == uuid.UUID(conn_id)
    assert result.config == {"host": "h"}
    assert result.schedule == "daily"
    assert session.execute.call_count == 1
    query_str = str(session.execute.call_args[0][0])
    assert "FOR UPDATE" not in query_str


@pytest.mark.unit
def test_get_connector_with_for_update_locks_row():
    session = MagicMock(name="session")
    conn_id = "11111111-1111-1111-1111-111111111111"
    session.execute.return_value.mappings.return_value.first.return_value = None

    result = connectors_db.get_connector(session, conn_id, for_update=True)

    assert result is None
    assert session.execute.call_count == 2
    executed_statements = [str(call.args[0]) for call in session.execute.call_args_list]
    assert connectors_db.LOCK_TIMEOUT_SQL in executed_statements[0]
    assert "FOR UPDATE" in executed_statements[1]


@pytest.mark.unit
def test_list_connectors_without_a_name_filter_has_no_where_clause():
    session = MagicMock(name="session")
    session.scalar.return_value = 0
    session.execute.return_value.mappings.return_value.all.return_value = []

    rows, total = connectors_db.list_connectors(session, limit=10, offset=0, name=None)

    assert (rows, total) == ([], 0)
    assert "WHERE" not in str(session.scalar.call_args[0][0])
    assert "WHERE" not in str(session.execute.call_args[0][0])


@pytest.mark.unit
def test_update_connector_without_a_name_leaves_the_name_alone():
    session = MagicMock(name="session")

    connectors_db.update_connector(
        session, "11111111-1111-1111-1111-111111111111", name=None, config={"host": "h"}
    )

    stmt_str = str(session.execute.call_args[0][0])
    assert "connection_details" in stmt_str
    assert "name=" not in stmt_str.replace(" ", "").lower()
    assert "SET name" not in stmt_str


@pytest.mark.unit
def test_update_connector_executes_update_statement():
    session = MagicMock(name="session")
    conn_id = "11111111-1111-1111-1111-111111111111"

    connectors_db.update_connector(
        session,
        conn_id,
        name="new-name",
        config={"host": "new-host"},
    )

    session.execute.assert_called_once()
    stmt_str = str(session.execute.call_args[0][0])
    assert "UPDATE connectors" in stmt_str
    assert "connection_details" in stmt_str


@pytest.mark.unit
def test_list_ready_versions_queries_forecasters():
    session = MagicMock(name="session")
    conn_id = "11111111-1111-1111-1111-111111111111"
    session.scalars.return_value.all.return_value = [2, 1]

    versions = connectors_db.list_ready_versions(session, conn_id)

    assert versions == [2, 1]
    session.scalars.assert_called_once()
    stmt_str = str(session.scalars.call_args[0][0])
    assert "forecasters.version" in stmt_str


@pytest.mark.unit
def test_list_jobs_queries_jobs():
    session = MagicMock(name="session")
    conn_id = "11111111-1111-1111-1111-111111111111"
    session.execute.return_value.mappings.return_value.all.return_value = [
        {
            "seq": 1, "status": "QUEUED", "message": None, "result": None,
            "created_at": NOW, "started_at": None, "finished_at": None,
        }
    ]

    jobs = connectors_db.list_jobs(session, conn_id)

    assert len(jobs) == 1
    assert isinstance(jobs[0], connectors_db.JobRow)
    assert jobs[0].seq == 1
    session.execute.assert_called_once()
    stmt_str = str(session.execute.call_args[0][0])
    assert "jobs" in stmt_str


CONN_ID = "11111111-1111-1111-1111-111111111111"
CONNECTOR_FIELDS = {
    "id": uuid.UUID(CONN_ID), "name": "prod", "type": "db2_for_i", "config": {"host": "h"},
    "status": "ACCEPTED", "message": None, "created_at": NOW, "updated_at": NOW,
    "last_trained_at": None, "source_table": "ORDERS", "target_column": "LATE",
    "schedule": "daily", "input_columns": ["REGION"],
}
FORECASTER_FIELDS = {"version": 3, "metrics": {"metric": "accuracy"}, "created_at": NOW}


@pytest.mark.unit
def test_get_default_connector_returns_the_oldest_with_its_input_columns():
    session = MagicMock(name="session")
    session.execute.return_value.mappings.return_value.first.return_value = CONNECTOR_FIELDS

    result = connectors_db.get_default_connector(session)

    assert isinstance(result, connectors_db.ConnectorRow)
    assert result.input_columns == ["REGION"]
    stmt_str = str(session.execute.call_args[0][0])
    assert "ORDER BY connectors.attached_at ASC" in stmt_str
    assert "LIMIT" in stmt_str
    assert "WHERE" not in stmt_str  # A FAILED connector is the default one too.


@pytest.mark.unit
def test_get_default_connector_without_a_connector_returns_none():
    session = MagicMock(name="session")
    session.execute.return_value.mappings.return_value.first.return_value = None

    assert connectors_db.get_default_connector(session) is None


@pytest.mark.unit
def test_list_forecasters_returns_rows_newest_first():
    session = MagicMock(name="session")
    session.execute.return_value.mappings.return_value.all.return_value = [FORECASTER_FIELDS]

    rows = connectors_db.list_forecasters(session, CONN_ID)

    assert rows == [connectors_db.ForecasterRow(**FORECASTER_FIELDS)]
    assert "ORDER BY forecasters.version DESC" in str(session.execute.call_args[0][0])


@pytest.mark.unit
def test_get_forecaster_filters_on_the_version():
    session = MagicMock(name="session")
    session.execute.return_value.mappings.return_value.first.return_value = FORECASTER_FIELDS

    row = connectors_db.get_forecaster(session, CONN_ID, 3)

    assert row.version == 3
    assert "forecasters.version =" in str(session.execute.call_args[0][0])


@pytest.mark.unit
def test_get_forecaster_unknown_version_returns_none():
    session = MagicMock(name="session")
    session.execute.return_value.mappings.return_value.first.return_value = None

    assert connectors_db.get_forecaster(session, CONN_ID, 9) is None


@pytest.mark.unit
def test_get_newest_forecaster_takes_the_highest_version():
    session = MagicMock(name="session")
    session.execute.return_value.mappings.return_value.first.return_value = FORECASTER_FIELDS

    row = connectors_db.get_newest_forecaster(session, CONN_ID)

    assert row.metrics == {"metric": "accuracy"}
    stmt_str = str(session.execute.call_args[0][0])
    assert "ORDER BY forecasters.version DESC" in stmt_str and "LIMIT" in stmt_str


@pytest.mark.unit
def test_get_newest_forecaster_without_one_returns_none():
    session = MagicMock(name="session")
    session.execute.return_value.mappings.return_value.first.return_value = None

    assert connectors_db.get_newest_forecaster(session, CONN_ID) is None
