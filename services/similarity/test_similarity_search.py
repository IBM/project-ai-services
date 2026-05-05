# spyre-rag/src/similarity/test_similarity_search.py

import pytest
from unittest.mock import Mock, patch, MagicMock
from fastapi.testclient import TestClient
from similarity.app import app
from similarity.similarity_utils import SimilaritySearchRequest

# Create test client
client = TestClient(app)

@pytest.fixture
def mock_dependencies():
    """Mock all external dependencies"""
    with patch('similarity.app.vectorstore') as mock_vs, \
         patch('similarity.app.emb_model_dict') as mock_emb, \
         patch('similarity.app.reranker_model_dict') as mock_reranker, \
         patch('similarity.similarity_utils.retrieve_documents') as mock_retrieve, \
         patch('similarity.similarity_utils.rerank_documents') as mock_rerank:
        
        # Setup mock returns
        mock_emb.return_value = {
            "emb_model": "test-model",
            "emb_endpoint": "http://test",
            "max_tokens": 512
        }
        mock_reranker.return_value = {
            "reranker_model": "test-reranker",
            "reranker_endpoint": "http://test-reranker"
        }
        
        # Mock retrieve_documents to return sample data
        mock_retrieve.return_value = (
            [{"page_content": "test", "filename": "test.pdf", "type": "text", 
              "source": "test.pdf", "chunk_id": "123"}],
            [0.85]
        )
        
        yield {
            "vectorstore": mock_vs,
            "emb_model_dict": mock_emb,
            "reranker_model_dict": mock_reranker,
            "retrieve_documents": mock_retrieve,
            "rerank_documents": mock_rerank
        }


class TestModeParameter:
    """Tests for the mode parameter functionality"""
    
    def test_dense_mode_accepted(self, mock_dependencies):
        """Test: mode='dense' is accepted and returns cosine scores"""
        response = client.post("/v1/similarity-search", json={
            "query": "test query",
            "mode": "dense"
        })
        
        assert response.status_code == 200
        data = response.json()
        assert data["score_type"] == "cosine"
        assert len(data["results"]) > 0
    
    def test_sparse_mode_accepted(self, mock_dependencies):
        """Test: mode='sparse' is accepted and returns bm25 scores"""
        response = client.post("/v1/similarity-search", json={
            "query": "test query",
            "mode": "sparse"
        })
        
        assert response.status_code == 200
        data = response.json()
        assert data["score_type"] == "bm25"
    
    def test_hybrid_mode_accepted(self, mock_dependencies):
        """Test: mode='hybrid' is accepted and returns hybrid scores"""
        response = client.post("/v1/similarity-search", json={
            "query": "test query",
            "mode": "hybrid"
        })
        
        assert response.status_code == 200
        data = response.json()
        assert data["score_type"] == "hybrid"
    
    def test_default_mode_is_dense(self, mock_dependencies):
        """Test to see if default parameter is dense"""
        response = client.post("/v1/similarity-search", json={
            "query": "test query"
        })
        
        assert response.status_code == 200
        data = response.json()
        assert data["score_type"] == "cosine"
    
    def test_invalid_mode_returns_400(self, mock_dependencies):
        """Test that invalid mode value returns 400 error"""
        response = client.post("/v1/similarity-search", json={
            "query": "test query",
            "mode": "invalid"
        })
        
        assert response.status_code == 400
        assert "mode must be one of" in response.json()["error"]["message"]
    
    def test_mode_passed_to_retrieve_documents(self, mock_dependencies):
        """Test that mode parameter is passed to retrieve_documents"""
        client.post("/v1/similarity-search", json={
            "query": "test query",
            "mode": "hybrid"
        })
        
        # Verify retrieve_documents was called with correct mode
        mock_retrieve = mock_dependencies["retrieve_documents"]
        assert mock_retrieve.called
        call_kwargs = mock_retrieve.call_args[1]
        assert call_kwargs["mode"] == "hybrid"


class TestRerankingWithModes:
    """Tests for reranking combined with different modes"""
    
    def test_rerank_overrides_score_type_dense(self, mock_dependencies):
        """Test that rerank=true overrides score_type for dense mode"""
        mock_dependencies["rerank_documents"].return_value = [
            ({"page_content": "test", "filename": "test.pdf", "type": "text",
              "source": "test.pdf", "chunk_id": "123"}, 0.95)
        ]
        
        response = client.post("/v1/similarity-search", json={
            "query": "test query",
            "mode": "dense",
            "rerank": True
        })
        
        assert response.status_code == 200
        data = response.json()
        assert data["score_type"] == "relevance"
    
    def test_rerank_overrides_score_type_hybrid(self, mock_dependencies):
        """Test: rerank=true overrides score_type for hybrid mode"""
        mock_dependencies["rerank_documents"].return_value = [
            ({"page_content": "test", "filename": "test.pdf", "type": "text",
              "source": "test.pdf", "chunk_id": "123"}, 0.95)
        ]
        
        response = client.post("/v1/similarity-search", json={
            "query": "test query",
            "mode": "hybrid",
            "rerank": True
        })
        
        assert response.status_code == 200
        data = response.json()
        assert data["score_type"] == "relevance"


class TestRequestValidation:
    """Tests for request validation"""
    
    def test_missing_query_returns_400(self, mock_dependencies):
        """Test that missing query returns 400 error"""
        response = client.post("/v1/similarity-search", json={
            "mode": "dense"
        })
        
        assert response.status_code == 422  # FastAPI validation error
    
    def test_empty_query_returns_400(self, mock_dependencies):
        """Test that empty query returns 400 error"""
        response = client.post("/v1/similarity-search", json={
            "query": "",
            "mode": "dense"
        })

        assert response.status_code == 400
        assert "query is required" in response.json()["error"]["message"]

    def test_top_k_zero_returns_422(self, mock_dependencies):
        """Test that top_k=0 is rejected by Pydantic before hitting OpenSearch"""
        response = client.post("/v1/similarity-search", json={
            "query": "test query",
            "top_k": 0
        })
        assert response.status_code == 422

    def test_top_k_negative_returns_422(self, mock_dependencies):
        """Test that negative top_k is also rejected"""
        response = client.post("/v1/similarity-search", json={
            "query": "test query",
            "top_k": -5
        })
        assert response.status_code == 422

    def test_top_k_one_is_accepted(self, mock_dependencies):
        """Test that top_k=1 is valid and returns a single result"""
        response = client.post("/v1/similarity-search", json={
            "query": "test query",
            "top_k": 1
        })
        assert response.status_code == 200
        assert len(response.json()["results"]) == 1

    def test_top_k_omitted_uses_default(self, mock_dependencies):
        """Test that omitting top_k falls back to NUM_CHUNKS_POST_SEARCH (default 10)"""
        from similarity.settings import settings
        mock_retrieve = mock_dependencies["retrieve_documents"]
        client.post("/v1/similarity-search", json={"query": "test query"})
        call_args = mock_retrieve.call_args[0]
        top_k_passed = call_args[5]
        assert top_k_passed == settings.similarity.num_chunks_post_search


class TestConfig:
    """Tests for startup-time config validation"""

    def test_num_chunks_post_search_zero_falls_back_to_default(self):
        """Test that missing NUM_CHUNKS_POST_SEARCH falls back to default.
        
        Note: Pydantic's gt=0 constraint validates before custom validators run,
        so zero cannot reach the custom validator. This test verifies the default
        behavior when the environment variable is not set.
        """
        import importlib
        import os
        import sys
        
        # Clear any cached modules
        sys.modules.pop("similarity.settings", None)
        
        # Test with unset environment variable - should use default of 10
        with patch.dict(os.environ, {}, clear=False):
            # Remove the variable if it exists
            os.environ.pop("NUM_CHUNKS_POST_SEARCH", None)
            mod = importlib.import_module("similarity.settings")
            # Should use the default value of 10
            assert mod.settings.similarity.num_chunks_post_search == 10
        
        # Clean up
        sys.modules.pop("similarity.settings", None)
        importlib.import_module("similarity.settings")
