"""
Unit tests of the training scheduler (training/scheduler.py).

The job store is replaced by APScheduler's in-memory one, so the tests run the real scheduler on the
real event loop: the one-off job that POST /v1/connectors registers fires at once and calls the job
function with the connector id, also when the endpoint (a plain function in a worker thread) registers it.
"""
import asyncio
from unittest.mock import MagicMock

import pytest
from apscheduler.jobstores.memory import MemoryJobStore

from forecast.orchestrator import run as orchestrator
from forecast.training import scheduler

pytestmark = pytest.mark.unit

CONNECTOR_ID = "c9e66790-7425-40de-944b-e07fc1f90ae7"


@pytest.fixture
def store(monkeypatch):
    """The scheduler's job store, replaced by an in-memory one that records how it was made."""
    made = MagicMock(side_effect=lambda **kwargs: MemoryJobStore())
    monkeypatch.setattr(scheduler, "SQLAlchemyJobStore", made)
    monkeypatch.setattr(scheduler, "_scheduler", None)
    return made


@pytest.fixture
def fired(monkeypatch):
    """Replace the job function, which APScheduler resolves from its text reference when a job is added."""
    calls = []

    async def run_training(connector_id):
        calls.append(connector_id)

    monkeypatch.setattr(orchestrator, "run_training", run_training)
    return calls


async def until(condition, seconds=5.0):
    for _ in range(int(seconds / 0.05)):
        if condition():
            return True
        await asyncio.sleep(0.05)
    return False


def test_register_before_the_scheduler_started_is_an_error(store):
    with pytest.raises(RuntimeError, match="not running"):
        scheduler.register_connector_job(CONNECTOR_ID, None)


def test_the_job_store_is_the_scheduler_schema_of_the_service_database(store):
    engine = object()

    async def scenario():
        scheduler.start_scheduler(engine)
        scheduler.stop_scheduler()

    asyncio.run(scenario())

    store.assert_called_once_with(engine=engine, tableschema="scheduler")


def test_the_first_training_fires_at_once_with_the_connector_id(store, fired):
    async def scenario():
        scheduler.start_scheduler(object())
        try:
            scheduler.register_connector_job(CONNECTOR_ID, None)
            return await until(lambda: fired)
        finally:
            scheduler.stop_scheduler()

    assert asyncio.run(scenario())
    assert fired == [CONNECTOR_ID]


def test_the_endpoint_registers_the_job_from_a_worker_thread(store, fired):
    async def scenario():
        scheduler.start_scheduler(object())
        try:
            await asyncio.to_thread(scheduler.register_connector_job, CONNECTOR_ID, "daily")
            return await until(lambda: fired)
        finally:
            scheduler.stop_scheduler()

    assert asyncio.run(scenario())
    assert fired == [CONNECTOR_ID]


def test_the_job_is_registered_once_per_connector_and_skipped_when_late(store, fired):
    async def scenario():
        sched = scheduler.start_scheduler(object())
        try:
            # Paused, so that the job stays in the store to inspect.
            sched.pause()
            scheduler.register_connector_job(CONNECTOR_ID, None)
            scheduler.register_connector_job(CONNECTOR_ID, None)
            return sched.get_jobs()
        finally:
            scheduler.stop_scheduler()

    (job,) = asyncio.run(scenario())

    assert job.id == f"train-first-{CONNECTOR_ID}"
    assert job.args == (CONNECTOR_ID,)
    assert job.misfire_grace_time == scheduler.MISFIRE_GRACE_S
    assert job.max_instances == 1


def test_the_job_function_is_named_by_reference_so_that_the_job_store_can_pickle_it():
    # SQLAlchemyJobStore pickles the job, which works for a function that it can import again.
    import importlib

    module, _, name = "forecast.orchestrator.run:run_training".partition(":")
    assert getattr(importlib.import_module(module), name) is orchestrator.run_training


def test_stop_scheduler_is_safe_before_start_and_twice(store):
    scheduler.stop_scheduler()

    async def scenario():
        scheduler.start_scheduler(object())
        scheduler.stop_scheduler()
        scheduler.stop_scheduler()

    asyncio.run(scenario())

    assert scheduler._scheduler is None
