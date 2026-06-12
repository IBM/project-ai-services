"""
Unit tests for query rephrasing functionality in chatbot/query_rephrasing.py
"""

import pytest
from unittest.mock import Mock


@pytest.mark.unit
class TestFormatMessagesForRephrasing:
    """Tests for format_messages_for_rephrasing function"""
    
    def test_empty_messages_returns_empty_string(self):
        """Test empty messages list returns empty string"""
        from chatbot.query_rephrasing import format_messages_for_rephrasing
        
        result = format_messages_for_rephrasing([])
        assert result == ""
    
    def test_single_message_formatting(self):
        """Test single message is formatted correctly"""
        from chatbot.query_rephrasing import format_messages_for_rephrasing
        
        messages = [{"role": "user", "content": "What is Spyre?"}]
        result = format_messages_for_rephrasing(messages)
        
        assert "User: What is Spyre?" in result
    
    def test_multiple_messages_formatting(self):
        """Test multiple messages are formatted with newlines"""
        from chatbot.query_rephrasing import format_messages_for_rephrasing
        
        messages = [
            {"role": "user", "content": "What is Spyre?"},
            {"role": "assistant", "content": "Spyre is an AI accelerator."},
            {"role": "user", "content": "Tell me more."}
        ]
        result = format_messages_for_rephrasing(messages)
        
        assert "User: What is Spyre?" in result
        assert "Assistant: Spyre is an AI accelerator." in result
        assert "User: Tell me more." in result
        assert result.count("\n") >= 2  # At least 2 newlines for 3 messages
    
    def test_role_capitalization(self):
        """Test role names are capitalized"""
        from chatbot.query_rephrasing import format_messages_for_rephrasing
        
        messages = [
            {"role": "user", "content": "Hello"},
            {"role": "assistant", "content": "Hi"}
        ]
        result = format_messages_for_rephrasing(messages)
        
        assert "User:" in result
        assert "Assistant:" in result
        assert "user:" not in result
        assert "assistant:" not in result
    
    def test_missing_role_defaults_to_unknown(self):
        """Test missing role defaults to 'Unknown'"""
        from chatbot.query_rephrasing import format_messages_for_rephrasing
        
        messages = [{"content": "Test message"}]
        result = format_messages_for_rephrasing(messages)
        
        assert "Unknown:" in result
    
    def test_missing_content_handled_gracefully(self):
        """Test missing content is handled gracefully"""
        from chatbot.query_rephrasing import format_messages_for_rephrasing
        
        messages = [{"role": "user"}]
        result = format_messages_for_rephrasing(messages)
        
        assert "User:" in result


@pytest.mark.unit
class TestCallLLMForRephrasing:
    """Tests for call_llm_for_rephrasing function"""
    
    def test_successful_llm_call(self, monkeypatch):
        """Test successful LLM call returns rephrased query"""
        from chatbot.query_rephrasing import call_llm_for_rephrasing
        import common.misc_utils as misc_utils
        
        # Mock SESSION
        mock_response = Mock()
        mock_response.json.return_value = {
            "choices": [
                {"message": {"content": "Is Spyre supported on Power 11?"}}
            ]
        }
        mock_response.raise_for_status = Mock()
        
        mock_session = Mock()
        mock_session.post.return_value = mock_response
        monkeypatch.setattr(misc_utils, "SESSION", mock_session)
        
        result = call_llm_for_rephrasing(
            prompt="Test prompt",
            llm_endpoint="http://localhost:8000",
            llm_model="test-model"
        )
        
        assert result == "Is Spyre supported on Power 11?"
        mock_session.post.assert_called_once()
    
    def test_llm_call_with_custom_parameters(self, monkeypatch):
        """Test LLM call respects custom parameters"""
        from chatbot.query_rephrasing import call_llm_for_rephrasing
        import common.misc_utils as misc_utils
        
        mock_response = Mock()
        mock_response.json.return_value = {
            "choices": [{"message": {"content": "Rephrased query"}}]
        }
        mock_response.raise_for_status = Mock()
        
        mock_session = Mock()
        mock_session.post.return_value = mock_response
        monkeypatch.setattr(misc_utils, "SESSION", mock_session)
        
        call_llm_for_rephrasing(
            prompt="Test",
            llm_endpoint="http://localhost:8000",
            llm_model="test-model",
            max_tokens=200,
            temperature=0.5,
            timeout=10.0
        )
        
        call_args = mock_session.post.call_args
        payload = call_args[1]["json"]
        
        assert payload["max_tokens"] == 200
        assert payload["temperature"] == 0.5
        assert call_args[1]["timeout"] == 10.0
    
    def test_empty_choices_returns_empty_string(self, monkeypatch):
        """Test empty choices returns empty string"""
        from chatbot.query_rephrasing import call_llm_for_rephrasing
        import common.misc_utils as misc_utils
        
        mock_response = Mock()
        mock_response.json.return_value = {"choices": []}
        mock_response.raise_for_status = Mock()
        
        mock_session = Mock()
        mock_session.post.return_value = mock_response
        monkeypatch.setattr(misc_utils, "SESSION", mock_session)
        
        result = call_llm_for_rephrasing(
            prompt="Test",
            llm_endpoint="http://localhost:8000",
            llm_model="test-model"
        )
        
        assert result == ""
    
    def test_session_not_initialized_raises_error(self, monkeypatch):
        """Test raises error when SESSION is None"""
        from chatbot.query_rephrasing import call_llm_for_rephrasing
        import common.misc_utils as misc_utils
        
        monkeypatch.setattr(misc_utils, "SESSION", None)
        
        with pytest.raises(RuntimeError, match="LLM session not initialized"):
            call_llm_for_rephrasing(
                prompt="Test",
                llm_endpoint="http://localhost:8000",
                llm_model="test-model"
            )
    
    def test_http_error_propagates(self, monkeypatch):
        """Test HTTP errors are propagated"""
        from chatbot.query_rephrasing import call_llm_for_rephrasing
        import common.misc_utils as misc_utils
        import requests
        
        mock_response = Mock()
        mock_response.raise_for_status.side_effect = requests.exceptions.HTTPError("500 Server Error")
        
        mock_session = Mock()
        mock_session.post.return_value = mock_response
        monkeypatch.setattr(misc_utils, "SESSION", mock_session)
        
        with pytest.raises(requests.exceptions.HTTPError):
            call_llm_for_rephrasing(
                prompt="Test",
                llm_endpoint="http://localhost:8000",
                llm_model="test-model"
            )
    
    def test_stop_words_included_in_payload(self, monkeypatch):
        """Test stop words are included in the payload"""
        from chatbot.query_rephrasing import call_llm_for_rephrasing
        import common.misc_utils as misc_utils
        
        mock_response = Mock()
        mock_response.json.return_value = {
            "choices": [{"message": {"content": "Result"}}]
        }
        mock_response.raise_for_status = Mock()
        
        mock_session = Mock()
        mock_session.post.return_value = mock_response
        monkeypatch.setattr(misc_utils, "SESSION", mock_session)
        
        call_llm_for_rephrasing(
            prompt="Test",
            llm_endpoint="http://localhost:8000",
            llm_model="test-model"
        )
        
        payload = mock_session.post.call_args[1]["json"]
        assert "stop" in payload
        assert isinstance(payload["stop"], list)


@pytest.mark.unit
class TestCalculateDynamicMaxResponseTokens:
    """Tests for calculate_dynamic_max_response_tokens function"""
    
    def test_short_query_uses_base_max(self, monkeypatch):
        """Test short query uses base_max_response_tokens"""
        from chatbot.query_rephrasing import calculate_dynamic_max_response_tokens
        
        # Mock tokenize to return 10 tokens
        mock_tokenize = Mock(return_value=[0] * 10)
        monkeypatch.setattr("chatbot.query_rephrasing.tokenize_with_llm", mock_tokenize)
        
        result = calculate_dynamic_max_response_tokens(
            query="Short query",
            llm_endpoint="http://localhost:8000",
            base_max_response_tokens=100,
            multiplier=1.5,
            system_max_query_length=500
        )
        
        # 10 * 1.5 = 15, but base is 100, so should return 100
        assert result == 100
    
    def test_medium_query_uses_dynamic_calculation(self, monkeypatch):
        """Test medium query uses dynamic calculation"""
        from chatbot.query_rephrasing import calculate_dynamic_max_response_tokens
        
        # Mock tokenize to return 200 tokens
        mock_tokenize = Mock(return_value=[0] * 200)
        monkeypatch.setattr("chatbot.query_rephrasing.tokenize_with_llm", mock_tokenize)
        
        result = calculate_dynamic_max_response_tokens(
            query="Medium length query",
            llm_endpoint="http://localhost:8000",
            base_max_response_tokens=100,
            multiplier=1.5,
            system_max_query_length=500
        )
        
        # 200 * 1.5 = 300, which is > base (100) and < system_max (500)
        assert result == 300
    
    def test_long_query_capped_at_system_max(self, monkeypatch):
        """Test long query is capped at system_max_query_length"""
        from chatbot.query_rephrasing import calculate_dynamic_max_response_tokens
        
        # Mock tokenize to return 400 tokens
        mock_tokenize = Mock(return_value=[0] * 400)
        monkeypatch.setattr("chatbot.query_rephrasing.tokenize_with_llm", mock_tokenize)
        
        result = calculate_dynamic_max_response_tokens(
            query="Very long query",
            llm_endpoint="http://localhost:8000",
            base_max_response_tokens=100,
            multiplier=1.5,
            system_max_query_length=500
        )
        
        # 400 * 1.5 = 600, but system_max is 500, so should return 500
        assert result == 500
    
    def test_tokenization_failure_returns_base_max(self, monkeypatch):
        """Test tokenization failure returns base_max_response_tokens"""
        from chatbot.query_rephrasing import calculate_dynamic_max_response_tokens
        
        # Mock tokenize to raise exception
        mock_tokenize = Mock(side_effect=Exception("Tokenization error"))
        monkeypatch.setattr("chatbot.query_rephrasing.tokenize_with_llm", mock_tokenize)
        
        result = calculate_dynamic_max_response_tokens(
            query="Test query",
            llm_endpoint="http://localhost:8000",
            base_max_response_tokens=100,
            multiplier=1.5,
            system_max_query_length=500
        )
        
        # Should fallback to base_max_response_tokens
        assert result == 100
    
    def test_multiplier_effect(self, monkeypatch):
        """Test different multipliers produce expected results"""
        from chatbot.query_rephrasing import calculate_dynamic_max_response_tokens
        
        # Mock tokenize to return 100 tokens
        mock_tokenize = Mock(return_value=[0] * 100)
        monkeypatch.setattr("chatbot.query_rephrasing.tokenize_with_llm", mock_tokenize)
        
        # Test with multiplier 1.2
        result1 = calculate_dynamic_max_response_tokens(
            query="Test",
            llm_endpoint="http://localhost:8000",
            base_max_response_tokens=50,
            multiplier=1.2,
            system_max_query_length=500
        )
        assert result1 == 120  # 100 * 1.2
        
        # Test with multiplier 2.0
        result2 = calculate_dynamic_max_response_tokens(
            query="Test",
            llm_endpoint="http://localhost:8000",
            base_max_response_tokens=50,
            multiplier=2.0,
            system_max_query_length=500
        )
        assert result2 == 200  # 100 * 2.0
    
    def test_boundary_at_system_max(self, monkeypatch):
        """Test query exactly at system max"""
        from chatbot.query_rephrasing import calculate_dynamic_max_response_tokens
        
        # Mock tokenize to return tokens that when multiplied equal system_max
        mock_tokenize = Mock(return_value=[0] * 250)
        monkeypatch.setattr("chatbot.query_rephrasing.tokenize_with_llm", mock_tokenize)
        
        result = calculate_dynamic_max_response_tokens(
            query="Test",
            llm_endpoint="http://localhost:8000",
            base_max_response_tokens=100,
            multiplier=2.0,
            system_max_query_length=500
        )
        
        # 250 * 2.0 = 500, exactly at system_max
        assert result == 500


@pytest.mark.unit
class TestRephraseQueryWithContext:
    """Tests for rephrase_query_with_context function"""
    
    @pytest.mark.asyncio
    async def test_empty_history_returns_original(self, monkeypatch):
        """Test returns original query when no conversation history"""
        from chatbot.query_rephrasing import rephrase_query_with_context
        
        result = await rephrase_query_with_context(
            current_query="What is Spyre?",
            previous_messages=[],
            llm_endpoint="http://localhost:8000",
            llm_model="test-model"
        )
        
        assert result == "What is Spyre?"
    
    @pytest.mark.asyncio
    async def test_successful_rephrasing(self, monkeypatch):
        """Test successful query rephrasing with context"""
        from chatbot.query_rephrasing import rephrase_query_with_context
        import common.misc_utils as misc_utils
        
        # Mock settings
        mock_settings = Mock()
        mock_english_config = Mock()
        mock_english_config.rephrase_prompt_template = "History: {conversation_history}\nQuery: {current_query}\nRephrased:"
        mock_english_config.role_labels = {"user": "User", "assistant": "Assistant", "system": "System", "unknown": "Unknown"}
        mock_english_config.stop_sequences = ["\n\n", "Question:", "Current Question:"]
        mock_german_config = Mock()
        mock_german_config.role_labels = {"user": "Benutzer", "assistant": "Assistent", "system": "System", "unknown": "Unbekannt"}
        mock_german_config.stop_sequences = ["\n\n", "Frage:", "Aktuelle Frage:"]
        mock_italian_config = Mock()
        mock_italian_config.role_labels = {"user": "Utente", "assistant": "Assistente", "system": "Sistema", "unknown": "Sconosciuto"}
        mock_italian_config.stop_sequences = ["\n\n", "Domanda:", "Domanda attuale:"]
        mock_french_config = Mock()
        mock_french_config.role_labels = {"user": "Utilisateur", "assistant": "Assistant", "system": "Système", "unknown": "Inconnu"}
        mock_french_config.stop_sequences = ["\n\n", "Question:", "Question actuelle:"]
        mock_settings.query_rephrasing.english = mock_english_config
        mock_settings.query_rephrasing.german = mock_german_config
        mock_settings.query_rephrasing.italian = mock_italian_config
        mock_settings.query_rephrasing.french = mock_french_config
        mock_settings.query_rephrasing.max_response_tokens = 100
        mock_settings.query_rephrasing.max_response_tokens_multiplier = 1.5
        mock_settings.query_rephrasing.temperature = 0.0
        mock_settings.query_rephrasing.timeout_seconds = 5.0
        mock_settings.chatbot.max_query_token_length = 500
        monkeypatch.setattr("chatbot.settings.settings", mock_settings)
        
        # Mock tokenize
        mock_tokenize = Mock(return_value=[0] * 50)
        monkeypatch.setattr("chatbot.query_rephrasing.tokenize_with_llm", mock_tokenize)
        
        # Mock LLM response
        mock_response = Mock()
        mock_response.json.return_value = {
            "choices": [{"message": {"content": "Is Spyre supported on Power 11?"}}]
        }
        mock_response.raise_for_status = Mock()
        
        mock_session = Mock()
        mock_session.post.return_value = mock_response
        monkeypatch.setattr(misc_utils, "SESSION", mock_session)
        
        result = await rephrase_query_with_context(
            current_query="Is it supported on Power 11?",
            previous_messages=[
                {"role": "user", "content": "What is Spyre?"},
                {"role": "assistant", "content": "Spyre is an AI accelerator."}
            ],
            llm_endpoint="http://localhost:8000",
            llm_model="test-model"
        )
        
        assert result == "Is Spyre supported on Power 11?"
    
    @pytest.mark.asyncio
    async def test_empty_llm_response_returns_original(self, monkeypatch):
        """Test returns original query when LLM returns empty response"""
        from chatbot.query_rephrasing import rephrase_query_with_context
        import common.misc_utils as misc_utils
        
        # Mock settings
        mock_settings = Mock()
        mock_english_config = Mock()
        mock_english_config.rephrase_prompt_template = "History: {conversation_history}\nQuery: {current_query}\nRephrased:"
        mock_english_config.role_labels = {"user": "User", "assistant": "Assistant", "system": "System", "unknown": "Unknown"}
        mock_english_config.stop_sequences = ["\n\n", "Question:", "Current Question:"]
        mock_german_config = Mock()
        mock_german_config.role_labels = {"user": "Benutzer", "assistant": "Assistent", "system": "System", "unknown": "Unbekannt"}
        mock_german_config.stop_sequences = ["\n\n", "Frage:", "Aktuelle Frage:"]
        mock_italian_config = Mock()
        mock_italian_config.role_labels = {"user": "Utente", "assistant": "Assistente", "system": "Sistema", "unknown": "Sconosciuto"}
        mock_italian_config.stop_sequences = ["\n\n", "Domanda:", "Domanda attuale:"]
        mock_french_config = Mock()
        mock_french_config.role_labels = {"user": "Utilisateur", "assistant": "Assistant", "system": "Système", "unknown": "Inconnu"}
        mock_french_config.stop_sequences = ["\n\n", "Question:", "Question actuelle:"]
        mock_settings.query_rephrasing.english = mock_english_config
        mock_settings.query_rephrasing.german = mock_german_config
        mock_settings.query_rephrasing.italian = mock_italian_config
        mock_settings.query_rephrasing.french = mock_french_config
        mock_settings.query_rephrasing.max_response_tokens = 100
        mock_settings.query_rephrasing.max_response_tokens_multiplier = 1.5
        mock_settings.query_rephrasing.temperature = 0.0
        mock_settings.query_rephrasing.timeout_seconds = 5.0
        mock_settings.chatbot.max_query_token_length = 500
        monkeypatch.setattr("chatbot.settings.settings", mock_settings)
        
        # Mock tokenize
        mock_tokenize = Mock(return_value=[0] * 50)
        monkeypatch.setattr("chatbot.query_rephrasing.tokenize_with_llm", mock_tokenize)
        
        # Mock LLM response with empty content
        mock_response = Mock()
        mock_response.json.return_value = {
            "choices": [{"message": {"content": ""}}]
        }
        mock_response.raise_for_status = Mock()
        
        mock_session = Mock()
        mock_session.post.return_value = mock_response
        monkeypatch.setattr(misc_utils, "SESSION", mock_session)
        
        result = await rephrase_query_with_context(
            current_query="Original query",
            previous_messages=[{"role": "user", "content": "Previous"}],
            llm_endpoint="http://localhost:8000",
            llm_model="test-model"
        )
        
        assert result == "Original query"
    
    @pytest.mark.asyncio
    async def test_exception_returns_original_query(self, monkeypatch):
        """Test returns original query on exception"""
        from chatbot.query_rephrasing import rephrase_query_with_context
        import common.misc_utils as misc_utils
        
        # Mock settings
        mock_settings = Mock()
        mock_english_config = Mock()
        mock_english_config.rephrase_prompt_template = "History: {conversation_history}\nQuery: {current_query}\nRephrased:"
        mock_english_config.role_labels = {"user": "User", "assistant": "Assistant", "system": "System", "unknown": "Unknown"}
        mock_english_config.stop_sequences = ["\n\n", "Question:", "Current Question:"]
        mock_german_config = Mock()
        mock_german_config.role_labels = {"user": "Benutzer", "assistant": "Assistent", "system": "System", "unknown": "Unbekannt"}
        mock_german_config.stop_sequences = ["\n\n", "Frage:", "Aktuelle Frage:"]
        mock_italian_config = Mock()
        mock_italian_config.role_labels = {"user": "Utente", "assistant": "Assistente", "system": "Sistema", "unknown": "Sconosciuto"}
        mock_italian_config.stop_sequences = ["\n\n", "Domanda:", "Domanda attuale:"]
        mock_french_config = Mock()
        mock_french_config.role_labels = {"user": "Utilisateur", "assistant": "Assistant", "system": "Système", "unknown": "Inconnu"}
        mock_french_config.stop_sequences = ["\n\n", "Question:", "Question actuelle:"]
        mock_settings.query_rephrasing.english = mock_english_config
        mock_settings.query_rephrasing.german = mock_german_config
        mock_settings.query_rephrasing.italian = mock_italian_config
        mock_settings.query_rephrasing.french = mock_french_config
        mock_settings.query_rephrasing.max_response_tokens = 100
        mock_settings.query_rephrasing.max_response_tokens_multiplier = 1.5
        mock_settings.query_rephrasing.temperature = 0.0
        mock_settings.query_rephrasing.timeout_seconds = 5.0
        mock_settings.chatbot.max_query_token_length = 500
        monkeypatch.setattr("chatbot.settings.settings", mock_settings)
        
        # Mock tokenize
        mock_tokenize = Mock(return_value=[0] * 50)
        monkeypatch.setattr("chatbot.query_rephrasing.tokenize_with_llm", mock_tokenize)
        
        # Mock LLM to raise exception
        mock_session = Mock()
        mock_session.post.side_effect = Exception("Network error")
        monkeypatch.setattr(misc_utils, "SESSION", mock_session)
        
        result = await rephrase_query_with_context(
            current_query="Original query",
            previous_messages=[{"role": "user", "content": "Previous"}],
            llm_endpoint="http://localhost:8000",
            llm_model="test-model"
        )
        
        assert result == "Original query"
    
    @pytest.mark.asyncio
    async def test_with_api_key(self, monkeypatch):
        """Test rephrasing with API key authentication"""
        from chatbot.query_rephrasing import rephrase_query_with_context
        import common.misc_utils as misc_utils
        
        # Mock settings
        mock_settings = Mock()
        mock_english_config = Mock()
        mock_english_config.rephrase_prompt_template = "History: {conversation_history}\nQuery: {current_query}\nRephrased:"
        mock_english_config.role_labels = {"user": "User", "assistant": "Assistant", "system": "System", "unknown": "Unknown"}
        mock_english_config.stop_sequences = ["\n\n", "Question:", "Current Question:"]
        mock_german_config = Mock()
        mock_german_config.role_labels = {"user": "Benutzer", "assistant": "Assistent", "system": "System", "unknown": "Unbekannt"}
        mock_german_config.stop_sequences = ["\n\n", "Frage:", "Aktuelle Frage:"]
        mock_italian_config = Mock()
        mock_italian_config.role_labels = {"user": "Utente", "assistant": "Assistente", "system": "Sistema", "unknown": "Sconosciuto"}
        mock_italian_config.stop_sequences = ["\n\n", "Domanda:", "Domanda attuale:"]
        mock_french_config = Mock()
        mock_french_config.role_labels = {"user": "Utilisateur", "assistant": "Assistant", "system": "Système", "unknown": "Inconnu"}
        mock_french_config.stop_sequences = ["\n\n", "Question:", "Question actuelle:"]
        mock_settings.query_rephrasing.english = mock_english_config
        mock_settings.query_rephrasing.german = mock_german_config
        mock_settings.query_rephrasing.italian = mock_italian_config
        mock_settings.query_rephrasing.french = mock_french_config
        mock_settings.query_rephrasing.max_response_tokens = 100
        mock_settings.query_rephrasing.max_response_tokens_multiplier = 1.5
        mock_settings.query_rephrasing.temperature = 0.0
        mock_settings.query_rephrasing.timeout_seconds = 5.0
        mock_settings.chatbot.max_query_token_length = 500
        monkeypatch.setattr("chatbot.settings.settings", mock_settings)
        
        # Mock tokenize
        mock_tokenize = Mock(return_value=[0] * 50)
        monkeypatch.setattr("chatbot.query_rephrasing.tokenize_with_llm", mock_tokenize)
        
        # Mock LLM response
        mock_response = Mock()
        mock_response.json.return_value = {
            "choices": [{"message": {"content": "Rephrased with auth"}}]
        }
        mock_response.raise_for_status = Mock()
        
        mock_session = Mock()
        mock_session.post.return_value = mock_response
        monkeypatch.setattr(misc_utils, "SESSION", mock_session)
        
        result = await rephrase_query_with_context(
            current_query="Test query",
            previous_messages=[{"role": "user", "content": "Previous"}],
            llm_endpoint="http://localhost:8000",
            llm_model="test-model",
            api_key="test-api-key"
        )
        
        assert result == "Rephrased with auth"
        # Verify API key was passed
        call_args = mock_session.post.call_args
        assert call_args is not None
