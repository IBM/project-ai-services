"""
Unit tests for conversational RAG utilities in chatbot/conversation_utils.py
"""

import pytest
from unittest.mock import Mock, patch


@pytest.mark.unit
class TestMessageToDict:
    """Tests for _message_to_dict helper function"""
    
    def test_dict_message_passthrough(self):
        """Test dict message is normalized correctly"""
        from chatbot.conversation_utils import _message_to_dict
        
        message = {"role": "user", "content": "Hello"}
        result = _message_to_dict(message)
        
        assert result == {"role": "user", "content": "Hello"}
    
    def test_dict_with_missing_role_defaults_to_user(self):
        """Test missing role defaults to 'user'"""
        from chatbot.conversation_utils import _message_to_dict
        
        message = {"content": "Hello"}
        result = _message_to_dict(message)
        
        assert result["role"] == "user"
        assert result["content"] == "Hello"
    
    def test_dict_with_missing_content_defaults_to_empty(self):
        """Test missing content defaults to empty string"""
        from chatbot.conversation_utils import _message_to_dict
        
        message = {"role": "assistant"}
        result = _message_to_dict(message)
        
        assert result["role"] == "assistant"
        assert result["content"] == ""
    
    def test_object_with_attributes(self):
        """Test object with role and content attributes"""
        from chatbot.conversation_utils import _message_to_dict
        
        message = Mock()
        message.role = "assistant"
        message.content = "Response"
        
        result = _message_to_dict(message)
        
        assert result == {"role": "assistant", "content": "Response"}
    
    def test_object_with_missing_attributes_uses_defaults(self):
        """Test object with missing attributes uses defaults"""
        from chatbot.conversation_utils import _message_to_dict
        
        message = Mock(spec=[])  # Object with no attributes
        
        result = _message_to_dict(message)
        
        assert result["role"] == "user"
        assert result["content"] == ""
    
@pytest.mark.unit
class TestGetConversationContext:
    """Tests for get_conversation_context function"""
    
    def test_empty_messages_returns_empty(self):
        """Test empty messages list returns empty query and history"""
        from chatbot.conversation_utils import get_conversation_context
        
        current_query, previous_messages = get_conversation_context([])
        
        assert current_query == ""
        assert previous_messages == []
    
    def test_single_message_returns_query_no_history(self):
        """Test single message returns query with no history"""
        from chatbot.conversation_utils import get_conversation_context
        
        messages = [{"role": "user", "content": "What is AI?"}]
        current_query, previous_messages = get_conversation_context(messages)
        
        assert current_query == "What is AI?"
        assert previous_messages == []
    
    def test_multiple_messages_splits_correctly(self):
        """Test multiple messages splits into query and history"""
        from chatbot.conversation_utils import get_conversation_context
        
        messages = [
            {"role": "user", "content": "What is AI?"},
            {"role": "assistant", "content": "AI is..."},
            {"role": "user", "content": "Tell me more"}
        ]
        current_query, previous_messages = get_conversation_context(messages)
        
        assert current_query == "Tell me more"
        assert len(previous_messages) == 2
        assert previous_messages[0]["content"] == "What is AI?"
        assert previous_messages[1]["content"] == "AI is..."
    
    def test_handles_pydantic_objects(self):
        """Test handles Pydantic-like message objects"""
        from chatbot.conversation_utils import get_conversation_context
        
        msg1 = Mock()
        msg1.role = "user"
        msg1.content = "First"
        
        msg2 = Mock()
        msg2.role = "assistant"
        msg2.content = "Second"
        
        msg3 = Mock()
        msg3.role = "user"
        msg3.content = "Third"
        
        messages = [msg1, msg2, msg3]
        current_query, previous_messages = get_conversation_context(messages)
        
        assert current_query == "Third"
        assert len(previous_messages) == 2
        assert previous_messages[0]["content"] == "First"
        assert previous_messages[1]["content"] == "Second"
    
    def test_mixed_dict_and_object_messages(self):
        """Test handles mixed dict and object messages"""
        from chatbot.conversation_utils import get_conversation_context
        
        msg_obj = Mock()
        msg_obj.role = "assistant"
        msg_obj.content = "Response"
        
        messages = [
            {"role": "user", "content": "Question"},
            msg_obj,
            {"role": "user", "content": "Follow-up"}
        ]
        current_query, previous_messages = get_conversation_context(messages)
        
        assert current_query == "Follow-up"
        assert len(previous_messages) == 2


@pytest.mark.unit
class TestTruncateHistoryByTokens:
    """Tests for truncate_history_by_tokens function"""
    
    def test_empty_messages_returns_empty(self):
        """Test empty messages returns empty list"""
        from chatbot.conversation_utils import truncate_history_by_tokens
        
        def mock_tokenize(text):
            return [0] * len(text)
        
        result = truncate_history_by_tokens(
            messages=[],
            token_budget=1000,
            tokenize_fn=mock_tokenize
        )
        
        assert result == []
    
    def test_all_messages_fit_within_budget(self):
        """Test all messages fit within budget"""
        from chatbot.conversation_utils import truncate_history_by_tokens
        
        # Mock tokenize function to return predictable token counts
        def mock_tokenize(text):
            return [0] * 10  # 10 tokens per message
        
        messages = [
            {"role": "user", "content": "Message 1"},
            {"role": "assistant", "content": "Response 1"},
            {"role": "user", "content": "Message 2"}
        ]
        
        result = truncate_history_by_tokens(
            messages=messages,
            token_budget=100,
            tokenize_fn=mock_tokenize
        )
        
        assert len(result) == 3
        assert result == messages
    
    def test_truncates_oldest_messages_first(self):
        """Test truncates oldest messages when over budget"""
        from chatbot.conversation_utils import truncate_history_by_tokens
        
        # Mock tokenize function
        def mock_tokenize(text):
            return [0] * 50  # 50 tokens per message
        
        messages = [
            {"role": "user", "content": "Old message"},
            {"role": "assistant", "content": "Old response"},
            {"role": "user", "content": "Recent message"}
        ]
        
        result = truncate_history_by_tokens(
            messages=messages,
            token_budget=100,  # Can fit 2 messages
            tokenize_fn=mock_tokenize
        )
        
        assert len(result) == 2
        assert result[0]["content"] == "Old response"
        assert result[1]["content"] == "Recent message"
    
    def test_keeps_newest_message_even_if_over_budget(self):
        """Test keeps newest message even if it exceeds budget"""
        from chatbot.conversation_utils import truncate_history_by_tokens
        
        # Mock tokenize function
        def mock_tokenize(text):
            if "Large" in text:
                return [0] * 2000  # 2000 tokens
            return [0] * 10
        
        messages = [
            {"role": "user", "content": "Small message"},
            {"role": "assistant", "content": "Large message that exceeds budget"}
        ]
        
        result = truncate_history_by_tokens(
            messages=messages,
            token_budget=100,
            tokenize_fn=mock_tokenize
        )
        
        # Should keep the newest message even though it's over budget
        assert len(result) == 1
        assert result[0]["content"] == "Large message that exceeds budget"
    
    def test_maintains_message_order(self):
        """Test maintains chronological order of messages"""
        from chatbot.conversation_utils import truncate_history_by_tokens
        
        def mock_tokenize(text):
            return [0] * 30
        
        messages = [
            {"role": "user", "content": "First"},
            {"role": "assistant", "content": "Second"},
            {"role": "user", "content": "Third"},
            {"role": "assistant", "content": "Fourth"}
        ]
        
        result = truncate_history_by_tokens(
            messages=messages,
            token_budget=100,
            tokenize_fn=mock_tokenize
        )
        
        # Should keep last 3 messages in order
        assert len(result) == 3
        assert result[0]["content"] == "Second"
        assert result[1]["content"] == "Third"
        assert result[2]["content"] == "Fourth"
    
    def test_exception_returns_original_messages(self):
        """Test returns original messages on exception"""
        from chatbot.conversation_utils import truncate_history_by_tokens
        
        # Mock tokenize function to raise exception
        def mock_tokenize(text):
            raise Exception("Tokenization error")
        
        messages = [
            {"role": "user", "content": "Message 1"},
            {"role": "assistant", "content": "Response 1"}
        ]
        
        result = truncate_history_by_tokens(
            messages=messages,
            token_budget=100,
            tokenize_fn=mock_tokenize
        )
        
        # Should return original messages as fallback
        assert result == messages
    
    def test_zero_token_budget_keeps_newest(self):
        """Test with zero token budget keeps newest message"""
        from chatbot.conversation_utils import truncate_history_by_tokens
        
        def mock_tokenize(text):
            return [0] * 10
        
        messages = [
            {"role": "user", "content": "Old"},
            {"role": "assistant", "content": "New"}
        ]
        
        result = truncate_history_by_tokens(
            messages=messages,
            token_budget=0,
            tokenize_fn=mock_tokenize
        )
        
        # Should keep newest message
        assert len(result) == 1
        assert result[0]["content"] == "New"
    
    def test_exact_budget_fit(self):
        """Test messages that exactly fit the budget"""
        from chatbot.conversation_utils import truncate_history_by_tokens
        
        def mock_tokenize(text):
            return [0] * 50
        
        messages = [
            {"role": "user", "content": "Message 1"},
            {"role": "assistant", "content": "Message 2"}
        ]
        
        result = truncate_history_by_tokens(
            messages=messages,
            token_budget=100,  # Exactly 2 * 50 tokens
            tokenize_fn=mock_tokenize
        )
        
        assert len(result) == 2
        assert result == messages
    
    def test_variable_token_counts(self):
        """Test with variable token counts per message"""
        from chatbot.conversation_utils import truncate_history_by_tokens
        
        token_counts = {
            "Short": 10,
            "Medium length": 50,
            "Very long message": 100
        }
        
        def mock_tokenize(text):
            return [0] * token_counts.get(text, 10)
        
        messages = [
            {"role": "user", "content": "Short"},
            {"role": "assistant", "content": "Medium length"},
            {"role": "user", "content": "Very long message"}
        ]
        
        result = truncate_history_by_tokens(
            messages=messages,
            token_budget=155,
            tokenize_fn=mock_tokenize
        )
        
        # Should keep the 2 most recent messages
        assert len(result) == 2
        assert result[0]["content"] == "Medium length"
        assert result[1]["content"] == "Very long message"


@pytest.mark.unit
class TestConversationalRAGIntegration:
    """Integration tests for conversational RAG flow"""
    
    def test_full_conversation_flow(self):
        """Test complete conversation flow with context extraction and truncation"""
        from chatbot.conversation_utils import get_conversation_context, truncate_history_by_tokens
        
        def mock_tokenize(text):
            return [0] * len(text)  # 1 token per character
        
        # Simulate a multi-turn conversation
        messages = [
            {"role": "user", "content": "What is Spyre?"},
            {"role": "assistant", "content": "Spyre is an AI accelerator platform."},
            {"role": "user", "content": "What hardware does it support?"},
            {"role": "assistant", "content": "It supports Power10 and Power11 systems."},
            {"role": "user", "content": "Is it available for Power11?"}
        ]
        
        # Extract current query and history
        current_query, previous_messages = get_conversation_context(messages)
        
        assert current_query == "Is it available for Power11?"
        assert len(previous_messages) == 4
        
        # Truncate history to fit budget
        truncated = truncate_history_by_tokens(
            messages=previous_messages,
            token_budget=100,
            tokenize_fn=mock_tokenize
        )
        
        # Should keep most recent messages that fit
        assert len(truncated) <= len(previous_messages)
        assert truncated[-1]["content"] == "It supports Power10 and Power11 systems."
    
    def test_conversation_with_no_history(self):
        """Test conversation with only current query"""
        from chatbot.conversation_utils import get_conversation_context
        
        messages = [{"role": "user", "content": "Hello"}]
        current_query, previous_messages = get_conversation_context(messages)
        
        assert current_query == "Hello"
        assert previous_messages == []
    
    def test_alternating_roles_preserved(self):
        """Test alternating user/assistant roles are preserved"""
        from chatbot.conversation_utils import get_conversation_context, truncate_history_by_tokens
        
        def mock_tokenize(text):
            return [0] * 10
        
        messages = [
            {"role": "user", "content": "Q1"},
            {"role": "assistant", "content": "A1"},
            {"role": "user", "content": "Q2"},
            {"role": "assistant", "content": "A2"},
            {"role": "user", "content": "Q3"}
        ]
        
        current_query, previous_messages = get_conversation_context(messages)
        truncated = truncate_history_by_tokens(previous_messages, 100, mock_tokenize)
        
        # Verify roles alternate correctly
        for i, msg in enumerate(truncated):
            if i % 2 == 0:
                assert msg["role"] == "user"
            else:
                assert msg["role"] == "assistant"
