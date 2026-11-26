"""
Unit tests of the training worker.

The worker has no database: the job file and the Parquet file of the data loader are its input, and
result.json and failure.json its report. AutoGluon training and the ONNX export run for real on a
small table, so the tests also check that the exported graph answers like the AutoGluon model.
"""
import stat
from types import SimpleNamespace
from uuid import UUID

import numpy as np
import onnxruntime as ort
import pandas as pd
import pytest

from forecast.errors import EXIT_FAILURE, EXIT_OK, EXIT_PERMANENT, PermanentFailure, TransientFailure
from forecast.settings import settings
from forecast.training import main as worker
from forecast.training.features import encode
from forecast.training.onnx_export import triton_config
from forecast.training.model_dir import write_model_dir
from forecast.utils import job_files
from forecast.utils.job_files import TrainingJob
from forecast.utils.names import forecaster_name
from forecast.training.training import _split

pytestmark = pytest.mark.unit

CONNECTOR_ID = UUID("c9e66790-7425-40de-944b-e07fc1f90ae7")
JOB = TrainingJob(
    job_id=7,
    connector_id=CONNECTOR_ID,
    source_table="SALES.ORDERS",
    target_column="LATE",
    input_names=["REGION", "QUANTITY", "PRICE"],
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


def as_inputs(df: pd.DataFrame, columns: list[str]) -> dict[str, np.ndarray]:
    """The inputs of the Triton model: every column is sent as text, one column per tensor."""
    return {name: df[[name]].astype(str).values.astype(object) for name in columns}


def run_worker(volumes, df: pd.DataFrame, job: TrainingJob = JOB):
    """Run the worker on ``df``, which the loader would have written to a Parquet file."""
    df.to_parquet(volumes.data)
    job_file = job_files.write_job(volumes.report, job)
    return worker.run(job_file, volumes.data, volumes.output)


@pytest.fixture
def volumes(tmp_path, monkeypatch):
    """Point the scratch directory at a temporary one and name the worker's input, output and report directory."""
    scratch = tmp_path / "scratch"
    monkeypatch.setattr(settings.forecast, "scratch_dir", scratch)
    monkeypatch.setattr(settings.forecast, "default_time_limit_seconds", 30)
    report = tmp_path / "run-7"
    report.mkdir()
    return SimpleNamespace(
        scratch=scratch, data=tmp_path / "data.parquet", output=tmp_path / "output" / "model", report=report
    )


# ---------------------------------------------------------------------------
# A successful run
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("target", ["classification", "regression"])
def test_run_writes_a_complete_forecaster_that_answers_like_autogluon(volumes, target):
    df = make_table(target=target)

    published = run_worker(volumes, df)

    assert published == volumes.output
    assert (published / "config.pbtxt").is_file()
    assert sorted(p.name for p in published.iterdir()) == ["1", "config.pbtxt"]

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
def test_exported_graph_matches_autogluon_including_nulls_and_unseen_categories(volumes, algorithm):
    from autogluon.features.generators import IdentityFeatureGenerator
    from autogluon.tabular import TabularPredictor

    from forecast.training.onnx_export import EXPORTABLE_HYPERPARAMETERS, export

    assert algorithm in EXPORTABLE_HYPERPARAMETERS
    try:  # LightGBM needs the OpenMP runtime (libgomp, or libomp on macOS), and AutoGluon skips it silently without.
        __import__({"GBM": "lightgbm", "CAT": "catboost"}[algorithm])
    except (ImportError, OSError) as exc:
        pytest.skip(f"{algorithm} cannot be imported here: {exc}")

    df = make_table(800)
    df.loc[::7, "REGION"] = None  # NULL categories in the training data
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

    # A NULL and a value that was not seen in training are both -1, which is what the model was trained on.
    unseen = pd.DataFrame({"REGION": [-1], "QUANTITY": [10], "PRICE": [50.5]}, dtype="float32")
    for text in ("MARS", "", "None", "NULL"):
        _, unseen_probabilities = run([text])
        np.testing.assert_allclose(unseen_probabilities, predictor.predict_proba(unseen).values, atol=1e-5)

    # The rows of the training data, NULLs included, are answered like AutoGluon answers them.
    texts = {
        name: df[name].map(lambda v: "None" if pd.isna(v) else str(v)).values.astype(object).reshape(-1, 1)
        for name in ["REGION", "QUANTITY", "PRICE"]
    }
    _, all_probabilities = session.run(None, texts)
    np.testing.assert_allclose(
        all_probabilities, predictor.predict_proba(features.drop(columns="LATE")).values, atol=1e-5
    )


@pytest.mark.parametrize(
    "text, expected",
    [("12", 12.0), ("19.99", 19.99), ("nan", np.nan), ("", np.nan), ("None", np.nan), ("NULL", np.nan),
     ("True", 1.0), ("false", 0.0), ("1", 1.0), ("0", 0.0)],
)
def test_a_numeric_input_reads_null_and_boolean_texts_instead_of_failing(text, expected):
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
        return helper.make_model(graph)

    predictor = SimpleNamespace(
        model_best="m",
        problem_type="regression",
        _trainer=SimpleNamespace(load_model=lambda name: SimpleNamespace(ag_key="X", model=None)),
    )
    onnx_export._CONVERTERS["X"] = converter
    try:
        model = onnx_export.export(predictor, Encoding(columns=["N"]))
    finally:
        del onnx_export._CONVERTERS["X"]

    (prediction,) = ort.InferenceSession(model.SerializeToString()).run(None, {"N": np.array([[text]], dtype=object)})
    np.testing.assert_allclose(prediction, [[expected]], equal_nan=True)


def test_encode_turns_a_null_category_into_minus_one():
    df = pd.DataFrame({"A": ["x", None, "y"], "B": [1.0, np.nan, 3.0]})

    features, encoding = encode(df, ["A", "B"])

    assert features["A"].tolist() == [0.0, -1.0, 1.0]
    assert features["B"].isna().tolist() == [False, True, False]
    assert encoding.categories == {"A": ["x", "y"]}


def test_run_writes_rows_read_first_and_then_the_full_result(volumes, monkeypatch):
    written = []
    original = job_files.write_result
    monkeypatch.setattr(worker, "write_result", lambda directory, result: (written.append(dict(result)), original(directory, result)))

    run_worker(volumes, make_table(300))

    assert written[0] == {"rows_read": 300}
    final = job_files.read_result(volumes.report)
    assert set(final) == {
        "rows_read", "algorithm", "validation_metric", "validation_score", "test_score", "training_seconds"
    }
    assert written[-1] == final
    assert final["rows_read"] == 300
    assert final["algorithm"] in {"LightGBM", "CatBoost"}
    assert final["validation_metric"] == "accuracy"
    assert 0 <= final["validation_score"] <= 1
    assert 0 <= final["test_score"] <= 1


def test_the_rows_read_are_reported_before_a_run_without_rows_fails(volumes):
    with pytest.raises(PermanentFailure, match="no rows"):
        run_worker(volumes, make_table(0))

    assert job_files.read_result(volumes.report) == {"rows_read": 0}


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
    run_worker(volumes, make_table(200))

    assert list(volumes.scratch.iterdir()) == []
    assert (volumes.output / "1" / "model.onnx").is_file()
    assert volumes.data.is_file()  # The input belongs to the API.


# ---------------------------------------------------------------------------
# The model directory
# ---------------------------------------------------------------------------


def identity_model():
    from onnx import TensorProto, helper

    graph = helper.make_graph(
        [helper.make_node("Identity", ["x"], ["y"])],
        "g",
        [helper.make_tensor_value_info("x", TensorProto.FLOAT, [None, 1])],
        [helper.make_tensor_value_info("y", TensorProto.FLOAT, [None, 1])],
    )
    return helper.make_model(graph, opset_imports=[helper.make_opsetid("", 13)])


def test_model_dir_has_directories_0755_and_files_0644(tmp_path):
    import os

    os.umask(0o022)

    written = write_model_dir(tmp_path / "out", "fc_x_5", identity_model())

    modes = {p.relative_to(written): stat.S_IMODE(p.stat().st_mode) for p in [written, *written.rglob("*")]}
    assert modes == {
        written.relative_to(written): 0o755,
        written.joinpath("1").relative_to(written): 0o755,
        written.joinpath("1", "model.onnx").relative_to(written): 0o644,
        written.joinpath("config.pbtxt").relative_to(written): 0o644,
    }
    assert 'name: "fc_x_5"' in (written / "config.pbtxt").read_text()


def test_model_dir_is_never_written_over_the_files_of_an_earlier_attempt(tmp_path):
    existing = tmp_path / "out"
    existing.mkdir()
    (existing / "keep").write_text("kept")

    with pytest.raises(FileExistsError):
        write_model_dir(existing, "fc_x_5", identity_model())

    assert (existing / "keep").read_text() == "kept"


def test_forecaster_name_is_connector_and_version():
    assert forecaster_name(CONNECTOR_ID, 12) == f"fc_{CONNECTOR_ID}_12"


# ---------------------------------------------------------------------------
# Failures and exit codes
# ---------------------------------------------------------------------------


def test_no_rows_is_a_permanent_failure_and_nothing_is_published(volumes):
    with pytest.raises(PermanentFailure, match="no rows"):
        run_worker(volumes, make_table(0))

    assert not volumes.output.exists()


def test_columns_are_trained_under_the_spelling_of_the_configuration(volumes):
    # The loader writes the table's spelling (IBM i: upper case); the user may have typed another.
    job = TrainingJob(
        job_id=7,
        connector_id=CONNECTOR_ID,
        source_table="SALES.ORDERS",
        target_column="late",
        input_names=["Region", "QUANTITY", "price"],
    )

    published = run_worker(volumes, make_table(200), job)

    session = ort.InferenceSession(str(published / "1" / "model.onnx"))
    assert [i.name for i in session.get_inputs()] == ["Region", "QUANTITY", "price"]


def test_a_column_that_the_file_lacks_is_a_permanent_failure(volumes):
    with pytest.raises(PermanentFailure, match="Column PRICE is missing"):
        run_worker(volumes, make_table(50).drop(columns="PRICE"))


def test_a_column_that_the_file_spells_two_ways_is_a_permanent_failure(volumes):
    df = make_table(50).rename(columns={"PRICE": "Price"})
    df["price"] = 1.0

    with pytest.raises(PermanentFailure, match="Column PRICE is ambiguous"):
        run_worker(volumes, df)


def test_a_target_with_one_value_is_a_permanent_failure(volumes):
    df = make_table(100)
    df["LATE"] = "N"

    with pytest.raises(PermanentFailure, match="at least two different values"):
        run_worker(volumes, df)


@pytest.fixture
def report(tmp_path):
    """The directory of the run: the job file is in it, and the worker reports next to it."""
    directory = tmp_path / "run-7"
    directory.mkdir()
    return directory


def argv(report):
    return [str(report / "job.json"), "data.parquet", "output"]


def failing_run(monkeypatch, error):
    def run(*args):
        raise error

    monkeypatch.setattr(worker, "run", run)


@pytest.mark.parametrize(
    "error, code",
    [
        (PermanentFailure("Connector gone"), EXIT_PERMANENT),
        (TransientFailure("Connection to the source database was lost"), EXIT_FAILURE),
    ],
)
def test_main_maps_failures_to_exit_codes_and_reports_their_message(monkeypatch, report, error, code):
    failing_run(monkeypatch, error)

    assert worker.main(argv(report)) == code

    assert job_files.read_failure(report) == str(error)


def test_main_reports_a_fixed_text_for_an_unexpected_error(monkeypatch, report):
    secret = "host ibmi.internal.example user forecast_ro password hunter2"
    failing_run(monkeypatch, RuntimeError(secret))

    assert worker.main(argv(report)) == EXIT_FAILURE

    message = job_files.read_failure(report)
    assert "RuntimeError" in message and "worker log" in message
    assert "hunter2" not in message


def test_main_logs_the_cause_of_a_permanent_failure_but_does_not_report_it(monkeypatch, report, caplog):
    cause = ValueError("row with customer 4711")
    error = PermanentFailure("AutoGluon could not train on this data (ValueError).")
    error.__cause__ = cause
    failing_run(monkeypatch, error)

    with caplog.at_level("ERROR"):
        assert worker.main(argv(report)) == EXIT_PERMANENT

    assert job_files.read_failure(report) == str(error)
    assert "customer 4711" in caplog.text


def test_main_exits_zero_on_success_and_reports_no_failure(monkeypatch, tmp_path, report):
    published = tmp_path / "fc_x_7"
    published.mkdir()
    monkeypatch.setattr(worker, "run", lambda *args: published)

    assert worker.main(argv(report)) == EXIT_OK

    assert not (report / "failure.json").exists()


def test_main_still_exits_when_the_failure_cannot_be_written(monkeypatch, report):
    failing_run(monkeypatch, RuntimeError("boom"))

    def cannot_write(*args):
        raise OSError("read-only file system")

    monkeypatch.setattr(worker, "write_failure", cannot_write)

    assert worker.main(argv(report)) == EXIT_FAILURE


def test_main_reports_a_bounded_message(monkeypatch, report):
    failing_run(monkeypatch, TransientFailure("x" * 5000))

    worker.main(argv(report))

    assert len(job_files.read_failure(report)) == 1000


def test_a_missing_job_file_is_a_permanent_failure_that_is_reported(report):
    assert worker.main(argv(report)) == EXIT_PERMANENT

    assert "job file" in job_files.read_failure(report)


def test_an_invalid_job_file_is_a_permanent_failure(volumes):
    (volumes.report / "job.json").write_text('{"job_id": "7"}')

    with pytest.raises(PermanentFailure, match="job file"):
        worker.run(volumes.report / "job.json", volumes.data, volumes.output)


def test_the_worker_needs_no_database(monkeypatch, volumes):
    """It imports and runs with no database setting, and has no code that would use one."""
    for name in ("POSTGRES_HOST", "POSTGRES_USER", "POSTGRES_PASSWORD", "DB_ENCRYPTION_KEY"):
        monkeypatch.delenv(name, raising=False)

    run_worker(volumes, make_table(200))

    assert not hasattr(worker, "JobStore") and not hasattr(worker, "worker_engine")
    assert "sqlalchemy" not in open(worker.__file__).read()


def test_a_table_with_too_few_labelled_rows_is_a_permanent_failure(volumes):
    df = make_table(100)
    df["LATE"] = ["Y", "N"] * 50
    df.loc[5:, "LATE"] = None  # Five rows keep a target value.

    with pytest.raises(PermanentFailure, match="Only 5 rows have a value in target column LATE"):
        run_worker(volumes, df)

    assert not volumes.output.exists()


def test_the_split_is_random_when_there_are_more_classes_than_test_rows():
    df = make_table(30)
    df["LATE"] = [f"C{i % 10}" for i in range(30)]  # Ten classes, a test split of six rows.

    train_rows, test_rows = _split(df, "LATE")

    assert len(test_rows) == 6 and len(train_rows) == 24


def test_autogluon_failing_on_the_data_is_a_retryable_failure_without_its_message(volumes, monkeypatch):
    from autogluon.tabular import TabularPredictor

    def fail(*args, **kwargs):
        raise ValueError("class 'customer 4711' has too few rows")

    monkeypatch.setattr(TabularPredictor, "fit", fail)

    with pytest.raises(TransientFailure, match=r"could not train on this data \(ValueError\)") as raised:
        run_worker(volumes, make_table(100))

    assert not isinstance(raised.value, PermanentFailure)
    assert "4711" not in str(raised.value)
    assert isinstance(raised.value.__cause__, ValueError)


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
    monkeypatch.setattr(sys, "argv", ["forecast.training", "job.json"])

    with pytest.raises(SystemExit) as exited:
        runpy.run_module("forecast.training", run_name="__main__")

    assert exited.value.code == EXIT_PERMANENT
