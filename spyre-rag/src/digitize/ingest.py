from pathlib import Path
import time
from typing import Optional, Callable

import common.db_utils as db
from common.emb_utils import get_embedder
from common.misc_utils import *
from digitize.doc_utils import process_documents
from digitize.status import StatusManager, get_utc_timestamp, get_job_document_stats
from digitize.types import JobStatus, DocStatus
from digitize.settings import settings

logger = get_logger("ingest")

def create_indexing_handler(emb_model_dict: dict, status_mgr: Optional[StatusManager], doc_id_dict: Optional[dict]):
    """
    Create an indexing handler that can be called immediately after chunking of a document.

    Args:
        emb_model_dict: Dictionary containing embedding model configuration
        status_mgr: Status manager for updating document status
        doc_id_dict: Mapping of document names to IDs

    Returns:
        Callable that handles indexing of a single document's chunks
    """
    # Initialize resources once
    vector_store = db.get_vector_store()
    embedder = get_embedder(
        emb_model_dict['emb_model'],
        emb_model_dict['emb_endpoint'],
        emb_model_dict['max_tokens']
    )

    def index_document_chunks(doc_id: str, chunks: list, path: str) -> bool:
        """
        Index a single document's chunks immediately after chunking completes.

        Args:
            doc_id: Document ID
            chunks: List of chunk dictionaries
            path: Original file path

        Returns:
            bool: True if indexing succeeded, False otherwise
        """
        nonlocal vector_store, embedder

        try:
            logger.debug(f"Immediately indexing {len(chunks)} chunks for document: {doc_id}")

            # Capture indexing timing
            indexing_start_time = time.time()

            # Index the chunks
            success = vector_store.insert_chunks(chunks, embedding=embedder)
            indexing_time = time.time() - indexing_start_time

            if not success:
                logger.error(f"Failed to index document {doc_id}")
                if status_mgr and doc_id_dict:
                    status_mgr.update_doc_metadata(
                        doc_id,
                        {
                            "status": DocStatus.FAILED,
                            "timing_in_secs": {"indexing": round(indexing_time, 2)}
                        },
                        error="Failed to index document chunks into vector database"
                    )
                    status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)
                return False

            # Update status to COMPLETED with indexing timing
            if status_mgr and doc_id_dict:
                logger.debug(f"Indexing Done: updating doc metadata to COMPLETED for document: {doc_id}")
                status_mgr.update_doc_metadata(
                    doc_id,
                    {
                        "status": DocStatus.COMPLETED,
                        "completed_at": get_utc_timestamp(),
                        "timing_in_secs": {"indexing": round(indexing_time, 2)}
                    }
                )
                status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED, JobStatus.IN_PROGRESS)

            logger.info(f"✅ Successfully indexed document {doc_id}")
            return True

        except Exception as e:
            logger.error(f"Exception during indexing for {doc_id}: {e}", exc_info=True)

            # Try to reinitialize connections for next document
            try:
                logger.debug("Reinitializing vector store and embedder after failure")
                vector_store = db.get_vector_store()
                embedder = get_embedder(
                    emb_model_dict['emb_model'],
                    emb_model_dict['emb_endpoint'],
                    emb_model_dict['max_tokens']
                )
            except Exception as reinit_error:
                logger.error(f"Failed to reinitialize connections: {reinit_error}")

            # Mark document as failed
            if status_mgr and doc_id_dict:
                status_mgr.update_doc_metadata(
                    doc_id,
                    {"status": DocStatus.FAILED},
                    error=f"Indexing exception: {str(e)}"
                )
                status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.IN_PROGRESS)

            return False

    return index_document_chunks

def ingest(directory_path: Path, job_id: Optional[str] = None, doc_id_dict: Optional[dict] = None):

    def ingestion_failed():
        logger.info("❌ Ingestion failed, please re-run the ingestion again, If the issue still persists, please report an issue in https://github.com/IBM/project-ai-services/issues")

    logger.info(f"Ingestion started from dir '{directory_path}'")

    # Initialize LLM session for all API calls (LLM and embedding)
    create_llm_session(pool_maxsize=settings.common.llm.llm_max_batch_size)

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

        out_path = setup_digitized_doc_dir()

        # Create indexing handler for immediate indexing after chunking
        indexing_handler = create_indexing_handler(emb_model_dict, status_mgr, doc_id_dict)

        start_time = time.time()
        # Reserve 100 tokens from embedding model's max_tokens to account for metadata
        # that will be prepended to content during final merge, ensuring total tokens stay within embedding model limits
        _, converted_pdf_stats = process_documents(
            input_file_paths, out_path, llm_model_dict['llm_model'], llm_model_dict['llm_endpoint'],  emb_model_dict["emb_endpoint"],
            max_tokens=emb_model_dict['max_tokens'] - 100, job_id=job_id, doc_id_dict=doc_id_dict,
            indexing_callback=indexing_handler)
        # converted_pdf_stats holds { file_name: {page_count: int, table_count: int, timings: {conversion: time_in_secs, process_text: time_in_secs, process_tables: time_in_secs, chunking: time_in_secs}} }
        if converted_pdf_stats is None:
            ingestion_failed()
            return

        # Note: Documents are now indexed immediately after chunking via the indexing_callback
        logger.info(f"All {len(converted_pdf_stats)} document(s) have been processed and indexed")

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
