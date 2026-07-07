"""
Table processing: extraction, merging, and summarisation.

Responsibilities:
- process_table  — extract/summarise tables from a converted document
- extract_table_headers — parse markdown table header row
- headers_match — compare two header lists
- merge_markdown_tables — combine two markdown tables by dropping the second header
- merge_consecutive_tables — merge tables spanning consecutive pages
"""

import json
import time
from pathlib import Path

from common.lang_utils import LanguageCodes, get_prompt_for_language
from common.llm_utils import summarize_and_classify_tables, tqdm_wrapper
from common.misc_utils import get_logger
from digitize.settings import settings

logger = get_logger("processing.tables")


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
            'page_number': current_page,
        }

    return merged_dict


def process_table(converted_doc, doc_path, out_path, gen_model, gen_endpoint, document_language=LanguageCodes.ENGLISH):
    table_count = 0
    process_time = 0.0
    filtered_table_dicts = {}
    t0 = time.time()

    # --- Table Extraction ---
    if not converted_doc.tables:
        logger.debug(f"No tables found in '{doc_path}'")
        out_path.write_text(json.dumps({}, indent=2), encoding="utf-8")
        return table_count, process_time

    # Determine if this is a DOCX file
    file_ext = Path(doc_path).suffix.lower()
    is_docx = file_ext == '.docx'

    # Lazy import to avoid circular dependency
    from digitize.parsing.docx import recover_table_caption_from_body_context

    table_dict = {}
    for table_ix, table in enumerate(tqdm_wrapper(converted_doc.tables, desc=f"Processing table content of '{doc_path}'")):
        table_dict[table_ix] = {}
        # Use Markdown format for better LLM understanding
        table_dict[table_ix]["markdown"] = table.export_to_markdown(doc=converted_doc)

        caption = table.caption_text(doc=converted_doc)
        if not caption:
            caption = recover_table_caption_from_body_context(converted_doc, table_ix)

        table_dict[table_ix]["caption"] = caption

        # Get page number from provenance if available (PDF files)
        # For DOCX files, assign sequential page numbers based on table order
        if table.prov and table.prov[0].page_no is not None:
            table_dict[table_ix]["page_number"] = table.prov[0].page_no
        elif is_docx:
            # Assign sequential page numbers for DOCX files (1-based)
            # This enables table merging logic to work for DOCX files
            table_dict[table_ix]["page_number"] = table_ix + 1
            logger.debug(f"Assigned page number {table_ix + 1} to DOCX table {table_ix}")
        else:
            table_dict[table_ix]["page_number"] = None

    logger.debug(f"Merging tables spanning multiple pages for '{doc_path}'")
    merged_table_dict = merge_consecutive_tables(table_dict)

    table_markdowns = [merged_table_dict[key]["markdown"] for key in sorted(merged_table_dict)]
    table_captions_list = [merged_table_dict[key]["caption"] for key in sorted(merged_table_dict)]
    # For PDF files: extract actual page numbers
    # For DOCX files: create list of None values (same length as other lists for zip())
    table_page_numbers = (
        [merged_table_dict[key]["page_number"] for key in sorted(merged_table_dict)]
        if not is_docx
        else [None] * len(merged_table_dict)
    )

    # Select appropriate prompt and max_tokens based on document language (lingua ISO format: 'EN', 'DE', etc.)
    prompt_templates = {
        LanguageCodes.ENGLISH: settings.table_summary.english.prompt,
        LanguageCodes.GERMAN: settings.table_summary.german.prompt,
        LanguageCodes.ITALIAN: settings.table_summary.italian.prompt,
        LanguageCodes.FRENCH: settings.table_summary.french.prompt,
    }
    selected_prompt = get_prompt_for_language(document_language, prompt_templates)

    # Select appropriate max_tokens based on document language
    max_tokens_config = {
        LanguageCodes.ENGLISH: settings.table_summary.english.max_tokens,
        LanguageCodes.GERMAN: settings.table_summary.german.max_tokens,
        LanguageCodes.ITALIAN: settings.table_summary.italian.max_tokens,
        LanguageCodes.FRENCH: settings.table_summary.french.max_tokens,
    }
    selected_max_tokens = max_tokens_config.get(document_language, settings.table_summary.english.max_tokens)

    logger.debug(
        f"Using language {document_language} prompt and max_tokens ({selected_max_tokens}) "
        f"for table summarization"
    )

    # Summarize and classify tables - use markdown directly
    table_summaries, decisions = summarize_and_classify_tables(
        table_markdowns, gen_model, gen_endpoint, doc_path,
        prompt_template=selected_prompt,
        max_tokens=selected_max_tokens,
    )

    filtered_table_dicts = {
        idx: {
            'summary': summary,
            'caption': caption,
            'page_number': page_num,
        }
        for idx, (keep, markdown, summary, caption, page_num) in enumerate(
            zip(decisions, table_markdowns, table_summaries, table_captions_list, table_page_numbers)
        )
        if keep
    }
    table_count = len(filtered_table_dicts)
    out_path.write_text(json.dumps(filtered_table_dicts, indent=2), encoding="utf-8")
    process_time = time.time() - t0

    return table_count, process_time
