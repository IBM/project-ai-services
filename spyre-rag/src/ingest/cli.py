import logging
import time
from glob import glob
import argparse

from common.misc_utils import *

def reset_db():
    vector_store = MilvusVectorStore()
    vector_store.reset_collection()
    logger.info(f"✅ DB Cleaned successfully!")

def ingest(directory_path):
    logger.info(f"Ingestion started from dir '{directory_path}'")

    # Process each document in the directory
    allowed_file_types = ['pdf']
    input_file_paths = []
    for f_type in allowed_file_types:
        input_file_paths.extend(glob(f'{directory_path}/**/*.{f_type}', recursive=True))

    file_cnt = len(input_file_paths)
    if not file_cnt > 0:
        logger.info(f"No documents found to process in '{directory_path}'")
        return

    logger.info(f"Processing {file_cnt} documents")

    emb_model_dict, llm_model_dict, _ = get_model_endpoints()
    # Initialize/reset the database before processing any files
    vector_store = MilvusVectorStore()
    collection_name = vector_store._generate_collection_name()
    
    out_path = setup_cache_dir(collection_name)

    start_time = time.time()
    converted_files = extract_document_data(
        input_file_paths, out_path, llm_model_dict['llm_model'], llm_model_dict['llm_endpoint'])
    logger.debug(f"Converted files: {converted_files}")

    original_filenames, input_txt_files, input_tab_files = get_txt_tab_filenames(converted_files, out_path)
    chunk_files = [f.replace(text_suffix, chunk_suffix) for f in input_txt_files]
    chunked_files = hierarchical_chunk_with_token_split(
        input_txt_files, chunk_files, emb_model_dict["emb_endpoint"],
        max_tokens=emb_model_dict['max_tokens'] - 100
    )
    logger.debug(f"Chunked files: {chunked_files}")

    combined_filtered_chunks = []
    for in_chunk_f, in_tab_f, orig_fn in zip(chunked_files, input_tab_files, original_filenames):
        # Combine all chunks (text, image summaries, table summaries)
        filtered_chunks = create_chunk_documents(
            in_chunk_f, in_tab_f, orig_fn)
        combined_filtered_chunks.extend(filtered_chunks)

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
    logger.debug(f"Time taken to ingest the documents into vector DB is: {file_processing_time:.2f} seconds")
    
    unprocessed_files = get_unprocessed_files(input_file_paths, chunked_files)
    if len(unprocessed_files):
        logger.info(f"Ingestion completed partially, please re-run the ingestion again to ingest the following files.\n{"\n".join(unprocessed_files)}\nIf the issue still persists, please report an issue in https://github.com/IBM/project-ai-services/issues")
    else:
        logger.info(f"✅ Ingestion completed successfully, you can query your documents via chatbot")

common_parser = argparse.ArgumentParser(add_help=False)
common_parser.add_argument("--debug", action="store_true", help="Enable debug logging")

parser = argparse.ArgumentParser(description="Data Ingestion CLI", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])
command_parser = parser.add_subparsers(dest="command", required=True)

ingest_parser = command_parser.add_parser("ingest", help="Ingest the DOCs", description="Ingest the DOCs into Milvus after all the processing\n", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])
ingest_parser.add_argument("--path", type=str, default="/var/docs", help="Path to the documents that needs to be ingested into the RAG")

command_parser.add_parser("clean-db", help="Clean the DB", description="Clean the Milvus DB\n", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])

command_args = parser.parse_args()
if command_args.debug:
    set_log_level(logging.DEBUG)
else:
    set_log_level(logging.INFO)


from common.db_utils import MilvusVectorStore
from ingest.doc_utils import extract_document_data, hierarchical_chunk_with_token_split, create_chunk_documents

logger = get_logger("Ingest")

if command_args.command == "ingest":
    ingest(command_args.path)
elif command_args.command == "clean-db":
    reset_db()

