"""
Table processing: extraction, merging, and summarisation.

Responsibilities:
- process_table  — extract/summarise tables from a converted document
- clean_markdown_table_and_caption — fix parser glitches where caption becomes table header
- extract_table_headers — parse markdown table header row smartly
- is_table_continuation — fuzzy matching of headers across pages
- merge_markdown_tables — combine tables by dropping duplicate setup rows
- merge_consecutive_tables — merge tables spanning consecutive pages
"""

import json
import time
import re
from pathlib import Path
from rapidfuzz import fuzz

from common.lang_utils import LanguageCodes, get_prompt_for_language
from common.llm_utils import summarize_and_classify_tables, tqdm_wrapper
from common.misc_utils import get_logger
from digitize.settings import settings

logger = get_logger("processing.tables")

def clean_markdown_table_and_caption(markdown_table: str, current_caption: str) -> tuple[str, str]:
    """
    Cleans up markdown tables where the PDF parser accidentally put the caption
    as the first header row, and the real headers as the first data row.
    """
    if not markdown_table:
        return markdown_table, current_caption

    lines = [line.strip() for line in markdown_table.strip().split('\n')]
    sep_idx = -1
    for i, line in enumerate(lines):
        if '|' in line and '-' in line and not any(c.isalnum() for c in line):
            sep_idx = i
            break

    if sep_idx > 0 and len(lines) > sep_idx + 1:
        header_line = lines[sep_idx - 1]
        headers = [h.strip() for h in header_line.strip('|').split('|') if h.strip()]

        first_data_line = lines[sep_idx + 1]

        # Check if the header row is a repeated string or looks like a caption
        is_repeated_header = len(set(headers)) == 1 and len(headers) >= 1
        starts_with_caption = False
        if headers:
            starts_with_caption = bool(re.match(r'^(table|tabelle|figure|abbildung|tableau|fig|figura)\s*\d+', headers[0], re.IGNORECASE))

        if is_repeated_header or starts_with_caption:
            potential_caption = headers[0] if headers else ""

            # Reconstruct the markdown with the first data row promoted to the header
            new_lines = []
            if sep_idx - 1 > 0:
                new_lines.extend(lines[:sep_idx - 1])

            new_lines.append(first_data_line)     # Promote data row to header
            new_lines.append(lines[sep_idx])      # Separator
            new_lines.extend(lines[sep_idx + 2:]) # Remaining data rows

            new_markdown = '\n'.join(new_lines)
            new_caption = current_caption if current_caption else potential_caption
            return new_markdown, new_caption

    return markdown_table, current_caption

def extract_table_headers(markdown_table: str) -> list[str]:
    """
    Extract headers from a markdown table.
    Smartly searches for the line directly above the separator (|---|).
    Args:
        markdown_table: Markdown formatted table string
    Returns:
        List of header strings, or empty list if no headers found
    """
    if not markdown_table or not markdown_table.strip():
        return []

    try:
        lines = [line.strip() for line in markdown_table.strip().split('\n')]

        # Find the separator line
        sep_idx = -1
        for i, line in enumerate(lines):
            if '|' in line and '-' in line and not any(c.isalnum() for c in line):
                sep_idx = i
                break

        # The real headers in Markdown are always directly above the separator
        if sep_idx > 0:
            header_line = lines[sep_idx - 1]
        else:
            # Fallback: The first line with a pipe symbol
            header_line = next((line for line in lines if '|' in line), None)

        if not header_line:
            return []

        # Remove leading and trailing pipes and split by pipe
        if header_line.startswith('|'):
            header_line = header_line[1:]
        if header_line.endswith('|'):
            header_line = header_line[:-1]

        # Split by pipe and strip whitespace from each header and filter out empty headers
        headers = [h.strip() for h in header_line.split('|') if h.strip()]
        return headers
    except Exception as e:
        logger.debug(f"Failed to extract headers from markdown table: {e}")
        return []

def is_table_continuation(headers1: list[str], headers2: list[str], fuzzy_threshold: float = 85.0) -> bool:
    """
    Determines if a table continues across pages by comparing ONLY headers.
    Independent of captions to unify PDF and DOCX processing.
    """
    if not headers1 or not headers2:
        return False

    # If column counts differ, it's not a continuation
    if len(headers1) != len(headers2):
        return False

    # Join headers as strings for fuzzy matching
    h1_str = "|".join(headers1).lower().strip()
    h2_str = "|".join(headers2).lower().strip()

    # Check exact match (Fast path)
    if h1_str == h2_str:
        return True

    # Fuzzy match for OCR errors in headers
    similarity = fuzz.ratio(h1_str, h2_str)
    if similarity >= fuzzy_threshold:
        return True

    return False

def merge_markdown_tables(table1_md: str, table2_md: str) -> str:
    """
    Merges two markdown tables.
    Dynamically removes all redundant title and header rows from the second table.
    """
    if not table1_md or not table2_md:
        return table1_md or table2_md or ""

    # Split tables into lines
    lines1 = [line.strip() for line in table1_md.strip().split('\n')]
    lines2 = [line.strip() for line in table2_md.strip().split('\n')]

    # Find separator in table 2
    sep_idx2 = -1
    for i, line in enumerate(lines2):
        # Bulletproof separator check: Line has a pipe, a dash, and NO alphanumeric characters
        if '|' in line and '-' in line and not any(c.isalnum() for c in line):
            sep_idx2 = i
            break

    # Fallback: just append all lines from table2 (shouldn't happen with valid markdown tables)
    if sep_idx2 == -1:
        return table1_md + '\n' + table2_md

    # Find separator in table 1 (to identify setup rows)
    sep_idx1 = -1
    for i, line in enumerate(lines1):
        if '|' in line and '-' in line and not any(c.isalnum() for c in line):
            sep_idx1 = i
            break

    data_start_idx = sep_idx2 + 1

    # Smart filtering: check if rows after the separator in table 2
    # already exist as a header/title in table 1. If so, skip them.
    while data_start_idx < len(lines2):
        row_to_check = lines2[data_start_idx]
        is_repeated_header = False

        # Compare against the first few rows of table 1 (including possible sub-headers)
        limit = sep_idx1 + 3 if sep_idx1 != -1 else len(lines1)
        for t1_row in lines1[:limit]:
            clean_t2 = row_to_check.replace('|', '').strip().lower()
            clean_t1 = t1_row.replace('|', '').strip().lower()

            if clean_t2 and clean_t1 and clean_t2 == clean_t1:
                is_repeated_header = True
                break

        if is_repeated_header:
            data_start_idx += 1
        else:
            break  # Reached the start of actual data

    merged_lines = lines1 + lines2[data_start_idx:]
    return '\n'.join(merged_lines)

def merge_consecutive_tables(table_dict: dict) -> dict:
    """
    Merge tables that span multiple consecutive pages (header check only).
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
        current_caption = current_table.get('caption', '')
        current_headers = extract_table_headers(current_markdown)

        merged_markdown = current_markdown
        last_merged_page = current_page

        # Look ahead at the next 2 pages
        for j in range(i + 1, min(i + 3, len(sorted_indices))):
            next_idx = sorted_indices[j]
            next_table = table_dict[next_idx]
            next_markdown = next_table.get('markdown', '')
            next_page = next_table.get('page_number')
            next_headers = extract_table_headers(next_markdown)

            # Check for consecutive pages AND fuzzy header match
            if (next_page is not None and
                    last_merged_page is not None and
                    next_page == last_merged_page + 1 and
                    is_table_continuation(current_headers, next_headers)):

                # Merge the tables
                merged_markdown = merge_markdown_tables(merged_markdown, next_markdown)
                last_merged_page = next_page
                skip_indices.add(next_idx)
                logger.debug(f"Merged table {next_idx} (page {next_page}) into table {idx} (page {current_page})")
            else:
                # Stop looking if pages are not consecutive or tables don't match
                # Stop looking if pages are not consecutive or headers don't match
                break

        # Store the merged (or original) table
        merged_dict[idx] = {
            'markdown': merged_markdown,
            'caption': current_caption, 
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

    file_ext = Path(doc_path).suffix.lower()
    is_docx = file_ext == '.docx'

    # Lazy import to avoid circular dependency
    from digitize.parsing.docx import recover_table_caption_from_body_context

    table_dict = {}
    for table_ix, table in enumerate(tqdm_wrapper(converted_doc.tables, desc=f"Processing table content for '{doc_path}'")):
        table_dict[table_ix] = {}

        # Use Markdown format for better LLM understanding
        raw_markdown = table.export_to_markdown(doc=converted_doc)
        caption = table.caption_text(doc=converted_doc)

        if not caption:
            caption = recover_table_caption_from_body_context(converted_doc, table_ix)

        # Clean the markdown to fix parser glitches and recover hidden captions
        clean_markdown, clean_caption = clean_markdown_table_and_caption(raw_markdown, caption)

        table_dict[table_ix]["markdown"] = clean_markdown
        table_dict[table_ix]["caption"] = clean_caption

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

    logger.debug(f"Merging cross-page tables for '{doc_path}'")
    merged_table_dict = merge_consecutive_tables(table_dict)

    table_markdowns = [merged_table_dict[key]["markdown"] for key in sorted(merged_table_dict)]
    table_captions_list = [merged_table_dict[key]["caption"] for key in sorted(merged_table_dict)]
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
        f"Using language prompt {document_language} and max_tokens ({selected_max_tokens}) "
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
