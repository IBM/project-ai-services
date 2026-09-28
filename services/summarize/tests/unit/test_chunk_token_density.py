"""
Regression test for chunked summarization exceeding context window on
token-dense documents.

Bug: MAX_INPUT_WORDS is computed using a fixed token_to_word_ratio_en (0.75),
but technical documents (IBM manuals, config-heavy PDFs) can have significantly
denser tokenization (e.g., 0.5–0.66 words per token).  When chunks are sized
at MAX_INPUT_WORDS words, their actual token count can blow past the model's
context window, producing a 400 from vLLM.

Fix: split_text_into_chunks accepts the document's actual word and token counts
and scales max_words down when the real ratio is worse than the configured one.
"""

import pytest

from summarize.chunk_utils import split_text_into_chunks
from summarize.summ_utils import word_count


def _make_text(n_words: int, para_size: int = 200) -> str:
    """Build a text block of ~*n_words* words with paragraph breaks and sentence punctuation.

    Sentences start with an uppercase word so that the sentence_splitter library
    recognises period-space-uppercase as a sentence boundary.
    """
    sentences = []
    words_so_far = 0
    sent_len = 12
    while words_so_far < n_words:
        tokens = [f"W{(words_so_far + j) % 997}" if j == 0
                  else f"x{(words_so_far + j) % 997}"
                  for j in range(sent_len)]
        sentences.append(" ".join(tokens) + ".")
        words_so_far += sent_len

    paras = []
    sents_per_para = max(1, para_size // sent_len)
    for start in range(0, len(sentences), sents_per_para):
        paras.append(" ".join(sentences[start : start + sents_per_para]))
    return "\n\n".join(paras)


@pytest.mark.unit
class TestChunkTokenDensityAdjustment:
    """
    split_text_into_chunks must account for the document's actual token
    density so that every chunk fits in the model's context window.
    """

    def test_chunks_adjusted_for_dense_tokenization(self):
        """
        Scenario from the production failure (IBM PowerHA PDF):
          - Document: 46 109 words, 69 886 tokens  →  0.66 words/token
          - Configured ratio: 0.75 words/token
          - max_model_len = 32 768, prompt overhead = 200 tokens
          - MAX_INPUT_WORDS = 18 789

        Without adjustment chunks sit at ~18 789 words ≈ 28 468 tokens at
        the document average, but sections with denser content push past
        32 568 (= 32 768 − 200) tokens, triggering a vLLM 400 error.

        After the fix the function accepts document_tokens / document_words
        and shrinks max_words proportionally so that even the densest
        sections stay within the context window.
        """
        max_model_len = 32768
        prompt_tokens = 200
        configured_ratio = 0.75
        coeff = 0.3

        # MAX_INPUT_WORDS as currently computed
        max_words = int(
            (max_model_len - prompt_tokens) * configured_ratio / (1 + coeff)
        )

        # Simulate a document whose real ratio is much worse than 0.75.
        # Using 0.50 words/token (realistic for code-heavy / config-heavy
        # technical content).  At this ratio every full-sized chunk would
        # need 18 789 / 0.50 = 37 578 tokens — well over 32 568.
        document_words = 50000
        document_tokens = 100000  # ratio = 0.50
        actual_ratio = document_words / document_tokens

        text = _make_text(document_words)

        chunks = split_text_into_chunks(
            text,
            max_words=max_words,
            overlap_sentences=1,
            document_tokens=document_tokens,
            document_words=document_words,
        )

        assert chunks, "expected at least one chunk"

        max_input_tokens = max_model_len - prompt_tokens
        for i, chunk in enumerate(chunks):
            wc = word_count(chunk)
            estimated_tokens = wc / actual_ratio
            assert estimated_tokens <= max_input_tokens, (
                f"Chunk {i}: {wc} words ≈ {estimated_tokens:.0f} tokens "
                f"(ratio={actual_ratio:.2f}), exceeds "
                f"max_input_tokens={max_input_tokens}"
            )

    def test_no_adjustment_when_ratio_is_favorable(self):
        """
        When the document's actual ratio is better than or equal to the
        configured ratio, max_words must NOT be reduced.
        """
        max_words = 200

        # Actual ratio (0.80) is better than configured (0.75) → no change
        document_words = 8000
        document_tokens = 10000  # ratio = 0.80

        text = _make_text(1000)

        unadjusted = split_text_into_chunks(
            text, max_words=max_words, overlap_sentences=1
        )
        adjusted = split_text_into_chunks(
            text,
            max_words=max_words,
            overlap_sentences=1,
            document_tokens=document_tokens,
            document_words=document_words,
        )

        assert len(adjusted) == len(unadjusted), (
            "Chunk count should be identical when actual ratio ≥ configured ratio"
        )

    def test_adjusted_chunks_smaller_than_unadjusted(self):
        """
        With a worse-than-configured ratio, the adjusted split must produce
        chunks whose maximum word count is strictly smaller than the
        unadjusted max_words.
        """
        max_words = 500
        document_words = 10000
        document_tokens = 20000  # ratio = 0.50, configured = 0.75

        text = _make_text(5000)

        chunks = split_text_into_chunks(
            text,
            max_words=max_words,
            overlap_sentences=1,
            document_tokens=document_tokens,
            document_words=document_words,
        )

        max_chunk_wc = max(word_count(c) for c in chunks)
        expected_adjusted = int(max_words * 0.50 / 0.75)
        assert max_chunk_wc <= expected_adjusted, (
            f"Largest chunk has {max_chunk_wc} words, "
            f"expected ≤ {expected_adjusted} after ratio adjustment"
        )
