"""
Unit tests of one training run (orchestrator/run.py).

The subprocesses are replaced by a script of outcomes (their own tests are in test_process.py and
test_data_loader_main.py), the database by a recording fake, and Triton by a stand-in. The
publish to the repository volume and the scratch directories are real, on a temporary directory.
"""
import asyncio
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID

import pytest
from sqlalchemy.exc import IntegrityError

from forecast.errors import EXIT_FAILURE, EXIT_PERMANENT
from forecast.orchestrator import run as orchestrator
from forecast.orchestrator.process import ProcessResult
from forecast.settings import settings
from forecast.triton_client.triton import TritonLoadError

pytestmark = pytest.mark.unit


CONNECTOR_ID = str(UUID("c9e66790-7425-40de-944b-e07fc1f90ae7"))
JOB_ID = 7
NAME = f"fc_{CONNECTOR_ID}_{JOB_ID}"

OK = ProcessResult(exit_code=0)
FAILS = ProcessResult(exit_code=EXIT_FAILURE)
FAILS_FOR_GOOD = ProcessResult(exit_code=EXIT_PERMANENT)
TIMED_OUT = ProcessResult(exit_code=-9, timed_out=True)
OUT_OF_MEMORY = ProcessResult(exit_code=-9, memory_exceeded=True)
KILLED = ProcessResult(exit_code=-9)


class FakeRuns:
    """Stands in for forecast.db.runs. Records the writes, and returns what the test sets."""

    def __init__(self, running=False, queued=JOB_ID, claimed=True):
        self.running, self.queued, self.claimed = running, queued, claimed
        self.message = None  # jobs.message
        self.written_messages = []
        self.completed = None
        self.failed = None
        self.claim_error = None
        self.complete_error = None

    def has_running_job(self, session, connector_id):
        return self.running

    def find_queued_job(self, session, connector_id):
        return self.queued

    def mark_running(self, session, job_id):
        if self.claim_error:
            raise self.claim_error
        return self.claimed

    def read_message(self, session, job_id):
        return self.message

    def set_message(self, session, job_id, message):
        self.message = message
        self.written_messages.append(message)

    def mark_completed(self, session, job_id, attempts):
        if self.complete_error:
            raise self.complete_error
        self.completed = (job_id, attempts)

    def mark_failed(self, session, job_id, message, attempts):
        self.failed = (job_id, message, attempts)


class Processes:
    """Stands in for run_process. ``loader`` and ``worker`` are the outcomes of the successive attempts.

    An outcome is a ProcessResult, or ``(ProcessResult, message)`` when the process wrote jobs.message
    before it ended. A process that ends well leaves its output where the real one would.
    """

    def __init__(self, fake_runs, loader=(OK,), worker=(OK,)):
        self.runs = fake_runs
        self.script = {"data loader": list(loader), "training worker": list(worker)}
        self.calls = []

    async def __call__(self, args, *, name, timeout_s, memory_limit_mb=None, env=None, **_):
        data_file = Path(args[-1]) if name == "data loader" else Path(args[-2])
        self.calls.append(
            SimpleNamespace(
                name=name,
                args=list(args),
                timeout_s=timeout_s,
                memory_limit_mb=memory_limit_mb,
                env=env,
                scratch=sorted(p.name for p in data_file.parent.iterdir()),  # What the attempt starts with.
                message=self.runs.message,
            )
        )
        outcome = self.script[name].pop(0)
        result, message = outcome if isinstance(outcome, tuple) else (outcome, None)
        if message:
            self.runs.message = message
        if result.ok and name == "data loader":
            data_file.write_bytes(b"parquet")
        if result.ok and name == "training worker":
            model = Path(args[-1])
            (model / "1").mkdir(parents=True)
            (model / "1" / "model.onnx").write_bytes(b"onnx")
            (model / "config.pbtxt").write_text("config")
        return result

    def of(self, name):
        return [call for call in self.calls if call.name == name]


@pytest.fixture
def env(tmp_path, monkeypatch):
    """Everything around a run: settings, a fake database, a script of subprocesses, Triton and sleep."""
    config = settings.forecast
    monkeypatch.setattr(config, "scratch_dir", tmp_path / "scratch")
    monkeypatch.setattr(config, "model_repo_path", tmp_path / "models")
    monkeypatch.setattr(config, "training_max_attempts", 2)
    monkeypatch.setattr(config, "training_retry_delay_seconds", 5)
    monkeypatch.setattr(config, "default_time_limit_seconds", 100)
    monkeypatch.setattr(config, "worker_memory_limit_mb", 2048)
    (tmp_path / "models").mkdir()

    fake_runs = FakeRuns()
    monkeypatch.setattr(orchestrator, "runs", fake_runs)

    @contextmanager
    def session():
        yield None

    monkeypatch.setattr(orchestrator, "get_db_session", session)

    sleeps = []

    async def sleep(seconds):
        sleeps.append(seconds)

    monkeypatch.setattr(orchestrator, "_sleep", sleep)

    loaded = []

    async def load_model(name):
        if isinstance(load_model.outcome, Exception):
            raise load_model.outcome
        loaded.append(name)

    load_model.outcome = None
    monkeypatch.setattr(orchestrator, "load_model", load_model)

    unloaded = []

    async def unload_model(name):
        # Recorded with the state of the repository at that moment: the unload comes before the removal.
        unloaded.append((name, (tmp_path / "models" / name).exists()))

    monkeypatch.setattr(orchestrator, "unload_model", unload_model)

    state = SimpleNamespace(
        runs=fake_runs,
        sleeps=sleeps,
        loaded=loaded,
        unloaded=unloaded,
        load_model=load_model,
        repository=tmp_path / "models",
        scratch=tmp_path / "scratch" / f"run-{JOB_ID}",
        processes=None,
    )

    def script(loader=(OK,), worker=(OK,)):
        state.processes = Processes(fake_runs, loader, worker)
        monkeypatch.setattr(orchestrator, "run_process", state.processes)
        return state.processes

    state.script = script
    script()
    return state


def run_training():
    asyncio.run(orchestrator.run_training(CONNECTOR_ID))


# ---------------------------------------------------------------------------
# A successful run
# ---------------------------------------------------------------------------


def test_a_successful_run_publishes_loads_and_completes_the_job(env):
    run_training()

    assert env.runs.completed == (JOB_ID, 1)
    assert env.runs.failed is None
    assert env.loaded == [NAME]
    assert env.unloaded == []
    assert (env.repository / NAME / "1" / "model.onnx").read_bytes() == b"onnx"
    assert (env.repository / NAME / "config.pbtxt").is_file()
    assert not (env.repository / ".staging" / str(JOB_ID)).exists()


def test_the_loader_runs_before_the_worker_on_the_same_data_file(env):
    run_training()

    loader, worker = env.processes.calls
    assert (loader.name, worker.name) == ("data loader", "training worker")
    assert loader.args[1:] == ["-m", "forecast.data_loader", str(JOB_ID), str(env.scratch / "data.parquet")]
    assert worker.args[1:] == [
        "-m", "forecast.training", str(JOB_ID), str(env.scratch / "data.parquet"), str(env.scratch / "model")
    ]


def test_the_worker_has_a_memory_limit_and_a_time_limit_and_the_loader_a_time_limit(env):
    config = settings.forecast

    run_training()

    loader, worker = env.processes.calls
    assert loader.timeout_s == config.connector_connect_timeout_s + config.connector_query_timeout_s + orchestrator.LOADER_GRACE_S
    assert loader.memory_limit_mb is None
    assert worker.timeout_s == 100 + orchestrator.WORKER_GRACE_S
    assert worker.memory_limit_mb == 2048


def test_the_worker_gets_no_database_login_of_the_api_and_no_encryption_key(env, monkeypatch):
    for name, value in {
        "POSTGRES_USER": "api", "POSTGRES_PASSWORD": "api-secret", "DB_ENCRYPTION_KEY": "key",
        "POSTGRES_HOST": "db", "WORKER_POSTGRES_USER": "forecast_worker", "WORKER_POSTGRES_PASSWORD": "w",
    }.items():
        monkeypatch.setenv(name, value)

    run_training()

    loader, worker = env.processes.calls
    assert loader.env is None  # The loader reads the connector, so it inherits everything.
    assert not {"POSTGRES_USER", "POSTGRES_PASSWORD", "DB_ENCRYPTION_KEY"} & set(worker.env)
    assert {"POSTGRES_HOST", "WORKER_POSTGRES_USER", "WORKER_POSTGRES_PASSWORD"} <= set(worker.env)


def test_the_scratch_directory_is_removed_after_a_successful_run(env):
    run_training()

    assert not env.scratch.exists()


def test_the_message_of_an_earlier_attempt_is_cleared_when_a_run_starts(env):
    env.runs.message = "left over"

    run_training()

    assert env.processes.calls[0].message is None


# ---------------------------------------------------------------------------
# Runs that are skipped
# ---------------------------------------------------------------------------


def test_a_connector_with_a_running_job_is_skipped_and_the_reason_logged(env, caplog):
    env.runs.running = True

    with caplog.at_level("WARNING"):
        run_training()

    assert env.processes.calls == []
    assert env.runs.completed is None and env.runs.failed is None
    assert "already has a RUNNING job" in caplog.text


def test_a_connector_without_a_queued_job_is_skipped(env, caplog):
    env.runs.queued = None

    with caplog.at_level("WARNING"):
        run_training()

    assert env.processes.calls == []
    assert "no QUEUED job" in caplog.text


def test_a_job_that_another_run_took_first_is_skipped(env):
    env.runs.claimed = False

    run_training()

    assert env.processes.calls == []


def test_a_second_running_job_in_the_database_is_skipped(env, caplog):
    env.runs.claim_error = IntegrityError("insert", {}, Exception("idx_jobs_single_running"))

    with caplog.at_level("WARNING"):
        run_training()

    assert env.processes.calls == []
    assert "another job is RUNNING" in caplog.text


def test_a_database_error_while_claiming_skips_the_run_and_logs_it(env, caplog):
    env.runs.claim_error = RuntimeError("connection pool timeout")

    with caplog.at_level("ERROR"):
        run_training()  # Does not raise, so APScheduler gets no error.

    assert env.processes.calls == []
    assert env.runs.failed is None
    assert "its job could not be claimed" in caplog.text


def test_two_runs_do_not_overlap(env, monkeypatch):
    active, most_active = 0, 0

    async def slow_process(args, *, name, **_):
        nonlocal active, most_active
        active += 1
        most_active = max(most_active, active)
        await asyncio.sleep(0.01)  # Another run would start here if the lock did not hold it back.
        active -= 1
        return FAILS_FOR_GOOD

    monkeypatch.setattr(orchestrator, "run_process", slow_process)
    monkeypatch.setattr(orchestrator, "_lock", asyncio.Lock())

    async def both():
        await asyncio.gather(orchestrator.run_training(CONNECTOR_ID), orchestrator.run_training(CONNECTOR_ID))

    asyncio.run(both())

    assert most_active == 1


# ---------------------------------------------------------------------------
# Failures and retries
# ---------------------------------------------------------------------------


def test_a_failure_that_would_only_repeat_is_not_retried(env):
    env.script(loader=[(FAILS_FOR_GOOD, "Table SALES.ORDERS does not exist")])

    run_training()

    assert len(env.processes.of("data loader")) == 1
    assert env.processes.of("training worker") == []
    assert env.runs.failed == (JOB_ID, "Table SALES.ORDERS does not exist", 1)
    assert env.sleeps == []
    assert env.loaded == [] and list(env.repository.iterdir()) == []


def test_a_failed_attempt_is_run_again_after_the_retry_delay(env):
    env.script(loader=[(FAILS, "Connection to the source was lost"), OK], worker=[OK])

    run_training()

    assert [call.name for call in env.processes.calls] == ["data loader", "data loader", "training worker"]
    assert env.sleeps == [5]
    assert env.runs.completed == (JOB_ID, 2)
    assert env.loaded == [NAME]


def test_a_retry_starts_from_an_empty_scratch_directory_and_without_the_last_message(env):
    env.script(
        loader=[OK, OK], worker=[(FAILS, "Training failed"), OK],
    )

    run_training()

    second_loader = env.processes.of("data loader")[1]
    assert second_loader.scratch == []  # The data file of the first attempt is gone.
    assert second_loader.message is None


def test_the_last_error_and_the_attempts_are_recorded_when_every_attempt_fails(env):
    env.script(loader=[OK, OK], worker=[(FAILS, "first error"), (FAILS, "last error")])

    run_training()

    assert env.runs.failed == (JOB_ID, "last error", 2)
    assert env.runs.completed is None
    assert len(env.processes.of("training worker")) == 2
    assert not env.scratch.exists()
    assert list(env.repository.iterdir()) == []


def test_a_single_attempt_means_no_retry(env, monkeypatch):
    monkeypatch.setattr(settings.forecast, "training_max_attempts", 1)
    env.script(loader=[(FAILS, "boom")])

    run_training()

    assert env.runs.failed == (JOB_ID, "boom", 1)
    assert env.sleeps == []


@pytest.mark.parametrize(
    "result, message",
    [
        (TIMED_OUT, "The training worker was stopped after the time limit of 400 s."),
        (OUT_OF_MEMORY, "The training worker was stopped after it used more than 2048 MB of memory."),
        (KILLED, "The training worker was killed by signal 9."),
    ],
)
def test_a_stopped_worker_is_retried_and_the_api_writes_the_message(env, result, message):
    env.script(loader=[OK, OK], worker=[result, result])

    run_training()

    assert len(env.processes.of("training worker")) == 2  # Not a failure that the worker called permanent.
    assert env.runs.failed == (JOB_ID, message, 2)
    assert message in env.runs.written_messages


def test_a_stopped_process_is_retried_even_when_it_was_killed_with_the_permanent_code(env):
    env.script(loader=[ProcessResult(exit_code=EXIT_PERMANENT, timed_out=True), OK])

    run_training()

    assert len(env.processes.of("data loader")) == 2


def test_a_stopped_loader_gets_its_message_too(env):
    env.script(loader=[TIMED_OUT, TIMED_OUT])

    run_training()

    limit = settings.forecast.connector_connect_timeout_s + settings.forecast.connector_query_timeout_s + 60
    assert env.runs.failed == (JOB_ID, f"The data loader was stopped after the time limit of {limit} s.", 2)


def test_a_stopped_process_does_not_keep_a_message_that_an_earlier_one_wrote(env):
    # The loader of attempt 1 wrote a message and failed. The worker of attempt 2 is then killed.
    env.script(loader=[(FAILS, "stale"), OK], worker=[TIMED_OUT])

    run_training()

    assert env.runs.failed[1].startswith("The training worker was stopped")


def test_a_process_that_leaves_no_message_gets_one_from_the_api(env):
    env.script(loader=[ProcessResult(exit_code=EXIT_FAILURE), ProcessResult(exit_code=EXIT_FAILURE)])

    run_training()

    assert env.runs.failed == (JOB_ID, "The data loader exited with code 1.", 2)
    assert "The data loader exited with code 1." in env.runs.written_messages


def test_a_long_message_is_cut(env):
    env.script(loader=[(FAILS_FOR_GOOD, "x" * 5000)])

    run_training()

    assert len(env.runs.failed[1]) == 1000


# ---------------------------------------------------------------------------
# After the worker: publish, Triton, the database
# ---------------------------------------------------------------------------


def test_a_forecaster_that_triton_cannot_load_is_removed_and_the_job_fails_without_a_retry(env):
    env.load_model.outcome = TritonLoadError("Triton could not load forecaster x: bad model")

    run_training()

    assert env.runs.failed == (JOB_ID, "Triton could not load forecaster x: bad model", 1)
    assert env.runs.completed is None
    assert not (env.repository / NAME).exists()
    # Unloaded first, with the directory still there: a load that timed out on the client may have completed.
    assert env.unloaded == [(NAME, True)]
    assert len(env.processes.of("training worker")) == 1  # The load is not retried.
    assert not env.scratch.exists()


def test_a_forecaster_that_cannot_be_recorded_is_removed_again(env):
    env.runs.complete_error = RuntimeError("password=secret host=10.1.2.3")

    run_training()

    assert env.loaded == [NAME]
    assert env.unloaded == [(NAME, True)]  # Loaded, so unloaded again before the directory goes.
    assert not (env.repository / NAME).exists()
    assert env.runs.failed == (
        JOB_ID, "The training run failed with an unexpected RuntimeError. The details are in the log.", 1
    )


async def _cancel_during_the_load(env, monkeypatch, cancel_the_unload_too):
    """Cancel run_training while Triton loads, and again while the handler unloads if asked to."""
    in_load, in_unload = asyncio.Event(), asyncio.Event()

    async def load_model(name):
        in_load.set()
        await asyncio.Event().wait()

    async def unload_model(name):
        env.unloaded.append((name, (env.repository / name).exists()))
        in_unload.set()
        if cancel_the_unload_too:
            await asyncio.Event().wait()

    monkeypatch.setattr(orchestrator, "load_model", load_model)
    monkeypatch.setattr(orchestrator, "unload_model", unload_model)
    task = asyncio.ensure_future(orchestrator.run_training(CONNECTOR_ID))
    await in_load.wait()
    task.cancel()
    if cancel_the_unload_too:
        await in_unload.wait()
        task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task


@pytest.mark.parametrize("cancel_the_unload_too", [False, True])
def test_a_cancel_during_the_load_unloads_and_removes_the_published_directory(env, monkeypatch, cancel_the_unload_too):
    asyncio.run(_cancel_during_the_load(env, monkeypatch, cancel_the_unload_too))

    # The unload ran first, with the directory in place. A second cancel, which ends the wait on Triton,
    # still lets the directory go.
    assert env.unloaded == [(NAME, True)]
    assert not (env.repository / NAME).exists()
    assert env.runs.completed is None
    assert not env.scratch.exists()


def test_a_publish_that_fails_fails_the_job_with_a_fixed_text(env):
    (env.repository / NAME).mkdir()  # The target exists.

    run_training()

    assert env.runs.failed == (
        JOB_ID, "The training run failed with an unexpected FileExistsError. The details are in the log.", 1
    )
    assert env.loaded == []
    assert env.unloaded == []  # Nothing was handed to Triton.


def test_an_unexpected_error_fails_the_job_and_its_details_stay_out_of_the_message(env, monkeypatch):
    async def explode(*args, **kwargs):
        raise OSError("connect to 10.1.2.3 as AIS failed")

    monkeypatch.setattr(orchestrator, "run_process", explode)

    run_training()

    assert env.runs.failed == (
        JOB_ID, "The training run failed with an unexpected OSError. The details are in the log.", 0
    )
    assert not env.scratch.exists()


def test_a_database_that_cannot_record_the_failure_does_not_raise(env, caplog):
    env.script(loader=[(FAILS_FOR_GOOD, "gone")])

    def broken(session, job_id, message, attempts):
        raise RuntimeError("database is down")

    env.runs.mark_failed = broken

    with caplog.at_level("ERROR"):
        run_training()

    assert "Could not mark job 7 FAILED" in caplog.text
