"""Job-related API endpoints.

Handles job creation, listing, retrieval, and deletion.

Exposes one router:
- ``router`` → mounted at ``/v1/extract``
"""



from fastapi import APIRouter

from common.misc_utils import get_logger

from extract.utils.schema import (
    SchemaValidationError
)


router = APIRouter()
logger = get_logger("jobs_router")

# ---------------------------------------------------------------------------
# Extraction stubs (POST /v1/extract, jobs CRUD) — implemented separately
# ---------------------------------------------------------------------------

@router.post("", tags=["extraction"], include_in_schema=True)
async def extract_sync():
    """Synchronous extraction — implementation in follow-up iteration."""
    raise SchemaValidationError("NOT_IMPLEMENTED", "POST /v1/extract not yet implemented.", status=501)


@router.post("/jobs", status_code=202, tags=["jobs"], include_in_schema=True)
async def create_extract_job():
    """Async extraction job — implementation in follow-up iteration."""
    raise SchemaValidationError("NOT_IMPLEMENTED", "POST /v1/extract/jobs not yet implemented.", status=501)


@router.get("/jobs", tags=["jobs"], include_in_schema=True)
async def list_extract_jobs():
    raise SchemaValidationError("NOT_IMPLEMENTED", "GET /v1/extract/jobs not yet implemented.", status=501)


@router.get("/jobs/{job_id}", tags=["jobs"], include_in_schema=True)
async def get_extract_job(job_id: str):
    raise SchemaValidationError("NOT_IMPLEMENTED", "GET /v1/extract/jobs/{job_id} not yet implemented.", status=501)


@router.get("/jobs/{job_id}/result", tags=["jobs"], include_in_schema=True)
async def get_extract_job_result(job_id: str):
    raise SchemaValidationError("NOT_IMPLEMENTED", "GET /v1/extract/jobs/{job_id}/result not yet implemented.", status=501)


@router.delete("/jobs/{job_id}", status_code=204, tags=["jobs"], include_in_schema=True)
async def delete_extract_job(job_id: str):
    raise SchemaValidationError("NOT_IMPLEMENTED", "DELETE /v1/extract/jobs/{job_id} not yet implemented.", status=501)


@router.delete("/jobs", status_code=204, tags=["jobs"], include_in_schema=True)
async def bulk_delete_extract_jobs():
    raise SchemaValidationError("NOT_IMPLEMENTED", "DELETE /v1/extract/jobs not yet implemented.", status=501)

