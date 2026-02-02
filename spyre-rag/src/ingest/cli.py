import logging
import os
import time
from glob import glob
import argparse

from common.misc_utils import *

def reset_db():
    vector_store = MilvusVectorStore()
    vector_store.reset_collection()
    logger.info(f"✅ DB Cleaned successfully!")

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
    vector_store = MilvusVectorStore()
    collection_name = vector_store._generate_collection_name()
    
    out_path = setup_cache_dir(collection_name)

    start_time = time.time()
    converted_files, converted_pdf_stats = extract_document_data(
        input_file_paths, out_path, llm_model_dict['llm_model'], llm_model_dict['llm_endpoint'])
    if not converted_files:
        ingestion_failed()
        return
    logger.debug(f"Converted files: {converted_files}")

    original_filenames, input_txt_files, input_tab_files = get_txt_tab_filenames(converted_files, out_path)
    chunk_files = [f.replace(text_suffix, chunk_suffix) for f in input_txt_files]
    chunked_files = hierarchical_chunk_with_token_split(
        input_txt_files, chunk_files, emb_model_dict["emb_endpoint"],
        max_tokens=emb_model_dict['max_tokens'] - 100
    )
    if not chunked_files:
        ingestion_failed()
        return
    logger.debug(f"Chunked files: {chunked_files}")

    combined_filtered_chunks = []
    for in_chunk_f, in_tab_f, orig_fn in zip(chunked_files, input_tab_files, original_filenames):
        # Combine all chunks (text, image summaries, table summaries)
        filtered_chunks = create_chunk_documents(
            in_chunk_f, in_tab_f, orig_fn)
        combined_filtered_chunks.extend(filtered_chunks)

    if not combined_filtered_chunks:
        ingestion_failed()
        return

    logger.info("Loading converted documents into DB")
    # Insert data into Milvus
    vector_store.insert_chunks(
        emb_model=emb_model_dict['emb_model'],
        emb_endpoint=emb_model_dict['emb_endpoint'],
        max_tokens=emb_model_dict['max_tokens'],
        chunks=combined_filtered_chunks
    )
    logger.info("Converted documents loaded into DB")

    # Log time taken for the file
    end_time = time.time()  # End the timer for the current file
    file_processing_time = end_time - start_time
    
    unprocessed_files = get_unprocessed_files(input_file_paths, chunked_files)
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

    if not converted_pdf_stats:
        return
    logger.info(f"Stats of processed PDFs:")
    max_file_len = max(len(key) for key in converted_pdf_stats.keys())
    total_pages = sum(converted_pdf_stats[file]["page_count"] for file in converted_pdf_stats)
    total_tables = sum(converted_pdf_stats[file]["table_count"] for file in converted_pdf_stats)

    header_format = f"| {"PDF":<{max_file_len}} | {"Total Pages":^{15}} | {"Total Tables":>{15}} |"
    print("-" * len(header_format))
    print(header_format)
    print("-" * len(header_format))
    for file in converted_pdf_stats:
        print(f"| {file:<{max_file_len}} | {converted_pdf_stats[file]["page_count"]:^{15}} | {converted_pdf_stats[file]["table_count"]:>{15}} |")
    print("-" * len(header_format))
    print(f"| {"Total":<{max_file_len}} | {total_pages:^{15}} | {total_tables:>{15}} |")
    print("-" * len(header_format))

common_parser = argparse.ArgumentParser(add_help=False)
common_parser.add_argument("--debug", action="store_true", help="Enable debug logging")

parser = argparse.ArgumentParser(description="Data Ingestion CLI", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])
command_parser = parser.add_subparsers(dest="command", required=True)

ingest_parser = command_parser.add_parser("ingest", help="Ingest the DOCs", description="Ingest the DOCs into Milvus after all the processing\n", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])
ingest_parser.add_argument("--path", type=str, default="/var/docs", help="Path to the documents that needs to be ingested into the RAG")

command_parser.add_parser("clean-db", help="Clean the DB", description="Clean the Milvus DB\n", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])

# Setting log level, 1st priority is to the flag received via cli, 2nd priority to the LOG_LEVEL env var.
log_level = logging.INFO

env_log_level = os.getenv("LOG_LEVEL", "")
if "debug" in env_log_level.lower():
    log_level = logging.DEBUG

command_args = parser.parse_args()
if command_args.debug:
    log_level = logging.DEBUG

set_log_level(log_level)

from common.db_utils import MilvusVectorStore
from ingest.doc_utils import extract_document_data, hierarchical_chunk_with_token_split, create_chunk_documents

logger = get_logger("Ingest")

if command_args.command == "ingest":
    ingest(command_args.path)
elif command_args.command == "clean-db":
    reset_db()
