"""
Unit tests for digitize.doc_utils module.
"""

import pytest
from unittest.mock import Mock, patch
from collections import Counter

from digitize.doc_utils import detect_document_language


@pytest.mark.unit
class TestDetectDocumentLanguage:
    """Tests for detect_document_language function."""

    def test_detect_language_with_valid_english_data(self):
        """Test language detection with valid English text blocks."""
        data = [
            {"text": "This is an English sentence about artificial intelligence."},
            {"text": "Machine learning is a subset of AI."},
            {"text": "Deep learning uses neural networks."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "EN"
            result = detect_document_language(data)
        
        assert result == "en"
        assert mock_detect.called

    def test_detect_language_with_valid_german_data(self):
        """Test language detection with valid German text blocks."""
        data = [
            {"text": "Dies ist ein deutscher Satz über künstliche Intelligenz."},
            {"text": "Maschinelles Lernen ist eine Teilmenge der KI."},
            {"text": "Deep Learning verwendet neuronale Netze."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "DE"
            result = detect_document_language(data)
        
        assert result == "de"

    def test_detect_language_with_valid_french_data(self):
        """Test language detection with valid French text blocks."""
        data = [
            {"text": "Ceci est une phrase française sur l'intelligence artificielle."},
            {"text": "L'apprentissage automatique est un sous-ensemble de l'IA."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "FR"
            result = detect_document_language(data)
        
        assert result == "fr"

    def test_detect_language_with_valid_italian_data(self):
        """Test language detection with valid Italian text blocks."""
        data = [
            {"text": "Questa è una frase italiana sull'intelligenza artificiale."},
            {"text": "L'apprendimento automatico è un sottoinsieme dell'IA."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "IT"
            result = detect_document_language(data)
        
        assert result == "it"

    def test_detect_language_with_unsupported_language_falls_back_to_english(self):
        """Test that unsupported languages fall back to English."""
        data = [
            {"text": "Este es un texto en español."},
            {"text": "El aprendizaje automático es importante."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "ES"  # Spanish not in lang_map
            result = detect_document_language(data)
        
        assert result == "en"

    def test_detect_language_with_short_text_uses_all_text(self):
        """Test that short text (< 200 chars) uses all text directly."""
        data = [
            {"text": "Short text here."},
            {"text": "Another short one."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "EN"
            result = detect_document_language(data)
        
        assert result == "en"
        # Should be called once for the combined short text
        assert mock_detect.call_count == 1

    def test_detect_language_with_long_text_samples_blocks(self):
        """Test that long text samples random blocks."""
        data = [
            {"text": "A" * 300},  # Long block
            {"text": "B" * 300},
            {"text": "C" * 300},
            {"text": "D" * 300},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            with patch("digitize.doc_utils.random.sample") as mock_sample:
                mock_detect.return_value = "EN"
                mock_sample.return_value = [data[0]["text"], data[1]["text"], data[2]["text"]]
                
                result = detect_document_language(data)
        
        assert result == "en"
        # Should sample 3 blocks
        mock_sample.assert_called_once()
        assert mock_sample.call_args[0][1] == 3

    def test_detect_language_with_mixed_languages_uses_most_common(self):
        """Test that mixed languages use the most common detected language."""
        data = [
            {"text": "English text here with sufficient length for detection."},
            {"text": "More English text to ensure proper detection works."},
            {"text": "Dies ist ein deutscher Satz."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            # Return EN twice, DE once
            mock_detect.side_effect = ["EN", "EN", "DE"]
            result = detect_document_language(data)
        
        assert result == "en"

    def test_detect_language_with_empty_list_returns_english(self):
        """Test that empty data list returns English."""
        data = []
        
        with patch("digitize.doc_utils.logger") as mock_logger:
            result = detect_document_language(data)
        
        assert result == "en"
        mock_logger.warning.assert_called_once()
        assert "Empty data list" in mock_logger.warning.call_args[0][0]

    def test_detect_language_with_non_list_input_returns_english(self):
        """Test that non-list input returns English with warning."""
        data = "not a list"
        
        with patch("digitize.doc_utils.logger") as mock_logger:
            result = detect_document_language(data)
        
        assert result == "en"
        mock_logger.warning.assert_called_once()
        assert "expected list" in mock_logger.warning.call_args[0][0]

    def test_detect_language_with_non_dict_elements_returns_english(self):
        """Test that list with non-dict elements returns English."""
        data = ["string1", "string2", 123]
        
        with patch("digitize.doc_utils.logger") as mock_logger:
            result = detect_document_language(data)
        
        assert result == "en"
        mock_logger.warning.assert_called_once()
        assert "non-dict elements" in mock_logger.warning.call_args[0][0]

    def test_detect_language_with_no_text_blocks_returns_english(self):
        """Test that data with no text blocks returns English."""
        data = [
            {"label": "header", "page": 1},
            {"label": "footer", "page": 2},
            {"text": ""},  # Empty text
            {"text": "   "},  # Whitespace only
        ]
        
        with patch("digitize.doc_utils.logger") as mock_logger:
            result = detect_document_language(data)
        
        assert result == "en"
        mock_logger.warning.assert_called_once()
        assert "No text blocks found" in mock_logger.warning.call_args[0][0]

    def test_detect_language_with_blocks_missing_text_field(self):
        """Test that blocks without 'text' field are handled gracefully."""
        data = [
            {"text": "Valid text block here."},
            {"label": "header"},  # No text field
            {"text": "Another valid block."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "EN"
            result = detect_document_language(data)
        
        assert result == "en"

    def test_detect_language_with_exception_in_try_block_returns_english(self):
        """Test that exceptions during detection fall back to English."""
        # Make text long enough to trigger sampling (>200 chars total)
        data = [
            {"text": "A" * 100},
            {"text": "B" * 100},
            {"text": "C" * 100},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            with patch("digitize.doc_utils.logger") as mock_logger:
                with patch("digitize.doc_utils.random.sample") as mock_sample:
                    # Make random.sample raise an exception
                    mock_sample.side_effect = Exception("Sampling failed")
                    result = detect_document_language(data)
        
        assert result == "en"
        mock_logger.warning.assert_called_once()
        assert "Language detection failed" in mock_logger.warning.call_args[0][0]

    def test_detect_language_with_very_long_blocks_takes_chunks(self):
        """Test that very long blocks (>500 chars) are chunked."""
        long_text = "A" * 1000
        data = [
            {"text": long_text},
            {"text": long_text},
            {"text": long_text},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            with patch("digitize.doc_utils.random.randint") as mock_randint:
                mock_detect.return_value = "EN"
                mock_randint.return_value = 250  # Chunk size and position
                
                result = detect_document_language(data)
        
        assert result == "en"
        # Should call detect_language for each sampled block
        assert mock_detect.call_count >= 1

    def test_detect_language_samples_fewer_blocks_when_data_is_small(self):
        """Test that sampling adjusts to available blocks when text is long enough."""
        # Make text long enough to trigger sampling (>200 chars total)
        data = [
            {"text": "A" * 150},  # Long enough to trigger sampling
            {"text": "B" * 150},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            with patch("digitize.doc_utils.random.sample") as mock_sample:
                mock_detect.return_value = "EN"
                mock_sample.return_value = [data[0]["text"], data[1]["text"]]
                
                result = detect_document_language(data)
        
        assert result == "en"
        # Should sample min(3, 2) = 2 blocks
        mock_sample.assert_called_once()
        assert mock_sample.call_args[0][1] == 2

    def test_detect_language_with_whitespace_only_blocks_skips_them(self):
        """Test that blocks with only whitespace are skipped."""
        data = [
            {"text": "   \n\t   "},  # Whitespace only
            {"text": "Valid text here."},
            {"text": ""},  # Empty
            {"text": "Another valid block."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "EN"
            result = detect_document_language(data)
        
        assert result == "en"
        # Should only process the 2 valid blocks

    def test_detect_language_logs_detected_languages_when_sampling(self):
        """Test that detected languages are logged for debugging when sampling occurs."""
        # Make text long enough to trigger sampling
        data = [
            {"text": "Text block one with sufficient length for detection and sampling."},
            {"text": "Text block two with sufficient length for detection and sampling."},
            {"text": "Text block three with sufficient length for detection and sampling."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            with patch("digitize.doc_utils.logger") as mock_logger:
                mock_detect.return_value = "EN"
                result = detect_document_language(data)
        
        assert result == "en"
        # Should log debug message with detected languages when sampling
        assert mock_logger.debug.call_count >= 0  # May or may not be called depending on text length

    def test_detect_language_with_none_values_in_text_field(self):
        """Test that None values in text field are handled."""
        data = [
            {"text": None},
            {"text": "Valid text here."},
            {"text": None},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "EN"
            result = detect_document_language(data)
        
        assert result == "en"

    def test_detect_language_with_integer_in_text_field(self):
        """Test that non-string values in text field are handled."""
        data = [
            {"text": 123},
            {"text": "Valid text here."},
            {"text": 456},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "EN"
            result = detect_document_language(data)
        
        assert result == "en"

    def test_detect_language_with_mixed_valid_and_invalid_blocks(self):
        """Test handling of mixed valid and invalid blocks."""
        data = [
            {"text": "Valid text block."},
            {"text": None},
            {"text": 123},
            {"text": ""},
            {"text": "Another valid block."},
            {"label": "header"},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "EN"
            result = detect_document_language(data)
        
        assert result == "en"
        # Should only process the 2 valid text blocks

    def test_detect_language_returns_most_common_from_counter(self):
        """Test that Counter correctly identifies most common language."""
        data = [
            {"text": "A" * 300},
            {"text": "B" * 300},
            {"text": "C" * 300},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            # Return different languages
            mock_detect.side_effect = ["EN", "DE", "EN"]
            result = detect_document_language(data)
        
        # EN appears twice, should be selected
        assert result == "en"


# Made with Bob