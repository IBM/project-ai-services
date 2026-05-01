import json
import time
import logging
import os
import shutil

# Set environment variables before importing third-party libraries
os.environ['GRPC_VERBOSITY'] = 'ERROR'
os.environ['TF_CPP_MIN_LOG_LEVEL'] = '2'

from tqdm import tqdm
from pathlib import Path
from docling_core.types.doc.document import DoclingDocument
from concurrent.futures import as_completed, ProcessPoolExecutor
from sentence_splitter import SentenceSplitter

# Set third-party library log levels before importing project modules
logging.getLogger('docling').setLevel(logging.CRITICAL)

# Import project modules after setting log levels
from common.thread_utils import ContextAwareThreadPoolExecutor
from common.llm_utils import summarize_and_classify_tables, tokenize_with_llm
from common.misc_utils import get_logger, text_suffix, table_suffix, text_chunk_suffix, table_chunk_suffix
from digitize.pdf_utils import get_toc, get_matching_header_lvl, load_pdf_pages, find_text_font_size, get_pdf_page_count, convert_doc
from digitize.status import (
    StatusManager,
    get_utc_timestamp
)
from digitize.types import DocStatus, JobStatus, OutputFormat
from digitize.settings import settings

logger = get_logger("doc_utils")

# Load configuration from settings modules
WORKER_SIZE = settings.digitize.doc_worker_size
HEAVY_PDF_CONVERT_WORKER_SIZE = settings.digitize.heavy_pdf_convert_worker_size
HEAVY_PDF_PAGE_THRESHOLD = settings.digitize.heavy_pdf_page_threshold
POOL_SIZE = settings.common.llm.llm_max_batch_size

is_debug = logger.isEnabledFor(logging.DEBUG)
tqdm_wrapper = tqdm if is_debug else (lambda x, **kwargs: x)

excluded_labels = {
    'page_header', 'page_footer', 'caption', 'reference', 'footnote'
}

def process_text(converted_doc, pdf_path, out_path):
    page_count = 0
    process_time = 0.0

    # Initialize TocHeaders to get the Table of Contents (TOC)
    t0 = time.time()
    toc_headers = None
    try:
        toc_headers, page_count = get_toc(pdf_path)
    except Exception as e:
        logger.debug(f"No TOC found or failed to load TOC: {e}")

    # Load pdf pages one time when TOC headers not found for retrieving the font size of header texts
    pdf_pages = None
    if not toc_headers:
        pdf_pages = load_pdf_pages(pdf_path)
        page_count = len(pdf_pages)

    # --- Text Extraction ---
    if not converted_doc.texts:
        logger.debug(f"No text content found in '{pdf_path}'")
        out_path.write_text(json.dumps([], indent=2), encoding="utf-8")
        return page_count, process_time

    structured_output = []
    last_header_level = 0
    for text_obj in tqdm_wrapper(converted_doc.texts, desc=f"Processing text content of '{pdf_path}'"):
        label = text_obj.label
        if label in excluded_labels:
            continue

        # Check if it's a section header and process TOC or fallback to font size extraction
        if label == "section_header":
            prov_list = text_obj.prov

            for prov in prov_list:
                page_no = prov.page_no

                if toc_headers:
                    header_prefix = get_matching_header_lvl(toc_headers, text_obj.text)
                    if header_prefix:
                        # If TOC matches, use the level from TOC
                        structured_output.append({
                            "label": label,
                            "text": f"{header_prefix} {text_obj.text}",
                            "page": page_no,
                            "font_size": None,  # Font size isn't necessary if TOC matches
                        })
                        last_header_level = len(header_prefix.strip())  # Update last header level
                    else:
                        # If no match, use the previous header level + 1
                        new_header_level = last_header_level + 1
                        structured_output.append({
                            "label": label,
                            "text": f"{'#' * new_header_level} {text_obj.text}",
                            "page": page_no,
                            "font_size": None,  # Font size isn't necessary if TOC matches
                        })
                else:
                    assert pdf_pages is not None
                    matches = find_text_font_size(pdf_pages, text_obj.text, page_no - 1)
                    if len(matches):
                        font_size = 0
                        count = 0
                        for match in matches:
                            font_size += match["font_size"] if match["match_score"] == 100 else 0
                            count += 1 if match["match_score"] == 100 else 0
                        font_size = font_size / count if count else None

                        structured_output.append({
                            "label": label,
                            "text": text_obj.text,
                            "page": page_no,
                            "font_size": round(font_size, 2) if font_size else None
                        })
        else:
            structured_output.append({
                "label": label,
                "text": text_obj.text,
                "page": text_obj.prov[0].page_no,
                "font_size": None
            })

    process_time = time.time() - t0
    out_path.write_text(json.dumps(structured_output, indent=2), encoding="utf-8")

    return page_count, process_time

def extract_table_headers(markdown_table: str) -> list[str]:
    """
    Extract headers from a markdown table by parsing the first row with pipe symbols.
    Handles cases where the first line might be a caption without pipes.

    Args:
        markdown_table: Markdown formatted table string

    Returns:
        List of header strings, or empty list if no headers found
    """
    if not markdown_table or not markdown_table.strip():
        return []

    try:
        lines = markdown_table.strip().split('\n')
        if not lines:
            return []

        # Find the first line that contains pipe symbols (actual table row)
        header_line = None
        for line in lines:
            line = line.strip()
            if '|' in line:
                header_line = line
                break

        if not header_line:
            return []

        # Remove leading and trailing pipes and split by pipe
        if header_line.startswith('|'):
            header_line = header_line[1:]
        if header_line.endswith('|'):
            header_line = header_line[:-1]

        # Split by pipe and strip whitespace from each header
        headers = [h.strip() for h in header_line.split('|')]

        # Filter out empty headers
        headers = [h for h in headers if h]

        return headers
    except Exception as e:
        logger.debug(f"Failed to extract headers from markdown table: {e}")
        return []


def headers_match(headers1: list[str], headers2: list[str]) -> bool:
    """
    Check if two header lists match (case-insensitive, whitespace normalized).

    Args:
        headers1: First list of headers
        headers2: Second list of headers

    Returns:
        True if headers match, False otherwise
    """
    if not headers1 or not headers2:
        return False

    if len(headers1) != len(headers2):
        return False

    # Normalize and compare headers
    normalized1 = [h.lower().strip() for h in headers1]
    normalized2 = [h.lower().strip() for h in headers2]

    return normalized1 == normalized2


def merge_markdown_tables(table1_md: str, table2_md: str) -> str:
    """
    Merge two markdown tables by removing the header from the second table
    and appending its rows to the first table.
    Handles cases where tables might have captions before the actual table.

    Args:
        table1_md: First markdown table (with headers)
        table2_md: Second markdown table (headers will be removed)

    Returns:
        Merged markdown table
    """
    if not table1_md or not table2_md:
        return table1_md or table2_md or ""

    # Split tables into lines
    lines1 = table1_md.strip().split('\n')
    lines2 = table2_md.strip().split('\n')

    # Find where the data rows start in table2 (skip caption, header and separator)
    # Look for the separator line (contains dashes and pipes: |---|---|)
    data_start_idx = 0
    for i, line in enumerate(lines2):
        line = line.strip()
        # Separator line typically contains dashes and pipes: |---|---|
        if '|' in line and '---' in line:
            data_start_idx = i + 1
            break

    # If we found a separator, append only the data rows from table2
    if 0 < data_start_idx < len(lines2):
        merged_lines = lines1 + lines2[data_start_idx:]
        return '\n'.join(merged_lines)

    # Fallback: just append all lines from table2 (shouldn't happen with valid markdown tables)
    return table1_md + '\n' + table2_md


def merge_consecutive_tables(table_dict: dict) -> dict:
    """
    Merge tables that span multiple consecutive pages with matching headers.

    Args:
        table_dict: Dictionary with table index as key and table data as value
                   Each value should have 'markdown', 'caption', and 'page_number' keys

    Returns:
        Dictionary with merged tables, using same structure as input
    """
    if not table_dict:
        return {}

    # Sort tables by index to process in order
    sorted_indices = sorted(table_dict.keys())

    merged_dict = {}
    skip_indices = set()

    for i, idx in enumerate(sorted_indices):
        if idx in skip_indices:
            continue

        current_table = table_dict[idx]
        current_markdown = current_table.get('markdown', '')
        current_page = current_table.get('page_number')
        current_headers = extract_table_headers(current_markdown)

        # Try to merge with subsequent tables on consecutive pages
        merged_markdown = current_markdown
        last_merged_page = current_page
        # look at the next 2 pages
        for j in range(i + 1, min(i + 3, len(sorted_indices))):
            next_idx = sorted_indices[j]
            next_table = table_dict[next_idx]
            next_markdown = next_table.get('markdown', '')
            next_page = next_table.get('page_number')
            next_headers = extract_table_headers(next_markdown)
            # Check if tables are on consecutive pages and have matching headers
            if (next_page is not None and
                last_merged_page is not None and
                next_page == last_merged_page + 1 and
                headers_match(current_headers, next_headers)):
                # Merge the tables
                merged_markdown = merge_markdown_tables(merged_markdown, next_markdown)
                last_merged_page = next_page
                skip_indices.add(next_idx)
                logger.debug(f"Merged table {next_idx} (page {next_page}) into table {idx} (page {current_page})")
            else:
                # Stop looking if pages are not consecutive or headers don't match
                break

        # Store the merged (or original) table
        merged_dict[idx] = {
            'markdown': merged_markdown,
            'caption': current_table.get('caption', ''),
            'page_number': current_page
        }

    return merged_dict

def process_table(converted_doc, pdf_path, out_path, gen_model, gen_endpoint):
    table_count = 0
    process_time = 0.0
    filtered_table_dicts = {}
    t0 = time.time()
    # --- Table Extraction ---
    if not converted_doc.tables:
        logger.debug(f"No tables found in '{pdf_path}'")
        out_path.write_text(json.dumps({}, indent=2), encoding="utf-8")
        return table_count, process_time

    table_dict = {}
    for table_ix, table in enumerate(tqdm_wrapper(converted_doc.tables, desc=f"Processing table content of '{pdf_path}'")):
        table_dict[table_ix] = {}
        # Use Markdown format for better LLM understanding
        table_dict[table_ix]["markdown"] = table.export_to_markdown(doc=converted_doc)
        table_dict[table_ix]["caption"] = table.caption_text(doc=converted_doc)
        table_dict[table_ix]["page_number"] = table.prov[0].page_no if table.prov else None

    # Merge tables that span multiple consecutive pages with matching headers
    logger.debug(f"Merging tables spanning multiple pages for '{pdf_path}'")
    merged_table_dict = merge_consecutive_tables(table_dict)

    table_markdowns = [merged_table_dict[key]["markdown"] for key in sorted(merged_table_dict)]
    table_captions_list = [merged_table_dict[key]["caption"] for key in sorted(merged_table_dict)]
    table_page_numbers = [merged_table_dict[key]["page_number"] for key in sorted(merged_table_dict)]

    # Summarize and classify tables - use markdown directly
    table_summaries, decisions = summarize_and_classify_tables(table_markdowns, gen_model, gen_endpoint, pdf_path)

    filtered_table_dicts = {
        idx: {
            'summary': summary,
            'caption': caption,
            'page_number': page_num
        }
        for idx, (keep, markdown, summary, caption, page_num) in enumerate(zip(decisions, table_markdowns, table_summaries, table_captions_list, table_page_numbers)) if keep
    }
    table_count = len(filtered_table_dicts)
    out_path.write_text(json.dumps(filtered_table_dicts, indent=2), encoding="utf-8")
    process_time = time.time() - t0

    return table_count, process_time

def process_converted_document(converted_json_path, pdf_path, out_path, gen_model, gen_endpoint, emb_endpoint, max_tokens, doc_id):
    """
    Process converted document to extract text and tables.
    No caching - always process fresh.
    """
    processed_text_json_path = (Path(out_path) / f"{doc_id}{text_suffix}")
    processed_table_json_path = (Path(out_path) / f"{doc_id}{table_suffix}")

    timings: dict[str, float] = {"process_text": 0.0, "process_tables": 0.0}

    try:
        converted_doc = None
        page_count = 0
        table_count = 0

        logger.debug("Loading from converted json")

        converted_doc = DoclingDocument.load_from_json(Path(converted_json_path))
        if not converted_doc:
            raise Exception(f"failed to load converted json into Docling Document")

        page_count, process_time = process_text(converted_doc, pdf_path, processed_text_json_path)
        timings["process_text"] = process_time

        table_count, process_time = process_table(converted_doc, pdf_path, processed_table_json_path, gen_model, gen_endpoint)
        timings["process_tables"] = process_time

        return processed_text_json_path, processed_table_json_path, page_count, table_count, timings
    except Exception as e:
        logger.error(f"Error processing converted document for PDF: {pdf_path}. Details: {e}", exc_info=True)

        return None, None, None, None, None

def convert_document(pdf_path, out_path, file_name):
    """
    Convert a single document to JSON format.
    This function runs in a separate process via ProcessPoolExecutor.
    """
    try:
        logger.info(f"Processing '{pdf_path}'")
        converted_json = (Path(out_path) / f"{file_name}.json")
        converted_json_f = str(converted_json)
        logger.debug(f"Converting '{pdf_path}'")
        t0 = time.time()

        converted_doc: DoclingDocument = convert_doc(pdf_path, cache_dir=out_path / file_name)
        converted_doc.save_as_json(str(converted_json_f))

        conversion_time = time.time() - t0
        logger.debug(f"'{pdf_path}' converted")
        return converted_json_f, conversion_time
    except Exception as e:
        logger.error(f"Error converting '{pdf_path}': {e}")
    return None, None

def clean_intermediate_files(doc_id, out_path):
    # Remove intermediate files but keep <doc_id>.json
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
        pg_count = get_pdf_page_count(path)
        if pg_count >= HEAVY_PDF_PAGE_THRESHOLD:
            heavy_files.append(path)
        else:
            light_files.append(path)

    status_mgr = StatusManager(job_id)

    def _run_batch(batch_paths, convert_worker, max_worker, doc_id_dict, indexing_callback=None):
        batch_stats = {}

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
                # Update status to IN_PROGRESS as soon as document is submitted for conversion
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

                    # Update persistence and session stats
                    batch_stats[path] = {"timings": {"digitizing": round(float(conv_time or 0), 2)}}

                    if doc_id is not None:
                        logger.debug(f"Conversion Done: updating doc & job metadata for document: {doc_id}")
                        status_mgr.update_doc_metadata(doc_id, {
                            "status": DocStatus.DIGITIZED,
                            "timing_in_secs": {**batch_stats[path]["timings"]}
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
                    txt_json, tab_json, pgs, tabs, timings = fut.result()

                    if not txt_json or not tab_json:
                        if doc_id is not None:
                            logger.error(f"Processing failed for {path}: txt_json or tab_json is None")
                            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"Failed to process document {doc_id}: processing returned None")
                            status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)
                        batch_stats.pop(path, {})
                        continue

                    total_processing_time = timings["process_text"] + timings["process_tables"]
                    batch_stats[path].update({
                        "page_count": pgs,
                        "table_count": tabs
                    })
                    batch_stats[path]["timings"]["processing"] = round(float(total_processing_time or 0), 2)

                    if doc_id is not None:
                        logger.debug(f"Processing Done: updating doc & job metadata for document: {doc_id}")
                        status_mgr.update_doc_metadata(doc_id, {
                            "status": DocStatus.PROCESSED,
                            "pages": pgs,
                            "tables": tabs,
                            "timing_in_secs": {**batch_stats[path]["timings"]}
                        })
                        status_mgr.update_job_progress(
                            doc_id=doc_id,
                            doc_status=DocStatus.PROCESSED,  # Transitioning within processing
                            job_status=JobStatus.IN_PROGRESS
                    )

                    c_future = chunker_executor.submit(
                        chunk_single_file, txt_json, tab_json, out_path,
                        emb_endpoint, max_tokens, doc_id=doc_id
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
                    # Get both text and table chunk results
                    text_chunk_json, table_chunk_json, total_time = fut.result()

                    # Consolidated error handling: fail document if either text or table chunking fails
                    if not text_chunk_json or not table_chunk_json:
                        if doc_id is not None:
                            logger.error(f"Chunking failed for {path}")
                            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"Chunking failed for document {doc_id}")
                            status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)
                        batch_stats.pop(path, {})
                        continue

                    batch_stats[path]["timings"]["chunking"] = round(float(total_time or 0), 2)

                    # Capture chunk counts in real time and update <doc_id>_metadata.json
                    chunk_count = count_chunks(text_chunk_json, table_chunk_json)
                    batch_stats[path]["chunk_count"] = chunk_count

                    if doc_id is not None:
                        logger.debug(f"Chunking Done: updating doc & job metadata for document: {doc_id}")
                        status_mgr.update_doc_metadata(doc_id, {
                            "status": DocStatus.CHUNKED,
                            "chunks": chunk_count,
                            "timing_in_secs": {**batch_stats[path]["timings"]}
                        })
                        status_mgr.update_job_progress(doc_id, DocStatus.CHUNKED, JobStatus.IN_PROGRESS)

                        # Submit indexing asynchronously if callback is provided
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
        h_conv_worker = min(HEAVY_PDF_CONVERT_WORKER_SIZE, len(heavy_files)) if heavy_files else 0
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

def collect_header_font_sizes(elements):
    """
    elements: list of dicts with at least keys: 'label', 'font_size'
    Returns a sorted list of unique section_header font sizes, descending.
    """
    sizes = {
        el['font_size']
        for el in elements
        if el.get('label') == 'section_header' and el.get('font_size') is not None
    }
    return sorted(sizes, reverse=True)

def get_header_level(text, font_size, sorted_font_sizes):
    """
    Determine header level based on markdown syntax or font size hierarchy.
    """
    text = text.strip()

    # Priority 1: Markdown syntax
    if text.startswith('#'):
        level = len(text.strip()) - len(text.strip().lstrip('#'))
        return level, text.strip().lstrip('#').strip()

    # Priority 2: Font size ranking
    try:
        level = sorted_font_sizes.index(font_size) + 1
    except ValueError:
        # Unknown font size → assign lowest priority
        level = len(sorted_font_sizes)

    return level, text


def count_tokens(text, emb_endpoint):
    token_len = len(tokenize_with_llm(text, emb_endpoint))
    return token_len

def split_text_into_token_chunks(text, emb_endpoint, max_tokens=512, overlap=50):
    sentences = SentenceSplitter(language='en').split(text)
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


def flush_chunk(current_chunk, chunks, emb_endpoint, max_tokens):
    content = current_chunk["content"].strip()
    if not content:
        return

    # Split content into token chunks
    token_chunks = split_text_into_token_chunks(content, emb_endpoint, max_tokens=max_tokens)

    for i, part in enumerate(token_chunks):
        chunk = {
            "chapter_title": current_chunk["chapter_title"],
            "section_title": current_chunk["section_title"],
            "subsection_title": current_chunk["subsection_title"],
            "subsubsection_title": current_chunk["subsubsection_title"],
            "content": part,
            "page_range": sorted(set(current_chunk["page_range"])),
            "source_nodes": current_chunk["source_nodes"].copy()
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


def chunk_text(input_path, out_path, emb_endpoint, max_tokens=512, doc_id=None):
    """
    Chunk text content from a document into smaller pieces based on token limits.
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
                "source_nodes": []
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

                    # Flush current chunk and update
                    flush_chunk(current_chunk, chunks, emb_endpoint, max_tokens)
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
            flush_chunk(current_chunk, chunks, emb_endpoint, max_tokens)

        # Save the processed chunks to the output file
        with open(processed_chunk_json_path, "w") as f:
            json.dump(chunks, f, indent=2)

        elapsed = time.time() - t0
        logger.debug(f"{len(chunks)} text chunks saved to {processed_chunk_json_path} in {elapsed:.2f}s")
        return processed_chunk_json_path, elapsed
    except Exception as e:
        logger.error(f"Error chunking text from '{input_path}': {e}")
        return None, None

def chunk_single_file(input_path, table_json_path, out_path, emb_endpoint, max_tokens=512, doc_id=None):
    """
    Orchestrates chunking of both text and tables for a single document.
    """
    t0 = time.time()

    try:
        # Chunk text content
        text_chunk_json, text_chunk_time = chunk_text(input_path, out_path, emb_endpoint, max_tokens, doc_id)

        # Chunk tables
        table_chunk_json, table_chunk_time = chunk_tables(table_json_path, out_path, emb_endpoint, max_tokens, doc_id)

        total_time = time.time() - t0
        return text_chunk_json, table_chunk_json, total_time
    except Exception as e:
        logger.error(f"Error chunking document '{input_path}': {e}")
        return None, None, None

def chunk_tables(input_path, out_path, emb_endpoint, max_tokens=512, doc_id=None):
    """
    Chunk table summaries into smaller pieces if they exceed token limits.
    Called internally by chunk_single_file() for sequential processing.
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

                # Use summary for chunking - summaries are more concise and meaningful for RAG
                summary_token_count = count_tokens(summary, emb_endpoint)

                if summary_token_count > max_tokens:
                    tables_chunked_count += 1
                    # Chunk the summary
                    chunks = split_text_into_token_chunks(summary, emb_endpoint, max_tokens=max_tokens, overlap=50)

                    for chunk_part_idx, chunk in enumerate(chunks):
                        # TODO: Consider adding chunking properties ("is_chunked", "chunk_part", "total_parts")
                        # in case future requirements need item-level chunk tracking or reassembly logic.
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

        # Save the chunked tables to the output file
        with open(processed_table_chunk_json_path, "w") as f:
            json.dump(chunked_tables, f, indent=2)

        elapsed = time.time() - t0
        logger.debug(f"Chunked {len(tab_data)} tables into {len(chunked_tables)} chunks in {elapsed:.2f}s")
        return processed_table_chunk_json_path, elapsed
    except Exception as e:
        logger.error(f"Error chunking tables from '{input_path}': {e}")
        return None, None

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
    Both inputs are already chunked to fit embedding limits.
    """
    with open(in_txt_chunk_f, "r") as f:
        txt_data = json.load(f)

    with open(in_tab_chunk_f, "r") as f:
        tab_data = json.load(f)

    created_at = get_utc_timestamp()

    # Process text chunks
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

            # Extract page number from page_range (use first page if multiple)
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
                "created_at": created_at
            })

    # Process table chunks
    tab_docs = []
    if len(tab_data):
        txt_count = len(txt_docs)

        for tab_idx, block in enumerate(tab_data):
            caption = block.get("caption", "")
            page_number = block.get("page_number")
            content = block.get("content", "")

            # Smart caption prefixing: only add if caption not already in content
            def _normalize(text: str) -> str:
                # Normalize text: lowercase, collapse whitespace, remove spaces around hyphens for comparison
                text = text.lower().strip()
                text = ' '.join(text.split())  # collapse whitespace
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
                "created_at": created_at
            })

    combined_docs = txt_docs + tab_docs

    # Add total_chunks to all documents
    total_chunks = len(combined_docs)
    for doc in combined_docs:
        doc["total_chunks"] = total_chunks

    logger.debug(f"Merged chunk documents: {total_chunks} total chunks")

    return combined_docs

def convert_document_format(pdf_path: str, out_path: Path, doc_id: str, output_format: OutputFormat):
    logger.info(f"Processing '{pdf_path}'")

    out_dir = Path(out_path)
    out_dir.mkdir(parents=True, exist_ok=True)

    t0 = time.time()

    # Convert PDF → DoclingDocument
    doc_obj = convert_doc(pdf_path, cache_dir=out_path / doc_id)

    conversion_time = time.time() - t0

    # Save requested format
    if output_format == OutputFormat.JSON:
        out_file = out_dir / f"{doc_id}.json"
        doc_obj.save_as_json(str(out_file))

    elif output_format == OutputFormat.MD:
        out_file = out_dir / f"{doc_id}.md"
        out_file.write_text(doc_obj.export_to_markdown(), encoding="utf-8")

    elif output_format == OutputFormat.TEXT:
        out_file = out_dir / f"{doc_id}.txt"
        out_file.write_text(doc_obj.export_to_text(), encoding="utf-8")

    logger.debug(f"Saved converted file to '{out_file}'")
    return str(out_file), conversion_time
