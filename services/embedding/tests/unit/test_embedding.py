# services/embedding/tests/unit/test_embedding.py

import sys
from pathlib import Path
from unittest.mock import patch

import pytest
from fastapi.testclient import TestClient

# Add services/ directory to path so embedding.* and common.* resolve.
services_path = Path(__file__).parent.parent.parent.parent
sys.path.insert(0, str(services_path))

from embedding.app import app

client = TestClient(app)

# ---------------------------------------------------------------------------
# Shared mock vectors
# ---------------------------------------------------------------------------

MOCK_VECTOR = [0.1, 0.2, 0.3]
MOCK_MODEL = "openai/clip-vit-base-patch32"
WRONG_MODEL = "unknown/model"


@pytest.fixture(autouse=True)
def patch_globals():
    """Patch module-level globals so tests never need a real vLLM server."""
    with patch("embedding.app._model_name", MOCK_MODEL), \
         patch("embedding.app._endpoint", "http://mock-endpoint"):
        yield


@pytest.fixture
def mock_generate():
    """Patch generate_embeddings to return a single mock vector."""
    with patch(
        "embedding.app.generate_embeddings",
        return_value=[MOCK_VECTOR],
    ) as m:
        yield m


@pytest.fixture
def mock_generate_multi():
    """Patch generate_embeddings to return two mock vectors."""
    with patch(
        "embedding.app.generate_embeddings",
        return_value=[MOCK_VECTOR, MOCK_VECTOR],
    ) as m:
        yield m


# ---------------------------------------------------------------------------
# POST /v1/embeddings — happy path
# ---------------------------------------------------------------------------

class TestEmbeddingsHappyPath:

    def test_single_text_input_returns_200(self, mock_generate):
        """A single text string returns one embedding object."""
        response = client.post(
            "/v1/embeddings",
            json={"model": MOCK_MODEL, "input": "a photo of a cat"},
        )
        assert response.status_code == 200
        data = response.json()
        assert data["model"] == MOCK_MODEL
        assert data["object"] == "list"
        assert len(data["data"]) == 1
        assert data["data"][0]["embedding"] == MOCK_VECTOR
        assert data["data"][0]["index"] == 0

    def test_list_input_returns_one_embedding_per_item(
        self, mock_generate_multi
    ):
        """A list of two strings returns two embedding objects."""
        response = client.post(
            "/v1/embeddings",
            json={
                "model": MOCK_MODEL,
                "input": ["hello world", "another text"],
            },
        )
        assert response.status_code == 200
        data = response.json()
        assert len(data["data"]) == 2
        assert data["data"][0]["index"] == 0
        assert data["data"][1]["index"] == 1

    def test_generate_embeddings_called_with_correct_args(
        self, mock_generate
    ):
        """generate_embeddings receives the resolved inputs, model, endpoint."""
        client.post(
            "/v1/embeddings",
            json={"model": MOCK_MODEL, "input": "test"},
        )
        mock_generate.assert_called_once_with(
            ["test"], MOCK_MODEL, "http://mock-endpoint"
        )


# ---------------------------------------------------------------------------
# POST /v1/embeddings — validation errors
# ---------------------------------------------------------------------------

class TestEmbeddingsValidation:

    def test_empty_string_input_returns_400(self, mock_generate):
        """An empty string in input returns 400."""
        response = client.post(
            "/v1/embeddings",
            json={"model": MOCK_MODEL, "input": "   "},
        )
        assert response.status_code == 400
        assert "input is required" in response.json()["error"]["message"]

    def test_wrong_model_returns_400(self, mock_generate):
        """Requesting a model that is not loaded returns 400."""
        response = client.post(
            "/v1/embeddings",
            json={"model": WRONG_MODEL, "input": "test"},
        )
        assert response.status_code == 400
        assert "not loaded" in response.json()["error"]["message"]

    def test_missing_input_field_returns_422(self, mock_generate):
        """Missing required input field returns 422 (Pydantic validation)."""
        response = client.post(
            "/v1/embeddings",
            json={"model": MOCK_MODEL},
        )
        assert response.status_code == 422

    def test_missing_model_field_returns_422(self, mock_generate):
        """Missing required model field returns 422 (Pydantic validation)."""
        response = client.post(
            "/v1/embeddings",
            json={"input": "test"},
        )
        assert response.status_code == 422


# ---------------------------------------------------------------------------
# POST /v1/embeddings — backend error handling
# ---------------------------------------------------------------------------

class TestEmbeddingsErrorHandling:

    def test_backend_exception_returns_500(self):
        """If generate_embeddings raises, the endpoint returns 500."""
        with patch(
            "embedding.app.generate_embeddings",
            side_effect=RuntimeError("vLLM unavailable"),
        ):
            response = client.post(
                "/v1/embeddings",
                json={"model": MOCK_MODEL, "input": "test"},
            )
        assert response.status_code == 500
        assert "error" in response.json()


# ---------------------------------------------------------------------------
# GET /health
# ---------------------------------------------------------------------------

class TestHealth:

    def test_health_returns_200(self):
        """Health endpoint always returns 200 with status ok."""
        response = client.get("/health")
        assert response.status_code == 200
        assert response.json() == {"status": "ok"}


# ---------------------------------------------------------------------------
# Settings
# ---------------------------------------------------------------------------

class TestSettings:

    def test_default_model_name(self):
        """Default model name is clip-vit-base-patch32."""
        from embedding.settings import EmbeddingServiceConfig
        cfg = EmbeddingServiceConfig()
        assert cfg.model_name == "openai/clip-vit-base-patch32"

    def test_model_name_overridable_via_env(self, monkeypatch):
        """EMBEDDING_MODEL_NAME env var overrides the default."""
        import importlib
        monkeypatch.setenv(
            "EMBEDDING_MODEL_NAME", "google/siglip-base-patch16-224"
        )
        import embedding.settings as mod
        importlib.reload(mod)
        assert (
            mod.EmbeddingServiceConfig().model_name
            == "google/siglip-base-patch16-224"
        )
        importlib.reload(mod)
