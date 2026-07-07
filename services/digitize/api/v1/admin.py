"""
Admin-level API endpoints.

Handles metadata import and export operations.
These endpoints are mounted at ``/v1`` (not under ``/v1/jobs``)
because their paths are top-level by convention.

Exposes one router:
- ``router`` → mounted at ``/v1`` (for /import and /export)
"""

from fastapi import APIRouter, HTTPException, Query

from common.error_utils import APIError, ErrorCode, http_error_responses
from common.misc_utils import get_logger
import digitize.models as models
import digitize.utils.db as db_ops
import digitize.utils.jobs as dg_util

router = APIRouter()
logger = get_logger("admin_router")


MAX_IMPORT_RECORDS = -1  # -1 = no limit


@router.post(
    "/import",
    response_model=models.ImportResponse,
    responses={
        400: http_error_responses[400],
        409: http_error_responses[409],
        413: http_error_responses[413],
        500: http_error_responses[500],
    },
    summary="Import metadata into PostgreSQL",
    description="Import job and document metadata into PostgreSQL using the export-compatible JSON payload.",
    response_description="Import summary with imported, skipped, failed records and warnings",
)
async def import_metadata(payload: models.ImportRequest):
    """Import job and document metadata into PostgreSQL."""
    try:
        if not await db_ops.acquire_import_export_lock():
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "Another import/export operation is already in progress. "
                "Please wait for it to complete.",
            )

        try:
            total_records = len(payload.data.jobs) + len(payload.data.documents)
            if MAX_IMPORT_RECORDS != -1 and total_records > MAX_IMPORT_RECORDS:
                APIError.raise_error(
                    ErrorCode.CONTEXT_LIMIT_EXCEEDED,
                    f"Request contains {total_records} records, "
                    f"maximum allowed is {MAX_IMPORT_RECORDS}",
                )

            has_active, active_job_ids = dg_util.has_active_jobs()
            if has_active:
                APIError.raise_error(
                    ErrorCode.RESOURCE_LOCKED,
                    f"Cannot import while jobs are active. "
                    f"Active jobs: {', '.join(active_job_ids)}",
                )

            return db_ops.import_metadata(payload)
        finally:
            await db_ops.release_import_export_lock()

    except HTTPException:
        raise
    except ValueError as exc:
        logger.error(f"Invalid import request: {exc}")
        APIError.raise_error(ErrorCode.INVALID_REQUEST, str(exc))
    except Exception as exc:
        logger.error(f"Failed to import metadata: {exc}", exc_info=True)
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            "Database connection failed during import",
        )


@router.get(
    "/export",
    response_model=models.ExportResponse,
    responses={
        400: http_error_responses[400],
        413: http_error_responses[413],
        500: http_error_responses[500],
    },
    summary="Export metadata from PostgreSQL",
    description="Export job and document metadata from PostgreSQL as JSON for backup and restore workflows.",
    response_description="Exported metadata with summary and pagination details",
)
async def export_metadata(
    limit: int = Query(
        db_ops.IMPORT_EXPORT_DEFAULT_LIMIT,
        description="Maximum combined records to export. Use -1 to export all.",
    ),
    offset: int = Query(0, ge=0, description="Number of combined records to skip"),
):
    """Export job and document metadata from PostgreSQL."""
    import os

    pid = os.getpid()
    logger.warning(f"[PID {pid}] 🔵 Export request received (limit={limit}, offset={offset})")

    try:
        logger.info(f"[PID {pid}] Trying to acquire lock for export...")
        if not await db_ops.acquire_import_export_lock():
            logger.warning(f"[PID {pid}] ❌ Export REJECTED — lock held by another process")
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "Another import/export operation is already in progress. "
                "Please wait for it to complete.",
            )

        logger.warning(f"[PID {pid}] ✅ Export proceeding with lock acquired")
        try:
            if limit < -1 or limit == 0:
                APIError.raise_error(
                    ErrorCode.INVALID_REQUEST,
                    "limit must be -1 or a positive integer",
                )

            has_active, active_job_ids = dg_util.has_active_jobs()
            if has_active:
                APIError.raise_error(
                    ErrorCode.RESOURCE_LOCKED,
                    f"Cannot export while jobs are active. "
                    f"Active jobs: {', '.join(active_job_ids)}",
                )

            logger.info(f"[PID {pid}] Starting export operation...")
            result = db_ops.export_metadata(limit=limit, offset=offset)
            logger.warning(f"[PID {pid}] ✅ Export completed successfully")
            return result
        finally:
            await db_ops.release_import_export_lock()

    except HTTPException:
        raise
    except ValueError as exc:
        logger.error(f"Invalid export request: {exc}")
        APIError.raise_error(ErrorCode.INVALID_REQUEST, str(exc))
    except Exception as exc:
        logger.error(f"Failed to export metadata: {exc}", exc_info=True)
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            "Database query failed during export",
        )
