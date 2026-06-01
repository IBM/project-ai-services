"""
Unit tests for backend utilities in chatbot/backend_utils.py
"""

import pytest
from unittest.mock import Mock, patch


@pytest.mark.unit
class TestSearchOnly:
    """Tests for search_only function delegating to perform_similarity_search"""

    def _patch_settings(self, monkeypatch, threshold=0.5, search_mode="hybrid"):
        mock_settings = Mock()
        mock_settings.chatbot.score_threshold = threshold
        mock_settings.chatbot.search_mode = search_mode
        monkeypatch.setattr("chatbot.backend_utils.settings", mock_settings)

    def test_delegates_to_perform_similarity_search_with_hybrid_rerank(self, monkeypatch):
        """search_only must call perform_similarity_search with mode='hybrid', rerank=True, return_timings=True."""
        from chatbot import backend_utils

        self._patch_settings(monkeypatch, threshold=0.0)

        mock_perform = Mock(return_value=([], [], "relevance", {"retrieve_time": 0.1, "rerank_time": 0.2}))
        monkeypatch.setattr("chatbot.backend_utils.perform_similarity_search", mock_perform)

        backend_utils.search_only(
            question="q",
            emb_model="m",
            emb_endpoint="http://emb",
            max_tokens=512,
            reranker_model="r",
            reranker_endpoint="http://rerank",
            top_k=10,
            top_r=5,
            vectorstore=Mock(),
        )

        kwargs = mock_perform.call_args.kwargs
        assert kwargs["mode"] == "hybrid"
        assert kwargs["rerank"] is True
        assert kwargs["return_timings"] is True
        assert kwargs["top_k"] == 10
        assert kwargs["emb_max_model_len"] == 512

    def test_returns_perf_stat_dict_from_shared_function(self, monkeypatch):
        """search_only must propagate the perf_stat_dict from perform_similarity_search."""
        from chatbot import backend_utils

        self._patch_settings(monkeypatch, threshold=0.0)

        perf = {"retrieve_time": 0.15, "rerank_time": 0.12}
        doc = {"page_content": "x", "filename": "f", "type": "text", "source": "f", "chunk_id": "1"}
        mock_perform = Mock(return_value=([doc], [0.9], "relevance", perf))
        monkeypatch.setattr("chatbot.backend_utils.perform_similarity_search", mock_perform)

        _, perf_stat_dict = backend_utils.search_only(
            question="q", emb_model="m", emb_endpoint="http://emb", max_tokens=512,
            reranker_model="r", reranker_endpoint="http://rerank",
            top_k=10, top_r=5, vectorstore=Mock(),
        )

        assert perf_stat_dict == perf

    def test_applies_top_r_cutoff(self, monkeypatch):
        """search_only must truncate to top_r documents after retrieval."""
        from chatbot import backend_utils

        self._patch_settings(monkeypatch, threshold=0.0)

        docs = [{"page_content": str(i), "filename": "f", "type": "text",
                 "source": "f", "chunk_id": str(i)} for i in range(10)]
        scores = [0.9 - 0.05 * i for i in range(10)]
        mock_perform = Mock(return_value=(docs, scores, "relevance", {}))
        monkeypatch.setattr("chatbot.backend_utils.perform_similarity_search", mock_perform)

        filtered_docs, _ = backend_utils.search_only(
            question="q", emb_model="m", emb_endpoint="http://emb", max_tokens=512,
            reranker_model="r", reranker_endpoint="http://rerank",
            top_k=10, top_r=3, vectorstore=Mock(),
        )

        assert len(filtered_docs) == 3
        assert filtered_docs == docs[:3]

    def test_filters_by_score_threshold(self, monkeypatch):
        """search_only must drop documents whose score is below settings.chatbot.score_threshold."""
        from chatbot import backend_utils

        self._patch_settings(monkeypatch, threshold=0.5)

        docs = [
            {"page_content": "keep", "filename": "f", "type": "text", "source": "f", "chunk_id": "1"},
            {"page_content": "drop", "filename": "f", "type": "text", "source": "f", "chunk_id": "2"},
        ]
        scores = [0.8, 0.3]
        mock_perform = Mock(return_value=(docs, scores, "relevance", {}))
        monkeypatch.setattr("chatbot.backend_utils.perform_similarity_search", mock_perform)

        filtered_docs, _ = backend_utils.search_only(
            question="q", emb_model="m", emb_endpoint="http://emb", max_tokens=512,
            reranker_model="r", reranker_endpoint="http://rerank",
            top_k=10, top_r=10, vectorstore=Mock(),
        )

        assert len(filtered_docs) == 1
        assert filtered_docs[0]["page_content"] == "keep"


@pytest.mark.unit
class TestValidateQueryLength:
    """Tests for validate_query_length function"""
    
    def test_valid_query_under_max_length(self, monkeypatch):
        """Test query under max length is valid"""
        from chatbot.backend_utils import validate_query_length
        
        # Mock tokenize to return 50 tokens
        mock_tokenize = Mock(return_value=[0] * 50)
        monkeypatch.setattr("common.validation_utils.tokenize_with_llm", mock_tokenize)
        
        # Mock settings
        mock_settings = Mock()
        mock_settings.chatbot.max_query_token_length = 100
        monkeypatch.setattr("chatbot.backend_utils.settings", mock_settings)
        
        is_valid, error_msg = validate_query_length(
            query="This is a valid query",
            emb_endpoint="http://localhost:8000"
        )
        
        assert is_valid is True
        assert error_msg is None
    
    def test_query_exceeding_max_length_is_invalid(self, monkeypatch):
        """Test query exceeding max length is invalid"""
        from chatbot.backend_utils import validate_query_length
        
        # Mock tokenize to return 150 tokens
        mock_tokenize = Mock(return_value=[0] * 150)
        monkeypatch.setattr("common.validation_utils.tokenize_with_llm", mock_tokenize)
        
        # Mock settings
        mock_settings = Mock()
        mock_settings.chatbot.max_query_token_length = 100
        monkeypatch.setattr("chatbot.backend_utils.settings", mock_settings)
        
        is_valid, error_msg = validate_query_length(
            query="This is a very long query that exceeds the maximum allowed length",
            emb_endpoint="http://localhost:8000"
        )
        
        assert is_valid is False
        assert error_msg is not None
        assert "exceeds maximum" in error_msg.lower()
        assert "150" in error_msg
        assert "100" in error_msg
    
    def test_empty_query(self, monkeypatch):
        """Test empty query"""
        from chatbot.backend_utils import validate_query_length
        
        # Mock tokenize to return 0 tokens
        mock_tokenize = Mock(return_value=[])
        monkeypatch.setattr("common.validation_utils.tokenize_with_llm", mock_tokenize)
        
        # Mock settings
        mock_settings = Mock()
        mock_settings.chatbot.max_query_token_length = 100
        monkeypatch.setattr("chatbot.backend_utils.settings", mock_settings)
        
        is_valid, error_msg = validate_query_length(
            query="",
            emb_endpoint="http://localhost:8000"
        )
        
        assert is_valid is True
        assert error_msg is None
    
    def test_query_exactly_at_max_length(self, monkeypatch):
        """Test query exactly at max length"""
        from chatbot.backend_utils import validate_query_length
        
        # Mock tokenize to return exactly max tokens
        mock_tokenize = Mock(return_value=[0] * 100)
        monkeypatch.setattr("common.validation_utils.tokenize_with_llm", mock_tokenize)
        
        # Mock settings
        mock_settings = Mock()
        mock_settings.chatbot.max_query_token_length = 100
        monkeypatch.setattr("chatbot.backend_utils.settings", mock_settings)
        
        is_valid, error_msg = validate_query_length(
            query="Query at exact limit",
            emb_endpoint="http://localhost:8000"
        )
        
        assert is_valid is True
        assert error_msg is None
    
    def test_tokenization_failure_allows_request(self, monkeypatch):
        """Test tokenization failure allows request to proceed"""
        from chatbot.backend_utils import validate_query_length
        
        # Mock tokenize to raise exception
        mock_tokenize = Mock(side_effect=Exception("Tokenization error"))
        monkeypatch.setattr("common.validation_utils.tokenize_with_llm", mock_tokenize)
        
        # Mock settings
        mock_settings = Mock()
        mock_settings.chatbot.max_query_token_length = 100
        monkeypatch.setattr("chatbot.backend_utils.settings", mock_settings)
        
        is_valid, error_msg = validate_query_length(
            query="Test query",
            emb_endpoint="http://localhost:8000"
        )
        
        # Should allow request to proceed despite tokenization failure
        assert is_valid is True
        assert error_msg is None
    
    def test_tokenize_called_with_correct_parameters(self, monkeypatch):
        """Test tokenize is called with correct parameters"""
        from chatbot.backend_utils import validate_query_length
        
        mock_tokenize = Mock(return_value=[0] * 50)
        monkeypatch.setattr("common.validation_utils.tokenize_with_llm", mock_tokenize)
        
        # Mock settings
        mock_settings = Mock()
        mock_settings.chatbot.max_query_token_length = 100
        monkeypatch.setattr("chatbot.backend_utils.settings", mock_settings)
        
        query = "Test query"
        endpoint = "http://localhost:8000"
        
        validate_query_length(query, endpoint)
        
        mock_tokenize.assert_called_once_with(query, endpoint)
