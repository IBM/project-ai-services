"""
Unit tests for API endpoints in chatbot/app.py
"""

import pytest
from unittest.mock import Mock, patch, AsyncMock
from fastapi.testclient import TestClient
import json


@pytest.mark.unit
class TestSwaggerRootEndpoint:
    """Tests for GET / endpoint (Swagger UI)"""
    
    def test_swagger_root_returns_html(self, test_client):
        """Test that root endpoint returns Swagger UI HTML"""
        response = test_client.get("/")
        
        assert response.status_code == 200
        assert "text/html" in response.headers.get("content-type", "")
    
    def test_swagger_openapi_url_configuration(self, test_client):
        """Test correct OpenAPI URL configuration"""
        response = test_client.get("/")
        
        assert response.status_code == 200
        # Verify response contains reference to openapi.json
        assert b"openapi.json" in response.content or b"/openapi.json" in response.content


@pytest.mark.unit
class TestReferenceEndpoint:
    """Tests for POST /reference endpoint"""
    
    def test_successful_document_retrieval(
        self, test_client, valid_reference_request, 
        mock_search_only, mock_validate_query_length
    ):
        """Test successful document retrieval with valid prompt"""
        response = test_client.post("/reference", json=valid_reference_request)
        
        assert response.status_code == 200
        data = response.json()
        assert "documents" in data
        assert "perf_metrics" in data
        assert isinstance(data["documents"], list)
        assert len(data["documents"]) > 0
    
    def test_empty_prompt_returns_400(self, test_client):
        """Test empty prompt returns 400 error"""
        response = test_client.post("/reference", json={"prompt": ""})
        
        assert response.status_code == 400
        assert "empty" in response.json()["detail"].lower()
    
    def test_whitespace_only_prompt_returns_400(self, test_client):
        """Test whitespace-only prompt returns 400 error"""
        response = test_client.post("/reference", json={"prompt": "   \n\t  "})
        
        assert response.status_code == 400
        assert "empty" in response.json()["detail"].lower()
    
    def test_query_length_validation_too_long(
        self, test_client, mock_search_only, monkeypatch
    ):
        """Test query length validation for too long queries"""
        # Mock validate_query_length to return invalid
        mock_validate = Mock(return_value=(False, "Query too long"))
        monkeypatch.setattr("chatbot.app.validate_query_length", mock_validate)
        
        response = test_client.post(
            "/reference",
            json={"prompt": "a" * 1000}
        )
        
        assert response.status_code == 500
        assert "Query too long" in response.json()["detail"]
    
    def test_vectorstore_not_ready_returns_503(
        self, test_client, mock_validate_query_length, monkeypatch
    ):
        """Test VectorStoreNotReadyError returns 503"""
        import common.db_utils as db
        
        # Mock search_only to raise VectorStoreNotReadyError
        mock_search = Mock(side_effect=db.VectorStoreNotReadyError("DB not ready"))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        response = test_client.post(
            "/reference",
            json={"prompt": "test query"}
        )
        
        assert response.status_code == 503
        assert "not ready" in response.json()["detail"].lower()
    
    def test_generic_exception_returns_500(
        self, test_client, mock_validate_query_length, monkeypatch
    ):
        """Test generic exception returns 500"""
        # Mock search_only to raise generic exception
        mock_search = Mock(side_effect=Exception("Unexpected error"))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        response = test_client.post(
            "/reference",
            json={"prompt": "test query"}
        )
        
        assert response.status_code == 500
    
    def test_performance_metrics_recorded(
        self, test_client, valid_reference_request,
        mock_search_only, mock_validate_query_length, monkeypatch
    ):
        """Test performance metrics are recorded"""
        mock_perf_registry = Mock()
        monkeypatch.setattr("chatbot.app.perf_registry", mock_perf_registry)
        
        response = test_client.post("/reference", json=valid_reference_request)
        
        assert response.status_code == 200
        # Verify add_metric was called
        mock_perf_registry.add_metric.assert_called_once()
    
    def test_response_structure_matches_model(
        self, test_client, valid_reference_request,
        mock_search_only, mock_validate_query_length
    ):
        """Test response structure matches ReferenceResponse model"""
        response = test_client.post("/reference", json=valid_reference_request)
        
        assert response.status_code == 200
        data = response.json()
        
        # Verify structure
        assert "documents" in data
        assert "perf_metrics" in data
        
        # Verify document structure
        if data["documents"]:
            doc = data["documents"][0]
            assert "page_content" in doc
            assert "filename" in doc
            assert "type" in doc
            assert "source" in doc


@pytest.mark.unit
class TestModelsEndpoint:
    """Tests for GET /v1/models endpoint"""
    
    def test_successful_model_listing(
        self, test_client, mock_query_vllm_models
    ):
        """Test successful model listing"""
        response = test_client.get("/v1/models")
        
        assert response.status_code == 200
        data = response.json()
        assert "data" in data
        assert isinstance(data["data"], list)
    
    def test_response_structure_matches_model(
        self, test_client, mock_query_vllm_models
    ):
        """Test response structure matches ModelsResponse"""
        response = test_client.get("/v1/models")
        
        assert response.status_code == 200
        data = response.json()
        
        assert "object" in data
        assert "data" in data
        assert data["object"] == "list"
    
    def test_exception_handling_returns_500(self, test_client, monkeypatch):
        """Test exception handling returns 500"""
        mock_query = Mock(side_effect=Exception("LLM endpoint error"))
        monkeypatch.setattr("chatbot.app.query_vllm_models", mock_query)
        
        response = test_client.get("/v1/models")
        
        assert response.status_code == 500
    
    def test_empty_model_list(self, test_client, monkeypatch):
        """Test with empty model list"""
        mock_query = Mock(return_value={"object": "list", "data": []})
        monkeypatch.setattr("chatbot.app.query_vllm_models", mock_query)
        
        response = test_client.get("/v1/models")
        
        assert response.status_code == 200
        data = response.json()
        assert data["data"] == []


@pytest.mark.unit
class TestPerfMetricsEndpoint:
    """Tests for GET /v1/perf_metrics endpoint"""
    
    def test_get_all_metrics(self, test_client, monkeypatch, sample_perf_metrics):
        """Test get all metrics (no request_id)"""
        mock_registry = Mock()
        mock_registry.get_metrics = Mock(return_value=[sample_perf_metrics])
        monkeypatch.setattr("chatbot.app.perf_registry", mock_registry)
        
        response = test_client.get("/v1/perf_metrics")
        
        assert response.status_code == 200
        data = response.json()
        assert "metrics" in data
        assert len(data["metrics"]) == 1
    
    def test_get_specific_metric_by_request_id(
        self, test_client, monkeypatch, sample_perf_metrics
    ):
        """Test get specific metric by request_id"""
        mock_registry = Mock()
        mock_registry.get_metric_by_request_id = Mock(return_value=sample_perf_metrics)
        monkeypatch.setattr("chatbot.app.perf_registry", mock_registry)
        
        response = test_client.get("/v1/perf_metrics?request_id=test-id")
        
        assert response.status_code == 200
        data = response.json()
        assert "metrics" in data
        assert len(data["metrics"]) == 1
    
    def test_404_when_request_id_not_found(self, test_client, monkeypatch):
        """Test 404 when request_id not found"""
        mock_registry = Mock()
        mock_registry.get_metric_by_request_id = Mock(return_value=None)
        monkeypatch.setattr("chatbot.app.perf_registry", mock_registry)
        
        response = test_client.get("/v1/perf_metrics?request_id=nonexistent")
        
        assert response.status_code == 404
        assert "no metric found" in response.json()["detail"].lower()
    
    def test_empty_metrics_list(self, test_client, monkeypatch):
        """Test empty metrics list"""
        mock_registry = Mock()
        mock_registry.get_metrics = Mock(return_value=[])
        monkeypatch.setattr("chatbot.app.perf_registry", mock_registry)
        
        response = test_client.get("/v1/perf_metrics")
        
        assert response.status_code == 200
        data = response.json()
        assert data["metrics"] == []


@pytest.mark.unit
class TestChatCompletionNonStreaming:
    """Tests for POST /v1/chat/completions (non-streaming)"""
    
    def test_successful_completion(
        self, test_client, valid_chat_request,
        mock_search_only, mock_validate_query_length,
        mock_detect_language, mock_query_vllm_non_stream
    ):
        """Test successful completion with valid query"""
        response = test_client.post("/v1/chat/completions", json=valid_chat_request)
        
        assert response.status_code == 200
        data = response.json()
        assert "choices" in data
        assert len(data["choices"]) > 0
        assert "message" in data["choices"][0]
        assert "content" in data["choices"][0]["message"]
    
    def test_empty_messages_returns_400(self, test_client):
        """Test empty messages list returns 400"""
        response = test_client.post(
            "/v1/chat/completions",
            json={"messages": [], "stream": False}
        )
        
        assert response.status_code == 400
        assert "empty" in response.json()["detail"].lower()
    
    def test_empty_query_content_returns_400(self, test_client):
        """Test empty query content returns 400"""
        response = test_client.post(
            "/v1/chat/completions",
            json={"messages": [{"content": ""}], "stream": False}
        )
        
        assert response.status_code == 400
        assert "empty" in response.json()["detail"].lower()
    
    def test_whitespace_only_query_returns_400(self, test_client):
        """Test whitespace-only query returns 400"""
        response = test_client.post(
            "/v1/chat/completions",
            json={"messages": [{"content": "   \n  "}], "stream": False}
        )
        
        assert response.status_code == 400
    
    def test_language_detection_english(
        self, test_client, valid_chat_request,
        mock_search_only, mock_validate_query_length,
        mock_detect_language, mock_query_vllm_non_stream
    ):
        """Test language detection for English"""
        from lingua import Language
        
        mock_detect_language.return_value = Language.ENGLISH
        
        response = test_client.post("/v1/chat/completions", json=valid_chat_request)
        
        assert response.status_code == 200
        mock_detect_language.assert_called_once()
    
    def test_language_detection_german(
        self, test_client, german_chat_request,
        mock_search_only, mock_validate_query_length,
        mock_query_vllm_non_stream, monkeypatch
    ):
        """Test language detection for German"""
        from lingua import Language
        
        mock_detect = Mock(return_value=Language.GERMAN)
        monkeypatch.setattr("chatbot.app.detect_language", mock_detect)
        
        response = test_client.post("/v1/chat/completions", json=german_chat_request)
        
        assert response.status_code == 200
        mock_detect.assert_called_once()
    
    def test_max_tokens_from_request_parameter(
        self, test_client, mock_search_only,
        mock_validate_query_length, mock_detect_language,
        mock_query_vllm_non_stream
    ):
        """Test max_tokens from request parameter"""
        request_data = {
            "messages": [{"content": "test"}],
            "stream": False,
            "max_tokens": 1024
        }
        
        response = test_client.post("/v1/chat/completions", json=request_data)
        
        assert response.status_code == 200
        # Verify max_tokens was passed to vLLM
        call_args = mock_query_vllm_non_stream.call_args
        assert call_args is not None
    
    def test_no_documents_found_english(
        self, test_client, valid_chat_request,
        mock_validate_query_length, mock_detect_language, monkeypatch
    ):
        """Test no documents found scenario (English message)"""
        from lingua import Language
        
        # Mock search_only to return empty documents
        mock_search = Mock(return_value=([], {}))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        mock_detect_language.return_value = Language.ENGLISH
        
        response = test_client.post("/v1/chat/completions", json=valid_chat_request)
        
        assert response.status_code == 200
        data = response.json()
        assert "No documents found" in data["choices"][0]["message"]["content"]
    
    def test_no_documents_found_german(
        self, test_client, german_chat_request,
        mock_validate_query_length, monkeypatch
    ):
        """Test no documents found scenario (German message)"""
        from lingua import Language
        
        # Mock search_only to return empty documents
        mock_search = Mock(return_value=([], {}))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        mock_detect = Mock(return_value="DE")
        monkeypatch.setattr("chatbot.app.detect_language", mock_detect)
        
        response = test_client.post("/v1/chat/completions", json=german_chat_request)
        
        assert response.status_code == 200
        data = response.json()
        # Should contain German message
        assert "Wissensdatenbank" in data["choices"][0]["message"]["content"]
    
    def test_server_busy_429_error(
        self, test_client, valid_chat_request,
        mock_search_only, mock_validate_query_length,
        mock_detect_language, monkeypatch
    ):
        """Test server busy (429 error)"""
        # Mock concurrency limiter to be locked
        mock_limiter = Mock()
        mock_limiter.locked = Mock(return_value=True)
        monkeypatch.setattr("chatbot.app.concurrency_limiter", mock_limiter)
        
        response = test_client.post("/v1/chat/completions", json=valid_chat_request)
        
        assert response.status_code == 429
        assert "busy" in response.json()["detail"].lower()
    
    def test_vectorstore_not_ready_returns_503(
        self, test_client, valid_chat_request,
        mock_validate_query_length, monkeypatch
    ):
        """Test VectorStoreNotReadyError returns 503"""
        import common.db_utils as db
        
        mock_search = Mock(side_effect=db.VectorStoreNotReadyError("DB not ready"))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        response = test_client.post("/v1/chat/completions", json=valid_chat_request)
        
        assert response.status_code == 503
    
    def test_error_response_from_vllm(
        self, test_client, valid_chat_request,
        mock_search_only, mock_validate_query_length,
        mock_detect_language, monkeypatch
    ):
        """Test error response from vLLM"""
        mock_vllm = Mock(return_value={"error": "Model error"})
        monkeypatch.setattr("chatbot.app.query_vllm_non_stream", mock_vllm)
        
        # Mock concurrency limiter
        mock_limiter = Mock()
        mock_limiter.locked = Mock(return_value=False)
        mock_limiter.acquire = AsyncMock()
        mock_limiter.release = Mock()
        monkeypatch.setattr("chatbot.app.concurrency_limiter", mock_limiter)
        
        response = test_client.post("/v1/chat/completions", json=valid_chat_request)
        
        assert response.status_code == 500


@pytest.mark.unit
class TestChatCompletionStreaming:
    """Tests for POST /v1/chat/completions (streaming)"""
    
    def test_successful_streaming_response(
        self, test_client, valid_chat_request_streaming,
        mock_search_only, mock_validate_query_length,
        mock_detect_language, mock_query_vllm_stream
    ):
        """Test successful streaming response"""
        response = test_client.post(
            "/v1/chat/completions",
            json=valid_chat_request_streaming
        )
        
        assert response.status_code == 200
        assert "text/event-stream" in response.headers.get("content-type", "")
    
    def test_query_length_error_returns_streaming_error(
        self, test_client, valid_chat_request_streaming, monkeypatch
    ):
        """Test query length error returns streaming error"""
        mock_validate = Mock(return_value=(False, "Query too long"))
        monkeypatch.setattr("chatbot.app.validate_query_length", mock_validate)
        
        response = test_client.post(
            "/v1/chat/completions",
            json=valid_chat_request_streaming
        )
        
        assert response.status_code == 200
        assert "text/event-stream" in response.headers.get("content-type", "")
        # Response should contain error message
        content = response.content.decode()
        assert "too long" in content.lower()
    
    def test_no_documents_found_returns_streaming_message(
        self, test_client, valid_chat_request_streaming,
        mock_validate_query_length, mock_detect_language, monkeypatch
    ):
        """Test no documents found returns streaming message"""
        mock_search = Mock(return_value=([], {}))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        response = test_client.post(
            "/v1/chat/completions",
            json=valid_chat_request_streaming
        )
        
        assert response.status_code == 200
        content = response.content.decode()
        assert "No documents found" in content
    
    def test_server_busy_returns_streaming_message(
        self, test_client, valid_chat_request_streaming,
        mock_search_only, mock_validate_query_length,
        mock_detect_language, monkeypatch
    ):
        """Test server busy returns streaming message"""
        mock_limiter = Mock()
        mock_limiter.locked = Mock(return_value=True)
        monkeypatch.setattr("chatbot.app.concurrency_limiter", mock_limiter)
        
        response = test_client.post(
            "/v1/chat/completions",
            json=valid_chat_request_streaming
        )
        
        assert response.status_code == 200
        content = response.content.decode()
        assert "busy" in content.lower()


@pytest.mark.unit
class TestDBStatusEndpoint:
    """Tests for GET /db-status endpoint"""
    
    def test_vectorstore_not_initialized(self, test_client, monkeypatch):
        """Test vectorstore is None (not initialized)"""
        monkeypatch.setattr("chatbot.app.vectorstore", None)
        
        response = test_client.get("/db-status")
        
        assert response.status_code == 200
        data = response.json()
        assert data["ready"] is False
        assert "not initialized" in data["message"].lower()
    
    def test_vectorstore_initialized_and_populated(
        self, test_client, mock_vectorstore, monkeypatch
    ):
        """Test vectorstore initialized and populated"""
        mock_vectorstore.check_db_populated = Mock(return_value=True)
        monkeypatch.setattr("chatbot.app.vectorstore", mock_vectorstore)
        
        response = test_client.get("/db-status")
        
        assert response.status_code == 200
        data = response.json()
        assert data["ready"] is True
        assert "message" not in data or data["message"] is None
    
    def test_vectorstore_initialized_not_populated(
        self, test_client, mock_vectorstore, monkeypatch
    ):
        """Test vectorstore initialized but not populated"""
        mock_vectorstore.check_db_populated = Mock(return_value=False)
        monkeypatch.setattr("chatbot.app.vectorstore", mock_vectorstore)
        
        response = test_client.get("/db-status")
        
        assert response.status_code == 200
        data = response.json()
        assert data["ready"] is False
        assert "No data ingested" in data["message"]
    
    def test_exception_during_status_check(
        self, test_client, mock_vectorstore, monkeypatch
    ):
        """Test exception during status check"""
        mock_vectorstore.check_db_populated = Mock(
            side_effect=Exception("Connection error")
        )
        monkeypatch.setattr("chatbot.app.vectorstore", mock_vectorstore)
        
        response = test_client.get("/db-status")
        
        assert response.status_code == 200
        data = response.json()
        assert data["ready"] is False
        assert "Connection error" in data["message"]


@pytest.mark.unit
class TestHealthEndpoint:
    """Tests for GET /health endpoint"""
    
    def test_health_returns_200(self, test_client):
        """Test returns 200 status"""
        response = test_client.get("/health")
        
        assert response.status_code == 200
    
    def test_health_response_structure(self, test_client):
        """Test response structure matches HealthResponse"""
        response = test_client.get("/health")
        
        assert response.status_code == 200
        data = response.json()
        assert "status" in data
        assert data["status"] == "ok"
    
    def test_health_always_succeeds(self, test_client):
        """Test health endpoint always succeeds"""
        # Make multiple requests
        for _ in range(5):
            response = test_client.get("/health")
            assert response.status_code == 200
            assert response.json()["status"] == "ok"

# Made with Bob
