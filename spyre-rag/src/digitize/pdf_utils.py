import logging
from typing import List, Dict, Any
import pdfplumber
from rapidfuzz import fuzz
from collections import defaultdict, Counter
from pdfminer.pdfdocument import PDFDocument, PDFNoOutlines
from pdfminer.pdfparser import PDFParser, PDFSyntaxError
from pdfminer.pdfpage import PDFPage
import pypdfium2 as pdfium

from common.misc_utils import get_logger

# To suppress the warnings raised from pdfminer package while extracting the font size
logging.propagate = False
logging.getLogger().setLevel(logging.ERROR)

logger = get_logger("PDF")

def get_pdf_page_count(file_path):
    try:
        pdf = pdfium.PdfDocument(file_path)
        count = len(pdf)
        pdf.close()
        return count
    except Exception as e:
        return 0

def get_matching_header_lvl(toc, title, threshold=80):
    title_l = title.lower()
    for toc_title in toc:
        score = fuzz.partial_ratio(title_l, toc_title.lower())
        if score >= threshold:
            return "#" * toc[toc_title]
    return ""

def get_toc(file):
    toc = {}
    page_count = 0
    with open(file, "rb") as fp:
        try:
            parser = PDFParser(fp)
            document = PDFDocument(parser)

            outlines = list(document.get_outlines())
            if not outlines:
                logger.debug("No outlines found.")

            for (level, title, _, _, _) in outlines:
                toc[title] = level
            page_count = len(list(PDFPage.create_pages(document)))

        except PDFNoOutlines:
            logger.debug("No outlines found.")
        except PDFSyntaxError:
            logger.debug("Corrupted PDF or non-PDF file.")
        finally:
            try:
                parser.close()
            except NameError:
                pass  # nothing to do
    return toc, page_count

def load_pdf_pages(pdf_path):
    pdf_pages = []
    with pdfplumber.open(pdf_path) as pdf:
        for page in pdf.pages:
            pdf_pages.append(page.extract_words(extra_attrs=["size", "fontname"]))
    return pdf_pages

def find_text_font_size(
    pdf_pages: List,
    search_string: str,
    page_number: int = 0,
    fuzz_threshold: float = 80,
    exact_match_first: bool = False
) -> List[Dict[str, Any]]:
    """ Searches for text in a PDF page and returns font info and bbox for fuzzy-matching lines. """
    matches = []

    try:
        if page_number >= len(pdf_pages):
            logger.debug(f"Page {page_number} does not exist in PDF.")
            return []

        words = pdf_pages[page_number]

        if not words:
            logger.debug("No words found on page.")
            return []

        # Group words into lines based on Y-coordinate
        lines_dict = defaultdict(list)
        for word in words:
            if not all(k in word for k in ("text", "top", "x0", "x1", "bottom", "size", "fontname")):
                continue  # skip incomplete word entries
            top_key = round(word["top"], 1)
            lines_dict[top_key].append(word)

        for line_words in lines_dict.values():
            sorted_line = sorted(line_words, key=lambda w: w["x0"])
            line_text = " ".join(w["text"] for w in sorted_line)

            # Try exact match if enabled
            if exact_match_first and search_string.lower() == line_text.lower():
                score = 100
            else:
                score = fuzz.partial_ratio(line_text.lower(), search_string.lower())

            if score >= fuzz_threshold:
                font_sizes = [w["size"] for w in sorted_line if w["size"] is not None]
                font_names = [w["fontname"] for w in sorted_line if w["fontname"]]

                # Most common font size and name as representative
                font_size = Counter(font_sizes).most_common(1)[0][0] if font_sizes else None
                font_name = Counter(font_names).most_common(1)[0][0] if font_names else None

                x0 = min(w["x0"] for w in sorted_line)
                x1 = max(w["x1"] for w in sorted_line)
                top = min(w["top"] for w in sorted_line)
                bottom = max(w["bottom"] for w in sorted_line)

                matches.append({
                    "matched_text": line_text,
                    "match_score": score,
                    "font_size": font_size,
                    "font_name": font_name,
                    "bbox": (x0, top, x1, bottom)
                })

    except Exception as e:
        logger.error(f"Error extracting font size: {e}")

    return matches

def convert_doc(path):
    doc_converter = get_doc_converter()
    doc = doc_converter.convert(path)
    return doc

def get_doc_converter():
    from docling.datamodel.base_models import InputFormat
    from docling.datamodel.pipeline_options import PdfPipelineOptions
    from docling.document_converter import DocumentConverter, PdfFormatOption

    # Accelerator & pipeline options
    pipeline_options = PdfPipelineOptions()
    # Docling model files are getting downloaded to this /var/docling-models dir by this project-ai-services/images/rag-base/download_docling_models.py script in project-ai-services/images/rag-base/Containerfile
    pipeline_options.artifacts_path = "/var/docling-models"
    pipeline_options.do_table_structure = True
    pipeline_options.table_structure_options.do_cell_matching = True
    pipeline_options.do_ocr = False

    doc_converter = DocumentConverter(
        allowed_formats=[
            InputFormat.PDF
        ],
        format_options={InputFormat.PDF: PdfFormatOption(pipeline_options=pipeline_options)}
    )

    return doc_converter
