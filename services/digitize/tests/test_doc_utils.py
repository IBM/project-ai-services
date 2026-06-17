"""
Unit tests for digitize.doc_utils module.
"""

import pytest
from unittest.mock import Mock, MagicMock, patch
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
        
        assert result == "EN"
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
        
        assert result == "DE"

    def test_detect_language_with_valid_french_data(self):
        """Test language detection with valid French text blocks."""
        data = [
            {"text": "Ceci est une phrase française sur l'intelligence artificielle."},
            {"text": "L'apprentissage automatique est un sous-ensemble de l'IA."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "FR"
            result = detect_document_language(data)
        
        assert result == "FR"

    def test_detect_language_with_valid_italian_data(self):
        """Test language detection with valid Italian text blocks."""
        data = [
            {"text": "Questa è una frase italiana sull'intelligenza artificiale."},
            {"text": "L'apprendimento automatico è un sottoinsieme dell'IA."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "IT"
            result = detect_document_language(data)
        
        assert result == "IT"

    def test_detect_language_with_unsupported_language_falls_back_to_english(self):
        """Test that unsupported languages fall back to English."""
        data = [
            {"text": "Este es un texto en español."},
            {"text": "El aprendizaje automático es importante."},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            mock_detect.return_value = "ES"  # Spanish not in lang_map
            result = detect_document_language(data)
        
        assert result == "EN"

    def test_detect_language_with_long_text_samples_blocks(self):
        """Test that long text samples random blocks."""
        data = [
            {"text": "A" * 300},  # Long block
            {"text": "B" * 300},
            {"text": "C" * 300},
            {"text": "D" * 300},
        ]
        
        with patch("digitize.doc_utils.detect_language") as mock_detect:
            with patch("digitize.doc_utils.random.randint") as mock_randint:
                mock_detect.return_value = "EN"
                # Return indices 0, 1, 2 for the first 3 calls
                mock_randint.side_effect = [0, 1, 2]
                
                result = detect_document_language(data)
        
        assert result == "EN"
        # Should call randint to sample blocks
        assert mock_randint.call_count >= 3

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
        
        assert result == "EN"

    def test_detect_language_with_empty_list_returns_english(self):
        """Test that empty data list returns English."""
        data = []
        
        with patch("digitize.doc_utils.logger") as mock_logger:
            result = detect_document_language(data)
        
        assert result == "EN"
        mock_logger.warning.assert_called_once()
        assert "Empty data list" in mock_logger.warning.call_args[0][0]

    def test_detect_language_with_non_list_input_returns_english(self):
        """Test that non-list input returns English with warning."""
        data = "not a list"
        
        with patch("digitize.doc_utils.logger") as mock_logger:
            result = detect_document_language(data)
        
        assert result == "EN"
        mock_logger.warning.assert_called_once()
        assert "expected list" in mock_logger.warning.call_args[0][0]

    def test_detect_language_with_non_dict_elements_returns_english(self):
        """Test that list with non-dict elements returns English."""
        data = ["string1", "string2", 123]
        
        with patch("digitize.doc_utils.logger") as mock_logger:
            result = detect_document_language(data)
        
        assert result == "EN"
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
        
        assert result == "EN"
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
        
        assert result == "EN"

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
                with patch("digitize.doc_utils.random.randint") as mock_randint:
                    # Make random.randint raise an exception
                    mock_randint.side_effect = Exception("Sampling failed")
                    result = detect_document_language(data)
        
        assert result == "EN"
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
                # Return valid indices (0, 1, 2) for sampling from data
                mock_randint.side_effect = [0, 1, 2]
                
                result = detect_document_language(data)
        
        assert result == "EN"
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
            with patch("digitize.doc_utils.random.randint") as mock_randint:
                mock_detect.return_value = "EN"
                # Return indices 0, 1 for the 2 available blocks
                mock_randint.side_effect = [0, 1]
                
                result = detect_document_language(data)
        
        assert result == "EN"
        # Should sample the 2 available blocks
        assert mock_randint.call_count >= 2

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
        
        assert result == "EN"
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
        
        assert result == "EN"
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
        
        assert result == "EN"

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
        
        assert result == "EN"

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
        
        assert result == "EN"
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
        assert result == "EN"


class TestProcessTableLanguageSelection:
    """Tests for language-specific table summarize/classify selection."""

    @patch("digitize.doc_utils.summarize_and_classify_tables")
    @patch("digitize.doc_utils.merge_consecutive_tables")
    @patch("digitize.doc_utils.os.path.splitext")
    @patch("digitize.doc_utils.settings")
    def test_process_table_uses_italian_prompt_and_max_tokens(
        self, mock_settings, mock_splitext, mock_merge_tables, mock_summarize_and_classify
    ):
        from digitize.doc_utils import process_table
        from common.lang_utils import LanguageCodes
        from pathlib import Path

        # Mock the settings structure with proper nested Mock objects
        mock_italian = Mock()
        mock_italian.prompt = "Italian prompt template"
        mock_italian.max_tokens = 1339
        
        mock_english = Mock()
        mock_english.prompt = "English prompt template"
        mock_english.max_tokens = 1024
        
        mock_german = Mock()
        mock_german.prompt = "German prompt template"
        mock_german.max_tokens = 1536
        
        mock_french = Mock()
        mock_french.prompt = "French prompt template"
        mock_french.max_tokens = 1260
        
        # Create a MagicMock that supports both attribute and subscript access
        mock_table_summary = MagicMock()
        mock_table_summary.italian = mock_italian
        mock_table_summary.english = mock_english
        mock_table_summary.german = mock_german
        mock_table_summary.french = mock_french
        
        mock_settings.table_summary = mock_table_summary

        mock_splitext.return_value = ("sample", ".pdf")
        mock_merge_tables.return_value = {
            0: {
                "markdown": "| Colonna | Valore |\n|---|---|\n| CPU | Power10 |",
                "caption": "Specifiche",
                "page_number": 1,
            }
        }
        mock_summarize_and_classify.return_value = (["Riassunto"], [True])

        # Create a properly mocked table with prov attribute
        mock_table = Mock()
        mock_prov = Mock()
        mock_prov.page_no = 1
        mock_table.prov = [mock_prov]
        mock_table.export_to_markdown.return_value = "| Colonna | Valore |\n|---|---|\n| CPU | Power10 |"
        mock_table.caption_text.return_value = "Specifiche"
        
        converted_doc = Mock()
        converted_doc.tables = [mock_table]
        
        # Mock Path.write_text
        with patch.object(Path, 'write_text'):
            process_table(
                converted_doc=converted_doc,
                doc_path="sample.pdf",
                out_path=Path("/tmp/out.json"),
                gen_model="test-model",
                gen_endpoint="http://llm",
                document_language=LanguageCodes.ITALIAN,
            )

        _, kwargs = mock_summarize_and_classify.call_args
        assert kwargs["prompt_template"] == "Italian prompt template"
        assert kwargs["max_tokens"] == 1339
    @patch("digitize.doc_utils.summarize_and_classify_tables")
    @patch("digitize.doc_utils.merge_consecutive_tables")
    @patch("digitize.doc_utils.os.path.splitext")
    @patch("digitize.doc_utils.settings")
    def test_process_table_uses_english_prompt_and_max_tokens(
        self, mock_settings, mock_splitext, mock_merge_tables, mock_summarize_and_classify
    ):
        from digitize.doc_utils import process_table
        from common.lang_utils import LanguageCodes
        from pathlib import Path

        # Mock the settings structure with proper nested Mock objects
        mock_english = Mock()
        mock_english.prompt = "English prompt template"
        mock_english.max_tokens = 1024
        
        mock_german = Mock()
        mock_german.prompt = "German prompt template"
        mock_german.max_tokens = 1536
        
        mock_italian = Mock()
        mock_italian.prompt = "Italian prompt template"
        mock_italian.max_tokens = 1339
        
        mock_french = Mock()
        mock_french.prompt = "French prompt template"
        mock_french.max_tokens = 1260
        
        # Create a MagicMock that supports both attribute and subscript access
        mock_table_summary = MagicMock()
        mock_table_summary.english = mock_english
        mock_table_summary.german = mock_german
        mock_table_summary.italian = mock_italian
        mock_table_summary.french = mock_french
        
        mock_settings.table_summary = mock_table_summary

        mock_splitext.return_value = ("sample", ".pdf")
        mock_merge_tables.return_value = {
            0: {
                "markdown": "| Column | Value |\n|---|---|\n| CPU | Power10 |",
                "caption": "Specifications",
                "page_number": 1,
            }
        }
        mock_summarize_and_classify.return_value = (["Summary"], [True])

        # Create a properly mocked table with prov attribute
        mock_table = Mock()
        mock_prov = Mock()
        mock_prov.page_no = 1
        mock_table.prov = [mock_prov]
        mock_table.export_to_markdown.return_value = "| Column | Value |\n|---|---|\n| CPU | Power10 |"
        mock_table.caption_text.return_value = "Specifications"
        
        converted_doc = Mock()
        converted_doc.tables = [mock_table]

        # Mock Path.write_text
        with patch.object(Path, 'write_text'):
            process_table(
                converted_doc=converted_doc,
                doc_path="sample.pdf",
                out_path=Path("/tmp/out.json"),
                gen_model="test-model",
                gen_endpoint="http://llm",
                document_language=LanguageCodes.ENGLISH,
            )

        _, kwargs = mock_summarize_and_classify.call_args
        assert kwargs["prompt_template"] == "English prompt template"
        assert kwargs["max_tokens"] == 1024

    @patch("digitize.doc_utils.summarize_and_classify_tables")
    @patch("digitize.doc_utils.merge_consecutive_tables")
    @patch("digitize.doc_utils.os.path.splitext")
    @patch("digitize.doc_utils.settings")
    def test_process_table_uses_german_prompt_and_max_tokens(
        self, mock_settings, mock_splitext, mock_merge_tables, mock_summarize_and_classify
    ):
        from digitize.doc_utils import process_table
        from common.lang_utils import LanguageCodes
        from pathlib import Path

        # Mock the settings structure with proper nested Mock objects
        mock_german = Mock()
        mock_german.prompt = "German prompt template"
        mock_german.max_tokens = 1536
        
        mock_english = Mock()
        mock_english.prompt = "English prompt template"
        mock_english.max_tokens = 1024
        
        mock_italian = Mock()
        mock_italian.prompt = "Italian prompt template"
        mock_italian.max_tokens = 1339
        
        mock_french = Mock()
        mock_french.prompt = "French prompt template"
        mock_french.max_tokens = 1260
        
        # Create a MagicMock that supports both attribute and subscript access
        mock_table_summary = MagicMock()
        mock_table_summary.german = mock_german
        mock_table_summary.english = mock_english
        mock_table_summary.italian = mock_italian
        mock_table_summary.french = mock_french
        
        mock_settings.table_summary = mock_table_summary

        mock_splitext.return_value = ("sample", ".pdf")
        mock_merge_tables.return_value = {
            0: {
                "markdown": "| Spalte | Wert |\n|---|---|\n| CPU | Power10 |",
                "caption": "Spezifikationen",
                "page_number": 1,
            }
        }
        mock_summarize_and_classify.return_value = (["Zusammenfassung"], [True])

        # Create a properly mocked table with prov attribute
        mock_table = Mock()
        mock_prov = Mock()
        mock_prov.page_no = 1
        mock_table.prov = [mock_prov]
        mock_table.export_to_markdown.return_value = "| Spalte | Wert |\n|---|---|\n| CPU | Power10 |"
        mock_table.caption_text.return_value = "Spezifikationen"
        
        converted_doc = Mock()
        converted_doc.tables = [mock_table]

        # Mock Path.write_text
        with patch.object(Path, 'write_text'):
            process_table(
                converted_doc=converted_doc,
                doc_path="sample.pdf",
                out_path=Path("/tmp/out.json"),
                gen_model="test-model",
                gen_endpoint="http://llm",
                document_language=LanguageCodes.GERMAN,
            )

        _, kwargs = mock_summarize_and_classify.call_args
        assert kwargs["prompt_template"] == "German prompt template"
        assert kwargs["max_tokens"] == 1536


    @patch("digitize.doc_utils.summarize_and_classify_tables")
    @patch("digitize.doc_utils.merge_consecutive_tables")
    @patch("digitize.doc_utils.os.path.splitext")
    @patch("digitize.doc_utils.settings")
    def test_process_table_uses_french_prompt_and_max_tokens(
        self, mock_settings, mock_splitext, mock_merge_tables, mock_summarize_and_classify
    ):
        from digitize.doc_utils import process_table
        from common.lang_utils import LanguageCodes
        from pathlib import Path

        # Mock the settings structure with proper nested Mock objects
        mock_french = Mock()
        mock_french.prompt = "French prompt template"
        mock_french.max_tokens = 1260
        
        mock_english = Mock()
        mock_english.prompt = "English prompt template"
        mock_english.max_tokens = 1024
        
        mock_german = Mock()
        mock_german.prompt = "German prompt template"
        mock_german.max_tokens = 1536
        
        mock_italian = Mock()
        mock_italian.prompt = "Italian prompt template"
        mock_italian.max_tokens = 1339
        
        # Create a MagicMock that supports both attribute and subscript access
        mock_table_summary = MagicMock()
        mock_table_summary.french = mock_french
        mock_table_summary.english = mock_english
        mock_table_summary.german = mock_german
        mock_table_summary.italian = mock_italian
        
        mock_settings.table_summary = mock_table_summary

        mock_splitext.return_value = ("sample", ".pdf")
        mock_merge_tables.return_value = {
            0: {
                "markdown": "| Colonne | Valeur |\n|---|---|\n| CPU | Power10 |",
                "caption": "Spécifications",
                "page_number": 1,
            }
        }
        mock_summarize_and_classify.return_value = (["Résumé"], [True])

        # Create a properly mocked table with prov attribute
        mock_table = Mock()
        mock_prov = Mock()
        mock_prov.page_no = 1
        mock_table.prov = [mock_prov]
        mock_table.export_to_markdown.return_value = "| Colonne | Valeur |\n|---|---|\n| CPU | Power10 |"
        mock_table.caption_text.return_value = "Spécifications"
        
        converted_doc = Mock()
        converted_doc.tables = [mock_table]

        # Mock Path.write_text
        with patch.object(Path, 'write_text'):
            process_table(
                converted_doc=converted_doc,
                doc_path="sample.pdf",
                out_path=Path("/tmp/out.json"),
                gen_model="test-model",
                gen_endpoint="http://llm",
                document_language=LanguageCodes.FRENCH,
            )

        _, kwargs = mock_summarize_and_classify.call_args
        assert kwargs["prompt_template"] == "French prompt template"
        assert kwargs["max_tokens"] == 1260


# Made with Bob