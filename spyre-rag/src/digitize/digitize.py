import json

import common.db_utils as db
from common.misc_utils import *
from digitize.status import StatusManager,get_utc_timestamp
from digitize.types import JobStatus, DocStatus
from digitize.pdf_utils import get_pdf_page_count
from digitize.doc_utils import convert_document_format
from concurrent.futures import ProcessPoolExecutor

from docling.datamodel.document import DoclingDocument, TextItem

logger = get_logger("digitize")

def digitize(directory_path, job_id=None, doc_id_dict: dict = None, output_format: str = "json"):
    """
    Digitize a single PDF file in the staging directory.
    Converts to JSON and optionally Markdown or text, updates metadata, but does not return anything.
    
    Args:
        directory_path: Path to staging directory containing exactly one PDF
        job_id: Job identifier for StatusManager
        doc_id_dict: Mapping from filename to document ID
        output_format: "json", "md", or "text"
    """
    directory_path = Path(directory_path)
    if not directory_path.exists():
        raise Exception(f"Staging directory does not exist: {directory_path}")

    # Initialize StatusManager
    status_mgr = StatusManager(job_id) if job_id else None

    # Prepare output/cache path
    vector_store = db.get_vector_store()
    index_name = vector_store.index_name
    out_path = setup_cache_dir(index_name)

    pdfs = list(directory_path.glob("*.pdf"))
    file_path = pdfs[0]
    filename = file_path.name
    doc_id = doc_id_dict.get(filename)
    if doc_id is None:
        raise Exception(f"Document ID not found for {filename}")

    try:
        # Mark document/job as IN_PROGRESS
        if status_mgr:
            logger.debug(f"Submitted for conversion: updating job & doc metadata to IN_PROGRESS for document: {doc_id}")
            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.COMPLETED, "completed_at": get_utc_timestamp()})
            status_mgr.update_job_progress(doc_id, DocStatus.IN_PROGRESS, JobStatus.IN_PROGRESS)

        # Convert document
        # Run conversion inside a single process worker
        with ProcessPoolExecutor(max_workers=1) as executor:
            future = executor.submit(
                convert_document_format,
                str(file_path),
                out_path,
                doc_id,
                output_format
            )

            _, output_file, conversion_time = future.result()

        if not output_file:
            raise Exception("Conversion failed")

        # Collect metadata
        page_count = get_pdf_page_count(str(file_path))

        # Mark COMPLETED
        if status_mgr:
            logger.debug(f"Conversion Done: updating doc & job metadata for document: {doc_id}")
            status_mgr.update_doc_metadata(doc_id, {
                "status": DocStatus.COMPLETED,
                "pages": page_count,
                "timing_in_secs": {"digitizing": round(conversion_time, 2)}
            })
            status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED, JobStatus.COMPLETED)

    except Exception as e:
        # Mark FAILED
        logger.error(f"Conversion failed for {filename}: converted_json is None")
        if status_mgr:
            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error="Failed to convert document: conversion returned None")
            status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.FAILED, error="Failed to convert document: conversion returned None")
        raise