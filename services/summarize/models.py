from enum import Enum
from typing import List, Optional

from pydantic import BaseModel, Field, field_validator, ConfigDict


class SummarizationType(str, Enum):
    DIRECT = "direct"
    CHUNKED = "chunked"

class SummarizationLevel(str, Enum):
    BRIEF = "brief"
    STANDARD = "standard"
    DETAILED = "detailed"



class JobStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"

class PaginationInfo(BaseModel):
    total: int
    limit: int
    offset: int

class JobsListResponse(BaseModel):
    pagination: PaginationInfo
    data: List[dict]

class JobCreatedResponse(BaseModel):
    """Response model for job creation."""
    job_id: str

 


class JobMetadata(BaseModel):
    """Metadata for chunked summarization in a job."""
    model_config = ConfigDict(use_enum_values=True)
    
    total_chunks: int = Field(default=0, ge=0, description="Total number of chunks")
    completed_chunks: int = Field(default=0, ge=0, description="Number of completed summarized chunks")
    failed_chunks: int = Field(default=0, ge=0, description="Number of failed summarized chunks")
    phase: str = Field(default="", description="Phase: summarizing or merging")


class JobState(BaseModel):
    """
    Represents the overall state of a job. Job tracks overall progress and statistics.
    """
    model_config = ConfigDict(use_enum_values=True)
    
    job_id: str
    job_name: Optional[str] = None
    status: JobStatus
    submitted_at: str
    completed_at: Optional[str] = None
    updated_at: Optional[str] = None
    document_name: Optional[str] = None
    document_word_count: Optional[int] = 0
    level: Optional[str] = None
    job_type: Optional[str] = None
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

    def to_dict(self) -> dict:
        """
        Serialize the job state to a JSON-compatible dictionary.

        Returns:
            Dictionary representation of the job state
        """
        return self.model_dump()
