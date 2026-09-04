"""
Utility functions for text chunking in asynchronous summarization jobs.

Implements paragraph-first, sentence-fallback chunking strategy with overlap
for processing large documents that exceed the context window.
"""

import re
from typing import List, Tuple
from sentence_splitter import SentenceSplitter

from common.misc_utils import get_logger
from summarize.settings import settings
from summarize.summ_utils import word_count, MAX_INPUT_WORDS

logger = get_logger("chunk_utils")

# Initialize sentence splitter (supports multiple languages)
sentence_splitter = SentenceSplitter(language='en')


def split_text_into_chunks(
    text: str,
    max_words: int | None = None,
    overlap_sentences: int | None = None
) -> List[str]:
    """
    Split text into chunks using paragraph-first, sentence-fallback strategy.
    
    Strategy:
    1. Split on paragraph boundaries (double newlines)
    2. Greedily pack paragraphs into chunks
    3. If a single paragraph exceeds max_words, fall back to sentence-level splitting
    4. Add overlap between chunks (last N sentences of previous chunk)
    
    Args:
        text: Input text to split
        max_words: Maximum words per chunk (defaults to MAX_INPUT_WORDS)
        overlap_sentences: Number of sentences to overlap (defaults to config)
        
    Returns:
        List of text chunks
    """
    if max_words is None:
        max_words = MAX_INPUT_WORDS
    
    if overlap_sentences is None:
        overlap_sentences = settings.summarize.chunk_overlap_sentences
    
    # Split into paragraphs
    paragraphs = re.split(r'\n\n+', text.strip())
    paragraphs = [p.strip() for p in paragraphs if p.strip()]
    
    if not paragraphs:
        return []
    
    chunks = []
    current_chunk = []
    current_word_count = 0
    previous_sentences = []  # For overlap
    
    for paragraph in paragraphs:
        para_word_count = word_count(paragraph)
        
        # Check if single paragraph exceeds max_words
        if para_word_count > max_words:
            # Finalize current chunk if it has content
            if current_chunk:
                chunk_text = '\n\n'.join(current_chunk)
                chunks.append(chunk_text)
                # Extract last N sentences for overlap
                previous_sentences = _extract_last_sentences(chunk_text, overlap_sentences)
                current_chunk = []
                current_word_count = 0
            
            # Split oversized paragraph into sentences
            sentence_chunks = _split_paragraph_into_chunks(
                paragraph, 
                max_words, 
                overlap_sentences,
                previous_sentences
            )
            chunks.extend(sentence_chunks)
            
            # Update previous_sentences from last chunk
            if sentence_chunks:
                previous_sentences = _extract_last_sentences(sentence_chunks[-1], overlap_sentences)
            
            continue
        
        # Check if adding this paragraph would exceed max_words
        if current_word_count + para_word_count > max_words and current_chunk:
            # Finalize current chunk
            chunk_text = '\n\n'.join(current_chunk)
            chunks.append(chunk_text)
            
            # Extract last N sentences for overlap
            previous_sentences = _extract_last_sentences(chunk_text, overlap_sentences)
            
            # Start new chunk with overlap
            if previous_sentences:
                overlap_text = ' '.join(previous_sentences)
                current_chunk = [overlap_text, paragraph]
                current_word_count = word_count(overlap_text) + para_word_count
            else:
                current_chunk = [paragraph]
                current_word_count = para_word_count
        else:
            # Add paragraph to current chunk
            if not current_chunk and previous_sentences:
                # First paragraph of new chunk - add overlap
                overlap_text = ' '.join(previous_sentences)
                current_chunk = [overlap_text, paragraph]
                current_word_count = word_count(overlap_text) + para_word_count
            else:
                current_chunk.append(paragraph)
                current_word_count += para_word_count
    
    # Add final chunk if it has content
    if current_chunk:
        chunk_text = '\n\n'.join(current_chunk)
        chunks.append(chunk_text)
    
    logger.info(f"Split text into {len(chunks)} chunks (max {max_words} words per chunk)")
    return chunks


def _split_paragraph_into_chunks(
    paragraph: str,
    max_words: int,
    overlap_sentences: int,
    previous_sentences: List[str]
) -> List[str]:
    """
    Split a single oversized paragraph into sentence-based chunks.
    
    Args:
        paragraph: Paragraph text to split
        max_words: Maximum words per chunk
        overlap_sentences: Number of sentences to overlap
        previous_sentences: Sentences from previous chunk for overlap
        
    Returns:
        List of sentence-based chunks
    """
    # Split into sentences
    sentences = sentence_splitter.split(paragraph)
    sentences = [s.strip() for s in sentences if s.strip()]
    
    if not sentences:
        return []
    
    chunks = []
    current_chunk = []
    current_word_count = 0
    
    # Add overlap from previous chunk if available
    if previous_sentences:
        overlap_text = ' '.join(previous_sentences)
        current_chunk = [overlap_text]
        current_word_count = word_count(overlap_text)
    
    for sentence in sentences:
        sentence_word_count = word_count(sentence)
        
        # If single sentence exceeds max_words, include it anyway (edge case)
        if sentence_word_count > max_words:
            if current_chunk:
                chunks.append(' '.join(current_chunk))
                current_chunk = []
                current_word_count = 0
            chunks.append(sentence)
            continue
        
        # Check if adding this sentence would exceed max_words
        if current_word_count + sentence_word_count > max_words and current_chunk:
            # Finalize current chunk
            chunks.append(' '.join(current_chunk))
            
            # Start new chunk with overlap
            overlap = current_chunk[-overlap_sentences:] if len(current_chunk) >= overlap_sentences else current_chunk
            current_chunk = overlap + [sentence]
            current_word_count = sum(word_count(s) for s in current_chunk)
        else:
            # Add sentence to current chunk
            current_chunk.append(sentence)
            current_word_count += sentence_word_count
    
    # Add final chunk if it has content
    if current_chunk:
        chunks.append(' '.join(current_chunk))
    
    return chunks


def _extract_last_sentences(text: str, n: int) -> List[str]:
    """
    Extract the last N sentences from text for overlap.
    
    Args:
        text: Text to extract sentences from
        n: Number of sentences to extract
        
    Returns:
        List of last N sentences
    """
    if n <= 0:
        return []
    
    sentences = sentence_splitter.split(text)
    sentences = [s.strip() for s in sentences if s.strip()]
    
    return sentences[-n:] if len(sentences) >= n else sentences


def estimate_chunk_summary_tokens(
    num_chunks: int,
    chunk_max_tokens: int
) -> int:
    """
    Estimate total tokens needed for all chunk summaries.
    
    Args:
        num_chunks: Number of chunks
        chunk_max_tokens: Maximum tokens per chunk summary
        
    Returns:
        Estimated total tokens for all chunk summaries
    """
    return num_chunks * chunk_max_tokens


def build_merge_messages(
    merged_chunk_summaries: str,
    target_words: int | None,
    min_words: int | None,
    max_words: int | None
) -> List[dict]:
    """
    Build messages for the merge step that combines chunk summaries.
    
    Args:
        merged_chunk_summaries: Concatenated chunk summaries
        target_words: Target word count for final summary (required for chunked)
        min_words: Minimum acceptable word count (required for chunked)
        max_words: Maximum acceptable word count (required for chunked)
        
    Returns:
        List of message dicts for LLM
    """
    system_prompt = settings.summarize.merge_system_prompt
    
    # For chunked summarization, target_words should always be set
    # Use defaults if somehow None
    if target_words is None:
        target_words = 500
    if min_words is None:
        min_words = int(target_words * 0.85)
    
    user_prompt = settings.summarize.merge_user_prompt.format(
        target_words=target_words,
        min_words=min_words,
        merged_chunk_summaries=merged_chunk_summaries
    )
    
    return [
        {"role": "system", "content": system_prompt},
        {"role": "user", "content": user_prompt}
    ]

# Made with Bob

