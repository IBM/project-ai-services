"""
Unit tests for digitize.docx_utils module.

Tests cover DOCX-specific functionality including:
- Page count estimation
- TOC extraction
- Table caption recovery
- Reference parsing and resolution
"""

import pytest
from unittest.mock import Mock, MagicMock, patch, mock_open
from pathlib import Path

from digitize.parsing.docx import (
    _parse_ref_index,
    _get_body_children_refs,
    _get_text_value_by_ref,
    _looks_like_table_caption,
    _get_ref_value,
    _get_doc_item_by_ref,
    recover_table_caption_from_body_context,
    estimate_docx_page_count,
    get_docx_toc,
    extract_toc_level_from_style,
)


@pytest.mark.unit
class TestParseRefIndex:
    """Tests for _parse_ref_index function."""

    def test_parse_valid_text_ref(self):
        """Test parsing valid text reference."""
        result = _parse_ref_index("#/texts/123", "texts")
        assert result == 123

    def test_parse_valid_table_ref(self):
        """Test parsing valid table reference."""
        result = _parse_ref_index("#/tables/5", "tables")
        assert result == 5

    def test_parse_zero_index(self):
        """Test parsing reference with zero index."""
        result = _parse_ref_index("#/texts/0", "texts")
        assert result == 0

    def test_parse_invalid_prefix(self):
        """Test parsing with wrong prefix returns None."""
        result = _parse_ref_index("#/texts/123", "tables")
        assert result is None

    def test_parse_non_string_ref(self):
        """Test parsing non-string reference returns None."""
        result = _parse_ref_index(123, "texts")
        assert result is None

    def test_parse_malformed_ref(self):
        """Test parsing malformed reference returns None."""
        result = _parse_ref_index("#/texts/abc", "texts")
        assert result is None

    def test_parse_empty_ref(self):
        """Test parsing empty reference returns None."""
        result = _parse_ref_index("", "texts")
        assert result is None

    def test_parse_ref_without_hash(self):
        """Test parsing reference without hash prefix returns None."""
        result = _parse_ref_index("/texts/123", "texts")
        assert result is None


@pytest.mark.unit
class TestGetBodyChildrenRefs:
    """Tests for _get_body_children_refs function."""

    def test_get_refs_from_dict_children(self):
        """Test extracting refs from dict-style children."""
        mock_doc = Mock()
        mock_doc.body.children = [
            {"$ref": "#/texts/0"},
            {"$ref": "#/texts/1"},
            {"$ref": "#/tables/0"},
        ]
        
        result = _get_body_children_refs(mock_doc)
        
        assert result == ["#/texts/0", "#/texts/1", "#/tables/0"]

    def test_get_refs_from_object_children(self):
        """Test extracting refs from object-style children."""
        mock_doc = Mock()
        child1 = Mock()
        child1.ref = "#/texts/0"
        child2 = Mock()
        child2.ref = "#/texts/1"
        
        mock_doc.body.children = [child1, child2]
        
        result = _get_body_children_refs(mock_doc)
        
        assert result == ["#/texts/0", "#/texts/1"]

    def test_get_refs_with_no_children(self):
        """Test with no children returns empty list."""
        mock_doc = Mock()
        mock_doc.body.children = []
        
        result = _get_body_children_refs(mock_doc)
        
        assert result == []

    def test_get_refs_with_none_children(self):
        """Test with None children returns empty list."""
        mock_doc = Mock()
        mock_doc.body.children = None
        
        result = _get_body_children_refs(mock_doc)
        
        assert result == []

    def test_get_refs_with_exception(self):
        """Test exception handling returns empty list."""
        mock_doc = Mock()
        mock_doc.body.children = Mock(side_effect=Exception("Test error"))
        
        result = _get_body_children_refs(mock_doc)
        
        assert result == []


@pytest.mark.unit
class TestGetTextValueByRef:
    """Tests for _get_text_value_by_ref function."""

    def test_get_text_from_text_field(self):
        """Test getting text from text field."""
        mock_doc = Mock()
        mock_text_obj = Mock()
        mock_text_obj.text = "Sample text content"
        mock_text_obj.orig = None
        mock_doc.texts = [mock_text_obj]
        
        result = _get_text_value_by_ref(mock_doc, "#/texts/0")
        
        assert result == "Sample text content"

    def test_get_text_from_orig_field(self):
        """Test getting text from orig field when text is None."""
        mock_doc = Mock()
        mock_text_obj = Mock()
        mock_text_obj.text = None
        mock_text_obj.orig = "Original text content"
        mock_doc.texts = [mock_text_obj]
        
        result = _get_text_value_by_ref(mock_doc, "#/texts/0")
        
        assert result == "Original text content"

    def test_get_text_strips_whitespace(self):
        """Test that returned text is stripped."""
        mock_doc = Mock()
        mock_text_obj = Mock()
        mock_text_obj.text = "  Text with spaces  "
        mock_doc.texts = [mock_text_obj]
        
        result = _get_text_value_by_ref(mock_doc, "#/texts/0")
        
        assert result == "Text with spaces"

    def test_get_text_with_invalid_ref(self):
        """Test with invalid reference returns empty string."""
        mock_doc = Mock()
        
        result = _get_text_value_by_ref(mock_doc, "#/tables/0")
        
        assert result == ""

    def test_get_text_with_out_of_bounds_index(self):
        """Test with out of bounds index returns empty string."""
        mock_doc = Mock()
        mock_doc.texts = []
        
        result = _get_text_value_by_ref(mock_doc, "#/texts/0")
        
        assert result == ""


@pytest.mark.unit
class TestLooksLikeTableCaption:
    """Tests for _looks_like_table_caption function."""

    def test_recognizes_standard_table_caption(self):
        """Test recognizing standard table caption format."""
        assert _looks_like_table_caption("Table 1: Sample caption") is True
        assert _looks_like_table_caption("Table 2.1 - Another caption") is True
        assert _looks_like_table_caption("TABLE 3: Uppercase caption") is True

    def test_recognizes_table_caption_with_dash(self):
        """Test recognizing table caption with dash separator."""
        assert _looks_like_table_caption("Table 1-1 VIOS release schedule") is True

    def test_recognizes_table_caption_with_period(self):
        """Test recognizing table caption with period separator."""
        assert _looks_like_table_caption("Table 2.3. Configuration options") is True

    def test_rejects_non_table_text(self):
        """Test rejecting non-table caption text."""
        assert _looks_like_table_caption("This is regular text") is False
        assert _looks_like_table_caption("A paragraph about tables") is False

    def test_rejects_empty_text(self):
        """Test rejecting empty text."""
        assert _looks_like_table_caption("") is False
        assert _looks_like_table_caption(None) is False

    def test_rejects_whitespace_only(self):
        """Test rejecting whitespace-only text."""
        assert _looks_like_table_caption("   ") is False
        assert _looks_like_table_caption("\n\t") is False


@pytest.mark.unit
class TestGetRefValue:
    """Tests for _get_ref_value function."""

    def test_get_ref_from_dict(self):
        """Test getting ref from dictionary."""
        ref_obj = {"$ref": "#/texts/0"}
        result = _get_ref_value(ref_obj)
        assert result == "#/texts/0"

    def test_get_ref_from_object_with_ref_attr(self):
        """Test getting ref from object with ref attribute."""
        ref_obj = Mock()
        ref_obj.ref = "#/texts/0"
        result = _get_ref_value(ref_obj)
        assert result == "#/texts/0"

    def test_get_ref_from_object_with_dollar_ref_attr(self):
        """Test getting ref from object with $ref attribute."""
        ref_obj = Mock()
        ref_obj.ref = None
        setattr(ref_obj, "$ref", "#/texts/0")
        result = _get_ref_value(ref_obj)
        assert result == "#/texts/0"

    def test_get_ref_returns_none_when_not_found(self):
        """Test returns None when no ref found."""
        ref_obj = Mock()
        ref_obj.ref = None
        setattr(ref_obj, "$ref", None)
        ref_obj.cref = None
        ref_obj.self_ref = None
        result = _get_ref_value(ref_obj)
        assert result is None


@pytest.mark.unit
class TestGetDocItemByRef:
    """Tests for _get_doc_item_by_ref function."""

    def test_get_text_item(self):
        """Test getting text item by reference."""
        mock_doc = Mock()
        mock_text = Mock()
        mock_doc.texts = [mock_text]
        
        result = _get_doc_item_by_ref(mock_doc, "#/texts/0")
        
        assert result == mock_text

    def test_get_table_item(self):
        """Test getting table item by reference."""
        mock_doc = Mock()
        mock_table = Mock()
        mock_doc.tables = [mock_table]
        
        result = _get_doc_item_by_ref(mock_doc, "#/tables/0")
        
        assert result == mock_table

    def test_get_item_with_invalid_ref(self):
        """Test with invalid reference returns None."""
        mock_doc = Mock()
        
        result = _get_doc_item_by_ref(mock_doc, "#/invalid/0")
        
        assert result is None


@pytest.mark.unit
class TestEstimateDocxPageCount:
    """Tests for estimate_docx_page_count function."""

    @patch('digitize.parsing.docx.Document')
    def test_estimate_with_paragraphs_only(self, mock_document_class):
        """Test page estimation with only paragraphs."""
        mock_doc = Mock()
        mock_para1 = Mock()
        mock_para1.text = "word " * 450  # Exactly 1 page worth
        mock_para2 = Mock()
        mock_para2.text = "word " * 225  # Half page
        mock_doc.paragraphs = [mock_para1, mock_para2]
        mock_doc.tables = []
        mock_document_class.return_value = mock_doc
        
        result = estimate_docx_page_count("test.docx")
        
        # 675 words / 450 words per page = 1.5, rounds to 2 pages
        assert result == 2

    @patch('digitize.parsing.docx.Document')
    def test_estimate_with_tables(self, mock_document_class):
        """Test page estimation including table content."""
        mock_doc = Mock()
        mock_doc.paragraphs = []
        
        # Create mock table with cells
        mock_cell = Mock()
        mock_cell.text = "word " * 100
        mock_row = Mock()
        mock_row.cells = [mock_cell, mock_cell]
        mock_table = Mock()
        mock_table.rows = [mock_row]
        mock_doc.tables = [mock_table]
        
        mock_document_class.return_value = mock_doc
        
        result = estimate_docx_page_count("test.docx")
        
        # 200 words / 450 = 0.44, rounds to 1 page (minimum)
        assert result >= 1

    @patch('digitize.parsing.docx.Document')
    def test_estimate_returns_minimum_one_page(self, mock_document_class):
        """Test that estimation returns at least 1 page."""
        mock_doc = Mock()
        mock_doc.paragraphs = []
        mock_doc.tables = []
        mock_document_class.return_value = mock_doc
        
        result = estimate_docx_page_count("test.docx")
        
        assert result == 1

    @patch('digitize.parsing.docx.Document')
    def test_estimate_handles_exception(self, mock_document_class):
        """Test that exceptions return 1 page."""
        mock_document_class.side_effect = Exception("File not found")
        
        result = estimate_docx_page_count("nonexistent.docx")
        
        assert result == 1


@pytest.mark.unit
class TestRecoverTableCaptionFromBodyContext:
    """Tests for recover_table_caption_from_body_context function."""

    def test_recover_caption_from_body_level(self):
        """Test recovering caption from body-level context."""
        mock_doc = Mock()
        
        # Setup body children refs
        with patch('digitize.parsing.docx._get_body_children_refs') as mock_get_body:
            with patch('digitize.parsing.docx._find_matching_caption_near_refs') as mock_find:
                mock_get_body.return_value = ["#/texts/0", "#/tables/0", "#/texts/1"]
                mock_find.return_value = "Table 1: Test Caption"
                
                result = recover_table_caption_from_body_context(mock_doc, 0)
                
                assert result == "Table 1: Test Caption"
                mock_find.assert_called_once()

    def test_recover_caption_from_parent_level(self):
        """Test recovering caption from parent-level context."""
        mock_doc = Mock()
        
        with patch('digitize.parsing.docx._get_body_children_refs') as mock_get_body:
            with patch('digitize.parsing.docx._find_matching_caption_near_refs') as mock_find:
                with patch('digitize.parsing.docx._get_parent_ref_for_table') as mock_get_parent:
                    with patch('digitize.parsing.docx._get_doc_item_by_ref') as mock_get_item:
                        with patch('digitize.parsing.docx._get_child_refs') as mock_get_children:
                            mock_get_body.return_value = ["#/texts/0", "#/tables/0"]
                            mock_find.side_effect = ["", "Table 2: Parent Caption"]  # First call fails, second succeeds
                            mock_get_parent.return_value = "#/groups/0"
                            mock_get_item.return_value = Mock()
                            mock_get_children.return_value = ["#/texts/1", "#/tables/0"]
                            
                            result = recover_table_caption_from_body_context(mock_doc, 0)
                            
                            assert result == "Table 2: Parent Caption"

    def test_recover_caption_from_section_header(self):
        """Test recovering caption from enclosing section header."""
        mock_doc = Mock()
        
        with patch('digitize.parsing.docx._get_body_children_refs') as mock_get_body:
            with patch('digitize.parsing.docx._find_matching_caption_near_refs') as mock_find:
                with patch('digitize.parsing.docx._get_parent_ref_for_table') as mock_get_parent:
                    with patch('digitize.parsing.docx._get_enclosing_section_header_for_table') as mock_get_header:
                        mock_get_body.return_value = ["#/tables/0"]
                        mock_find.return_value = ""
                        mock_get_parent.return_value = ""
                        mock_get_header.return_value = "Section Header Text"
                        
                        result = recover_table_caption_from_body_context(mock_doc, 0)
                        
                        assert result == "Section Header Text"

    def test_recover_caption_returns_empty_when_none_found(self):
        """Test returns empty string when no caption found."""
        mock_doc = Mock()
        
        with patch('digitize.parsing.docx._get_body_children_refs') as mock_get_body:
            with patch('digitize.parsing.docx._find_matching_caption_near_refs') as mock_find:
                with patch('digitize.parsing.docx._get_parent_ref_for_table') as mock_get_parent:
                    with patch('digitize.parsing.docx._get_enclosing_section_header_for_table') as mock_get_header:
                        mock_get_body.return_value = ["#/tables/0"]
                        mock_find.return_value = ""
                        mock_get_parent.return_value = ""
                        mock_get_header.return_value = ""
                        
                        result = recover_table_caption_from_body_context(mock_doc, 0)
                        
                        assert result == ""


@pytest.mark.unit
class TestGetDocxToc:
    """Tests for get_docx_toc function."""

    @patch('digitize.parsing.docx.extract_toc_combined')
    def test_get_toc_from_combined_extraction(self, mock_extract_combined):
        """Test getting TOC from combined extraction method."""
        mock_extract_combined.return_value = {
            "Chapter 1": 1,
            "Section 1.1": 2,
            "Chapter 2": 1,
        }
        
        result = get_docx_toc("test.docx")
        
        assert result == {"Chapter 1": 1, "Section 1.1": 2, "Chapter 2": 1}
        mock_extract_combined.assert_called_once_with("test.docx")

    @patch('digitize.parsing.docx.extract_toc_from_headings')
    @patch('digitize.parsing.docx.extract_toc_from_toc_styles')
    @patch('digitize.parsing.docx.extract_toc_combined')
    def test_get_toc_fallback_to_toc_styles(self, mock_combined, mock_toc_styles, mock_headings):
        """Test fallback to TOC styles when combined fails."""
        mock_combined.return_value = {}
        mock_toc_styles.return_value = {"Heading 1": 1}
        
        result = get_docx_toc("test.docx")
        
        assert result == {"Heading 1": 1}
        mock_toc_styles.assert_called_once()

    @patch('digitize.parsing.docx.extract_toc_from_headings')
    @patch('digitize.parsing.docx.extract_toc_from_toc_styles')
    @patch('digitize.parsing.docx.extract_toc_combined')
    def test_get_toc_fallback_to_headings(self, mock_combined, mock_toc_styles, mock_headings):
        """Test fallback to heading styles when other methods fail."""
        mock_combined.return_value = {}
        mock_toc_styles.return_value = {}
        mock_headings.return_value = {"Title": 1}
        
        result = get_docx_toc("test.docx")
        
        assert result == {"Title": 1}
        mock_headings.assert_called_once()

    @patch('digitize.parsing.docx.extract_toc_from_headings')
    @patch('digitize.parsing.docx.extract_toc_from_toc_styles')
    @patch('digitize.parsing.docx.extract_toc_combined')
    def test_get_toc_returns_empty_when_all_fail(self, mock_combined, mock_toc_styles, mock_headings):
        """Test returns empty dict when all methods fail."""
        mock_combined.return_value = {}
        mock_toc_styles.return_value = {}
        mock_headings.return_value = {}
        
        result = get_docx_toc("test.docx")
        
        assert result == {}

    @patch('digitize.parsing.docx.extract_toc_combined')
    def test_get_toc_handles_exception(self, mock_extract_combined):
        """Test exception handling returns empty dict."""
        mock_extract_combined.side_effect = Exception("File error")
        
        result = get_docx_toc("test.docx")
        
        assert result == {}


@pytest.mark.unit
class TestExtractTocLevelFromStyle:
    """Tests for extract_toc_level_from_style function."""

    def test_extract_level_from_toc_style(self):
        """Test extracting level from TOC style names."""
        assert extract_toc_level_from_style("TOC 1") == 1
        assert extract_toc_level_from_style("TOC 2") == 2
        assert extract_toc_level_from_style("TOC 3") == 3

    def test_extract_level_case_insensitive(self):
        """Test extraction is case insensitive."""
        assert extract_toc_level_from_style("toc 1") == 1
        assert extract_toc_level_from_style("Toc 2") == 2

    def test_extract_level_from_heading_style(self):
        """Test extracting level from Heading style names."""
        # Note: extract_toc_level_from_style only extracts from TOC styles, not Heading styles
        # Heading styles would return default level 1
        assert extract_toc_level_from_style("Heading 1") == 1
        assert extract_toc_level_from_style("Heading 2") == 1  # No 'toc' in name, returns default

    def test_extract_level_default_for_unknown(self):
        """Test default level for unknown styles."""
        assert extract_toc_level_from_style("Unknown Style") == 1
        assert extract_toc_level_from_style("TOC Heading") == 1


# Made with Bob