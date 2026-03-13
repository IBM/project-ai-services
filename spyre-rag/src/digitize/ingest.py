from pathlib import Path
import time
from typing import Optional
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

import common.db_utils as db
from common.emb_utils import get_embedder
from common.misc_utils import *
from digitize.doc_utils import process_documents, clean_intermediate_files
from digitize.status import StatusManager, get_utc_timestamp
from digitize.types import JobStatus, DocStatus

logger = get_logger("ingest")

def _index_single_document(doc_id, doc_chunks, vector_store, embedder, status_mgr, out_path):
    """
    Index a single document's chunks into the vector store.
    This function runs in a separate thread for parallel indexing.

    Returns:
        float: Time taken to index the document in seconds
    """
    try:
        logger.info(f"Indexing document {doc_id} into vector db")
        index_start_time = time.time()

        # Index the chunks
        vector_store.insert_chunks(doc_chunks, embedding=embedder)

        index_time = time.time() - index_start_time

        # Update status to COMPLETED after successful indexing
        status_mgr.update_doc_metadata(doc_id, {
            "status": DocStatus.COMPLETED,
            "completed_at": get_utc_timestamp(),
            "timing_in_secs": {"indexing": round(float(index_time), 2)}
        })
        status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED, JobStatus.IN_PROGRESS)

        # Clean up intermediate files after successful indexing
        clean_intermediate_files(doc_id, out_path)
        logger.debug(f"Preserved {doc_id}.json for future GET requests")

        return index_time

    except Exception as e:
        logger.error(f"Failed to index document {doc_id}: {str(e)}", exc_info=True)
        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"Failed to index document: {str(e)}")
        status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.FAILED, error=f"Failed to index document: {str(e)}")
        raise

def ingest(directory_path: Path, job_id: Optional[str] = None, doc_id_dict: Optional[dict] = None):

    def ingestion_failed():
        logger.info("❌ Ingestion failed, please re-run the ingestion again, If the issue still persists, please report an issue in https://github.com/IBM/project-ai-services/issues")

    logger.info(f"Ingestion started from dir '{directory_path}'")

    # Initialize status manager
    status_mgr = None
    if job_id:
        status_mgr = StatusManager(job_id)
        status_mgr.update_job_progress("", DocStatus.ACCEPTED, JobStatus.IN_PROGRESS)
        logger.info(f"Job {job_id} status updated to IN_PROGRESS")

    try:
        # Files are already staged and validated at API level in app.py
        # Just collect the PDF files from the staging directory
        input_file_paths = [str(p) for p in directory_path.glob("*.pdf")]

        total_pdfs = len(input_file_paths)

        logger.info(f"Processing {total_pdfs} document(s)")

        emb_model_dict, llm_model_dict, _ = get_model_endpoints()

        # Setup output directory for digitized documents
        out_path = setup_digitized_doc_dir()

        # Initialize vector store and embedder for parallel indexing
        vector_store = db.get_vector_store()
        embedder = get_embedder(emb_model_dict['emb_model'], emb_model_dict['emb_endpoint'], emb_model_dict['max_tokens'])

        # Thread pool for parallel indexing
        index_executor = ThreadPoolExecutor(max_workers=4)
        index_futures = {}

        def index_document(doc_id, doc_chunks):
            """Callback function to index a document in parallel"""
            future = index_executor.submit(_index_single_document,
                                          doc_id, doc_chunks, vector_store, embedder,
                                          status_mgr, out_path)
            index_futures[future] = doc_id
            logger.debug(f"Submitted document {doc_id} for parallel indexing")

        start_time = time.time()
        converted_pdf_stats = process_documents(
            input_file_paths, out_path, llm_model_dict['llm_model'], llm_model_dict['llm_endpoint'],  emb_model_dict["emb_endpoint"],
            max_tokens=emb_model_dict['max_tokens'] - 100, job_id=job_id, doc_id_dict=doc_id_dict,
            index_callback=index_document)
        # converted_pdf_stats holds { file_name: {page_count: int, table_count: int, timings: {conversion: time_in_secs, process_text: time_in_secs, process_tables: time_in_secs, chunking: time_in_secs}} }
        if converted_pdf_stats is None:
            index_executor.shutdown(wait=False)
            ingestion_failed()
            return

        # Wait for all indexing operations to complete
        logger.info("Waiting for all documents to finish indexing...")
        for future in as_completed(index_futures):
            doc_id = index_futures[future]
            try:
                index_time = future.result()
                logger.info(f"Document {doc_id} indexed successfully in {index_time:.2f}s")
            except Exception as e:
                logger.error(f"Failed to index document {doc_id}: {str(e)}", exc_info=True)

        index_executor.shutdown(wait=True)

        # Log time taken for the file
        end_time = time.time()  # End the timer for the current file
        file_processing_time = end_time - start_time

        unprocessed_files = get_unprocessed_files(input_file_paths, converted_pdf_stats.keys())
        if len(unprocessed_files):
            logger.info(f"Ingestion completed partially, please re-run the ingestion again to ingest the following files.\n{"\n".join(unprocessed_files)}\nIf the issue still persists, please report an issue in https://github.com/IBM/project-ai-services/issues")
        else:
            logger.info(f"✅ Ingestion completed successfully, Time taken: {file_processing_time:.2f} seconds. You can query your documents via chatbot")

        ingested = total_pdfs - len(unprocessed_files)
        percentage = (ingested / total_pdfs * 100) if total_pdfs else 0.0
        logger.info(
            f"Ingestion summary: {ingested}/{total_pdfs} files ingested "
            f"({percentage:.2f}% of total PDF files)"
        )

        # Update job status to COMPLETED if all documents processed successfully
        if status_mgr and not unprocessed_files:
            logger.info(f"All documents processed successfully, updating job {job_id} status to COMPLETED")
            status_mgr.update_job_progress("", DocStatus.COMPLETED, JobStatus.COMPLETED)

        return converted_pdf_stats

    except Exception as e:
        logger.error(f"Error during ingestion: {str(e)}", exc_info=True)
        ingestion_failed()

        # Update status to FAILED for all documents in this job
        if status_mgr and doc_id_dict:
            for doc_id in doc_id_dict.values():
                logger.debug(f"Ingestion failed: updating doc & job metadata to FAILED for document: {doc_id}")
                status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"Ingestion failed: {str(e)}")
                status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.FAILED, error=f"Ingestion failed: {str(e)}")

        return None
