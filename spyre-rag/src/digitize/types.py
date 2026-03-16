from enum import Enum
from typing import List, Optional, Dict, Any, Union
from pydantic import BaseModel


class OutputFormat(str, Enum):
    TEXT = "txt"
    MD = "md"
    JSON = "json"


class OperationType(str, Enum):
    INGESTION = "ingestion"
    DIGITIZATION = "digitization"


class JobStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"


class DocStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    DIGITIZED = "digitized"
    PROCESSED = "processed"
    CHUNKED = "chunked"
    COMPLETED = "completed"
    FAILED = "failed"

class PaginationInfo(BaseModel):
    total: int
    limit: int
    offset: int

class JobsListResponse(BaseModel):
    pagination: PaginationInfo
    data: List[dict]


class DocumentListItem(BaseModel):
    """Minimal document information for list responses."""
    id: str
    name: str
    type: str
    status: str


class DocumentsListResponse(BaseModel):
    """Response model for documents list endpoint with pagination."""
    pagination: PaginationInfo
    data: List[DocumentListItem]


class DocumentDetailResponse(BaseModel):
    """Detailed document information response."""
    id: str
    job_id: Optional[str] = None
    name: str
    type: str
    status: str
    output_format: str
    submitted_at: Optional[str] = None
    completed_at: Optional[str] = None
    error: Optional[str] = None
    metadata: Optional[Dict[str, Any]] = None


class DocumentContentResponse(BaseModel):
    """Document content response with format information."""
    result: Union[Dict[str, Any], str]
    output_format: str
