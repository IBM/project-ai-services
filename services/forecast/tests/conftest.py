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


@pytest.fixture(autouse=True)
def authenticated_admin(request, monkeypatch):
    """Give every TestClient an admin identity so endpoint tests exercise the handlers.

    Authentication itself is covered by common/tests/auth and test_auth_enforcement.py.
    Tests marked ``real_auth`` get the real middleware and no injected token.
    """
    from common.auth import Principal, TokenVerifier, set_verifier
    from common.auth.config import AuthConfig
    from fastapi.testclient import TestClient

    if request.node.get_closest_marker("real_auth"):
        yield
        set_verifier(None)
        return

    class _AdminVerifier(TokenVerifier):
        async def verify(self, token):
            return Principal(
                subject="test-admin", idp="local", kind="user", audience="forecast",
                roles=frozenset({"admin"}), token_id="test",
            )

    set_verifier(_AdminVerifier(AuthConfig(jwks_url="https://catalog.invalid/jwks", audience="forecast")))
    original_init = TestClient.__init__

    def init(self, *args, **kwargs):
        original_init(self, *args, **kwargs)
        self.headers.setdefault("Authorization", "Bearer test-token")

    monkeypatch.setattr(TestClient, "__init__", init)
    yield
    set_verifier(None)


@pytest.fixture(scope="session", autouse=True)
def mock_diagnostic_crash_handler():
    """Keep DISABLE_CRASH_HANDLER set for any late-bound code paths."""
    with patch.dict(os.environ, {"DISABLE_CRASH_HANDLER": "1"}), patch(
        "forecast.app.check_db_configured", return_value=None
    ):
        yield


@pytest.fixture(autouse=True)
def enable_logger_propagation():
    """Ensure named loggers propagate to root so pytest's caplog captures records."""
    import logging

    loggers = [logging.getLogger(name) for name in ("forecast_server", "forecast_db", "forecast_triton")]
    original_flags = [logger.propagate for logger in loggers]
    for logger in loggers:
        logger.propagate = True
    try:
        yield
    finally:
        for logger, original in zip(loggers, original_flags):
            logger.propagate = original
