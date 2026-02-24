from glob import glob
import logging
import time

import common.db_utils as db
from common.emb_utils import get_embedder
from common.misc_utils import *
from digitize.doc_utils import process_documents

logger = get_logger("ingest")

def ingest(directory_path):

    def ingestion_failed():
        logger.info("❌ Ingestion failed, please re-run the ingestion again, If the issue still persists, please report an issue in https://github.com/IBM/project-ai-services/issues")

    logger.info(f"Ingestion started from dir '{directory_path}'")

    # Process each document in the directory
    allowed_file_types = {'pdf': b'%PDF'}
    input_file_paths = []
    total_pdfs = 0

    for path in glob(f'{directory_path}/**/*', recursive=True):
        if not has_allowed_extension(path, allowed_file_types):
            continue

        total_pdfs += 1 

        if is_supported_file(path,allowed_file_types):
            input_file_paths.append(path)
        else:
            logger.warning(
                f"Skipping file with .pdf extension but unsupported format: {path}"
            )
    
    file_cnt = len(input_file_paths)
    if not file_cnt > 0:
        logger.info(f"No documents found to process in '{directory_path}'")
        return

    logger.info(f"Processing {file_cnt} document(s)")

    emb_model_dict, llm_model_dict, _ = get_model_endpoints()
    # Initialize/reset the database before processing any files
    vector_store = db.get_vector_store()
    index_name = vector_store.index_name
    
    out_path = setup_cache_dir(index_name)

    start_time = time.time()
    combined_chunks, converted_pdf_stats = process_documents(
        input_file_paths, out_path, llm_model_dict['llm_model'], llm_model_dict['llm_endpoint'],  emb_model_dict["emb_endpoint"],
        max_tokens=emb_model_dict['max_tokens'] - 100)
    # converted_pdf_stats holds { file_name: {page_count: int, table_count: int, timings: {conversion: time_in_secs, process_text: time_in_secs, process_tables: time_in_secs, chunking: time_in_secs}} }
    if converted_pdf_stats is None or combined_chunks is None:
        ingestion_failed()
        return

    if combined_chunks:
        logger.info("Loading processed documents into DB")
        embedder = get_embedder(emb_model_dict['emb_model'], emb_model_dict['emb_endpoint'], emb_model_dict['max_tokens'])
        # Insert data into Opensearch
        vector_store.insert_chunks(
            combined_chunks,
            embedder=embedder
        )
        logger.info("Processed documents loaded into DB")

    # Log time taken for the file
    end_time = time.time()  # End the timer for the current file
    file_processing_time = end_time - start_time
    
    unprocessed_files = get_unprocessed_files(input_file_paths, converted_pdf_stats.keys())
    if len(unprocessed_files):
        logger.info(f"Ingestion completed partially, please re-run the ingestion again to ingest the following files.\n{"\n".join(unprocessed_files)}\nIf the issue still persists, please report an issue in https://github.com/IBM/project-ai-services/issues")
    else:
        logger.info(f"✅ Ingestion completed successfully, Time taken: {file_processing_time:.2f} seconds. You can query your documents via chatbot")
    
    ingested = file_cnt - len(unprocessed_files)
    percentage = (ingested / total_pdfs * 100) if total_pdfs else 0.0
    logger.info(
        f"Ingestion summary: {ingested}/{total_pdfs} files ingested "
        f"({percentage:.2f}% of total PDF files)"
    )
    return converted_pdf_stats
