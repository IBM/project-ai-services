"""
Shared pytest fixtures and configuration for summarize tests.
"""

import pytest
from unittest.mock import Mock
from fastapi.testclient import TestClient


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

    return TestClient(summarize_app.app)

# Made with Bob
