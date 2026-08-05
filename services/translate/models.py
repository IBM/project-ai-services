"""
Pydantic models and enums for the Translation service.
"""
from dataclasses import dataclass, field
from enum import Enum
from typing import Any, Dict, List, Optional

from pydantic import BaseModel, ConfigDict, Field, field_validator

class JobStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"


class InputType(str, Enum):
    TEXT = "text"
    TXT = "txt"
    MD = "md"

# ---------------------------------------------------------------------------
# Sync endpoint models
# ---------------------------------------------------------------------------

class SyncTranslateRequest(BaseModel):
    """Request body for POST /v1/translate."""

    text: str = Field(..., description="Plain text to translate. Must be non-empty.")
    source_language: str = Field(
        default="auto",
        description=(
            "Source language name (e.g. 'German'). "
            "Omit or pass 'auto' to let the service detect automatically."
        ),
    )
    target_language: str = Field(
        ..., description="Target language name (e.g. 'English'). Must not be 'auto'."
    )


class SyncTranslateResponse(BaseModel):
    """Response body for POST /v1/translate (200)."""

    data: Dict[str, Any]   # translation, source_language, target_language
    meta: Dict[str, Any]   # model, processing_time_ms, input_type
    usage: Dict[str, int]  # input_tokens, output_tokens, total_tokens


# ---------------------------------------------------------------------------
# Async job models
# ---------------------------------------------------------------------------

class JobCreatedResponse(BaseModel):
    """Response for job creation (202)."""

    job_id: str


class PaginationInfo(BaseModel):
    total: int
    limit: int
    offset: int


class JobDetailResponse(BaseModel):
    """Response for GET /v1/translate/jobs/{job_id}."""

    model_config = ConfigDict(use_enum_values=True)

    job_id: str
    job_name: Optional[str] = None
    status: JobStatus
    source_language: str
    target_language: str
    input_type: str
    document_name: Optional[str] = None
    submitted_at: str
    completed_at: Optional[str] = None
    error: Optional[str] = None
    job_metadata: Optional[Dict[str, Any]] = None

    @field_validator("status", mode="before")
    @classmethod
    def validate_status(cls, v):
        if isinstance(v, JobStatus):
            return v
        try:
            return JobStatus(v)
        except (ValueError, TypeError):
            return JobStatus.ACCEPTED


class JobResultResponse(BaseModel):
    """Response for GET /v1/translate/jobs/{job_id}/result."""

    data: Dict[str, Any]   # translation, source_language, target_language
    meta: Dict[str, Any]   # model, processing_time_ms, input_type
    usage: Dict[str, Any]  # input_tokens, output_tokens, total_tokens


class JobState(BaseModel):
    """Lightweight representation used in list responses."""

    model_config = ConfigDict(use_enum_values=True)

    job_id: str
    job_name: Optional[str] = None
    status: JobStatus
    source_language: str
    target_language: str
    input_type: str
    document_name: Optional[str] = None
    submitted_at: str
    completed_at: Optional[str] = None
    error: Optional[str] = None

    @field_validator("status", mode="before")
    @classmethod
    def validate_status(cls, v):
        if isinstance(v, JobStatus):
            return v
        try:
            return JobStatus(v)
        except (ValueError, TypeError):
            return JobStatus.ACCEPTED


class JobsListResponse(BaseModel):
    pagination: PaginationInfo
    data: List[JobState]


# ---------------------------------------------------------------------------
# Internal chunk dataclass (used by chunk_utils, not serialised directly)
# ---------------------------------------------------------------------------

@dataclass
class TranslationChunk:
    """Represents a single translation chunk produced by the chunker."""

    index: int
    text: str
    join_after: str = "paragraph"   # "paragraph" | "sentence"
    token_count: int = 0

