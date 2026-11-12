"""
Unit tests for the connector database helpers in forecast.db.connectors.

Tests cover:
 - _name_filter: LIKE wildcards and escape characters are properly formatted
 - lock_connectors: timeout and table lock are executed
 - connector_exists & count_queued_jobs: scalar count queries
 - insert_connector: ORM models are instantiated with correct fields and added to session
 - get_connector: with and without for_update row locking
 - list_connectors: pagination, count, and name filtering
 - update_connector: updates name and connection_details
 - list_ready_versions & list_jobs: ORM queries and sorting
"""
import uuid
from unittest.mock import MagicMock

import pytest

from forecast.db import connectors as connectors_db
from forecast.db.models import Connector, Job, TrainingConfig


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
def test_count_queued_jobs_returns_count():
    session = MagicMock(name="session")
    session.scalar.return_value = 3
    assert connectors_db.count_queued_jobs(session) == 3

    session.scalar.return_value = None
    assert connectors_db.count_queued_jobs(session) == 0


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
        input_columns=[{"name": "COL1", "type": "string"}],
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
    assert training_config.input_columns == [{"name": "COL1", "type": "string"}]
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
        {"id": uuid.uuid4(), "name": "prod", "created_at": None, "last_trained_at": None}
    ]

    rows, total = connectors_db.list_connectors(session, limit=10, offset=0, name="prod")

    assert len(rows) == 1
    assert total == 1
    assert session.scalar.call_count == 1
    assert session.execute.call_count == 1


@pytest.mark.unit
def test_get_connector_without_for_update():
    session = MagicMock(name="session")
    conn_id = "11111111-1111-1111-1111-111111111111"
    session.execute.return_value.mappings.return_value.first.return_value = {
        "id": uuid.UUID(conn_id),
        "name": "prod",
    }

    result = connectors_db.get_connector(session, conn_id, for_update=False)

    assert result == {"id": uuid.UUID(conn_id), "name": "prod"}
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
        {"seq": 1, "status": "QUEUED"}
    ]

    jobs = connectors_db.list_jobs(session, conn_id)

    assert len(jobs) == 1
    assert jobs[0]["seq"] == 1
    session.execute.assert_called_once()
    stmt_str = str(session.execute.call_args[0][0])
    assert "jobs" in stmt_str
