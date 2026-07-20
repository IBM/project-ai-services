"""
Shared pytest fixtures for extract service tests.
"""

import os
import pytest
from unittest.mock import Mock, patch
from fastapi.testclient import TestClient

# CRITICAL: Patch the crash handler BEFORE importing any application modules.
os.environ['DISABLE_CRASH_HANDLER'] = '1'

import common.diagnostic_logger

def _mock_crash_handler(logger):
    """Mock crash handler that doesn't manipulate file descriptors."""
    mock_stderr_monitor = Mock(name="stderr_monitor")
    mock_stderr_monitor.start = Mock()
    mock_stderr_monitor.stop = Mock()
    return (
        Mock(name="diagnostic_logger"),
        mock_stderr_monitor,
        Mock(name="signal_handler"),
    )

common.diagnostic_logger.setup_comprehensive_crash_handler = _mock_crash_handler


VALID_SCHEMA = {
    "type": "object",
    "properties": {
        "invoice_number": {"type": "string"},
        "vendor_name": {"type": "string"},
        "total_amount": {"type": "number"},
        "currency": {"type": "string"},
    },
    "required": ["invoice_number", "vendor_name", "total_amount"],
}

VALID_SCHEMA_WITH_BOOL_REQUIRED = {
    "type": "object",
    "properties": {
        "invoice_number": {"type": "string", "required": True},
        "vendor_name": {"type": "string", "required": True},
        "total_amount": {"type": "number", "required": True},
        "currency": {"type": "string"},
    },
}

VALID_EXAMPLE = {
    "text": "INVOICE #INV-001\nVendor: Acme\nTOTAL: EUR 100.00",
    "output": {
        "invoice_number": "INV-001",
        "vendor_name": "Acme",
        "total_amount": 100.0,
        "currency": "EUR",
    },
}


@pytest.fixture
def mock_model_dict():
    return {"llm_endpoint": "http://localhost:8002", "llm_model": "test-model"}


@pytest.fixture
def extract_test_client(monkeypatch, mock_model_dict):
    """
    FastAPI test client for the extract app with all external boundaries mocked.

    Mocked boundaries:
      - llm_model_dict  (module-level)
      - initialize_models
      - create_llm_session
      - configure_uvicorn_logging
      - check_db_connection  (returns True)
      - Base.metadata.create_all  (no-op)
      - recover_zombie_jobs  (no-op)
    """
    import extract.app as extract_app

    monkeypatch.setattr(extract_app, "llm_model_dict", mock_model_dict, raising=False)
    monkeypatch.setattr(extract_app, "initialize_models", Mock())
    monkeypatch.setattr(extract_app, "create_llm_session", Mock())
    monkeypatch.setattr(extract_app, "configure_uvicorn_logging", Mock())

    with patch("extract.app.check_db_connection", return_value=True), \
         patch("extract.db.models.Base.metadata.create_all"), \
         patch("extract.job_utils.recover_zombie_jobs", return_value=0):
        client = TestClient(extract_app.app)
        yield client
        client.close()

# Made with Bob
