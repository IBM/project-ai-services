"""
Unit tests for backend utilities in chatbot/backend_utils.py
"""

import pytest
from unittest.mock import Mock, patch


@pytest.mark.unit
class TestSearchOnly:
    """Tests for search_only function delegating to perform_similarity_search"""

    def _patch_settings(self, monkeypatch, threshold=0.5, search_mode="hybrid", rerank=True, similarity_url="http://similarity:8080"):
        mock_settings = Mock()
        mock_settings.chatbot.score_threshold = threshold
        mock_settings.chatbot.search_mode = search_mode
        mock_settings.chatbot.rerank = rerank
        mock_settings.chatbot.similarity_service_url = similarity_url
        monkeypatch.setattr("chatbot.backend_utils.settings", mock_settings)

    def _mock_session(self, monkeypatch, mock_response):
        """Patch get_similarity_session to return a mock session with the given response."""
        from chatbot import backend_utils
        mock_post = Mock(return_value=mock_response)
        mock_session = Mock()
        mock_session.post = mock_post
        monkeypatch.setattr("chatbot.backend_utils.get_similarity_session", lambda: mock_session)
        return mock_post

    def test_delegates_to_similarity_service_api_with_correct_params(self, monkeypatch):
        """search_only must call similarity service API with correct parameters."""
        from chatbot import backend_utils

        self._patch_settings(monkeypatch, threshold=0.0, search_mode="hybrid", rerank=True)

        mock_response = Mock()
        mock_response.json.return_value = {
            "score_type": "relevance",
            "results": []
        }
        mock_response.raise_for_status = Mock()
        mock_response.headers = {}
        mock_post = self._mock_session(monkeypatch, mock_response)

        backend_utils.search_only(
            question="q",
            top_k=10,
            top_r=5,
        )

        # Verify the API was called with correct parameters
        assert mock_post.called
        call_args = mock_post.call_args
        assert call_args[0][0] == "http://similarity:8080/v1/similarity-search"
        json_payload = call_args[1]["json"]
        assert json_payload["query"] == "q"
        assert json_payload["mode"] == "hybrid"
        assert json_payload["top_k"] == 10
        assert json_payload["rerank"] is True

    def test_returns_perf_stat_dict_with_timing_from_headers(self, monkeypatch):
        """search_only must return perf_stat_dict with timing from similarity service headers."""
        from chatbot import backend_utils

        self._patch_settings(monkeypatch, threshold=0.0)

        doc = {"page_content": "x", "filename": "f", "type": "text", "source": "f", "chunk_id": "1", "score": 0.9}
        mock_response = Mock()
        mock_response.json.return_value = {"score_type": "relevance", "results": [doc]}
        mock_response.raise_for_status = Mock()
        # Mock response headers with timing information
        mock_response.headers = {
            "X-Retrieve-Time": "0.123",
            "X-Rerank-Time": "0.045"
        }
        self._mock_session(monkeypatch, mock_response)

        _, perf_stat_dict = backend_utils.search_only(
            question="q", top_k=10, top_r=5,
        )

        # Verify timing metrics from headers are included
        assert "retrieve_time" in perf_stat_dict
        assert perf_stat_dict["retrieve_time"] == 0.123
        assert "rerank_time" in perf_stat_dict
        assert perf_stat_dict["rerank_time"] == 0.045

    def test_applies_top_r_cutoff(self, monkeypatch):
        """search_only must truncate to top_r documents after retrieval."""
        from chatbot import backend_utils

        self._patch_settings(monkeypatch, threshold=0.0)

        docs = [{"page_content": str(i), "filename": "f", "type": "text",
                 "source": "f", "chunk_id": str(i), "score": 0.9 - 0.05 * i} for i in range(10)]
        mock_response = Mock()
        mock_response.json.return_value = {"score_type": "relevance", "results": docs}
        mock_response.raise_for_status = Mock()
        mock_response.headers = {}
        self._mock_session(monkeypatch, mock_response)

        filtered_docs, _ = backend_utils.search_only(
            question="q", top_k=10, top_r=3,
        )

        assert len(filtered_docs) == 3
        assert filtered_docs == docs[:3]

    def test_filters_by_score_threshold(self, monkeypatch):
        """search_only must drop documents whose score is below settings.chatbot.score_threshold."""
        from chatbot import backend_utils

        self._patch_settings(monkeypatch, threshold=0.5)

        docs = [
            {"page_content": "keep", "filename": "f", "type": "text", "source": "f", "chunk_id": "1", "score": 0.8},
            {"page_content": "drop", "filename": "f", "type": "text", "source": "f", "chunk_id": "2", "score": 0.3},
        ]
        mock_response = Mock()
        mock_response.json.return_value = {"score_type": "relevance", "results": docs}
        mock_response.raise_for_status = Mock()
        mock_response.headers = {}
        self._mock_session(monkeypatch, mock_response)

        filtered_docs, _ = backend_utils.search_only(
            question="q", top_k=10, top_r=10,
        )

        assert len(filtered_docs) == 1
        assert filtered_docs[0]["page_content"] == "keep"

@pytest.mark.unit
class TestSearchOnlyServiceToken:
    """search_only sends a catalog-issued token to similarity (DA2) and fails closed."""

    @pytest.fixture(autouse=True)
    def _clean_client(self):
        from common.auth.service_token import reset_service_token_client
        reset_service_token_client()
        yield
        reset_service_token_client()

    def _setup(self, monkeypatch):
        mock_settings = Mock()
        mock_settings.chatbot.score_threshold = 0.0
        mock_settings.chatbot.search_mode = "hybrid"
        mock_settings.chatbot.rerank = True
        mock_settings.chatbot.similarity_service_url = "http://similarity:7000"
        monkeypatch.setattr("chatbot.backend_utils.settings", mock_settings)

        response = Mock()
        response.json.return_value = {"score_type": "relevance", "results": []}
        response.raise_for_status = Mock()
        response.headers = {}
        post = Mock(return_value=response)
        session = Mock()
        session.post = post
        monkeypatch.setattr("chatbot.backend_utils.get_similarity_session", lambda: session)
        return post

    @staticmethod
    def _install(token=None, error=None):
        from common.auth.service_token import ServiceTokenClient, ServiceCredentialConfig, set_service_token_client

        calls = []

        def exchanger(config, audience):
            calls.append(audience)
            if error:
                raise error
            import time
            return token, time.time() + 900

        config = ServiceCredentialConfig(catalog_url="https://catalog.example", credential_file="/unused")
        set_service_token_client(ServiceTokenClient(config, exchanger))
        return calls

    def test_sends_bearer_token_for_the_similarity_audience(self, monkeypatch):
        from chatbot import backend_utils

        post = self._setup(monkeypatch)
        calls = self._install(token="a.b.c")

        backend_utils.search_only("q", top_k=10, top_r=5)

        assert post.call_args[1]["headers"] == {"Authorization": "Bearer a.b.c"}
        assert calls == ["similarity"]

    def test_token_is_reused_across_calls(self, monkeypatch):
        from chatbot import backend_utils

        self._setup(monkeypatch)
        calls = self._install(token="a.b.c")

        backend_utils.search_only("q", top_k=10, top_r=5)
        backend_utils.search_only("q", top_k=10, top_r=5)

        assert calls == ["similarity"]

    def test_no_credential_configured_sends_no_authorization_header(self, monkeypatch):
        """Old deployments keep working against an open similarity service."""
        from chatbot import backend_utils

        post = self._setup(monkeypatch)
        monkeypatch.delenv("AUTH_SERVICE_CREDENTIAL_FILE", raising=False)

        backend_utils.search_only("q", top_k=10, top_r=5)

        assert post.called
        assert "Authorization" not in post.call_args[1]["headers"]

    def test_configured_but_token_unavailable_sends_no_request(self, monkeypatch):
        from chatbot import backend_utils
        from common.auth import ServiceTokenUnavailable

        post = self._setup(monkeypatch)
        self._install(error=ServiceTokenUnavailable("catalog down"))

        with pytest.raises(RuntimeError, match="Similarity service unavailable"):
            backend_utils.search_only("q", top_k=10, top_r=5)

        assert not post.called

    def test_unsafe_auth_configuration_sends_no_request(self, monkeypatch):
        from chatbot import backend_utils

        post = self._setup(monkeypatch)
        monkeypatch.setenv("AUTH_SERVICE_CREDENTIAL_FILE", "/etc/secret/credential")
        monkeypatch.delenv("AUTH_CATALOG_API_URL", raising=False)

        with pytest.raises(RuntimeError, match="Similarity service unavailable"):
            backend_utils.search_only("q", top_k=10, top_r=5)

        assert not post.called

    def test_error_does_not_carry_the_credential(self, monkeypatch):
        from chatbot import backend_utils
        from common.auth import ServiceTokenUnavailable

        self._setup(monkeypatch)
        self._install(error=ServiceTokenUnavailable("catalog down"))

        with pytest.raises(RuntimeError) as exc:
            backend_utils.search_only("q", top_k=10, top_r=5)

        assert "Bearer" not in str(exc.value)




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
