import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import List, Optional

from digitize.types import JobStatus
from digitize import config


@dataclass
class JobDocumentSummary:
    """Compact per-document entry stored inside a job status file."""
    id: str
    name: str
    status: str

    def to_dict(self) -> dict:
        return {
            "id": self.id,
            "name": self.name,
            "status": self.status,
        }


@dataclass
class JobStats:
    """Statistics for documents in a job."""
    total_documents: int = 0
    completed: int = 0
    failed: int = 0
    in_progress: int = 0

    def to_dict(self) -> dict:
        return {
            "total_documents": self.total_documents,
            "completed": self.completed,
            "failed": self.failed,
            "in_progress": self.in_progress,
        }


@dataclass
class JobState:
    """
    Represents the overall state of a job. Job tracks overall progress and statistics
    Persisted as <job_id>_status.json under JOBS_DIR.
    """
    job_id: str
    operation: str
    status: JobStatus
    submitted_at: str
    completed_at: Optional[str] = None
    documents: List[JobDocumentSummary] = field(default_factory=list)
    stats: JobStats = field(default_factory=JobStats)
    error: Optional[str] = None

    def to_dict(self) -> dict:
        """Serialize the job state to a JSON-compatible dictionary."""
        return {
            "job_id": self.job_id,
            "operation": self.operation,
            "status": self.status.value if hasattr(self.status, "value") else self.status,
            "submitted_at": self.submitted_at,
            "completed_at": self.completed_at,
            "documents": [doc.to_dict() for doc in self.documents],
            "stats": self.stats.to_dict(),
            "error": self.error,
        }

    def save(self, jobs_dir: Path = config.JOBS_DIR) -> Path:
        """
        Persist the job state as <job_id>_status.json.

        Args:
            jobs_dir: Directory where the status file will be written.

        Returns:
            Path to the written status file.
        """
        jobs_dir.mkdir(parents=True, exist_ok=True)
        status_path = jobs_dir / f"{self.job_id}_status.json"
        with open(status_path, "w") as f:
            json.dump(self.to_dict(), f, indent=4)
        return status_path
