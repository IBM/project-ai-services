"""
Unit tests for HTML text processing in digitize.processing.text.
"""

import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

import pytest

from digitize.processing.text import (
    _build_structured_text,
    process_text_docx,
    process_text_html,
)


def _make_text_obj(label: str, text: str, level: int | None = None):
    obj = SimpleNamespace(label=label, text=text)
    if level is not None:
        obj.level = level
    return obj


@pytest.mark.unit
class TestProcessTextHtml:
    """Tests for process_text_html function."""

    def test_process_text_html_headings_mapping(self, tmp_path):
        """Test that HTML title and section headers are mapped with markdown hashes."""
        texts = [
            _make_text_obj("title", "Main Page Title"),
            _make_text_obj("text", "Some introductory paragraph."),
            _make_text_obj("section_header", "Section 1", level=1),
            _make_text_obj("text", "Paragraph under section 1."),
            _make_text_obj("section_header", "Subsection 1.1", level=2),
            _make_text_obj("section_header", "Deep Section", level=5),
        ]
        doc = SimpleNamespace(texts=texts)
        out_path = tmp_path / "out.json"

        page_count, process_time = process_text_html(doc, "sample.html", out_path)

        assert page_count >= 1
        assert process_time >= 0.0
        assert out_path.exists()

        records = json.loads(out_path.read_text(encoding="utf-8"))
        assert len(records) == 6

        assert records[0] == {
            "label": "section_header",
            "text": "# Main Page Title",
            "page": None,
            "font_size": None,
        }
        assert records[1] == {
            "label": "text",
            "text": "Some introductory paragraph.",
            "page": None,
            "font_size": None,
        }
        assert records[2] == {
            "label": "section_header",
            "text": "## Section 1",
            "page": None,
            "font_size": None,
        }
        assert records[4] == {
            "label": "section_header",
            "text": "### Subsection 1.1",
            "page": None,
            "font_size": None,
        }
        assert records[5] == {
            "label": "section_header",
            "text": "###### Deep Section",
            "page": None,
            "font_size": None,
        }

    def test_process_text_html_excludes_labels(self, tmp_path):
        """Test that caption, page_header, page_footer, footnote, and reference are excluded."""
        texts = [
            _make_text_obj("title", "Doc Title"),
            _make_text_obj("caption", "Figure 1: Diagram"),
            _make_text_obj("page_header", "Header Text"),
            _make_text_obj("page_footer", "Footer Text"),
            _make_text_obj("reference", "[1] Reference text"),
            _make_text_obj("footnote", "Footnote text"),
            _make_text_obj("text", "Body content"),
        ]
        doc = SimpleNamespace(texts=texts)
        out_path = tmp_path / "out.json"

        page_count, _ = process_text_html(doc, "sample.html", out_path)

        records = json.loads(out_path.read_text(encoding="utf-8"))
        assert len(records) == 2
        assert records[0]["text"] == "# Doc Title"
        assert records[1]["text"] == "Body content"

    def test_process_text_html_empty_doc(self, tmp_path):
        """Test that empty texts list writes empty JSON and returns page_count >= 1."""
        doc = SimpleNamespace(texts=[])
        out_path = tmp_path / "out.json"

        page_count, process_time = process_text_html(doc, "empty.html", out_path)

        assert page_count == 1
        assert process_time == 0.0
        assert json.loads(out_path.read_text(encoding="utf-8")) == []

    def test_process_text_html_page_count_estimation(self, tmp_path):
        """Test that page count estimation handles large text accurately (3000 chars per page)."""
        long_text = "A" * 7000  # ceil(7000 / 3000) = 3 pages
        doc = SimpleNamespace(texts=[_make_text_obj("text", long_text)])
        out_path = tmp_path / "out.json"

        page_count, _ = process_text_html(doc, "long.html", out_path)
        assert page_count == 3


@pytest.mark.unit
class TestBuildStructuredText:
    """Tests for _build_structured_text helper."""

    def test_build_structured_text_custom_resolver(self, tmp_path):
        """Test _build_structured_text with custom resolve_header function."""
        texts = [
            _make_text_obj("section_header", "Header One"),
            _make_text_obj("text", "Paragraph text"),
        ]
        doc = SimpleNamespace(texts=texts)
        out_path = tmp_path / "out.json"

        def custom_resolve(text_obj, last_level):
            return f">>> {text_obj.text}", last_level + 1

        page_count, process_time = _build_structured_text(
            doc, "doc.txt", out_path, custom_resolve, page_count=5
        )

        assert page_count == 5


@pytest.mark.unit
class TestProcessConvertedDocumentDispatch:
    """Tests for process_converted_document extension dispatching."""

    @patch("digitize.processing.orchestrator.process_table")
    @patch("digitize.processing.orchestrator.detect_document_language")
    @patch("digitize.processing.orchestrator.process_text_html")
    @patch("digitize.processing.orchestrator.process_text_docx")
    @patch("digitize.processing.orchestrator.process_text")
    @patch("digitize.processing.orchestrator.DoclingDocument.load_from_json")
    def test_dispatches_to_html_processor_for_html(
        self,
        mock_load,
        mock_process_text,
        mock_process_docx,
        mock_process_html,
        mock_detect_lang,
        mock_process_table,
        tmp_path,
    ):
        from digitize.processing.orchestrator import process_converted_document

        mock_load.return_value = Mock()
        mock_process_html.return_value = (2, 0.12)
        mock_detect_lang.return_value = "EN"
        mock_process_table.return_value = (0, 0.05, {})

        # Create dummy json file for text read
        text_json = tmp_path / "doc1_text.json"
        text_json.write_text("[]", encoding="utf-8")

        conv_json = tmp_path / "doc1.json"
        conv_json.write_text("{}", encoding="utf-8")

        res = process_converted_document(
            converted_json_path=str(conv_json),
            doc_path="page.html",
            out_path=str(tmp_path),
            gen_model="test-llm",
            gen_endpoint="http://llm",
            emb_endpoint="http://emb",
            max_tokens=2000,
            doc_id="doc1",
        )

        mock_process_html.assert_called_once()
        mock_process_docx.assert_not_called()
        mock_process_text.assert_not_called()
        assert res[2] == 2  # page_count

    @patch("digitize.processing.orchestrator.process_table")
    @patch("digitize.processing.orchestrator.detect_document_language")
    @patch("digitize.processing.orchestrator.process_text_html")
    @patch("digitize.processing.orchestrator.process_text_docx")
    @patch("digitize.processing.orchestrator.process_text")
    @patch("digitize.processing.orchestrator.DoclingDocument.load_from_json")
    def test_dispatches_to_docx_processor_for_docx(
        self,
        mock_load,
        mock_process_text,
        mock_process_docx,
        mock_process_html,
        mock_detect_lang,
        mock_process_table,
        tmp_path,
    ):
        from digitize.processing.orchestrator import process_converted_document

        mock_load.return_value = Mock()
        mock_process_docx.return_value = (3, 0.15)
        mock_detect_lang.return_value = "EN"
        mock_process_table.return_value = (0, 0.05, {})

        text_json = tmp_path / "doc2_text.json"
        text_json.write_text("[]", encoding="utf-8")

        conv_json = tmp_path / "doc2.json"
        conv_json.write_text("{}", encoding="utf-8")

        res = process_converted_document(
            converted_json_path=str(conv_json),
            doc_path="page.docx",
            out_path=str(tmp_path),
            gen_model="test-llm",
            gen_endpoint="http://llm",
            emb_endpoint="http://emb",
            max_tokens=2000,
            doc_id="doc2",
        )

        mock_process_docx.assert_called_once()
        mock_process_html.assert_not_called()
        mock_process_text.assert_not_called()
        assert res[2] == 3

    @patch("digitize.processing.orchestrator.process_table")
    @patch("digitize.processing.orchestrator.detect_document_language")
    @patch("digitize.processing.orchestrator.process_text_html")
    @patch("digitize.processing.orchestrator.process_text_docx")
    @patch("digitize.processing.orchestrator.process_text")
    @patch("digitize.processing.orchestrator.DoclingDocument.load_from_json")
    def test_dispatches_to_pdf_processor_for_pdf(
        self,
        mock_load,
        mock_process_text,
        mock_process_docx,
        mock_process_html,
        mock_detect_lang,
        mock_process_table,
        tmp_path,
    ):
        from digitize.processing.orchestrator import process_converted_document

        mock_load.return_value = Mock()
        mock_process_text.return_value = (10, 0.25)
        mock_detect_lang.return_value = "EN"
        mock_process_table.return_value = (0, 0.05, {})

        text_json = tmp_path / "doc3_text.json"
        text_json.write_text("[]", encoding="utf-8")

        conv_json = tmp_path / "doc3.json"
        conv_json.write_text("{}", encoding="utf-8")

        res = process_converted_document(
            converted_json_path=str(conv_json),
            doc_path="page.pdf",
            out_path=str(tmp_path),
            gen_model="test-llm",
            gen_endpoint="http://llm",
            emb_endpoint="http://emb",
            max_tokens=2000,
            doc_id="doc3",
        )

        mock_process_text.assert_called_once()
        mock_process_html.assert_not_called()
        mock_process_docx.assert_not_called()
        assert res[2] == 10
