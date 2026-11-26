"""
Unit tests of the API's database access for a training run (db/runs.py).

The session is a mock, so the tests check what is written and the SQL that is sent. They do not
run the statements: the claim, and its conflict on idx_jobs_single_running, are not tested against
PostgreSQL.
"""
from unittest.mock import MagicMock
from uuid import UUID

import pytest
from sqlalchemy.dialects import postgresql

from forecast.db import runs
from forecast.db.models import Connector, Forecaster, Job, TrainingConfig
from forecast.errors import PermanentFailure

pytestmark = pytest.mark.unit

CONNECTOR_ID = UUID("c9e66790-7425-40de-944b-e07fc1f90ae7")


def sql(statement) -> str:
    return str(statement.compile(dialect=postgresql.dialect(), compile_kwargs={"literal_binds": True}))


def make_session(job=None, connector=None):
    session = MagicMock(name="session")
    objects = {(Job, 7): job, (Connector, CONNECTOR_ID): connector}
    session.get.side_effect = lambda model, key: objects[(model, key)]
    return session


def test_find_queued_job_takes_the_oldest_queued_job_of_the_connector():
    session = MagicMock()
    session.scalar.return_value = 7

    assert runs.find_queued_job(session, CONNECTOR_ID) == 7

    query = sql(session.scalar.call_args.args[0])
    assert "jobs.status = 'QUEUED'" in query and str(CONNECTOR_ID) in query
    assert "ORDER BY jobs.seq" in query and "LIMIT 1" in query


@pytest.mark.parametrize("found, expected", [(7, True), (None, False)])
def test_has_running_job(found, expected):
    session = MagicMock()
    session.scalar.return_value = found

    assert runs.has_running_job(session, CONNECTOR_ID) is expected
    assert "jobs.status = 'RUNNING'" in sql(session.scalar.call_args.args[0])


def test_mark_running_sets_the_job_running_and_the_connector_training():
    session = MagicMock()
    session.execute.return_value.scalar_one_or_none.return_value = CONNECTOR_ID

    assert runs.mark_running(session, 7) is True

    job_update, connector_update = (sql(call.args[0]) for call in session.execute.call_args_list)
    assert "UPDATE jobs SET status='RUNNING', message=NULL, started_at=" in job_update
    assert "jobs.seq = 7 AND jobs.status = 'QUEUED'" in job_update  # Only a job that is still QUEUED.
    assert "RETURNING jobs.connector_id" in job_update
    assert "UPDATE connectors SET status='TRAINING', message=NULL" in connector_update
    assert str(CONNECTOR_ID) in connector_update


def test_mark_running_leaves_a_job_that_is_no_longer_queued_alone():
    session = MagicMock()
    session.execute.return_value.scalar_one_or_none.return_value = None

    assert runs.mark_running(session, 7) is False

    assert session.execute.call_count == 1  # The connector is not touched.


def test_the_message_is_read_and_replaced():
    session = MagicMock()
    session.scalar.return_value = "Table X has no rows"

    assert runs.read_message(session, 7) == "Table X has no rows"
    runs.set_message(session, 7, None)

    assert "message=NULL" in sql(session.execute.call_args.args[0])


def test_an_attempt_starts_without_the_message_and_the_result_of_the_one_before():
    session = MagicMock()

    runs.start_attempt(session, 7)

    update = sql(session.execute.call_args.args[0])
    assert "UPDATE jobs SET message=NULL, result=NULL WHERE jobs.seq = 7" in update  # SQL NULL, not JSON null.


def test_the_result_that_the_worker_reported_replaces_jobs_result():
    session = MagicMock()

    runs.set_result(session, 7, {"rows_read": 400})

    compiled = session.execute.call_args.args[0].compile(dialect=postgresql.dialect())
    assert str(compiled).startswith("UPDATE jobs SET result=")
    assert compiled.params["result"] == {"rows_read": 400} and compiled.params["seq_1"] == 7


def job_row(connector=True, config=True):
    training_config = TrainingConfig(
        connector_id=CONNECTOR_ID,
        source_table="SALES.ORDERS",
        target_column="LATE",
        input_columns=[{"name": "REGION"}, {"name": "QUANTITY", "type": "number"}],
    )
    return (CONNECTOR_ID if connector else None, training_config if config else None)


def test_the_training_job_has_what_the_worker_needs_and_not_the_connector():
    session = MagicMock()
    session.execute.return_value.first.return_value = job_row()

    job = runs.read_training_job(session, 7)

    assert (job.job_id, job.connector_id, job.source_table, job.target_column) == (7, CONNECTOR_ID, "SALES.ORDERS", "LATE")
    assert job.input_names == ["REGION", "QUANTITY"] and job.columns == ["REGION", "QUANTITY", "LATE"]
    query = sql(session.execute.call_args.args[0])
    assert "connectors" not in query  # The credentials stay with the loader.


@pytest.mark.parametrize(
    "row, message",
    [
        (None, "Job 7 does not exist"),
        (job_row(connector=False, config=False), "connector of job 7 does not exist"),
        (job_row(config=False), "has no training configuration"),
    ],
)
def test_a_missing_job_connector_or_configuration_is_a_permanent_failure(row, message):
    session = MagicMock()
    session.execute.return_value.first.return_value = row

    with pytest.raises(PermanentFailure, match=message):
        runs.read_training_job(session, 7)


def test_mark_completed_records_the_forecaster_and_completes_the_job_and_the_connector():
    job = Job(
        seq=7,
        connector_id=CONNECTOR_ID,
        status="RUNNING",
        message="an old message",
        result={
            "rows_read": 400, "algorithm": "CatBoost", "training_seconds": 12.5,
            "validation_metric": "accuracy", "validation_score": 0.9, "test_score": 0.88,
        },
    )
    connector = Connector(id=CONNECTOR_ID, status="TRAINING", message="old")
    session = make_session(job, connector)

    runs.mark_completed(session, 7, attempts=2)

    (forecaster,) = [c.args[0] for c in session.add.call_args_list]
    assert isinstance(forecaster, Forecaster)
    assert (forecaster.connector_id, forecaster.job_id, forecaster.version) == (CONNECTOR_ID, 7, 7)
    assert forecaster.metrics == {"metric": "accuracy", "validation_score": 0.9, "training_seconds": 12.5}
    assert job.status == "COMPLETED" and job.message is None and job.finished_at is not None
    assert job.result["attempts"] == 2 and job.result["rows_read"] == 400  # What the worker wrote is kept.
    assert connector.status == "TRAINED" and connector.message is None
    assert connector.last_trained_at == job.finished_at


def test_mark_completed_tolerates_a_result_with_missing_keys():
    job = Job(seq=7, connector_id=CONNECTOR_ID, status="RUNNING", result=None)
    session = make_session(job, Connector(id=CONNECTOR_ID, status="TRAINING"))

    runs.mark_completed(session, 7, attempts=1)

    assert session.add.call_args.args[0].metrics == {}
    assert job.result == {"attempts": 1}


def test_mark_failed_keeps_what_the_worker_reported_and_fails_the_connector():
    job = Job(seq=7, connector_id=CONNECTOR_ID, status="RUNNING", result={"rows_read": 400})
    connector = Connector(id=CONNECTOR_ID, status="TRAINING", message=None)
    session = make_session(job, connector)

    runs.mark_failed(session, 7, "Table X has no rows", attempts=1)

    assert job.status == "FAILED" and job.message == "Table X has no rows" and job.finished_at is not None
    assert job.result == {"rows_read": 400, "attempts": 1}
    assert connector.status == "FAILED" and connector.message == "Table X has no rows"
    session.add.assert_not_called()


def test_mark_failed_fails_a_job_whose_connector_was_deleted():
    job = Job(seq=7, connector_id=None, status="RUNNING", result=None)
    session = make_session(job)

    runs.mark_failed(session, 7, "The connector of job 7 does not exist", attempts=1)

    assert job.status == "FAILED"
    session.get.assert_called_once_with(Job, 7)
