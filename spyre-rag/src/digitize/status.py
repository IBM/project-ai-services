import json
import os
from datetime import datetime, timezone
from pathlib import Path
import threading
import time
from typing import Callable, Any, Dict, Optional
from digitize.types import JobStatus, DocStatus
from common.misc_utils import get_logger

CACHE_DIR = "/var/cache"

logger = get_logger("digitize_utils")

# Configuration for batched updates
BATCH_FLUSH_INTERVAL = 2.0  # Flush every 2 seconds
BATCH_SIZE_THRESHOLD = 10   # Flush when 10 updates accumulated

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
    """
    Thread-safe handler for updating Job and Document status files.
    Implements batched updates with in-memory state and periodic persistence.
    """
    def __init__(self, job_id: str):
        self.job_id = job_id
        self.job_status_file = Path(CACHE_DIR) / "jobs"/f"{job_id}_status.json"
        self._lock = threading.Lock()

        # In-memory state for batching
        self._doc_updates: Dict[str, Dict] = {}  # doc_id -> pending updates
        self._job_updates: Dict = {}  # pending job updates
        self._last_flush_time = time.time()
        self._update_count = 0

        # Background flush thread
        self._flush_thread: Optional[threading.Thread] = None
        self._stop_flush = threading.Event()
        self._start_flush_thread()

    def _get_timestamp(self):
        return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    def _start_flush_thread(self):
        """Start background thread for periodic flushing."""
        def flush_worker():
            while not self._stop_flush.is_set():
                time.sleep(BATCH_FLUSH_INTERVAL)
                if self._should_flush():
                    self._flush_pending_updates()

        self._flush_thread = threading.Thread(target=flush_worker, daemon=True)
        self._flush_thread.start()
        logger.debug(f"Started background flush thread for job {self.job_id}")

    def _should_flush(self) -> bool:
        """Check if we should flush based on time or batch size."""
        with self._lock:
            time_elapsed = time.time() - self._last_flush_time
            return (self._update_count >= BATCH_SIZE_THRESHOLD or
                    (time_elapsed >= BATCH_FLUSH_INTERVAL and self._update_count > 0))

    def _flush_pending_updates(self):
        """Flush all pending updates to disk."""
        with self._lock:
            if not self._doc_updates and not self._job_updates:
                return

            logger.debug(f"Flushing {len(self._doc_updates)} document updates and job updates for {self.job_id}")

            # Flush document updates
            for doc_id, updates in self._doc_updates.items():
                self._write_doc_metadata(doc_id, updates)

            # Flush job updates
            if self._job_updates:
                self._write_job_status(self._job_updates)

            # Clear pending updates
            self._doc_updates.clear()
            self._job_updates.clear()
            self._update_count = 0
            self._last_flush_time = time.time()

            logger.debug(f"Flush completed for job {self.job_id}")

    def flush(self):
        """Force flush all pending updates immediately."""
        self._flush_pending_updates()

    def shutdown(self):
        """Shutdown the status manager and flush pending updates."""
        logger.debug(f"Shutting down StatusManager for job {self.job_id}")
        self._stop_flush.set()
        if self._flush_thread:
            self._flush_thread.join(timeout=5.0)
        self._flush_pending_updates()
        logger.debug(f"StatusManager shutdown complete for job {self.job_id}")

    def update_doc_metadata(self, doc_id: str, details: dict, error: str = ""):
        """
        Queue document metadata updates for batched writing.
        Updates are accumulated in memory and flushed periodically or when threshold is reached.

        The new structure uses a 'metadata' wrapper for pages, tables, chunks, and timing_in_secs.
        """
        with self._lock:
            # Initialize doc updates if not present
            if doc_id not in self._doc_updates:
                self._doc_updates[doc_id] = {}

            # Sanitize and categorize fields
            metadata_fields = {}

            for k, v in details.items():
                # Fields that go into the metadata wrapper
                if k in ["pages", "tables", "chunks", "timing_in_secs"]:
                    if k == "timing_in_secs" and isinstance(v, dict):
                        if "timing_in_secs" not in metadata_fields:
                            metadata_fields["timing_in_secs"] = {}
                        metadata_fields["timing_in_secs"].update(v)
                    else:
                        metadata_fields[k] = v.value if hasattr(v, "value") else v
                else:
                    # Top-level fields
                    self._doc_updates[doc_id][k] = v.value if hasattr(v, "value") else v

            # Store metadata fields separately for proper nesting
            if metadata_fields:
                if "metadata" not in self._doc_updates[doc_id]:
                    self._doc_updates[doc_id]["metadata"] = {}
                self._doc_updates[doc_id]["metadata"].update(metadata_fields)

            # Update the error message if passed
            if error:
                self._doc_updates[doc_id]["error"] = str(error)
                if "status" not in self._doc_updates[doc_id]:
                    self._doc_updates[doc_id]["status"] = DocStatus.FAILED.value

            self._update_count += 1

            # Flush if threshold reached
            if self._update_count >= BATCH_SIZE_THRESHOLD:
                self._flush_pending_updates()

    def _write_doc_metadata(self, doc_id: str, updates: dict):
        """Write document metadata updates to disk (internal method)."""
        meta_file = Path(CACHE_DIR) / "docs" / f"{doc_id}_metadata.json"

        # Validate file existence
        if not meta_file.exists():
            logger.error(f"metadata file {doc_id}_metadata.json missing")
            return

        # Validate file is readable
        if not meta_file.is_file():
            logger.error(f"{meta_file} is not a regular file")
            return

        def update_metadata_file():
            # Read existing data
            with open(meta_file, "r") as f:
                data = json.load(f)

            # Update top-level fields
            for k, v in updates.items():
                if k == "metadata":
                    # Handle metadata wrapper specially
                    if "metadata" not in data:
                        data["metadata"] = {}

                    # Merge metadata fields
                    for mk, mv in v.items():
                        if mk == "timing_in_secs":
                            if "timing_in_secs" not in data["metadata"]:
                                data["metadata"]["timing_in_secs"] = {}
                            data["metadata"]["timing_in_secs"].update(mv)
                        else:
                            data["metadata"][mk] = mv
                else:
                    data[k] = v

            # Write atomically: write to temp file, then rename
            tmp_file = meta_file.with_suffix('.tmp')
            with open(tmp_file, "w") as f:
                json.dump(data, f, indent=4)
                f.flush()
                os.fsync(f.fileno())

            # Atomic rename
            os.replace(tmp_file, meta_file)

        try:
            # Retry on transient I/O failures
            retry_on_failure(update_metadata_file, max_retries=3, delay=0.5)
            logger.debug(f"✅ Successfully wrote metadata for {doc_id}")
        except (IOError, OSError) as e:
            logger.error(f"❌ Failed to read/write metadata file for {doc_id}: {str(e)}", exc_info=True)
        except json.JSONDecodeError as e:
            logger.error(f"❌ Failed to parse JSON metadata for {doc_id}: {str(e)}", exc_info=True)
        except Exception as e:
            logger.error(f"❌ Unexpected error updating metadata for {doc_id}: {str(e)}", exc_info=True)

    def update_job_progress(self, doc_id: str, doc_status: DocStatus, job_status: JobStatus, error: str = ""):
        """
        Queue job progress updates for batched writing.
        Updates are accumulated in memory and flushed periodically or when threshold is reached.
        """
        with self._lock:
            # Store job-level updates
            self._job_updates["status"] = job_status.value

            # Set completed_at if job is completed or failed
            if job_status in [JobStatus.COMPLETED, JobStatus.FAILED]:
                if "completed_at" not in self._job_updates:
                    self._job_updates["completed_at"] = self._get_timestamp()

            # If a job-level error is provided, set it at the top level
            if error and job_status == JobStatus.FAILED:
                self._job_updates["error"] = str(error)

            # Store document status update
            if doc_id:
                if "doc_statuses" not in self._job_updates:
                    self._job_updates["doc_statuses"] = {}
                self._job_updates["doc_statuses"][doc_id] = doc_status.value

            self._update_count += 1

            # Flush if threshold reached
            if self._update_count >= BATCH_SIZE_THRESHOLD:
                self._flush_pending_updates()

    def _write_job_status(self, updates: dict):
        """Write job status updates to disk (internal method)."""
        # Validate file existence
        if not self.job_status_file.exists():
            logger.error(f"{self.job_status_file} file is missing.")
            return

        # Validate file is readable
        if not self.job_status_file.is_file():
            logger.error(f"{self.job_status_file} is not a regular file")
            return

        def update_status_file():
            # Read existing data
            with open(self.job_status_file, "r") as f:
                data = json.load(f)

            # Update job-level fields
            if "status" in updates:
                data["status"] = updates["status"]

            if "completed_at" in updates:
                data["completed_at"] = updates["completed_at"]

            if "error" in updates:
                data["error"] = updates["error"]

            # Update document statuses
            if "doc_statuses" in updates:
                for doc_id, doc_status in updates["doc_statuses"].items():
                    for doc in data.get("documents", []):
                        if doc["id"] == doc_id:
                            doc["status"] = doc_status
                            break

            # Recalculate stats
            stats = {
                "total_documents": len(data.get("documents", [])),
                "completed": 0,
                "failed": 0,
                "in_progress": 0
            }

            for doc in data.get("documents", []):
                status = doc.get("status", "")
                if status == "completed":
                    stats["completed"] += 1
                elif status == "failed":
                    stats["failed"] += 1
                elif status == "in_progress":
                    stats["in_progress"] += 1

            data["stats"] = stats

            # Write atomically: write to temp file, then rename
            tmp_file = self.job_status_file.with_suffix('.tmp')
            with open(tmp_file, "w") as f:
                json.dump(data, f, indent=4)
                f.flush()
                os.fsync(f.fileno())

            # Atomic rename
            os.replace(tmp_file, self.job_status_file)

        try:
            # Retry on transient I/O failures
            retry_on_failure(update_status_file, max_retries=3, delay=0.5)
            logger.debug(f"✅ Successfully wrote job status for {self.job_id}")
        except (IOError, OSError) as e:
            logger.error(f"Failed to read/write job status file: {e}", exc_info=True)
        except json.JSONDecodeError as e:
            logger.error(f"Failed to parse JSON in job status file: {e}", exc_info=True)
        except Exception as e:
            logger.error(f"Unexpected error updating job status: {e}", exc_info=True)
