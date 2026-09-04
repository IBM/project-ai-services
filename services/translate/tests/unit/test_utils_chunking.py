"""
Unit tests for translate/utils/chunking.py — token-based document chunker.
"""

import asyncio
import pytest
from unittest.mock import AsyncMock, MagicMock, patch

from translate.models import TranslationChunk
from translate.utils.chunking import (
    _is_table_block,
    _pack_sentences_greedily,
    build_translation_chunks,
)


# ---------------------------------------------------------------------------
# _is_table_block
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestIsTableBlock:
    def test_gfm_table_is_detected(self):
        block = "| Col A | Col B |\n|-------|-------|\n| R1A   | R1B   |"
        assert _is_table_block(block) is True

    def test_plain_text_is_not_table(self):
        assert _is_table_block("This is a regular paragraph.") is False

    def test_single_line_starting_with_pipe_is_not_table(self):
        # Need at least two lines starting with |
        assert _is_table_block("| Col A |") is False

    def test_empty_string_is_not_table(self):
        assert _is_table_block("") is False

    def test_blank_lines_only_is_not_table(self):
        assert _is_table_block("   \n   \n   ") is False

    def test_separator_line_without_header_not_table(self):
        block = "Some text\n|-------|-------|"
        assert _is_table_block(block) is False

    def test_table_with_leading_whitespace_detected(self):
        block = "  | Col A | Col B |\n  |-------|-------|\n  | R1A   | R1B   |"
        assert _is_table_block(block) is True


# ---------------------------------------------------------------------------
# _pack_sentences_greedily
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestPackSentencesGreedily:
    @pytest.mark.asyncio
    async def test_single_sentence_fits_in_budget(self):
        sentences = ["Hello world."]

        with patch("translate.utils.chunking._count_tokens", new=AsyncMock(return_value=3)):
            chunks = await _pack_sentences_greedily(
                sentences=sentences,
                budget=10,
                llm_endpoint="http://vllm:8000",
                start_index=0,
                parent_join_after="paragraph",
            )

        assert len(chunks) == 1
        assert chunks[0].text == "Hello world."
        assert chunks[0].join_after == "paragraph"

    @pytest.mark.asyncio
    async def test_multiple_sentences_packed_greedily(self):
        sentences = ["S1.", "S2.", "S3."]

        # Each sentence = 3 tokens, budget = 6 → fits two per chunk
        call_count = [0]

        async def fake_count_tokens(text, endpoint):
            call_count[0] += 1
            return 3

        with patch("translate.utils.chunking._count_tokens", side_effect=fake_count_tokens):
            chunks = await _pack_sentences_greedily(
                sentences=sentences,
                budget=6,
                llm_endpoint="http://vllm:8000",
                start_index=0,
                parent_join_after="paragraph",
            )

        assert len(chunks) == 2
        assert "S1" in chunks[0].text and "S2" in chunks[0].text
        assert "S3" in chunks[1].text

    @pytest.mark.asyncio
    async def test_last_chunk_inherits_parent_join_after(self):
        sentences = ["S1.", "S2.", "S3."]

        async def fake_count(text, endpoint):
            return 3

        with patch("translate.utils.chunking._count_tokens", side_effect=fake_count):
            chunks = await _pack_sentences_greedily(
                sentences=sentences,
                budget=6,
                llm_endpoint="http://vllm:8000",
                start_index=0,
                parent_join_after="paragraph",
            )

        # Last chunk should have "paragraph", intermediate should have "sentence"
        assert chunks[-1].join_after == "paragraph"
        if len(chunks) > 1:
            assert chunks[0].join_after == "sentence"

    @pytest.mark.asyncio
    async def test_empty_sentences_list_returns_empty(self):
        with patch("translate.utils.chunking._count_tokens", new=AsyncMock(return_value=3)):
            chunks = await _pack_sentences_greedily(
                sentences=[],
                budget=10,
                llm_endpoint="http://vllm:8000",
                start_index=0,
                parent_join_after="paragraph",
            )

        assert chunks == []

    @pytest.mark.asyncio
    async def test_start_index_increments_correctly(self):
        sentences = ["A.", "B.", "C.", "D."]

        # Budget = 3 → one sentence per chunk
        async def fake_count(text, endpoint):
            return 3

        with patch("translate.utils.chunking._count_tokens", side_effect=fake_count):
            chunks = await _pack_sentences_greedily(
                sentences=sentences,
                budget=3,
                llm_endpoint="http://vllm:8000",
                start_index=5,  # offset
                parent_join_after="sentence",
            )

        indices = [c.index for c in chunks]
        assert indices == list(range(5, 5 + len(chunks)))


# ---------------------------------------------------------------------------
# build_translation_chunks
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestBuildTranslationChunks:
    @pytest.mark.asyncio
    async def test_empty_text_returns_empty(self):
        with patch("translate.utils.chunking.get_chunk_token_budget", return_value=1000), \
             patch("translate.utils.chunking.to_sentence_splitter_lang", return_value="en"):
            chunks = await build_translation_chunks("", llm_endpoint="http://vllm:8000")
        assert chunks == []

    @pytest.mark.asyncio
    async def test_whitespace_only_text_returns_empty(self):
        with patch("translate.utils.chunking.get_chunk_token_budget", return_value=1000), \
             patch("translate.utils.chunking.to_sentence_splitter_lang", return_value="en"):
            chunks = await build_translation_chunks("   \n\n   ", llm_endpoint="http://vllm:8000")
        assert chunks == []

    @pytest.mark.asyncio
    async def test_single_small_block_is_single_chunk(self):
        text = "This is a short paragraph."

        with patch("translate.utils.chunking.get_chunk_token_budget", return_value=1000), \
             patch("translate.utils.chunking.to_sentence_splitter_lang", return_value="en"), \
             patch("translate.utils.chunking._count_tokens", new=AsyncMock(return_value=5)):
            chunks = await build_translation_chunks(text, llm_endpoint="http://vllm:8000")

        assert len(chunks) == 1
        assert chunks[0].index == 0
        assert chunks[0].text == text.strip()
        assert chunks[0].join_after == "paragraph"

    @pytest.mark.asyncio
    async def test_two_blocks_packed_into_one_chunk_when_budget_allows(self):
        text = "Block one.\n\nBlock two."

        # Both blocks together fit inside budget=100
        with patch("translate.utils.chunking.get_chunk_token_budget", return_value=100), \
             patch("translate.utils.chunking.to_sentence_splitter_lang", return_value="en"), \
             patch("translate.utils.chunking._count_tokens", new=AsyncMock(return_value=5)):
            chunks = await build_translation_chunks(text, llm_endpoint="http://vllm:8000")

        assert len(chunks) == 1
        assert "Block one." in chunks[0].text
        assert "Block two." in chunks[0].text

    @pytest.mark.asyncio
    async def test_oversized_block_forces_new_chunk(self):
        text = "Short block.\n\nOversized block text."

        # First block = 5 tokens, second = 200 (exceeds budget of 100)
        token_map = {"Short block.": 5, "Oversized block text.": 200}

        async def fake_count(text, endpoint):
            return token_map.get(text, 5)

        mock_splitter = MagicMock()
        mock_splitter.split.return_value = ["Oversized block text."]

        with patch("translate.utils.chunking.get_chunk_token_budget", return_value=100), \
             patch("translate.utils.chunking.to_sentence_splitter_lang", return_value="en"), \
             patch("translate.utils.chunking._count_tokens", side_effect=fake_count), \
             patch("translate.utils.chunking.SentenceSplitter", return_value=mock_splitter), \
             patch("translate.utils.chunking._pack_sentences_greedily",
                   new=AsyncMock(return_value=[
                       TranslationChunk(index=1, text="Oversized block text.", token_count=200)
                   ])):
            chunks = await build_translation_chunks(text, llm_endpoint="http://vllm:8000")

        # At least two chunks: one for the normal block, one (or more) for the oversized
        assert len(chunks) >= 2

    @pytest.mark.asyncio
    async def test_gfm_table_gets_its_own_chunk(self):
        table_block = "| H1 | H2 |\n|----|----|\n| A  | B  |"
        text = f"Intro paragraph.\n\n{table_block}"

        # Intro = 5 tokens, table = 200 tokens (over budget)
        async def fake_count(text_arg, endpoint):
            if "|" in text_arg:
                return 200
            return 5

        with patch("translate.utils.chunking.get_chunk_token_budget", return_value=100), \
             patch("translate.utils.chunking.to_sentence_splitter_lang", return_value="en"), \
             patch("translate.utils.chunking._count_tokens", side_effect=fake_count), \
             patch("translate.utils.chunking.SentenceSplitter") as mock_ss:
            chunks = await build_translation_chunks(text, llm_endpoint="http://vllm:8000")

        # Table chunk should be present and NOT sentence-split
        table_chunks = [c for c in chunks if "|" in c.text]
        assert len(table_chunks) == 1
        assert table_chunks[0].join_after == "paragraph"

    @pytest.mark.asyncio
    async def test_chunk_indices_are_sequential(self):
        text = "\n\n".join([f"Paragraph {i}." for i in range(5)])

        with patch("translate.utils.chunking.get_chunk_token_budget", return_value=20), \
             patch("translate.utils.chunking.to_sentence_splitter_lang", return_value="en"), \
             patch("translate.utils.chunking._count_tokens", new=AsyncMock(return_value=8)), \
             patch("translate.utils.chunking.SentenceSplitter") as mock_ss:
            mock_ss.return_value.split.return_value = ["A sentence."]
            chunks = await build_translation_chunks(text, llm_endpoint="http://vllm:8000")

        indices = [c.index for c in chunks]
        assert indices == list(range(len(chunks)))

    @pytest.mark.asyncio
    async def test_source_language_code_passed_to_sentence_splitter(self):
        text = "Ein einfacher Paragraph."

        with patch("translate.utils.chunking.get_chunk_token_budget", return_value=1000), \
             patch("translate.utils.chunking.to_sentence_splitter_lang", return_value="de") as mock_lang, \
             patch("translate.utils.chunking._count_tokens", new=AsyncMock(return_value=5)), \
             patch("translate.utils.chunking.SentenceSplitter") as mock_ss_cls:
            mock_ss_cls.return_value = MagicMock()
            mock_ss_cls.return_value.split.return_value = [text]

            await build_translation_chunks(
                text, llm_endpoint="http://vllm:8000", source_language_code="DE"
            )

        mock_lang.assert_called_with("DE")

    @pytest.mark.asyncio
    async def test_none_source_language_defaults_to_en(self):
        text = "A simple paragraph."

        with patch("translate.utils.chunking.get_chunk_token_budget", return_value=1000), \
             patch("translate.utils.chunking.to_sentence_splitter_lang", return_value="en") as mock_lang, \
             patch("translate.utils.chunking._count_tokens", new=AsyncMock(return_value=5)), \
             patch("translate.utils.chunking.SentenceSplitter") as mock_ss_cls:
            mock_ss_cls.return_value = MagicMock()

            await build_translation_chunks(text, llm_endpoint="http://vllm:8000", source_language_code=None)

        mock_lang.assert_called_with("EN")

# Made with Bob
