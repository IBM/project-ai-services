from pathlib import Path
from typing import List, Optional

from pydantic import BaseModel, Field, field_validator

from digitize.types import JobStatus
from digitize.settings import settings


class JobDocumentSummary(BaseModel):
    """Compact per-document entry stored inside a job status file."""
    id: str
    name: str
    status: str

    class Config:
        """Pydantic configuration."""
        use_enum_values = True


class JobStats(BaseModel):
    """Statistics for documents in a job."""
    total_documents: int = Field(default=0, ge=0, description="Total number of documents")
    completed: int = Field(default=0, ge=0, description="Number of completed documents")
    failed: int = Field(default=0, ge=0, description="Number of failed documents")
    in_progress: int = Field(default=0, ge=0, description="Number of in-progress documents")

    class Config:
        """Pydantic configuration."""
        use_enum_values = True


class JobState(BaseModel):
    """
    Represents the overall state of a job. Job tracks overall progress and statistics.
    Persisted as <job_id>_status.json under JOBS_DIR.
    """
    job_id: str
    job_name: Optional[str] = None
    operation: str
    status: JobStatus
    submitted_at: str
    completed_at: Optional[str] = None
    documents: List[JobDocumentSummary] = Field(default_factory=list)
    stats: JobStats = Field(default_factory=JobStats)
    error: Optional[str] = None

    @field_validator('status', mode='before')
    @classmethod
    def validate_status(cls, v):
        """Convert string to JobStatus enum, default to ACCEPTED if invalid."""
        if isinstance(v, JobStatus):
            return v
        try:
            return JobStatus(v)
        except (ValueError, TypeError):
            return JobStatus.ACCEPTED

    @field_validator('documents', mode='before')
    @classmethod
    def validate_documents(cls, v):
        """Ensure documents is a list and filter out invalid entries."""
        if not isinstance(v, list):
            return []
        
        valid_docs = []
        for doc in v:
            if isinstance(doc, dict) and all(k in doc for k in ['id', 'name', 'status']):
                try:
                    valid_docs.append(JobDocumentSummary(**doc))
                except Exception:
                    continue
            elif isinstance(doc, JobDocumentSummary):
                valid_docs.append(doc)
        return valid_docs

    @field_validator('stats', mode='before')
    @classmethod
    def validate_stats(cls, v):
        """Ensure stats is valid, return default if not."""
        if isinstance(v, JobStats):
            return v
        if isinstance(v, dict):
            try:
                return JobStats(**v)
            except Exception:
                return JobStats()
        return JobStats()

    class Config:
        """Pydantic configuration."""
        use_enum_values = True

    def to_dict(self) -> dict:
        """
        Serialize the job state to a JSON-compatible dictionary.
        
        Returns:
            Dictionary representation of the job state
        """
        return self.model_dump()

    def save(self, jobs_dir: Path = settings.digitize.jobs_dir) -> Path:
        """
        Persist the job state as <job_id>_status.json.

        Args:
            jobs_dir: Directory where the status file will be written.

        Returns:
            Path to the written status file.
        """
        jobs_dir.mkdir(parents=True, exist_ok=True)
        status_path = jobs_dir / f"{self.job_id}_status.json"
        with open(status_path, "w", encoding="utf-8") as f:
            f.write(self.model_dump_json(indent=4))
        return status_path
