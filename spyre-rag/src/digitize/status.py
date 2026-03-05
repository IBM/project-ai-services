import json
from datetime import datetime, timezone
from pathlib import Path
import threading
import time
from typing import Callable, Any
from digitize.types import JobStatus, DocStatus
from common.misc_utils import get_logger

CACHE_DIR = "/var/cache"

logger = get_logger("digitize_utils")

def retry_on_failure(func: Callable, max_retries: int = 3, delay: float = 0.5, backoff: float = 2.0) -> Any:
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
        self.job_status_file = Path(CACHE_DIR) / "jobs" / f"{job_id}_status.json"
        self._lock = threading.Lock()

    def _get_timestamp(self):
        return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    def update_doc_metadata(self, doc_id: str, details: dict, error: str = ""):
        """
        Updates the detailed <doc_id>_metadata.json file synchronously.
        The new structure uses a 'metadata' wrapper for pages, tables, chunks, and timing_in_secs.
        """
        meta_file = Path(CACHE_DIR) / "docs" / f"{doc_id}_metadata.json"

        # Validate file existence
        if not meta_file.exists():
            logger.error(f"metadata file {doc_id}_metadata.json missing")
            return

        # Validate file is readable
        if not meta_file.is_file():
            logger.error(f"{meta_file} is not a regular file")
            return

        # Sanitize and categorize fields
        metadata_fields = {}
        top_level_fields = {}

        for k, v in details.items():
            # Fields that go into the metadata wrapper
            if k in ["pages", "tables", "chunks", "timing_in_secs"]:
                if k == "timing_in_secs" and isinstance(v, dict):
                    metadata_fields[k] = v
                else:
                    metadata_fields[k] = v.value if hasattr(v, "value") else v
            else:
                # Top-level fields
                top_level_fields[k] = v.value if hasattr(v, "value") else v

        # Update the error message if passed
        if error:
            top_level_fields["error"] = str(error)
            if "status" not in top_level_fields:
                top_level_fields["status"] = DocStatus.FAILED.value

        def update_metadata_file():
            with open(meta_file, "r+") as f:
                data = json.load(f)

                # Update top-level fields
                data.update(top_level_fields)

                # Update metadata wrapper
                if metadata_fields:
                    if "metadata" not in data:
                        data["metadata"] = {}

                    for mk, mv in metadata_fields.items():
                        if mk == "timing_in_secs":
                            if "timing_in_secs" not in data["metadata"]:
                                data["metadata"]["timing_in_secs"] = {}
                            data["metadata"]["timing_in_secs"].update(mv)
                        else:
                            data["metadata"][mk] = mv

                # Write back atomically
                f.seek(0)
                json.dump(data, f, indent=4)
                f.truncate()

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
        with self._lock:
            # Validate file existence
            if not self.job_status_file.exists():
                logger.error(f"{self.job_status_file} file is missing.")
                return

            # Validate file is readable
            if not self.job_status_file.is_file():
                logger.error(f"{self.job_status_file} is not a regular file")
                return

            def update_status_file():
                with open(self.job_status_file, "r+") as f:
                    data = json.load(f)

                    # Update job status
                    data["status"] = job_status.value

                    # Set completed_at if job is completed or failed
                    if job_status in [JobStatus.COMPLETED, JobStatus.FAILED]:
                        if "completed_at" not in data:
                            data["completed_at"] = self._get_timestamp()

                    # If a job-level error is provided, set it at the top level
                    if error and job_status == JobStatus.FAILED:
                        data["error"] = str(error)

                    # Update document status in documents list
                    if doc_id and "documents" in data:
                        doc_found = False
                        for doc in data["documents"]:
                            if doc.get("id") == doc_id:
                                doc["status"] = doc_status.value
                                doc_found = True
                                logger.debug(f"Updated document {doc_id} status to {doc_status.value} in documents list")
                                break
                        if not doc_found:
                            logger.warning(f"Document {doc_id} not found in documents list")

                    # Recalculate stats based on documents list
                    if "documents" in data and "stats" in data:
                        statuses = [doc.get("status") for doc in data["documents"]]
                        data["stats"]["completed"] = statuses.count(DocStatus.COMPLETED.value)
                        data["stats"]["failed"] = statuses.count(DocStatus.FAILED.value)
                        data["stats"]["in_progress"] = statuses.count(DocStatus.IN_PROGRESS.value)

                    f.seek(0)
                    json.dump(data, f, indent=4)
                    f.truncate()

            try:
                # Retry on transient I/O failures
                retry_on_failure(update_status_file, max_retries=3, delay=0.5)
            except (IOError, OSError) as e:
                logger.error(f"Failed to read/write job status file: {e}", exc_info=True)
            except json.JSONDecodeError as e:
                logger.error(f"Failed to parse JSON in job status file: {e}", exc_info=True)
            except Exception as e:
                logger.error(f"Unexpected error updating job status: {e}", exc_info=True)
