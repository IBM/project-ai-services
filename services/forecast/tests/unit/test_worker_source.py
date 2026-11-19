"""
Unit tests of the worker's contact with the data loader (training/source.py).

The data loader is a separate branch, so these tests install a stand-in ``forecast.data_loader``
with the same contract: ``DataLoaderError`` with an ``is_permanent_failure`` flag, and
``Db2DataLoader.extract_to_parquet``. The last test checks the real loader's exceptions
against the same split where the loader is part of the branch.
"""
import sys
import types
from types import SimpleNamespace

import pandas as pd
import pytest

from forecast.errors import PermanentFailure, TransientFailure
from forecast.tests.unit.test_worker import CONNECTOR_ID, JOB, make_table

pytestmark = pytest.mark.unit


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
    from forecast.training.source import load_training_data

    data = load_training_data(JOB, tmp_path / "data.parquet")

    assert len(data) == 50
    (call,) = loader.calls
    assert call["table"] == "SALES.ORDERS"
    assert call["columns"] == ["REGION", "QUANTITY", "PRICE", "LATE"]
    assert call["output_path"] == tmp_path / "data.parquet"
    assert call["connector_id"] == str(CONNECTOR_ID)
    assert call["connector_type"] == "db2_for_i"
    assert call["encrypted_connection_details"] == JOB.connection_details
    # No row limit and no batch size: the loader reads everything with its own settings.
    assert not {"max_rows", "batch_size"} & set(call)


def test_columns_come_back_as_named_in_the_training_configuration(loader, tmp_path):
    from forecast.training.source import load_training_data

    # The loader matches columns case-insensitively and writes the catalogue's spelling.
    loader.table = make_table(20).rename(columns=str.lower)

    data = load_training_data(JOB, tmp_path / "data.parquet")

    assert list(data.columns) == ["REGION", "QUANTITY", "PRICE", "LATE"]


@pytest.mark.parametrize(
    "error",
    [ConnectorNotFoundError("c"), DBAuthError("failure text")],
    ids=lambda error: type(error).__name__,
)
def test_loader_errors_that_would_only_repeat_are_permanent(loader, tmp_path, error):
    from forecast.training.source import load_training_data

    loader.error = error

    with pytest.raises(PermanentFailure, match=str(error)) as raised:
        load_training_data(JOB, tmp_path / "data.parquet")

    assert raised.value.__cause__ is error


def test_a_dropped_connection_is_retried(loader, tmp_path):
    from forecast.training.source import load_training_data

    loader.error = DBConnectionError("connection lost")

    with pytest.raises(TransientFailure, match="connection lost") as raised:
        load_training_data(JOB, tmp_path / "data.parquet")

    assert isinstance(raised.value.__cause__, DBConnectionError)


def test_the_real_loaders_exceptions_follow_the_same_split():
    exceptions = pytest.importorskip("forecast.data_loader.exceptions")

    assert exceptions.DataLoaderError.is_permanent_failure is False
    for name in ("ConnectorNotFoundError", "ConnectorDecryptionError", "DBAuthError", "DBTableNotFoundError",
                 "DBColumnNotFoundError", "InvalidTableReferenceError", "ParquetWriteError"):
        assert getattr(exceptions, name).is_permanent_failure is True, name
    assert exceptions.DBConnectionError.is_permanent_failure is False
