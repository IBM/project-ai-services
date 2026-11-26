"""
Unit tests of the API's contact with the data loader (orchestrator/source.py).

The data loader is a separate branch, so these tests install a stand-in ``forecast.data_loader``
with the same contract: ``DataLoaderError`` with an ``is_permanent_failure`` flag, and
``Db2DataLoader.extract_to_parquet``. The last two tests use the real loader: one checks that the
call fits the real signature, which a stand-in that accepts any arguments cannot, the other its
exceptions against the same split.
"""
import sys
import types
from types import SimpleNamespace

import pandas as pd
import pytest

from forecast.errors import PermanentFailure, TransientFailure
from forecast.tests.unit.test_worker import CONNECTOR_ID, JOB, make_table

pytestmark = pytest.mark.unit

CONNECTION = {"host": "h", "password": "encrypted"}


def load(path, **overrides):
    from forecast.orchestrator.source import load_training_data

    return load_training_data(
        **{
            "table": JOB.source_table,
            "columns": JOB.columns,
            "output_path": path,
            "connector_id": str(CONNECTOR_ID),
            "connection_details": CONNECTION,
            **overrides,
        }
    )


class FakeLoader:
    """Stands in for Db2DataLoader: writes ``table`` to the output path, or raises ``error``."""

    calls: list[dict] = []
    table: pd.DataFrame
    error: Exception | None = None

    def extract_to_parquet(self, **kwargs):
        FakeLoader.calls.append(kwargs)
        if FakeLoader.error:
            raise FakeLoader.error
        FakeLoader.table.to_parquet(kwargs["output_path"])
        return SimpleNamespace(columns=list(FakeLoader.table.columns), rows_written=len(FakeLoader.table))


class DataLoaderError(Exception):
    is_permanent_failure = False


class ConnectorNotFoundError(DataLoaderError):
    is_permanent_failure = True


class DBAuthError(DataLoaderError):
    is_permanent_failure = True


class DBConnectionError(DataLoaderError):
    is_permanent_failure = False


@pytest.fixture
def loader(monkeypatch):
    """Install a stand-in ``forecast.data_loader`` whose Db2DataLoader is FakeLoader."""
    module = types.ModuleType("forecast.data_loader")
    module.DataLoaderError = DataLoaderError
    module.Db2DataLoader = FakeLoader
    monkeypatch.setitem(sys.modules, "forecast.data_loader", module)

    FakeLoader.calls, FakeLoader.error = [], None
    FakeLoader.table = make_table(50)
    return FakeLoader


def test_loader_reads_every_row_with_the_encrypted_connector(loader, tmp_path):
    result = load(tmp_path / "data.parquet")

    assert result.rows_written == 50
    (call,) = loader.calls
    assert call["table"] == "SALES.ORDERS"
    assert call["columns"] == ["REGION", "QUANTITY", "PRICE", "LATE"]
    assert call["output_path"] == tmp_path / "data.parquet"
    assert call["connector_id"] == str(CONNECTOR_ID)
    assert call["encrypted_connection_details"] == CONNECTION
    # No row limit and no batch size: the loader reads everything with its own settings.
    assert not {"max_rows", "batch_size"} & set(call)


def test_the_rows_are_not_read_into_the_api_process(loader, tmp_path, monkeypatch):
    monkeypatch.setattr(pd, "read_parquet", lambda *a, **k: pytest.fail("the API must not read the table"))

    load(tmp_path / "data.parquet")


def test_the_call_fits_the_real_loaders_signature(tmp_path):
    from unittest.mock import patch

    from forecast.data_loader import Db2DataLoader, ExtractResult

    summary = ExtractResult(str(tmp_path / "data.parquet"), 50, JOB.columns, 1, 0.1, "SALES.ORDERS")
    # autospec rejects arguments that the real method does not take.
    with patch.object(Db2DataLoader, "extract_to_parquet", autospec=True, return_value=summary) as extract:
        assert load(tmp_path / "data.parquet") is summary

    assert extract.call_args.kwargs["encrypted_connection_details"] == CONNECTION


@pytest.mark.parametrize(
    "error",
    [ConnectorNotFoundError("c"), DBAuthError("failure text")],
    ids=lambda error: type(error).__name__,
)
def test_loader_errors_that_would_only_repeat_are_permanent(loader, tmp_path, error):
    loader.error = error

    with pytest.raises(PermanentFailure, match=str(error)) as raised:
        load(tmp_path / "data.parquet")

    assert raised.value.__cause__ is error


def test_a_dropped_connection_is_retried(loader, tmp_path):
    loader.error = DBConnectionError("connection lost")

    with pytest.raises(TransientFailure, match="connection lost") as raised:
        load(tmp_path / "data.parquet")

    assert isinstance(raised.value.__cause__, DBConnectionError)


def test_the_real_loaders_exceptions_follow_the_same_split():
    exceptions = pytest.importorskip("forecast.data_loader.exceptions")

    assert exceptions.DataLoaderError.is_permanent_failure is False
    for name in ("ConnectorNotFoundError", "ConnectorDecryptionError", "DBAuthError", "DBTableNotFoundError",
                 "DBColumnNotFoundError", "InvalidTableReferenceError", "ParquetWriteError"):
        assert getattr(exceptions, name).is_permanent_failure is True, name
    assert exceptions.DBConnectionError.is_permanent_failure is False
