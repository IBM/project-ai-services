"""
Text processing: extraction from PDF and DOCX converted documents.

Responsibilities:
- process_text      — PDF text extraction with TOC / font-size header levels
- process_text_docx — DOCX text extraction with TOC / fallback header levels
"""

import json
import time

from common.lang_utils import LanguageCodes
from common.llm_utils import tqdm_wrapper
from common.misc_utils import get_logger

logger = get_logger("processing.text")

excluded_labels = {
    'page_header', 'page_footer', 'caption', 'reference', 'footnote'
}


def process_text_docx(converted_doc, docx_path, out_path):
    """
    Process text content from DOCX files.
    Simplified implementation for DOCX processing without page numbers or font sizes.
    """
    from digitize.parsing.pdf import get_matching_header_lvl
    from digitize.parsing.docx import get_docx_toc, estimate_docx_page_count

    page_count = 0
    process_time = 0.0

    # Initialize TocHeaders to get the Table of Contents (TOC)
    t0 = time.time()

    toc_headers = None
    try:
        toc_headers = get_docx_toc(docx_path)
        page_count = estimate_docx_page_count(docx_path)
    except Exception as e:
        logger.debug(f"No TOC found or failed to load TOC: {e}")

    # --- Text Extraction ---
    if not converted_doc.texts:
        logger.debug(f"No text content found in '{docx_path}'")
        out_path.write_text(json.dumps([], indent=2), encoding="utf-8")
        return page_count, process_time

    structured_output = []
    last_header_level = 0
    for text_obj in tqdm_wrapper(converted_doc.texts, desc=f"Processing text content of '{docx_path}'"):
        label = text_obj.label
        if label in excluded_labels:
            continue

        # Check if it's a section header
        if label == "section_header":
            # For DOCX files, use TOC for heading levels
            page_no = None
            header_text = text_obj.text
            text_content = text_obj.text.strip()

            # Check if we have TOC for this header
            if toc_headers:
                header_prefix = get_matching_header_lvl(toc_headers, text_content)
                if header_prefix:
                    # Use TOC level
                    header_text = f"{header_prefix} {text_content}"
                    last_header_level = len(header_prefix.strip())
                    logger.debug(f"DOCX header '{text_content[:50]}...' matched TOC level {last_header_level}")
                else:
                    # No TOC match, use previous level + 1
                    new_header_level = last_header_level + 1
                    header_text = f"{'#' * new_header_level} {text_content}"
                    logger.debug(f"DOCX header '{text_content[:50]}...' assigned level {new_header_level}")

            structured_output.append({
                "label": label,
                "text": header_text,
                "page": page_no,
                "font_size": None
            })
        else:
            # For non-header elements
            page_no = None
            structured_output.append({
                "label": label,
                "text": text_obj.text,
                "page": page_no,
                "font_size": None
            })

    process_time = time.time() - t0
    out_path.write_text(json.dumps(structured_output, indent=2), encoding="utf-8")

    return page_count, process_time


def process_text(converted_doc, doc_path, out_path):
    from digitize.parsing.pdf import get_toc, load_pdf_pages, find_text_font_size, get_matching_header_lvl

    page_count = 0
    process_time = 0.0

    # Initialize TocHeaders to get the Table of Contents (TOC)
    t0 = time.time()

    toc_headers = None
    try:
        toc_headers, page_count = get_toc(doc_path)
    except Exception as e:
        logger.debug(f"No TOC found or failed to load TOC: {e}")

    # Load pdf pages one time when TOC headers not found for retrieving the font size of header texts
    pdf_pages = None
    if not toc_headers:
        pdf_pages = load_pdf_pages(doc_path)
        page_count = len(pdf_pages)

    # --- Text Extraction ---
    if not converted_doc.texts:
        logger.debug(f"No text content found in '{doc_path}'")
        out_path.write_text(json.dumps([], indent=2), encoding="utf-8")
        return page_count, process_time

    structured_output = []
    last_header_level = 0
    for text_obj in tqdm_wrapper(converted_doc.texts, desc=f"Processing text content of '{doc_path}'"):
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
                    # Try font size extraction
                    if pdf_pages:
                        # PDF font size extraction
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
            # For non-header elements, safely get page number
            page_no = text_obj.prov[0].page_no if text_obj.prov else None
            structured_output.append({
                "label": label,
                "text": text_obj.text,
                "page": page_no,
                "font_size": None
            })

    process_time = time.time() - t0
    out_path.write_text(json.dumps(structured_output, indent=2), encoding="utf-8")

    return page_count, process_time
