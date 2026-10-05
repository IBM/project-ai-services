"""
Pydantic schemas for the invoice processing service API.
"""

from datetime import datetime
from typing import Any, Generic, List, Literal, Optional, TypeVar
from pydantic import BaseModel, Field

T = TypeVar("T")


class PaginationInfo(BaseModel):
    page: int = Field(default=1, ge=1)
    page_size: int = Field(default=20, ge=1, le=100)
    total_items: int = Field(default=0, ge=0)
    total_pages: int = Field(default=0, ge=0)


class PaginatedResponse(BaseModel, Generic[T]):
    data: List[T]
    pagination: PaginationInfo


class SubmitInvoiceResponse(BaseModel):
    job_id: str
    input_type: str
    status: str
    message: str = "Invoice submitted successfully"


class InvoiceJobDetailResponse(BaseModel):
    job_id: str
    filename: str
    input_type: str
    pipeline_path: Optional[str] = None
    status: str
    submitted_at: datetime
    completed_at: Optional[datetime] = None
    updated_at: datetime
    error: Optional[str] = None
    digitize_job_id: Optional[str] = None
    extract_job_id: Optional[str] = None
    staged_header: Optional[dict[str, Any]] = None
    staged_lines: Optional[list[dict[str, Any]]] = None
    interface_ref: Optional[str] = None
    job_metadata: Optional[dict[str, Any]] = None
