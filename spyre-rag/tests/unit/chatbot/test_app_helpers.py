"""
Unit tests for helper functions in chatbot/app.py
"""

import pytest
from unittest.mock import Mock, AsyncMock, patch


@pytest.mark.unit
class TestGetStopWordsWithSpecialTokens:
    """Tests for get_stop_words_with_special_tokens function"""
    
    def test_none_stop_words_returns_special_tokens_only(self):
        """Test None stop words returns only special tokens"""
        from chatbot.app import get_stop_words_with_special_tokens
        
        result = get_stop_words_with_special_tokens(None)
        
        assert isinstance(result, list)
        assert "[/assistant]" in result
        assert "</s>" in result
        assert "<|endoftext|>" in result
        assert "<|im_end|>" in result
    
    def test_empty_list_stop_words_returns_special_tokens(self):
        """Test empty list returns special tokens"""
        from chatbot.app import get_stop_words_with_special_tokens
        
        result = get_stop_words_with_special_tokens([])
        
        assert isinstance(result, list)
        assert len(result) == 4  # 4 special tokens
        assert "[/assistant]" in result
    
    def test_list_stop_words_adds_special_tokens(self):
        """Test list of stop words gets special tokens added"""
        from chatbot.app import get_stop_words_with_special_tokens
        
        custom_stops = ["STOP", "END"]
        result = get_stop_words_with_special_tokens(custom_stops)
        
        assert "STOP" in result
        assert "END" in result
        assert "[/assistant]" in result
        assert "</s>" in result
        assert len(result) == 6  # 2 custom + 4 special
    
    def test_string_stop_word_converted_to_list(self):
        """Test string stop word is converted to list"""
        from chatbot.app import get_stop_words_with_special_tokens
        
        result = get_stop_words_with_special_tokens("STOP")
        
        assert isinstance(result, list)
        assert "S" in result  # String is iterated character by character
        assert "[/assistant]" in result
    
    def test_no_duplicate_special_tokens(self):
        """Test no duplicate special tokens when already present"""
        from chatbot.app import get_stop_words_with_special_tokens
        
        custom_stops = ["</s>", "CUSTOM"]
        result = get_stop_words_with_special_tokens(custom_stops)
        
        # Count occurrences of </s>
        count = result.count("</s>")
        assert count == 1  # Should only appear once
        assert "CUSTOM" in result
    
    def test_all_special_tokens_included(self):
        """Test all expected special tokens are included"""
        from chatbot.app import get_stop_words_with_special_tokens
        
        result = get_stop_words_with_special_tokens([])
        
        expected_tokens = ["[/assistant]", "</s>", "<|endoftext|>", "<|im_end|>"]
        for token in expected_tokens:
            assert token in result, f"Expected token {token} not found"
    
    def test_preserves_custom_stop_words(self):
        """Test custom stop words are preserved"""
        from chatbot.app import get_stop_words_with_special_tokens
        
        custom_stops = ["STOP1", "STOP2", "STOP3"]
        result = get_stop_words_with_special_tokens(custom_stops)
        
        for stop in custom_stops:
            assert stop in result


@pytest.mark.unit
class TestConversationalModeIntegration:
    """Tests for conversational mode integration in chat_completion endpoint"""
    
    @pytest.mark.asyncio
    async def test_query_rephrasing_with_conversational_mode_enabled(
        self, test_client, monkeypatch
    ):
        """Test query rephrasing is called when conversational mode enabled"""
        
        # Mock all dependencies
        mock_validate = Mock(return_value=(True, None))
        monkeypatch.setattr("chatbot.app.validate_query_length", mock_validate)
        
        # Mock detect_language to return string "EN" (not Language enum)
        mock_detect = Mock(return_value="EN")
        monkeypatch.setattr("chatbot.app.detect_language", mock_detect)
        
        mock_search = Mock(return_value=([{"page_content": "test"}], {}))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        mock_vllm = Mock(return_value={"choices": [{"message": {"content": "Response"}}]})
        monkeypatch.setattr("chatbot.app.query_vllm_non_stream", mock_vllm)
        
        # Mock is_auth_required to return False
        mock_is_auth = AsyncMock(return_value=False)
        monkeypatch.setattr("chatbot.app.is_auth_required", mock_is_auth)
        
        # Mock conversational mode enabled
        mock_settings = Mock()
        mock_settings.chatbot.conversational_mode = True
        mock_settings.chatbot.num_chunks_post_search = 10
        mock_settings.chatbot.num_chunks_post_reranker = 5
        mock_settings.chatbot.score_threshold = 0.5
        mock_settings.common.llm.llm_max_tokens = 512
        mock_settings.query_rephrasing.history_token_budget = 1000
        monkeypatch.setattr("chatbot.app.settings", mock_settings)
        
        # Mock tokenize
        mock_tokenize = Mock(return_value=[0] * 50)
        monkeypatch.setattr("chatbot.app.tokenize_with_llm", mock_tokenize)
        
        # Mock truncate_history_by_tokens
        mock_truncate = Mock(return_value=[{"role": "user", "content": "Previous"}])
        monkeypatch.setattr("chatbot.app.truncate_history_by_tokens", mock_truncate)
        
        # Mock rephrase_query_with_context
        mock_rephrase = AsyncMock(return_value="Rephrased query")
        monkeypatch.setattr("chatbot.app.rephrase_query_with_context", mock_rephrase)
        
        # Mock concurrency limiter
        mock_limiter = Mock()
        mock_limiter.locked = Mock(return_value=False)
        mock_limiter.acquire = AsyncMock()
        mock_limiter.release = Mock()
        monkeypatch.setattr("chatbot.app.concurrency_limiter", mock_limiter)
        
        # Mock vectorstore
        mock_vectorstore = Mock()
        monkeypatch.setattr("chatbot.app.vectorstore", mock_vectorstore)
        
        request_data = {
            "messages": [
                {"content": "What is Spyre?"},
                {"content": "Tell me more"}
            ],
            "stream": False
        }
        
        response = test_client.post("/v1/chat/completions", json=request_data)
        
        assert response.status_code == 200
        # Verify rephrase was called
        mock_rephrase.assert_called_once()
        # Verify search was called with rephrased query
        call_args = mock_search.call_args[0]
        assert call_args[0] == "Rephrased query"
    
    @pytest.mark.asyncio
    async def test_query_rephrasing_skipped_for_german(
        self, test_client, monkeypatch
    ):
        """Test query rephrasing is skipped for non-English queries"""
        
        # Mock all dependencies
        mock_validate = Mock(return_value=(True, None))
        monkeypatch.setattr("chatbot.app.validate_query_length", mock_validate)
        
        # Mock detect_language to return string "DE" (not Language enum)
        mock_detect = Mock(return_value="DE")
        monkeypatch.setattr("chatbot.app.detect_language", mock_detect)
        
        mock_search = Mock(return_value=([{"page_content": "test"}], {}))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        mock_vllm = Mock(return_value={"choices": [{"message": {"content": "Antwort"}}]})
        monkeypatch.setattr("chatbot.app.query_vllm_non_stream", mock_vllm)
        
        # Mock is_auth_required to return False
        mock_is_auth = AsyncMock(return_value=False)
        monkeypatch.setattr("chatbot.app.is_auth_required", mock_is_auth)
        
        # Mock conversational mode enabled
        mock_settings = Mock()
        mock_settings.chatbot.conversational_mode = True
        mock_settings.chatbot.num_chunks_post_search = 10
        mock_settings.chatbot.num_chunks_post_reranker = 5
        mock_settings.chatbot.score_threshold = 0.5
        mock_settings.common.llm.llm_max_tokens = 512
        monkeypatch.setattr("chatbot.app.settings", mock_settings)
        
        # Mock rephrase_query_with_context
        mock_rephrase = AsyncMock(return_value="Should not be called")
        monkeypatch.setattr("chatbot.app.rephrase_query_with_context", mock_rephrase)
        
        # Mock concurrency limiter
        mock_limiter = Mock()
        mock_limiter.locked = Mock(return_value=False)
        mock_limiter.acquire = AsyncMock()
        mock_limiter.release = Mock()
        monkeypatch.setattr("chatbot.app.concurrency_limiter", mock_limiter)
        
        # Mock vectorstore
        mock_vectorstore = Mock()
        monkeypatch.setattr("chatbot.app.vectorstore", mock_vectorstore)
        
        request_data = {
            "messages": [
                {"content": "Was ist Spyre?"},
                {"content": "Erzähl mir mehr"}
            ],
            "stream": False
        }
        
        response = test_client.post("/v1/chat/completions", json=request_data)
        
        assert response.status_code == 200
        # Verify rephrase was NOT called for German
        mock_rephrase.assert_not_called()
    
    @pytest.mark.asyncio
    async def test_query_rephrasing_skipped_without_history(
        self, test_client, monkeypatch
    ):
        """Test query rephrasing is skipped when no conversation history"""
        
        # Mock all dependencies
        mock_validate = Mock(return_value=(True, None))
        monkeypatch.setattr("chatbot.app.validate_query_length", mock_validate)
        
        # Mock detect_language to return string "EN" (not Language enum)
        mock_detect = Mock(return_value="EN")
        monkeypatch.setattr("chatbot.app.detect_language", mock_detect)
        
        mock_search = Mock(return_value=([{"page_content": "test"}], {}))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        mock_vllm = Mock(return_value={"choices": [{"message": {"content": "Response"}}]})
        monkeypatch.setattr("chatbot.app.query_vllm_non_stream", mock_vllm)
        
        # Mock is_auth_required to return False
        mock_is_auth = AsyncMock(return_value=False)
        monkeypatch.setattr("chatbot.app.is_auth_required", mock_is_auth)
        
        # Mock conversational mode enabled
        mock_settings = Mock()
        mock_settings.chatbot.conversational_mode = True
        mock_settings.chatbot.num_chunks_post_search = 10
        mock_settings.chatbot.num_chunks_post_reranker = 5
        mock_settings.chatbot.score_threshold = 0.5
        mock_settings.common.llm.llm_max_tokens = 512
        monkeypatch.setattr("chatbot.app.settings", mock_settings)
        
        # Mock rephrase_query_with_context
        mock_rephrase = AsyncMock(return_value="Should not be called")
        monkeypatch.setattr("chatbot.app.rephrase_query_with_context", mock_rephrase)
        
        # Mock concurrency limiter
        mock_limiter = Mock()
        mock_limiter.locked = Mock(return_value=False)
        mock_limiter.acquire = AsyncMock()
        mock_limiter.release = Mock()
        monkeypatch.setattr("chatbot.app.concurrency_limiter", mock_limiter)
        
        # Mock vectorstore
        mock_vectorstore = Mock()
        monkeypatch.setattr("chatbot.app.vectorstore", mock_vectorstore)
        
        # Single message - no history
        request_data = {
            "messages": [{"content": "What is Spyre?"}],
            "stream": False
        }
        
        response = test_client.post("/v1/chat/completions", json=request_data)
        
        assert response.status_code == 200
        # Verify rephrase was NOT called (no history)
        mock_rephrase.assert_not_called()
    
    @pytest.mark.asyncio
    async def test_history_truncation_before_rephrasing(
        self, test_client, monkeypatch
    ):
        """Test conversation history is truncated before rephrasing"""
        
        # Mock all dependencies
        mock_validate = Mock(return_value=(True, None))
        monkeypatch.setattr("chatbot.app.validate_query_length", mock_validate)
        
        # Mock detect_language to return string "EN" (not Language enum)
        mock_detect = Mock(return_value="EN")
        monkeypatch.setattr("chatbot.app.detect_language", mock_detect)
        
        mock_search = Mock(return_value=([{"page_content": "test"}], {}))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        mock_vllm = Mock(return_value={"choices": [{"message": {"content": "Response"}}]})
        monkeypatch.setattr("chatbot.app.query_vllm_non_stream", mock_vllm)
        
        # Mock is_auth_required to return False
        mock_is_auth = AsyncMock(return_value=False)
        monkeypatch.setattr("chatbot.app.is_auth_required", mock_is_auth)
        
        # Mock conversational mode enabled
        mock_settings = Mock()
        mock_settings.chatbot.conversational_mode = True
        mock_settings.chatbot.num_chunks_post_search = 10
        mock_settings.chatbot.num_chunks_post_reranker = 5
        mock_settings.chatbot.score_threshold = 0.5
        mock_settings.common.llm.llm_max_tokens = 512
        mock_settings.query_rephrasing.history_token_budget = 1000
        monkeypatch.setattr("chatbot.app.settings", mock_settings)
        
        # Mock tokenize
        mock_tokenize = Mock(return_value=[0] * 50)
        monkeypatch.setattr("chatbot.app.tokenize_with_llm", mock_tokenize)
        
        # Mock truncate_history_by_tokens
        truncated_history = [{"role": "user", "content": "Previous"}]
        mock_truncate = Mock(return_value=truncated_history)
        monkeypatch.setattr("chatbot.app.truncate_history_by_tokens", mock_truncate)
        
        # Mock rephrase_query_with_context
        mock_rephrase = AsyncMock(return_value="Rephrased")
        monkeypatch.setattr("chatbot.app.rephrase_query_with_context", mock_rephrase)
        
        # Mock concurrency limiter
        mock_limiter = Mock()
        mock_limiter.locked = Mock(return_value=False)
        mock_limiter.acquire = AsyncMock()
        mock_limiter.release = Mock()
        monkeypatch.setattr("chatbot.app.concurrency_limiter", mock_limiter)
        
        # Mock vectorstore
        mock_vectorstore = Mock()
        monkeypatch.setattr("chatbot.app.vectorstore", mock_vectorstore)
        
        request_data = {
            "messages": [
                {"content": "Message 1"},
                {"content": "Message 2"},
                {"content": "Current query"}
            ],
            "stream": False
        }
        
        response = test_client.post("/v1/chat/completions", json=request_data)
        
        assert response.status_code == 200
        # Verify truncate was called
        mock_truncate.assert_called_once()
        # Verify rephrase was called with truncated history
        rephrase_call_args = mock_rephrase.call_args
        assert rephrase_call_args[1]["previous_messages"] == truncated_history
    
    @pytest.mark.asyncio
    async def test_original_query_used_when_rephrasing_disabled(
        self, test_client, monkeypatch
    ):
        """Test original query is used when conversational mode disabled"""
        
        # Mock all dependencies
        mock_validate = Mock(return_value=(True, None))
        monkeypatch.setattr("chatbot.app.validate_query_length", mock_validate)
        
        # Mock detect_language to return string "EN" (not Language enum)
        mock_detect = Mock(return_value="EN")
        monkeypatch.setattr("chatbot.app.detect_language", mock_detect)
        
        mock_search = Mock(return_value=([{"page_content": "test"}], {}))
        monkeypatch.setattr("chatbot.app.search_only", mock_search)
        
        mock_vllm = Mock(return_value={"choices": [{"message": {"content": "Response"}}]})
        monkeypatch.setattr("chatbot.app.query_vllm_non_stream", mock_vllm)
        
        # Mock is_auth_required to return False
        mock_is_auth = AsyncMock(return_value=False)
        monkeypatch.setattr("chatbot.app.is_auth_required", mock_is_auth)
        
        # Mock conversational mode DISABLED
        mock_settings = Mock()
        mock_settings.chatbot.conversational_mode = False
        mock_settings.chatbot.num_chunks_post_search = 10
        mock_settings.chatbot.num_chunks_post_reranker = 5
        mock_settings.chatbot.score_threshold = 0.5
        mock_settings.common.llm.llm_max_tokens = 512
        monkeypatch.setattr("chatbot.app.settings", mock_settings)
        
        # Mock concurrency limiter
        mock_limiter = Mock()
        mock_limiter.locked = Mock(return_value=False)
        mock_limiter.acquire = AsyncMock()
        mock_limiter.release = Mock()
        monkeypatch.setattr("chatbot.app.concurrency_limiter", mock_limiter)
        
        # Mock vectorstore
        mock_vectorstore = Mock()
        monkeypatch.setattr("chatbot.app.vectorstore", mock_vectorstore)
        
        original_query = "Is it supported?"
        request_data = {
            "messages": [
                {"content": "What is Spyre?"},
                {"content": original_query}
            ],
            "stream": False
        }
        
        response = test_client.post("/v1/chat/completions", json=request_data)
        
        assert response.status_code == 200
        # Verify search was called with original query (not rephrased)
        call_args = mock_search.call_args[0]
        assert call_args[0] == original_query
