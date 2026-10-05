"""
Unit tests for db_load stubs.
"""

from unittest.mock import patch
from db_load.factory import get_loader
from db_load.noop import NoOpLoader
from settings import settings


def test_noop_loader():
    loader = NoOpLoader()
    assert loader.is_configured() is True
    ref = loader.load("job-123", {"INVOICE_NUM": "INV-1"}, [])
    assert ref == "NOOP"


def test_factory_returns_noop_when_disabled():
    with patch.object(settings.invoice, "db_load_enabled", False):
        loader = get_loader()
        assert isinstance(loader, NoOpLoader)


def test_factory_returns_noop_when_no_backend_configured():
    with patch.object(settings.invoice, "db_load_enabled", True):
        loader = get_loader()
        assert isinstance(loader, NoOpLoader)
