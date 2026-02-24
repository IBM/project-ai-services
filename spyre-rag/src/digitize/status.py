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
        self.job_status = Path(CACHE_DIR) / "jobs"/f"{job_id}_status.json"
        self._lock = threading.Lock()

    def _get_timestamp(self):
        return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    def update_doc_metadata(self, doc_id: str, details: dict):
        """Updates the detailed <doc_id>_metadata.json file."""
        meta_file = CACHE_DIR / f"{doc_id}_metadata.json"
        if not meta_file.exists():
            return

        with open(meta_file, "r+") as f:
            data = json.load(f)
            # Handle nested timing updates
            if "timing_in_secs" in details and "timing_in_secs" in data:
                data["timing_in_secs"].update(details.pop("timing_in_secs"))
            data.update(details)
            
            f.seek(0)
            json.dump(data, f, indent=4)
            f.truncate()

    def update_job_progress(self, doc_id: str, status: DocStatus):
       """ Updates the document status within the <job_id>_status.json """
       with self._lock:
            if not self.job_status.exists():
                logger.error(f"{self.job_status} file is missing.")
                return
            with open(self.job_status, "r+") as f:
                data = json.load(f)
                for doc in data.get("documents", []):
                    if doc["id"] == doc_id:
                        doc["status"] = status.value # Use Enum value
                        break
                f.seek(0)
                json.dump(data, f, indent=4)
                f.truncate()
