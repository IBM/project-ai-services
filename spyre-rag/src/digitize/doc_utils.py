import json
import time
import logging
import os
from pathlib import Path

from tqdm import tqdm
os.environ['GRPC_VERBOSITY'] = 'ERROR' 
os.environ['TF_CPP_MIN_LOG_LEVEL'] = '2'

from pathlib import Path
from docling.datamodel.document import DoclingDocument, TextItem
from concurrent.futures import as_completed, ProcessPoolExecutor, ThreadPoolExecutor, as_completed
from sentence_splitter import SentenceSplitter

from common.llm_utils import create_llm_session, summarize_and_classify_tables, tokenize_with_llm
from common.misc_utils import get_logger, generate_file_checksum, text_suffix, table_suffix
from common.misc_utils import get_logger, generate_file_checksum, text_suffix, table_suffix, chunk_suffix
from digitize.pdf_utils import get_toc, get_matching_header_lvl, load_pdf_pages, find_text_font_size, get_pdf_page_count, convert_doc
from digitize.status import StatusManager
from common.digitize_utils import DocStatus

logging.getLogger('docling').setLevel(logging.CRITICAL)

logger = get_logger("doc_utils")

# Constants for worker pool
WORKER_SIZE = 4
HEAVY_PDF_CONVERT_WORKER_SIZE = 2
HEAVY_PDF_PAGE_THRESHOLD = 500

is_debug = logger.isEnabledFor(logging.DEBUG) 
tqdm_wrapper = None
if is_debug:
    tqdm_wrapper = tqdm
else:
    tqdm_wrapper = lambda x, **kwargs: x

excluded_labels = {
    'page_header', 'page_footer', 'caption', 'reference', 'footnote'
}

POOL_SIZE = 32

create_llm_session(pool_maxsize=POOL_SIZE)

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
        table_dict[table_ix]["html"] = table.export_to_html(doc=converted_doc)
        table_dict[table_ix]["caption"] = table.caption_text(doc=converted_doc)

    table_htmls = [table_dict[key]["html"] for key in sorted(table_dict)]
    table_captions_list = [table_dict[key]["caption"] for key in sorted(table_dict)]

    table_summaries, decisions = summarize_and_classify_tables(table_htmls, gen_model, gen_endpoint, pdf_path)
    filtered_table_dicts = {
        idx: {
            'html': html,
            'caption': caption,
            'summary': summary
        }
        for idx, (keep, html, caption, summary) in enumerate(zip(decisions, table_htmls, table_captions_list, table_summaries)) if keep
    }
    table_count = len(filtered_table_dicts)
    out_path.write_text(json.dumps(filtered_table_dicts, indent=2), encoding="utf-8")
    process_time = time.time() - t0

    return table_count, process_time

def process_converted_document(converted_json_path, pdf_path, out_path, conversion_stats, gen_model, gen_endpoint, emb_endpoint, max_tokens):    
    stem = Path(pdf_path).stem
    processed_text_json_path = (Path(out_path) / f"{stem}{text_suffix}")
    processed_table_json_path = (Path(out_path) / f"{stem}{table_suffix}")

    if conversion_stats["text_processed"] and conversion_stats["table_processed"]:
        logger.debug(f"Text & Table of {pdf_path} is processed already!")
        page_count = get_pdf_page_count(pdf_path)
        table_count = processed_table_json_path.exists() and len(json.load(processed_table_json_path.open())) or 0
        return pdf_path, processed_text_json_path, processed_table_json_path, page_count, table_count, {}

    try:
        timings = {}
        converted_doc = None
        page_count = 0
        table_count = 0

        logger.debug("Loading from converted json")

        converted_doc = DoclingDocument.load_from_json(Path(converted_json_path))
        if not converted_doc:
            raise Exception(f"failed to load converted json into Docling Document")

        if not conversion_stats["text_processed"]:
            page_count, process_time = process_text(converted_doc, pdf_path, processed_text_json_path)
            timings["process_text"] = process_time

        if not conversion_stats["table_processed"]:
            table_count, process_time = process_table(converted_doc, pdf_path, processed_table_json_path, gen_model, gen_endpoint)
            timings["process_tables"] = process_time

        return pdf_path, processed_text_json_path, processed_table_json_path, page_count, table_count, timings
    except Exception as e:
        logger.error(f"Error processing converted document for PDF: {pdf_path}. Details: {e}", exc_info=True)

        return None, None, None, None, None, None

def convert_document(pdf_path, conversion_stats, out_path):
    try:
        logger.info(f"Processing '{pdf_path}'")
        converted_json = (Path(out_path) / f"{Path(pdf_path).stem}.json")
        converted_json_f = str(converted_json)
        if not conversion_stats["convert"]:
            return pdf_path, converted_json_f, 0.0

        logger.debug(f"Converting '{pdf_path}'")
        t0 = time.time()

        converted_doc = convert_doc(pdf_path).document
        converted_doc.save_as_json(str(converted_json_f))

        conversion_time = time.time() - t0
        logger.debug(f"'{pdf_path}' converted")
        return pdf_path, converted_json_f, conversion_time
    except Exception as e:
        logger.error(f"Error converting '{pdf_path}': {e}")
    return None, None, None

# ====================================================
# refactored process_documents
def process_documents(job_id, input_paths, out_path, doc_id_dict, llm_model, llm_endpoint, emb_endpoint, max_tokens):
    # Skip files that already exist by matching the cached checksum of the pdf
    # if there is no difference in checksum and processed text & table json also exist, would skip for convert and process list
    # if checksum is matching but either processed text or table json not exist, process the file, but don't convert
    # else add the file to convert and process list(filtered_input_paths)

    filtered_input_paths = {}
    for path in input_paths:
        stem = Path(path).stem
        checksum_path = Path(out_path) / f"{stem}.checksum"

        meta = {"convert": True}
        if checksum_path.exists():
            cached_cs = checksum_path.read_text().strip()
            if cached_cs == generate_file_checksum(path):
                meta["convert"] = not (Path(out_path) / f"{stem}.json").exists()

        filtered_input_paths[path] = meta

    # Update checksums for files marked for conversion
    for path in filtered_input_paths:
        if filtered_input_paths[path]["convert"]:
            checksum = generate_file_checksum(path)
            (Path(out_path) / f"{Path(path).stem}.checksum").write_text(checksum, encoding='utf-8')

    # Partition files into light and heavy based on page count
    light_files, heavy_files = {}, {}
    for path, meta in filtered_input_paths.items():
        pg_count = get_pdf_page_count(path)
        if pg_count >= HEAVY_PDF_PAGE_THRESHOLD:
            heavy_files[path] = meta
        else:
            light_files[path] = meta

    status_mgr = StatusManager(job_id)

    def _run_batch(batch_paths, convert_worker, max_worker):
        batch_stats = {}
        batch_chunk_paths = []
        batch_table_paths = []
        converted_paths = []

        if not batch_paths:
            return batch_stats, batch_chunk_paths, batch_table_paths

        with ProcessPoolExecutor(max_workers=convert_worker) as converter_executor, \
             ThreadPoolExecutor(max_workers=max_worker) as processor_executor, \
             ThreadPoolExecutor(max_workers=max_worker) as chunker_executor:

            # A. Submit Conversions
            conversion_futures = {
                converter_executor.submit(convert_document, path, batch_paths[path], out_path): path
                for path in batch_paths
            }

            process_futures = {}
            chunk_futures = {}

            # B. Handle Conversions -> Submit Processing
            for fut in as_completed(conversion_futures):
                path = conversion_futures[fut]
                doc_id = doc_id_dict[path]
                try:
                    path, converted_json, conv_time = fut.result()

                    if not converted_json:
                        continue

                    # Update persistence and session stats
                    converted_paths.append(path) 
                    batch_stats[path] = {"timings": {"digitizing": round(conv_time, 2)}}

                    status_mgr.update_doc_metadata(doc_id, {
                        "status": DocStatus.IN_PROGRESS.value,
                        "timing_in_secs": {"digitizing": round(conv_time, 2)}
                    })
                    status_mgr.update_job_progress(doc_id, DocStatus.IN_PROGRESS)

                    p_future = processor_executor.submit(
                        process_converted_document, converted_json, path, out_path,
                        batch_paths[path], llm_model, llm_endpoint, emb_endpoint, max_tokens
                    )
                    process_futures[p_future] = path
                except Exception as e:
                    logger.error(f"Error from conversion for {path}: {e}")
                    status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED.value, "error": str(e)})

            # C. Handle Processing -> Submit Chunking
            for fut in as_completed(process_futures):
                path = process_futures[fut]
                doc_id = doc_id_dict[path]
                try:
                    path, txt_json, tab_json, pgs, tabs, timings = fut.result()

                    if not tab_json:
                        continue

                    batch_stats[path].update({
                        "page_count": pgs,
                        "table_count": tabs,
                        "timings": {**batch_stats[path]["timings"], **timings}
                    })
                    batch_table_paths.append(tab_json)
                    status_mgr.update_doc_metadata(doc_id, {"pages": pgs, "tables": tabs})

                    c_future = chunker_executor.submit(
                        chunk_single_file, txt_json, path, out_path, batch_paths[path],
                        emb_endpoint, max_tokens
                    )
                    chunk_futures[c_future] = (path, tab_json)
                except Exception as e:
                    logger.error(f"Error from processing for {path}: {e}")
                    status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED.value, "error": str(e)})

            # D. Handle Chunking
            for fut in as_completed(chunk_futures):
                path, tab_json = chunk_futures[fut]
                doc_id = doc_id_dict[path]
                try:
                    chunk_json, _, chunk_time = fut.result()
                    batch_stats[path]["timings"]["chunking"] = round(chunk_time, 2)

                    if chunk_json:
                        batch_chunk_paths.append(chunk_json)
                        # Capture chunk counts in real time and update <doc_id>_metadata.json
                        final_chunks = create_chunk_documents(chunk_json, tab_json, path)
                        batch_stats[path]["chunk_count"] = len(final_chunks)

                        status_mgr.update_doc_metadata(doc_id, {
                            "status": DocStatus.COMPLETED.value,
                            "completed_at": status_mgr._get_timestamp(),
                            "chunks": len(final_chunks),
                            "timing_in_secs": {"chunking": round(chunk_time, 2)}
                        })
                        status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED)
                except Exception as e:
                    logger.error(f"Error from chunking for {path}: {e}")

        return batch_stats, batch_chunk_paths, batch_table_paths

    # Trigger the batches
    try:
        # Process Light Batch
        l_worker = min(WORKER_SIZE, len(light_files)) if light_files else 0
        l_stats, l_chunks_json, l_tabs_json = _run_batch(
            light_files, convert_worker=l_worker, max_worker=l_worker
        )

        # Process Heavy Batch
        h_worker = min(WORKER_SIZE, len(heavy_files)) if heavy_files else 0
        h_conv_worker = min(HEAVY_PDF_CONVERT_WORKER_SIZE, len(heavy_files)) if heavy_files else 0
        h_stats, h_chunks_json, h_tabs_json = _run_batch(
            heavy_files, convert_worker=h_conv_worker, max_worker=h_worker
        )

        # Combine statistics for the final return
        converted_pdf_stats = {**l_stats, **h_stats}
        all_chunk_json_paths = l_chunks_json + h_chunks_json
        all_table_json_paths = l_tabs_json + h_tabs_json


        combined_chunks = []
        # Final assembly: create_chunk_documents merges text/table outputs
        succeeded_files = converted_pdf_stats.keys()

        for path in succeeded_files:
            doc_id = doc_id_dict[path]
            stem = Path(path).stem
            c_path = Path(out_path) / f"{stem}_chunks.json" # Adjust suffix based on your constants
            t_path = Path(out_path) / f"{stem}_tables.json"

            # Verify the file was actually processed in the batch
            if c_path in all_chunk_json_paths and t_path in all_table_json_paths:
                # Re-invoke assembly if not already done in _run_batch
                # or use the combined_docs gathered during the batchs
                doc_chunks = create_chunk_documents(c_path, t_path, path)
                combined_chunks.extend(doc_chunks)

                # Final Status "Seal" for the document
                status_mgr.update_doc_metadata(doc_id, {
                    "status": DocStatus.COMPLETED.value,
                    "completed_at": status_mgr._get_timestamp(),
                    "chunks": len(doc_chunks)
                })
                status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED)
            else:
                logger.warning(f"Path mismatch for {path}: expected outputs not found in batch results.")

        return combined_chunks, converted_pdf_stats

    except Exception as e:
        logger.error(f"Error while processing the documents in job {job_id}: {e}")
        # In case of failure, mark all remaining docs in the job as failed
        return [], {}

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


def chunk_single_file(input_path, pdf_path, out_path, conversion_stats, emb_endpoint, max_tokens=512):
    t0 = time.time()
    stem = Path(pdf_path).stem
    processed_chunk_json_path = (Path(out_path) / f"{stem}{chunk_suffix}")

    if conversion_stats["chunked"]:
        logger.debug(f"{pdf_path} already chunked!")
        return processed_chunk_json_path, pdf_path, 0.0

    try:
        if not Path(processed_chunk_json_path).exists():
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

            for idx, block in enumerate(tqdm_wrapper(data, desc=f"Chunking {input_path}")):
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
            with open(processed_chunk_json_path, "w") as f:
                json.dump(chunks, f, indent=2)

            logger.debug(f"{len(chunks)} RAG chunks saved to {processed_chunk_json_path}")
        else:
            logger.debug(f"{processed_chunk_json_path} already exists.")
        return processed_chunk_json_path, pdf_path, time.time() - t0
    except Exception as e:
        logger.error(f"error chunking file '{input_path}': {e}")
    return None, None, None

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
