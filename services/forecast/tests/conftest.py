"""
Pytest configuration and fixtures for forecast service tests.

The tests run without a real database or Triton.
"""

import os
from unittest.mock import patch

import pytest

# CRITICAL: Set environment variable BEFORE any imports that might use diagnostic_logger
# This prevents stderr monitoring from starting during test collection
os.environ["DISABLE_CRASH_HANDLER"] = "1"

# DISABLE_CRASH_HANDLER is already set above; importing diagnostic_logger here
# ensures the module is loaded before any app code so the env-var guard inside
# setup_comprehensive_crash_handler fires on the first real call.
import common.diagnostic_logger  # noqa: F401,E402


@pytest.fixture(scope="session", autouse=True)
def mock_diagnostic_crash_handler():
    """Keep DISABLE_CRASH_HANDLER set for any late-bound code paths."""
    with patch.dict(os.environ, {"DISABLE_CRASH_HANDLER": "1"}), patch(
        "forecast.app.check_db_configured", return_value=None
    ):
        yield
