from pathlib import Path
from typing import List, Optional

from pydantic import BaseModel, Field, field_validator

from summarize.summ_types import JobStatus
from summarize.settings import settings



class JobMetadata(BaseModel):
    """Metadata for chunked summarization in a job."""
    total_chunks: int = Field(default=0, ge=0, description="Total number of chunks")
    completed_chunks: int = Field(default=0, ge=0, description="Number of completed summarized chunks")
    failed_chunks: int = Field(default=0, ge=0, description="Number of failed summarized chunks")
    phase: str = Field(description="Phase: summarizing or merging")

    class Config:
        """Pydantic configuration."""
        use_enum_values = True


class JobState(BaseModel):
    """
    Represents the overall state of a job. Job tracks overall progress and statistics.
    """
    job_id: str
    job_name: Optional[str] = None
    status: JobStatus
    submitted_at: str
    completed_at: Optional[str] = None
    updated_at: Optional[str] = None
    document_name: Optional[str] = None
    document_word_count: Optional[int]= 0,
    level: Optional[str]= None,
    job_type: Optional[str]= None,
    metadata: JobMetadata = Field(default_factory=JobMetadata)
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


    @field_validator('metadata', mode='before')
    @classmethod
    def validate_stats(cls, v):
        """Ensure stats is valid, return default if not."""
        if isinstance(v, JobMetadata):
            return v
        if isinstance(v, dict):
            try:
                return JobMetadata(**v)
            except Exception:
                return JobMetadata()
        return JobMetadata()

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