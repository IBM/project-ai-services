"""
Regression tests for chunk boundary bug in split_text_into_chunks.

The one bug covered is:

1. Overlap overflow (chunk_utils.py)
   When a new chunk is started, the overlap sentences from the previous chunk
   are prepended.  The old code added them unconditionally, without checking
   whether overlap + first_paragraph already exceeded the word budget.  Every
   subsequent paragraph then accumulated on top of an already-over-budget
   baseline, producing chunks that exceeded max_words.

   Fix: both "overflow branch" and "empty-chunk branch" now guard with
        `if overlap_words + para_words > max_words` before prepending the
        overlap.


"""

import pytest

from summarize.chunk_utils import split_text_into_chunks
from summarize.summ_utils import word_count


# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------

def _words(n: int, word: str = "word") -> str:
    """Return a string of exactly *n* copies of *word*."""
    return (word + " ") * n


# ---------------------------------------------------------------------------
# Bug 1: overlap must not push a new chunk over max_words
# ---------------------------------------------------------------------------

@pytest.mark.unit
class TestOverlapDoesNotExceedMaxWords:
    """
    split_text_into_chunks must never produce a chunk whose word count
    exceeds max_words, regardless of the overlap size.

    Each test constructs text where overlap sentences are long enough to,
    when combined with the first paragraph of the new chunk, exceed max_words.
    Without the fix, word_count(chunk) > max_words for at least one chunk.
    With the fix, every chunk stays at or below max_words.
    """

    def test_overflow_branch_overlap_does_not_exceed_max_tokens(self):
        """
        Overflow branch: chunk fills up → paragraph triggers finalise+new-chunk.

        Construction:
          max_words = 20
          overlap_sentences = 1 — the overlap sentence is 10 words long
          paragraph_A  fills the first chunk to exactly 18 words
          paragraph_B  (14 words) triggers the overflow: 18 + 14 = 32 > 20
            → chunk A is finalised, overlap (last sentence ≈ 18 words) is taken
            → overlap(18) + para_B(14) = 32 > 20
            → with fix: overlap skipped, chunk starts with just para_B(14) ≤ 20 ✓
          paragraph_C  (10 words) is added: 14 + 10 = 24 > 20
            → chunk B finalised, overlap from chunk B is taken
            → overlap from last sentence of chunk B ≈ 14 words
            → overlap(14) + para_C(10) = 24 > 20  ← OLD CODE overflows here
            → with fix: overlap skipped, chunk starts with just para_C(10) ≤ 20 ✓
        """
        max_words = 20
        overlap_sentences = 1

        para_A = _words(18, "alpha")
        para_B_large = _words(14, "beta")
        text = para_A + "\n\n" + para_B_large + "\n\n" + _words(10, "gamma")

        result = split_text_into_chunks(
            text,
            max_words=max_words,
            overlap_sentences=overlap_sentences,
        )

        assert result, "expected at least one chunk"
        for i, chunk in enumerate(result):
            wc = word_count(chunk)
            assert wc <= max_words, (
                f"Chunk {i} has {wc} words which exceeds max_words={max_words}.\n"
                f"Chunk text: {chunk!r}"
            )

    def test_empty_chunk_branch_overlap_does_not_exceed_max_tokens(self):
        """
        Empty-chunk branch: previous chunk was produced by the sentence-splitter
        (oversized paragraph path), so current_chunk is empty when the next
        paragraph arrives and previous_sentences is set.

        Without the fix the overlap is prepended unconditionally and the new
        chunk immediately starts over budget.

        Construction:
          max_words = 20, overlap_sentences = 1
          big_paragraph has 4 sentences of 8 words each = 32 words total.
          Each sentence ends with a capital-letter start on the next word so the
          sentence splitter treats them as separate sentences.  The paragraph
          exceeds max_words so it goes through _split_paragraph_into_chunks,
          producing multiple ≤20-word chunks.  The last sentence (8 words)
          becomes previous_sentences[0].

          small_para = 14 words — fits on its own (14 ≤ 20), but
          overlap(8) + small_para(14) = 22 > 20, so the old code would start
          the new chunk already over budget.  With the fix the overlap is
          dropped and the chunk starts with just small_para(14 ≤ 20).
        """
        max_words = 20
        overlap_sentences = 1

        sentences = [
            "Alpha beta gamma delta epsilon zeta eta theta.",
            "Iota kappa lambda mu nu xi omicron pi.",
            "Rho sigma tau upsilon phi chi psi omega.",
            "Able baker charlie delta echo foxtrot golf hotel.",
        ]
        big_paragraph = " ".join(sentences)   # 32 words — exceeds max_words=20
        small_para = " ".join(["word"] * 14)
        text = big_paragraph + "\n\n" + small_para

        result = split_text_into_chunks(
            text,
            max_words=max_words,
            overlap_sentences=overlap_sentences,
        )

        assert result, "expected at least one chunk"
        for i, chunk in enumerate(result):
            wc = word_count(chunk)
            assert wc <= max_words, (
                f"Chunk {i} has {wc} words which exceeds max_words={max_words}.\n"
                f"Chunk text: {chunk!r}"
            )

    def test_no_chunk_exceeds_max_tokens_across_many_paragraphs(self):
        """
        Stress test: 30 paragraphs of varying sizes with overlap=2.
        Every produced chunk must be ≤ max_words.
        """
        max_words = 30
        overlap_sentences = 2

        paragraphs = []
        for i in range(30):
            size = [5, 15, 28][i % 3]
            paragraphs.append(_words(size, f"w{i}"))

        text = "\n\n".join(paragraphs)

        result = split_text_into_chunks(
            text,
            max_words=max_words,
            overlap_sentences=overlap_sentences,
        )

        assert result, "expected at least one chunk"
        for i, chunk in enumerate(result):
            wc = word_count(chunk)
            assert wc <= max_words, (
                f"Chunk {i} has {wc} words which exceeds max_words={max_words}.\n"
                f"Chunk text: {chunk!r}"
            )


