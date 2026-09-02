"""
Unit tests for translate/workers/translation_worker.py — translation pipeline.
"""

import asyncio
import pytest
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, Mock, patch

from translate.models import JobStatus, TranslationChunk
from translate.workers.translation_worker import (
    _assemble_translation,
    _translate_chunk,
    run_translation_job,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_chunk(index: int, text: str, join_after: str = "paragraph", token_count: int = 10) -> TranslationChunk:
    return TranslationChunk(index=index, text=text, join_after=join_after, token_count=token_count)


# ---------------------------------------------------------------------------
# _assemble_translation
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestAssembleTranslation:
    def test_single_chunk(self):
        results = [(_make_chunk(0, "orig"), "Hallo", 5, 4)]
        text, total_in, total_out = _assemble_translation(results)
        assert text == "Hallo"
        assert total_in == 5
        assert total_out == 4

    def test_two_paragraph_chunks_joined_with_double_newline(self):
        results = [
            (_make_chunk(0, "A", join_after="paragraph"), "Eins", 3, 3),
            (_make_chunk(1, "B", join_after="paragraph"), "Zwei", 3, 3),
        ]
        text, _, _ = _assemble_translation(results)
        assert text == "Eins\n\nZwei"

    def test_two_sentence_chunks_joined_with_space(self):
        results = [
            (_make_chunk(0, "S1", join_after="sentence"), "Eins.", 3, 3),
            (_make_chunk(1, "S2", join_after="sentence"), "Zwei.", 3, 3),
        ]
        text, _, _ = _assemble_translation(results)
        assert text == "Eins. Zwei."

    def test_last_chunk_join_after_not_appended(self):
        """The separator is determined by join_after of every chunk EXCEPT the last."""
        results = [
            (_make_chunk(0, "A", join_after="paragraph"), "Alpha", 2, 2),
            (_make_chunk(1, "B", join_after="paragraph"), "Beta", 2, 2),
        ]
        text, _, _ = _assemble_translation(results)
        # Should be "Alpha\n\nBeta", NOT "Alpha\n\nBeta\n\n"
        assert text.endswith("Beta")
        assert not text.endswith("\n\n")

    def test_mixed_join_modes(self):
        """Sentence chunk followed by paragraph chunk."""
        results = [
            (_make_chunk(0, "S", join_after="sentence"), "Satz.", 2, 2),
            (_make_chunk(1, "P", join_after="paragraph"), "Absatz.", 2, 2),
        ]
        text, _, _ = _assemble_translation(results)
        # The separator between 0→1 is " " (from chunk 0's join_after="sentence")
        assert text == "Satz. Absatz."

    def test_out_of_order_results_are_sorted_by_index(self):
        results = [
            (_make_chunk(2, "C"), "Drei", 1, 1),
            (_make_chunk(0, "A"), "Eins", 1, 1),
            (_make_chunk(1, "B"), "Zwei", 1, 1),
        ]
        text, _, _ = _assemble_translation(results)
        assert text == "Eins\n\nZwei\n\nDrei"

    def test_total_tokens_are_summed(self):
        results = [
            (_make_chunk(0, "A"), "T1", 10, 8),
            (_make_chunk(1, "B"), "T2", 20, 15),
        ]
        _, total_in, total_out = _assemble_translation(results)
        assert total_in == 30
        assert total_out == 23

    def test_empty_results_list(self):
        text, total_in, total_out = _assemble_translation([])
        assert text == ""
        assert total_in == 0
        assert total_out == 0


# ---------------------------------------------------------------------------
# _translate_chunk
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestTranslateChunk:
    @pytest.mark.asyncio
    async def test_returns_tuple_with_chunk(self):
        chunk = _make_chunk(0, "Hello")
        mock_client = AsyncMock()

        with patch("translate.workers.translation_worker.build_messages", return_value=[]), \
             patch("translate.workers.translation_worker.query_vllm_translate",
                   new=AsyncMock(return_value=("Hallo", 5, 4))), \
             patch("translate.workers.translation_worker.settings") as mock_settings, \
             patch("translate.workers.translation_worker.concurrency_manager") as mock_cm:

            mock_settings.common.llm.endpoint = "http://vllm:8000"
            mock_settings.common.llm.model = "granite"
            mock_settings.translate.translation_temperature = 0.0

            # Real BoundedSemaphore to satisfy `async with`
            mock_cm.chunk_semaphore = asyncio.BoundedSemaphore(1)
            mock_cm.vllm_semaphore = asyncio.BoundedSemaphore(1)

            result = await _translate_chunk(
                chunk=chunk,
                resolved_source_language="German",
                target_language="English",
                http_client=mock_client,
                max_tokens=200,
            )

        returned_chunk, translation, in_tok, out_tok = result
        assert returned_chunk is chunk
        assert translation == "Hallo"
        assert in_tok == 5
        assert out_tok == 4


# ---------------------------------------------------------------------------
# run_translation_job (integration-style unit test)
# ---------------------------------------------------------------------------


@pytest.mark.unit
class TestRunTranslationJob:
    @pytest.mark.asyncio
    async def test_successful_job_marks_completed(self, tmp_path):
        staged_file = tmp_path / "job-1" / "doc.txt"
        staged_file.parent.mkdir(parents=True)
        staged_file.write_text("Hello world", encoding="utf-8")

        mock_db = MagicMock()
        mock_db.update_job = MagicMock(return_value=True)

        chunks = [_make_chunk(0, "Hello world", token_count=5)]
        results = [(chunks[0], "Hallo Welt", 5, 4)]

        with patch("translate.workers.translation_worker.db_manager", mock_db), \
             patch("translate.workers.translation_worker.resolve_source_language",
                   return_value=("German", "DE")), \
             patch("translate.workers.translation_worker.build_translation_chunks",
                   new=AsyncMock(return_value=chunks)), \
             patch("translate.workers.translation_worker.get_llm_max_model_len", return_value=32768), \
             patch("translate.workers.translation_worker.storage_manager") as mock_storage, \
             patch("translate.workers.translation_worker._translate_chunk",
                   new=AsyncMock(return_value=(chunks[0], "Hallo Welt", 5, 4))), \
             patch("translate.workers.translation_worker.settings") as mock_settings, \
             patch("translate.workers.translation_worker.concurrency_manager") as mock_cm, \
             patch("httpx.AsyncClient") as mock_http:

            mock_settings.common.llm.endpoint = "http://vllm:8000"
            mock_settings.common.llm.model = "granite"
            mock_settings.common.llm.api_key = None
            mock_settings.translate.prompt_overhead_tokens = 250
            mock_settings.translate.translation_temperature = 0.0
            mock_settings.translate.chunk_token_budget = 13107

            # Real semaphore for job_limiter
            mock_cm.job_limiter = asyncio.BoundedSemaphore(8)

            mock_storage.write_result = MagicMock()
            mock_storage.cleanup_staging = MagicMock()

            # httpx.AsyncClient context manager
            mock_http_instance = AsyncMock()
            mock_http_instance.__aenter__ = AsyncMock(return_value=mock_http_instance)
            mock_http_instance.__aexit__ = AsyncMock(return_value=None)
            mock_http.return_value = mock_http_instance

            await run_translation_job(
                job_id="job-1",
                staged_file_path=staged_file,
                source_language="auto",
                target_language="English",
                document_name="doc.txt",
                input_type="txt",
            )

        # DB should have been updated to completed
        update_calls = [str(c) for c in mock_db.update_job.call_args_list]
        assert any("COMPLETED" in c or "completed" in c for c in update_calls)
        # Staging directory should be cleaned up
        mock_storage.cleanup_staging.assert_called_once_with("job-1")

    @pytest.mark.asyncio
    async def test_job_marked_failed_on_exception(self, tmp_path):
        staged_file = tmp_path / "job-err" / "doc.txt"
        staged_file.parent.mkdir(parents=True)
        staged_file.write_text("text", encoding="utf-8")

        mock_db = MagicMock()
        mock_db.update_job = MagicMock(return_value=True)

        with patch("translate.workers.translation_worker.db_manager", mock_db), \
             patch("translate.workers.translation_worker.resolve_source_language",
                   side_effect=RuntimeError("lingua exploded")), \
             patch("translate.workers.translation_worker.storage_manager") as mock_storage, \
             patch("translate.workers.translation_worker.settings") as mock_settings, \
             patch("translate.workers.translation_worker.concurrency_manager") as mock_cm:

            mock_settings.common.llm.endpoint = "http://vllm:8000"
            mock_settings.common.llm.model = "granite"
            mock_settings.common.llm.api_key = None
            mock_settings.translate.prompt_overhead_tokens = 250

            mock_cm.job_limiter = asyncio.BoundedSemaphore(8)
            mock_storage.cleanup_staging = MagicMock()

            await run_translation_job(
                job_id="job-err",
                staged_file_path=staged_file,
                source_language="auto",
                target_language="English",
                document_name="doc.txt",
                input_type="txt",
            )

        # Should have been updated to FAILED
        update_calls = mock_db.update_job.call_args_list
        failed_calls = [c for c in update_calls if JobStatus.FAILED in c.args or c.kwargs.get("status") == JobStatus.FAILED]
        assert len(failed_calls) >= 1
        # Staging always cleaned up
        mock_storage.cleanup_staging.assert_called()

    @pytest.mark.asyncio
    async def test_staging_cleanup_always_runs_on_success(self, tmp_path):
        staged_file = tmp_path / "job-clean" / "doc.txt"
        staged_file.parent.mkdir(parents=True)
        staged_file.write_text("text", encoding="utf-8")

        mock_db = MagicMock()

        with patch("translate.workers.translation_worker.db_manager", mock_db), \
             patch("translate.workers.translation_worker.resolve_source_language",
                   return_value=(None, None)), \
             patch("translate.workers.translation_worker.build_translation_chunks",
                   new=AsyncMock(return_value=[])), \
             patch("translate.workers.translation_worker.get_llm_max_model_len", return_value=32768), \
             patch("translate.workers.translation_worker.storage_manager") as mock_storage, \
             patch("translate.workers.translation_worker.settings") as mock_settings, \
             patch("translate.workers.translation_worker.concurrency_manager") as mock_cm, \
             patch("httpx.AsyncClient") as mock_http:

            mock_settings.common.llm.endpoint = "http://vllm:8000"
            mock_settings.common.llm.model = "granite"
            mock_settings.common.llm.api_key = None
            mock_settings.translate.prompt_overhead_tokens = 250
            mock_settings.translate.translation_temperature = 0.0
            mock_settings.translate.chunk_token_budget = 13107

            mock_cm.job_limiter = asyncio.BoundedSemaphore(8)
            mock_storage.write_result = MagicMock()
            mock_storage.cleanup_staging = MagicMock()

            mock_http_instance = AsyncMock()
            mock_http_instance.__aenter__ = AsyncMock(return_value=mock_http_instance)
            mock_http_instance.__aexit__ = AsyncMock(return_value=None)
            mock_http.return_value = mock_http_instance

            await run_translation_job(
                job_id="job-clean",
                staged_file_path=staged_file,
                source_language="auto",
                target_language="English",
                document_name="doc.txt",
                input_type="txt",
            )

        mock_storage.cleanup_staging.assert_called_with("job-clean")

    @pytest.mark.asyncio
    async def test_context_limit_exceeded_marks_failed(self, tmp_path):
        """If a chunk has too many tokens (leaves no output room), job must fail."""
        staged_file = tmp_path / "job-ctx" / "doc.txt"
        staged_file.parent.mkdir(parents=True)
        staged_file.write_text("text", encoding="utf-8")

        # Chunk with tokens that exceed the model window
        huge_chunk = _make_chunk(0, "text", token_count=50000)

        mock_db = MagicMock()

        with patch("translate.workers.translation_worker.db_manager", mock_db), \
             patch("translate.workers.translation_worker.resolve_source_language",
                   return_value=(None, None)), \
             patch("translate.workers.translation_worker.build_translation_chunks",
                   new=AsyncMock(return_value=[huge_chunk])), \
             patch("translate.workers.translation_worker.get_llm_max_model_len", return_value=4096), \
             patch("translate.workers.translation_worker.storage_manager") as mock_storage, \
             patch("translate.workers.translation_worker.settings") as mock_settings, \
             patch("translate.workers.translation_worker.concurrency_manager") as mock_cm:

            mock_settings.common.llm.endpoint = "http://vllm:8000"
            mock_settings.common.llm.model = "granite"
            mock_settings.common.llm.api_key = None
            mock_settings.translate.prompt_overhead_tokens = 250
            mock_settings.translate.chunk_token_budget = 13107

            mock_cm.job_limiter = asyncio.BoundedSemaphore(8)
            mock_storage.cleanup_staging = MagicMock()

            await run_translation_job(
                job_id="job-ctx",
                staged_file_path=staged_file,
                source_language="auto",
                target_language="English",
                document_name="doc.txt",
                input_type="txt",
            )

        failed_calls = [c for c in mock_db.update_job.call_args_list
                        if c.kwargs.get("status") == JobStatus.FAILED]
        assert len(failed_calls) == 1

# Made with Bob
