import json
import time
import logging
import os

from tqdm import tqdm
os.environ['GRPC_VERBOSITY'] = 'ERROR' 
os.environ['TF_CPP_MIN_LOG_LEVEL'] = '2'

from pathlib import Path
from docling.datamodel.base_models import InputFormat
from docling.datamodel.document import DoclingDocument
from docling.datamodel.pipeline_options import PdfPipelineOptions
from concurrent.futures import as_completed, ProcessPoolExecutor
from docling.document_converter import DocumentConverter, PdfFormatOption
from sentence_splitter import SentenceSplitter

from common.llm_utils import classify_text_with_llm, summarize_table, tokenize_with_llm
from common.misc_utils import get_logger, generate_file_checksum, text_suffix, table_suffix
from ingest.pdf_utils import get_toc, get_matching_header_lvl, load_pdf_pages, find_text_font_size

logging.getLogger('docling').setLevel(logging.CRITICAL)

logger = get_logger("Docling")

excluded_labels = {
    'page_header', 'page_footer', 'caption', 'reference', 'footnote'
}

def process_converted_document(res, pdf_path, out_path, gen_model, gen_endpoint, start_time, timings):
    logger.debug(f"Processing '{pdf_path}'")
    
    doc_json = res.export_to_dict()
    stem = Path(pdf_path).stem

    # Initialize TocHeaders to get the Table of Contents (TOC)
    toc_headers = None
    try:
        toc_headers = get_toc(pdf_path)
    except Exception as e:
        logger.debug(f"No TOC found or failed to load TOC: {e}")

    # Load pdf pages one time when TOC headers not found for retrieving the font size of header texts
    pdf_pages = None
    if not toc_headers:
        pdf_pages = load_pdf_pages(pdf_path)

    # --- Text Extraction ---
    t0 = time.time()
    filtered_blocks, table_captions = [], []
    for block in doc_json.get('texts', []):
        block_type = block.get('label', '')
        if block_type not in excluded_labels:
            filtered_blocks.append(block)
        if block_type == 'caption':
            block_parent = block.get('parent', {}).get('$ref', '')
            if 'tables' in block_parent:
                table_captions.append(block)
    timings['extract_text_blocks'] = time.time() - t0

    if len(filtered_blocks):

        # t0 = time.time()
        # filtered_text_dicts = filter_with_llm(filtered_blocks, gen_model, gen_endpoint)
        # (Path(out_path) / f"{stem}_filtered_text.json").write_text(json.dumps(filtered_text_dicts, indent=2), encoding="utf-8")
        # timings['llm_filter_text'] = time.time() - t0

        filtered_text_dicts = filtered_blocks

        structured_output = []
        last_header_level = 0
        logger.debug(f"Processing text content of '{pdf_path}'")
        t0 = time.time()
        for text_obj in filtered_text_dicts:
            label = text_obj.get("label", "")

            # Check if it's a section header and process TOC or fallback to font size extraction
            if label == "section_header":
                prov_list = text_obj.get("prov", [])

                for prov in prov_list:
                    page_no = prov.get("page_no")
                    bbox_dict = prov.get("bbox")

                    if page_no is None or bbox_dict is None:
                        continue

                    if toc_headers:
                        header_prefix = get_matching_header_lvl(toc_headers, text_obj.get("text", ""))
                        if header_prefix:
                            # If TOC matches, use the level from TOC
                            structured_output.append({
                                "label": label,
                                "text": f"{header_prefix} {text_obj.get('text', '')}",
                                "page": page_no,
                                "font_size": None,  # Font size isn't necessary if TOC matches
                            })
                            last_header_level = len(header_prefix.strip())  # Update last header level
                        else:
                            # If no match, use the previous header level + 1
                            new_header_level = last_header_level + 1
                            structured_output.append({
                                "label": label,
                                "text": f"{'#' * new_header_level} {text_obj.get('text', '')}",
                                "page": page_no,
                                "font_size": None,  # Font size isn't necessary if TOC matches
                            })
                    else:
                        matches = find_text_font_size(pdf_pages, text_obj.get("text", ""), page_no - 1)
                        if len(matches):
                            font_size = 0
                            count = 0
                            for match in matches:
                                font_size += match["font_size"] if match["match_score"] == 100 else 0
                                count += 1 if match["match_score"] == 100 else 0
                            font_size = font_size / count if count else None

                            structured_output.append({
                                "label": label,
                                "text": text_obj.get("text", ""),
                                "page": page_no,
                                "font_size": round(font_size, 2) if font_size else None
                            })
            else:
                structured_output.append({
                    "label": label,
                    "text": text_obj.get("text", ""),
                    "page": text_obj.get("prov")[0].get("page_no"),
                    "font_size": None
                })

        timings["font_size_extraction"] = time.time() - t0

        (Path(out_path) / f"{stem}{text_suffix}").write_text(json.dumps(structured_output, indent=2), encoding="utf-8")
        
    else:
        (Path(out_path) / f"{stem}{text_suffix}").write_text(json.dumps(filtered_blocks, indent=2), encoding="utf-8")

    # --- Table Extraction ---
    if len(res.tables):
        logger.debug(f"Processing table content of '{pdf_path}'")
        t0 = time.time()
        table_htmls_dict = {}
        table_captions_dict = {i: None for i in range(len(res.tables))}
        for table_ix, table in enumerate(res.tables):
            table_htmls_dict[table_ix] = table.export_to_html(doc=res)
            for caption_idx, block in enumerate(table_captions):
                if block.get('parent')['$ref'] == f'#/tables/{table_ix}':
                    table_captions_dict[table_ix] = block.get('text', '')
                    table_captions.pop(caption_idx)
                    break
        table_htmls = [table_htmls_dict[key] for key in sorted(table_htmls_dict)]
        table_captions_list = [table_captions_dict[key] for key in sorted(table_captions_dict)]
        timings['extract_tables'] = time.time() - t0

        logger.debug(f"Summarizing tables of '{pdf_path}'")
        t0 = time.time()
        table_summaries = summarize_table(table_htmls, table_captions_list, gen_model, gen_endpoint)
        timings['summarize_tables'] = time.time() - t0

        logger.debug(f"Classifying table summaries of '{pdf_path}'")
        t0 = time.time()
        decisions = classify_text_with_llm(table_summaries, gen_model, gen_endpoint)
        filtered_table_dicts = {
            idx: {
                'html': html,
                'caption': caption,
                'summary': summary
            }
            for idx, (keep, html, caption, summary) in enumerate(zip(decisions, table_htmls, table_captions_list, table_summaries)) if keep
        }
        (Path(out_path) / f"{stem}{table_suffix}").write_text(json.dumps(filtered_table_dicts, indent=2), encoding="utf-8")
        timings['filter_tables'] = time.time() - t0
    else:
        (Path(out_path) / f"{stem}{table_suffix}").write_text(json.dumps([], indent=2), encoding="utf-8")

    total_time = time.time() - start_time
    logger.debug(f"Timing for {stem} Total: {total_time:.2f}s")
    for k, v in timings.items():
        logger.debug(f"  {k:<30}: {v:.2f}s")

def convert_and_process(path, doc_converter, out_path, llm_model, llm_endpoint):
    try:
        timings = {}
        start_time = time.time()
        f = (Path(out_path) / f"{Path(path).stem}_converted.json")
        logger.debug(f"Checking {str(f)}")
        converted_doc = None
        if f.exists():
            logger.debug("Loading from converted json")
            with Path(str(f)).open("r") as fp:
                doc_dict = json.load(fp)
                converted_doc = DoclingDocument.model_validate(doc_dict)
        else:
            logger.debug(f"Not exist, converting '{path}'")
            start_time = time.time()
            t0 = time.time()
            converted_doc = doc_converter.convert(path).document
            timings['conversion_time'] = time.time() - t0
            logger.debug(f"'{path}' converted")
            converted_doc.save_as_json(str(f))
        process_converted_document(converted_doc, path, out_path, llm_model, llm_endpoint, start_time, timings)
        logger.debug(f"Processed: {path}")
        return path
    except Exception as e:
        raise Exception(f"Error converting and processing '{path}': {e}")

def extract_document_data(input_paths, out_path, llm_model, llm_endpoint, force=False):
    # Accelerator & pipeline options
    pipeline_options = PdfPipelineOptions()
    # Docling model files are getting downloaded to this /var/docling-models dir by this project-ai-services/images/rag-base/download_docling_models.py script in project-ai-services/images/rag-base/Containerfile
    pipeline_options.artifacts_path = "/var/docling-models"
    

    pipeline_options.do_table_structure = True
    pipeline_options.table_structure_options.do_cell_matching = True
    pipeline_options.do_ocr = False


    # Skip files that already exist by matching the cached checksum of the pdf
    # if there is no difference in checksum and processed text & table json also exist, would skip for convert and process list
    # else add the file to convert and process list(filtered_input_paths) 
    filtered_input_paths = []
    converted_paths = []
    for path in input_paths:
        f = (Path(out_path) / f"{Path(path).stem}.checksum")
        if f.exists():
            checksum = f.read_text()
            if checksum != generate_file_checksum(path) \
                or not (Path(out_path) / f"{Path(path).stem}{text_suffix}").exists() \
                or not (Path(out_path) / f"{Path(path).stem}{table_suffix}").exists():
                filtered_input_paths.append(path)
            else:
                converted_paths.append(path)
        else:
            filtered_input_paths.append(path)

    for path in filtered_input_paths:
        checksum = generate_file_checksum(path)
        (Path(out_path) / f"{Path(path).stem}.checksum").write_text(checksum, encoding='utf-8')

    doc_converter = DocumentConverter(
        allowed_formats=[
            InputFormat.PDF
        ],
        format_options={InputFormat.PDF: PdfFormatOption(pipeline_options=pipeline_options)}
    )

    if filtered_input_paths:
        with ProcessPoolExecutor(max_workers=max(1, min(4, len(filtered_input_paths)))) as executor:
            futures = [
                executor.submit(convert_and_process, path, doc_converter, out_path, llm_model, llm_endpoint)
                for path in filtered_input_paths
            ]
            for future in tqdm(as_completed(futures), total=len(filtered_input_paths)):
                try:
                    converted_paths.append(future.result())
                except Exception as e:
                    logger.error(f"{e}")
    else:
        logger.debug("No files to convert and process")

    return converted_paths

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


def chunk_single_file(input_path, output_path, emb_endpoint, max_tokens=512):    
    if not Path(output_path).exists():
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

        for idx, block in enumerate(data):
            label = block.get("label")
            text = block.get("text", "").strip()
            try:
                page_no = block.get("prov", {})[0].get("page_no")
            except:
                page_no = 0
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
        with open(output_path, "w") as f:
            json.dump(chunks, f, indent=2)

        logger.debug(f"{len(chunks)} RAG chunks saved to {output_path}")
    else:
        logger.debug(f"{output_path} already exists.")

    return output_path

def hierarchical_chunk_with_token_split(input_paths, output_paths, emb_endpoint, max_tokens=512):
    logger.debug("Creating chunks from processed documents")
    if len(input_paths) != len(output_paths):
        raise ValueError("`input_paths` and `output_paths` must have the same length")

    # Process each input-output file pair in parallel using ProcessPoolExecutor
    with ProcessPoolExecutor(max_workers=8) as executor:
        futures = []
        for input_path, output_path in zip(input_paths, output_paths):
            logger.debug(f"Submitting '{input_path}' for chunking")
            futures.append(executor.submit(chunk_single_file, input_path, output_path, emb_endpoint, max_tokens))

        chunked_files = []
        # Wait for all futures to finish and handle exceptions
        for future in futures:
            try:
                chunked_files.append(future.result())  # Capture exceptions if any
            except Exception as e:
                logger.error(f"Error occurred while chunking: {e}")
    logger.debug("Chunks creation completed")
    return chunked_files

def create_chunk_documents(in_txt_f, in_tab_f, orig_fn):
    logger.debug(f"Creating combined chunk documents from '{in_txt_f}' & '{in_tab_f}'")
    with open(in_txt_f, "r") as f:
        txt_data = json.load(f)

    with open(in_tab_f, "r") as f:
        tab_data = json.load(f)

    txt_docs = []
    if len(txt_data):
        for _, block in enumerate(txt_data):
            meta_info = ''
            if block.get('chapter_title'):
                meta_info += f"Chapter: {block.get('chapter_title')} "
            if block.get('section_title'):
                meta_info += f"Section: {block.get('section_title')} "
            if block.get('subsection_title'):
                meta_info += f"Subsection: {block.get('subsection_title')} "
            if block.get('subsubsection_title'):
                meta_info += f"Subsubsection: {block.get('subsubsection_title')} "
            txt_docs.append({
                # "chunk_id": txt_id,
                "page_content": f'{meta_info}\n{block.get("content")}' if meta_info != '' else block.get("content"),
                "filename": orig_fn,
                "type": "text",
                "source": meta_info,
                "language": "en"
            })

    tab_docs = []
    if len(tab_data):
        tab_data = list(tab_data.values())
        for tab_id, block in enumerate(tab_data):
            # tab_docs.append(Document(
            #     page_content=block.get('summary'),
            #     metadata={"filename": orig_fn, "type": "table", "source": block.get('html'), "chunk_id": tab_id}
            # ))
            tab_docs.append({
                "page_content": block.get("summary"),
                "filename": orig_fn,
                "type": "table",
                "source": block.get("html"),
                "language": "en"
            })

    combined_docs = txt_docs + tab_docs

    logger.debug(f"Combined chunk documents created")

    return combined_docs
