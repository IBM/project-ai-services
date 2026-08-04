"""
Document-processing orchestrator.

Responsibilities:
- process_converted_document — load Docling JSON → text + table extraction
- clean_intermediate_files   — remove per-doc staging artefacts
- process_documents          — full pipeline: convert → process → chunk → index
- chunk_text / chunk_tables / chunk_single_file / flush_chunk
- split_text_into_token_chunks
- count_chunks / merge_chunked_documents
"""

import json
import shutil
import time
from concurrent.futures import ProcessPoolExecutor, as_completed
from pathlib import Path

from docling_core.types.doc.document import DoclingDocument
from sentence_splitter import SentenceSplitter

from common.lang_utils import LanguageCodes, to_sentence_splitter_lang
from common.llm_utils import tqdm_wrapper
from common.misc_utils import (
    get_logger,
    get_utc_timestamp,
    table_chunk_suffix,
    table_suffix,
    text_chunk_suffix,
    text_suffix,
)
from common.thread_utils import ContextAwareThreadPoolExecutor
from digitize.utils.db import get_status_manager
from digitize.models import DocStatus, JobStatus
from digitize.parsing.converter import convert_document
from digitize.parsing.pdf import get_document_page_count
from digitize.processing.language import (
    collect_header_font_sizes,
    count_tokens,
    detect_document_language,
    get_header_level,
)
from digitize.processing.tables import process_table
from digitize.processing.text import process_text, process_text_docx
from digitize.settings import settings

logger = get_logger("processing.orchestrator")

WORKER_SIZE = settings.digitize.doc_worker_size
HEAVY_DOC_CONVERT_WORKER_SIZE = settings.digitize.heavy_doc_convert_worker_size
HEAVY_DOC_PAGE_THRESHOLD = settings.digitize.heavy_doc_page_threshold


# ---------------------------------------------------------------------------
# Token-level chunking helpers
# ---------------------------------------------------------------------------

def split_text_into_token_chunks(text, emb_endpoint, max_tokens=512, overlap=50, language=LanguageCodes.ENGLISH):
    """
    Split text into token-based chunks using sentence boundaries.

    Args:
        text: The text to split
        emb_endpoint: Embedding endpoint for token counting
        max_tokens: Maximum tokens per chunk
        overlap: Number of tokens to overlap between chunks
        language: Language code ('en', 'de', 'it', 'fr'). Defaults to 'en'.

    Returns:
        List of text chunks
    """
    logger.debug(f"Using language for chunking: {language}")

    sentences = SentenceSplitter(language=language).split(text)
    chunks = []
    current_chunk = []
    current_token_count = 0

    for sentence in sentences:
        token_len = count_tokens(sentence, emb_endpoint)

        if current_token_count + token_len > max_tokens:
            # save current chunk
            chunk_text = " ".join(current_chunk)
            chunks.append(chunk_text)
            # overlap logic (optional)
            if overlap > 0 and len(current_chunk) > 0:
                overlap_text = current_chunk[-1]
                current_chunk = [overlap_text]
                current_token_count = count_tokens(overlap_text, emb_endpoint)
            else:
                current_chunk = []
                current_token_count = 0

        current_chunk.append(sentence)
        current_token_count += token_len

    # flush last
    if current_chunk:
        chunk_text = " ".join(current_chunk)
        chunks.append(chunk_text)

    return chunks


def flush_chunk(current_chunk, chunks, emb_endpoint, max_tokens, language=LanguageCodes.ENGLISH):
    content = current_chunk["content"].strip()
    if not content:
        return

    # Split content into token chunks
    token_chunks = split_text_into_token_chunks(content, emb_endpoint, max_tokens=max_tokens, language=language)

    for i, part in enumerate(token_chunks):
        chunk = {
            "chapter_title": current_chunk["chapter_title"],
            "section_title": current_chunk["section_title"],
            "subsection_title": current_chunk["subsection_title"],
            "subsubsection_title": current_chunk["subsubsection_title"],
            "content": part,
            "page_range": sorted(set(current_chunk["page_range"])),
            "source_nodes": current_chunk["source_nodes"].copy(),
        }
        if len(token_chunks) > 1:
            chunk["part_id"] = i + 1
        chunks.append(chunk)

    # Reset current_chunk after flushing
    current_chunk["chapter_title"] = ""
    current_chunk["section_title"] = ""
    current_chunk["subsection_title"] = ""
    current_chunk["subsubsection_title"] = ""
    current_chunk["content"] = ""
    current_chunk["page_range"] = []
    current_chunk["source_nodes"] = []


def chunk_text(input_path, out_path, emb_endpoint, max_tokens=512, doc_id=None, language=LanguageCodes.ENGLISH):
    """
    Chunk text content from a document into smaller pieces based on token limits.

    Args:
        input_path: Path to the text JSON file
        out_path: Output directory path
        emb_endpoint: Embedding endpoint for token counting
        max_tokens: Maximum tokens per chunk
        doc_id: Document ID
        language: Language code for sentence splitting (detected from document text)
    """
    t0 = time.time()
    processed_chunk_json_path = (Path(out_path) / f"{doc_id}{text_chunk_suffix}")

    try:
        with open(input_path, "r") as f:
            data = json.load(f)

            font_size_levels = collect_header_font_sizes(data)

            chunks = []
            current_chunk = {
                "chapter_title": None,
                "section_title": None,
                "subsection_title": None,
                "subsubsection_title": None,
                "content": "",
                "page_range": [],
                "source_nodes": [],
            }

            current_chapter = None
            current_section = None
            current_subsection = None
            current_subsubsection = None

            for idx, block in enumerate(tqdm_wrapper(data, desc=f"Chunking text from '{input_path}'")):
                label = block.get("label")
                text = block.get("text", "").strip()
                page_no = block.get("page", 0)
                ref = f"#texts/{idx}"

                if label == "section_header":
                    level, full_title = get_header_level(text, block.get("font_size"), font_size_levels)
                    if level == 1:
                        current_chapter = full_title
                        current_section = None
                        current_subsection = None
                        current_subsubsection = None
                    elif level == 2:
                        current_section = full_title
                        current_subsection = None
                        current_subsubsection = None
                    elif level == 3:
                        current_subsection = full_title
                        current_subsubsection = None
                    else:
                        current_subsubsection = full_title

                    flush_chunk(current_chunk, chunks, emb_endpoint, max_tokens, language)
                    current_chunk["chapter_title"] = current_chapter
                    current_chunk["section_title"] = current_section
                    current_chunk["subsection_title"] = current_subsection
                    current_chunk["subsubsection_title"] = current_subsubsection

                elif label in {"text", "list_item", "code", "formula"}:
                    if current_chunk["chapter_title"] is None:
                        current_chunk["chapter_title"] = current_chapter
                    if current_chunk["section_title"] is None:
                        current_chunk["section_title"] = current_section
                    if current_chunk["subsection_title"] is None:
                        current_chunk["subsection_title"] = current_subsection
                    if current_chunk["subsubsection_title"] is None:
                        current_chunk["subsubsection_title"] = current_subsubsection

                    if label == 'code':
                        current_chunk["content"] += f"```\n{text}\n``` "
                    elif label == 'formula':
                        current_chunk["content"] += f"${text}$ "
                    else:
                        current_chunk["content"] += f"{text} "
                    if page_no is not None:
                        current_chunk["page_range"].append(page_no)
                    current_chunk["source_nodes"].append(ref)
                else:
                    logger.debug(f'Skipping adding "{label}".')

            # Flush any remaining content
            flush_chunk(current_chunk, chunks, emb_endpoint, max_tokens, language)

        # Save the processed chunks to the output file
        with open(processed_chunk_json_path, "w") as f:
            json.dump(chunks, f, indent=2)

        elapsed = time.time() - t0
        logger.debug(f"{len(chunks)} text chunks saved to {processed_chunk_json_path} in {elapsed:.2f}s")
        return processed_chunk_json_path, elapsed
    except Exception as e:
        logger.error(f"Error chunking text from '{input_path}': {e}")
        return None, None


def chunk_tables(input_path, out_path, emb_endpoint, max_tokens=512, doc_id=None, language=LanguageCodes.ENGLISH):
    """
    Chunk table summaries into smaller pieces if they exceed token limits.
    Called internally by chunk_single_file() for sequential processing.

    Args:
        input_path: Path to the table JSON file
        out_path: Output directory path
        emb_endpoint: Embedding endpoint for token counting
        max_tokens: Maximum tokens per chunk
        doc_id: Document ID
        language: Language code for sentence splitting (detected from document text)
    """
    t0 = time.time()
    processed_table_chunk_json_path = (Path(out_path) / f"{doc_id}{table_chunk_suffix}")

    try:
        with open(input_path, "r") as f:
            tab_data = json.load(f)

        chunked_tables = []
        tables_chunked_count = 0

        if tab_data:
            tab_data_list = list(tab_data.values())

            for block in tqdm_wrapper(tab_data_list, desc=f"Chunking tables of '{input_path}'"):
                caption = block.get('caption', '')
                summary = block.get("summary", '')
                page_number = block.get('page_number')

                summary_token_count = count_tokens(summary, emb_endpoint)

                if summary_token_count > max_tokens:
                    tables_chunked_count += 1
                    chunks = split_text_into_token_chunks(summary, emb_endpoint, max_tokens=max_tokens, overlap=50, language=language)

                    for chunk_part_idx, chunk in enumerate(chunks):
                        chunked_tables.append({
                            "content": chunk,
                            "caption": caption,
                            "page_number": page_number,
                        })
                else:
                    chunked_tables.append({
                        "content": summary,
                        "caption": caption,
                        "page_number": page_number,
                    })

        with open(processed_table_chunk_json_path, "w") as f:
            json.dump(chunked_tables, f, indent=2)

        elapsed = time.time() - t0
        logger.debug(f"Chunked {len(tab_data)} tables into {len(chunked_tables)} chunks in {elapsed:.2f}s")
        return processed_table_chunk_json_path, elapsed
    except Exception as e:
        logger.error(f"Error chunking tables from '{input_path}': {e}")
        return None, None


def chunk_single_file(input_path, table_json_path, out_path, emb_endpoint, max_tokens=512, doc_id=None, language=LanguageCodes.ENGLISH):
    """
    Orchestrates chunking of both text and tables for a single document.

    Args:
        input_path: Path to the text JSON file
        table_json_path: Path to the table JSON file
        out_path: Output directory path
        emb_endpoint: Embedding endpoint for token counting
        max_tokens: Maximum tokens per chunk
        doc_id: Document ID
        language: Language code for sentence splitting (detected from document text)
    """
    t0 = time.time()
    try:
        sentence_splitter_lang = to_sentence_splitter_lang(language)
        text_chunk_json, text_chunk_time = chunk_text(input_path, out_path, emb_endpoint, max_tokens, doc_id, sentence_splitter_lang)
        table_chunk_json, table_chunk_time = chunk_tables(table_json_path, out_path, emb_endpoint, max_tokens, doc_id, sentence_splitter_lang)
        total_time = time.time() - t0
        return text_chunk_json, table_chunk_json, total_time
    except Exception as e:
        logger.error(f"Error chunking document '{input_path}': {e}")
        return None, None, None


def count_chunks(in_txt_f, in_tab_f):
    """Count total chunks from text and table JSON files without creating document objects."""
    with open(in_txt_f, "r") as f:
        txt_data = json.load(f)
    with open(in_tab_f, "r") as f:
        tab_data = json.load(f)
    txt_count = len(txt_data) if txt_data else 0
    tab_count = len(tab_data) if tab_data else 0
    return txt_count + tab_count


def merge_chunked_documents(in_txt_chunk_f, in_tab_chunk_f, orig_fn):
    """
    Merge pre-chunked text and table documents into final chunk list.
    """
    with open(in_txt_chunk_f, "r") as f:
        txt_data = json.load(f)
    with open(in_tab_chunk_f, "r") as f:
        tab_data = json.load(f)

    created_at = get_utc_timestamp()

    txt_docs = []
    if len(txt_data):
        for txt_idx, block in enumerate(txt_data):
            meta_info = ''
            if block.get('chapter_title'):
                meta_info += f"Chapter: {block.get('chapter_title')} "
            if block.get('section_title'):
                meta_info += f"Section: {block.get('section_title')} "
            if block.get('subsection_title'):
                meta_info += f"Subsection: {block.get('subsection_title')} "
            if block.get('subsubsection_title'):
                meta_info += f"Subsubsection: {block.get('subsubsection_title')} "

            page_range = block.get("page_range", [])
            page_number = page_range[0] if page_range and len(page_range) > 0 else None

            txt_docs.append({
                "page_content": f'{meta_info}\n{block.get("content")}' if meta_info != '' else block.get("content"),
                "filename": orig_fn,
                "type": "text",
                "source": meta_info,
                "language": "en",
                "page_number": page_number,
                "chunk_index": txt_idx,
                "created_at": created_at,
            })

    tab_docs = []
    if len(tab_data):
        txt_count = len(txt_docs)
        for tab_idx, block in enumerate(tab_data):
            caption = block.get("caption", "")
            page_number = block.get("page_number")
            content = block.get("content", "")

            def _normalize(text: str) -> str:
                text = text.lower().strip()
                text = ' '.join(text.split())
                return text.replace(' - ', '-').replace(' -', '-').replace('- ', '-')

            page_content = content
            if caption:
                norm_content = _normalize(content)
                if _normalize(caption) not in norm_content:
                    page_content = f"{caption}\n{content}"

            tab_docs.append({
                "page_content": page_content,
                "filename": orig_fn,
                "type": "table",
                "source": caption,
                "page_number": page_number,
                "language": "en",
                "chunk_index": txt_count + tab_idx,
                "created_at": created_at,
            })

    combined_docs = txt_docs + tab_docs
    total_chunks = len(combined_docs)
    for doc in combined_docs:
        doc["total_chunks"] = total_chunks

    logger.debug(f"Merged chunk documents: {total_chunks} total chunks")
    return combined_docs


# ---------------------------------------------------------------------------
# Per-document orchestration
# ---------------------------------------------------------------------------

def process_converted_document(converted_json_path, doc_path, out_path, gen_model, gen_endpoint, emb_endpoint, max_tokens, doc_id):
    """
    Process converted document to extract text and tables.
    No caching - always process fresh.
    Returns detected language along with other results.
    """
    processed_text_json_path = (Path(out_path) / f"{doc_id}{text_suffix}")
    processed_table_json_path = (Path(out_path) / f"{doc_id}{table_suffix}")

    timings: dict[str, float] = {"process_text": 0.0, "process_tables": 0.0}

    try:
        converted_doc = DoclingDocument.load_from_json(Path(converted_json_path))
        if not converted_doc:
            raise Exception("failed to load converted json into Docling Document")

        file_ext = Path(doc_path).suffix.lower()
        if file_ext == '.docx':
            page_count, process_time = process_text_docx(converted_doc, doc_path, processed_text_json_path)
        else:
            page_count, process_time = process_text(converted_doc, doc_path, processed_text_json_path)
        timings["process_text"] = process_time

        document_language = LanguageCodes.ENGLISH
        try:
            with open(processed_text_json_path, "r") as f:
                text_data = json.load(f)
                document_language = detect_document_language(text_data)
                logger.info(f"Detected document language: {document_language}")
        except Exception as e:
            logger.warning(f"Failed to detect document language, using default {LanguageCodes.ENGLISH}: {e}")

        table_count, process_time = process_table(
            converted_doc, doc_path, processed_table_json_path, gen_model, gen_endpoint, document_language
        )
        timings["process_tables"] = process_time

        return processed_text_json_path, processed_table_json_path, page_count, table_count, timings, document_language
    except Exception as e:
        logger.error(f"Error processing converted document: {doc_path}. Details: {e}", exc_info=True)
        return None, None, None, None, None, None


def clean_intermediate_files(doc_id, out_path):
    """Remove intermediate files but keep <doc_id>.json"""
    for pattern in [f"{doc_id}{text_suffix}", f"{doc_id}{table_suffix}", f"{doc_id}{text_chunk_suffix}", f"{doc_id}{table_chunk_suffix}"]:
        file_path = Path(out_path) / pattern
        if file_path.exists():
            try:
                if file_path.is_dir():
                    shutil.rmtree(file_path)
                else:
                    file_path.unlink()
            except Exception as e:
                logger.warning(f"Failed to clean up {file_path}: {e}")


# ---------------------------------------------------------------------------
# Full batch pipeline
# ---------------------------------------------------------------------------

def process_documents(input_paths, out_path, llm_model, llm_endpoint, emb_endpoint, max_tokens, job_id, doc_id_dict, indexing_callback=None):
    """
    Process documents for ingestion pipeline.
    Each request is treated as fresh.

    Args:
        input_paths: List of input file paths
        out_path: Output directory path
        llm_model: LLM model name
        llm_endpoint: LLM endpoint URL
        emb_endpoint: Embedding endpoint URL
        max_tokens: Maximum tokens for chunking
        job_id: Job ID for status tracking
        doc_id_dict: Mapping of filenames to document IDs
        indexing_callback: Optional callback function to index chunks immediately after chunking.
                          Signature: callback(doc_id: str, chunks: list, path: str) -> bool
    """
    # Partition files into light and heavy based on page count
    light_files, heavy_files = [], []
    for path in input_paths:
        pg_count = get_document_page_count(path)
        if pg_count >= HEAVY_DOC_PAGE_THRESHOLD:
            heavy_files.append(path)
        else:
            light_files.append(path)

    status_mgr = get_status_manager(job_id)

    def _run_batch(batch_paths, convert_worker, max_worker, doc_id_dict, indexing_callback=None):
        batch_stats = {}
        document_language = LanguageCodes.ENGLISH
        if not batch_paths:
            return batch_stats

        with ProcessPoolExecutor(max_workers=convert_worker) as converter_executor, \
             ContextAwareThreadPoolExecutor(max_workers=max_worker) as processor_executor, \
             ContextAwareThreadPoolExecutor(max_workers=max_worker) as chunker_executor, \
             ContextAwareThreadPoolExecutor(max_workers=max_worker) as indexer_executor:

            # A. Submit Conversions
            conversion_futures = {}
            process_futures = {}
            chunk_futures = {}
            indexing_futures = {}  # Track indexing futures

            for path in batch_paths:
                file_name = ""
                doc_id = doc_id_dict.get(Path(path).name)
                if doc_id is None:
                    file_name = path
                else:
                    file_name = doc_id
                future = converter_executor.submit(convert_document, path, out_path, file_name)
                conversion_futures[future] = path
                if doc_id is not None:
                    logger.debug(f"Submitted for conversion: updating job & doc metadata to IN_PROGRESS for document: {doc_id}")
                    status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.IN_PROGRESS})
                    status_mgr.update_job_progress(doc_id, DocStatus.IN_PROGRESS, JobStatus.IN_PROGRESS)

            process_futures = {}
            chunk_futures = {}

            # B. Handle Conversions -> Submit Processing
            for fut in as_completed(conversion_futures):
                path = conversion_futures[fut]
                doc_id = doc_id_dict.get(Path(path).name)
                try:
                    converted_json, conv_time = fut.result()
                    if not converted_json:
                        if doc_id is not None:
                            logger.error(f"Conversion failed for {path}: converted_json is None")
                            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error="Failed to convert document: conversion returned None")
                            status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.FAILED, error="Failed to convert document: conversion returned None")
                        continue

                    batch_stats[path] = {"timings": {"digitizing": round(float(conv_time or 0), 2)}}

                    if doc_id is not None:
                        logger.debug(f"Conversion Done: updating doc & job metadata for document: {doc_id}")
                        status_mgr.update_doc_metadata(doc_id, {
                            "status": DocStatus.DIGITIZED,
                            "timing_in_secs": {**batch_stats[path]["timings"]},
                        })
                        status_mgr.update_job_progress(doc_id, DocStatus.DIGITIZED, JobStatus.IN_PROGRESS)

                    p_future = processor_executor.submit(
                        process_converted_document, converted_json, path, out_path,
                        llm_model, llm_endpoint, emb_endpoint, max_tokens, doc_id=doc_id
                    )
                    process_futures[p_future] = str(path)
                except Exception as e:
                    logger.error(f"Error from conversion for {path}: {str(e)}", exc_info=True)
                    batch_stats.pop(path, {})
                    if doc_id is not None:
                        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"failed to convert document: {str(e)}")
                        status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)

            # C. Handle Processing -> Submit Chunking
            for fut in as_completed(process_futures):
                path = process_futures[fut]
                doc_id = doc_id_dict.get(Path(path).name)
                try:
                    txt_json, tab_json, pgs, tabs, timings, document_language = fut.result()

                    if not txt_json or not tab_json:
                        if doc_id is not None:
                            logger.error(f"Processing failed for {path}: txt_json or tab_json is None")
                            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"Failed to process document {doc_id}: processing returned None")
                            status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)
                        batch_stats.pop(path, {})
                        continue

                    total_processing_time = timings["process_text"] + timings["process_tables"]
                    batch_stats[path].update({"page_count": pgs, "table_count": tabs})
                    batch_stats[path]["timings"]["processing"] = round(float(total_processing_time or 0), 2)

                    if doc_id is not None:
                        logger.debug(f"Processing Done: updating doc & job metadata for document: {doc_id}")
                        status_mgr.update_doc_metadata(doc_id, {
                            "status": DocStatus.PROCESSED,
                            "pages": pgs,
                            "tables": tabs,
                            "timing_in_secs": {**batch_stats[path]["timings"]},
                        })
                        status_mgr.update_job_progress(
                            doc_id=doc_id,
                            doc_status=DocStatus.PROCESSED,
                            job_status=JobStatus.IN_PROGRESS,
                        )

                    c_future = chunker_executor.submit(
                        chunk_single_file, txt_json, tab_json, out_path,
                        emb_endpoint, max_tokens, doc_id=doc_id, language=document_language
                    )
                    chunk_futures[c_future] = str(path)
                except Exception as e:
                    if doc_id is not None:
                        logger.error(f"Error from processing for {path}: {str(e)}", exc_info=True)
                        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"failed to process document: {str(e)}")
                        status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)
                    batch_stats.pop(path, {})

            # D. Handle Chunking (both text and tables)
            for fut in as_completed(chunk_futures):
                path = chunk_futures[fut]
                doc_id = doc_id_dict.get(Path(path).name)
                try:
                    text_chunk_json, table_chunk_json, total_time = fut.result()

                    if not text_chunk_json or not table_chunk_json:
                        if doc_id is not None:
                            logger.error(f"Chunking failed for {path}")
                            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"Chunking failed for document {doc_id}")
                            status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)
                        batch_stats.pop(path, {})
                        continue

                    batch_stats[path]["timings"]["chunking"] = round(float(total_time or 0), 2)

                    chunk_count = count_chunks(text_chunk_json, table_chunk_json)
                    batch_stats[path]["chunk_count"] = chunk_count

                    if doc_id is not None:
                        logger.debug(f"Chunking Done: updating doc & job metadata for document: {doc_id}")
                        status_mgr.update_doc_metadata(doc_id, {
                            "status": DocStatus.CHUNKED,
                            "chunks": chunk_count,
                            "timing_in_secs": {**batch_stats[path]["timings"]},
                        })
                        status_mgr.update_job_progress(doc_id, DocStatus.CHUNKED, JobStatus.IN_PROGRESS)

                        if indexing_callback:
                            try:
                                # Create chunks for immediate indexing
                                doc_chunks = merge_chunked_documents(text_chunk_json, table_chunk_json, path)
                                # Inject doc_id into chunks
                                for chunk in doc_chunks:
                                    chunk["doc_id"] = doc_id

                                logger.debug(f"Submitting async indexing for document: {doc_id}")
                                # Submit to indexer executor for async processing
                                index_future = indexer_executor.submit(indexing_callback, doc_id, doc_chunks, path)
                                indexing_futures[index_future] = doc_id
                            except Exception as e:
                                logger.error(f"Error submitting indexing for {doc_id}: {e}", exc_info=True)
                                # Don't fail the entire pipeline if indexing submission fails
                except Exception as e:
                    if doc_id is not None:
                        logger.error(f"Error from chunking for {path}: {str(e)}", exc_info=True)
                        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"failed to chunk document: {str(e)}")
                        status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)
                    batch_stats.pop(path, {})

            # E. Wait for all indexing to complete (non-blocking for chunking)
            if indexing_futures:
                logger.info(f"Waiting for {len(indexing_futures)} indexing operations to complete...")
                for index_fut in as_completed(indexing_futures):
                    doc_id = indexing_futures[index_fut]
                    try:
                        # Get result to ensure any exceptions are raised
                        index_fut.result()
                        logger.debug(f"Indexing completed for document: {doc_id}")
                    except Exception as e:
                        logger.error(f"Indexing failed for document {doc_id}: {e}", exc_info=True)
                        # Error already handled by callback, just log here

        return batch_stats

    # Trigger the batches
    try:
        # Process Light Batch
        l_worker = min(WORKER_SIZE, len(light_files)) if light_files else 0
        l_stats = _run_batch(
            light_files, convert_worker=l_worker, max_worker=l_worker, doc_id_dict=doc_id_dict,
            indexing_callback=indexing_callback
        )

        # Process Heavy Batch
        h_worker = min(WORKER_SIZE, len(heavy_files)) if heavy_files else 0
        h_conv_worker = min(HEAVY_DOC_CONVERT_WORKER_SIZE, len(heavy_files)) if heavy_files else 0
        h_stats = _run_batch(
            heavy_files, convert_worker=h_conv_worker, max_worker=h_worker, doc_id_dict=doc_id_dict,
            indexing_callback=indexing_callback
        )

        # Combine statistics for the final return
        converted_pdf_stats = {**l_stats, **h_stats}

        # Indexing is now done inside _run_batch, so we just return stats
        # No need for post-processing assembly or indexing
        return {}, converted_pdf_stats

    except Exception as e:
        logger.error(f"Error while processing the documents in job {job_id}: {e}", exc_info=True)
        # Final job status will be determined based on the overall documents processed in ingest.py, hence skipping job status update

        # Clean up intermediate files for failed documents
        # Preserve <doc_id>.json even for failed jobs for debugging/GET requests
        try:
            for path in input_paths:
                doc_id = doc_id_dict.get(Path(path).name)
                if doc_id:
                    clean_intermediate_files(doc_id, out_path)
        except Exception as cleanup_error:
            logger.warning(f"Error during cleanup of failed job {job_id}: {cleanup_error}")

        return {}, {}
