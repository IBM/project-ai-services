"""
Pydantic request/response models for the Extract Information service.
"""

from datetime import datetime
from typing import Any, Dict, List, Optional

from pydantic import BaseModel, ConfigDict, Field, field_validator


# ---------------------------------------------------------------------------
# Enumerations (string-valued for JSON serialisation)
# ---------------------------------------------------------------------------

class JobStatus(str):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"


# ---------------------------------------------------------------------------
# Schema registry — request body
# ---------------------------------------------------------------------------

class ExampleItem(BaseModel):
    """A single few-shot example stored with a schema."""

    text: str = Field(..., description="Source text for this example")
    output: Dict[str, Any] = Field(..., description="Expected extraction output")


class SchemaRegisterRequest(BaseModel):
    """Request body for POST /v1/schemas."""

    name: str = Field(
        ...,
        min_length=1,
        max_length=200,
        pattern=r"^[a-zA-Z0-9._-]+$",
        description="Unique, human-readable schema name",
    )
    description: Optional[str] = Field(
        None, description="Free-text description of what the schema extracts"
    )
    json_schema: Dict[str, Any] = Field(
        ..., description="JSON Schema draft 2020-12.  Root must be type:object."
    )
    examples: Optional[List[ExampleItem]] = Field(
        None,
        max_length=5,
        description="Up to 5 few-shot examples",
    )
    custom_prompt: Optional[str] = Field(
        None,
        max_length=2000,
        description="Extra instructions appended to the system prompt",
    )


# ---------------------------------------------------------------------------
# Schema registry — response models
# ---------------------------------------------------------------------------

class SchemaCreatedResponse(BaseModel):
    """Response body for POST /v1/schemas (201 Created)."""

    schema_id: str
    name: str
    description: Optional[str] = None
    created_at: str


class SchemaListItem(BaseModel):
    """A single row in the GET /v1/schemas list (schema body omitted)."""

    schema_id: str
    name: str
    description: Optional[str] = None
    example_count: int = 0
    schema_tokens: int = 0
    examples_tokens: int = 0
    custom_prompt_tokens: int = 0
    created_at: str


class PaginationInfo(BaseModel):
    total: int
    limit: int
    offset: int


class SchemaListResponse(BaseModel):
    """Response body for GET /v1/schemas."""

    pagination: PaginationInfo
    data: List[SchemaListItem]


class SchemaDetailResponse(BaseModel):
    """Response body for GET /v1/schemas/{schema_id}."""

    schema_id: str
    name: str
    description: Optional[str] = None
    json_schema: Dict[str, Any]
    examples: Optional[List[Dict[str, Any]]] = None
    custom_prompt: Optional[str] = None
    schema_tokens: int
    examples_tokens: int
    custom_prompt_tokens: int
    created_at: str


# ---------------------------------------------------------------------------
# Extraction jobs — response models
# ---------------------------------------------------------------------------

class JobCreatedResponse(BaseModel):
    """Response body for POST /v1/extract/jobs (202 Accepted)."""
    job_id: str


class DocumentInfo(BaseModel):
    """Inline document info embedded in job detail responses."""
    name: str
    source_type: str
    digitize_job_id: Optional[str] = None
    digitize_doc_id: Optional[str] = None


class JobDetailResponse(BaseModel):
    """Response body for GET /v1/extract/jobs/{job_id}."""

    model_config = ConfigDict(use_enum_values=True)

    job_id: str
    job_name: Optional[str] = None
    schema_id: str
    status: str
    document: DocumentInfo
    metadata: Optional[Dict[str, Any]] = None
    submitted_at: str
    completed_at: Optional[str] = None
    error: Optional[str] = None


class JobListItem(BaseModel):
    """A single row in the GET /v1/extract/jobs list."""

    job_id: str
    job_name: Optional[str] = None
    schema_id: str
    status: str
    document_name: str
    submitted_at: str
    completed_at: Optional[str] = None


class JobsListResponse(BaseModel):
    pagination: PaginationInfo
    data: List[JobListItem]


class JobResultResponse(BaseModel):
    data: Dict[str, Any]
    status: str
    meta: Dict[str, Any]
    usage: Dict[str, Any]

# Made with Bob
