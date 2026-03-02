import json
from datetime import datetime, timezone
from pathlib import Path
import threading
from common.digitize_utils import JobStatus, DocStatus
from common.misc_utils import get_logger

CACHE_DIR = "/var/cache"

logger = get_logger("digitize_utils")

class StatusManager:
    """Thread-safe handler for updating Job and Document status files"""
    def __init__(self, job_id: str):
        self.job_id = job_id
        self.job_status_file = Path(CACHE_DIR) / "jobs"/f"{job_id}_status.json"
        self._lock = threading.Lock()

    def _get_timestamp(self):
        return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    def update_doc_metadata(self, doc_id: str, details: dict, error: str = None):
        """Updates the detailed <doc_id>_metadata.json file."""
        meta_file = Path(CACHE_DIR) /"docs"/ f"{doc_id}_metadata.json"
        if not meta_file.exists():
            logger.error(f"metadata file {doc_id}_metadata.json missing")
            return

        # Create a local copy to avoid modifying the original dictionary passed in
        # and convert any Enums to their values
        sanitized_details = {}
        for k, v in details.items():
            if k == "timing_in_secs" and isinstance(v, dict):
                sanitized_details[k] = v # Nested dicts handled later
            else:
                sanitized_details[k] = v.value if hasattr(v, "value") else v

        # Update the error message if passed
        if error:
            sanitized_details["error"] = str(error)
            if "status" not in sanitized_details:
                sanitized_details["status"] = DocStatus.FAILED.value
                logger.debug(f"Updateing documenta metadata for {doc_id} with error message: {str(error)}")

        try:
            with open(meta_file, "r+") as f:
                data = json.load(f)

                if "timing_in_secs" in sanitized_details and "timing_in_secs" in data:
                    new_timings = sanitized_details.pop("timing_in_secs")
                    data["timing_in_secs"].update(new_timings)

                data.update(sanitized_details)

                # Add last_updated_at timestamp
                data["last_updated_at"] = datetime.now(timezone.utc).isoformat()

                # Write back atomically
                f.seek(0)
                json.dump(data, f, indent=4)
                f.truncate()

        except Exception as e:
            logger.error(f"❌ Failed to write metadata for {doc_id}: {str(e)}", exc_info=True)
        logger.debug(f"✅ Successfully updated metadata for {doc_id}")

    def update_job_progress(self, doc_id: str, doc_status: DocStatus, job_status: JobStatus, error: str = None):
        """ Updates the document status within the <job_id>_status.json """
        with self._lock:
            if not self.job_status_file.exists():
                logger.error(f"{self.job_status_file} file is missing.")
                return

            try:
                with open(self.job_status_file, "r+") as f:
                    data = json.load(f)

                    # Update job status
                    data["status"] = job_status.value
                    data["last_updated_at"] = self._get_timestamp()

                    # If a job-level error is provided, set it at the top level
                    if error and job_status == JobStatus.FAILED:
                        data["error"] = str(error)
                    for doc in data.get("documents", []):
                        if doc["id"] == doc_id:
                            doc["status"] = doc_status.value # Use Enum value
                            break
                    f.seek(0)
                    json.dump(data, f, indent=4)
                    f.truncate()
            except Exception as e:
                logger.error(f"Failed to update status file for job: {e}", exc_info=True)
