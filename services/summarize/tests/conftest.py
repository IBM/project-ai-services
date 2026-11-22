"""
Shared pytest fixtures and configuration for summarize tests.
"""

import os
import pytest
from unittest.mock import Mock, patch
from fastapi.testclient import TestClient

# CRITICAL: Set environment variable BEFORE any imports that might use diagnostic_logger
# This prevents stderr monitoring from starting during test collection
os.environ['DISABLE_CRASH_HANDLER'] = '1'

# DISABLE_CRASH_HANDLER is already set above; importing diagnostic_logger here
# ensures the module is loaded before any app code so the env-var guard inside
# setup_comprehensive_crash_handler fires on the first real call.
import common.diagnostic_logger  # noqa: F401


@pytest.fixture(autouse=True)
def authenticated_admin(request, monkeypatch):
    """Give every TestClient an admin identity so endpoint tests exercise the handlers.

    Authentication itself is covered by common/tests/auth and test_auth_enforcement.py.
    Tests marked ``real_auth`` get the real middleware and no injected token.
    """
    from common.auth import Principal, TokenVerifier, set_verifier
    from common.auth.config import AuthConfig

    if request.node.get_closest_marker("real_auth"):
        yield
        set_verifier(None)
        return

    class _AdminVerifier(TokenVerifier):
        async def verify(self, token):
            return Principal(
                subject="test-admin", idp="local", kind="user", audience="summarize",
                roles=frozenset({"admin"}), token_id="test",
            )

    set_verifier(_AdminVerifier(AuthConfig(jwks_url="https://catalog.invalid/jwks", audience="summarize")))
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
    with patch.dict(os.environ, {"DISABLE_CRASH_HANDLER": "1"}):
        yield


@pytest.fixture
def summarize_sample_text():
    """Sample source text for summarize tests."""
    return (
        "Artificial intelligence systems are increasingly used in healthcare, "
        "finance, and transportation. They improve automation, accelerate "
        "analysis, and support decision making across large datasets."
    )


@pytest.fixture
def summarize_sample_summary():
    """Sample generated summary text."""
    return "Artificial intelligence improves automation and decision making across industries."


@pytest.fixture
def summarize_mock_model_dict():
    """Mock model dictionary for summarize tests."""
    return {
        "llm_endpoint": "http://localhost:8002",
    }


@pytest.fixture
def summarize_test_client(monkeypatch, summarize_mock_model_dict):
    """
    FastAPI test client for summarize app with external boundaries mocked.

    This keeps app imports deterministic and avoids startup calls to real services.
    """
    import summarize.app as summarize_app

    monkeypatch.setattr(summarize_app, "llm_model_dict", summarize_mock_model_dict, raising=False)
    monkeypatch.setattr(summarize_app, "initialize_models", Mock())
    monkeypatch.setattr(summarize_app, "create_llm_session", Mock())
    monkeypatch.setattr(summarize_app, "configure_uvicorn_logging", Mock())

    # Use context manager to ensure proper cleanup of file handles
    client = TestClient(summarize_app.app)
    yield client
    client.close()

# Made with Bob
