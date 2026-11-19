"""
Unit tests of the training worker.

The database and the data loader are replaced by fakes. AutoGluon training and the ONNX
export run for real on a small table, so the tests also check that the exported graph
answers like the AutoGluon model.
"""
import stat
from types import SimpleNamespace
from uuid import UUID

import numpy as np
import onnxruntime as ort
import pandas as pd
import pytest

from forecast.errors import EXIT_FAILURE, EXIT_OK, EXIT_PERMANENT, PermanentFailure, TransientFailure
from forecast.db import jobs
from forecast.db.jobs import JobStore, TrainingJob
from forecast.db.models import Connector, Job, TrainingConfig
from forecast.settings import settings
from forecast.training import main as worker
from forecast.training.features import encode
from forecast.training.onnx_export import triton_config
from forecast.training.publish import forecaster_name, publish
from forecast.training.training import _split

pytestmark = pytest.mark.unit

CONNECTOR_ID = UUID("c9e66790-7425-40de-944b-e07fc1f90ae7")
JOB = TrainingJob(
    Job(seq=7, connector_id=CONNECTOR_ID),
    Connector(id=CONNECTOR_ID, type="db2_for_i", connection_details={"host": "h", "password": "encrypted"}),
    TrainingConfig(
        connector_id=CONNECTOR_ID,
        source_table="SALES.ORDERS",
        target_column="LATE",
        input_columns=[{"name": "REGION"}, {"name": "QUANTITY"}, {"name": "PRICE"}],
    ),
)


def make_table(rows: int = 400, target: str = "classification") -> pd.DataFrame:
    rng = np.random.default_rng(0)
    df = pd.DataFrame(
        {
            "REGION": rng.choice(["EMEA", "APAC", "AMER"], rows),
            "QUANTITY": rng.integers(1, 50, rows).astype("int64"),
            "PRICE": rng.random(rows) * 100,
        }
    )
    df.loc[::19, "PRICE"] = np.nan
    score = df.QUANTITY * df.PRICE.fillna(50) / 100 + df.REGION.map({"EMEA": 0, "APAC": 4, "AMER": 8})
    df["LATE"] = np.where(score > 25, "Y", "N") if target == "classification" else score
    return df


class FakeStore:
    """Stands in for JobStore and records the writes."""

    def __init__(self, job: TrainingJob = JOB):
        self.job = job
        self.results: list[dict] = []
        self.failures: list[str] = []

    def read(self, job_id):
        if isinstance(self.job, Exception):
            raise self.job
        return self.job

    def write_result(self, job_id, result):
        self.results.append(result)

    def write_failure(self, job_id, message):
        self.failures.append(message)


def as_inputs(df: pd.DataFrame, columns: list[str]) -> dict[str, np.ndarray]:
    """The inputs of the Triton model: every column is sent as text, one column per tensor."""
    return {name: df[[name]].astype(str).values.astype(object) for name in columns}


def loader_of(df: pd.DataFrame):
    return lambda job, path: df


@pytest.fixture
def volumes(tmp_path, monkeypatch):
    """Point the repository volume and the scratch directory at temporary directories."""
    repository, scratch = tmp_path / "models", tmp_path / "scratch"
    repository.mkdir()
    monkeypatch.setattr(settings.forecast, "model_repo_path", repository)
    monkeypatch.setattr(settings.forecast, "scratch_dir", scratch)
    monkeypatch.setattr(settings.forecast, "default_time_limit_seconds", 30)
    return SimpleNamespace(repository=repository, scratch=scratch)


# ---------------------------------------------------------------------------
# A successful run
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("target", ["classification", "regression"])
def test_run_publishes_a_complete_forecaster_that_answers_like_autogluon(volumes, target):
    df = make_table(target=target)
    store = FakeStore()

    published = worker.run(JOB.job_id, store, loader_of(df))

    assert published == volumes.repository / f"fc_{CONNECTOR_ID}_7"
    assert (published / "config.pbtxt").is_file()
    assert [p.name for p in volumes.repository.iterdir() if p.name != ".staging"] == [published.name]
    assert list((volumes.repository / ".staging").iterdir()) == []

    config = (published / "config.pbtxt").read_text()
    assert f'name: "fc_{CONNECTOR_ID}_7"' in config and 'backend: "onnxruntime"' in config

    session = ort.InferenceSession(str(published / "1" / "model.onnx"))
    assert [i.name for i in session.get_inputs()] == ["REGION", "QUANTITY", "PRICE"]
    assert {i.type for i in session.get_inputs()} == {"tensor(string)"}
    outputs = dict(
        zip(
            [o.name for o in session.get_outputs()],
            session.run(None, as_inputs(df, ["REGION", "QUANTITY", "PRICE"])),
        )
    )
    if target == "classification":
        assert set(outputs) == {"prediction", "probabilities"}
        assert set(outputs["prediction"]) <= {"Y", "N"}
        assert outputs["probabilities"].shape == (len(df), 2)
        np.testing.assert_allclose(outputs["probabilities"].sum(axis=1), 1.0, atol=1e-4)
    else:
        assert set(outputs) == {"prediction"}
        assert outputs["prediction"].shape == (len(df), 1)


@pytest.mark.parametrize("algorithm", ["GBM", "CAT"])
def test_exported_graph_matches_autogluon_and_maps_unseen_categories_to_missing(volumes, algorithm):
    from autogluon.features.generators import IdentityFeatureGenerator
    from autogluon.tabular import TabularPredictor

    from forecast.training.onnx_export import EXPORTABLE_HYPERPARAMETERS, export

    assert algorithm in EXPORTABLE_HYPERPARAMETERS
    try:  # LightGBM needs the OpenMP runtime (libgomp, or libomp on macOS), and AutoGluon skips it silently without.
        __import__({"GBM": "lightgbm", "CAT": "catboost"}[algorithm])
    except (ImportError, OSError) as exc:
        pytest.skip(f"{algorithm} cannot be imported here: {exc}")

    df = make_table()
    features, encoding = encode(df, ["REGION", "QUANTITY", "PRICE"])
    features["LATE"] = df.LATE
    predictor = TabularPredictor(label="LATE", path=str(volumes.scratch / "ag"), verbosity=0).fit(
        features,
        time_limit=20,
        hyperparameters={algorithm: {}},
        fit_weighted_ensemble=False,
        feature_generator=IdentityFeatureGenerator(),
    )
    assert predictor._trainer.load_model(predictor.model_best).ag_key == algorithm
    session = ort.InferenceSession(export(predictor, encoding).SerializeToString())

    def run(regions):
        n = len(regions)
        return session.run(
            None,
            {
                "REGION": np.array(regions, dtype=object).reshape(n, 1),
                "QUANTITY": np.full((n, 1), "10", dtype=object),
                "PRICE": np.full((n, 1), "50.5", dtype=object),
            },
        )

    labels, probabilities = run(["EMEA", "APAC", "AMER"])
    expected = pd.DataFrame(
        {"REGION": [0, 1, 2], "QUANTITY": [10, 10, 10], "PRICE": [50.5, 50.5, 50.5]}, dtype="float32"
    )
    # The sorted categories are AMER=0, APAC=1, EMEA=2.
    expected["REGION"] = [2, 1, 0]
    np.testing.assert_allclose(probabilities, predictor.predict_proba(expected).values, atol=1e-5)
    assert list(labels) == list(predictor.predict(expected))

    unseen = pd.DataFrame({"REGION": [np.nan], "QUANTITY": [10], "PRICE": [50.5]}, dtype="float32")
    _, unseen_probabilities = run(["MARS"])
    np.testing.assert_allclose(unseen_probabilities, predictor.predict_proba(unseen).values, atol=1e-5)


def test_run_records_rows_read_then_the_full_result(volumes):
    store = FakeStore()

    worker.run(JOB.job_id, store, loader_of(make_table(300)))

    assert store.results[0] == {"rows_read": 300}
    final = store.results[-1]
    assert set(final) == {
        "rows_read", "algorithm", "validation_metric", "validation_score", "test_score", "training_seconds"
    }
    assert final["rows_read"] == 300
    assert final["algorithm"] in {"LightGBM", "CatBoost"}
    assert final["validation_metric"] == "accuracy"
    assert 0 <= final["validation_score"] <= 1
    assert 0 <= final["test_score"] <= 1


def test_the_test_split_is_held_out_and_keeps_the_class_ratio():
    df = make_table(500)

    train_rows, test_rows = _split(df, "LATE")

    assert len(test_rows) == 100 and len(train_rows) == 400
    assert set(train_rows.index).isdisjoint(test_rows.index)
    assert test_rows.LATE.eq("Y").mean() == pytest.approx(df.LATE.eq("Y").mean(), abs=0.01)


def test_the_test_split_of_a_regression_target_or_a_single_row_class_is_random():
    regression = make_table(100, target="regression")
    rare_class = make_table(100)
    rare_class.loc[0, "LATE"] = "RARE"

    for df in (regression, rare_class):
        train_rows, test_rows = _split(df, "LATE")
        assert len(test_rows) == 20 and len(train_rows) == 80


def test_run_removes_its_scratch_directory_only(volumes):
    worker.run(JOB.job_id, FakeStore(), loader_of(make_table(200)))

    assert list(volumes.scratch.iterdir()) == []
    assert (volumes.repository / f"fc_{CONNECTOR_ID}_7").is_dir()


# ---------------------------------------------------------------------------
# Publishing
# ---------------------------------------------------------------------------


def test_publish_creates_directories_as_0755_and_files_as_0644(tmp_path, volumes):
    import os

    from onnx import TensorProto, helper

    os.umask(0o022)
    graph = helper.make_graph(
        [helper.make_node("Identity", ["x"], ["y"])],
        "g",
        [helper.make_tensor_value_info("x", TensorProto.FLOAT, [None, 1])],
        [helper.make_tensor_value_info("y", TensorProto.FLOAT, [None, 1])],
    )
    model = helper.make_model(graph, opset_imports=[helper.make_opsetid("", 13)])

    published = publish(volumes.repository, 5, "fc_x_5", model)

    modes = {p.relative_to(published): stat.S_IMODE(p.stat().st_mode) for p in [published, *published.rglob("*")]}
    assert modes == {
        published.relative_to(published): 0o755,
        published.joinpath("1").relative_to(published): 0o755,
        published.joinpath("1", "model.onnx").relative_to(published): 0o644,
        published.joinpath("config.pbtxt").relative_to(published): 0o644,
    }
    assert stat.S_IMODE((volumes.repository / ".staging").stat().st_mode) == 0o755


def test_publish_never_replaces_or_deletes_an_existing_forecaster(volumes):
    existing = volumes.repository / "fc_x_5"
    existing.mkdir()
    (existing / "keep").write_text("kept")

    with pytest.raises(FileExistsError):
        publish(volumes.repository, 5, "fc_x_5", None)

    assert (existing / "keep").read_text() == "kept"


def test_forecaster_name_is_connector_and_version():
    assert forecaster_name(CONNECTOR_ID, 12) == f"fc_{CONNECTOR_ID}_12"


# ---------------------------------------------------------------------------
# Failures and exit codes
# ---------------------------------------------------------------------------


def test_no_rows_is_a_permanent_failure_and_nothing_is_published(volumes):
    with pytest.raises(PermanentFailure, match="no rows"):
        worker.run(JOB.job_id, FakeStore(), loader_of(make_table(0)))

    assert list(volumes.repository.iterdir()) == []


def test_a_target_with_one_value_is_a_permanent_failure(volumes):
    df = make_table(100)
    df["LATE"] = "N"

    with pytest.raises(PermanentFailure, match="at least two different values"):
        worker.run(JOB.job_id, FakeStore(), loader_of(df))


@pytest.mark.parametrize(
    "error, code",
    [
        (PermanentFailure("Connector gone"), EXIT_PERMANENT),
        (TransientFailure("Connection to the source database was lost"), EXIT_FAILURE),
    ],
)
def test_main_maps_failures_to_exit_codes_and_stores_their_message(monkeypatch, error, code):
    store = FakeStore(error)
    monkeypatch.setattr(worker, "worker_engine", lambda: None)
    monkeypatch.setattr(worker, "JobStore", lambda engine: store)

    assert worker.main(["7"]) == code

    assert store.failures == [str(error)]


def test_main_stores_a_fixed_text_for_an_unexpected_error(monkeypatch):
    secret = "host ibmi.internal.example user forecast_ro password hunter2"
    store = FakeStore(RuntimeError(secret))
    monkeypatch.setattr(worker, "worker_engine", lambda: None)
    monkeypatch.setattr(worker, "JobStore", lambda engine: store)

    assert worker.main(["7"]) == EXIT_FAILURE

    (message,) = store.failures
    assert "RuntimeError" in message and "worker log" in message
    assert "hunter2" not in message


def test_main_logs_the_cause_of_a_permanent_failure_but_does_not_store_it(monkeypatch, caplog):
    cause = ValueError("row with customer 4711")
    error = PermanentFailure("AutoGluon could not train on this data (ValueError).")
    error.__cause__ = cause
    store = FakeStore(error)
    monkeypatch.setattr(worker, "worker_engine", lambda: None)
    monkeypatch.setattr(worker, "JobStore", lambda engine: store)

    with caplog.at_level("ERROR"):
        assert worker.main(["7"]) == EXIT_PERMANENT

    assert store.failures == [str(error)]
    assert "customer 4711" in caplog.text


def test_main_exits_zero_on_success(monkeypatch, tmp_path):
    published = tmp_path / "fc_x_7"
    published.mkdir()
    monkeypatch.setattr(worker, "worker_engine", lambda: None)
    monkeypatch.setattr(worker, "JobStore", lambda engine: FakeStore())
    monkeypatch.setattr(worker, "run", lambda job_id, store: published)

    assert worker.main(["7"]) == EXIT_OK


def test_main_still_exits_when_the_failure_cannot_be_stored(monkeypatch):
    store = FakeStore(RuntimeError("boom"))
    store.write_failure = lambda *_: (_ for _ in ()).throw(RuntimeError("database down"))
    monkeypatch.setattr(worker, "worker_engine", lambda: None)
    monkeypatch.setattr(worker, "JobStore", lambda engine: store)

    assert worker.main(["7"]) == EXIT_FAILURE


def test_main_stores_a_bounded_message(monkeypatch):
    store = FakeStore(TransientFailure("x" * 5000))
    monkeypatch.setattr(worker, "worker_engine", lambda: None)
    monkeypatch.setattr(worker, "JobStore", lambda engine: store)

    worker.main(["7"])

    assert len(store.failures[0]) == 1000


def test_a_table_with_too_few_labelled_rows_is_a_permanent_failure(volumes):
    df = make_table(100)
    df["LATE"] = ["Y", "N"] * 50
    df.loc[5:, "LATE"] = None  # Five rows keep a target value.

    with pytest.raises(PermanentFailure, match="Only 5 rows have a value in target column LATE"):
        worker.run(JOB.job_id, FakeStore(), loader_of(df))

    assert list(volumes.repository.iterdir()) == []


def test_the_split_is_random_when_there_are_more_classes_than_test_rows():
    df = make_table(30)
    df["LATE"] = [f"C{i % 10}" for i in range(30)]  # Ten classes, a test split of six rows.

    train_rows, test_rows = _split(df, "LATE")

    assert len(test_rows) == 6 and len(train_rows) == 24


def test_autogluon_failing_on_the_data_is_a_permanent_failure_without_its_message(volumes, monkeypatch):
    from autogluon.tabular import TabularPredictor

    def fail(*args, **kwargs):
        raise ValueError("class 'customer 4711' has too few rows")

    monkeypatch.setattr(TabularPredictor, "fit", fail)

    with pytest.raises(PermanentFailure, match=r"could not train on this data \(ValueError\)") as raised:
        worker.run(JOB.job_id, FakeStore(), loader_of(make_table(100)))

    assert "4711" not in str(raised.value)
    assert isinstance(raised.value.__cause__, ValueError)


# ---------------------------------------------------------------------------
# Database access
# ---------------------------------------------------------------------------


class FakeSession:
    """Stands in for a SQLAlchemy Session: returns one row and records the statements."""

    def __init__(self, row=None):
        self.row = row
        self.statements = []

    def __call__(self, engine):
        return self

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False

    def begin(self):
        return self

    def execute(self, statement, **kwargs):
        self.statements.append(statement)
        return SimpleNamespace(first=lambda: self.row)


@pytest.fixture
def session(monkeypatch):
    fake = FakeSession()
    monkeypatch.setattr(jobs, "Session", fake)
    return fake


def job_row(connector=True, config=True):
    return (
        Job(seq=7, connector_id=CONNECTOR_ID),
        Connector(id=CONNECTOR_ID, type="db2_for_i", connection_details={"host": "h"}) if connector else None,
        TrainingConfig(
            connector_id=CONNECTOR_ID,
            source_table="SALES.ORDERS",
            target_column="LATE",
            input_columns=[{"name": "REGION"}],
        )
        if config
        else None,
    )


def test_job_store_reads_the_job_connector_and_training_configuration(session):
    session.row = job_row()

    job = JobStore(None).read(7)

    assert job.job_id == 7
    assert (job.connector_id, job.connector_type, job.connection_details) == (
        CONNECTOR_ID, "db2_for_i", {"host": "h"}
    )
    assert (job.source_table, job.target_column) == ("SALES.ORDERS", "LATE")
    assert job.input_names == ["REGION"] and job.columns == ["REGION", "LATE"]


def test_job_store_reads_only_the_columns_of_jobs_that_the_worker_role_may_read(session):
    from sqlalchemy.dialects import postgresql

    session.row = job_row()
    JobStore(None).read(7)

    sql = str(session.statements[0].compile(dialect=postgresql.dialect()))
    selected = sql.split("FROM")[0]
    assert "jobs.seq" in selected and "jobs.connector_id" in selected
    assert not {"jobs.result", "jobs.message", "jobs.status", "jobs.created_at"} & set(selected.split())


@pytest.mark.parametrize(
    "row, message",
    [
        (None, "Job 7 does not exist"),
        (job_row(connector=False, config=False), "connector of job 7 does not exist"),
        (job_row(config=False), "has no training configuration"),
    ],
)
def test_job_store_reports_a_missing_job_connector_or_configuration_as_permanent(session, row, message):
    session.row = row

    with pytest.raises(PermanentFailure, match=message):
        JobStore(None).read(7)


def test_job_store_writes_only_jobs_result_and_jobs_message(session):
    from sqlalchemy.dialects import postgresql

    store = JobStore(None)

    store.write_result(7, {"rows_read": 1})
    store.write_failure(7, "boom")

    written = [
        {key.rsplit("_", 1)[0] for key in statement.compile(dialect=postgresql.dialect()).params}
        - {"seq"}
        for statement in session.statements
    ]
    assert written == [{"result"}, {"message"}]
    assert all(statement.table.name == "jobs" for statement in session.statements)


def test_triton_config_lists_every_tensor_of_the_graph():
    from onnx import TensorProto, helper

    graph = helper.make_graph(
        [helper.make_node("Identity", ["a"], ["label"])],
        "g",
        [helper.make_tensor_value_info("a", TensorProto.STRING, [None, 1])],
        [helper.make_tensor_value_info("label", TensorProto.STRING, [None])],
    )

    config = triton_config("fc_x_1", helper.make_model(graph))

    assert 'name: "a"' in config and "TYPE_STRING" in config
    assert 'name: "label"' in config and "max_batch_size: 0" in config
    assert "KIND_CPU" in config


def test_worker_engine_connects_as_the_worker_role_without_a_pool(monkeypatch):
    from pydantic import SecretStr
    from sqlalchemy.pool import NullPool

    monkeypatch.setattr(settings.forecast, "worker_postgres_user", "forecast_worker")
    monkeypatch.setattr(settings.forecast, "worker_postgres_password", SecretStr("worker-pw"))
    monkeypatch.setenv("POSTGRES_HOST", "db.internal")
    monkeypatch.setenv("POSTGRES_PORT", "6543")
    monkeypatch.setenv("POSTGRES_DB", "forecast_metadata")

    engine = jobs.worker_engine()

    url = engine.url
    assert (url.username, url.password, url.host, url.port, url.database) == (
        "forecast_worker", "worker-pw", "db.internal", 6543, "forecast_metadata"
    )
    assert isinstance(engine.pool, NullPool)


def test_worker_engine_uses_the_default_port(monkeypatch):
    monkeypatch.setenv("POSTGRES_HOST", "db.internal")
    monkeypatch.delenv("POSTGRES_PORT", raising=False)

    assert jobs.worker_engine().url.port == 5432


# ---------------------------------------------------------------------------
# ONNX export and the module entry point
# ---------------------------------------------------------------------------


def test_export_adds_the_opsets_that_the_converter_left_out(monkeypatch):
    from onnx import TensorProto, helper

    from forecast.training import onnx_export
    from forecast.training.features import Encoding

    def converter(model, n_features):
        graph = helper.make_graph(
            [helper.make_node("Identity", ["features"], ["variable"])],
            "tree",
            [helper.make_tensor_value_info("features", TensorProto.FLOAT, [None, n_features])],
            [helper.make_tensor_value_info("variable", TensorProto.FLOAT, [None, 1])],
        )
        tree = helper.make_model(graph)
        tree.ir_version = 3
        del tree.opset_import[:]  # A converter that declares no operator sets at all.
        return tree

    monkeypatch.setitem(onnx_export._CONVERTERS, "GBM", converter)
    predictor = SimpleNamespace(
        model_best="m",
        problem_type="regression",
        _trainer=SimpleNamespace(load_model=lambda name: SimpleNamespace(ag_key="GBM", model=None)),
    )

    model = onnx_export.export(predictor, Encoding(columns=["X"]))

    assert {opset.domain: opset.version for opset in model.opset_import} == {"": 13, "ai.onnx.ml": 1}
    assert model.ir_version == 8
    session = ort.InferenceSession(model.SerializeToString())
    (prediction,) = session.run(None, {"X": np.array([["2.5"]], dtype=object)})
    assert prediction.tolist() == [[2.5]]


def test_the_module_entry_point_exits_with_the_code_of_main(monkeypatch):
    import runpy
    import sys

    monkeypatch.setattr(worker, "main", lambda argv: EXIT_PERMANENT)
    monkeypatch.setattr(sys, "argv", ["forecast.training", "7"])

    with pytest.raises(SystemExit) as exited:
        runpy.run_module("forecast.training", run_name="__main__")

    assert exited.value.code == EXIT_PERMANENT
