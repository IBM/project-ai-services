"""
Pytest configuration and test fixtures for invoice processing.
"""

import os

# Disable stderr redirect in tests
os.environ["DISABLE_CRASH_HANDLER"] = "1"
os.environ["POSTGRES_HOST"] = "localhost"
os.environ["POSTGRES_PORT"] = "5432"
os.environ["POSTGRES_DB"] = "test_db"
os.environ["POSTGRES_USER"] = "test_user"
os.environ["POSTGRES_PASSWORD"] = "test_pass"

import common.diagnostic_logger  # noqa: F401
from unittest.mock import MagicMock, patch
import pytest
from fastapi.testclient import TestClient


@pytest.fixture
def mock_db_connection():
    with patch("app.check_db_connection", return_value=True), \
         patch("app._initialize_database"):
        yield


@pytest.fixture
def client(mock_db_connection):
    with patch("app._register_invoice_schema"):
        from app import app
        with TestClient(app) as test_client:
            yield test_client
