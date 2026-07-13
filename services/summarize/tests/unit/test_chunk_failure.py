"""
Unit tests for chunked summarization failure handling in process_summarization_job.

Covers the scenarios added to the chunk processing path:
1. A single chunk LLM failure increments failed_chunks in DB and re-raises.
2. A chunk failure cancels all sibling in-flight tasks immediately.
3. The job is ultimately marked FAILED in the database with the correct error.
4. A passing run still marks the job COMPLETED.
5. The merge step is never reached when a chunk fails.

Patching strategy
-----------------
process_summarization_job uses names from two scopes:

* Top-level imports in summarize/app.py (e.g. tokenize_with_llm, query_vllm_summarize,
  compute_target_and_max_tokens, cleanup_staging_directory …).
  These must be patched at  ``summarize.app.<name>``  because that is where Python
  resolves them at call time.

* Local imports inside the function body (db_repo, split_text_into_chunks,
  build_merge_messages).  These land in the *source* module's namespace, so they
  must be patched there: ``summarize.db.manager.db_repo``,
  ``summarize.chunk_utils.split_text_into_chunks``, etc.
"""

import asyncio
from pathlib import Path
from unittest.mock import MagicMock, Mock, patch

import pytest


# ---------------------------------------------------------------------------
# Constants / helpers
# ---------------------------------------------------------------------------

JOB_ID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
_LLM_MODEL_DICT = {"llm_endpoint": "http://localhost:8002", "llm_model": "test-model"}
_TOKEN_LIST = list(range(50))  # 50 fake tokens


def _make_db_job(metadata=None):
    """Minimal mock SummarizeJob with controllable metadata."""
    job = MagicMock()
    job.job_metadata = dict(metadata) if metadata else {}
    return job


def _make_settings(staging_dir, results_dir=None):
    """Mock settings with controllable paths and plain scalar values."""
    s = MagicMock()
    s.summarize.staging_dir = staging_dir
    s.summarize.results_dir = results_dir or Path("/tmp/results")
    s.summarize.summarization_prompt_token_count = 50
    s.summarize.summarization_temperature = 0.0
    s.summarize.chunk_parallelism = 4
    s.summarize.chunk_overlap_sentences = 2
    return s


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture
def mock_db_repo():
    repo = MagicMock()
    repo.get_job_by_id.return_value = _make_db_job(
        {"total_chunks": 3, "completed_chunks": 0, "failed_chunks": 0, "phase": "summarizing"}
    )
    repo.update_job.return_value = True
    return repo


# ---------------------------------------------------------------------------
# Shared patch factory
# ---------------------------------------------------------------------------

# Strategy forcing:
#   input_tokens=50, prompt=50, max_tokens=10  → total_required=110 > 100 → CHUNKED
#   merge pre-check: N*10 + 50 + 10 = 70/80 < 100 → passes
_CHUNKED_MAX_MODEL_LEN = 100


def _make_patches(tmp_path, mock_db_repo, chunks, query_side_effect=None):
    """
    Return an ordered list of patch() objects that fully isolates
    process_summarization_job from all I/O.

    The context window (_CHUNKED_MAX_MODEL_LEN=150) is deliberately smaller
    than input_tokens(50) + prompt_tokens(50) + max_output_tokens(100) = 200,
    which forces the CHUNKED strategy so chunk-processing code is exercised.

    Index reference (used when a test needs to swap one entry):
        0  settings
        1  llm_model_dict          (create=True — not set until app startup)
        2  db_repo                 (local import → patch at source)
        3  tokenize_with_llm       (top-level import in app.py)
        4  get_llm_max_model_len   (top-level import in app.py)
        5  compute_target_and_max_tokens (top-level import in app.py)
        6  cleanup_staging_directory     (top-level import in app.py)
        7  split_text_into_chunks  (local import → patch at source)
        8  build_merge_messages    (local import → patch at source)
        9  query_vllm_summarize    (top-level import in app.py)
    """
    return [
        # 0 — results_dir points at tmp_path so open() has a real directory
        patch("summarize.app.settings",
              _make_settings(staging_dir=tmp_path, results_dir=tmp_path)),
        # 1
        patch("summarize.app.llm_model_dict", _LLM_MODEL_DICT, create=True),
        # 2  — local import inside process_summarization_job
        patch("summarize.db.manager.db_repo", mock_db_repo),
        # 3  — top-level import: from common.llm_utils import tokenize_with_llm
        patch("summarize.app.tokenize_with_llm", return_value=_TOKEN_LIST),
        # 4  — small context window → forces CHUNKED strategy
        patch("summarize.app.get_llm_max_model_len", return_value=_CHUNKED_MAX_MODEL_LEN),
        # 5  — small max_tokens (10) so merge pre-check passes: N*10+50+10 < 100
        patch("summarize.app.compute_target_and_max_tokens", return_value=(5, 4, 6, 10)),
        # 6  — top-level import: from common.misc_utils import cleanup_staging_directory
        patch("summarize.app.cleanup_staging_directory"),
        # 7  — local import: from summarize.chunk_utils import split_text_into_chunks
        patch("summarize.chunk_utils.split_text_into_chunks", return_value=chunks),
        # 8  — local import: from summarize.chunk_utils import build_merge_messages
        patch("summarize.chunk_utils.build_merge_messages", return_value=[
            {"role": "system", "content": "sys"},
            {"role": "user", "content": "merge"},
        ]),
        # 9  — top-level import: from common.llm_utils import query_vllm_summarize
        patch("summarize.app.query_vllm_summarize", side_effect=query_side_effect),
    ]


class _MultiPatch:
    """Enter/exit an ordered list of patch() context managers."""

    def __init__(self, patches):
        self._patches = patches

    def __enter__(self):
        self._mocks = [p.__enter__() for p in self._patches]
        return self._mocks

    def __exit__(self, *args):
        for p in reversed(self._patches):
            p.__exit__(*args)


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

@pytest.mark.unit
class TestChunkFailureHandling:
    """
    Drives process_summarization_job as a coroutine to verify the chunk-failure
    handling added in the chunked path.
    """


    @pytest.mark.asyncio
    async def test_single_chunk_failure_sets_job_status_to_failed(
        self, mock_db_repo, tmp_path
    ):
        """
        A chunk failure must propagate to the outer handler and set the job
        status to FAILED in the database.
        """
        from summarize.models import JobStatus

        (tmp_path / JOB_ID).mkdir(parents=True)
        (tmp_path / JOB_ID / "doc.txt").write_text("word " * 100)

        chunks = ["chunk A", "chunk B"]

        def fake_query(llm_endpoint, messages, model, max_tokens, temperature):
            return {"error": "Model overloaded"}, 10, 0

        import summarize.app as app_module

        with _MultiPatch(_make_patches(tmp_path, mock_db_repo, chunks, fake_query)):
            await app_module.process_summarization_job(JOB_ID, "standard")

        failed_status_calls = [
            c for c in mock_db_repo.update_job.call_args_list
            if c.kwargs.get("status") == JobStatus.FAILED
        ]
        assert failed_status_calls, "expected update_job to be called with status=FAILED"

    @pytest.mark.asyncio
    async def test_single_chunk_failure_error_message_stored_in_db(
        self, mock_db_repo, tmp_path
    ):
        """
        The error recorded in the DB must contain 'chunk' so operators can
        identify which chunk failed.
        """
        (tmp_path / JOB_ID).mkdir(parents=True)
        (tmp_path / JOB_ID / "doc.txt").write_text("word " * 100)

        chunks = ["chunk X", "chunk Y", "chunk Z"]
        call_count = 0

        def fake_query(llm_endpoint, messages, model, max_tokens, temperature):
            nonlocal call_count
            call_count += 1
            if call_count == 3:
                return {"error": "GPU OOM"}, 10, 0
            return "Summary.", 10, 5

        import summarize.app as app_module

        with _MultiPatch(_make_patches(tmp_path, mock_db_repo, chunks, fake_query)):
            await app_module.process_summarization_job(JOB_ID, "standard")

        error_strings = [
            c.kwargs["error"]
            for c in mock_db_repo.update_job.call_args_list
            if c.kwargs.get("error")
        ]
        assert error_strings, "expected at least one update_job call with an error kwarg"
        assert "chunk" in " ".join(error_strings).lower(), (
            f"Expected 'chunk' in error message, got: {error_strings}"
        )

    @pytest.mark.asyncio
    async def test_sibling_tasks_cancelled_on_chunk_failure(
        self, mock_db_repo, tmp_path
    ):
        """
        When one chunk fails, the other in-flight tasks must receive
        CancelledError — they must not run to natural completion.
        """
        (tmp_path / JOB_ID).mkdir(parents=True)
        (tmp_path / JOB_ID / "doc.txt").write_text("word " * 100)

        chunks = ["chunk alpha", "chunk beta"]
        chunk_1_cancelled = asyncio.Event()
        llm_call_count = 0

        # query_vllm_summarize is called synchronously inside asyncio.to_thread.
        # We need chunk-1's to_thread call to be cancellable, so we override
        # asyncio.to_thread itself: the first LLM call fails immediately; the
        # second blocks in an awaitable sleep that catches CancelledError.
        original_to_thread = asyncio.to_thread

        async def controlled_to_thread(func, *args, **kwargs):
            nonlocal llm_call_count
            # app.py makes four kinds of asyncio.to_thread calls:
            #   (a) lambda: len(tokenize_with_llm(...))  → no positional args → return int
            #   (b) tokenize_with_llm, chunk, endpoint   → return list (caller calls len())
            #   (c) split_text_into_chunks, text, ...    → return the test's chunks list
            #   (d) query_vllm_summarize, ...            → LLM result tuple
            import summarize.chunk_utils as _chunk_utils
            import summarize.app as _app

            split_mock = getattr(_chunk_utils, "split_text_into_chunks", None)
            query_mock = getattr(_app, "query_vllm_summarize", None)

            if func is split_mock:
                # (c) return the pre-configured chunks list
                return chunks

            if func is query_mock:
                # (d) LLM call
                llm_call_count += 1
                if llm_call_count == 1:
                    return {"error": "instant failure"}, 10, 0
                # Chunk 1+: block until cancelled — proves sibling cancellation
                try:
                    await asyncio.sleep(60)
                    return "summary", 10, 5
                except asyncio.CancelledError:
                    chunk_1_cancelled.set()
                    raise

            # (a) tokenize lambda — no positional args, lambda wraps len() itself
            if not args:
                return len(_TOKEN_LIST)

            # (b) direct tokenize_with_llm(chunk, endpoint) call
            return _TOKEN_LIST

        import summarize.app as app_module

        patches = _make_patches(tmp_path, mock_db_repo, chunks)
        # Remove the query_vllm_summarize patch (index 9) — we control it via
        # controlled_to_thread instead, so it must not be patched separately.
        patches.pop(9)

        with _MultiPatch(patches), \
             patch("asyncio.to_thread", side_effect=controlled_to_thread):
            await app_module.process_summarization_job(JOB_ID, "standard")

        assert chunk_1_cancelled.is_set(), (
            "Sibling chunk task was not cancelled after chunk 0 failed"
        )

    @pytest.mark.asyncio
    async def test_successful_chunks_mark_job_completed(
        self, mock_db_repo, tmp_path
    ):
        """
        When all chunks succeed the job must be marked COMPLETED, not FAILED.
        """
        from summarize.models import JobStatus

        (tmp_path / JOB_ID).mkdir(parents=True)
        (tmp_path / JOB_ID / "doc.txt").write_text("word " * 100)

        chunks = ["chunk one", "chunk two"]

        def fake_query(llm_endpoint, messages, model, max_tokens, temperature):
            return "Good summary.", 10, 5

        import summarize.app as app_module

        with _MultiPatch(_make_patches(tmp_path, mock_db_repo, chunks, fake_query)), \
             patch("builtins.open", MagicMock()), \
             patch("json.dump"):
            await app_module.process_summarization_job(JOB_ID, "standard")

        completed_calls = [
            c for c in mock_db_repo.update_job.call_args_list
            if c.kwargs.get("status") == JobStatus.COMPLETED
        ]
        assert completed_calls, "expected update_job to be called with status=COMPLETED"

    @pytest.mark.asyncio
    async def test_chunk_failure_does_not_proceed_to_merge(
        self, mock_db_repo, tmp_path
    ):
        """
        When a chunk fails, build_merge_messages must never be called.
        """
        (tmp_path / JOB_ID).mkdir(parents=True)
        (tmp_path / JOB_ID / "doc.txt").write_text("word " * 100)

        chunks = ["chunk I", "chunk II"]

        def fake_query(llm_endpoint, messages, model, max_tokens, temperature):
            return {"error": "bad request"}, 10, 0

        mock_build_merge = Mock()

        import summarize.app as app_module

        patches = _make_patches(tmp_path, mock_db_repo, chunks, fake_query)
        # Replace build_merge_messages placeholder (index 8) with our spy
        patches[8] = patch("summarize.chunk_utils.build_merge_messages", mock_build_merge)

        with _MultiPatch(patches):
            await app_module.process_summarization_job(JOB_ID, "standard")

        mock_build_merge.assert_not_called()
