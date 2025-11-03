import logging
import time
from glob import glob
from tqdm import tqdm
import argparse

from common.misc_utils import setup_cache_dir, set_log_level, get_logger, get_txt_tab_filenames, get_model_endpoints
from common.db_utils import MilvusVectorStore
from ingest.doc_utils import extract_document_data, hierarchical_chunk_with_token_split, create_chunk_documents

logger = get_logger("ingest")

def reset_db():
    vector_store = MilvusVectorStore()
    collection_name = vector_store._generate_collection_name()

    vector_store.reset_collection()
    logger.info(f"✅ Cleaned Vector DB: {collection_name}")

def ingest(directory_path, include_meta_info_in_main_text):
    logger.info(f"Ingestion started from dir '{directory_path}'")
    emb_model_dict, llm_model_dict, _ = get_model_endpoints()
    # Initialize/reset the database before processing any files
    vector_store = MilvusVectorStore()
    collection_name = vector_store._generate_collection_name()

    # Process each document in the directory
    allowed_file_types = ['pdf']
    input_file_paths = []
    for f_type in allowed_file_types:
        input_file_paths.extend(glob(f'{directory_path}/**/*.{f_type}', recursive=True))
    input_files_being_processed = '\n'.join(f for f in input_file_paths)
    logger.info(f"Processing following files: {input_files_being_processed}")
    
    out_path = setup_cache_dir(collection_name)

    start_time = time.time()
    extract_document_data(
        input_file_paths, out_path, llm_model_dict['llm_model'], llm_model_dict['llm_endpoint'])

    original_filenames, input_txt_files, input_tab_files = get_txt_tab_filenames(input_file_paths, out_path)
    output_chunk_files = [f.replace('_clean_text.json', '_clean_chunk.json') for f in input_txt_files]
    hierarchical_chunk_with_token_split(
        input_txt_files, output_chunk_files, llm_model_dict["llm_endpoint"],
        max_tokens=emb_model_dict['max_tokens'] - 100 if include_meta_info_in_main_text else emb_model_dict['max_tokens']
    )
    combined_filtered_chunks = []
    for in_txt_f, in_tab_f, orig_fn in zip(input_txt_files, input_tab_files, original_filenames):
        # Combine all chunks (text, image summaries, table summaries)
        filtered_chunks = create_chunk_documents(
            in_txt_f, in_tab_f, orig_fn, include_meta_info_in_main_text)
        combined_filtered_chunks.extend(filtered_chunks)

    # Insert data into Milvus
    vector_store.insert_chunks(
        emb_model=emb_model_dict['emb_model'],
        emb_endpoint=emb_model_dict['emb_endpoint'],
        max_tokens=emb_model_dict['max_tokens'],
        chunks=combined_filtered_chunks
    )

    logger.debug(f"Inserted {len(combined_filtered_chunks)} chunks to the vector DB: {collection_name}")

    # Log time taken for the file
    end_time = time.time()  # End the timer for the current file
    file_processing_time = end_time - start_time
    logger.info(f"Time taken to ingest the documents into vector DB is: {file_processing_time:.2f} seconds")

    logger.info(f"✅ Ingestion completed successfully!")

def main():
    parser = argparse.ArgumentParser(description="Data Ingestion CLI", formatter_class=argparse.RawTextHelpFormatter)
    parser.add_argument("--debug", action="store_true", help="Enable debug logging")

    command_parser = parser.add_subparsers(dest="command", required=True)

    ingest_parser = command_parser.add_parser("ingest", help="Ingest the DOCs", description="Ingest the DOCs into Milvus after all the processing\n", formatter_class=argparse.RawTextHelpFormatter)
    ingest_parser.add_argument("--path", type=str, default="/var/docs", help="Path to the documents that needs to be ingested into the RAG")
    ingest_parser.add_argument("--include-meta", action="store_true", help="Include meta info while ingesting the docs")

    command_parser.add_parser("clean-db", help="Clean the DB", description="Clean the Milvus DB\n", formatter_class=argparse.RawTextHelpFormatter)

    command_args = parser.parse_args()
    if command_args.debug:
        set_log_level(logging.DEBUG)
    else:
        set_log_level(logging.INFO)

    if command_args.command == "ingest":
        ingest(command_args.path, command_args.include_meta)
    elif command_args.command == "clean-db":
        reset_db()

if __name__ == "__main__":
    main()
