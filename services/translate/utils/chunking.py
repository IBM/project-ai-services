"""
Token-based document chunker for translation.

Splits text into ``TranslationChunk`` objects that fit within ``CHUNK_TOKEN_BUDGET``:
- Blocks are formed by splitting on ``\\n\\n``, then greedily packed by token count.
- Oversized prose blocks fall back to sentence-level splitting via ``SentenceSplitter``.
- GFM tables are never split — they occupy their own chunk even if oversized.
- Each chunk carries ``join_after`` (``"paragraph"`` or ``"sentence"``) to drive
  correct reassembly after translation.
"""

import asyncio
from typing import Optional

from sentence_splitter import SentenceSplitter

from common.lang_utils import to_sentence_splitter_lang
from common.llm_utils import tokenize_with_llm
from common.misc_utils import get_logger
from translate.models import TranslationChunk
from translate.utils.llm import get_chunk_token_budget

logger = get_logger("chunking")

# Conservative upper-bound token cost for each "\n\n" separator added by join().
_SEPARATOR_TOKEN_ESTIMATE = 2


def _is_table_block(block: str) -> bool:
    """Return True if *block* looks like a GFM markdown table."""
    stripped_lines = [ln.strip() for ln in block.splitlines() if ln.strip()]
    if len(stripped_lines) < 2:
        return False
    return stripped_lines[0].startswith("|") and stripped_lines[1].startswith("|")


def _count_tokens_sync(text: str, llm_endpoint: str) -> int:
    """Synchronous token count — wraps tokenize_with_llm (returns a list)."""
    return len(tokenize_with_llm(text, llm_endpoint))


async def _count_tokens(text: str, llm_endpoint: str) -> int:
    """Async wrapper: runs the blocking tokenise call in the default thread-pool."""
    return await asyncio.to_thread(_count_tokens_sync, text, llm_endpoint)


async def _pack_sentences_greedily(
    sentences: list[str],
    budget: int,
    llm_endpoint: str,
    start_index: int,
    parent_join_after: str,
) -> list[TranslationChunk]:
    """
    Greedily pack *sentences* into chunks whose token count stays ≤ *budget*.

    All produced chunks get ``join_after="sentence"`` except the very last one,
    which inherits *parent_join_after* (so the block boundary after the whole
    oversized paragraph is preserved correctly).
    """
    chunks: list[TranslationChunk] = []
    current_sentences: list[str] = []
    running_tokens = 0
    idx = start_index

    for sentence in sentences:
        s_tokens = await _count_tokens(sentence, llm_endpoint)
        if running_tokens + s_tokens > budget and current_sentences:
            chunk_text = " ".join(current_sentences)
            chunks.append(
                TranslationChunk(
                    index=idx,
                    text=chunk_text,
                    join_after="sentence",
                    token_count=running_tokens,
                )
            )
            idx += 1
            current_sentences = [sentence]
            running_tokens = s_tokens
        else:
            current_sentences.append(sentence)
            running_tokens += s_tokens

    if current_sentences:
        chunk_text = " ".join(current_sentences)
        chunks.append(
            TranslationChunk(
                index=idx,
                text=chunk_text,
                join_after=parent_join_after,
                token_count=running_tokens,
            )
        )

    return chunks


async def build_translation_chunks(
    text: str,
    llm_endpoint: str,
    source_language_code: Optional[str] = None,
) -> list[TranslationChunk]:
    """
    Split *text* into ``TranslationChunk`` objects ready for concurrent translation.

    Args:
        text: Full document text (may be markdown).
        llm_endpoint: vLLM endpoint used for ``/tokenize`` calls.
        source_language_code: Resolved ISO-639-1 code (e.g. ``"DE"``) used to pick
            the correct ``SentenceSplitter`` language.  Defaults to ``"en"`` if None
            or unresolvable (lingua fell below confidence threshold).

    Returns:
        Ordered list of ``TranslationChunk`` objects (index 0 … N-1).
    """
    budget = get_chunk_token_budget()
    splitter_lang = to_sentence_splitter_lang(source_language_code or "EN")

    # Step 1 — split on paragraph boundaries
    raw_blocks = text.split("\n\n")
    blocks = [b.strip() for b in raw_blocks if b.strip()]

    if not blocks:
        return []

    chunks: list[TranslationChunk] = []
    current_blocks: list[str] = []
    running_tokens = 0
    chunk_index = 0
    splitter = SentenceSplitter(language=splitter_lang)

    for block in blocks:
        is_table = _is_table_block(block)
        block_tokens = await _count_tokens(block, llm_endpoint)

        # Step 2 — greedy packing
        if block_tokens > budget:
            # --- Oversized block path ---
            # Close whatever is in the current running chunk first.
            if current_blocks:
                separator_tokens = _SEPARATOR_TOKEN_ESTIMATE * (len(current_blocks) - 1)
                chunks.append(
                    TranslationChunk(
                        index=chunk_index,
                        text="\n\n".join(current_blocks),
                        join_after="paragraph",
                        token_count=running_tokens + separator_tokens,
                    )
                )
                chunk_index += 1
                current_blocks = []
                running_tokens = 0

            if is_table:
                # Table atomicity: allow oversized chunk rather than split.
                chunks.append(
                    TranslationChunk(
                        index=chunk_index,
                        text=block,
                        join_after="paragraph",
                        token_count=block_tokens,
                    )
                )
                chunk_index += 1
            else:
                # Step 3 — sentence-level fallback for oversized prose blocks.
                sentences = splitter.split(text=block)
                sentence_chunks = await _pack_sentences_greedily(
                    sentences=sentences,
                    budget=budget,
                    llm_endpoint=llm_endpoint,
                    start_index=chunk_index,
                    parent_join_after="paragraph",
                )
                for sc in sentence_chunks:
                    sc.index = chunk_index
                    chunk_index += 1
                chunks.extend(sentence_chunks)
        else:
            # Normal packing: would adding this block overflow the running chunk?
            # Account for the one "\n\n" separator that join() will insert between
            # the existing content and the new block.
            separator_cost = _SEPARATOR_TOKEN_ESTIMATE if current_blocks else 0
            if running_tokens + separator_cost + block_tokens > budget and current_blocks:
                separator_tokens = _SEPARATOR_TOKEN_ESTIMATE * (len(current_blocks) - 1)
                chunks.append(
                    TranslationChunk(
                        index=chunk_index,
                        text="\n\n".join(current_blocks),
                        join_after="paragraph",
                        token_count=running_tokens + separator_tokens,
                    )
                )
                chunk_index += 1
                current_blocks = [block]
                running_tokens = block_tokens
            else:
                current_blocks.append(block)
                running_tokens += block_tokens

    # Flush the final open chunk.
    if current_blocks:
        separator_tokens = _SEPARATOR_TOKEN_ESTIMATE * (len(current_blocks) - 1)
        chunks.append(
            TranslationChunk(
                index=chunk_index,
                text="\n\n".join(current_blocks),
                join_after="paragraph",
                token_count=running_tokens + separator_tokens,
            )
        )

    logger.debug(
        f"Chunked document into {len(chunks)} chunk(s) "
        f"(budget={budget} tokens, splitter_lang={splitter_lang})"
    )
    return chunks

