from glob import glob
from common.misc_utils import *
from pathlib import Path
from digitize.status import StatusManager,get_utc_timestamp
from digitize.types import JobStatus, DocStatus, OutputFormat
from digitize.pdf_utils import get_pdf_page_count
from digitize.doc_utils import convert_document_format
from concurrent.futures import ProcessPoolExecutor

logger = get_logger("digitize")

def digitize(directory_path: Path, job_id: str, doc_id_dict: dict, output_format: OutputFormat):
    """
    Digitize a single PDF file in the staging directory.

    Args:
        directory_path: Path to staging directory containing exactly one PDF (pre-validated and staged by app.py)
        job_id: Job identifier for StatusManager
        doc_id_dict: Mapping from filename to document ID
        output_format: "json", "md", or "txt"

    Raises:
        Exception: If conversion fails

    Returns:
        None
    """
    # All validations are done at API level in app.py
    # Files are pre-staged and doc_id_dict is pre-created

    # Initialize StatusManager
    status_mgr = StatusManager(job_id) if job_id else None

    # Prepare output/cache path
    out_path = setup_digitized_doc_dir()

    # Get the single PDF file from staging directory
    pdfs = list(directory_path.glob("*.pdf"))
    file_path = pdfs[0]
    filename = file_path.name
    doc_id = doc_id_dict[filename]

    try:
        # Mark document/job as IN_PROGRESS
        if status_mgr:
            logger.debug(f"Submitted for conversion: updating job & doc metadata to IN_PROGRESS for document: {doc_id}")
            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.IN_PROGRESS})
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

            output_file, conversion_time = future.result()

        # Collect metadata
        page_count = get_pdf_page_count(str(file_path))

        # Mark COMPLETED
        if status_mgr:
            logger.debug(f"Conversion Done: updating doc & job metadata for document: {doc_id}")
            status_mgr.update_doc_metadata(doc_id, {
                "status": DocStatus.COMPLETED,
                "pages": page_count,
                "completed_at": get_utc_timestamp(),
                "timing_in_secs": {"digitizing": round(conversion_time, 2)}
            })
            status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED, JobStatus.COMPLETED)

    except Exception as e:
        # Mark FAILED
        logger.error(f"Conversion failed for {filename}: {str(e)}", exc_info=True)
        if status_mgr:
            status_mgr.update_doc_metadata(doc_id, {"status": DocStatus.FAILED}, error=f"Failed to convert document: {str(e)}")
            status_mgr.update_job_progress(doc_id, DocStatus.FAILED, JobStatus.FAILED, error=f"Digitization failed: {str(e)}")
        raise
