"""
Pydantic models for job state representation.

These models are used for data validation and API responses,
converting database ORM models to structured dictionaries.
"""

from typing import List, Optional
from pydantic import BaseModel, Field, field_validator
from digitize.models import JobStatus


class JobDocumentSummary(BaseModel):
    """Compact per-document entry for job status responses."""
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
    Represents the overall state of a job for API responses.

    This model is used to validate and serialize job data from the database.
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


# Made with Bob
