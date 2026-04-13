import json
import os
import tempfile
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
import threading
import time
from typing import Callable, Any, Mapping
from digitize.types import JobStatus, DocStatus, OutputFormat
from digitize.document import DocumentMetadata
from digitize.job import JobState, JobDocumentSummary, JobStats
from digitize.settings import settings
from common.misc_utils import get_logger

logger = get_logger("status")


def get_utc_timestamp() -> str:
    """
    Generate UTC timestamp in ISO format with 'Z' suffix.
    
    Returns:
        ISO 8601 formatted timestamp string with 'Z' suffix
    """
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def create_initial_document_metadata_dict() -> dict:
    """
    Create initial metadata structure for a new document.
    
    Returns:
        Dictionary with default metadata fields
    """
    return {
        "pages": 0,
        "tables": 0,
        "timing_in_secs": {
            "digitizing": None,
            "processing": None,
            "chunking": None,
            "indexing": None
        }
    }


def create_document_metadata(
    doc_name: str,
    doc_id: str,
    job_id: str,
    output_format: OutputFormat,
    operation: str,
    submitted_at: str,
    docs_dir: Path = settings.digitize.docs_dir
) -> DocumentMetadata:
    """
    Create and persist a single document metadata file.
    
    Args:
        doc_name: Name of the document file
        doc_id: Unique identifier for the document
        job_id: ID of the parent job
        operation: Type of operation (e.g., 'ingestion', 'digitization')
        submitted_at: ISO timestamp when the document was submitted
        docs_dir: Directory where metadata files are stored
        
    Returns:
        The created DocumentMetadata object
    """
    doc_metadata = DocumentMetadata(
        id=doc_id,
        name=doc_name,
        type=operation,
        status=DocStatus.ACCEPTED,
        output_format=output_format,
        submitted_at=submitted_at,
        completed_at=None,
        error=None,
        job_id=job_id,
        metadata=create_initial_document_metadata_dict()
    )
    doc_meta_path = doc_metadata.save(docs_dir)
    logger.debug(f"Created document metadata file: {doc_meta_path}")
    return doc_metadata


def create_job_state(
    job_id: str,
    operation: str,
    submitted_at: str,
    doc_id_dict: dict[str, str],
    documents_info: list[str],
    jobs_dir: Path = settings.digitize.jobs_dir,
    job_name: str | None = None
) -> JobState:
    """
    Create and persist the job state file.
    
    Args:
        job_id: Unique identifier for the job
        operation: Type of operation (e.g., 'ingestion', 'digitization')
        submitted_at: ISO timestamp when the job was submitted
        doc_id_dict: Mapping of document names to their IDs
        documents_info: List of document filenames
        jobs_dir: Directory where job status files are stored
        job_name: Optional human-readable name for the job
        
    Returns:
        The created JobState object
    """
    job_doc_summaries = [
        JobDocumentSummary(id=doc_id_dict[doc], name=doc, status=DocStatus.ACCEPTED.value)
        for doc in documents_info
    ]
    
    job_state = JobState(
        job_id=job_id,
        job_name=job_name,
        operation=operation,
        status=JobStatus.ACCEPTED,
        submitted_at=submitted_at,
        completed_at=None,
        documents=job_doc_summaries,
        stats=JobStats(
            total_documents=len(documents_info),
            completed=0,
            failed=0,
            in_progress=0
        ),
        error=None
    )
    job_status_path = job_state.save(jobs_dir)
    logger.debug(f"Created job status file: {job_status_path}")
    return job_state


def get_job_document_stats(job_id: str, jobs_dir: Path = settings.digitize.jobs_dir) -> dict:
    """
    Get statistics about documents in a job by reading the job status file.

    Args:
        job_id: Unique identifier for the job
        jobs_dir: Directory where job status files are stored

    Returns:
        Dictionary containing:
        - failed_docs: List of failed document objects with id, name, status
        - completed_docs: List of completed document objects with id, name, status
        - total_docs: Total number of documents
        - failed_count: Number of failed documents
        - completed_count: Number of completed documents
    """
    job_status_file = jobs_dir / f"{job_id}_status.json"

    if not job_status_file.exists():
        error_msg = f"Job status file not found: {job_status_file}"
        logger.error(error_msg)
        raise FileNotFoundError(error_msg)

    try:
        with open(job_status_file, "r") as f:
            job_data = json.load(f)

        documents = job_data.get("documents", [])
        failed_docs = [doc for doc in documents if doc.get("status") == DocStatus.FAILED.value]
        completed_docs = [doc for doc in documents if doc.get("status") == DocStatus.COMPLETED.value]

        return {
            "failed_docs": failed_docs,
            "completed_docs": completed_docs,
            "total_docs": len(documents),
            "failed_count": len(failed_docs),
            "completed_count": len(completed_docs)
        }
    except Exception as e:
        logger.error(f"Error reading job status file {job_status_file}: {e}")
        raise e

def retry_on_failure(
    func: Callable,
    max_retries: int = settings.digitize.retry_max_attempts,
    delay: float = settings.digitize.retry_initial_delay,
    backoff: float = settings.digitize.retry_backoff_multiplier
) -> Any:
    """
    Retry a function on transient failures with exponential backoff.

    Args:
        func: Function to retry
        max_retries: Maximum number of retry attempts
        delay: Initial delay between retries in seconds
        backoff: Multiplier for delay after each retry

    Returns:
        Result of the function call

    Raises:
        Last exception if all retries fail
    """

    last_exception: Exception = Exception("No attempts made")
    current_delay = delay

    for attempt in range(max_retries):
        try:
            return func()
        except (IOError, OSError) as e:
            last_exception = e
            if attempt < max_retries - 1:
                logger.warning(f"Transient failure (attempt {attempt + 1}/{max_retries}): {e}. Retrying in {current_delay}s...")
                time.sleep(current_delay)
                current_delay *= backoff
            else:
                logger.error(f"All {max_retries} retry attempts failed")
        except Exception as e:
            # Non-transient errors should not be retried
            logger.error(f"Non-transient error encountered: {e}")
            raise

    raise last_exception

class StatusManager:
    """Thread-safe handler for updating Job and Document status files with synchronous writes"""
    def __init__(self, job_id: str):
        self.job_id = job_id
        self.job_status_file = settings.digitize.jobs_dir / f"{job_id}_status.json"
        self._job_lock = threading.Lock()
        self._doc_locks: dict[str, threading.Lock] = {}
        self._doc_locks_lock = threading.Lock()

    def _get_doc_lock(self, doc_id: str) -> threading.Lock:
        """Get or create a lock for a specific document."""
        with self._doc_locks_lock:
            if doc_id not in self._doc_locks:
                self._doc_locks[doc_id] = threading.Lock()
            return self._doc_locks[doc_id]

    def _validate_file_exists(self, file_path: Path, file_description: str) -> bool:
        """Validate file exists and is readable. Returns True if valid."""
        if not file_path.exists():
            logger.error(f"{file_description} file is missing.")
            return False
        if not file_path.is_file():
            logger.error(f"{file_path} is not a regular file")
            return False
        return True

    def _atomic_write_json(self, file_path: Path, data: dict) -> None:
        """Atomically write JSON data to file using temp file + rename pattern."""
        temp_fd, temp_path = tempfile.mkstemp(dir=file_path.parent, suffix=".tmp")
        try:
            with os.fdopen(temp_fd, "w") as f:
                json.dump(data, f, indent=4)
            os.replace(temp_path, file_path)
        except Exception:
            if os.path.exists(temp_path):
                os.unlink(temp_path)
            raise

    def _update_job_level_fields(self, data: dict, job_status: JobStatus, error: str) -> None:
        """Update job-level status, timestamp, and error fields."""
        data["status"] = job_status.value

        # Only set completed_at if job is truly finished (all documents processed)
        if job_status in [JobStatus.COMPLETED, JobStatus.FAILED]:
            stats = data.get("stats", {})
            total_docs = stats.get("total_documents", 0)
            completed_docs = stats.get("completed", 0)
            failed_docs = stats.get("failed", 0)

            # Job is truly complete when all documents are either completed or failed
            if total_docs > 0 and (completed_docs + failed_docs) == total_docs:
                data["completed_at"] = get_utc_timestamp()

        if error and job_status == JobStatus.FAILED:
            data["error"] = str(error)

    def _update_document_status(self, documents: list, doc_id: str, doc_status: DocStatus) -> bool:
        """Update document status in documents list. Returns True if found."""
        for doc in documents:
            if doc.get("id") == doc_id:
                doc["status"] = doc_status.value
                logger.debug(f"Updated document {doc_id} status to {doc_status.value}")
                return True
        logger.warning(f"Document {doc_id} not found in documents list")
        return False

    def _recalculate_stats(self, data: dict) -> None:
        """Recalculate job stats based on document statuses."""
        if "documents" not in data or "stats" not in data:
            return

        status_counts = Counter(doc.get("status") for doc in data["documents"])
        data["stats"]["completed"] = status_counts[DocStatus.COMPLETED.value]
        data["stats"]["failed"] = status_counts[DocStatus.FAILED.value]
        data["stats"]["in_progress"] = (
            status_counts[DocStatus.IN_PROGRESS.value] +
            status_counts[DocStatus.DIGITIZED.value] +
            status_counts[DocStatus.PROCESSED.value] +
            status_counts[DocStatus.CHUNKED.value]
        )

    @staticmethod
    def _extract_value(v: Any) -> Any:
        """Extract .value from enums, return raw value otherwise."""
        return v.value if hasattr(v, "value") else v

    @staticmethod
    def _categorize_fields(details: Mapping[str, Any]) -> tuple[dict[str, Any], dict[str, Any]]:
        """Separate fields into metadata wrapper and top-level categories."""
        METADATA_KEYS = {"pages", "tables", "chunks", "timing_in_secs"}

        metadata_fields = {
            k: v if k == "timing_in_secs" and isinstance(v, dict) else StatusManager._extract_value(v)
            for k, v in details.items() if k in METADATA_KEYS
        }

        top_level_fields = {
            k: StatusManager._extract_value(v)
            for k, v in details.items() if k not in METADATA_KEYS
        }

        return metadata_fields, top_level_fields

    @staticmethod
    def _apply_metadata_updates(
        data: dict[str, Any],
        metadata_fields: dict[str, Any],
        top_level_fields: dict[str, Any]
    ) -> None:
        """
        Apply field updates to data dictionary in-place.
        Preserves existing metadata fields when updating.
        """
        data.update(top_level_fields)

        # Always ensure metadata wrapper exists
        data.setdefault("metadata", {})
        
        # Apply metadata field updates while preserving existing values
        if metadata_fields:
            for mk, mv in metadata_fields.items():
                if mk == "timing_in_secs":
                    # Merge timing updates with existing timing data
                    data["metadata"].setdefault("timing_in_secs", {}).update(mv)
                else:
                    # Only update if value is provided (not None)
                    if mv is not None:
                        data["metadata"][mk] = mv

    def update_doc_metadata(self, doc_id: str, details: Mapping[str, Any], error: str = "") -> None:
        """
        Updates the detailed <doc_id>_metadata.json file synchronously with atomic writes.
        The new structure uses a 'metadata' wrapper for pages, tables, chunks, and timing_in_secs.
        """
        doc_lock = self._get_doc_lock(doc_id)
        with doc_lock:
            meta_file = settings.digitize.docs_dir / f"{doc_id}_metadata.json"

            if not self._validate_file_exists(meta_file, f"metadata file {doc_id}_metadata.json"):
                return

            # Categorize fields into metadata and top-level
            metadata_fields, top_level_fields = self._categorize_fields(details)

            # Update the error message if passed
            if error:
                top_level_fields["error"] = str(error)
                if "status" not in top_level_fields:
                    top_level_fields["status"] = DocStatus.FAILED.value

            def update_metadata_file():
                # Read existing data
                with open(meta_file, "r") as f:
                    data = json.load(f)

                # Apply updates
                self._apply_metadata_updates(data, metadata_fields, top_level_fields)

                # Atomic write using shared helper
                self._atomic_write_json(meta_file, data)

            try:
                # Retry on transient I/O failures
                retry_on_failure(update_metadata_file, max_retries=3, delay=0.5)
                logger.debug(f"✅ Successfully updated metadata for {doc_id}")
            except (IOError, OSError) as e:
                logger.error(f"❌ Failed to read/write metadata file for {doc_id}: {str(e)}", exc_info=True)
            except json.JSONDecodeError as e:
                logger.error(f"❌ Failed to parse JSON metadata for {doc_id}: {str(e)}", exc_info=True)
            except Exception as e:
                logger.error(f"❌ Unexpected error updating metadata for {doc_id}: {str(e)}", exc_info=True)

    def update_job_progress(self, doc_id: str, doc_status: DocStatus, job_status: JobStatus, error: str = ""):
        """
        Updates the document status within the <job_id>_status.json synchronously.
        """
        with self._job_lock:
            if not self._validate_file_exists(self.job_status_file, "job status"):
                return

            def update_status_file():
                # Read existing data
                with open(self.job_status_file, "r") as f:
                    data = json.load(f)

                # Update document status first
                if doc_id and "documents" in data:
                    self._update_document_status(data["documents"], doc_id, doc_status)

                # Recalculate stats based on updated document statuses
                self._recalculate_stats(data)

                # Update job-level fields (status, completed_at, error) after stats are current
                self._update_job_level_fields(data, job_status, error)

                # Atomic write using shared helper
                self._atomic_write_json(self.job_status_file, data)

            try:
                # Retry on transient I/O failures
                retry_on_failure(update_status_file, max_retries=3, delay=0.5)
            except (IOError, OSError) as e:
                logger.error(f"Failed to read/write job status file: {e}", exc_info=True)
            except json.JSONDecodeError as e:
                logger.error(f"Failed to parse JSON in job status file: {e}", exc_info=True)
            except Exception as e:
                logger.error(f"Unexpected error updating job status: {e}", exc_info=True)
