"""
Unit tests for common/lang_utils.py module.

Tests cover language detection, prompt selection, and max tokens mapping.
"""

import pytest
from unittest.mock import Mock, patch, MagicMock
from lingua import Language


@pytest.mark.unit
class TestLanguageCodes:
    """Tests for language codes mapping."""
    
    def test_language_codes_defined(self):
        """Test language codes are correctly defined."""
        from common.lang_utils import language_codes
        
        assert language_codes["English"] == "EN"
        assert language_codes["German"] == "DE"
        assert len(language_codes) == 2


@pytest.mark.unit
class TestGetPromptForLanguage:
    """Tests for get_prompt_for_language function."""
    
    def test_get_english_prompt(self):
        """Test returns English prompt for EN language code."""
        from common.lang_utils import get_prompt_for_language, language_codes
        
        prompts = {
            language_codes["English"]: "English prompt template",
            language_codes["German"]: "German prompt template"
        }
        
        result = get_prompt_for_language(language_codes["English"], prompts)
        assert result == "English prompt template"
    
    def test_get_german_prompt(self):
        """Test returns German prompt for DE language code."""
        from common.lang_utils import get_prompt_for_language, language_codes
        
        prompts = {
            language_codes["English"]: "English prompt template",
            language_codes["German"]: "German prompt template"
        }
        
        result = get_prompt_for_language(language_codes["German"], prompts)
        assert result == "German prompt template"
    
    def test_fallback_to_english_for_unsupported_language(self):
        """Test falls back to English for unsupported language codes."""
        from common.lang_utils import get_prompt_for_language, language_codes
        
        prompts = {
            language_codes["English"]: "English prompt template",
            language_codes["German"]: "German prompt template"
        }
        
        result = get_prompt_for_language("FR", prompts)
        assert result == "English prompt template"
    
    def test_returns_empty_string_when_no_prompts(self):
        """Test returns empty string when prompts dict is empty."""
        from common.lang_utils import get_prompt_for_language
        
        result = get_prompt_for_language("EN", {})
        assert result == ""
    
    def test_returns_empty_string_when_english_not_in_prompts(self):
        """Test returns empty string when English fallback not available."""
        from common.lang_utils import get_prompt_for_language
        
        prompts = {"FR": "French prompt"}
        result = get_prompt_for_language("ES", prompts)
        assert result == ""


@pytest.mark.unit
class TestGetMaxTokensMap:
    """Tests for get_max_tokens_map function."""
    
    @patch('common.lang_utils.language_codes', {"English": "EN", "German": "DE"})
    def test_get_max_tokens_map_returns_dict(self):
        """Test returns dictionary with language codes and max tokens."""
        from common.lang_utils import get_max_tokens_map
        
        # Mock chatbot settings
        with patch('common.lang_utils.chatbot_settings') as mock_settings:
            mock_settings.llm.english.max_tokens = 500
            mock_settings.llm.german.max_tokens = 700
            
            result = get_max_tokens_map()
            
            assert isinstance(result, dict)
            assert result["EN"] == 500
            assert result["DE"] == 700
    
    @patch('common.lang_utils.language_codes', {"English": "EN", "German": "DE"})
    def test_get_max_tokens_map_different_values(self):
        """Test returns correct max tokens for different languages."""
        from common.lang_utils import get_max_tokens_map
        
        with patch('common.lang_utils.chatbot_settings') as mock_settings:
            mock_settings.llm.english.max_tokens = 1000
            mock_settings.llm.german.max_tokens = 1200
            
            result = get_max_tokens_map()
            
            assert result["EN"] == 1000
            assert result["DE"] == 1200


@pytest.mark.unit
class TestSetupLanguageDetector:
    """Tests for setup_language_detector function."""
    
    def test_setup_language_detector_initializes_detector(self):
        """Test initializes language detector with provided languages."""
        from common.lang_utils import setup_language_detector, _language_detector
        
        # Reset global detector
        import common.lang_utils as lang_utils
        lang_utils._language_detector = None
        
        languages = [Language.ENGLISH, Language.GERMAN]
        setup_language_detector(languages)
        
        # Verify detector was initialized
        assert lang_utils._language_detector is not None
    
    def test_setup_language_detector_only_once(self):
        """Test does not reinitialize if already set up."""
        from common.lang_utils import setup_language_detector
        import common.lang_utils as lang_utils
        
        # Set up detector
        languages = [Language.ENGLISH, Language.GERMAN]
        setup_language_detector(languages)
        first_detector = lang_utils._language_detector
        
        # Try to set up again
        setup_language_detector(languages)
        second_detector = lang_utils._language_detector
        
        # Should be the same instance
        assert first_detector is second_detector


@pytest.mark.unit
class TestDetectLanguage:
    """Tests for detect_language function."""
    
    def test_detect_english_text(self):
        """Test detects English text correctly."""
        from common.lang_utils import detect_language, setup_language_detector, language_codes
        
        # Setup detector
        setup_language_detector([Language.ENGLISH, Language.GERMAN])
        
        text = "This is a test in English language for detection."
        result = detect_language(text, min_confidence=0.7)
        
        assert result == language_codes["English"]
    
    def test_detect_german_text(self):
        """Test detects German text correctly."""
        from common.lang_utils import detect_language, setup_language_detector, language_codes
        
        # Setup detector
        setup_language_detector([Language.ENGLISH, Language.GERMAN])
        
        text = "Dies ist ein Test in deutscher Sprache zur Erkennung."
        result = detect_language(text, min_confidence=0.7)
        
        assert result == language_codes["German"]
    
    def test_detect_language_low_confidence_fallback(self):
        """Test falls back to English when confidence is too low."""
        from common.lang_utils import detect_language, setup_language_detector, language_codes
        
        # Setup detector
        setup_language_detector([Language.ENGLISH, Language.GERMAN])
        
        # Very short text may have low confidence
        text = "Hi"
        result = detect_language(text, min_confidence=0.99)  # Very high threshold
        
        # Should fallback to English
        assert result == language_codes["English"]
    
    def test_detect_language_without_setup_returns_english(self):
        """Test returns English when detector not initialized."""
        from common.lang_utils import detect_language, language_codes
        import common.lang_utils as lang_utils
        
        # Reset detector
        lang_utils._language_detector = None
        
        text = "Any text"
        result = detect_language(text)
        
        assert result == language_codes["English"]
    
    def test_detect_language_custom_min_confidence(self):
        """Test respects custom min_confidence parameter."""
        from common.lang_utils import detect_language, setup_language_detector
        
        # Setup detector
        setup_language_detector([Language.ENGLISH, Language.GERMAN])
        
        text = "This is English text."
        
        # With low confidence threshold
        result_low = detect_language(text, min_confidence=0.1)
        assert result_low in ["EN", "DE"]  # Should detect something
        
        # With very high confidence threshold
        result_high = detect_language(text, min_confidence=0.999)
        assert result_high == "EN"  # Should fallback to English
    
    def test_detect_language_empty_text(self):
        """Test handles empty text gracefully."""
        from common.lang_utils import detect_language, setup_language_detector, language_codes
        
        # Setup detector
        setup_language_detector([Language.ENGLISH, Language.GERMAN])
        
        result = detect_language("", min_confidence=0.7)
        
        # Should fallback to English
        assert result == language_codes["English"]
    
    def test_detect_language_mixed_language_text(self):
        """Test handles mixed language text."""
        from common.lang_utils import detect_language, setup_language_detector
        
        # Setup detector
        setup_language_detector([Language.ENGLISH, Language.GERMAN])
        
        # Mixed English and German
        text = "This is English. Das ist Deutsch."
        result = detect_language(text, min_confidence=0.7)
        
        # Should detect one of the languages
        assert result in ["EN", "DE"]


@pytest.mark.unit
class TestLanguageUtilsIntegration:
    """Integration tests for language utilities."""
    
    def test_full_language_detection_workflow(self):
        """Test complete workflow from setup to detection."""
        from common.lang_utils import (
            setup_language_detector,
            detect_language,
            get_prompt_for_language,
            language_codes
        )
        
        # Setup
        setup_language_detector([Language.ENGLISH, Language.GERMAN])
        
        # Detect English
        english_text = "What is artificial intelligence?"
        detected_lang = detect_language(english_text)
        assert detected_lang == language_codes["English"]
        
        # Get appropriate prompt
        prompts = {
            language_codes["English"]: "English prompt",
            language_codes["German"]: "German prompt"
        }
        prompt = get_prompt_for_language(detected_lang, prompts)
        assert prompt == "English prompt"
    
    def test_german_detection_and_prompt_selection(self):
        """Test German detection and prompt selection workflow."""
        from common.lang_utils import (
            setup_language_detector,
            detect_language,
            get_prompt_for_language,
            language_codes
        )
        
        # Setup
        setup_language_detector([Language.ENGLISH, Language.GERMAN])
        
        # Detect German
        german_text = "Was ist künstliche Intelligenz?"
        detected_lang = detect_language(german_text)
        assert detected_lang == language_codes["German"]
        
        # Get appropriate prompt
        prompts = {
            language_codes["English"]: "English prompt",
            language_codes["German"]: "German prompt"
        }
        prompt = get_prompt_for_language(detected_lang, prompts)
        assert prompt == "German prompt"
    
    def test_max_tokens_map_integration(self):
        """Test max tokens map integration with language detection."""
        from common.lang_utils import (
            setup_language_detector,
            detect_language,
            get_max_tokens_map,
            language_codes
        )
        
        # Setup
        setup_language_detector([Language.ENGLISH, Language.GERMAN])
        
        with patch('common.lang_utils.chatbot_settings') as mock_settings:
            mock_settings.llm.english.max_tokens = 500
            mock_settings.llm.german.max_tokens = 700
            
            # Detect language
            text = "This is English"
            detected_lang = detect_language(text)
            
            # Get max tokens for detected language
            max_tokens_map = get_max_tokens_map()
            max_tokens = max_tokens_map.get(detected_lang, 500)
            
            assert max_tokens == 500  # English max tokens

# Made with Bob
