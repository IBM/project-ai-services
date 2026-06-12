"""
Unit tests for German language support in common/llm_utils.py module.

Tests cover German system prompt selection and formatting in query_vllm_payload.
"""

import pytest
from unittest.mock import Mock, patch, MagicMock


@pytest.mark.unit
class TestQueryVLLMPayloadGermanSupport:
    """Tests for German language support in query_vllm_payload function."""
    
    @patch('common.llm_utils.resolve_model_max_len')
    @patch('chatbot.settings.settings')
    @patch('common.llm_utils.tokenize_with_llm')
    @patch('chatbot.conversation_utils.truncate_history_by_tokens')
    def test_uses_german_system_prompt_for_de_language(
        self, mock_truncate, mock_tokenize, mock_settings, mock_resolve_len
    ):
        """Test uses German system prompt when language is DE."""
        from common.llm_utils import query_vllm_payload
        
        # Setup mocks
        mock_settings.chatbot.german.system_prompt = "Sie sind ein hilfreicher Assistent."
        mock_settings.chatbot.german.query_system_prompt = (
            "Sitzungssprache: Deutsch\n\n"
            "Abgerufener Kontext:\n{context}\n\n"
            "Suchanfrage:\n{rephrased_query}"
        )
        mock_settings.chatbot.english.system_prompt = "You are a helpful assistant."
        mock_settings.chatbot.english.query_system_prompt = "Session language: English\n\n{context}\n\n{rephrased_query}"
        mock_settings.chatbot.initial_system_token_overhead = 50
        mock_settings.chatbot.rag_system_token_overhead = 50
        mock_settings.chatbot.history_token_budget = 500
        
        mock_tokenize.return_value = [0] * 10
        mock_truncate.return_value = []
        mock_resolve_len.return_value = 4096
        
        # Call with German language
        headers, payload = query_vllm_payload(
            question="Test query",
            documents=[{"page_content": "Test context"}],
            llm_endpoint="http://test",
            llm_model="test-model",
            stop_words=[],
            max_new_tokens=500,
            temperature=0.7,
            stream=False,
            lang="DE",
            previous_messages=[],
            rephrased_query="Test query"
        )
        
        # Verify German system prompt is used
        messages = payload["messages"]
        assert len(messages) >= 2
        assert messages[0]["role"] == "system"
        assert messages[0]["content"] == "Sie sind ein hilfreicher Assistent."
        
        # Verify German query system prompt is used
        assert any("Sitzungssprache: Deutsch" in msg["content"] for msg in messages if msg["role"] == "system")
    
    @patch('common.llm_utils.resolve_model_max_len')
    @patch('chatbot.settings.settings')
    @patch('common.llm_utils.tokenize_with_llm')
    @patch('chatbot.conversation_utils.truncate_history_by_tokens')
    def test_uses_english_system_prompt_for_en_language(
        self, mock_truncate, mock_tokenize, mock_settings, mock_resolve_len
    ):
        """Test uses English system prompt when language is EN."""
        from common.llm_utils import query_vllm_payload
        
        # Setup mocks
        mock_settings.chatbot.english.system_prompt = "You are a helpful assistant."
        mock_settings.chatbot.english.query_system_prompt = "Session language: English\n\n{context}\n\n{rephrased_query}"
        mock_settings.chatbot.german.system_prompt = "Sie sind ein hilfreicher Assistent."
        mock_settings.chatbot.german.query_system_prompt = "Sitzungssprache: Deutsch\n\n{context}\n\n{rephrased_query}"
        mock_settings.chatbot.initial_system_token_overhead = 50
        mock_settings.chatbot.rag_system_token_overhead = 50
        mock_settings.chatbot.history_token_budget = 500
        
        mock_tokenize.return_value = [0] * 10
        mock_truncate.return_value = []
        mock_resolve_len.return_value = 4096
        
        # Call with English language
        headers, payload = query_vllm_payload(
            question="Test query",
            documents=[{"page_content": "Test context"}],
            llm_endpoint="http://test",
            llm_model="test-model",
            stop_words=[],
            max_new_tokens=500,
            temperature=0.7,
            stream=False,
            lang="EN",
            previous_messages=[],
            rephrased_query="Test query"
        )
        
        # Verify English system prompt is used
        messages = payload["messages"]
        assert messages[0]["role"] == "system"
        assert messages[0]["content"] == "You are a helpful assistant."
        
        # Verify English query system prompt is used
        assert any("Session language: English" in msg["content"] for msg in messages if msg["role"] == "system")
    
    @patch('common.llm_utils.resolve_model_max_len')
    @patch('chatbot.settings.settings')
    @patch('common.llm_utils.tokenize_with_llm')
    @patch('chatbot.conversation_utils.truncate_history_by_tokens')
    def test_fallback_to_english_for_unsupported_language(
        self, mock_truncate, mock_tokenize, mock_settings, mock_resolve_len
    ):
        """Test falls back to English for unsupported language codes."""
        from common.llm_utils import query_vllm_payload
        
        # Setup mocks
        mock_settings.chatbot.english.system_prompt = "You are a helpful assistant."
        mock_settings.chatbot.english.query_system_prompt = "Session language: English\n\n{context}\n\n{rephrased_query}"
        mock_settings.chatbot.german.system_prompt = "Sie sind ein hilfreicher Assistent."
        mock_settings.chatbot.initial_system_token_overhead = 50
        mock_settings.chatbot.rag_system_token_overhead = 50
        mock_settings.chatbot.history_token_budget = 500
        
        mock_tokenize.return_value = [0] * 10
        mock_truncate.return_value = []
        mock_resolve_len.return_value = 4096
        
        # Call with unsupported language
        headers, payload = query_vllm_payload(
            question="Test query",
            documents=[{"page_content": "Test context"}],
            llm_endpoint="http://test",
            llm_model="test-model",
            stop_words=[],
            max_new_tokens=500,
            temperature=0.7,
            stream=False,
            lang="ES",  # Spanish - not supported, should fallback to English
            previous_messages=[],
            rephrased_query="Test query"
        )
        
        # Should fallback to English
        messages = payload["messages"]
        assert messages[0]["role"] == "system"
        assert messages[0]["content"] == "You are a helpful assistant."
    
    @patch('common.llm_utils.resolve_model_max_len')
    @patch('chatbot.settings.settings')
    @patch('common.llm_utils.tokenize_with_llm')
    @patch('chatbot.conversation_utils.truncate_history_by_tokens')
    def test_german_query_system_prompt_formatting(
        self, mock_truncate, mock_tokenize, mock_settings, mock_resolve_len
    ):
        """Test German query system prompt is formatted correctly with context and query."""
        from common.llm_utils import query_vllm_payload
        
        # Setup mocks
        mock_settings.chatbot.german.system_prompt = "Sie sind ein hilfreicher Assistent."
        mock_settings.chatbot.german.query_system_prompt = (
            "Sitzungssprache: Deutsch\n\n"
            "Abgerufener Kontext:\n{context}\n\n"
            "Suchanfrage:\n{rephrased_query}"
        )
        mock_settings.chatbot.initial_system_token_overhead = 50
        mock_settings.chatbot.rag_system_token_overhead = 50
        mock_settings.chatbot.history_token_budget = 500
        
        mock_tokenize.return_value = [0] * 10
        mock_truncate.return_value = []
        mock_resolve_len.return_value = 4096
        
        test_context = "Dies ist der Testkontext."
        test_query = "Was ist die Antwort?"
        
        # Call with German language
        headers, payload = query_vllm_payload(
            question=test_query,
            documents=[{"page_content": test_context}],
            llm_endpoint="http://test",
            llm_model="test-model",
            stop_words=[],
            max_new_tokens=500,
            temperature=0.7,
            stream=False,
            lang="DE",
            previous_messages=[],
            rephrased_query=test_query
        )
        
        # Find the query system message
        messages = payload["messages"]
        query_system_msg = None
        for msg in messages:
            if msg["role"] == "system" and "Sitzungssprache: Deutsch" in msg["content"]:
                query_system_msg = msg
                break
        
        assert query_system_msg is not None
        assert test_context in query_system_msg["content"]
        assert test_query in query_system_msg["content"]
        assert "Abgerufener Kontext:" in query_system_msg["content"]
        assert "Suchanfrage:" in query_system_msg["content"]
    
    @patch('common.llm_utils.resolve_model_max_len')
    @patch('chatbot.settings.settings')
    @patch('common.llm_utils.tokenize_with_llm')
    @patch('chatbot.conversation_utils.truncate_history_by_tokens')
    def test_german_with_conversation_history(
        self, mock_truncate, mock_tokenize, mock_settings, mock_resolve_len
    ):
        """Test German prompt includes conversation history."""
        from common.llm_utils import query_vllm_payload
        
        # Setup mocks
        mock_settings.chatbot.german.system_prompt = "Sie sind ein hilfreicher Assistent."
        mock_settings.chatbot.german.query_system_prompt = "Sitzungssprache: Deutsch\n\n{context}\n\n{rephrased_query}"
        mock_settings.chatbot.initial_system_token_overhead = 50
        mock_settings.chatbot.rag_system_token_overhead = 50
        mock_settings.chatbot.history_token_budget = 500
        
        mock_tokenize.return_value = [0] * 10
        
        # Mock truncated history
        truncated_history = [
            {"role": "user", "content": "Was ist KI?"},
            {"role": "assistant", "content": "KI steht für Künstliche Intelligenz."}
        ]
        mock_truncate.return_value = truncated_history
        mock_resolve_len.return_value = 4096
        
        # Call with German language and history
        headers, payload = query_vllm_payload(
            question="Erzähl mir mehr",
            documents=[{"page_content": "Test context"}],
            llm_endpoint="http://test",
            llm_model="test-model",
            stop_words=[],
            max_new_tokens=500,
            temperature=0.7,
            stream=False,
            lang="DE",
            previous_messages=truncated_history,
            rephrased_query="Erzähl mir mehr"
        )
        
        # Verify history is included
        messages = payload["messages"]
        assert len(messages) >= 4  # system, history (2), query system, user
        
        # Find history messages
        history_contents = [msg["content"] for msg in messages if msg["role"] in ["user", "assistant"]]
        assert "Was ist KI?" in history_contents
        assert "KI steht für Künstliche Intelligenz." in history_contents


@pytest.mark.unit
class TestLanguageSwitchingInQueryVLLMPayload:
    """Tests for language switching behavior in query_vllm_payload."""
    
    @patch('common.llm_utils.resolve_model_max_len')
    @patch('chatbot.settings.settings')
    @patch('common.llm_utils.tokenize_with_llm')
    @patch('chatbot.conversation_utils.truncate_history_by_tokens')
    def test_consistent_language_across_calls(
        self, mock_truncate, mock_tokenize, mock_settings, mock_resolve_len
    ):
        """Test language remains consistent across multiple calls with same lang parameter."""
        from common.llm_utils import query_vllm_payload
        
        # Setup mocks
        mock_settings.chatbot.german.system_prompt = "Sie sind ein hilfreicher Assistent."
        mock_settings.chatbot.german.query_system_prompt = "Sitzungssprache: Deutsch\n\n{context}\n\n{rephrased_query}"
        mock_settings.chatbot.initial_system_token_overhead = 50
        mock_settings.chatbot.rag_system_token_overhead = 50
        mock_settings.chatbot.history_token_budget = 500
        
        mock_tokenize.return_value = [0] * 10
        mock_truncate.return_value = []
        mock_resolve_len.return_value = 4096
        
        # Multiple calls with German
        headers1, payload1 = query_vllm_payload(
            question="Query 1",
            documents=[{"page_content": "Context 1"}],
            llm_endpoint="http://test",
            llm_model="test-model",
            stop_words=[],
            max_new_tokens=500,
            temperature=0.7,
            stream=False,
            lang="DE",
            previous_messages=[],
            rephrased_query="Query 1"
        )
        
        headers2, payload2 = query_vllm_payload(
            question="Query 2",
            documents=[{"page_content": "Context 2"}],
            llm_endpoint="http://test",
            llm_model="test-model",
            stop_words=[],
            max_new_tokens=500,
            temperature=0.7,
            stream=False,
            lang="DE",
            previous_messages=[],
            rephrased_query="Query 2"
        )
        
        # Both should use German system prompt
        assert payload1["messages"][0]["content"] == payload2["messages"][0]["content"]
        assert "Sie sind ein hilfreicher Assistent" in payload1["messages"][0]["content"]
        assert "Sie sind ein hilfreicher Assistent" in payload2["messages"][0]["content"]

# Made with Bob
