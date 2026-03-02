import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import List, Optional

from digitize.types import JobStatus


CACHE_DIR = "/var/cache"
JOBS_DIR = f"{CACHE_DIR}/jobs"


@dataclass
class JobDocumentSummary:
    """Compact per-document entry stored inside a job status file."""
    id: str
    name: str
    status: JobStatus = JobStatus.ACCEPTED

    def to_dict(self) -> dict:
        return {
            "id": self.id,
            "name": self.name,
            "status": self.status.value if hasattr(self.status, "value") else self.status,
        }


@dataclass
class JobState:
    """
    Represents the overall state of a job.
    Persisted as <job_id>_status.json under JOBS_DIR.
    """
    job_id: str
    operation: str
    submitted_at: str
    status: JobStatus = JobStatus.ACCEPTED
    last_updated_at: Optional[str] = None
    documents: List[JobDocumentSummary] = field(default_factory=list)
    error: str = ""

    def __post_init__(self):
        # Default last_updated_at to submitted_at when not explicitly provided
        if self.last_updated_at is None:
            self.last_updated_at = self.submitted_at

    def to_dict(self) -> dict:
        """Serialize the job state to a JSON-compatible dictionary."""
        return {
            "job_id": self.job_id,
            "operation": self.operation,
            "status": self.status.value if hasattr(self.status, "value") else self.status,
            "submitted_at": self.submitted_at,
            "last_updated_at": self.last_updated_at,
            "documents": [doc.to_dict() for doc in self.documents],
            "error": self.error,
        }

    def save(self, jobs_dir: str = JOBS_DIR) -> Path:
        """
        Persist the job state as <job_id>_status.json.

        Args:
            jobs_dir: Directory where the status file will be written.

        Returns:
            Path to the written status file.
        """
        Path(jobs_dir).mkdir(parents=True, exist_ok=True)
        status_path = Path(jobs_dir) / f"{self.job_id}_status.json"
        with open(status_path, "w") as f:
            json.dump(self.to_dict(), f, indent=4)
        return status_path
