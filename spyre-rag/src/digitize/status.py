import json
import os
from pathlib import Path


CACHE_DIR = "/var/cache"

class StatusManager:
    """Handler to atomic updates to the status JSON acting as source of truth"""
    def __init__(self, job_id: str):
        self.path = Path(CACHE_DIR) / "jobs"/f"{job_id}_status.json"
        self.job_id = job_id

    def update(self, data_details: dict):
        """Reads, merges, and atomically writes status."""
        data = {}
        if self.path.exists():
            with open(self.path, "r") as f:
                data = json.load(f)
        
        # Deep merge/update logic
        for key, value in data_details.items():
            if isinstance(value, dict) and key in data:
                data[key].update(value)
            else:
                data[key] = value

        temp_path = self.path.with_suffix(".tmp")
        with open(temp_path, "w") as f:
            json.dump(data, f, indent=4)
        temp_path.replace(self.path)


def update_status(job_id: str, status: str, details: dict = None):
    """
    Atomically writes the current pipeline status to the JSON file.
    """
    status_path = Path(CACHE_DIR) / "jobs"/f"{job_id}_status.json"

    # Load existing data to preserve initial documents submission info
    if status_path.exists():
        with open(status_path, "r") as f:
            data = json.load(f)
    else:
        data = {"job_id": job_id, "history": []}

    data["current_status"] = status
    if details:
        data.update(details)
    
    # Atomic write using a temporary file to prevent corruption during crashes
    temp_path = status_path.with_suffix(".tmp")
    with open(temp_path, "w") as f:
        json.dump(data, f, indent=4)
    temp_path.replace(status_path)