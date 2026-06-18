import logging
import shutil
import tempfile
import time
from pathlib import Path
from typing import Optional

# Local application imports
from common.misc_utils import get_logger, DoclingConversionError
from common.retry_utils import retry_on_transient_error
from digitize.settings import settings
from digitize.pdf_utils import get_document_page_count
from digitize.models import OutputFormat

# Docling document conversion libraries
from docling.datamodel.document import ConversionResult
from docling.document_converter import DocumentConverter
from docling_core.types.doc.document import DoclingDocument

logger = get_logger("docling_utils")

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def convert_chunk(doc_converter: DocumentConverter, path: Path, chunk_num: int, start_page: int, end_page: int, chunk_cache_dir: Path):
    """Convert a single chunk of a document.

    Args:
        doc_converter: DocumentConverter instance
        path: Path to the file
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
        path: Path to the document file to convert
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
    total_pages = get_document_page_count(str(path))

    # If document has configured chunk size pages or fewer, convert normally
    if total_pages <= settings.digitize.doc_chunk_size:
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
    total_chunks = (total_pages + settings.digitize.doc_chunk_size - 1) // settings.digitize.doc_chunk_size
    logger.debug(
        f"Converting {path} document with {total_pages} pages in {total_chunks} "
        f"chunks of {settings.digitize.doc_chunk_size}"
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

        for start_page in range(1, total_pages + 1, settings.digitize.doc_chunk_size):
            end_page = min(start_page + settings.digitize.doc_chunk_size - 1, total_pages)
            chunk_num = (start_page - 1) // settings.digitize.doc_chunk_size + 1

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
            InputFormat.PDF,
            InputFormat.DOCX
        ],
        format_options={InputFormat.PDF: PdfFormatOption(pipeline_options=pipeline_options)}
    )

    return doc_converter

def convert_document_format(doc_path: str, out_path: Path, doc_id: str, output_format: OutputFormat):
    logger.info(f"Processing '{doc_path}'")

    out_dir = Path(out_path)
    out_dir.mkdir(parents=True, exist_ok=True)

    t0 = time.time()

    # Convert document → DoclingDocument
    doc_obj = convert_doc(doc_path, cache_dir=out_path / doc_id)

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

def convert_document(doc_path, out_path, file_name):
    """
    Convert a single document to JSON format.
    This function runs in a separate process via ProcessPoolExecutor.
    """
    try:
        logger.info(f"Processing '{doc_path}'")
        converted_json = (Path(out_path) / f"{file_name}.json")
        converted_json_f = str(converted_json)
        logger.debug(f"Converting '{doc_path}'")
        t0 = time.time()

        converted_doc: DoclingDocument = convert_doc(doc_path, cache_dir=out_path / file_name)
        converted_doc.save_as_json(str(converted_json_f))

        conversion_time = time.time() - t0
        logger.debug(f"'{doc_path}' converted")
        return converted_json_f, conversion_time
    except Exception as e:
        logger.error(f"Error converting '{doc_path}': {e}")
    return None, None