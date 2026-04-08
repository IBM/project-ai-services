from pathlib import Path
import time
import json
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

    def finalize_orphaned_documents(status_mgr, job_id, doc_id_dict):
        """
        Job Reaper/Finalizer: Ensures all documents reach a terminal state (COMPLETED or FAILED).
        This prevents job deadlock by marking any stuck documents as FAILED.
        Called in the finally block to guarantee execution even on catastrophic failures.
        """
        if not status_mgr or not job_id or not doc_id_dict:
            return

        try:
            doc_stats = get_job_document_stats(job_id)
            total_docs = doc_stats["total_docs"]
            completed_count = doc_stats["completed_count"]
            failed_count = doc_stats["failed_count"]

            # Check if there are orphaned documents (not in terminal state)
            terminal_count = completed_count + failed_count
            if terminal_count < total_docs:
                orphaned_count = total_docs - terminal_count
                logger.warning(f"Job Reaper: Found {orphaned_count} orphaned document(s) in job {job_id}")

                # Mark all orphaned documents as FAILED
                for filename, doc_id in doc_id_dict.items():
                    # Check if document is in terminal state
                    is_completed = any(doc["id"] == doc_id for doc in doc_stats["completed_docs"])
                    is_failed = any(doc["id"] == doc_id for doc in doc_stats["failed_docs"])

                    if not is_completed and not is_failed:
                        error_msg = "Document processing incomplete - marked as failed during job finalization"
                        logger.warning(f"Job Reaper: Finalizing orphaned document {doc_id} as FAILED")
                        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=error_msg)
                        status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)

                logger.info(f"Job Reaper: Marked {orphaned_count} orphaned document(s) as FAILED")
        except Exception as e:
            logger.error(f"Job Reaper: Error during job finalization for {job_id}: {e}", exc_info=True)

    logger.info(f"Ingestion started from dir '{directory_path}'")
    
    # Initialize LLM session for all API calls (LLM and embedding)
    create_llm_session(pool_maxsize=config.LLM_POOL_SIZE)

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
        doc_chunks_dict, converted_pdf_stats = process_documents(
            input_file_paths, out_path, llm_model_dict['llm_model'], llm_model_dict['llm_endpoint'],  emb_model_dict["emb_endpoint"],
            max_tokens=emb_model_dict['max_tokens'] - 100, job_id=job_id, doc_id_dict=doc_id_dict)
        # converted_pdf_stats holds { file_name: {page_count: int, table_count: int, timings: {conversion: time_in_secs, process_text: time_in_secs, process_tables: time_in_secs, chunking: time_in_secs}} }
        if converted_pdf_stats is None or doc_chunks_dict is None:
            ingestion_failed()
            return

        if doc_chunks_dict:
            # Always index documents - treating each request as fresh
            logger.info("Loading processed documents into vector DB")

            embedder = get_embedder(emb_model_dict['emb_model'], emb_model_dict['emb_endpoint'], emb_model_dict['max_tokens'])

            # Track failed document count for summary logging
            failed_count = 0
            total_count = len(doc_chunks_dict)

            # Index each document separately and update status
            for doc_id, chunks in doc_chunks_dict.items():
                logger.debug(f"Indexing {len(chunks)} chunks for document: {doc_id}")

                try:
                    success = vector_store.insert_chunks(chunks, embedding=embedder)
                except Exception as e:
                    # Catch exceptions from insert_chunks (e.g., after all retries failed)
                    # Mark this document as failed and continue with remaining documents
                    success = False
                    failed_count += 1
                    logger.error(f"Failed to index document {doc_id}: {str(e)}")

                    # Reinitialize vector store and embedder after a failure to ensure clean state for next document
                    # This prevents cascading failures due to corrupted connection state
                    try:
                        logger.debug("Reinitializing vector store and embedder after failure")
                        vector_store = db.get_vector_store()
                        embedder = get_embedder(emb_model_dict['emb_model'], emb_model_dict['emb_endpoint'], emb_model_dict['max_tokens'])
                        logger.debug("Successfully reinitialized connections")
                    except Exception as reinit_error:
                        logger.error(f"Failed to reinitialize connections: {reinit_error}")
                        # Continue anyway - the next document will try with existing connections

                # Update document status immediately after indexing attempt, regardless of success or failure
                if status_mgr and doc_id_dict:
                    if not success:
                        # Mark as FAILED if indexing failed
                        failed_count += 1
                        logger.error(f"Failed to index document: {doc_id}")
                        logger.error(f"Indexing failed: updating doc metadata to FAILED for document: {doc_id}")
                        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error="Failed to index document chunks into vector database")
                        status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)
                    else:
                        # Mark as COMPLETED if indexing succeeded
                        logger.debug(f"Indexing Done: updating doc metadata to COMPLETED for document: {doc_id}")
                        status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.COMPLETED, "completed_at": get_utc_timestamp()})
                        status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED, JobStatus.IN_PROGRESS)

            if failed_count > 0:
                logger.error(f"Indexing failed for {failed_count}/{total_count} document(s)")
            else:
                logger.info(f"All {total_count} processed document(s) loaded into Vector DB successfully")

        # Log time taken for the file
        end_time: float = time.time()  # End the timer for the current file
        file_processing_time = end_time - start_time

        # Determine final job status by reading actual document statuses from job status file
        if status_mgr and job_id:
            doc_stats = get_job_document_stats(job_id)
            failed_docs = doc_stats["failed_docs"]
            completed_docs = doc_stats["completed_docs"]

            logger.info(
                    f"Ingestion summary: {len(completed_docs)}/{total_pdfs} files ingested "
                    f"({len(completed_docs) / total_pdfs * 100:.2f}% of total PDF files)"
                )

            if len(failed_docs) > 0:
                # At least one document failed
                failed_doc_names = [doc["name"] for doc in failed_docs]
                failed_files_list = "\n".join(failed_doc_names)

                # Detailed error message for logs
                detailed_error_message = (
                    f"{len(failed_docs)} document(s) failed to process.\n"
                    f"Failed documents:\n{failed_files_list}\n"
                    f"Please submit a new ingestion job to process these documents. "
                    f"If the issue persists, please report at https://github.com/IBM/project-ai-services/issues"
                )
                logger.warning(detailed_error_message)

                # User-friendly error message for job status
                job_error_message = (
                    f"{len(failed_docs)} of {total_pdfs} document(s) failed to ingest. "
                    f"Check the document status for details on the failures."
                )

                status_mgr.update_job_progress("", DocStatus.FAILED, JobStatus.FAILED, error=job_error_message)
            else:
                # All documents completed successfully
                logger.info(f"✅ Ingestion completed successfully, Time taken: {file_processing_time:.2f} seconds. You can query your documents via chatbot")
                logger.info(
                    f"Ingestion summary: {len(completed_docs)}/{total_pdfs} files ingested "
                    f"(100.00% of total PDF files)"
                )

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

                # Update job status to FAILED after marking unprocessed documents
                logger.error(f"Catastrophic ingestion error, updating job {job_id} status to FAILED")
                status_mgr.update_job_progress("", DocStatus.FAILED, JobStatus.FAILED, error=f"Ingestion failed: {str(e)}")
            except FileNotFoundError as fnf_error:
                logger.error(f"Job status file not found during error handling: {fnf_error}")

                # Re-raise the exception to propagate to app server
                raise fnf_error

        return None

    finally:
        # Job Reaper: ALWAYS run finalization to ensure no orphaned documents
        # This critical section prevents job deadlock
        if status_mgr and job_id and doc_id_dict:
            logger.debug(f"Running job finalizer for job {job_id}")
            finalize_orphaned_documents(status_mgr, job_id, doc_id_dict)

            # After finalization, ensure job reaches terminal state
            try:
                doc_stats = get_job_document_stats(job_id)
                total_docs = doc_stats["total_docs"]
                completed_count = doc_stats["completed_count"]
                failed_count = doc_stats["failed_count"]
                terminal_count = completed_count + failed_count

                # If all documents are in terminal state, finalize the job
                if terminal_count == total_docs:
                    # Read current job status to avoid overwriting
                    job_file = config.JOBS_DIR / f"{job_id}_status.json"
                    if job_file.exists():
                        with open(job_file, "r") as f:
                            job_data = json.load(f)
                        current_status = job_data.get("status")

                        # Only update if job is still IN_PROGRESS
                        if current_status == JobStatus.IN_PROGRESS.value:
                            if failed_count > 0:
                                error_msg = f"{failed_count} of {total_docs} document(s) failed"
                                status_mgr.update_job_progress("", DocStatus.FAILED, JobStatus.FAILED, error=error_msg)
                                logger.info(f"Job {job_id} finalized as FAILED ({failed_count} failures)")
                            else:
                                status_mgr.update_job_progress("", DocStatus.COMPLETED, JobStatus.COMPLETED)
                                logger.info(f"Job {job_id} finalized as COMPLETED")
                else:
                    logger.warning(f"Job {job_id} has {total_docs - terminal_count} documents not in terminal state after finalization")
            except Exception as final_error:
                logger.error(f"Error during final job status update for {job_id}: {final_error}", exc_info=True)
