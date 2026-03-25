"""
Shared pytest fixtures and configuration for chatbot tests.
"""

import pytest
from unittest.mock import Mock, MagicMock, AsyncMock
from fastapi.testclient import TestClient
from typing import Dict, List
import sys
from pathlib import Path

# Add src directory to path for imports
src_path = Path(__file__).parent.parent / "src"
sys.path.insert(0, str(src_path))


@pytest.fixture
def mock_settings():
    """Mock settings object with default configuration."""
    settings = Mock()
    settings.max_concurrent_requests = 32
    settings.num_chunks_post_search = 10
    settings.num_chunks_post_reranker = 3
    settings.llm_max_tokens = 512
    settings.score_threshold = 0.4
    settings.max_query_token_length = 512
    return settings


@pytest.fixture
def mock_vectorstore():
    """Mock vectorstore with common methods."""
    store = Mock()
    store.check_db_populated = Mock(return_value=True)
    return store


@pytest.fixture
def mock_model_dicts():
    """Mock model endpoint dictionaries."""
    return {
        'emb_model_dict': {
            'emb_model': 'test-embedding-model',
            'emb_endpoint': 'http://localhost:8001',
            'max_tokens': 512
        },
        'llm_model_dict': {
            'llm_model': 'test-llm-model',
            'llm_endpoint': 'http://localhost:8002'
        },
        'reranker_model_dict': {
            'reranker_model': 'test-reranker-model',
            'reranker_endpoint': 'http://localhost:8003'
        }
    }


@pytest.fixture
def sample_documents():
    """Sample document data for testing."""
    return [
        {
            "page_content": "Artificial intelligence (AI) is intelligence demonstrated by machines.",
            "filename": "ai_basics.pdf",
            "type": "text",
            "source": "/docs/ai_basics.pdf",
            "chunk_id": 1
        },
        {
            "page_content": "Machine learning is a subset of artificial intelligence.",
            "filename": "ml_intro.pdf",
            "type": "text",
            "source": "/docs/ml_intro.pdf",
            "chunk_id": 2
        },
        {
            "page_content": "Deep learning uses neural networks with multiple layers.",
            "filename": "deep_learning.pdf",
            "type": "text",
            "source": "/docs/deep_learning.pdf",
            "chunk_id": 3
        }
    ]


@pytest.fixture
def sample_perf_metrics():
    """Sample performance metrics for testing."""
    return {
        "retrieve_time": 0.15,
        "rerank_time": 0.12,
        "inference_time": 1.25,
        "completion_tokens": 150,
        "prompt_tokens": 500,
        "request_id": "test-request-id-123",
        "timestamp": 1678901234.567,
        "readable_timestamp": "2023-03-15 14:30:34"
    }


@pytest.fixture
def valid_reference_request():
    """Valid reference request payload."""
    return {"prompt": "What is artificial intelligence?"}


@pytest.fixture
def valid_chat_request():
    """Valid chat completion request payload."""
    return {
        "messages": [{"content": "What is artificial intelligence?"}],
        "stream": False,
        "max_tokens": 512,
        "temperature": 0.1
    }


@pytest.fixture
def valid_chat_request_streaming():
    """Valid streaming chat completion request payload."""
    return {
        "messages": [{"content": "What is artificial intelligence?"}],
        "stream": True,
        "max_tokens": 512,
        "temperature": 0.1
    }


@pytest.fixture
def german_chat_request():
    """German language chat request."""
    return {
        "messages": [{"content": "Was ist künstliche Intelligenz?"}],
        "stream": False
    }


@pytest.fixture
def mock_vllm_response():
    """Mock vLLM non-streaming response."""
    return {
        "choices": [
            {
                "message": {
                    "content": "Based on the retrieved documents, artificial intelligence is intelligence demonstrated by machines."
                }
            }
        ]
    }


@pytest.fixture
def mock_vllm_stream():
    """Mock vLLM streaming response generator."""
    def stream_generator():
        chunks = [
            'data: {"choices":[{"delta":{"content":"Based on"}}]}\n\n',
            'data: {"choices":[{"delta":{"content":" the retrieved"}}]}\n\n',
            'data: {"choices":[{"delta":{"content":" documents"}}]}\n\n',
            'data: {"choices":[{"delta":{"content":"..."}}]}\n\n',
        ]
        for chunk in chunks:
            yield chunk
    return stream_generator()


@pytest.fixture
def mock_models_response():
    """Mock models list response."""
    return {
        "object": "list",
        "data": [
            {
                "id": "test-llm-model",
                "object": "model",
                "created": 1234567890,
                "owned_by": "test"
            }
        ]
    }


@pytest.fixture
def mock_perf_registry():
    """Mock performance registry."""
    registry = Mock()
    registry.add_metric = Mock()
    registry.get_metrics = Mock(return_value=[])
    registry.get_metric_by_request_id = Mock(return_value=None)
    return registry


@pytest.fixture
def mock_language_detector():
    """Mock language detector."""
    from lingua import Language
    detector = Mock()
    detector.detect_language_of = Mock(return_value=Language.ENGLISH)
    return detector


@pytest.fixture(autouse=True)
def reset_global_state(monkeypatch):
    """Reset global state before each test."""
    # This fixture runs automatically before each test
    # to ensure clean state
    pass


@pytest.fixture
def test_client(monkeypatch, mock_settings, mock_vectorstore, mock_model_dicts):
    """
    FastAPI test client with mocked dependencies.
    
    This fixture patches all external dependencies and provides
    a test client for making requests to the API.
    """
    # Patch settings
    monkeypatch.setattr("chatbot.app.settings", mock_settings)
    
    # Patch vectorstore
    monkeypatch.setattr("chatbot.app.vectorstore", mock_vectorstore)
    
    # Patch model dictionaries
    monkeypatch.setattr("chatbot.app.emb_model_dict", mock_model_dicts['emb_model_dict'])
    monkeypatch.setattr("chatbot.app.llm_model_dict", mock_model_dicts['llm_model_dict'])
    monkeypatch.setattr("chatbot.app.reranker_model_dict", mock_model_dicts['reranker_model_dict'])
    
    # Import app after patching
    from chatbot.app import app
    
    # Create test client
    client = TestClient(app)
    return client


@pytest.fixture
def mock_search_only(monkeypatch, sample_documents, sample_perf_metrics):
    """Mock search_only function."""
    mock = Mock(return_value=(sample_documents, sample_perf_metrics))
    monkeypatch.setattr("chatbot.app.search_only", mock)
    return mock


@pytest.fixture
def mock_validate_query_length(monkeypatch):
    """Mock validate_query_length function."""
    mock = Mock(return_value=(True, None))
    monkeypatch.setattr("chatbot.app.validate_query_length", mock)
    return mock


@pytest.fixture
def mock_detect_language(monkeypatch):
    """Mock detect_language function."""
    from lingua import Language
    mock = Mock(return_value=Language.ENGLISH)
    monkeypatch.setattr("chatbot.app.detect_language", mock)
    return mock


@pytest.fixture
def mock_query_vllm_non_stream(monkeypatch, mock_vllm_response):
    """Mock query_vllm_non_stream function."""
    mock = Mock(return_value=mock_vllm_response)
    monkeypatch.setattr("chatbot.app.query_vllm_non_stream", mock)
    return mock


@pytest.fixture
def mock_query_vllm_stream(monkeypatch, mock_vllm_stream):
    """Mock query_vllm_stream function."""
    mock = Mock(return_value=mock_vllm_stream)
    monkeypatch.setattr("chatbot.app.query_vllm_stream", mock)
    return mock


@pytest.fixture
def mock_query_vllm_models(monkeypatch, mock_models_response):
    """Mock query_vllm_models function."""
    mock = Mock(return_value=mock_models_response)
    monkeypatch.setattr("chatbot.app.query_vllm_models", mock)
    return mock


# Markers for test categorization
def pytest_configure(config):
    """Register custom markers."""
    config.addinivalue_line("markers", "unit: Unit tests")
    config.addinivalue_line("markers", "integration: Integration tests")
    config.addinivalue_line("markers", "slow: Slow running tests")
    config.addinivalue_line("markers", "requires_db: Tests requiring database connection")
    config.addinivalue_line("markers", "requires_llm: Tests requiring LLM endpoint")

# Made with Bob
