from enum import Enum
from typing import List, Optional, Dict, Any, Union
from pydantic import BaseModel



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
