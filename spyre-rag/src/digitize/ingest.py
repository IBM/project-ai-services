from pathlib import Path
import time
from typing import Optional

import common.db_utils as db
from common.emb_utils import get_embedder
from common.misc_utils import *
from digitize.doc_utils import process_documents
from digitize.status import StatusManager, get_utc_timestamp
from digitize.types import JobStatus, DocStatus

logger = get_logger("ingest")

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

        # Initialize/reset the database before processing any files
        vector_store = db.get_vector_store()
        out_path = setup_digitized_doc_dir()

        start_time = time.time()
        combined_chunks, converted_pdf_stats = process_documents(
            input_file_paths, out_path, llm_model_dict['llm_model'], llm_model_dict['llm_endpoint'],  emb_model_dict["emb_endpoint"],
            max_tokens=emb_model_dict['max_tokens'] - 100, job_id=job_id, doc_id_dict=doc_id_dict)
        # converted_pdf_stats holds { file_name: {page_count: int, table_count: int, timings: {conversion: time_in_secs, process_text: time_in_secs, process_tables: time_in_secs, chunking: time_in_secs}} }
        if converted_pdf_stats is None or combined_chunks is None:
            ingestion_failed()
            return

        if combined_chunks:
            # Always index documents - treating each request as fresh
            logger.info("Loading processed documents into DB")

            embedder = get_embedder(emb_model_dict['emb_model'], emb_model_dict['emb_endpoint'], emb_model_dict['max_tokens'])
            # Insert data into Opensearch
            vector_store.insert_chunks(
                combined_chunks,
                embedding=embedder
            )
            logger.info("Processed documents loaded into DB")

            # Update status to COMPLETED for successfully indexed documents
            if status_mgr and doc_id_dict:
                for path in converted_pdf_stats.keys():
                    from pathlib import Path
                    doc_id = doc_id_dict.get(Path(path).name)
                    if doc_id:
                        logger.debug(f"Indexing Done: updating doc & job metadata to COMPLETED for document: {doc_id}")
                        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.COMPLETED, "completed_at": get_utc_timestamp()})
                        status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED, JobStatus.COMPLETED)

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
