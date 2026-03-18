from pathlib import Path
import time
from typing import Optional

import common.db_utils as db
from common.emb_utils import get_embedder
from common.misc_utils import *
from digitize.doc_utils import process_documents
from digitize.status import StatusManager, get_utc_timestamp, get_job_document_stats
from digitize.types import JobStatus, DocStatus
import digitize.config as config

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
            logger.info("Loading processed documents into vector DB")

            embedder = get_embedder(emb_model_dict['emb_model'], emb_model_dict['emb_endpoint'], emb_model_dict['max_tokens'])
            # Insert data into Opensearch
            vector_store.insert_chunks(
                combined_chunks,
                embedding=embedder
            )
            logger.info("Processed documents loaded into Vector DB")

            # Mark successfully indexed documents as COMPLETED
            if status_mgr and doc_id_dict:
                for path in converted_pdf_stats.keys():
                    from pathlib import Path
                    doc_id = doc_id_dict.get(Path(path).name)
                    if doc_id:
                        logger.debug(f"Indexing Done: updating doc metadata to COMPLETED for document: {doc_id}")
                        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.COMPLETED, "completed_at": get_utc_timestamp()})
                        status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED, JobStatus.IN_PROGRESS)

        # Log time taken for the file
        end_time: float = time.time()  # End the timer for the current file
        file_processing_time = end_time - start_time

        # Determine final job status by reading actual document statuses from job status file
        if status_mgr and job_id:
            doc_stats = get_job_document_stats(job_id)
            failed_docs = doc_stats["failed_docs"]
            completed_docs = doc_stats["completed_docs"]

            if len(failed_docs) > 0:
                # At least one document failed
                failed_doc_ids = [doc["id"] for doc in failed_docs]
                failed_doc_ids_list = ", ".join(failed_doc_ids)
                failed_doc_names = [doc["name"] for doc in failed_docs]
                failed_files_list = "\n".join(failed_doc_names)

                # Detailed error message for logs
                detailed_error_message = (
                    f"Ingestion completed partially. {len(failed_docs)} document(s) failed to process.\n"
                    f"Failed documents:\n{failed_files_list}\n"
                    f"Please submit a new ingestion job to process these documents. "
                    f"If the issue persists, please report at https://github.com/IBM/project-ai-services/issues"
                )
                logger.warning(detailed_error_message)
                logger.info(
                    f"Ingestion summary: {len(completed_docs)}/{total_pdfs} files ingested "
                    f"({len(completed_docs) / total_pdfs * 100:.2f}% of total PDF files)"
                )

                # User-friendly error message for job status
                job_error_message = (
                    f"{len(failed_docs)} document(s) failed to ingest. "
                    f"Perform GET /v1/documents/{{id}} to know the reason for failure. Failed document ids: {failed_doc_ids_list}"
                )

                logger.debug(f"Some documents failed to process, updating job {job_id} status to FAILED")
                status_mgr.update_job_progress("", DocStatus.FAILED, JobStatus.FAILED, error=job_error_message)
            else:
                # All documents completed successfully
                logger.info(f"✅ Ingestion completed successfully, Time taken: {file_processing_time:.2f} seconds. You can query your documents via chatbot")
                logger.info(
                    f"Ingestion summary: {len(completed_docs)}/{total_pdfs} files ingested "
                    f"(100.00% of total PDF files)"
                )

                logger.debug(f"All documents processed successfully, updating job {job_id} status to COMPLETED")
                status_mgr.update_job_progress("", DocStatus.COMPLETED, JobStatus.COMPLETED)

        return converted_pdf_stats

    except Exception as e:
        logger.error(f"Error during ingestion: {str(e)}", exc_info=True)
        ingestion_failed()

        # Update status to FAILED only for documents that haven't been processed yet
        if status_mgr and doc_id_dict and job_id:
            try:
                doc_stats = get_job_document_stats(job_id)
                processed_doc_ids = set(
                    [doc["id"] for doc in doc_stats["completed_docs"]] +
                    [doc["id"] for doc in doc_stats["failed_docs"]]
                )

                # Only mark unprocessed documents as failed
                for doc_id in doc_id_dict.values():
                    if doc_id not in processed_doc_ids:
                        logger.debug(f"Catastrophic error: marking unprocessed document {doc_id} as FAILED")
                        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"Ingestion failed: {str(e)}")
                        status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)
            except FileNotFoundError as fnf_error:
                logger.error(f"Job status file not found during error handling: {fnf_error}")
                # If job status file is missing, mark all documents as failed
                for doc_id in doc_id_dict.values():
                    logger.debug(f"Catastrophic error (no status file): marking document {doc_id} as FAILED")
                    status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"Ingestion failed: {str(e)}")
        return None
