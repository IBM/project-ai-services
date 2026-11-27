"""
Unit tests of the files that the API and the training worker exchange (utils/job_files.py).

The worker is the less trusted side, so most tests are about what the API does with a file that is
missing, corrupt, oversized, of the wrong type or not a regular file.
"""
import json
import os
from uuid import UUID

import pytest

from forecast.errors import PermanentFailure
from forecast.utils import job_files
from forecast.utils.job_files import TrainingJob

pytestmark = pytest.mark.unit

JOB = TrainingJob(
    job_id=7,
    connector_id=UUID("c9e66790-7425-40de-944b-e07fc1f90ae7"),
    source_table="SALES.ORDERS",
    target_column="LATE",
    input_names=["REGION", "QUANTITY"],
)

FULL_RESULT = {
    "rows_read": 400,
    "algorithm": "CatBoost",
    "validation_metric": "accuracy",
    "validation_score": 0.9,
    "test_score": 0.88,
    "training_seconds": 12.5,
}


# ---------------------------------------------------------------------------
# job.json
# ---------------------------------------------------------------------------


def test_a_job_survives_the_trip_through_its_file(tmp_path):
    path = job_files.write_job(tmp_path, JOB)

    assert path == tmp_path / "job.json"
    assert job_files.read_job(path) == JOB
    assert JOB.columns == ["REGION", "QUANTITY", "LATE"]


@pytest.mark.parametrize(
    "content",
    [
        None,  # No file.
        "not json",
        "[]",
        "{}",
        json.dumps({"job_id": 7, "connector_id": "not-a-uuid", "source_table": "T", "target_column": "C", "input_names": []}),
        json.dumps({"job_id": "7", "connector_id": str(JOB.connector_id), "source_table": "T", "target_column": "C", "input_names": []}),
        json.dumps({"job_id": True, "connector_id": str(JOB.connector_id), "source_table": "T", "target_column": "C", "input_names": []}),
        json.dumps({"job_id": 7, "connector_id": str(JOB.connector_id), "source_table": 5, "target_column": "C", "input_names": []}),
        json.dumps({"job_id": 7, "connector_id": str(JOB.connector_id), "source_table": "T", "target_column": "C", "input_names": "A"}),
        json.dumps({"job_id": 7, "connector_id": str(JOB.connector_id), "source_table": "T", "target_column": "C", "input_names": [1]}),
    ],
)
def test_a_missing_or_invalid_job_file_is_a_permanent_failure(tmp_path, content):
    path = tmp_path / "job.json"
    if content is not None:
        path.write_text(content)

    with pytest.raises(PermanentFailure, match="job file of the training worker is missing or invalid"):
        job_files.read_job(path)


# ---------------------------------------------------------------------------
# Writing: whole or not at all
# ---------------------------------------------------------------------------


def test_a_write_that_fails_keeps_the_old_content_and_leaves_no_temporary_file(tmp_path):
    job_files.write_result(tmp_path, {"rows_read": 1})

    with pytest.raises(TypeError):
        job_files.write_result(tmp_path, {"rows_read": object()})

    assert json.loads((tmp_path / "result.json").read_text()) == {"rows_read": 1}
    assert [p.name for p in tmp_path.iterdir()] == ["result.json"]


def test_a_second_write_replaces_the_first(tmp_path):
    job_files.write_result(tmp_path, {"rows_read": 1})
    job_files.write_result(tmp_path, FULL_RESULT)

    assert job_files.read_result(tmp_path) == FULL_RESULT


# ---------------------------------------------------------------------------
# result.json
# ---------------------------------------------------------------------------


def test_the_known_keys_of_a_result_are_read(tmp_path):
    job_files.write_result(tmp_path, FULL_RESULT)

    assert job_files.read_result(tmp_path) == FULL_RESULT


def test_a_result_with_only_rows_read_is_read(tmp_path):
    job_files.write_result(tmp_path, {"rows_read": 0})

    assert job_files.read_result(tmp_path) == {"rows_read": 0}


def test_keys_that_the_api_does_not_know_are_ignored(tmp_path):
    job_files.write_result(tmp_path, {**FULL_RESULT, "attempts": 99, "status": "COMPLETED", "extra": {"a": 1}})

    assert job_files.read_result(tmp_path) == FULL_RESULT


@pytest.mark.parametrize(
    "key, value",
    [
        ("rows_read", "400"),
        ("rows_read", 1.5),
        ("rows_read", True),
        ("algorithm", 7),
        ("validation_metric", ["accuracy"]),
        ("validation_score", "0.9"),
        ("validation_score", True),
        ("test_score", None),
        ("training_seconds", {"s": 1}),
        ("validation_score", float("nan")),
        ("test_score", float("inf")),
    ],
)
def test_a_value_of_the_wrong_type_is_left_out(tmp_path, key, value):
    job_files.write_result(tmp_path, {**FULL_RESULT, key: value})

    assert job_files.read_result(tmp_path) == {k: v for k, v in FULL_RESULT.items() if k != key}


def test_a_long_text_in_a_result_is_cut(tmp_path):
    job_files.write_result(tmp_path, {"algorithm": "x" * 5000})

    assert job_files.read_result(tmp_path) == {"algorithm": "x" * 100}


@pytest.mark.parametrize("content", ["not json", "[1, 2]", '"text"', "", '{"rows_read": '])
def test_a_corrupt_result_file_gives_an_empty_result(tmp_path, content):
    (tmp_path / "result.json").write_text(content)

    assert job_files.read_result(tmp_path) == {}


def test_a_missing_result_file_gives_an_empty_result(tmp_path):
    assert job_files.read_result(tmp_path) == {}


def test_an_oversized_result_file_is_not_read(tmp_path):
    padding = " " * job_files.MAX_REPORT_FILE_BYTES
    (tmp_path / "result.json").write_text('{"rows_read": 5}' + padding)

    assert job_files.read_result(tmp_path) == {}


def test_a_result_file_at_the_size_limit_is_read(tmp_path):
    body = '{"rows_read": 5}'
    (tmp_path / "result.json").write_text(body + " " * (job_files.MAX_REPORT_FILE_BYTES - len(body)))

    assert job_files.read_result(tmp_path) == {"rows_read": 5}


def test_a_symbolic_link_is_not_followed(tmp_path):
    secret = tmp_path / "elsewhere.json"
    secret.write_text(json.dumps(FULL_RESULT))
    (tmp_path / "run").mkdir()
    os.symlink(secret, tmp_path / "run" / "result.json")
    os.symlink(secret, tmp_path / "run" / "failure.json")

    assert job_files.read_result(tmp_path / "run") == {}
    assert job_files.read_failure(tmp_path / "run") is None


def test_a_named_pipe_is_not_waited_for(tmp_path):
    os.mkfifo(tmp_path / "result.json")
    os.mkfifo(tmp_path / "failure.json")

    assert job_files.read_result(tmp_path) == {}  # Returns instead of blocking on a writer that never comes.
    assert job_files.read_failure(tmp_path) is None


def test_a_directory_in_place_of_a_file_gives_nothing(tmp_path):
    (tmp_path / "result.json").mkdir()

    assert job_files.read_result(tmp_path) == {}


# ---------------------------------------------------------------------------
# failure.json
# ---------------------------------------------------------------------------


def test_a_failure_message_is_read(tmp_path):
    job_files.write_failure(tmp_path, "Table SALES.ORDERS has no rows")

    assert job_files.read_failure(tmp_path) == "Table SALES.ORDERS has no rows"


def test_a_long_failure_message_is_cut(tmp_path):
    job_files.write_failure(tmp_path, "x" * 5000)

    assert job_files.read_failure(tmp_path) == "x" * 1000


@pytest.mark.parametrize("content", ["not json", "[]", "{}", '{"message": 7}', '{"message": ["a"]}', '{"message": ""}', '{"message": "   "}', '{"msg": "x"}'])
def test_a_failure_file_without_a_usable_message_gives_none(tmp_path, content):
    (tmp_path / "failure.json").write_text(content)

    assert job_files.read_failure(tmp_path) is None


def test_a_missing_failure_file_gives_none(tmp_path):
    assert job_files.read_failure(tmp_path) is None


def test_an_oversized_failure_file_is_not_read(tmp_path):
    (tmp_path / "failure.json").write_text(json.dumps({"message": "x" * job_files.MAX_REPORT_FILE_BYTES}))

    assert job_files.read_failure(tmp_path) is None
