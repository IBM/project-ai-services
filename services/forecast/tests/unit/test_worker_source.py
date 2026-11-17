"""
Unit tests of the worker's contact with the data loader (worker/source.py).

The real loader's exceptions are used, so the permanent/retry split follows the loader's
own ``is_permanent_failure``. Only Db2DataLoader is replaced. The tests are skipped where
the loader is not part of the branch.
"""
from types import SimpleNamespace

import pandas as pd
import pytest

pytest.importorskip("forecast.data_loader")

from forecast.data_loader import (  # noqa: E402
    ConnectorDecryptionError,
    ConnectorNotFoundError,
    DBAuthError,
    DBColumnNotFoundError,
    DBConnectionError,
    DBTableNotFoundError,
    InvalidTableReferenceError,
    ParquetWriteError,
)
from common.forecast_errors import PermanentFailure  # noqa: E402
from forecast.tests.unit.test_worker import CONNECTOR_ID, JOB, make_table  # noqa: E402

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


@pytest.fixture
def loader(monkeypatch):
    """Replace the real loader's Db2DataLoader. Its exceptions stay the real ones."""
    from forecast import data_loader

    FakeLoader.calls, FakeLoader.error = [], None
    FakeLoader.table = make_table(50)
    monkeypatch.setattr(data_loader, "Db2DataLoader", FakeLoader)
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
    # No row ceiling and no batch size: the loader reads everything with its own settings.
    assert not {"max_training_rows", "batch_size"} & set(call)


def test_columns_come_back_as_named_in_the_training_configuration(loader, tmp_path):
    from forecast.training.source import load_training_data

    # The loader matches columns case-insensitively and writes the catalogue's spelling.
    loader.table = make_table(20).rename(columns=str.lower)

    data = load_training_data(JOB, tmp_path / "data.parquet")

    assert list(data.columns) == ["REGION", "QUANTITY", "PRICE", "LATE"]


@pytest.mark.parametrize(
    "error",
    [
        ConnectorNotFoundError("c"),
        ConnectorDecryptionError("c"),
        DBTableNotFoundError("failure text"),
        DBColumnNotFoundError("failure text"),
        InvalidTableReferenceError("failure text"),
        DBAuthError("failure text"),
        ParquetWriteError("failure text"),
    ],
    ids=lambda error: type(error).__name__,
)
def test_loader_errors_that_would_only_repeat_are_permanent(loader, tmp_path, error):
    from forecast.training.source import load_training_data

    loader.error = error

    with pytest.raises(PermanentFailure, match=str(error)):
        load_training_data(JOB, tmp_path / "data.parquet")


def test_a_dropped_connection_is_retried(loader, tmp_path):
    from forecast.training.source import load_training_data

    loader.error = DBConnectionError("connection lost")

    with pytest.raises(DBConnectionError):
        load_training_data(JOB, tmp_path / "data.parquet")
