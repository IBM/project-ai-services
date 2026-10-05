"""
Invoice Processing v1 API endpoints.
"""

from typing import Optional
from fastapi import APIRouter, File, HTTPException, Query, UploadFile, status
from common.error_utils import APIError, ErrorCode
from common.misc_utils import get_logger

from models import (
    InvoiceJobDetailResponse,
    PaginatedResponse,
    SubmitInvoiceResponse,
)

logger = get_logger("api.v1.invoices")

router = APIRouter()


@router.post(
    "",
    response_model=SubmitInvoiceResponse,
    status_code=status.HTTP_501_NOT_IMPLEMENTED,
    summary="Submit invoice document for processing (Stub)",
)
async def submit_invoice(
    file: UploadFile = File(..., description="Invoice file (PDF or image)"),
):
    """Submit an invoice file for async processing."""
    raise HTTPException(
        status_code=status.HTTP_501_NOT_IMPLEMENTED,
        detail="Invoice submission endpoint is not implemented in boilerplate scaffold.",
    )


@router.get(
    "/{job_id}",
    response_model=InvoiceJobDetailResponse,
    status_code=status.HTTP_501_NOT_IMPLEMENTED,
    summary="Get invoice processing job detail (Stub)",
)
async def get_invoice_job(job_id: str):
    """Retrieve details and status for an invoice processing job."""
    raise HTTPException(
        status_code=status.HTTP_501_NOT_IMPLEMENTED,
        detail="Invoice job retrieval is not implemented in boilerplate scaffold.",
    )


@router.get(
    "",
    response_model=PaginatedResponse[InvoiceJobDetailResponse],
    status_code=status.HTTP_501_NOT_IMPLEMENTED,
    summary="List invoice processing jobs (Stub)",
)
async def list_invoice_jobs(
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    status_filter: Optional[str] = Query(default=None, alias="status"),
):
    """List invoice processing jobs with pagination."""
    raise HTTPException(
        status_code=status.HTTP_501_NOT_IMPLEMENTED,
        detail="Invoice job listing is not implemented in boilerplate scaffold.",
    )


@router.post(
    "/{job_id}/approve",
    status_code=status.HTTP_501_NOT_IMPLEMENTED,
    summary="Approve invoice for database loading (Stub)",
)
async def approve_invoice_job(job_id: str):
    """Approve staged invoice."""
    raise HTTPException(
        status_code=status.HTTP_501_NOT_IMPLEMENTED,
        detail="Invoice job approval is not implemented in boilerplate scaffold.",
    )


@router.post(
    "/{job_id}/reject",
    status_code=status.HTTP_501_NOT_IMPLEMENTED,
    summary="Reject staged invoice (Stub)",
)
async def reject_invoice_job(job_id: str):
    """Reject staged invoice."""
    raise HTTPException(
        status_code=status.HTTP_501_NOT_IMPLEMENTED,
        detail="Invoice job rejection is not implemented in boilerplate scaffold.",
    )
