# Standard library imports
import logging
import shutil
import tempfile
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any, Dict, List, Optional

# Third-party PDF processing libraries
import pdfplumber
import pypdfium2 as pdfium
from pdfminer.pdfdocument import PDFDocument, PDFNoOutlines
from pdfminer.pdfpage import PDFPage
from pdfminer.pdfparser import PDFParser, PDFSyntaxError
from rapidfuzz import fuzz

# Docling document conversion libraries
from docling.datamodel.document import ConversionResult
from docling.document_converter import DocumentConverter
from docling_core.types.doc.document import DoclingDocument

# Local application imports
from common.misc_utils import get_logger, DoclingConversionError
from common.retry_utils import retry_on_transient_error
from digitize.settings import settings

# To suppress the warnings raised from pdfminer package while extracting the font size
logging.getLogger("pdfminer").propagate = False
logging.getLogger("pdfminer").setLevel(logging.ERROR)

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
    parser = None
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
            if parser is not None:
                try:
                    parser.close()
                except Exception:
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

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def convert_chunk(doc_converter: DocumentConverter, path: Path, chunk_num: int, start_page: int, end_page: int, chunk_cache_dir: Path):
    """Convert a single chunk of a PDF document.
    
    Args:
        doc_converter: DocumentConverter instance
        path: Path to the PDF file
        chunk_num: Chunk number for logging
        start_page: Starting page number (1-based)
        end_page: Ending page number (1-based, inclusive)
        chunk_cache_dir: Directory to save chunk results
        
    Returns:
        Path to the saved chunk JSON file
        
    Raises:
        DoclingConversionError: If conversion or saving fails
    """
    try:
        # Convert this chunk
        conv_res: ConversionResult = doc_converter.convert(source=path, page_range=(start_page, end_page))
        
        # Save chunk result to cache
        chunk_filename = chunk_cache_dir / f"chunk_{chunk_num:04d}.json"
        conv_res.document.save_as_json(str(chunk_filename))
        logger.debug(f"Saved chunk of {path}'s chunk {chunk_num} to {chunk_filename}")

        return chunk_filename
    except Exception as e:
        # Wrap any exception in DoclingConversionError for retry handling
        error_msg = f"Failed to convert chunk {chunk_num} (pages {start_page}-{end_page}) of {path}: {str(e)}"
        logger.error(error_msg)
        raise DoclingConversionError(error_msg) from e

def convert_doc(path: str | Path, cache_dir: Optional[Path] = None) -> DoclingDocument:
    """
    Convert a document to DoclingDocument, processing in 100-page chunks.
    
    Args:
        path: Path to the PDF file to convert
        cache_dir: Optional cache directory for storing chunk results.
                   Will be cleaned up after processing.
        
    Returns:
        DoclingDocument containing the concatenated result
    """
    
    # Input validation
    path = Path(path)
    if not path.exists():
        raise FileNotFoundError(f"Document not found: {path}")
    
    doc_converter: DocumentConverter = get_doc_converter()
    
    # Get total page count
    total_pages = get_pdf_page_count(path)
    
    # If document has configured chunk size pages or fewer, convert normally
    if total_pages <= settings.digitize.pdf_chunk_size:
        logger.debug(f"Converting {path} document with {total_pages} pages in single pass")
        
        @retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
        def _convert_single_doc():
            try:
                return doc_converter.convert(source=path).document
            except Exception as e:
                error_msg = f"Failed to convert document {path}: {str(e)}"
                logger.error(error_msg)
                raise DoclingConversionError(error_msg) from e
        
        return _convert_single_doc()
    
    # Process in chunks
    # Calculate total chunks using ceiling division for the configured PDF chunk size.
    # This ensures all pages are covered even if the last chunk is smaller.
    total_chunks = (total_pages + settings.digitize.pdf_chunk_size - 1) // settings.digitize.pdf_chunk_size
    logger.debug(
        f"Converting {path} document with {total_pages} pages in {total_chunks} "
        f"chunks of {settings.digitize.pdf_chunk_size}"
    )
    
    # Determine cache directory for storing chunk results
    if cache_dir is None:
        chunk_cache_dir = Path(tempfile.mkdtemp(prefix="docling_chunks_"))
    else:
        chunk_cache_dir = Path(cache_dir)
    
    chunk_cache_dir.mkdir(parents=True, exist_ok=True)
    
    try:
        # Process document in chunks and save each chunk
        chunk_files = []
        
        for start_page in range(1, total_pages + 1, settings.digitize.pdf_chunk_size):
            end_page = min(start_page + settings.digitize.pdf_chunk_size - 1, total_pages)
            chunk_num = (start_page - 1) // settings.digitize.pdf_chunk_size + 1
            
            logger.debug(f"Processing {path}'s chunk {chunk_num}/{total_chunks} (pages {start_page}-{end_page})")
            chunk_file = convert_chunk(doc_converter, path, chunk_num, start_page, end_page, chunk_cache_dir)
            chunk_files.append(chunk_file)
        
        # Load all chunk documents and concatenate
        docs = [DoclingDocument.load_from_json(filename=f) for f in chunk_files]
        concatenated_doc = DoclingDocument.concatenate(docs=docs)
        
        logger.debug(f"Successfully concatenated {path}'s {len(docs)} chunks into single document")
        
        return concatenated_doc
    
    finally:
        # Always cleanup cache directory
        try:
            shutil.rmtree(chunk_cache_dir)
            logger.debug(f"Cleaned up cache directory: {chunk_cache_dir}")
        except Exception as e:
            logger.warning(f"Failed to cleanup cache directory {chunk_cache_dir}: {e}")

def get_doc_converter():
    import os
    from pathlib import Path
    from docling.datamodel.base_models import InputFormat
    from docling.datamodel.pipeline_options import PdfPipelineOptions
    from docling.document_converter import DocumentConverter, PdfFormatOption

    # Accelerator & pipeline options
    pipeline_options = PdfPipelineOptions()
    
    # Only set artifacts_path if DOCLING_MODELS_PATH environment variable is set
    docling_models_path = os.environ.get('DOCLING_MODELS_PATH')
    if docling_models_path:
        artifacts_path = Path(docling_models_path)
        if artifacts_path.exists():
            pipeline_options.artifacts_path = artifacts_path
            logger.debug(f"Using docling models from: {artifacts_path}")
        else:
            logger.warning(f"DOCLING_MODELS_PATH set to {artifacts_path} but directory does not exist")
    else:
        logger.debug("DOCLING_MODELS_PATH not set. Docling will use default model loading behavior.")
    
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
