"""
Unit tests for backend utilities in chatbot/backend_utils.py
"""

import pytest
from unittest.mock import Mock, patch


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
