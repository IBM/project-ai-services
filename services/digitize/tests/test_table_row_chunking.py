"""
Unit tests for table row-group chunking and the keyword-enriched table summary prompts.

Covers:
- split_markdown_table_into_row_chunks (header/caption repetition, row integrity, edge cases)
- process_table integration (chunks are what gets summarized, caption/page inherited)
- Table summary prompts contain the keyword-preservation rules in every language
"""

import json
from pathlib import Path
from unittest.mock import MagicMock, Mock, patch

import pytest

from digitize.processing.tables import split_markdown_table_into_row_chunks

HEADER = "| Corridor | Metric | FY21 | FY22 |"
SEPARATOR = "|---|---|---|---|"


def _make_table(n_rows: int) -> str:
    rows = [f"| Corridor{i} | Actual | {i}.1 | {i}.2 |" for i in range(n_rows)]
    return "\n".join([HEADER, SEPARATOR, *rows])


def _word_count(text: str) -> int:
    return len(text.split())


@pytest.mark.unit
class TestSplitMarkdownTableIntoRowChunks:
    def test_empty_table_returns_no_chunks(self):
        assert split_markdown_table_into_row_chunks("") == []
        assert split_markdown_table_into_row_chunks("   \n  ") == []

    def test_small_table_returned_as_single_chunk(self):
        table = _make_table(2)
        chunks = split_markdown_table_into_row_chunks(table, "", max_tokens=1000)
        assert chunks == [table]

    def test_small_table_gets_caption_prepended(self):
        table = _make_table(2)
        chunks = split_markdown_table_into_row_chunks(table, "Table 1: Ridership", max_tokens=1000)
        assert len(chunks) == 1
        assert chunks[0].startswith("Table 1: Ridership\n" + HEADER)

    def test_large_table_is_split_into_multiple_chunks(self):
        chunks = split_markdown_table_into_row_chunks(
            _make_table(30), "Table 1", max_tokens=40, count_tokens_fn=_word_count
        )
        assert len(chunks) > 1

    def test_every_chunk_repeats_caption_header_and_separator(self):
        chunks = split_markdown_table_into_row_chunks(
            _make_table(30), "Table 1: Ridership", max_tokens=40, count_tokens_fn=_word_count
        )
        for chunk in chunks:
            lines = chunk.split("\n")
            assert lines[0] == "Table 1: Ridership"
            assert lines[1] == HEADER
            assert lines[2] == SEPARATOR
            assert len(lines) > 3  # at least one data row

    def test_all_rows_preserved_in_order_without_duplicates(self):
        n_rows = 30
        chunks = split_markdown_table_into_row_chunks(
            _make_table(n_rows), "Table 1", max_tokens=40, count_tokens_fn=_word_count
        )
        rows = [
            line for chunk in chunks for line in chunk.split("\n")[3:]
        ]
        assert rows == [f"| Corridor{i} | Actual | {i}.1 | {i}.2 |" for i in range(n_rows)]

    def test_chunks_respect_max_tokens(self):
        max_tokens = 40
        chunks = split_markdown_table_into_row_chunks(
            _make_table(30), "Table 1", max_tokens=max_tokens, count_tokens_fn=_word_count
        )
        for chunk in chunks:
            # joined with newlines, so word count equals the sum of line word counts
            assert _word_count(chunk) <= max_tokens

    def test_caption_already_in_table_is_not_duplicated(self):
        table = "Table 1: Ridership\n" + _make_table(30)
        chunks = split_markdown_table_into_row_chunks(
            table, "Table 1: Ridership", max_tokens=40, count_tokens_fn=_word_count
        )
        assert len(chunks) > 1
        for chunk in chunks:
            assert chunk.lower().count("table 1: ridership") == 1

    def test_oversized_single_row_gets_its_own_chunk_and_warns(self):
        long_row = "| " + " ".join(f"word{i}" for i in range(50)) + " | x | y | z |"
        table = "\n".join([HEADER, SEPARATOR, "| a | b | 1 | 2 |", long_row, "| c | d | 3 | 4 |"])
        # The project logger has propagate=False, so patch it rather than using caplog.
        with patch("digitize.processing.tables.logger") as mock_logger:
            chunks = split_markdown_table_into_row_chunks(
                table, "", max_tokens=30, count_tokens_fn=_word_count
            )
        assert any(long_row in chunk for chunk in chunks)
        assert sum(chunk.count(long_row) for chunk in chunks) == 1
        warnings = [c.args[0] for c in mock_logger.warning.call_args_list]
        assert any("exceeds max_tokens" in msg for msg in warnings)

    def test_table_without_separator_is_split_without_header(self):
        table = "\n".join(f"| r{i} | v{i} |" for i in range(20))
        chunks = split_markdown_table_into_row_chunks(
            table, "", max_tokens=10, count_tokens_fn=_word_count
        )
        assert len(chunks) > 1
        assert "\n".join(chunks) == table

    def test_alignment_padding_is_stripped(self):
        """Docling/tabulate pads cells to column width; padding must not count as tokens."""
        padded = "\n".join([
            "| Corridor        | Metric   | FY21   | FY22   |",
            "|-----------------|----------|--------|--------|",
            "| Northeast       | Actual   | 10.1   | 12.4   |",
        ])
        chunks = split_markdown_table_into_row_chunks(padded, "", max_tokens=1000)
        assert chunks == [
            "\n".join([
                "| Corridor | Metric | FY21 | FY22 |",
                "|---|---|---|---|",
                "| Northeast | Actual | 10.1 | 12.4 |",
            ])
        ]

    def test_padded_table_packs_multiple_rows_per_chunk(self):
        """Regression: padding made the header exceed the budget, giving one row per chunk."""
        pad = " " * 40
        rows = [f"| Row{i}{pad} | Actual{pad} | {i}.1{pad} | {i}.2{pad} |" for i in range(20)]
        padded = "\n".join([
            f"| Corridor{pad} | Metric{pad} | FY21{pad} | FY22{pad} |",
            f"|{'-' * 50}|{'-' * 50}|{'-' * 50}|{'-' * 50}|",
            *rows,
        ])
        chunks = split_markdown_table_into_row_chunks(padded, "", max_tokens=200)
        assert len(chunks) < 20  # rows are grouped, not one per chunk
        assert any(len(chunk.split("\n")) > 3 for chunk in chunks)

    def test_default_token_estimate_splits_large_table(self):
        chunks = split_markdown_table_into_row_chunks(_make_table(200), "Table 1", max_tokens=200)
        assert len(chunks) > 1
        for chunk in chunks:
            assert chunk.split("\n")[1] == HEADER

    def test_emits_info_logs(self):
        with patch("digitize.processing.tables.logger") as mock_logger:
            split_markdown_table_into_row_chunks(
                _make_table(30), "Table 1", max_tokens=40, count_tokens_fn=_word_count
            )
        messages = " ".join(c.args[0] for c in mock_logger.info.call_args_list)
        assert "Row chunking" in messages
        assert "chunks" in messages


def _mock_table(markdown: str, caption: str, page: int):
    table = Mock()
    prov = Mock()
    prov.page_no = page
    table.prov = [prov]
    table.export_to_markdown.return_value = markdown
    table.caption_text.return_value = caption
    return table


@pytest.mark.unit
class TestProcessTableRowChunking:
    @patch("digitize.processing.tables.summarize_and_classify_tables")
    @patch("digitize.processing.tables.settings")
    def test_large_table_is_summarized_as_row_chunks(self, mock_settings, mock_summarize):
        from common.lang_utils import LanguageCodes

        english = Mock()
        english.prompt = "English prompt {content}"
        english.max_tokens = 1024
        table_summary = MagicMock()
        table_summary.english = english
        mock_settings.table_summary = table_summary

        def fake_summarize(markdowns, *args, **kwargs):
            return [f"summary {i}" for i in range(len(markdowns))], [True] * len(markdowns), {}

        mock_summarize.side_effect = fake_summarize

        converted_doc = Mock()
        converted_doc.tables = [_mock_table(_make_table(30), "Table 1: Ridership", 3)]

        with patch.object(Path, "write_text") as mock_write:
            table_count, _, failures = _run_process_table(converted_doc, LanguageCodes.ENGLISH, 60)

        markdowns = mock_summarize.call_args[0][0]
        assert len(markdowns) > 1
        for md in markdowns:
            assert md.split("\n")[0] == "Table 1: Ridership"
            assert HEADER in md
        assert table_count == len(markdowns)
        assert failures == {}

        saved = mock_write.call_args[0][0]
        assert '"caption": "Table 1: Ridership"' in saved
        assert '"page_number": 3' in saved

    @patch("digitize.processing.tables.summarize_and_classify_tables")
    @patch("digitize.processing.tables.settings")
    def test_discarded_chunk_is_kept_when_sibling_chunk_is_kept(self, mock_settings, mock_summarize):
        """A real summary of a chunk is rescued if another chunk of the same table was kept."""
        from common.lang_utils import LanguageCodes

        english = Mock()
        english.prompt = "English prompt {content}"
        english.max_tokens = 1024
        table_summary = MagicMock()
        table_summary.english = english
        mock_settings.table_summary = table_summary

        def fake_summarize(markdowns, *args, **kwargs):
            n = len(markdowns)
            assert n >= 3
            summaries = [f"summary {i}" for i in range(n)]
            decisions = [True] * n
            decisions[1] = False                # real summary, LLM said no -> rescued
            summaries[2] = "No summary."        # failed parse, LLM said no -> stays dropped
            decisions[2] = False
            return summaries, decisions, {}

        mock_summarize.side_effect = fake_summarize

        converted_doc = Mock()
        converted_doc.tables = [_mock_table(_make_table(30), "Table 1", 3)]

        with patch.object(Path, "write_text") as mock_write:
            table_count, _, _ = _run_process_table(converted_doc, LanguageCodes.ENGLISH, 60)

        n_chunks = len(mock_summarize.call_args[0][0])
        assert table_count == n_chunks - 1  # only the "No summary." chunk is dropped
        saved = mock_write.call_args[0][0]
        assert '"summary": "summary 1"' in saved
        assert '"summary": "summary 2"' not in saved

    @patch("digitize.processing.tables.summarize_and_classify_tables")
    @patch("digitize.processing.tables.settings")
    def test_discarded_table_with_no_kept_chunks_stays_discarded(self, mock_settings, mock_summarize):
        from common.lang_utils import LanguageCodes

        english = Mock()
        english.prompt = "English prompt {content}"
        english.max_tokens = 1024
        table_summary = MagicMock()
        table_summary.english = english
        mock_settings.table_summary = table_summary
        mock_summarize.side_effect = lambda markdowns, *a, **k: (
            ["irrelevant"] * len(markdowns), [False] * len(markdowns), {}
        )

        converted_doc = Mock()
        converted_doc.tables = [_mock_table(_make_table(30), "Table 1", 3)]

        with patch.object(Path, "write_text"):
            table_count, _, _ = _run_process_table(converted_doc, LanguageCodes.ENGLISH, 60)

        assert table_count == 0

    @patch("digitize.processing.tables.summarize_and_classify_tables")
    @patch("digitize.processing.tables.settings")
    def test_failed_chunk_falls_back_to_raw_markdown_and_is_logged(self, mock_settings, mock_summarize):
        """A chunk whose LLM call failed is kept with its own markdown as the summary, and logged."""
        from common.lang_utils import LanguageCodes

        english = Mock()
        english.prompt = "English prompt {content}"
        english.max_tokens = 1024
        table_summary = MagicMock()
        table_summary.english = english
        mock_settings.table_summary = table_summary

        def fake_summarize(markdowns, *args, **kwargs):
            n = len(markdowns)
            assert n >= 3
            summaries = [f"summary {i}" for i in range(n)]
            decisions = [True] * n
            summaries[1], decisions[1] = "No summary.", False
            return summaries, decisions, {1: "Failed to process table 1: Invalid table summary response"}

        mock_summarize.side_effect = fake_summarize

        converted_doc = Mock()
        converted_doc.tables = [_mock_table(_make_table(30), "Table 1", 3)]

        with patch.object(Path, "write_text") as mock_write, \
                patch("digitize.processing.tables.logger") as mock_logger:
            table_count, _, failures = _run_process_table(converted_doc, LanguageCodes.ENGLISH, 60)

        markdowns = mock_summarize.call_args[0][0]
        # no chunk is lost, and the failure is still reported to the caller
        assert table_count == len(markdowns)
        assert list(failures) == [1]

        saved = json.loads(mock_write.call_args[0][0])
        assert saved["1"]["summary"] == markdowns[1]       # raw markdown stored in the summary column
        assert HEADER in saved["1"]["summary"]             # header row is repeated in the chunk
        assert saved["1"]["summary"] != "No summary."
        assert saved["0"]["summary"] == "summary 0"        # successful chunks keep their LLM summary

        infos = [c.args[0] for c in mock_logger.info.call_args_list]
        assert any("Table chunk 2/" in m and "using its raw markdown" in m for m in infos)
        assert any("Raw-markdown fallback applied to 1 table chunk(s)" in m and "[2]" in m for m in infos)
        assert any("dropped chunk numbers: []" in m for m in infos)

    @patch("digitize.processing.tables.summarize_and_classify_tables")
    @patch("digitize.processing.tables.settings")
    def test_small_table_stays_single_chunk(self, mock_settings, mock_summarize):
        from common.lang_utils import LanguageCodes

        english = Mock()
        english.prompt = "English prompt {content}"
        english.max_tokens = 1024
        table_summary = MagicMock()
        table_summary.english = english
        mock_settings.table_summary = table_summary
        mock_summarize.return_value = (["summary"], [True], {})

        converted_doc = Mock()
        converted_doc.tables = [_mock_table(_make_table(2), "Table 1", 1)]

        with patch.object(Path, "write_text"):
            table_count, _, _ = _run_process_table(converted_doc, LanguageCodes.ENGLISH, 1000)

        assert len(mock_summarize.call_args[0][0]) == 1
        assert table_count == 1


def _run_process_table(converted_doc, language, chunk_max_tokens):
    from digitize.processing.tables import process_table

    return process_table(
        converted_doc=converted_doc,
        doc_path="charts.pdf",
        out_path=Path("/tmp/out.json"),
        gen_model="test-model",
        gen_endpoint="http://llm",
        document_language=language,
        table_chunk_max_tokens=chunk_max_tokens,
    )


@pytest.mark.unit
class TestEnrichedTableSummaryPrompts:
    @pytest.mark.parametrize("language", ["english", "german", "italian", "french", "japanese"])
    def test_prompt_keeps_content_placeholder(self, language):
        from digitize.settings import settings

        prompt = getattr(settings.table_summary, language).prompt
        assert "{content}" in prompt
        # must still be usable with str.format (no stray braces)
        assert "TABLE_BODY" in prompt.format(content="TABLE_BODY")

    @pytest.mark.parametrize(
        "language,marker",
        [
            ("english", "KEYWORD-SEARCHABLE SUMMARY RULES"),
            ("german", "STICHWORT-DURCHSUCHBARE"),
            ("italian", "RICERCABILE PER PAROLE CHIAVE"),
            ("french", "RECHERCHABLE PAR MOTS-CLÉS"),
            ("japanese", "キーワード検索可能な要約のルール"),
        ],
    )
    def test_prompt_contains_keyword_rules(self, language, marker):
        from digitize.settings import settings

        assert marker in getattr(settings.table_summary, language).prompt

    def test_english_prompt_requires_row_label_and_column_header_per_value(self):
        from digitize.settings import settings

        prompt = settings.table_summary.english.prompt
        assert "row label" in prompt
        assert "column header" in prompt
        assert "FY22" in prompt  # period keyword guidance / example

    def test_prompts_request_json_output_with_schema_keys(self):
        """Prompts must ask for the JSON keys enforced by TABLE_SUMMARY_SCHEMA and render cleanly."""
        from digitize.settings import settings

        for language in ["english", "german", "italian", "french", "japanese"]:
            prompt = getattr(settings.table_summary, language).prompt
            rendered = prompt.format(content="| A | B |")
            assert '{"summary":' in rendered, language
            assert '"decision":' in rendered, language
            assert "| A | B |" in rendered, language
            # the old line-label output format must be gone
            assert "\nSummary:" not in rendered, language
            assert "\nDecision:" not in rendered, language

    def test_table_chunk_max_tokens_default(self):
        from digitize.settings import settings

        assert settings.digitize.table_chunk_max_tokens == 200
